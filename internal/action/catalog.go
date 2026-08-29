package action

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type PolicyInput struct {
	Name                string
	Enabled             bool
	ToolIDs             []string
	Risks               []string
	ResourceTypes       []string
	MinimumApprovers    int32
	SeparationOfDuty    bool
	ApproverRoleIDs     []uuid.UUID
	ExpiresAfterSeconds int32
	ExpectedVersion     int64
}

type ApprovalView struct {
	Request      db.ApprovalRequest
	Action       db.PendingAction
	Requirements []db.ApprovalRequirementSnapshot
	Decisions    []db.ApprovalDecision
}

type ExecutionView struct {
	Execution              db.Execution
	Action                 db.PendingAction
	OneTimeResultAvailable bool
}

func (service Service) ListPolicies(ctx context.Context, enterpriseID uuid.UUID) ([]db.ApprovalPolicy, error) {
	return service.Store.Queries.ListApprovalPolicies(ctx, enterpriseID)
}

func (service Service) CreatePolicy(ctx context.Context, actorID string, enterpriseID uuid.UUID, input PolicyInput, idempotencyKey string) (db.ApprovalPolicy, error) {
	return postgresExecute(ctx, service, actorID, "approval_policy.create", idempotencyKey, input, func(q *db.Queries) (db.ApprovalPolicy, error) {
		value, err := q.CreateApprovalPolicy(ctx, db.CreateApprovalPolicyParams{ID: newID(), EnterpriseID: enterpriseID, Name: input.Name,
			Enabled: input.Enabled, ToolIds: input.ToolIDs, Risks: input.Risks, ResourceTypes: input.ResourceTypes,
			MinimumApprovers: input.MinimumApprovers, SeparationOfDuty: input.SeparationOfDuty,
			ApproverRoleIds: input.ApproverRoleIDs, ExpiresAfterSeconds: expirySeconds(input.ExpiresAfterSeconds)})
		return value, err
	})
}

func (service Service) UpdatePolicy(ctx context.Context, enterpriseID, policyID uuid.UUID, input PolicyInput) (db.ApprovalPolicy, error) {
	return service.Store.Queries.UpdateApprovalPolicy(ctx, db.UpdateApprovalPolicyParams{ID: policyID, EnterpriseID: enterpriseID,
		Name: input.Name, Enabled: input.Enabled, ToolIds: input.ToolIDs, Risks: input.Risks, ResourceTypes: input.ResourceTypes,
		MinimumApprovers: input.MinimumApprovers, SeparationOfDuty: input.SeparationOfDuty,
		ApproverRoleIds: input.ApproverRoleIDs, ExpiresAfterSeconds: expirySeconds(input.ExpiresAfterSeconds), Version: input.ExpectedVersion})
}

func (service Service) ListApprovalViews(ctx context.Context, enterpriseID uuid.UUID, limit int32) ([]ApprovalView, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	requests, err := service.Store.Queries.ListApprovalRequests(ctx, db.ListApprovalRequestsParams{EnterpriseID: enterpriseID, Limit: limit})
	if err != nil {
		return nil, err
	}
	result := make([]ApprovalView, 0, len(requests))
	for _, request := range requests {
		view, err := service.approvalView(ctx, request)
		if err != nil {
			return nil, err
		}
		result = append(result, view)
	}
	return result, nil
}

func (service Service) GetApprovalView(ctx context.Context, enterpriseID, requestID uuid.UUID) (ApprovalView, error) {
	request, err := service.Store.Queries.GetApprovalRequest(ctx, db.GetApprovalRequestParams{ID: requestID, EnterpriseID: enterpriseID})
	if err != nil {
		return ApprovalView{}, err
	}
	return service.approvalView(ctx, request)
}

func (service Service) approvalView(ctx context.Context, request db.ApprovalRequest) (ApprovalView, error) {
	action, err := service.Store.Queries.GetPendingActionByID(ctx, db.GetPendingActionByIDParams{ID: request.PendingActionID, EnterpriseID: request.EnterpriseID})
	if err != nil {
		return ApprovalView{}, err
	}
	requirements, err := service.Store.Queries.ListApprovalRequirements(ctx, db.ListApprovalRequirementsParams{ApprovalRequestID: request.ID, EnterpriseID: request.EnterpriseID})
	if err != nil {
		return ApprovalView{}, err
	}
	decisions, err := service.Store.Queries.ListApprovalDecisions(ctx, db.ListApprovalDecisionsParams{ApprovalRequestID: request.ID, EnterpriseID: request.EnterpriseID})
	return ApprovalView{Request: request, Action: action, Requirements: requirements, Decisions: decisions}, err
}

func (service Service) ListExecutionViews(ctx context.Context, enterpriseID uuid.UUID, limit int32) ([]ExecutionView, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	items, err := service.Store.Queries.ListExecutions(ctx, db.ListExecutionsParams{EnterpriseID: enterpriseID, Limit: limit})
	if err != nil {
		return nil, err
	}
	result := make([]ExecutionView, 0, len(items))
	for _, execution := range items {
		action, err := service.Store.Queries.GetPendingActionByID(ctx, db.GetPendingActionByIDParams{ID: execution.PendingActionID, EnterpriseID: enterpriseID})
		if err != nil {
			return nil, err
		}
		available, err := service.Store.Queries.HasExecutionOneTimeResult(ctx, db.HasExecutionOneTimeResultParams{ExecutionID: execution.ID, EnterpriseID: enterpriseID})
		if err != nil {
			return nil, err
		}
		result = append(result, ExecutionView{Execution: execution, Action: action, OneTimeResultAvailable: available})
	}
	return result, nil
}

func (service Service) GetExecutionView(ctx context.Context, enterpriseID, executionID uuid.UUID) (ExecutionView, error) {
	execution, err := service.Store.Queries.GetExecution(ctx, db.GetExecutionParams{ID: executionID, EnterpriseID: enterpriseID})
	if err != nil {
		return ExecutionView{}, err
	}
	action, err := service.Store.Queries.GetPendingActionByID(ctx, db.GetPendingActionByIDParams{ID: execution.PendingActionID, EnterpriseID: enterpriseID})
	if err != nil {
		return ExecutionView{}, err
	}
	available, err := service.Store.Queries.HasExecutionOneTimeResult(ctx, db.HasExecutionOneTimeResultParams{ExecutionID: execution.ID, EnterpriseID: enterpriseID})
	return ExecutionView{Execution: execution, Action: action, OneTimeResultAvailable: available}, err
}

func postgresExecute[T any](ctx context.Context, service Service, actorID, operation, key string, input any, fn func(*db.Queries) (T, error)) (T, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, operation, key, input, 201, fn)
}

func expirySeconds(value int32) int32 {
	if value < 60 {
		return int32((24 * time.Hour) / time.Second)
	}
	return value
}
