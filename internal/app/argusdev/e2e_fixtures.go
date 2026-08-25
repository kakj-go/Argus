package argusdev

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
)

type fixtureFeatures struct {
	SSH, Replay, WinRS, Artifact bool
}

func suiteFixtureFeatures(suite string) fixtureFeatures {
	features := fixtureFeatures{}
	for _, dependency := range suiteDependencies[suite] {
		switch dependency {
		case "m3":
			features.SSH = true
		case "m4":
			features.Replay = true
		case "m6":
			features.SSH = true
			features.WinRS = true
		case "m7", "m10-query":
			features.SSH = true
			features.Replay = true
			features.Artifact = true
		}
	}
	return features
}

func (a *App) installE2EFixtures(ctx context.Context, env *E2EEnvironment) error {
	features := suiteFixtureFeatures(env.Options.Suite)
	if !features.SSH && !features.Replay && !features.WinRS && !features.Artifact {
		return nil
	}
	chart, err := loader.Load(filepath.Join(a.root, "tests", "e2e", "helm", "argus-e2e-fixtures"))
	if err != nil {
		return err
	}
	settings := cli.New()
	settings.KubeContext = env.Options.KubeContext
	configuration := action.NewConfiguration()
	if err := configuration.Init(settings.RESTClientGetter(), env.SystemNS, "secret"); err != nil {
		return err
	}
	configuration.KubeClient = kube.New(settings.RESTClientGetter())
	install := action.NewInstall(configuration)
	install.ReleaseName = env.ReleaseID + "-e2e-fixtures"
	install.Namespace = env.SystemNS
	install.WaitStrategy = kube.StatusWatcherStrategy
	install.WaitForJobs = true
	install.Timeout = 10 * time.Minute
	install.CreateNamespace = false
	images := env.State.FixtureImages
	replayTLS, err := generateFixtureCertificate("argus-replay-model", []string{
		"argus-replay-model", "argus-replay-model." + env.SandboxNS + ".svc",
	}, nil)
	if err != nil {
		return err
	}
	artifactTLS := fixtureCertificate{}
	if env.CollectorArtifacts != nil {
		artifactTLS = env.CollectorArtifacts.TLS
	}
	winrsTLS, err := generateFixtureCertificate("8.8.8.8", nil, []net.IP{net.ParseIP("8.8.8.8")})
	if err != nil {
		return err
	}
	values := map[string]any{
		"releaseId":  env.ReleaseID,
		"namespaces": map[string]any{"system": env.SystemNS, "sandbox": env.SandboxNS, "observability": env.ObservNS},
		"images": map[string]any{
			"pullPolicy": "Never", "sshTarget": images["ssh"], "replayModel": images["replay"],
			"winrsTarget": images["winrs"], "artifactServer": images["artifact"],
		},
		"features": map[string]any{"sshTarget": features.SSH, "replayModel": features.Replay, "winrsTarget": features.WinRS, "artifactServer": features.Artifact},
		"tls": map[string]any{
			"replay":   map[string]any{"ca": replayTLS.CA, "certificate": replayTLS.Certificate, "privateKey": replayTLS.PrivateKey},
			"winrs":    map[string]any{"ca": winrsTLS.CA, "certificate": winrsTLS.Certificate, "privateKey": winrsTLS.PrivateKey},
			"artifact": map[string]any{"ca": artifactTLS.CA, "certificate": artifactTLS.Certificate, "privateKey": artifactTLS.PrivateKey},
		},
		"loader": map[string]any{"enabled": len(images) > 0, "images": fixtureImageList(images)},
	}
	if _, err = install.RunWithContext(ctx, chart, values); err != nil {
		return err
	}
	if err := a.patchArtifactTrust(ctx, env, features); err != nil {
		return err
	}
	return a.patchDirectExecutorFixtures(ctx, env, features)
}

func (a *App) patchArtifactTrust(ctx context.Context, env *E2EEnvironment, features fixtureFeatures) error {
	if !features.Artifact {
		return nil
	}
	volume := map[string]any{"name": "e2e-otelcol-artifact-ca", "secret": map[string]any{"secretName": "argus-e2e-artifact-tls"}}
	mount := map[string]any{"name": "e2e-otelcol-artifact-ca", "mountPath": "/var/run/secrets/argus/e2e-otelcol-artifact-ca", "readOnly": true}
	environment := map[string]any{"name": "ARGUS_OTELCOL_ARTIFACT_CA_PATH", "value": "/var/run/secrets/argus/e2e-otelcol-artifact-ca/ca.crt"}
	workloads := append(e2eWorkerDeployments(env.Profile), "argus-direct-executor")
	for _, workload := range workloads {
		patch := map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
			"containers": []any{map[string]any{"name": workload, "env": []any{environment}, "volumeMounts": []any{mount}}},
			"volumes":    []any{volume},
		}}}}
		if err := env.Kube.PatchDeployment(ctx, env.SystemNS, workload, patch); err != nil {
			return err
		}
		if err := env.Kube.WaitDeployment(ctx, env.SystemNS, workload, 5*time.Minute); err != nil {
			return err
		}
	}
	return nil
}

func e2eWorkerDeployments(profile string) []string {
	if profile == "evaluation" {
		return []string{"argus-worker"}
	}
	return []string{
		"argus-worker-agent",
		"argus-worker-action",
		"argus-worker-compaction",
		"argus-worker-sandbox",
	}
}

func (a *App) patchDirectExecutorFixtures(ctx context.Context, env *E2EEnvironment, features fixtureFeatures) error {
	if !features.SSH && !features.WinRS {
		return nil
	}
	containers := []any{}
	volumes := []any{}
	if features.SSH {
		containers = append(containers, map[string]any{
			"name": "argus-e2e-direct-ssh", "image": env.State.FixtureImages["ssh"], "imagePullPolicy": "Never",
			"env":   []any{map[string]any{"name": "ARGUS_E2E_SSH_PASSWORD", "value": "M3-e2e-ssh-password"}},
			"ports": []any{map[string]any{"name": "e2e-direct-ssh", "containerPort": 2222}},
		})
	}
	if features.WinRS {
		containers = append(containers,
			map[string]any{"name": "argus-direct-executor", "env": []any{map[string]any{"name": "SSL_CERT_FILE", "value": "/etc/argus/e2e-winrs/ca.crt"}}, "volumeMounts": []any{map[string]any{"name": "e2e-winrs-tls", "mountPath": "/etc/argus/e2e-winrs", "readOnly": true}}},
			map[string]any{
				"name": "argus-e2e-direct-winrs", "image": env.State.FixtureImages["winrs"], "imagePullPolicy": "Never",
				"command":      []string{"/usr/local/bin/argus-e2e-winrs"},
				"env":          []any{map[string]any{"name": "ARGUS_E2E_WINRS_PASSWORD", "value": "M6-e2e-winrs-password"}},
				"ports":        []any{map[string]any{"name": "e2e-winrs", "containerPort": 5986}},
				"volumeMounts": []any{map[string]any{"name": "e2e-winrs-tls", "mountPath": "/tls", "readOnly": true}},
			})
		volumes = append(volumes, map[string]any{"name": "e2e-winrs-tls", "secret": map[string]any{"secretName": "argus-e2e-winrs-tls"}})
	}
	if features.Artifact {
		containers = append(containers, map[string]any{
			"name": "argus-e2e-systemd-host", "image": env.State.FixtureImages["systemd"], "imagePullPolicy": "Never",
			"securityContext": map[string]any{"privileged": true, "runAsNonRoot": false, "runAsUser": 0},
			"ports":           []any{map[string]any{"name": "e2e-systemd-ssh", "containerPort": 22}},
			"volumeMounts":    []any{map[string]any{"name": "e2e-cgroup", "mountPath": "/sys/fs/cgroup"}},
		})
		volumes = append(volumes, map[string]any{"name": "e2e-cgroup", "hostPath": map[string]any{"path": "/sys/fs/cgroup", "type": "Directory"}})
	}
	patch := map[string]any{"spec": map[string]any{"template": map[string]any{"spec": map[string]any{
		"initContainers": []any{map[string]any{
			"name": "argus-e2e-direct-address", "image": env.State.FixtureImages["ssh"], "imagePullPolicy": "Never",
			"command":         []string{"/bin/sh", "-c", "/sbin/ip addr add 8.8.8.8/32 dev lo && /sbin/ip addr add 10.255.255.1/32 dev lo"},
			"securityContext": map[string]any{"runAsNonRoot": false, "runAsUser": 0, "capabilities": map[string]any{"add": []string{"NET_ADMIN"}}},
		}},
		"containers": containers, "volumes": volumes,
	}}}}
	if err := env.Kube.PatchDeployment(ctx, env.SystemNS, "argus-direct-executor", patch); err != nil {
		return err
	}
	return env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-direct-executor", 5*time.Minute)
}

func (a *App) uninstallE2EFixtures(ctx context.Context, env *E2EEnvironment) error {
	settings := cli.New()
	settings.KubeContext = env.Options.KubeContext
	configuration := action.NewConfiguration()
	if err := configuration.Init(settings.RESTClientGetter(), env.SystemNS, "secret"); err != nil {
		return err
	}
	uninstall := action.NewUninstall(configuration)
	uninstall.WaitStrategy = kube.StatusWatcherStrategy
	uninstall.Timeout = 5 * time.Minute
	uninstall.IgnoreNotFound = true
	_, err := uninstall.Run(env.ReleaseID + "-e2e-fixtures")
	return err
}

func (a *App) cleanupE2EFixtures(ctx context.Context, env *E2EEnvironment) error {
	removeErr := env.Kube.RemoveImages(ctx, "kube-system",
		"app.kubernetes.io/name=argus-e2e-image-loader,argus.io/release-id="+env.ReleaseID,
		"loader", fixtureImagesForCleanup(env.State.FixtureImages))
	uninstallErr := a.uninstallE2EFixtures(ctx, env)
	localErr := a.removeLocalFixtureImages(ctx, env.State.FixtureImages)
	return errors.Join(removeErr, uninstallErr, localErr)
}

func (a *App) removeLocalFixtureImages(ctx context.Context, images map[string]string) error {
	locals := localFixtureImagesForCleanup(images)
	if len(locals) == 0 {
		return nil
	}
	args := append([]string{"image", "rm", "--force"}, locals...)
	return a.runner.Run(ctx, nil, "docker", args...)
}

func fixtureImageList(images map[string]string) []any {
	result := make([]any, 0, len(images))
	for _, name := range []string{"ssh", "replay", "winrs", "artifact", "systemd", "otelcol"} {
		if image := images[name]; image != "" {
			result = append(result, image)
		}
	}
	return result
}

func fixtureImagesForCleanup(images map[string]string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range fixtureImageList(images) {
		image, _ := value.(string)
		if image != "" && !seen[image] {
			seen[image] = true
			result = append(result, image)
		}
	}
	return result
}

func localFixtureImagesForCleanup(images map[string]string) []string {
	values := fixtureImagesForCleanup(images)
	result := make([]string, 0, len(values))
	for _, image := range values {
		registry, remainder, found := strings.Cut(image, "/")
		if !found {
			continue
		}
		_, port, err := net.SplitHostPort(registry)
		if err != nil {
			continue
		}
		result = append(result, "localhost:"+port+"/"+remainder)
	}
	return result
}
