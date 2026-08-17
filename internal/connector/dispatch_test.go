package connector

import (
	"testing"
	"time"

	"github.com/google/uuid"
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
