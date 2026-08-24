package argusdev

import (
	"context"
	"fmt"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestIsPortForwardNotFoundTreatsDeletedServiceAsSuccessfulCleanup(t *testing.T) {
	if !isPortForwardNotFound(fmt.Errorf(`services "argus-e2e-ssh-target" not found`)) {
		t.Fatal("expected deleted Service error to be ignored")
	}
	if isPortForwardNotFound(fmt.Errorf("port-forward timed out")) {
		t.Fatal("unexpectedly ignored a real port-forward failure")
	}
}

func TestCleanupManagedCollectorRBACOnlyDeletesTemporaryNamespaceBindings(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset()
	for _, item := range []struct {
		name      string
		namespace string
	}{
		{name: "argus-otelcol-owned", namespace: "argus-telemetry"},
		{name: "argus-otelcol-formal", namespace: "argus-formal-telemetry"},
	} {
		labels := map[string]string{"app.kubernetes.io/part-of": "argus"}
		if _, err := client.RbacV1().ClusterRoles().Create(ctx, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: item.name, Labels: labels}}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := client.RbacV1().ClusterRoleBindings().Create(ctx, &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: item.name, Labels: labels},
			RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: item.name},
			Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "argus-otelcol", Namespace: item.namespace}},
		}, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	env := &E2EEnvironment{Kube: &E2EKube{Client: client}, ManagedNamespaces: []string{"argus-telemetry"}}
	if err := cleanupManagedCollectorRBAC(ctx, env); err != nil {
		t.Fatal(err)
	}
	if _, err := client.RbacV1().ClusterRoles().Get(ctx, "argus-otelcol-owned", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("temporary Collector ClusterRole was not deleted: %v", err)
	}
	if _, err := client.RbacV1().ClusterRoleBindings().Get(ctx, "argus-otelcol-owned", metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatalf("temporary Collector ClusterRoleBinding was not deleted: %v", err)
	}
	if _, err := client.RbacV1().ClusterRoles().Get(ctx, "argus-otelcol-formal", metav1.GetOptions{}); err != nil {
		t.Fatalf("unrelated Collector ClusterRole was deleted: %v", err)
	}
}

func TestDedicatedClusterConflictsReportsHelmOwner(t *testing.T) {
	ctx := context.Background()
	client := fake.NewSimpleClientset(&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{
		Name: "strimzi-cluster-operator-namespaced",
		Annotations: map[string]string{
			"meta.helm.sh/release-name":      "argus-strimzi",
			"meta.helm.sh/release-namespace": "argus-observability",
		},
	}})
	kube := &E2EKube{Client: client}
	conflicts, err := kube.DedicatedClusterConflicts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 1 {
		t.Fatalf("conflicts = %#v", conflicts)
	}
	want := "ClusterRole/strimzi-cluster-operator-namespaced owned by Helm release argus-strimzi in argus-observability"
	if conflicts[0] != want {
		t.Fatalf("conflict = %q, want %q", conflicts[0], want)
	}
}

func TestDedicatedClusterConflictsAllowsCleanCluster(t *testing.T) {
	kube := &E2EKube{Client: fake.NewSimpleClientset()}
	conflicts, err := kube.DedicatedClusterConflicts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(conflicts) != 0 {
		t.Fatalf("conflicts = %#v", conflicts)
	}
}
