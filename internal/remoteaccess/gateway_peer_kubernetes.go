package remoteaccess

import (
	"context"
	"errors"
	"net"
	"net/netip"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var podsResource = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

type KubernetesGatewayPeerResolver struct {
	Client    dynamic.Interface
	Namespace string
}

func (resolver KubernetesGatewayPeerResolver) ResolveEndpoint(ctx context.Context, owner, port string) (string, error) {
	if resolver.Client == nil || resolver.Namespace == "" || owner == "" || port == "" {
		return "", ErrSessionUnavailable
	}
	pod, err := resolver.Client.Resource(podsResource).Namespace(resolver.Namespace).Get(ctx, owner, metav1.GetOptions{})
	if err != nil || pod.GetDeletionTimestamp() != nil || pod.GetLabels()["app.kubernetes.io/name"] != "argus-connector-gateway" {
		return "", ErrSessionUnavailable
	}
	phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
	address, _, _ := unstructured.NestedString(pod.Object, "status", "podIP")
	if phase != "Running" || !podReady(pod) {
		return "", ErrSessionUnavailable
	}
	ip, err := netip.ParseAddr(address)
	if err != nil || ip.IsUnspecified() {
		return "", errors.New("Gateway peer Pod has no valid address")
	}
	return net.JoinHostPort(ip.String(), port), nil
}

func podReady(pod *unstructured.Unstructured) bool {
	conditions, _, _ := unstructured.NestedSlice(pod.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if ok && condition["type"] == "Ready" && condition["status"] == "True" {
			return true
		}
	}
	return false
}
