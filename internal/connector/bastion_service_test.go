package connector

import "testing"

func TestBastionSnapshotStatusReplacementAllowsOfflineTransition(t *testing.T) {
	for _, status := range []string{"suspected_offline", "offline", "uninstalled"} {
		if got := bastionSnapshotStatus("replace", status); got != "disconnected" {
			t.Fatalf("replace status %q normalized to %q", status, got)
		}
	}
}

func TestBastionSnapshotStatusOtherOperationsRemainExact(t *testing.T) {
	if got := bastionSnapshotStatus("delete", "offline"); got != "offline" {
		t.Fatalf("delete status normalized to %q", got)
	}
}
