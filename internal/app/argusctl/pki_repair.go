package argusctl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

const pkiRepairTokenTTL = 10 * time.Minute

type pkiRepairInstruction struct {
	NodeKind        string    `json:"nodeKind"`
	NodeID          string    `json:"nodeId"`
	Scope           string    `json:"scope"`
	Epoch           int64     `json:"epoch"`
	SHA256          string    `json:"sha256"`
	TokenExpiresAt  time.Time `json:"tokenExpiresAt"`
	Command         string    `json:"command"`
	SecurityWarning string    `json:"securityWarning"`
}

func (a *App) pkiRepairCommand(ctx context.Context, cfg *InstallConfig, nodeKind, nodeID, scope, targetNamespace, output string) error {
	if nodeKind != "connector" && nodeKind != "collector" && nodeKind != "kubernetes_connector" {
		return errors.New("pki repair-command requires --node-kind connector|collector|kubernetes_connector")
	}
	id, err := uuid.Parse(strings.TrimSpace(nodeID))
	if err != nil {
		return errors.New("pki repair-command requires a UUID --node-id")
	}
	if scope != "linux-system" && scope != "linux-user" && scope != "kubernetes" {
		return errors.New("pki repair-command --scope must be linux-system, linux-user, or kubernetes")
	}
	if scope == "kubernetes" && (len(targetNamespace) > 63 || !dnsLabel.MatchString(targetNamespace)) {
		return errors.New("pki repair-command requires a valid --target-namespace")
	}
	session, err := a.openPKISession(ctx, cfg)
	if err != nil {
		return err
	}
	defer session.Close()
	current, err := (trustbundle.Service{Store: session.store}).Current(ctx)
	if err != nil {
		return err
	}
	var command string
	var expiresAt time.Time
	if nodeKind == "connector" || nodeKind == "kubernetes_connector" {
		requiredRole := "bastion"
		if nodeKind == "kubernetes_connector" {
			requiredRole = "kubernetes"
			if scope != "kubernetes" {
				return errors.New("kubernetes_connector repair requires --scope kubernetes")
			}
		} else if scope == "kubernetes" {
			return errors.New("connector repair requires a Linux scope")
		}
		connector, token, expiry, issueErr := issueConnectorRepairToken(ctx, session, id, requiredRole)
		if issueErr != nil {
			return issueErr
		}
		expiresAt = expiry
		if nodeKind == "kubernetes_connector" {
			command = kubernetesConnectorRepairCommand(cfg, connector.ID, token, current, targetNamespace)
		} else {
			command = linuxConnectorRepairCommand(cfg, token, current, scope)
		}
	} else {
		collector, token, expiry, issueErr := issueCollectorRepairToken(ctx, session, id, scope)
		if issueErr != nil {
			return issueErr
		}
		expiresAt = expiry
		switch collector.ResourceType {
		case "kubernetes_cluster":
			command = kubernetesCollectorRepairCommand(cfg, collector.ID, token, current, targetNamespace)
		case "host":
			command = linuxCollectorRepairCommand(cfg, token, current, scope)
		default:
			return fmt.Errorf("Collector resource type %q cannot be repaired", collector.ResourceType)
		}
	}
	result := pkiRepairInstruction{NodeKind: nodeKind, NodeID: id.String(), Scope: scope, Epoch: current.Epoch,
		SHA256: current.Material.SHA256, TokenExpiresAt: expiresAt, Command: command,
		SecurityWarning: "The command contains a one-time enrollment token. Run it before expiry and remove it from shell history after use."}
	return writeOutput(a.stdout, output, result, func(writer io.Writer) {
		_, _ = fmt.Fprintf(writer, "WARNING: %s\nToken expires at %s.\n\n%s\n", result.SecurityWarning, expiresAt.Format(time.RFC3339), command)
	})
}

func issueConnectorRepairToken(ctx context.Context, session *pkiSession, connectorID uuid.UUID, requiredRole string) (db.Connector, string, time.Time, error) {
	connector, err := session.store.Queries.GetConnectorByID(ctx, connectorID)
	if err != nil || connector.Role != requiredRole || connector.Status == "uninstalled" || connector.Status == "revoked" {
		return db.Connector{}, "", time.Time{}, errors.New("repair Connector does not exist or is no longer active")
	}
	token, err := identity.RandomToken(32)
	if err != nil {
		return db.Connector{}, "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(pkiRepairTokenTTL)
	policy, _ := json.Marshal(map[string]any{"capabilities": connector.Capabilities, "purpose": "pki_repair"})
	err = session.store.InTx(ctx, func(q *db.Queries) error {
		target := connector.BastionScopeID
		if connector.Role == "kubernetes" {
			target = connector.KubernetesClusterID
		}
		if !target.Valid {
			return errors.New("repair Connector binding is invalid")
		}
		if err := q.RevokeActiveEnrollmentTokens(ctx, db.RevokeActiveEnrollmentTokensParams{EnterpriseID: connector.EnterpriseID, BastionScopeID: target}); err != nil {
			return err
		}
		_, err := q.CreateConnectorEnrollmentToken(ctx, db.CreateConnectorEnrollmentTokenParams{ID: uuid.New(),
			PreallocatedConnectorID: connector.ID, EnterpriseID: connector.EnterpriseID, Role: connector.Role, Purpose: "pki_repair",
			BastionScopeID: connector.BastionScopeID, KubernetesClusterID: connector.KubernetesClusterID,
			PreallocatedHostID: connector.HostID, TokenHash: identity.TokenHash(token), Policy: policy,
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}, CreatedBy: uuid.Nil, ReleaseVersionID: uuid.NullUUID{}})
		return err
	})
	return connector, token, expiresAt, err
}

func issueCollectorRepairToken(ctx context.Context, session *pkiSession, collectorID uuid.UUID, scope string) (db.CollectorInstance, string, time.Time, error) {
	collector, err := session.store.Queries.GetCollectorInstanceByID(ctx, collectorID)
	if err != nil || collector.Status == "uninstalled" || collector.Status == "result_unknown" {
		return db.CollectorInstance{}, "", time.Time{}, errors.New("repair Collector does not exist or is no longer active")
	}
	if collector.ResourceType == "kubernetes_cluster" && scope != "kubernetes" {
		return db.CollectorInstance{}, "", time.Time{}, errors.New("a Kubernetes Collector repair requires --scope kubernetes")
	}
	if collector.ResourceType == "host" && (scope == "kubernetes" || collector.Platform == "windows") {
		return db.CollectorInstance{}, "", time.Time{}, errors.New("this Collector requires a Linux scope; Windows repair is not available from the POSIX instruction")
	}
	token, err := identity.RandomToken(32)
	if err != nil {
		return db.CollectorInstance{}, "", time.Time{}, err
	}
	expiresAt := time.Now().UTC().Add(pkiRepairTokenTTL)
	err = session.store.InTx(ctx, func(q *db.Queries) error {
		if err := q.RevokeCollectorCertificates(ctx, db.RevokeCollectorCertificatesParams{CollectorID: collector.ID, RevokeReason: pgtype.Text{String: "pki_repair", Valid: true}}); err != nil {
			return err
		}
		if err := q.RevokePKISubjectCertificates(ctx, db.RevokePKISubjectCertificatesParams{SubjectKind: "collector",
			SubjectID: collector.ID.String(), RevocationReason: "pki_repair"}); err != nil {
			return err
		}
		_, err := q.CreateTelemetryEnrollmentToken(ctx, db.CreateTelemetryEnrollmentTokenParams{ID: uuid.New(), CollectorID: collector.ID,
			TokenHash: identity.TokenHash(token), ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
		return err
	})
	return collector, token, expiresAt, err
}

func linuxConnectorRepairCommand(cfg *InstallConfig, token string, bundle trustbundle.Bundle, scope string) string {
	server := "https://" + cfg.Spec.Exposure.EnterpriseHost
	body := repairBundlePreamble(bundle)
	if scope == "linux-user" {
		return body + `
ARGUS_REPAIR_DATA="${XDG_DATA_HOME:-$HOME/.local/share}/argus-connector"
systemctl --user stop argus-connector.service
if ! "${XDG_BIN_HOME:-$HOME/.local/bin}/argus-connector" repair --server ` + posixQuote(server) + ` --token ` + posixQuote(token) + ` --ca-file "$ARGUS_REPAIR_CA" --data-dir "$ARGUS_REPAIR_DATA"; then
  systemctl --user start argus-connector.service
  exit 1
fi
systemctl --user start argus-connector.service
systemctl --user is-active --quiet argus-connector.service`
	}
	return body + `
if [ "$(id -u)" -eq 0 ]; then ARGUS_REPAIR_SUDO=""; elif command -v sudo >/dev/null 2>&1; then ARGUS_REPAIR_SUDO=sudo; else echo 'root or sudo is required; use --scope linux-user when supported' >&2; exit 1; fi
$ARGUS_REPAIR_SUDO systemctl stop argus-connector.service
if ! $ARGUS_REPAIR_SUDO /usr/local/bin/argus-connector repair --server ` + posixQuote(server) + ` --token ` + posixQuote(token) + ` --ca-file "$ARGUS_REPAIR_CA" --data-dir /var/lib/argus-connector; then
  $ARGUS_REPAIR_SUDO systemctl start argus-connector.service
  exit 1
fi
$ARGUS_REPAIR_SUDO chown -R argus-connector:argus-connector /var/lib/argus-connector
$ARGUS_REPAIR_SUDO systemctl start argus-connector.service
$ARGUS_REPAIR_SUDO systemctl is-active --quiet argus-connector.service`
}

func linuxCollectorRepairCommand(cfg *InstallConfig, token string, bundle trustbundle.Bundle, scope string) string {
	endpoint := collectorEnrollmentEndpoint(cfg)
	body := repairBundlePreamble(bundle)
	if scope == "linux-user" {
		return body + `
ARGUS_REPAIR_ROOT="${XDG_DATA_HOME:-$HOME/.local/share}/argus-otelcol"
ARGUS_REPAIR_ETC="${XDG_CONFIG_HOME:-$HOME/.config}/argus-otelcol"
systemctl --user stop argus-otelcol.service
install -m 0600 "$ARGUS_REPAIR_CA" "$ARGUS_REPAIR_ETC/server-ca.pem"
printf '%s' ` + posixQuote(token) + ` > "$ARGUS_REPAIR_ETC/enrollment-token"
chmod 0600 "$ARGUS_REPAIR_ETC/enrollment-token"
rm -f "$ARGUS_REPAIR_ROOT/identity/client.pem" "$ARGUS_REPAIR_ROOT/identity/server.pem" "$ARGUS_REPAIR_ROOT/identity/trust-bundle.json"
systemctl --user start argus-otelcol.service
ARGUS_REPAIR_I=0
while [ "$ARGUS_REPAIR_I" -lt 90 ]; do
  if [ -s "$ARGUS_REPAIR_ROOT/identity/client.pem" ] && [ -s "$ARGUS_REPAIR_ROOT/identity/server.pem" ] && [ ! -e "$ARGUS_REPAIR_ETC/enrollment-token" ]; then exit 0; fi
  ARGUS_REPAIR_I=$((ARGUS_REPAIR_I+1)); sleep 1
done
echo 'Collector PKI repair did not become ready' >&2; exit 1`
	}
	return body + `
if [ "$(id -u)" -eq 0 ]; then ARGUS_REPAIR_SUDO=""; elif command -v sudo >/dev/null 2>&1; then ARGUS_REPAIR_SUDO=sudo; else echo 'root or sudo is required; use --scope linux-user when supported' >&2; exit 1; fi
$ARGUS_REPAIR_SUDO systemctl stop argus-otelcol.service
$ARGUS_REPAIR_SUDO install -m 0600 "$ARGUS_REPAIR_CA" /etc/argus-otelcol/server-ca.pem
printf '%s' ` + posixQuote(token) + ` | $ARGUS_REPAIR_SUDO sh -c 'umask 077; cat > /etc/argus-otelcol/enrollment-token'
$ARGUS_REPAIR_SUDO rm -f /var/lib/argus-otelcol/identity/client.pem /var/lib/argus-otelcol/identity/server.pem /var/lib/argus-otelcol/identity/trust-bundle.json
$ARGUS_REPAIR_SUDO systemctl start argus-otelcol.service
ARGUS_REPAIR_I=0
while [ "$ARGUS_REPAIR_I" -lt 90 ]; do
  if $ARGUS_REPAIR_SUDO test -s /var/lib/argus-otelcol/identity/client.pem && $ARGUS_REPAIR_SUDO test -s /var/lib/argus-otelcol/identity/server.pem && $ARGUS_REPAIR_SUDO test ! -e /etc/argus-otelcol/enrollment-token; then exit 0; fi
  ARGUS_REPAIR_I=$((ARGUS_REPAIR_I+1)); sleep 1
done
echo 'Collector PKI repair did not become ready' >&2; exit 1
# enrollment endpoint: ` + endpoint
}

func kubernetesConnectorRepairCommand(cfg *InstallConfig, connectorID uuid.UUID, token string, bundle trustbundle.Bundle, namespace string) string {
	_ = connectorID // The installed state is authoritative; the repair binary verifies it against the token.
	return `printf '%s' ` + posixQuote(base64.StdEncoding.EncodeToString(bundle.Material.PEM)) + ` | base64 -d | kubectl -n ` + posixQuote(namespace) +
		` exec -i deployment/argus-kubernetes-connector -- /usr/local/bin/argus-connector repair --server ` + posixQuote("https://"+cfg.Spec.Exposure.EnterpriseHost) +
		` --token ` + posixQuote(token) + ` --ca-file /dev/stdin --data-dir /var/lib/argus-connector
kubectl -n ` + posixQuote(namespace) + ` rollout restart deployment/argus-kubernetes-connector
kubectl -n ` + posixQuote(namespace) + ` rollout status deployment/argus-kubernetes-connector --timeout=180s`
}

func kubernetesCollectorRepairCommand(cfg *InstallConfig, collectorID uuid.UUID, token string, bundle trustbundle.Bundle, connectorNamespace string) string {
	return `printf '%s' ` + posixQuote(base64.StdEncoding.EncodeToString(bundle.Material.PEM)) + ` | base64 -d | kubectl -n ` + posixQuote(connectorNamespace) +
		` exec -i deployment/argus-kubernetes-connector -- /usr/local/bin/argus-connector repair-collector --server ` + posixQuote(collectorEnrollmentEndpoint(cfg)) +
		` --collector-id ` + posixQuote(collectorID.String()) + ` --token ` + posixQuote(token) + ` --ca-file /dev/stdin --trust-bundle-epoch ` + fmt.Sprint(bundle.Epoch) +
		` --namespace argus-telemetry
kubectl -n argus-telemetry rollout restart deployment/argus-otelcol-gateway
kubectl -n argus-telemetry rollout restart daemonset/argus-otelcol-agent
kubectl -n argus-telemetry rollout status deployment/argus-otelcol-gateway --timeout=180s
kubectl -n argus-telemetry rollout status daemonset/argus-otelcol-agent --timeout=300s`
}

func repairBundlePreamble(bundle trustbundle.Bundle) string {
	return `set -eu
for ARGUS_REPAIR_TOOL in base64 sha256sum openssl mktemp; do command -v "$ARGUS_REPAIR_TOOL" >/dev/null 2>&1 || { echo "$ARGUS_REPAIR_TOOL is required" >&2; exit 1; }; done
ARGUS_REPAIR_DIR=$(mktemp -d "${TMPDIR:-/tmp}/argus-pki-repair.XXXXXX")
trap 'rm -rf "$ARGUS_REPAIR_DIR"' EXIT HUP INT TERM
umask 077
ARGUS_REPAIR_CA="$ARGUS_REPAIR_DIR/ca.pem"
printf '%s' ` + posixQuote(base64.StdEncoding.EncodeToString(bundle.Material.PEM)) + ` | base64 -d > "$ARGUS_REPAIR_CA"
printf '%s  %s\n' ` + posixQuote(bundle.Material.SHA256) + ` "$ARGUS_REPAIR_CA" | sha256sum -c - >/dev/null
openssl crl2pkcs7 -nocrl -certfile "$ARGUS_REPAIR_CA" >/dev/null 2>&1`
}

func collectorEnrollmentEndpoint(cfg *InstallConfig) string {
	return fmt.Sprintf("https://%s:4318/v1/identity/enroll", ingestBase(cfg))
}

func posixQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}
