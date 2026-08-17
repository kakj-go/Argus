package resource

import (
	"testing"

	"github.com/kakj-go/Argus/internal/authorization"
)

func TestNamespaceScopeUsesClusterSlashNamespaceID(t *testing.T) {
	scope := authorization.Scope{ID: "scope", EnterpriseID: "enterprise", ResourceTypes: []string{"kubernetes_namespace"},
		ExplicitResourceIDs: []string{"cluster-id/production"}, Status: "active"}
	allowed, _ := authorization.AnyScopeMatches([]authorization.Scope{scope}, authorization.Resource{EnterpriseID: "enterprise", Type: "kubernetes_namespace", ID: "cluster-id/production"})
	if !allowed {
		t.Fatal("expected cluster_id/namespace explicit ID to match")
	}
}
