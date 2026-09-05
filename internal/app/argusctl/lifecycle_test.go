package argusctl

import (
	"context"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestOwnedCRDSelectorsIncludeRunScopedOpenSandboxRelease(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.ReleaseID = "argus-e2e-20260815-a15c"

	selectors := ownedCRDSelectors(cfg)
	want := "app.kubernetes.io/instance=" + cfg.upstreamReleaseName("os")
	if !slices.Contains(selectors, want) {
		t.Fatalf("owned CRD selectors %v do not contain %q", selectors, want)
	}
}

func TestDeleteManagedRootCASecret(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: "cert-manager",
		Name:      "argus-e2e-root-ca",
		Labels:    map[string]string{"argus.io/release-id": "argus-e2e", "argus.io/pki-role": "managed-root"},
	}})

	if err := deleteManagedRootCASecret(context.Background(), client, "argus-e2e"); err != nil {
		t.Fatal(err)
	}
	_, err := client.CoreV1().Secrets("cert-manager").Get(context.Background(), "argus-e2e-root-ca", metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("managed root CA Secret still exists: %v", err)
	}
}

func TestDeleteManagedRootCASecretRejectsDifferentOwner(t *testing.T) {
	client := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Namespace: "cert-manager",
		Name:      "argus-e2e-root-ca",
		Labels:    map[string]string{"argus.io/release-id": "another-release", "argus.io/pki-role": "managed-root"},
	}})

	if err := deleteManagedRootCASecret(context.Background(), client, "argus-e2e"); err == nil {
		t.Fatal("expected release ownership mismatch")
	}
	if _, err := client.CoreV1().Secrets("cert-manager").Get(context.Background(), "argus-e2e-root-ca", metav1.GetOptions{}); err != nil {
		t.Fatalf("mismatched Secret should remain: %v", err)
	}
}
