package telemetry

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/artifactcheck"
	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/installinstruction"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

// hostInstallTokenTTL 与 Bastion 注册令牌一致:命令生成后 24 小时内有效、单次消费。
const hostInstallTokenTTL = 24 * time.Hour

// SelfEnrollService 承载 self_enrolled(只出不进)主机的一次性安装命令生命周期:
// commit/再生成时签发令牌,ingest 公开端点完成 bootstrap 交换、激活与状态收敛。
type SelfEnrollService struct {
	Store              *postgres.Store
	Actions            resource.PendingActionService
	Identity           IdentityService
	EnrollmentEndpoint string
	IngestGRPCEndpoint string
	IngestHTTPEndpoint string
	// SigningPublicKeys 来自 ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS(与执行侧同一
	// 信任根)。签名、公钥或匹配关系缺失时 bootstrap 必须 fail closed。
	SigningPublicKeys map[string]string
	// BootstrapSecretKey encrypts the first bootstrap exchange result so a
	// same-device network retry receives exactly the original enrollment token.
	BootstrapSecretKey []byte
	Artifacts          artifactcheck.Checker
	TrustBundles       trustbundle.Service
	TrustBundlePath    string
	TrustBundleEpoch   int64
	BootstrapTLSMode   installinstruction.DownloadTLSMode
	InstallerSHA256    string
}

type hostCommandActionPlan struct {
	Operation        string                `json:"operation"`
	HostID           uuid.UUID             `json:"host_id"`
	HostVersion      int64                 `json:"host_version"`
	CollectorID      uuid.UUID             `json:"collector_id"`
	CollectorVersion int64                 `json:"collector_version"`
	ArtifactURI      string                `json:"artifact_uri"`
	Frozen           hostInstallFrozenPlan `json:"frozen"`
}

// hostInstallFrozenPlan 是令牌绑定的冻结计划:bootstrap 交换只能按该计划执行,
// 任何偏离都要求重新生成命令,与 ConnectionTest 的冻结纪律等价。
type hostInstallFrozenPlan struct {
	Mode                  string      `json:"mode"`
	CollectorID           uuid.UUID   `json:"collector_id"`
	HostID                uuid.UUID   `json:"host_id"`
	DesiredRevision       int64       `json:"desired_revision"`
	DistributionVersionID uuid.UUID   `json:"distribution_version_id"`
	ProfileIDs            []uuid.UUID `json:"profile_ids"`
	Platform              string      `json:"platform"`
	PlanHash              []byte      `json:"plan_hash"`
}

// BootstrapPayload 是 GET /v1/host-install/{token} 的响应体。
type BootstrapPayload struct {
	SigningPublicKey        string
	SigningKeyID            string
	Mode                    string
	CollectorID             uuid.UUID
	HostID                  uuid.UUID
	DistributionVersionID   string
	DesiredRevision         int64
	ConfigBundle            []byte
	TrustBundle             []byte
	TrustBundleEpoch        int64
	TrustBundleSHA256       string
	TrustBundleFingerprints []string
	Artifact                collectorArtifact
	HasArtifact             bool
	EnrollmentToken         string
	EnrollmentEndpoint      string
	IngestGRPCEndpoint      string
	IngestHTTPEndpoint      string
	ExpiresAt               time.Time
}

type UninstallBootstrapPayload struct {
	HostID          uuid.UUID
	CollectorID     uuid.UUID
	CompletionURL   string
	CompletionToken string
	ExpiresAt       time.Time
}

// IssueInstallToken 为一个 self_enrolled 主机的 Collector 签发一次性安装令牌,
// 同时撤销该主机旧的 active 令牌(一台主机同时最多一个 active 命令)。
func (service SelfEnrollService) IssueInstallToken(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, actorID, actorType string, frozen hostInstallFrozenPlan) (db.HostEnrollmentToken, string, error) {
	if _, err := q.RevokeActiveHostEnrollmentTokens(ctx, db.RevokeActiveHostEnrollmentTokensParams{
		EnterpriseID: enterpriseID, PreallocatedHostID: frozen.HostID}); err != nil {
		return db.HostEnrollmentToken{}, "", err
	}
	token, err := identity.RandomToken(32)
	if err != nil {
		return db.HostEnrollmentToken{}, "", err
	}
	encoded, err := json.Marshal(frozen)
	if err != nil {
		return db.HostEnrollmentToken{}, "", err
	}
	planHash := sha256.Sum256(encoded)
	record, err := q.CreateHostEnrollmentToken(ctx, db.CreateHostEnrollmentTokenParams{
		ID: newTelemetryID(), EnterpriseID: enterpriseID, PreallocatedHostID: frozen.HostID,
		CollectorID: frozen.CollectorID, TokenHash: sha256Sum(token), FrozenPlan: encoded, FrozenPlanHash: planHash[:],
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(hostInstallTokenTTL), Valid: true}, CreatedBy: uuid.MustParse(actorID),
	})
	if err != nil {
		return db.HostEnrollmentToken{}, "", err
	}
	if _, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true},
		ActorType: actorTypeForAudit(actorType), ActorID: actorID, Action: "host.install_token.create", ResourceType: "host",
		ResourceID: frozen.HostID.String(), Result: "success", Details: map[string]any{"mode": frozen.Mode, "collector_id": frozen.CollectorID.String()}}); err != nil {
		return db.HostEnrollmentToken{}, "", err
	}
	return record, token, nil
}

// BuildInstallInstructions creates system and user-mode bootstraps. Both use
// the same Argus Trust Bundle for installer, bootstrap, artifact, enrollment,
// and ingest HTTPS verification.
func (service SelfEnrollService) BuildInstallInstructions(ctx context.Context, artifactURI, token, mode string, expiresAt time.Time) ([]installinstruction.Set, error) {
	scriptBase, err := artifactScriptBase(artifactURI)
	if err != nil {
		return nil, err
	}
	if mode != "install" && mode != "uninstall" {
		return nil, errors.New("host installation mode is invalid")
	}
	bundle, err := service.currentTrustBundle(ctx)
	if err != nil {
		return nil, err
	}
	arguments := []string{"--base", strings.TrimRight(service.IngestHTTPEndpoint, "/")}
	if mode == "uninstall" {
		arguments = append(arguments, "--uninstall")
	}
	sets := make([]installinstruction.Set, 0, 2)
	for _, scope := range []installinstruction.Scope{installinstruction.ScopeLinuxSystem, installinstruction.ScopeLinuxUser} {
		warnings := []string{}
		if scope == installinstruction.ScopeLinuxUser {
			warnings = append(warnings, "User services require an active login session unless systemd linger is enabled.",
				"Profiles requiring host capabilities, kernel data, or system directories are unavailable in user mode.")
		}
		instruction, buildErr := installinstruction.BuildPOSIX(installinstruction.POSIXOptions{Scope: scope,
			InstallerURL: scriptBase + "/install/host.sh", InstallerSHA256: service.InstallerSHA256,
			BootstrapScriptURL: strings.TrimRight(service.IngestHTTPEndpoint, "/") + "/v1/host-bootstrap-script",
			DownloadTLSMode:    service.BootstrapTLSMode,
			TrustBundlePEM:     bundle.Material.PEM, TrustBundleEpoch: bundle.Epoch, Token: token, ExpiresAt: expiresAt,
			InstallerArguments: arguments, CapabilityWarnings: warnings})
		if buildErr != nil {
			return nil, buildErr
		}
		sets = append(sets, instruction)
	}
	return sets, nil
}

// BootstrapScript returns the strict generated installer used by the short
// download command. Script retrieval is intentionally separate from token
// consumption so curl retries cannot consume the enrollment authorization.
func (service SelfEnrollService) BootstrapScript(ctx context.Context, token string, scope installinstruction.Scope) (string, error) {
	if service.Store == nil || strings.TrimSpace(token) == "" ||
		(scope != installinstruction.ScopeLinuxSystem && scope != installinstruction.ScopeLinuxUser) {
		return "", ErrHostInstallTokenInvalid
	}
	tokenHash := sha256Sum(token)
	record, err := service.Store.Queries.GetHostEnrollmentTokenByHash(ctx, tokenHash)
	mode := "install"
	var frozen hostInstallFrozenPlan
	var enterpriseID uuid.UUID
	var expiresAt time.Time
	if err == nil {
		if record.Status != "active" || !record.ExpiresAt.Valid || !time.Now().UTC().Before(record.ExpiresAt.Time) ||
			json.Unmarshal(record.FrozenPlan, &frozen) != nil || frozen.Mode != "install" ||
			frozen.HostID != record.PreallocatedHostID || frozen.CollectorID != record.CollectorID {
			return "", ErrHostInstallTokenInvalid
		}
		enterpriseID, expiresAt = record.EnterpriseID, record.ExpiresAt.Time
	} else if errors.Is(err, pgx.ErrNoRows) {
		uninstall, uninstallErr := service.Store.Queries.GetHostUninstallTokenByHash(ctx, tokenHash)
		if uninstallErr != nil || uninstall.Status != "active" || !uninstall.ExpiresAt.Valid ||
			!time.Now().UTC().Before(uninstall.ExpiresAt.Time) || json.Unmarshal(uninstall.FrozenPlan, &frozen) != nil ||
			frozen.Mode != "uninstall" || frozen.HostID != uninstall.HostID || frozen.CollectorID != uninstall.CollectorID {
			return "", ErrHostInstallTokenInvalid
		}
		mode, enterpriseID, expiresAt = "uninstall", uninstall.EnterpriseID, uninstall.ExpiresAt.Time
	} else {
		return "", ErrHostInstallTokenInvalid
	}
	distributions, err := service.Store.Queries.ListCollectorDistributionVersions(ctx)
	if err != nil {
		return "", err
	}
	index := slices.IndexFunc(distributions, func(item db.CollectorDistributionVersion) bool {
		return item.ID == frozen.DistributionVersionID
	})
	if index < 0 {
		return "", ErrDistributionPending
	}
	artifact, err := artifactForPlatform(distributions[index].ArtifactManifest, frozen.Platform)
	if err != nil {
		return "", err
	}
	instructions, err := service.BuildInstallInstructions(ctx, artifact.URI, token, mode, expiresAt)
	if err != nil {
		return "", err
	}
	for _, instruction := range instructions {
		if instruction.Scope != scope {
			continue
		}
		_, _ = audit.Append(ctx, service.Store.Queries, audit.Entry{Domain: "enterprise",
			EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: "system", ActorID: "host-bootstrap",
			Action: "host.bootstrap_script.download", ResourceType: "host", ResourceID: frozen.HostID.String(), Result: "success",
			Details: map[string]any{"operation": mode, "scope": scope, "tls_mode": service.BootstrapTLSMode}})
		return instruction.BootstrapScript + "\n", nil
	}
	return "", ErrHostInstallTokenInvalid
}

func (service SelfEnrollService) currentTrustBundle(ctx context.Context) (trustbundle.Bundle, error) {
	if service.TrustBundles.Store != nil {
		return service.TrustBundles.Current(ctx)
	}
	value, err := os.ReadFile(service.TrustBundlePath)
	if err != nil {
		return trustbundle.Bundle{}, fmt.Errorf("read host installation Trust Bundle: %w", err)
	}
	material, err := trustbundle.Parse(value, time.Now().UTC())
	if err != nil {
		return trustbundle.Bundle{}, err
	}
	if service.TrustBundleEpoch < 1 {
		return trustbundle.Bundle{}, errors.New("host installation Trust Bundle epoch is invalid")
	}
	return trustbundle.Bundle{Epoch: service.TrustBundleEpoch, State: trustbundle.StateStable, Material: material,
		CurrentCAFingerprints: material.Fingerprints, StartedAt: time.Now().UTC()}, nil
}

func artifactScriptBase(artifactURI string) (string, error) {
	parsed, err := url.Parse(artifactURI)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("collector artifact origin is invalid")
	}
	base := parsed.Scheme + "://" + parsed.Host
	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) >= 2 && segments[0] != "" {
		base += "/" + segments[0]
	}
	return base, nil
}

func (service SelfEnrollService) ensureArtifactAvailability(ctx context.Context, artifactURI string) error {
	if service.Artifacts == nil {
		return nil
	}
	scriptBase, err := artifactScriptBase(artifactURI)
	if err != nil {
		return ErrCollectorArtifactUnavailable
	}
	if err = service.Artifacts.Check(ctx, artifactURI, scriptBase+"/install/host.sh"); err != nil {
		return fmt.Errorf("%w: %v", ErrCollectorArtifactUnavailable, err)
	}
	return nil
}

func sha256Sum(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func actorTypeForAudit(actorType string) string {
	switch actorType {
	case "", "user", "enterprise_user":
		return "enterprise_user"
	case "service_account":
		return "service_account"
	default:
		// 未知来源保持原值并由 audit_events 的约束 fail closed，不能把未知
		// 身份静默记作企业用户。
		return actorType
	}
}

func (service SelfEnrollService) PreviewEnrollmentRotate(ctx context.Context, subject resource.Subject, enterpriseID, hostID uuid.UUID, expectedVersion int64, idempotencyKey string) (db.PendingAction, error) {
	return service.previewHostCommand(ctx, subject, enterpriseID, hostID, expectedVersion, "enrollment_rotate", idempotencyKey)
}

func (service SelfEnrollService) PreviewUninstallCommand(ctx context.Context, subject resource.Subject, enterpriseID, hostID uuid.UUID, expectedVersion int64, idempotencyKey string) (db.PendingAction, error) {
	return service.previewHostCommand(ctx, subject, enterpriseID, hostID, expectedVersion, "uninstall_command", idempotencyKey)
}

func (service SelfEnrollService) previewHostCommand(ctx context.Context, subject resource.Subject, enterpriseID, hostID uuid.UUID, expectedVersion int64, operation, idempotencyKey string) (db.PendingAction, error) {
	if service.Store == nil || !slices.Contains(subject.AuthorizedResourceIDs, hostID) {
		return db.PendingAction{}, ErrDenied
	}
	plan, host, err := service.hostCommandPlan(ctx, service.Store.Queries, enterpriseID, hostID, expectedVersion, operation)
	if err != nil {
		return db.PendingAction{}, err
	}
	actionType, title, summary, risk := "host.enrollment.rotate", "Rotate host enrollment", "Generate a new self-enrollment installation command for "+host.Name, "write"
	if operation == "uninstall_command" {
		actionType, title, summary, risk = "host.uninstall.command", "Uninstall self-enrolled host", "Generate a destructive uninstall command for "+host.Name, "dangerous"
	}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{
		ActionType: actionType, Title: title, Summary: summary, Risk: risk,
		ResourceType: "host", ResourceID: uuid.NullUUID{UUID: hostID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: expectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"host_id": hostID, "name": host.Name, "collector_id": plan.CollectorID, "operation": operation},
		Diff:    []map[string]string{{"kind": "change", "text": summary}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: resource.NewResourceAuthorizationSnapshot("host", hostID),
		CommitHandler:         "argus." + actionType + ".commit",
	}, idempotencyKey)
}

func (service SelfEnrollService) hostCommandPlan(ctx context.Context, q *db.Queries, enterpriseID, hostID uuid.UUID, expectedVersion int64, operation string) (hostCommandActionPlan, db.Host, error) {
	host, err := q.GetHost(ctx, db.GetHostParams{ID: hostID, EnterpriseID: enterpriseID})
	if err != nil || host.ConnectionMode != "self_enrolled" || host.Status == "deleted" || host.ResourceVersion != expectedVersion {
		return hostCommandActionPlan{}, db.Host{}, resource.ErrVersionConflict
	}
	collector, err := q.GetCollectorForResource(ctx, db.GetCollectorForResourceParams{EnterpriseID: enterpriseID, ResourceType: "host", ResourceID: hostID})
	if err != nil {
		return hostCommandActionPlan{}, db.Host{}, ErrNotFound
	}
	if collector.Status == "uninstalling" || collector.Status == "uninstalled" || (operation == "uninstall_command" && host.Status != "active") {
		return hostCommandActionPlan{}, db.Host{}, ErrQueryInvalid
	}
	distributions, err := q.ListCollectorDistributionVersions(ctx)
	if err != nil {
		return hostCommandActionPlan{}, db.Host{}, err
	}
	index := slices.IndexFunc(distributions, func(item db.CollectorDistributionVersion) bool { return item.ID == collector.DistributionVersionID })
	if index < 0 {
		return hostCommandActionPlan{}, db.Host{}, ErrDistributionPending
	}
	artifact, err := artifactForPlatform(distributions[index].ArtifactManifest, collector.Platform)
	if err != nil {
		return hostCommandActionPlan{}, db.Host{}, err
	}
	if err = service.ensureArtifactAvailability(ctx, artifact.URI); err != nil {
		return hostCommandActionPlan{}, db.Host{}, err
	}
	frozen := hostInstallFrozenPlan{Mode: "install", CollectorID: collector.ID, HostID: hostID,
		DesiredRevision: collector.DesiredRevision, DistributionVersionID: collector.DistributionVersionID,
		Platform: collector.Platform, ProfileIDs: []uuid.UUID{}}
	return hostCommandActionPlan{Operation: operation, HostID: hostID, HostVersion: host.ResourceVersion,
		CollectorID: collector.ID, CollectorVersion: collector.Version, ArtifactURI: artifact.URI, Frozen: frozen}, host, nil
}

func (service SelfEnrollService) RevalidateHostCommandAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) ([]byte, error) {
	var plan hostCommandActionPlan
	if json.Unmarshal(raw, &plan) != nil {
		return nil, resource.ErrActionInvalidated
	}
	current, _, err := service.hostCommandPlan(ctx, q, action.EnterpriseID, plan.HostID, plan.HostVersion, plan.Operation)
	if err != nil || current.CollectorID != plan.CollectorID || current.CollectorVersion != plan.CollectorVersion || current.ArtifactURI != plan.ArtifactURI {
		return nil, resource.ErrActionInvalidated
	}
	return resource.HashResourceAuthorizationSnapshot("host", plan.HostID)
}

func (service SelfEnrollService) CommitHostCommandAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (resource.ActionCommitResult, error) {
	var plan hostCommandActionPlan
	if json.Unmarshal(raw, &plan) != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	expiresAt := time.Now().UTC().Add(hostInstallTokenTTL)
	resultKind := "host_install_command"
	if plan.Operation == "enrollment_rotate" {
		_, token, err := service.IssueInstallToken(ctx, q, action.EnterpriseID, action.CreatorSubjectID.String(), action.CreatorSubjectType, plan.Frozen)
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		instructions, err := service.BuildInstallInstructions(ctx, plan.ArtifactURI, token, "install", expiresAt)
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		return resource.ActionCommitResult{ResourceType: "host", ResourceID: plan.HostID, ResourceVersion: plan.HostVersion,
			Summary: "Self-enrolled host command issued", OneTimeCommand: &resource.OneTimeCommandResult{InstructionSets: instructions, ExpiresAt: expiresAt},
			OneTimeResultKind: resultKind}, nil
	} else if plan.Operation == "uninstall_command" {
		token, err := service.issueUninstallToken(ctx, q, action, plan)
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		expiresAt = time.Now().UTC().Add(30 * time.Minute)
		resultKind = "host_uninstall_command"
		instructions, err := service.BuildInstallInstructions(ctx, plan.ArtifactURI, token, "uninstall", expiresAt)
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		return resource.ActionCommitResult{ResourceType: "host", ResourceID: plan.HostID, ResourceVersion: plan.HostVersion,
			Summary: "Self-enrolled host command issued", OneTimeCommand: &resource.OneTimeCommandResult{InstructionSets: instructions, ExpiresAt: expiresAt},
			OneTimeResultKind: resultKind}, nil
	} else {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
}

func (service SelfEnrollService) issueUninstallToken(ctx context.Context, q *db.Queries, action db.PendingAction, plan hostCommandActionPlan) (string, error) {
	if _, err := q.RevokeActiveHostUninstallTokens(ctx, db.RevokeActiveHostUninstallTokensParams{EnterpriseID: action.EnterpriseID, HostID: plan.HostID}); err != nil {
		return "", err
	}
	token, err := identity.RandomToken(32)
	if err != nil {
		return "", err
	}
	plan.Frozen.Mode = "uninstall"
	encoded, err := json.Marshal(plan.Frozen)
	if err != nil {
		return "", err
	}
	encoded, err = resource.CanonicalJSON(encoded)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	expiresAt := time.Now().UTC().Add(30 * time.Minute)
	_, err = q.CreateHostUninstallToken(ctx, db.CreateHostUninstallTokenParams{ID: newTelemetryID(), EnterpriseID: action.EnterpriseID,
		HostID: plan.HostID, CollectorID: plan.CollectorID, TokenHash: sha256Sum(token), FrozenPlan: encoded, FrozenPlanHash: hash[:],
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}, CreatedBy: action.CreatorSubjectID})
	if err != nil {
		return "", err
	}
	// The command-issuance Execution completes when the one-time result becomes
	// available, while this bootstrap operation tracks the subsequent remote
	// uninstall through its completion callback. Keeping the operation separate
	// prevents a previous successful install operation from masking uninstall
	// convergence.
	if _, err = q.CreateTelemetryCollectorOperation(ctx, db.CreateTelemetryCollectorOperationParams{
		ID: newTelemetryID(), EnterpriseID: action.EnterpriseID, CollectorID: plan.CollectorID,
		PendingActionID: action.ID, Operation: "uninstall", ExecutorKind: "bootstrap",
		Plan: encoded, PlanHash: hash[:], ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return "", err
	}
	_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: action.EnterpriseID, Valid: true},
		ActorType: actorTypeForAudit(action.CreatorSubjectType), ActorID: action.CreatorSubjectID.String(), Action: "host.uninstall_token.create",
		ResourceType: "host", ResourceID: plan.HostID.String(), Result: "success", Details: map[string]any{"collector_id": plan.CollectorID.String()}})
	return token, err
}

func (service SelfEnrollService) ExchangeUninstall(ctx context.Context, token, arch, hostname, address string) (payload UninstallBootstrapPayload, resultErr error) {
	if service.Store == nil || token == "" || len(service.BootstrapSecretKey) != 32 {
		return UninstallBootstrapPayload{}, ErrHostUninstallTokenInvalid
	}
	deviceHash := sha256Sum(strings.Join([]string{arch, hostname, address}, "\x00"))
	resultErr = service.Store.InTx(ctx, func(q *db.Queries) error {
		record, err := q.GetHostUninstallTokenByHashForUpdate(ctx, sha256Sum(token))
		if err != nil || record.Status == "revoked" || record.Status == "expired" ||
			(record.Status == "active" && !time.Now().UTC().Before(record.ExpiresAt.Time)) {
			return ErrHostUninstallTokenInvalid
		}
		completionToken := service.uninstallCompletionToken(record)
		if record.Status == "consumed" {
			if subtle.ConstantTimeCompare(record.ConsumedDeviceHash, deviceHash) != 1 ||
				subtle.ConstantTimeCompare(record.CompletionTokenHash, sha256Sum(completionToken)) != 1 {
				return ErrHostUninstallTokenConflict
			}
		} else if record.Status == "active" {
			var frozen hostInstallFrozenPlan
			if json.Unmarshal(record.FrozenPlan, &frozen) != nil || frozen.Mode != "uninstall" ||
				frozen.HostID != record.HostID || frozen.CollectorID != record.CollectorID {
				return ErrHostUninstallTokenInvalid
			}
			if _, err = q.ConsumeHostUninstallToken(ctx, db.ConsumeHostUninstallTokenParams{ID: record.ID,
				ConsumedDeviceHash: deviceHash, CompletionTokenHash: sha256Sum(completionToken)}); err != nil {
				return ErrHostUninstallTokenConflict
			}
			if _, err = q.MarkCollectorUninstalling(ctx, db.MarkCollectorUninstallingParams{EnterpriseID: record.EnterpriseID,
				ResourceType: "host", ResourceID: record.HostID, Column4: 0}); err != nil {
				return err
			}
			if _, err = q.RevokeActiveHostEnrollmentTokens(ctx, db.RevokeActiveHostEnrollmentTokensParams{
				EnterpriseID: record.EnterpriseID, PreallocatedHostID: record.HostID}); err != nil {
				return err
			}
			if _, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: record.EnterpriseID, Valid: true},
				ActorType: "system", ActorID: "host-uninstall", Action: "host.uninstall_token.exchange", ResourceType: "host",
				ResourceID: record.HostID.String(), Result: "success", Details: map[string]any{"collector_id": record.CollectorID.String(), "token_id": record.ID.String()}}); err != nil {
				return err
			}
		} else {
			return ErrHostUninstallTokenInvalid
		}
		payload = UninstallBootstrapPayload{HostID: record.HostID, CollectorID: record.CollectorID,
			CompletionURL:   strings.TrimRight(service.IngestHTTPEndpoint, "/") + "/v1/host-uninstall/" + record.ID.String() + "/complete",
			CompletionToken: completionToken, ExpiresAt: record.ExpiresAt.Time.UTC()}
		return nil
	})
	if resultErr != nil {
		return UninstallBootstrapPayload{}, resultErr
	}
	return payload, nil
}

func (service SelfEnrollService) CompleteUninstall(ctx context.Context, tokenID uuid.UUID, completionToken string) error {
	if service.Store == nil || tokenID == uuid.Nil || completionToken == "" {
		return ErrHostUninstallTokenInvalid
	}
	return service.Store.InTx(ctx, func(q *db.Queries) error {
		record, err := q.GetHostUninstallTokenForUpdate(ctx, tokenID)
		if err != nil || subtle.ConstantTimeCompare(record.CompletionTokenHash, sha256Sum(completionToken)) != 1 {
			return ErrHostUninstallTokenInvalid
		}
		if record.Status == "completed" {
			return nil
		}
		if record.Status != "consumed" || !time.Now().UTC().Before(record.ExpiresAt.Time) {
			return ErrHostUninstallTokenInvalid
		}
		if _, err = q.CompleteHostUninstallToken(ctx, db.CompleteHostUninstallTokenParams{ID: record.ID,
			EnterpriseID: record.EnterpriseID, CompletionTokenHash: sha256Sum(completionToken)}); err != nil {
			return ErrHostUninstallTokenInvalid
		}
		collector, err := q.GetCollectorInstance(ctx, db.GetCollectorInstanceParams{ID: record.CollectorID, EnterpriseID: record.EnterpriseID})
		if err != nil {
			return err
		}
		if err = q.RevokeCollectorCertificates(ctx, db.RevokeCollectorCertificatesParams{CollectorID: record.CollectorID,
			RevokeReason: pgtype.Text{String: "host_uninstalled", Valid: true}}); err != nil {
			return err
		}
		if err = finishBootstrapOperation(ctx, q, record.EnterpriseID, collector, "uninstall"); err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: record.EnterpriseID, Valid: true},
			ActorType: "system", ActorID: "host-uninstall", Action: "host.uninstall.complete", ResourceType: "host",
			ResourceID: record.HostID.String(), Result: "success", Details: map[string]any{"collector_id": record.CollectorID.String(), "token_id": record.ID.String()}})
		return err
	})
}

func (service SelfEnrollService) uninstallCompletionToken(record db.HostUninstallToken) string {
	mac := hmac.New(sha256.New, service.BootstrapSecretKey)
	_, _ = mac.Write([]byte(fmt.Sprintf("argus.host_uninstall_completion/v1\x00%s\x00%s", record.EnterpriseID, record.ID)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// ExchangeBootstrap atomically consumes an install token and caches the first
// response as an AEAD envelope. It deliberately does not activate the Host:
// activation happens only after the Collector certificate enrollment succeeds.
func (service SelfEnrollService) ExchangeBootstrap(ctx context.Context, token string, arch, hostname, address string) (payload BootstrapPayload, resultErr error) {
	if service.Store == nil || token == "" {
		return BootstrapPayload{}, ErrHostInstallTokenInvalid
	}
	deviceHash := sha256Sum(strings.Join([]string{arch, hostname, address}, "\x00"))
	resultErr = service.Store.InTx(ctx, func(q *db.Queries) error {
		record, err := q.GetHostEnrollmentTokenByHashForUpdate(ctx, sha256Sum(token))
		if err != nil || record.Status == "revoked" || record.Status == "expired" ||
			(record.Status == "active" && !time.Now().UTC().Before(record.ExpiresAt.Time)) {
			return ErrHostInstallTokenInvalid
		}
		if record.Status == "consumed" {
			if subtle.ConstantTimeCompare(record.ConsumedDeviceHash, deviceHash) != 1 {
				return ErrHostInstallTokenConflict
			}
			if !record.ExchangeExpiresAt.Valid || !time.Now().UTC().Before(record.ExchangeExpiresAt.Time) {
				return ErrHostInstallTokenInvalid
			}
			payload, err = decryptBootstrapExchange(service.BootstrapSecretKey, record)
			return err
		}
		if record.Status != "active" {
			return ErrHostInstallTokenInvalid
		}
		var frozen hostInstallFrozenPlan
		if json.Unmarshal(record.FrozenPlan, &frozen) != nil || frozen.CollectorID == uuid.Nil ||
			frozen.CollectorID != record.CollectorID || frozen.HostID != record.PreallocatedHostID || frozen.Mode != "install" {
			return ErrHostInstallTokenInvalid
		}
		payload = BootstrapPayload{Mode: frozen.Mode, CollectorID: frozen.CollectorID, HostID: frozen.HostID,
			DesiredRevision: frozen.DesiredRevision, EnrollmentEndpoint: service.EnrollmentEndpoint,
			IngestGRPCEndpoint: service.IngestGRPCEndpoint, IngestHTTPEndpoint: service.IngestHTTPEndpoint,
			ExpiresAt: time.Now().UTC().Add(enrollmentTokenTTL)}
		if err = service.prepareInstall(ctx, q, record, frozen, &payload); err != nil {
			return err
		}
		if err = service.attachTrustedSigningKey(&payload); err != nil {
			return err
		}
		nonce, ciphertext, err := encryptBootstrapExchange(service.BootstrapSecretKey, record, payload)
		if err != nil {
			return err
		}
		rows, err := q.ConsumeHostEnrollmentToken(ctx, db.ConsumeHostEnrollmentTokenParams{
			ID: record.ID, ConsumedDeviceHash: deviceHash, ReportedHostname: sanitizeSelfReport(hostname, 253),
			ReportedAddress: sanitizeSelfReport(address, 512), ReportedArchitecture: selfReportedArch(arch)})
		if err != nil || rows != 1 {
			return ErrHostInstallTokenConflict
		}
		if _, err = q.StoreHostEnrollmentExchange(ctx, db.StoreHostEnrollmentExchangeParams{ID: record.ID,
			ExchangeKeyVersion: pgtype.Int4{Int32: 1, Valid: true}, ExchangeNonce: nonce, ExchangeCiphertext: ciphertext,
			ExchangeExpiresAt: pgtype.Timestamptz{Time: payload.ExpiresAt, Valid: true}}); err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise",
			EnterpriseID: uuid.NullUUID{UUID: record.EnterpriseID, Valid: true}, ActorType: "system", ActorID: "host-bootstrap",
			Action: "host.install_token.exchange", ResourceType: "host", ResourceID: frozen.HostID.String(), Result: "success",
			Details: map[string]any{"mode": frozen.Mode, "device_hash": hex.EncodeToString(deviceHash)}})
		return err
	})
	if resultErr != nil {
		return BootstrapPayload{}, resultErr
	}
	return payload, nil
}

func (service SelfEnrollService) prepareInstall(ctx context.Context, q *db.Queries, exchange db.HostEnrollmentToken, frozen hostInstallFrozenPlan, payload *BootstrapPayload) error {
	collector, err := q.GetCollectorInstance(ctx, db.GetCollectorInstanceParams{ID: frozen.CollectorID, EnterpriseID: exchange.EnterpriseID})
	if err != nil || collector.Status == "uninstalled" || collector.ResourceType != "host" || collector.ResourceID != frozen.HostID {
		return ErrHostInstallTokenInvalid
	}
	revision, err := q.GetCollectorConfigRevision(ctx, db.GetCollectorConfigRevisionParams{CollectorID: collector.ID, Revision: collector.DesiredRevision})
	if err != nil {
		return err
	}
	distributions, err := q.ListCollectorDistributionVersions(ctx)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(distributions, func(item db.CollectorDistributionVersion) bool { return item.ID == collector.DistributionVersionID })
	if index < 0 {
		return ErrDistributionPending
	}
	artifact, err := artifactForPlatform(distributions[index].ArtifactManifest, collector.Platform)
	if err != nil {
		return err
	}
	enrollmentToken, err := service.Identity.CreateEnrollmentTokenForHost(ctx, q, collector.ID, exchange.ID)
	if err != nil {
		return err
	}
	runtimeConfig, err := configbundle.Extract(revision.RenderedConfig, "host")
	if err != nil {
		return err
	}
	bundle, err := service.currentTrustBundle(ctx)
	if err != nil {
		return err
	}
	payload.ConfigBundle = runtimeConfig
	payload.TrustBundle = bundle.Material.PEM
	payload.TrustBundleEpoch = bundle.Epoch
	payload.TrustBundleSHA256 = bundle.Material.SHA256
	payload.TrustBundleFingerprints = bundle.Material.Fingerprints
	payload.Artifact, payload.HasArtifact = artifact, true
	payload.DistributionVersionID = collector.DistributionVersionID.String()
	payload.EnrollmentToken = enrollmentToken
	return nil
}

// finishBootstrapOperation 把 executor_kind=bootstrap 的安装/卸载 operation
// 收敛为 succeeded,并复用 Direct Executor 相同的 Collector/Route 收敛语义。
func finishBootstrapOperation(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, collector db.CollectorInstance, operation string) error {
	expectedStatus := "converged"
	if operation == "uninstall" {
		expectedStatus = "uninstalled"
	}
	if collector.Status == expectedStatus {
		return nil
	}
	latest, err := q.GetLatestCollectorOperation(ctx, db.GetLatestCollectorOperationParams{CollectorID: collector.ID, EnterpriseID: enterpriseID})
	if err != nil {
		// 没有排队中的 operation(例如直接再生成命令)时只收敛 Collector 状态。
		if errors.Is(err, pgx.ErrNoRows) {
			_, err = q.ApplyCollectorOperationSuccess(ctx, db.ApplyCollectorOperationSuccessParams{ID: collector.ID, EnterpriseID: enterpriseID, Column3: operation})
			return err
		}
		return err
	}
	if latest.ExecutorKind != "bootstrap" || latest.Operation != operation {
		return nil
	}
	// Repair the projected Collector status if an earlier transaction or release
	// completed the operation without converging the projection. This stays
	// idempotent because the expected status returns above.
	if latest.Status == "succeeded" {
		_, err = q.ApplyCollectorOperationSuccess(ctx, db.ApplyCollectorOperationSuccessParams{ID: collector.ID, EnterpriseID: enterpriseID, Column3: operation})
		return err
	}
	if latest.Status != "queued" && latest.Status != "result_unknown" {
		return resource.ErrActionInvalidated
	}
	if _, err = q.FinishTelemetryCollectorOperation(ctx, db.FinishTelemetryCollectorOperationParams{ID: latest.ID,
		EnterpriseID: enterpriseID, Fence: latest.Fence, Status: "succeeded", ResultHash: latest.PlanHash}); err != nil {
		return err
	}
	if _, err = q.ApplyCollectorOperationSuccess(ctx, db.ApplyCollectorOperationSuccessParams{ID: collector.ID, EnterpriseID: enterpriseID, Column3: operation}); err != nil {
		return err
	}
	if operation != "uninstall" {
		if _, err = q.MarkCollectorConfigEffective(ctx, db.MarkCollectorConfigEffectiveParams{CollectorID: collector.ID, Revision: collector.DesiredRevision}); err != nil {
			return err
		}
		if _, err = q.MarkTelemetryRouteActive(ctx, db.MarkTelemetryRouteActiveParams{CollectorID: collector.ID, EnterpriseID: enterpriseID}); err != nil {
			return err
		}
		_, err = q.FinalizeCollectorClaimMigrations(ctx, db.FinalizeCollectorClaimMigrationsParams{EnterpriseID: enterpriseID, CollectorID: collector.ID})
		return err
	}
	_, err = q.MarkTelemetryTunnelRemoved(ctx, db.MarkTelemetryTunnelRemovedParams{CollectorID: collector.ID,
		EnterpriseID: enterpriseID, Column3: "", LastDropReason: "collector_uninstalled"})
	return err
}

func (service SelfEnrollService) attachTrustedSigningKey(payload *BootstrapPayload) error {
	if payload == nil || !payload.HasArtifact || payload.Artifact.Signature == "" || payload.Artifact.SigningKeyID == "" {
		return ErrDistributionPending
	}
	encoded, ok := service.SigningPublicKeys[payload.Artifact.SigningKeyID]
	if !ok || encoded == "" {
		return ErrDistributionPending
	}
	publicKey, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return ErrDistributionPending
	}
	signature, err := base64.RawStdEncoding.DecodeString(payload.Artifact.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return ErrDistributionPending
	}
	payload.SigningPublicKey = encoded
	payload.SigningKeyID = payload.Artifact.SigningKeyID
	return nil
}

func encryptBootstrapExchange(key []byte, record db.HostEnrollmentToken, payload BootstrapPayload) ([]byte, []byte, error) {
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	defer clear(plaintext)
	aead, err := bootstrapExchangeAEAD(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, err
	}
	return nonce, aead.Seal(nil, nonce, plaintext, bootstrapExchangeAAD(record)), nil
}

func decryptBootstrapExchange(key []byte, record db.HostEnrollmentToken) (BootstrapPayload, error) {
	if !record.ExchangeKeyVersion.Valid || record.ExchangeKeyVersion.Int32 != 1 {
		return BootstrapPayload{}, ErrHostInstallTokenInvalid
	}
	aead, err := bootstrapExchangeAEAD(key)
	if err != nil || len(record.ExchangeNonce) != aead.NonceSize() {
		return BootstrapPayload{}, ErrHostInstallTokenInvalid
	}
	plaintext, err := aead.Open(nil, record.ExchangeNonce, record.ExchangeCiphertext, bootstrapExchangeAAD(record))
	if err != nil {
		return BootstrapPayload{}, ErrHostInstallTokenInvalid
	}
	defer clear(plaintext)
	var payload BootstrapPayload
	if json.Unmarshal(plaintext, &payload) != nil || payload.EnrollmentToken == "" || !payload.HasArtifact ||
		payload.CollectorID != record.CollectorID || payload.HostID != record.PreallocatedHostID {
		return BootstrapPayload{}, ErrHostInstallTokenInvalid
	}
	return payload, nil
}

func bootstrapExchangeAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("host bootstrap encryption key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func bootstrapExchangeAAD(record db.HostEnrollmentToken) []byte {
	return []byte(fmt.Sprintf("argus.host_enrollment_exchange/v1\x00%s\x00%s", record.EnterpriseID, record.ID))
}

func sanitizeSelfReport(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) > limit {
		return value[:limit]
	}
	return value
}

func selfReportedArch(value string) string {
	if value == "amd64" || value == "arm64" {
		return value
	}
	return ""
}

// LoadSelfEnrollSigningKeys 读取 ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS(JSON:
// keyID → base64 raw ed25519 公钥),与执行侧 collectormanager 使用同一信任根格式。
func LoadSelfEnrollSigningKeys() map[string]string {
	raw := os.Getenv("ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS")
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var encoded map[string]string
	if json.Unmarshal([]byte(raw), &encoded) != nil {
		return nil
	}
	return encoded
}
