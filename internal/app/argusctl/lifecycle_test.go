package argusctl

import (
	"slices"
	"testing"
)

func TestOwnedCRDSelectorsIncludeRunScopedOpenSandboxRelease(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.ReleaseID = "argus-e2e-20260815-a15c"

	selectors := ownedCRDSelectors(cfg)
	want := "app.kubernetes.io/instance=" + cfg.upstreamReleaseName("os")
	if !slices.Contains(selectors, want) {
		t.Fatalf("owned CRD selectors %v do not contain %q", selectors, want)
	}
}
