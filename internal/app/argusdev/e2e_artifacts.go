package argusdev

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const collectorBuilderVersion = "v0.133.0"

type E2ECollectorArtifacts struct {
	Version             string
	LinuxPath           string
	LinuxURI            string
	LinuxSHA256         string
	LinuxSignature      string
	LinuxByteSize       uint64
	LinuxAMD64Path      string
	LinuxAMD64URI       string
	LinuxAMD64SHA256    string
	LinuxAMD64Signature string
	LinuxAMD64ByteSize  uint64
	WindowsPath         string
	WindowsURI          string
	WindowsSHA256       string
	WindowsSignature    string
	WindowsByteSize     uint64
	SigningKeyID        string
	SigningPublicKey    string
	TLS                 fixtureCertificate
}

// E2EArtifactSigning is the ephemeral trust root shared by every immutable
// artifact published in one E2E run. Production Collector and Connector
// publishing use the same persisted signing key, so the suite mirrors that
// trust boundary instead of creating an untrusted second release key.
type E2EArtifactSigning struct {
	KeyID      string
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
}

func (env *E2EEnvironment) clearArtifactSigningPrivateKey() {
	if env.ArtifactSigning == nil {
		return
	}
	clear(env.ArtifactSigning.PrivateKey)
	env.ArtifactSigning.PrivateKey = nil
}

func (a *App) prepareE2ECollectorArtifacts(ctx context.Context, env *E2EEnvironment) error {
	if !suiteHas(env.Options.Suite, "m7") && env.Options.Suite != "m10-query" && env.Options.Suite != "p4" {
		return nil
	}
	if env.Options.Suite != "p4" && env.ImagePlatform != "linux/arm64" {
		return fmt.Errorf("%w: %s requires an arm64 Kubernetes node for the locked Collector distribution", errCapability, env.Options.Suite)
	}
	linuxPath := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-linux-arm64.tar.gz")
	linuxAMD64Path := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-linux-amd64.tar.gz")
	windowsPath := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-windows-amd64.zip")
	if err := a.ensureCollectorArtifact(ctx, "linux-arm64", linuxPath); err != nil {
		return err
	}
	if err := a.ensureCollectorArtifact(ctx, "windows-amd64", windowsPath); err != nil {
		return err
	}
	if err := a.ensureCollectorArtifact(ctx, "linux-amd64", linuxAMD64Path); err != nil {
		return err
	}
	if env.ArtifactSigning == nil || len(env.ArtifactSigning.PrivateKey) != ed25519.PrivateKeySize || len(env.ArtifactSigning.PublicKey) != ed25519.PublicKeySize {
		return errors.New("E2E artifact signing root is unavailable")
	}
	linuxHash, linuxSize, linuxSignature, err := signCollectorArtifact(env.ArtifactSigning.PrivateKey, linuxPath)
	if err != nil {
		return err
	}
	windowsHash, windowsSize, windowsSignature, err := signCollectorArtifact(env.ArtifactSigning.PrivateKey, windowsPath)
	if err != nil {
		return err
	}
	linuxAMD64Hash, linuxAMD64Size, linuxAMD64Signature, err := signCollectorArtifact(env.ArtifactSigning.PrivateKey, linuxAMD64Path)
	if err != nil {
		return err
	}
	dnsName := "argus-e2e-artifact-server." + env.SystemNS + ".svc"
	env.CollectorArtifacts = &E2ECollectorArtifacts{
		Version: "0.1.0-m7", LinuxPath: linuxPath,
		LinuxURI:    "https://" + dnsName + ":8443/m7/linux-arm64.tar.gz",
		LinuxSHA256: linuxHash, LinuxSignature: linuxSignature, LinuxByteSize: linuxSize,
		LinuxAMD64Path: linuxAMD64Path, LinuxAMD64URI: "https://" + dnsName + ":8443/m7/linux-amd64.tar.gz",
		LinuxAMD64SHA256: linuxAMD64Hash, LinuxAMD64Signature: linuxAMD64Signature, LinuxAMD64ByteSize: linuxAMD64Size,
		WindowsPath: windowsPath, WindowsURI: "https://artifacts.argus.invalid/m7/windows-amd64.zip",
		WindowsSHA256: windowsHash, WindowsSignature: windowsSignature, WindowsByteSize: windowsSize,
		SigningKeyID:     env.ArtifactSigning.KeyID,
		SigningPublicKey: base64.RawStdEncoding.EncodeToString(env.ArtifactSigning.PublicKey), TLS: env.ArtifactTLS,
	}
	staging := filepath.Join(a.root, "build", "e2e-artifacts", "m7")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	for source, name := range map[string]string{linuxPath: "linux-arm64.tar.gz", linuxAMD64Path: "linux-amd64.tar.gz"} {
		if err := copyE2EArtifact(source, filepath.Join(staging, name)); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) ensureCollectorArtifact(ctx context.Context, platform, destination string) error {
	return a.buildCollectorArtifact(ctx, platform, destination, false)
}

func (a *App) buildCollectorArtifact(ctx context.Context, platform, destination string, force bool) error {
	binaryName := "argus-otelcol"
	if platform == "windows-amd64" {
		binaryName += ".exe"
	}
	dist := filepath.Join(a.root, "build", "otelcol", "dist", platform)
	binaryPath := filepath.Join(dist, binaryName)
	inputFingerprint, err := a.collectorArtifactInputFingerprint(platform)
	if err != nil {
		return err
	}
	if !force && collectorArtifactCurrent(destination, binaryPath, inputFingerprint) {
		return nil
	}
	builderConfig := filepath.Join("deploy", "otelcol", "builder-"+platform+".yaml")
	if err := a.runner.Run(ctx, nil, "go", "run", "go.opentelemetry.io/collector/cmd/builder@"+collectorBuilderVersion, "--skip-compilation", "--config", builderConfig); err != nil {
		return err
	}
	runner := a.runner
	runner.Dir = dist
	env := map[string]string{"CGO_ENABLED": "0"}
	switch platform {
	case "linux-arm64":
		env["GOOS"], env["GOARCH"] = "linux", "arm64"
	case "linux-amd64":
		env["GOOS"], env["GOARCH"] = "linux", "amd64"
	case "windows-amd64":
		env["GOOS"], env["GOARCH"] = "windows", "amd64"
	default:
		return fmt.Errorf("unsupported Collector artifact platform %s", platform)
	}
	if err := runner.Run(ctx, env, "go", "build", "-trimpath", "-o", binaryName, "."); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if platform == "windows-amd64" {
		err = writeCollectorZip(binaryPath, filepath.Join(a.root, "deploy", "otelcol", "install-windows.ps1"), destination)
	} else {
		err = writeCollectorTarGz(binaryPath, destination)
	}
	if err != nil {
		return err
	}
	return writePrivate(collectorArtifactFingerprintPath(destination), []byte(inputFingerprint+"\n"))
}

// collectorArtifactInputFingerprint binds a cached distribution to every
// version-controlled input that can change the custom Collector binary. The
// build/ directory is intentionally ignored, so existence alone can never be
// used as freshness evidence.
func (a *App) collectorArtifactInputFingerprint(platform string) (string, error) {
	config := filepath.Join(a.root, "deploy", "otelcol", "builder-"+platform+".yaml")
	inputs := []string{config}
	for _, directory := range []string{
		filepath.Join(a.root, "internal", "otelcol", "argusidentity"),
		filepath.Join(a.root, "internal", "otelcol", "argusgatewayidentity"),
	} {
		err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.Type().IsRegular() {
				inputs = append(inputs, path)
			}
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	if platform == "windows-amd64" {
		inputs = append(inputs, filepath.Join(a.root, "deploy", "otelcol", "install-windows.ps1"))
	}
	sort.Strings(inputs)
	digest := sha256.New()
	_, _ = fmt.Fprintf(digest, "argus.collector_artifact_inputs/v1\x00%s\x00%s\x00", platform, collectorBuilderVersion)
	for _, path := range inputs {
		value, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		relative, err := filepath.Rel(a.root, path)
		if err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(digest, "%d:%s:%d:", len(relative), filepath.ToSlash(relative), len(value))
		_, _ = digest.Write(value)
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func collectorArtifactCurrent(destination, binaryPath, inputFingerprint string) bool {
	for _, path := range []string{destination, binaryPath} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			return false
		}
	}
	value, err := os.ReadFile(collectorArtifactFingerprintPath(destination))
	return err == nil && string(value) == inputFingerprint+"\n"
}

func collectorArtifactFingerprintPath(destination string) string {
	return destination + ".inputs.sha256"
}

func signCollectorArtifact(privateKey ed25519.PrivateKey, path string) (string, uint64, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, "", err
	}
	digest := sha256.Sum256(data)
	signature := ed25519.Sign(privateKey, digest[:])
	return hex.EncodeToString(digest[:]), uint64(len(data)), base64.RawStdEncoding.EncodeToString(signature), nil
}

func writeCollectorTarGz(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	compressed := gzip.NewWriter(output)
	archive := tar.NewWriter(compressed)
	info, err := input.Stat()
	if err != nil {
		return err
	}
	header := &tar.Header{Name: "argus-otelcol", Mode: 0o755, Size: info.Size(), ModTime: time.Unix(0, 0).UTC()}
	if err := archive.WriteHeader(header); err != nil {
		return err
	}
	if _, err := io.Copy(archive, input); err != nil {
		return err
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

func writeCollectorZip(binary, installer, destination string) error {
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer output.Close()
	archive := zip.NewWriter(output)
	for _, item := range []struct{ path, name string }{{binary, "argus-otelcol.exe"}, {installer, "install-windows.ps1"}} {
		data, err := os.ReadFile(item.path)
		if err != nil {
			return err
		}
		header := &zip.FileHeader{Name: item.name, Method: zip.Deflate}
		header.SetModTime(time.Unix(0, 0).UTC())
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}
	return archive.Close()
}
