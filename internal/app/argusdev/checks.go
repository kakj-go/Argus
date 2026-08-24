package argusdev

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *App) runCheck(ctx context.Context, args []string) error {
	if len(args) != 1 || !oneOf(args[0], "query-parsers", "production-artifacts", "web-entrypoints", "all") {
		return fmt.Errorf("%w: usage: argus-dev check query-parsers|production-artifacts|web-entrypoints|all", errUsage)
	}
	checks := []struct {
		name string
		run  func(context.Context) error
	}{
		{"query-parsers", a.checkQueryParsers},
		{"production-artifacts", a.checkProductionArtifacts},
		{"web-entrypoints", a.checkWebEntrypoints},
	}
	for _, check := range checks {
		if args[0] != "all" && args[0] != check.name {
			continue
		}
		_, _ = fmt.Fprintf(a.stdout, "Running %s check\n", check.name)
		if err := check.run(ctx); err != nil {
			return fmt.Errorf("%s check: %w", check.name, err)
		}
	}
	return nil
}

type queryParserLock struct {
	SchemaVersion string `json:"schema_version"`
	Engines       []struct {
		Language string `json:"language"`
		Adapter  string `json:"adapter"`
		Notice   string `json:"notice"`
		Module   string `json:"module"`
		Version  string `json:"version"`
		License  string `json:"license"`
		Commit   string `json:"commit"`
	} `json:"engines"`
}

func (a *App) checkQueryParsers(ctx context.Context) error {
	data, err := os.ReadFile(filepath.Join(a.root, "deploy", "query-parsers.lock.json"))
	if err != nil {
		return err
	}
	var lock queryParserLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return err
	}
	if lock.SchemaVersion != "argus.telemetry_query_engines/v2" || len(lock.Engines) != 3 {
		return fmt.Errorf("unexpected query parser lock schema or engine count")
	}
	languages := make([]string, 0, len(lock.Engines))
	for _, engine := range lock.Engines {
		languages = append(languages, engine.Language)
		if engine.Adapter == "" {
			return fmt.Errorf("%s adapter is empty", engine.Language)
		}
		for _, relative := range []string{engine.Adapter, engine.Notice} {
			if relative == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(a.root, filepath.FromSlash(relative))); err != nil {
				return fmt.Errorf("query engine lock references missing file %s", relative)
			}
		}
		if engine.Module == "" {
			continue
		}
		if !oneOf(engine.License, "Apache-2.0", "MIT") {
			return fmt.Errorf("unexpected %s license %s", engine.Module, engine.License)
		}
		version, err := a.runner.Output(ctx, nil, "go", "list", "-m", "-f", "{{.Version}}", engine.Module)
		if err != nil {
			return err
		}
		if version != engine.Version {
			return fmt.Errorf("query engine version drift: %s lock=%s go.mod=%s", engine.Module, engine.Version, version)
		}
		download, err := a.goModuleDownload(ctx, engine.Module+"@"+engine.Version)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(download.Dir, "LICENSE")); err != nil {
			return fmt.Errorf("query engine license missing for %s", engine.Module)
		}
		if engine.Language == "promql" && download.Origin.Hash != engine.Commit {
			return fmt.Errorf("PromQL engine commit drift: lock=%s module=%s", engine.Commit, download.Origin.Hash)
		}
	}
	sort.Strings(languages)
	if strings.Join(languages, ",") != "kql,promql,skywalking_graphql" {
		return fmt.Errorf("unexpected query languages: %s", strings.Join(languages, ","))
	}
	_, _ = fmt.Fprintln(a.stdout, "query parser lock is current")
	return nil
}

type moduleDownload struct {
	Dir    string `json:"Dir"`
	Origin struct {
		Hash string `json:"Hash"`
	} `json:"Origin"`
}

func (a *App) goModuleDownload(ctx context.Context, module string) (moduleDownload, error) {
	output, err := a.runner.Output(ctx, nil, "go", "mod", "download", "-json", module)
	if err != nil {
		return moduleDownload{}, err
	}
	var result moduleDownload
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		return result, err
	}
	return result, nil
}

var (
	forbiddenBinaryMarkers = [][]byte{
		[]byte("argus M4 replay model"), []byte("ARGUS_REPLAY_"), []byte("ARGUS_ALLOW_PRIVATE_MODEL"), []byte("quota-exceeded"),
	}
	forbiddenDeployPattern = regexp.MustCompile(`ARGUS_(ALLOW_PRIVATE_MODEL|REPLAY_PROVIDER|DISABLE_(ORIGIN|CSRF|TLS|AUTH)|PLAINTEXT_SECRET)`)
	forbiddenSecretPattern = regexp.MustCompile(strings.Join([]string{
		`BEGIN ` + `(RSA |EC |OPENSSH |)PRIVATE KEY`,
		`argus_` + `ak_[A-Za-z0-9_-]+\.[A-Za-z0-9_-]{16,}`,
		`ARGUS_OPENBAO_` + `TOKEN=[^$<{]`,
	}, `|`))
)

func (a *App) checkProductionArtifacts(ctx context.Context) (returnErr error) {
	image := os.Getenv("ARGUS_PRODUCTION_IMAGE")
	built := image == ""
	if built {
		image = "argus/argus-backend:production-scan"
		if err := a.runner.Run(ctx, nil, "docker", "build", "--quiet", "--file", "deploy/docker/backend.Dockerfile", "--tag", image, "."); err != nil {
			return err
		}
		defer func() { _ = a.runner.Run(context.Background(), nil, "docker", "image", "rm", "--force", image) }()
	}
	container, err := a.runner.Output(ctx, nil, "docker", "create", image)
	if err != nil {
		return err
	}
	defer func() { _ = a.runner.Run(context.Background(), nil, "docker", "rm", "--force", container) }()
	temporary, err := os.CreateTemp("", "argus-production-rootfs-*.tar")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := a.runner.RunIO(ctx, nil, nil, temporary, a.stderr, "docker", "export", container); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := scanProductionRootFS(path); err != nil {
		return err
	}
	backendDockerfile, err := os.ReadFile(filepath.Join(a.root, "deploy", "docker", "backend.Dockerfile"))
	if err != nil {
		return err
	}
	if err := validateBackendDockerfile(backendDockerfile); err != nil {
		return err
	}
	if bytes.Contains(backendDockerfile, []byte("GO_BUILD_TAGS=m4e2e")) || bytes.Contains(backendDockerfile, []byte("tags=m4e2e")) {
		return fmt.Errorf("production Dockerfile enables the M4 E2E build tag")
	}
	if err := scanRepositoryFiles(a.root, []string{"deploy/helm", "deploy/docker"}, forbiddenDeployPattern, true); err != nil {
		return err
	}
	if err := scanRepositoryFiles(a.root, []string{"cmd", "internal", "web", "deploy/helm", "deploy/docker"}, forbiddenSecretPattern, false); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(a.stdout, "production artifacts contain no forbidden replay, mock, key, or bypass markers")
	return nil
}

func validateBackendDockerfile(data []byte) error {
	required := map[string]string{
		"COPY api/openapi ./api/openapi": "bundled OpenAPI runtime package",
		"RUN set -eu;":                   "fail-fast shell execution",
	}
	for snippet, purpose := range required {
		if !bytes.Contains(data, []byte(snippet)) {
			return fmt.Errorf("backend Dockerfile is missing %s (%s)", purpose, snippet)
		}
	}
	return nil
}

func scanProductionRootFS(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := tar.NewReader(file)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(header.Name)
		if strings.Contains(name, "/argus-replay-model/") || strings.Contains(name, "/mock-seed/") || strings.HasSuffix(name, "/argus-replay-model") {
			return fmt.Errorf("production image contains E2E artifact %s", name)
		}
		if !strings.HasPrefix(name, "usr/local/bin/") || header.Size <= 0 {
			continue
		}
		content, err := io.ReadAll(io.LimitReader(reader, header.Size))
		if err != nil {
			return err
		}
		for _, marker := range forbiddenBinaryMarkers {
			if bytes.Contains(content, marker) {
				return fmt.Errorf("production binary %s contains E2E marker %q", filepath.Base(name), marker)
			}
		}
	}
}

func scanRepositoryFiles(root string, roots []string, pattern *regexp.Regexp, ignoreE2E bool) error {
	for _, relativeRoot := range roots {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(relativeRoot)), func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			normalized := filepath.ToSlash(relative)
			if entry.IsDir() {
				if oneOf(entry.Name(), "node_modules", "dist", ".turbo") {
					return filepath.SkipDir
				}
				if ignoreE2E && strings.Contains(strings.ToLower(normalized), "e2e") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(normalized, "_test.go") || strings.Contains(normalized, "/e2e/") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if pattern.Match(content) {
				return fmt.Errorf("forbidden production marker in %s", normalized)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) checkWebEntrypoints(ctx context.Context) (returnErr error) {
	values := []string{
		"--set-string", "runtime.postgresqlPassword=m1-smoke", "--set-string", "runtime.redisPassword=m1-smoke",
		"--set-string", "runtime.idempotencyEncryptionKey=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"--set-string", "runtime.cursorSigningKey=BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB",
		"--set-string", "runtime.pendingActionEncryptionKey=CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC",
		"--set-string", "runtime.objectStoreUrl=http://argus-minio:9000", "--set-string", "runtime.objectStoreAccessKey=m1-smoke",
		"--set-string", "runtime.objectStoreSecretKey=m1-smoke-secret", "--set-string", "runtime.otelcolLinuxArm64Uri=https://artifacts.argus.invalid/linux-arm64.tar.gz",
		"--set-string", "runtime.otelcolLinuxArm64Sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"--set-string", "runtime.otelcolLinuxArm64Signature=m1-smoke-signature", "--set", "runtime.otelcolLinuxArm64ByteSize=1",
		"--set-string", "runtime.otelcolWindowsAmd64Uri=https://artifacts.argus.invalid/windows-amd64.zip",
		"--set-string", "runtime.otelcolWindowsAmd64Sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"--set-string", "runtime.otelcolWindowsAmd64Signature=m1-smoke-signature", "--set", "runtime.otelcolWindowsAmd64ByteSize=1",
		"--set-string", "runtime.otelcolSigningKeyId=m1-smoke", "--set-string", "runtime.otelcolSigningPublicKey=m1-smoke-public-key",
		"--set-string", "runtime.otelcolKubernetesImage=argus-otelcol:m1-smoke",
		"--set-json", `runtime.secretKEKKeyring={"current_version":1,"keys":{"1":"DDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDDD"}}`,
	}
	lintArgs := append([]string{"lint", "deploy/helm/argus-platform"}, values...)
	if err := a.runner.Run(ctx, nil, "helm", lintArgs...); err != nil {
		return err
	}
	templateArgs := []string{"template", "argus", "deploy/helm/argus-platform", "--set", "profile=production"}
	templateArgs = append(templateArgs, values...)
	rendered, err := a.runner.Output(ctx, nil, "helm", templateArgs...)
	if err != nil {
		return err
	}
	for _, expected := range []string{
		"host: cards.argus.example.com", "name: cards", "containerPort: 8083",
		"ARGUS_DIRECT_EXECUTOR_CLIENT_NAMES: argus-server,argus-connector-gateway",
		"secretName: argus-server-direct-executor-client-tls", "secretName: argus-connector-gateway-direct-executor-client-tls",
		"name: argus-connector-gateway-remote-access", "IdempotentWrite",
	} {
		if !strings.Contains(rendered, expected) {
			return fmt.Errorf("rendered platform chart is missing %q", expected)
		}
	}
	if !renderedDocumentContains(rendered, "Deployment", "name: argus-telemetry-query", "name: ARGUS_REDIS_URL") ||
		!renderedDocumentContains(rendered, "NetworkPolicy", "name: argus-telemetry-query", "port: 6379") {
		return fmt.Errorf("telemetry query Redis deployment or NetworkPolicy is missing")
	}
	image := "argus-web:m1-smoke"
	container := "argus-web-m1-smoke-" + strconv.Itoa(os.Getpid())
	defer func() {
		_ = a.runner.Run(context.Background(), nil, "docker", "rm", "--force", container)
		_ = a.runner.Run(context.Background(), nil, "docker", "image", "rm", "--force", image)
	}()
	if err := a.runner.Run(ctx, nil, "docker", "build", "--file", "deploy/docker/web.Dockerfile", "--tag", image,
		"--build-arg", "VITE_API_MODE=real", "--build-arg", "VITE_API_BASE_URL=https://api.argus.invalid",
		"--build-arg", "VITE_CARD_ORIGIN=https://cards.argus.invalid", "--build-arg", "VITE_PLATFORM_URL=https://platform.argus.invalid",
		"--build-arg", "VITE_DIRECT_EGRESS_ADDRESSES=198.51.100.10", "."); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "docker", "run", "--detach", "--name", container,
		"--publish", "127.0.0.1::8080", "--publish", "127.0.0.1::8081", "--publish", "127.0.0.1::8083", image); err != nil {
		return err
	}
	paths := map[int]string{8080: "/hosts/example", 8081: "/setup", 8083: "/runtime"}
	markers := map[int]string{8080: `<div id="root"></div>`, 8081: `<div id="root"></div>`, 8083: `<main id="card-root"></main>`}
	for containerPort, path := range paths {
		mapping, err := a.runner.Output(ctx, nil, "docker", "port", container, fmt.Sprintf("%d/tcp", containerPort))
		if err != nil {
			return err
		}
		hostPort := mapping[strings.LastIndex(mapping, ":")+1:]
		baseURL := "http://127.0.0.1:" + hostPort
		if err := waitHTTP(ctx, baseURL+"/healthz", "ok", 30*time.Second); err != nil {
			return err
		}
		if err := waitHTTP(ctx, baseURL+path, markers[containerPort], 10*time.Second); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintln(a.stdout, "web image, Nginx deep links, and Helm entrypoints passed")
	return nil
}

func renderedDocumentContains(rendered, kind string, values ...string) bool {
	for _, document := range strings.Split(rendered, "\n---") {
		if !strings.Contains(document, "kind: "+kind) {
			continue
		}
		matched := true
		for _, value := range values {
			matched = matched && strings.Contains(document, value)
		}
		if matched {
			return true
		}
	}
	return false
}

func waitHTTP(ctx context.Context, url, expected string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 3 * time.Second}
	for time.Now().Before(deadline) {
		request, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		response, err := client.Do(request)
		if err == nil {
			body, readErr := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if readErr == nil && response.StatusCode < 400 && strings.Contains(string(body), expected) {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("%s did not return content containing %q", url, expected)
}
