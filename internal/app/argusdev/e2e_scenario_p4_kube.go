package argusdev

import (
	"context"
	"net"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const p4SSHPassword = "M3-e2e-ssh-password"

type p4TargetNetwork string

const (
	p4NetworkOpen       p4TargetNetwork = "open"
	p4NetworkRootTunnel p4TargetNetwork = "root_tunnel"
	p4NetworkMember     p4TargetNetwork = "member"
)

type p4Target struct {
	Name       string
	Namespace  string
	ExternalIP string
	Network    p4TargetNetwork
}

func (a *App) patchP4DirectExecutor(ctx context.Context, env *E2EEnvironment) error {
	hosts, err := e2eExposureHosts(env.ConfigPath)
	if err != nil {
		return err
	}
	patch := map[string]any{"spec": map[string]any{
		"replicas": int32(2),
		"template": map[string]any{"spec": map[string]any{"hostAliases": []any{
			map[string]any{"ip": env.Endpoints.IngressIP, "hostnames": []string{hosts["enterprise"], hosts["platform"], hosts["cards"], hosts["remote"]}},
			map[string]any{"ip": env.Endpoints.ConnectorIP, "hostnames": []string{hosts["connector"]}},
		}}},
	}}
	if err = env.Kube.PatchDeployment(ctx, env.SystemNS, "argus-direct-executor", patch); err != nil {
		return err
	}
	return env.Kube.WaitDeployment(ctx, env.SystemNS, "argus-direct-executor", 5*time.Minute)
}

func (a *App) createP4Target(ctx context.Context, env *E2EEnvironment, name, externalIP string, network p4TargetNetwork) (p4Target, error) {
	target := p4Target{
		Name: kubernetesNameForDev("p4-" + name), Namespace: kubernetesNameForDev(env.ReleaseID + "-p4-" + name),
		ExternalIP: externalIP, Network: network,
	}
	labels := map[string]string{
		"app.kubernetes.io/name":     "argus-p4-target",
		"app.kubernetes.io/instance": target.Name,
		"app.kubernetes.io/part-of":  "argus-e2e",
		"argus.io/release-id":        env.ReleaseID,
		"argus.io/p4-run":            env.Options.RunID,
	}
	namespaceLabels := map[string]string{
		"argus.io/release-id":                env.ReleaseID,
		"argus.io/p4-run":                    env.Options.RunID,
		"pod-security.kubernetes.io/enforce": "privileged",
		"pod-security.kubernetes.io/audit":   "privileged",
		"pod-security.kubernetes.io/warn":    "privileged",
	}
	if _, err := env.Kube.Client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: target.Namespace, Labels: namespaceLabels},
	}, metav1.CreateOptions{}); err != nil && !apierrors.IsAlreadyExists(err) {
		return p4Target{}, err
	}
	env.ManagedNamespaces = append(env.ManagedNamespaces, target.Namespace)
	hostAliases, err := p4HostAliases(env)
	if err != nil {
		return p4Target{}, err
	}
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: target.Name, Namespace: target.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/instance": target.Name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					HostAliases: hostAliases,
					Containers: []corev1.Container{{
						Name: "systemd", Image: env.State.FixtureImages["systemd"], ImagePullPolicy: corev1.PullNever,
						Command:         []string{"/sbin/init"},
						SecurityContext: &corev1.SecurityContext{Privileged: boolPointer(true), RunAsNonRoot: boolPointer(false), RunAsUser: int64PointerValue(0)},
						Ports:           []corev1.ContainerPort{{Name: "ssh", ContainerPort: 22}},
						ReadinessProbe: &corev1.Probe{ProbeHandler: corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("ssh")}},
							InitialDelaySeconds: 2, PeriodSeconds: 2},
						VolumeMounts: []corev1.VolumeMount{{Name: "cgroup", MountPath: "/sys/fs/cgroup"}},
					}},
					Volumes: []corev1.Volume{{Name: "cgroup", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys/fs/cgroup", Type: hostPathType(corev1.HostPathDirectory)}}}},
				},
			},
		},
	}
	if _, err = env.Kube.Client.AppsV1().Deployments(target.Namespace).Create(ctx, deployment, metav1.CreateOptions{}); err != nil {
		return p4Target{}, err
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: target.Name, Namespace: target.Namespace, Labels: labels},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app.kubernetes.io/instance": target.Name}, ExternalIPs: []string{target.ExternalIP},
			Ports: []corev1.ServicePort{{Name: "ssh", Port: 22, TargetPort: intstr.FromString("ssh")}},
		},
	}
	if _, err = env.Kube.Client.CoreV1().Services(target.Namespace).Create(ctx, service, metav1.CreateOptions{}); err != nil {
		return p4Target{}, err
	}
	if err = a.createP4TargetPolicies(ctx, env, target, labels); err != nil {
		return p4Target{}, err
	}
	if err = env.Kube.WaitDeployment(ctx, target.Namespace, target.Name, 5*time.Minute); err != nil {
		return p4Target{}, err
	}
	return target, nil
}

func hostPathType(value corev1.HostPathType) *corev1.HostPathType { return &value }

func p4HostAliases(env *E2EEnvironment) ([]corev1.HostAlias, error) {
	hosts, err := e2eExposureHosts(env.ConfigPath)
	if err != nil {
		return nil, err
	}
	return []corev1.HostAlias{
		{IP: env.Endpoints.IngressIP, Hostnames: []string{hosts["enterprise"], hosts["platform"], hosts["cards"], hosts["remote"]}},
		{IP: env.Endpoints.ConnectorIP, Hostnames: []string{hosts["connector"]}},
	}, nil
}

func (a *App) createP4TargetPolicies(ctx context.Context, env *E2EEnvironment, target p4Target, labels map[string]string) error {
	podSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/instance": target.Name}}
	ingress := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: target.Name + "-ingress", Namespace: target.Namespace},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: podSelector, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{
					{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": env.SystemNS}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "argus-direct-executor"}}},
					{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"argus.io/release-id": env.ReleaseID}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "argus-p4-target"}}},
				},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocol(corev1.ProtocolTCP), Port: intOrString(22)}},
			}},
		},
	}
	if _, err := env.Kube.Client.NetworkingV1().NetworkPolicies(target.Namespace).Create(ctx, ingress, metav1.CreateOptions{}); err != nil {
		return err
	}
	if target.Network == p4NetworkOpen {
		return nil
	}
	egress := []networkingv1.NetworkPolicyEgressRule{
		{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}}},
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocol(corev1.ProtocolUDP), Port: intOrString(53)}, {Protocol: protocol(corev1.ProtocolTCP), Port: intOrString(53)}}},
		{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": env.SystemNS}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "argus-e2e-artifact-server"}}}},
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocol(corev1.ProtocolTCP), Port: intOrString(8443)}}},
		{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"argus.io/release-id": env.ReleaseID}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "argus-p4-target"}}}},
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocol(corev1.ProtocolTCP), Port: intOrString(22)}}},
	}
	if target.Network == p4NetworkMember {
		cidr := env.Endpoints.IngressIP + "/32"
		if net.ParseIP(env.Endpoints.IngressIP).To4() == nil {
			cidr = env.Endpoints.IngressIP + "/128"
		}
		egress = append(egress, networkingv1.NetworkPolicyEgressRule{
			To:    []networkingv1.NetworkPolicyPeer{{IPBlock: &networkingv1.IPBlock{CIDR: cidr}}},
			Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocol(corev1.ProtocolTCP), Port: intOrString(443)}},
		})
	}
	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: target.Name + "-egress", Namespace: target.Namespace, Labels: labels},
		Spec:       networkingv1.NetworkPolicySpec{PodSelector: podSelector, PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, Egress: egress},
	}
	_, err := env.Kube.Client.NetworkingV1().NetworkPolicies(target.Namespace).Create(ctx, policy, metav1.CreateOptions{})
	return err
}

func protocol(value corev1.Protocol) *corev1.Protocol { return &value }
func intOrString(value int) *intstr.IntOrString {
	port := intstr.FromInt32(int32(value))
	return &port
}

func (a *App) execP4Target(ctx context.Context, env *E2EEnvironment, target p4Target, command ...string) (string, error) {
	return env.Kube.Exec(ctx, target.Namespace, "app.kubernetes.io/instance="+target.Name, "systemd", command...)
}

// blockP4ExecutorSSH temporarily removes only the Direct Executor peer from a
// target's ingress policy. Kubernetes exec remains available, so a signal can
// be injected into the local Collector while no Executor can rebuild SSH.
func (a *App) blockP4ExecutorSSH(ctx context.Context, env *E2EEnvironment, target p4Target) (func() error, error) {
	client := env.Kube.Client.NetworkingV1().NetworkPolicies(target.Namespace)
	name := target.Name + "-ingress"
	policy, err := client.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	original := policy.Spec.DeepCopy()
	for ruleIndex := range policy.Spec.Ingress {
		filtered := make([]networkingv1.NetworkPolicyPeer, 0, len(policy.Spec.Ingress[ruleIndex].From))
		for _, peer := range policy.Spec.Ingress[ruleIndex].From {
			namespace := ""
			application := ""
			if peer.NamespaceSelector != nil {
				namespace = peer.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
			}
			if peer.PodSelector != nil {
				application = peer.PodSelector.MatchLabels["app.kubernetes.io/name"]
			}
			if namespace == env.SystemNS && application == "argus-direct-executor" {
				continue
			}
			filtered = append(filtered, peer)
		}
		policy.Spec.Ingress[ruleIndex].From = filtered
	}
	if _, err = client.Update(ctx, policy, metav1.UpdateOptions{}); err != nil {
		return nil, err
	}
	restore := func() error {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		current, getErr := client.Get(restoreCtx, name, metav1.GetOptions{})
		if getErr != nil {
			return getErr
		}
		current.Spec = *original.DeepCopy()
		_, updateErr := client.Update(restoreCtx, current, metav1.UpdateOptions{})
		return updateErr
	}
	return restore, nil
}
