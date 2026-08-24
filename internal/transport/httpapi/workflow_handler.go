package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	actionservice "github.com/kakj-go/Argus/internal/action"
	workflowapi "github.com/kakj-go/Argus/internal/gen/openapi/workflowapi"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type WorkflowHandler struct {
	Identity EnterpriseIdentityHandler
	Service  actionservice.Service
}

func (handler WorkflowHandler) ListApprovalPolicies(ctx context.Context, _ workflowapi.ListApprovalPoliciesRequestObject) (workflowapi.ListApprovalPoliciesResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "approval_policy.read")
	if apiError != nil {
		return workflowapi.ListApprovalPoliciesdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListPolicies(ctx, p.EnterpriseIDValue())
	if err != nil {
		return workflowapi.ListApprovalPoliciesdefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	result := make([]workflowapi.ApprovalPolicy, 0, len(items))
	for _, item := range items {
		result = append(result, toWorkflowPolicy(item))
	}
	return workflowapi.ListApprovalPolicies200JSONResponse(result), nil
}

func (handler WorkflowHandler) CreateApprovalPolicy(ctx context.Context, request workflowapi.CreateApprovalPolicyRequestObject) (workflowapi.CreateApprovalPolicyResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "approval_policy.manage")
	if apiError != nil {
		return workflowapi.CreateApprovalPolicydefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	input := workflowPolicyInput(*request.Body)
	value, err := handler.Service.CreatePolicy(ctx, p.ActorID(), p.EnterpriseIDValue(), input, request.Params.IdempotencyKey)
	if err != nil {
		return workflowapi.CreateApprovalPolicydefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	return workflowapi.CreateApprovalPolicy201JSONResponse(toWorkflowPolicy(value)), nil
}

func (handler WorkflowHandler) UpdateApprovalPolicy(ctx context.Context, request workflowapi.UpdateApprovalPolicyRequestObject) (workflowapi.UpdateApprovalPolicyResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "approval_policy.manage")
	if apiError != nil {
		return workflowapi.UpdateApprovalPolicydefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.UpdatePolicy(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id), workflowPolicyInput(*request.Body))
	if err != nil {
		return workflowapi.UpdateApprovalPolicydefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	return workflowapi.UpdateApprovalPolicy200JSONResponse(toWorkflowPolicy(value)), nil
}

func (handler WorkflowHandler) ListApprovalRequests(ctx context.Context, _ workflowapi.ListApprovalRequestsRequestObject) (workflowapi.ListApprovalRequestsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "approval.read")
	if apiError != nil {
		return workflowapi.ListApprovalRequestsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListApprovalViews(ctx, p.EnterpriseIDValue(), 100)
	if err != nil {
		return workflowapi.ListApprovalRequestsdefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	result := make([]workflowapi.ApprovalRequestView, 0, len(items))
	for _, item := range items {
		result = append(result, toWorkflowApprovalView(item))
	}
	return workflowapi.ListApprovalRequests200JSONResponse(result), nil
}

func (handler WorkflowHandler) GetApprovalRequest(ctx context.Context, request workflowapi.GetApprovalRequestRequestObject) (workflowapi.GetApprovalRequestResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "approval.read")
	if apiError != nil {
		return workflowapi.GetApprovalRequestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetApprovalView(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return workflowapi.GetApprovalRequestdefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	return workflowapi.GetApprovalRequest200JSONResponse(toWorkflowApprovalView(value)), nil
}

func (handler WorkflowHandler) DecideApprovalRequest(ctx context.Context, request workflowapi.DecideApprovalRequestRequestObject) (workflowapi.DecideApprovalRequestResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "approval.decide")
	if apiError != nil {
		return workflowapi.DecideApprovalRequestdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	reason := ""
	if request.Body.Reason != nil {
		reason = *request.Body.Reason
	}
	_, err := handler.Service.Decide(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), string(request.Body.Decision), reason, request.Params.IdempotencyKey)
	if err != nil {
		return workflowapi.DecideApprovalRequestdefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	value, err := handler.Service.GetApprovalView(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return workflowapi.DecideApprovalRequestdefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	return workflowapi.DecideApprovalRequest200JSONResponse(toWorkflowApprovalView(value)), nil
}

func (handler WorkflowHandler) ListExecutions(ctx context.Context, _ workflowapi.ListExecutionsRequestObject) (workflowapi.ListExecutionsResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "execution.read")
	if apiError != nil {
		return workflowapi.ListExecutionsdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	items, err := handler.Service.ListExecutionViews(ctx, p.EnterpriseIDValue(), 100)
	if err != nil {
		return workflowapi.ListExecutionsdefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	result := make([]workflowapi.Execution, 0, len(items))
	for _, item := range items {
		result = append(result, toWorkflowExecution(item))
	}
	return workflowapi.ListExecutions200JSONResponse{Items: result, Page: emptyWorkflowPage()}, nil
}

func (handler WorkflowHandler) GetExecution(ctx context.Context, request workflowapi.GetExecutionRequestObject) (workflowapi.GetExecutionResponseObject, error) {
	p, apiError := handler.auth(ctx, false, "", "execution.read")
	if apiError != nil {
		return workflowapi.GetExecutiondefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.GetExecutionView(ctx, p.EnterpriseIDValue(), uuid.UUID(request.Id))
	if err != nil {
		return workflowapi.GetExecutiondefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	return workflowapi.GetExecution200JSONResponse(toWorkflowExecution(value)), nil
}

func (handler WorkflowHandler) ClaimExecutionOneTimeResult(ctx context.Context, request workflowapi.ClaimExecutionOneTimeResultRequestObject) (workflowapi.ClaimExecutionOneTimeResultResponseObject, error) {
	p, apiError := handler.auth(ctx, true, request.Params.XCSRFToken, "execution.read")
	if apiError != nil {
		return workflowapi.ClaimExecutionOneTimeResultdefaultJSONResponse{Body: *apiError, StatusCode: http.StatusForbidden}, nil
	}
	value, err := handler.Service.ClaimOneTimeResult(ctx, p.ActorID(), p.EnterpriseIDValue(), uuid.UUID(request.Id), request.Params.IdempotencyKey)
	if err != nil {
		return workflowapi.ClaimExecutionOneTimeResultdefaultJSONResponse{Body: workflowError(ctx, err), StatusCode: workflowStatus(err)}, nil
	}
	command := value.Enrollment.InstallCommand
	return workflowapi.ClaimExecutionOneTimeResult200JSONResponse{
		SchemaVersion: workflowapi.ArgusActionOneTimeResultv1,
		ExecutionId:   value.ExecutionID,
		ResultKind:    workflowapi.ConnectorEnrollment,
		Enrollment:    workflowapi.EnrollmentResult{EnrollmentId: value.Enrollment.EnrollmentID, InstallCommand: &command, ExpiresAt: value.Enrollment.ExpiresAt},
		ExpiresAt:     value.ExpiresAt,
	}, nil
}

func (handler WorkflowHandler) auth(ctx context.Context, mutation bool, csrf, permission string) (identity.Principal, *workflowapi.ApiError) {
	p, value := handler.Identity.enterprisePrincipal(ctx, mutation, csrf, permission)
	if value == nil {
		return p, nil
	}
	return identity.Principal{}, &workflowapi.ApiError{Code: value.Code, Message: value.Message, MessageKey: value.MessageKey,
		Params: copyErrorParams[map[string]workflowapi.ApiError_Params_AdditionalProperties](value.Params), RequestId: value.RequestId,
		Retryable: value.Retryable, TraceId: value.TraceId}
}

func workflowPolicyInput(value workflowapi.ApprovalPolicyWrite) actionservice.PolicyInput {
	risks := make([]string, len(value.Risks))
	for index, risk := range value.Risks {
		risks[index] = string(risk)
	}
	expires := int32(0)
	if value.ExpiresAfterSeconds != nil {
		expires = int32(*value.ExpiresAfterSeconds)
	}
	selector, _ := json.Marshal(value.LabelSelector)
	if value.LabelSelector == nil {
		selector = nil
	}
	return actionservice.PolicyInput{Name: value.Name, Enabled: value.Enabled, ToolIDs: value.ToolIds, Risks: risks,
		ResourceTypes: value.ResourceTypes, LabelSelector: selector, MinimumApprovers: int32(value.MinimumApprovers),
		SeparationOfDuty: value.SeparationOfDuty, ApproverRoleIDs: value.ApproverRoleIds, ExpiresAfterSeconds: expires,
		ExpectedVersion: value.ExpectedVersion}
}

func toWorkflowPolicy(value db.ApprovalPolicy) workflowapi.ApprovalPolicy {
	risks := make([]workflowapi.ApprovalPolicyRisks, len(value.Risks))
	for index, risk := range value.Risks {
		risks[index] = workflowapi.ApprovalPolicyRisks(risk)
	}
	result := workflowapi.ApprovalPolicy{Id: value.ID, Name: value.Name, Enabled: value.Enabled, ToolIds: value.ToolIds,
		Risks: risks, ResourceTypes: value.ResourceTypes, MinimumApprovers: int(value.MinimumApprovers), SeparationOfDuty: value.SeparationOfDuty,
		ApproverRoleIds: value.ApproverRoleIds, Version: value.Version, CreatedAt: value.CreatedAt.Time, UpdatedAt: value.UpdatedAt.Time}
	if value.ExpiresAfterSeconds > 0 {
		expires := int(value.ExpiresAfterSeconds)
		result.ExpiresAfterSeconds = &expires
	}
	if len(value.LabelSelector) > 0 {
		var selector workflowapi.LabelSelector
		if json.Unmarshal(value.LabelSelector, &selector) == nil {
			result.LabelSelector = &selector
		}
	}
	return result
}

func toWorkflowApprovalView(value actionservice.ApprovalView) workflowapi.ApprovalRequestView {
	requirements := make([]workflowapi.ApprovalRequirement, 0, len(value.Requirements))
	for _, item := range value.Requirements {
		requirements = append(requirements, workflowapi.ApprovalRequirement{PolicyId: item.PolicyID, PolicyVersion: item.PolicyVersion,
			MinimumApprovers: int(item.MinimumApprovers), ApprovedCount: int(item.ApprovedCount), SeparationOfDuty: item.SeparationOfDuty,
			Status: workflowapi.ApprovalRequirementStatus(item.Status)})
	}
	decisions := make([]workflowapi.ApprovalDecision, 0, len(value.Decisions))
	for _, item := range value.Decisions {
		reason := item.Reason
		decisions = append(decisions, workflowapi.ApprovalDecision{DecisionId: item.ID.String(), ActorUserId: item.ActorUserID.String(),
			Decision: item.Decision, Reason: &reason, DecidedAt: item.DecidedAt.Time})
	}
	return workflowapi.ApprovalRequestView{ApprovalRequestId: value.Request.ID, ActionRef: value.Action.ActionRef,
		Status: workflowapi.ApprovalRequestViewStatus(value.Request.Status), Requirements: requirements, Decisions: decisions,
		ExpiresAt: value.Request.ExpiresAt.Time, CreatedAt: value.Request.CreatedAt.Time, UpdatedAt: value.Request.UpdatedAt.Time}
}

func toWorkflowExecution(value actionservice.ExecutionView) workflowapi.Execution {
	available := value.OneTimeResultAvailable
	result := workflowapi.Execution{ExecutionId: value.Execution.ID.String(), ActionRef: value.Action.ActionRef,
		Status: value.Execution.Status, OneTimeResultAvailable: &available, CreatedAt: value.Execution.CreatedAt.Time, UpdatedAt: value.Execution.UpdatedAt.Time}
	if value.Execution.ResultRef.Valid {
		result.ResultRef = &value.Execution.ResultRef.String
	}
	if value.Execution.ErrorCode.Valid {
		result.ErrorCode = &value.Execution.ErrorCode.String
	}
	return result
}

func workflowError(ctx context.Context, err error) workflowapi.ApiError {
	code, key := "INTERNAL_ERROR", "errors.common.internal"
	defer func() { logMappedError(ctx, code, err) }()
	switch {
	case errors.Is(err, actionservice.ErrInvalidated):
		code, key = "ACTION_INVALIDATED", "errors.actions.invalidated"
	case errors.Is(err, actionservice.ErrApprovalIneligible):
		code, key = "APPROVAL_NOT_ELIGIBLE", "errors.approval.not_eligible"
	case errors.Is(err, actionservice.ErrApprovalRequired):
		code, key = "APPROVAL_POLICY_REQUIRED", "errors.approval.policy_required"
	case errors.Is(err, actionservice.ErrOneTimeResultNotAvailable):
		code, key = "ACTION_RESULT_NOT_AVAILABLE", "errors.execution.one_time_result_not_available"
	case errors.Is(err, actionservice.ErrOneTimeResultExpired):
		code, key = "ACTION_RESULT_EXPIRED", "errors.execution.one_time_result_expired"
	case errors.Is(err, actionservice.ErrOneTimeResultConsumed):
		code, key = "ACTION_RESULT_ALREADY_CONSUMED", "errors.execution.one_time_result_consumed"
	case errors.Is(err, postgres.ErrIdempotencyExpired):
		code, key = "IDEMPOTENCY_RESULT_EXPIRED", "errors.common.idempotency_result_expired"
	case errors.Is(err, pgx.ErrNoRows):
		code, key = "RESOURCE_NOT_FOUND", "errors.common.resource_not_found"
	}
	requestID := "server-generated-request"
	if current, ok := RequestFromContext(ctx); ok {
		requestID = current.RequestID
	}
	return workflowapi.ApiError{Code: code, MessageKey: key, RequestId: requestID}
}

func workflowStatus(err error) int {
	switch {
	case errors.Is(err, actionservice.ErrApprovalIneligible):
		return http.StatusForbidden
	case errors.Is(err, actionservice.ErrOneTimeResultNotAvailable):
		return http.StatusNotFound
	case errors.Is(err, actionservice.ErrOneTimeResultExpired):
		return http.StatusGone
	case errors.Is(err, actionservice.ErrOneTimeResultConsumed), errors.Is(err, postgres.ErrIdempotencyExpired):
		return http.StatusConflict
	case errors.Is(err, actionservice.ErrInvalidated), errors.Is(err, actionservice.ErrUnavailable):
		return http.StatusConflict
	case errors.Is(err, pgx.ErrNoRows):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

func emptyWorkflowPage() workflowapi.CursorPage {
	return workflowapi.CursorPage{HasMore: false, Partial: workflowapi.PartialMetadata{Partial: false, Reasons: []workflowapi.PartialMetadataReasons{}}}
}
