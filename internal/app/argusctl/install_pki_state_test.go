package argusctl

import (
	"context"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestPreserveRuntimePKIStateAcrossPostRotationUpgrade(t *testing.T) {
	root, err := findRepoRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(root, "deploy", "profiles", "evaluation.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "argus-runtime-config", Namespace: cfg.Spec.Namespaces.System}, Data: map[string]string{
			"ARGUS_TRUST_BUNDLE_EPOCH": "1", "ARGUS_CONNECTOR_ISSUER_GENERATION": "2", "ARGUS_TELEMETRY_ISSUER_GENERATION": "2",
		}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cfg.trustSourceName(), Namespace: "cert-manager", Annotations: map[string]string{
			"argus.io/trust-bundle-epoch": "3",
		}}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: cfg.Spec.ReleaseID + "-root-ca", Namespace: "cert-manager", Annotations: map[string]string{
			"argus.io/pki-epoch": "2",
		}}},
	)
	values := map[string]any{"runtime": map[string]any{"trustBundleEpoch": int64(1)}}
	state, err := preserveRuntimePKIState(context.Background(), &kubeClients{typed: client}, cfg, values)
	if err != nil {
		t.Fatal(err)
	}
	if state.TrustBundleEpoch != 3 || state.IssuerGeneration != 2 {
		t.Fatalf("preserved PKI state = %#v", state)
	}
	runtimeValues := values["runtime"].(map[string]any)
	if runtimeValues["trustBundleEpoch"] != int64(3) || runtimeValues["connectorIssuerGeneration"] != int64(2) || runtimeValues["telemetryIssuerGeneration"] != int64(2) {
		t.Fatalf("runtime values did not preserve live PKI state: %#v", runtimeValues)
	}
}
