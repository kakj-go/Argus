package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	kubernetesapi "github.com/kakj-go/Argus/internal/gen/openapi/kubernetesapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type KubernetesHandler struct {
	Identity EnterpriseIdentityHandler
	Service  resource.Service
}

func (handler KubernetesHandler) ListKubernetesClusters(ctx context.Context, _ kubernetesapi.ListKubernetesClustersRequestObject) (kubernetesapi.ListKubernetesClustersResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "kubernetes.read")
	if apiError != nil {
		return kubernetesapi.ListKubernetesClustersdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListKubernetesClusters(ctx, p.EnterpriseIDValue(), p.AuthorizedResourceIDs)
	if err != nil {
		return kubernetesapi.ListKubernetesClustersdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	converted := make([]kubernetesapi.KubernetesCluster, 0, len(items))
	for _, item := range items {
		converted = append(converted, toKubernetesCluster(item))
	}
	return kubernetesapi.ListKubernetesClusters200JSONResponse{Items: converted, Page: emptyKubernetesPage()}, nil
}

func (handler KubernetesHandler) GetKubernetesCluster(ctx context.Context, request kubernetesapi.GetKubernetesClusterRequestObject) (kubernetesapi.GetKubernetesClusterResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "kubernetes.read")
	if apiError != nil {
		return kubernetesapi.GetKubernetesClusterdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	item, err := handler.Service.GetKubernetesCluster(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id), p.AuthorizedResourceIDs)
	if err != nil {
		return kubernetesapi.GetKubernetesClusterdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return kubernetesapi.GetKubernetesCluster200JSONResponse(toKubernetesCluster(item)), nil
}

func (handler KubernetesHandler) CreateKubernetesConnectionTest(ctx context.Context, request kubernetesapi.CreateKubernetesConnectionTestRequestObject) (kubernetesapi.CreateKubernetesConnectionTestResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "kubernetes.manage")
	if apiError != nil {
		return kubernetesapi.CreateKubernetesConnectionTestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return kubernetesapi.CreateKubernetesConnectionTestdefaultJSONResponse{Body: kubernetesError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := resource.KubernetesInput{APIServer: request.Body.ApiServer, ConnectionMode: string(request.Body.ConnectionMode),
		BastionScopeID: optionalUUID(request.Body.BastionScopeId), CredentialID: optionalUUID(request.Body.CredentialId)}
	test, err := handler.Service.CreateKubernetesConnectionTest(ctx, resourceSubject(p), p.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	if err != nil {
		return kubernetesapi.CreateKubernetesConnectionTestdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return kubernetesapi.CreateKubernetesConnectionTest202JSONResponse(toKubernetesConnectionTest(test)), nil
}

func (handler KubernetesHandler) PreviewCreateKubernetesCluster(ctx context.Context, request kubernetesapi.PreviewCreateKubernetesClusterRequestObject) (kubernetesapi.PreviewCreateKubernetesClusterResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "kubernetes.manage")
	if apiError != nil {
		return kubernetesapi.PreviewCreateKubernetesClusterdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return kubernetesapi.PreviewCreateKubernetesClusterdefaultJSONResponse{Body: kubernetesError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := resource.KubernetesInput{Name: request.Body.Name, APIServer: request.Body.ApiServer, ConnectionMode: string(request.Body.ConnectionMode),
		Environment: string(request.Body.Environment), Labels: stringMap(request.Body.Labels), BastionScopeID: optionalUUID(request.Body.BastionScopeId),
		CredentialID: optionalUUID(request.Body.CredentialId), ConnectionTestID: optionalUUID(request.Body.ConnectionTestId)}
	if request.Body.ConnectorImagePullSecrets != nil {
		input.ConnectorImagePullSecrets = append([]string(nil), (*request.Body.ConnectorImagePullSecrets)...)
	}
	if request.Body.DefaultNamespace != nil {
		input.DefaultNamespace = *request.Body.DefaultNamespace
	}
	action, err := handler.Service.PreviewCreateKubernetesCluster(ctx, resourceSubject(p), p.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	if err != nil {
		return kubernetesapi.PreviewCreateKubernetesClusterdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return kubernetesapi.PreviewCreateKubernetesCluster201JSONResponse(convertPending[kubernetesapi.PendingActionPublicSchema](action)), nil
}

func (handler KubernetesHandler) PreviewUpdateKubernetesCluster(ctx context.Context, request kubernetesapi.PreviewUpdateKubernetesClusterRequestObject) (kubernetesapi.PreviewUpdateKubernetesClusterResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "kubernetes.manage")
	if apiError != nil {
		return kubernetesapi.PreviewUpdateKubernetesClusterdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return kubernetesapi.PreviewUpdateKubernetesClusterdefaultJSONResponse{Body: kubernetesError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := kubernetesUpdateInput(*request.Body)
	action, err := handler.Service.PreviewUpdateKubernetesCluster(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), input, request.Params.IdempotencyKey)
	if err != nil {
		return kubernetesapi.PreviewUpdateKubernetesClusterdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return kubernetesapi.PreviewUpdateKubernetesCluster201JSONResponse(convertPending[kubernetesapi.PendingActionPublicSchema](action)), nil
}

func (handler KubernetesHandler) PreviewDeleteKubernetesCluster(ctx context.Context, request kubernetesapi.PreviewDeleteKubernetesClusterRequestObject) (kubernetesapi.PreviewDeleteKubernetesClusterResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "kubernetes.manage")
	if apiError != nil {
		return kubernetesapi.PreviewDeleteKubernetesClusterdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return kubernetesapi.PreviewDeleteKubernetesClusterdefaultJSONResponse{Body: kubernetesError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Service.PreviewDeleteKubernetesCluster(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Body.ExpectedVersion, request.Params.IdempotencyKey)
	if err != nil {
		return kubernetesapi.PreviewDeleteKubernetesClusterdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return kubernetesapi.PreviewDeleteKubernetesCluster201JSONResponse(convertPending[kubernetesapi.PendingActionPublicSchema](action)), nil
}

func (handler KubernetesHandler) ListKubernetesResources(ctx context.Context, request kubernetesapi.ListKubernetesResourcesRequestObject) (kubernetesapi.ListKubernetesResourcesResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "kubernetes.read")
	if apiError != nil {
		return kubernetesapi.ListKubernetesResourcesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	limit := 100
	if request.Params.Limit != nil {
		limit = *request.Params.Limit
	}
	query := resource.KubernetesQuery{ResourceType: string(request.Params.ResourceType), Limit: limit}
	if request.Params.Namespace != nil {
		query.Namespace = *request.Params.Namespace
	}
	if request.Params.Query != nil {
		query.Query = *request.Params.Query
	}
	items, err := handler.Service.ListKubernetesResources(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), query)
	if err != nil {
		return kubernetesapi.ListKubernetesResourcesdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	converted := make([]kubernetesapi.KubernetesResource, 0, len(items))
	for _, item := range items {
		value := kubernetesapi.KubernetesResource{ClusterId: request.Id, ResourceType: kubernetesapi.KubernetesResourceResourceType(item.ResourceType),
			Name: item.Name, Labels: kubernetesapi.Labels(item.Labels), Summary: item.Summary}
		if item.Namespace != "" {
			value.Namespace = &item.Namespace
		}
		converted = append(converted, value)
	}
	return kubernetesapi.ListKubernetesResources200JSONResponse{Items: converted, Page: emptyKubernetesPage()}, nil
}

func (handler KubernetesHandler) GetKubernetesPodLogs(ctx context.Context, request kubernetesapi.GetKubernetesPodLogsRequestObject) (kubernetesapi.GetKubernetesPodLogsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "kubernetes.logs")
	if apiError != nil {
		return kubernetesapi.GetKubernetesPodLogsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	tail := int64(500)
	if request.Params.TailLines != nil {
		tail = int64(*request.Params.TailLines)
	}
	query := resource.PodLogsQuery{Namespace: request.Params.Namespace, Pod: request.Params.Pod, TailLines: tail}
	if request.Params.Container != nil {
		query.Container = *request.Params.Container
	}
	content, truncated, err := handler.Service.GetKubernetesPodLogs(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), query)
	if err != nil {
		return kubernetesapi.GetKubernetesPodLogsdefaultJSONResponse{Body: kubernetesError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	result := kubernetesapi.PodLogs{ClusterId: request.Id, Namespace: query.Namespace, Pod: query.Pod, Content: string(content), Bytes: len(content), Truncated: truncated}
	if query.Container != "" {
		result.Container = &query.Container
	}
	return kubernetesapi.GetKubernetesPodLogs200JSONResponse(result), nil
}

func (handler KubernetesHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *kubernetesapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &kubernetesapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]kubernetesapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func kubernetesUpdateInput(value kubernetesapi.KubernetesPreviewUpdate) resource.KubernetesInput {
	result := resource.KubernetesInput{ExpectedVersion: value.ExpectedVersion, BastionScopeID: optionalUUID(value.BastionScopeId), CredentialID: optionalUUID(value.CredentialId),
		ConnectionTestID: optionalUUID(value.ConnectionTestId)}
	if value.Name != nil {
		result.Name = *value.Name
	}
	if value.ApiServer != nil {
		result.APIServer = *value.ApiServer
	}
	if value.ConnectionMode != nil {
		result.ConnectionMode = string(*value.ConnectionMode)
	}
	if value.DefaultNamespace != nil {
		result.DefaultNamespace = *value.DefaultNamespace
	}
	if value.Environment != nil {
		result.Environment = string(*value.Environment)
	}
	if value.Labels != nil {
		result.Labels = stringMap(*value.Labels)
	}
	return result
}

func toKubernetesCluster(value db.KubernetesCluster) kubernetesapi.KubernetesCluster {
	labels, _ := resource.DecodeLabels(value.Labels)
	result := kubernetesapi.KubernetesCluster{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerUUID(value.EnterpriseID), Name: value.Name, ApiServer: value.ApiServer,
		ConnectionMode: kubernetesapi.KubernetesConnectionMode(value.ConnectionMode), Environment: kubernetesapi.Environment(value.Environment), Labels: kubernetesapi.Labels(labels),
		LabelsVersion: value.LabelsVersion, ResourceVersion: value.ResourceVersion, ConnectionStatus: kubernetesapi.KubernetesClusterConnectionStatus(value.ConnectionStatus),
		Status: kubernetesapi.KubernetesClusterStatus(value.Status), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.BastionScopeID.Valid {
		id := openapi_types.UUID(value.BastionScopeID.UUID)
		result.BastionScopeId = &id
	}
	if value.ConnectorID.Valid {
		id := openapi_types.UUID(value.ConnectorID.UUID)
		result.ConnectorId = &id
	}
	if value.CredentialID.Valid {
		id := openapi_types.UUID(value.CredentialID.UUID)
		result.CredentialId = &id
	}
	if value.DefaultNamespace != "" {
		result.DefaultNamespace = &value.DefaultNamespace
	}
	if value.KubernetesVersion != "" {
		result.KubernetesVersion = &value.KubernetesVersion
	}
	nodeCount, readyCount := int(value.NodeCount), int(value.ReadyNodeCount)
	result.NodeCount, result.ReadyNodeCount = &nodeCount, &readyCount
	return result
}

func toKubernetesConnectionTest(value db.ConnectionTest) kubernetesapi.ConnectionTest {
	encoded, _ := jsonMarshal(toHostConnectionTest(value))
	var result kubernetesapi.ConnectionTest
	_ = jsonUnmarshal(encoded, &result)
	return result
}

func jsonMarshal(value any) ([]byte, error)        { return json.Marshal(value) }
func jsonUnmarshal(value []byte, target any) error { return json.Unmarshal(value, target) }

func emptyKubernetesPage() kubernetesapi.CursorPage {
	return kubernetesapi.CursorPage{NextCursor: nil, HasMore: false, Partial: kubernetesapi.PartialMetadata{Partial: false, Reasons: []kubernetesapi.PartialMetadataReasons{}}}
}

func kubernetesError(ctx context.Context, err error) kubernetesapi.ApiError {
	base := hostErrorBase(ctx, err)
	if errors.Is(err, resource.ErrKubernetesUnavailable) {
		base.Code, base.MessageKey = "KUBERNETES_QUERY_UNAVAILABLE", "errors.kubernetes.query_unavailable"
	}
	logMappedError(ctx, base.Code, err)
	return kubernetesapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]kubernetesapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}
