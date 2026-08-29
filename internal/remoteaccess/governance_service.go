package remoteaccess

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type RuleInput struct {
	Name, Description                        string
	Priority                                 int32
	Protocols, Actions, SourceCIDRs, Effects []string
	TimeWindows                              []byte
	ApprovalWorkflowID, SessionProfileID     uuid.UUID
	Status                                   string
	ExpectedVersion                          int64
}

type WorkflowInput struct {
	Name, Description                        string
	ApproverRoleIDs, EscalationRoleIDs       []uuid.UUID
	MinimumApprovals, ApprovalTimeoutSeconds int32
	EscalationAfterSeconds                   int32
	SeparationOfDuties                       bool
	TimeoutEffect, Status                    string
	ExpectedVersion                          int64
}

type SessionProfileInput struct {
	Name, Description                                                                                                   string
	MaxSessionSeconds, IdleTimeoutSeconds, RetentionDays                                                                int32
	RecordingMode, CommandAuditMode, ClipboardMode, FileUploadMode, FileDownloadMode, PortForwardMode, SessionShareMode string
	Status                                                                                                              string
	ExpectedVersion                                                                                                     int64
}

func (s Service) ListRules(ctx context.Context, enterpriseID uuid.UUID) ([]db.RemoteAccessRule, error) {
	return s.Store.Queries.ListRemoteAccessRules(ctx, enterpriseID)
}
func (s Service) GetRule(ctx context.Context, enterpriseID, id uuid.UUID) (db.RemoteAccessRule, error) {
	return s.Store.Queries.GetRemoteAccessRule(ctx, db.GetRemoteAccessRuleParams{ID: id, EnterpriseID: enterpriseID})
}
func (s Service) CreateRule(ctx context.Context, actor Actor, input RuleInput, key string) (db.RemoteAccessRule, error) {
	if ValidateGovernanceCreateStatus(input.Status) != nil || ValidateRule(input.Name, input.Protocols, input.Actions, input.Effects, input.ApprovalWorkflowID, input.SessionProfileID, input.Status) != nil {
		return db.RemoteAccessRule{}, ErrInvalidRequest
	}
	if err := ValidateSourceCIDRs(input.SourceCIDRs); err != nil {
		return db.RemoteAccessRule{}, ErrInvalidRequest
	}
	if err := ValidateTimeWindows(input.TimeWindows); err != nil {
		return db.RemoteAccessRule{}, ErrInvalidRequest
	}
	if err := s.validateRuleReferences(ctx, actor.EnterpriseID, input.ApprovalWorkflowID, input.SessionProfileID); err != nil {
		return db.RemoteAccessRule{}, err
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.rule.create", key, input, 201, func(q *db.Queries) (db.RemoteAccessRule, error) {
		value, err := s.createRule(ctx, actor, input, q)
		if err != nil {
			return db.RemoteAccessRule{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.rule.create", "remote_access_rule", value.ID, "created")
	})
}
func (s Service) createRule(ctx context.Context, actor Actor, input RuleInput, q *db.Queries) (db.RemoteAccessRule, error) {
	return q.CreateRemoteAccessRule(ctx, db.CreateRemoteAccessRuleParams{ID: newID(), EnterpriseID: actor.EnterpriseID, Name: input.Name, Description: input.Description, Priority: input.Priority, Protocols: input.Protocols, Actions: input.Actions, SourceCidrs: input.SourceCIDRs, TimeWindows: jsonArrayOrEmpty(input.TimeWindows), Effects: input.Effects, ApprovalWorkflowID: nullableID(input.ApprovalWorkflowID), SessionProfileID: nullableID(input.SessionProfileID), Status: input.Status, CreatedBy: actor.UserID})
}
func (s Service) UpdateRule(ctx context.Context, actor Actor, id uuid.UUID, input RuleInput, key string) (db.RemoteAccessRule, error) {
	if input.ExpectedVersion < 1 {
		return db.RemoteAccessRule{}, ErrInvalidRequest
	}
	if ValidateSourceCIDRs(input.SourceCIDRs) != nil {
		return db.RemoteAccessRule{}, ErrInvalidRequest
	}
	if ValidateTimeWindows(input.TimeWindows) != nil {
		return db.RemoteAccessRule{}, ErrInvalidRequest
	}
	if err := s.validateRuleReferences(ctx, actor.EnterpriseID, input.ApprovalWorkflowID, input.SessionProfileID); err != nil {
		return db.RemoteAccessRule{}, err
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.rule.update", key, input, 200, func(q *db.Queries) (db.RemoteAccessRule, error) {
		current, err := q.GetRemoteAccessRule(ctx, db.GetRemoteAccessRuleParams{ID: id, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return db.RemoteAccessRule{}, err
		}
		if current.Status == GovernanceArchived {
			return db.RemoteAccessRule{}, ErrInvalidTransition
		}
		if ValidateRule(input.Name, input.Protocols, input.Actions, input.Effects, input.ApprovalWorkflowID, input.SessionProfileID, current.Status) != nil {
			return db.RemoteAccessRule{}, ErrInvalidRequest
		}
		value, err := q.UpdateRemoteAccessRule(ctx, db.UpdateRemoteAccessRuleParams{ID: id, EnterpriseID: actor.EnterpriseID, Name: input.Name, Description: input.Description, Priority: input.Priority, Protocols: input.Protocols, Actions: input.Actions, SourceCidrs: input.SourceCIDRs, TimeWindows: jsonArrayOrEmpty(input.TimeWindows), Effects: input.Effects, ApprovalWorkflowID: nullableID(input.ApprovalWorkflowID), SessionProfileID: nullableID(input.SessionProfileID), Version: input.ExpectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.RemoteAccessRule{}, ErrVersionConflict
		}
		if err != nil {
			return db.RemoteAccessRule{}, err
		}
		if err := s.invalidateGovernanceSource(ctx, q, actor.EnterpriseID, "rule", id, "rule_changed"); err != nil {
			return db.RemoteAccessRule{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.rule.update", "remote_access_rule", id, "updated")
	})
}
func (s Service) TransitionRule(ctx context.Context, actor Actor, id uuid.UUID, from, to string, expectedVersion int64, key string) (db.RemoteAccessRule, error) {
	if expectedVersion < 1 || !ValidGovernanceTransition(from, to) {
		return db.RemoteAccessRule{}, ErrInvalidTransition
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.rule."+to, key, map[string]any{"id": id, "from": from, "to": to, "expected_version": expectedVersion}, 200, func(q *db.Queries) (db.RemoteAccessRule, error) {
		value, err := q.TransitionRemoteAccessRule(ctx, db.TransitionRemoteAccessRuleParams{ID: id, EnterpriseID: actor.EnterpriseID, Status: to, Status_2: from, Version: expectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.RemoteAccessRule{}, ErrVersionConflict
		}
		if err != nil {
			return db.RemoteAccessRule{}, err
		}
		if err := s.invalidateGovernanceSource(ctx, q, actor.EnterpriseID, "rule", id, "rule_"+to); err != nil {
			return db.RemoteAccessRule{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.rule."+to, "remote_access_rule", id, to)
	})
}
func (s Service) RuleReferences(ctx context.Context, enterpriseID, id uuid.UUID) (db.CountRemoteAccessRuleReferencesRow, error) {
	if _, err := s.GetRule(ctx, enterpriseID, id); err != nil {
		return db.CountRemoteAccessRuleReferencesRow{}, err
	}
	return s.Store.Queries.CountRemoteAccessRuleReferences(ctx, id)
}

func (s Service) ListWorkflows(ctx context.Context, enterpriseID uuid.UUID) ([]db.RemoteAccessApprovalWorkflow, error) {
	return s.Store.Queries.ListRemoteAccessApprovalWorkflows(ctx, enterpriseID)
}
func (s Service) GetWorkflow(ctx context.Context, enterpriseID, id uuid.UUID) (db.RemoteAccessApprovalWorkflow, error) {
	return s.Store.Queries.GetRemoteAccessApprovalWorkflow(ctx, db.GetRemoteAccessApprovalWorkflowParams{ID: id, EnterpriseID: enterpriseID})
}
func (s Service) CreateWorkflow(ctx context.Context, actor Actor, input WorkflowInput, key string) (db.RemoteAccessApprovalWorkflow, error) {
	if ValidateGovernanceCreateStatus(input.Status) != nil || ValidateWorkflowRoles(input.Name, input.ApproverRoleIDs, input.EscalationRoleIDs, int(input.MinimumApprovals), int(input.ApprovalTimeoutSeconds), int(input.EscalationAfterSeconds), input.Status) != nil || (input.TimeoutEffect != "reject" && input.TimeoutEffect != "expire") {
		return db.RemoteAccessApprovalWorkflow{}, ErrInvalidRequest
	}
	if err := s.validateWorkflowRoleReferences(ctx, actor.EnterpriseID, input.ApproverRoleIDs, input.EscalationRoleIDs); err != nil {
		return db.RemoteAccessApprovalWorkflow{}, err
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.workflow.create", key, input, 201, func(q *db.Queries) (db.RemoteAccessApprovalWorkflow, error) {
		value, err := q.CreateRemoteAccessApprovalWorkflow(ctx, db.CreateRemoteAccessApprovalWorkflowParams{ID: newID(), EnterpriseID: actor.EnterpriseID, Name: input.Name, Description: input.Description, ApproverRoleIds: input.ApproverRoleIDs, MinimumApprovals: input.MinimumApprovals, SeparationOfDuties: input.SeparationOfDuties, ApprovalTimeoutSeconds: input.ApprovalTimeoutSeconds, EscalationAfterSeconds: input.EscalationAfterSeconds, TimeoutEffect: input.TimeoutEffect, EscalationRoleIds: input.EscalationRoleIDs, Status: input.Status, CreatedBy: actor.UserID})
		if err != nil {
			return db.RemoteAccessApprovalWorkflow{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.workflow.create", "remote_access_approval_workflow", value.ID, "created")
	})
}
func (s Service) UpdateWorkflow(ctx context.Context, actor Actor, id uuid.UUID, input WorkflowInput, key string) (db.RemoteAccessApprovalWorkflow, error) {
	if input.ExpectedVersion < 1 || (input.TimeoutEffect != "reject" && input.TimeoutEffect != "expire") {
		return db.RemoteAccessApprovalWorkflow{}, ErrInvalidRequest
	}
	if err := s.validateWorkflowRoleReferences(ctx, actor.EnterpriseID, input.ApproverRoleIDs, input.EscalationRoleIDs); err != nil {
		return db.RemoteAccessApprovalWorkflow{}, err
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.workflow.update", key, input, 200, func(q *db.Queries) (db.RemoteAccessApprovalWorkflow, error) {
		current, err := q.GetRemoteAccessApprovalWorkflow(ctx, db.GetRemoteAccessApprovalWorkflowParams{ID: id, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return db.RemoteAccessApprovalWorkflow{}, err
		}
		if current.Status == GovernanceArchived {
			return db.RemoteAccessApprovalWorkflow{}, ErrInvalidTransition
		}
		if ValidateWorkflowRoles(input.Name, input.ApproverRoleIDs, input.EscalationRoleIDs, int(input.MinimumApprovals), int(input.ApprovalTimeoutSeconds), int(input.EscalationAfterSeconds), current.Status) != nil {
			return db.RemoteAccessApprovalWorkflow{}, ErrInvalidRequest
		}
		value, err := q.UpdateRemoteAccessApprovalWorkflow(ctx, db.UpdateRemoteAccessApprovalWorkflowParams{ID: id, EnterpriseID: actor.EnterpriseID, Name: input.Name, Description: input.Description, ApproverRoleIds: input.ApproverRoleIDs, MinimumApprovals: input.MinimumApprovals, SeparationOfDuties: input.SeparationOfDuties, ApprovalTimeoutSeconds: input.ApprovalTimeoutSeconds, EscalationAfterSeconds: input.EscalationAfterSeconds, TimeoutEffect: input.TimeoutEffect, EscalationRoleIds: input.EscalationRoleIDs, Version: input.ExpectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.RemoteAccessApprovalWorkflow{}, ErrVersionConflict
		}
		if err != nil {
			return db.RemoteAccessApprovalWorkflow{}, err
		}
		if err := s.invalidateGovernanceSource(ctx, q, actor.EnterpriseID, "workflow", id, "workflow_changed"); err != nil {
			return db.RemoteAccessApprovalWorkflow{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.workflow.update", "remote_access_approval_workflow", id, "updated")
	})
}
func (s Service) TransitionWorkflow(ctx context.Context, actor Actor, id uuid.UUID, from, to string, expectedVersion int64, key string) (db.RemoteAccessApprovalWorkflow, error) {
	if expectedVersion < 1 || !ValidGovernanceTransition(from, to) {
		return db.RemoteAccessApprovalWorkflow{}, ErrInvalidTransition
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.workflow."+to, key, map[string]any{"id": id, "from": from, "to": to, "expected_version": expectedVersion}, 200, func(q *db.Queries) (db.RemoteAccessApprovalWorkflow, error) {
		value, err := q.TransitionRemoteAccessApprovalWorkflow(ctx, db.TransitionRemoteAccessApprovalWorkflowParams{ID: id, EnterpriseID: actor.EnterpriseID, Status: to, Status_2: from, Version: expectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.RemoteAccessApprovalWorkflow{}, ErrVersionConflict
		}
		if err != nil {
			return db.RemoteAccessApprovalWorkflow{}, err
		}
		if err := s.invalidateGovernanceSource(ctx, q, actor.EnterpriseID, "workflow", id, "workflow_"+to); err != nil {
			return db.RemoteAccessApprovalWorkflow{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.workflow."+to, "remote_access_approval_workflow", id, to)
	})
}
func (s Service) WorkflowReferences(ctx context.Context, enterpriseID, id uuid.UUID) (db.CountRemoteAccessApprovalWorkflowReferencesRow, error) {
	if _, err := s.GetWorkflow(ctx, enterpriseID, id); err != nil {
		return db.CountRemoteAccessApprovalWorkflowReferencesRow{}, err
	}
	return s.Store.Queries.CountRemoteAccessApprovalWorkflowReferences(ctx, uuid.NullUUID{UUID: id, Valid: true})
}

func (s Service) ListSessionProfiles(ctx context.Context, enterpriseID uuid.UUID) ([]db.RemoteAccessSessionProfile, error) {
	return s.Store.Queries.ListRemoteAccessSessionProfiles(ctx, enterpriseID)
}
func (s Service) GetSessionProfile(ctx context.Context, enterpriseID, id uuid.UUID) (db.RemoteAccessSessionProfile, error) {
	return s.Store.Queries.GetRemoteAccessSessionProfile(ctx, db.GetRemoteAccessSessionProfileParams{ID: id, EnterpriseID: enterpriseID})
}
func (s Service) CreateSessionProfile(ctx context.Context, actor Actor, input SessionProfileInput, key string) (db.RemoteAccessSessionProfile, error) {
	if ValidateGovernanceCreateStatus(input.Status) != nil || ValidateSessionProfile(input.Name, int(input.MaxSessionSeconds), int(input.IdleTimeoutSeconds), int(input.RetentionDays), input.RecordingMode, input.CommandAuditMode, input.Status) != nil || !validProfileModes(input) {
		return db.RemoteAccessSessionProfile{}, ErrInvalidRequest
	}
	if advancedProfileChannelEnabled(input) {
		return db.RemoteAccessSessionProfile{}, ErrChannelUnavailable
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.session_profile.create", key, input, 201, func(q *db.Queries) (db.RemoteAccessSessionProfile, error) {
		value, err := q.CreateRemoteAccessSessionProfile(ctx, db.CreateRemoteAccessSessionProfileParams{ID: newID(), EnterpriseID: actor.EnterpriseID, Name: input.Name, Description: input.Description, MaxSessionSeconds: input.MaxSessionSeconds, IdleTimeoutSeconds: input.IdleTimeoutSeconds, RecordingMode: input.RecordingMode, CommandAuditMode: input.CommandAuditMode, ClipboardMode: input.ClipboardMode, FileUploadMode: input.FileUploadMode, FileDownloadMode: input.FileDownloadMode, PortForwardMode: input.PortForwardMode, SessionShareMode: input.SessionShareMode, RetentionDays: input.RetentionDays, Status: input.Status, CreatedBy: actor.UserID})
		if err != nil {
			return db.RemoteAccessSessionProfile{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.session_profile.create", "remote_access_session_profile", value.ID, "created")
	})
}
func (s Service) UpdateSessionProfile(ctx context.Context, actor Actor, id uuid.UUID, input SessionProfileInput, key string) (db.RemoteAccessSessionProfile, error) {
	if input.ExpectedVersion < 1 || !validProfileModes(input) {
		return db.RemoteAccessSessionProfile{}, ErrInvalidRequest
	}
	if advancedProfileChannelEnabled(input) {
		return db.RemoteAccessSessionProfile{}, ErrChannelUnavailable
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.session_profile.update", key, input, 200, func(q *db.Queries) (db.RemoteAccessSessionProfile, error) {
		current, err := q.GetRemoteAccessSessionProfile(ctx, db.GetRemoteAccessSessionProfileParams{ID: id, EnterpriseID: actor.EnterpriseID})
		if err != nil {
			return db.RemoteAccessSessionProfile{}, err
		}
		if current.Status == GovernanceArchived {
			return db.RemoteAccessSessionProfile{}, ErrInvalidTransition
		}
		if ValidateSessionProfile(input.Name, int(input.MaxSessionSeconds), int(input.IdleTimeoutSeconds), int(input.RetentionDays), input.RecordingMode, input.CommandAuditMode, current.Status) != nil {
			return db.RemoteAccessSessionProfile{}, ErrInvalidRequest
		}
		value, err := q.UpdateRemoteAccessSessionProfile(ctx, db.UpdateRemoteAccessSessionProfileParams{ID: id, EnterpriseID: actor.EnterpriseID, Name: input.Name, Description: input.Description, MaxSessionSeconds: input.MaxSessionSeconds, IdleTimeoutSeconds: input.IdleTimeoutSeconds, RecordingMode: input.RecordingMode, CommandAuditMode: input.CommandAuditMode, ClipboardMode: input.ClipboardMode, FileUploadMode: input.FileUploadMode, FileDownloadMode: input.FileDownloadMode, PortForwardMode: input.PortForwardMode, SessionShareMode: input.SessionShareMode, RetentionDays: input.RetentionDays, Version: input.ExpectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.RemoteAccessSessionProfile{}, ErrVersionConflict
		}
		if err != nil {
			return db.RemoteAccessSessionProfile{}, err
		}
		if err := s.invalidateGovernanceSource(ctx, q, actor.EnterpriseID, "session_profile", id, "session_profile_changed"); err != nil {
			return db.RemoteAccessSessionProfile{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.session_profile.update", "remote_access_session_profile", id, "updated")
	})
}
func (s Service) TransitionSessionProfile(ctx context.Context, actor Actor, id uuid.UUID, from, to string, expectedVersion int64, key string) (db.RemoteAccessSessionProfile, error) {
	if expectedVersion < 1 || !ValidGovernanceTransition(from, to) {
		return db.RemoteAccessSessionProfile{}, ErrInvalidTransition
	}
	return postgres.ExecuteIdempotent(ctx, s.Store, s.Idempotency, "enterprise", actor.UserID.String(), "remote_access.session_profile."+to, key, map[string]any{"id": id, "from": from, "to": to, "expected_version": expectedVersion}, 200, func(q *db.Queries) (db.RemoteAccessSessionProfile, error) {
		value, err := q.TransitionRemoteAccessSessionProfile(ctx, db.TransitionRemoteAccessSessionProfileParams{ID: id, EnterpriseID: actor.EnterpriseID, Status: to, Status_2: from, Version: expectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.RemoteAccessSessionProfile{}, ErrVersionConflict
		}
		if err != nil {
			return db.RemoteAccessSessionProfile{}, err
		}
		if err := s.invalidateGovernanceSource(ctx, q, actor.EnterpriseID, "session_profile", id, "session_profile_"+to); err != nil {
			return db.RemoteAccessSessionProfile{}, err
		}
		return value, appendAudit(ctx, q, actor.UserID, actor.EnterpriseID, "remote_access.session_profile."+to, "remote_access_session_profile", id, to)
	})
}
func (s Service) SessionProfileReferences(ctx context.Context, enterpriseID, id uuid.UUID) (db.CountRemoteAccessSessionProfileReferencesRow, error) {
	if _, err := s.GetSessionProfile(ctx, enterpriseID, id); err != nil {
		return db.CountRemoteAccessSessionProfileReferencesRow{}, err
	}
	return s.Store.Queries.CountRemoteAccessSessionProfileReferences(ctx, uuid.NullUUID{UUID: id, Valid: true})
}

func (s Service) validateRuleReferences(ctx context.Context, enterpriseID, workflowID, profileID uuid.UUID) error {
	if workflowID != uuid.Nil {
		value, err := s.GetWorkflow(ctx, enterpriseID, workflowID)
		if err != nil || value.EnterpriseID != enterpriseID {
			return ErrScopeDenied
		}
	}
	if profileID != uuid.Nil {
		value, err := s.GetSessionProfile(ctx, enterpriseID, profileID)
		if err != nil || value.EnterpriseID != enterpriseID {
			return ErrScopeDenied
		}
	}
	return nil
}

func (s Service) validateWorkflowRoleReferences(ctx context.Context, enterpriseID uuid.UUID, approverRoleIDs, escalationRoleIDs []uuid.UUID) error {
	for _, roleID := range append(append([]uuid.UUID(nil), approverRoleIDs...), escalationRoleIDs...) {
		role, err := s.Store.Queries.GetRole(ctx, db.GetRoleParams{ID: roleID, EnterpriseID: enterpriseID})
		if err != nil || role.EnterpriseID != enterpriseID || role.Status != "active" {
			return ErrScopeDenied
		}
	}
	return nil
}

func nullableID(value uuid.UUID) uuid.NullUUID {
	return uuid.NullUUID{UUID: value, Valid: value != uuid.Nil}
}
func jsonOrEmpty(value []byte) []byte {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte("{}")
	}
	return value
}
func jsonArrayOrEmpty(value []byte) []byte {
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return []byte("[]")
	}
	return value
}
func validProfileModes(input SessionProfileInput) bool {
	validBinary := func(value string) bool { return value == "enabled" || value == "disabled" }
	return validBinary(input.ClipboardMode) && validBinary(input.FileUploadMode) && validBinary(input.FileDownloadMode) && validBinary(input.PortForwardMode) && validBinary(input.SessionShareMode)
}

func advancedProfileChannelEnabled(input SessionProfileInput) bool {
	return input.ClipboardMode == "enabled" || input.FileUploadMode == "enabled" || input.FileDownloadMode == "enabled" ||
		input.PortForwardMode == "enabled" || input.SessionShareMode == "enabled"
}
