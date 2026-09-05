package httpapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestToPlatformPKIStatusProjectsBundleAndNodeCounts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	enterpriseID := uuid.New()
	status := toPlatformPKIStatus([]db.PkiTrustBundle{{
		Epoch: 4, State: "overlapping", BundleSha256: "bundle-sha",
		CurrentCaFingerprints: []string{"old"}, NextCaFingerprints: []string{"new"},
		StartedAt: pgtype.Timestamptz{Time: now, Valid: true},
		RetireAt:  pgtype.Timestamptz{Time: now.Add(time.Hour), Valid: true},
	}}, []db.PkiNodeTrustAck{
		{NodeKind: "connector", NodeID: "connector-1", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, Epoch: 4, Status: "acked", BundleSha256: "bundle-sha", CaFingerprints: []string{"old", "new"}, AcknowledgedAt: pgtype.Timestamptz{Time: now, Valid: true}, UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}},
		{NodeKind: "collector", NodeID: "collector-1", Epoch: 4, Status: "pending", UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}},
		{NodeKind: "collector", NodeID: "collector-2", Epoch: 4, Status: "failed", Error: "invalid bundle", UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}},
		{NodeKind: "kubernetes_connector", NodeID: "connector-2", Epoch: 4, Status: "trust_expired", UpdatedAt: pgtype.Timestamptz{Time: now, Valid: true}},
	})

	if status.AcknowledgedNodes != 1 || status.PendingNodes != 1 || status.FailedNodes != 1 || status.TrustExpiredNodes != 1 {
		t.Fatalf("unexpected node counts: %#v", status)
	}
	if len(status.Bundles) != 1 || status.Bundles[0].RetireAt == nil || len(status.Nodes) != 4 {
		t.Fatalf("unexpected PKI projection: %#v", status)
	}
	if status.Nodes[0].EnterpriseId == nil || uuid.UUID(*status.Nodes[0].EnterpriseId) != enterpriseID || status.Nodes[0].AcknowledgedAt == nil {
		t.Fatalf("acknowledged node metadata was not projected: %#v", status.Nodes[0])
	}
}
