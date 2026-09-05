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
		if cfg.Spec.PKI.Mode == PKIModeManaged {
			if err := deleteManagedRootCASecret(ctx, clients.typed, cfg.Spec.ReleaseID); err != nil {
				return err
			}
		}
		if err := deletePKITrustSource(ctx, clients.typed, cfg); err != nil {
			return err
		}
		if err := deletePKICleanupRBAC(ctx, clients.typed, cfg.Spec.ReleaseID); err != nil {
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

func deletePKICleanupRBAC(ctx context.Context, client kubernetes.Interface, releaseID string) error {
	const namespace = "cert-manager"
	name := releaseID + "-pki-root-cleanup"
	if binding, err := client.RbacV1().RoleBindings(namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		if binding.Labels["argus.io/release-id"] != releaseID {
			return fmt.Errorf("refusing to delete PKI cleanup RoleBinding not owned by release %q", releaseID)
		}
		if err = client.RbacV1().RoleBindings(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	if role, err := client.RbacV1().Roles(namespace).Get(ctx, name, metav1.GetOptions{}); err == nil {
		if role.Labels["argus.io/release-id"] != releaseID {
			return fmt.Errorf("refusing to delete PKI cleanup Role not owned by release %q", releaseID)
		}
		if err = client.RbacV1().Roles(namespace).Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	} else if !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}

func deleteManagedRootCASecret(ctx context.Context, client kubernetes.Interface, releaseID string) error {
	const namespace = "cert-manager"
	baseName := releaseID + "-root-ca"
	base, err := client.CoreV1().Secrets(namespace).Get(ctx, baseName, metav1.GetOptions{})
	if err == nil && (base.Labels["argus.io/release-id"] != releaseID || base.Labels["argus.io/pki-role"] != "managed-root") {
		return fmt.Errorf("refusing to delete managed root CA Secret %s/%s not owned by release %q", namespace, baseName, releaseID)
	}
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("inspect managed root CA Secret %s/%s: %w", namespace, baseName, err)
	}
	secrets, err := client.CoreV1().Secrets(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "argus.io/release-id=" + releaseID + ",argus.io/pki-role=managed-root",
	})
	if err != nil {
		return fmt.Errorf("list managed root CA Secrets for release %s: %w", releaseID, err)
	}
	for _, secret := range secrets.Items {
		if err := client.CoreV1().Secrets(namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete managed root CA Secret %s/%s: %w", namespace, secret.Name, err)
		}
	}
	return nil
}

func deletePKITrustSource(ctx context.Context, client kubernetes.Interface, cfg *InstallConfig) error {
	configMaps := client.CoreV1().ConfigMaps("cert-manager")
	current, err := configMaps.Get(ctx, cfg.trustSourceName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect PKI trust source: %w", err)
	}
	if current.Labels["argus.io/release-id"] != cfg.Spec.ReleaseID {
		return fmt.Errorf("refusing to delete PKI trust source not owned by release %q", cfg.Spec.ReleaseID)
	}
	if err := configMaps.Delete(ctx, current.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete PKI trust source: %w", err)
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
