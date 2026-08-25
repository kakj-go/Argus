package authorization

import (
	"encoding/json"
	"testing"
)

func TestScopeUnionExplicitLabelsAndEmpty(t *testing.T) {
	raw := json.RawMessage(`{"schema_version":"argus.label_selector/v1","requirements":[{"key":"team","operator":"in","values":["sre","platform"]},{"key":"environment","operator":"eq","values":["staging"]}]}`)
	normalized, _, err := NormalizeSelector(raw)
	if err != nil {
		t.Fatal(err)
	}
	resource := Resource{EnterpriseID: "enterprise-1", Type: "host", ID: "host-2", Labels: map[string]string{"team": "sre", "environment": "staging"}}
	scopes := []Scope{
		{ID: "empty", EnterpriseID: "enterprise-1", ResourceTypes: []string{"host"}, Status: "active"},
		{ID: "labels", EnterpriseID: "enterprise-1", ResourceTypes: []string{"host"}, LabelSelector: normalized, Status: "active"},
	}
	allowed, matched := AnyScopeMatches(scopes, resource)
	if !allowed || len(matched) != 1 || matched[0] != "labels" {
		t.Fatalf("unexpected match: %v %v", allowed, matched)
	}
	resource.EnterpriseID = "enterprise-2"
	if allowed, _ := AnyScopeMatches(scopes, resource); allowed {
		t.Fatal("cross-enterprise resource matched")
	}
}

func TestNormalizeSelectorRejectsDuplicateKeys(t *testing.T) {
	_, _, err := NormalizeSelector(json.RawMessage(`{"schema_version":"argus.label_selector/v1","requirements":[{"key":"team","operator":"exists"},{"key":"team","operator":"not_exists"}]}`))
	if err == nil {
		t.Fatal("duplicate selector key accepted")
	}
}

func TestScopeMatchAllAllowsPrecreationResource(t *testing.T) {
	scope := Scope{
		ID:                  "all",
		EnterpriseID:        "enterprise-1",
		ResourceTypes:       []string{"host", "kubernetes_cluster"},
		ExplicitResourceIDs: []string{},
		MatchAll:            true,
		Status:              "active",
	}
	for _, resource := range []Resource{
		{EnterpriseID: "enterprise-1", Type: "host", ID: "preview-host-id"},
		{EnterpriseID: "enterprise-1", Type: "kubernetes_cluster", ID: "preview-cluster-id"},
	} {
		if !ScopeMatches(scope, resource) {
			t.Fatalf("match_all scope rejected %s preview resource", resource.Type)
		}
	}
}

func TestEmptyScopeStillMatchesNothing(t *testing.T) {
	scope := Scope{ID: "empty", EnterpriseID: "enterprise-1", ResourceTypes: []string{"host"}, Status: "active"}
	if ScopeMatches(scope, Resource{EnterpriseID: "enterprise-1", Type: "host", ID: "host-1"}) {
		t.Fatal("empty scope unexpectedly matched a resource")
	}
}
