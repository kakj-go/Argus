package argusctl

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	backupMagic     = "ARGUSM8\x01"
	backupChunkSize = 1 << 20
)

type backupManifest struct {
	FormatVersion int               `json:"format_version"`
	RunID         string            `json:"run_id"`
	CreatedAt     time.Time         `json:"created_at"`
	Profile       string            `json:"profile"`
	ReleaseID     string            `json:"release_id"`
	Namespaces    Namespaces        `json:"namespaces"`
	Files         map[string]string `json:"files"`
}

type upgradeState struct {
	ReleaseID    string    `json:"release_id"`
	ConfigDigest string    `json:"config_digest"`
	Stage        string    `json:"stage"`
	UpdatedAt    time.Time `json:"updated_at"`
	Message      string    `json:"message"`
}

func (a *App) runBackup(ctx context.Context, operation string, args []string) error {
	flags := flag.NewFlagSet("backup "+operation, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/local-hardening.yaml", "ArgusInstallConfig file")
	artifacts := flags.String("artifacts", "artifacts/m8-backup", "backup directory")
	backupPath := flags.String("backup", "", "encrypted backup path")
	keyPath := flags.String("key-file", "", "backup recovery key path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg.Spec.Profile != "local-hardening" {
		return errors.New("M8 backup is restricted to the local-hardening profile")
	}
	switch operation {
	case "create":
		return a.createBackup(ctx, cfg, *artifacts)
	case "verify":
		if *backupPath == "" {
			return errors.New("backup verify requires --backup")
		}
		return a.verifyBackup(*backupPath, *keyPath, true)
	case "list":
		return a.listBackups(*artifacts)
	default:
		return fmt.Errorf("unsupported backup operation %q", operation)
	}
}

func (a *App) createBackup(ctx context.Context, cfg *InstallConfig, root string) error {
	runID := time.Now().UTC().Format("20060102T150405Z") + "-" + cfg.Spec.ReleaseID
	directory := filepath.Join(root, runID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp("", "argus-m8-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := os.Chmod(stage, 0o700); err != nil {
		return err
	}
	if err := a.captureBackupComponents(ctx, cfg, stage); err != nil {
		return err
	}
	manifest := backupManifest{FormatVersion: 1, RunID: runID, CreatedAt: time.Now().UTC(), Profile: cfg.Spec.Profile, ReleaseID: cfg.Spec.ReleaseID, Namespaces: cfg.Spec.Namespaces, Files: map[string]string{}}
	entries, err := os.ReadDir(stage)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "manifest.json" {
			continue
		}
		digest, err := fileSHA256(filepath.Join(stage, entry.Name()))
		if err != nil {
			return err
		}
		manifest.Files[entry.Name()] = digest
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stage, "manifest.json"), append(manifestJSON, '\n'), 0o600); err != nil {
		return err
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	archivePath := filepath.Join(directory, runID+".argusbak")
	keyPath := archivePath + ".key"
	if err := encryptDirectory(stage, archivePath, key); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, []byte(base64.RawURLEncoding.EncodeToString(key)+"\n"), 0o600); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "backup=%s\nkey_file=%s\nrun_id=%s\n", archivePath, keyPath, runID)
	return nil
}

func (a *App) captureBackupComponents(ctx context.Context, cfg *InstallConfig, stage string) error {
	kube := []string{"--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.System}
	passwordEncoded, err := a.runner.quiet(ctx, "kubectl", append(kube, "get", "secret", "argus-data-credentials", "-o", "jsonpath={.data.postgresql-password}")...)
	if err != nil {
		return err
	}
	password, err := base64.StdEncoding.DecodeString(strings.TrimSpace(passwordEncoded))
	if err != nil {
		return fmt.Errorf("decode PostgreSQL credential: %w", err)
	}
	postgres, err := a.runner.quiet(ctx, "kubectl", append(kube, "exec", "statefulset/argus-postgresql", "--", "env", "PGPASSWORD="+string(password), "pg_dump", "-U", "argus", "-d", "argus", "--format=custom")...)
	if err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(stage, "postgres.dump"), []byte(postgres)); err != nil {
		return err
	}
	openBaoSnapshot := "/tmp/argus-m8.snap"
	openBaoCommand := `state=/openbao/data/argus-bootstrap.json; root=$(sed -n 's/.*"root_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$state"); BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN="$root" bao operator raft snapshot save -force ` + openBaoSnapshot
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "exec", "statefulset/argus-openbao", "--", "sh", "-ec", openBaoCommand)...); err != nil {
		return err
	}
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "cp", "argus-openbao-0:"+openBaoSnapshot, filepath.Join(stage, "openbao.snap"))...); err != nil {
		return err
	}
	if err := a.capturePodArchive(ctx, kube, "argus-minio-0", "/data", filepath.Join(stage, "minio.tgz")); err != nil {
		return err
	}
	observability := []string{"--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability}
	pod, err := a.runner.quiet(ctx, "kubectl", append(observability, "get", "pods", "-l", "clickhouse.altinity.com/chi=argus-clickhouse", "-o", "jsonpath={.items[0].metadata.name}")...)
	if err != nil || strings.TrimSpace(pod) == "" {
		return fmt.Errorf("locate ClickHouse pod: %w", err)
	}
	if err := a.capturePodArchive(ctx, observability, strings.TrimSpace(pod), "/var/lib/clickhouse", filepath.Join(stage, "clickhouse.tgz")); err != nil {
		return err
	}
	resources, err := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "get", "configmap,secret", "--namespace", cfg.Spec.Namespaces.System, "-l", "app.kubernetes.io/part-of=argus", "-o", "yaml")
	if err != nil {
		return err
	}
	if err := writePrivate(filepath.Join(stage, "catalog-and-config.yaml"), []byte(resources)); err != nil {
		return err
	}
	openBaoToken, err := a.runner.quiet(ctx, "kubectl", append(kube, "get", "secret", cfg.Spec.ReleaseID+"-generated-credentials", "-o", "jsonpath={.data.openbao-token}")...)
	if err != nil {
		return err
	}
	decodedToken, err := base64.StdEncoding.DecodeString(strings.TrimSpace(openBaoToken))
	if err != nil || len(decodedToken) < 32 {
		return errors.New("backup OpenBao client token is invalid")
	}
	if err := writePrivate(filepath.Join(stage, "openbao-client-token"), decodedToken); err != nil {
		return err
	}
	configData, err := os.ReadFile(cfg.path)
	if err != nil {
		return err
	}
	return writePrivate(filepath.Join(stage, "install-config.yaml"), configData)
}

func (a *App) capturePodArchive(ctx context.Context, kube []string, pod, source, target string) error {
	remote := "/tmp/argus-m8-backup.tgz"
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "exec", pod, "--", "tar", "-czf", remote, "-C", source, ".")...); err != nil {
		return err
	}
	_, err := a.runner.quiet(ctx, "kubectl", append(kube, "cp", pod+":"+remote, target)...)
	return err
}

func (a *App) verifyBackup(backupPath, keyPath string, print bool) error {
	stage, manifest, err := openBackup(backupPath, keyPath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	for name, expected := range manifest.Files {
		actual, err := fileSHA256(filepath.Join(stage, name))
		if err != nil || actual != expected {
			return fmt.Errorf("backup component %s failed integrity verification", name)
		}
	}
	if print {
		_, _ = fmt.Fprintf(a.stdout, "backup verified: run_id=%s created_at=%s files=%d\n", manifest.RunID, manifest.CreatedAt.Format(time.RFC3339), len(manifest.Files))
	}
	return nil
}

func (a *App) listBackups(root string) error {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() && strings.HasSuffix(entry.Name(), ".argusbak") {
			paths = append(paths, path)
		}
		return err
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		_, _ = fmt.Fprintln(a.stdout, path)
	}
	return nil
}

func (a *App) runRestore(ctx context.Context, operation string, args []string) error {
	flags := flag.NewFlagSet("restore "+operation, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/local-hardening.yaml", "target ArgusInstallConfig file")
	backupPath := flags.String("backup", "", "encrypted backup path")
	keyPath := flags.String("key-file", "", "backup recovery key path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *backupPath == "" {
		return errors.New("restore requires --backup")
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg.Spec.Profile != "local-hardening" {
		return errors.New("M8 restore is restricted to the local-hardening profile")
	}
	if operation == "verify" {
		return a.verifyBackup(*backupPath, *keyPath, true)
	}
	stage, manifest, err := openBackup(*backupPath, *keyPath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := ensureDistinctRestoreTarget(cfg, manifest); err != nil {
		return err
	}
	if err := a.ensureRestoreNamespacesAbsent(ctx, cfg); err != nil {
		return err
	}
	if operation == "plan" {
		_, _ = fmt.Fprintf(a.stdout, "restore plan: backup=%s target=%s/%s/%s\n", manifest.RunID, cfg.Spec.Namespaces.System, cfg.Spec.Namespaces.Sandbox, cfg.Spec.Namespaces.Observability)
		return nil
	}
	if operation != "apply" {
		return fmt.Errorf("unsupported restore operation %q", operation)
	}
	openBaoToken, err := os.ReadFile(filepath.Join(stage, "openbao-client-token"))
	if err != nil || len(bytes.TrimSpace(openBaoToken)) < 32 {
		return errors.New("backup does not contain a valid OpenBao client token")
	}
	a.restoreOpenBaoToken = strings.TrimSpace(string(openBaoToken))
	defer func() { a.restoreOpenBaoToken = "" }()
	if err := a.install(ctx, cfg); err != nil {
		return err
	}
	if err := a.applyRestore(ctx, cfg, stage); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "restore applied: backup=%s target_release=%s\n", manifest.RunID, cfg.Spec.ReleaseID)
	return nil
}

func ensureDistinctRestoreTarget(cfg *InstallConfig, manifest backupManifest) error {
	if cfg.Spec.ReleaseID == manifest.ReleaseID || cfg.Spec.Namespaces.System == manifest.Namespaces.System || cfg.Spec.Namespaces.Sandbox == manifest.Namespaces.Sandbox || cfg.Spec.Namespaces.Observability == manifest.Namespaces.Observability {
		return errors.New("restore target must use a unique release ID and three new namespaces")
	}
	return nil
}

func (a *App) ensureRestoreNamespacesAbsent(ctx context.Context, cfg *InstallConfig) error {
	for _, namespace := range []string{cfg.Spec.Namespaces.System, cfg.Spec.Namespaces.Sandbox, cfg.Spec.Namespaces.Observability} {
		if _, err := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "get", "namespace", namespace); err == nil {
			return fmt.Errorf("restore target namespace %s already exists", namespace)
		}
	}
	return nil
}

func (a *App) applyRestore(ctx context.Context, cfg *InstallConfig, stage string) error {
	kube := []string{"--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.System}
	passwordEncoded, err := a.runner.quiet(ctx, "kubectl", append(kube, "get", "secret", "argus-data-credentials", "-o", "jsonpath={.data.postgresql-password}")...)
	if err != nil {
		return err
	}
	password, err := base64.StdEncoding.DecodeString(strings.TrimSpace(passwordEncoded))
	if err != nil {
		return err
	}
	dump, err := os.Open(filepath.Join(stage, "postgres.dump"))
	if err != nil {
		return err
	}
	defer dump.Close()
	if _, err := a.runner.quietInput(ctx, dump, "kubectl", append(kube, "exec", "-i", "statefulset/argus-postgresql", "--", "env", "PGPASSWORD="+string(password), "pg_restore", "-U", "argus", "-d", "argus", "--clean", "--if-exists", "--no-owner")...); err != nil {
		return err
	}
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "cp", filepath.Join(stage, "openbao.snap"), "argus-openbao-0:/tmp/argus-m8-restore.snap")...); err != nil {
		return err
	}
	openBaoCommand := `state=/openbao/data/argus-bootstrap.json; root=$(sed -n 's/.*"root_token"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$state"); BAO_ADDR=http://127.0.0.1:8200 BAO_TOKEN="$root" bao operator raft snapshot restore -force /tmp/argus-m8-restore.snap`
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "exec", "statefulset/argus-openbao", "--", "sh", "-ec", openBaoCommand)...); err != nil {
		return err
	}
	if err := a.restorePodArchive(ctx, kube, "argus-minio-0", filepath.Join(stage, "minio.tgz"), "/data"); err != nil {
		return err
	}
	observability := []string{"--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.Observability}
	pod, err := a.runner.quiet(ctx, "kubectl", append(observability, "get", "pods", "-l", "clickhouse.altinity.com/chi=argus-clickhouse", "-o", "jsonpath={.items[0].metadata.name}")...)
	if err != nil || strings.TrimSpace(pod) == "" {
		return fmt.Errorf("locate ClickHouse restore pod: %w", err)
	}
	if err := a.restorePodArchive(ctx, observability, strings.TrimSpace(pod), filepath.Join(stage, "clickhouse.tgz"), "/var/lib/clickhouse"); err != nil {
		return err
	}
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "rollout", "restart", "statefulset/argus-openbao")...); err != nil {
		return err
	}
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "rollout", "status", "statefulset/argus-openbao", "--timeout=5m")...); err != nil {
		return fmt.Errorf("wait for restored OpenBao: %w", err)
	}
	if _, err := a.runner.quiet(ctx, "kubectl", append(observability, "delete", "pod", strings.TrimSpace(pod), "--wait=true", "--timeout=5m")...); err != nil {
		return fmt.Errorf("restart restored ClickHouse pod: %w", err)
	}
	if _, err := a.runner.quiet(ctx, "kubectl", append(observability, "wait", "pod", "-l", "clickhouse.altinity.com/chi=argus-clickhouse", "--for=condition=Ready", "--timeout=10m")...); err != nil {
		return fmt.Errorf("wait for restored ClickHouse: %w", err)
	}
	return a.verify(ctx, cfg, "text", "")
}

func (a *App) restorePodArchive(ctx context.Context, kube []string, pod, source, target string) error {
	remote := "/tmp/argus-m8-restore.tgz"
	if _, err := a.runner.quiet(ctx, "kubectl", append(kube, "cp", source, pod+":"+remote)...); err != nil {
		return err
	}
	_, err := a.runner.quiet(ctx, "kubectl", append(kube, "exec", pod, "--", "tar", "-xzf", remote, "-C", target)...)
	return err
}

func (a *App) runUpgrade(ctx context.Context, operation string, args []string) error {
	flags := flag.NewFlagSet("upgrade "+operation, flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	configPath := flags.String("config", "deploy/profiles/local-hardening.yaml", "ArgusInstallConfig file")
	artifacts := flags.String("artifacts", "artifacts/m8-upgrade", "upgrade state directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		return err
	}
	if cfg.Spec.Profile != "local-hardening" {
		return errors.New("local upgrade commands require the local-hardening profile")
	}
	statePath := filepath.Join(*artifacts, cfg.Spec.ReleaseID+".json")
	digest, err := fileSHA256(cfg.path)
	if err != nil {
		return err
	}
	switch operation {
	case "plan":
		state := upgradeState{ReleaseID: cfg.Spec.ReleaseID, ConfigDigest: digest, Stage: "planned", UpdatedAt: time.Now().UTC(), Message: "PostgreSQL -> ClickHouse -> Catalog -> workloads"}
		if err := writeUpgradeState(statePath, state); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(a.stdout, state.Message)
		return nil
	case "status":
		state, err := readUpgradeState(statePath)
		if err != nil {
			return err
		}
		return json.NewEncoder(a.stdout).Encode(state)
	case "apply":
		state, err := readUpgradeState(statePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if state.ConfigDigest != "" && state.ConfigDigest != digest {
			return errors.New("upgrade config changed after plan; create a new plan")
		}
		state = upgradeState{ReleaseID: cfg.Spec.ReleaseID, ConfigDigest: digest, Stage: "schema_applied", UpdatedAt: time.Now().UTC(), Message: "schema advancement is non-destructive and cannot be rolled back"}
		if err := writeUpgradeState(statePath, state); err != nil {
			return err
		}
		if err := a.install(ctx, cfg); err != nil {
			state.Stage, state.Message, state.UpdatedAt = "interrupted", err.Error(), time.Now().UTC()
			_ = writeUpgradeState(statePath, state)
			return err
		}
		state.Stage, state.Message, state.UpdatedAt = "complete", "upgrade completed", time.Now().UTC()
		return writeUpgradeState(statePath, state)
	case "rollback":
		state, err := readUpgradeState(statePath)
		if err != nil {
			return err
		}
		if state.Stage != "planned" {
			return errors.New("destructive schema rollback is forbidden after upgrade apply starts")
		}
		state.Stage, state.Message, state.UpdatedAt = "rolled_back", "planned upgrade cancelled before schema changes", time.Now().UTC()
		return writeUpgradeState(statePath, state)
	default:
		return fmt.Errorf("unsupported upgrade operation %q", operation)
	}
}

func writeUpgradeState(path string, state upgradeState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, append(data, '\n'))
}

func readUpgradeState(path string) (upgradeState, error) {
	var state upgradeState
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func encryptDirectory(source, target string, key []byte) error {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	prefix := make([]byte, 8)
	if _, err := rand.Read(prefix); err != nil {
		return err
	}
	if _, err := file.Write(append([]byte(backupMagic), prefix...)); err != nil {
		return err
	}
	pipeReader, pipeWriter := io.Pipe()
	go func() { pipeWriter.CloseWithError(writeTarGzip(pipeWriter, source)) }()
	buffer := make([]byte, backupChunkSize)
	var counter uint32
	for {
		count, readErr := io.ReadFull(pipeReader, buffer)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		if count > 0 {
			nonce := append(append([]byte{}, prefix...), byte(counter>>24), byte(counter>>16), byte(counter>>8), byte(counter))
			sealed := gcm.Seal(nil, nonce, buffer[:count], []byte(backupMagic))
			if err := binary.Write(file, binary.BigEndian, uint32(len(sealed))); err != nil {
				return err
			}
			if _, err := file.Write(sealed); err != nil {
				return err
			}
			counter++
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	return file.Sync()
}

func openBackup(backupPath, keyPath string) (string, backupManifest, error) {
	var manifest backupManifest
	if keyPath == "" {
		keyPath = backupPath + ".key"
	}
	encoded, err := os.ReadFile(keyPath)
	if err != nil {
		return "", manifest, err
	}
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil || len(key) != 32 {
		return "", manifest, errors.New("backup recovery key is invalid")
	}
	stage, err := os.MkdirTemp("", "argus-m8-restore-")
	if err != nil {
		return "", manifest, err
	}
	if err := os.Chmod(stage, 0o700); err != nil {
		os.RemoveAll(stage)
		return "", manifest, err
	}
	if err := decryptArchive(backupPath, stage, key); err != nil {
		os.RemoveAll(stage)
		return "", manifest, err
	}
	data, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		os.RemoveAll(stage)
		return "", manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.FormatVersion != 1 {
		os.RemoveAll(stage)
		return "", manifest, errors.New("backup manifest is invalid or unsupported")
	}
	return stage, manifest, nil
}

func decryptArchive(source, target string, key []byte) error {
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	header := make([]byte, len(backupMagic)+8)
	if _, err := io.ReadFull(file, header); err != nil || string(header[:len(backupMagic)]) != backupMagic {
		return errors.New("backup header is invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	pipeReader, pipeWriter := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- extractBackupTarGzip(pipeReader, target) }()
	prefix := header[len(backupMagic):]
	var counter uint32
	for {
		var length uint32
		if err := binary.Read(file, binary.BigEndian, &length); err != nil {
			if err == io.EOF {
				break
			}
			pipeWriter.CloseWithError(err)
			return err
		}
		if length > backupChunkSize+uint32(gcm.Overhead()) {
			return errors.New("backup chunk is too large")
		}
		sealed := make([]byte, length)
		if _, err := io.ReadFull(file, sealed); err != nil {
			return err
		}
		nonce := append(append([]byte{}, prefix...), byte(counter>>24), byte(counter>>16), byte(counter>>8), byte(counter))
		plaintext, err := gcm.Open(nil, nonce, sealed, []byte(backupMagic))
		if err != nil {
			return errors.New("backup authentication failed")
		}
		if _, err := pipeWriter.Write(plaintext); err != nil {
			return err
		}
		counter++
	}
	if err := pipeWriter.Close(); err != nil {
		return err
	}
	return <-done
}

func writeTarGzip(writer io.Writer, source string) error {
	gzipWriter := gzip.NewWriter(writer)
	tarWriter := tar.NewWriter(gzipWriter)
	err := filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name, err := filepath.Rel(source, path)
		if err != nil || name == "." {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(name)
		if err := tarWriter.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if closeErr := tarWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gzipWriter.Close(); err == nil {
		err = closeErr
	}
	return err
}

func extractBackupTarGzip(reader io.Reader, target string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(header.Name)
		if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return errors.New("backup archive contains an unsafe path")
		}
		path := filepath.Join(target, clean)
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, tarReader)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
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

func writePrivate(path string, data []byte) error {
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
