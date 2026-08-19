package httpapi

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	telemetryapi "github.com/kakj-go/Argus/internal/gen/openapi/telemetryapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
)

type TelemetryHandler struct {
	Identity           EnterpriseIdentityHandler
	Service            telemetryservice.Service
	CollectorIdentity  telemetryservice.IdentityService
	IngestGRPCEndpoint string
	IngestHTTPEndpoint string
}

func (handler TelemetryHandler) EnrollTelemetryCollector(ctx context.Context, request telemetryapi.EnrollTelemetryCollectorRequestObject) (telemetryapi.EnrollTelemetryCollectorResponseObject, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok || request.Body == nil || request.Body.CsrPem == nil || metadata.Request.Header.Get("Authorization") != "" || metadata.Request.Header.Get("Cookie") != "" {
		return telemetryapi.EnrollTelemetryCollectordefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrEnrollmentInvalid), StatusCode: http.StatusUnauthorized}, nil
	}
	result, err := handler.CollectorIdentity.Enroll(ctx, request.Params.XArgusTelemetryEnrollmentToken, uuid.UUID(request.Body.CollectorId).String(), *request.Body.CsrPem)
	if err != nil {
		return telemetryapi.EnrollTelemetryCollectordefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	certificate := result.CertificatePEM
	grpcEndpoint, httpEndpoint := handler.IngestGRPCEndpoint, handler.IngestHTTPEndpoint
	return telemetryapi.EnrollTelemetryCollector201JSONResponse{
		CollectorId: openapi_types.UUID(result.CollectorID), CertificatePem: &certificate, CaBundlePem: result.CABundlePEM,
		IngestGrpcEndpoint: &grpcEndpoint, IngestHttpEndpoint: &httpEndpoint, CertificateExpiresAt: result.ExpiresAt,
	}, nil
}

func (handler TelemetryHandler) ListCollectorDistributions(ctx context.Context, _ telemetryapi.ListCollectorDistributionsRequestObject) (telemetryapi.ListCollectorDistributionsResponseObject, error) {
	_, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.ListCollectorDistributionsdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	distributions, _, err := handler.Service.Catalog(ctx)
	if err != nil {
		return telemetryapi.ListCollectorDistributionsdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	result := make([]telemetryapi.CollectorDistributionVersion, 0, len(distributions))
	for _, item := range distributions {
		result = append(result, toCollectorDistribution(item))
	}
	return telemetryapi.ListCollectorDistributions200JSONResponse(result), nil
}

func (handler TelemetryHandler) ListCollectionProfiles(ctx context.Context, _ telemetryapi.ListCollectionProfilesRequestObject) (telemetryapi.ListCollectionProfilesResponseObject, error) {
	_, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.ListCollectionProfilesdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	_, profiles, err := handler.Service.Catalog(ctx)
	if err != nil {
		return telemetryapi.ListCollectionProfilesdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	result := make([]telemetryapi.CollectionProfile, 0, len(profiles))
	for _, item := range profiles {
		result = append(result, toCollectionProfile(item))
	}
	return telemetryapi.ListCollectionProfiles200JSONResponse(result), nil
}

func (handler TelemetryHandler) ListCollectorInstances(ctx context.Context, request telemetryapi.ListCollectorInstancesRequestObject) (telemetryapi.ListCollectorInstancesResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.ListCollectorInstancesdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	resourceType := ""
	if request.Params.ResourceType != nil {
		resourceType = string(*request.Params.ResourceType)
	}
	resourceID := uuid.NullUUID{}
	if request.Params.ResourceId != nil {
		resourceID = uuid.NullUUID{UUID: uuid.UUID(*request.Params.ResourceId), Valid: true}
	}
	limit := int32(50)
	if request.Params.Limit != nil {
		limit = int32(*request.Params.Limit)
	}
	items, err := handler.Service.ListCollectors(ctx, actor, resourceType, resourceID, limit)
	if err != nil {
		return telemetryapi.ListCollectorInstancesdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	result := make([]telemetryapi.CollectorInstance, 0, len(items))
	for _, item := range items {
		result = append(result, toCollectorWithRoute(ctx, handler.Service.Store.Queries, item))
	}
	return telemetryapi.ListCollectorInstances200JSONResponse{Items: result, Page: emptyTelemetryPage()}, nil
}

func (handler TelemetryHandler) GetCollectorInstance(ctx context.Context, request telemetryapi.GetCollectorInstanceRequestObject) (telemetryapi.GetCollectorInstanceResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.GetCollectorInstancedefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	item, err := handler.Service.GetCollector(ctx, actor, uuid.UUID(request.Id))
	if err != nil {
		return telemetryapi.GetCollectorInstancedefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.GetCollectorInstance200JSONResponse(toCollectorWithRoute(ctx, handler.Service.Store.Queries, item)), nil
}

func (handler TelemetryHandler) GetHostCollector(ctx context.Context, request telemetryapi.GetHostCollectorRequestObject) (telemetryapi.GetHostCollectorResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.GetHostCollectordefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	item, err := handler.Service.GetCollectorForResource(ctx, actor, "host", uuid.UUID(request.Id))
	if errors.Is(err, telemetryservice.ErrNotFound) {
		return telemetryapi.GetHostCollector204Response{}, nil
	}
	if err != nil {
		return telemetryapi.GetHostCollectordefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.GetHostCollector200JSONResponse(toCollectorWithRoute(ctx, handler.Service.Store.Queries, item)), nil
}

func (handler TelemetryHandler) GetKubernetesCollector(ctx context.Context, request telemetryapi.GetKubernetesCollectorRequestObject) (telemetryapi.GetKubernetesCollectorResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.GetKubernetesCollectordefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	item, err := handler.Service.GetCollectorForResource(ctx, actor, "kubernetes_cluster", uuid.UUID(request.Id))
	if errors.Is(err, telemetryservice.ErrNotFound) {
		return telemetryapi.GetKubernetesCollector204Response{}, nil
	}
	if err != nil {
		return telemetryapi.GetKubernetesCollectordefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.GetKubernetesCollector200JSONResponse(toCollectorWithRoute(ctx, handler.Service.Store.Queries, item)), nil
}

func (handler TelemetryHandler) PreviewHostCollectorAction(ctx context.Context, request telemetryapi.PreviewHostCollectorActionRequestObject) (telemetryapi.PreviewHostCollectorActionResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "telemetry.collector.manage")
	if apiError != nil {
		return telemetryapi.PreviewHostCollectorActiondefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	action, err := handler.previewCollector(ctx, actor, "host", uuid.UUID(request.Id), request.Action, request.Body, request.Params.IdempotencyKey)
	if err != nil {
		return telemetryapi.PreviewHostCollectorActiondefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.PreviewHostCollectorAction201JSONResponse(convertPending[telemetryapi.PendingActionPublicSchema](action)), nil
}

func (handler TelemetryHandler) PreviewKubernetesCollectorAction(ctx context.Context, request telemetryapi.PreviewKubernetesCollectorActionRequestObject) (telemetryapi.PreviewKubernetesCollectorActionResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "telemetry.collector.manage")
	if apiError != nil {
		return telemetryapi.PreviewKubernetesCollectorActiondefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	action, err := handler.previewCollector(ctx, actor, "kubernetes_cluster", uuid.UUID(request.Id), request.Action, request.Body, request.Params.IdempotencyKey)
	if err != nil {
		return telemetryapi.PreviewKubernetesCollectorActiondefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.PreviewKubernetesCollectorAction201JSONResponse(convertPending[telemetryapi.PendingActionPublicSchema](action)), nil
}

func (handler TelemetryHandler) ListCollectionClaims(ctx context.Context, request telemetryapi.ListCollectionClaimsRequestObject) (telemetryapi.ListCollectionClaimsResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.ListCollectionClaimsdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	resourceID := uuid.NullUUID{}
	if request.Params.ResourceId != nil {
		resourceID = uuid.NullUUID{UUID: uuid.UUID(*request.Params.ResourceId), Valid: true}
	}
	items, err := handler.Service.ListClaims(ctx, actor, resourceID)
	if err != nil {
		return telemetryapi.ListCollectionClaimsdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	result := make([]telemetryapi.CollectionClaim, 0, len(items))
	for _, item := range items {
		result = append(result, toCollectionClaim(item))
	}
	return telemetryapi.ListCollectionClaims200JSONResponse(result), nil
}

func (handler TelemetryHandler) ListKubernetesNodeHostBindings(ctx context.Context, request telemetryapi.ListKubernetesNodeHostBindingsRequestObject) (telemetryapi.ListKubernetesNodeHostBindingsResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.ListKubernetesNodeHostBindingsdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	items, err := handler.Service.ListBindings(ctx, actor, uuid.UUID(request.Params.KubernetesClusterId))
	if err != nil {
		return telemetryapi.ListKubernetesNodeHostBindingsdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	result := make([]telemetryapi.KubernetesNodeHostBinding, 0, len(items))
	for _, item := range items {
		result = append(result, toNodeHostBinding(item))
	}
	return telemetryapi.ListKubernetesNodeHostBindings200JSONResponse(result), nil
}

func (handler TelemetryHandler) PreviewKubernetesNodeHostBinding(ctx context.Context, request telemetryapi.PreviewKubernetesNodeHostBindingRequestObject) (telemetryapi.PreviewKubernetesNodeHostBindingResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "telemetry.collector.manage")
	if apiError != nil {
		return telemetryapi.PreviewKubernetesNodeHostBindingdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	if request.Body == nil {
		return telemetryapi.PreviewKubernetesNodeHostBindingdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Service.PreviewBinding(ctx, actor, uuid.UUID(request.Id), uuid.UUID(request.Body.HostId), request.Body.ExpectedVersion, request.Params.IdempotencyKey)
	if err != nil {
		return telemetryapi.PreviewKubernetesNodeHostBindingdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.PreviewKubernetesNodeHostBinding201JSONResponse(convertPending[telemetryapi.PendingActionPublicSchema](action)), nil
}

func (handler TelemetryHandler) ListTelemetryRoutes(ctx context.Context, _ telemetryapi.ListTelemetryRoutesRequestObject) (telemetryapi.ListTelemetryRoutesResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.collector.read")
	if apiError != nil {
		return telemetryapi.ListTelemetryRoutesdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	items, err := handler.Service.ListRoutes(ctx, actor)
	if err != nil {
		return telemetryapi.ListTelemetryRoutesdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	result := make([]telemetryapi.TelemetryRoute, 0, len(items))
	for _, item := range items {
		result = append(result, toTelemetryRoute(item))
	}
	return telemetryapi.ListTelemetryRoutes200JSONResponse(result), nil
}

func (handler TelemetryHandler) CreateTelemetryRouteTest(ctx context.Context, request telemetryapi.CreateTelemetryRouteTestRequestObject) (telemetryapi.CreateTelemetryRouteTestResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, true, request.Params.XCSRFToken, "telemetry.collector.manage")
	if apiError != nil {
		return telemetryapi.CreateTelemetryRouteTestdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	if request.Body == nil {
		return telemetryapi.CreateTelemetryRouteTestdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	gateway := uuid.NullUUID{}
	if request.Body.GatewayCollectorId != nil {
		gateway = uuid.NullUUID{UUID: uuid.UUID(*request.Body.GatewayCollectorId), Valid: true}
	}
	item, err := handler.Service.CreateRouteTest(ctx, actor, uuid.UUID(request.Body.CollectorId), string(request.Body.RouteKind), gateway)
	if err != nil {
		return telemetryapi.CreateTelemetryRouteTestdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.CreateTelemetryRouteTest202JSONResponse(toRouteTest(item)), nil
}

func (handler TelemetryHandler) GetTelemetryUsage(ctx context.Context, _ telemetryapi.GetTelemetryUsageRequestObject) (telemetryapi.GetTelemetryUsageResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.usage.read")
	if apiError != nil {
		return telemetryapi.GetTelemetryUsagedefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	to := time.Now().UTC()
	from := to.AddDate(0, -1, 0)
	usage, policy, err := handler.Service.Usage(ctx, actor, from, to)
	if err != nil {
		return telemetryapi.GetTelemetryUsagedefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	metrics, logs, traces := int(policy.MetricsDays), int(policy.LogsDays), int(policy.TracesDays)
	return telemetryapi.GetTelemetryUsage200JSONResponse{Body: telemetryapi.TelemetryUsage{PeriodStart: from, PeriodEnd: to, IngestedBytes: usage.IngestedBytes,
		MetricPoints: usage.MetricPoints, LogRecords: usage.LogRecords, Spans: usage.Spans, EstimatedStorageBytes: usage.EstimatedStorageBytes},
		Headers: telemetryapi.GetTelemetryUsage200ResponseHeaders{XRetentionMetricsDays: &metrics, XRetentionLogsDays: &logs, XRetentionTracesDays: &traces}}, nil
}

func (handler TelemetryHandler) QueryTelemetryMetrics(ctx context.Context, request telemetryapi.QueryTelemetryMetricsRequestObject) (telemetryapi.QueryTelemetryMetricsResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "telemetry.query.metrics")
	if apiError != nil {
		return telemetryapi.QueryTelemetryMetricsdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	if request.Body == nil {
		return telemetryapi.QueryTelemetryMetricsdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	limit, step, cursor := optionalInt(request.Body.Limit), optionalInt(request.Body.StepSeconds), optionalString(request.Body.Cursor)
	items, meta, err := handler.Service.QueryMetrics(ctx, actor, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.From, request.Body.To, limit, cursor, request.Body.MetricName, string(request.Body.Aggregation), step, hasPermission(principal, "telemetry.sensitive_fields.read"))
	if err != nil {
		return telemetryapi.QueryTelemetryMetricsdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryTelemetryMetrics200JSONResponse{Series: toMetricSeries(items), Meta: toQueryMeta(meta)}, nil
}

func (handler TelemetryHandler) QueryTelemetryLogs(ctx context.Context, request telemetryapi.QueryTelemetryLogsRequestObject) (telemetryapi.QueryTelemetryLogsResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "telemetry.query.logs")
	if apiError != nil {
		return telemetryapi.QueryTelemetryLogsdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	if request.Body == nil {
		return telemetryapi.QueryTelemetryLogsdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	filter := map[string]any{"service_name": request.Body.ServiceName, "severity": request.Body.Severity, "text": request.Body.Text}
	items, meta, err := handler.Service.QueryLogs(ctx, actor, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.From, request.Body.To, optionalInt(request.Body.Limit), optionalString(request.Body.Cursor), filter, hasPermission(principal, "telemetry.sensitive_fields.read"))
	if err != nil {
		return telemetryapi.QueryTelemetryLogsdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryTelemetryLogs200JSONResponse{Records: toLogRecords(items), Meta: toQueryMeta(meta)}, nil
}

func (handler TelemetryHandler) QueryTelemetryTraces(ctx context.Context, request telemetryapi.QueryTelemetryTracesRequestObject) (telemetryapi.QueryTelemetryTracesResponseObject, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", "telemetry.query.traces")
	if apiError != nil {
		return telemetryapi.QueryTelemetryTracesdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	if request.Body == nil {
		return telemetryapi.QueryTelemetryTracesdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	filter := map[string]any{"service_name": request.Body.ServiceName, "operation": request.Body.Operation, "status": request.Body.Status, "min_duration_ms": request.Body.MinDurationMs}
	items, meta, err := handler.Service.QueryTraces(ctx, actor, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.From, request.Body.To, optionalInt(request.Body.Limit), optionalString(request.Body.Cursor), filter, hasPermission(principal, "telemetry.sensitive_fields.read"))
	if err != nil {
		return telemetryapi.QueryTelemetryTracesdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryTelemetryTraces200JSONResponse{Traces: toTraceSummaries(items), Meta: toQueryMeta(meta)}, nil
}

func (handler TelemetryHandler) QueryTelemetryOverview(ctx context.Context, request telemetryapi.QueryTelemetryOverviewRequestObject) (telemetryapi.QueryTelemetryOverviewResponseObject, error) {
	actor, _, apiError := handler.actor(ctx, false, "", "telemetry.query.metrics")
	if apiError != nil {
		return telemetryapi.QueryTelemetryOverviewdefaultJSONResponse{Body: *apiError, StatusCode: telemetryAuthStatus(*apiError)}, nil
	}
	if request.Body == nil {
		return telemetryapi.QueryTelemetryOverviewdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	item, err := handler.Service.QueryOverview(ctx, actor, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.LookbackSeconds)
	if err != nil {
		return telemetryapi.QueryTelemetryOverviewdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryTelemetryOverview200JSONResponse{ResourceCount: item.ResourceCount, HealthyCollectors: item.HealthyCollectors, DegradedCollectors: item.DegradedCollectors,
		MetricPoints: item.MetricPoints, LogRecords: item.LogRecords, Spans: item.Spans, WindowSeconds: item.WindowSeconds, Partial: item.Partial}, nil
}

func (handler TelemetryHandler) actor(ctx context.Context, mutation bool, csrf, permission string) (telemetryservice.Actor, identity.Principal, *telemetryapi.ApiError) {
	principal, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value != nil {
		converted := telemetryapi.ApiError{Code: value.Code, MessageKey: value.MessageKey, RequestId: value.RequestId, Retryable: value.Retryable}
		return telemetryservice.Actor{}, identity.Principal{}, &converted
	}
	if principal.EnterpriseUser == nil {
		denied := telemetryError(ctx, identity.ErrSessionInvalid)
		return telemetryservice.Actor{}, identity.Principal{}, &denied
	}
	return telemetryservice.Actor{EnterpriseID: principal.EnterpriseUser.EnterpriseID, SubjectID: principal.EnterpriseUser.ID,
		AuthorizationVersion: principal.EnterpriseUser.AuthorizationVersion, DataScopeIDs: slices.Clone(principal.DataScopeIDs)}, principal, nil
}

func (handler TelemetryHandler) previewCollector(ctx context.Context, actor telemetryservice.Actor, resourceType string, resourceID uuid.UUID, action string, body *telemetryapi.CollectorPreview, key string) (db.PendingAction, error) {
	if body == nil {
		return db.PendingAction{}, telemetryservice.ErrQueryInvalid
	}
	gateway := uuid.NullUUID{}
	if body.GatewayCollectorId != nil {
		gateway = uuid.NullUUID{UUID: uuid.UUID(*body.GatewayCollectorId), Valid: true}
	}
	expected := int64(0)
	if body.ExpectedVersion != nil {
		expected = *body.ExpectedVersion
	}
	return handler.Service.PreviewCollectorAction(ctx, actor, resourceType, resourceID, action, telemetryservice.CollectorPreviewInput{
		DistributionVersionID: uuid.UUID(body.DistributionVersionId), ProfileIDs: fromOpenAPIUUIDs(body.ProfileIds), RouteKind: string(body.RouteKind), GatewayCollectorID: gateway, ExpectedVersion: expected}, key)
}

func telemetryStatus(err error) int {
	switch {
	case errors.Is(err, telemetryservice.ErrEnrollmentInvalid), errors.Is(err, telemetryservice.ErrCertificateFenced):
		return http.StatusUnauthorized
	case errors.Is(err, telemetryservice.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, telemetryservice.ErrDenied):
		return http.StatusForbidden
	case errors.Is(err, telemetryservice.ErrDistributionPending), errors.Is(err, telemetryservice.ErrQueryInvalid):
		return http.StatusBadRequest
	case errors.Is(err, telemetryservice.ErrQueryBudget):
		return http.StatusRequestEntityTooLarge
	default:
		return http.StatusServiceUnavailable
	}
}

func telemetryAuthStatus(apiError telemetryapi.ApiError) int {
	switch apiError.Code {
	case "AUTHORIZATION_VERSION_STALE":
		return http.StatusConflict
	case "SESSION_EXPIRED", "SESSION_REVOKED", "SESSION_INVALID":
		return http.StatusUnauthorized
	default:
		return http.StatusForbidden
	}
}

func telemetryError(ctx context.Context, err error) telemetryapi.ApiError {
	base := setupError(ctx, err)
	code := "TELEMETRY_UNAVAILABLE"
	switch {
	case errors.Is(err, telemetryservice.ErrEnrollmentInvalid):
		code = "TELEMETRY_ENROLLMENT_INVALID"
	case errors.Is(err, telemetryservice.ErrCertificateFenced):
		code = "TELEMETRY_CERTIFICATE_FENCED"
	case errors.Is(err, telemetryservice.ErrNotFound):
		code = "COLLECTOR_NOT_FOUND"
	case errors.Is(err, telemetryservice.ErrDistributionPending):
		code = "COLLECTOR_DISTRIBUTION_VALIDATION_PENDING"
	case errors.Is(err, telemetryservice.ErrQueryInvalid):
		code = "TELEMETRY_QUERY_INVALID"
	case errors.Is(err, telemetryservice.ErrQueryBudget):
		code = "TELEMETRY_QUERY_BUDGET_EXCEEDED"
	}
	return telemetryapi.ApiError{Code: code, MessageKey: "errors.telemetry." + code, RequestId: base.RequestId, Retryable: base.Retryable}
}

func emptyTelemetryPage() telemetryapi.CursorPage {
	return telemetryapi.CursorPage{HasMore: false, NextCursor: nil, Partial: telemetryapi.PartialMetadata{Partial: false, Reasons: []telemetryapi.PartialMetadataReasons{}}}
}
func optionalInt(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func toCollectorDistribution(value db.CollectorDistributionVersion) telemetryapi.CollectorDistributionVersion {
	var artifacts []telemetryapi.CollectorArtifact
	_ = json.Unmarshal(value.ArtifactManifest, &artifacts)
	return telemetryapi.CollectorDistributionVersion{Id: value.ID, Name: value.Name, Version: value.Version, CollectorVersion: value.CollectorVersion, ConfigSchemaVersion: value.ConfigSchemaVersion, SupportStatus: telemetryapi.CollectorSupportStatus(value.SupportStatus), Components: value.Components, Artifacts: artifacts, CreatedAt: value.CreatedAt.Time}
}
func toCollectionProfile(value db.CollectionProfile) telemetryapi.CollectionProfile {
	signals := make([]telemetryapi.TelemetrySignal, len(value.Signals))
	for i := range value.Signals {
		signals[i] = telemetryapi.TelemetrySignal(value.Signals[i])
	}
	platforms := make([]telemetryapi.CollectorPlatform, len(value.SupportedPlatforms))
	for i := range value.SupportedPlatforms {
		platforms[i] = telemetryapi.CollectorPlatform(value.SupportedPlatforms[i])
	}
	return telemetryapi.CollectionProfile{Id: value.ID, Key: value.ProfileKey, Version: value.Version, Name: value.Name, Description: value.Description, Signals: signals, RequiredComponents: value.RequiredComponents, SupportedPlatforms: platforms, ClaimTypes: value.ClaimTypes, ConfigSchemaVersion: value.ConfigSchemaVersion, SupportStatus: telemetryapi.CollectorSupportStatus(value.SupportStatus)}
}
func toCollector(value db.CollectorInstance) telemetryapi.CollectorInstance {
	enterprise := openapi_types.UUID(value.EnterpriseID)
	result := telemetryapi.CollectorInstance{Id: value.ID, EnterpriseId: &enterprise, ResourceType: telemetryapi.CollectorInstanceResourceType(value.ResourceType), ResourceId: value.ResourceID, DistributionVersionId: value.DistributionVersionID, Platform: telemetryapi.CollectorPlatform(value.Platform), Role: telemetryapi.CollectorRole(value.Role), Status: telemetryapi.CollectorStatus(value.Status), DesiredRevision: value.DesiredRevision, EffectiveRevision: value.EffectiveRevision, Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.LastSeenAt.Valid {
		result.LastSeenAt = &value.LastSeenAt.Time
	}
	return result
}
func toCollectorWithRoute(ctx context.Context, q *db.Queries, value db.CollectorInstance) telemetryapi.CollectorInstance {
	result := toCollector(value)
	if q == nil {
		return result
	}
	route, err := q.GetTelemetryRouteByCollector(ctx, db.GetTelemetryRouteByCollectorParams{CollectorID: value.ID, EnterpriseID: value.EnterpriseID})
	if err == nil {
		converted := toTelemetryRoute(route)
		result.Route = &converted
	}
	return result
}
func toCollectionClaim(value db.CollectionClaim) telemetryapi.CollectionClaim {
	enterprise := openapi_types.UUID(value.EnterpriseID)
	result := telemetryapi.CollectionClaim{Id: value.ID, EnterpriseId: &enterprise, PhysicalResourceRef: value.PhysicalResourceRef, CollectorId: value.CollectorID, ClaimType: value.ClaimType, Signal: telemetryapi.TelemetrySignal(value.Signal), SelectorHash: hex.EncodeToString(value.SelectorHash), Ownership: telemetryapi.CollectionClaimOwnership(value.Ownership), Status: telemetryapi.CollectionClaimStatus(value.Status), CreatedAt: value.CreatedAt.Time}
	if value.ProfileID.Valid {
		id := openapi_types.UUID(value.ProfileID.UUID)
		result.ProfileId = &id
	}
	if value.ExpiresAt.Valid {
		result.ExpiresAt = &value.ExpiresAt.Time
	}
	return result
}
func toNodeHostBinding(value db.KubernetesNodeHostBinding) telemetryapi.KubernetesNodeHostBinding {
	enterprise := openapi_types.UUID(value.EnterpriseID)
	result := telemetryapi.KubernetesNodeHostBinding{Id: value.ID, EnterpriseId: &enterprise, KubernetesClusterId: value.KubernetesClusterID, NodeUid: value.NodeUid, NodeName: value.NodeName, MatchedBy: telemetryapi.KubernetesNodeHostBindingMatchedBy(value.MatchedBy), EvidenceHash: hex.EncodeToString(value.EvidenceHash), Confidence: int(value.Confidence), Status: telemetryapi.KubernetesNodeHostBindingStatus(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.HostID.Valid {
		id := openapi_types.UUID(value.HostID.UUID)
		result.HostId = &id
	}
	return result
}
func toTelemetryRoute(value db.TelemetryRoute) telemetryapi.TelemetryRoute {
	enterprise := openapi_types.UUID(value.EnterpriseID)
	result := telemetryapi.TelemetryRoute{Id: value.ID, EnterpriseId: &enterprise, CollectorId: value.CollectorID, Kind: telemetryapi.TelemetryRouteKind(value.Kind), Status: telemetryapi.TelemetryRouteStatus(value.Status), Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.GatewayCollectorID.Valid {
		id := openapi_types.UUID(value.GatewayCollectorID.UUID)
		result.GatewayCollectorId = &id
	}
	if value.LastTestedAt.Valid {
		result.LastTestedAt = &value.LastTestedAt.Time
	}
	return result
}
func toRouteTest(value db.TelemetryRouteTest) telemetryapi.RouteTestResult {
	result := telemetryapi.RouteTestResult{Id: value.ID, Status: telemetryapi.RouteTestResultStatus(value.Status), StartedAt: value.CreatedAt.Time, ExpiresAt: value.ExpiresAt.Time}
	if value.ResultCode.Valid {
		result.ErrorCode = &value.ResultCode.String
	}
	if value.CompletedAt.Valid {
		result.CompletedAt = &value.CompletedAt.Time
	}
	return result
}
func toQueryMeta(value telemetryservice.QueryMeta) telemetryapi.TelemetryQueryMeta {
	reasons := make([]telemetryapi.TelemetryQueryMetaPartialReasons, len(value.PartialReasons))
	for i := range value.PartialReasons {
		reasons[i] = telemetryapi.TelemetryQueryMetaPartialReasons(value.PartialReasons[i])
	}
	result := telemetryapi.TelemetryQueryMeta{SchemaVersion: telemetryapi.TelemetryQueryMetaSchemaVersion(value.SchemaVersion), Partial: value.Partial, PartialReasons: reasons, AppliedResourceCount: value.AppliedResourceCount, ScannedBytes: value.ScannedBytes, ElapsedMs: value.ElapsedMS}
	if value.NextCursor != "" {
		result.NextCursor = &value.NextCursor
	}
	return result
}
func toMetricSeries(values []telemetryservice.MetricSeries) []telemetryapi.MetricSeries {
	result := make([]telemetryapi.MetricSeries, 0, len(values))
	for _, value := range values {
		unit := value.Unit
		points := make([]telemetryapi.MetricPoint, len(value.Points))
		for i := range value.Points {
			points[i] = telemetryapi.MetricPoint{Timestamp: value.Points[i].Timestamp, Value: float32(value.Points[i].Value)}
		}
		item := telemetryapi.MetricSeries{ResourceId: value.ResourceID, MetricName: value.MetricName, Points: points}
		if unit != "" {
			item.Unit = &unit
		}
		result = append(result, item)
	}
	return result
}
func toLogRecords(values []telemetryservice.LogRecord) []telemetryapi.LogRecord {
	result := make([]telemetryapi.LogRecord, 0, len(values))
	for _, value := range values {
		item := telemetryapi.LogRecord{Timestamp: value.Timestamp, ResourceId: value.ResourceID, Severity: value.Severity, Body: value.Body}
		if value.ServiceName != "" {
			item.ServiceName = &value.ServiceName
		}
		if value.TraceID != "" {
			item.TraceId = &value.TraceID
		}
		result = append(result, item)
	}
	return result
}
func toTraceSummaries(values []telemetryservice.TraceSummary) []telemetryapi.TraceSummary {
	result := make([]telemetryapi.TraceSummary, 0, len(values))
	for _, value := range values {
		result = append(result, telemetryapi.TraceSummary{TraceId: value.TraceID, ResourceId: value.ResourceID, ServiceName: value.ServiceName, RootSpanName: value.RootSpanName, Status: telemetryapi.TraceSummaryStatus(value.Status), StartedAt: value.StartedAt, DurationMs: float32(value.DurationMS), SpanCount: value.SpanCount})
	}
	return result
}
