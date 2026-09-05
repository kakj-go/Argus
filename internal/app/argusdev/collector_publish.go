package argusdev

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	defaultPublishRepository = "docker.io/kakj-go/argus-otelcol"
	defaultArtifactBucket    = "argus-collector-artifacts"
	defaultSigningKeyID      = "argus-collector-signing-v1"
	signingKeyFile           = "deploy/.keys/otelcol-signing-key.json"
)

// parseFlags 把 `--key value` / `--flag` 形式的参数解析为 map,剩余位置参数原样返回。
func parseFlags(args []string) (map[string]string, []string, error) {
	flags := map[string]string{}
	var positional []string
	for index := 0; index < len(args); index++ {
		item := args[index]
		if !strings.HasPrefix(item, "--") {
			positional = append(positional, item)
			continue
		}
		name, value, found := strings.Cut(strings.TrimPrefix(item, "--"), "=")
		if !found {
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				flags[name] = "true"
				continue
			}
			index++
			value = args[index]
		}
		flags[name] = value
	}
	return flags, positional, nil
}

// collectorVersion 从 builder 配置读取分发包版本,作为镜像 tag 与产物路径的默认值。
func (a *App) collectorVersion() (string, error) {
	path := filepath.Join(a.root, "deploy", "otelcol", "builder-linux-arm64.yaml")
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if version, found := strings.CutPrefix(line, "version:"); found {
			version = strings.TrimSpace(version)
			if version != "" {
				return version, nil
			}
		}
	}
	return "", fmt.Errorf("collector version not found in %s", path)
}

// publishCollectorImage 构建 argus-otelcol 镜像并按需推送到 registry。
// 推送时发布 linux/arm64 + linux/amd64 双架构并合成多架构 manifest,
// 使同一镜像引用在两种架构的 Kubernetes 集群上均可拉取;
// 本地构建(无 --push)只构建宿主友好的 arm64 单架构。
func (a *App) publishCollectorImage(ctx context.Context, args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	repository := flags["repository"]
	if repository == "" {
		repository = defaultPublishRepository
	}
	tag := flags["tag"]
	if tag == "" {
		if tag, err = a.collectorVersion(); err != nil {
			return err
		}
	}
	// 两个架构的二进制先就绪(交叉编译,无需模拟器;镜像阶段只 COPY)。
	for _, platform := range []string{"linux-arm64", "linux-amd64"} {
		artifact := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-"+platform+".tar.gz")
		if err := a.buildCollectorArtifact(ctx, platform, artifact, false); err != nil {
			return err
		}
	}
	reference := repository + ":" + tag
	dockerfile := filepath.Join("deploy", "docker", "otelcol.Dockerfile")
	if flags["push"] != "true" {
		fmt.Fprintf(a.stdout, "building image %s locally (linux/arm64)\n", reference)
		if err := a.runner.Run(ctx, nil, "docker", "build", "--platform", "linux/arm64",
			"-f", dockerfile, "-t", reference, a.root); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "image built locally; rerun with --push to publish a multi-arch manifest to %s\n", repository)
		return nil
	}
	var archReferences []string
	for _, arch := range []struct{ platform, distDir, suffix string }{
		{"linux/arm64", "build/otelcol/dist/linux-arm64", "arm64"},
		{"linux/amd64", "build/otelcol/dist/linux-amd64", "amd64"},
	} {
		archReference := reference + "-" + arch.suffix
		fmt.Fprintf(a.stdout, "building and pushing %s (%s)\n", archReference, arch.platform)
		if err := a.runner.Run(ctx, nil, "docker", "buildx", "build", "--platform", arch.platform,
			"--build-arg", "DIST_PATH="+arch.distDir, "-f", dockerfile, "-t", archReference, "--push", a.root); err != nil {
			return err
		}
		archReferences = append(archReferences, archReference)
	}
	fmt.Fprintf(a.stdout, "creating multi-arch manifest %s\n", reference)
	createArgs := append([]string{"imagetools", "create", "-t", reference}, archReferences...)
	if err := a.runner.Run(ctx, nil, "docker", createArgs...); err != nil {
		return err
	}
	fmt.Fprintf(a.stdout, "published multi-arch image %s (linux/arm64, linux/amd64)\n", reference)
	return nil
}

type signingKeyMaterial struct {
	KeyID      string `json:"key_id"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
}

// loadSigningKey 加载持久签名密钥:优先环境变量 ARGUS_OTELCOL_SIGNING_PRIVATE_KEY
// (base64 RawStd 私钥),其次 deploy/.keys/otelcol-signing-key.json;不存在时生成并落盘。
func (a *App) loadSigningKey(keyID string) (ed25519.PrivateKey, ed25519.PublicKey, string, error) {
	if seed := strings.TrimSpace(os.Getenv("ARGUS_OTELCOL_SIGNING_PRIVATE_KEY")); seed != "" {
		decoded, err := base64.RawStdEncoding.DecodeString(seed)
		if err != nil || len(decoded) != ed25519.PrivateKeySize {
			return nil, nil, "", fmt.Errorf("ARGUS_OTELCOL_SIGNING_PRIVATE_KEY must be a base64 ed25519 private key")
		}
		private := ed25519.PrivateKey(decoded)
		return private, private.Public().(ed25519.PublicKey), keyID, nil
	}
	path := filepath.Join(a.root, signingKeyFile)
	if material, err := os.ReadFile(path); err == nil {
		var stored signingKeyMaterial
		if json.Unmarshal(material, &stored) == nil && stored.PrivateKey != "" && stored.PublicKey != "" {
			private, decodeErr := base64.RawStdEncoding.DecodeString(stored.PrivateKey)
			public, publicErr := base64.RawStdEncoding.DecodeString(stored.PublicKey)
			if decodeErr != nil || publicErr != nil || len(private) != ed25519.PrivateKeySize || len(public) != ed25519.PublicKeySize {
				return nil, nil, "", fmt.Errorf("signing key file %s is corrupted", path)
			}
			if stored.KeyID != "" {
				keyID = stored.KeyID
			}
			return ed25519.PrivateKey(private), ed25519.PublicKey(public), keyID, nil
		}
		return nil, nil, "", fmt.Errorf("signing key file %s is corrupted", path)
	}
	fmt.Fprintf(a.stdout, "generating new signing key at %s (keep it secret; reuse it for future releases)\n", path)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, "", err
	}
	encoded, err := json.MarshalIndent(signingKeyMaterial{KeyID: keyID,
		PrivateKey: base64.RawStdEncoding.EncodeToString(private),
		PublicKey:  base64.RawStdEncoding.EncodeToString(public)}, "", "  ")
	if err != nil {
		return nil, nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, "", err
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return nil, nil, "", err
	}
	return private, public, keyID, nil
}

// publishCollectorArtifacts 构建、签名并上传主机 Collector 产物到对象存储,
// 打印可直接粘贴进安装配置 spec.telemetry 的值块。
func (a *App) publishCollectorArtifacts(ctx context.Context, args []string) error {
	flags, _, err := parseFlags(args)
	if err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	endpoint := flags["endpoint"]
	bucket := flags["bucket"]
	accessKey, secretKey := flags["access-key"], flags["secret-key"]
	if bucket == "" {
		bucket = defaultArtifactBucket
	}
	publicBase := flags["public-base"]
	if publicBase == "" {
		publicBase = endpoint
	}
	keyID := flags["key-id"]
	if keyID == "" {
		keyID = defaultSigningKeyID
	}
	parsed, parseErr := url.Parse(endpoint)
	if parseErr != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") ||
		accessKey == "" || secretKey == "" || publicBase == "" {
		return fmt.Errorf("%w: usage: argus-dev collector publish-artifacts --endpoint <https://host> --access-key <k> --secret-key <k> [--bucket %s] [--public-base <https://host>] [--key-id <id>] [--windows] [--skip-arm64] [--skip-amd64]",
			errUsage, defaultArtifactBucket)
	}
	withWindows := flags["windows"] == "true"
	version, err := a.collectorVersion()
	if err != nil {
		return err
	}
	private, public, keyID, err := a.loadSigningKey(keyID)
	if err != nil {
		return err
	}
	client, err := minio.New(parsed.Host, &minio.Options{Creds: credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: parsed.Scheme == "https", Region: "us-east-1"})
	if err != nil {
		return err
	}
	base := strings.TrimSuffix(publicBase, "/")
	fmt.Fprintf(a.stdout, "\nspec.telemetry values for the install config:\n\n")
	fmt.Fprintf(a.stdout, "  collectorVersion: %s\n", version)
	// linux 双架构默认都发布;主机安装按探测到的架构选择产物。
	linuxPlatforms := []struct {
		label, path, suffix, specPrefix string
		skip                            bool
	}{
		{"linux-arm64", "argus-otelcol-linux-arm64.tar.gz", "linux-arm64.tar.gz", "linuxArm64", flags["skip-arm64"] == "true"},
		{"linux-amd64", "argus-otelcol-linux-amd64.tar.gz", "linux-amd64.tar.gz", "linuxAmd64", flags["skip-amd64"] == "true"},
	}
	for _, item := range linuxPlatforms {
		if item.skip {
			continue
		}
		path := filepath.Join(a.root, "build", "otelcol", "artifacts", item.path)
		if err := a.buildCollectorArtifact(ctx, item.label, path, true); err != nil {
			return err
		}
		hash, size, signature, err := signCollectorArtifact(private, path)
		if err != nil {
			return err
		}
		key := "argus-otelcol/" + version + "/" + item.suffix
		fmt.Fprintf(a.stdout, "uploading %s → %s/%s\n", path, bucket, key)
		if _, err = client.FPutObject(ctx, bucket, key, path, minio.PutObjectOptions{ContentType: "application/gzip"}); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "  %sUri: %s/%s/%s\n", item.specPrefix, base, bucket, key)
		fmt.Fprintf(a.stdout, "  %sSha256: %s\n", item.specPrefix, hash)
		fmt.Fprintf(a.stdout, "  %sSignature: %s\n", item.specPrefix, signature)
		fmt.Fprintf(a.stdout, "  %sByteSize: %d\n", item.specPrefix, size)
	}
	// 自助注册主机的安装脚本与产物同桶发布;脚本 URL 由安装命令按产物 origin 推导。
	scriptPath := filepath.Join(a.root, "deploy", "scripts", "host-install.sh")
	scriptKey := "install/host.sh"
	fmt.Fprintf(a.stdout, "uploading %s → %s/%s\n", scriptPath, bucket, scriptKey)
	if _, err = client.FPutObject(ctx, bucket, scriptKey, scriptPath, minio.PutObjectOptions{ContentType: "application/x-sh"}); err != nil {
		return err
	}

	windowsPath := filepath.Join(a.root, "build", "otelcol", "artifacts", "argus-otelcol-windows-amd64.zip")
	if withWindows {
		if err := a.buildCollectorArtifact(ctx, "windows-amd64", windowsPath, true); err != nil {
			return err
		}
		windowsHash, windowsSize, windowsSignature, err := signCollectorArtifact(private, windowsPath)
		if err != nil {
			return err
		}
		windowsKey := "argus-otelcol/" + version + "/windows-amd64.zip"
		fmt.Fprintf(a.stdout, "uploading %s → %s/%s\n", windowsPath, bucket, windowsKey)
		if _, err = client.FPutObject(ctx, bucket, windowsKey, windowsPath, minio.PutObjectOptions{ContentType: "application/zip"}); err != nil {
			return err
		}
		fmt.Fprintf(a.stdout, "  windowsAmd64Uri: %s/%s/%s\n", base, bucket, windowsKey)
		fmt.Fprintf(a.stdout, "  windowsAmd64Sha256: %s\n", windowsHash)
		fmt.Fprintf(a.stdout, "  windowsAmd64Signature: %s\n", windowsSignature)
		fmt.Fprintf(a.stdout, "  windowsAmd64ByteSize: %d\n", windowsSize)
	}
	fmt.Fprintf(a.stdout, "  signingKeyId: %s\n", keyID)
	fmt.Fprintf(a.stdout, "  signingPublicKey: %s\n", base64.RawStdEncoding.EncodeToString(public))
	fmt.Fprintf(a.stdout, "\nARGUS_OTELCOL_SIGNING_PUBLIC_KEYS={%q:%q}\n", keyID, base64.RawStdEncoding.EncodeToString(public))
	return nil
}
