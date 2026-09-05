package connector

import (
	"context"
	"testing"
	"time"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func validTunnelDesiredForTest(id string) *connectorv1.TelemetryTunnelDesired {
	return &connectorv1.TelemetryTunnelDesired{
		TunnelId: id, CollectorId: "collector", Epoch: 1, Fence: 1,
		TargetAddress: "127.0.0.1", TargetPort: 22, TargetUsername: "argus",
		PinnedHostKey: "SHA256:test", LoopbackPort: 4317, ForwardTarget: "127.0.0.1:4317",
		IdentityLoopbackPort: 4318, IdentityForwardTarget: "127.0.0.1:4318",
		CredentialLeaseId: "lease", LeaseExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}
}

func TestValidateTunnelDesiredFailsClosed(t *testing.T) {
	valid := validTunnelDesiredForTest("tunnel")
	if err := validateTunnelDesired(valid); err != nil {
		t.Fatalf("valid desired tunnel rejected: %v", err)
	}
	valid.PinnedHostKey = ""
	if err := validateTunnelDesired(valid); err == nil {
		t.Fatal("desired tunnel without a pinned host key was accepted")
	}
}

func TestMemberTunnelSnapshotClosesOmittedTunnel(t *testing.T) {
	supervisor := newMemberTunnelSupervisor(1, 1024*1024)
	ctx, cancel := context.WithCancel(context.Background())
	entry := &memberTunnel{desired: validTunnelDesiredForTest("tunnel"), credential: []byte("secret"), cancel: cancel, status: "establishing"}
	supervisor.tunnels["tunnel"] = entry
	supervisor.ReconcileSnapshot(nil)
	if supervisor.Has("tunnel", 1, 1) {
		t.Fatal("omitted tunnel remained active after a full desired snapshot")
	}
	if ctx.Err() == nil {
		t.Fatal("omitted tunnel context was not cancelled")
	}
	for _, value := range entry.credential {
		if value != 0 {
			t.Fatal("omitted tunnel credential was not cleared")
		}
	}
}
