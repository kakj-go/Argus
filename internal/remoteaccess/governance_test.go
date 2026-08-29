package remoteaccess

import (
	"testing"

	"github.com/google/uuid"
)

func TestValidateRuleEffects(t *testing.T) {
	workflow, profile := uuid.New(), uuid.New()
	if err := ValidateRule("rule", []string{"ssh"}, []string{"terminal"}, []string{"require_mfa", "require_approval", "notify"}, workflow, profile, GovernanceDraft); err != nil {
		t.Fatalf("expected combined effects to pass: %v", err)
	}
	if err := ValidateRule("deny", []string{"ssh"}, []string{"terminal"}, []string{"deny", "notify"}, uuid.Nil, uuid.Nil, GovernanceDraft); err == nil {
		t.Fatal("deny must not combine with other effects")
	}
	if err := ValidateRule("duplicate", []string{"ssh"}, []string{"terminal"}, []string{"notify", "notify"}, uuid.Nil, uuid.Nil, GovernanceDraft); err == nil {
		t.Fatal("duplicate effects must fail")
	}
	if err := ValidateRule("approval", []string{"ssh"}, []string{"terminal"}, []string{"require_approval"}, uuid.Nil, uuid.Nil, GovernanceDraft); err == nil {
		t.Fatal("approval effect without workflow must fail")
	}
	if err := ValidateRule("profile-only", []string{"ssh"}, []string{"terminal"}, nil, uuid.Nil, profile, GovernanceDraft); err != nil {
		t.Fatalf("profile-only rule should pass: %v", err)
	}
	if err := ValidateRule("empty", []string{"ssh"}, []string{"terminal"}, nil, uuid.Nil, uuid.Nil, GovernanceDraft); err == nil {
		t.Fatal("rule without effects or profile must fail")
	}
}

func TestGovernanceBoundaries(t *testing.T) {
	role := uuid.New()
	if err := ValidateWorkflowRoles("workflow", []uuid.UUID{role}, []uuid.UUID{uuid.New()}, 1, 3600, 900, GovernanceDraft); err != nil {
		t.Fatalf("valid approver and escalation roles rejected: %v", err)
	}
	if err := ValidateWorkflowRoles("workflow", []uuid.UUID{role}, []uuid.UUID{role}, 1, 3600, 900, GovernanceDraft); err == nil {
		t.Fatal("duplicate role across approver and escalation sets must fail")
	}
	if err := ValidateWorkflowRoles("workflow", []uuid.UUID{uuid.Nil}, nil, 1, 3600, 900, GovernanceDraft); err == nil {
		t.Fatal("nil role id must fail")
	}
	if err := ValidateWorkflow("workflow", []uuid.UUID{uuid.New()}, 2, 3600, GovernanceDraft); err == nil {
		t.Fatal("minimum approvals above role count must fail")
	}
	if err := ValidateWorkflowRoles("workflow", []uuid.UUID{role}, nil, 1, 3600, 3600, GovernanceDraft); err == nil {
		t.Fatal("escalation threshold at the approval deadline must fail")
	}
	if err := ValidateSessionProfile("profile", 60, 61, 90, "required", "required", GovernanceDraft); err == nil {
		t.Fatal("idle timeout above maximum must fail")
	}
	if err := ValidateRule("rule", []string{"rdp"}, []string{"terminal"}, []string{"notify"}, uuid.Nil, uuid.Nil, GovernanceDraft); err == nil {
		t.Fatal("unknown protocol must fail")
	}
	if err := ValidateSourceCIDRs([]string{"not-a-cidr"}); err == nil {
		t.Fatal("invalid CIDR must fail")
	}
	if err := ValidateSourceCIDRs([]string{"10.0.0.1/24", "10.0.0.0/24"}); err == nil {
		t.Fatal("equivalent CIDRs must not repeat")
	}
	tooManyCIDRs := make([]string, 65)
	for i := range tooManyCIDRs {
		tooManyCIDRs[i] = "10.0.0.0/8"
	}
	if err := ValidateSourceCIDRs(tooManyCIDRs); err == nil {
		t.Fatal("more than 64 CIDRs must fail")
	}
}

func TestAdvancedProfileChannelsRemainUnavailable(t *testing.T) {
	input := SessionProfileInput{ClipboardMode: "disabled", FileUploadMode: "disabled", FileDownloadMode: "disabled", PortForwardMode: "disabled", SessionShareMode: "disabled"}
	if advancedProfileChannelEnabled(input) {
		t.Fatal("disabled advanced channels reported enabled")
	}
	input.FileDownloadMode = "enabled"
	if !advancedProfileChannelEnabled(input) {
		t.Fatal("enabled advanced channel must be rejected")
	}
}

func TestValidateTimeWindows(t *testing.T) {
	valid := []byte(`[{"day_of_week":1,"start":"09:00","end":"17:00","timezone":"Asia/Shanghai"}]`)
	if err := ValidateTimeWindows(valid); err != nil {
		t.Fatalf("valid time window rejected: %v", err)
	}
	for _, value := range [][]byte{
		[]byte("null"),
		[]byte(`{"day_of_week":1}`),
		[]byte(`[{"day_of_week":1,"start":"09:00","end":"09:00","timezone":"Asia/Shanghai"}]`),
		[]byte(`[{"day_of_week":1,"start":"09:00","end":"17:00","timezone":"Mars/Olympus"}]`),
	} {
		if err := ValidateTimeWindows(value); err == nil {
			t.Fatal("invalid time window accepted")
		}
	}
}

func TestGovernanceTransitions(t *testing.T) {
	if !ValidGovernanceTransition(GovernanceDraft, GovernanceEnabled) {
		t.Fatal("draft should enable")
	}
	if ValidGovernanceTransition(GovernanceDraft, GovernanceDisabled) {
		t.Fatal("draft should not disable directly")
	}
	if !ValidGovernanceTransition(GovernanceArchived, GovernanceDraft) {
		t.Fatal("archived should restore to draft")
	}
	if ValidGovernanceTransition(GovernanceDraft, GovernanceArchived) {
		t.Fatal("draft should not be archived without first being enabled")
	}
	if err := ValidateGovernanceCreateStatus(GovernanceDraft); err != nil {
		t.Fatalf("draft should be a valid creation status: %v", err)
	}
	for _, status := range []string{GovernanceEnabled, GovernanceDisabled, GovernanceArchived} {
		if err := ValidateGovernanceCreateStatus(status); err == nil {
			t.Fatalf("%s should not be accepted as a creation status", status)
		}
	}
}
