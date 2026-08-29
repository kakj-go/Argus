package remoteaccess

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type RemoteAccessGovernanceReconciler struct {
	Service Service
	Poll    time.Duration
	Batch   int32
	Logger  *slog.Logger
}

func (reconciler RemoteAccessGovernanceReconciler) Run(ctx context.Context) error {
	if reconciler.Service.Store == nil {
		return errors.New("remote access reconciler requires PostgreSQL")
	}
	if reconciler.Poll <= 0 {
		reconciler.Poll = 30 * time.Second
	}
	if reconciler.Batch <= 0 {
		reconciler.Batch = 100
	}
	if reconciler.Logger == nil {
		reconciler.Logger = slog.Default()
	}
	ticker := time.NewTicker(reconciler.Poll)
	defer ticker.Stop()
	for {
		if err := reconciler.reconcile(ctx); err != nil && ctx.Err() == nil {
			reconciler.Logger.Error("remote access governance reconciliation failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (reconciler RemoteAccessGovernanceReconciler) reconcile(ctx context.Context) error {
	if err := reconciler.Service.Store.InTx(ctx, func(q *db.Queries) error {
		sessions, err := q.LockOptionalSessionsMissingRecording(ctx, reconciler.Batch)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			recordingID, err := reconciler.Service.createSessionRecording(ctx, q, session.EnterpriseID, session.ID)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]any{"enterprise_id": session.EnterpriseID, "session_id": session.ID,
				"recording_id": recordingID, "recording_mode": "optional", "status": "recovered"})
			if err != nil {
				return err
			}
			if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.recording.recovered",
				AggregateType: "remote_access_session", AggregateID: session.ID.String(), Payload: payload}); err != nil {
				return err
			}
			if err := appendSystemAuditDetails(ctx, q, session.EnterpriseID, "remote_access.recording.recover", "remote_access_recording", recordingID, map[string]any{
				"status": "recovered", "recording_id": recordingID, "remote_session_id": session.ID,
				"recording_mode": "optional", "authorization_version": session.AuthorizationVersion,
				"snapshot_hash": hex.EncodeToString(session.DecisionSnapshotHash),
			}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconciler.Service.Store.InTx(ctx, func(q *db.Queries) error {
		sessions, err := q.ConvergeInvalidRemoteAccessSessions(ctx, reconciler.Batch)
		if err != nil {
			return err
		}
		if err := reconciler.Service.publishTerminations(ctx, q, sessions, "authorization_invalidated"); err != nil {
			return err
		}
		for _, session := range sessions {
			if err := appendSystemAudit(ctx, q, session.EnterpriseID, "remote_access.authorization.revoked", "remote_access_session", session.ID, "terminating"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconciler.Service.Store.InTx(ctx, func(q *db.Queries) error {
		sessions, err := q.ConvergeStuckTerminatingRemoteAccessSessions(ctx, reconciler.Batch)
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if err := appendSystemAudit(ctx, q, session.EnterpriseID, "remote_access.session.reconcile", "remote_access_session", session.ID, "failed"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconciler.Service.Store.InTx(ctx, func(q *db.Queries) error {
		requests, err := q.ExpirePendingRemoteAccessRequests(ctx, reconciler.Batch)
		if err != nil {
			return err
		}
		for _, request := range requests {
			payload, err := json.Marshal(map[string]any{"enterprise_id": request.EnterpriseID, "request_id": request.ID, "status": "expired"})
			if err != nil {
				return err
			}
			if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.request.expired",
				AggregateType: "remote_access_request", AggregateID: request.ID.String(), Payload: payload}); err != nil {
				return err
			}
			if err := appendSystemAudit(ctx, q, request.EnterpriseID, "remote_access.request.expire", "remote_access_request", request.ID, "expired"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconciler.Service.Store.InTx(ctx, func(q *db.Queries) error {
		items, err := q.EscalateRemoteAccessRequirements(ctx, reconciler.Batch)
		if err != nil {
			return err
		}
		for _, requirement := range items {
			request, err := q.GetRemoteAccessRequestForReconcile(ctx, requirement.RequestID)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]any{"enterprise_id": request.EnterpriseID, "request_id": request.ID,
				"requirement_id": requirement.ID, "escalation_role_ids": requirement.EscalationRoleIds})
			if err != nil {
				return err
			}
			if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.approval.escalated",
				AggregateType: "remote_access_request", AggregateID: request.ID.String(), Payload: payload}); err != nil {
				return err
			}
			if err := appendSystemAudit(ctx, q, request.EnterpriseID, "remote_access.approval.escalate", "remote_access_requirement", requirement.ID, "escalated"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconciler.Service.Store.InTx(ctx, func(q *db.Queries) error {
		recordings, err := q.ExpireRemoteAccessRecordings(ctx, reconciler.Batch)
		if err != nil {
			return err
		}
		for _, recording := range recordings {
			payload, err := json.Marshal(map[string]any{"enterprise_id": recording.EnterpriseID, "recording_id": recording.ID, "session_id": recording.SessionID, "status": "expired"})
			if err != nil {
				return err
			}
			if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.recording.expired", AggregateType: "remote_access_recording", AggregateID: recording.ID.String(), Payload: payload}); err != nil {
				return err
			}
			if err := appendSystemAudit(ctx, q, recording.EnterpriseID, "remote_access.recording.expire", "remote_access_recording", recording.ID, "expired"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	return reconciler.Service.Store.InTx(ctx, func(q *db.Queries) error {
		items, err := q.ExpireRemoteAccessRequirements(ctx, reconciler.Batch)
		if err != nil {
			return err
		}
		for _, requirement := range items {
			request, err := q.GetRemoteAccessRequestForReconcile(ctx, requirement.RequestID)
			if err != nil {
				return err
			}
			if request.Status != "awaiting_approval" {
				continue
			}
			status := "expired"
			if requirement.TimeoutEffect == "reject" {
				status = "rejected"
			}
			request, err = q.UpdateRemoteAccessRequestStatus(ctx, db.UpdateRemoteAccessRequestStatusParams{ID: request.ID,
				EnterpriseID: request.EnterpriseID, Status: status, Status_2: "awaiting_approval"})
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			reason := "approval_timeout_" + requirement.TimeoutEffect
			if err := q.RevokeRemoteAccessLeasesByRequest(ctx, db.RevokeRemoteAccessLeasesByRequestParams{RequestID: request.ID, Reason: text(reason)}); err != nil {
				return err
			}
			sessions, err := q.TerminateRemoteAccessSessionsByRequest(ctx, db.TerminateRemoteAccessSessionsByRequestParams{RequestID: request.ID, Reason: text(reason)})
			if err != nil {
				return err
			}
			if err := reconciler.Service.publishTerminations(ctx, q, sessions, reason); err != nil {
				return err
			}
			payload, err := json.Marshal(map[string]any{"enterprise_id": request.EnterpriseID, "request_id": request.ID,
				"requirement_id": requirement.ID, "timeout_effect": requirement.TimeoutEffect, "status": status})
			if err != nil {
				return err
			}
			if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "remote_access.approval.timed_out",
				AggregateType: "remote_access_request", AggregateID: request.ID.String(), Payload: payload}); err != nil {
				return err
			}
			if err := appendSystemAudit(ctx, q, request.EnterpriseID, "remote_access.approval.timeout", "remote_access_request", request.ID, status); err != nil {
				return err
			}
		}
		return nil
	})
}

func appendSystemAudit(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, actionName, resourceType string, resourceID uuid.UUID, status string) error {
	return appendSystemAuditDetails(ctx, q, enterpriseID, actionName, resourceType, resourceID, map[string]any{"status": status})
}

func appendSystemAuditDetails(ctx context.Context, q *db.Queries, enterpriseID uuid.UUID, actionName, resourceType string, resourceID uuid.UUID, details map[string]any) error {
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true},
		ActorType: "system", ActorID: "argus-worker", Action: actionName, ResourceType: resourceType, ResourceID: resourceID.String(),
		Result: "success", Details: details})
	return err
}
