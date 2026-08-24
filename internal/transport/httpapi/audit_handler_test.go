package httpapi

import (
	"testing"

	auditapi "github.com/kakj-go/Argus/internal/gen/openapi/audit"
)

func TestApplyAuditPresentationAddsNamesWithoutReplacingFacts(t *testing.T) {
	event := auditapi.AuditEvent{
		ActorId: "actor-uuid",
		Action:  "secret.create",
	}
	result := applyAuditPresentation(event, auditActorPresentation{
		displayName: "Chen Xi",
		username:    "chenxi",
	}, "Production SSH key")

	if result.ActorId != "actor-uuid" || result.Action != "secret.create" {
		t.Fatal("presentation fields must not replace immutable audit facts")
	}
	if result.ActorDisplayName == nil || *result.ActorDisplayName != "Chen Xi" {
		t.Fatalf("unexpected actor display name: %#v", result.ActorDisplayName)
	}
	if result.ActorUsername == nil || *result.ActorUsername != "chenxi" {
		t.Fatalf("unexpected actor username: %#v", result.ActorUsername)
	}
	if result.ResourceDisplayName == nil || *result.ResourceDisplayName != "Production SSH key" {
		t.Fatalf("unexpected resource display name: %#v", result.ResourceDisplayName)
	}
}

func TestApplyAuditPresentationLeavesMissingNamesUnset(t *testing.T) {
	result := applyAuditPresentation(auditapi.AuditEvent{}, auditActorPresentation{}, "")
	if result.ActorDisplayName != nil || result.ActorUsername != nil || result.ResourceDisplayName != nil {
		t.Fatal("unresolved presentation fields must remain absent")
	}
}
