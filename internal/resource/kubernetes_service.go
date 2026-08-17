package resource

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func (service Service) ListKubernetesClusters(ctx context.Context, enterpriseID uuid.UUID, scopeIDs []uuid.UUID) ([]db.KubernetesCluster, error) {
	items, err := service.Store.Queries.ListKubernetesClusters(ctx, enterpriseID)
	if err != nil {
		return nil, err
	}
	return service.Access.FilterKubernetesClusters(ctx, enterpriseID, scopeIDs, items)
}

func (service Service) GetKubernetesCluster(ctx context.Context, enterpriseID, clusterID uuid.UUID, scopeIDs []uuid.UUID) (db.KubernetesCluster, error) {
	cluster, err := service.Store.Queries.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: clusterID, EnterpriseID: enterpriseID})
	if err != nil {
		return db.KubernetesCluster{}, err
	}
	labels, err := DecodeLabels(cluster.Labels)
	if err != nil {
		return db.KubernetesCluster{}, err
	}
	allowed, _, err := service.Access.CanAccess(ctx, enterpriseID, scopeIDs, "kubernetes_cluster", cluster.ID.String(), labels)
	if err != nil {
		return db.KubernetesCluster{}, err
	}
	if !allowed {
		return db.KubernetesCluster{}, ErrResourceDenied
	}
	return cluster, nil
}

func (service Service) CreateKubernetesConnectionTest(ctx context.Context, subject Subject, enterpriseID uuid.UUID, input KubernetesInput, idempotencyKey string) (db.ConnectionTest, error) {
	test, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Actions.Idempotency, "enterprise", subject.ActorID, "kubernetes.connection_test.create", idempotencyKey, input, 202, func(q *db.Queries) (db.ConnectionTest, error) {
		plan, params, err := service.kubernetesConnectionPlan(ctx, q, enterpriseID, subject.ActorID, input)
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
		return test, appendResourceAudit(ctx, q, subject.ActorID, enterpriseID, "kubernetes.connection_test.create", "connection_test", test.ID, map[string]any{"status": "queued"})
	})
	if err != nil {
		return db.ConnectionTest{}, err
	}
	if test.Path == "direct" && service.DirectCommands != nil {
		// The durable queue remains recoverable when the low-latency RPC hint is lost.
		_ = service.DirectCommands.DispatchConnectionTest(ctx, test)
	} else if test.Path == "connector" && service.Commands != nil && test.ConnectorID.Valid && test.ConnectionEpoch.Valid {
		service.Commands.NotifyConnectorCommand(ctx, test.ConnectorID.UUID, test.ConnectionEpoch.Int64)
	}
	return test, nil
}

func (service Service) kubernetesConnectionPlan(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, actorID string, input KubernetesInput) (connectionPlan, db.CreateConnectionTestParams, error) {
	if input.ConnectionMode != "via_bastion" && input.ConnectionMode != "direct" {
		return connectionPlan{}, db.CreateConnectionTestParams{}, ErrInvalidConnectionMode
	}
	if !input.CredentialID.Valid {
		return connectionPlan{}, db.CreateConnectionTestParams{}, ErrConnectionTestNeeded
	}
	credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: input.CredentialID.UUID, EnterpriseID: enterpriseID})
	if err != nil || credential.Status != "active" || credential.Protocol != "kubernetes" {
		return connectionPlan{}, db.CreateConnectionTestParams{}, ErrConnectionTestNeeded
	}
	plan := connectionPlan{TargetType: "kubernetes_cluster", Address: input.APIServer, ConnectionMode: input.ConnectionMode,
		BastionScopeID: input.BastionScopeID, CredentialID: input.CredentialID, CredentialVersion: credential.Version}
	params := db.CreateConnectionTestParams{ID: newResourceID(), EnterpriseID: enterpriseID, TargetType: "kubernetes_cluster", Path: "direct",
		CredentialID: input.CredentialID, CredentialVersion: pgtype.Int8{Int64: credential.Version, Valid: true}, CreatedBy: uuid.MustParse(actorID),
		ExpiresAt: pgtype.Timestamptz{Time: service.testExpiry(), Valid: true}}
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
		parsed, addresses, err := service.Direct.ResolveHTTPS(ctx, input.APIServer)
		if err != nil {
			return connectionPlan{}, db.CreateConnectionTestParams{}, err
		}
		plan.Address = parsed.String()
		for _, address := range addresses {
			plan.ResolvedIPs = append(plan.ResolvedIPs, address.String())
		}
	}
	return plan, params, nil
}

func (service Service) PreviewCreateKubernetesCluster(ctx context.Context, subject Subject, enterpriseID uuid.UUID, input KubernetesInput, idempotencyKey string) (db.PendingAction, error) {
	result := ConnectionTestResult{}
	if input.ConnectionMode != "in_cluster" {
		_, checked, err := service.requireKubernetesConnectionTest(ctx, service.Store.Queries, enterpriseID, input)
		if err != nil {
			return db.PendingAction{}, err
		}
		result = checked
	} else if input.CredentialID.Valid || input.BastionScopeID.Valid {
		return db.PendingAction{}, ErrInvalidConnectionMode
	}
	labelsJSON, _, err := NormalizeUserLabels(input.Labels)
	if err != nil {
		return db.PendingAction{}, err
	}
	labels, _ := DecodeLabels(labelsJSON)
	clusterID := newResourceID()
	allowed, matched, err := service.Access.CanAccess(ctx, enterpriseID, subject.DataScopeIDs, "kubernetes_cluster", clusterID.String(), labels)
	if err != nil || !allowed {
		return db.PendingAction{}, ErrResourceDenied
	}
	impact, _, err := ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "kubernetes_cluster", clusterID.String(), map[string]string{}, labels)
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := kubernetesActionPlan{Operation: "create", ClusterID: clusterID, Input: input, Impact: impact, Version: result.RemoteVersion}
	return service.prepareAction(ctx, subject, enterpriseID, PrepareActionInput{ActionType: "kubernetes.create", Title: "Create Kubernetes cluster",
		Summary: "Create a validated Kubernetes cluster", Risk: "write", ResourceType: "kubernetes_cluster", ResourceID: uuid.NullUUID{UUID: clusterID, Valid: true},
		AuthorizationVersion: subject.AuthorizationVersion, Preview: map[string]any{"cluster_id": clusterID, "name": input.Name, "matched_data_scope_ids": matched,
			"affected_subject_count": len(impact.AffectedSubjects)}, Diff: []map[string]string{{"kind": "add", "text": "Create Kubernetes cluster " + input.Name}},
		ImmutablePlan: plan, ResourceScopeSnapshot: impact, CommitHandler: "argus.kubernetes.create.commit"}, idempotencyKey)
}

func (service Service) PreviewUpdateKubernetesCluster(ctx context.Context, subject Subject, enterpriseID, clusterID uuid.UUID, input KubernetesInput, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.GetKubernetesCluster(ctx, enterpriseID, clusterID, subject.DataScopeIDs)
	if err != nil || current.ResourceVersion != input.ExpectedVersion {
		return db.PendingAction{}, ErrVersionConflict
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
	version := current.KubernetesVersion
	pathChanged := kubernetesNetworkPathChanged(current, input)
	if pathChanged && effectiveKubernetesConnectionMode(current, input) == "in_cluster" && current.ConnectionMode != "in_cluster" {
		return db.PendingAction{}, ErrInvalidConnectionMode
	}
	if pathChanged || input.ConnectionTestID.Valid {
		_, result, err := service.requireKubernetesUpdateConnectionTest(ctx, service.Store.Queries, enterpriseID, current, input)
		if err != nil {
			return db.PendingAction{}, err
		}
		version = result.RemoteVersion
	}
	impact, _, err := ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "kubernetes_cluster", clusterID.String(), before, after)
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := kubernetesActionPlan{Operation: "update", ClusterID: clusterID, Input: input, Impact: impact, Version: version}
	return service.prepareAction(ctx, subject, enterpriseID, PrepareActionInput{ActionType: "kubernetes.update", Title: "Update Kubernetes cluster",
		Summary: "Apply validated Kubernetes cluster changes", Risk: "write", ResourceType: "kubernetes_cluster", ResourceID: uuid.NullUUID{UUID: clusterID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: input.ExpectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"cluster_id": clusterID, "affected_subject_count": len(impact.AffectedSubjects)},
		Diff:    []map[string]string{{"kind": "change", "text": "Update Kubernetes cluster " + current.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: impact, CommitHandler: "argus.kubernetes.update.commit"}, idempotencyKey)
}

func (service Service) requireKubernetesUpdateConnectionTest(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, current db.KubernetesCluster, input KubernetesInput) (db.ConnectionTest, ConnectionTestResult, error) {
	mode := effectiveKubernetesConnectionMode(current, input)
	if mode == "in_cluster" {
		if current.ConnectionMode == "in_cluster" && !kubernetesNetworkPathChanged(current, input) && !input.ConnectionTestID.Valid {
			return db.ConnectionTest{}, ConnectionTestResult{RemoteVersion: current.KubernetesVersion}, nil
		}
		return db.ConnectionTest{}, ConnectionTestResult{}, ErrConnectionTestNeeded
	}
	effective := KubernetesInput{APIServer: current.ApiServer, ConnectionMode: mode, BastionScopeID: current.BastionScopeID,
		CredentialID: current.CredentialID, ConnectionTestID: input.ConnectionTestID}
	if input.APIServer != "" {
		effective.APIServer = input.APIServer
	}
	if input.ConnectionMode != "" {
		effective.BastionScopeID, effective.CredentialID = input.BastionScopeID, input.CredentialID
	}
	return service.requireKubernetesConnectionTest(ctx, q, enterpriseID, effective)
}

func effectiveKubernetesConnectionMode(current db.KubernetesCluster, input KubernetesInput) string {
	if input.ConnectionMode != "" {
		return input.ConnectionMode
	}
	return current.ConnectionMode
}

func kubernetesNetworkPathChanged(current db.KubernetesCluster, input KubernetesInput) bool {
	if input.APIServer != "" && input.APIServer != current.ApiServer {
		return true
	}
	if input.ConnectionMode == "" {
		return false
	}
	return input.ConnectionMode != current.ConnectionMode || input.BastionScopeID != current.BastionScopeID || input.CredentialID != current.CredentialID
}

func (service Service) PreviewDeleteKubernetesCluster(ctx context.Context, subject Subject, enterpriseID, clusterID uuid.UUID, expectedVersion int64, idempotencyKey string) (db.PendingAction, error) {
	current, err := service.GetKubernetesCluster(ctx, enterpriseID, clusterID, subject.DataScopeIDs)
	if err != nil || current.ResourceVersion != expectedVersion {
		return db.PendingAction{}, ErrVersionConflict
	}
	before, _ := DecodeLabels(current.Labels)
	impact, _, err := ComputeLabelImpact(ctx, service.Store.Queries, enterpriseID, "kubernetes_cluster", clusterID.String(), before, map[string]string{})
	if err != nil {
		return db.PendingAction{}, err
	}
	plan := kubernetesActionPlan{Operation: "delete", ClusterID: clusterID, Input: KubernetesInput{ExpectedVersion: expectedVersion}, Impact: impact}
	return service.prepareAction(ctx, subject, enterpriseID, PrepareActionInput{ActionType: "kubernetes.delete", Title: "Delete Kubernetes cluster",
		Summary: "Logically delete Kubernetes cluster " + current.Name, Risk: "dangerous", ResourceType: "kubernetes_cluster", ResourceID: uuid.NullUUID{UUID: clusterID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: expectedVersion, Valid: true}, AuthorizationVersion: subject.AuthorizationVersion,
		Preview: map[string]any{"cluster_id": clusterID, "affected_subject_count": len(impact.AffectedSubjects)},
		Diff:    []map[string]string{{"kind": "remove", "text": "Delete Kubernetes cluster " + current.Name}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: impact, CommitHandler: "argus.kubernetes.delete.commit"}, idempotencyKey)
}

func (service Service) commitKubernetes(ctx context.Context, q *db.Queries, actorID string, enterpriseID uuid.UUID, plan kubernetesActionPlan) (ActionCommitResult, error) {
	var cluster db.KubernetesCluster
	var err error
	switch plan.Operation {
	case "create":
		labels, hash, normalizeErr := NormalizeStoredLabels(plan.Input.Labels)
		if normalizeErr != nil {
			return ActionCommitResult{}, normalizeErr
		}
		status := "connected"
		if plan.Input.ConnectionMode == "in_cluster" {
			status = "pending_connector"
		}
		cluster, err = q.CreateKubernetesCluster(ctx, db.CreateKubernetesClusterParams{ID: plan.ClusterID, EnterpriseID: enterpriseID, Name: plan.Input.Name,
			ApiServer: plan.Input.APIServer, ConnectionMode: plan.Input.ConnectionMode, BastionScopeID: plan.Input.BastionScopeID,
			CredentialID: plan.Input.CredentialID, DefaultNamespace: plan.Input.DefaultNamespace, Environment: plan.Input.Environment,
			Labels: labels, LabelsHash: hash, ConnectionStatus: status})
	case "update":
		params := db.UpdateKubernetesClusterParams{ID: plan.ClusterID, EnterpriseID: enterpriseID, ResourceVersion: plan.Input.ExpectedVersion,
			Name: text(plan.Input.Name), Environment: text(plan.Input.Environment), ApiServer: text(plan.Input.APIServer), ConnectionMode: text(plan.Input.ConnectionMode),
			SetBastionScope: plan.Input.ConnectionMode != "", BastionScopeID: plan.Input.BastionScopeID, SetCredential: plan.Input.ConnectionMode != "",
			CredentialID: plan.Input.CredentialID, DefaultNamespace: text(plan.Input.DefaultNamespace)}
		if plan.Input.ConnectionTestID.Valid {
			params.ConnectionStatus = pgtype.Text{String: "connected", Valid: true}
		}
		if plan.Input.Labels != nil {
			current, getErr := q.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: plan.ClusterID, EnterpriseID: enterpriseID})
			if getErr != nil {
				return ActionCommitResult{}, getErr
			}
			before, _ := DecodeLabels(current.Labels)
			params.Labels, params.LabelsHash, err = NormalizeStoredLabels(MergeSystemLabels(plan.Input.Labels, before))
			if err != nil {
				return ActionCommitResult{}, err
			}
		}
		cluster, err = q.UpdateKubernetesCluster(ctx, params)
	case "delete":
		cluster, err = q.DeleteKubernetesCluster(ctx, db.DeleteKubernetesClusterParams{ID: plan.ClusterID, EnterpriseID: enterpriseID, ResourceVersion: plan.Input.ExpectedVersion})
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
	result := ActionCommitResult{ResourceType: "kubernetes_cluster", ResourceID: cluster.ID, ResourceVersion: cluster.ResourceVersion, Summary: "Kubernetes cluster change committed"}
	if plan.Operation == "create" && plan.Input.ConnectionMode == "in_cluster" {
		if service.ClusterEnrollment == nil {
			return ActionCommitResult{}, ErrActionInvalidated
		}
		enrollment, err := service.ClusterEnrollment.CreateKubernetesEnrollment(ctx, q, actorID, enterpriseID, cluster.ID)
		if err != nil {
			return ActionCommitResult{}, err
		}
		result.Enrollment = &enrollment
	}
	return result, nil
}

func (service Service) KubernetesNamespaceAllowed(ctx context.Context, subject Subject, enterpriseID, clusterID uuid.UUID, namespace string) error {
	if _, err := service.GetKubernetesCluster(ctx, enterpriseID, clusterID, subject.DataScopeIDs); err != nil {
		return err
	}
	allowed, err := service.Access.CanAccessNamespace(ctx, enterpriseID, subject.DataScopeIDs, clusterID, namespace)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrResourceDenied
	}
	return nil
}

func (service Service) ListKubernetesResources(ctx context.Context, subject Subject, enterpriseID, clusterID uuid.UUID, query KubernetesQuery) ([]KubernetesObject, error) {
	cluster, err := service.GetKubernetesCluster(ctx, enterpriseID, clusterID, subject.DataScopeIDs)
	if err != nil {
		return nil, err
	}
	if service.Kubernetes == nil || query.Limit < 1 || query.Limit > 500 {
		return nil, ErrKubernetesUnavailable
	}
	namespaced := query.ResourceType == "pod" || query.ResourceType == "deployment" || query.ResourceType == "statefulset" || query.ResourceType == "daemonset" || query.ResourceType == "service"
	if namespaced {
		if query.Namespace == "" {
			return nil, ErrResourceDenied
		}
		if err := service.KubernetesNamespaceAllowed(ctx, subject, enterpriseID, clusterID, query.Namespace); err != nil {
			return nil, err
		}
	}
	items, err := service.Kubernetes.List(ctx, cluster, query)
	if err != nil {
		return nil, err
	}
	if query.ResourceType != "namespace" {
		return items, nil
	}
	filtered := make([]KubernetesObject, 0, len(items))
	for _, item := range items {
		if err := service.KubernetesNamespaceAllowed(ctx, subject, enterpriseID, clusterID, item.Name); err == nil {
			filtered = append(filtered, item)
		}
	}
	return filtered, nil
}

func (service Service) GetKubernetesPodLogs(ctx context.Context, subject Subject, enterpriseID, clusterID uuid.UUID, query PodLogsQuery) ([]byte, bool, error) {
	cluster, err := service.GetKubernetesCluster(ctx, enterpriseID, clusterID, subject.DataScopeIDs)
	if err != nil {
		return nil, false, err
	}
	if err := service.KubernetesNamespaceAllowed(ctx, subject, enterpriseID, clusterID, query.Namespace); err != nil {
		return nil, false, err
	}
	if service.Kubernetes == nil || query.TailLines < 1 || query.TailLines > 5000 {
		return nil, false, ErrKubernetesUnavailable
	}
	content, truncated, err := service.Kubernetes.PodLogs(ctx, cluster, query)
	if err != nil {
		return nil, false, err
	}
	if len(content) > 1<<20 {
		content = content[:1<<20]
		truncated = true
	}
	return content, truncated, nil
}

func (service Service) ConnectionTestExpired(test db.ConnectionTest) bool {
	return time.Now().UTC().After(test.ExpiresAt.Time)
}
