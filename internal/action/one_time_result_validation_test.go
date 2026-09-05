package action

import (
	"strings"
	"testing"
	"time"

	"github.com/kakj-go/Argus/internal/installinstruction"
)

func TestValidOneTimeResultPayloadRequiresCommandForLinux(t *testing.T) {
	set := validInstructionSet(installinstruction.ScopeLinuxSystem)
	set.DownloadTLSMode = string(installinstruction.DownloadTLSInsecureFirstFetch)
	if !validOneTimeResultPayload([]installinstruction.Set{set}) {
		t.Fatal("valid Linux one-line instruction was rejected")
	}
	set.Command = ""
	if validOneTimeResultPayload([]installinstruction.Set{set}) {
		t.Fatal("Linux instruction without its one-line command was accepted")
	}
}

func TestValidOneTimeResultPayloadRequiresOneKubernetesCommand(t *testing.T) {
	set := validInstructionSet(installinstruction.ScopeKubernetes)
	if !validOneTimeResultPayload([]installinstruction.Set{set}) {
		t.Fatal("valid Kubernetes one-command instruction was rejected")
	}
	set.Command = ""
	if validOneTimeResultPayload([]installinstruction.Set{set}) {
		t.Fatal("Kubernetes instruction without its command was accepted")
	}
}

func validInstructionSet(scope installinstruction.Scope) installinstruction.Set {
	return installinstruction.Set{
		Scope: scope, Command: "install",
		ExpiresAt: time.Now().UTC().Add(time.Hour), TrustBundleEpoch: 1,
		TrustBundleSHA256: strings.Repeat("b", 64), InstallerSHA256: strings.Repeat("a", 64), CapabilityWarnings: []string{},
	}
}
