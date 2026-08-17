package resource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrResourceDenied        = errors.New("resource is outside data scope")
	ErrConnectionTestNeeded  = errors.New("successful connection test required")
	ErrInvalidConnectionMode = errors.New("invalid connection mode")
	ErrVersionConflict       = errors.New("resource version conflict")
	ErrKubernetesUnavailable = errors.New("kubernetes reader unavailable")
)

type CommandEnqueuer interface {
	EnqueueConnectionTest(context.Context, *db.Queries, db.ConnectionTest) error
	NotifyConnectorCommand(context.Context, uuid.UUID, int64)
}

type DirectDispatcher interface {
	DispatchConnectionTest(context.Context, db.ConnectionTest) error
}

type KubernetesReader interface {
	List(context.Context, db.KubernetesCluster, KubernetesQuery) ([]KubernetesObject, error)
	PodLogs(context.Context, db.KubernetesCluster, PodLogsQuery) ([]byte, bool, error)
}

type ActionExtension interface {
	RevalidateAction(context.Context, *db.Queries, db.PendingAction, json.RawMessage) ([]byte, error)
	CommitAction(context.Context, *db.Queries, db.PendingAction, json.RawMessage) (ActionCommitResult, error)
}

type KubernetesEnrollmentCreator interface {
	CreateKubernetesEnrollment(context.Context, *db.Queries, string, uuid.UUID, uuid.UUID) (EnrollmentResult, error)
}

type KubernetesQuery struct {
	ResourceType, Namespace, Query string
	Limit                          int
}

type KubernetesObject struct {
	ResourceType, Namespace, Name string
	Labels                        map[string]string
	Summary                       map[string]any
}

type PodLogsQuery struct {
	Namespace, Pod, Container string
	TailLines                 int64
}

type Service struct {
	Store             *postgres.Store
	Actions           PendingActionService
	Access            AccessService
	Direct            DirectTargetValidator
	Commands          CommandEnqueuer
	DirectCommands    DirectDispatcher
	Kubernetes        KubernetesReader
	Extension         ActionExtension
	ClusterEnrollment KubernetesEnrollmentCreator
	TestTTL           time.Duration
}

type Subject struct {
	ActorID              string
	ActorType            string
	AuthorizationVersion int64
	DataScopeIDs         []uuid.UUID
	RunID                uuid.NullUUID
}

func (service Service) prepareAction(ctx context.Context, subject Subject, enterpriseID uuid.UUID, input PrepareActionInput, idempotencyKey string) (db.PendingAction, error) {
	input.RunID = subject.RunID
	if subject.ActorType == "service_account" {
		return service.Actions.PrepareForSubject(ctx, ActionSubject{ID: subject.ActorID, Type: "service_account", AuthorizationVersion: subject.AuthorizationVersion}, enterpriseID, input, idempotencyKey)
	}
	return service.Actions.Prepare(ctx, subject.ActorID, enterpriseID, input, idempotencyKey)
}

type HostInput struct {
	Name, Hostname, Address, Platform, ConnectionMode, Environment, Username string
	Port                                                                     int32
	BastionScopeID, CredentialID, ConnectionTestID                           uuid.NullUUID
	Labels                                                                   map[string]string
	ExpectedVersion                                                          int64
}

type KubernetesInput struct {
	Name, APIServer, ConnectionMode, DefaultNamespace, Environment string
	BastionScopeID, CredentialID, ConnectionTestID                 uuid.NullUUID
	Labels                                                         map[string]string
	ExpectedVersion                                                int64
}

type connectionPlan struct {
	TargetType        string        `json:"target_type"`
	Address           string        `json:"address"`
	Port              int32         `json:"port,omitempty"`
	Platform          string        `json:"platform,omitempty"`
	Username          string        `json:"username,omitempty"`
	ConnectionMode    string        `json:"connection_mode"`
	BastionScopeID    uuid.NullUUID `json:"bastion_scope_id,omitempty"`
	ConnectorID       uuid.NullUUID `json:"connector_id,omitempty"`
	CredentialID      uuid.NullUUID `json:"credential_id,omitempty"`
	CredentialVersion int64         `json:"credential_version,omitempty"`
	ResolvedIPs       []string      `json:"resolved_ips,omitempty"`
}

type ConnectionTestResult struct {
	Checks             []map[string]string `json:"checks"`
	LatencyMS          int64               `json:"latency_ms,omitempty"`
	ResolvedIPs        []string            `json:"resolved_ips,omitempty"`
	HostKeyFingerprint string              `json:"host_key_fingerprint,omitempty"`
	RemoteVersion      string              `json:"remote_version,omitempty"`
}

type hostActionPlan struct {
	Operation string      `json:"operation"`
	HostID    uuid.UUID   `json:"host_id"`
	Input     HostInput   `json:"input"`
	Impact    LabelImpact `json:"impact"`
	PinnedKey string      `json:"pinned_host_key,omitempty"`
}

type kubernetesActionPlan struct {
	Operation string          `json:"operation"`
	ClusterID uuid.UUID       `json:"cluster_id"`
	Input     KubernetesInput `json:"input"`
	Impact    LabelImpact     `json:"impact"`
	Version   string          `json:"kubernetes_version,omitempty"`
}

func (service Service) ListHosts(ctx context.Context, enterpriseID uuid.UUID, scopeIDs []uuid.UUID) ([]db.Host, error) {
	items, err := service.Store.Queries.ListHosts(ctx, enterpriseID)
	if err != nil {
		return nil, err
	}
	return service.Access.FilterHosts(ctx, enterpriseID, scopeIDs, items)
}

func (service Service) GetHost(ctx context.Context, enterpriseID, hostID uuid.UUID, scopeIDs []uuid.UUID) (db.Host, error) {
	host, err := service.Store.Queries.GetHost(ctx, db.GetHostParams{ID: hostID, EnterpriseID: enterpriseID})
	if err != nil {
		return db.Host{}, err
	}
	labels, err := DecodeLabels(host.Labels)
	if err != nil {
		return db.Host{}, err
	}
	allowed, _, err := service.Access.CanAccess(ctx, enterpriseID, scopeIDs, "host", host.ID.String(), labels)
	if err != nil {
		return db.Host{}, err
	}
	if !allowed {
		return db.Host{}, ErrResourceDenied
	}
	return host, nil
}

func (service Service) CreateHostConnectionTest(ctx context.Context, subject Subject, enterpriseID uuid.UUID, input HostInput, idempotencyKey string) (db.ConnectionTest, error) {
	test, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Actions.Idempotency, "enterprise", subject.ActorID, "host.connection_test.create", idempotencyKey, input, 202, func(q *db.Queries) (db.ConnectionTest, error) {
		plan, params, err := service.hostConnectionPlan(ctx, q, enterpriseID, subject.ActorID, input)
		if err != nil {
			return db.ConnectionTest{}, err
		}
		encoded, _ := json.Marshal(plan)
		hash := sha256.Sum256(encoded)
		params.RequestPlan, params.RequestHash = encoded, hash[:]
		test, err := q.CreateConnectionTest(ctx, params)
		if err != nil {
			return db.ConnectionTest{}, err
		}
		if test.Path == "connector" && service.Commands != nil {
			if err := service.Commands.EnqueueConnectionTest(ctx, q, test); err != nil {
				return db.ConnectionTest{}, err
			}
		}
		return test, appendResourceAudit(ctx, q, subject.ActorID, enterpriseID, "host.connection_test.create", "connection_test", test.ID, map[string]any{"status": "queued"})
	})
	if err != nil {
		return db.ConnectionTest{}, err
	}
	if test.Path == "direct" && service.DirectCommands != nil {
		// The PostgreSQL queue is authoritative. RPC dispatch only reduces latency;
		// the Direct Executor scan recovers dropped dispatches and Pod restarts.
		_ = service.DirectCommands.DispatchConnectionTest(ctx, test)
	} else if test.Path == "connector" && service.Commands != nil && test.ConnectorID.Valid && test.ConnectionEpoch.Valid {
		service.Commands.NotifyConnectorCommand(ctx, test.ConnectorID.UUID, test.ConnectionEpoch.Int64)
	}
	return test, nil
}

func (service Service) hostConnectionPlan(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, actorID string, input HostInput) (connectionPlan, db.CreateConnectionTestParams, error) {
	if input.ConnectionMode != "via_bastion" && input.ConnectionMode != "direct_ssh" && input.ConnectionMode != "direct_winrm" {
		return connectionPlan{}, db.CreateConnectionTestParams{}, ErrInvalidConnectionMode
	}
	plan := connectionPlan{TargetType: "host", Address: input.Address, Port: input.Port, Platform: input.Platform, Username: input.Username,
		ConnectionMode: input.ConnectionMode, BastionScopeID: input.BastionScopeID}
	params := db.CreateConnectionTestParams{ID: newResourceID(), EnterpriseID: enterpriseID, TargetType: "host", Path: "direct", CreatedBy: uuid.MustParse(actorID),
		ExpiresAt: pgtype.Timestamptz{Time: service.testExpiry(), Valid: true}}
	if !input.CredentialID.Valid {
		return connectionPlan{}, db.CreateConnectionTestParams{}, ErrConnectionTestNeeded
	}
	if input.CredentialID.Valid {
		credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: input.CredentialID.UUID, EnterpriseID: enterpriseID})
		if err != nil || credential.Status != "active" {
			return connectionPlan{}, db.CreateConnectionTestParams{}, ErrConnectionTestNeeded
		}
		expectedProtocol := "ssh"
		if input.ConnectionMode == "direct_winrm" || (input.ConnectionMode == "via_bastion" && input.Platform == "windows") {
			expectedProtocol = "winrm"
		}
		if credential.Protocol != expectedProtocol || input.Username == "" {
			return connectionPlan{}, db.CreateConnectionTestParams{}, ErrConnectionTestNeeded
		}
		plan.CredentialID = uuid.NullUUID{UUID: credential.ID, Valid: true}
		plan.CredentialVersion = credential.Version
		params.CredentialID, params.CredentialVersion = plan.CredentialID, pgtype.Int8{Int64: credential.Version, Valid: true}
	}
	if input.ConnectionMode == "via_bastion" {
		if !input.BastionScopeID.Valid {
			return connectionPlan{}, db.CreateConnectionTestParams{}, ErrInvalidConnectionMode
		}
		scope, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: input.BastionScopeID.UUID, EnterpriseID: enterpriseID})
		if err != nil || scope.Status != "active" || !scope.ActiveConnectorID.Valid {
			return connectionPlan{}, db.CreateConnectionTestParams{}, ErrConnectionTestNeeded
		}
		connector, err := q.GetConnector(ctx, db.GetConnectorParams{ID: scope.ActiveConnectorID.UUID, EnterpriseID: enterpriseID})
		if err != nil || connector.Status != "online" || connector.ConnectionEpoch < 1 {
			return connectionPlan{}, db.CreateConnectionTestParams{}, ErrConnectionTestNeeded
		}
		plan.ConnectorID = scope.ActiveConnectorID
		params.Path, params.ConnectorID, params.ConnectionEpoch = "connector", scope.ActiveConnectorID, pgtype.Int8{Int64: connector.ConnectionEpoch, Valid: true}
	} else {
		addresses, err := service.Direct.Resolve(ctx, input.Address)
		if err != nil {
			return connectionPlan{}, db.CreateConnectionTestParams{}, err
		}
		for _, address := range addresses {
			plan.ResolvedIPs = append(plan.ResolvedIPs, address.String())
		}
	}
	return plan, params, nil
}

func (service Service) GetConnectionTest(ctx context.Context, enterpriseID, testID uuid.UUID) (db.ConnectionTest, error) {
	test, err := service.Store.Queries.GetConnectionTest(ctx, db.GetConnectionTestParams{ID: testID, EnterpriseID: enterpriseID})
	if err != nil {
		return db.ConnectionTest{}, err
	}
	if time.Now().UTC().After(test.ExpiresAt.Time) && test.Status != "expired" {
		test.Status = "expired"
	}
	return test, nil
}

func (service Service) CompleteConnectionTest(ctx context.Context, enterpriseID, testID uuid.UUID, requestHash []byte, status string, result ConnectionTestResult, errorCode string) (db.ConnectionTest, error) {
	test, err := service.Store.Queries.GetConnectionTest(ctx, db.GetConnectionTestParams{ID: testID, EnterpriseID: enterpriseID})
	if err != nil || !subtleEqual(test.RequestHash, requestHash) || time.Now().UTC().After(test.ExpiresAt.Time) {
		return db.ConnectionTest{}, ErrConnectionTestNeeded
	}
	if status != "succeeded" && status != "failed" && status != "result_unknown" {
		return db.ConnectionTest{}, ErrConnectionTestNeeded
	}
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 64*1024 {
		return db.ConnectionTest{}, ErrConnectionTestNeeded
	}
	return service.Store.Queries.CompleteConnectionTest(ctx, db.CompleteConnectionTestParams{ID: testID, EnterpriseID: enterpriseID, Status: status,
		Result: encoded, ErrorCode: pgtype.Text{String: errorCode, Valid: errorCode != ""}})
}

func (service Service) PreviewCreateHost(ctx context.Context, subject Subject, enterpriseID uuid.UUID, input HostInput, idempotencyKey string) (db.PendingAction, error) {
	test, result, err := service.requireHostConnectionTest(ctx, service.Store.Queries, enterpriseID, input)
	if err != nil {
		return db.PendingAction{}, err
	}
	labelsJSON, _, err := NormalizeUserLabels(input.Labels)
	if err != nil {
		return db.PendingAction{}, err
	}
	labels, _ := DecodeLabels(labelsJSON)
	hostID := newResourceID()
	allowed, matched, err := service.Access.CanAccess(ctx, enterpriseID, subject.DataScopeIDs, "host", hostID.String(), labels)
	if err != nil || !allowed {
		return db.PendingAction{}, ErrResourceDenied
	}
	impact, _, err := ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "host", hostID.String(), map[string]string{}, labels)
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := hostActionPlan{Operation: "create", HostID: hostID, Input: input, Impact: impact, PinnedKey: result.HostKeyFingerprint}
	return service.prepareAction(ctx, subject, enterpriseID, PrepareActionInput{ActionType: "host.create", Title: "Create host", Summary: "Create a validated host resource",
		Risk: "write", ResourceType: "host", ResourceID: uuid.NullUUID{UUID: hostID, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"host_id": hostID, "name": input.Name, "connection_test_id": test.ID, "matched_data_scope_ids": matched, "affected_subject_count": len(impact.AffectedSubjects)},
		Diff:    []map[string]string{{"kind": "add", "text": "Create host " + input.Name}}, ImmutablePlan: plan, ResourceScopeSnapshot: impact, CommitHandler: "argus.host.create.commit"}, idempotencyKey)
}

func (service Service) PreviewUpdateHost(ctx context.Context, subject Subject, enterpriseID, hostID uuid.UUID, input HostInput, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.GetHost(ctx, enterpriseID, hostID, subject.DataScopeIDs)
	if err != nil || current.ResourceVersion != input.ExpectedVersion {
		return db.PendingAction{}, ErrVersionConflict
	}
	if input.ConnectionMode != "" && !editableHostConnectionMode(input.ConnectionMode) {
		return db.PendingAction{}, ErrInvalidConnectionMode
	}
	before, _ := DecodeLabels(current.Labels)
	after := before
	if input.Labels != nil {
		encoded, _, err := NormalizeUserLabels(MergeSystemLabels(input.Labels, before))
		if err != nil {
			return db.PendingAction{}, err
		}
		after, _ = DecodeLabels(encoded)
	}
	result := ConnectionTestResult{HostKeyFingerprint: current.PinnedHostKey}
	if hostNetworkPathChanged(current, input) || input.ConnectionTestID.Valid {
		_, result, err = service.requireHostUpdateConnectionTest(ctx, service.Store.Queries, enterpriseID, current, input)
		if err != nil {
			return db.PendingAction{}, err
		}
	}
	impact, _, err := ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "host", hostID.String(), before, after)
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := hostActionPlan{Operation: "update", HostID: hostID, Input: input, Impact: impact, PinnedKey: result.HostKeyFingerprint}
	return service.prepareAction(ctx, subject, enterpriseID, PrepareActionInput{ActionType: "host.update", Title: "Update host", Summary: "Apply validated host changes",
		Risk: "write", ResourceType: "host", ResourceID: uuid.NullUUID{UUID: hostID, Valid: true}, ExpectedResourceVersion: pgtype.Int8{Int64: input.ExpectedVersion, Valid: true},
		AuthorizationVersion: subject.AuthorizationVersion, Preview: map[string]any{"host_id": hostID, "affected_subject_count": len(impact.AffectedSubjects)},
		Diff: []map[string]string{{"kind": "change", "text": "Update host " + current.Name}}, ImmutablePlan: plan, ResourceScopeSnapshot: impact, CommitHandler: "argus.host.update.commit"}, idempotencyKey)
}

func (service Service) PreviewDeleteHost(ctx context.Context, subject Subject, enterpriseID, hostID uuid.UUID, expectedVersion int64, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.GetHost(ctx, enterpriseID, hostID, subject.DataScopeIDs)
	if err != nil || current.ResourceVersion != expectedVersion || current.ConnectionMode == "connector_local" {
		return db.PendingAction{}, ErrVersionConflict
	}
	before, _ := DecodeLabels(current.Labels)
	impact, _, err := ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "host", hostID.String(), before, map[string]string{})
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := hostActionPlan{Operation: "delete", HostID: hostID, Input: HostInput{ExpectedVersion: expectedVersion}, Impact: impact}
	return service.prepareAction(ctx, subject, enterpriseID, PrepareActionInput{ActionType: "host.delete", Title: "Delete host", Summary: "Logically delete host " + current.Name,
		Risk: "dangerous", ResourceType: "host", ResourceID: uuid.NullUUID{UUID: hostID, Valid: true}, ExpectedResourceVersion: pgtype.Int8{Int64: expectedVersion, Valid: true},
		AuthorizationVersion: subject.AuthorizationVersion, Preview: map[string]any{"host_id": hostID, "affected_subject_count": len(impact.AffectedSubjects)},
		Diff: []map[string]string{{"kind": "remove", "text": "Delete host " + current.Name}}, ImmutablePlan: plan, ResourceScopeSnapshot: impact, CommitHandler: "argus.host.delete.commit"}, idempotencyKey)
}

func (service Service) Confirm(ctx context.Context, subject Subject, enterpriseID uuid.UUID, actionRef, idempotencyKey string) (ActionConfirmation, error) {
	return service.Actions.Confirm(ctx, subject.ActorID, enterpriseID, subject.AuthorizationVersion, actionRef, idempotencyKey, service.revalidateAction, service.commitAction)
}

// RevalidatePendingAction is the internal Action Executor boundary. Public HTTP
// handlers must never accept or forward the immutable plan carried here.
func (service Service) RevalidatePendingAction(ctx context.Context, q *db.Queries, action db.PendingAction, plan json.RawMessage) ([]byte, error) {
	return service.revalidateAction(ctx, q, action, plan)
}

// CommitPendingAction applies a previously frozen and revalidated plan. It is
// intentionally only used by the Action Executor, never by the model catalog.
func (service Service) CommitPendingAction(ctx context.Context, q *db.Queries, action db.PendingAction, plan json.RawMessage) (ActionCommitResult, error) {
	return service.commitAction(ctx, q, action, plan)
}

// ExecutePendingAction is the sole internal commit boundary used by the M4
// Action Executor. It verifies and consumes the private action token.
func (service Service) ExecutePendingAction(ctx context.Context, q *db.Queries, action db.PendingAction) (ActionCommitResult, error) {
	return service.Actions.ExecuteReady(ctx, q, action, service.revalidateAction, service.commitAction)
}

func (service Service) revalidateAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) ([]byte, error) {
	switch action.ResourceType {
	case "host":
		var plan hostActionPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return nil, err
		}
		before := map[string]string{}
		var current db.Host
		if plan.Operation != "create" {
			var err error
			current, err = q.GetHost(ctx, db.GetHostParams{ID: plan.HostID, EnterpriseID: action.EnterpriseID})
			if err != nil || current.ResourceVersion != plan.Input.ExpectedVersion {
				return nil, ErrActionInvalidated
			}
			before, _ = DecodeLabels(current.Labels)
		}
		if plan.Operation == "update" && (hostNetworkPathChanged(current, plan.Input) || plan.Input.ConnectionTestID.Valid) {
			if _, _, err := service.requireHostUpdateConnectionTest(ctx, q, action.EnterpriseID, current, plan.Input); err != nil {
				return nil, ErrActionInvalidated
			}
		}
		after := plan.Input.Labels
		if plan.Operation == "delete" {
			after = map[string]string{}
		} else if after == nil {
			after = before
		} else {
			after = MergeSystemLabels(after, before)
		}
		_, hash, err := ComputeLabelImpact(ctx, q, action.EnterpriseID, "host", plan.HostID.String(), before, after)
		return hash, err
	case "kubernetes_cluster":
		var plan kubernetesActionPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return nil, err
		}
		before := map[string]string{}
		var current db.KubernetesCluster
		if plan.Operation != "create" {
			var err error
			current, err = q.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: plan.ClusterID, EnterpriseID: action.EnterpriseID})
			if err != nil || current.ResourceVersion != plan.Input.ExpectedVersion {
				return nil, ErrActionInvalidated
			}
			before, _ = DecodeLabels(current.Labels)
		}
		if plan.Operation == "update" && (kubernetesNetworkPathChanged(current, plan.Input) || plan.Input.ConnectionTestID.Valid) {
			if _, _, err := service.requireKubernetesUpdateConnectionTest(ctx, q, action.EnterpriseID, current, plan.Input); err != nil {
				return nil, ErrActionInvalidated
			}
		}
		after := plan.Input.Labels
		if plan.Operation == "delete" {
			after = map[string]string{}
		} else if after == nil {
			after = before
		} else {
			after = MergeSystemLabels(after, before)
		}
		_, hash, err := ComputeLabelImpact(ctx, q, action.EnterpriseID, "kubernetes_cluster", plan.ClusterID.String(), before, after)
		return hash, err
	default:
		if service.Extension != nil {
			return service.Extension.RevalidateAction(ctx, q, action, raw)
		}
		return nil, ErrActionInvalidated
	}
}

func (service Service) commitAction(ctx context.Context, q *db.Queries, action db.PendingAction, raw json.RawMessage) (ActionCommitResult, error) {
	if action.ResourceType == "host" {
		var plan hostActionPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return ActionCommitResult{}, err
		}
		return service.commitHost(ctx, q, action.EnterpriseID, plan)
	}
	if action.ResourceType == "kubernetes_cluster" {
		var plan kubernetesActionPlan
		if err := json.Unmarshal(raw, &plan); err != nil {
			return ActionCommitResult{}, err
		}
		return service.commitKubernetes(ctx, q, action.CreatorSubjectID.String(), action.EnterpriseID, plan)
	}
	if service.Extension != nil {
		return service.Extension.CommitAction(ctx, q, action, raw)
	}
	return ActionCommitResult{}, ErrActionInvalidated
}

func (service Service) commitHost(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, plan hostActionPlan) (ActionCommitResult, error) {
	var host db.Host
	var err error
	switch plan.Operation {
	case "create":
		labels, hash, normalizeErr := NormalizeStoredLabels(plan.Input.Labels)
		if normalizeErr != nil {
			return ActionCommitResult{}, normalizeErr
		}
		host, err = q.CreateHost(ctx, db.CreateHostParams{ID: plan.HostID, EnterpriseID: enterpriseID, Name: plan.Input.Name, Hostname: plan.Input.Hostname,
			Address: plan.Input.Address, Port: plan.Input.Port, Platform: plan.Input.Platform, ConnectionMode: plan.Input.ConnectionMode,
			BastionScopeID: plan.Input.BastionScopeID, Environment: plan.Input.Environment, Labels: labels, LabelsHash: hash, ConnectionStatus: "online", PinnedHostKey: plan.PinnedKey})
		if err == nil && plan.Input.CredentialID.Valid {
			protocol, privilege := "ssh", "sudo"
			if plan.Input.ConnectionMode == "direct_winrm" || (plan.Input.ConnectionMode == "via_bastion" && plan.Input.Platform == "windows") {
				protocol, privilege = "winrm", "administrator"
			}
			_, err = q.CreateManagedAccount(ctx, db.CreateManagedAccountParams{ID: newResourceID(), EnterpriseID: enterpriseID, HostID: host.ID,
				Username: plan.Input.Username, PrivilegeLevel: privilege, CredentialID: plan.Input.CredentialID.UUID, AllowedProtocols: []string{protocol}})
		}
	case "update":
		params := db.UpdateHostParams{ID: plan.HostID, EnterpriseID: enterpriseID, ResourceVersion: plan.Input.ExpectedVersion, Name: text(plan.Input.Name),
			Environment: text(plan.Input.Environment), Hostname: text(plan.Input.Hostname), Address: text(plan.Input.Address), ConnectionMode: text(plan.Input.ConnectionMode),
			SetBastionScope: plan.Input.ConnectionMode != "", BastionScopeID: plan.Input.BastionScopeID, PinnedHostKey: text(plan.PinnedKey)}
		if plan.Input.Port > 0 {
			params.Port = pgtype.Int4{Int32: plan.Input.Port, Valid: true}
		}
		if plan.Input.ConnectionTestID.Valid {
			params.ConnectionStatus = pgtype.Text{String: "online", Valid: true}
		}
		if plan.Input.Labels != nil {
			current, getErr := q.GetHost(ctx, db.GetHostParams{ID: plan.HostID, EnterpriseID: enterpriseID})
			if getErr != nil {
				return ActionCommitResult{}, getErr
			}
			before, _ := DecodeLabels(current.Labels)
			params.Labels, params.LabelsHash, err = NormalizeStoredLabels(MergeSystemLabels(plan.Input.Labels, before))
			if err != nil {
				return ActionCommitResult{}, err
			}
		}
		host, err = q.UpdateHost(ctx, params)
	case "delete":
		host, err = q.DeleteHost(ctx, db.DeleteHostParams{ID: plan.HostID, EnterpriseID: enterpriseID, ResourceVersion: plan.Input.ExpectedVersion})
	default:
		return ActionCommitResult{}, ErrActionInvalidated
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ActionCommitResult{}, ErrActionInvalidated
	}
	if err != nil {
		return ActionCommitResult{}, err
	}
	if err := ApplyLabelImpact(ctx, q, enterpriseID, plan.Impact); err != nil {
		return ActionCommitResult{}, err
	}
	return ActionCommitResult{ResourceType: "host", ResourceID: host.ID, ResourceVersion: host.ResourceVersion, Summary: "Host change committed"}, nil
}

func (service Service) requireConnectionTest(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, testID uuid.NullUUID, targetType string) (db.ConnectionTest, connectionPlan, ConnectionTestResult, error) {
	if !testID.Valid {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	test, err := q.GetConnectionTest(ctx, db.GetConnectionTestParams{ID: testID.UUID, EnterpriseID: enterpriseID})
	if err != nil || test.TargetType != targetType || test.Status != "succeeded" || time.Now().UTC().After(test.ExpiresAt.Time) {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	var plan connectionPlan
	if err := json.Unmarshal(test.RequestPlan, &plan); err != nil || plan.TargetType != targetType {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	hash := sha256.Sum256(encoded)
	if !subtleEqual(hash[:], test.RequestHash) {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	var result ConnectionTestResult
	if err := json.Unmarshal(test.Result, &result); err != nil {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	if len(plan.ResolvedIPs) > 0 && !slices.Equal(plan.ResolvedIPs, result.ResolvedIPs) {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	if err := service.validateConnectionPlanState(ctx, q, enterpriseID, test, plan); err != nil {
		return db.ConnectionTest{}, connectionPlan{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	return test, plan, result, nil
}

func (service Service) requireHostConnectionTest(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, input HostInput) (db.ConnectionTest, ConnectionTestResult, error) {
	test, plan, result, err := service.requireConnectionTest(ctx, q, enterpriseID, input.ConnectionTestID, "host")
	if err != nil || !hostConnectionPlanMatches(plan, input) {
		return db.ConnectionTest{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	return test, result, nil
}

func (service Service) requireHostUpdateConnectionTest(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, current db.Host, input HostInput) (db.ConnectionTest, ConnectionTestResult, error) {
	test, plan, result, err := service.requireConnectionTest(ctx, q, enterpriseID, input.ConnectionTestID, "host")
	if err != nil || !hostUpdateConnectionPlanMatches(plan, current, input) {
		return db.ConnectionTest{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	return test, result, nil
}

func (service Service) requireKubernetesConnectionTest(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, input KubernetesInput) (db.ConnectionTest, ConnectionTestResult, error) {
	test, plan, result, err := service.requireConnectionTest(ctx, q, enterpriseID, input.ConnectionTestID, "kubernetes_cluster")
	address := input.APIServer
	if input.ConnectionMode == "direct" {
		parsed, parseErr := url.Parse(input.APIServer)
		if parseErr != nil {
			return db.ConnectionTest{}, ConnectionTestResult{}, ErrConnectionTestNeeded
		}
		address = parsed.String()
	}
	if err != nil || !kubernetesConnectionPlanMatches(plan, input, address) {
		return db.ConnectionTest{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	return test, result, nil
}

func hostConnectionPlanMatches(plan connectionPlan, input HostInput) bool {
	return plan.TargetType == "host" && plan.Address == input.Address && plan.Port == input.Port && plan.Platform == input.Platform &&
		plan.Username == input.Username && plan.ConnectionMode == input.ConnectionMode && plan.BastionScopeID == input.BastionScopeID &&
		plan.CredentialID == input.CredentialID
}

func hostNetworkPathChanged(current db.Host, input HostInput) bool {
	if input.Address != "" && input.Address != current.Address {
		return true
	}
	if input.Port > 0 && input.Port != current.Port {
		return true
	}
	if input.ConnectionMode == "" {
		return false
	}
	return input.ConnectionMode != current.ConnectionMode || input.BastionScopeID != current.BastionScopeID
}

func editableHostConnectionMode(mode string) bool {
	return mode == "via_bastion" || mode == "direct_ssh" || mode == "direct_winrm"
}

func hostUpdateConnectionPlanMatches(plan connectionPlan, current db.Host, input HostInput) bool {
	address, port, mode, scopeID := current.Address, current.Port, current.ConnectionMode, current.BastionScopeID
	if input.Address != "" {
		address = input.Address
	}
	if input.Port > 0 {
		port = input.Port
	}
	if input.ConnectionMode != "" {
		mode, scopeID = input.ConnectionMode, input.BastionScopeID
	}
	return plan.TargetType == "host" && plan.Address == address && plan.Port == port && plan.Platform == current.Platform &&
		plan.ConnectionMode == mode && plan.BastionScopeID == scopeID && plan.CredentialID.Valid && plan.Username != ""
}

func kubernetesConnectionPlanMatches(plan connectionPlan, input KubernetesInput, normalizedAddress string) bool {
	return plan.TargetType == "kubernetes_cluster" && plan.Address == normalizedAddress && plan.ConnectionMode == input.ConnectionMode &&
		plan.BastionScopeID == input.BastionScopeID && plan.CredentialID == input.CredentialID
}

func (service Service) validateConnectionPlanState(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, test db.ConnectionTest, plan connectionPlan) error {
	if !plan.CredentialID.Valid || !test.CredentialID.Valid || plan.CredentialID != test.CredentialID || !test.CredentialVersion.Valid || plan.CredentialVersion != test.CredentialVersion.Int64 {
		return ErrConnectionTestNeeded
	}
	credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: plan.CredentialID.UUID, EnterpriseID: enterpriseID})
	if err != nil || credential.Status != "active" || credential.Version != plan.CredentialVersion {
		return ErrConnectionTestNeeded
	}
	if plan.ConnectorID.Valid {
		if !test.ConnectorID.Valid || plan.ConnectorID != test.ConnectorID || !test.ConnectionEpoch.Valid {
			return ErrConnectionTestNeeded
		}
		connector, err := q.GetConnector(ctx, db.GetConnectorParams{ID: plan.ConnectorID.UUID, EnterpriseID: enterpriseID})
		if err != nil || connector.Status != "online" || connector.ConnectionEpoch != test.ConnectionEpoch.Int64 {
			return ErrConnectionTestNeeded
		}
		if !plan.BastionScopeID.Valid {
			return ErrConnectionTestNeeded
		}
		scope, err := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: plan.BastionScopeID.UUID, EnterpriseID: enterpriseID})
		if err != nil || scope.Status != "active" || scope.ActiveConnectorID != plan.ConnectorID {
			return ErrConnectionTestNeeded
		}
		return nil
	}
	if test.ConnectorID.Valid || test.ConnectionEpoch.Valid || len(plan.ResolvedIPs) == 0 {
		return ErrConnectionTestNeeded
	}
	allowed := make([]netip.Addr, 0, len(plan.ResolvedIPs))
	for _, value := range plan.ResolvedIPs {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return ErrConnectionTestNeeded
		}
		allowed = append(allowed, address)
	}
	host := plan.Address
	if plan.TargetType == "kubernetes_cluster" {
		parsed, err := url.Parse(plan.Address)
		if err != nil || parsed.Hostname() == "" {
			return ErrConnectionTestNeeded
		}
		host = parsed.Hostname()
	}
	return service.Direct.Revalidate(ctx, host, allowed)
}

func (service Service) testExpiry() time.Time {
	ttl := service.TestTTL
	if ttl <= 0 || ttl > 15*time.Minute {
		ttl = 10 * time.Minute
	}
	return time.Now().UTC().Add(ttl)
}

func text(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
