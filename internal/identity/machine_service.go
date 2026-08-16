package identity

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type MachineService struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
}

type ServiceAccountRecord struct {
	Account      db.ServiceAccount
	DataScopeIDs []uuid.UUID
}
type ServiceAccountInput struct {
	Name, Description string
	AllowedToolIDs    []string
	DataScopeIDs      []uuid.UUID
	Status            *string
	ExpectedVersion   int64
}
type CreatedAPIKey struct {
	Key    db.ApiKey
	Secret string
}

type apiKeyReplay struct {
	ID, EnterpriseID, ServiceAccountID, Name, Prefix, Status, Secret string
	Version, AuthorizationVersion                                    int64
	ExpiresAt                                                        *time.Time
	CreatedAt                                                        time.Time
}

func (service MachineService) ListServiceAccounts(ctx context.Context, enterpriseID uuid.UUID) ([]ServiceAccountRecord, error) {
	accounts, err := service.Store.Queries.ListServiceAccounts(ctx, enterpriseID)
	if err != nil {
		return nil, err
	}
	result := make([]ServiceAccountRecord, 0, len(accounts))
	for _, account := range accounts {
		scopes, err := service.Store.Queries.ListServiceAccountDataScopes(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, ServiceAccountRecord{Account: account, DataScopeIDs: scopes})
	}
	return result, nil
}

func (service MachineService) CreateServiceAccount(ctx context.Context, actorID string, enterpriseID uuid.UUID, input ServiceAccountInput, idempotencyKey string) (ServiceAccountRecord, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "service_account.create", idempotencyKey, input, 201, func(q *db.Queries) (ServiceAccountRecord, error) {
		account, err := q.CreateServiceAccount(ctx, db.CreateServiceAccountParams{ID: mustUUIDV7(), EnterpriseID: enterpriseID, Name: input.Name, Description: input.Description, AllowedToolIds: input.AllowedToolIDs})
		if err != nil {
			return ServiceAccountRecord{}, err
		}
		for _, scopeID := range input.DataScopeIDs {
			if err := q.AddServiceAccountDataScope(ctx, db.AddServiceAccountDataScopeParams{ServiceAccountID: account.ID, DataScopeID: scopeID, EnterpriseID: enterpriseID}); err != nil {
				return ServiceAccountRecord{}, err
			}
		}
		if err := q.InitializeAuthorizationVersion(ctx, db.InitializeAuthorizationVersionParams{EnterpriseID: enterpriseID, SubjectType: "service_account", SubjectID: account.ID}); err != nil {
			return ServiceAccountRecord{}, err
		}
		_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: ActorTypeFromContext(ctx), ActorID: actorID, Action: "service_account.create", ResourceType: "service_account", ResourceID: account.ID.String(), Result: "success", Details: map[string]any{"summary": "service account created"}})
		if err != nil {
			return ServiceAccountRecord{}, err
		}
		return ServiceAccountRecord{Account: account, DataScopeIDs: append([]uuid.UUID(nil), input.DataScopeIDs...)}, nil
	})
}

func (service MachineService) UpdateServiceAccount(ctx context.Context, actorID string, enterpriseID, accountID uuid.UUID, input ServiceAccountInput) (ServiceAccountRecord, error) {
	var result ServiceAccountRecord
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		account, err := q.UpdateServiceAccount(ctx, db.UpdateServiceAccountParams{ID: accountID, EnterpriseID: enterpriseID, ExpectedVersion: input.ExpectedVersion, Description: pgtype.Text{String: input.Description, Valid: input.Description != ""}, AllowedToolIds: input.AllowedToolIDs, Status: machineText(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if input.DataScopeIDs != nil {
			if err := q.ReplaceServiceAccountDataScopes(ctx, accountID); err != nil {
				return err
			}
			for _, scopeID := range input.DataScopeIDs {
				if err := q.AddServiceAccountDataScope(ctx, db.AddServiceAccountDataScopeParams{ServiceAccountID: accountID, DataScopeID: scopeID, EnterpriseID: enterpriseID}); err != nil {
					return err
				}
			}
		}
		scopes, err := q.ListServiceAccountDataScopes(ctx, accountID)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"enterprise_id": enterpriseID.String(), "service_account_id": accountID.String(), "authorization_version": account.AuthorizationVersion})
		if err := q.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: mustUUIDV7(), Topic: "authorization.service_account.changed", AggregateType: "service_account", AggregateID: accountID.String(), Payload: payload}); err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: ActorTypeFromContext(ctx), ActorID: actorID, Action: "service_account.update", ResourceType: "service_account", ResourceID: accountID.String(), Result: "success", Details: map[string]any{"authorization_version": account.AuthorizationVersion}})
		result = ServiceAccountRecord{Account: account, DataScopeIDs: scopes}
		return err
	})
	return result, err
}

func (service MachineService) ListAPIKeys(ctx context.Context, enterpriseID, accountID uuid.UUID) ([]db.ApiKey, error) {
	return service.Store.Queries.ListApiKeys(ctx, db.ListApiKeysParams{EnterpriseID: enterpriseID, ServiceAccountID: accountID})
}

func (service MachineService) CreateAPIKey(ctx context.Context, actorID string, enterpriseID, accountID uuid.UUID, name string, expiresAt *time.Time, idempotencyKey string) (CreatedAPIKey, error) {
	request, _ := json.Marshal(map[string]any{"service_account_id": accountID.String(), "name": name, "expires_at": expiresAt})
	return service.createAPIKey(ctx, actorID, enterpriseID, accountID, name, expiresAt, idempotencyKey, "api_key.create", request, nil)
}

func (service MachineService) RotateAPIKey(ctx context.Context, actorID string, enterpriseID, keyID uuid.UUID, expectedVersion int64, idempotencyKey string) (CreatedAPIKey, error) {
	current, err := service.Store.Queries.GetApiKey(ctx, db.GetApiKeyParams{ID: keyID, EnterpriseID: enterpriseID})
	if err != nil {
		return CreatedAPIKey{}, err
	}
	var expires *time.Time
	if current.ExpiresAt.Valid {
		expires = &current.ExpiresAt.Time
	}
	request, _ := json.Marshal(map[string]any{"key_id": keyID.String(), "expected_version": expectedVersion})
	return service.createAPIKey(ctx, actorID, enterpriseID, current.ServiceAccountID, current.Name, expires, idempotencyKey, "api_key.rotate", request, &current)
}

func (service MachineService) createAPIKey(ctx context.Context, actorID string, enterpriseID, accountID uuid.UUID, name string, expiresAt *time.Time, idempotencyKey, operation string, request []byte, old *db.ApiKey) (CreatedAPIKey, error) {
	var created CreatedAPIKey
	err := service.Store.InTx(ctx, func(q *db.Queries) error {
		begin, err := service.Idempotency.Begin(ctx, q, "enterprise", actorID, operation, idempotencyKey, request)
		if err != nil {
			return err
		}
		if begin.Replay {
			var replay apiKeyReplay
			if err := json.Unmarshal(begin.Body, &replay); err != nil {
				return err
			}
			created = replayCreatedAPIKey(replay)
			return nil
		}
		account, err := q.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: accountID, EnterpriseID: enterpriseID})
		if err != nil || account.Status != "active" {
			return ErrSessionRevoked
		}
		prefix, err := RandomToken(6)
		if err != nil {
			return err
		}
		secret, err := RandomToken(32)
		if err != nil {
			return err
		}
		full := "argus_ak_" + prefix + "." + secret
		params := db.CreateApiKeyParams{ID: mustUUIDV7(), EnterpriseID: enterpriseID, ServiceAccountID: accountID, Name: name, Prefix: prefix, SecretHash: TokenHash(secret), AuthorizationVersion: account.AuthorizationVersion}
		if expiresAt != nil {
			params.ExpiresAt = pgtype.Timestamptz{Time: expiresAt.UTC(), Valid: true}
		}
		key, err := q.CreateApiKey(ctx, params)
		if err != nil {
			return err
		}
		if old != nil {
			if _, err := q.RevokeApiKey(ctx, db.RevokeApiKeyParams{ID: old.ID, EnterpriseID: enterpriseID, Version: old.Version}); err != nil {
				return err
			}
		}
		created = CreatedAPIKey{Key: key, Secret: full}
		body, _ := json.Marshal(toReplay(created))
		if err := service.Idempotency.Complete(ctx, q, "enterprise", actorID, operation, idempotencyKey, httpStatusCreated(operation), body); err != nil {
			return err
		}
		action := operation
		if _, err := audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: ActorTypeFromContext(ctx), ActorID: actorID, Action: action, ResourceType: "api_key", ResourceID: key.ID.String(), Result: "success", Details: map[string]any{"summary": strings.ReplaceAll(action, "_", " ")}}); err != nil {
			return err
		}
		return nil
	})
	return created, err
}

func (service MachineService) RevokeAPIKey(ctx context.Context, actorID string, enterpriseID, keyID uuid.UUID, expectedVersion int64) error {
	return service.Store.InTx(ctx, func(q *db.Queries) error {
		key, err := q.RevokeApiKey(ctx, db.RevokeApiKeyParams{ID: keyID, EnterpriseID: enterpriseID, Version: expectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		_, err = audit.Append(ctx, q, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: ActorTypeFromContext(ctx), ActorID: actorID, Action: "api_key.revoke", ResourceType: "api_key", ResourceID: key.ID.String(), Result: "success", Details: map[string]any{"status": "revoked"}})
		return err
	})
}

func (service MachineService) AuthenticateAPIKey(ctx context.Context, value string) (Principal, error) {
	if !strings.HasPrefix(value, "argus_ak_") {
		return Principal{}, ErrInvalidCredentials
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "argus_ak_"), ".", 2)
	if len(parts) != 2 {
		return Principal{}, ErrInvalidCredentials
	}
	record, err := service.Store.Queries.GetApiKeyByPrefix(ctx, parts[0])
	if err != nil {
		return Principal{}, ErrInvalidCredentials
	}
	hash := sha256.Sum256([]byte(parts[1]))
	if len(record.SecretHash) != len(hash) || subtle.ConstantTimeCompare(hash[:], record.SecretHash) != 1 || record.Status != "active" || record.ServiceAccountStatus != "active" {
		return Principal{}, ErrInvalidCredentials
	}
	if record.AuthorizationVersion != record.ServiceAccountAuthorizationVersion {
		return Principal{}, ErrAuthorizationVersion
	}
	if record.ExpiresAt.Valid && !time.Now().UTC().Before(record.ExpiresAt.Time) {
		return Principal{}, ErrTemporaryExpired
	}
	account, err := service.Store.Queries.GetServiceAccount(ctx, db.GetServiceAccountParams{ID: record.ServiceAccountID, EnterpriseID: record.EnterpriseID})
	if err != nil {
		return Principal{}, err
	}
	permissions, err := service.Store.Queries.ListEffectiveServiceAccountPermissions(ctx, db.ListEffectiveServiceAccountPermissionsParams{EnterpriseID: account.EnterpriseID, ServiceAccountID: account.ID})
	if err != nil {
		return Principal{}, err
	}
	scopes, err := service.Store.Queries.ListServiceAccountDataScopes(ctx, account.ID)
	if err != nil {
		return Principal{}, err
	}
	if err := service.Store.Queries.MarkApiKeyUsed(ctx, record.ID); err != nil {
		return Principal{}, err
	}
	return Principal{ServiceAccount: &account, Permissions: permissions, DataScopeIDs: scopes}, nil
}

func toReplay(value CreatedAPIKey) apiKeyReplay {
	r := apiKeyReplay{ID: value.Key.ID.String(), EnterpriseID: value.Key.EnterpriseID.String(), ServiceAccountID: value.Key.ServiceAccountID.String(), Name: value.Key.Name, Prefix: value.Key.Prefix, Status: value.Key.Status, Secret: value.Secret, Version: value.Key.Version, AuthorizationVersion: value.Key.AuthorizationVersion, CreatedAt: value.Key.CreatedAt.Time}
	if value.Key.ExpiresAt.Valid {
		r.ExpiresAt = &value.Key.ExpiresAt.Time
	}
	return r
}
func replayCreatedAPIKey(value apiKeyReplay) CreatedAPIKey {
	key := db.ApiKey{ID: uuid.MustParse(value.ID), EnterpriseID: uuid.MustParse(value.EnterpriseID), ServiceAccountID: uuid.MustParse(value.ServiceAccountID), Name: value.Name, Prefix: value.Prefix, Status: value.Status, Version: value.Version, AuthorizationVersion: value.AuthorizationVersion, CreatedAt: pgtype.Timestamptz{Time: value.CreatedAt, Valid: true}}
	if value.ExpiresAt != nil {
		key.ExpiresAt = pgtype.Timestamptz{Time: *value.ExpiresAt, Valid: true}
	}
	return CreatedAPIKey{Key: key, Secret: value.Secret}
}
func machineText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}
func httpStatusCreated(operation string) int {
	if operation == "api_key.create" {
		return 201
	}
	return 200
}
func (value apiKeyReplay) String() string { return fmt.Sprintf("%s/%s", value.Prefix, value.ID) }
