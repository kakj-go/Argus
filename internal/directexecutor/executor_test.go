package directexecutor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/kakj-go/Argus/internal/resource"
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
