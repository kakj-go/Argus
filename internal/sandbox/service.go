package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/integration/opensandbox"
	"github.com/kakj-go/Argus/internal/runtime"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrUnavailable     = errors.New("sandbox unavailable")
	ErrVersionConflict = errors.New("sandbox version conflict")
	ErrQuotaExceeded   = errors.New("sandbox quota exceeded")
)

type BackendInput struct {
	Name, Endpoint, APIKey, Status string
	ExpectedVersion                int64
}

type ImageInput struct {
	BackendID                      uuid.UUID
	Name, ImageRef, Digest, Status string
	ExpectedVersion                int64
}

type ProfileInput struct {
	Name, NetworkMode, Status            string
	BackendID, ImageID                   uuid.UUID
	TaskKinds                            []string
	CPUMillis, MemoryMiB, TimeoutSeconds int32
	ExpectedVersion                      int64
}

type Service struct {
	Store   *postgres.Store
	Keyring secret.Keyring
}

func (service Service) CreateBackend(ctx context.Context, input BackendInput) (db.SandboxBackend, error) {
	id := newID()
	envelope, err := service.encryptCredential(id, input.APIKey)
	if err != nil {
		return db.SandboxBackend{}, err
	}
	return service.Store.Queries.CreateSandboxBackend(ctx, createBackendParams(id, input, envelope))
}

func (service Service) UpdateBackend(ctx context.Context, id uuid.UUID, input BackendInput) (db.SandboxBackend, error) {
	params := db.UpdateSandboxBackendParams{ID: id, Name: input.Name, Endpoint: input.Endpoint, Status: input.Status, Version: input.ExpectedVersion}
	if input.APIKey != "" {
		envelope, err := service.encryptCredential(id, input.APIKey)
		if err != nil {
			return db.SandboxBackend{}, err
		}
		setUpdateCredential(&params, envelope)
	}
	value, err := service.Store.Queries.UpdateSandboxBackend(ctx, params)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.SandboxBackend{}, ErrVersionConflict
	}
	return value, err
}

func (service Service) ListBackends(ctx context.Context) ([]db.SandboxBackend, error) {
	return service.Store.Queries.ListSandboxBackends(ctx)
}
func (service Service) ListImages(ctx context.Context) ([]db.SandboxImage, error) {
	return service.Store.Queries.ListSandboxImages(ctx)
}
func (service Service) ListProfiles(ctx context.Context) ([]db.SandboxProfile, error) {
	return service.Store.Queries.ListSandboxProfiles(ctx)
}
func (service Service) ListSessions(ctx context.Context) ([]db.SandboxSession, error) {
	return service.Store.Queries.ListSandboxSessions(ctx, 200)
}
func (service Service) ListUsage(ctx context.Context) ([]db.SandboxUsage, error) {
	return service.Store.Queries.ListSandboxUsage(ctx, 200)
}

func (service Service) TestBackend(ctx context.Context, id uuid.UUID) (db.SandboxBackend, error) {
	backend, client, err := service.client(ctx, id)
	if err != nil {
		return db.SandboxBackend{}, err
	}
	status := "healthy"
	if err := client.Health(ctx); err != nil {
		status = "unhealthy"
	}
	return service.Store.Queries.SetSandboxBackendHealth(ctx, db.SetSandboxBackendHealthParams{ID: backend.ID, HealthStatus: status})
}

func (service Service) CreateImage(ctx context.Context, input ImageInput) (db.SandboxImage, error) {
	return service.Store.Queries.CreateSandboxImage(ctx, db.CreateSandboxImageParams{ID: newID(), BackendID: input.BackendID, Name: input.Name,
		ImageRef: input.ImageRef, Digest: input.Digest, Status: input.Status})
}

func (service Service) UpdateImage(ctx context.Context, id uuid.UUID, input ImageInput) (db.SandboxImage, error) {
	value, err := service.Store.Queries.UpdateSandboxImage(ctx, db.UpdateSandboxImageParams{ID: id, BackendID: input.BackendID, Name: input.Name,
		ImageRef: input.ImageRef, Digest: input.Digest, Status: input.Status, Version: input.ExpectedVersion})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.SandboxImage{}, ErrVersionConflict
	}
	return value, err
}

func (service Service) CreateProfile(ctx context.Context, input ProfileInput) (db.SandboxProfile, error) {
	return service.Store.Queries.CreateSandboxProfile(ctx, db.CreateSandboxProfileParams{ID: newID(), Name: input.Name, BackendID: input.BackendID,
		ImageID: input.ImageID, TaskKinds: input.TaskKinds, CpuMillis: input.CPUMillis, MemoryMib: input.MemoryMiB,
		TimeoutSeconds: input.TimeoutSeconds, NetworkMode: input.NetworkMode, Status: input.Status})
}

func (service Service) UpdateProfile(ctx context.Context, id uuid.UUID, input ProfileInput) (db.SandboxProfile, error) {
	value, err := service.Store.Queries.UpdateSandboxProfile(ctx, db.UpdateSandboxProfileParams{ID: id, Name: input.Name, BackendID: input.BackendID,
		ImageID: input.ImageID, TaskKinds: input.TaskKinds, CpuMillis: input.CPUMillis, MemoryMib: input.MemoryMiB,
		TimeoutSeconds: input.TimeoutSeconds, NetworkMode: input.NetworkMode, Status: input.Status, Version: input.ExpectedVersion})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.SandboxProfile{}, ErrVersionConflict
	}
	return value, err
}

func (service Service) GetQuota(ctx context.Context, enterpriseID uuid.UUID) (db.SandboxQuota, error) {
	return service.Store.Queries.GetSandboxQuota(ctx, enterpriseID)
}
func (service Service) UpdateQuota(ctx context.Context, enterpriseID uuid.UUID, concurrent int32, seconds, expected int64) (db.SandboxQuota, error) {
	value, err := service.Store.Queries.UpsertSandboxQuota(ctx, db.UpsertSandboxQuotaParams{EnterpriseID: enterpriseID,
		MaxConcurrentSessions: concurrent, MonthlySessionSeconds: seconds, Version: expected})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.SandboxQuota{}, ErrVersionConflict
	}
	return value, err
}

func (service Service) Terminate(ctx context.Context, id uuid.UUID) (db.SandboxSession, error) {
	session, err := service.Store.Queries.GetSandboxSession(ctx, id)
	if err != nil || session.Status == "terminated" || session.Status == "failed" {
		return session, err
	}
	profile, err := service.Store.Queries.GetSandboxProfile(ctx, session.ProfileID)
	if err != nil {
		return db.SandboxSession{}, err
	}
	_, client, err := service.client(ctx, profile.BackendID)
	if err != nil {
		return db.SandboxSession{}, err
	}
	_, _ = service.Store.Queries.UpdateSandboxSessionStatus(ctx, db.UpdateSandboxSessionStatusParams{ID: id, Status: "terminating"})
	if err := client.Delete(ctx, session.UpstreamSessionID); err != nil && !opensandbox.IsNotFound(err) {
		return service.Store.Queries.UpdateSandboxSessionStatus(ctx, db.UpdateSandboxSessionStatusParams{ID: id, Status: "unknown"})
	}
	return service.finalizeSession(ctx, id, "terminated")
}

func (service Service) Renew(ctx context.Context, id uuid.UUID) (db.SandboxSession, error) {
	session, err := service.Store.Queries.GetSandboxSession(ctx, id)
	if err != nil || (session.Status != "creating" && session.Status != "running") {
		return db.SandboxSession{}, ErrUnavailable
	}
	profile, err := service.Store.Queries.GetSandboxProfile(ctx, session.ProfileID)
	if err != nil {
		return db.SandboxSession{}, err
	}
	_, client, err := service.client(ctx, profile.BackendID)
	if err != nil {
		return db.SandboxSession{}, err
	}
	expiresAt := time.Now().UTC().Add(time.Duration(profile.TimeoutSeconds) * time.Second)
	upstream, err := client.Renew(ctx, session.UpstreamSessionID, expiresAt)
	if err != nil {
		return db.SandboxSession{}, err
	}
	if !upstream.ExpiresAt.IsZero() {
		expiresAt = upstream.ExpiresAt.UTC()
	}
	return service.Store.Queries.UpdateSandboxSessionExpiry(ctx, db.UpdateSandboxSessionExpiryParams{
		ID: id, Status: upstreamStatus(upstream.Status.State), ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
}

func (service Service) ReconcileSession(ctx context.Context, session db.SandboxSession) (db.SandboxSession, error) {
	if session.Status == "terminated" || session.Status == "failed" {
		return session, nil
	}
	profile, err := service.Store.Queries.GetSandboxProfile(ctx, session.ProfileID)
	if err != nil {
		return db.SandboxSession{}, err
	}
	_, client, err := service.client(ctx, profile.BackendID)
	if err != nil {
		return db.SandboxSession{}, err
	}
	upstream, err := client.Get(ctx, session.UpstreamSessionID)
	if opensandbox.IsNotFound(err) {
		return service.finalizeSession(ctx, session.ID, "terminated")
	}
	if err != nil {
		return service.Store.Queries.UpdateSandboxSessionStatus(ctx, db.UpdateSandboxSessionStatusParams{ID: session.ID, Status: "unknown"})
	}
	status := upstreamStatus(upstream.Status.State)
	if status == "failed" || status == "terminated" {
		return service.finalizeSession(ctx, session.ID, status)
	}
	if !session.ExpiresAt.Time.After(time.Now().UTC()) {
		return service.Terminate(ctx, session.ID)
	}
	startedAt := pgtype.Timestamptz{}
	if status == "running" && !session.StartedAt.Valid {
		startedAt = pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}
	}
	return service.Store.Queries.UpdateSandboxSessionStatus(ctx, db.UpdateSandboxSessionStatusParams{ID: session.ID, Status: status, StartedAt: startedAt})
}

func (service Service) finalizeSession(ctx context.Context, id uuid.UUID, status string) (db.SandboxSession, error) {
	var result db.SandboxSession
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		session, err := q.GetSandboxSessionForUpdate(ctx, id)
		if err != nil {
			return err
		}
		if session.Status == "terminated" || session.Status == "failed" {
			result = session
			return nil
		}
		now := time.Now().UTC()
		result, err = q.UpdateSandboxSessionStatus(ctx, db.UpdateSandboxSessionStatusParams{
			ID: id, Status: status, TerminatedAt: pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return err
		}
		startedAt := session.CreatedAt.Time
		if session.StartedAt.Valid {
			startedAt = session.StartedAt.Time
		}
		seconds := int64(now.Sub(startedAt).Seconds())
		if seconds < 0 {
			seconds = 0
		}
		month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		_, err = q.AddSandboxUsage(ctx, db.AddSandboxUsageParams{
			EnterpriseID:   session.EnterpriseID,
			Month:          pgtype.Date{Time: month, Valid: true},
			SessionCount:   1,
			SessionSeconds: seconds,
		})
		return err
	})
	return result, err
}

type TaskPayload struct {
	EnterpriseID uuid.UUID `json:"enterprise_id"`
	TaskID       uuid.UUID `json:"task_id"`
	TaskKind     string    `json:"task_kind"`
}

type Runner struct{ Service Service }

func (runner Runner) Handle(ctx context.Context, task runtime.Task) error {
	var payload TaskPayload
	if json.Unmarshal(task.Payload, &payload) != nil || payload.EnterpriseID == uuid.Nil || payload.TaskID == uuid.Nil || payload.TaskKind == "" {
		return runtime.Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: ErrUnavailable, Permanent: true}
	}
	if existing, err := runner.Service.Store.Queries.GetSandboxSessionByTask(ctx, payload.TaskID); err == nil {
		_, reconcileErr := runner.Service.ReconcileSession(ctx, existing)
		return reconcileErr
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	profile, err := runner.Service.Store.Queries.SelectSandboxProfile(ctx, payload.TaskKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return runtime.Error{ErrorCode: "SANDBOX_PROFILE_UNAVAILABLE", Cause: err, Permanent: true}
	}
	if err != nil {
		return err
	}
	quota, err := runner.Service.Store.Queries.GetSandboxQuota(ctx, payload.EnterpriseID)
	if err != nil {
		return runtime.Error{ErrorCode: "SANDBOX_QUOTA_EXCEEDED", Cause: err, Permanent: true}
	}
	active, err := runner.Service.Store.Queries.CountActiveSandboxSessions(ctx, payload.EnterpriseID)
	if err != nil {
		return err
	}
	if active >= quota.MaxConcurrentSessions {
		return runtime.Error{ErrorCode: "SANDBOX_QUOTA_EXCEEDED", Cause: ErrQuotaExceeded, Permanent: true}
	}
	month := monthStart(time.Now().UTC())
	committed, err := runner.Service.Store.Queries.GetSandboxMonthlyCommittedSeconds(ctx, db.GetSandboxMonthlyCommittedSecondsParams{
		EnterpriseID: payload.EnterpriseID, Month: pgtype.Date{Time: month, Valid: true},
	})
	if err != nil {
		return err
	}
	reservationSeconds, ok := sandboxReservationSeconds(quota.MonthlySessionSeconds, committed, int64(profile.TimeoutSeconds))
	if !ok {
		return runtime.Error{ErrorCode: "SANDBOX_QUOTA_EXCEEDED", Cause: ErrQuotaExceeded, Permanent: true}
	}
	image, err := runner.Service.Store.Queries.GetSandboxImage(ctx, profile.ImageID)
	if err != nil {
		return err
	}
	_, client, err := runner.Service.client(ctx, profile.BackendID)
	if err != nil {
		return err
	}
	created, found, err := findUpstreamSession(ctx, client, payload.TaskID)
	if err != nil {
		return err
	}
	if !found {
		created, err = client.Create(ctx, opensandbox.CreateRequest{Image: opensandbox.ImageSpec{URI: image.ImageRef + "@" + image.Digest}, Timeout: int(reservationSeconds),
			ResourceLimits: map[string]string{"cpu": fmt.Sprintf("%dm", profile.CpuMillis), "memory": fmt.Sprintf("%dMi", profile.MemoryMib)},
			Metadata:       map[string]string{"argus.enterprise_id": payload.EnterpriseID.String(), "argus.task_id": payload.TaskID.String(), "argus.profile_revision": fmt.Sprint(profile.Revision)}})
		if err != nil {
			created, found, _ = findUpstreamSession(ctx, client, payload.TaskID)
			if !found {
				return err
			}
		}
	}
	status := upstreamStatus(created.Status.State)
	expires := created.ExpiresAt
	if expires.IsZero() {
		expires = time.Now().UTC().Add(time.Duration(reservationSeconds) * time.Second)
	}
	var existing db.SandboxSession
	err = runner.Service.Store.InTx(ctx, func(q *db.Queries) error {
		if value, lookupErr := q.GetSandboxSessionByTask(ctx, payload.TaskID); lookupErr == nil {
			existing = value
			return nil
		} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
			return lookupErr
		}
		lockedQuota, lockErr := q.GetSandboxQuotaForUpdate(ctx, payload.EnterpriseID)
		if lockErr != nil {
			return lockErr
		}
		lockedActive, lockErr := q.CountActiveSandboxSessions(ctx, payload.EnterpriseID)
		if lockErr != nil {
			return lockErr
		}
		lockedCommitted, lockErr := q.GetSandboxMonthlyCommittedSeconds(ctx, db.GetSandboxMonthlyCommittedSecondsParams{
			EnterpriseID: payload.EnterpriseID, Month: pgtype.Date{Time: month, Valid: true},
		})
		if lockErr != nil {
			return lockErr
		}
		remaining, allowed := sandboxReservationSeconds(lockedQuota.MonthlySessionSeconds, lockedCommitted, reservationSeconds)
		if lockedActive >= lockedQuota.MaxConcurrentSessions || !allowed || remaining < reservationSeconds {
			return ErrQuotaExceeded
		}
		_, createErr := q.CreateSandboxSession(ctx, db.CreateSandboxSessionParams{ID: newID(), EnterpriseID: payload.EnterpriseID,
			TaskID: payload.TaskID, ProfileID: profile.ID, ProfileRevision: profile.Revision, UpstreamSessionID: created.ID, Status: status,
			ExpiresAt: pgtype.Timestamptz{Time: expires, Valid: true}, StartedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: status == "running"}})
		return createErr
	})
	if existing.ID != uuid.Nil {
		if existing.UpstreamSessionID != created.ID {
			_ = client.Delete(ctx, created.ID)
		}
		return nil
	}
	if errors.Is(err, ErrQuotaExceeded) {
		_ = client.Delete(ctx, created.ID)
		return runtime.Error{ErrorCode: "SANDBOX_QUOTA_EXCEEDED", Cause: err, Permanent: true}
	}
	if err != nil {
		if existing, lookupErr := runner.Service.Store.Queries.GetSandboxSessionByTask(ctx, payload.TaskID); lookupErr == nil {
			if existing.UpstreamSessionID != created.ID {
				_ = client.Delete(ctx, created.ID)
			}
			return nil
		}
		_ = client.Delete(ctx, created.ID)
	}
	return err
}

func sandboxReservationSeconds(limit, committed, requested int64) (int64, bool) {
	if limit <= 0 || requested <= 0 || committed >= limit {
		return 0, false
	}
	return min(requested, limit-committed), true
}

func monthStart(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func findUpstreamSession(ctx context.Context, client *opensandbox.Client, taskID uuid.UUID) (opensandbox.Sandbox, bool, error) {
	items, err := client.List(ctx)
	if err != nil {
		return opensandbox.Sandbox{}, false, err
	}
	for _, item := range items {
		if item.Metadata["argus.task_id"] == taskID.String() {
			return item, true, nil
		}
	}
	return opensandbox.Sandbox{}, false, nil
}

type Reconciler struct {
	Service Service
	Poll    time.Duration
	Logger  *slog.Logger
}

func (reconciler Reconciler) Run(ctx context.Context) error {
	if reconciler.Poll <= 0 {
		reconciler.Poll = 5 * time.Second
	}
	if reconciler.Logger == nil {
		reconciler.Logger = slog.Default()
	}
	ticker := time.NewTicker(reconciler.Poll)
	defer ticker.Stop()
	for {
		items, err := reconciler.Service.Store.Queries.ListSandboxSessionsForReconcile(ctx, db.ListSandboxSessionsForReconcileParams{
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}, Limit: 100,
		})
		if err != nil {
			reconciler.Logger.Error("sandbox reconciliation scan failed", "error", err)
		} else {
			for _, item := range items {
				if _, err := reconciler.Service.ReconcileSession(ctx, item); err != nil {
					reconciler.Logger.Warn("sandbox session reconciliation failed", "session_id", item.ID, "error", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (service Service) client(ctx context.Context, id uuid.UUID) (db.SandboxBackend, *opensandbox.Client, error) {
	backend, err := service.Store.Queries.GetSandboxBackend(ctx, id)
	if err != nil || backend.Status != "enabled" {
		return db.SandboxBackend{}, nil, ErrUnavailable
	}
	apiKey := ""
	if backend.CredentialCiphertext != nil {
		plaintext, err := service.Keyring.Decrypt(backendEnvelope(backend), []byte("argus.sandbox_backend/v1\x00"+backend.ID.String()))
		if err != nil {
			return db.SandboxBackend{}, nil, err
		}
		apiKey = string(plaintext)
		clear(plaintext)
	}
	client, err := opensandbox.NewClient(backend.Endpoint, apiKey)
	return backend, client, err
}

func (service Service) encryptCredential(id uuid.UUID, value string) (secret.Envelope, error) {
	if value == "" {
		return secret.Envelope{}, nil
	}
	return service.Keyring.Encrypt([]byte(value), []byte("argus.sandbox_backend/v1\x00"+id.String()))
}

func upstreamStatus(value string) string {
	switch value {
	case "Running", "running":
		return "running"
	case "Failed", "failed":
		return "failed"
	case "Terminated", "terminated", "Deleted", "deleted":
		return "terminated"
	default:
		return "creating"
	}
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

func createBackendParams(id uuid.UUID, input BackendInput, envelope secret.Envelope) db.CreateSandboxBackendParams {
	return db.CreateSandboxBackendParams{ID: id, Name: input.Name, Endpoint: input.Endpoint, Status: input.Status,
		CredentialProvider:   pgtype.Text{String: envelope.Provider, Valid: envelope.Provider != ""},
		CredentialKeyID:      pgtype.Text{String: envelope.KeyID, Valid: envelope.KeyID != ""},
		CredentialKeyVersion: pgtype.Int4{Int32: int32(envelope.KeyVersion), Valid: envelope.KeyVersion > 0},
		CredentialWrappedDek: envelope.WrappedDEK, CredentialWrapNonce: envelope.WrapNonce, CredentialNonce: envelope.Nonce,
		CredentialCiphertext: envelope.Ciphertext, CredentialValueHash: envelope.ValueHash}
}

func setUpdateCredential(params *db.UpdateSandboxBackendParams, envelope secret.Envelope) {
	params.CredentialProvider = pgtype.Text{String: envelope.Provider, Valid: true}
	params.CredentialKeyID = pgtype.Text{String: envelope.KeyID, Valid: true}
	params.CredentialKeyVersion = pgtype.Int4{Int32: int32(envelope.KeyVersion), Valid: true}
	params.CredentialWrappedDek = envelope.WrappedDEK
	params.CredentialWrapNonce = envelope.WrapNonce
	params.CredentialNonce = envelope.Nonce
	params.CredentialCiphertext = envelope.Ciphertext
	params.CredentialValueHash = envelope.ValueHash
}

func backendEnvelope(value db.SandboxBackend) secret.Envelope {
	return secret.Envelope{Provider: value.CredentialProvider.String, KeyID: value.CredentialKeyID.String,
		KeyVersion: int(value.CredentialKeyVersion.Int32), WrappedDEK: value.CredentialWrappedDek, WrapNonce: value.CredentialWrapNonce,
		Nonce: value.CredentialNonce, Ciphertext: value.CredentialCiphertext, ValueHash: value.CredentialValueHash}
}
