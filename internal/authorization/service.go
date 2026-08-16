package authorization

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var ErrBuiltinRoleImmutable = errors.New("builtin role is immutable")

type Service struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
}

type RoleRecord struct {
	Role        db.Role
	Permissions []string
}
type BindingRecord struct {
	Binding      db.RoleBinding
	DataScopeIDs []uuid.UUID
}

type RoleInput struct {
	Name, Description string
	Permissions       []string
	Status            *string
	ExpectedVersion   int64
}
type ScopeInput struct {
	Name, Description                  string
	ResourceTypes, ExplicitResourceIDs []string
	LabelSelector                      json.RawMessage
	Status                             *string
	ExpectedVersion                    int64
}
type BindingInput struct {
	SubjectType           string
	SubjectID, RoleID     uuid.UUID
	DataScopeIDs          []uuid.UUID
	ValidFrom, ValidUntil *time.Time
	SetValidFrom          bool
	SetValidUntil         bool
	Status                *string
	ExpectedVersion       int64
}

func (service Service) Permissions(ctx context.Context) ([]string, error) {
	return service.Store.Queries.ListPermissions(ctx)
}

func (service Service) ListRoles(ctx context.Context, enterpriseID uuid.UUID) ([]RoleRecord, error) {
	roles, err := service.Store.Queries.ListRoles(ctx, enterpriseID)
	if err != nil {
		return nil, err
	}
	result := make([]RoleRecord, 0, len(roles))
	for _, role := range roles {
		permissions, err := service.Store.Queries.ListRolePermissions(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, RoleRecord{Role: role, Permissions: permissions})
	}
	return result, nil
}

func (service Service) CreateRole(ctx context.Context, actorID string, enterpriseID uuid.UUID, input RoleInput, idempotencyKey string) (RoleRecord, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "role.create", idempotencyKey, input, 201, func(queries *db.Queries) (RoleRecord, error) {
		role, err := queries.CreateRole(ctx, db.CreateRoleParams{ID: newID(), EnterpriseID: enterpriseID, Name: input.Name, Description: input.Description})
		if err != nil {
			return RoleRecord{}, err
		}
		for _, permission := range input.Permissions {
			if _, exists := PermissionRegistry[permission]; !exists {
				return RoleRecord{}, errors.New("unknown permission")
			}
			if err := queries.AddRolePermission(ctx, db.AddRolePermissionParams{RoleID: role.ID, PermissionID: permission}); err != nil {
				return RoleRecord{}, err
			}
		}
		if _, err := audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: identity.ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "role.create", ResourceType: "role", ResourceID: role.ID.String(), Result: "success", Details: map[string]any{"summary": "custom role created"}}); err != nil {
			return RoleRecord{}, err
		}
		return RoleRecord{Role: role, Permissions: append([]string(nil), input.Permissions...)}, nil
	})
}

func (service Service) UpdateRole(ctx context.Context, actorID string, enterpriseID, roleID uuid.UUID, input RoleInput) (RoleRecord, error) {
	var result RoleRecord
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		current, err := queries.GetRole(ctx, db.GetRoleParams{ID: roleID, EnterpriseID: enterpriseID})
		if err != nil {
			return err
		}
		if current.Builtin {
			return ErrBuiltinRoleImmutable
		}
		role, err := queries.UpdateRole(ctx, db.UpdateRoleParams{ID: roleID, EnterpriseID: enterpriseID, ExpectedVersion: input.ExpectedVersion,
			Name: optionalText(input.Name), Description: optionalText(input.Description), Status: textValue(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if input.Permissions != nil {
			if err := queries.DeleteRolePermissions(ctx, roleID); err != nil {
				return err
			}
			for _, permission := range input.Permissions {
				if _, exists := PermissionRegistry[permission]; !exists {
					return errors.New("unknown permission")
				}
				if err := queries.AddRolePermission(ctx, db.AddRolePermissionParams{RoleID: roleID, PermissionID: permission}); err != nil {
					return err
				}
			}
		}
		permissions, err := queries.ListRolePermissions(ctx, roleID)
		if err != nil {
			return err
		}
		if err := service.invalidateEnterprise(ctx, queries, enterpriseID, "role", roleID.String()); err != nil {
			return err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: identity.ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "role.update", ResourceType: "role", ResourceID: roleID.String(), Result: "success", Details: map[string]any{"summary": "custom role updated"}})
		result = RoleRecord{Role: role, Permissions: permissions}
		return err
	})
	return result, err
}

func (service Service) ListScopes(ctx context.Context, enterpriseID uuid.UUID) ([]db.DataScope, error) {
	return service.Store.Queries.ListDataScopes(ctx, enterpriseID)
}

func (service Service) CreateScope(ctx context.Context, actorID string, enterpriseID uuid.UUID, input ScopeInput, idempotencyKey string) (db.DataScope, error) {
	normalized, hash, err := NormalizeSelector(input.LabelSelector)
	if err != nil {
		return db.DataScope{}, err
	}
	request := struct {
		Input      ScopeInput `json:"input"`
		Normalized []byte     `json:"normalized"`
	}{Input: input, Normalized: normalized}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "data_scope.create", idempotencyKey, request, 201, func(queries *db.Queries) (db.DataScope, error) {
		scope, err := queries.CreateDataScope(ctx, db.CreateDataScopeParams{ID: newID(), EnterpriseID: enterpriseID, Name: input.Name, Description: input.Description,
			ResourceTypes: input.ResourceTypes, ExplicitResourceIds: input.ExplicitResourceIDs, LabelSelector: normalized, SelectorHash: hash})
		if err != nil {
			return db.DataScope{}, err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: identity.ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "data_scope.create", ResourceType: "data_scope", ResourceID: scope.ID.String(), Result: "success", Details: map[string]any{"summary": "data scope created"}})
		if err != nil {
			return db.DataScope{}, err
		}
		return scope, nil
	})
}

func (service Service) UpdateScope(ctx context.Context, actorID string, enterpriseID, scopeID uuid.UUID, input ScopeInput) (db.DataScope, error) {
	normalized, hash, err := NormalizeSelector(input.LabelSelector)
	if err != nil {
		return db.DataScope{}, err
	}
	var scope db.DataScope
	err = service.Store.InTx(ctx, func(queries *db.Queries) error {
		var err error
		scope, err = queries.UpdateDataScope(ctx, db.UpdateDataScopeParams{ID: scopeID, EnterpriseID: enterpriseID, Name: input.Name, Description: input.Description,
			ResourceTypes: input.ResourceTypes, ExplicitResourceIds: input.ExplicitResourceIDs, LabelSelector: normalized, SelectorHash: hash,
			Status: textValue(input.Status), ExpectedVersion: input.ExpectedVersion})
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if err := service.invalidateEnterprise(ctx, queries, enterpriseID, "data_scope", scopeID.String()); err != nil {
			return err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: identity.ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "data_scope.update", ResourceType: "data_scope", ResourceID: scopeID.String(), Result: "success", Details: map[string]any{"summary": "data scope updated"}})
		return err
	})
	return scope, err
}

func (service Service) ListBindings(ctx context.Context, enterpriseID uuid.UUID) ([]BindingRecord, error) {
	bindings, err := service.Store.Queries.ListRoleBindings(ctx, enterpriseID)
	if err != nil {
		return nil, err
	}
	result := make([]BindingRecord, 0, len(bindings))
	for _, binding := range bindings {
		scopes, err := service.Store.Queries.ListRoleBindingDataScopes(ctx, binding.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, BindingRecord{Binding: binding, DataScopeIDs: scopes})
	}
	return result, nil
}

func (service Service) CreateBinding(ctx context.Context, actorID string, enterpriseID uuid.UUID, input BindingInput, idempotencyKey string) (BindingRecord, error) {
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "role_binding.create", idempotencyKey, input, 201, func(queries *db.Queries) (BindingRecord, error) {
		binding, err := queries.CreateRoleBinding(ctx, db.CreateRoleBindingParams{ID: newID(), EnterpriseID: enterpriseID, SubjectType: input.SubjectType,
			SubjectID: input.SubjectID, RoleID: input.RoleID, ValidFrom: timeValue(input.ValidFrom), ValidUntil: timeValue(input.ValidUntil)})
		if err != nil {
			return BindingRecord{}, err
		}
		for _, scopeID := range input.DataScopeIDs {
			if err := queries.AddRoleBindingDataScope(ctx, db.AddRoleBindingDataScopeParams{RoleBindingID: binding.ID, DataScopeID: scopeID, EnterpriseID: enterpriseID}); err != nil {
				return BindingRecord{}, err
			}
		}
		if err := service.invalidateEnterprise(ctx, queries, enterpriseID, "role_binding", binding.ID.String()); err != nil {
			return BindingRecord{}, err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: identity.ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "role_binding.create", ResourceType: "role_binding", ResourceID: binding.ID.String(), Result: "success", Details: map[string]any{"summary": "role binding created"}})
		if err != nil {
			return BindingRecord{}, err
		}
		return BindingRecord{Binding: binding, DataScopeIDs: append([]uuid.UUID(nil), input.DataScopeIDs...)}, nil
	})
}

func (service Service) UpdateBinding(ctx context.Context, actorID string, enterpriseID, bindingID uuid.UUID, input BindingInput) (BindingRecord, error) {
	var result BindingRecord
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		binding, err := queries.UpdateRoleBinding(ctx, db.UpdateRoleBindingParams{ID: bindingID, EnterpriseID: enterpriseID, ExpectedVersion: input.ExpectedVersion,
			ValidFrom: timeValue(input.ValidFrom), SetValidFrom: input.SetValidFrom, ValidUntil: timeValue(input.ValidUntil), SetValidUntil: input.SetValidUntil, Status: textValue(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if input.DataScopeIDs != nil {
			if err := queries.ReplaceRoleBindingDataScopes(ctx, bindingID); err != nil {
				return err
			}
			for _, scopeID := range input.DataScopeIDs {
				if err := queries.AddRoleBindingDataScope(ctx, db.AddRoleBindingDataScopeParams{RoleBindingID: bindingID, DataScopeID: scopeID, EnterpriseID: enterpriseID}); err != nil {
					return err
				}
			}
		}
		scopes, err := queries.ListRoleBindingDataScopes(ctx, bindingID)
		if err != nil {
			return err
		}
		if err := service.invalidateEnterprise(ctx, queries, enterpriseID, "role_binding", bindingID.String()); err != nil {
			return err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: identity.ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "role_binding.update", ResourceType: "role_binding", ResourceID: bindingID.String(), Result: "success", Details: map[string]any{"summary": "role binding updated"}})
		result = BindingRecord{Binding: binding, DataScopeIDs: scopes}
		return err
	})
	return result, err
}

func (service Service) invalidateEnterprise(ctx context.Context, queries *db.Queries, enterpriseID uuid.UUID, aggregateType, aggregateID string) error {
	affected, err := queries.BumpEnterpriseUsersAuthorizationVersion(ctx, enterpriseID)
	if err != nil {
		return err
	}
	serviceAccounts, err := queries.BumpEnterpriseServiceAccountsAuthorizationVersion(ctx, enterpriseID)
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(map[string]any{"enterprise_id": enterpriseID.String(), "affected_user_count": len(affected), "affected_service_account_count": len(serviceAccounts)})
	return queries.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: newID(), Topic: "authorization.changed", AggregateType: aggregateType, AggregateID: aggregateID, Payload: payload})
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}
func textValue(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}
func optionalText(value string) pgtype.Text { return pgtype.Text{String: value, Valid: value != ""} }
func timeValue(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: value.UTC(), Valid: true}
}
