package remoteaccess

import (
	"bytes"
	"encoding/json"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDecisionRequiresGrantBeforeRules(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	rule := decisionTestRule(enterprise, uuid.New(), 10, []string{"notify"})
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, nil, []RemoteAccessRule{rule}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionDenied || !containsString(decision.ReasonCodes, ReasonGrantRequired) {
		t.Fatalf("expected grant denial, got %#v", decision)
	}
}

func TestDecisionAllowsMatchingGrantWithoutRules(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionAllowed || !containsString(decision.ReasonCodes, ReasonAllowed) {
		t.Fatalf("matching grant should allow without rules, got %#v", decision)
	}
	if len(decision.MatchedRuleIDs) != 0 || !reflect.DeepEqual(decision.SessionProfile, defaultSessionProfileSnapshot()) {
		t.Fatalf("rule-free access must use the secure system profile: %#v", decision)
	}
}

func TestDecisionDenyOverridesOtherRules(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	notify := decisionTestRule(enterprise, uuid.New(), 1, []string{"notify"})
	deny := decisionTestRule(enterprise, uuid.New(), 100, []string{"deny"})
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{notify, deny}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionDenied || !containsString(decision.ReasonCodes, ReasonDenyRule) {
		t.Fatalf("deny must win, got %#v", decision)
	}
	if len(decision.MatchedRuleIDs) != 2 {
		t.Fatalf("expected both rules in explanation, got %d", len(decision.MatchedRuleIDs))
	}
}

func TestDecisionMergesRequirementsAndStrictestProfile(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	workflowA := decisionTestWorkflow(enterprise, uuid.New(), 1, 3600)
	workflowB := decisionTestWorkflow(enterprise, uuid.New(), 2, 600)
	profileA := decisionTestProfile(enterprise, uuid.New(), 3600, 900, "optional", "optional", "enabled")
	profileB := decisionTestProfile(enterprise, uuid.New(), 1800, 600, "required", "required", "disabled")
	ruleA := decisionTestRule(enterprise, uuid.New(), 10, []string{"require_mfa", "require_approval"})
	ruleA.ApprovalWorkflowID, ruleA.SessionProfileID = workflowA.ID, profileA.ID
	ruleB := decisionTestRule(enterprise, uuid.New(), 10, []string{"require_approval"})
	ruleB.ApprovalWorkflowID, ruleB.SessionProfileID = workflowB.ID, profileB.ID
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{ruleB, ruleA}, []ApprovalWorkflow{workflowB, workflowA}, []SessionProfile{profileB, profileA})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionAwaitingMFA {
		t.Fatalf("fresh MFA should be required first, got %s", decision.Outcome)
	}
	if len(decision.ApprovalRequirements) != 2 || decision.SessionProfile.MaxSessionSeconds != 1800 || decision.SessionProfile.IdleTimeoutSeconds != 600 {
		t.Fatalf("requirements/profile were not merged strictly: %#v", decision)
	}
	if decision.SessionProfile.RecordingMode != "required" || decision.SessionProfile.ClipboardMode != "disabled" {
		t.Fatalf("unsafe profile mode won merge: %#v", decision.SessionProfile)
	}
	intent.StepUpAuthenticated, intent.StepUpAt = true, intent.At.Add(-time.Minute)
	decision, err = (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{ruleA, ruleB}, []ApprovalWorkflow{workflowA, workflowB}, []SessionProfile{profileA, profileB})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionAwaitingApproval || !containsString(decision.ReasonCodes, ReasonApprovalRequired) {
		t.Fatalf("approval should remain after MFA, got %#v", decision)
	}
}

func TestDecisionStepUpExpires(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	rule := decisionTestRule(enterprise, uuid.New(), 1, []string{"require_mfa"})
	intent.StepUpAuthenticated, intent.StepUpAt = true, intent.At.Add(-11*time.Minute)
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{rule}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionAwaitingMFA {
		t.Fatalf("expired step-up must fail closed, got %s", decision.Outcome)
	}
}

func TestDecisionSnapshotIsStableAcrossInputOrder(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	workflow := decisionTestWorkflow(enterprise, uuid.New(), 1, 3600)
	profile := decisionTestProfile(enterprise, uuid.New(), 3600, 900, "required", "required", "disabled")
	ruleA := decisionTestRule(enterprise, uuid.New(), 10, []string{"require_approval"})
	ruleA.ApprovalWorkflowID, ruleA.SessionProfileID = workflow.ID, profile.ID
	ruleB := decisionTestRule(enterprise, uuid.New(), 10, []string{"notify"})
	first, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{ruleA, ruleB}, []ApprovalWorkflow{workflow}, []SessionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	second, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{ruleB, ruleA}, []ApprovalWorkflow{workflow}, []SessionProfile{profile})
	if err != nil {
		t.Fatal(err)
	}
	if first.SnapshotHash != second.SnapshotHash {
		t.Fatalf("snapshot hash changed with input order: %x != %x", first.SnapshotHash, second.SnapshotHash)
	}
}

func TestDecisionSnapshotRoundTripRejectsTampering(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, hash, err := EncodeAccessDecision(decision)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeAccessDecision(encoded, hash)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Outcome != decision.Outcome || !bytes.Equal(decoded.SnapshotHash[:], hash) {
		t.Fatalf("snapshot round trip changed the decision: %#v", decoded)
	}
	var tampered map[string]any
	if err := json.Unmarshal(encoded, &tampered); err != nil {
		t.Fatal(err)
	}
	tampered["outcome"] = string(DecisionDenied)
	encoded, err = json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAccessDecision(encoded, hash); err == nil {
		t.Fatal("tampered decision snapshot must be rejected")
	}
}

func TestRequestStatusForDecision(t *testing.T) {
	tests := map[DecisionOutcome]string{
		DecisionAwaitingMFA:      "awaiting_mfa",
		DecisionAwaitingApproval: "awaiting_approval",
		DecisionAllowed:          "authorized",
		DecisionDenied:           "invalidated",
	}
	for outcome, expected := range tests {
		if actual := requestStatusForDecision(outcome); actual != expected {
			t.Fatalf("outcome %q: expected %q, got %q", outcome, expected, actual)
		}
	}
}

func TestDecisionSourcesEqualFailsClosed(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	rule := decisionTestRule(enterprise, uuid.New(), 10, []string{"notify"})
	previous, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{rule}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	current := previous
	if !decisionSourcesEqual(previous, current) {
		t.Fatal("identical decision sources must be accepted")
	}
	current.MatchedRuleSnapshots = append([]ObjectVersionSnapshot(nil), previous.MatchedRuleSnapshots...)
	current.MatchedRuleSnapshots[0].Version++
	if decisionSourcesEqual(previous, current) {
		t.Fatal("rule version changes must fail closed")
	}
	current = previous
	current.Outcome = DecisionAwaitingMFA
	if decisionSourcesEqual(previous, current) {
		t.Fatal("a fresh MFA requirement must fail closed")
	}
}

func TestDecisionAppliesRuleOnlyWhenCIDRAndTimeWindowMatch(t *testing.T) {
	enterprise, user, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	at := time.Date(2026, time.August, 24, 10, 30, 0, 0, time.UTC) // Monday
	intent := decisionTestIntent(enterprise, user, host, account)
	intent.At, intent.SourceIP = at, netip.MustParseAddr("10.20.30.40")
	grant := decisionTestGrant(enterprise, user, host, account, at)
	rule := decisionTestRule(enterprise, uuid.New(), 1, []string{"deny"})
	rule.SourceCIDRs = []string{"10.20.0.0/16"}
	rule.TimeWindows = []TimeWindow{{DayOfWeek: 1, Start: "09:00", End: "17:00", Timezone: "UTC"}}
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{rule}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionDenied || !containsString(decision.ReasonCodes, ReasonDenyRule) {
		t.Fatalf("matching CIDR/time window should apply the deny rule, got %#v", decision)
	}
	intent.SourceIP = netip.MustParseAddr("192.0.2.1")
	decision, err = (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{rule}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionAllowed || len(decision.MatchedRuleIDs) != 0 {
		t.Fatalf("non-matching optional rule must preserve grant access: %#v", decision)
	}
}

func TestDecisionRejectsCrossEnterpriseReferences(t *testing.T) {
	enterprise, foreign := uuid.New(), uuid.New()
	user, host, account := uuid.New(), uuid.New(), uuid.New()
	intent := decisionTestIntent(enterprise, user, host, account)
	grant := decisionTestGrant(enterprise, user, host, account, intent.At)
	workflow := decisionTestWorkflow(foreign, uuid.New(), 1, 3600)
	rule := decisionTestRule(enterprise, uuid.New(), 1, []string{"require_approval"})
	rule.ApprovalWorkflowID = workflow.ID
	decision, err := (RemoteAccessDecisionService{}).Evaluate(intent, []Grant{grant}, []RemoteAccessRule{rule}, []ApprovalWorkflow{workflow}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Outcome != DecisionDenied || !containsString(decision.ReasonCodes, ReasonRuleConfiguration) {
		t.Fatalf("foreign workflow must fail closed, got %#v", decision)
	}
}

func decisionTestIntent(enterprise, user, host, account uuid.UUID) AccessIntent {
	return AccessIntent{EnterpriseID: enterprise, UserID: user, HostID: host, ManagedAccountID: account, Protocol: "ssh", Action: "terminal",
		At: time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC), AuthorizationVersion: 1, ResourceAuthorized: true, HostActive: true, ManagedAccountActive: true}
}

func decisionTestGrant(enterprise, user, host, account uuid.UUID, at time.Time) Grant {
	return Grant{ID: uuid.New(), EnterpriseID: enterprise, SubjectType: "user", SubjectID: user, HostIDs: []uuid.UUID{host}, ManagedAccountIDs: []uuid.UUID{account}, Protocols: []string{"ssh"}, Actions: []string{"terminal"}, ValidFrom: at.Add(-time.Hour), ValidUntil: at.Add(time.Hour), Status: GovernanceEnabled, Version: 1}
}

func decisionTestRule(enterprise, id uuid.UUID, priority int, effects []string) RemoteAccessRule {
	return RemoteAccessRule{ID: id, EnterpriseID: enterprise, Name: id.String(), Priority: priority, Protocols: []string{"ssh"}, Actions: []string{"terminal"}, Effects: effects, Status: GovernanceEnabled, Version: 1}
}

func decisionTestWorkflow(enterprise, id uuid.UUID, minimum, timeout int) ApprovalWorkflow {
	return ApprovalWorkflow{ID: id, EnterpriseID: enterprise, ApproverRoleIDs: []uuid.UUID{uuid.New(), uuid.New()}, MinimumApprovals: minimum, SeparationOfDuties: true, ApprovalTimeoutSeconds: timeout, EscalationAfterSeconds: timeout / 2, TimeoutEffect: "expire", Status: GovernanceEnabled, Version: 1}
}

func decisionTestProfile(enterprise, id uuid.UUID, maxSession, idle int, recording, audit, binary string) SessionProfile {
	return SessionProfile{ID: id, EnterpriseID: enterprise, MaxSessionSeconds: maxSession, IdleTimeoutSeconds: idle, RecordingMode: recording, CommandAuditMode: audit,
		ClipboardMode: binary, FileUploadMode: binary, FileDownloadMode: binary, PortForwardMode: binary, SessionShareMode: binary, RetentionDays: 90, Status: GovernanceEnabled, Version: 1}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
