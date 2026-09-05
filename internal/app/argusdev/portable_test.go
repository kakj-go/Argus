package argusdev

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMergeEnvKeepsDeterministicOrder(t *testing.T) {
	got := mergeEnv([]string{"Path=old", "KEEP=value"}, map[string]string{"PATH": "new", "added": "yes"})
	want := []string{"added=yes", "KEEP=value", "PATH=new"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergeEnv() = %#v, want %#v", got, want)
	}
}

func TestHashDifferenceIsSorted(t *testing.T) {
	got := hashDifference(map[string]string{"b": "old", "c": "same"}, map[string]string{"a": "new", "b": "changed", "c": "same"})
	if got != "+ a\n~ b" {
		t.Fatalf("hashDifference() = %q", got)
	}
}

func TestArchiveDirectoryIsDeterministic(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "a.txt"), []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(t.TempDir(), "first.tar.gz")
	second := filepath.Join(t.TempDir(), "second.tar.gz")
	if err := archiveDirectory(directory, first, "bin"); err != nil {
		t.Fatal(err)
	}
	if err := archiveDirectory(directory, second, "bin"); err != nil {
		t.Fatal(err)
	}
	one, _ := os.ReadFile(first)
	two, _ := os.ReadFile(second)
	if !bytes.Equal(one, two) {
		t.Fatal("archive output is not deterministic")
	}
	reader, err := gzip.NewReader(bytes.NewReader(one))
	if err != nil {
		t.Fatal(err)
	}
	archive := tar.NewReader(reader)
	var names []string
	for {
		header, err := archive.Next()
		if err != nil {
			break
		}
		names = append(names, header.Name)
		if !header.ModTime.Equal(time.Unix(0, 0)) {
			t.Fatalf("archive timestamp = %s", header.ModTime)
		}
	}
	if !reflect.DeepEqual(names, []string{"bin/a.txt", "bin/b.txt"}) {
		t.Fatalf("archive entries = %#v", names)
	}
}

func TestChecksumManifestUsesPortableRelativePaths(t *testing.T) {
	repoRoot := t.TempDir()
	releaseRoot := t.TempDir()
	repoFile := filepath.Join(repoRoot, "api", "contract.json")
	releaseFile := filepath.Join(releaseRoot, "images", "backend.tar.gz")
	for path, content := range map[string]string{repoFile: "contract", releaseFile: "image"} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest := filepath.Join(releaseRoot, "offline-manifest.sha256")
	if err := writeChecksumManifest(repoRoot, releaseRoot, []string{repoFile, releaseFile}, manifest); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, repoRoot) || strings.Contains(text, releaseRoot) || !strings.Contains(text, "api/contract.json") || !strings.Contains(text, "images/backend.tar.gz") {
		t.Fatalf("manifest contains non-portable paths:\n%s", text)
	}
}

func TestSignFileProducesVerifiableSignature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "artifact")
	signaturePath := filepath.Join(t.TempDir(), "artifact.sig")
	content := []byte("signed artifact")
	if err := os.WriteFile(source, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signFile(privateKey, source, signaturePath); err != nil {
		t.Fatal(err)
	}
	signature, err := os.ReadFile(signaturePath)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(publicKey, content, signature) {
		t.Fatal("release signature did not verify")
	}
}

func TestWaitHTTPRetriesAndHonorsCancellation(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = response.Write([]byte("ready"))
	}))
	defer server.Close()
	if err := waitHTTP(context.Background(), server.URL, "ready", 3*time.Second); err != nil {
		t.Fatal(err)
	}
	if attempts.Load() < 2 {
		t.Fatal("waitHTTP did not retry")
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitHTTP(cancelled, server.URL, "never", time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitHTTP error = %v, want context canceled", err)
	}
}

func TestGenerateTOTP(t *testing.T) {
	code, err := generateTOTP("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time.Unix(59, 0))
	if err != nil {
		t.Fatal(err)
	}
	if code != "287082" {
		t.Fatalf("TOTP = %s", code)
	}
}

func TestRedaction(t *testing.T) {
	redacted := redactJSON(map[string]any{
		"password": "secret",
		"nested":   map[string]any{"token": "value", "safe": "kept"},
		"env":      []any{map[string]any{"name": "ARGUS_E2E_SSH_PASSWORD", "value": "plaintext"}},
	})
	encoded, _ := json.Marshal(redacted)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "plaintext") || !strings.Contains(string(encoded), "kept") {
		t.Fatalf("unexpected redaction: %s", encoded)
	}
	document, err := redactedJSONDocument(map[string]any{"env": []any{map[string]any{"name": "API_TOKEN", "value": "token-value"}}})
	if err != nil || strings.Contains(string(document), "token-value") || !json.Valid(document) {
		t.Fatalf("redacted JSON document = %q, %v", document, err)
	}
	diagnostic := redactDiagnostic([]byte("safe line\nAuthorization: bearer\nsecret=value\nkept"))
	if string(diagnostic) != "safe line\nkept" {
		t.Fatalf("diagnostic redaction = %q", diagnostic)
	}
}

func TestDoctorJSON(t *testing.T) {
	var output bytes.Buffer
	report := DoctorReport{Scope: "portable", OS: "test", Arch: "test", Ready: true, Checks: []DoctorCheck{{Name: "tool/go", Status: "pass", Message: "go"}}}
	if err := writeDoctor(&output, "json", report); err != nil {
		t.Fatal(err)
	}
	var decoded DoctorReport
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(decoded, report) {
		t.Fatalf("doctor JSON = %#v", decoded)
	}
}

func TestSuiteDependencies(t *testing.T) {
	want := map[string][]string{
		"m2": {"m2"}, "m3": {"m2", "m3"}, "m4": {"m2", "m4"},
		"m5": {"m2", "m3", "m4", "m5"}, "m6": {"m2", "m3", "m6"},
		"m7":        {"m2", "m3", "m4", "m5", "m7"},
		"m10-query": {"m2", "m3", "m4", "m5", "m7", "m10-query"},
		"m8":        {"m6", "m7", "m8"},
		"p4":        {"m2", "p4"},
	}
	if !reflect.DeepEqual(suiteDependencies, want) {
		t.Fatalf("suite dependencies = %#v", suiteDependencies)
	}
}

func TestValidateBackendDockerfile(t *testing.T) {
	valid := []byte("COPY api/openapi ./api/openapi\nRUN set -eu; \\\n")
	if err := validateBackendDockerfile(valid); err != nil {
		t.Fatalf("valid Dockerfile rejected: %v", err)
	}
	for name, data := range map[string][]byte{
		"missing OpenAPI package": []byte("RUN set -eu; \\\n"),
		"missing fail fast":       []byte("COPY api/openapi ./api/openapi\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateBackendDockerfile(data); err == nil {
				t.Fatal("invalid Dockerfile accepted")
			}
		})
	}
}
