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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/kakj-go/Argus/internal/trustbundle"
)

func TestApplyKubernetesUsesFixedTypedResources(t *testing.T) {
	client := fake.NewClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: KubernetesNamespace, Labels: map[string]string{"app.kubernetes.io/part-of": "argus"}}})
	command := collectorCommand(t, testConfigBundle(t), "linux_arm64", "https://artifacts.example/collector.tar.gz", []byte("artifact"))
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
	if !slices.ContainsFunc(agent.VolumeMounts, func(value corev1.VolumeMount) bool {
		return value.Name == "identity-bootstrap" && value.MountPath == "/var/run/argus-bootstrap/identity" && value.ReadOnly
	}) || !slices.ContainsFunc(agent.VolumeMounts, func(value corev1.VolumeMount) bool {
		return value.Name == "identity-state" && value.MountPath == "/var/lib/argus-otelcol/identity" && !value.ReadOnly
	}) || !slices.ContainsFunc(daemonSet.Spec.Template.Spec.Volumes, func(value corev1.Volume) bool {
		return value.Name == "identity-state" && value.EmptyDir != nil
	}) {
		t.Fatal("Collector identity is not bootstrapped into a writable runtime volume")
	}
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
	if roles, listErr := client.RbacV1().ClusterRoles().List(context.Background(), metav1.ListOptions{}); listErr != nil || len(roles.Items) != 0 {
		t.Fatal("runtime Collector convergence must not create cluster-scoped RBAC")
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
	if !slices.ContainsFunc(container.Env, func(value corev1.EnvVar) bool {
		return value.Name == "ARGUS_TELEMETRY_KUBERNETES_MIRROR" && value.Value == "1"
	}) {
		t.Fatal("Gateway does not persist rotated identity material")
	}
	identitySecret, err := client.CoreV1().Secrets(KubernetesNamespace).Get(context.Background(), collectorIdentitySecret, metav1.GetOptions{})
	if err != nil || len(identitySecret.Data["trust-bundle.json"]) == 0 {
		t.Fatal("Collector bootstrap Secret is missing versioned Trust Bundle state")
	}
	identityRole, err := client.RbacV1().Roles(KubernetesNamespace).Get(context.Background(), "argus-otelcol-identity", metav1.GetOptions{})
	if err != nil || !slices.ContainsFunc(identityRole.Rules, func(rule rbacv1.PolicyRule) bool {
		return slices.Contains(rule.Resources, "secrets") && slices.Equal(rule.ResourceNames, []string{collectorIdentitySecret})
	}) {
		t.Fatal("Collector identity persistence is not constrained to its fixed Secret")
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
	issue := func(serial int64, uriValue string, dnsNames []string, usage x509.ExtKeyUsage) ([]byte, []byte) {
		key, keyErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if keyErr != nil {
			t.Fatal(keyErr)
		}
		uri, _ := url.Parse(uriValue)
		leafTemplate := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "argus-otelcol"}, URIs: []*url.URL{uri},
			DNSNames: dnsNames, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature,
			ExtKeyUsage: []x509.ExtKeyUsage{usage}}
		leafDER, issueErr := x509.CreateCertificate(rand.Reader, leafTemplate, ca, &key.PublicKey, caKey)
		if issueErr != nil {
			t.Fatal(issueErr)
		}
		privateDER, marshalErr := x509.MarshalPKCS8PrivateKey(key)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	}
	clientCertificate, clientKey := issue(2, "spiffe://argus/telemetry/collectors/"+collectorID, nil, x509.ExtKeyUsageClientAuth)
	serverCertificate, serverKey := issue(3, "spiffe://argus/telemetry/collector-servers/"+collectorID,
		[]string{"collector-" + collectorID + ".argus.telemetry"}, x509.ExtKeyUsageServerAuth)
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	trust, err := trustbundle.Parse(caPEM, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	return IdentityMaterial{ClientCertificatePEM: clientCertificate, ClientPrivateKeyPEM: clientKey,
		ServerCertificatePEM: serverCertificate, ServerPrivateKeyPEM: serverKey, CABundlePEM: trust.PEM,
		TrustBundleEpoch: 1, TrustBundleSHA256: trust.SHA256, TrustBundleCAFingerprints: trust.Fingerprints}
}
