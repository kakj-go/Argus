package connector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var ErrBastionState = errors.New("bastion scope state conflict")

type BastionService struct {
	Store      *postgres.Store
	Actions    resource.PendingActionService
	Enrollment Service
}

type BastionInput struct {
	Name, Environment string
	Labels            map[string]string
	ExpectedVersion   int64
}

type bastionPlan struct {
	Operation string               `json:"operation"`
	ScopeID   uuid.UUID            `json:"scope_id"`
	HostID    uuid.UUID            `json:"host_id"`
	Input     BastionInput         `json:"input"`
	Impact    resource.LabelImpact `json:"impact"`
}

type bastionSnapshot struct {
	ResourceVersion   int64  `json:"resource_version"`
	FencingGeneration int64  `json:"fencing_generation"`
	MemberCount       int64  `json:"member_count"`
	Status            string `json:"status"`
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

func (service BastionService) PreviewCreate(ctx context.Context, subject resource.Subject, enterpriseID uuid.UUID, input BastionInput, idempotencyKey string) (db.PendingAction, error) {
	labelsJSON, _, err := resource.NormalizeUserLabels(input.Labels)
	if err != nil {
		return db.PendingAction{}, err
	}
	labels, _ := resource.DecodeLabels(labelsJSON)
	scopeID, hostID := newID(), newID()
	allowed, matched, err := (resource.AccessService{Store: service.Store}).CanAccess(ctx, enterpriseID, subject.DataScopeIDs, "host", hostID.String(), labels)
	if err != nil || !allowed {
		return db.PendingAction{}, resource.ErrResourceDenied
	}
	impact, _, err := resource.ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "host", hostID.String(), map[string]string{}, labels)
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := bastionPlan{Operation: "create", ScopeID: scopeID, HostID: hostID, Input: input, Impact: impact}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{ActionType: "bastion_scope.create", Title: "Create bastion",
		Summary: "Create a stable Bastion Scope and one-time enrollment", Risk: "dangerous", ResourceType: "bastion_scope",
		ResourceID: uuid.NullUUID{UUID: scopeID, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"scope_id": scopeID, "host_id": hostID, "name": input.Name, "matched_data_scope_ids": matched},
		Diff:    []map[string]string{{"kind": "add", "text": "Create bastion " + input.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: impact, CommitHandler: "argus.bastion_scope.create.commit"}, idempotencyKey)
}

func (service BastionService) PreviewUpdate(ctx context.Context, subject resource.Subject, enterpriseID, scopeID uuid.UUID, input BastionInput, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.Get(ctx, enterpriseID, scopeID)
	if err != nil || current.ResourceVersion != input.ExpectedVersion || !current.ConnectorHostID.Valid {
		return db.PendingAction{}, resource.ErrVersionConflict
	}
	before, _ := resource.DecodeLabels(current.Labels)
	after := before
	if input.Labels != nil {
		encoded, _, normalizeErr := resource.NormalizeUserLabels(resource.MergeSystemLabels(input.Labels, before))
		if normalizeErr != nil {
			return db.PendingAction{}, normalizeErr
		}
		after, _ = resource.DecodeLabels(encoded)
	}
	impact, _, err := resource.ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "host", current.ConnectorHostID.UUID.String(), before, after)
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := bastionPlan{Operation: "update", ScopeID: scopeID, HostID: current.ConnectorHostID.UUID, Input: input, Impact: impact}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{ActionType: "bastion_scope.update", Title: "Update bastion",
		Summary: "Update Bastion Scope metadata", Risk: "write", ResourceType: "bastion_scope", ResourceID: uuid.NullUUID{UUID: scopeID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: input.ExpectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"scope_id": scopeID, "affected_subject_count": len(impact.AffectedSubjects)},
		Diff:    []map[string]string{{"kind": "change", "text": "Update bastion " + current.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: impact, CommitHandler: "argus.bastion_scope.update.commit"}, idempotencyKey)
}

func (service BastionService) PreviewLifecycle(ctx context.Context, subject resource.Subject, enterpriseID, scopeID uuid.UUID, expectedVersion int64, operation, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.Get(ctx, enterpriseID, scopeID)
	if err != nil || current.ResourceVersion != expectedVersion {
		return db.PendingAction{}, resource.ErrVersionConflict
	}
	deletable := current.Status == "uninstalled" || (current.Status == "offline" && !current.ActiveConnectorID.Valid)
	if operation == "delete" && (current.MemberCount != 0 || !deletable) {
		return db.PendingAction{}, ErrBastionState
	}
	if operation == "replace" && current.Status != "offline" && current.Status != "uninstalled" && current.Status != "suspected_offline" {
		return db.PendingAction{}, ErrBastionState
	}
	snapshot := bastionSnapshot{ResourceVersion: current.ResourceVersion, FencingGeneration: current.FencingGeneration, MemberCount: current.MemberCount, Status: current.Status}
	plan := bastionPlan{Operation: operation, ScopeID: scopeID, HostID: current.ConnectorHostID.UUID, Input: BastionInput{ExpectedVersion: expectedVersion}}
	risk, verb := "dangerous", "Delete"
	if operation == "replace" {
		verb = "Replace connector for"
	}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, resource.PrepareActionInput{ActionType: "bastion_scope." + operation, Title: verb + " bastion",
		Summary: verb + " " + current.Name, Risk: risk, ResourceType: "bastion_scope", ResourceID: uuid.NullUUID{UUID: scopeID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: expectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"scope_id": scopeID, "member_count": current.MemberCount, "fencing_generation": current.FencingGeneration},
		Diff:    []map[string]string{{"kind": "remove", "text": verb + " " + current.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: snapshot, CommitHandler: "argus.bastion_scope." + operation + ".commit"}, idempotencyKey)
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
		Preview: map[string]any{"connector_id": connector.ID, "role": connector.Role, "connection_epoch": connector.ConnectionEpoch},
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
		_, hash, err := resource.ComputeLabelImpact(ctx, q, action.EnterpriseID, "host", plan.HostID.String(), map[string]string{}, plan.Input.Labels)
		return hash, err
	}
	current, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID})
	if err != nil || current.ResourceVersion != plan.Input.ExpectedVersion {
		return nil, resource.ErrActionInvalidated
	}
	if plan.Operation == "update" {
		before, _ := resource.DecodeLabels(current.Labels)
		after := before
		if plan.Input.Labels != nil {
			after = resource.MergeSystemLabels(plan.Input.Labels, before)
		}
		_, hash, err := resource.ComputeLabelImpact(ctx, q, action.EnterpriseID, "host", plan.HostID.String(), before, after)
		return hash, err
	}
	snapshot := bastionSnapshot{ResourceVersion: current.ResourceVersion, FencingGeneration: current.FencingGeneration, MemberCount: current.MemberCount, Status: current.Status}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func (service BastionService) CommitAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (resource.ActionCommitResult, error) {
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
		scope, err := q.CreateBastionScope(ctx, db.CreateBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID, Name: plan.Input.Name,
			Environment: plan.Input.Environment, Labels: labels, LabelsHash: hash})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		if _, err := q.CreateHost(ctx, db.CreateHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID, Name: plan.Input.Name,
			Hostname: "", Address: "connector://" + plan.HostID.String(), Port: 1, Platform: "linux", ConnectionMode: "connector_local",
			BastionScopeID: uuid.NullUUID{UUID: scope.ID, Valid: true}, Environment: plan.Input.Environment, Labels: labels, LabelsHash: hash,
			ConnectionStatus: "onboarding", PinnedHostKey: ""}); err != nil {
			return resource.ActionCommitResult{}, err
		}
		enrollment, err := service.Enrollment.CreateEnrollment(ctx, q, action.CreatorSubjectID.String(), action.EnterpriseID, CreateEnrollmentInput{Role: "bastion",
			Purpose: "initial_registration", BastionScopeID: uuid.NullUUID{UUID: scope.ID, Valid: true}, HostID: uuid.NullUUID{UUID: plan.HostID, Valid: true},
			Policy: json.RawMessage(`{"capabilities":["host.connection_probe","kubernetes.connection_probe","kubernetes.query","credential.lease","connector.uninstall"]}`)})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		if err := resource.ApplyLabelImpact(ctx, q, action.EnterpriseID, plan.Impact); err != nil {
			return resource.ActionCommitResult{}, err
		}
		return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion,
			Summary: "Bastion Scope created", Enrollment: &resource.EnrollmentResult{EnrollmentID: enrollment.Record.ID,
				InstallCommand: enrollment.InstallCommand, ExpiresAt: enrollment.Record.ExpiresAt.Time}}, nil
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
		if err := resource.ApplyLabelImpact(ctx, q, action.EnterpriseID, plan.Impact); err != nil {
			return resource.ActionCommitResult{}, err
		}
		return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion, Summary: "Bastion Scope updated"}, nil
	case "replace":
		current, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID})
		if err != nil {
			return resource.ActionCommitResult{}, ErrBastionState
		}
		scope, err := q.FenceBastionScope(ctx, db.FenceBastionScopeParams{ID: plan.ScopeID, EnterpriseID: action.EnterpriseID, ResourceVersion: plan.Input.ExpectedVersion})
		if err != nil {
			return resource.ActionCommitResult{}, ErrBastionState
		}
		if current.ActiveConnectorID.Valid {
			_ = q.RevokeConnectorCertificates(ctx, db.RevokeConnectorCertificatesParams{ConnectorID: current.ActiveConnectorID.UUID, EnterpriseID: action.EnterpriseID})
		}
		_ = q.RevokeActiveEnrollmentTokens(ctx, db.RevokeActiveEnrollmentTokensParams{EnterpriseID: action.EnterpriseID, BastionScopeID: uuid.NullUUID{UUID: plan.ScopeID, Valid: true}})
		enrollment, err := service.Enrollment.CreateEnrollment(ctx, q, action.CreatorSubjectID.String(), action.EnterpriseID, CreateEnrollmentInput{Role: "bastion",
			Purpose: "connector_replacement", BastionScopeID: uuid.NullUUID{UUID: plan.ScopeID, Valid: true}, HostID: uuid.NullUUID{UUID: plan.HostID, Valid: true}, Policy: json.RawMessage(`{}`)})
		if err != nil {
			return resource.ActionCommitResult{}, err
		}
		return resource.ActionCommitResult{ResourceType: "bastion_scope", ResourceID: scope.ID, ResourceVersion: scope.ResourceVersion,
			Summary: "Connector fenced and replacement enrollment created", Enrollment: &resource.EnrollmentResult{EnrollmentID: enrollment.Record.ID,
				InstallCommand: enrollment.InstallCommand, ExpiresAt: enrollment.Record.ExpiresAt.Time}}, nil
	case "delete":
		if plan.HostID != uuid.Nil {
			if _, err := q.DeleteBastionRootHost(ctx, db.DeleteBastionRootHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID,
				BastionScopeID: uuid.NullUUID{UUID: plan.ScopeID, Valid: true}}); err != nil {
				return resource.ActionCommitResult{}, ErrBastionState
			}
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

var _ resource.ActionExtension = BastionService{}
