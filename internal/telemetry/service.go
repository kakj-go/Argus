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
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
)

var (
	ErrUnavailable                  = errors.New("telemetry service unavailable")
	ErrNotFound                     = errors.New("telemetry resource not found")
	ErrDenied                       = errors.New("telemetry resource is not explicitly authorized")
	ErrDistributionPending          = errors.New("collector distribution validation pending")
	ErrCollectorArtifactUnavailable = errors.New("collector artifact is unavailable")
	ErrQueryInvalid                 = errors.New("telemetry query invalid")
	// ErrRouteTransportInvalid: 推送矩阵外的 transport/kind/路径组合。
	ErrRouteTransportInvalid = errors.New("COLLECTOR_ROUTE_TRANSPORT_INVALID")
	// ErrTunnelQuotaExceeded: 企业活跃反向隧道数达到上限。
	ErrTunnelQuotaExceeded = errors.New("TUNNEL_QUOTA_EXCEEDED")
	// ErrSelfEnrolledOperationUnsupported: self_enrolled 主机没有入站执行路径,
	// configure/upgrade/repair 必须以新的自助安装命令收敛。
	ErrSelfEnrolledOperationUnsupported = errors.New("HOST_OPERATION_UNSUPPORTED_FOR_SELF_ENROLLED")
	// ErrHostInstallToken: 自助安装令牌缺失/过期/撤销。
	ErrHostInstallTokenInvalid = errors.New("HOST_INSTALL_TOKEN_INVALID")
	// ErrHostInstallTokenConflict: 令牌已被其他设备消费。
	ErrHostInstallTokenConflict   = errors.New("HOST_INSTALL_TOKEN_CONFLICT")
	ErrHostUninstallTokenInvalid  = errors.New("HOST_UNINSTALL_TOKEN_INVALID")
	ErrHostUninstallTokenConflict = errors.New("HOST_UNINSTALL_TOKEN_CONFLICT")
	ErrQueryBudget                = errors.New("telemetry query budget exceeded")
	ErrQueryParse                 = errors.New("telemetry query parse error")
	ErrQueryType                  = errors.New("telemetry query type error")
	ErrQueryUnsupported           = errors.New("telemetry query feature unsupported")
	ErrQueryComplexity            = errors.New("telemetry query complexity limit")
	ErrQueryScope                 = errors.New("telemetry query scope denied")
)

type Actor struct {
	EnterpriseID          uuid.UUID
	SubjectID             uuid.UUID
	AuthorizationVersion  int64
	AuthorizedResourceIDs []uuid.UUID
}

type Service struct {
	Store  *postgres.Store
	Access resource.AccessService
	// TunnelQuota 是企业级活跃反向隧道上限(PlanV4);0 时使用默认值。
	TunnelQuota            int64
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
	// Transport 是遥测物理路径(PlanV4);调用方必须显式选择。场景②主机无出站时
	// 选择 executor_tunnel,堡垒机成员连不上网关 OTLP 端口时选择 bastion_tunnel。
	Transport          string
	LoopbackPort       int32
	GatewayCollectorID uuid.NullUUID
	ExpectedVersion    int64
	// KubernetesImage 为空时使用服务端配置的默认镜像;仅 kubernetes_cluster 资源允许提供。
	KubernetesImage  string
	ImagePullSecrets []string
}

type collectorActionPlan struct {
	Operation             string      `json:"operation"`
	ResourceType          string      `json:"resource_type"`
	ResourceID            uuid.UUID   `json:"resource_id"`
	DistributionVersionID uuid.UUID   `json:"distribution_version_id"`
	ProfileIDs            []uuid.UUID `json:"profile_ids"`
	ProfileKeys           []string    `json:"profile_keys"`
	RouteKind             string      `json:"route_kind"`
	// RouteTransport 是遥测数据的物理路径(direct/executor_tunnel/bastion_tunnel),
	// 与 Transport(由谁执行安装命令)正交;PlanV4 场景②③使用隧道。
	RouteTransport        string        `json:"route_transport,omitempty"`
	LoopbackPort          int32         `json:"loopback_port,omitempty"`
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
	// Tunnel* freezes the independent telemetry transport target. These fields
	// intentionally do not reuse Target*: a mode-C bastion Collector is managed
	// locally by its Connector while Direct Executor owns a separate SSH tunnel
	// to the same host.
	TunnelInitiator         string        `json:"tunnel_initiator,omitempty"`
	TunnelConnectorID       uuid.NullUUID `json:"tunnel_connector_id,omitempty"`
	TunnelTargetAddress     string        `json:"tunnel_target_address,omitempty"`
	TunnelTargetPort        int32         `json:"tunnel_target_port,omitempty"`
	TunnelTargetUsername    string        `json:"tunnel_target_username,omitempty"`
	TunnelPinnedHostKey     string        `json:"tunnel_pinned_host_key,omitempty"`
	TunnelCredentialID      uuid.NullUUID `json:"tunnel_credential_id,omitempty"`
	TunnelCredentialVersion int64         `json:"tunnel_credential_version,omitempty"`
	EnrollmentDialAddress   string        `json:"enrollment_dial_address,omitempty"`
	KubernetesImage         string        `json:"kubernetes_image,omitempty"`
	ImagePullSecrets        []string      `json:"image_pull_secrets,omitempty"`
}

// telemetryTransport 返回遥测路由的 transport 与回环端口。空 transport
// 保持为空，由计划重校验按非法路由拒绝，不做兼容回退。
func (plan collectorActionPlan) telemetryTransport() (string, pgtype.Int4) {
	transport := plan.RouteTransport
	if transport == "direct" || plan.LoopbackPort == 0 {
		return transport, pgtype.Int4{}
	}
	return transport, pgtype.Int4{Int32: plan.LoopbackPort, Valid: true}
}

type collectorExecutionTarget struct {
	Transport, Address, Username, PinnedHostKey string
	Port, ResourceVersion                       int64
	ConnectorID, CredentialID                   uuid.NullUUID
	ConnectionEpoch, CredentialVersion          int64
}

type collectorTunnelTarget struct {
	Initiator, Address, Username, PinnedHostKey, EnrollmentDialAddress string
	Port, CredentialVersion                                            int64
	ConnectorID, CredentialID                                          uuid.NullUUID
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
	// self_enrolled 主机没有入站执行路径:仅允许首装与卸载(均生成一次性自助命令),
	// 配置变更/升级/修复必须重新生成命令在目标侧收敛;路由固定直推。
	routeTransport := input.Transport
	if !slices.Contains([]string{"direct", "executor_tunnel", "bastion_tunnel"}, routeTransport) {
		return db.PendingAction{}, ErrRouteTransportInvalid
	}
	if routeTransport != "direct" && operation != "uninstall" {
		if err := service.checkTunnelQuota(ctx, actor.EnterpriseID); err != nil {
			return db.PendingAction{}, err
		}
	}
	loopbackPort := input.LoopbackPort
	if loopbackPort == 0 {
		loopbackPort = defaultTunnelLoopbackPort
	}
	if target.Transport == "bootstrap" {
		if operation != "install" && operation != "uninstall" {
			return db.PendingAction{}, ErrSelfEnrolledOperationUnsupported
		}
		if input.RouteKind != "direct_argus" {
			return db.PendingAction{}, ErrQueryInvalid
		}
	}
	tunnelTarget, err := resolveCollectorTunnelTarget(ctx, service.Store.Queries, actor.EnterpriseID,
		resourceType, resourceID, routeTransport, input.RouteKind, int(loopbackPort), target)
	if err != nil {
		return db.PendingAction{}, err
	}
	artifactHashes, err := artifactHashes(distribution.ArtifactManifest)
	if err != nil {
		return db.PendingAction{}, ErrQueryInvalid
	}
	plan := collectorActionPlan{Operation: operation, ResourceType: resourceType, ResourceID: resourceID,
		DistributionVersionID: input.DistributionVersionID, ProfileIDs: slices.Clone(input.ProfileIDs), RouteKind: input.RouteKind,
		RouteTransport: routeTransport, LoopbackPort: loopbackPort,
		GatewayCollectorID: input.GatewayCollectorID, ExpectedVersion: input.ExpectedVersion, ArtifactHashes: artifactHashes,
		Platform: platform, Role: role, ProfileKeys: slices.Clone(profiles)}
	if input.RouteKind == "bastion_gateway" {
		plan.GatewayEndpoint, plan.GatewayServerName, err = telemetryGatewayEndpoint(ctx, service.Store.Queries, actor.EnterpriseID, input.GatewayCollectorID, routeTransport)
		if err != nil {
			return db.PendingAction{}, err
		}
	}
	plan.Transport, plan.ConnectorID, plan.ConnectionEpoch = target.Transport, target.ConnectorID, target.ConnectionEpoch
	plan.TargetResourceVersion, plan.TargetAddress, plan.TargetPort = target.ResourceVersion, target.Address, int32(target.Port)
	plan.TargetUsername, plan.PinnedHostKey = target.Username, target.PinnedHostKey
	plan.CredentialID, plan.CredentialVersion = target.CredentialID, target.CredentialVersion
	plan.TunnelInitiator, plan.TunnelConnectorID = tunnelTarget.Initiator, tunnelTarget.ConnectorID
	plan.TunnelTargetAddress, plan.TunnelTargetPort = tunnelTarget.Address, int32(tunnelTarget.Port)
	plan.TunnelTargetUsername, plan.TunnelPinnedHostKey = tunnelTarget.Username, tunnelTarget.PinnedHostKey
	plan.TunnelCredentialID, plan.TunnelCredentialVersion = tunnelTarget.CredentialID, tunnelTarget.CredentialVersion
	plan.EnrollmentDialAddress = tunnelTarget.EnrollmentDialAddress
	if image, imageErr := collectorPreviewImage(resourceType, input.KubernetesImage, service.OtelcolKubernetesImage); imageErr != nil {
		return db.PendingAction{}, imageErr
	} else if resourceType == "kubernetes_cluster" {
		plan.KubernetesImage = image
	}
	imagePullSecrets, imagePullErr := normalizeImagePullSecrets(resourceType, input.ImagePullSecrets)
	if imagePullErr != nil {
		return db.PendingAction{}, imagePullErr
	}
	plan.ImagePullSecrets = imagePullSecrets
	preview := map[string]any{"operation": operation, "resource_type": resourceType, "resource_id": resourceID,
		"distribution": distribution.Version, "profiles": profiles, "route_kind": input.RouteKind}
	if plan.KubernetesImage != "" {
		preview["kubernetes_image"] = plan.KubernetesImage
	}
	if len(plan.ImagePullSecrets) != 0 {
		preview["image_pull_secrets"] = plan.ImagePullSecrets
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

// defaultTunnelQuota 是企业级活跃反向隧道默认上限;超限时提示改用堡垒机网关。
const defaultTunnelQuota = 256

// defaultTunnelLoopbackPort 是隧道形态 Collector 出口的默认回环端口;
// Preview 探测到冲突时可固化替代值。
const defaultTunnelLoopbackPort = 4317

// tunnelIdentityLoopbackPort reserves the port immediately after the OTLP
// gRPC listener for Collector enrollment and certificate rotation. Both
// identity endpoints terminate on telemetry-ingest HTTP and therefore share
// one TLS-preserving TCP forward.
func tunnelIdentityLoopbackPort(loopbackPort int) (int, error) {
	if loopbackPort < 1 || loopbackPort >= 65535 {
		return 0, ErrRouteTransportInvalid
	}
	return loopbackPort + 1, nil
}

// validateRouteTransport 执行推送矩阵(PlanV4):transport 与 kind、资源形态、
// 执行路径的组合约束。
func (service Service) checkTunnelQuota(ctx context.Context, enterpriseID uuid.UUID) error {
	limit := service.TunnelQuota
	if limit <= 0 {
		limit = defaultTunnelQuota
	}
	count, err := service.Store.Queries.CountActiveTunnelsByEnterprise(ctx, enterpriseID)
	if err != nil {
		return err
	}
	if count >= limit {
		return ErrTunnelQuotaExceeded
	}
	return nil
}

func validateRouteTransport(transport, routeKind, resourceType, execution string, tunnelTarget collectorTunnelTarget) error {
	switch transport {
	case "direct":
		return nil
	case "executor_tunnel":
		// 独立主机由 Direct Executor 直接执行；模式 C 堡垒机由 Connector
		// 本地执行，但遥测仍由独立的 Direct Executor tunnel 承载。
		if resourceType != "host" || routeKind != "direct_argus" ||
			(execution != "direct" && execution != "connector") || tunnelTarget.Initiator != "direct_executor" {
			return ErrRouteTransportInvalid
		}
		return nil
	case "bastion_tunnel":
		// 仅堡垒机成员(非堡垒机本机)连不上网关 OTLP 端口时使用。
		if resourceType != "host" || routeKind != "bastion_gateway" || execution != "connector" ||
			tunnelTarget.Initiator != "connector" || !tunnelTarget.ConnectorID.Valid {
			return ErrRouteTransportInvalid
		}
		return nil
	default:
		return ErrRouteTransportInvalid
	}
}

func resolveCollectorTunnelTarget(
	ctx context.Context,
	q *db.Queries,
	enterpriseID uuid.UUID,
	resourceType string,
	resourceID uuid.UUID,
	transport string,
	routeKind string,
	loopbackPort int,
	execution collectorExecutionTarget,
) (collectorTunnelTarget, error) {
	var result collectorTunnelTarget
	if transport == "direct" {
		if err := validateRouteTransport(transport, routeKind, resourceType, execution.Transport, result); err != nil {
			return result, err
		}
		return result, nil
	}
	if resourceType != "host" {
		return result, ErrRouteTransportInvalid
	}
	host, err := q.GetHost(ctx, db.GetHostParams{ID: resourceID, EnterpriseID: enterpriseID})
	if err != nil {
		return result, ErrRouteTransportInvalid
	}
	copyExecutionTarget := func(initiator string, connectorID uuid.NullUUID) {
		result = collectorTunnelTarget{Initiator: initiator, ConnectorID: connectorID,
			Address: execution.Address, Port: execution.Port, Username: execution.Username,
			PinnedHostKey: execution.PinnedHostKey, CredentialID: execution.CredentialID,
			CredentialVersion: execution.CredentialVersion}
	}
	switch transport {
	case "executor_tunnel":
		switch host.ConnectionMode {
		case "direct_ssh":
			copyExecutionTarget("direct_executor", uuid.NullUUID{})
		case "connector_local":
			if !host.BastionScopeID.Valid || !execution.ConnectorID.Valid {
				return result, ErrRouteTransportInvalid
			}
			scope, scopeErr := q.GetBastionScope(ctx, db.GetBastionScopeParams{
				ID: host.BastionScopeID.UUID, EnterpriseID: enterpriseID})
			if scopeErr != nil || scope.OnboardingMode != "direct_install_tunnel" ||
				!scope.ActiveConnectorID.Valid || scope.ActiveConnectorID.UUID != execution.ConnectorID.UUID {
				return result, ErrRouteTransportInvalid
			}
			control, controlErr := q.GetConnectorControlTunnelByConnector(ctx, db.GetConnectorControlTunnelByConnectorParams{
				ConnectorID: execution.ConnectorID.UUID, EnterpriseID: enterpriseID})
			if controlErr != nil || control.BastionScopeID != scope.ID || control.HostID != host.ID ||
				control.Status != "established" {
				return result, ErrUnavailable
			}
			result = collectorTunnelTarget{Initiator: "direct_executor", Address: control.TargetAddress,
				Port: int64(control.TargetPort), Username: control.TargetUsername, PinnedHostKey: control.PinnedHostKey,
				CredentialID: uuid.NullUUID{UUID: control.CredentialID, Valid: true}, CredentialVersion: control.CredentialVersion}
		default:
			return result, ErrRouteTransportInvalid
		}
	case "bastion_tunnel":
		if host.ConnectionMode != "via_bastion" {
			return result, ErrRouteTransportInvalid
		}
		copyExecutionTarget("connector", execution.ConnectorID)
	default:
		return result, ErrRouteTransportInvalid
	}
	if result.Address == "" || result.Port < 1 || result.Username == "" || result.PinnedHostKey == "" ||
		!result.CredentialID.Valid || result.CredentialVersion < 1 {
		return collectorTunnelTarget{}, ErrRouteTransportInvalid
	}
	if err := validateRouteTransport(transport, routeKind, resourceType, execution.Transport, result); err != nil {
		return collectorTunnelTarget{}, err
	}
	identityPort, err := tunnelIdentityLoopbackPort(loopbackPort)
	if err != nil {
		return collectorTunnelTarget{}, err
	}
	result.EnrollmentDialAddress = net.JoinHostPort("127.0.0.1", fmt.Sprint(identityPort))
	return result, nil
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

func normalizeImagePullSecrets(resourceType string, values []string) ([]string, error) {
	if resourceType != "kubernetes_cluster" {
		if len(values) != 0 {
			return nil, ErrQueryInvalid
		}
		return nil, nil
	}
	if len(values) > 16 {
		return nil, ErrQueryInvalid
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value != strings.TrimSpace(value) || len(validation.IsDNS1123Subdomain(value)) != 0 {
			return nil, ErrQueryInvalid
		}
		if _, exists := seen[value]; exists {
			return nil, ErrQueryInvalid
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result, nil
}

func telemetryGatewayEndpoint(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, gatewayID uuid.NullUUID, transport string) (string, string, error) {
	if !gatewayID.Valid {
		return "", "", ErrQueryInvalid
	}
	gateway, err := q.GetCollectorInstance(ctx, db.GetCollectorInstanceParams{ID: gatewayID.UUID, EnterpriseID: enterpriseID})
	if err != nil || gateway.ResourceType != "host" || gateway.Role != "edge_gateway" || gateway.Status == "uninstalled" {
		return "", "", ErrQueryInvalid
	}
	host, err := q.GetHost(ctx, db.GetHostParams{ID: gateway.ResourceID, EnterpriseID: enterpriseID})
	if err != nil || host.ConnectionMode != "connector_local" {
		return "", "", ErrQueryInvalid
	}
	serverName := CollectorCertificateDNSName(gateway.ID)
	if transport == "bastion_tunnel" {
		// 隧道在 Connector 所在堡垒机本地转发到 Gateway listener；Host 的
		// connector:// 地址是资源定位符，不参与数据面拨号。
		return "", serverName, nil
	}
	address := strings.TrimSpace(host.Address)
	if transport != "direct" || address == "" || strings.Contains(address, "://") || strings.ContainsAny(address, "/?#@ \t\r\n") {
		return "", "", ErrRouteTransportInvalid
	}
	return "grpcs://" + net.JoinHostPort(address, "4317"), serverName, nil
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
		case "self_enrolled":
			// 只出不进主机:无入站执行路径,安装经 bootstrap 一次性命令在目标侧自装。
			result.Transport = "bootstrap"
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
		if host.ConnectionMode != "connector_local" && host.ConnectionMode != "self_enrolled" {
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
		Preview: map[string]any{"binding_id": binding.ID, "host_id": hostID, "node_name": binding.NodeName},
		Diff:    []map[string]string{{"kind": "change", "text": "Confirm node to Host binding"}}, ImmutablePlan: plan,
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

// hostCollectorPlatform 按主机 OS + 探测架构解析分发产物平台。Linux
// 架构缺失或不受支持时 fail closed，禁止静默选择错误产物。
func hostCollectorPlatform(host db.Host) (string, error) {
	if host.Platform == "windows" {
		return "windows_amd64", nil
	}
	if host.Platform != "linux" || !host.Architecture.Valid {
		return "", ErrDistributionPending
	}
	switch host.Architecture.String {
	case "amd64":
		return "linux_amd64", nil
	case "arm64":
		return "linux_arm64", nil
	default:
		return "", ErrDistributionPending
	}
}

func (service Service) collectorTarget(ctx context.Context, enterpriseID uuid.UUID, resourceType string, resourceID uuid.UUID, routeKind string) (string, string, error) {
	switch resourceType {
	case "host":
		host, err := service.Store.Queries.GetHost(ctx, db.GetHostParams{ID: resourceID, EnterpriseID: enterpriseID})
		if err != nil {
			return "", "", ErrNotFound
		}
		platform, platformErr := hostCollectorPlatform(host)
		if platformErr != nil {
			return "", "", platformErr
		}
		role := "direct"
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

func (service Service) CreateRouteTest(ctx context.Context, actor Actor, collectorID uuid.UUID, routeKind, transport string, gatewayID uuid.NullUUID) (db.TelemetryRouteTest, error) {
	collector, err := service.GetCollector(ctx, actor, collectorID)
	if err != nil {
		return db.TelemetryRouteTest{}, err
	}
	if routeKind != "direct_argus" && routeKind != "bastion_gateway" {
		return db.TelemetryRouteTest{}, ErrQueryInvalid
	}
	if transport != "direct" && transport != "executor_tunnel" && transport != "bastion_tunnel" {
		return db.TelemetryRouteTest{}, ErrRouteTransportInvalid
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
	if errors.Is(err, pgx.ErrNoRows) || route.Kind != routeKind || route.Transport != transport || route.GatewayCollectorID != gatewayID {
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
