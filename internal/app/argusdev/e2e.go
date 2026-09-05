package argusdev

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type E2EOptions struct {
	Suite        string
	KubeContext  string
	RunID        string
	Artifacts    string
	UnitOnly     bool
	Argusctl     string
	BaselinesRun bool
}

type E2EEnvironment struct {
	Options            E2EOptions
	Root               string
	WorkDir            string
	ConfigPath         string
	Profile            string
	ReleaseID          string
	SystemNS           string
	SandboxNS          string
	ObservNS           string
	ImageTag           string
	ImagePlatform      string
	Argusctl           string
	Kube               *E2EKube
	Endpoints          *E2EEndpoints
	Processes          []*Process
	ManagedNamespaces  []string
	ManagedClusterRBAC []string
	State              *ScenarioState
	CollectorArtifacts *E2ECollectorArtifacts
	ConnectorArtifacts *E2EConnectorArtifacts
	ArtifactSigning    *E2EArtifactSigning
	ArtifactTLS        fixtureCertificate
	installed          bool
	imagesAttempted    bool
	installAttempted   bool
	leaseAcquired      bool
	fixtureAttempted   bool
	fixtureReady       bool
}

var suiteDependencies = map[string][]string{
	"m2":        {"m2"},
	"m3":        {"m2", "m3"},
	"m4":        {"m2", "m4"},
	"m5":        {"m2", "m3", "m4", "m5"},
	"m6":        {"m2", "m3", "m6"},
	"m7":        {"m2", "m3", "m4", "m5", "m7"},
	"m10-query": {"m2", "m3", "m4", "m5", "m7", "m10-query"},
	"m8":        {"m6", "m7", "m8"},
	"p4":        {"m2", "p4"},
}

func (a *App) runE2E(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "run" {
		return fmt.Errorf("%w: usage: argus-dev e2e run --suite SUITE [options]", errUsage)
	}
	flags := flag.NewFlagSet("e2e run", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	suite := flags.String("suite", envOr("ARGUS_E2E_SUITE", ""), "E2E suite")
	kubeContext := flags.String("kube-context", envOr("ARGUS_E2E_KUBE_CONTEXT", ""), "Kubernetes context")
	runID := flags.String("run-id", envOr("ARGUS_E2E_RUN_ID", ""), "run identifier")
	artifacts := flags.String("artifacts", envOr("ARGUS_E2E_ARTIFACTS", ""), "artifact directory")
	unitOnly := flags.Bool("unit-only", envBool("ARGUS_E2E_UNIT_ONLY"), "run the suite's non-cluster gate only")
	argusctl := flags.String("argusctl", envOr("ARGUS_E2E_ARGUSCTL", envOr("ARGUSCTL_BIN", "")), "existing argusctl binary")
	if err := flags.Parse(args[1:]); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%w: unexpected E2E arguments", errUsage)
	}
	if _, exists := suiteDependencies[*suite]; !exists {
		return fmt.Errorf("%w: unsupported suite %q", errUsage, *suite)
	}
	if *runID == "" {
		*runID = time.Now().UTC().Format("20060102150405") + fmt.Sprintf("-%d", os.Getpid())
	}
	if *artifacts == "" {
		*artifacts = filepath.Join(a.root, "artifacts", *suite+"-e2e", *runID)
	} else if !filepath.IsAbs(*artifacts) {
		*artifacts = filepath.Join(a.root, *artifacts)
	}
	options := E2EOptions{Suite: *suite, KubeContext: *kubeContext, RunID: *runID, Artifacts: *artifacts, UnitOnly: *unitOnly, Argusctl: *argusctl}
	if *unitOnly {
		return a.runE2EUnitGate(ctx, options)
	}
	return a.runE2ECluster(ctx, options)
}

func (a *App) runE2EUnitGate(ctx context.Context, options E2EOptions) error {
	if options.Suite == "m10-query" {
		for _, target := range []string{"./internal/transport/httpapi", "./internal/telemetry", "./tests/contract"} {
			if err := a.runner.Run(ctx, nil, "go", "test", target); err != nil {
				return err
			}
		}
		return nil
	}
	return a.runner.Run(ctx, nil, "go", "test", "./...")
}

func (a *App) runE2ECluster(ctx context.Context, options E2EOptions) (returnErr error) {
	if strings.TrimSpace(options.KubeContext) == "" {
		contextName, err := a.doctorOutput(ctx, "kubectl", "config", "current-context")
		if err != nil {
			return fmt.Errorf("%w: resolve Kubernetes context: %v", errCapability, err)
		}
		options.KubeContext = contextName
	}
	if report := a.doctorWithOptions(ctx, "e2e", doctorOptions{KubeContext: options.KubeContext, E2ESuite: options.Suite}); !report.Ready {
		_ = writeDoctor(a.stderr, "text", report)
		return fmt.Errorf("%w: doctor e2e found missing requirements", errCapability)
	}
	if options.Suite == "m8" && !options.BaselinesRun {
		for _, baseline := range []string{"m6", "m7"} {
			baselineOptions := options
			baselineOptions.Suite = baseline
			baselineOptions.RunID = options.RunID + "-" + baseline
			baselineOptions.Artifacts = filepath.Join(options.Artifacts, baseline+"-baseline")
			baselineOptions.BaselinesRun = true
			if err := a.runE2ECluster(ctx, baselineOptions); err != nil {
				return fmt.Errorf("M8 %s baseline: %w", baseline, err)
			}
		}
		options.BaselinesRun = true
	}
	if err := os.MkdirAll(options.Artifacts, 0o700); err != nil {
		return err
	}
	workDir, err := os.MkdirTemp("", "argus-dev-e2e-*")
	if err != nil {
		return err
	}
	env := &E2EEnvironment{Options: options, Root: a.root, WorkDir: workDir, State: NewScenarioState(options.RunID)}
	defer func() {
		returnErr = errors.Join(returnErr, a.cleanupE2E(env))
		_ = os.RemoveAll(workDir)
	}()
	if err := a.prepareE2EEnvironment(ctx, env); err != nil {
		return err
	}
	if err := env.Kube.AcquireLease(ctx, "argus-"+options.Suite+"-e2e", options.RunID); err != nil {
		return err
	}
	env.leaseAcquired = true
	if err := a.invokeArgusctl(ctx, env, "preflight", "--config", env.ConfigPath); err != nil {
		return err
	}
	if err := a.invokeArgusctl(ctx, env, "plan", "--config", env.ConfigPath); err != nil {
		return err
	}
	env.imagesAttempted = true
	if err := a.prepareE2EImages(ctx, env); err != nil {
		return err
	}
	env.installAttempted = true
	if err := a.invokeArgusctl(ctx, env, "install", "--config", env.ConfigPath); err != nil {
		return err
	}
	env.installed = true
	env.fixtureAttempted = true
	if err := a.installE2EFixtures(ctx, env); err != nil {
		return err
	}
	env.fixtureReady = true
	if err := a.resolveE2EAccess(ctx, env); err != nil {
		return err
	}
	if err := a.registerE2EConnectorRelease(ctx, env); err != nil {
		return err
	}
	if err := a.runE2EScenarios(ctx, env); err != nil {
		return err
	}
	if err := a.invokeArgusctl(ctx, env, "verify", "--config", env.ConfigPath, "--artifacts", filepath.Join(options.Artifacts, "verify")); err != nil {
		return err
	}
	result := fmt.Sprintf("{\"run_id\":%q,\"suite\":%q,\"status\":\"passed\"}\n", options.RunID, options.Suite)
	return writePrivate(filepath.Join(options.Artifacts, "result.json"), []byte(result))
}

func (a *App) prepareE2EEnvironment(ctx context.Context, env *E2EEnvironment) error {
	if env.Options.KubeContext == "" {
		contextName, err := a.runner.Output(ctx, nil, "kubectl", "config", "current-context")
		if err != nil {
			return err
		}
		env.Options.KubeContext = contextName
	}
	kube, err := NewE2EKube(env.Options.KubeContext, env.Options.Artifacts)
	if err != nil {
		return err
	}
	env.Kube = kube
	architecture, err := kube.NodeArchitecture(ctx)
	if err != nil {
		return err
	}
	if architecture != "arm64" && architecture != "amd64" {
		return fmt.Errorf("unsupported Kubernetes node architecture %s", architecture)
	}
	env.ImagePlatform = "linux/" + architecture
	release := releaseIDForDev(env.Options.Suite + "-" + env.Options.RunID)
	env.ReleaseID = release
	env.SystemNS = kubernetesNameForDev(release + "-system")
	env.SandboxNS = kubernetesNameForDev(release + "-sandbox")
	env.ObservNS = kubernetesNameForDev(release + "-observability")
	env.ImageTag = kubernetesNameForDev("e2e-" + env.Options.RunID)
	if err := a.prepareE2EArtifactServer(env); err != nil {
		return err
	}
	if err := a.prepareE2ECollectorArtifacts(ctx, env); err != nil {
		return err
	}
	if err := a.prepareE2EConnectorArtifacts(ctx, env); err != nil {
		return err
	}
	env.clearArtifactSigningPrivateKey()
	profile := "evaluation"
	if env.Options.Suite == "m8" {
		profile = "local-hardening"
	}
	configPath, err := a.writeE2EConfig(env, profile)
	if err != nil {
		return err
	}
	env.ConfigPath = configPath
	if env.Options.Argusctl != "" {
		env.Argusctl = env.Options.Argusctl
		return nil
	}
	binary := filepath.Join(env.WorkDir, "argusctl")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := a.runner.Run(ctx, nil, "go", "build", "-trimpath", "-o", binary, "./cmd/argusctl"); err != nil {
		return err
	}
	env.Argusctl = binary
	return nil
}

func (a *App) writeE2EConfig(env *E2EEnvironment, profile string) (string, error) {
	source := filepath.Join(a.root, "deploy", "profiles", profile+".yaml")
	data, err := os.ReadFile(source)
	if err != nil {
		return "", err
	}
	var document map[string]any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return "", err
	}
	spec := nestedMap(document, "spec")
	env.Profile = profile
	document["metadata"] = map[string]any{"name": env.ReleaseID}
	spec["profile"] = profile
	spec["kubeContext"] = env.Options.KubeContext
	spec["releaseId"] = env.ReleaseID
	spec["namespaces"] = map[string]any{"system": env.SystemNS, "sandbox": env.SandboxNS, "observability": env.ObservNS}
	// Ingress-nginx rejects duplicate host/path pairs across namespaces. Give
	// every E2E release its own DNS suffix so a suite can coexist with a local
	// Argus installation (and with another suite) without mutating either one.
	exposure := nestedMap(spec, "exposure")
	e2eDomain := env.ReleaseID + ".argus.test"
	exposure["enterpriseHost"] = "enterprise." + e2eDomain
	exposure["platformHost"] = "platform." + e2eDomain
	exposure["connectorHost"] = "connector." + e2eDomain
	images := nestedMap(spec, "images")
	images["tag"] = env.ImageTag
	images["pullPolicy"] = "Never"
	if artifacts := env.CollectorArtifacts; artifacts != nil {
		spec["telemetry"] = map[string]any{
			"collectorVersion": artifacts.Version,
			"linuxArm64Uri":    artifacts.LinuxURI, "linuxArm64Sha256": artifacts.LinuxSHA256,
			"linuxArm64Signature": artifacts.LinuxSignature, "linuxArm64ByteSize": artifacts.LinuxByteSize,
			"linuxAmd64Uri": artifacts.LinuxAMD64URI, "linuxAmd64Sha256": artifacts.LinuxAMD64SHA256,
			"linuxAmd64Signature": artifacts.LinuxAMD64Signature, "linuxAmd64ByteSize": artifacts.LinuxAMD64ByteSize,
			"windowsAmd64Uri": artifacts.WindowsURI, "windowsAmd64Sha256": artifacts.WindowsSHA256,
			"windowsAmd64Signature": artifacts.WindowsSignature, "windowsAmd64ByteSize": artifacts.WindowsByteSize,
			"signingKeyId": artifacts.SigningKeyID, "signingPublicKey": artifacts.SigningPublicKey,
		}
	}
	if profile == "local-hardening" {
		security := nestedMap(spec, "security")
		security["platformMfaRequired"] = true
	}
	encoded, err := yaml.Marshal(document)
	if err != nil {
		return "", err
	}
	path := filepath.Join(env.Options.Artifacts, "install-config.yaml")
	if err := writePrivate(path, encoded); err != nil {
		return "", err
	}
	return path, nil
}

func nestedMap(parent map[string]any, key string) map[string]any {
	if value, ok := parent[key].(map[string]any); ok {
		return value
	}
	value := map[string]any{}
	parent[key] = value
	return value
}

func (a *App) invokeArgusctl(ctx context.Context, env *E2EEnvironment, args ...string) error {
	return a.runner.Run(ctx, nil, env.Argusctl, args...)
}

func (a *App) cleanupE2E(env *E2EEnvironment) error {
	var cleanupErrors []error
	record := func(operation string, err error) {
		if err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("%s: %w", operation, err))
		}
	}
	for index := len(env.Processes) - 1; index >= 0; index-- {
		record("stop local process", env.Processes[index].Stop(5*time.Second))
	}
	if env.Kube != nil {
		diagnosticCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		record("collect redacted diagnostics", env.Kube.CollectDiagnostics(diagnosticCtx, env, filepath.Join(env.Options.Artifacts, "diagnostics")))
		cancel()
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	if env.Kube != nil && env.fixtureAttempted {
		record("clean E2E fixtures", a.cleanupE2EFixtures(cleanupCtx, env))
	} else if env.imagesAttempted {
		record("clean local E2E fixture images", a.removeLocalFixtureImages(cleanupCtx, env.State.FixtureImages))
	}
	// The local registry may be owned by this E2E release and removed by
	// argusctl cleanup, so delete the exact run tags while it is reachable.
	if env.imagesAttempted && env.ConfigPath != "" {
		record("delete remote E2E image tags", a.removeRemoteE2EImages(cleanupCtx, env))
	}
	if env.installAttempted && env.Argusctl != "" && env.ConfigPath != "" {
		record("uninstall Argus", a.invokeArgusctl(cleanupCtx, env, "uninstall", "--config", env.ConfigPath, "--delete-data", "--delete-owned-crds", "--yes"))
	} else if env.imagesAttempted && env.Argusctl != "" && env.ConfigPath != "" {
		record("clean E2E images", a.invokeArgusctl(cleanupCtx, env, "images", "clean", "--config", env.ConfigPath))
	}
	if env.Kube != nil {
		record("delete managed Collector RBAC", cleanupManagedCollectorRBAC(cleanupCtx, env))
		for _, namespace := range env.ManagedNamespaces {
			if namespace != "" {
				record("delete namespace "+namespace, env.Kube.DeleteNamespace(cleanupCtx, namespace))
			}
		}
		for _, namespace := range []string{env.SystemNS, env.SandboxNS, env.ObservNS} {
			if namespace != "" {
				record("delete namespace "+namespace, env.Kube.DeleteNamespace(cleanupCtx, namespace))
			}
		}
		for _, name := range env.ManagedClusterRBAC {
			err := env.Kube.Client.RbacV1().ClusterRoleBindings().Delete(cleanupCtx, name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				record("delete ClusterRoleBinding "+name, err)
			}
			err = env.Kube.Client.RbacV1().ClusterRoles().Delete(cleanupCtx, name, metav1.DeleteOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				record("delete ClusterRole "+name, err)
			}
		}
		if env.leaseAcquired {
			record("release E2E Lease", env.Kube.ReleaseLease(cleanupCtx, "argus-"+env.Options.Suite+"-e2e"))
		}
	}
	return errors.Join(cleanupErrors...)
}

func cleanupManagedCollectorRBAC(ctx context.Context, env *E2EEnvironment) error {
	managedNamespaces := make(map[string]struct{}, len(env.ManagedNamespaces))
	for _, namespace := range env.ManagedNamespaces {
		if namespace != "" {
			managedNamespaces[namespace] = struct{}{}
		}
	}
	if len(managedNamespaces) == 0 {
		return nil
	}
	bindings, err := env.Kube.Client.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{LabelSelector: "app.kubernetes.io/part-of=argus"})
	if err != nil {
		return err
	}
	var cleanupErrors []error
	for _, binding := range bindings.Items {
		if !strings.HasPrefix(binding.Name, "argus-otelcol-") {
			continue
		}
		owned := false
		for _, subject := range binding.Subjects {
			if _, exists := managedNamespaces[subject.Namespace]; exists {
				owned = true
				break
			}
		}
		if !owned {
			continue
		}
		if err := env.Kube.Client.RbacV1().ClusterRoleBindings().Delete(ctx, binding.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("delete ClusterRoleBinding %s: %w", binding.Name, err))
		}
		if err := env.Kube.Client.RbacV1().ClusterRoles().Delete(ctx, binding.RoleRef.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("delete ClusterRole %s: %w", binding.RoleRef.Name, err))
		}
	}
	return errors.Join(cleanupErrors...)
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func kubernetesNameForDev(value string) string {
	return boundedKubernetesNameForDev(value, 63)
}

func releaseIDForDev(value string) string {
	return boundedKubernetesNameForDev(value, 34)
}

func boundedKubernetesNameForDev(value string, maxLength int) string {
	value = strings.ToLower(value)
	var result strings.Builder
	for _, character := range value {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			result.WriteRune(character)
		} else {
			result.WriteByte('-')
		}
	}
	name := strings.Trim(result.String(), "-")
	if len(name) > maxLength {
		digest := sha256.Sum256([]byte(name))
		suffix := hex.EncodeToString(digest[:])[:8]
		prefixLength := maxLength - len(suffix) - 1
		prefix := strings.TrimRight(name[:prefixLength], "-")
		name = prefix + "-" + suffix
	}
	return name
}

func openArtifact(path string) (io.WriteCloser, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
}
