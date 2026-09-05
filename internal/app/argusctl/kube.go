package argusctl

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

type kubeClients struct {
	rest      *rest.Config
	typed     kubernetes.Interface
	dynamic   dynamic.Interface
	rawConfig clientcmdapi.Config
}

func clientsFor(contextName string) (*kubeClients, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	deferred := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)
	raw, err := deferred.RawConfig()
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig: %w", err)
	}
	if _, ok := raw.Contexts[contextName]; !ok {
		return nil, fmt.Errorf("kube context %q does not exist", contextName)
	}
	restConfig, err := deferred.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("build Kubernetes client config: %w", err)
	}
	restConfig.QPS = 40
	restConfig.Burst = 80
	typed, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes dynamic client: %w", err)
	}
	return &kubeClients{rest: restConfig, typed: typed, dynamic: dynamicClient, rawConfig: raw}, nil
}

type Check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Message  string `json:"message"`
	Blocking bool   `json:"blocking"`
}

type PreflightReport struct {
	Profile          string         `json:"profile"`
	Context          string         `json:"context"`
	ReleaseID        string         `json:"releaseId"`
	Checks           []Check        `json:"checks"`
	Network          NetworkProfile `json:"network"`
	EstimatedPods    int            `json:"estimatedPods"`
	EstimatedPVCs    int            `json:"estimatedPvcs"`
	EstimatedStorage string         `json:"estimatedStorage"`
	Ready            bool           `json:"ready"`
}

func (a *App) preflight(ctx context.Context, cfg *InstallConfig, output string) error {
	report, err := a.buildPreflight(ctx, cfg)
	if renderErr := writeOutput(a.stdout, output, report, func(w io.Writer) {
		_, _ = fmt.Fprintf(w, "Preflight %s on %s\n", report.ReleaseID, report.Context)
		for _, check := range report.Checks {
			_, _ = fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
		}
		_, _ = fmt.Fprintf(w, "Estimate: %d pods, %d PVCs, %s requested storage\n", report.EstimatedPods, report.EstimatedPVCs, report.EstimatedStorage)
	}); renderErr != nil {
		return renderErr
	}
	return err
}

func (a *App) buildPreflight(ctx context.Context, cfg *InstallConfig) (PreflightReport, error) {
	report := PreflightReport{
		Profile: cfg.Spec.Profile, Context: cfg.Spec.KubeContext, ReleaseID: cfg.Spec.ReleaseID,
		EstimatedPods: 22, EstimatedPVCs: 6, EstimatedStorage: "19Gi", Ready: true,
	}
	add := func(name, status, message string, blocking bool) {
		report.Checks = append(report.Checks, Check{Name: name, Status: status, Message: message, Blocking: blocking})
		if blocking && status == "fail" {
			report.Ready = false
		}
	}

	clients, err := clientsFor(cfg.Spec.KubeContext)
	if err != nil {
		add("kubernetes-context", "fail", err.Error(), true)
		return report, fmt.Errorf("preflight failed")
	}
	version, err := clients.typed.Discovery().ServerVersion()
	if err != nil {
		add("kubernetes-api", "fail", err.Error(), true)
	} else {
		add("kubernetes-api", "pass", version.GitVersion, true)
	}
	report.Network = discoverNetworkProfile(ctx, clients, cfg)
	add("network-profile", "pass", networkProfileMessage(report.Network), false)
	for _, warning := range report.Network.Warnings {
		add("network-security", "warn", warning, false)
	}

	nodes, err := clients.typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil || len(nodes.Items) == 0 {
		add("nodes", "fail", "no schedulable Kubernetes node found", true)
	} else {
		var cpuMilli, memoryBytes, podCount int64
		architectures := map[string]bool{}
		for _, node := range nodes.Items {
			if node.Spec.Unschedulable {
				continue
			}
			cpuMilli += node.Status.Allocatable.Cpu().MilliValue()
			memoryBytes += node.Status.Allocatable.Memory().Value()
			podCount += node.Status.Allocatable.Pods().Value()
			architectures[node.Status.NodeInfo.Architecture] = true
		}
		if !architectures["arm64"] && !architectures["amd64"] {
			add("node-architecture", "fail", fmt.Sprintf("unsupported architectures %v", sortedKeys(architectures)), true)
		} else {
			add("node-architecture", "pass", strings.Join(sortedKeys(architectures), ","), true)
		}
		if cpuMilli < 10_000 || memoryBytes < 15*1024*1024*1024 || podCount < int64(report.EstimatedPods+10) {
			add("cluster-capacity", "fail", fmt.Sprintf("allocatable cpu=%dm memory=%s pods=%d", cpuMilli, resource.NewQuantity(memoryBytes, resource.BinarySI), podCount), true)
		} else {
			add("cluster-capacity", "pass", fmt.Sprintf("allocatable cpu=%dm memory=%s pods=%d", cpuMilli, resource.NewQuantity(memoryBytes, resource.BinarySI), podCount), true)
		}
	}

	storageClass, err := clients.typed.StorageV1().StorageClasses().Get(ctx, cfg.Spec.StorageClass, metav1.GetOptions{})
	if err != nil {
		add("storage-class", "fail", fmt.Sprintf("%s is not available: %v", cfg.Spec.StorageClass, err), true)
	} else if cfg.Spec.Profile == "production" && (storageClass.AllowVolumeExpansion == nil || !*storageClass.AllowVolumeExpansion) {
		add("storage-class", "fail", fmt.Sprintf("%s does not support volume expansion", cfg.Spec.StorageClass), true)
	} else {
		message := cfg.Spec.StorageClass
		if storageClass.AllowVolumeExpansion == nil || !*storageClass.AllowVolumeExpansion {
			message += " (volume expansion unavailable; accepted for local profiles)"
		}
		add("storage-class", statusFor(cfg.Spec.Profile == "production"), message, cfg.Spec.Profile == "production")
	}

	ingressClass, ingressClassErr := clients.typed.NetworkingV1().IngressClasses().Get(ctx, cfg.Spec.Exposure.IngressClassName, metav1.GetOptions{})
	if ingressClassErr != nil {
		add("ingress-class", "fail", fmt.Sprintf("IngressClass %s is not available: %v; domain-based exposure requires an ingress controller", cfg.Spec.Exposure.IngressClassName, ingressClassErr), true)
	} else {
		add("ingress-class", "pass", ingressClass.Name, true)
	}

	if pkiErr := checkPKI(ctx, clients, cfg); pkiErr != nil {
		add("pki", "fail", pkiErr.Error(), true)
	} else {
		add("pki", "pass", string(cfg.Spec.PKI.Mode), true)
	}

	var fs syscall.Statfs_t
	if err := syscall.Statfs(filepathDir(cfg.path), &fs); err != nil {
		add("host-disk", "warn", err.Error(), false)
	} else {
		free := uint64(fs.Bavail) * uint64(fs.Bsize)
		if free < 25*1024*1024*1024 {
			add("host-disk", "fail", fmt.Sprintf("only %s free; at least 25Gi is required for images and PVC backing", byteSize(free)), true)
		} else {
			add("host-disk", "pass", fmt.Sprintf("%s free", byteSize(free)), true)
		}
	}

	if cfg.Spec.Profile == "production" {
		productionChecks(ctx, clients, cfg, add)
	} else {
		if report.Network.Policy.Enforcement != "enforced" {
			add("network-policy", "warn", "NetworkPolicy enforcement was not positively verified", false)
		}
		if cfg.Spec.OpenSandbox.RuntimeClassName == "" && cfg.Spec.OpenSandbox.AllowSharedRuntime {
			add("sandbox-runtime", "warn", "shared ordinary-container runtime explicitly accepted for the local profile", false)
		}
	}
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "amd64" {
		add("host-architecture", "fail", runtime.GOARCH, true)
	} else {
		add("host-architecture", "pass", runtime.GOARCH, true)
	}

	if !report.Ready {
		return report, fmt.Errorf("preflight failed")
	}
	return report, nil
}

func productionChecks(ctx context.Context, clients *kubeClients, cfg *InstallConfig, add func(string, string, string, bool)) {
	if cfg.Spec.OpenSandbox.RuntimeClassName == "" {
		add("runtime-class", "fail", "SANDBOX_RUNTIME_ADR_REQUIRED", true)
	} else if _, err := clients.typed.NodeV1().RuntimeClasses().Get(ctx, cfg.Spec.OpenSandbox.RuntimeClassName, metav1.GetOptions{}); err != nil {
		add("runtime-class", "fail", err.Error(), true)
	} else {
		add("runtime-class", "pass", cfg.Spec.OpenSandbox.RuntimeClassName, true)
	}
	if _, err := clients.typed.Discovery().ServerResourcesForGroupVersion("metrics.k8s.io/v1beta1"); err != nil {
		add("metrics-server", "fail", "metrics.k8s.io is unavailable", true)
	} else {
		add("metrics-server", "pass", "metrics.k8s.io/v1beta1", true)
	}
	add("network-policy", "warn", "NETWORK_POLICY_ENFORCEMENT_UNVERIFIED", false)
	add("postgresql-ha", "fail", "POSTGRES_HA_ADR_REQUIRED", true)
}

// checkPKI verifies the existing global ClusterIssuer. Managed mode creates
// the issuer after cert-manager is installed and therefore has no external
// cluster prerequisite.
func checkPKI(ctx context.Context, clients *kubeClients, cfg *InstallConfig) error {
	switch cfg.Spec.PKI.Mode {
	case PKIModeExistingClusterIssuer:
		issuerGVR := schema.GroupVersionResource{Group: cfg.Spec.PKI.IssuerRef.Group, Version: "v1", Resource: "clusterissuers"}
		issuer, err := clients.dynamic.Resource(issuerGVR).Get(ctx, cfg.Spec.PKI.IssuerRef.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("ClusterIssuer %s is not available: %w", cfg.Spec.PKI.IssuerRef.Name, err)
		}
		conditions, found, err := unstructuredNestedSlice(issuer.Object, "status", "conditions")
		if err == nil && found {
			for _, raw := range conditions {
				condition, _ := raw.(map[string]any)
				if condition["type"] == "Ready" && condition["status"] == "True" {
					return nil
				}
			}
		}
		return fmt.Errorf("ClusterIssuer %s exists but is not Ready", cfg.Spec.PKI.IssuerRef.Name)
	default:
		return nil
	}
}

func unstructuredNestedSlice(object map[string]any, fields ...string) ([]any, bool, error) {
	var current any = object
	for index, field := range fields {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("field %s is not an object", fields[index])
		}
		current, ok = mapping[field]
		if !ok {
			return nil, false, nil
		}
	}
	slice, ok := current.([]any)
	if !ok {
		return nil, false, fmt.Errorf("field %s is not a list", fields[len(fields)-1])
	}
	return slice, true, nil
}

func (a *App) plan(cfg *InstallConfig, output string) error {
	plan := struct {
		Profile      string     `json:"profile"`
		ReleaseID    string     `json:"releaseId"`
		Namespaces   Namespaces `json:"namespaces"`
		Stages       []string   `json:"stages"`
		Images       []string   `json:"images"`
		Degradations []string   `json:"degradations,omitempty"`
		Blockers     []string   `json:"blockers,omitempty"`
	}{
		Profile: cfg.Spec.Profile, ReleaseID: cfg.Spec.ReleaseID, Namespaces: cfg.Spec.Namespaces,
		Stages: []string{"foundation", "data-operators", "data", "sandbox", "telemetry-pipeline", "platform"},
		Images: []string{cfg.Image("argus-backend"), cfg.Image("argus-web"), cfg.Image("minio")},
	}
	if cfg.Spec.Profile == "evaluation" {
		plan.Degradations = []string{"NETWORK_POLICY_ENFORCEMENT_UNVERIFIED", "SHARED_CONTAINER_SANDBOX_RUNTIME"}
	} else if cfg.Spec.Profile == "local-hardening" {
		plan.Degradations = []string{"LOCAL_SINGLE_NODE_OPENBAO", "NO_PRODUCTION_HA_OR_CAPACITY_CLAIMS", "ARM64_ONLY"}
	} else {
		plan.Blockers = []string{"POSTGRES_HA_ADR_REQUIRED", "SANDBOX_RUNTIME_ADR_REQUIRED"}
	}
	return writeOutput(a.stdout, output, plan, func(w io.Writer) {
		_, _ = fmt.Fprintf(w, "Argus %s plan (%s)\n", plan.ReleaseID, plan.Profile)
		for index, stage := range plan.Stages {
			_, _ = fmt.Fprintf(w, "%d. %s\n", index+1, stage)
		}
		for _, degradation := range plan.Degradations {
			_, _ = fmt.Fprintf(w, "WARN: %s\n", degradation)
		}
		for _, blocker := range plan.Blockers {
			_, _ = fmt.Fprintf(w, "BLOCKED: %s\n", blocker)
		}
	})
}

func writeOutput(w io.Writer, format string, value any, textRenderer func(io.Writer)) error {
	if format == "json" {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	textRenderer(w)
	return nil
}

func (c *kubeClients) setStage(ctx context.Context, cfg *InstallConfig, stage, state, message string) error {
	name := cfg.Spec.ReleaseID + "-install-status"
	configMaps := c.typed.CoreV1().ConfigMaps(cfg.Spec.Namespaces.System)
	current, err := configMaps.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		current = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{"app.kubernetes.io/part-of": "argus", "argus.io/release-id": cfg.Spec.ReleaseID},
		}, Data: map[string]string{}}
		current.Data["release-id"] = cfg.Spec.ReleaseID
		current.Data["profile"] = cfg.Spec.Profile
		current, err = configMaps.Create(ctx, current, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create install status ConfigMap: %w", err)
		}
	}
	if current.Data == nil {
		current.Data = map[string]string{}
	}
	current.Data["current-stage"] = stage
	current.Data["state"] = state
	current.Data["message"] = message
	current.Data["updated-at"] = time.Now().UTC().Format(time.RFC3339)
	_, err = configMaps.Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func (c *kubeClients) setNetworkProfile(ctx context.Context, cfg *InstallConfig, profile NetworkProfile) error {
	name := cfg.Spec.ReleaseID + "-install-status"
	configMaps := c.typed.CoreV1().ConfigMaps(cfg.Spec.Namespaces.System)
	current, err := configMaps.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("load install status ConfigMap for network profile: %w", err)
	}
	if current.Data == nil {
		current.Data = map[string]string{}
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		return fmt.Errorf("encode network profile: %w", err)
	}
	current.Data["network-profile"] = string(encoded)
	current.Data["network-policy-enforcement"] = profile.Policy.Enforcement
	current.Data["egress-gateway-status"] = profile.Egress.Status
	current.Data["security-posture"] = profile.SecurityPosture
	_, err = configMaps.Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func (c *kubeClients) waitResource(ctx context.Context, gvr schema.GroupVersionResource, namespace, name string, ready func(map[string]any) bool, timeout time.Duration) error {
	resourceClient := c.dynamic.Resource(gvr).Namespace(namespace)
	return wait.PollUntilContextTimeout(ctx, 3*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		item, err := resourceClient.Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return ready(item.Object), nil
	})
}

func filepathDir(path string) string {
	index := strings.LastIndex(path, string(os.PathSeparator))
	if index < 0 {
		return "."
	}
	return path[:index]
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func statusFor(fail bool) string {
	if fail {
		return "fail"
	}
	return "warn"
}

func hasPodPrefix(pods []corev1.Pod, prefix string) bool {
	for _, pod := range pods {
		if strings.HasPrefix(pod.Name, prefix) {
			return true
		}
	}
	return false
}

func byteSize(bytes uint64) string {
	return fmt.Sprintf("%.1fGi", float64(bytes)/(1024*1024*1024))
}
