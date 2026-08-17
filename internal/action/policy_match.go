package action

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/resource"
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

func matchingApprovalPolicies(ctx context.Context, q *db.Queries, action db.PendingAction, plan db.PendingActionPlan, candidates []db.ApprovalPolicy) ([]db.ApprovalPolicy, error) {
	needsLabels := false
	for _, policy := range candidates {
		if len(policy.LabelSelector) > 0 && string(policy.LabelSelector) != "null" {
			needsLabels = true
			break
		}
	}
	if !needsLabels {
		return candidates, nil
	}
	labels, err := approvalActionLabels(ctx, q, action, plan.ImmutablePlan)
	if err != nil {
		return nil, err
	}
	result := make([]db.ApprovalPolicy, 0, len(candidates))
	for _, policy := range candidates {
		if len(policy.LabelSelector) == 0 || string(policy.LabelSelector) == "null" {
			result = append(result, policy)
			continue
		}
		if authorization.ScopeMatches(authorization.Scope{
			ID:            policy.ID.String(),
			EnterpriseID:  action.EnterpriseID.String(),
			ResourceTypes: []string{action.ResourceType},
			LabelSelector: policy.LabelSelector,
			Status:        "active",
		}, authorization.Resource{
			EnterpriseID: action.EnterpriseID.String(),
			Type:         action.ResourceType,
			ID:           action.ResourceID.UUID.String(),
			Labels:       labels,
		}) {
			result = append(result, policy)
		}
	}
	return result, nil
}

func approvalActionLabels(ctx context.Context, q *db.Queries, action db.PendingAction, immutablePlan []byte) (map[string]string, error) {
	current := map[string]string{}
	if action.ResourceID.Valid {
		var raw []byte
		var err error
		switch action.ResourceType {
		case "host":
			var value db.Host
			value, err = q.GetHost(ctx, db.GetHostParams{ID: action.ResourceID.UUID, EnterpriseID: action.EnterpriseID})
			raw = value.Labels
		case "kubernetes_cluster":
			var value db.KubernetesCluster
			value, err = q.GetKubernetesCluster(ctx, db.GetKubernetesClusterParams{ID: action.ResourceID.UUID, EnterpriseID: action.EnterpriseID})
			raw = value.Labels
		}
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
		if len(raw) > 0 {
			current, err = resource.DecodeLabels(raw)
			if err != nil {
				return nil, err
			}
		}
	}
	planned, ok := findLabels(immutablePlan)
	if !ok {
		return current, nil
	}
	return resource.MergeSystemLabels(planned, current), nil
}

func findLabels(raw []byte) (map[string]string, bool) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return nil, false
	}
	return findLabelsValue(value)
}

func findLabelsValue(value any) (map[string]string, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if labels, ok := typed["labels"].(map[string]any); ok {
			result := make(map[string]string, len(labels))
			for key, item := range labels {
				text, ok := item.(string)
				if !ok {
					return nil, false
				}
				result[key] = text
			}
			return result, true
		}
		for _, item := range typed {
			if result, ok := findLabelsValue(item); ok {
				return result, true
			}
		}
	case []any:
		for _, item := range typed {
			if result, ok := findLabelsValue(item); ok {
				return result, true
			}
		}
	}
	return nil, false
}
