package argusctl

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestEncryptedBackupArchiveRoundTripAndTamperDetection(t *testing.T) {
	source := t.TempDir()
	if err := writePrivate(filepath.Join(source, "manifest.json"), []byte(`{"format_version":1}`)); err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("argus-local-hardening\n"), 100000)
	if err := writePrivate(filepath.Join(source, "payload.bin"), payload); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, 32)
	archive := filepath.Join(t.TempDir(), "backup.argusbak")
	if err := encryptDirectory(source, archive, key); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := decryptArchive(archive, target, key); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(target, "payload.bin"))
	if err != nil || !bytes.Equal(restored, payload) {
		t.Fatal("backup payload did not round-trip")
	}
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] ^= 0xff
	if err := os.WriteFile(archive, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := decryptArchive(archive, t.TempDir(), key); err == nil {
		t.Fatal("tampered backup was accepted")
	}
}

func TestRestoreTargetMustBeDistinct(t *testing.T) {
	manifest := backupManifest{ReleaseID: "source", Namespaces: Namespaces{System: "source-system", Sandbox: "source-sandbox", Observability: "source-observability"}}
	cfg := &InstallConfig{}
	cfg.Spec.ReleaseID = "target"
	cfg.Spec.Namespaces = Namespaces{System: "target-system", Sandbox: "target-sandbox", Observability: "target-observability"}
	if err := ensureDistinctRestoreTarget(cfg, manifest); err != nil {
		t.Fatalf("distinct target rejected: %v", err)
	}
	cfg.Spec.Namespaces.System = manifest.Namespaces.System
	if err := ensureDistinctRestoreTarget(cfg, manifest); err == nil {
		t.Fatal("source namespace reuse was accepted")
	}
}

func TestOpenBaoMaintenanceCommandsSelectUnsealContainer(t *testing.T) {
	kube := []string{"--context", "test", "--namespace", "argus-system"}
	execArgs := openBaoExecArgs(kube, "bao status")
	if !slices.Contains(execArgs, "--container") || !slices.Contains(execArgs, openBaoMaintenanceContainer) {
		t.Fatalf("OpenBao exec does not select the maintenance container: %v", execArgs)
	}
	copyArgs := openBaoCopyArgs(kube, "argus-openbao-0:/tmp/snapshot", t.TempDir())
	if !slices.Contains(copyArgs, "--container") || !slices.Contains(copyArgs, openBaoMaintenanceContainer) {
		t.Fatalf("OpenBao copy does not select the maintenance container: %v", copyArgs)
	}
	if len(kube) != 4 {
		t.Fatalf("OpenBao argument helpers mutated the shared kubectl prefix: %v", kube)
	}
}

func TestOpenBaoSnapshotSaveUsesSupportedArguments(t *testing.T) {
	command := openBaoSnapshotSaveCommand("/tmp/argus-m8.snap")
	if strings.Contains(command, "snapshot save -force") {
		t.Fatalf("OpenBao snapshot save still uses the unsupported -force flag: %s", command)
	}
	if !strings.Contains(command, "rm -f /tmp/argus-m8.snap") || !strings.Contains(command, "snapshot save /tmp/argus-m8.snap") {
		t.Fatalf("OpenBao snapshot save is not idempotent: %s", command)
	}
}

func TestMinIOVolumeMaintenancePodMountsTheExistingClaim(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.Namespaces.System = "argus-system"
	cfg.Spec.Images = Images{
		Mode:       "local-registry",
		Registry:   "host.docker.internal:5001",
		Tag:        "restore-test",
		PullPolicy: "Never",
	}
	manifest := minioVolumeMaintenancePod(cfg, "argus-minio-restore", "data-argus-minio-0")
	for _, expected := range []string{
		"namespace: argus-system",
		"image: host.docker.internal:5001/argus/minio:restore-test",
		"imagePullPolicy: Never",
		"fsGroup: 1000",
		"claimName: data-argus-minio-0",
		"test -w /data",
	} {
		if !strings.Contains(manifest, expected) {
			t.Fatalf("MinIO maintenance pod is missing %q:\n%s", expected, manifest)
		}
	}
}
