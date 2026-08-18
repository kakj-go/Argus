package agent

import (
	"testing"

	"github.com/kakj-go/Argus/internal/mcp"
)

func TestSelectCardDraftToolsPrefersExplicitToolID(t *testing.T) {
	t.Parallel()
	catalog := []mcp.Metadata{{ID: "connector.list"}, {ID: "host.get"}, {ID: "host.list"}, {ID: "kubernetes.cluster.list"}}
	selected := selectCardDraftTools(catalog, "Create a host inventory Card using host.list.")
	if len(selected) == 0 || selected[0].ID != "host.list" {
		t.Fatalf("selected = %#v", selected)
	}
}

func TestSelectCardDraftToolsUsesBoundedSemanticCandidates(t *testing.T) {
	t.Parallel()
	catalog := []mcp.Metadata{{ID: "host.list"}, {ID: "host.get"}, {ID: "host.create.preview"}, {ID: "host.update.preview"}, {ID: "host.delete.preview"}, {ID: "connector.list"}}
	selected := selectCardDraftTools(catalog, "Create a host inventory table Card.")
	if len(selected) == 0 || len(selected) > 4 || selected[0].ID != "host.list" {
		t.Fatalf("selected = %#v", selected)
	}
}
