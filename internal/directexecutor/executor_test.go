package directexecutor

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

func TestClassifyConnectionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "refused", err: syscall.ECONNREFUSED, want: "CONNECTION_REFUSED"},
		{name: "unroutable", err: syscall.EHOSTUNREACH, want: "TARGET_UNROUTABLE"},
		{name: "auth", err: errors.New("SSH authentication failed"), want: "AUTH_FAILED"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyConnectionError(test.err); got != test.want {
				t.Fatalf("classifyConnectionError() = %q, want %q", got, test.want)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func TestCollectorOperationPlanHashSurvivesJSONBNormalization(t *testing.T) {
	original, err := resource.CanonicalJSON([]byte(`{"collector_id":"018f08d2-7d43-7a54-a8fb-f2f3f2f0d111","operation":"install","artifact":{"platform":"linux_arm64","byte_size":"1024"}}`))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := resource.CanonicalJSON([]byte(`{ "artifact": { "byte_size": "1024", "platform": "linux_arm64" }, "operation": "install", "collector_id": "018f08d2-7d43-7a54-a8fb-f2f3f2f0d111" }`))
	if err != nil {
		t.Fatal(err)
	}
	left, right := sha256.Sum256(original), sha256.Sum256(stored)
	if !bytes.Equal(left[:], right[:]) {
		t.Fatal("jsonb-equivalent Collector plans produced different hashes")
	}
}

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestVerifyEgressRequiresAdvertisedObservedAddress(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Scheme != "https" {
			t.Fatal("verification request must use HTTPS")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"ip":"203.0.113.10"}`))}, nil
	})}
	if observed, err := verifyEgress(context.Background(), "https://egress.example/check", []string{"203.0.113.10"}, client); err != nil || observed != "203.0.113.10" {
		t.Fatalf("unexpected verification result observed=%q err=%v", observed, err)
	}
	if observed, err := verifyEgress(context.Background(), "https://egress.example/check", []string{"203.0.113.11"}, client); err == nil || observed != "203.0.113.10" {
		t.Fatal("expected mismatched egress address to fail")
	}
}

func TestVerifyEgressRejectsUnsafeEndpointAndOversizedResponse(t *testing.T) {
	if _, err := verifyEgress(context.Background(), "http://egress.example/check", []string{"203.0.113.10"}, http.DefaultClient); err == nil {
		t.Fatal("expected non-HTTPS verification endpoint to fail")
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(make([]byte, 4097)))}, nil
	})}
	if _, err := verifyEgress(context.Background(), "https://egress.example/check", []string{"203.0.113.10"}, client); err == nil {
		t.Fatal("expected oversized verification response to fail")
	}
}

func TestVerifyEgressAllowsObservedAddressWithoutAdvertisement(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("203.0.113.10"))}, nil
	})}
	observed, err := verifyEgress(context.Background(), "https://egress.example/check", nil, client)
	if err != nil || observed != "203.0.113.10" {
		t.Fatalf("expected observed-only verification, got observed=%q err=%v", observed, err)
	}
}

func TestCaptureHostKeyUsesSHA256Fingerprint(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := ""
	if err := captureHostKey(&fingerprint)("host.example", nil, key); err != nil {
		t.Fatal(err)
	}
	if fingerprint != ssh.FingerprintSHA256(key) || !strings.HasPrefix(fingerprint, "SHA256:") {
		t.Fatalf("unexpected fingerprint %q", fingerprint)
	}
}

func TestSafeKubernetesConfigRejectsTLSAndExecBypass(t *testing.T) {
	valid := []byte(`apiVersion: v1
kind: Config
clusters:
- name: target
  cluster:
    server: https://api.example:6443
    certificate-authority-data: Y2E=
contexts:
- name: target
  context: {cluster: target, user: target}
current-context: target
users:
- name: target
  user: {token: test}
`)
	if _, err := safeKubernetesConfig(valid); err != nil {
		t.Fatalf("expected bounded kubeconfig to pass: %v", err)
	}
	for name, mutation := range map[string]string{
		"insecure": strings.ReplaceAll(string(valid), "certificate-authority-data: Y2E=", "insecure-skip-tls-verify: true"),
		"exec":     strings.ReplaceAll(string(valid), "user: {token: test}", "user:\n    exec: {apiVersion: client.authentication.k8s.io/v1, command: sh}"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := safeKubernetesConfig([]byte(mutation)); err == nil {
				t.Fatal("expected unsafe kubeconfig to fail")
			}
		})
	}
}

func TestRedirectsAreAlwaysRejected(t *testing.T) {
	request := &http.Request{URL: &url.URL{Scheme: "https", Host: "other.example"}}
	if err := rejectRedirect(request, nil); err == nil {
		t.Fatal("expected redirect rejection")
	}
}

func TestParseAddressesRejectsInvalidValues(t *testing.T) {
	if _, err := parseAddresses([]string{"not-an-ip"}); !errors.Is(err, resource.ErrDirectTargetDenied) {
		t.Fatalf("expected invalid address denial, got %v", err)
	}
}

func TestConnectorInstallTunnelErrorsAreStable(t *testing.T) {
	tests := map[error]string{
		errHostKeyMismatch:              "SSH_HOST_KEY_CHANGED",
		errTunnelQuotaExceeded:          "TUNNEL_QUOTA_EXCEEDED",
		secret.ErrCredentialUnavailable: "CREDENTIAL_UNAVAILABLE",
		errCredentialVersionStale:       "CREDENTIAL_VERSION_STALE",
	}
	for input, expected := range tests {
		if actual := connectorInstallErrorCode(input); actual != expected {
			t.Fatalf("connectorInstallErrorCode(%v) = %q, want %q", input, actual, expected)
		}
	}
}

func TestConnectorInstallPersistsCollectorArtifactTrust(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	command := &connectorv1.ConnectorInstallCommand{Artifact: &connectorv1.CollectorArtifact{SigningKeyId: "release-key"},
		ArtifactSigningPublicKey: base64.RawStdEncoding.EncodeToString(publicKey)}
	decoded, err := connectorInstallSigningKey(command)
	if err != nil || !bytes.Equal(decoded, publicKey) {
		t.Fatalf("frozen signing root rejected: %v", err)
	}
	unit := connectorSystemdUnit(command, true)
	for _, required := range []string{
		"ARGUS_OTELCOL_SIGNING_PUBLIC_KEYS_FILE=/etc/argus-connector/otelcol-signing-keys.json",
		"ARGUS_OTELCOL_ARTIFACT_CA_PATH=/etc/argus-connector/otelcol-artifact-ca.pem",
		"ReadWritePaths=/var/lib/argus-connector /etc/argus-connector /var/lib/argus-otelcol",
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("Connector unit omitted %q", required)
		}
	}
	if strings.Contains(unit, "User=argus-connector") {
		t.Fatal("Connector local installer unexpectedly runs without host installation privileges")
	}
	command.ArtifactSigningPublicKey = "invalid"
	if _, err = connectorInstallSigningKey(command); err == nil {
		t.Fatal("invalid frozen signing root accepted")
	}
}

func TestConnectorSSHInstallPinsPrivateCAForEnrollment(t *testing.T) {
	caPEM := directExecutorTestCA(t)
	material, err := trustbundle.Parse(caPEM, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	command := &connectorv1.ConnectorInstallCommand{
		ConnectorId: "01a05d47-319d-70b7-a768-f8aa2401f0da", EnrollmentEndpoint: "https://argus.private.example",
		EnrollDialAddress: "127.0.0.1:8443", TrustBundlePem: caPEM, TrustBundleEpoch: 2,
		TrustBundleSha256: material.SHA256, TrustBundleCaFingerprints: material.Fingerprints,
	}
	parsed, err := connectorInstallTrustBundle(command)
	if err != nil || parsed.SHA256 != material.SHA256 {
		t.Fatalf("valid private Trust Bundle rejected: %v", err)
	}
	enroll := connectorEnrollCommand(command, "one-time-token")
	for _, expected := range []string{"ARGUS_CONNECTOR_ENROLL_ADDRESS='127.0.0.1:8443'", "--server 'https://argus.private.example'", "--ca-file /etc/argus-connector/server-ca.pem"} {
		if !strings.Contains(enroll, expected) {
			t.Fatalf("remote enrollment command omitted %q: %s", expected, enroll)
		}
	}
	command.TrustBundleSha256 = strings.Repeat("0", 64)
	if _, err = connectorInstallTrustBundle(command); err == nil {
		t.Fatal("tampered remote installation Trust Bundle was accepted")
	}
}

func directExecutorTestCA(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(99), Subject: pkix.Name{CommonName: "remote-install-root"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: raw})
}
