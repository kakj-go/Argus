package argusdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type E2EKube struct {
	Context   string
	Artifacts string
	Client    kubernetes.Interface
	Config    *rest.Config
}

func NewE2EKube(contextName, artifacts string) (*E2EKube, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &E2EKube{Context: contextName, Artifacts: artifacts, Client: client, Config: config}, nil
}

func (k *E2EKube) NodeArchitecture(ctx context.Context) (string, error) {
	nodes, err := k.Client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", err
	}
	if len(nodes.Items) == 0 {
		return "", fmt.Errorf("Kubernetes context %s has no nodes", k.Context)
	}
	architecture := nodes.Items[0].Status.NodeInfo.Architecture
	for _, node := range nodes.Items[1:] {
		if node.Status.NodeInfo.Architecture != architecture {
			return "", fmt.Errorf("mixed node architectures are not supported by local E2E")
		}
	}
	return architecture, nil
}

// DedicatedClusterConflicts reports cluster-scoped resources whose fixed names
// prevent the upstream operator charts from being owned by an E2E Helm release.
func (k *E2EKube) DedicatedClusterConflicts(ctx context.Context) ([]string, error) {
	names := []string{
		"strimzi-cluster-operator-namespaced",
		"opensandbox-manager-role",
		"opensandbox-server-role",
	}
	conflicts := make([]string, 0, len(names))
	for _, name := range names {
		role, err := k.Client.RbacV1().ClusterRoles().Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect ClusterRole %s: %w", name, err)
		}
		owner := strings.TrimSpace(role.Annotations["meta.helm.sh/release-name"])
		ownerNamespace := strings.TrimSpace(role.Annotations["meta.helm.sh/release-namespace"])
		description := "ClusterRole/" + name
		if owner != "" {
			description += " owned by Helm release " + owner
			if ownerNamespace != "" {
				description += " in " + ownerNamespace
			}
		}
		conflicts = append(conflicts, description)
	}
	sort.Strings(conflicts)
	return conflicts, nil
}

func (k *E2EKube) AcquireLease(ctx context.Context, name, holder string) error {
	leases := k.Client.CoordinationV1().Leases("kube-system")
	if existing, err := leases.Get(ctx, name, metav1.GetOptions{}); err == nil {
		identity := "unknown"
		if existing.Spec.HolderIdentity != nil {
			identity = *existing.Spec.HolderIdentity
		}
		return fmt.Errorf("E2E lease %s is already held by %s", name, identity)
	}
	duration := int32(7200)
	now := metav1.NewMicroTime(time.Now().UTC())
	_, err := leases.Create(ctx, &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "kube-system", Labels: map[string]string{"app.kubernetes.io/part-of": "argus-e2e"}},
		Spec:       coordinationv1.LeaseSpec{HolderIdentity: &holder, LeaseDurationSeconds: &duration, AcquireTime: &now, RenewTime: &now},
	}, metav1.CreateOptions{})
	return err
}

func (k *E2EKube) ReleaseLease(ctx context.Context, name string) error {
	leases := k.Client.CoordinationV1().Leases("kube-system")
	if err := leases.Delete(ctx, name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	for {
		_, err := leases.Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (k *E2EKube) SecretValue(ctx context.Context, namespace, name, key string) (string, error) {
	secret, err := k.Client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	value, found := secret.Data[key]
	if !found {
		return "", fmt.Errorf("secret %s/%s has no %s", namespace, name, key)
	}
	return string(value), nil
}

func (k *E2EKube) CollectDiagnostics(ctx context.Context, env *E2EEnvironment, directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	var diagnosticErrors []error
	namespaces := append([]string{}, env.ManagedNamespaces...)
	namespaces = append(namespaces, env.SystemNS, env.SandboxNS, env.ObservNS)
	sort.Strings(namespaces)
	seen := map[string]bool{}
	for _, namespace := range namespaces {
		if namespace == "" {
			continue
		}
		if seen[namespace] {
			continue
		}
		seen[namespace] = true
		pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			if !apierrors.IsNotFound(err) {
				diagnosticErrors = append(diagnosticErrors, fmt.Errorf("list Pods in %s: %w", namespace, err))
			}
			continue
		}
		data, err := redactedJSONDocument(pods)
		if err == nil {
			err = writePrivate(filepath.Join(directory, "pods-"+namespace+".json"), data)
		}
		if err != nil {
			diagnosticErrors = append(diagnosticErrors, fmt.Errorf("write Pod diagnostics for %s: %w", namespace, err))
		}
		events, err := k.Client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			diagnosticErrors = append(diagnosticErrors, fmt.Errorf("list Events in %s: %w", namespace, err))
		} else {
			eventData, encodeErr := redactedJSONDocument(events)
			if encodeErr == nil {
				encodeErr = writePrivate(filepath.Join(directory, "events-"+namespace+".json"), eventData)
			}
			if encodeErr != nil {
				diagnosticErrors = append(diagnosticErrors, fmt.Errorf("write Event diagnostics for %s: %w", namespace, encodeErr))
			}
		}
		for _, pod := range pods.Items {
			containers := append([]string{}, containerNames(pod.Spec.InitContainers)...)
			containers = append(containers, containerNames(pod.Spec.Containers)...)
			sort.Strings(containers)
			for _, container := range containers {
				logs, logErr := k.Client.CoreV1().Pods(namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: container, TailLines: int64Pointer(1000)}).DoRaw(ctx)
				if logErr != nil {
					continue
				}
				logs = redactDiagnostic(logs)
				filename := kubernetesNameForDev(namespace+"-"+pod.Name+"-"+container) + ".log"
				if err := writePrivate(filepath.Join(directory, filename), logs); err != nil {
					diagnosticErrors = append(diagnosticErrors, fmt.Errorf("write logs for %s/%s/%s: %w", namespace, pod.Name, container, err))
				}
			}
		}
	}
	return errors.Join(diagnosticErrors...)
}

func redactedJSONDocument(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}
	redacted, err := json.MarshalIndent(redactJSON(document), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(redacted, '\n'), nil
}

func containerNames(containers []corev1.Container) []string {
	result := make([]string, 0, len(containers))
	for _, container := range containers {
		result = append(result, container.Name)
	}
	return result
}

func int64Pointer(value int64) *int64 { return &value }

var sensitiveDiagnosticFields = []string{"password", "token", "secret", "csrf", "authorization", "cookie"}

func redactDiagnostic(input []byte) []byte {
	lines := strings.Split(string(input), "\n")
	kept := lines[:0]
	for _, line := range lines {
		lower := strings.ToLower(line)
		sensitive := false
		for _, field := range sensitiveDiagnosticFields {
			if strings.Contains(lower, field) {
				sensitive = true
				break
			}
		}
		if !sensitive {
			kept = append(kept, line)
		}
	}
	return []byte(strings.Join(kept, "\n"))
}

func (k *E2EKube) PodNames(ctx context.Context, namespace, selector string) ([]string, error) {
	pods, err := k.Client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(pods.Items))
	for _, pod := range pods.Items {
		result = append(result, pod.Name)
	}
	return result, nil
}
