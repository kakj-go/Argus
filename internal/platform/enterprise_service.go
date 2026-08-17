package platform

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/authorization"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type EnterpriseService struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
}

type CreateEnterpriseInput struct {
	Name, Code, Timezone, DefaultLocale, Remark string
}

type CreatedCredential struct {
	User              db.EnterpriseUser
	TemporaryPassword string
	ExpiresAt         time.Time
}

type UpdateEnterpriseInput struct {
	Name, Timezone, DefaultLocale, Remark *string
	ExpectedVersion                       int64
}

func (service EnterpriseService) CreateEnterprise(ctx context.Context, actorID string, input CreateEnterpriseInput, idempotencyKey string) (db.Enterprise, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "platform", actorID, "enterprise.create", idempotencyKey, input, 201, func(queries *db.Queries) (db.Enterprise, error) {
		enterpriseID := newUUID()
		departmentID := newUUID()
		scopeID := newUUID()
		enterprise, err := queries.CreateEnterprise(ctx, db.CreateEnterpriseParams{
			ID: enterpriseID, Name: input.Name, Code: input.Code, Timezone: input.Timezone,
			DefaultLocale: input.DefaultLocale, Remark: input.Remark,
		})
		if err != nil {
			return db.Enterprise{}, err
		}
		if _, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{
			ID: departmentID, EnterpriseID: enterpriseID, Name: "Default", Description: "Default department", IsDefault: true,
		}); err != nil {
			return db.Enterprise{}, err
		}
		for permission, description := range authorization.PermissionRegistry {
			if err := queries.UpsertPermission(ctx, db.UpsertPermissionParams{ID: permission, Description: description, RegistryVersion: authorization.PermissionRegistryVersion}); err != nil {
				return db.Enterprise{}, err
			}
		}
		for _, builtin := range authorization.BuiltinRoles {
			role, err := queries.CreateRole(ctx, db.CreateRoleParams{
				ID: newUUID(), EnterpriseID: enterpriseID, IdentityKey: pgtype.Text{String: builtin.Key, Valid: true},
				Name: builtin.Name, Description: builtin.Description, Builtin: true,
			})
			if err != nil {
				return db.Enterprise{}, err
			}
			for _, permission := range builtin.Permissions {
				if err := queries.AddRolePermission(ctx, db.AddRolePermissionParams{RoleID: role.ID, PermissionID: permission}); err != nil {
					return db.Enterprise{}, err
				}
			}
		}
		emptyHash := sha256.Sum256([]byte("null"))
		if _, err := queries.CreateDataScope(ctx, db.CreateDataScopeParams{
			ID: scopeID, EnterpriseID: enterpriseID, Name: "Default empty scope", Description: "Matches no resources",
			ResourceTypes: []string{"host", "kubernetes_cluster"}, ExplicitResourceIds: []string{}, SelectorHash: emptyHash[:],
		}); err != nil {
			return db.Enterprise{}, err
		}
		if err := audit.InitializeChain(ctx, queries, "enterprise", uuid.NullUUID{UUID: enterpriseID, Valid: true}); err != nil {
			return db.Enterprise{}, err
		}
		if _, err := audit.Append(ctx, queries, audit.Entry{
			Domain: "platform", ActorType: "platform_user", ActorID: actorID,
			Action: "enterprise.create", ResourceType: "enterprise", ResourceID: enterpriseID.String(), Result: "success",
			Details: map[string]any{"summary": "enterprise created", "enterprise_code": input.Code, "status": "active"},
		}); err != nil {
			return db.Enterprise{}, err
		}
		payload, _ := json.Marshal(map[string]any{"enterprise_id": enterpriseID.String(), "status": "active"})
		if err := queries.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newUUID(), Topic: "authorization.enterprise.changed", AggregateType: "enterprise", AggregateID: enterpriseID.String(), Payload: payload}); err != nil {
			return db.Enterprise{}, err
		}
		return enterprise, nil
	})
}

func (service EnterpriseService) ListEnterprises(ctx context.Context) ([]db.Enterprise, error) {
	return service.Store.Queries.ListAllEnterprises(ctx)
}

func (service EnterpriseService) GetEnterprise(ctx context.Context, id uuid.UUID) (db.Enterprise, error) {
	return service.Store.Queries.GetEnterprise(ctx, id)
}

func (service EnterpriseService) UpdateEnterprise(ctx context.Context, id uuid.UUID, input UpdateEnterpriseInput) (db.Enterprise, error) {
	value, err := service.Store.Queries.UpdateEnterprise(ctx, db.UpdateEnterpriseParams{
		ID: id, ExpectedVersion: input.ExpectedVersion, Name: nullableString(input.Name), Timezone: nullableString(input.Timezone),
		DefaultLocale: nullableString(input.DefaultLocale), Remark: nullableString(input.Remark),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Enterprise{}, identity.ErrVersionConflict
	}
	return value, err
}

func (service EnterpriseService) ListAdmins(ctx context.Context, enterpriseID uuid.NullUUID) ([]db.EnterpriseUser, error) {
	return service.Store.Queries.ListEnterpriseAdmins(ctx, enterpriseID)
}

func (service EnterpriseService) DisableAdmin(ctx context.Context, actorID string, userID uuid.UUID, expectedVersion int64) (db.EnterpriseUser, error) {
	var user db.EnterpriseUser
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		var err error
		user, err = queries.DisableEnterpriseUser(ctx, db.DisableEnterpriseUserParams{ID: userID, Version: expectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if err := queries.RevokeSubjectSessions(ctx, db.RevokeSubjectSessionsParams{Audience: "enterprise", UserID: userID, RevokeReason: pgtype.Text{String: "user_disabled", Valid: true}}); err != nil {
			return err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "platform", ActorType: "platform_user", ActorID: actorID,
			Action: "enterprise_admin.disable", ResourceType: "enterprise_user", ResourceID: userID.String(), Result: "success", Details: map[string]any{"status": "disabled"}})
		return err
	})
	return user, err
}

func (service EnterpriseService) ChangeStatus(ctx context.Context, actorID string, enterpriseID uuid.UUID, status string, expectedVersion int64) (db.Enterprise, error) {
	var enterprise db.Enterprise
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		current, err := queries.GetEnterprise(ctx, enterpriseID)
		if err != nil {
			return err
		}
		if current.Status == "disabled" || (status != "active" && status != "suspended" && status != "disabled") {
			return ErrVersionConflict
		}
		enterprise, err = queries.ChangeEnterpriseStatus(ctx, db.ChangeEnterpriseStatusParams{ID: enterpriseID, Status: status, Version: expectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if status != "active" {
			if err := queries.RevokeEnterpriseSessions(ctx, db.RevokeEnterpriseSessionsParams{EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, RevokeReason: pgtype.Text{String: "enterprise_" + status, Valid: true}}); err != nil {
				return err
			}
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "platform", ActorType: "platform_user", ActorID: actorID,
			Action: "enterprise." + status, ResourceType: "enterprise", ResourceID: enterpriseID.String(), Result: "success", Details: map[string]any{"status": status}})
		return err
	})
	return enterprise, err
}

func (service EnterpriseService) CreateAdmin(ctx context.Context, actorID string, enterpriseID uuid.UUID, username, displayName, email, idempotencyKey string) (CreatedCredential, error) {
	password, err := identity.RandomToken(24)
	if err != nil {
		return CreatedCredential{}, err
	}
	if err := identity.ValidatePassword(password, username, email); err != nil {
		return CreatedCredential{}, err
	}
	encoded, err := identity.HashPassword(password)
	if err != nil {
		return CreatedCredential{}, err
	}
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	request := map[string]any{"enterprise_id": enterpriseID.String(), "username": username, "display_name": displayName, "email": email}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "platform", actorID, "enterprise_admin.create", idempotencyKey, request, 201, func(queries *db.Queries) (CreatedCredential, error) {
		enterprise, err := queries.GetEnterprise(ctx, enterpriseID)
		if err != nil || enterprise.Status != "active" {
			return CreatedCredential{}, ErrEnterpriseUnavailable
		}
		department, err := queries.GetDefaultDepartment(ctx, enterpriseID)
		if err != nil {
			return CreatedCredential{}, err
		}
		user, err := queries.CreateEnterpriseUser(ctx, db.CreateEnterpriseUserParams{ID: newUUID(), EnterpriseID: enterpriseID, DepartmentID: department.ID,
			Username: username, DisplayName: displayName, Email: pgtype.Text{String: email, Valid: email != ""}})
		if err != nil {
			return CreatedCredential{}, err
		}
		if _, err := queries.CreatePasswordCredential(ctx, db.CreatePasswordCredentialParams{ID: newUUID(), Audience: "enterprise", SubjectID: user.ID,
			EncodedHash: encoded, Temporary: true, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}}); err != nil {
			return CreatedCredential{}, err
		}
		if _, err := queries.CreateTemporaryCredential(ctx, db.CreateTemporaryCredentialParams{ID: newUUID(), Audience: "enterprise", UserID: user.ID,
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}}); err != nil {
			return CreatedCredential{}, err
		}
		if err := queries.InitializeAuthorizationVersion(ctx, db.InitializeAuthorizationVersionParams{EnterpriseID: enterpriseID, SubjectType: "user", SubjectID: user.ID}); err != nil {
			return CreatedCredential{}, err
		}
		role, err := queries.GetBuiltinRole(ctx, db.GetBuiltinRoleParams{EnterpriseID: enterpriseID, IdentityKey: pgtype.Text{String: "enterprise_admin", Valid: true}})
		if err != nil {
			return CreatedCredential{}, err
		}
		binding, err := queries.CreateRoleBinding(ctx, db.CreateRoleBindingParams{ID: newUUID(), EnterpriseID: enterpriseID, SubjectType: "user", SubjectID: user.ID, RoleID: role.ID})
		if err != nil {
			return CreatedCredential{}, err
		}
		defaultScope, err := service.defaultScope(ctx, queries, enterpriseID)
		if err != nil {
			return CreatedCredential{}, err
		}
		if err := queries.AddRoleBindingDataScope(ctx, db.AddRoleBindingDataScopeParams{RoleBindingID: binding.ID, DataScopeID: defaultScope.ID, EnterpriseID: enterpriseID}); err != nil {
			return CreatedCredential{}, err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "platform", ActorType: "platform_user", ActorID: actorID,
			Action: "enterprise_admin.create", ResourceType: "enterprise_user", ResourceID: user.ID.String(), Result: "success",
			Details: map[string]any{"summary": "enterprise administrator created", "username": username}})
		if err != nil {
			return CreatedCredential{}, err
		}
		return CreatedCredential{User: user, TemporaryPassword: password, ExpiresAt: expiresAt}, nil
	})
}

func (service EnterpriseService) ResetAdminPassword(ctx context.Context, actorID string, userID uuid.UUID, idempotencyKey string) (CreatedCredential, error) {
	user, err := service.Store.Queries.GetEnterpriseUserByID(ctx, userID)
	if err != nil {
		return CreatedCredential{}, err
	}
	password, err := identity.RandomToken(24)
	if err != nil {
		return CreatedCredential{}, err
	}
	encoded, err := identity.HashPassword(password)
	if err != nil {
		return CreatedCredential{}, err
	}
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "platform", actorID, "enterprise_admin.reset_password", idempotencyKey, map[string]any{"user_id": userID.String()}, 200, func(queries *db.Queries) (CreatedCredential, error) {
		if _, err := queries.CreatePasswordCredential(ctx, db.CreatePasswordCredentialParams{ID: newUUID(), Audience: "enterprise", SubjectID: user.ID,
			EncodedHash: encoded, Temporary: true, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}}); err != nil {
			return CreatedCredential{}, err
		}
		if _, err := queries.CreateTemporaryCredential(ctx, db.CreateTemporaryCredentialParams{ID: newUUID(), Audience: "enterprise", UserID: user.ID,
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}}); err != nil {
			return CreatedCredential{}, err
		}
		if err := queries.RevokeSubjectSessions(ctx, db.RevokeSubjectSessionsParams{Audience: "enterprise", UserID: user.ID, RevokeReason: pgtype.Text{String: "password_reset", Valid: true}}); err != nil {
			return CreatedCredential{}, err
		}
		_, err := audit.Append(ctx, queries, audit.Entry{Domain: "platform", ActorType: "platform_user", ActorID: actorID,
			Action: "enterprise_admin.reset_password", ResourceType: "enterprise_user", ResourceID: user.ID.String(), Result: "success",
			Details: map[string]any{"summary": "enterprise administrator password reset", "username": user.Username}})
		if err != nil {
			return CreatedCredential{}, err
		}
		return CreatedCredential{User: user, TemporaryPassword: password, ExpiresAt: expiresAt}, nil
	})
}

func (service EnterpriseService) defaultScope(ctx context.Context, queries *db.Queries, enterpriseID uuid.UUID) (db.DataScope, error) {
	scopes, err := queries.ListDataScopes(ctx, enterpriseID)
	if err != nil {
		return db.DataScope{}, err
	}
	for _, scope := range scopes {
		if scope.Name == "Default empty scope" {
			return scope, nil
		}
	}
	return db.DataScope{}, fmt.Errorf("default data scope missing")
}

var ErrEnterpriseUnavailable = errors.New("enterprise unavailable")
var ErrVersionConflict = errors.New("enterprise state conflict")

func newUUID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}

func nullableString(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}
