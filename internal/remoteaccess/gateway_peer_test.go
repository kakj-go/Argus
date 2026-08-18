package remoteaccess

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type fakeRouteResolver struct {
	owner      string
	savedOwner string
	deleted    bool
	resolveErr error
	persistErr error
}

func (resolver *fakeRouteResolver) Resolve(context.Context, uuid.UUID, int64) (string, error) {
	return resolver.owner, resolver.resolveErr
}
func (resolver *fakeRouteResolver) SaveRoute(_ context.Context, _ ConnectionTarget, owner string) error {
	resolver.savedOwner = owner
	return resolver.persistErr
}
func (resolver *fakeRouteResolver) DeleteRoute(context.Context, ConnectionTarget) {
	resolver.deleted = true
}

type fakeBackendFactory struct {
	opened bool
	value  *fakeBackendSession
}

func (factory *fakeBackendFactory) Open(context.Context, ConnectionTarget, int, int) (BackendSession, error) {
	factory.opened = true
	if factory.value == nil {
		factory.value = &fakeBackendSession{}
	}
	return factory.value, nil
}

type fakePeerOpener struct {
	owner  string
	opened bool
	value  *fakeBackendSession
}

func (opener *fakePeerOpener) Open(_ context.Context, owner string, _ ConnectionTarget, _, _ int) (BackendSession, error) {
	opener.owner, opener.opened = owner, true
	if opener.value == nil {
		opener.value = &fakeBackendSession{}
	}
	return opener.value, nil
}

type fakeBackendSession struct{ closed bool }

func (*fakeBackendSession) Send(context.Context, ClientFrame) error { return nil }
func (*fakeBackendSession) Receive(context.Context) (BackendFrame, error) {
	return BackendFrame{}, io.EOF
}
func (session *fakeBackendSession) Close(context.Context, string) error {
	session.closed = true
	return nil
}

func TestDistributedConnectorFactoryForwardsToOwnerAndPersistsWSSRoute(t *testing.T) {
	resolver := &fakeRouteResolver{owner: "gateway-b"}
	local, peer := &fakeBackendFactory{}, &fakePeerOpener{}
	factory := DistributedConnectorFactory{InstanceID: "gateway-a", Local: local, Resolver: resolver, Peers: peer}
	target := ConnectionTarget{Session: db.RemoteAccessSession{ID: uuid.New(), SessionFence: 3}, ConnectorID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, ConnectionEpoch: 9, LeaseExpiresAt: time.Now().Add(time.Minute)}
	session, err := factory.Open(context.Background(), target, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if local.opened || !peer.opened || peer.owner != "gateway-b" {
		t.Fatalf("unexpected route local=%v peer=%v owner=%q", local.opened, peer.opened, peer.owner)
	}
	if resolver.savedOwner != "gateway-a" {
		t.Fatalf("route must point at WSS owner, got %q", resolver.savedOwner)
	}
	if err := session.Close(context.Background(), "done"); err != nil || !resolver.deleted || !peer.value.closed {
		t.Fatalf("route cleanup failed: err=%v deleted=%v closed=%v", err, resolver.deleted, peer.value.closed)
	}
}

func TestDistributedConnectorFactoryUsesLocalOwner(t *testing.T) {
	resolver := &fakeRouteResolver{owner: "gateway-a"}
	local, peer := &fakeBackendFactory{}, &fakePeerOpener{}
	factory := DistributedConnectorFactory{InstanceID: "gateway-a", Local: local, Resolver: resolver, Peers: peer}
	target := ConnectionTarget{Session: db.RemoteAccessSession{ID: uuid.New(), SessionFence: 1}, ConnectorID: uuid.NullUUID{UUID: uuid.New(), Valid: true}, ConnectionEpoch: 2, LeaseExpiresAt: time.Now().Add(time.Minute)}
	if _, err := factory.Open(context.Background(), target, 80, 24); err != nil {
		t.Fatal(err)
	}
	if !local.opened || peer.opened {
		t.Fatalf("local owner routing mismatch local=%v peer=%v", local.opened, peer.opened)
	}
}

func TestKubernetesGatewayPeerResolverUsesReadyGatewayPodIP(t *testing.T) {
	pod := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod",
		"metadata": map[string]any{"name": "gateway-b", "namespace": "argus-system", "labels": map[string]any{"app.kubernetes.io/name": "argus-connector-gateway"}},
		"status":   map[string]any{"phase": "Running", "podIP": "10.42.0.17", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}},
	}}
	resolver := KubernetesGatewayPeerResolver{Client: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), pod), Namespace: "argus-system"}
	endpoint, err := resolver.ResolveEndpoint(context.Background(), "gateway-b", "9446")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "10.42.0.17:9446" {
		t.Fatalf("unexpected Gateway peer endpoint %q", endpoint)
	}
}
