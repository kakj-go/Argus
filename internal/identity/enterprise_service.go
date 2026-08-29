package identity

import (
	"context"
	"encoding/json"
	"errors"
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
	ErrDefaultDepartment  = errors.New("default department is immutable")
	ErrDepartmentNotEmpty = errors.New("department has active members")
)

type EnterpriseService struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
}

type CreatedEnterpriseCredential struct {
	User              db.EnterpriseUser
	TemporaryPassword string
	ExpiresAt         time.Time
}

type EnterpriseUserUpdate struct {
	DisplayName     *string
	Email           *string
	SetEmail        bool
	DepartmentID    *uuid.UUID
	Status          *string
	ExpectedVersion int64
}

type DepartmentUpdate struct {
	Name, Description, Status *string
	ExpectedVersion           int64
}

func (service EnterpriseService) ListUsers(ctx context.Context, enterpriseID uuid.UUID) ([]db.EnterpriseUser, error) {
	return service.Store.Queries.ListEnterpriseUsers(ctx, enterpriseID)
}

func (service EnterpriseService) GetUser(ctx context.Context, enterpriseID, userID uuid.UUID) (db.EnterpriseUser, error) {
	return service.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: userID, EnterpriseID: enterpriseID})
}

func (service EnterpriseService) CreateUser(ctx context.Context, actorID string, enterpriseID uuid.UUID, username, displayName, email string, departmentID uuid.UUID, roleIDs []uuid.UUID, idempotencyKey string) (CreatedEnterpriseCredential, error) {
	password, err := RandomToken(24)
	if err != nil {
		return CreatedEnterpriseCredential{}, err
	}
	if err := ValidatePassword(password, username, email); err != nil {
		return CreatedEnterpriseCredential{}, err
	}
	encoded, err := HashPassword(password)
	if err != nil {
		return CreatedEnterpriseCredential{}, err
	}
	expiresAt := time.Now().UTC().Add(24 * time.Hour)
	request := map[string]any{"enterprise_id": enterpriseID.String(), "username": username, "display_name": displayName, "email": email, "department_id": departmentID.String(), "role_ids": roleIDs}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "identity.create", idempotencyKey, request, 201, func(queries *db.Queries) (CreatedEnterpriseCredential, error) {
		if _, err := queries.GetDepartment(ctx, db.GetDepartmentParams{ID: departmentID, EnterpriseID: enterpriseID}); err != nil {
			return CreatedEnterpriseCredential{}, err
		}
		user, err := queries.CreateEnterpriseUser(ctx, db.CreateEnterpriseUserParams{ID: mustUUIDV7(), EnterpriseID: enterpriseID, DepartmentID: departmentID,
			Username: username, DisplayName: displayName, Email: pgtype.Text{String: email, Valid: email != ""}})
		if err != nil {
			return CreatedEnterpriseCredential{}, err
		}
		if _, err := queries.CreatePasswordCredential(ctx, db.CreatePasswordCredentialParams{ID: mustUUIDV7(), Audience: "enterprise", SubjectID: user.ID,
			EncodedHash: encoded, Temporary: true, ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}}); err != nil {
			return CreatedEnterpriseCredential{}, err
		}
		if _, err := queries.CreateTemporaryCredential(ctx, db.CreateTemporaryCredentialParams{ID: mustUUIDV7(), Audience: "enterprise", UserID: user.ID,
			ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true}}); err != nil {
			return CreatedEnterpriseCredential{}, err
		}
		if err := queries.InitializeAuthorizationVersion(ctx, db.InitializeAuthorizationVersionParams{EnterpriseID: enterpriseID, SubjectType: "user", SubjectID: user.ID}); err != nil {
			return CreatedEnterpriseCredential{}, err
		}
		if len(roleIDs) > 0 {
			for _, roleID := range roleIDs {
				if _, err := queries.GetRole(ctx, db.GetRoleParams{ID: roleID, EnterpriseID: enterpriseID}); err != nil {
					return CreatedEnterpriseCredential{}, err
				}
				_, err := queries.CreateRoleBinding(ctx, db.CreateRoleBindingParams{ID: mustUUIDV7(), EnterpriseID: enterpriseID, SubjectType: "user", SubjectID: user.ID, RoleID: roleID})
				if err != nil {
					return CreatedEnterpriseCredential{}, err
				}
			}
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true},
			ActorType: ActorTypeFromContext(ctx), ActorID: actorID, Action: "identity.create", ResourceType: "enterprise_user", ResourceID: user.ID.String(), Result: "success",
			Details: map[string]any{"summary": "enterprise user created", "username": username}})
		if err != nil {
			return CreatedEnterpriseCredential{}, err
		}
		return CreatedEnterpriseCredential{User: user, TemporaryPassword: password, ExpiresAt: expiresAt}, nil
	})
}

func (service EnterpriseService) UpdateUser(ctx context.Context, actorID string, enterpriseID, userID uuid.UUID, input EnterpriseUserUpdate) (db.EnterpriseUser, error) {
	var user db.EnterpriseUser
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		if input.DepartmentID != nil {
			if _, err := queries.GetDepartment(ctx, db.GetDepartmentParams{ID: *input.DepartmentID, EnterpriseID: enterpriseID}); err != nil {
				return err
			}
		}
		var err error
		user, err = queries.UpdateEnterpriseUser(ctx, db.UpdateEnterpriseUserParams{ID: userID, EnterpriseID: enterpriseID, ExpectedVersion: input.ExpectedVersion,
			DisplayName: textPointer(input.DisplayName), SetEmail: input.SetEmail, Email: textPointer(input.Email), DepartmentID: uuidPointer(input.DepartmentID), Status: textPointer(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		if input.Status != nil && *input.Status == "disabled" {
			if err := queries.RevokeSubjectSessions(ctx, db.RevokeSubjectSessionsParams{Audience: "enterprise", UserID: userID, RevokeReason: pgtype.Text{String: "user_disabled", Valid: true}}); err != nil {
				return err
			}
		}
		if err := revocation.Users(ctx, queries, enterpriseID, []uuid.UUID{userID}, "user_changed"); err != nil {
			return err
		}
		payload, _ := json.Marshal(map[string]any{"enterprise_id": enterpriseID.String(), "user_id": userID.String(), "authorization_version": user.AuthorizationVersion})
		if err := queries.InsertOutboxEvent(ctx, db.InsertOutboxEventParams{ID: mustUUIDV7(), Topic: "authorization.user.changed", AggregateType: "enterprise_user", AggregateID: userID.String(), Payload: payload}); err != nil {
			return err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "identity.update", ResourceType: "enterprise_user", ResourceID: userID.String(), Result: "success", Details: map[string]any{"authorization_version": user.AuthorizationVersion}})
		return err
	})
	return user, err
}

func (service EnterpriseService) ListDepartments(ctx context.Context, enterpriseID uuid.UUID) ([]db.Department, error) {
	return service.Store.Queries.ListDepartments(ctx, enterpriseID)
}

func (service EnterpriseService) CreateDepartment(ctx context.Context, actorID string, enterpriseID uuid.UUID, name, description, idempotencyKey string) (db.Department, error) {
	request := map[string]any{"enterprise_id": enterpriseID.String(), "name": name, "description": description}
	return postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "enterprise", actorID, "department.create", idempotencyKey, request, 201, func(queries *db.Queries) (db.Department, error) {
		department, err := queries.CreateDepartment(ctx, db.CreateDepartmentParams{ID: mustUUIDV7(), EnterpriseID: enterpriseID, Name: name, Description: description})
		if err != nil {
			return db.Department{}, err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "department.create", ResourceType: "department", ResourceID: department.ID.String(), Result: "success", Details: map[string]any{"summary": "department created"}})
		if err != nil {
			return db.Department{}, err
		}
		return department, nil
	})
}

func (service EnterpriseService) UpdateDepartment(ctx context.Context, actorID string, enterpriseID, departmentID uuid.UUID, input DepartmentUpdate) (db.Department, error) {
	var department db.Department
	err := service.Store.InTx(ctx, func(queries *db.Queries) error {
		current, err := queries.GetDepartment(ctx, db.GetDepartmentParams{ID: departmentID, EnterpriseID: enterpriseID})
		if err != nil {
			return err
		}
		if current.IsDefault && input.Status != nil && *input.Status == "disabled" {
			return ErrDefaultDepartment
		}
		if input.Status != nil && *input.Status == "disabled" {
			count, err := queries.CountDepartmentUsers(ctx, db.CountDepartmentUsersParams{EnterpriseID: enterpriseID, DepartmentID: departmentID})
			if err != nil {
				return err
			}
			if count > 0 {
				return ErrDepartmentNotEmpty
			}
		}
		department, err = queries.UpdateDepartment(ctx, db.UpdateDepartmentParams{ID: departmentID, EnterpriseID: enterpriseID, ExpectedVersion: input.ExpectedVersion,
			Name: textPointer(input.Name), Description: textPointer(input.Description), Status: textPointer(input.Status)})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrVersionConflict
		}
		if err != nil {
			return err
		}
		affected, err := queries.BumpDepartmentUsersAuthorizationVersion(ctx, db.BumpDepartmentUsersAuthorizationVersionParams{EnterpriseID: enterpriseID, DepartmentID: departmentID})
		if err != nil {
			return err
		}
		_, err = audit.Append(ctx, queries, audit.Entry{Domain: "enterprise", EnterpriseID: uuid.NullUUID{UUID: enterpriseID, Valid: true}, ActorType: ActorTypeFromContext(ctx), ActorID: actorID,
			Action: "department.update", ResourceType: "department", ResourceID: departmentID.String(), Result: "success", Details: map[string]any{"affected_count": len(affected)}})
		return err
	})
	return department, err
}

func textPointer(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

func uuidPointer(value *uuid.UUID) uuid.NullUUID {
	if value == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *value, Valid: true}
}
