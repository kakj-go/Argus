package argusdev

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type E2ECollectorArtifacts struct {
	Version          string
	LinuxPath        string
	LinuxURI         string
	LinuxSHA256      string
	LinuxSignature   string
	LinuxByteSize    uint64
	WindowsPath      string
	WindowsURI       string
	WindowsSHA256    string
	WindowsSignature string
	WindowsByteSize  uint64
	SigningKeyID     string
	SigningPublicKey string
	TLS              fixtureCertificate
}

func (a *App) prepareE2ECollectorArtifacts(ctx context.Context, env *E2EEnvironment) error {
	if !suiteHas(env.Options.Suite, "m7") && env.Options.Suite != "m10-query" {
		return nil
	}
	if env.ImagePlatform != "linux/arm64" {
		return fmt.Errorf("%w: %s requires an arm64 Kubernetes node for the locked Collector distribution", errCapability, env.Options.Suite)
	}
	linuxPath := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-linux-arm64.tar.gz")
	windowsPath := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-windows-amd64.zip")
	if err := a.ensureCollectorArtifact(ctx, "linux-arm64", linuxPath); err != nil {
		return err
	}
	if err := a.ensureCollectorArtifact(ctx, "windows-amd64", windowsPath); err != nil {
		return err
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	linuxHash, linuxSize, linuxSignature, err := signCollectorArtifact(privateKey, linuxPath)
	if err != nil {
		return err
	}
	windowsHash, windowsSize, windowsSignature, err := signCollectorArtifact(privateKey, windowsPath)
	if err != nil {
		return err
	}
	serviceName := "argus-e2e-artifact-server"
	dnsName := serviceName + "." + env.SystemNS + ".svc"
	tls, err := generateFixtureCertificate(serviceName, []string{serviceName, dnsName}, nil)
	if err != nil {
		return err
	}
	env.CollectorArtifacts = &E2ECollectorArtifacts{
		Version: "0.1.0-m7", LinuxPath: linuxPath,
		LinuxURI:    "https://" + dnsName + ":8443/m7/linux-arm64.tar.gz",
		LinuxSHA256: linuxHash, LinuxSignature: linuxSignature, LinuxByteSize: linuxSize,
		WindowsPath: windowsPath, WindowsURI: "https://artifacts.argus.invalid/m7/windows-amd64.zip",
		WindowsSHA256: windowsHash, WindowsSignature: windowsSignature, WindowsByteSize: windowsSize,
		SigningKeyID:     "argus-e2e-" + env.Options.RunID,
		SigningPublicKey: base64.RawStdEncoding.EncodeToString(publicKey), TLS: tls,
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
	artifactReady := false
	if info, err := os.Stat(destination); err == nil && info.Size() > 0 {
		artifactReady = true
	}
	if binaryInfo, err := os.Stat(binaryPath); !force && artifactReady && err == nil && binaryInfo.Size() > 0 {
		return nil
	}
	builderConfig := filepath.Join("build", "otelcol", "builder-"+platform+".yaml")
	if err := a.runner.Run(ctx, nil, "go", "run", "go.opentelemetry.io/collector/cmd/builder@v0.133.0", "--skip-compilation", "--config", builderConfig); err != nil {
		return err
	}
	runner := a.runner
	runner.Dir = dist
	env := map[string]string{"CGO_ENABLED": "0"}
	switch platform {
	case "linux-arm64":
		env["GOOS"], env["GOARCH"] = "linux", "arm64"
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
	if platform == "linux-arm64" {
		return writeCollectorTarGz(binaryPath, destination)
	}
	return writeCollectorZip(binaryPath, filepath.Join(a.root, "build", "otelcol", "install-windows.ps1"), destination)
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
