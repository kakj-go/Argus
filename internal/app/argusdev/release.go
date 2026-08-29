package argusdev

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
)

var strongCopyleftPattern = regexp.MustCompile(`(?i)"license"\s*:\s*"(?:AGPL|GPL)(?:-|"|$)`)

func (a *App) runRelease(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "local" {
		return fmt.Errorf("%w: usage: argus-dev release local [--version VERSION] [--output DIR]", errUsage)
	}
	flags := flag.NewFlagSet("release local", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	defaultVersion := os.Getenv("ARGUS_RELEASE_VERSION")
	if defaultVersion == "" {
		defaultVersion = "local-" + time.Now().UTC().Format("20060102T150405Z")
	}
	version := flags.String("version", defaultVersion, "release version")
	output := flags.String("output", "", "release output directory")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if flags.NArg() != 0 || strings.TrimSpace(*version) == "" {
		return fmt.Errorf("%w: invalid release arguments", errUsage)
	}
	if report := a.doctor(ctx, "release"); !report.Ready {
		return fmt.Errorf("%w: doctor release found missing requirements", errCapability)
	}
	out := *output
	if out == "" {
		out = filepath.Join(a.root, "artifacts", "m8-release", *version)
	} else if !filepath.IsAbs(out) {
		out = filepath.Join(a.root, out)
	}
	return a.releaseLocal(ctx, *version, out)
}

func (a *App) releaseLocal(ctx context.Context, version, out string) (returnErr error) {
	for _, directory := range []string{"bin", "charts", "images", "sbom", "signatures"} {
		if err := os.MkdirAll(filepath.Join(out, directory), 0o700); err != nil {
			return err
		}
	}
	backendImage := "argus/argus-backend:" + version
	webImage := "argus/argus-web:" + version
	defer func() {
		_ = a.runner.Run(context.Background(), nil, "docker", "image", "rm", "--force", backendImage, webImage)
	}()

	_, _ = fmt.Fprintln(a.stdout, "running local release gates")
	if err := a.contractCheck(ctx); err != nil {
		return err
	}
	if err := a.contractBreaking(ctx); err != nil {
		return err
	}
	if err := a.checkQueryParsers(ctx); err != nil {
		return err
	}
	for _, command := range []struct {
		name string
		args []string
	}{
		{"go", []string{"test", "./..."}}, {"go", []string{"vet", "-stdmethods=false", "./..."}},
		{"pnpm", []string{"typecheck"}}, {"pnpm", []string{"lint"}}, {"pnpm", []string{"test"}},
		{"pnpm", []string{"build"}}, {"pnpm", []string{"check:bundle"}}, {"pnpm", []string{"check:real-build"}},
		{"pnpm", []string{"e2e"}},
	} {
		if err := a.runner.Run(ctx, nil, command.name, command.args...); err != nil {
			return err
		}
	}
	if err := a.checkProductionArtifacts(ctx); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "git", "diff", "--check"); err != nil {
		return err
	}

	for _, name := range []string{"argus-server", "argus-worker", "argus-connector-gateway", "argus-telemetry", "argusctl", "argus-migrate"} {
		env := map[string]string{"CGO_ENABLED": "0", "GOOS": "linux", "GOARCH": "arm64"}
		if err := a.runner.Run(ctx, env, "go", "build", "-trimpath", "-o", filepath.Join(out, "bin", name), "./cmd/"+name); err != nil {
			return err
		}
	}
	if err := a.runToFile(ctx, filepath.Join(out, "go-modules.sbom.jsonl"), "go", "list", "-m", "-json", "all"); err != nil {
		return err
	}
	licenses := filepath.Join(out, "web-licenses.json")
	if err := a.runToFile(ctx, licenses, "pnpm", "licenses", "list", "--json"); err != nil {
		return err
	}
	licenseData, err := os.ReadFile(licenses)
	if err != nil {
		return err
	}
	if strongCopyleftPattern.Match(licenseData) {
		return fmt.Errorf("disallowed strong-copyleft runtime dependency found")
	}
	vulnerability := filepath.Join(out, "govulncheck.json")
	if _, err := exec.LookPath("govulncheck"); err == nil {
		err = a.runToFile(ctx, vulnerability, "govulncheck", "-json", "./...")
	} else {
		err = a.runToFile(ctx, vulnerability, "go", "tool", "govulncheck", "-json", "./...")
	}
	if err != nil {
		return err
	}

	if err := a.runner.Run(ctx, nil, "docker", "build", "--quiet", "--platform", "linux/arm64", "--file", "deploy/docker/backend.Dockerfile", "--tag", backendImage, "."); err != nil {
		return err
	}
	if err := a.runner.Run(ctx, nil, "docker", "build", "--quiet", "--platform", "linux/arm64", "--file", "deploy/docker/web.Dockerfile", "--tag", webImage,
		"--build-arg", "VITE_API_MODE=real", "--build-arg", "VITE_API_BASE_URL=/", "."); err != nil {
		return err
	}
	if err := a.saveDockerImage(ctx, backendImage, filepath.Join(out, "images", "argus-backend-linux-arm64.tar.gz")); err != nil {
		return err
	}
	if err := a.saveDockerImage(ctx, webImage, filepath.Join(out, "images", "argus-web-linux-arm64.tar.gz")); err != nil {
		return err
	}
	if err := a.generateSPDX(ctx, backendImage, filepath.Join(out, "sbom", "argus-backend.spdx.json")); err != nil {
		return err
	}
	if err := a.generateSPDX(ctx, webImage, filepath.Join(out, "sbom", "argus-web.spdx.json")); err != nil {
		return err
	}
	charts, err := filepath.Glob(filepath.Join(a.root, "deploy", "helm", "*"))
	if err != nil {
		return err
	}
	for _, directory := range charts {
		if _, err := os.Stat(filepath.Join(directory, "Chart.yaml")); err != nil {
			continue
		}
		chart, err := loader.LoadDir(directory)
		if err != nil {
			return err
		}
		if _, err := chartutil.Save(chart, filepath.Join(out, "charts")); err != nil {
			return err
		}
	}
	commit, err := a.runner.Output(ctx, nil, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(out, "commit.txt"), []byte(commit+"\n")); err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(out, "version.txt"), []byte(version+"\n")); err != nil {
		return err
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return err
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	if err := writePrivate(filepath.Join(out, "local-signing-public.pem"), publicPEM); err != nil {
		return err
	}
	artifacts, err := releaseArtifacts(filepath.Join(out, "images"), filepath.Join(out, "charts"))
	if err != nil {
		return err
	}
	for _, artifact := range artifacts {
		if err := signFile(privateKey, artifact, filepath.Join(out, "signatures", filepath.Base(artifact)+".sig")); err != nil {
			return err
		}
	}
	binaryArchive := filepath.Join(out, "argus-"+version+"-linux-arm64.tar.gz")
	if err := archiveDirectory(filepath.Join(out, "bin"), binaryArchive, "bin"); err != nil {
		return err
	}
	releaseData, _ := json.MarshalIndent(map[string]any{
		"version": version, "completion_state": "local_hardening_complete", "platform": "linux/arm64",
		"production_ready": false, "production_profile_installable": false,
	}, "", "  ")
	if err := writePrivate(filepath.Join(out, "release.json"), append(releaseData, '\n')); err != nil {
		return err
	}
	manifestPath := filepath.Join(out, "offline-manifest.sha256")
	manifestRoots := []string{
		filepath.Join(a.root, "api", "openapi", "generated"), filepath.Join(a.root, "migrations"), filepath.Join(a.root, "deploy", "helm"),
		filepath.Join(out, "bin"), filepath.Join(out, "charts"), filepath.Join(out, "images"), filepath.Join(out, "sbom"), filepath.Join(out, "signatures"),
		binaryArchive, filepath.Join(out, "release.json"),
	}
	if err := writeChecksumManifest(a.root, out, manifestRoots, manifestPath); err != nil {
		return err
	}
	if err := signFile(privateKey, manifestPath, filepath.Join(out, "offline-manifest.sig")); err != nil {
		return err
	}
	if err := makePrivateTree(out); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "local release evidence: %s\n", out)
	return nil
}

func (a *App) runToFile(ctx context.Context, path, name string, args ...string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return a.runner.RunIO(ctx, nil, nil, file, a.stderr, name, args...)
}

func (a *App) saveDockerImage(ctx context.Context, image, path string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewWriterLevel(file, gzip.BestCompression)
	if err != nil {
		return err
	}
	if err := a.runner.RunIO(ctx, nil, nil, compressed, a.stderr, "docker", "save", image); err != nil {
		_ = compressed.Close()
		return err
	}
	return compressed.Close()
}

func (a *App) generateSPDX(ctx context.Context, image, path string) error {
	if help, err := a.runner.Output(ctx, nil, "docker", "sbom", "--help"); err == nil && strings.Contains(help, "spdx-json") {
		if err := a.runToFile(ctx, path, "docker", "sbom", image, "--format", "spdx-json"); err != nil {
			return err
		}
	} else if help, err := a.runner.Output(ctx, nil, "docker", "scout", "sbom", "--help"); err == nil && strings.Contains(help, "--format") {
		if err := a.runToFile(ctx, path, "docker", "scout", "sbom", "--format", "spdx", "local://"+image); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("an SPDX-capable docker sbom or docker scout plugin is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document struct {
		SPDXVersion string `json:"spdxVersion"`
	}
	if err := json.Unmarshal(data, &document); err != nil || !strings.HasPrefix(document.SPDXVersion, "SPDX-") {
		return fmt.Errorf("invalid SPDX document %s", path)
	}
	return nil
}

func releaseArtifacts(directories ...string) ([]string, error) {
	var result []string
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				result = append(result, filepath.Join(directory, entry.Name()))
			}
		}
	}
	sort.Strings(result)
	return result, nil
}

func signFile(privateKey ed25519.PrivateKey, source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(privateKey, data)
	return writePrivate(destination, signature)
}

func archiveDirectory(source, destination, prefix string) error {
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed := gzip.NewWriter(file)
	defer compressed.Close()
	archive := tar.NewWriter(compressed)
	defer archive.Close()
	return filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(prefix, relative))
		header.ModTime = time.Unix(0, 0).UTC()
		header.AccessTime = header.ModTime
		header.ChangeTime = header.ModTime
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(archive, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func writeChecksumManifest(repoRoot, releaseRoot string, roots []string, destination string) error {
	type entry struct{ path, hash string }
	var entries []entry
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			hash, err := fileSHA256(root)
			if err != nil {
				return err
			}
			path, err := manifestPath(repoRoot, releaseRoot, root)
			if err != nil {
				return err
			}
			entries = append(entries, entry{path, hash})
			continue
		}
		err = filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil || item.IsDir() {
				return walkErr
			}
			hash, err := fileSHA256(path)
			if err != nil {
				return err
			}
			manifestName, err := manifestPath(repoRoot, releaseRoot, path)
			if err != nil {
				return err
			}
			entries = append(entries, entry{manifestName, hash})
			return nil
		})
		if err != nil {
			return err
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	var content strings.Builder
	for _, item := range entries {
		_, _ = fmt.Fprintf(&content, "%s  %s\n", item.hash, item.path)
	}
	return writePrivate(destination, []byte(content.String()))
}

func manifestPath(repoRoot, releaseRoot, path string) (string, error) {
	for _, root := range []string{releaseRoot, repoRoot} {
		relative, err := filepath.Rel(root, path)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.ToSlash(relative), nil
		}
	}
	return "", fmt.Errorf("manifest path %s is outside repository and release roots", path)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writePrivate(path string, content []byte) error {
	return os.WriteFile(path, content, 0o600)
}

func makePrivateTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o700)
		}
		return os.Chmod(path, 0o600)
	})
}
