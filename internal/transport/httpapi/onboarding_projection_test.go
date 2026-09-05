package httpapi

import (
	"testing"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestHostOnboardingProjectionUsesServerFacts(t *testing.T) {
	host := db.Host{ConnectionMode: "self_enrolled", ConnectionStatus: "onboarding"}
	tests := []struct {
		name string
		fact db.ListHostOnboardingFactsRow
		want string
	}{
		{name: "approval", fact: db.ListHostOnboardingFactsRow{ActionStatus: "awaiting_approval"}, want: "awaiting_approval"},
		{name: "claimable command", fact: db.ListHostOnboardingFactsRow{OneTimeResultState: "available", EnrollmentStatus: "active"}, want: "command_available"},
		{name: "claimed command", fact: db.ListHostOnboardingFactsRow{OneTimeResultState: "consumed", EnrollmentStatus: "active"}, want: "command_consumed"},
		{name: "bootstrap exchanged", fact: db.ListHostOnboardingFactsRow{OneTimeResultState: "consumed", EnrollmentStatus: "consumed"}, want: "installing"},
		{name: "failed", fact: db.ListHostOnboardingFactsRow{ExecutionStatus: "failed", ErrorCode: "HOST_INSTALL_FAILED"}, want: "install_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := deriveHostOnboarding(host, test.fact); actual.State != test.want {
				t.Fatalf("state = %q, want %q", actual.State, test.want)
			}
		})
	}
	host.ConnectionStatus = "online"
	if actual := deriveHostOnboarding(host, db.ListHostOnboardingFactsRow{CollectorStatus: "converged"}); actual.State != "registered" {
		t.Fatalf("converged state = %q", actual.State)
	}
}

func TestBastionOnboardingSeparatesOperationFromCommand(t *testing.T) {
	scope := db.ListBastionScopesRow{Status: "pending"}
	operationID := uuid.New()
	installing := deriveBastionOnboarding(scope, db.ListBastionOnboardingFactsRow{OperationID: operationID, OperationStatus: "running"})
	if installing.State != "installing" || !installing.OperationID.Valid || installing.OperationID.UUID != operationID {
		t.Fatalf("installing projection = %#v", installing)
	}
	failed := deriveBastionOnboarding(scope, db.ListBastionOnboardingFactsRow{OperationID: operationID, OperationStatus: "failed", ErrorCode: "SSH_HOST_KEY_CHANGED"})
	if failed.State != "install_failed" || failed.ErrorCode != "SSH_HOST_KEY_CHANGED" {
		t.Fatalf("failed projection = %#v", failed)
	}
	command := deriveBastionOnboarding(scope, db.ListBastionOnboardingFactsRow{OneTimeResultState: "available", EnrollmentStatus: "active"})
	if command.State != "command_available" {
		t.Fatalf("command projection = %#v", command)
	}
}

func TestBastionScopeProjectsControlTunnelSeparately(t *testing.T) {
	scope := db.GetBastionScopeRow{
		ID:                  uuid.New(),
		EnterpriseID:        uuid.New(),
		Name:                "mode-c",
		Environment:         "production",
		Status:              "active",
		OnboardingMode:      "direct_install_tunnel",
		ControlTunnelStatus: "established",
		FencingGeneration:   1,
		ResourceVersion:     1,
	}
	projected := toBastionScope(scope, onboardingView{State: "registered"})
	if projected.ControlTunnelStatus == nil || string(*projected.ControlTunnelStatus) != "established" {
		t.Fatalf("control tunnel projection = %#v", projected.ControlTunnelStatus)
	}
}
