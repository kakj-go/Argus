package remoteaccess

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"reflect"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type evaluatedAccess struct {
	Decision     AccessDecision
	DecisionJSON []byte
	SnapshotHash []byte
	Host         db.Host
	Account      db.ManagedAccount
	Grants       []db.RemoteAccessGrant
}

type SimulationInput struct {
	HostID              uuid.UUID
	ManagedAccountID    uuid.UUID
	Protocol            string
	Action              string
	SourceIP            netip.Addr
	At                  time.Time
	StepUpAuthenticated bool
}

func (service Service) Simulate(ctx context.Context, actor Actor, input SimulationInput) (AccessDecision, error) {
	if input.HostID == uuid.Nil || input.ManagedAccountID == uuid.Nil || !validProtocol(input.Protocol) || input.Action != "terminal" {
		return AccessDecision{}, ErrInvalidRequest
	}
	if input.SourceIP.IsValid() {
		actor.SourceIP = input.SourceIP
	}
	actor.StepUpAuthenticated = input.StepUpAuthenticated
	evaluated, err := service.evaluateAccess(ctx, service.Store.Queries, actor, RequestInput{HostID: input.HostID,
		ManagedAccountID: input.ManagedAccountID, Protocol: input.Protocol, Action: input.Action, Reason: "simulation"}, input.At)
	if err != nil {
		return AccessDecision{}, err
	}
	return evaluated.Decision, nil
}

func (service Service) evaluateAccess(ctx context.Context, q *db.Queries, actor Actor, input RequestInput, at time.Time) (evaluatedAccess, error) {
	host, err := q.GetHost(ctx, db.GetHostParams{ID: input.HostID, EnterpriseID: actor.EnterpriseID})
	if err != nil {
		return evaluatedAccess{}, ErrScopeDenied
	}
	hostLabels, err := resource.DecodeLabels(host.Labels)
	if err != nil {
		return evaluatedAccess{}, err
	}
	allowed := service.Access.CanAccess(actor.AuthorizedResourceIDs, host.ID)
	account, err := q.GetManagedAccount(ctx, db.GetManagedAccountParams{ID: input.ManagedAccountID, EnterpriseID: actor.EnterpriseID})
	if err != nil || account.HostID != host.ID || !slices.Contains(account.AllowedProtocols, accountProtocol(input.Protocol)) {
		return evaluatedAccess{}, ErrScopeDenied
	}
	grantRows, err := q.ListCandidateRemoteAccessGrants(ctx, db.ListCandidateRemoteAccessGrantsParams{EnterpriseID: actor.EnterpriseID, ActorID: actor.UserID})
	if err != nil {
		return evaluatedAccess{}, err
	}
	ruleRows, err := q.ListRemoteAccessRules(ctx, actor.EnterpriseID)
	if err != nil {
		return evaluatedAccess{}, err
	}
	workflowRows, err := q.ListRemoteAccessApprovalWorkflows(ctx, actor.EnterpriseID)
	if err != nil {
		return evaluatedAccess{}, err
	}
	profileRows, err := q.ListRemoteAccessSessionProfiles(ctx, actor.EnterpriseID)
	if err != nil {
		return evaluatedAccess{}, err
	}
	grants := make([]Grant, 0, len(grantRows))
	for _, row := range grantRows {
		grants = append(grants, grantFromRow(row))
	}
	rules := make([]RemoteAccessRule, 0, len(ruleRows))
	for _, row := range ruleRows {
		value, convertErr := ruleFromRow(row)
		if convertErr != nil {
			return evaluatedAccess{}, convertErr
		}
		rules = append(rules, value)
	}
	workflows := make([]ApprovalWorkflow, 0, len(workflowRows))
	for _, row := range workflowRows {
		workflows = append(workflows, workflowFromRow(row))
	}
	profiles := make([]SessionProfile, 0, len(profileRows))
	for _, row := range profileRows {
		profiles = append(profiles, profileFromRow(row))
	}
	if at.IsZero() {
		at = service.now()
	}
	decision, err := (RemoteAccessDecisionService{Now: service.Now}).Evaluate(AccessIntent{EnterpriseID: actor.EnterpriseID, UserID: actor.UserID,
		DepartmentID: actor.DepartmentID, HostID: host.ID, HostLabels: hostLabels, ManagedAccountID: account.ID,
		ManagedAccountLabels: map[string]string{"argus.io/username": account.Username, "argus.io/privilege-level": account.PrivilegeLevel},
		Protocol:             input.Protocol, Action: input.Action, SourceIP: actor.SourceIP, At: at, AuthorizationVersion: actor.AuthorizationVersion,
		ResourceAuthorized: allowed, HostActive: host.Status == "active", ManagedAccountActive: account.Status == "active",
		StepUpAuthenticated: actor.StepUpAuthenticated}, grants, rules, workflows, profiles)
	if err != nil {
		return evaluatedAccess{}, err
	}
	encoded, hash, err := EncodeAccessDecision(decision)
	if err != nil {
		return evaluatedAccess{}, err
	}
	return evaluatedAccess{Decision: decision, DecisionJSON: encoded, SnapshotHash: hash, Host: host, Account: account, Grants: grantRows}, nil
}

func (service Service) createEvaluatedRequest(ctx context.Context, q *db.Queries, actor Actor, input RequestInput, evaluated evaluatedAccess) (RequestView, error) {
	grant, err := firstMatchedGrant(evaluated.Decision, evaluated.Grants)
	if err != nil {
		return RequestView{}, err
	}
	status := requestStatusForDecision(evaluated.Decision.Outcome)
	now := service.now()
	expires := decisionRequestExpiry(now, grant, evaluated.Decision)
	reasons, err := json.Marshal(evaluated.Decision.ReasonCodes)
	if err != nil {
		return RequestView{}, err
	}
	grantSnapshots, err := json.Marshal(evaluated.Decision.MatchedGrantSnapshots)
	if err != nil {
		return RequestView{}, err
	}
	ruleSnapshots, err := json.Marshal(evaluated.Decision.MatchedRuleSnapshots)
	if err != nil {
		return RequestView{}, err
	}
	request, err := q.CreateRemoteAccessRequest(ctx, db.CreateRemoteAccessRequestParams{ID: newID(), EnterpriseID: actor.EnterpriseID,
		RequesterID: actor.UserID, GrantID: grant.ID, HostID: input.HostID, ManagedAccountID: input.ManagedAccountID,
		Protocol: input.Protocol, Action: input.Action, Reason: input.Reason, Status: status, AuthorizationVersion: actor.AuthorizationVersion,
		ExpiresAt: timestamp(expires), DecisionOutcome: text(string(evaluated.Decision.Outcome)), DecisionReasonCodes: reasons,
		DecisionSnapshot: evaluated.DecisionJSON, DecisionSnapshotHash: evaluated.SnapshotHash, MatchedGrantSnapshots: grantSnapshots,
		MatchedRuleSnapshots: ruleSnapshots, DecisionAt: timestamp(now)})
	if err != nil {
		return RequestView{}, err
	}
	var requirements []db.RemoteAccessRequirementSnapshot
	if status == "awaiting_approval" {
		requirements, err = service.createDecisionRequirements(ctx, q, request.ID, evaluated.Decision, now)
		if err != nil {
			return RequestView{}, err
		}
	}
	if status == "authorized" {
		if _, err := service.issueLease(ctx, q, request, requirements); err != nil {
			return RequestView{}, err
		}
	}
	if err := publishDecisionNotification(ctx, q, request.ID, evaluated.Decision, evaluated.SnapshotHash); err != nil {
		return RequestView{}, err
	}
	if err := appendDecisionAudit(ctx, q, actor, request.ID, "remote_access.request.create", status, evaluated.Decision, evaluated.SnapshotHash); err != nil {
		return RequestView{}, err
	}
	return service.loadRequest(ctx, q, request)
}

func (service Service) ResumeRequest(ctx context.Context, actor Actor, requestID uuid.UUID, idempotencyKey string) (RequestView, error) {
	key := struct {
		RequestID uuid.UUID `json:"request_id"`
	}{RequestID: requestID}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actor.UserID.String(), "remote_access.request.resume", idempotencyKey, key, 200, func(q *db.Queries) (RequestView, error) {
		request, err := q.GetRemoteAccessRequestForUpdate(ctx, db.GetRemoteAccessRequestForUpdateParams{ID: requestID, EnterpriseID: actor.EnterpriseID})
		if err != nil || request.RequesterID != actor.UserID || request.Status != "awaiting_mfa" {
			return RequestView{}, ErrInvalidTransition
		}
		if request.AuthorizationVersion != actor.AuthorizationVersion {
			updated, updateErr := q.UpdateRemoteAccessRequestStatus(ctx, db.UpdateRemoteAccessRequestStatusParams{ID: request.ID, EnterpriseID: request.EnterpriseID, Status: "invalidated", Status_2: "awaiting_mfa"})
			if updateErr != nil {
				return RequestView{}, ErrAuthorizationStale
			}
			return service.loadRequest(ctx, q, updated)
		}
		if !service.now().Before(request.ExpiresAt.Time) {
			updated, updateErr := q.UpdateRemoteAccessRequestStatus(ctx, db.UpdateRemoteAccessRequestStatusParams{ID: request.ID, EnterpriseID: request.EnterpriseID, Status: "expired", Status_2: "awaiting_mfa"})
			if updateErr != nil {
				return RequestView{}, updateErr
			}
			return service.loadRequest(ctx, q, updated)
		}
		input := RequestInput{HostID: request.HostID, ManagedAccountID: request.ManagedAccountID, Protocol: request.Protocol, Action: request.Action, Reason: request.Reason}
		evaluated, err := service.evaluateAccess(ctx, q, actor, input, service.now())
		if err != nil {
			return RequestView{}, err
		}
		status := requestStatusForDecision(evaluated.Decision.Outcome)
		grant, grantErr := firstMatchedGrant(evaluated.Decision, evaluated.Grants)
		if grantErr != nil || grant.ID != request.GrantID {
			status = "invalidated"
		}
		reasons, err := json.Marshal(evaluated.Decision.ReasonCodes)
		if err != nil {
			return RequestView{}, err
		}
		grantSnapshots, err := json.Marshal(evaluated.Decision.MatchedGrantSnapshots)
		if err != nil {
			return RequestView{}, err
		}
		ruleSnapshots, err := json.Marshal(evaluated.Decision.MatchedRuleSnapshots)
		if err != nil {
			return RequestView{}, err
		}
		expires := decisionRequestExpiry(service.now(), grant, evaluated.Decision)
		updated, err := q.ResumeRemoteAccessRequest(ctx, db.ResumeRemoteAccessRequestParams{ID: request.ID, EnterpriseID: request.EnterpriseID,
			Status: status, DecisionOutcome: text(string(evaluated.Decision.Outcome)), DecisionReasonCodes: reasons,
			DecisionSnapshot: evaluated.DecisionJSON, DecisionSnapshotHash: evaluated.SnapshotHash,
			MatchedGrantSnapshots: grantSnapshots, MatchedRuleSnapshots: ruleSnapshots, ExpiresAt: timestamp(expires)})
		if err != nil {
			return RequestView{}, err
		}
		var requirements []db.RemoteAccessRequirementSnapshot
		if status == "awaiting_approval" {
			requirements, err = service.createDecisionRequirements(ctx, q, request.ID, evaluated.Decision, service.now())
			if err != nil {
				return RequestView{}, err
			}
		}
		if status == "authorized" {
			if _, err := service.issueLease(ctx, q, updated, requirements); err != nil {
				return RequestView{}, err
			}
		}
		if err := publishDecisionNotification(ctx, q, request.ID, evaluated.Decision, evaluated.SnapshotHash); err != nil {
			return RequestView{}, err
		}
		if err := appendDecisionAudit(ctx, q, actor, request.ID, "remote_access.request.resume", status, evaluated.Decision, evaluated.SnapshotHash); err != nil {
			return RequestView{}, err
		}
		return service.loadRequest(ctx, q, updated)
	})
}

func (service Service) createDecisionRequirements(ctx context.Context, q *db.Queries, requestID uuid.UUID, decision AccessDecision, now time.Time) ([]db.RemoteAccessRequirementSnapshot, error) {
	result := make([]db.RemoteAccessRequirementSnapshot, 0, len(decision.ApprovalRequirements))
	for _, snapshot := range decision.ApprovalRequirements {
		encoded, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		ruleID, ruleVersion := firstRuleSource(snapshot, decision.MatchedRuleSnapshots)
		profileID, profileVersion := firstObjectSource(decision.SessionProfile.SourceProfiles)
		deadline := now.Add(snapshot.ApprovalTimeout)
		value, err := q.CreateRemoteAccessRequirement(ctx, db.CreateRemoteAccessRequirementParams{ID: newID(), RequestID: requestID,
			ApproverRoleIds: slices.Clone(snapshot.ApproverRoleIDs), MinimumApprovals: int32(snapshot.MinimumApprovals),
			SeparationOfDuties: snapshot.SeparationOfDuties, RequireMfa: false,
			MaxSessionSeconds: int32(decision.SessionProfile.MaxSessionSeconds), IdleTimeoutSeconds: int32(decision.SessionProfile.IdleTimeoutSeconds),
			RuleID: nullableID(ruleID), RuleVersion: nullableVersion(ruleVersion), WorkflowID: nullableID(snapshot.WorkflowID),
			WorkflowVersion: nullableVersion(snapshot.WorkflowVersion), SessionProfileID: nullableID(profileID), SessionProfileVersion: nullableVersion(profileVersion),
			ApprovalSnapshot: encoded, DeadlineAt: timestamp(deadline), EscalationAt: timestamp(now.Add(snapshot.EscalationAfter)), TimeoutEffect: snapshot.TimeoutEffect,
			EscalationRoleIds: slices.Clone(snapshot.EscalationRoleIDs)})
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func requestStatusForDecision(outcome DecisionOutcome) string {
	switch outcome {
	case DecisionAwaitingMFA:
		return "awaiting_mfa"
	case DecisionAwaitingApproval:
		return "awaiting_approval"
	case DecisionAllowed:
		return "authorized"
	default:
		return "invalidated"
	}
}

func decisionRequestExpiry(now time.Time, grant db.RemoteAccessGrant, decision AccessDecision) time.Time {
	expires := now.Add(RequestTTL)
	if decision.Outcome == DecisionAllowed || decision.Outcome == DecisionAwaitingMFA {
		expires = now.Add(LeaseTTL)
	}
	for _, requirement := range decision.ApprovalRequirements {
		if deadline := now.Add(requirement.ApprovalTimeout); deadline.Before(expires) {
			expires = deadline
		}
	}
	if grant.ValidUntil.Valid && grant.ValidUntil.Time.Before(expires) {
		expires = grant.ValidUntil.Time
	}
	return expires
}

func firstRuleSource(requirement ApprovalRequirementSnapshot, rules []ObjectVersionSnapshot) (uuid.UUID, int64) {
	if len(requirement.SourceRuleIDs) == 0 {
		return uuid.Nil, 0
	}
	id := requirement.SourceRuleIDs[0]
	for _, rule := range rules {
		if rule.ID == id {
			return id, rule.Version
		}
	}
	return uuid.Nil, 0
}

func firstObjectSource(values []ObjectVersionSnapshot) (uuid.UUID, int64) {
	if len(values) == 0 {
		return uuid.Nil, 0
	}
	return values[0].ID, values[0].Version
}

func nullableVersion(value int64) pgtype.Int8 {
	return pgtype.Int8{Int64: value, Valid: value > 0}
}

func grantFromRow(row db.RemoteAccessGrant) Grant {
	return Grant{ID: row.ID, EnterpriseID: row.EnterpriseID, SubjectType: row.SubjectType, SubjectID: row.SubjectID,
		HostIDs: slices.Clone(row.HostIds), ManagedAccountIDs: slices.Clone(row.ManagedAccountIds),
		Protocols: slices.Clone(row.Protocols), Actions: slices.Clone(row.Actions), ValidFrom: row.ValidFrom.Time, ValidUntil: row.ValidUntil.Time,
		Status: row.Status, Version: row.Version}
}

func ruleFromRow(row db.RemoteAccessRule) (RemoteAccessRule, error) {
	var windows []TimeWindow
	if len(row.TimeWindows) > 0 && string(row.TimeWindows) != "null" {
		if err := json.Unmarshal(row.TimeWindows, &windows); err != nil {
			return RemoteAccessRule{}, err
		}
	}
	return RemoteAccessRule{ID: row.ID, EnterpriseID: row.EnterpriseID, Name: row.Name, Description: row.Description,
		Priority:  int(row.Priority),
		Protocols: slices.Clone(row.Protocols), Actions: slices.Clone(row.Actions), SourceCIDRs: slices.Clone(row.SourceCidrs), TimeWindows: windows,
		Effects: slices.Clone(row.Effects), ApprovalWorkflowID: nullableUUID(row.ApprovalWorkflowID), SessionProfileID: nullableUUID(row.SessionProfileID),
		Status: row.Status, Version: row.Version}, nil
}

func workflowFromRow(row db.RemoteAccessApprovalWorkflow) ApprovalWorkflow {
	return ApprovalWorkflow{ID: row.ID, EnterpriseID: row.EnterpriseID, Name: row.Name, Description: row.Description,
		ApproverRoleIDs: slices.Clone(row.ApproverRoleIds), EscalationRoleIDs: slices.Clone(row.EscalationRoleIds),
		MinimumApprovals: int(row.MinimumApprovals), SeparationOfDuties: row.SeparationOfDuties,
		ApprovalTimeoutSeconds: int(row.ApprovalTimeoutSeconds), EscalationAfterSeconds: int(row.EscalationAfterSeconds), TimeoutEffect: row.TimeoutEffect, Status: row.Status, Version: row.Version}
}

func profileFromRow(row db.RemoteAccessSessionProfile) SessionProfile {
	return SessionProfile{ID: row.ID, EnterpriseID: row.EnterpriseID, Name: row.Name, Description: row.Description,
		MaxSessionSeconds: int(row.MaxSessionSeconds), IdleTimeoutSeconds: int(row.IdleTimeoutSeconds), RecordingMode: row.RecordingMode,
		CommandAuditMode: row.CommandAuditMode, ClipboardMode: row.ClipboardMode, FileUploadMode: row.FileUploadMode,
		FileDownloadMode: row.FileDownloadMode, PortForwardMode: row.PortForwardMode, SessionShareMode: row.SessionShareMode,
		RetentionDays: int(row.RetentionDays), Status: row.Status, Version: row.Version}
}

func nullableUUID(value uuid.NullUUID) uuid.UUID {
	if value.Valid {
		return value.UUID
	}
	return uuid.Nil
}

func decisionError(decision AccessDecision) error {
	if decision.Outcome != DecisionDenied {
		return nil
	}
	for _, reason := range decision.ReasonCodes {
		switch reason {
		case ReasonGrantRequired:
			return ErrGrantRequired
		case ReasonResourceAuthorizationDenied, ReasonHostInactive, ReasonManagedAccountInactive, ReasonEnterpriseMismatch:
			return ErrScopeDenied
		case ReasonInvalidIntent, ReasonRuleConfiguration:
			return ErrInvalidRequest
		}
	}
	return ErrScopeDenied
}

func firstMatchedGrant(decision AccessDecision, rows []db.RemoteAccessGrant) (db.RemoteAccessGrant, error) {
	if len(decision.MatchedGrantSnapshots) == 0 {
		return db.RemoteAccessGrant{}, ErrGrantRequired
	}
	id := decision.MatchedGrantSnapshots[0].ID
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return db.RemoteAccessGrant{}, errors.New("matched grant row missing")
}

func sourceIPFromSnapshot(snapshot AccessIntentSnapshot) netip.Addr {
	value, _ := netip.ParseAddr(snapshot.SourceIP)
	return value
}

func decisionSourcesEqual(previous, current AccessDecision) bool {
	if current.Outcome == DecisionDenied || current.Outcome == DecisionAwaitingMFA {
		return false
	}
	return slices.Equal(previous.MatchedGrantSnapshots, current.MatchedGrantSnapshots) &&
		slices.Equal(previous.MatchedRuleSnapshots, current.MatchedRuleSnapshots) &&
		reflect.DeepEqual(previous.ApprovalRequirements, current.ApprovalRequirements) &&
		reflect.DeepEqual(previous.SessionProfile, current.SessionProfile) &&
		previous.AuthorizationVersion == current.AuthorizationVersion &&
		previous.Intent.EnterpriseID == current.Intent.EnterpriseID && previous.Intent.UserID == current.Intent.UserID &&
		previous.Intent.HostID == current.Intent.HostID && previous.Intent.ManagedAccountID == current.Intent.ManagedAccountID &&
		previous.Intent.Protocol == current.Intent.Protocol && previous.Intent.Action == current.Intent.Action
}
