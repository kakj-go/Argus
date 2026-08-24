package argusdev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDeleteRegistryTagUsesDigestAndManifestAccept(t *testing.T) {
	var deletedPath string
	var targetAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/manifests/e2e-test"):
			targetAccept = r.Header.Get("Accept")
			w.Header().Set("Docker-Content-Digest", "sha256:e2e")
		case r.Method == http.MethodHead && strings.HasSuffix(r.URL.Path, "/manifests/dev"):
			w.Header().Set("Docker-Content-Digest", "sha256:dev")
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/tags/list"):
			_, _ = fmt.Fprint(w, `{"tags":["dev","e2e-test"]}`)
		case r.Method == http.MethodDelete:
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusAccepted)
		default:
			http.Error(w, "unexpected request", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	image := strings.TrimPrefix(server.URL, "http://") + "/argus/test:e2e-test"
	if err := deleteRegistryTag(context.Background(), server.Client(), image); err != nil {
		t.Fatal(err)
	}
	if targetAccept != registryManifestAccept {
		t.Fatalf("Accept = %q, want %q", targetAccept, registryManifestAccept)
	}
	if deletedPath != "/v2/argus/test/manifests/sha256:e2e" {
		t.Fatalf("deleted path = %q", deletedPath)
	}
}

func TestDeleteRegistryTagNotFoundIsIdempotent(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		http.NotFound(w, nil)
	}))
	defer server.Close()

	image := strings.TrimPrefix(server.URL, "http://") + "/argus/test:e2e-test"
	if err := deleteRegistryTag(context.Background(), server.Client(), image); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want one manifest lookup", requests)
	}
}

func TestDeleteRegistryTagRejectsSharedDigest(t *testing.T) {
	deleteCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Docker-Content-Digest", "sha256:shared")
		case http.MethodGet:
			_, _ = fmt.Fprint(w, `{"tags":["dev","e2e-test"]}`)
		case http.MethodDelete:
			deleteCalled = true
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer server.Close()

	image := strings.TrimPrefix(server.URL, "http://") + "/argus/test:e2e-test"
	err := deleteRegistryTag(context.Background(), server.Client(), image)
	if err == nil || !strings.Contains(err.Error(), "shares manifest") {
		t.Fatalf("error = %v, want shared manifest refusal", err)
	}
	if deleteCalled {
		t.Fatal("shared manifest was deleted")
	}
}

func TestDeleteRegistryTagReportsUnsupportedDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodHead:
			w.Header().Set("Docker-Content-Digest", "sha256:e2e")
		case http.MethodGet:
			_, _ = fmt.Fprint(w, `{"tags":["e2e-test"]}`)
		case http.MethodDelete:
			http.Error(w, "disabled", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	image := strings.TrimPrefix(server.URL, "http://") + "/argus/test:e2e-test"
	err := deleteRegistryTag(context.Background(), server.Client(), image)
	if err == nil || !strings.Contains(err.Error(), "405 Method Not Allowed") {
		t.Fatalf("error = %v, want unsupported delete failure", err)
	}
}

func TestRemoteE2EImagesForCleanupIncludesCoreAndFixtures(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "install.yaml")
	config := []byte("spec:\n  images:\n    registry: host.docker.internal:5001\n")
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	images, err := remoteE2EImagesForCleanup(configPath, "e2e-run", map[string]string{
		"backend": "host.docker.internal:5001/argus/argus-backend:e2e-run",
		"ssh":     "host.docker.internal:5001/argus/argus-e2e-ssh:e2e-run",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"host.docker.internal:5001/argus/argus-backend:e2e-run",
		"host.docker.internal:5001/argus/argus-e2e-ssh:e2e-run",
		"host.docker.internal:5001/argus/argus-web:e2e-run",
		"host.docker.internal:5001/argus/minio:e2e-run",
	}
	if !reflect.DeepEqual(images, want) {
		t.Fatalf("images = %#v, want %#v", images, want)
	}
}
