package remoteaccess

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"sort"
	"time"

	"github.com/google/uuid"
)

// DecisionOutcome is deliberately small and stable. HTTP adapters may map
// these values to transport-specific states, but the domain meaning must not
// change between request, simulation and session creation.
type DecisionOutcome string

const (
	DecisionAllowed          DecisionOutcome = "allowed"
	DecisionDenied           DecisionOutcome = "denied"
	DecisionAwaitingMFA      DecisionOutcome = "awaiting_mfa"
	DecisionAwaitingApproval DecisionOutcome = "awaiting_approval"
)

const (
	ReasonInvalidIntent               = "REMOTE_ACCESS_INVALID_INTENT"
	ReasonEnterpriseMismatch          = "REMOTE_ACCESS_ENTERPRISE_MISMATCH"
	ReasonResourceAuthorizationDenied = "REMOTE_ACCESS_RESOURCE_AUTHORIZATION_DENIED"
	ReasonHostInactive                = "REMOTE_ACCESS_HOST_INACTIVE"
	ReasonManagedAccountInactive      = "REMOTE_ACCESS_MANAGED_ACCOUNT_INACTIVE"
	ReasonGrantRequired               = "REMOTE_ACCESS_GRANT_REQUIRED"
	ReasonRuleConfiguration           = "REMOTE_ACCESS_RULE_CONFIGURATION_INVALID"
	ReasonDenyRule                    = "REMOTE_ACCESS_RULE_DENY"
	ReasonMFARequired                 = "REMOTE_ACCESS_MFA_REQUIRED"
	ReasonApprovalRequired            = "REMOTE_ACCESS_APPROVAL_REQUIRED"
	ReasonAllowed                     = "REMOTE_ACCESS_ALLOWED"
)

var ErrInvalidDecisionInput = errors.New("invalid remote access decision input")

// AccessIntent is the complete, non-secret input to an authorization
// decision. Resource status and explicit resource authorization are supplied by the caller after
// checking the authoritative stores; a false value always fails closed.
type AccessIntent struct {
	EnterpriseID         uuid.UUID
	UserID               uuid.UUID
	DepartmentID         uuid.UUID
	SubjectLabels        map[string]string
	HostID               uuid.UUID
	HostLabels           map[string]string
	ManagedAccountID     uuid.UUID
	ManagedAccountLabels map[string]string
	Protocol             string
	Action               string
	SourceIP             netip.Addr
	At                   time.Time
	AuthorizationVersion int64
	ResourceAuthorized   bool
	HostActive           bool
	ManagedAccountActive bool
	StepUpAuthenticated  bool
	StepUpAt             time.Time
	StepUpMaxAge         time.Duration
}

type ApprovalRequirementSnapshot struct {
	WorkflowID         uuid.UUID     `json:"workflow_id"`
	WorkflowVersion    int64         `json:"workflow_version"`
	ApproverRoleIDs    []uuid.UUID   `json:"approver_role_ids"`
	MinimumApprovals   int           `json:"minimum_approvals"`
	SeparationOfDuties bool          `json:"separation_of_duties"`
	ApprovalTimeout    time.Duration `json:"approval_timeout"`
	EscalationAfter    time.Duration `json:"escalation_after"`
	TimeoutEffect      string        `json:"timeout_effect"`
	EscalationRoleIDs  []uuid.UUID   `json:"escalation_role_ids"`
	SourceRuleIDs      []uuid.UUID   `json:"source_rule_ids"`
}

type ObjectVersionSnapshot struct {
	ID      uuid.UUID `json:"id"`
	Version int64     `json:"version"`
}

type AccessIntentSnapshot struct {
	EnterpriseID         uuid.UUID `json:"enterprise_id"`
	UserID               uuid.UUID `json:"user_id"`
	DepartmentID         uuid.UUID `json:"department_id"`
	HostID               uuid.UUID `json:"host_id"`
	ManagedAccountID     uuid.UUID `json:"managed_account_id"`
	Protocol             string    `json:"protocol"`
	Action               string    `json:"action"`
	SourceIP             string    `json:"source_ip,omitempty"`
	EvaluatedAt          time.Time `json:"evaluated_at"`
	AuthorizationVersion int64     `json:"authorization_version"`
}

// SessionProfileSnapshot is an immutable effective profile. Source IDs are
// retained for explanation and revocation indexing; the effective values are
// what Lease and Session must enforce.
type SessionProfileSnapshot struct {
	SourceProfiles     []ObjectVersionSnapshot `json:"source_profiles"`
	MaxSessionSeconds  int                     `json:"max_session_seconds"`
	IdleTimeoutSeconds int                     `json:"idle_timeout_seconds"`
	RecordingMode      string                  `json:"recording_mode"`
	CommandAuditMode   string                  `json:"command_audit_mode"`
	ClipboardMode      string                  `json:"clipboard_mode"`
	FileUploadMode     string                  `json:"file_upload_mode"`
	FileDownloadMode   string                  `json:"file_download_mode"`
	PortForwardMode    string                  `json:"port_forward_mode"`
	SessionShareMode   string                  `json:"session_share_mode"`
	RetentionDays      int                     `json:"retention_days"`
}

type AccessDecision struct {
	Intent                AccessIntentSnapshot          `json:"intent"`
	Outcome               DecisionOutcome               `json:"outcome"`
	MatchedGrantIDs       []uuid.UUID                   `json:"matched_grant_ids"`
	MatchedGrantSnapshots []ObjectVersionSnapshot       `json:"matched_grant_snapshots"`
	MatchedRuleIDs        []uuid.UUID                   `json:"matched_rule_ids"`
	MatchedRuleSnapshots  []ObjectVersionSnapshot       `json:"matched_rule_snapshots"`
	ApprovalRequirements  []ApprovalRequirementSnapshot `json:"approval_requirements"`
	SessionProfile        SessionProfileSnapshot        `json:"session_profile"`
	Notifications         bool                          `json:"notifications"`
	AuthorizationVersion  int64                         `json:"authorization_version"`
	ReasonCodes           []string                      `json:"reason_codes"`
	Explanation           []string                      `json:"explanation"`
	SnapshotHash          [32]byte                      `json:"-"`
}

// RemoteAccessDecisionService is intentionally pure. Repository adapters
// load current rows and resource facts, then invoke Evaluate. This keeps the
// same semantics available to HTTP, request, session and simulation paths.
type RemoteAccessDecisionService struct {
	Now func() time.Time
}

func (s RemoteAccessDecisionService) Evaluate(intent AccessIntent, grants []Grant, rules []RemoteAccessRule, workflows []ApprovalWorkflow, profiles []SessionProfile) (AccessDecision, error) {
	if intent.At.IsZero() {
		intent.At = s.now()
	}
	decision := AccessDecision{Intent: snapshotIntent(intent), Outcome: DecisionDenied, AuthorizationVersion: intent.AuthorizationVersion, SessionProfile: defaultSessionProfileSnapshot()}
	if intent.EnterpriseID == uuid.Nil || intent.UserID == uuid.Nil || intent.HostID == uuid.Nil || intent.ManagedAccountID == uuid.Nil || !validProtocol(intent.Protocol) || intent.Action != "terminal" {
		return finalizeDecision(decision, ReasonInvalidIntent, "访问意图缺少必要的资源或协议字段"), nil
	}
	if !intent.ResourceAuthorized {
		return finalizeDecision(decision, ReasonResourceAuthorizationDenied, "当前主体未获目标主机授权"), nil
	}
	if !intent.HostActive {
		return finalizeDecision(decision, ReasonHostInactive, "目标主机不可用"), nil
	}
	if !intent.ManagedAccountActive {
		return finalizeDecision(decision, ReasonManagedAccountInactive, "托管账号不可用"), nil
	}

	for _, grant := range grants {
		if grant.Authorizes(Intent{
			EnterpriseID: intent.EnterpriseID, UserID: intent.UserID, DepartmentID: intent.DepartmentID,
			HostID: intent.HostID, HostLabels: intent.HostLabels, ManagedAccountID: intent.ManagedAccountID,
			Protocol: intent.Protocol, Action: intent.Action, AuthorizationTime: intent.At,
		}) {
			decision.MatchedGrantIDs = append(decision.MatchedGrantIDs, grant.ID)
			decision.MatchedGrantSnapshots = append(decision.MatchedGrantSnapshots, ObjectVersionSnapshot{ID: grant.ID, Version: grant.Version})
		}
	}
	if len(decision.MatchedGrantIDs) == 0 {
		return finalizeDecision(decision, ReasonGrantRequired, "没有匹配的远程访问授权"), nil
	}
	sortUUIDs(decision.MatchedGrantIDs)
	sortObjectSnapshots(decision.MatchedGrantSnapshots)

	workflowByID := make(map[uuid.UUID]ApprovalWorkflow, len(workflows))
	for _, workflow := range workflows {
		if workflow.EnterpriseID == intent.EnterpriseID {
			workflowByID[workflow.ID] = workflow
		}
	}
	profileByID := make(map[uuid.UUID]SessionProfile, len(profiles))
	for _, profile := range profiles {
		if profile.EnterpriseID == intent.EnterpriseID {
			profileByID[profile.ID] = profile
		}
	}

	orderedRules := slices.Clone(rules)
	sort.Slice(orderedRules, func(i, j int) bool {
		if orderedRules[i].Priority != orderedRules[j].Priority {
			return orderedRules[i].Priority < orderedRules[j].Priority
		}
		return orderedRules[i].ID.String() < orderedRules[j].ID.String()
	})
	requirements := make(map[uuid.UUID]*ApprovalRequirementSnapshot)
	denyMatched := false
	for _, rule := range orderedRules {
		if rule.EnterpriseID != intent.EnterpriseID || rule.Status != GovernanceEnabled || !slices.Contains(rule.Protocols, intent.Protocol) || !slices.Contains(rule.Actions, intent.Action) {
			continue
		}
		matched, err := ruleMatches(rule, intent)
		if err != nil {
			return finalizeDecision(decision, ReasonRuleConfiguration, "规则配置无法安全解析"), nil
		}
		if !matched {
			continue
		}
		decision.MatchedRuleIDs = append(decision.MatchedRuleIDs, rule.ID)
		decision.MatchedRuleSnapshots = append(decision.MatchedRuleSnapshots, ObjectVersionSnapshot{ID: rule.ID, Version: rule.Version})
		if slices.Contains(rule.Effects, "deny") {
			denyMatched = true
			continue
		}
		if slices.Contains(rule.Effects, "notify") {
			decision.Notifications = true
		}
		if slices.Contains(rule.Effects, "require_approval") {
			workflow, ok := workflowByID[rule.ApprovalWorkflowID]
			if !ok || workflow.Status != GovernanceEnabled || workflow.EnterpriseID != intent.EnterpriseID {
				return finalizeDecision(decision, ReasonRuleConfiguration, "审批流程不存在或未启用"), nil
			}
			requirement, err := mergeApprovalRequirement(requirements, workflow, rule.ID)
			if err != nil {
				return finalizeDecision(decision, ReasonRuleConfiguration, "审批流程版本不一致"), nil
			}
			requirements[workflow.ID] = requirement
		}
		if rule.SessionProfileID != uuid.Nil {
			profile, ok := profileByID[rule.SessionProfileID]
			if !ok || profile.Status != GovernanceEnabled || profile.EnterpriseID != intent.EnterpriseID {
				return finalizeDecision(decision, ReasonRuleConfiguration, "会话策略不存在或未启用"), nil
			}
			mergeSessionProfile(&decision.SessionProfile, profile)
		}
	}
	sortUUIDs(decision.MatchedRuleIDs)
	sortObjectSnapshots(decision.MatchedRuleSnapshots)
	if denyMatched {
		return finalizeDecision(decision, ReasonDenyRule, "命中拒绝规则"), nil
	}
	decision.ApprovalRequirements = sortedRequirements(requirements)
	if hasMFARequirement(orderedRules, decision.MatchedRuleIDs) && !freshStepUp(intent) {
		decision.Outcome = DecisionAwaitingMFA
		return finalizeDecision(decision, ReasonMFARequired, "需要重新完成多因素认证"), nil
	}
	if len(decision.ApprovalRequirements) > 0 {
		decision.Outcome = DecisionAwaitingApproval
		return finalizeDecision(decision, ReasonApprovalRequired, "需要完成审批流程"), nil
	}
	decision.Outcome = DecisionAllowed
	return finalizeDecision(decision, ReasonAllowed, "访问已通过授权决策"), nil
}

func (s RemoteAccessDecisionService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func ruleMatches(rule RemoteAccessRule, intent AccessIntent) (bool, error) {
	if len(rule.SourceCIDRs) > 0 {
		if !intent.SourceIP.IsValid() {
			return false, nil
		}
		matched := false
		for _, raw := range rule.SourceCIDRs {
			prefix, parseErr := netip.ParsePrefix(raw)
			if parseErr != nil {
				return false, parseErr
			}
			if prefix.Contains(intent.SourceIP) {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	if len(rule.TimeWindows) > 0 && !timeWindowMatches(rule.TimeWindows, intent.At) {
		return false, nil
	}
	return true, nil
}

func timeWindowMatches(windows []TimeWindow, at time.Time) bool {
	for _, window := range windows {
		location, err := time.LoadLocation(window.Timezone)
		if err != nil {
			return false
		}
		local := at.In(location)
		if int(local.Weekday()) != window.DayOfWeek {
			continue
		}
		start, startErr := time.Parse("15:04", window.Start)
		end, endErr := time.Parse("15:04", window.End)
		minutes := local.Hour()*60 + local.Minute()
		if startErr == nil && endErr == nil {
			startMinutes := start.Hour()*60 + start.Minute()
			endMinutes := end.Hour()*60 + end.Minute()
			if minutes >= startMinutes && minutes < endMinutes {
				return true
			}
		}
	}
	return false
}

func mergeApprovalRequirement(requirements map[uuid.UUID]*ApprovalRequirementSnapshot, workflow ApprovalWorkflow, ruleID uuid.UUID) (*ApprovalRequirementSnapshot, error) {
	current := requirements[workflow.ID]
	if current == nil {
		return &ApprovalRequirementSnapshot{WorkflowID: workflow.ID, WorkflowVersion: workflow.Version,
			ApproverRoleIDs: sortedUUIDCopy(workflow.ApproverRoleIDs), MinimumApprovals: workflow.MinimumApprovals,
			SeparationOfDuties: workflow.SeparationOfDuties, ApprovalTimeout: time.Duration(workflow.ApprovalTimeoutSeconds) * time.Second,
			EscalationAfter: time.Duration(workflow.EscalationAfterSeconds) * time.Second, TimeoutEffect: workflow.TimeoutEffect,
			EscalationRoleIDs: sortedUUIDCopy(workflow.EscalationRoleIDs), SourceRuleIDs: []uuid.UUID{ruleID}}, nil
	}
	if current.WorkflowVersion != workflow.Version {
		return nil, ErrInvalidDecisionInput
	}
	current.MinimumApprovals = max(current.MinimumApprovals, workflow.MinimumApprovals)
	current.SeparationOfDuties = current.SeparationOfDuties || workflow.SeparationOfDuties
	if timeout := time.Duration(workflow.ApprovalTimeoutSeconds) * time.Second; timeout > 0 && (current.ApprovalTimeout == 0 || timeout < current.ApprovalTimeout) {
		current.ApprovalTimeout = timeout
	}
	if escalationAfter := time.Duration(workflow.EscalationAfterSeconds) * time.Second; escalationAfter > 0 && (current.EscalationAfter == 0 || escalationAfter < current.EscalationAfter) {
		current.EscalationAfter = escalationAfter
	}
	if workflow.TimeoutEffect == "reject" {
		current.TimeoutEffect = "reject"
	}
	current.EscalationRoleIDs = unionUUIDs(current.EscalationRoleIDs, workflow.EscalationRoleIDs)
	current.SourceRuleIDs = unionUUIDs(current.SourceRuleIDs, []uuid.UUID{ruleID})
	return current, nil
}

func mergeSessionProfile(target *SessionProfileSnapshot, profile SessionProfile) {
	if target.MaxSessionSeconds == 0 || profile.MaxSessionSeconds < target.MaxSessionSeconds {
		target.MaxSessionSeconds = profile.MaxSessionSeconds
	}
	if target.IdleTimeoutSeconds == 0 || profile.IdleTimeoutSeconds < target.IdleTimeoutSeconds {
		target.IdleTimeoutSeconds = profile.IdleTimeoutSeconds
	}
	target.RecordingMode = stricterMode(target.RecordingMode, profile.RecordingMode, true)
	target.CommandAuditMode = stricterMode(target.CommandAuditMode, profile.CommandAuditMode, true)
	target.ClipboardMode = stricterMode(target.ClipboardMode, profile.ClipboardMode, false)
	target.FileUploadMode = stricterMode(target.FileUploadMode, profile.FileUploadMode, false)
	target.FileDownloadMode = stricterMode(target.FileDownloadMode, profile.FileDownloadMode, false)
	target.PortForwardMode = stricterMode(target.PortForwardMode, profile.PortForwardMode, false)
	target.SessionShareMode = stricterMode(target.SessionShareMode, profile.SessionShareMode, false)
	if profile.RetentionDays > target.RetentionDays {
		target.RetentionDays = profile.RetentionDays
	}
	ref := ObjectVersionSnapshot{ID: profile.ID, Version: profile.Version}
	if !slices.Contains(target.SourceProfiles, ref) {
		target.SourceProfiles = append(target.SourceProfiles, ref)
	}
	sortObjectSnapshots(target.SourceProfiles)
}

func stricterMode(current, candidate string, ordered bool) string {
	if current == "" {
		return candidate
	}
	if !ordered {
		if current == "disabled" || candidate == "disabled" {
			return "disabled"
		}
		return "enabled"
	}
	rank := map[string]int{"disabled": 1, "optional": 2, "required": 3}
	if rank[candidate] > rank[current] {
		return candidate
	}
	return current
}

func defaultSessionProfileSnapshot() SessionProfileSnapshot {
	return SessionProfileSnapshot{MaxSessionSeconds: int(DefaultMaxDuration / time.Second), IdleTimeoutSeconds: int(DefaultIdleTimeout / time.Second),
		RecordingMode: "required", CommandAuditMode: "required", ClipboardMode: "disabled", FileUploadMode: "disabled",
		FileDownloadMode: "disabled", PortForwardMode: "disabled", SessionShareMode: "disabled", RetentionDays: 90}
}

func hasMFARequirement(rules []RemoteAccessRule, matchedIDs []uuid.UUID) bool {
	matched := make(map[uuid.UUID]struct{}, len(matchedIDs))
	for _, id := range matchedIDs {
		matched[id] = struct{}{}
	}
	for _, rule := range rules {
		if _, ok := matched[rule.ID]; ok && slices.Contains(rule.Effects, "require_mfa") {
			return true
		}
	}
	return false
}

func freshStepUp(intent AccessIntent) bool {
	if !intent.StepUpAuthenticated {
		return false
	}
	if intent.StepUpAt.IsZero() {
		return true
	}
	maxAge := intent.StepUpMaxAge
	if maxAge <= 0 {
		maxAge = 10 * time.Minute
	}
	return !intent.At.Before(intent.StepUpAt) && intent.At.Sub(intent.StepUpAt) <= maxAge
}

func sortedRequirements(values map[uuid.UUID]*ApprovalRequirementSnapshot) []ApprovalRequirementSnapshot {
	result := make([]ApprovalRequirementSnapshot, 0, len(values))
	for _, value := range values {
		value.ApproverRoleIDs = sortedUUIDCopy(value.ApproverRoleIDs)
		value.EscalationRoleIDs = sortedUUIDCopy(value.EscalationRoleIDs)
		value.SourceRuleIDs = sortedUUIDCopy(value.SourceRuleIDs)
		result = append(result, *value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].WorkflowID.String() < result[j].WorkflowID.String() })
	return result
}

func finalizeDecision(decision AccessDecision, reason, explanation string) AccessDecision {
	decision.ReasonCodes = appendUniqueString(decision.ReasonCodes, reason)
	decision.Explanation = appendUniqueString(decision.Explanation, explanation)
	encoded, _ := canonicalDecisionJSON(decision)
	decision.SnapshotHash = sha256.Sum256(encoded)
	return decision
}

func snapshotIntent(intent AccessIntent) AccessIntentSnapshot {
	sourceIP := ""
	if intent.SourceIP.IsValid() {
		sourceIP = intent.SourceIP.String()
	}
	return AccessIntentSnapshot{EnterpriseID: intent.EnterpriseID, UserID: intent.UserID, DepartmentID: intent.DepartmentID,
		HostID: intent.HostID, ManagedAccountID: intent.ManagedAccountID, Protocol: intent.Protocol, Action: intent.Action,
		SourceIP: sourceIP, EvaluatedAt: intent.At.UTC(), AuthorizationVersion: intent.AuthorizationVersion}
}

func canonicalDecisionJSON(decision AccessDecision) ([]byte, error) {
	canonical := struct {
		Intent                AccessIntentSnapshot
		Outcome               DecisionOutcome
		MatchedGrantIDs       []uuid.UUID
		MatchedGrantSnapshots []ObjectVersionSnapshot
		MatchedRuleIDs        []uuid.UUID
		MatchedRuleSnapshots  []ObjectVersionSnapshot
		ApprovalRequirements  []ApprovalRequirementSnapshot
		SessionProfile        SessionProfileSnapshot
		Notifications         bool
		AuthorizationVersion  int64
		ReasonCodes           []string
	}{decision.Intent, decision.Outcome, decision.MatchedGrantIDs, decision.MatchedGrantSnapshots, decision.MatchedRuleIDs, decision.MatchedRuleSnapshots, decision.ApprovalRequirements, decision.SessionProfile, decision.Notifications, decision.AuthorizationVersion, decision.ReasonCodes}
	return json.Marshal(canonical)
}

func EncodeAccessDecision(decision AccessDecision) ([]byte, []byte, error) {
	encoded, err := json.Marshal(decision)
	if err != nil {
		return nil, nil, err
	}
	canonical, err := canonicalDecisionJSON(decision)
	if err != nil {
		return nil, nil, err
	}
	hash := sha256.Sum256(canonical)
	return encoded, hash[:], nil
}

func DecodeAccessDecision(encoded, expectedHash []byte) (AccessDecision, error) {
	var decision AccessDecision
	if len(encoded) == 0 || len(expectedHash) != sha256.Size || json.Unmarshal(encoded, &decision) != nil {
		return AccessDecision{}, ErrInvalidDecisionInput
	}
	canonical, err := canonicalDecisionJSON(decision)
	if err != nil {
		return AccessDecision{}, ErrInvalidDecisionInput
	}
	hash := sha256.Sum256(canonical)
	if !bytes.Equal(hash[:], expectedHash) {
		return AccessDecision{}, ErrInvalidDecisionInput
	}
	decision.SnapshotHash = hash
	return decision, nil
}

func sortedUUIDCopy(values []uuid.UUID) []uuid.UUID {
	copyValue := slices.Clone(values)
	sortUUIDs(copyValue)
	return uniqueUUIDs(copyValue)
}

func sortUUIDs(values []uuid.UUID) {
	sort.Slice(values, func(i, j int) bool { return values[i].String() < values[j].String() })
}

func sortObjectSnapshots(values []ObjectVersionSnapshot) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].ID != values[j].ID {
			return values[i].ID.String() < values[j].ID.String()
		}
		return values[i].Version < values[j].Version
	})
}

func uniqueUUIDs(values []uuid.UUID) []uuid.UUID {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func unionUUIDs(left, right []uuid.UUID) []uuid.UUID {
	return sortedUUIDCopy(append(slices.Clone(left), right...))
}

func appendUniqueString(values []string, value string) []string {
	if !slices.Contains(values, value) {
		values = append(values, value)
	}
	return values
}
