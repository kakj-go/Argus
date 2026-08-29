package action

import (
	"context"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func approvalPolicyQuery(enterpriseID uuid.UUID, action db.PendingAction, plan db.PendingActionPlan) db.ListMatchingApprovalPoliciesParams {
	return db.ListMatchingApprovalPoliciesParams{
		EnterpriseID: enterpriseID,
		Column2:      action.Risk,
		Column3:      plan.CommitTool,
		Column4:      action.ResourceType,
	}
}

func matchingApprovalPolicies(_ context.Context, _ *db.Queries, _ db.PendingAction, _ db.PendingActionPlan, candidates []db.ApprovalPolicy) ([]db.ApprovalPolicy, error) {
	return candidates, nil
}
