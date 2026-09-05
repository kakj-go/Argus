package resource

import "testing"

func TestValidateSelfEnrolledInput(t *testing.T) {
	valid := HostInput{Name: "office-01", Platform: "linux", Architecture: "arm64", ConnectionMode: "self_enrolled"}
	if err := validateSelfEnrolledInput(valid); err != nil {
		t.Fatalf("valid self_enrolled input rejected: %v", err)
	}
	windows := valid
	windows.Platform = "windows"
	if err := validateSelfEnrolledInput(windows); err == nil {
		t.Fatal("windows must be rejected for self_enrolled")
	}
	noArch := valid
	noArch.Architecture = ""
	if err := validateSelfEnrolledInput(noArch); err == nil {
		t.Fatal("missing architecture must be rejected for self_enrolled")
	}
	withAddress := valid
	withAddress.Address = "10.0.0.1"
	if err := validateSelfEnrolledInput(withAddress); err == nil {
		t.Fatal("inbound address must be rejected for self_enrolled")
	}
	withCredential := valid
	withCredential.Username = "root"
	if err := validateSelfEnrolledInput(withCredential); err == nil {
		t.Fatal("inbound credentials must be rejected for self_enrolled")
	}
}
