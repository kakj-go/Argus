package argusdev

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

const e2eInstallMinimumDiskBytes = uint64(25 * 1024 * 1024 * 1024)

var e2eImageBuildRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second}

type ScenarioState struct {
	RunID         string
	FixtureImages map[string]string
	Values        map[string]string
	HTTP          *ScenarioHTTP
}

func NewScenarioState(runID string) *ScenarioState {
	return &ScenarioState{RunID: runID, FixtureImages: map[string]string{}, Values: map[string]string{}}
}

func (a *App) prepareE2EImages(ctx context.Context, env *E2EEnvironment) error {
	if err := a.retryE2EImageBuild(ctx, "Argus images", func(buildCtx context.Context) error {
		return a.invokeArgusctl(buildCtx, env, "images", "build", "--config", env.ConfigPath, "--platform", env.ImagePlatform)
	}); err != nil {
		return err
	}
	registry, err := e2eRegistry(env.ConfigPath)
	if err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(registry)
	if err != nil {
		return err
	}
	localPrefix := "localhost:" + port + "/argus/"
	clusterPrefix := registry + "/argus/"
	env.State.FixtureImages["backend"] = clusterPrefix + "argus-backend:" + env.ImageTag
	env.State.FixtureImages["web"] = clusterPrefix + "argus-web:" + env.ImageTag
	env.State.FixtureImages["minio"] = clusterPrefix + "minio:" + env.ImageTag
	buildLabel := "io.argus.e2e.run=" + env.ImageTag
	backendArgs := []string{"buildx", "build", "--platform", env.ImagePlatform, "--file", "deploy/docker/backend.Dockerfile", "--tag", localPrefix + "argus-backend:" + env.ImageTag, "--label", buildLabel, "--push"}
	if suiteHas(env.Options.Suite, "m4") || suiteHas(env.Options.Suite, "m5") || suiteHas(env.Options.Suite, "m7") || env.Options.Suite == "m10-query" {
		backendArgs = append(backendArgs, "--build-arg", "GO_BUILD_TAGS=m4e2e")
	}
	backendArgs = append(backendArgs, ".")
	if err := a.retryE2EImageBuild(ctx, "E2E backend image", func(buildCtx context.Context) error {
		return a.runner.Run(buildCtx, nil, "docker", backendArgs...)
	}); err != nil {
		return err
	}
	if err := a.retryE2EImageBuild(ctx, "E2E web image", func(buildCtx context.Context) error {
		return a.runner.Run(buildCtx, nil, "docker", "buildx", "build", "--platform", env.ImagePlatform, "--file", "deploy/docker/web.Dockerfile",
			"--tag", localPrefix+"argus-web:"+env.ImageTag, "--label", buildLabel, "--push",
			"--build-arg", "VITE_API_MODE=real", "--build-arg", "VITE_API_BASE_URL=/",
			"--build-arg", "VITE_DIRECT_EGRESS_ADDRESSES=127.0.0.1", ".")
	}); err != nil {
		return err
	}
	if err := a.retryE2EImageBuild(ctx, "E2E MinIO image", func(buildCtx context.Context) error {
		return a.runner.Run(buildCtx, nil, "docker", "buildx", "build", "--platform", env.ImagePlatform, "--file", "deploy/docker/minio.Dockerfile",
			"--tag", localPrefix+"minio:"+env.ImageTag, "--label", buildLabel, "--push", ".")
	}); err != nil {
		return err
	}
	features := suiteFixtureFeatures(env.Options.Suite)
	if features.SSH || features.WinRS {
		if err := a.buildFixtureImage(ctx, env, "ssh", "deploy/docker/e2e-ssh.Dockerfile", localPrefix, clusterPrefix); err != nil {
			return err
		}
		env.State.FixtureImages["winrs"] = env.State.FixtureImages["ssh"]
	}
	if features.Replay {
		if err := a.buildFixtureImage(ctx, env, "replay", "deploy/docker/replay-model.Dockerfile", localPrefix, clusterPrefix); err != nil {
			return err
		}
	}
	if features.Artifact {
		env.State.FixtureImages["otelcol"] = clusterPrefix + "argus-otelcol:" + env.ImageTag
		if err := a.retryE2EImageBuild(ctx, "E2E OpenTelemetry Collector image", func(buildCtx context.Context) error {
			return a.runner.Run(buildCtx, nil, "docker", "buildx", "build", "--platform", "linux/arm64", "--file", "deploy/docker/otelcol.Dockerfile",
				"--tag", localPrefix+"argus-otelcol:"+env.ImageTag, "--label", buildLabel, "--push", ".")
		}); err != nil {
			return err
		}
		if err := a.buildFixtureImage(ctx, env, "artifact", "deploy/docker/e2e-artifact-server.Dockerfile", localPrefix, clusterPrefix); err != nil {
			return err
		}
		if err := a.buildFixtureImage(ctx, env, "systemd", "deploy/docker/e2e-systemd-host.Dockerfile", localPrefix, clusterPrefix); err != nil {
			return err
		}
	}
	if err := a.invokeArgusctl(ctx, env, "images", "load", "--config", env.ConfigPath); err != nil {
		return err
	}
	return a.ensureE2EInstallDisk(ctx)
}

func (a *App) ensureE2EInstallDisk(ctx context.Context) error {
	free, pruned, err := reclaimE2EInstallDisk(
		ctx,
		a.root,
		availableDiskBytes,
		func(pruneCtx context.Context) error {
			return a.runner.Run(pruneCtx, nil, "docker", "builder", "prune", "--all", "--force")
		},
	)
	if err != nil {
		return err
	}
	if pruned {
		_, _ = fmt.Fprintf(a.stdout, "Reclaimed BuildKit cache before E2E install; %.1fGi free\n", float64(free)/float64(1<<30))
	}
	return nil
}

func reclaimE2EInstallDisk(
	ctx context.Context,
	root string,
	probe func(string) (uint64, error),
	prune func(context.Context) error,
) (uint64, bool, error) {
	free, err := probe(root)
	if err != nil {
		return 0, false, fmt.Errorf("%w: inspect E2E install disk: %v", errCapability, err)
	}
	if free >= e2eInstallMinimumDiskBytes {
		return free, false, nil
	}
	if err := prune(ctx); err != nil {
		return free, false, fmt.Errorf("%w: reclaim BuildKit cache before E2E install: %v", errCapability, err)
	}
	free, err = probe(root)
	if err != nil {
		return 0, true, fmt.Errorf("%w: inspect E2E install disk after BuildKit cleanup: %v", errCapability, err)
	}
	if free < e2eInstallMinimumDiskBytes {
		return free, true, fmt.Errorf(
			"%w: only %.1fGi free after BuildKit cleanup; at least 25Gi is required for E2E install",
			errCapability,
			float64(free)/float64(1<<30),
		)
	}
	return free, true, nil
}

func (a *App) buildFixtureImage(ctx context.Context, env *E2EEnvironment, name, dockerfile, localPrefix, clusterPrefix string) error {
	imageName := "argus-e2e-" + name
	local := localPrefix + imageName + ":" + env.ImageTag
	cluster := clusterPrefix + imageName + ":" + env.ImageTag
	if err := a.retryE2EImageBuild(ctx, "E2E fixture image "+name, func(buildCtx context.Context) error {
		return a.runner.Run(buildCtx, nil, "docker", "buildx", "build", "--platform", env.ImagePlatform, "--file", dockerfile, "--tag", local,
			"--label", "io.argus.e2e.run="+env.ImageTag, "--push", ".")
	}); err != nil {
		return err
	}
	env.State.FixtureImages[name] = cluster
	return nil
}

func (a *App) retryE2EImageBuild(ctx context.Context, description string, operation func(context.Context) error) error {
	return retryImageBuild(ctx, e2eImageBuildRetryDelays, operation, func(attempt int, err error, delay time.Duration) {
		_, _ = fmt.Fprintf(a.stdout, "%s failed on attempt %d; retrying in %s: %v\n", description, attempt, delay, err)
	})
}

func retryImageBuild(
	ctx context.Context,
	delays []time.Duration,
	operation func(context.Context) error,
	onRetry func(attempt int, err error, delay time.Duration),
) error {
	totalAttempts := len(delays) + 1
	for attempt := 1; attempt <= totalAttempts; attempt++ {
		err := operation(ctx)
		if err == nil {
			return nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if errors.Is(err, errCapability) {
			return err
		}
		if attempt == totalAttempts {
			return fmt.Errorf("image build failed after %d attempts: %w", totalAttempts, err)
		}

		delay := delays[attempt-1]
		if onRetry != nil {
			onRetry(attempt, err, delay)
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func e2eRegistry(configPath string) (string, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return "", err
	}
	var document struct {
		Spec struct {
			Images struct {
				Registry string `json:"registry"`
			} `json:"images"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(data, &document); err != nil {
		return "", err
	}
	if strings.TrimSpace(document.Spec.Images.Registry) == "" {
		return "", fmt.Errorf("E2E install config has no image registry")
	}
	return document.Spec.Images.Registry, nil
}

func suiteHas(suite, dependency string) bool {
	for _, item := range suiteDependencies[suite] {
		if item == dependency {
			return true
		}
	}
	return false
}
