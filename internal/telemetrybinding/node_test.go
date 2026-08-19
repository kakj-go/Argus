package telemetrybinding

import (
	"bytes"
	"context"
	"errors"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectNormalizesNodeEvidence(t *testing.T) {
	client := fake.NewClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", UID: types.UID("node-uid-a")},
		Spec: corev1.NodeSpec{ProviderID: "provider://node-a"}, Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: "10.0.0.2"}, {Type: corev1.NodeInternalIP, Address: "10.0.0.1"}, {Type: corev1.NodeInternalIP, Address: "10.0.0.1"}},
			NodeInfo:  corev1.NodeSystemInfo{MachineID: "machine-a", SystemUUID: "system-a"},
		}})
	values, err := Collect(context.Background(), client)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].NodeUID != "node-uid-a" || values[0].NodeName != "node-a" ||
		len(values[0].InternalIPs) != 2 || values[0].InternalIPs[0] != "10.0.0.1" || values[0].InternalIPs[1] != "10.0.0.2" {
		t.Fatalf("unexpected normalized evidence: %#v", values)
	}
}

func TestIdentityHashIgnoresTransientAddressesButDetectsStrongIdentityDrift(t *testing.T) {
	base := NodeEvidence{NodeUID: "node-uid", NodeName: "node-a", ProviderID: "provider://node-a",
		MachineID: "machine-a", SystemUUID: "system-a", InternalIPs: []string{"10.0.0.1"}}
	baseHash, err := identityHash(base)
	if err != nil {
		t.Fatal(err)
	}
	addressesChanged := base
	addressesChanged.InternalIPs = []string{"fd00::1", "10.0.0.2"}
	addressHash, err := identityHash(addressesChanged)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(baseHash[:], addressHash[:]) {
		t.Fatal("transient Node addresses must not invalidate a manually verified identity binding")
	}
	identityChanged := base
	identityChanged.SystemUUID = "system-b"
	driftedHash, err := identityHash(identityChanged)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(baseHash[:], driftedHash[:]) {
		t.Fatal("strong Node identity drift must invalidate a manually verified binding")
	}
}

func TestValidateRejectsEmptyDuplicateAndInvalidEvidence(t *testing.T) {
	valid := NodeEvidence{NodeUID: "node-uid", NodeName: "node", InternalIPs: []string{"10.0.0.1"}}
	for name, values := range map[string][]NodeEvidence{
		"empty":       nil,
		"duplicate":   {valid, valid},
		"missing_uid": {{NodeName: "node"}},
		"invalid_ip":  {{NodeUID: "node-uid", NodeName: "node", InternalIPs: []string{"not-an-ip"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := Validate(values); !errors.Is(err, ErrInvalidNodeEvidence) {
				t.Fatalf("expected invalid evidence, got %v", err)
			}
		})
	}
}
