package argusdev

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sigs.k8s.io/yaml"
)

type countingCloser struct{ calls atomic.Int32 }

func (c *countingCloser) Close() error {
	c.calls.Add(1)
	return nil
}

func TestCollectorArtifactSigning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collector.tar.gz")
	data := []byte("collector artifact")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	digest, size, encodedSignature, err := signCollectorArtifact(privateKey, path)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(data)
	if digest != hex.EncodeToString(wantDigest[:]) || size != uint64(len(data)) {
		t.Fatalf("artifact metadata = digest %q size %d", digest, size)
	}
	signature, err := base64.RawStdEncoding.DecodeString(encodedSignature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(publicKey, wantDigest[:], signature) {
		t.Fatal("artifact signature did not verify")
	}
}

func TestCollectorArchives(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "argus-otelcol")
	installer := filepath.Join(directory, "install-windows.ps1")
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installer, []byte("installer"), 0o644); err != nil {
		t.Fatal(err)
	}

	tarPath := filepath.Join(directory, "collector.tar.gz")
	if err := writeCollectorTarGz(binary, tarPath); err != nil {
		t.Fatal(err)
	}
	tarFile, err := os.Open(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	defer tarFile.Close()
	gzipReader, err := gzip.NewReader(tarFile)
	if err != nil {
		t.Fatal(err)
	}
	header, err := tar.NewReader(gzipReader).Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "argus-otelcol" || header.Mode != 0o755 || !header.ModTime.Equal(time.Unix(0, 0)) {
		t.Fatalf("unexpected tar header: %#v", header)
	}

	zipPath := filepath.Join(directory, "collector.zip")
	if err := writeCollectorZip(binary, installer, zipPath); err != nil {
		t.Fatal(err)
	}
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zipReader.Close()
	var names []string
	contents := map[string]string{}
	for _, file := range zipReader.File {
		names = append(names, file.Name)
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		contents[file.Name] = string(data)
	}
	if !reflect.DeepEqual(names, []string{"argus-otelcol.exe", "install-windows.ps1"}) {
		t.Fatalf("zip entries = %#v", names)
	}
	if contents["argus-otelcol.exe"] != "binary" || contents["install-windows.ps1"] != "installer" {
		t.Fatalf("zip contents = %#v", contents)
	}
}

func TestParseConnectorEnrollmentCommand(t *testing.T) {
	command := "argus-connector enroll --connector-id connector-id --token token-value --server https://argus.example --role bastion"
	got, err := parseConnectorEnrollmentCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	want := connectorEnrollmentCommand{ConnectorID: "connector-id", Token: "token-value", Server: "https://argus.example", Role: "bastion"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed command = %#v, want %#v", got, want)
	}
	for _, invalid := range []string{
		"connector enroll --connector-id id --token token --server https://argus.example --role bastion",
		"argus-connector enroll --connector-id id --token token --server https://argus.example",
		"argus-connector enroll --connector-id id --connector-id other --token token --server https://argus.example --role bastion",
		"argus-connector enroll --connector-id id --token token --server https://argus.example --role invalid",
		"argus-connector enroll --connector-id id --token token --server https://argus.example --role bastion trailing",
	} {
		if _, err := parseConnectorEnrollmentCommand(invalid); err == nil {
			t.Fatalf("invalid command was accepted: %q", invalid)
		}
	}
}

func TestQueryEnvelopeHelpers(t *testing.T) {
	meta := map[string]any{"plan_hash": string(bytes.Repeat([]byte{'a'}, 64)), "partial": false, "scanned_bytes": float64(10), "elapsed_ms": float64(2)}
	response := map[string]any{
		"schema_version": "argus.kql_result/v1",
		"result_type":    "log_entries",
		"meta":           meta,
		"data":           []any{map[string]any{"body": "expected"}},
	}
	if err := assertKQLResponse(response, "expected", 1); err != nil {
		t.Fatal(err)
	}
	if err := assertKQLResponse(response, "missing", 1); err == nil {
		t.Fatal("missing KQL body was accepted")
	}
	if validQueryMeta(map[string]any{"plan_hash": "short", "partial": false, "scanned_bytes": float64(10), "elapsed_ms": float64(2)}) {
		t.Fatal("invalid query metadata was accepted")
	}
}

func TestNonEmptyResultRef(t *testing.T) {
	for _, value := range []any{"artifact-id", map[string]any{"id": "artifact-id"}, []any{"artifact-id"}} {
		if !nonEmptyResultRef(value) {
			t.Fatalf("non-empty result ref %#v was rejected", value)
		}
	}
	for _, value := range []any{nil, "", "  ", map[string]any{}, []any{}, float64(1), false} {
		if nonEmptyResultRef(value) {
			t.Fatalf("empty result ref %#v was accepted", value)
		}
	}
}

func nonEmptyResultRef(value any) bool {
	switch item := value.(type) {
	case string:
		return strings.TrimSpace(item) != ""
	case map[string]any:
		for _, key := range []string{"id", "ref", "artifact_ref"} {
			if text, ok := item[key].(string); ok && strings.TrimSpace(text) != "" {
				return true
			}
		}
	case []any:
		return len(item) > 0 && nonEmptyResultRef(item[0])
	}
	return false
}

func TestRefreshM4ApproverLoginReadsAuthenticatedSession(t *testing.T) {
	const csrf = "0123456789abcdef0123456789abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/enterprise/auth/login" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["username"] != "m4-approver" || body["password"] != "Q8!mV4@rT7#pL2$x" {
			t.Fatalf("unexpected login body: %#v", body)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"status":"authenticated","authenticated_session":{"csrf_token":"` + csrf + `"}}`))
	}))
	defer server.Close()

	state := NewScenarioState("test")
	state.HTTP = NewScenarioHTTP(server.URL, t.TempDir(), nil)
	state.Values["m4_approver_password"] = "Q8!mV4@rT7#pL2$x"
	env := &E2EEnvironment{State: state, Endpoints: &E2EEndpoints{EnterpriseOrigin: "http://enterprise.example"}}
	if err := (&App{}).refreshM4ApproverLogin(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if got := state.Values["m4_approver_csrf"]; got != csrf {
		t.Fatalf("m4 approver CSRF = %q, want %q", got, csrf)
	}
}

func TestValidateM6Ticket(t *testing.T) {
	validTicket := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	result := map[string]any{
		"ticket":           validTicket,
		"websocket_url":    "wss://argus.example.test/remote-access",
		"protocol_version": "argus.remote_access/v1",
	}
	if ticket, err := validateM6Ticket(result); err != nil || ticket != validTicket {
		t.Fatalf("valid ticket = %q, %v", ticket, err)
	}
	for _, invalid := range []map[string]any{
		{},
		{"ticket": "short", "websocket_url": "wss://argus.example.test/remote-access", "protocol_version": "argus.remote_access/v1"},
		{"ticket": strings.Repeat("a", 44), "websocket_url": "wss://argus.example.test/remote-access", "protocol_version": "argus.remote_access/v1"},
		{"ticket": validTicket, "websocket_url": "wss://argus.example.test/remote-access?ticket=leaked", "protocol_version": "argus.remote_access/v1"},
		{"ticket": validTicket, "websocket_url": "wss://argus.example.test/remote-access", "protocol_version": "argus.remote_access/v2"},
	} {
		if _, err := validateM6Ticket(invalid); err == nil {
			t.Fatalf("invalid ticket contract was accepted: %#v", invalid)
		}
	}
}

func TestKubeForwardStopIsIdempotent(t *testing.T) {
	closer := &countingCloser{}
	forward := &KubeForward{stop: make(chan struct{}), done: make(chan error, 1), closer: closer}
	forward.done <- nil
	if err := forward.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := forward.Stop(); err != nil {
		t.Fatal(err)
	}
	if closer.calls.Load() != 1 {
		t.Fatalf("closer called %d times", closer.calls.Load())
	}
	select {
	case <-forward.stop:
	default:
		t.Fatal("stop channel was not closed")
	}
}

func TestScanRepositoryFilesSkipsDependencyDirectories(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "web", "src")
	dependency := filepath.Join(root, "web", "node_modules", "dependency")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dependency, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dependency, "fixture.txt"), []byte("forbidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	pattern := regexp.MustCompile("forbidden")
	if err := scanRepositoryFiles(root, []string{"web"}, pattern, false); err != nil {
		t.Fatalf("dependency directory was scanned: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "fixture.txt"), []byte("forbidden"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := scanRepositoryFiles(root, []string{"web"}, pattern, false); err == nil {
		t.Fatal("source marker was not detected")
	}
}

func TestFixtureImagesForCleanup(t *testing.T) {
	images := map[string]string{
		"backend": "host.docker.internal:5001/argus/argus-backend:tag",
		"ssh":     "host.docker.internal:5001/argus/argus-e2e-ssh:tag",
		"winrs":   "host.docker.internal:5001/argus/argus-e2e-ssh:tag",
		"replay":  "host.docker.internal:5001/argus/argus-e2e-replay:tag",
	}
	want := []string{"host.docker.internal:5001/argus/argus-e2e-ssh:tag", "host.docker.internal:5001/argus/argus-e2e-replay:tag"}
	if got := fixtureImagesForCleanup(images); !reflect.DeepEqual(got, want) {
		t.Fatalf("fixture images = %#v, want %#v", got, want)
	}
	localWant := []string{"localhost:5001/argus/argus-e2e-ssh:tag", "localhost:5001/argus/argus-e2e-replay:tag"}
	if got := localFixtureImagesForCleanup(images); !reflect.DeepEqual(got, localWant) {
		t.Fatalf("local fixture images = %#v, want %#v", got, localWant)
	}
}

func TestReclaimE2EInstallDisk(t *testing.T) {
	t.Run("keeps cache when capacity is sufficient", func(t *testing.T) {
		pruneCalls := 0
		free, pruned, err := reclaimE2EInstallDisk(
			context.Background(),
			"workspace",
			func(string) (uint64, error) { return e2eInstallMinimumDiskBytes, nil },
			func(context.Context) error { pruneCalls++; return nil },
		)
		if err != nil || pruned || free != e2eInstallMinimumDiskBytes || pruneCalls != 0 {
			t.Fatalf("free=%d pruned=%t pruneCalls=%d err=%v", free, pruned, pruneCalls, err)
		}
	})

	t.Run("prunes cache once when capacity is low", func(t *testing.T) {
		probes := []uint64{20 << 30, 31 << 30}
		probeCalls := 0
		pruneCalls := 0
		free, pruned, err := reclaimE2EInstallDisk(
			context.Background(),
			"workspace",
			func(string) (uint64, error) {
				value := probes[probeCalls]
				probeCalls++
				return value, nil
			},
			func(context.Context) error { pruneCalls++; return nil },
		)
		if err != nil || !pruned || free != 31<<30 || probeCalls != 2 || pruneCalls != 1 {
			t.Fatalf("free=%d pruned=%t probeCalls=%d pruneCalls=%d err=%v", free, pruned, probeCalls, pruneCalls, err)
		}
	})

	t.Run("fails when cleanup cannot restore capacity", func(t *testing.T) {
		free, pruned, err := reclaimE2EInstallDisk(
			context.Background(),
			"workspace",
			func(string) (uint64, error) { return 20 << 30, nil },
			func(context.Context) error { return nil },
		)
		if !pruned || free != 20<<30 || !errors.Is(err, errCapability) {
			t.Fatalf("free=%d pruned=%t err=%v", free, pruned, err)
		}
	})
}

func TestReleaseIDForDevFitsEveryHelmStageName(t *testing.T) {
	value := "m3-form-validation-20260823-m3-final"
	got := releaseIDForDev(value)
	if len(got) > 34 {
		t.Fatalf("release ID length = %d, want at most 34: %q", len(got), got)
	}
	if got == releaseIDForDev(value+"-different") {
		t.Fatalf("distinct long run IDs produced the same release ID %q", got)
	}
	if !regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`).MatchString(got) {
		t.Fatalf("release ID is not a DNS label: %q", got)
	}
}

func TestSyncPlaywrightMFAStateTracksCodesConsumedByBrowser(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, ".argus-enterprise-totp-last-code"), []byte("123456\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := &E2EEnvironment{State: NewScenarioState("mfa-state")}
	if err := syncPlaywrightMFAState(env, directory); err != nil {
		t.Fatal(err)
	}
	if got := env.State.Values["enterprise_mfa_last"]; got != "123456" {
		t.Fatalf("enterprise MFA state = %q, want 123456", got)
	}
}

func TestWriteE2EConfigUsesStructuredProfile(t *testing.T) {
	root := t.TempDir()
	profileDirectory := filepath.Join(root, "deploy", "profiles")
	if err := os.MkdirAll(profileDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	profile := []byte("apiVersion: install.argus.io/v1alpha1\nkind: ArgusInstallConfig\nmetadata:\n  name: base\nspec:\n  profile: evaluation\n  images:\n    tag: base\n")
	if err := os.WriteFile(filepath.Join(profileDirectory, "evaluation.yaml"), profile, 0o600); err != nil {
		t.Fatal(err)
	}
	artifacts := t.TempDir()
	app := &App{root: root}
	env := &E2EEnvironment{
		Options:   E2EOptions{Artifacts: artifacts, KubeContext: "portable-test"},
		ReleaseID: "argus-e2e-test", SystemNS: "argus-e2e-test-system", SandboxNS: "argus-e2e-test-sandbox",
		ObservNS: "argus-e2e-test-observability", ImageTag: "e2e-test",
	}
	path, err := app.writeE2EConfig(env, "evaluation")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	spec := nestedMap(document, "spec")
	images := nestedMap(spec, "images")
	if spec["releaseId"] != env.ReleaseID || spec["kubeContext"] != env.Options.KubeContext || images["tag"] != env.ImageTag {
		t.Fatalf("generated config = %#v", document)
	}
	if strings.Contains(string(data), "m4e2e") || strings.Contains(string(data), "ARGUS_E2E") {
		t.Fatalf("generated install config exposed E2E-only fields: %s", data)
	}
}
