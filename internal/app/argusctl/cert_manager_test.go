package argusctl

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCertManagerVersionCompatible(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		actual string
		want   bool
	}{{"1.20.3", true}, {"v1.20.4", true}, {"1.20.2", false}, {"1.19.9", false}, {"1.21.0", false}, {"", false}} {
		if got := certManagerVersionCompatible(test.actual, certManagerVersion); got != test.want {
			t.Fatalf("compatibility for %q = %v, want %v", test.actual, got, test.want)
		}
	}
}

func TestCertManagerDeploymentVersion(t *testing.T) {
	t.Parallel()
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/version": "v1.20.3"}}}
	if got := certManagerDeploymentVersion(deployment); got != "1.20.3" {
		t.Fatalf("label version = %q", got)
	}
	deployment.Labels = nil
	deployment.Spec.Template.Spec.Containers = []corev1.Container{{Name: "cert-manager-controller", Image: "quay.io/jetstack/cert-manager-controller:v1.20.4"}}
	if got := certManagerDeploymentVersion(deployment); got != "1.20.4" {
		t.Fatalf("image version = %q", got)
	}
}
