package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/kakj-go/Argus/internal/connector"
	connectorapi "github.com/kakj-go/Argus/internal/gen/openapi/connectorapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/installinstruction"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

type ConnectorHandler struct {
	Identity EnterpriseIdentityHandler
	Service  connector.Service
	Bastion  connector.BastionService
	Queries  *db.Queries
}

func (handler ConnectorHandler) GetConnectorBootstrapScript(ctx context.Context, request connectorapi.GetConnectorBootstrapScriptRequestObject) (connectorapi.GetConnectorBootstrapScriptResponseObject, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok || metadata.Request.Header.Get("Authorization") != "" || metadata.Request.Header.Get("Cookie") != "" {
		return connectorapi.GetConnectorBootstrapScriptdefaultJSONResponse{
			Body: connectorError(ctx, connector.ErrEnrollmentInvalid), StatusCode: http.StatusUnauthorized}, nil
	}
	token := strings.TrimSpace(metadata.Request.Header.Get("X-Argus-Enrollment-Token"))
	if token == "" || len(token) > 256 {
		return connectorapi.GetConnectorBootstrapScriptdefaultJSONResponse{
			Body: connectorError(ctx, connector.ErrEnrollmentInvalid), StatusCode: http.StatusUnauthorized}, nil
	}
	script, err := handler.Service.BootstrapScript(ctx, token, installinstruction.Scope(request.Params.Scope))
	if err != nil {
		return connectorapi.GetConnectorBootstrapScriptdefaultJSONResponse{
			Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	cacheControl, disposition, contentTypeOptions := "no-store", `inline; filename="argus-connector-bootstrap.sh"`, "nosniff"
	return connectorapi.GetConnectorBootstrapScript200TextxShellscriptResponse{
		Body: io.NopCloser(strings.NewReader(script)), ContentLength: int64(len(script)),
		Headers: connectorapi.GetConnectorBootstrapScript200ResponseHeaders{
			CacheControl: &cacheControl, ContentDisposition: &disposition, XContentTypeOptions: &contentTypeOptions,
		},
	}, nil
}

func (handler ConnectorHandler) EnrollConnector(ctx context.Context, request connectorapi.EnrollConnectorRequestObject) (connectorapi.EnrollConnectorResponseObject, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok || request.Body == nil || request.Body.CsrPem == nil {
		return connectorapi.EnrollConnectordefaultJSONResponse{Body: connectorError(ctx, connector.ErrEnrollmentInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	token := metadata.Request.Header.Get("X-Argus-Enrollment-Token")
	if token == "" || metadata.Request.Header.Get("Authorization") != "" || metadata.Request.Header.Get("Cookie") != "" {
		return connectorapi.EnrollConnectordefaultJSONResponse{Body: connectorError(ctx, connector.ErrEnrollmentInvalid), StatusCode: http.StatusUnauthorized}, nil
	}
	result, err := handler.Service.Enroll(ctx, connector.EnrollInput{Token: token, CSRPem: *request.Body.CsrPem,
		DeviceFingerprint: request.Body.DeviceFingerprint, InstanceID: request.Body.InstanceId, Architecture: string(request.Body.Architecture), Name: request.Body.Name,
		SoftwareVersion: request.Body.SoftwareVersion, Capabilities: request.Body.Capabilities})
	if err != nil {
		return connectorapi.EnrollConnectordefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.EnrollConnector201JSONResponse(toConnectorEnrollResult(result)), nil
}

func (handler ConnectorHandler) ListBastionScopes(ctx context.Context, _ connectorapi.ListBastionScopesRequestObject) (connectorapi.ListBastionScopesResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "bastion_scope.read")
	if apiError != nil {
		return connectorapi.ListBastionScopesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Bastion.List(ctx, p.EnterpriseIDValue())
	if err != nil {
		return connectorapi.ListBastionScopesdefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	converted := make([]connectorapi.BastionScope, 0, len(items))
	onboarding := loadBastionOnboarding(ctx, handler.Queries, p.EnterpriseIDValue(), items)
	for _, item := range items {
		converted = append(converted, toBastionScope(item, onboarding[item.ID]))
	}
	return connectorapi.ListBastionScopes200JSONResponse{Items: converted, Page: emptyConnectorPage()}, nil
}

func (handler ConnectorHandler) GetBastionScope(ctx context.Context, request connectorapi.GetBastionScopeRequestObject) (connectorapi.GetBastionScopeResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "bastion_scope.read")
	if apiError != nil {
		return connectorapi.GetBastionScopedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	item, err := handler.Bastion.Get(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return connectorapi.GetBastionScopedefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	encoded, _ := jsonMarshal(item)
	var listRow db.ListBastionScopesRow
	_ = jsonUnmarshal(encoded, &listRow)
	onboarding := loadBastionOnboarding(ctx, handler.Queries, p.EnterpriseIDValue(), []db.ListBastionScopesRow{listRow})
	return connectorapi.GetBastionScope200JSONResponse(toBastionScope(item, onboarding[item.ID])), nil
}

func (handler ConnectorHandler) PreviewCreateBastionScope(ctx context.Context, request connectorapi.PreviewCreateBastionScopeRequestObject) (connectorapi.PreviewCreateBastionScopeResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "bastion_scope.manage")
	if apiError != nil {
		return connectorapi.PreviewCreateBastionScopedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return connectorapi.PreviewCreateBastionScopedefaultJSONResponse{Body: connectorError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := connector.BastionInput{Name: request.Body.Name,
		Environment: string(request.Body.Environment), Labels: stringMap(request.Body.Labels),
		InstallMode: string(request.Body.InstallMode)}
	if request.Body.Address != nil {
		input.Address = *request.Body.Address
	}
	if request.Body.Port != nil {
		input.Port = int32(*request.Body.Port)
	}
	if request.Body.Username != nil {
		input.Username = *request.Body.Username
	}
	if request.Body.CredentialId != nil {
		input.CredentialID = uuid.NullUUID{UUID: uuid.UUID(*request.Body.CredentialId), Valid: true}
	}
	if request.Body.ConnectionTestId != nil {
		input.ConnectionTestID = uuid.NullUUID{UUID: uuid.UUID(*request.Body.ConnectionTestId), Valid: true}
	}
	action, err := handler.Bastion.PreviewCreate(ctx, resourceSubject(p), p.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	if err != nil {
		return connectorapi.PreviewCreateBastionScopedefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.PreviewCreateBastionScope201JSONResponse(convertPending[connectorapi.PendingActionPublicSchema](action)), nil
}

func (handler ConnectorHandler) PreviewUpdateBastionScope(ctx context.Context, request connectorapi.PreviewUpdateBastionScopeRequestObject) (connectorapi.PreviewUpdateBastionScopeResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "bastion_scope.manage")
	if apiError != nil {
		return connectorapi.PreviewUpdateBastionScopedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return connectorapi.PreviewUpdateBastionScopedefaultJSONResponse{Body: connectorError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := connector.BastionInput{ExpectedVersion: request.Body.ExpectedVersion}
	if request.Body.Name != nil {
		input.Name = *request.Body.Name
	}
	if request.Body.Environment != nil {
		input.Environment = string(*request.Body.Environment)
	}
	if request.Body.Labels != nil {
		input.Labels = stringMap(*request.Body.Labels)
	}
	action, err := handler.Bastion.PreviewUpdate(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), input, request.Params.IdempotencyKey)
	if err != nil {
		return connectorapi.PreviewUpdateBastionScopedefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.PreviewUpdateBastionScope201JSONResponse(convertPending[connectorapi.PendingActionPublicSchema](action)), nil
}

func (handler ConnectorHandler) PreviewDeleteBastionScope(ctx context.Context, request connectorapi.PreviewDeleteBastionScopeRequestObject) (connectorapi.PreviewDeleteBastionScopeResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "bastion_scope.manage")
	if apiError != nil {
		return connectorapi.PreviewDeleteBastionScopedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return connectorapi.PreviewDeleteBastionScopedefaultJSONResponse{Body: connectorError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Bastion.PreviewLifecycle(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Body.ExpectedVersion, "delete", request.Params.IdempotencyKey)
	if err != nil {
		return connectorapi.PreviewDeleteBastionScopedefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.PreviewDeleteBastionScope201JSONResponse(convertPending[connectorapi.PendingActionPublicSchema](action)), nil
}

func (handler ConnectorHandler) PreviewBastionEnrollmentRotate(ctx context.Context, request connectorapi.PreviewBastionEnrollmentRotateRequestObject) (connectorapi.PreviewBastionEnrollmentRotateResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "connector.manage")
	if apiError != nil {
		return connectorapi.PreviewBastionEnrollmentRotatedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return connectorapi.PreviewBastionEnrollmentRotatedefaultJSONResponse{Body: connectorError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Bastion.PreviewLifecycle(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Body.ExpectedVersion, "rotate", request.Params.IdempotencyKey)
	if err != nil {
		return connectorapi.PreviewBastionEnrollmentRotatedefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.PreviewBastionEnrollmentRotate201JSONResponse(convertPending[connectorapi.PendingActionPublicSchema](action)), nil
}

func (handler ConnectorHandler) PreviewBastionConnectorReplacement(ctx context.Context, request connectorapi.PreviewBastionConnectorReplacementRequestObject) (connectorapi.PreviewBastionConnectorReplacementResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "connector.manage")
	if apiError != nil {
		return connectorapi.PreviewBastionConnectorReplacementdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return connectorapi.PreviewBastionConnectorReplacementdefaultJSONResponse{Body: connectorError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := connector.BastionInput{ExpectedVersion: request.Body.ExpectedVersion}
	if request.Body.Address != nil {
		input.Address = *request.Body.Address
	}
	if request.Body.Port != nil {
		input.Port = int32(*request.Body.Port)
	}
	if request.Body.Username != nil {
		input.Username = *request.Body.Username
	}
	if request.Body.CredentialId != nil {
		input.CredentialID = uuid.NullUUID{UUID: uuid.UUID(*request.Body.CredentialId), Valid: true}
	}
	if request.Body.ConnectionTestId != nil {
		input.ConnectionTestID = uuid.NullUUID{UUID: uuid.UUID(*request.Body.ConnectionTestId), Valid: true}
	}
	action, err := handler.Bastion.PreviewReplacement(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), input, request.Params.IdempotencyKey)
	if err != nil {
		return connectorapi.PreviewBastionConnectorReplacementdefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.PreviewBastionConnectorReplacement201JSONResponse(convertPending[connectorapi.PendingActionPublicSchema](action)), nil
}

func (handler ConnectorHandler) GetConnectorInstallOperation(ctx context.Context, request connectorapi.GetConnectorInstallOperationRequestObject) (connectorapi.GetConnectorInstallOperationResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "connector.read")
	if apiError != nil {
		return connectorapi.GetConnectorInstallOperationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	view, err := handler.Bastion.GetInstallOperation(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return connectorapi.GetConnectorInstallOperationdefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.GetConnectorInstallOperation200JSONResponse(toConnectorInstallOperation(view)), nil
}

func (handler ConnectorHandler) PreviewRetryConnectorInstallOperation(ctx context.Context, request connectorapi.PreviewRetryConnectorInstallOperationRequestObject) (connectorapi.PreviewRetryConnectorInstallOperationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "connector.manage")
	if apiError != nil {
		return connectorapi.PreviewRetryConnectorInstallOperationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	action, err := handler.Bastion.PreviewRetryInstall(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Params.IdempotencyKey)
	if err != nil {
		return connectorapi.PreviewRetryConnectorInstallOperationdefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.PreviewRetryConnectorInstallOperation201JSONResponse(convertPending[connectorapi.PendingActionPublicSchema](action)), nil
}

func (handler ConnectorHandler) ListConnectors(ctx context.Context, _ connectorapi.ListConnectorsRequestObject) (connectorapi.ListConnectorsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "connector.read")
	if apiError != nil {
		return connectorapi.ListConnectorsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Bastion.ListConnectors(ctx, p.EnterpriseIDValue())
	if err != nil {
		return connectorapi.ListConnectorsdefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	converted := make([]connectorapi.Connector, 0, len(items))
	for _, item := range items {
		converted = append(converted, toConnector(item))
	}
	return connectorapi.ListConnectors200JSONResponse{Items: converted, Page: emptyConnectorPage()}, nil
}

func (handler ConnectorHandler) GetConnector(ctx context.Context, request connectorapi.GetConnectorRequestObject) (connectorapi.GetConnectorResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "connector.read")
	if apiError != nil {
		return connectorapi.GetConnectordefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	item, err := handler.Bastion.GetConnector(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return connectorapi.GetConnectordefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.GetConnector200JSONResponse(toConnector(item)), nil
}

func (handler ConnectorHandler) PreviewUninstallConnector(ctx context.Context, request connectorapi.PreviewUninstallConnectorRequestObject) (connectorapi.PreviewUninstallConnectorResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "connector.manage")
	if apiError != nil {
		return connectorapi.PreviewUninstallConnectordefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return connectorapi.PreviewUninstallConnectordefaultJSONResponse{Body: connectorError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Bastion.PreviewConnectorUninstall(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Body.ExpectedVersion, request.Params.IdempotencyKey)
	if err != nil {
		return connectorapi.PreviewUninstallConnectordefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.PreviewUninstallConnector201JSONResponse(convertPending[connectorapi.PendingActionPublicSchema](action)), nil
}

func (handler ConnectorHandler) RequestConnectorCertificateRotation(ctx context.Context, request connectorapi.RequestConnectorCertificateRotationRequestObject) (connectorapi.RequestConnectorCertificateRotationResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "connector.manage")
	if apiError != nil {
		return connectorapi.RequestConnectorCertificateRotationdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.RequestCertificateRotation(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Params.ExpectedVersion)
	if err != nil {
		return connectorapi.RequestConnectorCertificateRotationdefaultJSONResponse{Body: connectorError(ctx, err), StatusCode: connectorStatus(err)}, nil
	}
	return connectorapi.RequestConnectorCertificateRotation202JSONResponse(toConnector(value)), nil
}

func (handler ConnectorHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *connectorapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &connectorapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]connectorapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

type bastionRow interface {
	db.GetBastionScopeRow | db.ListBastionScopesRow
}

func toBastionScope[T bastionRow](value T, onboarding onboardingView) connectorapi.BastionScope {
	encoded, _ := jsonMarshal(value)
	var row db.GetBastionScopeRow
	_ = jsonUnmarshal(encoded, &row)
	labels, _ := resource.DecodeLabels(row.Labels)
	result := connectorapi.BastionScope{Id: openapi_types.UUID(row.ID), EnterpriseId: pointerUUID(row.EnterpriseID), Name: row.Name,
		Environment: connectorapi.Environment(row.Environment), Labels: connectorapi.Labels(labels), Status: connectorapi.BastionScopeStatus(row.Status),
		FencingGeneration: row.FencingGeneration, ResourceVersion: row.ResourceVersion, MemberCount: int(row.MemberCount), Onboarding: toBastionOnboarding(onboarding),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time}
	mode := connectorapi.BastionScopeOnboardingMode(row.OnboardingMode)
	result.OnboardingMode = &mode
	if row.ConnectorHostID.Valid {
		id := openapi_types.UUID(row.ConnectorHostID.UUID)
		result.ConnectorHostId = &id
	}
	if row.ActiveConnectorID.Valid {
		id := openapi_types.UUID(row.ActiveConnectorID.UUID)
		result.ActiveConnectorId = &id
	}
	if row.ControlTunnelStatus != "" {
		status := connectorapi.BastionScopeControlTunnelStatus(row.ControlTunnelStatus)
		result.ControlTunnelStatus = &status
	}
	return result
}

func toBastionOnboarding(value onboardingView) connectorapi.OnboardingProjection {
	result := connectorapi.OnboardingProjection{State: connectorapi.OnboardingProjectionState(value.State), UpdatedAt: value.UpdatedAt}
	if value.PendingActionRef != "" {
		result.PendingActionRef = &value.PendingActionRef
	}
	if value.ExecutionID.Valid {
		id := openapi_types.UUID(value.ExecutionID.UUID)
		result.ExecutionId = &id
	}
	if value.OperationID.Valid {
		id := openapi_types.UUID(value.OperationID.UUID)
		result.OperationId = &id
	}
	if value.ErrorCode != "" {
		result.ErrorCode = &value.ErrorCode
	}
	return result
}

func toConnector(value db.Connector) connectorapi.Connector {
	result := connectorapi.Connector{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerUUID(value.EnterpriseID), Role: connectorapi.ConnectorRole(value.Role), Name: value.Name,
		Capabilities: value.Capabilities, Status: connectorapi.ConnectorStatus(value.Status), ConnectionEpoch: value.ConnectionEpoch,
		CertificateExpiresAt: value.CertificateExpiresAt.Time, Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.SoftwareVersion != "" {
		result.SoftwareVersion = &value.SoftwareVersion
	}
	if value.HostID.Valid {
		id := openapi_types.UUID(value.HostID.UUID)
		result.HostId = &id
	}
	if value.BastionScopeID.Valid {
		id := openapi_types.UUID(value.BastionScopeID.UUID)
		result.BastionScopeId = &id
	}
	if value.KubernetesClusterID.Valid {
		id := openapi_types.UUID(value.KubernetesClusterID.UUID)
		result.KubernetesClusterId = &id
	}
	if value.ConnectedAt.Valid {
		result.ConnectedAt = &value.ConnectedAt.Time
	}
	if value.LastHeartbeatAt.Valid {
		result.LastHeartbeatAt = &value.LastHeartbeatAt.Time
	}
	return result
}

func toConnectorInstallOperation(view connector.ConnectorInstallOperationView) connectorapi.ConnectorInstallOperation {
	operation := view.Operation
	events := make([]connectorapi.ConnectorInstallOperationEvent, 0, len(view.Events))
	for _, event := range view.Events {
		item := connectorapi.ConnectorInstallOperationEvent{Id: event.ID, Stage: connectorapi.ConnectorInstallOperationEventStage(event.Stage),
			Status: connectorapi.ConnectorInstallOperationEventStatus(event.Status), OccurredAt: event.OccurredAt.Time}
		if event.ErrorCode.Valid {
			item.ErrorCode = &event.ErrorCode.String
		}
		events = append(events, item)
	}
	result := connectorapi.ConnectorInstallOperation{Id: operation.ID, ConnectorId: operation.ConnectorID,
		BastionScopeId: operation.BastionScopeID, HostId: operation.HostID,
		InstallMode: connectorapi.ConnectorInstallOperationInstallMode(operation.InstallMode),
		Stage:       connectorapi.ConnectorInstallOperationStage(operation.Stage), Status: connectorapi.ConnectorInstallOperationStatus(operation.Status),
		Attempt: int(operation.Attempts), MaxAttempts: 3, Events: events, ExpiresAt: operation.ExpiresAt.Time,
		CreatedAt: operation.CreatedAt.Time, UpdatedAt: operation.UpdatedAt.Time}
	connectionTestID := openapi_types.UUID(operation.ConnectionTestID)
	result.ConnectionTestId = &connectionTestID
	releaseID := openapi_types.UUID(operation.ReleaseVersionID)
	result.ReleaseVersionId = &releaseID
	if operation.RetryOf.Valid {
		id := openapi_types.UUID(operation.RetryOf.UUID)
		result.RetryOf = &id
	}
	if operation.ConnectorOnlineAt.Valid {
		result.ConnectorOnlineAt = &operation.ConnectorOnlineAt.Time
	}
	if operation.CompletedAt.Valid {
		result.CompletedAt = &operation.CompletedAt.Time
	}
	if operation.ErrorCode.Valid {
		result.ErrorCode = &operation.ErrorCode.String
	}
	if view.ControlTunnel != nil {
		status := connectorapi.ConnectorInstallOperationControlTunnelStatus(view.ControlTunnel.Status)
		result.ControlTunnelStatus = &status
	}
	return result
}

func toConnectorEnrollResult(value connector.EnrollmentResult) connectorapi.ConnectorEnrollResult {
	result := connectorapi.ConnectorEnrollResult{ConnectorId: openapi_types.UUID(value.Connector.ID), Role: connectorapi.ConnectorRole(value.Connector.Role),
		CertificatePem: &value.CertificatePEM, TrustBundle: toConnectorTrustBundle(value.TrustBundle), GatewayEndpoint: value.GatewayEndpoint,
		CertificateExpiresAt: value.Connector.CertificateExpiresAt.Time, Result: connectorapi.ConnectorEnrollResultResult(value.Result)}
	if value.Connector.HostID.Valid {
		id := openapi_types.UUID(value.Connector.HostID.UUID)
		result.HostId = &id
	}
	if value.Connector.BastionScopeID.Valid {
		id := openapi_types.UUID(value.Connector.BastionScopeID.UUID)
		result.BastionScopeId = &id
	}
	if value.Connector.KubernetesClusterID.Valid {
		id := openapi_types.UUID(value.Connector.KubernetesClusterID.UUID)
		result.KubernetesClusterId = &id
	}
	return result
}

func toConnectorTrustBundle(value trustbundle.Bundle) connectorapi.TrustBundleSnapshot {
	result := connectorapi.TrustBundleSnapshot{Epoch: value.Epoch, State: connectorapi.TrustBundleSnapshotState(value.State),
		BundlePem: string(value.Material.PEM), BundleSha256: value.Material.SHA256,
		CurrentCaFingerprints: value.CurrentCAFingerprints, NextCaFingerprints: value.NextCAFingerprints, StartedAt: value.StartedAt}
	if !value.RetireAt.IsZero() {
		result.RetireAt = &value.RetireAt
	}
	return result
}

func emptyConnectorPage() connectorapi.CursorPage {
	return connectorapi.CursorPage{NextCursor: nil, HasMore: false, Partial: connectorapi.PartialMetadata{Partial: false, Reasons: []connectorapi.PartialMetadataReasons{}}}
}

func connectorError(ctx context.Context, err error) connectorapi.ApiError {
	base := hostErrorBase(ctx, err)
	defer func() { logMappedError(ctx, base.Code, err) }()
	switch {
	case errors.Is(err, connector.ErrEnrollmentInvalid):
		base.Code, base.MessageKey = "CONNECTOR_ENROLLMENT_INVALID", "errors.connector.enrollment_invalid"
	case errors.Is(err, connector.ErrEnrollmentConflict):
		base.Code, base.MessageKey = "CONNECTOR_ENROLLMENT_CONFLICT", "errors.connector.enrollment_conflict"
	case errors.Is(err, connector.ErrConnectorFenced):
		base.Code, base.MessageKey = "CONNECTOR_FENCED", "errors.connector.fenced"
	case errors.Is(err, connector.ErrBastionNameConflict):
		base.Code, base.MessageKey = "RESOURCE_NAME_CONFLICT", "errors.common.resource_name_conflict"
	case errors.Is(err, connector.ErrCommandState), errors.Is(err, connector.ErrBastionState):
		base.Code, base.MessageKey = "CONNECTOR_COMMAND_STATE_CONFLICT", "errors.connector.state_conflict"
	case errors.Is(err, connector.ErrConnectorArtifactInvalid):
		base.Code, base.MessageKey = "CONNECTOR_ARTIFACT_INVALID", "errors.connector.artifact_invalid"
	case errors.Is(err, connector.ErrConnectorArtifactUnavailable):
		base.Code, base.MessageKey = "CONNECTOR_ARTIFACT_UNAVAILABLE", "errors.connector.artifact_unavailable"
		base.Retryable = retryablePointer(true)
	}
	return connectorapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]connectorapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func connectorStatus(err error) int {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	case errors.Is(err, connector.ErrEnrollmentInvalid):
		return http.StatusUnauthorized
	case errors.Is(err, connector.ErrEnrollmentConflict), errors.Is(err, connector.ErrConnectorFenced), errors.Is(err, connector.ErrBastionNameConflict), errors.Is(err, connector.ErrCommandState), errors.Is(err, connector.ErrBastionState):
		return http.StatusConflict
	case errors.Is(err, connector.ErrConnectorArtifactInvalid):
		return http.StatusUnprocessableEntity
	case errors.Is(err, connector.ErrConnectorArtifactUnavailable):
		return http.StatusServiceUnavailable
	default:
		return resourceStatus(err)
	}
}
