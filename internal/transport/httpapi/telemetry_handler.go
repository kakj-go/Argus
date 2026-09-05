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
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
	"github.com/kakj-go/Argus/internal/telemetry/queryengine"
	"github.com/kakj-go/Argus/internal/trustbundle"
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
	if !ok || request.Body == nil || request.Body.ClientCsrPem == nil || request.Body.ServerCsrPem == nil || metadata.Request.Header.Get("Authorization") != "" || metadata.Request.Header.Get("Cookie") != "" {
		return telemetryapi.EnrollTelemetryCollectordefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrEnrollmentInvalid), StatusCode: http.StatusUnauthorized}, nil
	}
	result, err := handler.CollectorIdentity.Enroll(ctx, request.Params.XArgusTelemetryEnrollmentToken, uuid.UUID(request.Body.CollectorId).String(),
		*request.Body.ClientCsrPem, *request.Body.ServerCsrPem)
	if err != nil {
		return telemetryapi.EnrollTelemetryCollectordefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	clientCertificate := result.ClientCertificatePEM
	serverCertificate := result.ServerCertificatePEM
	grpcEndpoint, httpEndpoint := handler.IngestGRPCEndpoint, handler.IngestHTTPEndpoint
	return telemetryapi.EnrollTelemetryCollector201JSONResponse{
		CollectorId: openapi_types.UUID(result.CollectorID), ClientCertificatePem: &clientCertificate,
		ServerCertificatePem: &serverCertificate, TrustBundle: toTelemetryTrustBundle(result.TrustBundle),
		IngestGrpcEndpoint: &grpcEndpoint, IngestHttpEndpoint: &httpEndpoint, CertificateExpiresAt: result.ExpiresAt,
	}, nil
}

func toTelemetryTrustBundle(value trustbundle.Bundle) telemetryapi.TrustBundleSnapshot {
	result := telemetryapi.TrustBundleSnapshot{Epoch: value.Epoch, State: telemetryapi.TrustBundleSnapshotState(value.State),
		BundlePem: string(value.Material.PEM), BundleSha256: value.Material.SHA256,
		CurrentCaFingerprints: value.CurrentCAFingerprints, NextCaFingerprints: value.NextCAFingerprints, StartedAt: value.StartedAt}
	if !value.RetireAt.IsZero() {
		result.RetireAt = &value.RetireAt
	}
	return result
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
		result = append(result, toCollectorDistribution(item, handler.Service.OtelcolKubernetesImage))
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
	item, err := handler.Service.CreateRouteTest(ctx, actor, uuid.UUID(request.Body.CollectorId), string(request.Body.RouteKind), string(request.Body.Transport), gateway)
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

func (handler TelemetryHandler) QueryMetricsInstant(ctx context.Context, request telemetryapi.QueryMetricsInstantRequestObject) (telemetryapi.QueryMetricsInstantResponseObject, error) {
	if request.Body == nil {
		return telemetryapi.QueryMetricsInstantdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	result, err := handler.executeEngine(ctx, "telemetry.query.metrics", queryengine.LanguagePromQL, request.Body.Query, "", "", nil, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.TimeRange.From, request.Body.TimeRange.To, 0, request.Body.Budget, true, false)
	if err != nil {
		return telemetryapi.QueryMetricsInstantdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryMetricsInstant200JSONResponse(prometheusQueryResponse(result)), nil
}

func (handler TelemetryHandler) QueryMetricsRange(ctx context.Context, request telemetryapi.QueryMetricsRangeRequestObject) (telemetryapi.QueryMetricsRangeResponseObject, error) {
	if request.Body == nil {
		return telemetryapi.QueryMetricsRangedefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	result, err := handler.executeEngine(ctx, "telemetry.query.metrics", queryengine.LanguagePromQL, request.Body.Query, "", "", nil, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.TimeRange.From, request.Body.TimeRange.To, time.Duration(request.Body.StepSeconds)*time.Second, request.Body.Budget, false, false)
	if err != nil {
		return telemetryapi.QueryMetricsRangedefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryMetricsRange200JSONResponse(prometheusQueryResponse(result)), nil
}

func (handler TelemetryHandler) QueryLogsKQL(ctx context.Context, request telemetryapi.QueryLogsKQLRequestObject) (telemetryapi.QueryLogsKQLResponseObject, error) {
	if request.Body == nil {
		return telemetryapi.QueryLogsKQLdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	pipeline := ""
	if request.Body.Pipeline != nil {
		pipeline = *request.Body.Pipeline
	}
	result, err := handler.executeEngine(ctx, "telemetry.query.logs", queryengine.LanguageKQL, request.Body.Query, pipeline, "", nil, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.TimeRange.From, request.Body.TimeRange.To, 0, request.Body.Budget, false, true)
	if err != nil {
		return telemetryapi.QueryLogsKQLdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryLogsKQL200JSONResponse(kqlQueryResponse(result)), nil
}

func (handler TelemetryHandler) QueryTracesGraphQL(ctx context.Context, request telemetryapi.QueryTracesGraphQLRequestObject) (telemetryapi.QueryTracesGraphQLResponseObject, error) {
	if request.Body == nil {
		return telemetryapi.QueryTracesGraphQLdefaultJSONResponse{Body: telemetryError(ctx, telemetryservice.ErrQueryInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	operation, variables := "", map[string]any(nil)
	if request.Body.OperationName != nil {
		operation = *request.Body.OperationName
	}
	if request.Body.Variables != nil {
		variables = *request.Body.Variables
	}
	result, err := handler.executeEngine(ctx, "telemetry.query.traces", queryengine.LanguageTrace, request.Body.Query, "", operation, variables, fromOpenAPIUUIDs(request.Body.ResourceIds), request.Body.TimeRange.From, request.Body.TimeRange.To, 0, request.Body.Budget, false, true)
	if err != nil {
		return telemetryapi.QueryTracesGraphQLdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	response, err := skyWalkingGraphQLResponse(result)
	if err != nil {
		return telemetryapi.QueryTracesGraphQLdefaultJSONResponse{Body: telemetryError(ctx, err), StatusCode: telemetryStatus(err)}, nil
	}
	return telemetryapi.QueryTracesGraphQL200JSONResponse(response), nil
}

func (handler TelemetryHandler) executeEngine(ctx context.Context, permission string, language queryengine.Language, expression, pipeline, operation string, variables map[string]any, resources []uuid.UUID, from, to time.Time, step time.Duration, budget *telemetryapi.TelemetryQueryBudget, instant, sensitive bool) (queryengine.Result, error) {
	actor, principal, apiError := handler.actor(ctx, false, "", permission)
	if apiError != nil {
		return queryengine.Result{}, telemetryRequestError{apiError: *apiError}
	}
	if sensitive {
		sensitive = hasPermission(principal, "telemetry.sensitive_fields.read")
	}
	resources, partial, err := handler.Service.AuthorizedResources(ctx, actor, resources)
	if err != nil {
		return queryengine.Result{}, err
	}
	if partial {
		return queryengine.Result{}, telemetryservice.ErrQueryScope
	}
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return queryengine.Result{}, telemetryservice.ErrQueryInvalid
	}
	if handler.Service.Engine == nil {
		return queryengine.Result{}, telemetryservice.ErrUnavailable
	}
	result, err := handler.Service.Engine.ExecuteEngineQuery(ctx, queryengine.Request{Language: language, Expression: expression, Pipeline: pipeline, Operation: operation, Variables: variables, Instant: instant, Start: from, End: to, Step: step, Scope: queryengine.Scope{EnterpriseID: actor.EnterpriseID, ResourceIDs: resources, AuthorizationVersion: actor.AuthorizationVersion, SensitiveFields: sensitive}, Budget: telemetryEngineBudget(budget)})
	if err != nil {
		return queryengine.Result{}, err
	}
	return result, nil
}

func telemetryEngineBudget(input *telemetryapi.TelemetryQueryBudget) queryengine.Budget {
	budget := queryengine.Budget{MaxRows: telemetryservice.DefaultMaxRows, MaxSamples: telemetryservice.DefaultMaxSamples, MaxSeries: telemetryservice.DefaultMaxSeries, MaxScanBytes: telemetryservice.DefaultMaxScanBytes, MaxResultBytes: 8 << 20, Timeout: telemetryservice.DefaultTimeout}
	if input == nil {
		return budget
	}
	if input.MaxRows != nil {
		budget.MaxRows = *input.MaxRows
	}
	if input.MaxSamples != nil {
		budget.MaxSamples = *input.MaxSamples
	}
	if input.MaxSeries != nil {
		budget.MaxSeries = *input.MaxSeries
	}
	if input.MaxScanBytes != nil {
		budget.MaxScanBytes = *input.MaxScanBytes
	}
	if input.MaxResultBytes != nil {
		budget.MaxResultBytes = *input.MaxResultBytes
	}
	if input.TimeoutMs != nil {
		budget.Timeout = time.Duration(*input.TimeoutMs) * time.Millisecond
	}
	return budget
}

func prometheusQueryResponse(result queryengine.Result) telemetryapi.PrometheusQueryResponse {
	return telemetryapi.PrometheusQueryResponse{
		Status: telemetryapi.Success,
		Data: telemetryapi.PrometheusQueryData{
			ResultType: telemetryapi.PrometheusQueryDataResultType(result.ResultType),
			Result:     result.Data,
		},
		Warnings:  queryWarnings(result.Meta.Warnings),
		ArgusMeta: telemetryQueryMeta(result.Meta),
	}
}

func kqlQueryResponse(result queryengine.Result) telemetryapi.KQLQueryResponse {
	return telemetryapi.KQLQueryResponse{
		SchemaVersion: telemetryapi.ArgusKqlResultv1,
		ResultType:    telemetryapi.KQLQueryResponseResultType(result.ResultType),
		Data:          result.Data,
		Warnings:      queryWarnings(result.Meta.Warnings),
		Partial:       result.Meta.Partial,
		Meta:          telemetryQueryMeta(result.Meta),
	}
}

func skyWalkingGraphQLResponse(result queryengine.Result) (telemetryapi.SkyWalkingGraphQLResponse, error) {
	data, ok := result.Data.(map[string]any)
	if !ok {
		encoded, err := json.Marshal(result.Data)
		if err != nil {
			return telemetryapi.SkyWalkingGraphQLResponse{}, err
		}
		if err := json.Unmarshal(encoded, &data); err != nil {
			return telemetryapi.SkyWalkingGraphQLResponse{}, err
		}
	}
	response := telemetryapi.SkyWalkingGraphQLResponse{Data: data}
	response.Extensions.Argus = telemetryQueryMeta(result.Meta)
	return response, nil
}

func telemetryQueryMeta(meta queryengine.QueryMeta) telemetryapi.TelemetryQueryMeta {
	return telemetryapi.TelemetryQueryMeta{
		PlanHash: meta.PlanHash, Engine: meta.Engine, EngineVersion: meta.EngineVersion,
		ScannedBytes: meta.ScannedBytes, ScannedRows: meta.ScannedRows, ReturnedRows: meta.ReturnedRows,
		LoadedSamples: meta.LoadedSamples, ElapsedMs: meta.ElapsedMillis, Partial: meta.Partial,
	}
}

func queryWarnings(warnings []string) []string {
	if warnings == nil {
		return []string{}
	}
	return warnings
}

type telemetryRequestError struct {
	apiError telemetryapi.ApiError
}

func (err telemetryRequestError) Error() string {
	return err.apiError.Code
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
		converted := telemetryapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
			Params: copyErrorParams[map[string]telemetryapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
			Retryable: value.Retryable, TraceId: value.TraceId}
		return telemetryservice.Actor{}, identity.Principal{}, &converted
	}
	if principal.EnterpriseUser == nil {
		denied := telemetryError(ctx, identity.ErrSessionInvalid)
		return telemetryservice.Actor{}, identity.Principal{}, &denied
	}
	return telemetryservice.Actor{EnterpriseID: principal.EnterpriseUser.EnterpriseID, SubjectID: principal.EnterpriseUser.ID,
		AuthorizationVersion: principal.EnterpriseUser.AuthorizationVersion, AuthorizedResourceIDs: slices.Clone(principal.AuthorizedResourceIDs)}, principal, nil
}

func (handler TelemetryHandler) previewCollector(ctx context.Context, actor telemetryservice.Actor, resourceType string, resourceID uuid.UUID, action string, body *telemetryapi.CollectorPreview, key string) (db.PendingAction, error) {
	if body == nil {
		return db.PendingAction{}, telemetryservice.ErrQueryInvalid
	}
	input, err := collectorPreviewInputFromAPI(*body)
	if err != nil {
		return db.PendingAction{}, err
	}
	return handler.Service.PreviewCollectorAction(ctx, actor, resourceType, resourceID, action, input, key)
}

func collectorPreviewInputFromAPI(body telemetryapi.CollectorPreview) (telemetryservice.CollectorPreviewInput, error) {
	gateway := uuid.NullUUID{}
	if body.GatewayCollectorId != nil {
		gateway = uuid.NullUUID{UUID: uuid.UUID(*body.GatewayCollectorId), Valid: true}
	}
	expected := int64(0)
	if body.ExpectedVersion != nil {
		expected = *body.ExpectedVersion
	}
	kubernetesImage := ""
	if body.KubernetesImage != nil {
		kubernetesImage = *body.KubernetesImage
	}
	imagePullSecrets := []string(nil)
	if body.ImagePullSecrets != nil {
		imagePullSecrets = slices.Clone(*body.ImagePullSecrets)
	}
	loopbackPort := int32(0)
	if body.LoopbackPort != nil {
		if *body.LoopbackPort < 1 || *body.LoopbackPort >= 65535 {
			return telemetryservice.CollectorPreviewInput{}, telemetryservice.ErrQueryInvalid
		}
		loopbackPort = int32(*body.LoopbackPort)
	}
	return telemetryservice.CollectorPreviewInput{
		DistributionVersionID: uuid.UUID(body.DistributionVersionId), ProfileIDs: fromOpenAPIUUIDs(body.ProfileIds),
		RouteKind: string(body.RouteKind), Transport: string(body.Transport), LoopbackPort: loopbackPort,
		GatewayCollectorID: gateway, ExpectedVersion: expected, KubernetesImage: kubernetesImage, ImagePullSecrets: imagePullSecrets,
	}, nil
}

func telemetryStatus(err error) int {
	var requestError telemetryRequestError
	if errors.As(err, &requestError) {
		return telemetryAuthStatus(requestError.apiError)
	}
	switch {
	case errors.Is(err, telemetryservice.ErrEnrollmentInvalid), errors.Is(err, telemetryservice.ErrCertificateFenced):
		return http.StatusUnauthorized
	case errors.Is(err, telemetryservice.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, telemetryservice.ErrDenied):
		return http.StatusForbidden
	case errors.Is(err, telemetryservice.ErrDistributionPending), errors.Is(err, telemetryservice.ErrQueryInvalid):
		return http.StatusBadRequest
	case errors.Is(err, telemetryservice.ErrQueryBudget), errors.Is(err, queryengine.ErrBudget):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, telemetryservice.ErrQueryParse), errors.Is(err, telemetryservice.ErrQueryType), errors.Is(err, telemetryservice.ErrQueryUnsupported), errors.Is(err, telemetryservice.ErrQueryComplexity), errors.Is(err, telemetryservice.ErrQueryInvalid):
		return http.StatusBadRequest
	case errors.Is(err, telemetryservice.ErrQueryScope):
		return http.StatusForbidden
	case errors.Is(err, postgres.ErrIdempotencyConflict), errors.Is(err, postgres.ErrIdempotencyExpired):
		return http.StatusConflict
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
	var requestError telemetryRequestError
	if errors.As(err, &requestError) {
		logMappedError(ctx, requestError.apiError.Code, err)
		return requestError.apiError
	}
	base := setupErrorBase(ctx, err)
	code := "TELEMETRY_DEPENDENCY_UNAVAILABLE"
	defer func() { logMappedError(ctx, code, err) }()
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
	case errors.Is(err, telemetryservice.ErrRouteTransportInvalid):
		code = "COLLECTOR_ROUTE_TRANSPORT_INVALID"
	case errors.Is(err, telemetryservice.ErrTunnelQuotaExceeded):
		code = "TUNNEL_QUOTA_EXCEEDED"
	case errors.Is(err, telemetryservice.ErrSelfEnrolledOperationUnsupported):
		code = "HOST_OPERATION_UNSUPPORTED_FOR_SELF_ENROLLED"
	case errors.Is(err, telemetryservice.ErrQueryParse):
		code = "QUERY_PARSE_ERROR"
	case errors.Is(err, telemetryservice.ErrQueryType):
		code = "QUERY_TYPE_ERROR"
	case errors.Is(err, telemetryservice.ErrQueryUnsupported):
		code = "QUERY_FEATURE_UNSUPPORTED"
	case errors.Is(err, telemetryservice.ErrQueryComplexity):
		code = "QUERY_COMPLEXITY_LIMIT"
	case errors.Is(err, telemetryservice.ErrQueryScope):
		code = "QUERY_SCOPE_DENIED"
	case errors.Is(err, telemetryservice.ErrQueryBudget), errors.Is(err, queryengine.ErrBudget):
		code = "QUERY_BUDGET_EXCEEDED"
	case errors.Is(err, postgres.ErrIdempotencyConflict):
		code = "IDEMPOTENCY_CONFLICT"
	case errors.Is(err, postgres.ErrIdempotencyExpired):
		code = "IDEMPOTENCY_RESULT_EXPIRED"
	}
	messageKey := "errors.telemetry." + code
	retryable := base.Retryable
	switch code {
	case "QUERY_BUDGET_EXCEEDED":
		messageKey = "errors.telemetry.query_budget_exceeded"
	case "TELEMETRY_DEPENDENCY_UNAVAILABLE":
		messageKey = "errors.telemetry.dependency_unavailable"
		retryable = pointer(true)
	case "IDEMPOTENCY_CONFLICT":
		messageKey = "errors.common.idempotency_conflict"
	case "IDEMPOTENCY_RESULT_EXPIRED":
		messageKey = "errors.common.idempotency_result_expired"
	}
	return telemetryapi.ApiError{Code: code, MessageKey: messageKey, RequestId: base.RequestId, Retryable: retryable}
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
func toCollectorDistribution(value db.CollectorDistributionVersion, defaultKubernetesImage string) telemetryapi.CollectorDistributionVersion {
	var artifacts []telemetryapi.CollectorArtifact
	_ = json.Unmarshal(value.ArtifactManifest, &artifacts)
	result := telemetryapi.CollectorDistributionVersion{Id: value.ID, Name: value.Name, Version: value.Version, CollectorVersion: value.CollectorVersion, ConfigSchemaVersion: value.ConfigSchemaVersion, SupportStatus: telemetryapi.CollectorSupportStatus(value.SupportStatus), Components: value.Components, Artifacts: artifacts, CreatedAt: value.CreatedAt.Time}
	if defaultKubernetesImage != "" {
		image := defaultKubernetesImage
		result.KubernetesImage = &image
	}
	return result
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
	if operation, operationErr := q.GetLatestCollectorOperation(ctx, db.GetLatestCollectorOperationParams{CollectorID: value.ID, EnterpriseID: value.EnterpriseID}); operationErr == nil && operation.ErrorCode.Valid {
		errorCode := operation.ErrorCode.String
		result.LastOperationErrorCode = &errorCode
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
