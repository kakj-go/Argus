package resource

import (
	"testing"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestHostConnectionPlanBindsEverySecurityRelevantInput(t *testing.T) {
	credentialID := uuid.MustParse("018f47e2-9a4c-7b31-8acd-02a2475e8d2f")
	scopeID := uuid.MustParse("018f47e2-9a4c-7b31-8acd-02a2475e8d30")
	input := HostInput{Address: "host.example", Port: 22, Platform: "linux", Username: "argus", ConnectionMode: "via_bastion",
		BastionScopeID: uuid.NullUUID{UUID: scopeID, Valid: true}, CredentialID: uuid.NullUUID{UUID: credentialID, Valid: true}}
	plan := connectionPlan{TargetType: "host", Address: input.Address, Port: input.Port, Platform: input.Platform, Username: input.Username,
		ConnectionMode: input.ConnectionMode, BastionScopeID: input.BastionScopeID, CredentialID: input.CredentialID}
	if !hostConnectionPlanMatches(plan, input) {
		t.Fatal("matching Host connection plan was rejected")
	}
	mutations := []HostInput{input, input, input, input, input, input}
	mutations[0].Address = "other.example"
	mutations[1].Port = 2222
	mutations[2].Platform = "windows"
	mutations[3].Username = "root"
	mutations[4].ConnectionMode = "direct_ssh"
	mutations[5].CredentialID.UUID = uuid.MustParse("018f47e2-9a4c-7b31-8acd-02a2475e8d31")
	for index, mutation := range mutations {
		if hostConnectionPlanMatches(plan, mutation) {
			t.Fatalf("mutation %d reused a Connection Test for different Host input", index)
		}
	}
}

func TestHostNetworkPathChangeRequiresFreshConnectionTest(t *testing.T) {
	scopeA := uuid.MustParse("018f47e2-9a4c-7b31-8acd-02a2475e8d30")
	scopeB := uuid.MustParse("018f47e2-9a4c-7b31-8acd-02a2475e8d31")
	current := db.Host{Address: "10.0.0.10", Port: 22, Platform: "linux", ConnectionMode: "via_bastion",
		BastionScopeID: uuid.NullUUID{UUID: scopeA, Valid: true}}
	if hostNetworkPathChanged(current, HostInput{Name: "renamed"}) {
		t.Fatal("metadata-only update was treated as a network path change")
	}
	changes := []HostInput{
		{Address: "10.0.0.11"},
		{Port: 2222},
		{ConnectionMode: "via_bastion", BastionScopeID: uuid.NullUUID{UUID: scopeB, Valid: true}},
		{ConnectionMode: "direct_ssh"},
	}
	for index, change := range changes {
		if !hostNetworkPathChanged(current, change) {
			t.Fatalf("network path change %d was not detected", index)
		}
	}
	if editableHostConnectionMode("connector_local") {
		t.Fatal("connector_local must not be accepted through ordinary Host updates")
	}
}

func TestHostUpdatePlanBindsChangedRoute(t *testing.T) {
	scopeA, scopeB := uuid.New(), uuid.New()
	credentialID := uuid.New()
	current := db.Host{Address: "10.0.0.10", Port: 22, Platform: "linux", ConnectionMode: "via_bastion",
		BastionScopeID: uuid.NullUUID{UUID: scopeA, Valid: true}}
	input := HostInput{Address: "10.0.1.10", Port: 2222, ConnectionMode: "via_bastion", BastionScopeID: uuid.NullUUID{UUID: scopeB, Valid: true}}
	plan := connectionPlan{TargetType: "host", Address: input.Address, Port: input.Port, Platform: current.Platform, Username: "ops",
		ConnectionMode: input.ConnectionMode, BastionScopeID: input.BastionScopeID, CredentialID: uuid.NullUUID{UUID: credentialID, Valid: true}}
	if !hostUpdateConnectionPlanMatches(plan, current, input) {
		t.Fatal("matching migration Connection Test was rejected")
	}
	input.BastionScopeID = uuid.NullUUID{UUID: scopeA, Valid: true}
	if hostUpdateConnectionPlanMatches(plan, current, input) {
		t.Fatal("Connection Test was reusable for a different target Bastion Scope")
	}
}

func TestKubernetesNetworkPathChangeDetection(t *testing.T) {
	credentialID := uuid.New()
	current := db.KubernetesCluster{ApiServer: "https://api.example:6443", ConnectionMode: "direct",
		CredentialID: uuid.NullUUID{UUID: credentialID, Valid: true}}
	if kubernetesNetworkPathChanged(current, KubernetesInput{Name: "renamed"}) {
		t.Fatal("metadata-only Kubernetes update was treated as a path change")
	}
	if !kubernetesNetworkPathChanged(current, KubernetesInput{APIServer: "https://other.example:6443"}) {
		t.Fatal("API server change was not detected")
	}
	if !kubernetesNetworkPathChanged(current, KubernetesInput{ConnectionMode: "in_cluster"}) {
		t.Fatal("transition into in_cluster was not detected")
	}
}

func TestKubernetesConnectionPlanBindsTargetCredentialAndPath(t *testing.T) {
	credentialID := uuid.MustParse("018f47e2-9a4c-7b31-8acd-02a2475e8d2f")
	input := KubernetesInput{APIServer: "https://api.example:6443", ConnectionMode: "direct",
		CredentialID: uuid.NullUUID{UUID: credentialID, Valid: true}}
	plan := connectionPlan{TargetType: "kubernetes_cluster", Address: input.APIServer, ConnectionMode: input.ConnectionMode, CredentialID: input.CredentialID}
	if !kubernetesConnectionPlanMatches(plan, input, input.APIServer) {
		t.Fatal("matching Kubernetes connection plan was rejected")
	}
	input.APIServer = "https://other.example:6443"
	if kubernetesConnectionPlanMatches(plan, input, input.APIServer) {
		t.Fatal("Kubernetes Connection Test was reusable for a different API server")
	}
}
