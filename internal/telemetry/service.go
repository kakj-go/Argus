package telemetry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
)

var (
	ErrUnavailable         = errors.New("telemetry service unavailable")
	ErrNotFound            = errors.New("telemetry resource not found")
	ErrDenied              = errors.New("telemetry resource is not explicitly authorized")
	ErrDistributionPending = errors.New("collector distribution validation pending")
	ErrQueryInvalid        = errors.New("telemetry query invalid")
	ErrQueryBudget         = errors.New("telemetry query budget exceeded")
	ErrQueryParse          = errors.New("telemetry query parse error")
	ErrQueryType           = errors.New("telemetry query type error")
	ErrQueryUnsupported    = errors.New("telemetry query feature unsupported")
	ErrQueryComplexity     = errors.New("telemetry query complexity limit")
	ErrQueryScope          = errors.New("telemetry query scope denied")
)

type Actor struct {
	EnterpriseID          uuid.UUID
	SubjectID             uuid.UUID
	AuthorizationVersion  int64
	AuthorizedResourceIDs []uuid.UUID
}

type Service struct {
	Store                  *postgres.Store
	Access                 resource.AccessService
	Actions                resource.PendingActionService
	Query                  OverviewBackend
	Engine                 EngineQueryBackend
	OtelcolKubernetesImage string
}

type EngineQueryBackend interface {
	ExecuteEngineQuery(context.Context, queryengine.Request) (queryengine.Result, error)
}

type CollectorPreviewInput struct {
	DistributionVersionID uuid.UUID
	ProfileIDs            []uuid.UUID
	RouteKind             string
	GatewayCollectorID    uuid.NullUUID
	ExpectedVersion       int64
	// KubernetesImage 为空时使用服务端配置的默认镜像;仅 kubernetes_cluster 资源允许提供。
	KubernetesImage string
}

type collectorActionPlan struct {
	Operation             string        `json:"operation"`
	ResourceType          string        `json:"resource_type"`
	ResourceID            uuid.UUID     `json:"resource_id"`
	DistributionVersionID uuid.UUID     `json:"distribution_version_id"`
	ProfileIDs            []uuid.UUID   `json:"profile_ids"`
	ProfileKeys           []string      `json:"profile_keys"`
	RouteKind             string        `json:"route_kind"`
	GatewayCollectorID    uuid.NullUUID `json:"gateway_collector_id,omitempty"`
	GatewayEndpoint       string        `json:"gateway_endpoint,omitempty"`
	GatewayServerName     string        `json:"gateway_server_name,omitempty"`
	ExpectedVersion       int64         `json:"expected_version"`
	ArtifactHashes        []string      `json:"artifact_hashes"`
	Platform              string        `json:"platform"`
	Role                  string        `json:"role"`
	Transport             string        `json:"transport"`
	ConnectorID           uuid.NullUUID `json:"connector_id,omitempty"`
	ConnectionEpoch       int64         `json:"connection_epoch,omitempty"`
	TargetResourceVersion int64         `json:"target_resource_version"`
	TargetAddress         string        `json:"target_address,omitempty"`
	TargetPort            int32         `json:"target_port,omitempty"`
	TargetUsername        string        `json:"target_username,omitempty"`
	PinnedHostKey         string        `json:"pinned_host_key,omitempty"`
	CredentialID          uuid.NullUUID `json:"credential_id,omitempty"`
	CredentialVersion     int64         `json:"credential_version,omitempty"`
	KubernetesImage       string        `json:"kubernetes_image,omitempty"`
}

type collectorExecutionTarget struct {
	Transport, Address, Username, PinnedHostKey string
	Port, ResourceVersion                       int64
	ConnectorID, CredentialID                   uuid.NullUUID
	ConnectionEpoch, CredentialVersion          int64
}

type bindingActionPlan struct {
	Operation       string    `json:"operation"`
	BindingID       uuid.UUID `json:"binding_id"`
	HostID          uuid.UUID `json:"host_id"`
	ExpectedVersion int64     `json:"expected_version"`
	EvidenceHash    string    `json:"evidence_hash"`
}

func (service Service) ListCollectors(ctx context.Context, actor Actor, resourceType string, resourceID uuid.NullUUID, limit int32) ([]db.CollectorInstance, error) {
	if service.Store == nil {
		return nil, ErrUnavailable
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := service.Store.Queries.ListCollectorInstances(ctx, db.ListCollectorInstancesParams{
		EnterpriseID: actor.EnterpriseID,
		ResourceType: pgtype.Text{String: resourceType, Valid: resourceType != ""},
		ResourceID:   resourceID,
		Limit:        limit,
	})
	if err != nil {
		return nil, err
	}
	result := make([]db.CollectorInstance, 0, len(rows))
	for _, row := range rows {
		allowed, err := service.canAccessCollector(ctx, actor, row)
		if err != nil {
			return nil, err
		}
		if allowed {
			result = append(result, row)
		}
	}
	return result, nil
}

func (service Service) GetCollector(ctx context.Context, actor Actor, collectorID uuid.UUID) (db.CollectorInstance, error) {
	row, err := service.Store.Queries.GetCollectorInstance(ctx, db.GetCollectorInstanceParams{ID: collectorID, EnterpriseID: actor.EnterpriseID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CollectorInstance{}, ErrNotFound
	}
	if err != nil {
		return db.CollectorInstance{}, err
	}
	allowed, err := service.canAccessCollector(ctx, actor, row)
	if err != nil {
		return db.CollectorInstance{}, err
	}
	if !allowed {
		return db.CollectorInstance{}, ErrNotFound
	}
	return row, nil
}

func (service Service) GetCollectorForResource(ctx context.Context, actor Actor, resourceType string, resourceID uuid.UUID) (db.CollectorInstance, error) {
	if err := service.requireResource(ctx, actor, resourceType, resourceID); err != nil {
		return db.CollectorInstance{}, err
	}
	row, err := service.Store.Queries.GetCollectorForResource(ctx, db.GetCollectorForResourceParams{EnterpriseID: actor.EnterpriseID, ResourceType: resourceType, ResourceID: resourceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CollectorInstance{}, ErrNotFound
	}
	return row, err
}

func (service Service) ListClaims(ctx context.Context, actor Actor, resourceID uuid.NullUUID) ([]db.CollectionClaim, error) {
	rows, err := service.Store.Queries.ListCollectionClaims(ctx, db.ListCollectionClaimsParams{EnterpriseID: actor.EnterpriseID,
		PhysicalResourceRef: pgtype.Text{String: resourceID.UUID.String(), Valid: resourceID.Valid}})
	if err != nil {
		return nil, err
	}
	result := make([]db.CollectionClaim, 0, len(rows))
	for _, row := range rows {
		collector, err := service.GetCollector(ctx, actor, row.CollectorID)
		if err == nil && collector.ID != uuid.Nil {
			result = append(result, row)
		} else if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	return result, nil
}

func (service Service) ListBindings(ctx context.Context, actor Actor, clusterID uuid.UUID) ([]db.KubernetesNodeHostBinding, error) {
	if err := service.requireResource(ctx, actor, "kubernetes_cluster", clusterID); err != nil {
		return nil, err
	}
	return service.Store.Queries.ListKubernetesNodeHostBindings(ctx, db.ListKubernetesNodeHostBindingsParams{EnterpriseID: actor.EnterpriseID, KubernetesClusterID: clusterID})
}

func (service Service) Usage(ctx context.Context, actor Actor, from, to time.Time) (db.GetTelemetryUsageRow, db.TelemetryRetentionPolicy, error) {
	if from.IsZero() || to.IsZero() || !from.Before(to) || to.Sub(from) > 366*24*time.Hour {
		return db.GetTelemetryUsageRow{}, db.TelemetryRetentionPolicy{}, ErrQueryInvalid
	}
	policy, err := service.Store.Queries.EnsureTelemetryRetentionPolicy(ctx, actor.EnterpriseID)
	if err != nil {
		return db.GetTelemetryUsageRow{}, db.TelemetryRetentionPolicy{}, err
	}
	usage, err := service.Store.Queries.GetTelemetryUsage(ctx, db.GetTelemetryUsageParams{EnterpriseID: actor.EnterpriseID,
		UsageDate: pgtype.Date{Time: from.UTC(), Valid: true}, UsageDate_2: pgtype.Date{Time: to.UTC(), Valid: true}})
	return usage, policy, err
}

func (service Service) PreviewCollectorAction(ctx context.Context, actor Actor, resourceType string, resourceID uuid.UUID, operation string, input CollectorPreviewInput, idempotencyKey string) (db.PendingAction, error) {
	if !slices.Contains([]string{"install", "configure", "upgrade", "repair", "uninstall"}, operation) {
		return db.PendingAction{}, ErrQueryInvalid
	}
	if err := service.requireResource(ctx, actor, resourceType, resourceID); err != nil {
		return db.PendingAction{}, err
	}
	distribution, err := service.distribution(ctx, input.DistributionVersionID)
	if err != nil {
		return db.PendingAction{}, err
	}
	if distribution.SupportStatus != "supported" {
		return db.PendingAction{}, ErrDistributionPending
	}
	if input.RouteKind != "direct_argus" && input.RouteKind != "bastion_gateway" {
		return db.PendingAction{}, ErrQueryInvalid
	}
	platform, role, err := service.collectorTarget(ctx, actor.EnterpriseID, resourceType, resourceID, input.RouteKind)
	if err != nil {
		return db.PendingAction{}, err
	}
	if !distributionSupportsPlatform(distribution.ArtifactManifest, platform) {
		return db.PendingAction{}, ErrDistributionPending
	}
	profiles, err := service.profiles(ctx, input.ProfileIDs, distribution.ConfigSchemaVersion, distribution.SupportStatus, platform)
	if err != nil {
		return db.PendingAction{}, err
	}
	if operation != "install" {
		current, currentErr := service.Store.Queries.GetCollectorForResource(ctx, db.GetCollectorForResourceParams{EnterpriseID: actor.EnterpriseID, ResourceType: resourceType, ResourceID: resourceID})
		if currentErr != nil || input.ExpectedVersion <= 0 || current.Version != input.ExpectedVersion {
			return db.PendingAction{}, ErrNotFound
		}
	}
	target, err := resolveCollectorExecutionTarget(ctx, service.Store.Queries, actor.EnterpriseID, resourceType, resourceID)
	if err != nil {
		return db.PendingAction{}, err
	}
	artifactHashes, err := artifactHashes(distribution.ArtifactManifest)
	if err != nil {
		return db.PendingAction{}, ErrQueryInvalid
	}
	plan := collectorActionPlan{Operation: operation, ResourceType: resourceType, ResourceID: resourceID,
		DistributionVersionID: input.DistributionVersionID, ProfileIDs: slices.Clone(input.ProfileIDs), RouteKind: input.RouteKind,
		GatewayCollectorID: input.GatewayCollectorID, ExpectedVersion: input.ExpectedVersion, ArtifactHashes: artifactHashes,
		Platform: platform, Role: role, ProfileKeys: slices.Clone(profiles)}
	if input.RouteKind == "bastion_gateway" {
		plan.GatewayEndpoint, plan.GatewayServerName, err = telemetryGatewayEndpoint(ctx, service.Store.Queries, actor.EnterpriseID, input.GatewayCollectorID)
		if err != nil {
			return db.PendingAction{}, err
		}
	}
	plan.Transport, plan.ConnectorID, plan.ConnectionEpoch = target.Transport, target.ConnectorID, target.ConnectionEpoch
	plan.TargetResourceVersion, plan.TargetAddress, plan.TargetPort = target.ResourceVersion, target.Address, int32(target.Port)
	plan.TargetUsername, plan.PinnedHostKey = target.Username, target.PinnedHostKey
	plan.CredentialID, plan.CredentialVersion = target.CredentialID, target.CredentialVersion
	if image, imageErr := collectorPreviewImage(resourceType, input.KubernetesImage, service.OtelcolKubernetesImage); imageErr != nil {
		return db.PendingAction{}, imageErr
	} else if resourceType == "kubernetes_cluster" {
		plan.KubernetesImage = image
	}
	preview := map[string]any{"operation": operation, "resource_type": resourceType, "resource_id": resourceID,
		"distribution": distribution.Version, "profiles": profiles, "route_kind": input.RouteKind}
	if plan.KubernetesImage != "" {
		preview["kubernetes_image"] = plan.KubernetesImage
	}
	risk := collectorActionRisk(operation)
	return service.Actions.Prepare(ctx, actor.SubjectID.String(), actor.EnterpriseID, resource.PrepareActionInput{
		ActionType: "telemetry.collector." + operation, Title: "Collector " + operation,
		Summary: "Apply a deterministic Collector " + operation + " plan", Risk: risk, ResourceType: resourceType,
		ResourceID: uuid.NullUUID{UUID: resourceID, Valid: true}, ExpectedResourceVersion: pgtype.Int8{Int64: input.ExpectedVersion, Valid: input.ExpectedVersion > 0},
		AuthorizationVersion: actor.AuthorizationVersion, Preview: preview,
		Diff:          []map[string]string{{"kind": "change", "text": "Collector " + operation + " for " + resourceID.String()}},
		ImmutablePlan: plan, ResourceScopeSnapshot: map[string]any{"resource_id": resourceID, "profiles": input.ProfileIDs, "route_kind": input.RouteKind},
		CommitHandler: "argus.telemetry.collector." + operation + ".commit",
	}, idempotencyKey)
}

// validKubernetesImage 是 Collector 镜像引用的唯一格式规则:非空、无空白、
// 含 tag 或 digest 分隔符、长度有界;preview 与 revalidate 共用。
func validKubernetesImage(image string) bool {
	return image != "" && len(image) <= 512 && !strings.ContainsAny(image, " \t\r\n") && strings.Contains(image, ":")
}

// collectorPreviewImage 解析 preview 请求中的镜像输入:host 资源不允许携带;
// k8s 资源按 用户覆盖 → 服务端默认 顺序解析,二者皆空视为服务未配置,
// 解析结果非空且格式合法时返回;对 host 资源恒返回空串。
func collectorPreviewImage(resourceType, inputImage, configuredDefault string) (string, error) {
	trimmed := strings.TrimSpace(inputImage)
	if resourceType != "kubernetes_cluster" {
		if trimmed != "" {
			return "", ErrQueryInvalid
		}
		return "", nil
	}
	image := trimmed
	if image == "" {
		image = configuredDefault
	}
	if image == "" {
		return "", ErrUnavailable
	}
	if !validKubernetesImage(image) {
		return "", ErrQueryInvalid
	}
	return image, nil
}

func telemetryGatewayEndpoint(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, gatewayID uuid.NullUUID) (string, string, error) {
	if !gatewayID.Valid {
		return "", "", ErrQueryInvalid
	}
	gateway, err := q.GetCollectorInstance(ctx, db.GetCollectorInstanceParams{ID: gatewayID.UUID, EnterpriseID: enterpriseID})
	if err != nil || gateway.ResourceType != "host" || gateway.Role != "edge_gateway" || gateway.Status == "uninstalled" {
		return "", "", ErrQueryInvalid
	}
	host, err := q.GetHost(ctx, db.GetHostParams{ID: gateway.ResourceID, EnterpriseID: enterpriseID})
	if err != nil || host.ConnectionMode != "connector_local" || host.Address == "" {
		return "", "", ErrQueryInvalid
	}
	return "grpcs://" + net.JoinHostPort(host.Address, "4317"), CollectorCertificateDNSName(gateway.ID), nil
}

func resolveCollectorExecutionTarget(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, resourceType string, resourceID uuid.UUID) (collectorExecutionTarget, error) {
	var result collectorExecutionTarget
	switch resourceType {
	case "host":
		host, err := q.GetHost(ctx, db.GetHostParams{ID: resourceID, EnterpriseID: enterpriseID})
		if err != nil || host.Platform != "linux" {
			return result, ErrDistributionPending
		}
		result.Address, result.Port, result.PinnedHostKey, result.ResourceVersion = host.Address, int64(host.Port), host.PinnedHostKey, host.ResourceVersion
		switch host.ConnectionMode {
		case "direct_ssh":
			result.Transport = "direct"
		case "connector_local", "via_bastion":
			result.Transport = "connector"
			if !host.BastionScopeID.Valid {
				return result, ErrQueryInvalid
			}
			scope, scopeErr := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: host.BastionScopeID.UUID, EnterpriseID: enterpriseID})
			if scopeErr != nil || !scope.ActiveConnectorID.Valid {
				return result, ErrUnavailable
			}
			connector, connectorErr := q.GetConnector(ctx, db.GetConnectorParams{ID: scope.ActiveConnectorID.UUID, EnterpriseID: enterpriseID})
			if connectorErr != nil || connector.Status != "online" || connector.ConnectionEpoch < 1 {
				return result, ErrUnavailable
			}
			result.ConnectorID = uuid.NullUUID{UUID: connector.ID, Valid: true}
			result.ConnectionEpoch = connector.ConnectionEpoch
		default:
			return result, ErrQueryInvalid
		}
		if host.ConnectionMode != "connector_local" {
			accounts, err := q.ListManagedAccounts(ctx, enterpriseID)
			if err != nil {
				return result, err
			}
			matches := make([]db.ManagedAccount, 0, 1)
			for _, account := range accounts {
				if account.HostID == host.ID && account.Status == "active" && slices.Contains(account.AllowedProtocols, "ssh") {
					matches = append(matches, account)
				}
			}
			if len(matches) != 1 {
				return result, ErrQueryInvalid
			}
			credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: matches[0].CredentialID, EnterpriseID: enterpriseID})
			if err != nil || credential.Status != "active" {
				return result, ErrUnavailable
			}
			result.Username, result.CredentialID, result.CredentialVersion = matches[0].Username,
				uuid.NullUUID{UUID: credential.ID, Valid: true}, credential.Version
		}
	case "kubernetes_cluster":
		cluster, err := q.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: resourceID, EnterpriseID: enterpriseID})
		if err != nil {
			return result, ErrNotFound
		}
		result.Address, result.ResourceVersion = cluster.ApiServer, cluster.ResourceVersion
		switch cluster.ConnectionMode {
		case "direct":
			result.Transport = "direct"
		case "in_cluster":
			result.Transport = "connector"
			result.ConnectorID = cluster.ConnectorID
		case "via_bastion":
			result.Transport = "connector"
			if !cluster.BastionScopeID.Valid {
				return result, ErrQueryInvalid
			}
			scope, scopeErr := q.GetBastionScope(ctx, db.GetBastionScopeParams{ID: cluster.BastionScopeID.UUID, EnterpriseID: enterpriseID})
			if scopeErr != nil {
				return result, ErrUnavailable
			}
			result.ConnectorID = scope.ActiveConnectorID
		default:
			return result, ErrQueryInvalid
		}
		if result.Transport == "connector" {
			if !result.ConnectorID.Valid {
				return result, ErrUnavailable
			}
			connector, connectorErr := q.GetConnector(ctx, db.GetConnectorParams{ID: result.ConnectorID.UUID, EnterpriseID: enterpriseID})
			if connectorErr != nil || connector.Status != "online" || connector.ConnectionEpoch < 1 {
				return result, ErrUnavailable
			}
			result.ConnectionEpoch = connector.ConnectionEpoch
		}
		if cluster.ConnectionMode != "in_cluster" {
			if !cluster.CredentialID.Valid {
				return result, ErrQueryInvalid
			}
			credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: cluster.CredentialID.UUID, EnterpriseID: enterpriseID})
			if err != nil || credential.Status != "active" {
				return result, ErrUnavailable
			}
			result.CredentialID, result.CredentialVersion = cluster.CredentialID, credential.Version
		}
	default:
		return result, ErrNotFound
	}
	return result, nil
}

func (service Service) PreviewBinding(ctx context.Context, actor Actor, bindingID, hostID uuid.UUID, expectedVersion int64, idempotencyKey string) (db.PendingAction, error) {
	binding, err := service.Store.Queries.GetKubernetesNodeHostBinding(ctx, db.GetKubernetesNodeHostBindingParams{ID: bindingID, EnterpriseID: actor.EnterpriseID})
	if errors.Is(err, pgx.ErrNoRows) || binding.Version != expectedVersion {
		return db.PendingAction{}, ErrNotFound
	}
	if _, err = service.Store.Queries.GetHost(ctx, db.GetHostParams{ID: hostID, EnterpriseID: actor.EnterpriseID}); err != nil {
		return db.PendingAction{}, ErrNotFound
	}
	if err = service.requireResource(ctx, actor, "kubernetes_cluster", binding.KubernetesClusterID); err != nil {
		return db.PendingAction{}, err
	}
	if err = service.requireResource(ctx, actor, "host", hostID); err != nil {
		return db.PendingAction{}, err
	}
	plan := bindingActionPlan{Operation: "confirm_node_host_binding", BindingID: binding.ID, HostID: hostID,
		ExpectedVersion: expectedVersion, EvidenceHash: hex.EncodeToString(binding.EvidenceHash)}
	return service.Actions.Prepare(ctx, actor.SubjectID.String(), actor.EnterpriseID, resource.PrepareActionInput{
		ActionType: "telemetry.node_host_binding.confirm", Title: "Confirm Kubernetes node binding", Summary: "Bind node " + binding.NodeName + " to an authorized Host",
		Risk: "write", ResourceType: "kubernetes_cluster", ResourceID: uuid.NullUUID{UUID: binding.KubernetesClusterID, Valid: true},
		ExpectedResourceVersion: pgtype.Int8{Int64: expectedVersion, Valid: true}, AuthorizationVersion: actor.AuthorizationVersion,
		Preview: plan, Diff: []map[string]string{{"kind": "change", "text": "Confirm node to Host binding"}}, ImmutablePlan: plan,
		ResourceScopeSnapshot: map[string]any{"cluster_id": binding.KubernetesClusterID, "host_id": hostID, "evidence_hash": hex.EncodeToString(binding.EvidenceHash)},
		CommitHandler:         "argus.telemetry.node_host_binding.confirm.commit",
	}, idempotencyKey)
}

func collectorActionRisk(operation string) string {
	if operation == "uninstall" || operation == "upgrade" {
		return "dangerous"
	}
	return "write"
}

func (service Service) canAccessCollector(ctx context.Context, actor Actor, collector db.CollectorInstance) (bool, error) {
	err := service.requireResource(ctx, actor, collector.ResourceType, collector.ResourceID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrDenied) {
		return false, nil
	}
	return false, err
}

func (service Service) requireResource(ctx context.Context, actor Actor, resourceType string, resourceID uuid.UUID) error {
	switch resourceType {
	case "host":
		_, err := service.Store.Queries.GetHost(ctx, db.GetHostParams{ID: resourceID, EnterpriseID: actor.EnterpriseID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	case "kubernetes_cluster":
		_, err := service.Store.Queries.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: resourceID, EnterpriseID: actor.EnterpriseID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
	default:
		return ErrNotFound
	}
	if !service.Access.CanAccess(actor.AuthorizedResourceIDs, resourceID) {
		return ErrNotFound
	}
	return nil
}

func (service Service) distribution(ctx context.Context, id uuid.UUID) (db.CollectorDistributionVersion, error) {
	items, err := service.Store.Queries.ListCollectorDistributionVersions(ctx)
	if err != nil {
		return db.CollectorDistributionVersion{}, err
	}
	for _, item := range items {
		if item.ID == id {
			return item, nil
		}
	}
	return db.CollectorDistributionVersion{}, ErrNotFound
}

func (service Service) profiles(ctx context.Context, ids []uuid.UUID, schemaVersion, supportStatus, platform string) ([]string, error) {
	if len(ids) == 0 || len(ids) > 32 {
		return nil, ErrQueryInvalid
	}
	items, err := service.Store.Queries.ListCollectionProfiles(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		found := false
		for _, item := range items {
			if item.ID == id && item.SupportStatus == supportStatus && item.ConfigSchemaVersion == schemaVersion && slices.Contains(item.SupportedPlatforms, platform) {
				result = append(result, item.ProfileKey+"@"+item.Version)
				found = true
				break
			}
		}
		if !found {
			return nil, ErrQueryInvalid
		}
	}
	return result, nil
}

// hostCollectorPlatform 按主机 OS + 探测架构解析分发产物平台;
// 未探测到架构的存量主机按 amd64 处理(服务器多数派架构)。
func hostCollectorPlatform(host db.Host) string {
	if host.Platform == "windows" {
		return "windows_amd64"
	}
	if host.Architecture.Valid && host.Architecture.String == "arm64" {
		return "linux_arm64"
	}
	return "linux_amd64"
}

func (service Service) collectorTarget(ctx context.Context, enterpriseID uuid.UUID, resourceType string, resourceID uuid.UUID, routeKind string) (string, string, error) {
	switch resourceType {
	case "host":
		host, err := service.Store.Queries.GetHost(ctx, db.GetHostParams{ID: resourceID, EnterpriseID: enterpriseID})
		if err != nil {
			return "", "", ErrNotFound
		}
		platform, role := hostCollectorPlatform(host), "direct"
		if host.ConnectionMode == "connector_local" {
			role = "edge_gateway"
		} else if routeKind == "bastion_gateway" {
			role = "leaf"
		}
		return platform, role, nil
	case "kubernetes_cluster":
		if _, err := service.Store.Queries.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: resourceID, EnterpriseID: enterpriseID}); err != nil {
			return "", "", ErrNotFound
		}
		return "linux_arm64", "daemonset", nil
	default:
		return "", "", ErrNotFound
	}
}

func distributionSupportsPlatform(raw json.RawMessage, platform string) bool {
	var artifacts []struct {
		Platform string `json:"platform"`
	}
	if json.Unmarshal(raw, &artifacts) != nil {
		return false
	}
	return slices.ContainsFunc(artifacts, func(artifact struct {
		Platform string `json:"platform"`
	}) bool {
		return artifact.Platform == platform
	})
}

func artifactHashes(raw json.RawMessage) ([]string, error) {
	var artifacts []struct {
		Sha256 string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &artifacts); err != nil || len(artifacts) == 0 {
		return nil, ErrQueryInvalid
	}
	result := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		decoded, err := hex.DecodeString(artifact.Sha256)
		if err != nil || len(decoded) != sha256.Size {
			return nil, ErrQueryInvalid
		}
		result = append(result, artifact.Sha256)
	}
	return result, nil
}

func (service Service) Catalog(ctx context.Context) ([]db.CollectorDistributionVersion, []db.CollectionProfile, error) {
	distributions, err := service.Store.Queries.ListCollectorDistributionVersions(ctx)
	if err != nil {
		return nil, nil, err
	}
	profiles, err := service.Store.Queries.ListCollectionProfiles(ctx)
	return distributions, profiles, err
}

func (service Service) ListRoutes(ctx context.Context, actor Actor) ([]db.TelemetryRoute, error) {
	rows, err := service.Store.Queries.ListTelemetryRoutes(ctx, actor.EnterpriseID)
	if err != nil {
		return nil, err
	}
	result := make([]db.TelemetryRoute, 0, len(rows))
	for _, row := range rows {
		if _, err := service.GetCollector(ctx, actor, row.CollectorID); err == nil {
			result = append(result, row)
		} else if !errors.Is(err, ErrNotFound) {
			return nil, err
		}
	}
	return result, nil
}

func (service Service) CreateRouteTest(ctx context.Context, actor Actor, collectorID uuid.UUID, routeKind string, gatewayID uuid.NullUUID) (db.TelemetryRouteTest, error) {
	collector, err := service.GetCollector(ctx, actor, collectorID)
	if err != nil {
		return db.TelemetryRouteTest{}, err
	}
	if routeKind != "direct_argus" && routeKind != "bastion_gateway" {
		return db.TelemetryRouteTest{}, ErrQueryInvalid
	}
	if routeKind == "bastion_gateway" {
		if !gatewayID.Valid {
			return db.TelemetryRouteTest{}, ErrQueryInvalid
		}
		gateway, err := service.GetCollector(ctx, actor, gatewayID.UUID)
		if err != nil || gateway.Role != "edge_gateway" {
			return db.TelemetryRouteTest{}, ErrQueryInvalid
		}
	}
	route, err := service.Store.Queries.GetTelemetryRouteByCollector(ctx, db.GetTelemetryRouteByCollectorParams{CollectorID: collector.ID, EnterpriseID: actor.EnterpriseID})
	if errors.Is(err, pgx.ErrNoRows) || route.Kind != routeKind || route.GatewayCollectorID != gatewayID {
		return db.TelemetryRouteTest{}, ErrNotFound
	}
	if err != nil {
		return db.TelemetryRouteTest{}, err
	}
	now := time.Now().UTC()
	test, err := service.Store.Queries.CreateTelemetryRouteTest(ctx, db.CreateTelemetryRouteTestParams{ID: uuid.Must(uuid.NewV7()), EnterpriseID: actor.EnterpriseID,
		RouteID: route.ID, ExpiresAt: pgtype.Timestamptz{Time: now.Add(10 * time.Minute), Valid: true}})
	if err != nil {
		return db.TelemetryRouteTest{}, err
	}
	status, code := routeTestOutcome(collector.Status, route.Status)
	evidence := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%d", collector.ID, route.ID, collector.EffectiveRevision, route.Version)))
	return service.Store.Queries.CompleteTelemetryRouteTest(ctx, db.CompleteTelemetryRouteTestParams{
		ID: test.ID, EnterpriseID: actor.EnterpriseID, Status: status, ResultCode: code, ResultHash: evidence[:],
	})
}

func routeTestOutcome(collectorStatus, routeStatus string) (string, pgtype.Text) {
	if collectorStatus == "converged" && routeStatus == "active" {
		return "succeeded", pgtype.Text{}
	}
	return "failed", pgtype.Text{String: "TELEMETRY_ROUTE_NOT_CONVERGED", Valid: true}
}

func stableHash(value any) string {
	encoded, _ := json.Marshal(value)
	hash := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", hash[:])
}
