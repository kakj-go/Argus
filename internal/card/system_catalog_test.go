package card

import "testing"

func TestSystemCatalogRevisionActivatesTelemetryWithNewRevision(t *testing.T) {
	telemetry := SystemCatalogCard{Slug: "telemetry-overview"}
	if revision := systemCatalogRevision(telemetry, false, ""); revision != SystemCatalogRevision {
		t.Fatalf("pending telemetry revision = %d, want %d", revision, SystemCatalogRevision)
	}
	if revision := systemCatalogRevision(telemetry, true, "abc123"); revision != TelemetrySystemCatalogRevision {
		t.Fatalf("active telemetry revision = %d, want %d", revision, TelemetrySystemCatalogRevision)
	}
	if revision := systemCatalogRevision(SystemCatalogCard{Slug: "host-list"}, true, "abc123"); revision != SystemCatalogRevision {
		t.Fatalf("non-telemetry revision = %d, want %d", revision, SystemCatalogRevision)
	}
}
