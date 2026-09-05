package httpapi

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type onboardingView struct {
	State            string
	PendingActionRef string
	ExecutionID      uuid.NullUUID
	OperationID      uuid.NullUUID
	ErrorCode        string
	UpdatedAt        time.Time
}

func loadHostOnboarding(ctx context.Context, queries *db.Queries, enterpriseID uuid.UUID, hosts []db.Host) map[uuid.UUID]onboardingView {
	result := make(map[uuid.UUID]onboardingView, len(hosts))
	ids := make([]uuid.UUID, 0, len(hosts))
	byID := make(map[uuid.UUID]db.Host, len(hosts))
	for _, host := range hosts {
		ids = append(ids, host.ID)
		byID[host.ID] = host
		result[host.ID] = onboardingView{State: "registered", UpdatedAt: host.UpdatedAt.Time}
	}
	if queries == nil || len(ids) == 0 {
		return result
	}
	facts, err := queries.ListHostOnboardingFacts(ctx, db.ListHostOnboardingFactsParams{EnterpriseID: enterpriseID, Column2: ids})
	if err != nil {
		return result
	}
	for _, fact := range facts {
		result[fact.ResourceID] = deriveHostOnboarding(byID[fact.ResourceID], fact)
	}
	return result
}

func deriveHostOnboarding(host db.Host, fact db.ListHostOnboardingFactsRow) onboardingView {
	view := onboardingView{State: "registered", PendingActionRef: fact.ActionRef, ExecutionID: fact.ExecutionID,
		ErrorCode: fact.ErrorCode, UpdatedAt: fact.ProjectionUpdatedAt.Time}
	if !fact.ProjectionUpdatedAt.Valid {
		view.UpdatedAt = host.UpdatedAt.Time
	}
	if host.ConnectionMode != "self_enrolled" || (host.ConnectionStatus == "online" && fact.CollectorStatus == "converged") {
		return view
	}
	if fact.ActionStatus == "awaiting_approval" {
		view.State, view.ErrorCode = "awaiting_approval", ""
		return view
	}
	if fact.ExecutionStatus == "failed" || fact.CollectorStatus == "result_unknown" {
		view.State = "install_failed"
		if view.ErrorCode == "" {
			view.ErrorCode = "HOST_INSTALL_FAILED"
		}
		return view
	}
	if fact.EnrollmentStatus == "consumed" || fact.CollectorStatus == "installing" {
		view.State, view.ErrorCode = "installing", ""
		return view
	}
	switch fact.OneTimeResultState {
	case "available":
		view.State, view.ErrorCode = "command_available", ""
	case "consumed":
		view.State, view.ErrorCode = "command_consumed", ""
	case "expired":
		view.State, view.ErrorCode = "command_expired", ""
	default:
		view.State = "install_failed"
		if view.ErrorCode == "" {
			view.ErrorCode = "HOST_ONBOARDING_STATE_UNAVAILABLE"
		}
	}
	return view
}

func loadBastionOnboarding(ctx context.Context, queries *db.Queries, enterpriseID uuid.UUID, scopes []db.ListBastionScopesRow) map[uuid.UUID]onboardingView {
	result := make(map[uuid.UUID]onboardingView, len(scopes))
	ids := make([]uuid.UUID, 0, len(scopes))
	byID := make(map[uuid.UUID]db.ListBastionScopesRow, len(scopes))
	for _, scope := range scopes {
		ids = append(ids, scope.ID)
		byID[scope.ID] = scope
		result[scope.ID] = onboardingView{State: "install_failed", ErrorCode: "BASTION_ONBOARDING_STATE_UNAVAILABLE", UpdatedAt: scope.UpdatedAt.Time}
	}
	if queries == nil || len(ids) == 0 {
		return result
	}
	facts, err := queries.ListBastionOnboardingFacts(ctx, db.ListBastionOnboardingFactsParams{EnterpriseID: enterpriseID, Column2: ids})
	if err != nil {
		return result
	}
	for _, fact := range facts {
		result[fact.ResourceID] = deriveBastionOnboarding(byID[fact.ResourceID], fact)
	}
	return result
}

func deriveBastionOnboarding(scope db.ListBastionScopesRow, fact db.ListBastionOnboardingFactsRow) onboardingView {
	view := onboardingView{State: "install_failed", PendingActionRef: fact.ActionRef, ExecutionID: fact.ExecutionID,
		OperationID: uuid.NullUUID{UUID: fact.OperationID, Valid: fact.OperationID != uuid.Nil}, ErrorCode: fact.ErrorCode,
		UpdatedAt: fact.ProjectionUpdatedAt.Time}
	if !fact.ProjectionUpdatedAt.Valid {
		view.UpdatedAt = scope.UpdatedAt.Time
	}
	if scope.ActiveConnectorID.Valid || fact.ConnectorStatus != "" || scope.Status == "active" {
		view.State, view.ErrorCode = "registered", ""
		return view
	}
	if fact.ActionStatus == "awaiting_approval" {
		view.State, view.ErrorCode = "awaiting_approval", ""
		return view
	}
	switch fact.OperationStatus {
	case "queued", "running", "result_unknown":
		view.State, view.ErrorCode = "installing", ""
		return view
	case "failed", "expired":
		view.State = "install_failed"
		if view.ErrorCode == "" {
			view.ErrorCode = "CONNECTOR_INSTALL_FAILED"
		}
		return view
	case "succeeded":
		view.State, view.ErrorCode = "install_failed", "CONNECTOR_ONLINE_NOT_CONVERGED"
		return view
	}
	if fact.ExecutionStatus == "failed" {
		view.State = "install_failed"
		if view.ErrorCode == "" {
			view.ErrorCode = "CONNECTOR_INSTALL_FAILED"
		}
		return view
	}
	if fact.EnrollmentStatus == "consumed" {
		view.State, view.ErrorCode = "installing", ""
		return view
	}
	switch fact.OneTimeResultState {
	case "available":
		view.State, view.ErrorCode = "command_available", ""
	case "consumed":
		view.State, view.ErrorCode = "command_consumed", ""
	case "expired":
		view.State, view.ErrorCode = "command_expired", ""
	default:
		if view.ErrorCode == "" {
			view.ErrorCode = "BASTION_ONBOARDING_STATE_UNAVAILABLE"
		}
	}
	return view
}
