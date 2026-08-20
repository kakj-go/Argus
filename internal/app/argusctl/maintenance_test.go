package argusctl

import (
	"bytes"
	"os"
	"path/filepath"
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
