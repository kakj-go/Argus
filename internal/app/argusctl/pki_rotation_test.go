package argusctl

import (
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRotationPartitionsSinglePurposeLeaves(t *testing.T) {
	certificates := []argusCertificate{
		{Namespace: "argus-system", Name: "gateway", Usage: "server auth"},
		{Namespace: "argus-system", Name: "gateway-client", Usage: "client auth"},
	}
	servers, clients := partitionCertificates(certificates)
	if len(servers) != 1 || servers[0].Name != "gateway" || len(clients) != 1 || clients[0].Name != "gateway-client" {
		t.Fatalf("unexpected partition: servers=%#v clients=%#v", servers, clients)
	}
}

func TestExistingIssuerRotationRequiresDistinctNextIssuer(t *testing.T) {
	servers := []argusCertificate{{Namespace: "argus-system", Name: "gateway", Usage: "server auth", IssuerName: "customer-next"}}
	if err := requireDistinctExistingRotationIssuer(servers, "customer-next"); err == nil {
		t.Fatal("in-place customer ClusterIssuer mutation was accepted")
	}
	servers[0].IssuerName = "customer-former"
	if err := requireDistinctExistingRotationIssuer(servers, "customer-next"); err != nil {
		t.Fatalf("distinct next ClusterIssuer rejected: %v", err)
	}
}

func TestStagedServerCertificateNameFitsKubernetesLimit(t *testing.T) {
	name := stagedServerCertificateName(strings.Repeat("a", 253), 42)
	if len(name) > 253 || !strings.HasSuffix(name, "-next-42") {
		t.Fatalf("invalid staged Certificate name %q (%d bytes)", name, len(name))
	}
}

func TestRuntimeIssuerRolloutTargetsOnlyConfigConsumers(t *testing.T) {
	consumer := &appsv1.Deployment{Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
		EnvFrom: []corev1.EnvFromSource{{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "argus-runtime-config"}}}},
	}}}}}}
	if !deploymentReferencesConfigMap(consumer, "argus-runtime-config") {
		t.Fatal("runtime ConfigMap consumer was not selected for issuer rollout")
	}
	if deploymentReferencesConfigMap(&appsv1.Deployment{}, "argus-runtime-config") {
		t.Fatal("unrelated Deployment was selected for issuer rollout")
	}
	for _, name := range []string{"argus-server", "argus-connector-gateway", "argus-worker", "argus-worker-agent"} {
		if !runtimeIssuerWorkload(name) {
			t.Fatalf("identity issuer workload %s was not selected", name)
		}
	}
	if runtimeIssuerWorkload("argus-direct-executor") {
		t.Fatal("non-issuer Direct Executor was selected for issuer rollout")
	}
}

func TestSetContainerEnvReplacesIssuerWithoutDuplicates(t *testing.T) {
	container := &corev1.Container{Env: []corev1.EnvVar{{Name: "ARGUS_TELEMETRY_ISSUER_NAME", Value: "former"}}}
	setContainerEnv(container, "ARGUS_TELEMETRY_ISSUER_NAME", "next")
	setContainerEnv(container, "ARGUS_TELEMETRY_ISSUER_GENERATION", "2")
	if len(container.Env) != 2 || container.Env[0].Value != "next" || container.Env[1].Value != "2" {
		t.Fatalf("runtime issuer environment was not replaced atomically: %#v", container.Env)
	}
}

func TestControlPlaneNodeIDUsesOnlyCurrentReadyPod(t *testing.T) {
	ready := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "argus-worker-current", Labels: map[string]string{"app.kubernetes.io/name": "argus-worker"}},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Args: []string{"--pool=agent"}}}},
		Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
	}
	if id, ok := controlPlaneNodeIDForPod(&ready); !ok || id != "worker-agent/argus-worker-current" {
		t.Fatalf("unexpected Ready worker identity %q, ok=%v", id, ok)
	}
	notReady := ready.DeepCopy()
	notReady.Status.Conditions[0].Status = corev1.ConditionFalse
	if _, ok := controlPlaneNodeIDForPod(notReady); ok {
		t.Fatal("non-Ready Pod was treated as an active control-plane identity")
	}
	terminating := ready.DeepCopy()
	now := metav1.NewTime(time.Now())
	terminating.DeletionTimestamp = &now
	if _, ok := controlPlaneNodeIDForPod(terminating); ok {
		t.Fatal("terminating Pod was treated as an active control-plane identity")
	}
	unrelated := ready.DeepCopy()
	unrelated.Labels["app.kubernetes.io/name"] = "argus-web"
	if _, ok := controlPlaneNodeIDForPod(unrelated); ok {
		t.Fatal("unrelated Ready Pod was treated as a Bundle acknowledger")
	}
}
