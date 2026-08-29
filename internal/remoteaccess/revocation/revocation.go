package revocation

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

// Source invalidates every runtime object whose immutable snapshot or direct
// target column references the changed authorization source.
func Source(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, sourceType string, sourceID uuid.UUID, reason string) error {
	affectedUsers, err := q.BumpRemoteAccessGovernanceUsersAuthorizationVersion(ctx, db.BumpRemoteAccessGovernanceUsersAuthorizationVersionParams{
		EnterpriseID: enterpriseID, SourceType: sourceType, SourceID: sourceID,
	})
	if err != nil {
		return err
	}
	if err := q.InvalidateRemoteAccessRequestsByGovernanceSource(ctx, db.InvalidateRemoteAccessRequestsByGovernanceSourceParams{
		EnterpriseID: enterpriseID, SourceType: sourceType, SourceID: sourceID,
	}); err != nil {
		return err
	}
	if err := q.RevokeRemoteAccessLeasesByGovernanceSource(ctx, db.RevokeRemoteAccessLeasesByGovernanceSourceParams{
		Reason: text(reason), EnterpriseID: enterpriseID, SourceType: sourceType, SourceID: sourceID,
	}); err != nil {
		return err
	}
	sessions, err := q.TerminateRemoteAccessSessionsByGovernanceSource(ctx, db.TerminateRemoteAccessSessionsByGovernanceSourceParams{
		Reason: text(reason), EnterpriseID: enterpriseID, SourceType: sourceType, SourceID: sourceID,
	})
	if err != nil {
		return err
	}
	if err := publishTerminations(ctx, q, sessions, reason); err != nil {
		return err
	}
	return publishSummary(ctx, q, enterpriseID, sourceType, sourceID.String(), reason, affectedUsers, len(sessions))
}

// Users applies runtime revocation after the caller has changed the users'
// authoritative authorization versions.
func Users(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, userIDs []uuid.UUID, reason string) error {
	if len(userIDs) == 0 {
		return nil
	}
	if err := q.InvalidateRemoteAccessRequestsByUsers(ctx, db.InvalidateRemoteAccessRequestsByUsersParams{EnterpriseID: enterpriseID, UserIds: userIDs}); err != nil {
		return err
	}
	if err := q.RevokeRemoteAccessLeasesByUsers(ctx, db.RevokeRemoteAccessLeasesByUsersParams{Reason: text(reason), EnterpriseID: enterpriseID, UserIds: userIDs}); err != nil {
		return err
	}
	sessions, err := q.TerminateRemoteAccessSessionsByUsers(ctx, db.TerminateRemoteAccessSessionsByUsersParams{Reason: text(reason), EnterpriseID: enterpriseID, UserIds: userIDs})
	if err != nil {
		return err
	}
	if err := publishTerminations(ctx, q, sessions, reason); err != nil {
		return err
	}
	return publishSummary(ctx, q, enterpriseID, "user", "bulk", reason, userIDs, len(sessions))
}

func publishTerminations(ctx context.Context, q *db.Queries, sessions []db.RemoteAccessSession, reason string) error {
	for _, session := range sessions {
		payload, err := json.Marshal(map[string]any{"session_id": session.ID.String(), "session_fence": session.SessionFence, "reason": reason})
		if err != nil {
			return err
		}
		if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.session.terminate",
			AggregateType: "remote_access_session", AggregateID: session.ID.String(), Payload: payload}); err != nil {
			return err
		}
	}
	return nil
}

func publishSummary(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, sourceType, sourceID, reason string, affectedUsers []uuid.UUID, terminated int) error {
	if len(affectedUsers) == 0 && terminated == 0 {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"enterprise_id": enterpriseID, "source_type": sourceType, "source_id": sourceID,
		"reason": reason, "affected_user_ids": affectedUsers, "terminated_session_count": terminated})
	if err != nil {
		return err
	}
	return q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.authorization.revoked",
		AggregateType: "remote_access_" + sourceType, AggregateID: sourceID, Payload: payload})
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}

func text(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
