package connector

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestConnectorReleaseSelectsSignedArchitecture(t *testing.T) {
	manifest, err := json.Marshal(connectorReleaseManifest{SchemaVersion: "argus.connector_release/v2", InstallScriptSHA256: strings.Repeat("e", 64),
		SigningKeyID: "release-key", SigningPublicKey: strings.Repeat("a", 43), Artifacts: []connectorReleaseArtifact{
			{Architecture: "amd64", URI: "https://artifacts.invalid/amd64", SHA256: strings.Repeat("a", 64),
				Signature: strings.Repeat("b", 86), SigningKeyID: "release-key", ByteSize: 100},
			{Architecture: "arm64", URI: "https://artifacts.invalid/arm64", SHA256: strings.Repeat("c", 64),
				Signature: strings.Repeat("d", 86), SigningKeyID: "release-key", ByteSize: 101},
		}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := connectorArtifactForArchitecture(manifest, "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Architecture != "arm64" || artifact.SigningKeyID != "release-key" {
		t.Fatalf("artifact = %#v", artifact)
	}
	var invalid connectorReleaseManifest
	if json.Unmarshal(manifest, &invalid) != nil {
		t.Fatal("decode manifest")
	}
	invalid.Artifacts[0].SigningKeyID = "other-key"
	broken, _ := json.Marshal(invalid)
	if _, err = connectorArtifactForArchitecture(broken, "amd64"); err == nil {
		t.Fatal("artifact signed by a key outside the release trust record must be rejected")
	}
}

func TestModeAManifestRequiresBothArchitecturesAndInstallerURLs(t *testing.T) {
	manifest := connectorReleaseManifest{SchemaVersion: "argus.connector_release/v2", ManifestURI: "https://artifacts.invalid/manifest.json",
		InstallScriptURI: "https://artifacts.invalid/install.sh", InstallScriptSHA256: strings.Repeat("e", 64), SigningKeyID: "release-key", SigningPublicKey: strings.Repeat("a", 43),
		Artifacts: []connectorReleaseArtifact{
			{Architecture: "amd64", URI: "https://artifacts.invalid/amd64", SHA256: strings.Repeat("a", 64), Signature: strings.Repeat("b", 86), SigningKeyID: "release-key", ByteSize: 100},
			{Architecture: "arm64", URI: "https://artifacts.invalid/arm64", SHA256: strings.Repeat("c", 64), Signature: strings.Repeat("d", 86), SigningKeyID: "release-key", ByteSize: 101},
		}}
	raw, _ := json.Marshal(manifest)
	if _, err := connectorManualInstallRelease(raw); err != nil {
		t.Fatalf("complete Mode A manifest rejected: %v", err)
	}
	manifest.Artifacts = manifest.Artifacts[:1]
	raw, _ = json.Marshal(manifest)
	if _, err := connectorManualInstallRelease(raw); err == nil {
		t.Fatal("Mode A manifest without arm64 was accepted")
	}
}

func TestConnectorBootstrapScriptURLUsesEnrollmentOrigin(t *testing.T) {
	if got := connectorBootstrapScriptURL("https://argus.example.com/"); got != "https://argus.example.com/api/v1/connectors/bootstrap-script" {
		t.Fatalf("connectorBootstrapScriptURL() = %q", got)
	}
}
