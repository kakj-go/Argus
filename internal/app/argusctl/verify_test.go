package argusctl

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestOpenSandboxSmokePodUsesWorkloadImageReference(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.Namespaces.Sandbox = "argus-sandbox"
	cfg.Spec.Images = Images{
		Mode:       "local-registry",
		Registry:   "host.docker.internal:5001",
		Tag:        "e2e-test",
		PullPolicy: "Never",
	}

	manifest := openSandboxSmokePod(cfg, "argus-opensandbox-smoke")
	if !strings.Contains(manifest, "image: host.docker.internal:5001/argus/argus-backend:e2e-test") {
		t.Fatalf("smoke pod image does not match the installed workload image:\n%s", manifest)
	}
	if strings.Contains(manifest, "image: localhost:5001/") {
		t.Fatalf("smoke pod must not use the host-only registry reference:\n%s", manifest)
	}
	if !strings.Contains(manifest, "imagePullPolicy: Never") {
		t.Fatalf("smoke pod did not preserve the configured pull policy:\n%s", manifest)
	}
}

func TestLocalRegistryManifestDigestUsesLoopbackRegistryAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodHead || request.URL.Path != "/v2/argus/argus-backend/manifests/dev" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Docker-Content-Digest", "sha256:abc123")
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &InstallConfig{Spec: InstallSpec{Images: Images{Mode: "local-registry", Registry: parsed.Host, Tag: "dev"}}}
	digest, err := localRegistryManifestDigest(context.Background(), cfg, "argus-backend")
	if err != nil {
		t.Fatal(err)
	}
	if digest != "sha256:abc123" {
		t.Fatalf("digest = %q", digest)
	}
}

func TestRotationEpochAndStagedIngressMetadataAreExact(t *testing.T) {
	epoch, ok := rotationEpochFromFormerIssuer("argus", "argus-ca-former-42")
	if !ok || epoch != 42 {
		t.Fatalf("former Issuer epoch = %d, ok=%v", epoch, ok)
	}
	for _, invalid := range []string{"argus-ca", "other-ca-former-42", "argus-ca-former-0", "argus-ca-former-nope"} {
		if _, ok = rotationEpochFromFormerIssuer("argus", invalid); ok {
			t.Fatalf("invalid former Issuer %q was accepted", invalid)
		}
	}
	staged := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{
			"name":        "argus-enterprise-next-42",
			"labels":      map[string]any{"argus.io/release-id": "argus", pkiRoleLabel: pkiStagedServerRole},
			"annotations": map[string]any{pkiEpochAnnotation: "42", "argus.io/pki-direction": "forward", pkiSourceCertificate: "argus-enterprise", pkiSourceSecret: "argus-enterprise-tls", pkiTargetIssuer: "argus-ca"},
		},
	}}
	if err := validateStagedIngressCertificateMetadata(staged, "argus", 42, "argus-enterprise", "argus-enterprise-tls", "argus-ca"); err != nil {
		t.Fatalf("valid staged metadata rejected: %v", err)
	}
	staged.SetAnnotations(map[string]string{pkiEpochAnnotation: "42", "argus.io/pki-direction": "rollback"})
	if err := validateStagedIngressCertificateMetadata(staged, "argus", 42, "argus-enterprise", "argus-enterprise-tls", "argus-ca"); err == nil {
		t.Fatal("rollback/incomplete staged metadata was accepted as a forward replacement")
	}
}

func TestMinioSmokePodRetriesTransientWriteReadiness(t *testing.T) {
	manifest := minioSmokePod(&InstallConfig{Spec: InstallSpec{Namespaces: Namespaces{System: "argus-system"}}})
	for _, expected := range []string{"seq 1 12", "sleep 5", "MinIO object roundtrip did not become writable after bounded retries"} {
		if !strings.Contains(manifest, expected) {
			t.Fatalf("MinIO smoke pod is missing bounded retry %q:\n%s", expected, manifest)
		}
	}
}

func TestValidateIngressCertificateAcceptsDedicatedServerCertificate(t *testing.T) {
	certificate := map[string]any{
		"spec": map[string]any{
			"secretName": "argus-platform-tls",
			"dnsNames":   []any{"platform.argus.test"},
			"usages":     []any{"server auth"},
			"issuerRef":  map[string]any{"name": "argus-ca", "kind": "ClusterIssuer"},
		},
		"status": map[string]any{
			"conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
		},
	}
	if err := validateIngressCertificate(certificate, "argus-platform", "argus-platform-tls", "platform.argus.test", "argus-ca"); err != nil {
		t.Fatalf("valid dedicated Certificate rejected: %v", err)
	}
}

func TestValidateIngressCertificateRejectsWrongIdentityOrUsage(t *testing.T) {
	tests := []struct {
		name        string
		certificate map[string]any
		want        string
	}{
		{
			name: "wrong secret",
			certificate: map[string]any{
				"spec": map[string]any{"secretName": "legacy-secret", "dnsNames": []any{"enterprise.argus.test"}, "usages": []any{"server auth"},
					"issuerRef": map[string]any{"name": "argus-ca", "kind": "ClusterIssuer"}},
				"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
			},
			want: "expected argus-enterprise-tls",
		},
		{
			name: "multiple usages",
			certificate: map[string]any{
				"spec": map[string]any{"secretName": "argus-enterprise-tls", "dnsNames": []any{"enterprise.argus.test"},
					"usages": []any{"server auth", "client auth"}, "issuerRef": map[string]any{"name": "argus-ca", "kind": "ClusterIssuer"}},
				"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
			},
			want: "only server auth",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateIngressCertificate(test.certificate, "argus-enterprise", "argus-enterprise-tls", "enterprise.argus.test", "argus-ca")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateIngressCertificate() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestCurlStatusArgsPinsIngressAndBypassesProxy(t *testing.T) {
	args, err := curlStatusArgs(
		"https://platform.argus.test/api/v1/setup/status",
		"https://platform.argus.test",
		"127.0.0.1",
		"/tmp/argus-ca.pem",
	)
	if err != nil {
		t.Fatal(err)
	}
	wantPairs := [][]string{
		{"--cacert", "/tmp/argus-ca.pem"},
		{"--noproxy", "*"},
		{"--connect-to", "platform.argus.test:443:127.0.0.1:443"},
		{"-H", "Origin: https://platform.argus.test"},
	}
	for _, pair := range wantPairs {
		found := false
		for index := 0; index+1 < len(args); index++ {
			if reflect.DeepEqual(args[index:index+2], pair) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("curl args %#v do not contain pair %#v", args, pair)
		}
	}
	for _, forbidden := range []string{"-k", "--insecure", "-ksS"} {
		for _, argument := range args {
			if argument == forbidden {
				t.Fatalf("curl args contain TLS bypass %q: %#v", forbidden, args)
			}
		}
	}
}

func TestWebIngressHostsExcludeLegacyRemoteDomain(t *testing.T) {
	cfg := &InstallConfig{}
	cfg.Spec.Exposure.EnterpriseHost = "enterprise.argus.test"
	cfg.Spec.Exposure.PlatformHost = "platform.argus.test"
	hosts := webIngressHosts(cfg)
	want := []string{"enterprise.argus.test", "platform.argus.test", "cards.argus.test", "artifacts.argus.test"}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("webIngressHosts() = %#v, want %#v", hosts, want)
	}
	for _, host := range hosts {
		if strings.HasPrefix(host, "remote.") {
			t.Fatalf("legacy remote domain must not be part of ingress hosts: %#v", hosts)
		}
	}
}
