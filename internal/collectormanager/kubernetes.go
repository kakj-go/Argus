package collectormanager

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/otelcol/configbundle"
)

const KubernetesNamespace = "argus-telemetry"

const collectorIdentitySecret = "argus-otelcol-identity"

type KubernetesOptions struct {
	Client            kubernetes.Interface
	Image             string
	WaitForEnrollment bool
	IdentityMaterial  *IdentityMaterial
}

func (manager Manager) ApplyKubernetes(ctx context.Context, command *connectorv1.CollectorManagementCommand, options KubernetesOptions) (Result, error) {
	if err := Validate(command); err != nil {
		return Result{}, err
	}
	if command.GetResourceType() != "kubernetes_cluster" || options.Client == nil {
		return Result{}, ErrInvalidCommand
	}
	image := strings.TrimSpace(command.GetKubernetesImage())
	if options.Image != "" && strings.TrimSpace(options.Image) != image {
		return Result{}, ErrInvalidCommand
	}
	if image == "" || strings.ContainsAny(image, " \t\r\n") || !strings.Contains(image, ":") {
		return Result{}, ErrUnsupportedPlatform
	}
	if command.GetOperation() == "uninstall" {
		return manager.uninstallKubernetes(ctx, command, options.Client)
	}
	labels := map[string]string{"app.kubernetes.io/part-of": "argus", "argus.io/collector-id": command.GetCollectorId()}
	if err := applyNamespace(ctx, options.Client, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: KubernetesNamespace, Labels: labels}}); err != nil {
		return Result{}, err
	}
	agentConfig, err := configbundle.Extract(command.GetRenderedConfig(), "kubernetes_agent")
	if err != nil {
		return Result{}, err
	}
	gatewayConfig, err := configbundle.Extract(command.GetRenderedConfig(), "kubernetes_gateway")
	if err != nil {
		return Result{}, err
	}
	trust, err := commandTrustBundle(command)
	if err != nil {
		return Result{}, err
	}
	configName := "argus-otelcol-config"
	if err := applyConfigMap(ctx, options.Client, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: configName, Namespace: KubernetesNamespace, Labels: labels},
		Data: map[string]string{"agent.yaml": string(agentConfig), "gateway.yaml": string(gatewayConfig), "server-ca.pem": string(trust.PEM),
			"trust-bundle-epoch": strconv.FormatUint(command.GetTrustBundleEpoch(), 10), "trust-bundle-sha256": trust.SHA256}}); err != nil {
		return Result{}, err
	}
	if command.GetOperation() == "install" {
		var material IdentityMaterial
		if options.IdentityMaterial != nil {
			material = *options.IdentityMaterial
			if err := validateIdentityMaterial(command.GetCollectorId(), material); err != nil {
				return Result{}, err
			}
		} else {
			var enrollErr error
			material, enrollErr = EnrollIdentity(ctx, command)
			if enrollErr != nil {
				return Result{}, enrollErr
			}
		}
		state, _ := json.Marshal(map[string]any{"epoch": material.TrustBundleEpoch, "bundle_sha256": material.TrustBundleSHA256,
			"ca_fingerprints": material.TrustBundleCAFingerprints})
		if err := applySecret(ctx, options.Client, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: collectorIdentitySecret, Namespace: KubernetesNamespace, Labels: labels},
			Data: map[string][]byte{"client.pem": material.ClientCertificatePEM, "client-key.pem": material.ClientPrivateKeyPEM,
				"server.pem": material.ServerCertificatePEM, "server-key.pem": material.ServerPrivateKeyPEM, "ca.pem": material.CABundlePEM,
				"trust-bundle.json": state, "trust-bundle-epoch": []byte(strconv.FormatUint(material.TrustBundleEpoch, 10)),
				"trust-bundle-sha256": []byte(material.TrustBundleSHA256)}}); err != nil {
			return Result{}, err
		}
	} else if _, err := options.Client.CoreV1().Secrets(KubernetesNamespace).Get(ctx, collectorIdentitySecret, metav1.GetOptions{}); err != nil {
		return Result{}, err
	}
	serviceAccount := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: "argus-otelcol", Namespace: KubernetesNamespace, Labels: labels}}
	if err := applyServiceAccount(ctx, options.Client, serviceAccount); err != nil {
		return Result{}, err
	}
	stateRole := &rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: "argus-otelcol-identity", Namespace: KubernetesNamespace, Labels: labels},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{""}, Resources: []string{"secrets"}, ResourceNames: []string{collectorIdentitySecret}, Verbs: []string{"get", "update", "patch"}},
			{APIGroups: []string{""}, Resources: []string{"configmaps"}, ResourceNames: []string{configName}, Verbs: []string{"get", "update", "patch"}},
		}}
	if err := applyRole(ctx, options.Client, stateRole); err != nil {
		return Result{}, err
	}
	stateBinding := &rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: stateRole.Name, Namespace: KubernetesNamespace, Labels: labels},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: stateRole.Name},
		Subjects: []rbacv1.Subject{{Kind: "ServiceAccount", Name: serviceAccount.Name, Namespace: KubernetesNamespace}}}
	if err := applyRoleBinding(ctx, options.Client, stateBinding); err != nil {
		return Result{}, err
	}
	if err := applyService(ctx, options.Client, collectorGatewayService(labels)); err != nil {
		return Result{}, err
	}
	if err := applyDaemonSet(ctx, options.Client, collectorDaemonSet(command, image, configName, labels)); err != nil {
		return Result{}, err
	}
	if err := applyDeployment(ctx, options.Client, collectorGatewayDeployment(command, image, configName, labels)); err != nil {
		return Result{}, err
	}
	if options.WaitForEnrollment {
		if err := waitForGateway(ctx, options.Client); err != nil {
			return Result{}, err
		}
	}
	return buildResult(command, "converged"), nil
}

func (manager Manager) uninstallKubernetes(ctx context.Context, command *connectorv1.CollectorManagementCommand, client kubernetes.Interface) (Result, error) {
	propagation := metav1.DeletePropagationForeground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	for _, remove := range []func() error{
		func() error {
			return ignoreMissing(client.AppsV1().DaemonSets(KubernetesNamespace).Delete(ctx, "argus-otelcol-agent", options))
		},
		func() error {
			return ignoreMissing(client.AppsV1().Deployments(KubernetesNamespace).Delete(ctx, "argus-otelcol-gateway", options))
		},
		func() error {
			return ignoreMissing(client.CoreV1().Services(KubernetesNamespace).Delete(ctx, "argus-otelcol-gateway", options))
		},
		func() error {
			return ignoreMissing(client.CoreV1().ConfigMaps(KubernetesNamespace).Delete(ctx, "argus-otelcol-config", options))
		},
		func() error {
			return ignoreMissing(client.CoreV1().Secrets(KubernetesNamespace).Delete(ctx, collectorIdentitySecret, options))
		},
		func() error {
			return ignoreMissing(client.RbacV1().RoleBindings(KubernetesNamespace).Delete(ctx, "argus-otelcol-identity", options))
		},
		func() error {
			return ignoreMissing(client.RbacV1().Roles(KubernetesNamespace).Delete(ctx, "argus-otelcol-identity", options))
		},
	} {
		if err := remove(); err != nil {
			return Result{}, err
		}
	}
	return buildResult(command, "uninstalled"), nil
}

func collectorDaemonSet(command *connectorv1.CollectorManagementCommand, image, configName string, labels map[string]string) *appsv1.DaemonSet {
	workloadLabels := copyLabels(labels)
	workloadLabels["app.kubernetes.io/name"] = "argus-otelcol-agent"
	return &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "argus-otelcol-agent", Namespace: KubernetesNamespace, Labels: workloadLabels,
		Annotations: map[string]string{"argus.io/config-sha256": command.GetConfigSha256(), "argus.io/artifact-sha256": command.GetArtifact().GetSha256()}},
		Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "argus-otelcol-agent"}},
			Template: collectorPodTemplate(command, image, configName, "argus-otelcol-agent")}}
}

func collectorGatewayDeployment(command *connectorv1.CollectorManagementCommand, image, configName string, labels map[string]string) *appsv1.Deployment {
	one := int32(1)
	minReadySeconds := int32(5)
	workloadLabels := copyLabels(labels)
	workloadLabels["app.kubernetes.io/name"] = "argus-otelcol-gateway"
	return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "argus-otelcol-gateway", Namespace: KubernetesNamespace, Labels: workloadLabels,
		Annotations: map[string]string{"argus.io/config-sha256": command.GetConfigSha256(), "argus.io/artifact-sha256": command.GetArtifact().GetSha256()}},
		Spec: appsv1.DeploymentSpec{Replicas: &one, MinReadySeconds: minReadySeconds,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app.kubernetes.io/name": "argus-otelcol-gateway"}},
			Template: collectorPodTemplate(command, image, configName, "argus-otelcol-gateway")}}
}

func collectorPodTemplate(command *connectorv1.CollectorManagementCommand, image, configName, component string) corev1.PodTemplateSpec {
	labels := map[string]string{"app.kubernetes.io/name": component, "app.kubernetes.io/part-of": "argus", "argus.io/collector-id": command.GetCollectorId()}
	nonRoot := true
	runAsUser := int64(10001)
	fsGroup := int64(10001)
	isGateway := component == "argus-otelcol-gateway"
	configKey := "agent.yaml"
	if isGateway {
		configKey = "gateway.yaml"
	}
	container := corev1.Container{Name: "collector", Image: image, Args: []string{"--config=/etc/argus-otelcol/" + configKey},
		Env:   []corev1.EnvVar{{Name: "ARGUS_COLLECTOR_ID", Value: command.GetCollectorId()}, {Name: "ARGUS_RESOURCE_ID", Value: command.GetResourceId()}},
		Ports: []corev1.ContainerPort{{Name: "otlp-grpc", ContainerPort: 4317}, {Name: "otlp-http", ContainerPort: 4318}},
		VolumeMounts: []corev1.VolumeMount{{Name: "config", MountPath: "/etc/argus-otelcol", ReadOnly: true},
			{Name: "identity-bootstrap", MountPath: "/var/run/argus-bootstrap/identity", ReadOnly: true},
			{Name: "identity-state", MountPath: "/var/lib/argus-otelcol/identity"}},
		Resources: corev1.ResourceRequirements{}}
	// Secret is bootstrap-only. The identity extension copies it into the
	// writable emptyDir and atomically persists Gateway rotations back through
	// the fixed-name Role above.
	identityMode := int32(0o440)
	volumes := []corev1.Volume{
		{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configName}}}},
		{Name: "identity-bootstrap", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: collectorIdentitySecret, DefaultMode: &identityMode}}},
		{Name: "identity-state", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
	}
	if isGateway {
		container.Env = append(container.Env,
			corev1.EnvVar{Name: "ARGUS_TELEMETRY_KUBERNETES_MIRROR", Value: "1"},
			corev1.EnvVar{Name: "ARGUS_TELEMETRY_KUBERNETES_NAMESPACE", ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"}}})
		container.Ports = append(container.Ports, corev1.ContainerPort{Name: "health", ContainerPort: 13133})
		container.StartupProbe = collectorHealthProbe(60, 2)
		container.ReadinessProbe = collectorHealthProbe(3, 2)
		container.LivenessProbe = collectorHealthProbe(3, 10)
	} else {
		container.Env = append(container.Env, corev1.EnvVar{Name: "K8S_NODE_NAME", ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
		}})
		container.VolumeMounts = append(container.VolumeMounts,
			corev1.VolumeMount{Name: "hostfs", MountPath: "/hostfs", ReadOnly: true},
			corev1.VolumeMount{Name: "podlogs", MountPath: "/var/log/pods", ReadOnly: true},
			corev1.VolumeMount{Name: "kubelet-pki", MountPath: "/var/run/argus-kubelet/pki", ReadOnly: true})
		volumes = append(volumes,
			corev1.Volume{Name: "hostfs", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/"}}},
			corev1.Volume{Name: "podlogs", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/log/pods"}}},
			corev1.Volume{Name: "kubelet-pki", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/var/lib/kubelet/pki"}}})
	}
	imagePullSecrets := make([]corev1.LocalObjectReference, 0, len(command.GetImagePullSecrets()))
	for _, name := range command.GetImagePullSecrets() {
		imagePullSecrets = append(imagePullSecrets, corev1.LocalObjectReference{Name: name})
	}
	return corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels, Annotations: map[string]string{"argus.io/config-sha256": command.GetConfigSha256()}},
		Spec: corev1.PodSpec{ServiceAccountName: "argus-otelcol", SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &nonRoot, RunAsUser: &runAsUser, FSGroup: &fsGroup},
			ImagePullSecrets: imagePullSecrets, Containers: []corev1.Container{container}, Volumes: volumes}}
}

func collectorHealthProbe(failureThreshold int32, periodSeconds int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler:     corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstr.FromString("health")}},
		FailureThreshold: failureThreshold,
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   1,
	}
}

func waitForGateway(ctx context.Context, client kubernetes.Interface) error {
	err := wait.PollUntilContextTimeout(ctx, time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		deployment, err := client.AppsV1().Deployments(KubernetesNamespace).Get(ctx, "argus-otelcol-gateway", metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		generationObserved := deployment.Status.ObservedGeneration >= deployment.Generation
		return generationObserved && deployment.Status.UpdatedReplicas == 1 && deployment.Status.ReadyReplicas == 1 &&
			deployment.Status.AvailableReplicas == 1 && deployment.Status.UnavailableReplicas == 0, nil
	})
	return err
}

func collectorGatewayService(labels map[string]string) *corev1.Service {
	return &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "argus-otelcol-gateway", Namespace: KubernetesNamespace, Labels: labels},
		Spec: corev1.ServiceSpec{Selector: map[string]string{"app.kubernetes.io/name": "argus-otelcol-gateway"}, Ports: []corev1.ServicePort{
			{Name: "otlp-grpc", Port: 4317, TargetPort: intstr.FromString("otlp-grpc")}, {Name: "otlp-http", Port: 4318, TargetPort: intstr.FromString("otlp-http")},
		}}}
}

func copyLabels(value map[string]string) map[string]string {
	result := make(map[string]string, len(value)+1)
	for key, item := range value {
		result[key] = item
	}
	return result
}

func ignoreMissing(err error) error {
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func applyNamespace(ctx context.Context, client kubernetes.Interface, desired *corev1.Namespace) error {
	current, err := client.CoreV1().Namespaces().Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return errors.New("Argus telemetry namespace is missing; rerun the verified Kubernetes Connector installer")
	}
	if err != nil {
		return err
	}
	if current.Labels["app.kubernetes.io/part-of"] != "argus" {
		return errors.New("Argus telemetry namespace ownership label is missing")
	}
	return nil
}

func applyConfigMap(ctx context.Context, client kubernetes.Interface, desired *corev1.ConfigMap) error {
	current, err := client.CoreV1().ConfigMaps(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().ConfigMaps(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	current.Labels, current.Data = desired.Labels, desired.Data
	_, err = client.CoreV1().ConfigMaps(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func applySecret(ctx context.Context, client kubernetes.Interface, desired *corev1.Secret) error {
	current, err := client.CoreV1().Secrets(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().Secrets(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	current.Labels, current.Data = desired.Labels, desired.Data
	_, err = client.CoreV1().Secrets(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func applyServiceAccount(ctx context.Context, client kubernetes.Interface, desired *corev1.ServiceAccount) error {
	current, err := client.CoreV1().ServiceAccounts(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().ServiceAccounts(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	current.Labels = desired.Labels
	_, err = client.CoreV1().ServiceAccounts(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func applyRole(ctx context.Context, client kubernetes.Interface, desired *rbacv1.Role) error {
	current, err := client.RbacV1().Roles(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.RbacV1().Roles(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	current.Labels, current.Rules = desired.Labels, desired.Rules
	_, err = client.RbacV1().Roles(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func applyRoleBinding(ctx context.Context, client kubernetes.Interface, desired *rbacv1.RoleBinding) error {
	current, err := client.RbacV1().RoleBindings(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.RbacV1().RoleBindings(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if current.RoleRef != desired.RoleRef {
		return errors.New("Collector RoleBinding role is immutable")
	}
	current.Labels, current.Subjects = desired.Labels, desired.Subjects
	_, err = client.RbacV1().RoleBindings(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func applyService(ctx context.Context, client kubernetes.Interface, desired *corev1.Service) error {
	current, err := client.CoreV1().Services(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.CoreV1().Services(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	current.Labels, current.Spec.Selector, current.Spec.Ports = desired.Labels, desired.Spec.Selector, desired.Spec.Ports
	_, err = client.CoreV1().Services(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func applyDaemonSet(ctx context.Context, client kubernetes.Interface, desired *appsv1.DaemonSet) error {
	current, err := client.AppsV1().DaemonSets(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.AppsV1().DaemonSets(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	current.Labels, current.Annotations, current.Spec.Template = desired.Labels, desired.Annotations, desired.Spec.Template
	_, err = client.AppsV1().DaemonSets(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}

func applyDeployment(ctx context.Context, client kubernetes.Interface, desired *appsv1.Deployment) error {
	current, err := client.AppsV1().Deployments(desired.Namespace).Get(ctx, desired.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err = client.AppsV1().Deployments(desired.Namespace).Create(ctx, desired, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	current.Labels, current.Annotations, current.Spec.Replicas, current.Spec.Template = desired.Labels, desired.Annotations, desired.Spec.Replicas, desired.Spec.Template
	_, err = client.AppsV1().Deployments(desired.Namespace).Update(ctx, current, metav1.UpdateOptions{})
	return err
}
