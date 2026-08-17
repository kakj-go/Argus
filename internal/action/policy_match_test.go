package action

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestFindLabelsFromImmutablePlan(t *testing.T) {
	t.Parallel()
	labels, ok := findLabels([]byte(`{"operation":"update","input":{"name":"web","labels":{"env":"prod","tier":"api"}}}`))
	if !ok {
		t.Fatal("expected labels in immutable plan")
	}
	want := map[string]string{"env": "prod", "tier": "api"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("labels = %#v, want %#v", labels, want)
	}
}

func TestFindLabelsRejectsNonStringValues(t *testing.T) {
	t.Parallel()
	if _, ok := findLabels([]byte(`{"input":{"labels":{"env":1}}}`)); ok {
		t.Fatal("non-string label values must not be accepted")
	}
}

func TestPolicySnapshotHashIncludesEveryIndependentPolicy(t *testing.T) {
	t.Parallel()
	first := db.ApprovalPolicy{ID: uuid.New(), Version: 1, MinimumApprovers: 1}
	second := db.ApprovalPolicy{ID: uuid.New(), Version: 2, MinimumApprovers: 2, SeparationOfDuty: true}
	combined := hashPolicies([]db.ApprovalPolicy{first, second})
	if bytes.Equal(combined, hashPolicies([]db.ApprovalPolicy{first})) || bytes.Equal(combined, hashPolicies([]db.ApprovalPolicy{second})) {
		t.Fatal("combined policy snapshot must not collapse to either individual policy")
	}
	if !bytes.Equal(combined, hashPolicies([]db.ApprovalPolicy{second, first})) {
		t.Fatal("policy snapshot hash must be deterministic regardless of query order")
	}
}

func TestApprovalPolicyQueryMatchesFrozenCommitTool(t *testing.T) {
	t.Parallel()
	enterpriseID := uuid.New()
	action := db.PendingAction{Risk: "write", ActionType: "host.update", ResourceType: "host"}
	plan := db.PendingActionPlan{CommitTool: "argus.host.update.commit"}

	query := approvalPolicyQuery(enterpriseID, action, plan)
	if query.EnterpriseID != enterpriseID || query.Column2 != "write" || query.Column4 != "host" {
		t.Fatalf("approval policy query lost action scope: %#v", query)
	}
	if query.Column3 != plan.CommitTool {
		t.Fatalf("tool id = %q, want frozen commit tool %q", query.Column3, plan.CommitTool)
	}
	if query.Column3 == action.ActionType {
		t.Fatal("approval policy matching must not use the business action type as the tool id")
	}
}
