package connector

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"

	"github.com/kakj-go/Argus/internal/operationsecret"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

var (
	ErrBastionState                 = errors.New("bastion scope state conflict")
	ErrBastionNameConflict          = errors.New("bastion name already belongs to a live resource")
	ErrConnectorArtifactInvalid     = errors.New("connector artifact manifest is invalid")
	ErrConnectorArtifactUnavailable = errors.New("connector artifact is unavailable")
	ErrControlTunnelUnavailable     = errors.New("connector control tunnel is unavailable")
)

type BastionService struct {
	Store                         *postgres.Store
	Actions                       resource.PendingActionService
	Enrollment                    Service
	OperationSecretKey            []byte
	ConnectorEnrollForwardTarget  string
	ConnectorGatewayForwardTarget string
}

// NewBastionService is the single production composition boundary for bastion
// actions. Both the HTTP server (preview) and Action Worker (commit) must use
// the same runtime inputs; otherwise an action can be accepted by one process
// and become impossible to commit in the other.
func NewBastionService(
	store *postgres.Store,
	actions resource.PendingActionService,
	enrollment Service,
	operationSecretKey []byte,
	connectorEnrollForwardTarget string,
	connectorGatewayForwardTarget string,
) (BastionService, error) {
	if store == nil || len(operationSecretKey) != 32 || connectorEnrollForwardTarget == "" || connectorGatewayForwardTarget == "" {
		return BastionService{}, errors.New("bastion service runtime configuration is incomplete")
	}
	return BastionService{
		Store:                         store,
		Actions:                       actions,
		Enrollment:                    enrollment,
		OperationSecretKey:            bytes.Clone(operationSecretKey),
		ConnectorEnrollForwardTarget:  connectorEnrollForwardTarget,
		ConnectorGatewayForwardTarget: connectorGatewayForwardTarget,
	}, nil
}

type BastionInput struct {
	Name, Environment string
	Labels            map[string]string
	ExpectedVersion   int64
	// PlanV4 Task 06 安装模式:command=一次性命令;direct_install=平台 SSH
	// 代装(双向可达);direct_install_tunnel=代装+控制通道隧道(堡垒机无出站)。
	InstallMode      string
	Address          string
	Port             int32
	Username         string
	Platform         string
	CredentialID     uuid.NullUUID
	ConnectionTestID uuid.NullUUID
}

type bastionPlan struct {
	Operation           string        `json:"operation"`
	ScopeID             uuid.UUID     `json:"scope_id"`
	HostID              uuid.UUID     `json:"host_id"`
	Input               BastionInput  `json:"input"`
	Architecture        string        `json:"architecture,omitempty"`
	ReleaseVersionID    uuid.NullUUID `json:"release_version_id,omitempty"`
	ReleaseManifestHash []byte        `json:"release_manifest_hash,omitempty"`
}

type bastionSnapshot struct {
	ResourceVersion   int64 `json:"resource_version"`
	FencingGeneration int64 `json:"fencing_generation"`
	MemberCount       int64 `json:"member_count"`
	// Replacement is intentionally tolerant of the expected offline transition
	// between preview and commit. Other lifecycle actions retain exact status.
	Status string `json:"status"`
}

func bastionSnapshotStatus(operation, status string) string {
	if operation == "replace" && replacementStatusAllowed(status) {
		return "replaceable"
	}
	return status
}

func replacementStatusAllowed(status string) bool {
	return status == "active" || status == "suspected_offline" || status == "offline" || status == "uninstalled"
}

type connectorUninstallPlan struct {
	ConnectorID       uuid.UUID     `json:"connector_id"`
	ExpectedVersion   int64         `json:"expected_version"`
	ConnectionEpoch   int64         `json:"connection_epoch"`
	BastionScopeID    uuid.NullUUID `json:"bastion_scope_id,omitempty"`
	KubernetesID      uuid.NullUUID `json:"kubernetes_cluster_id,omitempty"`
	FencingGeneration int64         `json:"fencing_generation"`
}

type connectorUninstallSnapshot struct {
	ConnectorID       uuid.UUID `json:"connector_id"`
	Version           int64     `json:"version"`
	ConnectionEpoch   int64     `json:"connection_epoch"`
	Status            string    `json:"status"`
	ResourceID        uuid.UUID `json:"resource_id"`
	ResourceVersion   int64     `json:"resource_version"`
	FencingGeneration int64     `json:"fencing_generation,omitempty"`
}

type ConnectorInstallOperationView struct {
	Operation     db.ConnectorInstallOperation
	Events        []db.ConnectorInstallOperationEvent
	ControlTunnel *db.ConnectorControlTunnel
}

type connectorInstallRetryPlan struct {
	SourceOperationID uuid.UUID `json:"source_operation_id"`
	ScopeID           uuid.UUID `json:"scope_id"`
	ScopeVersion      int64     `json:"scope_version"`
	HostID            uuid.UUID `json:"host_id"`
	ConnectionTestID  uuid.UUID `json:"connection_test_id"`
	InstallMode       string    `json:"install_mode"`
}

func bastionEnrollmentPolicy() json.RawMessage {
	return json.RawMessage(`{"capabilities":["host.connection_probe","kubernetes.connection_probe","kubernetes.query","credential.lease","connector.uninstall"]}`)
}

func (service BastionService) List(ctx context.Context, enterpriseID uuid.UUID) ([]db.ListBastionScopesRow, error) {
	return service.Store.Queries.ListBastionScopes(ctx, enterpriseID)
}

func (service BastionService) Get(ctx context.Context, enterpriseID, scopeID uuid.UUID) (db.GetBastionScopeRow, error) {
	return service.Store.Queries.GetBastionScope(ctx, db.GetBastionScopeParams{ID: scopeID, EnterpriseID: enterpriseID})
}

func (service BastionService) ListConnectors(ctx context.Context, enterpriseID uuid.UUID) ([]db.Connector, error) {
	return service.Store.Queries.ListConnectors(ctx, enterpriseID)
}

func (service BastionService) GetConnector(ctx context.Context, enterpriseID, connectorID uuid.UUID) (db.Connector, error) {
	return service.Store.Queries.GetConnector(ctx, db.GetConnectorParams{ID: connectorID, EnterpriseID: enterpriseID})
}

func (service BastionService) GetInstallOperation(ctx context.Context, enterpriseID, operationID uuid.UUID) (ConnectorInstallOperationView, error) {
	operation, err := service.Store.Queries.GetConnectorInstallOperation(ctx, db.GetConnectorInstallOperationParams{ID: operationID, EnterpriseID: enterpriseID})
	if err != nil {
		return ConnectorInstallOperationView{}, err
	}
	events, err := service.Store.Queries.ListConnectorInstallOperationEvents(ctx, db.ListConnectorInstallOperationEventsParams{
		OperationID: operationID, EnterpriseID: enterpriseID,
	})
	if err != nil {
		return ConnectorInstallOperationView{}, err
	}
	view := ConnectorInstallOperationView{Operation: operation, Events: events}
	if operation.InstallMode == "direct_install_tunnel" {
		tunnel, tunnelErr := service.Store.Queries.GetConnectorControlTunnelByConnector(ctx, db.GetConnectorControlTunnelByConnectorParams{
			ConnectorID: operation.ConnectorID, EnterpriseID: enterpriseID,
		})
		if tunnelErr == nil {
			view.ControlTunnel = &tunnel
		} else if !errors.Is(tunnelErr, pgx.ErrNoRows) {
			return ConnectorInstallOperationView{}, tunnelErr
		}
	}
	return view, nil
}

func (service BastionService) PreviewRetryInstall(ctx context.Context, subject resource.Subject, enterpriseID, operationID uuid.UUID, idempotencyKey string) (db.PendingAction, error) {
	operation, command, scope, err := service.validateRetryInstall(ctx, service.Store.Queries, enterpriseID, operationID)
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := connectorInstallRetryPlan{SourceOperationID: operation.ID, ScopeID: scope.ID, ScopeVersion: scope.ResourceVersion,
		HostID: operation.HostID, ConnectionTestID: operation.ConnectionTestID, InstallMode: operation.InstallMode}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{
		ActionType: "bastion.connector.install.retry", Title: "Retry Connector installation", Summary: "Retry Connector installation for " + scope.Name,
		Risk: "write", ResourceType: "bastion_scope", ResourceID: uuid.NullUUID{UUID: scope.ID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: scope.ResourceVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"scope_id": scope.ID, "name": scope.Name, "source_operation_id": operation.ID, "install_mode": operation.InstallMode,
			"target_address": command.GetTargetAddress()},
		Diff: []map[string]string{{"kind": "change", "text": "Retry Connector installation"}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: resource.NewResourceAuthorizationSnapshot("host", operation.HostID),
		CommitHandler:         "argus.bastion.connector.install.retry.commit",
	}, idempotencyKey)
}

func (service BastionService) validateRetryInstall(ctx context.Context, q *db.Queries, enterpriseID, operationID uuid.UUID) (db.ConnectorInstallOperation, *connectorv1.ConnectorInstallCommand, db.GetBastionScopeRow, error) {
	operation, err := q.GetConnectorInstallOperation(ctx, db.GetConnectorInstallOperationParams{ID: operationID, EnterpriseID: enterpriseID})
	if err != nil || (operation.Status != "failed" && operation.Status != "expired" && operation.Status != "result_unknown") {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, ErrBastionState
	}
	latest, err := q.GetLatestConnectorInstallOperationByScope(ctx, db.GetLatestConnectorInstallOperationByScopeParams{
		BastionScopeID: operation.BastionScopeID, EnterpriseID: enterpriseID,
	})
	if err != nil || latest.ID != operation.ID {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, ErrBastionState
	}
	scope, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: operation.BastionScopeID, EnterpriseID: enterpriseID})
	if err != nil || scope.Status != "pending" || scope.ActiveConnectorID.Valid || scope.OnboardingMode != operation.InstallMode {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, ErrBastionState
	}
	var command connectorv1.ConnectorInstallCommand
	if protojson.Unmarshal(operation.Plan, &command) != nil {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, ErrBastionState
	}
	canonical, err := resource.CanonicalJSON(operation.Plan)
	if err != nil {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, ErrBastionState
	}
	planHash := sha256.Sum256(canonical)
	if !bytes.Equal(planHash[:], operation.PlanHash) || command.GetReleaseVersionId() != operation.ReleaseVersionID.String() {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, ErrBastionState
	}
	release, err := q.GetConnectorReleaseVersion(ctx, operation.ReleaseVersionID)
	if err != nil || !connectorCommandMatchesRelease(&command, release) {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, ErrConnectorArtifactUnavailable
	}
	test, err := q.GetConnectionTest(ctx, db.GetConnectionTestParams{ID: operation.ConnectionTestID, EnterpriseID: enterpriseID})
	if err != nil || test.Status != "succeeded" || !time.Now().UTC().Before(test.ExpiresAt.Time) {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, resource.ErrConnectionTestNeeded
	}
	var result resource.ConnectionTestResult
	if json.Unmarshal(test.Result, &result) != nil || result.HostKeyFingerprint != command.GetPinnedHostKey() {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, resource.ErrConnectionTestNeeded
	}
	credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: test.CredentialID.UUID, EnterpriseID: enterpriseID})
	if err != nil || !test.CredentialVersion.Valid || credential.Version != test.CredentialVersion.Int64 || credential.Status != "active" {
		return db.ConnectorInstallOperation{}, nil, db.GetBastionScopeRow{}, resource.ErrConnectionTestNeeded
	}
	return operation, &command, scope, nil
}

func (service BastionService) PreviewCreate(ctx context.Context, subject resource.Subject, enterpriseID uuid.UUID, input BastionInput, idempotencyKey string) (db.PendingAction, error) {
	_, _, err := resource.NormalizeUserLabels(input.Labels)
	if err != nil {
		return db.PendingAction{}, err
	}
	available, err := service.Store.Queries.BastionNameAvailable(ctx, db.BastionNameAvailableParams{
		EnterpriseID: enterpriseID,
		Name:         input.Name,
	})
	if err != nil {
		return db.PendingAction{}, err
	}
	if !available.Valid || !available.Bool {
		return db.PendingAction{}, ErrBastionNameConflict
	}
	scopeID, hostID := newID(), newID()
	snapshot := resource.NewResourceAuthorizationSnapshot("host", hostID)
	plan := bastionPlan{Operation: "create", ScopeID: scopeID, HostID: hostID, Input: input}
	if err := service.freezeDirectInstallPlan(ctx, service.Store.Queries, enterpriseID, &plan); err != nil {
		return db.PendingAction{}, err
	}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{ActionType: "bastion_scope.create", Title: "Create bastion",
		Summary: "Create a stable Bastion Scope and one-time enrollment", Risk: "dangerous", ResourceType: "bastion_scope",
		ResourceID: uuid.NullUUID{UUID: scopeID, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"scope_id": scopeID, "host_id": hostID, "name": input.Name},
		Diff:    []map[string]string{{"kind": "add", "text": "Create bastion " + input.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: snapshot, CommitHandler: "argus.bastion_scope.create.commit"}, idempotencyKey)
}

// PreviewReplacement freezes the replacement path. Mode A produces a new
// command; B/C require a fresh connection test because the original test is a
// short-lived observation and must never be reused as a long-term credential
// or host-key assertion.
func (service BastionService) PreviewReplacement(
	ctx context.Context,
	subject resource.Subject,
	enterpriseID, scopeID uuid.UUID,
	input BastionInput,
	idempotencyKey string,
) (db.PendingAction, error) {
	current, err := service.Get(ctx, enterpriseID, scopeID)
	if err != nil || current.ResourceVersion != input.ExpectedVersion {
		return db.PendingAction{}, resource.ErrVersionConflict
	}
	if !current.ActiveConnectorID.Valid || !current.ConnectorHostID.Valid || !replacementStatusAllowed(current.Status) {
		return db.PendingAction{}, ErrBastionState
	}
	input.InstallMode = current.OnboardingMode
	plan := bastionPlan{Operation: "replace", ScopeID: scopeID, HostID: current.ConnectorHostID.UUID, Input: input}
	if err = service.freezeDirectInstallPlan(ctx, service.Store.Queries, enterpriseID, &plan); err != nil {
		return db.PendingAction{}, err
	}
	snapshot := bastionSnapshot{ResourceVersion: current.ResourceVersion, FencingGeneration: current.FencingGeneration,
		MemberCount: current.MemberCount, Status: bastionSnapshotStatus("replace", current.Status)}
	preview := map[string]any{"scope_id": scopeID, "name": current.Name, "active_connector_id": current.ActiveConnectorID.UUID,
		"install_mode": current.OnboardingMode, "member_count": current.MemberCount, "fencing_generation": current.FencingGeneration}
	if input.ConnectionTestID.Valid {
		preview["connection_test_id"] = input.ConnectionTestID.UUID
	}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{
		ActionType: "bastion.connector.replace", Title: "Replace bastion Connector", Summary: "Fence and replace Connector for " + current.Name,
		Risk: "dangerous", ResourceType: "bastion_scope", ResourceID: uuid.NullUUID{UUID: scopeID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: input.ExpectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: preview, Diff: []map[string]string{
			{"kind": "remove", "text": "Fence the active Connector"},
			{"kind": "add", "text": "Install and enroll its replacement"},
		}, ImmutablePlan: plan, ResourceScopeSnapshot: snapshot, CommitHandler: "argus.bastion.connector.replace.commit",
	}, idempotencyKey)
}

func (service BastionService) PreviewUpdate(ctx context.Context, subject resource.Subject, enterpriseID, scopeID uuid.UUID, input BastionInput, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.Get(ctx, enterpriseID, scopeID)
	if err != nil || current.ResourceVersion != input.ExpectedVersion || !current.ConnectorHostID.Valid {
		return db.PendingAction{}, resource.ErrVersionConflict
	}
	if input.Labels != nil {
		before, _ := resource.DecodeLabels(current.Labels)
		encoded, _, normalizeErr := resource.NormalizeUserLabels(resource.MergeSystemLabels(input.Labels, before))
		if normalizeErr != nil {
			return db.PendingAction{}, normalizeErr
		}
		_ = encoded
	}
	snapshot := resource.NewResourceAuthorizationSnapshot("host", current.ConnectorHostID.UUID)
	plan := bastionPlan{Operation: "update", ScopeID: scopeID, HostID: current.ConnectorHostID.UUID, Input: input}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{ActionType: "bastion_scope.update", Title: "Update bastion",
		Summary: "Update Bastion Scope metadata", Risk: "write", ResourceType: "bastion_scope", ResourceID: uuid.NullUUID{UUID: scopeID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: input.ExpectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"scope_id": scopeID, "name": current.Name},
		Diff:    []map[string]string{{"kind": "change", "text": "Update bastion " + current.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: snapshot, CommitHandler: "argus.bastion_scope.update.commit"}, idempotencyKey)
}

func (service BastionService) PreviewLifecycle(ctx context.Context, subject resource.Subject, enterpriseID, scopeID uuid.UUID, expectedVersion int64, operation, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.Get(ctx, enterpriseID, scopeID)
	if err != nil || current.ResourceVersion != expectedVersion {
		return db.PendingAction{}, resource.ErrVersionConflict
	}
	deletable := current.Status == "uninstalled" || current.Status == "pending" ||
		(current.Status == "offline" && !current.ActiveConnectorID.Valid)
	if operation == "delete" && (current.MemberCount != 0 || !deletable) {
		return db.PendingAction{}, ErrBastionState
	}
	if operation == "rotate" && (current.OnboardingMode != "command" || current.Status != "pending" || current.ActiveConnectorID.Valid) {
		return db.PendingAction{}, ErrBastionState
	}
	if operation == "replace" {
		return db.PendingAction{}, resource.ErrActionInvalidated
	}
	if !current.ConnectorHostID.Valid {
		// Every live Scope owns exactly one live connector_local root Host. Never
		// prepare a lifecycle action with uuid.Nil: delete must be all-or-nothing.
		return db.PendingAction{}, ErrBastionState
	}
	snapshot := bastionSnapshot{ResourceVersion: current.ResourceVersion, FencingGeneration: current.FencingGeneration, MemberCount: current.MemberCount, Status: bastionSnapshotStatus(operation, current.Status)}
	plan := bastionPlan{Operation: operation, ScopeID: scopeID, HostID: current.ConnectorHostID.UUID, Input: BastionInput{ExpectedVersion: expectedVersion}}
	if operation == "rotate" {
		plan.Input.InstallMode = "command"
		if err := service.freezeDirectInstallPlan(ctx, service.Store.Queries, enterpriseID, &plan); err != nil {
			return db.PendingAction{}, err
		}
	}
	risk, verb, actionType := "dangerous", "Delete", "bastion_scope.delete"
	if operation == "replace" {
		verb, actionType = "Replace connector for", "bastion.connector.replace"
	} else if operation == "rotate" {
		risk, verb, actionType = "write", "Rotate enrollment for", "bastion.enrollment.rotate"
	}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{ActionType: actionType, Title: verb + " bastion",
		Summary: verb + " " + current.Name, Risk: risk, ResourceType: "bastion_scope", ResourceID: uuid.NullUUID{UUID: scopeID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: expectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"scope_id": scopeID, "name": current.Name, "member_count": current.MemberCount, "fencing_generation": current.FencingGeneration},
		Diff:    []map[string]string{{"kind": "remove", "text": verb + " " + current.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: snapshot, CommitHandler: "argus." + actionType + ".commit"}, idempotencyKey)
}

func (service BastionService) PreviewConnectorUninstall(ctx context.Context, subject resource.Subject, enterpriseID, connectorID uuid.UUID, expectedVersion int64, idempotencyKey string) (db.PendingAction, error) {
	connector, err := service.Store.Queries.GetConnector(ctx, db.GetConnectorParams{ID: connectorID, EnterpriseID: enterpriseID})
	if err != nil || connector.Version != expectedVersion || connector.Status != "online" || connector.ConnectionEpoch < 1 {
		return db.PendingAction{}, ErrBastionState
	}
	snapshot, plan, err := service.connectorUninstallState(ctx, service.Store.Queries, connector)
	if err != nil {
		return db.PendingAction{}, err
	}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{
		ActionType: "connector.uninstall", Title: "Uninstall connector", Summary: "Remove the Connector identity and fence its managed route",
		Risk: "dangerous", ResourceType: "connector", ResourceID: uuid.NullUUID{UUID: connector.ID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: expectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"connector_id": connector.ID, "name": connector.Name, "role": connector.Role, "connection_epoch": connector.ConnectionEpoch},
		Diff:    []map[string]string{{"kind": "remove", "text": "Uninstall connector " + connector.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: snapshot, CommitHandler: "argus.connector.uninstall.commit",
	}, idempotencyKey)
}

func (service BastionService) connectorUninstallState(ctx context.Context, q *db.Queries, connector db.Connector) (connectorUninstallSnapshot, connectorUninstallPlan, error) {
	snapshot := connectorUninstallSnapshot{ConnectorID: connector.ID, Version: connector.Version, ConnectionEpoch: connector.ConnectionEpoch, Status: connector.Status}
	plan := connectorUninstallPlan{ConnectorID: connector.ID, ExpectedVersion: connector.Version, ConnectionEpoch: connector.ConnectionEpoch,
		BastionScopeID: connector.BastionScopeID, KubernetesID: connector.KubernetesClusterID}
	switch connector.Role {
	case "bastion":
		if !connector.BastionScopeID.Valid {
			return snapshot, plan, ErrBastionState
		}
		scope, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: connector.BastionScopeID.UUID, EnterpriseID: connector.EnterpriseID})
		if err != nil || !scope.ActiveConnectorID.Valid || scope.ActiveConnectorID.UUID != connector.ID || scope.Status != "active" {
			return snapshot, plan, ErrBastionState
		}
		snapshot.ResourceID, snapshot.ResourceVersion, snapshot.FencingGeneration = scope.ID, scope.ResourceVersion, scope.FencingGeneration
		plan.FencingGeneration = scope.FencingGeneration
	case "kubernetes":
		if !connector.KubernetesClusterID.Valid {
			return snapshot, plan, ErrBastionState
		}
		cluster, err := q.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: connector.KubernetesClusterID.UUID, EnterpriseID: connector.EnterpriseID})
		if err != nil || !cluster.ConnectorID.Valid || cluster.ConnectorID.UUID != connector.ID || cluster.ConnectionStatus != "connected" {
			return snapshot, plan, ErrBastionState
		}
		snapshot.ResourceID, snapshot.ResourceVersion = cluster.ID, cluster.ResourceVersion
	default:
		return snapshot, plan, ErrBastionState
	}
	return snapshot, plan, nil
}

func (service BastionService) RevalidateAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) ([]byte, error) {
	if action.ActionType == "bastion.connector.install.retry" {
		var plan connectorInstallRetryPlan
		if json.Unmarshal(raw, &plan) != nil {
			return nil, resource.ErrActionInvalidated
		}
		operation, _, scope, err := service.validateRetryInstall(ctx, q, action.EnterpriseID, plan.SourceOperationID)
		if err != nil || scope.ID != plan.ScopeID || scope.ResourceVersion != plan.ScopeVersion || operation.HostID != plan.HostID ||
			operation.ConnectionTestID != plan.ConnectionTestID || operation.InstallMode != plan.InstallMode {
			return nil, resource.ErrActionInvalidated
		}
		return resource.HashResourceAuthorizationSnapshot("host", plan.HostID)
	}
	if action.ResourceType == "connector" {
		var plan connectorUninstallPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return nil, err
		}
		connector, err := q.GetConnector(ctx, db.GetConnectorParams{ID: plan.ConnectorID, EnterpriseID: action.EnterpriseID})
		if err != nil || connector.Version != plan.ExpectedVersion || connector.ConnectionEpoch != plan.ConnectionEpoch || connector.Status != "online" {
			return nil, resource.ErrActionInvalidated
		}
		snapshot, currentPlan, err := service.connectorUninstallState(ctx, q, connector)
		if err != nil || currentPlan.BastionScopeID != plan.BastionScopeID || currentPlan.KubernetesID != plan.KubernetesID || currentPlan.FencingGeneration != plan.FencingGeneration {
			return nil, resource.ErrActionInvalidated
		}
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(encoded)
		return hash[:], nil
	}
	if action.ResourceType != "bastion_scope" {
		return nil, resource.ErrActionInvalidated
	}
	var plan bastionPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return nil, err
	}
	if plan.Operation == "create" {
		if err := service.validateFrozenDirectInstallPlan(ctx, q, action.EnterpriseID, plan); err != nil {
			return nil, resource.ErrActionInvalidated
		}
		return resource.HashResourceAuthorizationSnapshot("host", plan.HostID)
	}
	current, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID})
	if err != nil || current.ResourceVersion != plan.Input.ExpectedVersion {
		return nil, resource.ErrActionInvalidated
	}
	if plan.Operation == "update" {
		return resource.HashResourceAuthorizationSnapshot("host", plan.HostID)
	}
	if plan.Operation == "replace" {
		if !current.ActiveConnectorID.Valid || !current.ConnectorHostID.Valid || current.ConnectorHostID.UUID != plan.HostID ||
			!replacementStatusAllowed(current.Status) || current.OnboardingMode != plan.Input.InstallMode ||
			service.validateFrozenDirectInstallPlan(ctx, q, action.EnterpriseID, plan) != nil {
			return nil, resource.ErrActionInvalidated
		}
	}
	if plan.Operation == "rotate" && (current.OnboardingMode != "command" || current.Status != "pending" || current.ActiveConnectorID.Valid ||
		service.validateFrozenDirectInstallPlan(ctx, q, action.EnterpriseID, plan) != nil) {
		return nil, resource.ErrActionInvalidated
	}
	snapshot := bastionSnapshot{ResourceVersion: current.ResourceVersion, FencingGeneration: current.FencingGeneration, MemberCount: current.MemberCount, Status: bastionSnapshotStatus(plan.Operation, current.Status)}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func (service BastionService) CommitAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (resource.ActionCommitResult, error) {
	if action.ActionType == "bastion.connector.install.retry" {
		return service.commitRetryInstall(ctx, q, action, raw)
	}
	if action.ResourceType == "connector" {
		return service.commitConnectorUninstall(ctx, q, action, raw)
	}
	if action.ResourceType != "bastion_scope" {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	var plan bastionPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return resource.ActionCommitResult{}, err
	}
	switch plan.Operation {
	case "create":
		labels, hash, err := resource.NormalizeStoredLabels(plan.Input.Labels)
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		onboardingMode := plan.Input.InstallMode
		scope, err := q.CreateBastionScope(ctx, db.CreateBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID, Name: plan.Input.Name,
			Environment: plan.Input.Environment, Labels: labels, LabelsHash: hash, OnboardingMode: onboardingMode})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		hostAddress, hostPort, pinnedHostKey := "connector://"+plan.HostID.String(), int32(1), ""
		if plan.Input.InstallMode == "direct_install" || plan.Input.InstallMode == "direct_install_tunnel" {
			_, connectionResult, validationErr := validateDirectInstallInput(ctx, q, action.EnterpriseID, plan.Input)
			if validationErr != nil {
				return resource.ActionCommitResult{}, validationErr
			}
			hostAddress, hostPort, pinnedHostKey = plan.Input.Address, plan.Input.Port, connectionResult.HostKeyFingerprint
		}
		host, err := q.CreateHost(ctx, db.CreateHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID, Name: plan.Input.Name,
			Hostname: "", Address: hostAddress, Port: hostPort, Platform: "linux",
			Architecture: pgtype.Text{String: plan.Architecture, Valid: plan.Architecture != ""}, ConnectionMode: "connector_local",
			BastionScopeID: uuid.NullUUID{UUID: scope.ID, Valid: true}, Environment: plan.Input.Environment, Labels: labels, LabelsHash: hash,
			ConnectionStatus: "onboarding", PinnedHostKey: pinnedHostKey})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		scope, err = q.AttachBastionRootHost(ctx, db.AttachBastionRootHostParams{
			ID: plan.ScopeID, EnterpriseID: action.EnterpriseID,
			ConnectorHostID: uuid.NullUUID{UUID: host.ID, Valid: true},
		})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		creator := action.CreatorSubjectID
		if _, err := q.AddDataAuthorizationGrant(ctx, db.AddDataAuthorizationGrantParams{ID: newID(), EnterpriseID: action.EnterpriseID,
			SubjectType: action.CreatorSubjectType, SubjectID: creator, ResourceType: "host", ResourceID: host.ID,
			CreatedBy: uuid.NullUUID{UUID: creator, Valid: true}}); err != nil {
			return resource.ActionCommitResult{}, err
		}
		enrollment, err := service.Enrollment.CreateEnrollment(ctx, q, action.CreatorSubjectID.String(), action.EnterpriseID, CreateEnrollmentInput{Role: "bastion",
			Purpose: "initial_registration", BastionScopeID: uuid.NullUUID{UUID: scope.ID, Valid: true}, HostID: uuid.NullUUID{UUID: plan.HostID, Valid: true},
			ManualInstall: plan.Input.InstallMode == "command", ReleaseVersionID: manualConnectorReleaseID(plan), Policy: bastionEnrollmentPolicy()})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		if plan.Input.InstallMode == "direct_install" || plan.Input.InstallMode == "direct_install_tunnel" {
			// 平台代装:enroll 命令(内含一次性令牌)只进入执行器操作计划并经
			// SSH 交付,不经 one-time result 到达浏览器。
			operation, opErr := service.enqueueDirectInstall(ctx, q, action, plan, scope, enrollment, uuid.NullUUID{})
			if opErr != nil {
				return resource.ActionCommitResult{}, opErr
			}
			return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion,
				Summary: "Bastion Scope created; platform install queued", ConnectorInstallOperationID: uuid.NullUUID{UUID: operation.ID, Valid: true}}, nil
		}
		return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion,
			Summary: "Bastion Scope created", OneTimeCommand: &resource.OneTimeCommandResult{
				InstructionSets: enrollment.InstructionSets, ExpiresAt: enrollment.Record.ExpiresAt.Time}, OneTimeResultKind: "connector_install_command"}, nil
	case "update":
		params := db.UpdateBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID, ResourceVersion: plan.Input.ExpectedVersion,
			Name: pgtype.Text{String: plan.Input.Name, Valid: plan.Input.Name != ""}, Environment: pgtype.Text{String: plan.Input.Environment, Valid: plan.Input.Environment != ""}}
		if plan.Input.Labels != nil {
			current, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID})
			if err != nil {
				return resource.ActionCommitResult{}, err
			}
			before, _ := resource.DecodeLabels(current.Labels)
			params.Labels, params.LabelsHash, err = resource.NormalizeStoredLabels(resource.MergeSystemLabels(plan.Input.Labels, before))
			if err != nil {
				return resource.ActionCommitResult{}, err
			}
			host, err := q.GetHost(ctx, db.GetHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID})
			if err != nil {
				return resource.ActionCommitResult{}, err
			}
			if _, err := q.UpdateHost(ctx, db.UpdateHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID, ResourceVersion: host.ResourceVersion,
				Labels: params.Labels, LabelsHash: params.LabelsHash}); err != nil {
				return resource.ActionCommitResult{}, err
			}
		}
		scope, err := q.UpdateBastionScope(ctx, params)
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion, Summary: "Bastion Scope updated"}, nil
	case "rotate", "replace":
		current, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID})
		if err != nil {
			return resource.ActionCommitResult{}, ErrBastionState
		}
		var scope db.BastionScope
		if plan.Operation == "rotate" {
			scope, err = q.TouchPendingBastionScope(ctx, db.TouchPendingBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID, ResourceVersion: plan.Input.ExpectedVersion})
			if err != nil {
				return resource.ActionCommitResult{}, ErrBastionState
			}
		} else {
			scope, err = q.FenceBastionScope(ctx, db.FenceBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID, ResourceVersion: plan.Input.ExpectedVersion})
			if err != nil {
				return resource.ActionCommitResult{}, ErrBastionState
			}
		}
		if plan.Operation == "replace" && current.ActiveConnectorID.Valid {
			connectorID := current.ActiveConnectorID.UUID
			if rows, fenceErr := q.FenceConnectorForReplacement(ctx, db.FenceConnectorForReplacementParams{
				ID: connectorID, EnterpriseID: action.EnterpriseID,
			}); fenceErr != nil || rows != 1 {
				return resource.ActionCommitResult{}, ErrBastionState
			}
			if err = q.RevokeConnectorCertificates(ctx, db.RevokeConnectorCertificatesParams{
				ConnectorID: connectorID, EnterpriseID: action.EnterpriseID,
			}); err != nil {
				return resource.ActionCommitResult{}, err
			}
			if err = q.RevokePKISubjectCertificates(ctx, db.RevokePKISubjectCertificatesParams{SubjectKind: "connector",
				SubjectID: connectorID.String(), RevocationReason: "connector_replaced"}); err != nil {
				return resource.ActionCommitResult{}, err
			}
			if _, err = q.DeleteConnectorSessionsForReplacement(ctx, db.DeleteConnectorSessionsForReplacementParams{
				ConnectorID: connectorID, EnterpriseID: action.EnterpriseID,
			}); err != nil {
				return resource.ActionCommitResult{}, err
			}
			if _, err = q.RevokeConnectorControlTunnelLeases(ctx, db.RevokeConnectorControlTunnelLeasesParams{
				ConnectorID: connectorID, EnterpriseID: action.EnterpriseID,
			}); err != nil {
				return resource.ActionCommitResult{}, err
			}
			if _, err = q.MarkConnectorControlTunnelRemoved(ctx, db.MarkConnectorControlTunnelRemovedParams{
				ConnectorID: connectorID, EnterpriseID: action.EnterpriseID, LastDropReason: "connector_replaced",
			}); err != nil {
				return resource.ActionCommitResult{}, err
			}
			if plan.Input.InstallMode == "direct_install" || plan.Input.InstallMode == "direct_install_tunnel" {
				host, hostErr := q.GetHost(ctx, db.GetHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID})
				if hostErr != nil {
					return resource.ActionCommitResult{}, hostErr
				}
				_, connectionResult, hostErr := validateDirectInstallInput(ctx, q, action.EnterpriseID, plan.Input)
				if hostErr != nil {
					return resource.ActionCommitResult{}, hostErr
				}
				if _, hostErr = q.UpdateHost(ctx, db.UpdateHostParams{ID: host.ID, EnterpriseID: action.EnterpriseID,
					ResourceVersion: host.ResourceVersion, Address: pgtype.Text{String: plan.Input.Address, Valid: true},
					Port: pgtype.Int4{Int32: plan.Input.Port, Valid: true}, PinnedHostKey: pgtype.Text{String: connectionResult.HostKeyFingerprint, Valid: true},
					Architecture: pgtype.Text{String: plan.Architecture, Valid: true}}); hostErr != nil {
					return resource.ActionCommitResult{}, hostErr
				}
			}
		}
		if err = q.RevokeActiveEnrollmentTokens(ctx, db.RevokeActiveEnrollmentTokensParams{EnterpriseID: action.EnterpriseID,
			BastionScopeID: uuid.NullUUID{UUID: plan.ScopeID, Valid: true}}); err != nil {
			return resource.ActionCommitResult{}, err
		}
		purpose := "initial_registration"
		if plan.Operation == "replace" {
			purpose = "connector_replacement"
		}
		enrollment, err := service.Enrollment.CreateEnrollment(ctx, q, action.CreatorSubjectID.String(), action.EnterpriseID, CreateEnrollmentInput{Role: "bastion",
			Purpose: purpose, BastionScopeID: uuid.NullUUID{UUID: plan.ScopeID, Valid: true}, HostID: uuid.NullUUID{UUID: plan.HostID, Valid: true},
			ManualInstall: plan.Input.InstallMode == "command", ReleaseVersionID: manualConnectorReleaseID(plan), Policy: bastionEnrollmentPolicy()})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		if plan.Operation == "replace" && (scope.OnboardingMode == "direct_install" || scope.OnboardingMode == "direct_install_tunnel") {
			operation, opErr := service.enqueueDirectInstall(ctx, q, action, plan, scope, enrollment, uuid.NullUUID{})
			if opErr != nil {
				return resource.ActionCommitResult{}, opErr
			}
			return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion,
				Summary: "Connector replacement install queued", ConnectorInstallOperationID: uuid.NullUUID{UUID: operation.ID, Valid: true}}, nil
		}
		return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion,
			Summary: "Connector enrollment command created", OneTimeCommand: &resource.OneTimeCommandResult{
				InstructionSets: enrollment.InstructionSets, ExpiresAt: enrollment.Record.ExpiresAt.Time}, OneTimeResultKind: "connector_install_command"}, nil
	case "delete":
		// 撤销未消费的注册令牌:删除后旧安装命令必须失效。
		_ = q.RevokeActiveEnrollmentTokens(ctx, db.RevokeActiveEnrollmentTokensParams{EnterpriseID: action.EnterpriseID,
			BastionScopeID: uuid.NullUUID{UUID: plan.ScopeID, Valid: true}})
		_, _ = q.RevokeConnectorControlTunnelLeasesByScope(ctx, db.RevokeConnectorControlTunnelLeasesByScopeParams{
			BastionScopeID: plan.ScopeID, EnterpriseID: action.EnterpriseID})
		_, _ = q.MarkConnectorControlTunnelsRemovedByScope(ctx, db.MarkConnectorControlTunnelsRemovedByScopeParams{
			BastionScopeID: plan.ScopeID, EnterpriseID: action.EnterpriseID, LastDropReason: "bastion_scope_deleted"})
		if plan.HostID == uuid.Nil {
			return resource.ActionCommitResult{}, ErrBastionState
		}
		if _, err := q.DeleteBastionRootHost(ctx, db.DeleteBastionRootHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID,
			BastionScopeID: uuid.NullUUID{UUID: plan.ScopeID, Valid: true}}); err != nil {
			return resource.ActionCommitResult{}, ErrBastionState
		}
		scope, err := q.DeleteBastionScope(ctx, db.DeleteBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID, ResourceVersion: plan.Input.ExpectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return resource.ActionCommitResult{}, ErrBastionState
		}
		return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion, Summary: "Bastion Scope deleted"}, err
	default:
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
}

func (service BastionService) commitRetryInstall(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (resource.ActionCommitResult, error) {
	var retry connectorInstallRetryPlan
	if json.Unmarshal(raw, &retry) != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	_, oldCommand, scope, err := service.validateRetryInstall(ctx, q, action.EnterpriseID, retry.SourceOperationID)
	if err != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	_ = q.RevokeActiveEnrollmentTokens(ctx, db.RevokeActiveEnrollmentTokensParams{EnterpriseID: action.EnterpriseID,
		BastionScopeID: uuid.NullUUID{UUID: scope.ID, Valid: true}})
	enrollment, err := service.Enrollment.CreateEnrollment(ctx, q, action.CreatorSubjectID.String(), action.EnterpriseID, CreateEnrollmentInput{
		Role: "bastion", Purpose: "install_retry", BastionScopeID: uuid.NullUUID{UUID: scope.ID, Valid: true},
		HostID: uuid.NullUUID{UUID: retry.HostID, Valid: true}, Policy: json.RawMessage(`{}`),
	})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	credentialID, err := uuid.Parse(oldCommand.GetCredentialId())
	if err != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	input := BastionInput{InstallMode: retry.InstallMode, Address: oldCommand.GetTargetAddress(), Port: int32(oldCommand.GetTargetPort()),
		Username: oldCommand.GetTargetUsername(), CredentialID: uuid.NullUUID{UUID: credentialID, Valid: true},
		ConnectionTestID: uuid.NullUUID{UUID: retry.ConnectionTestID, Valid: true}}
	releaseID, parseErr := uuid.Parse(oldCommand.GetReleaseVersionId())
	if parseErr != nil {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	release, err := q.GetConnectorReleaseVersion(ctx, releaseID)
	if err != nil || !connectorCommandMatchesRelease(oldCommand, release) {
		return resource.ActionCommitResult{}, resource.ErrActionInvalidated
	}
	scopeRecord := db.BastionScope{ID: scope.ID, EnterpriseID: scope.EnterpriseID, Name: scope.Name, Environment: scope.Environment,
		Status: scope.Status, ConnectorHostID: scope.ConnectorHostID, ActiveConnectorID: scope.ActiveConnectorID,
		FencingGeneration: scope.FencingGeneration, ResourceVersion: scope.ResourceVersion, OnboardingMode: scope.OnboardingMode}
	operation, err := service.enqueueDirectInstall(ctx, q, action,
		bastionPlan{Operation: "retry", ScopeID: scope.ID, HostID: retry.HostID, Input: input,
			ReleaseVersionID: uuid.NullUUID{UUID: release.ID, Valid: true}, ReleaseManifestHash: bytes.Clone(release.ManifestHash)}, scopeRecord, enrollment,
		uuid.NullUUID{UUID: retry.SourceOperationID, Valid: true})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion,
		Summary: "Connector install retry queued", ConnectorInstallOperationID: uuid.NullUUID{UUID: operation.ID, Valid: true}}, nil
}

func (service BastionService) commitConnectorUninstall(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (resource.ActionCommitResult, error) {
	var plan connectorUninstallPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		return resource.ActionCommitResult{}, err
	}
	connector, err := q.StartConnectorUninstall(ctx, db.StartConnectorUninstallParams{ID: plan.ConnectorID, EnterpriseID: action.EnterpriseID,
		Version: plan.ExpectedVersion, ConnectionEpoch: plan.ConnectionEpoch})
	if err != nil {
		return resource.ActionCommitResult{}, ErrBastionState
	}
	if plan.BastionScopeID.Valid {
		rows, err := q.MarkBastionScopeUninstalling(ctx, db.MarkBastionScopeUninstallingParams{EnterpriseID: action.EnterpriseID,
			ActiveConnectorID: uuid.NullUUID{UUID: connector.ID, Valid: true}})
		if err != nil || rows != 1 {
			return resource.ActionCommitResult{}, ErrBastionState
		}
	} else {
		rows, err := q.MarkKubernetesConnectorUninstalling(ctx, db.MarkKubernetesConnectorUninstallingParams{EnterpriseID: action.EnterpriseID,
			ConnectorID: uuid.NullUUID{UUID: connector.ID, Valid: true}})
		if err != nil || rows != 1 {
			return resource.ActionCommitResult{}, ErrBastionState
		}
	}
	payload, err := json.Marshal(map[string]any{"connector_id": connector.ID.String(), "bastion_scope_id": nullUUIDString(plan.BastionScopeID),
		"expected_connection_epoch": connector.ConnectionEpoch, "fencing_generation": plan.FencingGeneration})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	hash := sha256.Sum256(payload)
	commandID, err := randomID("cmd_")
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	command, err := q.CreateConnectorCommand(ctx, db.CreateConnectorCommandParams{ID: newID(), CommandID: commandID, EnterpriseID: action.EnterpriseID,
		ConnectorID: connector.ID, ConnectionEpoch: connector.ConnectionEpoch, OperationRef: action.ActionRef, CommandType: "connector_uninstall",
		PayloadSchemaVersion: "argus.connector_command/v1", Payload: payload, PayloadHash: hash[:], IdempotencyKey: action.ActionRef,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(5 * time.Minute), Valid: true}})
	if err != nil {
		return resource.ActionCommitResult{}, err
	}
	return resource.ActionCommitResult{ResourceType: "connector", ResourceID: connector.ID, ResourceVersion: connector.Version,
		Summary: "Connector uninstall command queued", ConnectorCommandID: uuid.NullUUID{UUID: command.ID, Valid: true}}, nil
}

func nullUUIDString(value uuid.NullUUID) string {
	if !value.Valid {
		return ""
	}
	return value.UUID.String()
}

func manualConnectorReleaseID(plan bastionPlan) uuid.NullUUID {
	if plan.Input.InstallMode == "command" {
		return plan.ReleaseVersionID
	}
	return uuid.NullUUID{}
}

var _ resource.ActionExtension = BastionService{}

// directInstallPlanSnapshot 是代装模式冻结校验的连接测试计划投影。
type directInstallPlanSnapshot struct {
	TargetType     string    `json:"target_type"`
	Address        string    `json:"address"`
	Port           int32     `json:"port"`
	Username       string    `json:"username"`
	ConnectionMode string    `json:"connection_mode"`
	CredentialID   uuid.UUID `json:"credential_id"`
}

type connectorReleaseManifest struct {
	SchemaVersion       string                     `json:"schema_version"`
	ManifestURI         string                     `json:"manifest_uri"`
	InstallScriptURI    string                     `json:"install_script_uri"`
	InstallScriptSHA256 string                     `json:"install_script_sha256"`
	SigningKeyID        string                     `json:"signing_key_id"`
	SigningPublicKey    string                     `json:"signing_public_key"`
	Artifacts           []connectorReleaseArtifact `json:"artifacts"`
}

type connectorReleaseArtifact struct {
	Architecture string `json:"architecture"`
	URI          string `json:"uri"`
	SHA256       string `json:"sha256"`
	Signature    string `json:"signature"`
	SigningKeyID string `json:"signing_key_id"`
	ByteSize     int64  `json:"byte_size"`
}

// freezeDirectInstallPlan freezes both the short-lived connection observation
// and an immutable Connector release digest into the Pending Action.
func (service BastionService) freezeDirectInstallPlan(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, plan *bastionPlan) error {
	if plan == nil {
		return resource.ErrActionInvalidated
	}
	if plan.Input.InstallMode == "command" {
		if plan.Architecture != "" || plan.Input.Address != "" || plan.Input.Port != 0 || plan.Input.Username != "" || plan.Input.CredentialID.Valid || plan.Input.ConnectionTestID.Valid {
			return resource.ErrInvalidConnectionMode
		}
		release, err := q.GetActiveConnectorReleaseVersion(ctx)
		if err != nil {
			return ErrConnectorArtifactUnavailable
		}
		manifest, err := connectorManualInstallRelease(release.Manifest)
		if err != nil {
			return err
		}
		if err = service.checkConnectorReleaseAvailability(ctx, manifest, true, ""); err != nil {
			return err
		}
		plan.ReleaseVersionID = uuid.NullUUID{UUID: release.ID, Valid: true}
		plan.ReleaseManifestHash = bytes.Clone(release.ManifestHash)
		return nil
	}
	_, testResult, err := validateDirectInstallInput(ctx, q, enterpriseID, plan.Input)
	if err != nil {
		return err
	}
	release, err := q.GetActiveConnectorReleaseVersion(ctx)
	if err != nil {
		return ErrConnectorArtifactUnavailable
	}
	manifest, artifact, err := connectorReleaseForArchitecture(release.Manifest, testResult.Architecture)
	if err != nil {
		return err
	}
	if err = service.checkConnectorReleaseAvailability(ctx, manifest, false, artifact.URI); err != nil {
		return err
	}
	plan.Architecture = testResult.Architecture
	plan.ReleaseVersionID = uuid.NullUUID{UUID: release.ID, Valid: true}
	plan.ReleaseManifestHash = bytes.Clone(release.ManifestHash)
	return nil
}

func (service BastionService) validateFrozenDirectInstallPlan(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, plan bastionPlan) error {
	if plan.Input.InstallMode == "command" {
		if plan.Architecture != "" || !plan.ReleaseVersionID.Valid || len(plan.ReleaseManifestHash) != sha256.Size {
			return resource.ErrActionInvalidated
		}
		release, err := q.GetConnectorReleaseVersion(ctx, plan.ReleaseVersionID.UUID)
		if err != nil || !bytes.Equal(release.ManifestHash, plan.ReleaseManifestHash) {
			return resource.ErrActionInvalidated
		}
		manifest, err := connectorManualInstallRelease(release.Manifest)
		if err != nil {
			return err
		}
		return service.checkConnectorReleaseAvailability(ctx, manifest, true, "")
	}
	_, result, err := validateDirectInstallInput(ctx, q, enterpriseID, plan.Input)
	if err != nil || plan.Architecture != result.Architecture || !plan.ReleaseVersionID.Valid || len(plan.ReleaseManifestHash) != sha256.Size {
		return resource.ErrActionInvalidated
	}
	release, err := q.GetConnectorReleaseVersion(ctx, plan.ReleaseVersionID.UUID)
	if err != nil || !bytes.Equal(release.ManifestHash, plan.ReleaseManifestHash) {
		return resource.ErrActionInvalidated
	}
	manifest, artifact, err := connectorReleaseForArchitecture(release.Manifest, result.Architecture)
	if err != nil {
		return err
	}
	return service.checkConnectorReleaseAvailability(ctx, manifest, false, artifact.URI)
}

// validateDirectInstallInput 校验模式B/C:direct 字段齐全,且引用一个与本表单
// 完全匹配、未过期、成功的 direct_ssh 主机连接测试(冻结纪律与主机创建一致)。
func validateDirectInstallInput(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, input BastionInput) (db.ConnectionTest, resource.ConnectionTestResult, error) {
	switch input.InstallMode {
	case "command":
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, nil
	case "direct_install", "direct_install_tunnel":
	default:
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, resource.ErrInvalidConnectionMode
	}
	if input.Address == "" || input.Port <= 0 || input.Username == "" || !input.CredentialID.Valid {
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, resource.ErrConnectionTestNeeded
	}
	if !input.ConnectionTestID.Valid {
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, resource.ErrConnectionTestNeeded
	}
	test, err := q.GetConnectionTest(ctx, db.GetConnectionTestParams{ID: input.ConnectionTestID.UUID, EnterpriseID: enterpriseID})
	if err != nil || test.Status != "succeeded" || !time.Now().UTC().Before(test.ExpiresAt.Time) ||
		!test.CredentialID.Valid || test.CredentialID.UUID != input.CredentialID.UUID || !test.CredentialVersion.Valid {
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, resource.ErrConnectionTestNeeded
	}
	var plan directInstallPlanSnapshot
	if json.Unmarshal(test.RequestPlan, &plan) != nil || plan.TargetType != "host" || plan.ConnectionMode != "direct_ssh" ||
		plan.Address != input.Address || plan.Port != input.Port || plan.Username != input.Username || plan.CredentialID != input.CredentialID.UUID {
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, resource.ErrConnectionTestNeeded
	}
	credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: input.CredentialID.UUID, EnterpriseID: enterpriseID})
	if err != nil || credential.Status != "active" || credential.Version != test.CredentialVersion.Int64 {
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, resource.ErrConnectionTestNeeded
	}
	var result resource.ConnectionTestResult
	if json.Unmarshal(test.Result, &result) != nil || result.HostKeyFingerprint == "" ||
		(result.Architecture != "amd64" && result.Architecture != "arm64") {
		return db.ConnectionTest{}, resource.ConnectionTestResult{}, resource.ErrConnectionTestNeeded
	}
	return test, result, nil
}

// enqueueDirectInstall 为模式B/C构造执行器安装计划并入队。凭据取自连接测试
// 冻结的版本;模式C携带隧道拨号地址(Executor 建立反向转发后覆写 Connector
// 的 enroll/长连接拨号,TLS 仍按真实域名校验)。
func (service BastionService) enqueueDirectInstall(ctx context.Context, q *db.Queries, action db.PendingAction, plan bastionPlan, scope db.BastionScope, enrollment CreatedEnrollment, retryOf uuid.NullUUID) (db.ConnectorInstallOperation, error) {
	credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: plan.Input.CredentialID.UUID, EnterpriseID: action.EnterpriseID})
	if err != nil || credential.Status != "active" {
		return db.ConnectorInstallOperation{}, resource.ErrConnectionTestNeeded
	}
	test, err := q.GetConnectionTest(ctx, db.GetConnectionTestParams{ID: plan.Input.ConnectionTestID.UUID, EnterpriseID: action.EnterpriseID})
	if err != nil || test.Status != "succeeded" || !time.Now().UTC().Before(test.ExpiresAt.Time) ||
		!test.CredentialVersion.Valid || test.CredentialVersion.Int64 != credential.Version {
		return db.ConnectorInstallOperation{}, resource.ErrConnectionTestNeeded
	}
	var testResult resource.ConnectionTestResult
	if json.Unmarshal(test.Result, &testResult) != nil || testResult.HostKeyFingerprint == "" ||
		(testResult.Architecture != "amd64" && testResult.Architecture != "arm64") {
		return db.ConnectorInstallOperation{}, resource.ErrConnectionTestNeeded
	}
	if !plan.ReleaseVersionID.Valid || len(plan.ReleaseManifestHash) != sha256.Size {
		return db.ConnectorInstallOperation{}, ErrConnectorArtifactUnavailable
	}
	release, err := q.GetConnectorReleaseVersion(ctx, plan.ReleaseVersionID.UUID)
	if err != nil || !bytes.Equal(release.ManifestHash, plan.ReleaseManifestHash) {
		return db.ConnectorInstallOperation{}, ErrConnectorArtifactUnavailable
	}
	manifest, artifact, err := connectorReleaseForArchitecture(release.Manifest, testResult.Architecture)
	if err != nil {
		return db.ConnectorInstallOperation{}, err
	}
	bundle, err := service.Enrollment.installationTrustBundle(ctx)
	if err != nil {
		return db.ConnectorInstallOperation{}, fmt.Errorf("load Connector installation Trust Bundle: %w", err)
	}
	if bundle.State == trustbundle.StateFailed || bundle.Epoch < 1 {
		return db.ConnectorInstallOperation{}, errors.New("Connector installation Trust Bundle is unavailable")
	}
	operationKind := "install"
	if plan.Operation == "replace" {
		operationKind = "replace"
	}
	command := &connectorv1.ConnectorInstallCommand{
		ConnectorId: enrollment.ConnectorID.String(), BastionScopeId: scope.ID.String(), HostId: plan.HostID.String(),
		Operation: operationKind, InstallMode: plan.Input.InstallMode, EnrollmentEndpoint: service.Enrollment.EnrollmentURL,
		TargetAddress: plan.Input.Address, TargetPort: uint32(plan.Input.Port), TargetUsername: plan.Input.Username,
		PinnedHostKey: testResult.HostKeyFingerprint, CredentialId: credential.ID.String(), CredentialVersion: uint64(credential.Version),
		Platform: "linux_" + testResult.Architecture, ReleaseVersionId: release.ID.String(),
		Artifact: &connectorv1.CollectorArtifact{Platform: "linux_" + testResult.Architecture, Uri: artifact.URI,
			Sha256: artifact.SHA256, Signature: artifact.Signature, SigningKeyId: artifact.SigningKeyID, ByteSize: uint64(artifact.ByteSize)},
		ArtifactSigningPublicKey: manifest.SigningPublicKey,
		TrustBundlePem:           bundle.Material.PEM, TrustBundleEpoch: uint64(bundle.Epoch),
		TrustBundleSha256: bundle.Material.SHA256, TrustBundleCaFingerprints: append([]string(nil), bundle.Material.Fingerprints...),
	}
	if plan.Input.InstallMode == "direct_install_tunnel" {
		command.EnrollDialAddress = "127.0.0.1:8443"
		command.GatewayDialAddress = "127.0.0.1:9443"
	}
	encoded, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(command)
	if err != nil {
		return db.ConnectorInstallOperation{}, err
	}
	canonical, err := resource.CanonicalJSON(encoded)
	if err != nil {
		return db.ConnectorInstallOperation{}, err
	}
	hash := sha256.Sum256(canonical)
	operationID := newID()
	expiresAt := minActionTime(action.ExpiresAt.Time, time.Now().UTC().Add(10*time.Minute))
	nonce, ciphertext, err := operationsecret.Encrypt(service.OperationSecretKey, action.EnterpriseID, operationID,
		operationsecret.Material{EnrollmentToken: enrollment.Token})
	if err != nil {
		return db.ConnectorInstallOperation{}, err
	}
	operation, err := q.CreateConnectorInstallOperation(ctx, db.CreateConnectorInstallOperationParams{
		ID: operationID, EnterpriseID: action.EnterpriseID, ConnectorID: enrollment.ConnectorID, BastionScopeID: scope.ID,
		HostID: plan.HostID, PendingActionID: action.ID, RetryOf: retryOf, ReleaseVersionID: release.ID, ConnectionTestID: test.ID,
		InstallMode: plan.Input.InstallMode, Plan: encoded, PlanHash: hash[:],
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}})
	if err != nil {
		return db.ConnectorInstallOperation{}, err
	}
	if _, err = q.CreateConnectorInstallOperationSecret(ctx, db.CreateConnectorInstallOperationSecretParams{
		OperationID: operation.ID, EnterpriseID: action.EnterpriseID, KeyVersion: operationsecret.KeyVersion,
		Nonce: nonce, Ciphertext: ciphertext, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	}); err != nil {
		return db.ConnectorInstallOperation{}, err
	}
	if _, err = q.CreateConnectorInstallOperationEvent(ctx, db.CreateConnectorInstallOperationEventParams{
		ID: newID(), OperationID: operation.ID, EnterpriseID: action.EnterpriseID, Sequence: 1,
		Stage: "queued", Status: "started",
	}); err != nil {
		return db.ConnectorInstallOperation{}, err
	}
	if plan.Input.InstallMode == "direct_install_tunnel" {
		if service.ConnectorEnrollForwardTarget == "" || service.ConnectorGatewayForwardTarget == "" {
			return db.ConnectorInstallOperation{}, ErrControlTunnelUnavailable
		}
		if _, err = q.CreateConnectorControlTunnel(ctx, db.CreateConnectorControlTunnelParams{
			ID: newID(), EnterpriseID: action.EnterpriseID, ConnectorID: enrollment.ConnectorID, BastionScopeID: scope.ID,
			HostID: plan.HostID, CredentialID: credential.ID, CredentialVersion: credential.Version,
			TargetAddress: plan.Input.Address, TargetPort: plan.Input.Port, TargetUsername: plan.Input.Username,
			PinnedHostKey: testResult.HostKeyFingerprint, EnrollForwardTarget: service.ConnectorEnrollForwardTarget,
			GatewayForwardTarget: service.ConnectorGatewayForwardTarget,
		}); err != nil {
			return db.ConnectorInstallOperation{}, err
		}
	}
	return operation, nil
}

func connectorArtifactForArchitecture(raw json.RawMessage, architecture string) (connectorReleaseArtifact, error) {
	_, artifact, err := connectorReleaseForArchitecture(raw, architecture)
	return artifact, err
}

func connectorManualInstallRelease(raw json.RawMessage) (connectorReleaseManifest, error) {
	manifest, _, err := connectorReleaseForArchitecture(raw, "amd64")
	if err != nil {
		return connectorReleaseManifest{}, err
	}
	if manifest.ManifestURI == "" || manifest.InstallScriptURI == "" || !validSHA256(manifest.InstallScriptSHA256) {
		return connectorReleaseManifest{}, ErrConnectorArtifactInvalid
	}
	if _, err = connectorArtifactForArchitecture(raw, "arm64"); err != nil {
		return connectorReleaseManifest{}, err
	}
	return manifest, nil
}

func (service BastionService) checkConnectorReleaseAvailability(
	ctx context.Context,
	manifest connectorReleaseManifest,
	manual bool,
	artifactURI string,
) error {
	if service.Enrollment.Artifacts == nil {
		return nil
	}
	urls := []string{artifactURI}
	if manual {
		amd64, amd64Err := connectorArtifactFromManifest(manifest, "amd64")
		arm64, arm64Err := connectorArtifactFromManifest(manifest, "arm64")
		if amd64Err != nil || arm64Err != nil {
			return ErrConnectorArtifactInvalid
		}
		urls = []string{manifest.ManifestURI, manifest.InstallScriptURI, amd64.URI, arm64.URI}
	}
	if err := service.Enrollment.Artifacts.Check(ctx, urls...); err != nil {
		return fmt.Errorf("%w: %v", ErrConnectorArtifactUnavailable, err)
	}
	return nil
}

func connectorReleaseForArchitecture(raw json.RawMessage, architecture string) (connectorReleaseManifest, connectorReleaseArtifact, error) {
	var manifest connectorReleaseManifest
	if json.Unmarshal(raw, &manifest) != nil || manifest.SchemaVersion != "argus.connector_release/v2" ||
		manifest.SigningKeyID == "" || !validConnectorSigningPublicKey(manifest.SigningPublicKey) || !validSHA256(manifest.InstallScriptSHA256) {
		return connectorReleaseManifest{}, connectorReleaseArtifact{}, ErrConnectorArtifactInvalid
	}
	artifact, err := connectorArtifactFromManifest(manifest, architecture)
	return manifest, artifact, err
}

func connectorArtifactFromManifest(manifest connectorReleaseManifest, architecture string) (connectorReleaseArtifact, error) {
	for _, artifact := range manifest.Artifacts {
		if artifact.Architecture == architecture && artifact.URI != "" && len(artifact.SHA256) == 64 &&
			artifact.Signature != "" && artifact.SigningKeyID == manifest.SigningKeyID && artifact.ByteSize > 0 {
			return artifact, nil
		}
	}
	return connectorReleaseArtifact{}, ErrConnectorArtifactUnavailable
}

func validConnectorSigningPublicKey(value string) bool {
	decoded, err := base64.RawStdEncoding.DecodeString(value)
	return err == nil && len(decoded) == ed25519.PublicKeySize
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func connectorCommandMatchesRelease(command *connectorv1.ConnectorInstallCommand, release db.ConnectorReleaseVersion) bool {
	if command == nil {
		return false
	}
	if command.GetReleaseVersionId() != release.ID.String() || command.GetArtifact() == nil {
		return false
	}
	architecture := ""
	switch command.GetPlatform() {
	case "linux_amd64":
		architecture = "amd64"
	case "linux_arm64":
		architecture = "arm64"
	default:
		return false
	}
	manifest, artifact, err := connectorReleaseForArchitecture(release.Manifest, architecture)
	if err != nil {
		return false
	}
	frozen := command.GetArtifact()
	return frozen.GetPlatform() == command.GetPlatform() && frozen.GetUri() == artifact.URI && frozen.GetSha256() == artifact.SHA256 &&
		frozen.GetSignature() == artifact.Signature && frozen.GetSigningKeyId() == artifact.SigningKeyID &&
		frozen.GetByteSize() == uint64(artifact.ByteSize) && command.GetArtifactSigningPublicKey() == manifest.SigningPublicKey
}

func minActionTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
