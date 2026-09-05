package connector

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/storage/postgres"
)

func TestNewBastionServiceRequiresSharedRuntimeInputs(t *testing.T) {
	t.Parallel()
	key := bytes.Repeat([]byte{0x42}, 32)
	store := new(postgres.Store)
	for name, candidate := range map[string]struct {
		store         *postgres.Store
		key           []byte
		enrollTarget  string
		gatewayTarget string
	}{
		"store":          {key: key, enrollTarget: "enroll:8443", gatewayTarget: "gateway:9443"},
		"key":            {store: store, key: key[:31], enrollTarget: "enroll:8443", gatewayTarget: "gateway:9443"},
		"enroll target":  {store: store, key: key, gatewayTarget: "gateway:9443"},
		"gateway target": {store: store, key: key, enrollTarget: "enroll:8443"},
	} {
		candidate := candidate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewBastionService(candidate.store, resource.PendingActionService{}, Service{}, candidate.key,
				candidate.enrollTarget, candidate.gatewayTarget); err == nil {
				t.Fatal("incomplete runtime configuration was accepted")
			}
		})
	}
	service, err := NewBastionService(store, resource.PendingActionService{}, Service{}, key, "enroll:8443", "gateway:9443")
	if err != nil {
		t.Fatal(err)
	}
	key[0] ^= 0xff
	if service.OperationSecretKey[0] == key[0] {
		t.Fatal("operation secret key aliases caller memory")
	}
}

func TestBastionSnapshotStatusReplacementAllowsOfflineTransition(t *testing.T) {
	for _, status := range []string{"active", "suspected_offline", "offline", "uninstalled"} {
		if got := bastionSnapshotStatus("replace", status); got != "replaceable" {
			t.Fatalf("replace status %q normalized to %q", status, got)
		}
	}
}

func TestReplacementStatusRejectsPendingScope(t *testing.T) {
	if replacementStatusAllowed("pending") {
		t.Fatal("pending scope without an active Connector must not be replaceable")
	}
}

func TestBastionSnapshotStatusOtherOperationsRemainExact(t *testing.T) {
	if got := bastionSnapshotStatus("delete", "offline"); got != "offline" {
		t.Fatalf("delete status normalized to %q", got)
	}
}

func TestPlanV4SchemaLetsReplacementReuseStableInstanceID(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	migration, err := os.ReadFile(filepath.Join(root, "migrations", "postgresql", "00031_p4_connector_install_operations.sql"))
	if err != nil {
		t.Fatal(err)
	}
	up := strings.SplitN(string(migration), "-- +goose Down", 2)[0]
	for _, required := range []string{
		"DROP CONSTRAINT connectors_enterprise_id_instance_id_key",
		"CREATE UNIQUE INDEX connectors_enterprise_instance_live_unique",
		"WHERE status NOT IN ('revoked','uninstalled')",
	} {
		if !strings.Contains(up, required) {
			t.Fatalf("PlanV4 Connector replacement schema is missing %q", required)
		}
	}
}

func TestBastionRootHostLifecycleKeepsDeletedNamesReusable(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))
	base, err := os.ReadFile(filepath.Join(root, "migrations", "postgresql", "00002_m3_resources_connectors.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"bastion_scopes_name_unique ON bastion_scopes (enterprise_id, lower(name)) WHERE status <> 'deleted'",
		"hosts_name_unique ON hosts (enterprise_id, lower(name)) WHERE status <> 'deleted'",
	} {
		if !strings.Contains(string(base), required) {
			t.Fatalf("live-name uniqueness is missing %q", required)
		}
	}

	lifecycle, err := os.ReadFile(filepath.Join(root, "migrations", "postgresql", "00033_bastion_root_host_lifecycle.sql"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"scope.status = 'deleted'",
		"SET connector_host_id = root.id",
		"hosts_bastion_root_live_unique",
		"status <> 'deleted'",
	} {
		if !strings.Contains(string(lifecycle), required) {
			t.Fatalf("Bastion root lifecycle migration is missing %q", required)
		}
	}
}
