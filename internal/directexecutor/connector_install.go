package directexecutor

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/kakj-go/Argus/internal/collectormanager"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/operationsecret"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

const connectorInstallTimeout = 10 * time.Minute

var (
	errControlTunnelUnavailable = errors.New("connector control tunnel is unavailable")
	errTunnelQuotaExceeded      = errors.New("tunnel quota exceeded")
	errCredentialVersionStale   = errors.New("credential version is stale")
)

func (executor *Executor) runConnectorInstallLoop(ctx context.Context) {
	ticker := time.NewTicker(700 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = executor.Store.Queries.RecoverConnectorInstallOperations(ctx)
			_, _ = executor.Store.Queries.ExpireConnectorInstallOperations(ctx)
			reserved := executor.reserveAvailable()
			if reserved == 0 {
				continue
			}
			operations, err := executor.Store.Queries.ClaimConnectorInstallOperations(ctx, db.ClaimConnectorInstallOperationsParams{
				Limit: int32(reserved), LeaseOwner: executor.InstanceID,
			})
			if err != nil {
				executor.release(reserved)
				continue
			}
			executor.release(reserved - len(operations))
			for _, operation := range operations {
				go func(value db.ConnectorInstallOperation) {
					defer executor.release(1)
					executor.executeConnectorInstall(ctx, value)
				}(operation)
			}
		}
	}
}

func (executor *Executor) executeConnectorInstall(parent context.Context, operation db.ConnectorInstallOperation) {
	ctx, cancel := context.WithDeadline(parent, operation.ExpiresAt.Time)
	defer cancel()
	leaseDone := make(chan struct{})
	go executor.renewConnectorInstallLease(ctx, operation, leaseDone)
	defer close(leaseDone)

	command, material, err := executor.loadConnectorInstallPlan(ctx, operation)
	if err != nil {
		executor.retryOrFinishConnectorInstall(ctx, operation, connectorInstallErrorCode(err), err)
		return
	}
	executor.appendConnectorInstallEvent(ctx, operation, "queued", "succeeded", "")
	executor.appendConnectorInstallEvent(ctx, operation, "ssh_connecting", "started", "")
	if err = executor.installConnectorOverSSH(ctx, &operation, command, material); err != nil {
		executor.retryOrFinishConnectorInstall(ctx, operation, connectorInstallErrorCode(err), err)
		return
	}
	if err = executor.waitConnectorOnline(ctx, &operation); err != nil {
		executor.retryOrFinishConnectorInstall(ctx, operation, connectorInstallErrorCode(err), err)
		return
	}
	_, _ = executor.Store.Queries.MarkConnectorInstallOnline(ctx, db.MarkConnectorInstallOnlineParams{ID: operation.ID, EnterpriseID: operation.EnterpriseID})
	executor.finishConnectorInstall(ctx, operation, "succeeded", "")
}

func (executor *Executor) loadConnectorInstallPlan(ctx context.Context, operation db.ConnectorInstallOperation) (*connectorv1.ConnectorInstallCommand, operationsecret.Material, error) {
	canonical, err := resource.CanonicalJSON(operation.Plan)
	if err != nil {
		return nil, operationsecret.Material{}, err
	}
	hash := sha256.Sum256(canonical)
	if !strings.EqualFold(fmt.Sprintf("%x", hash), fmt.Sprintf("%x", operation.PlanHash)) {
		return nil, operationsecret.Material{}, errors.New("connector install plan hash mismatch")
	}
	var command connectorv1.ConnectorInstallCommand
	if protojson.Unmarshal(canonical, &command) != nil || (command.GetOperation() != "install" && command.GetOperation() != "replace") ||
		command.GetConnectorId() != operation.ConnectorID.String() || command.GetBastionScopeId() != operation.BastionScopeID.String() ||
		command.GetHostId() != operation.HostID.String() || command.GetEnrollmentEndpoint() == "" || command.GetArtifact() == nil ||
		command.GetReleaseVersionId() != operation.ReleaseVersionID.String() {
		return nil, operationsecret.Material{}, errors.New("connector install plan is invalid")
	}
	if _, err = connectorInstallTrustBundle(&command); err != nil {
		return nil, operationsecret.Material{}, err
	}
	secretRecord, err := executor.Store.Queries.GetConnectorInstallOperationSecret(ctx, db.GetConnectorInstallOperationSecretParams{
		OperationID: operation.ID, EnterpriseID: operation.EnterpriseID,
	})
	if err != nil || secretRecord.KeyVersion != operationsecret.KeyVersion {
		return nil, operationsecret.Material{}, errors.New("connector install secret is unavailable")
	}
	material, err := operationsecret.Decrypt(executor.OperationSecretKey, secretRecord.Nonce, secretRecord.Ciphertext,
		operation.EnterpriseID, operation.ID)
	if err != nil {
		return nil, operationsecret.Material{}, err
	}
	return &command, material, nil
}

func (executor *Executor) installConnectorOverSSH(ctx context.Context, operation *db.ConnectorInstallOperation, command *connectorv1.ConnectorInstallCommand, material operationsecret.Material) error {
	publicKey, err := connectorInstallSigningKey(command)
	if err != nil {
		return err
	}
	installTrust, err := connectorInstallTrustBundle(command)
	if err != nil {
		return err
	}
	addresses, err := executor.Validator.Resolve(ctx, command.GetTargetAddress())
	if err != nil || len(addresses) == 0 {
		return resource.ErrDirectTargetDenied
	}
	credentialID, err := uuid.Parse(command.GetCredentialId())
	if err != nil || command.GetCredentialVersion() < 1 {
		return errors.New("connector install credential reference is invalid")
	}
	credential, err := executor.Store.Queries.GetCredential(ctx, db.GetCredentialParams{
		ID: credentialID, EnterpriseID: operation.EnterpriseID,
	})
	if err != nil || credential.Status != "active" {
		return secret.ErrCredentialUnavailable
	}
	if credential.Version != int64(command.GetCredentialVersion()) {
		return errCredentialVersionStale
	}
	lease, err := executor.Secrets.IssueLease(secret.WithActorType(ctx, "direct_executor"), executor.InstanceID, operation.EnterpriseID, secret.LeaseRequest{
		CredentialID: credentialID, OperationRef: "connector_install:" + operation.ID.String(), TargetResourceType: "host",
		TargetResourceID: operation.HostID, RecipientType: "direct_executor", RecipientID: executor.InstanceID,
		Protocol: "ssh", TTL: secret.MaxLeaseTTL,
	})
	if err != nil {
		return err
	}
	defer clear(lease.Value)
	client, err := executor.dialPinnedSSH(ctx, addresses[0], int32(command.GetTargetPort()), command.GetTargetUsername(),
		command.GetPinnedHostKey(), lease.Value)
	if err != nil {
		return err
	}
	defer client.Close()
	if err = executor.Validator.Revalidate(ctx, command.GetTargetAddress(), addresses); err != nil {
		return err
	}
	if err = executor.advanceConnectorInstall(ctx, operation, "artifact_verifying"); err != nil {
		return err
	}
	artifactFile, err := os.CreateTemp("", ".argus-connector-artifact-*")
	if err != nil {
		return err
	}
	artifactPath := artifactFile.Name()
	defer func() {
		_ = artifactFile.Close()
		_ = os.Remove(artifactPath)
	}()
	if err = artifactFile.Chmod(0o600); err != nil {
		return err
	}
	if err = (collectormanager.Manager{HTTPClient: executor.CollectorArtifactHTTPClient,
		TrustedSigningKeys: map[string]ed25519.PublicKey{command.GetArtifact().GetSigningKeyId(): publicKey}}).
		FetchArtifactTo(ctx, command.GetArtifact(), artifactFile); err != nil {
		return err
	}
	if _, err = artifactFile.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err = executor.advanceConnectorInstall(ctx, operation, "artifact_transferring"); err != nil {
		return err
	}
	upload := `umask 077; install -d -m 0755 /usr/local/bin; tmp=$(mktemp /usr/local/bin/.argus-connector.XXXXXX); cat > "$tmp"; chmod 0755 "$tmp"; mv -f "$tmp" /usr/local/bin/argus-connector`
	if err = runSSHCommandReader(client, upload, artifactFile); err != nil {
		return err
	}
	if err = executor.advanceConnectorInstall(ctx, operation, "service_installing"); err != nil {
		return err
	}
	prepareRuntime := `umask 077; id argus-connector >/dev/null 2>&1 || useradd --system --home-dir /var/lib/argus-connector --shell /usr/sbin/nologin argus-connector; install -d -m 0755 /etc/argus-connector; install -d -m 0700 -o argus-connector -g argus-connector /var/lib/argus-connector; install -d -m 0700 /var/lib/argus-otelcol /etc/argus-otelcol`
	if err = runSSHCommand(client, prepareRuntime, nil); err != nil {
		return err
	}
	trust, _ := json.Marshal(map[string]string{command.GetArtifact().GetSigningKeyId(): command.GetArtifactSigningPublicKey()})
	writeTrust := `umask 077; tmp=$(mktemp /etc/argus-connector/.otelcol-signing-keys.XXXXXX); cat > "$tmp"; chmod 0644 "$tmp"; mv -f "$tmp" /etc/argus-connector/otelcol-signing-keys.json`
	if err = runSSHCommand(client, writeTrust, trust); err != nil {
		return err
	}
	writeServerCA := `umask 077; tmp=$(mktemp /etc/argus-connector/.server-ca.XXXXXX); cat > "$tmp"; chmod 0644 "$tmp"; mv -f "$tmp" /etc/argus-connector/server-ca.pem`
	if err = runSSHCommand(client, writeServerCA, installTrust.PEM); err != nil {
		return err
	}
	hasArtifactCA := len(executor.CollectorArtifactCABundle) > 0
	if hasArtifactCA {
		if len(executor.CollectorArtifactCABundle) > 1<<20 {
			return errors.New("collector artifact CA bundle is too large")
		}
		writeCA := `umask 077; tmp=$(mktemp /etc/argus-connector/.otelcol-artifact-ca.XXXXXX); cat > "$tmp"; chmod 0644 "$tmp"; mv -f "$tmp" /etc/argus-connector/otelcol-artifact-ca.pem`
		if err = runSSHCommand(client, writeCA, executor.CollectorArtifactCABundle); err != nil {
			return err
		}
	} else if err = runSSHCommand(client, `rm -f /etc/argus-connector/otelcol-artifact-ca.pem`, nil); err != nil {
		return err
	}
	unit := connectorSystemdUnit(command, hasArtifactCA)
	writeUnit := `umask 077; tmp=$(mktemp /etc/systemd/system/.argus-connector.XXXXXX); cat > "$tmp"; chmod 0644 "$tmp"; mv -f "$tmp" /etc/systemd/system/argus-connector.service; systemctl daemon-reload`
	if err = runSSHCommand(client, writeUnit, []byte(unit)); err != nil {
		return err
	}
	if operation.InstallMode == "direct_install_tunnel" {
		if err = executor.advanceConnectorInstall(ctx, operation, "control_tunnel_establishing"); err != nil {
			return err
		}
		if err = executor.waitControlTunnelEstablished(ctx, *operation); err != nil {
			return err
		}
	}
	if err = executor.advanceConnectorInstall(ctx, operation, "enrolling"); err != nil {
		return err
	}
	if command.GetOperation() == "replace" {
		prepareIdentity := `set -eu; root=/var/lib/argus-connector; marker="$root/.desired-connector-id"; current=""; [ -f "$marker" ] && current=$(cat "$marker"); if [ "$current" != ` + shellQuote(command.GetConnectorId()) + ` ]; then systemctl disable --now argus-connector.service >/dev/null 2>&1 || true; rm -rf "$root"; install -d -m 0700 -o argus-connector -g argus-connector "$root"; printf '%s' ` + shellQuote(command.GetConnectorId()) + ` > "$marker"; chown argus-connector:argus-connector "$marker"; fi`
		if err = runSSHCommand(client, prepareIdentity, nil); err != nil {
			return err
		}
	} else {
		marker := `printf '%s' ` + shellQuote(command.GetConnectorId()) + ` > /var/lib/argus-connector/.desired-connector-id; chown argus-connector:argus-connector /var/lib/argus-connector/.desired-connector-id`
		if err = runSSHCommand(client, marker, nil); err != nil {
			return err
		}
	}
	enroll := connectorEnrollCommand(command, material.EnrollmentToken)
	if err = runSSHCommand(client, enroll, nil); err != nil {
		return err
	}
	_, _ = executor.Store.Queries.ConsumeConnectorInstallOperationSecret(ctx, db.ConsumeConnectorInstallOperationSecretParams{
		OperationID: operation.ID, EnterpriseID: operation.EnterpriseID,
	})
	activate := "chown -R argus-connector:argus-connector /var/lib/argus-connector; systemctl enable --now argus-connector.service; systemctl is-active --quiet argus-connector.service"
	if err = runSSHCommand(client, activate, nil); err != nil {
		return err
	}
	return executor.advanceConnectorInstall(ctx, operation, "waiting_connector_online")
}

func connectorEnrollCommand(command *connectorv1.ConnectorInstallCommand, token string) string {
	value := "/usr/local/bin/argus-connector enroll --connector-id " + shellQuote(command.GetConnectorId()) +
		" --token " + shellQuote(token) + " --server " + shellQuote(command.GetEnrollmentEndpoint()) +
		" --ca-file /etc/argus-connector/server-ca.pem --role bastion"
	if command.GetEnrollDialAddress() != "" {
		value = "ARGUS_CONNECTOR_ENROLL_ADDRESS=" + shellQuote(command.GetEnrollDialAddress()) + " " + value
	}
	return value
}

func connectorInstallTrustBundle(command *connectorv1.ConnectorInstallCommand) (trustbundle.Material, error) {
	if command == nil || command.GetTrustBundleEpoch() < 1 || len(command.GetTrustBundlePem()) == 0 ||
		len(command.GetTrustBundlePem()) > 1<<20 || command.GetTrustBundleSha256() == "" ||
		len(command.GetTrustBundleCaFingerprints()) == 0 {
		return trustbundle.Material{}, errors.New("connector install Trust Bundle is invalid")
	}
	material, err := trustbundle.Parse(command.GetTrustBundlePem(), time.Now().UTC())
	if err != nil || material.SHA256 != command.GetTrustBundleSha256() ||
		!slices.Equal(material.Fingerprints, command.GetTrustBundleCaFingerprints()) {
		return trustbundle.Material{}, errors.New("connector install Trust Bundle digest or fingerprints do not match")
	}
	return material, nil
}

func connectorSystemdUnit(command *connectorv1.ConnectorInstallCommand, hasArtifactCA bool) string {
	environment := "Environment=ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS_FILE=/etc/argus-connector/otelcol-signing-keys.json\n"
	if hasArtifactCA {
		environment += "Environment=ARGUS_OTELCOL_ARTIFACT_CA_PATH=/etc/argus-connector/otelcol-artifact-ca.pem\n"
	}
	if command.GetEnrollDialAddress() != "" {
		environment += fmt.Sprintf("Environment=ARGUS_CONNECTOR_ENROLL_ADDRESS=%s\n", command.GetEnrollDialAddress())
	}
	if command.GetGatewayDialAddress() != "" {
		environment += fmt.Sprintf("Environment=ARGUS_CONNECTOR_DIAL_ADDRESS=%s\n", command.GetGatewayDialAddress())
	}
	return fmt.Sprintf(`[Unit]
Description=Argus Connector
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
%sExecStart=/usr/local/bin/argus-connector run
Restart=always
RestartSec=5
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
ReadWritePaths=/var/lib/argus-connector /etc/argus-connector /var/lib/argus-otelcol /etc/argus-otelcol /usr/local/bin /etc/systemd/system /run/systemd/system

[Install]
WantedBy=multi-user.target
`, environment)
}

func connectorInstallSigningKey(command *connectorv1.ConnectorInstallCommand) (ed25519.PublicKey, error) {
	if command == nil || command.GetArtifact() == nil || command.GetArtifact().GetSigningKeyId() == "" {
		return nil, errors.New("connector install signing trust is unavailable")
	}
	decoded, err := base64.RawStdEncoding.DecodeString(command.GetArtifactSigningPublicKey())
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		return nil, errors.New("connector install signing trust is invalid")
	}
	return ed25519.PublicKey(decoded), nil
}

func (executor *Executor) waitControlTunnelEstablished(ctx context.Context, operation db.ConnectorInstallOperation) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		tunnel, err := executor.Store.Queries.GetConnectorControlTunnelByConnector(ctx, db.GetConnectorControlTunnelByConnectorParams{
			ConnectorID: operation.ConnectorID, EnterpriseID: operation.EnterpriseID,
		})
		if err == nil && tunnel.Status == "established" && tunnel.Epoch > 0 {
			return nil
		}
		if err == nil && tunnel.Status == "down" {
			switch tunnel.LastDropReason {
			case "tunnel_quota_exceeded":
				return errTunnelQuotaExceeded
			case "host_key_changed":
				return errHostKeyMismatch
			case "credential_revoked", "credential_unavailable":
				return secret.ErrCredentialUnavailable
			case "target_resolution_failed", "target_revalidation_failed":
				return resource.ErrDirectTargetDenied
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (executor *Executor) waitConnectorOnline(ctx context.Context, operation *db.ConnectorInstallOperation) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		connector, err := executor.Store.Queries.GetConnector(ctx, db.GetConnectorParams{ID: operation.ConnectorID, EnterpriseID: operation.EnterpriseID})
		if err == nil && connector.Status == "online" && connector.ConnectionEpoch > 0 {
			if operation.InstallMode != "direct_install_tunnel" {
				return executor.advanceConnectorInstall(ctx, operation, "completed")
			}
			tunnel, tunnelErr := executor.Store.Queries.GetConnectorControlTunnelByConnector(ctx, db.GetConnectorControlTunnelByConnectorParams{
				ConnectorID: operation.ConnectorID, EnterpriseID: operation.EnterpriseID,
			})
			if tunnelErr == nil && tunnel.Status == "established" {
				return executor.advanceConnectorInstall(ctx, operation, "completed")
			}
			if tunnelErr == nil && tunnel.Status == "down" && tunnel.LastDropReason == "tunnel_quota_exceeded" {
				return errTunnelQuotaExceeded
			}
		} else if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (executor *Executor) advanceConnectorInstall(ctx context.Context, operation *db.ConnectorInstallOperation, stage string) error {
	previous := operation.Stage
	updated, err := executor.Store.Queries.AdvanceConnectorInstallOperation(ctx, db.AdvanceConnectorInstallOperationParams{
		ID: operation.ID, EnterpriseID: operation.EnterpriseID, Fence: operation.Fence, Stage: stage,
	})
	if err == nil {
		executor.appendConnectorInstallEvent(ctx, *operation, previous, "succeeded", "")
		*operation = updated
		executor.appendConnectorInstallEvent(ctx, *operation, stage, "started", "")
	}
	return err
}

func (executor *Executor) appendConnectorInstallEvent(ctx context.Context, operation db.ConnectorInstallOperation, stage, status, errorCode string) {
	events, err := executor.Store.Queries.ListConnectorInstallOperationEvents(ctx, db.ListConnectorInstallOperationEventsParams{
		OperationID: operation.ID, EnterpriseID: operation.EnterpriseID,
	})
	if err != nil {
		return
	}
	sequence := int64(1)
	if len(events) > 0 {
		sequence = events[len(events)-1].Sequence + 1
	}
	_, _ = executor.Store.Queries.CreateConnectorInstallOperationEvent(ctx, db.CreateConnectorInstallOperationEventParams{
		ID: uuid.New(), OperationID: operation.ID, EnterpriseID: operation.EnterpriseID, Sequence: sequence,
		Stage: stage, Status: status, ErrorCode: pgtype.Text{String: errorCode, Valid: errorCode != ""},
	})
}

func (executor *Executor) retryOrFinishConnectorInstall(ctx context.Context, operation db.ConnectorInstallOperation, errorCode string, cause error) {
	slog.Warn("connector install attempt failed", "operation_id", operation.ID, "stage", operation.Stage,
		"attempt", operation.Attempts, "error_code", errorCode, "error", cause)
	if operation.Attempts < 3 && time.Now().UTC().Before(operation.ExpiresAt.Time) {
		if _, err := executor.Store.Queries.RequeueConnectorInstallOperation(ctx, db.RequeueConnectorInstallOperationParams{
			ID: operation.ID, EnterpriseID: operation.EnterpriseID, Fence: operation.Fence,
			ErrorCode: pgtype.Text{String: errorCode, Valid: true},
		}); err == nil {
			executor.appendConnectorInstallEvent(ctx, operation, operation.Stage, "retrying", errorCode)
			return
		}
	}
	executor.appendConnectorInstallEvent(ctx, operation, operation.Stage, "failed", errorCode)
	executor.finishConnectorInstall(ctx, operation, "failed", errorCode)
}

func (executor *Executor) finishConnectorInstall(ctx context.Context, operation db.ConnectorInstallOperation, status, errorCode string) {
	_, err := executor.Store.Queries.FinishConnectorInstallOperation(ctx, db.FinishConnectorInstallOperationParams{
		ID: operation.ID, EnterpriseID: operation.EnterpriseID, Fence: operation.Fence,
		Status: status, ErrorCode: pgtype.Text{String: errorCode, Valid: errorCode != ""},
	})
	if err != nil {
		slog.Error("finish connector install failed", "operation_id", operation.ID, "error", err)
		return
	}
	stage := operation.Stage
	if status == "succeeded" {
		stage = "completed"
	}
	executor.appendConnectorInstallEvent(ctx, operation, stage, status, errorCode)
}

func (executor *Executor) renewConnectorInstallLease(ctx context.Context, operation db.ConnectorInstallOperation, done <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			rows, _ := executor.Store.Queries.RenewConnectorInstallOperationLease(ctx, db.RenewConnectorInstallOperationLeaseParams{
				ID: operation.ID, EnterpriseID: operation.EnterpriseID, Fence: operation.Fence, LeaseOwner: executor.InstanceID,
			})
			if rows == 0 {
				return
			}
		}
	}
}

func connectorInstallErrorCode(err error) string {
	switch {
	case errors.Is(err, collectormanager.ErrArtifactInvalid):
		return "CONNECTOR_ARTIFACT_INVALID"
	case errors.Is(err, resource.ErrDirectTargetDenied):
		return "CONNECTOR_INSTALL_TARGET_DENIED"
	case errors.Is(err, errHostKeyMismatch):
		return "SSH_HOST_KEY_CHANGED"
	case errors.Is(err, errTunnelQuotaExceeded):
		return "TUNNEL_QUOTA_EXCEEDED"
	case errors.Is(err, secret.ErrCredentialUnavailable):
		return "CREDENTIAL_UNAVAILABLE"
	case errors.Is(err, errCredentialVersionStale):
		return "CREDENTIAL_VERSION_STALE"
	case errors.Is(err, errControlTunnelUnavailable):
		return "CONTROL_TUNNEL_UNAVAILABLE"
	case errors.Is(err, context.DeadlineExceeded):
		return "CONNECTOR_INSTALL_TIMEOUT"
	default:
		return "CONNECTOR_INSTALL_FAILED"
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
