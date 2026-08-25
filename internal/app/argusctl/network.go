package argusctl

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
)

type NetworkProfile struct {
	Policy           NetworkPolicyProfile   `json:"policy"`
	Egress           EgressProfile          `json:"egress"`
	ProtectedTargets ProtectedTargetProfile `json:"protectedTargets"`
	SecurityPosture  string                 `json:"securityPosture"`
	Warnings         []string               `json:"warnings,omitempty"`
}

type NetworkPolicyProfile struct {
	APISupported bool   `json:"apiSupported"`
	Enforcement  string `json:"enforcement"`
	ProbeMessage string `json:"probeMessage,omitempty"`
}

type EgressProfile struct {
	Mode             string   `json:"mode"`
	DetectedProvider string   `json:"detectedProvider,omitempty"`
	Status           string   `json:"status"`
	Verified         bool     `json:"verified"`
	ExpectedIPs      []string `json:"expectedIPs,omitempty"`
	ObservedIPs      []string `json:"observedIPs,omitempty"`
	VerificationURL  string   `json:"verificationURL,omitempty"`
}

type ProtectedTargetProfile struct {
	CIDRs     []string `json:"cidrs"`
	Addresses []string `json:"addresses"`
	Sources   []string `json:"sources"`
}

func discoverNetworkProfile(ctx context.Context, clients *kubeClients, cfg *InstallConfig) NetworkProfile {
	profile := NetworkProfile{
		Policy:          NetworkPolicyProfile{Enforcement: "unsupported"},
		Egress:          EgressProfile{Mode: "default-route", Status: "absent", ExpectedIPs: append([]string(nil), cfg.Spec.Network.Egress.ExpectedIPs...), VerificationURL: cfg.Spec.Network.Egress.VerificationURL},
		SecurityPosture: "baseline",
	}
	if cfg.Spec.Network.Mode == "portable" {
		profile.SecurityPosture = "degraded"
	}
	if resources, err := clients.typed.Discovery().ServerResourcesForGroupVersion("networking.k8s.io/v1"); err == nil {
		for _, resource := range resources.APIResources {
			if resource.Kind == "NetworkPolicy" {
				profile.Policy.APISupported = true
				profile.Policy.Enforcement = "unverified"
				break
			}
		}
	}
	if !profile.Policy.APISupported {
		profile.SecurityPosture = "degraded"
		profile.Warnings = append(profile.Warnings, "NetworkPolicy API is unavailable; internal network isolation is unsupported")
	} else if cfg.Spec.Network.Mode == "portable" {
		profile.Policy.Enforcement = "unverified"
		profile.Policy.ProbeMessage = "portable mode does not require CNI NetworkPolicy enforcement"
	} else if enforcement, message := probeNetworkPolicyEnforcement(ctx, clients); enforcement != "" {
		profile.Policy.Enforcement = enforcement
		profile.Policy.ProbeMessage = message
		if enforcement != "enforced" {
			profile.SecurityPosture = "degraded"
			profile.Warnings = append(profile.Warnings, message)
		}
	}
	profile.ProtectedTargets = discoverProtectedTargets(ctx, clients, cfg)
	if provider := detectEgressProvider(ctx, clients); provider != "" {
		profile.Egress.DetectedProvider = provider
		profile.Egress.Status = "external-unverified"
		profile.Egress.Mode = "external-gateway"
	} else if len(profile.Egress.ExpectedIPs) > 0 || profile.Egress.VerificationURL != "" {
		profile.Egress.Status = "external-unverified"
	}
	if profile.Egress.Status != "verified" {
		profile.SecurityPosture = "degraded"
		if profile.Egress.Status == "absent" {
			profile.Warnings = append(profile.Warnings, "no external Egress Gateway was detected; Direct Executor uses the cluster default route")
		} else {
			profile.Warnings = append(profile.Warnings, "external Egress Gateway was detected but its advertised exit could not be verified")
		}
	}
	if cfg.Spec.Network.Mode == "network-policy" && profile.Policy.Enforcement != "enforced" {
		profile.Warnings = append(profile.Warnings, "NetworkPolicy enforcement could not be verified; installation continues in degraded mode")
	}
	return profile
}

func probeNetworkPolicyEnforcement(ctx context.Context, clients *kubeClients) (string, string) {
	name := fmt.Sprintf("argus-network-probe-%d", time.Now().UnixNano())
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"app.kubernetes.io/part-of": "argus-network-probe"}}}
	if _, err := clients.typed.CoreV1().Namespaces().Create(ctx, namespace, metav1.CreateOptions{}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe namespace could not be created: %v", err)
	}
	defer func() {
		_ = clients.typed.CoreV1().Namespaces().Delete(context.Background(), name, metav1.DeleteOptions{})
	}()
	server := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "server", Namespace: name, Labels: map[string]string{"role": "server"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "server", Image: "busybox:1.36", Command: []string{"sh", "-c", "while true; do printf 'HTTP/1.1 200 OK\r\n\r\nprobe' | nc -l -p 8080; done"}, Ports: []corev1.ContainerPort{{ContainerPort: 8080}}}}, RestartPolicy: corev1.RestartPolicyAlways}}
	if _, err := clients.typed.CoreV1().Pods(name).Create(ctx, server, metav1.CreateOptions{}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe server could not be created: %v", err)
	}
	if _, err := clients.typed.CoreV1().Services(name).Create(ctx, &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "server", Namespace: name}, Spec: corev1.ServiceSpec{Selector: map[string]string{"role": "server"}, Ports: []corev1.ServicePort{{Port: 8080, TargetPort: intstr.FromInt(8080)}}}}, metav1.CreateOptions{}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe service could not be created: %v", err)
	}
	if err := wait.PollUntilContextTimeout(ctx, time.Second, 20*time.Second, true, func(ctx context.Context) (bool, error) {
		pod, err := clients.typed.CoreV1().Pods(name).Get(ctx, "server", metav1.GetOptions{})
		return err == nil && pod.Status.Phase == corev1.PodRunning, nil
	}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe server did not become ready: %v", err)
	}
	if err := runNetworkProbeJob(ctx, clients, name, "allowed", "client", []string{"sh", "-c", "wget -q -T 3 -O - http://server:8080 | grep -q probe"}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe baseline connectivity failed: %v", err)
	}
	policy := &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "deny-client-egress", Namespace: name}, Spec: networkingv1.NetworkPolicySpec{PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{"role": "client"}}, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: []networkingv1.NetworkPolicyEgressRule{}}}
	if _, err := clients.typed.NetworkingV1().NetworkPolicies(name).Create(ctx, policy, metav1.CreateOptions{}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe policy could not be created: %v", err)
	}
	if err := runNetworkProbeJob(ctx, clients, name, "denied", "client", []string{"sh", "-c", "if wget -q -T 3 -O - http://server:8080 | grep -q probe; then exit 1; fi"}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe did not observe an enforced deny: %v", err)
	}
	if err := runNetworkProbeJob(ctx, clients, name, "allowed-after-policy", "observer", []string{"sh", "-c", "wget -q -T 3 -O - http://server:8080 | grep -q probe"}); err != nil {
		return "unverified", fmt.Sprintf("NetworkPolicy probe denied an unselected client: %v", err)
	}
	return "enforced", "NetworkPolicy probe confirmed deny and allow behavior"
}

func runNetworkProbeJob(ctx context.Context, clients *kubeClients, namespace, suffix, role string, command []string) error {
	backoff := int32(0)
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "probe-" + suffix, Namespace: namespace, Labels: map[string]string{"role": role}}, Spec: batchv1.JobSpec{BackoffLimit: &backoff, TTLSecondsAfterFinished: int32ptr(30), Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"role": role}}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, Containers: []corev1.Container{{Name: "client", Image: "busybox:1.36", Command: command}}}}}}
	_, err := clients.typed.BatchV1().Jobs(namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return err
	}
	return waitForJob(ctx, clients, namespace, job.Name, 20*time.Second)
}

func int32ptr(value int32) *int32 {
	return &value
}

func discoverProtectedTargets(ctx context.Context, clients *kubeClients, cfg *InstallConfig) ProtectedTargetProfile {
	cidrs := map[string]bool{}
	addresses := map[string]bool{}
	sources := map[string]bool{}
	addAddress := func(value, source string) {
		if address, err := netip.ParseAddr(value); err == nil {
			addresses[address.Unmap().String()] = true
			sources[source] = true
		}
	}
	addCIDR := func(value, source string) {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			cidrs[prefix.Masked().String()] = true
			sources[source] = true
		}
	}
	for _, value := range []string{"169.254.169.254/32", "100.100.100.200/32", "fd00:ec2::254/128"} {
		addCIDR(value, "metadata")
	}
	for _, namespace := range []string{cfg.Spec.Namespaces.System, cfg.Spec.Namespaces.Sandbox, cfg.Spec.Namespaces.Observability} {
		if services, err := clients.typed.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			for _, service := range services.Items {
				for _, ip := range service.Spec.ClusterIPs {
					if ip != "None" {
						addAddress(ip, "argus-service")
					}
				}
			}
		}
		if pods, err := clients.typed.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			for _, pod := range pods.Items {
				addAddress(pod.Status.PodIP, "argus-pod")
			}
		}
		if slices, err := clients.typed.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{}); err == nil {
			for _, slice := range slices.Items {
				for _, endpoint := range slice.Endpoints {
					for _, address := range endpoint.Addresses {
						addAddress(address, "argus-endpointslice")
					}
				}
			}
		}
	}
	if services, err := clients.typed.CoreV1().Services("kube-system").List(ctx, metav1.ListOptions{}); err == nil {
		for _, service := range services.Items {
			if service.Name == "kube-dns" || service.Name == "coredns" {
				for _, ip := range service.Spec.ClusterIPs {
					addAddress(ip, "cluster-dns")
				}
			}
		}
	}
	if nodes, err := clients.typed.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
		for _, node := range nodes.Items {
			for _, address := range node.Status.Addresses {
				if address.Type == corev1.NodeInternalIP {
					addAddress(address.Address, "node-internal-ip")
				}
			}
			if node.Spec.PodCIDR != "" {
				addCIDR(node.Spec.PodCIDR, "pod-cidr")
			}
			for _, cidr := range node.Spec.PodCIDRs {
				addCIDR(cidr, "pod-cidr")
			}
		}
	}
	if parsed, err := url.Parse(clients.rest.Host); err == nil {
		if host := parsed.Hostname(); host != "" {
			if address, err := netip.ParseAddr(host); err == nil {
				addAddress(address.String(), "kubernetes-api")
			} else if resolved, err := net.LookupHost(host); err == nil {
				for _, value := range resolved {
					addAddress(value, "kubernetes-api")
				}
			}
		}
	}
	for _, value := range cfg.Spec.Security.ProtectedCIDRs {
		addCIDR(value, "user-configured")
	}
	return ProtectedTargetProfile{CIDRs: sortedSet(cidrs), Addresses: sortedSet(addresses), Sources: sortedSet(sources)}
}

func detectEgressProvider(ctx context.Context, clients *kubeClients) string {
	if _, err := clients.typed.Discovery().ServerResourcesForGroupVersion("cilium.io/v2"); err == nil {
		return "cilium"
	}
	if _, err := clients.typed.Discovery().ServerResourcesForGroupVersion("projectcalico.org/v3"); err == nil {
		return "calico"
	}
	if pods, err := clients.typed.CoreV1().Pods("kube-system").List(ctx, metav1.ListOptions{}); err == nil {
		for _, pod := range pods.Items {
			name := strings.ToLower(pod.Name + " " + pod.Labels["k8s-app"] + " " + pod.Labels["app.kubernetes.io/name"])
			if strings.Contains(name, "cilium") {
				return "cilium"
			}
			if strings.Contains(name, "calico") {
				return "calico"
			}
			if strings.Contains(name, "istio-egress") || strings.Contains(name, "egress-gateway") {
				return "istio"
			}
		}
	}
	return ""
}

func sortedSet(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func protectedPrefixes(profile NetworkProfile) []string {
	values := make(map[string]bool, len(profile.ProtectedTargets.CIDRs)+len(profile.ProtectedTargets.Addresses))
	for _, value := range profile.ProtectedTargets.CIDRs {
		values[value] = true
	}
	for _, value := range profile.ProtectedTargets.Addresses {
		address, err := netip.ParseAddr(value)
		if err != nil {
			continue
		}
		bits := 128
		if address.Is4() {
			bits = 32
		}
		values[netip.PrefixFrom(address, bits).String()] = true
	}
	return sortedSet(values)
}

func networkProfileMessage(profile NetworkProfile) string {
	return fmt.Sprintf("NetworkPolicy=%s; EgressGateway=%s; posture=%s", profile.Policy.Enforcement, profile.Egress.Status, profile.SecurityPosture)
}
