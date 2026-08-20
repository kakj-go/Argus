package remoteaccess

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type ConnectionTarget struct {
	Session           db.RemoteAccessSession
	EnterpriseID      uuid.UUID
	UserID            uuid.UUID
	HostID            uuid.UUID
	ManagedAccountID  uuid.UUID
	CredentialLeaseID uuid.UUID
	ConnectionMode    string
	ConnectorID       uuid.NullUUID
	ConnectionEpoch   int64
	Protocol          string
	Address           string
	Hostname          string
	Port              int32
	PinnedHostKey     string
	Username          string
	IdleTimeout       time.Duration
	MaxDuration       time.Duration
	LeaseExpiresAt    time.Time
}

type GatewayService struct {
	Store             *postgres.Store
	Credentials       secret.Service
	InstanceID        string
	DirectRecipientID string
	Now               func() time.Time
}

type GatewayRecording struct {
	Record   db.RemoteAccessRecording
	Recorder *Recorder
	Started  time.Time
}

func (service GatewayService) AuthorizeConnection(ctx context.Context, sessionID uuid.UUID, rawTicket string) (ConnectionTarget, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(rawTicket)
	if err != nil || len(decoded) != 32 || sessionID == uuid.Nil {
		return ConnectionTarget{}, ErrTicketBinding
	}
	hash := sha256.Sum256(decoded)
	clear(decoded)
	var result ConnectionTarget
	err = service.Store.InTx(ctx, func(q *db.Queries) error {
		now := service.now()
		ticket, err := q.ConsumeRemoteAccessTicketForGateway(ctx, db.ConsumeRemoteAccessTicketForGatewayParams{TicketHash: hash[:], SessionID: sessionID, Now: timestamp(now)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTicketConsumed
		}
		if err != nil {
			return err
		}
		target, err := q.GetRemoteAccessSessionTarget(ctx, sessionID)
		if err != nil || target.SessionFence != ticket.SessionFence || target.AuthorizationVersion != ticket.AuthorizationVersion {
			return ErrTicketBinding
		}
		recipientType, recipientID := "direct_executor", service.DirectRecipientID
		connectionEpoch := int64(0)
		if target.ConnectionMode == "via_bastion" || target.ConnectionMode == "connector_local" {
			if !target.ConnectorID.Valid || !target.ConnectionEpoch.Valid {
				return ErrSessionUnavailable
			}
			recipientType, recipientID, connectionEpoch = "connector", target.ConnectorID.UUID.String(), target.ConnectionEpoch.Int64
		}
		if recipientID == "" || service.InstanceID == "" {
			return ErrSessionUnavailable
		}
		lease, err := service.Credentials.PrepareLeaseWithQueries(secret.WithActorType(ctx, "remote_access_gateway"), q, service.InstanceID, target.EnterpriseID,
			secret.LeaseRequest{CredentialID: target.CredentialID, OperationRef: target.ID.String(), TargetResourceType: "remote_access_session",
				TargetResourceID: target.ID, RecipientType: recipientType, RecipientID: recipientID, Protocol: accountProtocol(target.Protocol), TTL: 5 * time.Minute})
		if err != nil {
			return err
		}
		session, err := q.MarkRemoteAccessSessionConnecting(ctx, db.MarkRemoteAccessSessionConnectingParams{ID: target.ID, SessionFence: target.SessionFence})
		if err != nil {
			return err
		}
		result = ConnectionTarget{Session: session, EnterpriseID: target.EnterpriseID, UserID: target.UserID, HostID: target.HostID,
			ManagedAccountID: target.ManagedAccountID, CredentialLeaseID: lease.ID, ConnectionMode: target.ConnectionMode, ConnectorID: target.ConnectorID,
			ConnectionEpoch: connectionEpoch, Protocol: target.Protocol, Address: target.Address, Hostname: target.Hostname, Port: target.Port,
			PinnedHostKey: target.PinnedHostKey, Username: target.Username, IdleTimeout: time.Duration(target.IdleTimeoutSeconds) * time.Second,
			MaxDuration: time.Duration(target.MaxDurationSeconds) * time.Second, LeaseExpiresAt: target.LeaseExpiresAt.Time}
		return nil
	})
	return result, err
}

// ResolvePeerTarget reconstructs a Connector target on the Gateway replica
// that owns the Connector stream. The peer supplies only identifiers created
// by the ticket-consuming Gateway; every binding is checked against durable
// PostgreSQL facts before the local Connector stream is opened.
func (service GatewayService) ResolvePeerTarget(ctx context.Context, sessionID uuid.UUID, fence int64, connectorID uuid.UUID, epoch int64, credentialLeaseID uuid.UUID) (ConnectionTarget, error) {
	if service.Store == nil || sessionID == uuid.Nil || fence < 1 || connectorID == uuid.Nil || epoch < 1 || credentialLeaseID == uuid.Nil {
		return ConnectionTarget{}, ErrSessionUnavailable
	}
	target, err := service.Store.Queries.GetRemoteAccessSessionTarget(ctx, sessionID)
	if err != nil || (target.Status != "connecting" && target.Status != "active") || target.SessionFence != fence ||
		!target.ConnectorID.Valid || target.ConnectorID.UUID != connectorID || !target.ConnectionEpoch.Valid || target.ConnectionEpoch.Int64 != epoch {
		return ConnectionTarget{}, ErrSessionUnavailable
	}
	lease, err := service.Store.Queries.GetCredentialLease(ctx, db.GetCredentialLeaseParams{ID: credentialLeaseID, EnterpriseID: target.EnterpriseID})
	now := service.now()
	if err != nil || lease.Status != "active" || !lease.ExpiresAt.Valid || !now.Before(lease.ExpiresAt.Time) ||
		lease.OperationRef != sessionID.String() || lease.TargetResourceType != "remote_access_session" || lease.TargetResourceID != sessionID ||
		lease.RecipientType != "connector" || lease.RecipientID != connectorID.String() || lease.Protocol != accountProtocol(target.Protocol) {
		return ConnectionTarget{}, ErrSessionUnavailable
	}
	return connectionTarget(target, credentialLeaseID), nil
}

func connectionTarget(target db.GetRemoteAccessSessionTargetRow, credentialLeaseID uuid.UUID) ConnectionTarget {
	return ConnectionTarget{Session: db.RemoteAccessSession{ID: target.ID, EnterpriseID: target.EnterpriseID, UserID: target.UserID,
		HttpSessionID: target.HttpSessionID, LeaseID: target.LeaseID, HostID: target.HostID, ManagedAccountID: target.ManagedAccountID,
		Protocol: target.Protocol, ConnectionMode: target.ConnectionMode, ConnectorID: target.ConnectorID, ConnectorEpoch: target.ConnectorEpoch,
		Status: target.Status, SessionFence: target.SessionFence, AuthorizationVersion: target.AuthorizationVersion,
		IdleTimeoutSeconds: target.IdleTimeoutSeconds, MaxDurationSeconds: target.MaxDurationSeconds, ConnectBefore: target.ConnectBefore,
		ConnectedAt: target.ConnectedAt, TerminatedAt: target.TerminatedAt, TerminationReason: target.TerminationReason,
		CreatedAt: target.CreatedAt, UpdatedAt: target.UpdatedAt}, EnterpriseID: target.EnterpriseID, UserID: target.UserID,
		HostID: target.HostID, ManagedAccountID: target.ManagedAccountID, CredentialLeaseID: credentialLeaseID,
		ConnectionMode: target.ConnectionMode, ConnectorID: target.ConnectorID, ConnectionEpoch: target.ConnectionEpoch.Int64,
		Protocol: target.Protocol, Address: target.Address, Hostname: target.Hostname, Port: target.Port, PinnedHostKey: target.PinnedHostKey,
		Username: target.Username, IdleTimeout: time.Duration(target.IdleTimeoutSeconds) * time.Second,
		MaxDuration: time.Duration(target.MaxDurationSeconds) * time.Second, LeaseExpiresAt: target.LeaseExpiresAt.Time}
}

func (service GatewayService) MarkActive(ctx context.Context, sessionID uuid.UUID, fence int64) error {
	_, err := service.Store.Queries.MarkRemoteAccessSessionActive(ctx, db.MarkRemoteAccessSessionActiveParams{ID: sessionID, SessionFence: fence})
	return err
}

func (service GatewayService) CheckActive(ctx context.Context, sessionID uuid.UUID, fence, authorizationVersion int64) error {
	target, err := service.Store.Queries.GetRemoteAccessSessionTarget(ctx, sessionID)
	if err != nil || target.Status != "active" || target.SessionFence != fence || target.AuthorizationVersion != authorizationVersion {
		return ErrAuthorizationStale
	}
	current, err := service.Store.Queries.GetCurrentRemoteAccessAuthorizationVersion(ctx, db.GetCurrentRemoteAccessAuthorizationVersionParams{
		UserID: target.UserID, EnterpriseID: target.EnterpriseID})
	if err != nil || current != authorizationVersion {
		return ErrAuthorizationStale
	}
	return nil
}

func (service GatewayService) OpenRecording(ctx context.Context, sessionID uuid.UUID, objects ObjectStore) (GatewayRecording, error) {
	record, err := service.Store.Queries.GetRemoteAccessRecordingForGateway(ctx, sessionID)
	if err != nil {
		return GatewayRecording{}, err
	}
	var envelope secret.Envelope
	if json.Unmarshal(record.WrappedDek, &envelope) != nil {
		return GatewayRecording{}, ErrRecordingUnavailable
	}
	dek, err := service.Credentials.Keyring.DecryptContext(ctx, envelope, recordingKeyAAD(record.EnterpriseID, record.ID, record.SessionID))
	if err != nil || len(dek) != 32 {
		clear(dek)
		return GatewayRecording{}, ErrRecordingUnavailable
	}
	now := service.now()
	return GatewayRecording{Record: record, Started: now, Recorder: &Recorder{Store: objects, RecordingID: record.ID.String(), DEK: dek, Now: service.Now}}, nil
}

func (service GatewayService) PersistChunks(ctx context.Context, recording GatewayRecording, chunks []ChunkMetadata) error {
	for _, chunk := range chunks {
		err := service.Store.InTx(ctx, func(q *db.Queries) error {
			if _, err := q.CreateRemoteAccessRecordingChunk(ctx, db.CreateRemoteAccessRecordingChunkParams{RecordingID: recording.Record.ID,
				Sequence: int64(chunk.Sequence), ObjectKey: chunk.ObjectKey, Nonce: chunk.Nonce, CiphertextSize: int64(chunk.CipherBytes),
				EventCount: int32(chunk.EventCount), StartedAt: timestamp(chunk.StartedAt), EndedAt: timestamp(chunk.EndedAt),
				PreviousHash: chunk.PreviousHash[:], ChunkHash: chunk.Hash[:]}); err != nil {
				return err
			}
			_, err := q.AdvanceRemoteAccessRecording(ctx, db.AdvanceRemoteAccessRecordingParams{ID: recording.Record.ID,
				ChunkCount: int32(chunk.Sequence), EventCount: int64(chunk.EventCount), SizeBytes: int64(chunk.CipherBytes),
				DurationMs: chunk.EndedAt.Sub(recording.Started).Milliseconds(), FinalHash: chunk.Hash[:]})
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (service GatewayService) RecordCommandEvent(ctx context.Context, sessionID uuid.UUID, sequence uint64, eventType string, commandHash []byte) error {
	_, err := service.Store.Queries.CreateRemoteAccessCommandEvent(ctx, db.CreateRemoteAccessCommandEventParams{ID: uuid.New(), SessionID: sessionID,
		Sequence: int64(sequence), EventType: eventType, CommandHash: commandHash, OccurredAt: timestamp(service.now())})
	return err
}

func (service GatewayService) FinishRecording(ctx context.Context, recording GatewayRecording, status string) error {
	if recording.Recorder != nil {
		defer clear(recording.Recorder.DEK)
		if len(recording.Recorder.buffer) > 0 {
			chunk, err := recording.Recorder.Flush(ctx)
			if err != nil {
				status = "failed"
			} else if err := service.PersistChunks(ctx, recording, []ChunkMetadata{chunk}); err != nil {
				status = "failed"
			}
		}
	}
	if status != "available" && status != "incomplete" && status != "failed" {
		status = "failed"
	}
	_, err := service.Store.Queries.FinishRemoteAccessRecording(ctx, db.FinishRemoteAccessRecordingParams{ID: recording.Record.ID, Status: status})
	return err
}

func (service GatewayService) Finish(ctx context.Context, sessionID uuid.UUID, fence int64, status, reason string) error {
	if status != "terminated" && status != "failed" && status != "connection_lost" && status != "invalidated" && status != "expired" {
		status = "failed"
	}
	_, err := service.Store.Queries.FinishRemoteAccessSession(ctx, db.FinishRemoteAccessSessionParams{ID: sessionID, SessionFence: fence, Status: status, TerminationReason: text(reason)})
	return err
}

func (service GatewayService) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}
