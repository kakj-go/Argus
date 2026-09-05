package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/remoteaccess/revocation"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var (
	ErrVersionConflict       = errors.New("resource version conflict")
	ErrSecretReferenced      = errors.New("secret is still referenced")
	ErrSecretValueRequired   = errors.New("secret value is required")
	ErrCredentialUnavailable = errors.New("credential unavailable")
	ErrInvalidLease          = errors.New("invalid credential lease")
)

// MaxLeaseTTL bounds how long a decrypted credential may remain usable by a
// runtime recipient. Callers with longer operations must renew their own
// operation lease independently and request credentials only for this window.
const MaxLeaseTTL = 5 * time.Minute

type Service struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
	Keyring     Keyring
}

type SecretRecord struct {
	Secret         db.Secret
	ReferenceCount int64
}

type SecretInput struct {
	Name, Type, Description, Value string
	Status                         *string
	ExpectedVersion                int64
}

type CredentialInput struct {
	Name, Protocol, Username string
	SecretID                 uuid.UUID
	Status                   *string
	ExpectedVersion          int64
}

type ManagedAccountInput struct {
	HostID                   uuid.UUID
	Username, PrivilegeLevel string
	CredentialID             uuid.UUID
	AllowedProtocols         []string
	Status                   *string
	ExpectedVersion          int64
}

type LeaseRequest struct {
	CredentialID       uuid.UUID
	OperationRef       string
	TargetResourceType string
	TargetResourceID   uuid.UUID
	RecipientType      string
	RecipientID        string
	Protocol           string
	TTL                time.Duration
}

type IssuedLease struct {
	Lease db.CredentialLease
	Value []byte
}

func (service Service) FulfillLease(ctx context.Context, enterpriseID, leaseID uuid.UUID, recipientType, recipientID string) (IssuedLease, error) {
	var result IssuedLease
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		lease, err := q.GetCredentialLease(ctx, db.GetCredentialLeaseParams{ID: leaseID, EnterpriseID: enterpriseID})
		if err != nil || lease.Status != "active" || !time.Now().UTC().Before(lease.ExpiresAt.Time) || lease.RecipientType != recipientType || lease.RecipientID != recipientID {
			return ErrInvalidLease
		}
		credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: lease.CredentialID, EnterpriseID: enterpriseID})
		if err != nil || credential.Status != "active" || credential.Protocol != lease.Protocol {
			return ErrCredentialUnavailable
		}
		version, err := q.GetSecretVersionByID(ctx, db.GetSecretVersionByIDParams{ID: lease.SecretVersionID, EnterpriseID: enterpriseID})
		if err != nil || version.SecretID != credential.SecretID {
			return ErrCredentialUnavailable
		}
		secretRecord, err := q.GetSecret(ctx, db.GetSecretParams{ID: credential.SecretID, EnterpriseID: enterpriseID})
		if err != nil || secretRecord.Status != "active" {
			return ErrCredentialUnavailable
		}
		value, err := service.Keyring.DecryptContext(ctx, Envelope{Provider: version.Provider, KeyID: version.KeyID, KeyVersion: int(version.KeyVersion), WrappedDEK: version.WrappedDek,
			WrapNonce: version.WrapNonce, Nonce: version.Nonce, Ciphertext: version.Ciphertext, ValueHash: version.ValueHash},
			secretAAD(enterpriseID, credential.SecretID, version.Version, secretRecord.Type))
		if err != nil {
			return err
		}
		rows, err := q.ConsumeCredentialLease(ctx, db.ConsumeCredentialLeaseParams{ID: lease.ID, EnterpriseID: enterpriseID})
		if err != nil || rows != 1 {
			clear(value)
			return ErrInvalidLease
		}
		lease.Status = "consumed"
		result = IssuedLease{Lease: lease, Value: value}
		return nil
	})
	return result, err
}

func (service Service) List(ctx context.Context, enterpriseID uuid.UUID) ([]SecretRecord, error) {
	rows, err := service.Store.Queries.ListSecrets(ctx, enterpriseID)
	if err != nil {
		return nil, err
	}
	result := make([]SecretRecord, 0, len(rows))
	for _, row := range rows {
		result = append(result, SecretRecord{Secret: db.Secret{ID: row.ID, EnterpriseID: row.EnterpriseID, Name: row.Name, Type: row.Type,
			Description: row.Description, Status: row.Status, CurrentVersion: row.CurrentVersion, LastAccessedAt: row.LastAccessedAt,
			Version: row.Version, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, ReferenceCount: row.ReferenceCount})
	}
	return result, nil
}

func (service Service) Get(ctx context.Context, enterpriseID, secretID uuid.UUID) (SecretRecord, error) {
	row, err := service.Store.Queries.GetSecret(ctx, db.GetSecretParams{ID: secretID, EnterpriseID: enterpriseID})
	if err != nil {
		return SecretRecord{}, err
	}
	return SecretRecord{Secret: db.Secret{ID: row.ID, EnterpriseID: row.EnterpriseID, Name: row.Name, Type: row.Type,
		Description: row.Description, Status: row.Status, CurrentVersion: row.CurrentVersion, LastAccessedAt: row.LastAccessedAt,
		Version: row.Version, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, ReferenceCount: row.ReferenceCount}, nil
}

func (service Service) Create(ctx context.Context, actorID string, enterpriseID uuid.UUID, input SecretInput, idempotencyKey string) (SecretRecord, error) {
	if input.Value == "" {
		return SecretRecord{}, ErrSecretValueRequired
	}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "secret.create", idempotencyKey, input, 201, func(q *db.Queries) (SecretRecord, error) {
		secretID := newUUID()
		envelope, err := service.Keyring.EncryptContext(ctx, []byte(input.Value), secretAAD(enterpriseID, secretID, 1, input.Type))
		if err != nil {
			return SecretRecord{}, err
		}
		secret, err := q.CreateSecret(ctx, db.CreateSecretParams{ID: secretID, EnterpriseID: enterpriseID, Name: input.Name, Type: input.Type, Description: input.Description, CreatedBy: uuid.MustParse(actorID)})
		if err != nil {
			return SecretRecord{}, err
		}
		if _, err := q.CreateSecretVersion(ctx, envelopeParams(secret, 1, envelope)); err != nil {
			return SecretRecord{}, err
		}
		if err := appendAudit(ctx, q, actorID, enterpriseID, "secret.create", "secret", secret.ID, map[string]any{"summary": "secret metadata created"}); err != nil {
			return SecretRecord{}, err
		}
		return SecretRecord{Secret: secret}, nil
	})
}

func (service Service) Update(ctx context.Context, actorID string, enterpriseID, secretID uuid.UUID, input SecretInput) (SecretRecord, error) {
	var result SecretRecord
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		secret, err := q.UpdateSecretMetadata(ctx, db.UpdateSecretMetadataParams{ID: secretID, EnterpriseID: enterpriseID, Version: input.ExpectedVersion,
			Name: optionalText(input.Name), Description: optionalText(input.Description), Status: pointerText(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		row, err := q.GetSecret(ctx, db.GetSecretParams{ID: secretID, EnterpriseID: enterpriseID})
		if err != nil {
			return err
		}
		result = SecretRecord{Secret: secret, ReferenceCount: row.ReferenceCount}
		return appendAudit(ctx, q, actorID, enterpriseID, "secret.update", "secret", secret.ID, map[string]any{"status": secret.Status})
	})
	return result, err
}

func (service Service) Rotate(ctx context.Context, actorID string, enterpriseID, secretID uuid.UUID, input SecretInput, idempotencyKey string) (SecretRecord, error) {
	if input.Value == "" {
		return SecretRecord{}, ErrSecretValueRequired
	}
	request := struct {
		SecretID        string `json:"secret_id"`
		Value           string `json:"value"`
		ExpectedVersion int64  `json:"expected_version"`
	}{secretID.String(), input.Value, input.ExpectedVersion}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "secret.rotate", idempotencyKey, request, 200, func(q *db.Queries) (SecretRecord, error) {
		current, err := q.GetSecret(ctx, db.GetSecretParams{ID: secretID, EnterpriseID: enterpriseID})
		if err != nil {
			return SecretRecord{}, err
		}
		advanced, err := q.AdvanceSecretVersion(ctx, db.AdvanceSecretVersionParams{ID: secretID, EnterpriseID: enterpriseID, Version: input.ExpectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return SecretRecord{}, ErrVersionConflict
		}
		if err != nil {
			return SecretRecord{}, err
		}
		envelope, err := service.Keyring.EncryptContext(ctx, []byte(input.Value), secretAAD(enterpriseID, secretID, advanced.CurrentVersion, advanced.Type))
		if err != nil {
			return SecretRecord{}, err
		}
		if _, err := q.CreateSecretVersion(ctx, envelopeParams(advanced, advanced.CurrentVersion, envelope)); err != nil {
			return SecretRecord{}, err
		}
		if err := q.RevokeCredentialLeasesBySecret(ctx, db.RevokeCredentialLeasesBySecretParams{EnterpriseID: enterpriseID, SecretID: secretID}); err != nil {
			return SecretRecord{}, err
		}
		if err := q.ExpireConnectionTestsByCredential(ctx, db.ExpireConnectionTestsByCredentialParams{EnterpriseID: enterpriseID, SecretID: secretID}); err != nil {
			return SecretRecord{}, err
		}
		if err := appendAudit(ctx, q, actorID, enterpriseID, "secret.rotate", "secret", secretID, map[string]any{"before": current.CurrentVersion, "after": advanced.CurrentVersion}); err != nil {
			return SecretRecord{}, err
		}
		return SecretRecord{Secret: advanced, ReferenceCount: current.ReferenceCount}, nil
	})
}

func (service Service) Disable(ctx context.Context, actorID string, enterpriseID, secretID uuid.UUID, expectedVersion int64) error {
	return service.Store.InTx(ctx, func(q *db.Queries) error {
		rows, err := q.DisableSecret(ctx, db.DisableSecretParams{ID: secretID, EnterpriseID: enterpriseID, Version: expectedVersion})
		if err != nil {
			return err
		}
		if rows != 1 {
			current, getErr := q.GetSecret(ctx, db.GetSecretParams{ID: secretID, EnterpriseID: enterpriseID})
			if getErr == nil && current.ReferenceCount > 0 {
				return ErrSecretReferenced
			}
			return ErrVersionConflict
		}
		return appendAudit(ctx, q, actorID, enterpriseID, "secret.disable", "secret", secretID, map[string]any{"status": "disabled"})
	})
}

func (service Service) ListCredentials(ctx context.Context, enterpriseID uuid.UUID) ([]db.Credential, error) {
	return service.Store.Queries.ListCredentials(ctx, enterpriseID)
}

func (service Service) CreateCredential(ctx context.Context, actorID string, enterpriseID uuid.UUID, input CredentialInput, idempotencyKey string) (db.Credential, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "credential.create", idempotencyKey, input, 201, func(q *db.Queries) (db.Credential, error) {
		secret, err := q.GetSecret(ctx, db.GetSecretParams{ID: input.SecretID, EnterpriseID: enterpriseID})
		if err != nil || secret.Status != "active" || !protocolSupportsSecret(input.Protocol, secret.Type) {
			return db.Credential{}, ErrCredentialUnavailable
		}
		credential, err := q.CreateCredential(ctx, db.CreateCredentialParams{ID: newUUID(), EnterpriseID: enterpriseID, Name: input.Name, Protocol: input.Protocol, Username: input.Username, SecretID: input.SecretID})
		if err != nil {
			return db.Credential{}, err
		}
		if err := appendAudit(ctx, q, actorID, enterpriseID, "credential.create", "credential", credential.ID, map[string]any{"summary": "credential reference created"}); err != nil {
			return db.Credential{}, err
		}
		return credential, nil
	})
}

func (service Service) UpdateCredential(ctx context.Context, actorID string, enterpriseID, credentialID uuid.UUID, input CredentialInput) (db.Credential, error) {
	var result db.Credential
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		var secretID uuid.NullUUID
		if input.SecretID != uuid.Nil {
			secretID = uuid.NullUUID{UUID: input.SecretID, Valid: true}
		}
		credential, err := q.UpdateCredential(ctx, db.UpdateCredentialParams{ID: credentialID, EnterpriseID: enterpriseID, Version: input.ExpectedVersion,
			Name: optionalText(input.Name), Username: optionalText(input.Username), SecretID: secretID, Status: pointerText(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		secret, err := q.GetSecret(ctx, db.GetSecretParams{ID: credential.SecretID, EnterpriseID: enterpriseID})
		if err != nil || secret.Status != "active" || !protocolSupportsSecret(credential.Protocol, secret.Type) {
			return ErrCredentialUnavailable
		}
		// Credential version is an authorization boundary for every runtime
		// consumer. Revoke outstanding leases in this transaction so a tunnel
		// heartbeat cannot renew authorization issued for the previous version.
		if _, err = q.RevokeCredentialLeasesByCredential(ctx, db.RevokeCredentialLeasesByCredentialParams{
			CredentialID: credential.ID, EnterpriseID: enterpriseID,
		}); err != nil {
			return err
		}
		result = credential
		return appendAudit(ctx, q, actorID, enterpriseID, "credential.update", "credential", credential.ID, map[string]any{"status": credential.Status})
	})
	return result, err
}

func (service Service) ListManagedAccounts(ctx context.Context, enterpriseID uuid.UUID) ([]db.ManagedAccount, error) {
	return service.Store.Queries.ListManagedAccounts(ctx, enterpriseID)
}

func (service Service) CreateManagedAccount(ctx context.Context, actorID string, enterpriseID uuid.UUID, input ManagedAccountInput, idempotencyKey string) (db.ManagedAccount, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "managed_account.create", idempotencyKey, input, 201, func(q *db.Queries) (db.ManagedAccount, error) {
		credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: input.CredentialID, EnterpriseID: enterpriseID})
		if err != nil || credential.Status != "active" {
			return db.ManagedAccount{}, ErrCredentialUnavailable
		}
		account, err := q.CreateManagedAccount(ctx, db.CreateManagedAccountParams{ID: newUUID(), EnterpriseID: enterpriseID, HostID: input.HostID,
			Username: input.Username, PrivilegeLevel: input.PrivilegeLevel, CredentialID: input.CredentialID, AllowedProtocols: input.AllowedProtocols})
		if err != nil {
			return db.ManagedAccount{}, err
		}
		if err := appendAudit(ctx, q, actorID, enterpriseID, "managed_account.create", "managed_account", account.ID, map[string]any{"summary": "managed account created"}); err != nil {
			return db.ManagedAccount{}, err
		}
		return account, nil
	})
}

func (service Service) UpdateManagedAccount(ctx context.Context, actorID string, enterpriseID, accountID uuid.UUID, input ManagedAccountInput) (db.ManagedAccount, error) {
	var result db.ManagedAccount
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		var credentialID uuid.NullUUID
		if input.CredentialID != uuid.Nil {
			credentialID = uuid.NullUUID{UUID: input.CredentialID, Valid: true}
		}
		account, err := q.UpdateManagedAccount(ctx, db.UpdateManagedAccountParams{ID: accountID, EnterpriseID: enterpriseID, Version: input.ExpectedVersion,
			Username: optionalText(input.Username), PrivilegeLevel: optionalText(input.PrivilegeLevel), CredentialID: credentialID,
			AllowedProtocols: input.AllowedProtocols, Status: pointerText(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if err := revocation.Source(ctx, q, enterpriseID, "managed_account", account.ID, "managed_account_changed"); err != nil {
			return err
		}
		result = account
		return appendAudit(ctx, q, actorID, enterpriseID, "managed_account.update", "managed_account", account.ID, map[string]any{"status": account.Status})
	})
	return result, err
}

func (service Service) IssueLease(ctx context.Context, actorID string, enterpriseID uuid.UUID, request LeaseRequest) (IssuedLease, error) {
	var result IssuedLease
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		lease, err := service.PrepareLeaseWithQueries(ctx, q, actorID, enterpriseID, request)
		if err != nil {
			return err
		}
		version, err := q.GetSecretVersionByID(ctx, db.GetSecretVersionByIDParams{ID: lease.SecretVersionID, EnterpriseID: enterpriseID})
		if err != nil {
			return ErrCredentialUnavailable
		}
		credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: lease.CredentialID, EnterpriseID: enterpriseID})
		if err != nil {
			return ErrCredentialUnavailable
		}
		secretRecord, err := q.GetSecret(ctx, db.GetSecretParams{ID: credential.SecretID, EnterpriseID: enterpriseID})
		if err != nil || secretRecord.Status != "active" {
			return ErrCredentialUnavailable
		}
		value, err := service.Keyring.DecryptContext(ctx, Envelope{Provider: version.Provider, KeyID: version.KeyID, KeyVersion: int(version.KeyVersion), WrappedDEK: version.WrappedDek,
			WrapNonce: version.WrapNonce, Nonce: version.Nonce, Ciphertext: version.Ciphertext, ValueHash: version.ValueHash},
			secretAAD(enterpriseID, credential.SecretID, version.Version, secretRecord.Type))
		if err != nil {
			return err
		}
		result = IssuedLease{Lease: lease, Value: value}
		return nil
	})
	return result, err
}

// RenewLease extends an unexpired runtime authorization without decrypting the
// credential again. Recipient identity and the frozen credential version guard
// the update, so replacement or revocation fences the next heartbeat.
func (service Service) RenewLease(
	ctx context.Context,
	enterpriseID, leaseID uuid.UUID,
	recipientType, recipientID string,
	credentialVersion int64,
	ttl time.Duration,
) (db.CredentialLease, error) {
	if leaseID == uuid.Nil || enterpriseID == uuid.Nil || credentialVersion <= 0 || ttl <= 0 || ttl > MaxLeaseTTL ||
		(recipientType != "connector" && recipientType != "direct_executor") || recipientID == "" {
		return db.CredentialLease{}, ErrInvalidLease
	}
	lease, err := service.Store.Queries.RenewCredentialLease(ctx, db.RenewCredentialLeaseParams{
		ID: leaseID, EnterpriseID: enterpriseID, RecipientType: recipientType, RecipientID: recipientID,
		CredentialVersion: credentialVersion,
		ExpiresAt:         pgtype.Timestamptz{Time: time.Now().UTC().Add(ttl), Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.CredentialLease{}, ErrInvalidLease
	}
	return lease, err
}

// PrepareLeaseWithQueries records a one-time lease without decrypting its Secret.
// Callers use it when the recipient will redeem the lease over an authenticated channel.
func (service Service) PrepareLeaseWithQueries(ctx context.Context, q *db.Queries, actorID string, enterpriseID uuid.UUID, request LeaseRequest) (db.CredentialLease, error) {
	if request.TTL <= 0 || request.TTL > MaxLeaseTTL || request.OperationRef == "" || request.TargetResourceID == uuid.Nil ||
		(request.RecipientType != "connector" && request.RecipientType != "direct_executor") || request.RecipientID == "" {
		return db.CredentialLease{}, ErrInvalidLease
	}
	credential, err := q.GetCredential(ctx, db.GetCredentialParams{ID: request.CredentialID, EnterpriseID: enterpriseID})
	if err != nil || credential.Status != "active" || credential.Protocol != request.Protocol {
		return db.CredentialLease{}, ErrCredentialUnavailable
	}
	version, err := q.GetCurrentSecretVersion(ctx, db.GetCurrentSecretVersionParams{SecretID: credential.SecretID, EnterpriseID: enterpriseID})
	if err != nil {
		return db.CredentialLease{}, ErrCredentialUnavailable
	}
	secretRecord, err := q.GetSecret(ctx, db.GetSecretParams{ID: credential.SecretID, EnterpriseID: enterpriseID})
	if err != nil || secretRecord.Status != "active" {
		return db.CredentialLease{}, ErrCredentialUnavailable
	}
	lease, err := q.CreateCredentialLease(ctx, db.CreateCredentialLeaseParams{ID: newUUID(), EnterpriseID: enterpriseID, CredentialID: credential.ID,
		SecretVersionID: version.ID, OperationRef: request.OperationRef, TargetResourceType: request.TargetResourceType, TargetResourceID: request.TargetResourceID,
		RecipientType: request.RecipientType, RecipientID: request.RecipientID, Protocol: request.Protocol,
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().UTC().Add(request.TTL), Valid: true}})
	if err != nil {
		return db.CredentialLease{}, err
	}
	if err := q.MarkSecretAccessed(ctx, db.MarkSecretAccessedParams{ID: credential.SecretID, EnterpriseID: enterpriseID}); err != nil {
		return db.CredentialLease{}, err
	}
	if err := appendAudit(ctx, q, actorID, enterpriseID, "credential.use", request.TargetResourceType, request.TargetResourceID, map[string]any{"summary": "credential lease issued"}); err != nil {
		return db.CredentialLease{}, err
	}
	return lease, nil
}

func (service Service) ConsumeLease(ctx context.Context, enterpriseID, leaseID uuid.UUID) error {
	rows, err := service.Store.Queries.ConsumeCredentialLease(ctx, db.ConsumeCredentialLeaseParams{ID: leaseID, EnterpriseID: enterpriseID})
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrInvalidLease
	}
	return nil
}

func envelopeParams(secret db.Secret, version int32, envelope Envelope) db.CreateSecretVersionParams {
	return db.CreateSecretVersionParams{ID: newUUID(), SecretID: secret.ID, EnterpriseID: secret.EnterpriseID, Version: version,
		Provider: envelope.Provider, KeyID: envelope.KeyID, KeyVersion: int32(envelope.KeyVersion), WrappedDek: envelope.WrappedDEK,
		WrapNonce: envelope.WrapNonce, Nonce: envelope.Nonce, Ciphertext: envelope.Ciphertext, ValueHash: envelope.ValueHash}
}

func secretAAD(enterpriseID, secretID uuid.UUID, version int32, secretType string) []byte {
	return []byte(fmt.Sprintf("argus.secret/v1\x00%s\x00%s\x00%d\x00%s", enterpriseID, secretID, version, secretType))
}

func protocolSupportsSecret(protocol, secretType string) bool {
	supported := map[string]map[string]bool{
		"ssh":        {"ssh_password": true, "ssh_private_key": true},
		"winrm":      {"winrm_password": true},
		"kubernetes": {"kubeconfig": true},
		"http":       {"api_token": true, "basic_auth": true},
	}
	return supported[protocol][secretType]
}

func appendAudit(ctx context.Context, q *db.Queries, actorID string, enterpriseID uuid.UUID, action, resourceType string, resourceID uuid.UUID, details map[string]any) error {
	_, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: actorType(ctx), ActorID: actorID,
		Action: action, ResourceType: resourceType, ResourceID: resourceID.String(), Result: "success", Details: details})
	return err
}

func actorType(ctx context.Context) string {
	if value, ok := ctx.Value(actorTypeKey{}).(string); ok {
		return value
	}
	return "enterprise_user"
}

type actorTypeKey struct{}

func WithActorType(ctx context.Context, value string) context.Context {
	return context.WithValue(ctx, actorTypeKey{}, value)
}

func optionalText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: value != ""}
}

func pointerText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func newUUID() uuid.UUID {
	value, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return value
}

func MarshalLeaseMetadata(value IssuedLease) ([]byte, error) {
	return json.Marshal(value.Lease)
}
