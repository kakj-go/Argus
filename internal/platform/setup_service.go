package platform

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/audit"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

var ErrAlreadyInitialized = errors.New("platform is already initialized")

type SetupInput struct {
	PlatformName  string
	DefaultLocale string
	Timezone      string
	ExternalURL   string
	Username      string
	DisplayName   string
	Email         string
	Password      string
}

type SetupService struct {
	Store       *postgres.Store
	Idempotency postgres.Idempotency
}

func (service SetupService) Status(ctx context.Context) (db.PlatformState, *db.PlatformSetting, error) {
	state, err := service.Store.Queries.GetPlatformState(ctx)
	if err != nil {
		return db.PlatformState{}, nil, err
	}
	settings, err := service.Store.Queries.GetPlatformSettings(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.PlatformState{State: state.State, InitializedAt: state.InitializedAt, UpdatedAt: state.UpdatedAt}, nil, nil
	}
	if err != nil {
		return db.PlatformState{}, nil, err
	}
	return db.PlatformState{State: state.State, InitializedAt: state.InitializedAt, UpdatedAt: state.UpdatedAt}, &settings, nil
}

func (service SetupService) Initialize(ctx context.Context, input SetupInput, idempotencyKey string) (uuid.UUID, error) {
	if err := identity.ValidatePassword(input.Password, input.Username, input.Email); err != nil {
		return uuid.Nil, err
	}
	encodedPassword, err := identity.HashPassword(input.Password)
	if err != nil {
		return uuid.Nil, err
	}
	userID, err := postgres.ExecuteIdempotent(ctx, service.Store, service.Idempotency, "setup", "platform", "setup.initialize", idempotencyKey, input, 201, func(queries *db.Queries) (uuid.UUID, error) {
		userID, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, err
		}
		credentialID, err := uuid.NewV7()
		if err != nil {
			return uuid.Nil, err
		}
		state, err := queries.LockPlatformState(ctx)
		if err != nil {
			return uuid.Nil, err
		}
		if state.State != "uninitialized" {
			return uuid.Nil, ErrAlreadyInitialized
		}
		rows, err := queries.MarkPlatformInitializing(ctx)
		if err != nil || rows != 1 {
			if err != nil {
				return uuid.Nil, err
			}
			return uuid.Nil, ErrAlreadyInitialized
		}
		if _, err := queries.CreatePlatformSettings(ctx, db.CreatePlatformSettingsParams{
			PlatformName: input.PlatformName, DefaultLocale: input.DefaultLocale,
			Timezone: input.Timezone, ExternalUrl: input.ExternalURL,
		}); err != nil {
			return uuid.Nil, err
		}
		if _, err := queries.CreatePlatformUser(ctx, db.CreatePlatformUserParams{
			ID: userID, Username: input.Username, DisplayName: input.DisplayName,
			Email: pgtype.Text{String: input.Email, Valid: input.Email != ""},
		}); err != nil {
			return uuid.Nil, err
		}
		if _, err := queries.CreatePasswordCredential(ctx, db.CreatePasswordCredentialParams{
			ID: credentialID, Audience: "platform", SubjectID: userID,
			EncodedHash: encodedPassword, Temporary: false,
		}); err != nil {
			return uuid.Nil, err
		}
		if err := audit.InitializeChain(ctx, queries, "platform", uuid.NullUUID{}); err != nil {
			return uuid.Nil, err
		}
		if _, err := audit.Append(ctx, queries, audit.Entry{
			Domain: "platform", ActorType: "platform_user", ActorID: userID.String(),
			Action: "setup.initialize", ResourceType: "platform", ResourceID: "singleton",
			Result: "success", Details: map[string]any{"summary": "platform initialized", "username": input.Username},
		}); err != nil {
			return uuid.Nil, err
		}
		if err := queries.MarkPlatformInitialized(ctx); err != nil {
			return uuid.Nil, err
		}
		return userID, nil
	})
	if err != nil {
		return uuid.Nil, fmt.Errorf("initialize platform: %w", err)
	}
	return userID, nil
}

func temporaryExpiry() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().UTC().Add(24 * time.Hour), Valid: true}
}
