package directexecutor

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/kakj-go/Argus/internal/resource"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

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
	if err := verifyEgress(context.Background(), "https://egress.example/check", []string{"203.0.113.10"}, client); err != nil {
		t.Fatal(err)
	}
	if err := verifyEgress(context.Background(), "https://egress.example/check", []string{"203.0.113.11"}, client); err == nil {
		t.Fatal("expected mismatched egress address to fail")
	}
}

func TestVerifyEgressRejectsUnsafeEndpointAndOversizedResponse(t *testing.T) {
	if err := verifyEgress(context.Background(), "http://egress.example/check", []string{"203.0.113.10"}, http.DefaultClient); err == nil {
		t.Fatal("expected non-HTTPS verification endpoint to fail")
	}
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(make([]byte, 4097)))}, nil
	})}
	if err := verifyEgress(context.Background(), "https://egress.example/check", []string{"203.0.113.10"}, client); err == nil {
		t.Fatal("expected oversized verification response to fail")
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
