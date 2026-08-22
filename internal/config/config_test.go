package config

import "testing"

func TestLoadServerPlatformMFARequiredDefaultsToFalse(t *testing.T) {
	t.Setenv("ARGUS_PLATFORM_MFA_REQUIRED", "")
	if LoadServer().PlatformMFARequired {
		t.Fatal("PlatformMFARequired must default to false")
	}
}

func TestLoadServerPlatformMFARequiredCanBeEnabled(t *testing.T) {
	t.Setenv("ARGUS_PLATFORM_MFA_REQUIRED", "true")
	if !LoadServer().PlatformMFARequired {
		t.Fatal("PlatformMFARequired was not enabled")
	}
}
