package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	actionservice "github.com/kakj-go/Argus/internal/action"
	hostapi "github.com/kakj-go/Argus/internal/gen/openapi/hostapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	telemetryservice "github.com/kakj-go/Argus/internal/telemetry"
)

type HostOnboardingActionService interface {
	PreviewEnrollmentRotate(context.Context, resource.Subject, uuid.UUID, uuid.UUID, int64, string) (db.PendingAction, error)
	PreviewUninstallCommand(context.Context, resource.Subject, uuid.UUID, uuid.UUID, int64, string) (db.PendingAction, error)
}

type HostHandler struct {
	Identity   EnterpriseIdentityHandler
	Service    resource.Service
	Queries    *db.Queries
	Onboarding HostOnboardingActionService
}

func (handler HostHandler) ListHosts(ctx context.Context, _ hostapi.ListHostsRequestObject) (hostapi.ListHostsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "host.read")
	if apiError != nil {
		return hostapi.ListHostsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListHosts(ctx, p.EnterpriseIDValue(), p.AuthorizedResourceIDs)
	if err != nil {
		return hostapi.ListHostsdefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	converted := make([]hostapi.Host, 0, len(items))
	probeStates := handler.probeStates(ctx, items)
	onboarding := loadHostOnboarding(ctx, handler.Queries, p.EnterpriseIDValue(), items)
	for _, item := range items {
		converted = append(converted, toHost(item, probeStates[item.ID], onboarding[item.ID]))
	}
	return hostapi.ListHosts200JSONResponse{Items: converted, Page: emptyHostPage()}, nil
}

// probeStates 批量读取主机的实时探活状态;查询失败时退化为不展示(不影响列表)。
func (handler HostHandler) probeStates(ctx context.Context, items []db.Host) map[uuid.UUID]db.HostProbeState {
	if handler.Queries == nil || len(items) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	states, err := handler.Queries.ListHostProbeStatesByHosts(ctx, ids)
	if err != nil {
		return nil
	}
	result := make(map[uuid.UUID]db.HostProbeState, len(states))
	for _, state := range states {
		result[state.HostID] = state
	}
	return result
}

func (handler HostHandler) GetHost(ctx context.Context, request hostapi.GetHostRequestObject) (hostapi.GetHostResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "host.read")
	if apiError != nil {
		return hostapi.GetHostdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	item, err := handler.Service.GetHost(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id), p.AuthorizedResourceIDs)
	if err != nil {
		return hostapi.GetHostdefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	var probeState db.HostProbeState
	if handler.Queries != nil {
		probeState, _ = handler.Queries.GetHostProbeState(ctx, uuid.UUID(request.Id))
	}
	onboarding := loadHostOnboarding(ctx, handler.Queries, p.EnterpriseIDValue(), []db.Host{item})
	return hostapi.GetHost200JSONResponse(toHost(item, probeState, onboarding[item.ID])), nil
}

func (handler HostHandler) CreateHostConnectionTest(ctx context.Context, request hostapi.CreateHostConnectionTestRequestObject) (hostapi.CreateHostConnectionTestResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "host.test")
	if apiError != nil {
		return hostapi.CreateHostConnectionTestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return hostapi.CreateHostConnectionTestdefaultJSONResponse{Body: hostError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := resource.HostInput{Address: request.Body.Address, Port: int32(request.Body.Port), Platform: string(request.Body.Platform), ConnectionMode: string(request.Body.ConnectionMode),
		BastionScopeID: optionalUUID(request.Body.BastionScopeId), CredentialID: uuid.NullUUID{UUID: uuid.UUID(request.Body.CredentialId), Valid: true}, Username: request.Body.Username}
	test, err := handler.Service.CreateHostConnectionTest(ctx, resourceSubject(p), p.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	if err != nil {
		return hostapi.CreateHostConnectionTestdefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return hostapi.CreateHostConnectionTest202JSONResponse(toHostConnectionTest(test)), nil
}

func (handler HostHandler) PreviewCreateHost(ctx context.Context, request hostapi.PreviewCreateHostRequestObject) (hostapi.PreviewCreateHostResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "host.manage")
	if apiError != nil {
		return hostapi.PreviewCreateHostdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return hostapi.PreviewCreateHostdefaultJSONResponse{Body: hostError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := hostCreateInput(*request.Body)
	action, err := handler.Service.PreviewCreateHost(ctx, resourceSubject(p), p.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	if err != nil {
		return hostapi.PreviewCreateHostdefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return hostapi.PreviewCreateHost201JSONResponse(pendingForHost(action)), nil
}

// hostCreateInput 将可选的连接字段转换为 HostInput;self_enrolled 模式下
// 地址/端口/凭据/账号/连接测试均不填写。
func hostCreateInput(value hostapi.HostPreviewCreate) resource.HostInput {
	input := resource.HostInput{Name: value.Name, Platform: string(value.Platform), ConnectionMode: string(value.ConnectionMode),
		Environment: string(value.Environment), Labels: stringMap(value.Labels), BastionScopeID: optionalUUID(value.BastionScopeId)}
	if value.Hostname != nil {
		input.Hostname = *value.Hostname
	}
	if value.Address != nil {
		input.Address = *value.Address
	}
	if value.Port != nil {
		input.Port = int32(*value.Port)
	}
	if value.Architecture != nil {
		input.Architecture = string(*value.Architecture)
	}
	if value.CredentialId != nil {
		input.CredentialID = uuid.NullUUID{UUID: uuid.UUID(*value.CredentialId), Valid: true}
	}
	if value.Username != nil {
		input.Username = *value.Username
	}
	if value.ConnectionTestId != nil {
		input.ConnectionTestID = uuid.NullUUID{UUID: uuid.UUID(*value.ConnectionTestId), Valid: true}
	}
	return input
}

func (handler HostHandler) PreviewHostEnrollmentRotate(ctx context.Context, request hostapi.PreviewHostEnrollmentRotateRequestObject) (hostapi.PreviewHostEnrollmentRotateResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "host.manage")
	if apiError != nil {
		return hostapi.PreviewHostEnrollmentRotatedefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil || handler.Onboarding == nil {
		return hostapi.PreviewHostEnrollmentRotatedefaultJSONResponse{Body: hostError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Onboarding.PreviewEnrollmentRotate(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Body.ExpectedVersion, request.Params.IdempotencyKey)
	if err != nil {
		return hostapi.PreviewHostEnrollmentRotatedefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return hostapi.PreviewHostEnrollmentRotate201JSONResponse(pendingForHost(action)), nil
}

func (handler HostHandler) PreviewHostUninstallCommand(ctx context.Context, request hostapi.PreviewHostUninstallCommandRequestObject) (hostapi.PreviewHostUninstallCommandResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "host.manage")
	if apiError != nil {
		return hostapi.PreviewHostUninstallCommanddefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil || handler.Onboarding == nil {
		return hostapi.PreviewHostUninstallCommanddefaultJSONResponse{Body: hostError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Onboarding.PreviewUninstallCommand(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Body.ExpectedVersion, request.Params.IdempotencyKey)
	if err != nil {
		return hostapi.PreviewHostUninstallCommanddefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return hostapi.PreviewHostUninstallCommand201JSONResponse(pendingForHost(action)), nil
}

func (handler HostHandler) PreviewUpdateHost(ctx context.Context, request hostapi.PreviewUpdateHostRequestObject) (hostapi.PreviewUpdateHostResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "host.manage")
	if apiError != nil {
		return hostapi.PreviewUpdateHostdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return hostapi.PreviewUpdateHostdefaultJSONResponse{Body: hostError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	input := hostUpdateInput(*request.Body)
	action, err := handler.Service.PreviewUpdateHost(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), input, request.Params.IdempotencyKey)
	if err != nil {
		return hostapi.PreviewUpdateHostdefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return hostapi.PreviewUpdateHost201JSONResponse(pendingForHost(action)), nil
}

func (handler HostHandler) PreviewDeleteHost(ctx context.Context, request hostapi.PreviewDeleteHostRequestObject) (hostapi.PreviewDeleteHostResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "host.manage")
	if apiError != nil {
		return hostapi.PreviewDeleteHostdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	if request.Body == nil {
		return hostapi.PreviewDeleteHostdefaultJSONResponse{Body: hostError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	action, err := handler.Service.PreviewDeleteHost(ctx, resourceSubject(p), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Body.ExpectedVersion, request.Params.IdempotencyKey)
	if err != nil {
		return hostapi.PreviewDeleteHostdefaultJSONResponse{Body: hostError(ctx, err), StatusCode: resourceStatus(err)}, nil
	}
	return hostapi.PreviewDeleteHost201JSONResponse(pendingForHost(action)), nil
}

func (handler HostHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *hostapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &hostapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]hostapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func resourceSubject(value identity.Principal) resource.Subject {
	return resource.Subject{ActorID: value.ActorID(), AuthorizationVersion: value.AuthorizationVersion(), AuthorizedResourceIDs: value.AuthorizedResourceIDs}
}

func hostUpdateInput(value hostapi.HostPreviewUpdate) resource.HostInput {
	result := resource.HostInput{ExpectedVersion: value.ExpectedVersion, BastionScopeID: optionalUUID(value.BastionScopeId),
		ConnectionTestID: optionalUUID(value.ConnectionTestId)}
	if value.Name != nil {
		result.Name = *value.Name
	}
	if value.Hostname != nil {
		result.Hostname = *value.Hostname
	}
	if value.Address != nil {
		result.Address = *value.Address
	}
	if value.Port != nil {
		result.Port = int32(*value.Port)
	}
	if value.ConnectionMode != nil {
		result.ConnectionMode = string(*value.ConnectionMode)
	}
	if value.Environment != nil {
		result.Environment = string(*value.Environment)
	}
	if value.Labels != nil {
		result.Labels = stringMap(*value.Labels)
	}
	return result
}

func toHost(value db.Host, probe db.HostProbeState, onboarding onboardingView) hostapi.Host {
	labels, _ := resource.DecodeLabels(value.Labels)
	result := hostapi.Host{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerUUID(value.EnterpriseID), Name: value.Name, Address: value.Address, Port: int(value.Port),
		Platform: hostapi.HostPlatform(value.Platform), ConnectionMode: hostapi.HostConnectionMode(value.ConnectionMode), Environment: hostapi.Environment(value.Environment),
		Labels: hostapi.Labels(labels), LabelsVersion: value.LabelsVersion, ResourceVersion: value.ResourceVersion, ConnectionStatus: hostapi.HostConnectionStatus(value.ConnectionStatus),
		Status: hostapi.HostStatus(value.Status), Onboarding: toHostOnboarding(onboarding), CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.Hostname != "" {
		result.Hostname = &value.Hostname
	}
	if value.BastionScopeID.Valid {
		id := openapi_types.UUID(value.BastionScopeID.UUID)
		result.BastionScopeId = &id
	}
	if value.ConnectorID.Valid {
		id := openapi_types.UUID(value.ConnectorID.UUID)
		result.ConnectorId = &id
	}
	if value.PinnedHostKey != "" {
		result.PinnedHostKey = &value.PinnedHostKey
	}
	if value.Architecture.Valid {
		architecture := hostapi.HostArchitecture(value.Architecture.String)
		result.Architecture = &architecture
	}
	if probe.HostID != uuid.Nil {
		liveStatus := hostapi.HostLiveStatus(probe.Status)
		result.LiveStatus = &liveStatus
		probeLatency := int(probe.LatencyMs)
		result.ProbeLatencyMs = &probeLatency
		probedAt := probe.LastCheckedAt.Time
		result.LastProbeAt = &probedAt
	}
	if value.LastSeenAt.Valid {
		result.LastSeenAt = &value.LastSeenAt.Time
	}
	return result
}

func toHostOnboarding(value onboardingView) hostapi.OnboardingProjection {
	result := hostapi.OnboardingProjection{State: hostapi.OnboardingProjectionState(value.State), UpdatedAt: value.UpdatedAt}
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

func toHostConnectionTest(value db.ConnectionTest) hostapi.ConnectionTest {
	var resultValue resource.ConnectionTestResult
	_ = json.Unmarshal(value.Result, &resultValue)
	checks := make([]struct {
		Detail *string                            `json:"detail,omitempty"`
		Name   string                             `json:"name"`
		Status hostapi.ConnectionTestChecksStatus `json:"status"`
	}, 0, len(resultValue.Checks))
	for _, check := range resultValue.Checks {
		name, status, detail := check["name"], check["status"], check["detail"]
		item := struct {
			Detail *string                            `json:"detail,omitempty"`
			Name   string                             `json:"name"`
			Status hostapi.ConnectionTestChecksStatus `json:"status"`
		}{Name: name, Status: hostapi.ConnectionTestChecksStatus(status)}
		if detail != "" {
			item.Detail = &detail
		}
		checks = append(checks, item)
	}
	result := hostapi.ConnectionTest{Id: openapi_types.UUID(value.ID), EnterpriseId: pointerUUID(value.EnterpriseID), TargetType: hostapi.ConnectionTestTargetType(value.TargetType),
		Path: hostapi.ConnectionTestPath(value.Path), Status: hostapi.ConnectionTestStatus(value.Status), Checks: checks, ExpiresAt: value.ExpiresAt.Time,
		CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.ResourceID.Valid {
		id := openapi_types.UUID(value.ResourceID.UUID)
		result.ResourceId = &id
	}
	if resultValue.LatencyMS > 0 {
		latency := int(resultValue.LatencyMS)
		result.LatencyMs = &latency
	}
	if len(resultValue.ResolvedIPs) > 0 {
		result.ResolvedIps = &resultValue.ResolvedIPs
	}
	if resultValue.HostKeyFingerprint != "" {
		result.HostKeyFingerprint = &resultValue.HostKeyFingerprint
	}
	if resultValue.RemoteVersion != "" {
		result.RemoteVersion = &resultValue.RemoteVersion
	}
	if value.ErrorCode.Valid {
		result.ErrorCode = &value.ErrorCode.String
	}
	return result
}

func pendingForHost(value db.PendingAction) hostapi.PendingActionPublicSchema {
	return convertPending[hostapi.PendingActionPublicSchema](value)
}

func convertPending[T any](value db.PendingAction) T {
	var preview any = map[string]any{}
	var diff any = []any{}
	_ = json.Unmarshal(value.Preview, &preview)
	_ = json.Unmarshal(value.Diff, &diff)
	available := []string{}
	switch value.Status {
	case "awaiting_confirmation":
		available = []string{"confirm", "cancel"}
	case "awaiting_approval":
		available = []string{"approve", "reject", "cancel"}
	}
	body := map[string]any{"schema_version": "argus.pending_action/v1", "action_ref": value.ActionRef, "action_type": value.ActionType, "title": value.Title, "summary": value.Summary,
		"risk": value.Risk, "preview": preview, "diff": diff, "status": value.Status, "available_actions": available,
		"expires_at": value.ExpiresAt.Time, "created_at": value.CreatedAt.Time, "updated_at": value.UpdatedAt.Time}
	if value.ResultSummary != "" {
		body["result_summary"] = value.ResultSummary
	}
	encoded, _ := json.Marshal(body)
	var result T
	_ = json.Unmarshal(encoded, &result)
	return result
}

func optionalUUID(value *openapi_types.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: uuid.UUID(*value), Valid: true}
}

func stringMap[T ~string](value map[string]T) map[string]string {
	result := make(map[string]string, len(value))
	for key, item := range value {
		result[key] = string(item)
	}
	return result
}

func emptyHostPage() hostapi.CursorPage {
	return hostapi.CursorPage{NextCursor: nil, HasMore: false, Partial: hostapi.PartialMetadata{Partial: false, Reasons: []hostapi.PartialMetadataReasons{}}}
}

func hostError(ctx context.Context, err error) hostapi.ApiError {
	base := hostErrorBase(ctx, err)
	logMappedError(ctx, base.Code, err)
	return base
}

func hostErrorBase(ctx context.Context, err error) hostapi.ApiError {
	base := setupErrorBase(ctx, err)
	switch {
	case errors.Is(err, resource.ErrResourceDenied):
		base.Code, base.MessageKey = "RESOURCE_NOT_FOUND", "errors.common.resource_not_found"
	case errors.Is(err, resource.ErrVersionConflict):
		base.Code, base.MessageKey = "VERSION_CONFLICT", "errors.common.version_conflict"
	case errors.Is(err, resource.ErrConnectionTestNeeded):
		base.Code, base.MessageKey = "CONNECTION_TEST_REQUIRED", "errors.connection_test.required"
	case errors.Is(err, resource.ErrDirectTargetDenied):
		base.Code, base.MessageKey = "DIRECT_TARGET_DENIED", "errors.direct.target_denied"
	case errors.Is(err, resource.ErrWinRMTLSRequired):
		base.Code, base.MessageKey = "WINRM_TLS_REQUIRED", "errors.remote_access.winrm_tls_required"
	case errors.Is(err, resource.ErrActionInvalidated):
		base.Code, base.MessageKey = "PENDING_ACTION_INVALIDATED", "errors.actions.pending_action_invalidated"
	case errors.Is(err, resource.ErrSelfEnrollUnsupported):
		base.Code, base.MessageKey = "HOST_SELF_ENROLL_UNSUPPORTED_PLATFORM", "errors.host_install.unsupported_platform"
	case errors.Is(err, resource.ErrSelfEnrollConflictingInput):
		base.Code, base.MessageKey = "INVALID_ARGUMENT", "errors.common.invalid_argument"
	case errors.Is(err, telemetryservice.ErrDistributionPending):
		base.Code, base.MessageKey = "COLLECTOR_DISTRIBUTION_VALIDATION_PENDING", "errors.telemetry.distribution_validation_pending"
	case errors.Is(err, telemetryservice.ErrCollectorArtifactUnavailable):
		base.Code, base.MessageKey = "COLLECTOR_ARTIFACT_UNAVAILABLE", "errors.telemetry.artifact_unavailable"
		base.Retryable = retryablePointer(true)
	case errors.Is(err, telemetryservice.ErrSelfEnrolledOperationUnsupported):
		base.Code, base.MessageKey = "HOST_OPERATION_UNSUPPORTED_FOR_SELF_ENROLLED", "errors.host_install.operation_unsupported"
	case errors.Is(err, telemetryservice.ErrHostInstallTokenInvalid):
		base.Code, base.MessageKey = "HOST_INSTALL_TOKEN_INVALID", "errors.host_install.token_invalid"
	case errors.Is(err, telemetryservice.ErrHostInstallTokenConflict):
		base.Code, base.MessageKey = "HOST_INSTALL_TOKEN_CONFLICT", "errors.host_install.token_conflict"
	case errors.Is(err, telemetryservice.ErrHostUninstallTokenInvalid):
		base.Code, base.MessageKey = "HOST_UNINSTALL_TOKEN_INVALID", "errors.host_uninstall.token_invalid"
	case errors.Is(err, telemetryservice.ErrHostUninstallTokenConflict):
		base.Code, base.MessageKey = "HOST_UNINSTALL_TOKEN_CONFLICT", "errors.host_uninstall.token_conflict"
	}
	return hostapi.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey,
		Params: copyErrorParams[map[string]hostapi.ApiError_Params_AdditionalProperties](base.Params), RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}

func resourceStatus(err error) int {
	switch {
	case errors.Is(err, pgx.ErrNoRows), errors.Is(err, resource.ErrResourceDenied):
		return http.StatusNotFound
	case errors.Is(err, resource.ErrDirectTargetDenied):
		return http.StatusForbidden
	case errors.Is(err, actionservice.ErrStepUpRequired):
		return http.StatusForbidden
	case errors.Is(err, resource.ErrConnectionTestNeeded), errors.Is(err, resource.ErrWinRMTLSRequired), errors.Is(err, resource.ErrSelfEnrollUnsupported):
		return http.StatusUnprocessableEntity
	case errors.Is(err, telemetryservice.ErrCollectorArtifactUnavailable):
		return http.StatusServiceUnavailable
	default:
		return http.StatusConflict
	}
}
