package argusctl

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func (a *App) tunnel(ctx context.Context, cfg *InstallConfig) error {
	if cfg.Spec.Exposure.Mode != "port-forward" {
		return fmt.Errorf("tunnel requires exposure.mode=port-forward")
	}
	_, _ = fmt.Fprintln(a.stdout, "Enterprise http://127.0.0.1:4173")
	_, _ = fmt.Fprintln(a.stdout, "Platform   http://127.0.0.1:4174")
	_, _ = fmt.Fprintln(a.stdout, "Cards      http://127.0.0.1:4176")
	_, err := a.runner.run(ctx, nil, "kubectl", "--context", cfg.Spec.KubeContext, "--namespace", cfg.Spec.Namespaces.System, "port-forward", "service/argus-web", "4173:8080", "4174:8081", "4176:8083")
	return err
}

func (a *App) uninstall(ctx context.Context, cfg *InstallConfig, deleteCRDs bool) error {
	root, err := findRepoRoot(filepath.Dir(cfg.path))
	if err == nil {
		_ = a.collectArtifacts(ctx, cfg, filepath.Join(root, "artifacts", "k8s-e2e", cfg.Spec.ReleaseID, "uninstall"), nil)
	}
	helm := helmManager{contextName: cfg.Spec.KubeContext, cacheDir: filepath.Join(root, "deploy", ".cache", "charts"), log: a.stderr}
	releases := []struct{ name, namespace string }{
		{cfg.Spec.ReleaseID + "-telemetry-pipeline", cfg.Spec.Namespaces.Observability},
		{cfg.Spec.ReleaseID + "-platform", cfg.Spec.Namespaces.System},
		{cfg.Spec.ReleaseID + "-sandbox", cfg.Spec.Namespaces.Sandbox},
		{cfg.upstreamReleaseName("os"), cfg.Spec.Namespaces.Sandbox},
		{cfg.Spec.ReleaseID + "-data", cfg.Spec.Namespaces.System},
		{cfg.Spec.ReleaseID + "-data-operators", cfg.Spec.Namespaces.Observability},
		{cfg.upstreamReleaseName("ch"), cfg.Spec.Namespaces.Observability},
		{cfg.upstreamReleaseName("st"), cfg.Spec.Namespaces.Observability},
	}
	for _, release := range releases {
		if err := helm.uninstall(release.name, release.namespace); err != nil {
			_, _ = fmt.Fprintf(a.stderr, "warning: %v\n", err)
		}
	}
	clients, clientErr := clientsFor(cfg.Spec.KubeContext)
	if clientErr == nil {
		if err := deleteTelemetryRootCASecret(ctx, clients.typed, cfg.Spec.ReleaseID); err != nil {
			return err
		}
		for _, namespace := range []string{cfg.Spec.Namespaces.System, cfg.Spec.Namespaces.Sandbox, cfg.Spec.Namespaces.Observability} {
			if current, getErr := clients.typed.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{}); getErr == nil && current.Labels["argus.io/release-id"] == cfg.Spec.ReleaseID {
				_ = clients.typed.CoreV1().Namespaces().Delete(ctx, namespace, metav1.DeleteOptions{})
			}
		}
	}
	if err := helm.uninstall(cfg.Spec.ReleaseID+"-foundation", "default"); err != nil {
		_, _ = fmt.Fprintf(a.stderr, "warning: %v\n", err)
	}
	if deleteCRDs {
		if err := a.deleteOwnedCRDs(ctx, cfg); err != nil {
			return err
		}
	}
	if err := a.imagesClean(ctx, cfg); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.stdout, "Argus %s removed\n", cfg.Spec.ReleaseID)
	return nil
}

func deleteTelemetryRootCASecret(ctx context.Context, client kubernetes.Interface, releaseID string) error {
	const namespace = "cert-manager"
	name := releaseID + "-telemetry-root-ca"
	secret, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect telemetry root CA Secret %s/%s: %w", namespace, name, err)
	}
	if owner := secret.Labels["argus.io/release-id"]; owner != "" && owner != releaseID {
		return fmt.Errorf("refusing to delete telemetry root CA Secret %s/%s owned by release %q", namespace, name, owner)
	}
	if err := client.CoreV1().Secrets(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete telemetry root CA Secret %s/%s: %w", namespace, name, err)
	}
	return nil
}

func (a *App) deleteOwnedCRDs(ctx context.Context, cfg *InstallConfig) error {
	output, err := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "get", "crd", "--selector", "argus.io/owner-release="+cfg.Spec.ReleaseID, "--output", "name")
	if err != nil {
		return err
	}
	resources := strings.Fields(output)
	if len(resources) == 0 {
		_, _ = fmt.Fprintln(a.stderr, "warning: no CRDs were marked as owned; refusing broad CRD deletion")
		return nil
	}
	args := []string{"--context", cfg.Spec.KubeContext, "delete"}
	args = append(args, resources...)
	args = append(args, "--wait=true")
	_, err = a.runner.run(ctx, nil, "kubectl", args...)
	return err
}

func (a *App) markOwnedCRDs(ctx context.Context, cfg *InstallConfig) error {
	for _, selector := range ownedCRDSelectors(cfg) {
		resources, err := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "get", "crd", "--selector", selector, "--output", "name")
		if err != nil {
			continue
		}
		for _, resource := range strings.Fields(resources) {
			if _, err := a.runner.quiet(ctx, "kubectl", "--context", cfg.Spec.KubeContext, "label", resource, "argus.io/owner-release="+cfg.Spec.ReleaseID, "--overwrite"); err != nil {
				return err
			}
		}
	}
	return nil
}

func ownedCRDSelectors(cfg *InstallConfig) []string {
	return []string{
		"app=strimzi",
		"clickhouse.altinity.com/chop",
		"clickhouse-keeper.altinity.com/chop",
		"app.kubernetes.io/instance=" + cfg.upstreamReleaseName("os"),
	}
}
