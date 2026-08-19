package collectormanager

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestApplyKubernetesUsesFixedTypedResources(t *testing.T) {
	client := fake.NewClientset()
	command := collectorCommand(testConfigBundle(t), "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
	command.ResourceType = "kubernetes_cluster"
	command.KubernetesImage = "registry.example/argus-otelcol:test"
	identity := testKubernetesIdentity(t, command.GetCollectorId())
	result, err := (Manager{}).ApplyKubernetes(context.Background(), command, KubernetesOptions{Client: client, Image: "registry.example/argus-otelcol:test", IdentityMaterial: &identity})
	if err != nil {
		t.Fatalf("apply Kubernetes Collector: %v", err)
	}
	if result.Status != "converged" {
		t.Fatalf("unexpected result: %#v", result)
	}
	daemonSet, err := client.AppsV1().DaemonSets(KubernetesNamespace).Get(context.Background(), "argus-otelcol-agent", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("fixed DaemonSet missing: %v", err)
	}
	agent := daemonSet.Spec.Template.Spec.Containers[0]
	if !slices.ContainsFunc(agent.Env, func(value corev1.EnvVar) bool {
		return value.Name == "K8S_NODE_NAME" && value.ValueFrom != nil && value.ValueFrom.FieldRef != nil && value.ValueFrom.FieldRef.FieldPath == "spec.nodeName"
	}) {
		t.Fatal("DaemonSet does not inject the kubelet target Node name")
	}
	if !slices.ContainsFunc(agent.VolumeMounts, func(value corev1.VolumeMount) bool {
		return value.Name == "podlogs" && value.MountPath == "/var/log/pods" && value.ReadOnly
	}) {
		t.Fatal("DaemonSet does not mount Kubernetes pod logs read-only")
	}
	if !slices.ContainsFunc(agent.VolumeMounts, func(value corev1.VolumeMount) bool {
		return value.Name == "kubelet-pki" && value.MountPath == "/var/run/argus-kubelet/pki" && value.ReadOnly
	}) {
		t.Fatal("DaemonSet does not mount the kubelet public certificate chain read-only")
	}
	roleName := "argus-otelcol-" + strings.ReplaceAll(command.GetCollectorId(), "-", "")[:12]
	role, err := client.RbacV1().ClusterRoles().Get(context.Background(), roleName, metav1.GetOptions{})
	if err != nil || !hasResource(role.Rules, "batch", "jobs") || !hasResource(role.Rules, "autoscaling", "horizontalpodautoscalers") ||
		!hasResource(role.Rules, "apps", "replicasets") || !hasResource(role.Rules, "", "nodes/stats") {
		t.Fatal("fixed Collector RBAC does not cover enabled Kubernetes receivers")
	}
	deployment, err := client.AppsV1().Deployments(KubernetesNamespace).Get(context.Background(), "argus-otelcol-gateway", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("fixed Gateway missing: %v", err)
	}
	container := deployment.Spec.Template.Spec.Containers[0]
	if deployment.Spec.MinReadySeconds != 5 || container.StartupProbe == nil || container.ReadinessProbe == nil || container.LivenessProbe == nil {
		t.Fatal("Gateway identity readiness is not part of Collector convergence")
	}
	if container.ReadinessProbe.HTTPGet == nil || container.ReadinessProbe.HTTPGet.Port.StrVal != "health" {
		t.Fatal("Gateway readiness does not use the fixed health endpoint")
	}
	if _, err = (Manager{}).ApplyKubernetes(context.Background(), command, KubernetesOptions{Client: client, Image: "registry.example/argus-otelcol:other", IdentityMaterial: &identity}); err != ErrInvalidCommand {
		t.Fatalf("mismatched frozen Kubernetes image returned %v", err)
	}
	command.Operation = "uninstall"
	if _, err = (Manager{}).ApplyKubernetes(context.Background(), command, KubernetesOptions{Client: client, Image: "registry.example/argus-otelcol:test"}); err != nil {
		t.Fatalf("uninstall Kubernetes Collector: %v", err)
	}
}

func testKubernetesIdentity(t *testing.T, collectorID string) IdentityMaterial {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-ca"}, NotBefore: now.Add(-time.Minute),
		NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	uri, _ := url.Parse("spiffe://argus/telemetry/collectors/" + collectorID)
	leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "argus-otelcol"}, URIs: []*url.URL{uri},
		DNSNames:  []string{"collector-" + collectorID + ".argus.telemetry"},
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTemplate, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalPKCS8PrivateKey(key)
	return IdentityMaterial{CertificatePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), CABundlePEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})}
}

func hasResource(rules []rbacv1.PolicyRule, group, resource string) bool {
	return slices.ContainsFunc(rules, func(rule rbacv1.PolicyRule) bool {
		return slices.Contains(rule.APIGroups, group) && slices.Contains(rule.Resources, resource)
	})
}
