package connector

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
)

func TestDispatchHubRoutesOnlyToOwningConnector(t *testing.T) {
	hub := NewDispatchHub()
	firstID, secondID := uuid.New(), uuid.New()
	first, unregisterFirst := hub.Register(firstID)
	defer unregisterFirst()
	second, unregisterSecond := hub.Register(secondID)
	defer unregisterSecond()
	hub.Notify(firstID)
	select {
	case <-first:
	case <-time.After(time.Second):
		t.Fatal("owner did not receive dispatch wakeup")
	}
	select {
	case <-second:
		t.Fatal("dispatch wakeup leaked to another Connector")
	default:
	}
}

func TestConnectorUninstallRequiresCompleteTypedResult(t *testing.T) {
	valid, err := anypb.New(&connectorv1.ConnectorUninstallResult{IdentityRemoved: true, ServiceStopped: true})
	if err != nil {
		t.Fatal(err)
	}
	if !validConnectorUninstallResult(valid) {
		t.Fatal("complete Connector uninstall result was rejected")
	}
	for _, result := range []*connectorv1.ConnectorUninstallResult{
		{IdentityRemoved: true},
		{ServiceStopped: true},
		{},
	} {
		encoded, err := anypb.New(result)
		if err != nil {
			t.Fatal(err)
		}
		if validConnectorUninstallResult(encoded) {
			t.Fatal("partial Connector uninstall result was accepted")
		}
	}
}

func TestConnectorUninstallReconcileStaysResultUnknown(t *testing.T) {
	if got := reconciledCommandStatus("connector_uninstall", "succeeded"); got != "result_unknown" {
		t.Fatalf("unverifiable uninstall reconciliation became %q", got)
	}
	if got := reconciledCommandStatus("host_connection_probe", "succeeded"); got != "succeeded" {
		t.Fatalf("read-only probe reconciliation changed to %q", got)
	}
}

func TestHostProbeProjectionPreservesTargetArchitecture(t *testing.T) {
	result := hostProbeConnectionTestResult(&connectorv1.HostConnectionProbeResult{
		ResolvedIps: []string{"10.20.30.40"}, HostKeyFingerprint: "SHA256:test",
		RemoteVersion: "SSH-2.0-test", LatencyMillis: 17, Architecture: "arm64",
	})
	if result.Architecture != "arm64" || result.LatencyMS != 17 || result.HostKeyFingerprint != "SHA256:test" {
		t.Fatalf("Connector Host probe projection lost frozen fields: %+v", result)
	}
}

func TestCollectorManagementFailureDoesNotRequireSuccessProjection(t *testing.T) {
	collectorID := uuid.NewString()
	payload, _ := json.Marshal(map[string]any{"collector_id": collectorID, "operation": "install", "desired_revision": 1, "config_sha256": "hash"})
	if _, _, err := validateCollectorManagementOutcome(payload, "failed", json.RawMessage(`{}`)); err != nil {
		t.Fatalf("explicit Collector failure was treated as unknown: %v", err)
	}
	if _, _, err := validateCollectorManagementOutcome(payload, "succeeded", json.RawMessage(`{}`)); err == nil {
		t.Fatal("Collector success without a verifiable projection was accepted")
	}
}

func TestCollectorManagementSuccessAcceptsStoredTypedProjection(t *testing.T) {
	collectorID := uuid.NewString()
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&connectorv1.CollectorManagementCommand{
		CollectorId: collectorID, Operation: "install", DesiredRevision: 2, ConfigSha256: "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	typed, err := anypb.New(&connectorv1.CollectorManagementResult{
		CollectorId: collectorID, EffectiveRevision: 2, AppliedConfigSha256: "abc123", Status: "converged",
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := marshalTypedResult(typed)
	if err != nil {
		t.Fatal(err)
	}
	if json.Valid(stored) == false || string(stored) == "{}" {
		t.Fatalf("stored Collector projection is invalid: %s", stored)
	}
	request, _, err := validateCollectorManagementOutcome(payload, "succeeded", stored)
	if err != nil {
		t.Fatalf("stored Collector success projection was rejected: %v; projection=%s", err, stored)
	}
	if request.Transport != "" {
		t.Fatalf("unexpected default route transport %q", request.Transport)
	}
}

func TestCollectorManagementOutcomePreservesTunnelTransport(t *testing.T) {
	collectorID := uuid.NewString()
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&connectorv1.CollectorManagementCommand{
		CollectorId: collectorID, Operation: "install", DesiredRevision: 2,
		ConfigSha256: "abc123", Transport: "executor_tunnel",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&connectorv1.CollectorManagementResult{
		CollectorId: collectorID, EffectiveRevision: 2, AppliedConfigSha256: "abc123", Status: "converged",
	})
	if err != nil {
		t.Fatal(err)
	}
	request, _, err := validateCollectorManagementOutcome(payload, "succeeded", result)
	if err != nil || request.Transport != "executor_tunnel" {
		t.Fatalf("transport projection = %q, err=%v", request.Transport, err)
	}
}

func TestKubernetesCollectorManagementRequiresBoundedNodeEvidence(t *testing.T) {
	collectorID, clusterID := uuid.NewString(), uuid.NewString()
	payload, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&connectorv1.CollectorManagementCommand{
		CollectorId: collectorID, Operation: "install", ResourceId: clusterID, ResourceType: "kubernetes_cluster",
		DesiredRevision: 2, ConfigSha256: "abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	result := &connectorv1.CollectorManagementResult{CollectorId: collectorID, EffectiveRevision: 2,
		AppliedConfigSha256: "abc123", Status: "converged"}
	encode := func() json.RawMessage {
		value, encodeErr := protojson.MarshalOptions{UseProtoNames: true}.Marshal(result)
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}
		return value
	}
	if _, _, err = validateCollectorManagementOutcome(payload, "succeeded", encode()); err == nil {
		t.Fatal("Kubernetes Collector success without Node evidence was accepted")
	}
	result.KubernetesNodes = []*connectorv1.KubernetesNodeEvidence{{NodeUid: "node-uid", NodeName: "node-a", InternalIps: []string{"10.0.0.1"}}}
	if _, _, err = validateCollectorManagementOutcome(payload, "succeeded", encode()); err != nil {
		t.Fatalf("valid Kubernetes Node evidence was rejected: %v", err)
	}
	result.KubernetesNodes = append(result.KubernetesNodes, result.KubernetesNodes[0])
	if _, _, err = validateCollectorManagementOutcome(payload, "succeeded", encode()); err == nil {
		t.Fatal("duplicate Kubernetes Node evidence was accepted")
	}
}

func TestHostCollectorManagementRejectsNodeEvidence(t *testing.T) {
	collectorID := uuid.NewString()
	payload, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&connectorv1.CollectorManagementCommand{
		CollectorId: collectorID, Operation: "install", ResourceId: uuid.NewString(), ResourceType: "host",
		DesiredRevision: 1, ConfigSha256: "abc123",
	})
	result, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&connectorv1.CollectorManagementResult{
		CollectorId: collectorID, EffectiveRevision: 1, AppliedConfigSha256: "abc123", Status: "converged",
		KubernetesNodes: []*connectorv1.KubernetesNodeEvidence{{NodeUid: "node-uid", NodeName: "node-a", InternalIps: []string{"10.0.0.1"}}},
	})
	if _, _, err := validateCollectorManagementOutcome(payload, "succeeded", result); err == nil {
		t.Fatal("Host Collector result carrying Kubernetes Node evidence was accepted")
	}
}

func TestDispatchHubUnregisterAndCoalesceNotifications(t *testing.T) {
	hub := NewDispatchHub()
	connectorID := uuid.New()
	wakeup, unregister := hub.Register(connectorID)
	hub.Notify(connectorID)
	hub.Notify(connectorID)
	select {
	case <-wakeup:
	case <-time.After(time.Second):
		t.Fatal("registered Connector did not receive dispatch wakeup")
	}
	select {
	case <-wakeup:
		t.Fatal("duplicate dispatch wakeups were not coalesced")
	default:
	}
	unregister()
	hub.Notify(connectorID)
	select {
	case <-wakeup:
		t.Fatal("unregistered Connector received dispatch wakeup")
	default:
	}
}

func TestRemoveExpiredActiveCommandsReleasesInflightSlots(t *testing.T) {
	now := time.Now()
	active := map[string]activeCommand{
		"expired": {expiresAt: now.Add(-time.Second)},
		"current": {acknowledged: true, expiresAt: now.Add(time.Minute)},
	}
	acknowledgements := map[uint64]string{2: "expired", 3: "current"}
	removeExpiredActiveCommands(now, active, acknowledgements)
	if _, ok := active["expired"]; ok {
		t.Fatal("expired command still occupied an inflight slot")
	}
	if _, ok := acknowledgements[2]; ok {
		t.Fatal("expired command still had a pending acknowledgement")
	}
	if !active["current"].acknowledged || acknowledgements[3] != "current" {
		t.Fatal("current command state was changed")
	}
}

func TestConnectorCommandStateMachineRejectsUnknownReplay(t *testing.T) {
	if !allowedCommandTransition("queued", "dispatched") || !allowedCommandTransition("running", "result_unknown") {
		t.Fatal("expected documented Connector command transitions")
	}
	for _, transition := range [][2]string{{"timed_out", "succeeded"}, {"expired", "dispatched"}, {"result_unknown", "running"}} {
		if allowedCommandTransition(transition[0], transition[1]) {
			t.Fatalf("unsafe transition %s -> %s was accepted", transition[0], transition[1])
		}
	}
}
