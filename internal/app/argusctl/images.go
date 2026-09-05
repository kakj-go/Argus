package argusctl

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const localRegistryImage = "registry:3.0.0"

func (a *App) imagesBuild(ctx context.Context, cfg *InstallConfig, platform string) error {
	if platform != "linux/arm64" && platform != "linux/amd64" && platform != "linux/arm64,linux/amd64" {
		return fmt.Errorf("unsupported platform %q", platform)
	}
	root, err := findRepoRoot(filepath.Dir(cfg.path))
	if err != nil {
		return err
	}
	if err := a.buildConnectorDistributionArtifacts(ctx, root); err != nil {
		return err
	}
	if cfg.Spec.Images.Mode == "local-registry" && !strings.Contains(platform, ",") {
		if err := a.ensureRegistry(ctx, cfg); err != nil {
			return err
		}
	}
	localRegistryPush := cfg.Spec.Images.Mode == "local-registry" && !strings.Contains(platform, ",")
	builder := ""
	if !localRegistryPush {
		builder = buildxBuilderName(cfg)
		if err := a.ensureBuildxBuilder(ctx, builder); err != nil {
			return err
		}
	}
	artifacts := filepath.Join(root, "artifacts", "images", cfg.Spec.ReleaseID)
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return err
	}
	builds := []struct {
		name       string
		dockerfile string
	}{
		{"argus-backend", "deploy/docker/backend.Dockerfile"},
		{"argus-web", "deploy/docker/web.Dockerfile"},
		{"minio", "deploy/docker/minio.Dockerfile"},
	}
	for _, build := range builds {
		image := cfg.Image(build.name)
		if cfg.Spec.Images.Mode == "local-registry" {
			image = localRegistryReference(cfg, build.name)
		}
		args := []string{"buildx", "build"}
		if builder != "" {
			args = append(args, "--builder", builder)
		}
		args = append(args, "--platform", platform, "--file", filepath.Join(root, build.dockerfile), "--tag", image)
		if localRegistryPush {
			args = append(args, "--push")
		} else if strings.Contains(platform, ",") {
			args = append(args, "--push")
		} else {
			args = append(args, "--output", "type=oci,dest="+filepath.Join(artifacts, build.name+"-"+strings.ReplaceAll(platform, "/", "-")+".tar"))
		}
		args = append(args, root)
		_, _ = fmt.Fprintf(a.stdout, "Building %s for %s\n", build.name, platform)
		if _, err := a.runner.run(ctx, nil, "docker", args...); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) ensureRegistry(ctx context.Context, cfg *InstallConfig) error {
	if cfg.Spec.Images.Mode != "local-registry" {
		return nil
	}
	_, port, err := net.SplitHostPort(cfg.Spec.Images.Registry)
	if err != nil {
		return fmt.Errorf("local registry must use host:port: %w", err)
	}
	containerName := cfg.registryContainerName()
	running, inspectErr := a.runner.quiet(ctx, "docker", "inspect", "--format={{.State.Running}}", containerName)
	if inspectErr == nil {
		if strings.TrimSpace(running) != "true" {
			if _, err := a.runner.run(ctx, nil, "docker", "start", containerName); err != nil {
				return fmt.Errorf("start existing local registry %s: %w", containerName, err)
			}
		}
		return waitForRegistry(ctx, "127.0.0.1:"+port)
	}
	_, err = a.runner.run(ctx, nil, "docker", "run", "--detach", "--restart", "unless-stopped", "--name", containerName, "--label", "argus.io/release-id="+cfg.Spec.ReleaseID, "--publish", port+":5000", localRegistryImage)
	if err != nil {
		return err
	}
	return waitForRegistry(ctx, "127.0.0.1:"+port)
}

func (a *App) ensureBuildxBuilder(ctx context.Context, name string) error {
	if _, err := a.runner.quiet(ctx, "docker", "buildx", "inspect", name); err != nil {
		if _, err := a.runner.run(ctx, nil, "docker", "buildx", "create", "--name", name, "--driver", "docker-container"); err != nil {
			return err
		}
	}
	_, err := a.runner.run(ctx, nil, "docker", "buildx", "inspect", "--bootstrap", name)
	return err
}

func (a *App) imagesLoad(ctx context.Context, cfg *InstallConfig) error {
	if cfg.Spec.Images.Mode != "local-registry" {
		return fmt.Errorf("images load is only valid for local-registry mode")
	}
	if err := a.ensureRegistry(ctx, cfg); err != nil {
		return err
	}
	loaderName := kubernetesName("argus-image-loader-" + cfg.Spec.ReleaseID)
	manifest := imageLoaderManifest(loaderName, cfg)
	if _, err := a.runner.run(ctx, strings.NewReader(manifest), "kubectl", "--context", cfg.Spec.KubeContext, "apply", "--filename", "-"); err != nil {
		return err
	}
	// Tags such as "dev" are intentionally reusable. Restarting the loader
	// reruns its init container so containerd refreshes those tags on every load.
	if _, err := a.runner.run(ctx, nil, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", "kube-system", "rollout", "restart", "daemonset/"+loaderName); err != nil {
		return err
	}
	_, err := a.runner.run(ctx, nil, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", "kube-system", "rollout", "status", "daemonset/"+loaderName, "--timeout=10m")
	return err
}

func (a *App) imagesClean(ctx context.Context, cfg *InstallConfig) error {
	loaderName := kubernetesName("argus-image-loader-" + cfg.Spec.ReleaseID)
	podsOutput, _ := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", "kube-system", "get", "pods", "--selector", "app.kubernetes.io/name="+loaderName, "--output", "name")
	for _, pod := range strings.Fields(podsOutput) {
		for _, image := range []string{cfg.Image("argus-backend"), cfg.Image("argus-web"), cfg.Image("minio")} {
			_, _ = a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", "kube-system", "exec", pod, "--", "/host/ctr", "--address", "/run/containerd/containerd.sock", "--namespace", "k8s.io", "images", "remove", image)
		}
	}
	_, _ = a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", "kube-system", "delete", "daemonset", loaderName, "--ignore-not-found=true", "--wait=true")
	// The registry image declares /var/lib/registry as a volume. Removing the
	// container without --volumes leaves one anonymous volume behind for every
	// E2E run, eventually exhausting the Docker Desktop disk.
	_, _ = a.runner.quiet(ctx, "docker", "container", "rm", "--force", "--volumes", cfg.registryContainerName())
	_, _ = a.runner.quiet(ctx, "docker", "buildx", "rm", "--force", buildxBuilderName(cfg))
	for _, image := range []string{cfg.Image("argus-backend"), cfg.Image("argus-web"), cfg.Image("minio")} {
		_, _ = a.runner.quiet(ctx, "docker", "image", "rm", "--force", image)
	}
	for _, image := range []string{"argus-backend", "argus-web", "minio"} {
		_, _ = a.runner.quiet(ctx, "docker", "image", "rm", "--force", localRegistryReference(cfg, image))
	}
	return nil
}

func buildxBuilderName(cfg *InstallConfig) string {
	return kubernetesName("argus-buildx-" + cfg.Spec.ReleaseID)
}

func localRegistryReference(cfg *InstallConfig, name string) string {
	_, port, err := net.SplitHostPort(cfg.Spec.Images.Registry)
	if err != nil {
		return cfg.Image(name)
	}
	return fmt.Sprintf("localhost:%s/argus/%s:%s", port, name, cfg.Spec.Images.Tag)
}

func imageLoaderManifest(name string, cfg *InstallConfig) string {
	images := []string{cfg.Image("argus-backend"), cfg.Image("argus-web"), cfg.Image("minio")}
	quoted := make([]string, 0, len(images))
	for _, image := range images {
		quoted = append(quoted, fmt.Sprintf("%q", image))
	}
	return fmt.Sprintf(`apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: %s
  namespace: kube-system
  labels:
    app.kubernetes.io/name: %s
    app.kubernetes.io/part-of: argus
    argus.io/release-id: %s
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: %s
  template:
    metadata:
      labels:
        app.kubernetes.io/name: %s
        app.kubernetes.io/part-of: argus
        argus.io/release-id: %s
    spec:
      initContainers:
        - name: load-images
          image: debian:13-slim
          securityContext:
            privileged: true
          command: ["/bin/sh", "-ec"]
          args:
            - |
              for image in %s; do
                /host/ctr --address /run/containerd/containerd.sock --namespace k8s.io images pull --plain-http "$image"
              done
          volumeMounts:
            - {name: ctr, mountPath: /host/ctr, readOnly: true}
            - {name: containerd, mountPath: /run/containerd/containerd.sock}
      containers:
        - name: loader
          image: debian:13-slim
          command: ["/bin/sh", "-c", "sleep 365d"]
          securityContext:
            privileged: true
          volumeMounts:
            - {name: ctr, mountPath: /host/ctr, readOnly: true}
            - {name: containerd, mountPath: /run/containerd/containerd.sock}
      volumes:
        - name: ctr
          hostPath: {path: /usr/local/bin/ctr, type: File}
        - name: containerd
          hostPath: {path: /run/containerd/containerd.sock, type: Socket}
`, name, name, cfg.Spec.ReleaseID, name, name, cfg.Spec.ReleaseID, strings.Join(quoted, " "))
}

var invalidKubernetesName = regexp.MustCompile(`[^a-z0-9-]+`)

func kubernetesName(value string) string {
	value = strings.Trim(invalidKubernetesName.ReplaceAllString(strings.ToLower(value), "-"), "-")
	if len(value) > 63 {
		value = strings.TrimRight(value[:63], "-")
	}
	return value
}

func waitForRegistry(ctx context.Context, address string) error {
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			connection.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return fmt.Errorf("registry %s did not become reachable", address)
}

func bytesReader(value string) *bytes.Reader {
	return bytes.NewReader([]byte(value))
}
