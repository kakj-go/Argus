package pkirotation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/fake"
	kubernetesfake "k8s.io/client-go/kubernetes/fake"

	"github.com/kakj-go/Argus/internal/trustbundle"
)

func TestStagedServerCutoverKeepsSourceUntilPromotion(t *testing.T) {
	ca, caKey, caPEM := rotationTestCA(t)
	leafPEM, keyPEM := rotationTestLeaf(t, ca, caKey)
	material, err := trustbundle.Parse(caPEM, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	const namespace = "argus-system"
	source := rotationCertificate("argus-gateway", namespace, "argus-gateway-tls", "argus-ca-former", nil, nil)
	stageLabels := map[string]string{"argus.io/release-id": "argus", pkiRoleLabel: pkiStagedServerRole}
	stageAnnotations := map[string]string{pkiEpochAnnotation: "2", pkiSourceCertificate: "argus-gateway",
		pkiSourceSecret: "argus-gateway-tls", pkiTargetIssuer: "argus-ca", pkiDirection: trustbundle.DirectionForward}
	stage := rotationCertificate("argus-gateway-next-2", namespace, "argus-gateway-next-2", "argus-ca", stageLabels, stageAnnotations)
	issuer := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer", "metadata": map[string]any{"name": "argus-ca"},
		"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	}}
	dynamicClient := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		certificateGVR: "CertificateList", issuerGVR: "ClusterIssuerList",
	}, source, stage, issuer)
	// The dynamic fake's concrete map key is the Kubernetes schema type. Keep
	// construction in a helper so the test remains explicit about list kinds.
	_ = dynamicClient

	typed := kubernetesfake.NewSimpleClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argus-gateway-tls", Namespace: namespace}, Data: map[string][]byte{
			corev1.TLSCertKey: []byte("former"), corev1.TLSPrivateKeyKey: []byte("former-key"),
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argus-gateway-next-2", Namespace: namespace}, Data: map[string][]byte{
			corev1.TLSCertKey: leafPEM, corev1.TLSPrivateKeyKey: keyPEM, "ca.crt": caPEM,
		}},
	)
	reconciler := Reconciler{Typed: typed, Dynamic: dynamicClient, Config: Config{ReleaseID: "argus", Namespaces: []string{namespace}}}
	if _, err = reconciler.loadStagedServers(context.Background(), 2, trustbundle.DirectionRollback, material); err == nil {
		t.Fatal("load staged server accepted a stage from the opposite rotation direction")
	}
	staged, err := reconciler.loadStagedServers(context.Background(), 2, trustbundle.DirectionForward, material)
	if err != nil {
		t.Fatalf("load staged server: %v", err)
	}
	if len(staged) != 1 {
		t.Fatalf("staged servers=%d, want 1", len(staged))
	}
	live, err := typed.CoreV1().Secrets(namespace).Get(context.Background(), "argus-gateway-tls", metav1.GetOptions{})
	if err != nil || string(live.Data[corev1.TLSCertKey]) != "former" {
		t.Fatalf("staging changed the serving Secret: %v", err)
	}
	if err = reconciler.promoteStagedServers(context.Background(), staged, material, time.Second); err != nil {
		t.Fatalf("promote staged server: %v", err)
	}
	live, err = typed.CoreV1().Secrets(namespace).Get(context.Background(), "argus-gateway-tls", metav1.GetOptions{})
	if err != nil || string(live.Data[corev1.TLSCertKey]) != string(leafPEM) || string(live.Data[corev1.TLSPrivateKeyKey]) != string(keyPEM) {
		t.Fatalf("serving Secret was not atomically replaced: %v", err)
	}
	updated, err := dynamicClient.Resource(certificateGVR).Namespace(namespace).Get(context.Background(), "argus-gateway", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	issuerName, _, _ := unstructured.NestedString(updated.Object, "spec", "issuerRef", "name")
	if issuerName != "argus-ca" {
		t.Fatalf("source Certificate issuer=%q, want argus-ca", issuerName)
	}
}

func TestControlPlaneNodeIDRequiresCurrentReadyPod(t *testing.T) {
	ready := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "argus-worker-current", Labels: map[string]string{"app.kubernetes.io/name": "argus-worker"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Args: []string{"--pool=agent"}}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	if id, ok := controlPlaneNodeID(ready); !ok || id != "worker-agent/argus-worker-current" {
		t.Fatalf("unexpected Ready worker identity %q, ok=%v", id, ok)
	}
	notReady := ready.DeepCopy()
	notReady.Status.Conditions[0].Status = corev1.ConditionFalse
	if _, ok := controlPlaneNodeID(notReady); ok {
		t.Fatal("non-Ready Pod was treated as an active control-plane identity")
	}
	terminating := ready.DeepCopy()
	now := metav1.NewTime(time.Now())
	terminating.DeletionTimestamp = &now
	if _, ok := controlPlaneNodeID(terminating); ok {
		t.Fatal("terminating Pod was treated as an active control-plane identity")
	}
	unrelated := ready.DeepCopy()
	unrelated.Labels["app.kubernetes.io/name"] = "argus-web"
	if _, ok := controlPlaneNodeID(unrelated); ok {
		t.Fatal("unrelated Ready Pod was treated as a Bundle acknowledger")
	}
}

func rotationCertificate(name, namespace, secret, issuer string, labels, annotations map[string]string) *unstructured.Unstructured {
	if labels == nil {
		labels = map[string]string{"argus.io/release-id": "argus"}
	}
	value := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "cert-manager.io/v1", "kind": "Certificate",
		"metadata": map[string]any{"name": name, "namespace": namespace},
		"spec": map[string]any{"secretName": secret, "usages": []any{"server auth"},
			"issuerRef": map[string]any{"name": issuer, "kind": "ClusterIssuer", "group": "cert-manager.io"}},
	}}
	value.SetLabels(labels)
	value.SetAnnotations(annotations)
	return value
}

func rotationTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "next-root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
}

func rotationTestLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "argus-gateway"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(12 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	raw, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encodedKey})
}
