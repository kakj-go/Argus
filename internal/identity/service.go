package identity

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/keywrap"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
)

var (
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrTemporaryExpired     = errors.New("temporary credential expired")
	ErrSessionInvalid       = errors.New("session is invalid")
	ErrSessionExpired       = errors.New("session expired")
	ErrSessionRevoked       = errors.New("session revoked")
	ErrAudienceMismatch     = errors.New("session audience mismatch")
	ErrAuthorizationVersion = errors.New("authorization version stale")
	ErrEnterpriseSuspended  = errors.New("enterprise suspended")
	ErrEnterpriseDisabled   = errors.New("enterprise disabled")
	ErrLoginDependency      = errors.New("login rate limiter unavailable")
	ErrLoginRateLimited     = errors.New("login rate limited")
	ErrVersionConflict      = errors.New("resource version conflict")
	ErrCSRFInvalid          = errors.New("csrf token invalid")
	ErrMFARequired          = errors.New("mfa required")
	ErrMFAInvalid           = errors.New("mfa proof invalid")
	ErrMFAEnrollment        = errors.New("mfa enrollment required")
	ErrStepUpRequired       = errors.New("step-up authentication required")
	ErrBreakGlassDisabled   = errors.New("break-glass is disabled")
)

type Service struct {
	Store             *postgres.Store
	Redis             *redisstore.Client
	IdleTTL           time.Duration
	AbsoluteTTL       time.Duration
	Now               func() time.Time
	KeyWrapping       keywrap.Provider
	Idempotency       postgres.Idempotency
	BreakGlassEnabled bool
}

type LoginResult struct {
	Challenge *PasswordChallenge
	MFA       *MFAChallenge
	Session   *IssuedSession
}

type MFAChallenge struct {
	Token     string
	Audience  string
	ExpiresAt time.Time
}

type PasswordChallenge struct {
	Token     string
	Audience  string
	ExpiresAt time.Time
}

type IssuedSession struct {
	Principal Principal
	Token     string
	CSRFToken string
}

type Principal struct {
	Session        db.Session
	PlatformUser   *db.PlatformUser
	EnterpriseUser *db.EnterpriseUser
	ServiceAccount *db.ServiceAccount
	DataScopeIDs   []uuid.UUID
	Permissions    []string
}

type actorContextKey struct{}

func (principal Principal) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, actorContextKey{}, principal.ActorType())
}

func ActorTypeFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(actorContextKey{}).(string); ok && value != "" {
		return value
	}
	return "enterprise_user"
}

func (principal Principal) EnterpriseID() (uuid.UUID, bool) {
	if principal.EnterpriseUser != nil {
		return principal.EnterpriseUser.EnterpriseID, true
	}
	if principal.ServiceAccount != nil {
		return principal.ServiceAccount.EnterpriseID, true
	}
	return uuid.Nil, false
}

func (principal Principal) EnterpriseIDValue() uuid.UUID {
	value, _ := principal.EnterpriseID()
	return value
}

func (principal Principal) ActorID() string {
	if principal.EnterpriseUser != nil {
		return principal.EnterpriseUser.ID.String()
	}
	if principal.ServiceAccount != nil {
		return principal.ServiceAccount.ID.String()
	}
	if principal.PlatformUser != nil {
		return principal.PlatformUser.ID.String()
	}
	return ""
}

func (principal Principal) ActorType() string {
	if principal.EnterpriseUser != nil {
		return "enterprise_user"
	}
	if principal.ServiceAccount != nil {
		return "service_account"
	}
	if principal.PlatformUser != nil {
		return "platform_user"
	}
	return "system"
}

func (principal Principal) AuthorizationVersion() int64 {
	if principal.EnterpriseUser != nil {
		return principal.EnterpriseUser.AuthorizationVersion
	}
	if principal.ServiceAccount != nil {
		return principal.ServiceAccount.AuthorizationVersion
	}
	return 0
}

func (service Service) Login(ctx context.Context, audience, username, password, ip string) (LoginResult, error) {
	if err := service.checkLoginLimit(ctx, audience, username, ip); err != nil {
		return LoginResult{}, err
	}
	userID, credential, err := service.lookupCredential(ctx, audience, username)
	if err != nil {
		return LoginResult{}, ErrInvalidCredentials
	}
	valid, verifyErr := VerifyPassword(credential.EncodedHash, password)
	if verifyErr != nil || !valid {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err := service.clearLoginLimit(ctx, audience, username, ip); err != nil {
		return LoginResult{}, err
	}
	if credential.Temporary {
		if !credential.ExpiresAt.Valid || !service.now().Before(credential.ExpiresAt.Time) {
			return LoginResult{}, ErrTemporaryExpired
		}
		temporary, err := service.Store.Queries.GetActiveTemporaryCredential(ctx, db.GetActiveTemporaryCredentialParams{Audience: audience, UserID: userID})
		if err != nil {
			return LoginResult{}, ErrTemporaryExpired
		}
		challenge, err := RandomToken(32)
		if err != nil {
			return LoginResult{}, err
		}
		if _, err := service.Store.Queries.SetTemporaryCredentialChallenge(ctx, db.SetTemporaryCredentialChallengeParams{ID: temporary.ID, ChallengeHash: TokenHash(challenge)}); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{Challenge: &PasswordChallenge{Token: challenge, Audience: audience, ExpiresAt: temporary.ExpiresAt.Time}}, nil
	}
	if mfa, err := service.Store.Queries.GetMfaCredential(ctx, db.GetMfaCredentialParams{Audience: audience, UserID: userID}); err == nil && mfa.Status == "active" {
		challenge, err := service.createMFAChallenge(ctx, audience, userID, "login")
		if err != nil {
			return LoginResult{}, err
		}
		return LoginResult{MFA: &challenge}, nil
	}
	issued, err := service.issueSession(ctx, service.Store.Queries, audience, userID)
	if err != nil {
		return LoginResult{}, err
	}
	if audience == "platform" {
		_ = service.Store.Queries.MarkPlatformLogin(ctx, userID)
	} else {
		_ = service.Store.Queries.MarkEnterpriseLogin(ctx, userID)
	}
	return LoginResult{Session: &issued}, nil
}

func (service Service) CompletePasswordChange(ctx context.Context, audience, challenge, temporaryPassword, newPassword string) (IssuedSession, error) {
	temporary, err := service.Store.Queries.GetTemporaryCredentialByChallenge(ctx, TokenHash(challenge))
	if err != nil || temporary.Audience != audience {
		return IssuedSession{}, ErrTemporaryExpired
	}
	credential, err := service.Store.Queries.GetPasswordCredential(ctx, db.GetPasswordCredentialParams{Audience: audience, SubjectID: temporary.UserID})
	if err != nil || !credential.Temporary {
		return IssuedSession{}, ErrTemporaryExpired
	}
	valid, verifyErr := VerifyPassword(credential.EncodedHash, temporaryPassword)
	if verifyErr != nil || !valid {
		return IssuedSession{}, ErrInvalidCredentials
	}
	username, email, err := service.identityStrings(ctx, audience, temporary.UserID)
	if err != nil {
		return IssuedSession{}, err
	}
	if err := ValidatePassword(newPassword, username, email); err != nil {
		return IssuedSession{}, err
	}
	encoded, err := HashPassword(newPassword)
	if err != nil {
		return IssuedSession{}, err
	}
	var issued IssuedSession
	err = service.Store.InTx(ctx, func(queries *db.Queries) error {
		if _, err := queries.UpdatePasswordCredential(ctx, db.UpdatePasswordCredentialParams{
			Audience: audience, SubjectID: temporary.UserID, EncodedHash: encoded, Version: credential.Version,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrVersionConflict
			}
			return err
		}
		rows, err := queries.ConsumeTemporaryCredential(ctx, temporary.ID)
		if err != nil || rows != 1 {
			return ErrTemporaryExpired
		}
		if err := queries.RevokeSubjectSessions(ctx, db.RevokeSubjectSessionsParams{Audience: audience, UserID: temporary.UserID, RevokeReason: pgtype.Text{String: "password_changed", Valid: true}}); err != nil {
			return err
		}
		issued, err = service.issueSession(ctx, queries, audience, temporary.UserID)
		return err
	})
	return issued, err
}

func (service Service) Authenticate(ctx context.Context, audience, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrSessionInvalid
	}
	session, err := service.Store.Queries.GetSessionByTokenHash(ctx, TokenHash(token))
	if err != nil {
		return Principal{}, ErrSessionInvalid
	}
	if session.Audience != audience {
		return Principal{}, ErrAudienceMismatch
	}
	if session.RevokedAt.Valid {
		return Principal{}, ErrSessionRevoked
	}
	now := service.now()
	if !now.Before(session.IdleExpiresAt.Time) || !now.Before(session.AbsoluteExpiresAt.Time) {
		return Principal{}, ErrSessionExpired
	}
	principal, err := service.loadPrincipal(ctx, session)
	if err != nil {
		return Principal{}, err
	}
	idleExpires := now.Add(service.idleTTL())
	if idleExpires.After(session.AbsoluteExpiresAt.Time) {
		idleExpires = session.AbsoluteExpiresAt.Time
	}
	session, err = service.Store.Queries.TouchSession(ctx, db.TouchSessionParams{
		ID: session.ID, LastSeenAt: pgtype.Timestamptz{Time: now, Valid: true},
		IdleExpiresAt: pgtype.Timestamptz{Time: idleExpires, Valid: true},
	})
	if err != nil {
		return Principal{}, ErrSessionExpired
	}
	principal.Session = session
	return principal, nil
}

func (service Service) ValidateCSRF(principal Principal, token string) error {
	if token == "" || subtle.ConstantTimeCompare(principal.Session.CsrfHash, TokenHash(token)) != 1 {
		return ErrCSRFInvalid
	}
	return nil
}

func (service Service) Logout(ctx context.Context, principal Principal) error {
	_, err := service.Store.Queries.RevokeSession(ctx, db.RevokeSessionParams{ID: principal.Session.ID, RevokeReason: pgtype.Text{String: "logout", Valid: true}})
	return err
}

func (service Service) ChangePassword(ctx context.Context, principal Principal, currentPassword, newPassword string, expectedVersion int64) error {
	credential, err := service.Store.Queries.GetPasswordCredential(ctx, db.GetPasswordCredentialParams{Audience: principal.Session.Audience, SubjectID: principal.Session.UserID})
	if err != nil {
		return ErrInvalidCredentials
	}
	valid, verifyErr := VerifyPassword(credential.EncodedHash, currentPassword)
	if verifyErr != nil || !valid {
		return ErrInvalidCredentials
	}
	if credential.Version != expectedVersion {
		return ErrVersionConflict
	}
	username, email, err := service.identityStrings(ctx, principal.Session.Audience, principal.Session.UserID)
	if err != nil {
		return err
	}
	if err := ValidatePassword(newPassword, username, email); err != nil {
		return err
	}
	encoded, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	return service.Store.InTx(ctx, func(queries *db.Queries) error {
		if _, err := queries.UpdatePasswordCredential(ctx, db.UpdatePasswordCredentialParams{
			Audience: principal.Session.Audience, SubjectID: principal.Session.UserID,
			EncodedHash: encoded, Version: expectedVersion,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrVersionConflict
			}
			return err
		}
		return queries.RevokeSubjectSessions(ctx, db.RevokeSubjectSessionsParams{
			Audience: principal.Session.Audience, UserID: principal.Session.UserID,
			RevokeReason: pgtype.Text{String: "password_changed", Valid: true},
		})
	})
}

func (service Service) issueSession(ctx context.Context, queries *db.Queries, audience string, userID uuid.UUID) (IssuedSession, error) {
	return service.issueSessionWithAMR(ctx, queries, audience, userID, []string{"password"})
}

func (service Service) issueSessionWithAMR(ctx context.Context, queries *db.Queries, audience string, userID uuid.UUID, amr []string) (IssuedSession, error) {
	token, err := RandomToken(32)
	if err != nil {
		return IssuedSession{}, err
	}
	csrf, err := RandomToken(32)
	if err != nil {
		return IssuedSession{}, err
	}
	now := service.now()
	params := db.CreateSessionParams{
		ID: mustUUIDV7(), TokenHash: TokenHash(token), CsrfHash: TokenHash(csrf), Audience: audience,
		UserID: userID, Locale: "zh-CN", IdleExpiresAt: pgtype.Timestamptz{Time: now.Add(service.idleTTL()), Valid: true},
		AbsoluteExpiresAt: pgtype.Timestamptz{Time: now.Add(service.absoluteTTL()), Valid: true}, LastSeenAt: pgtype.Timestamptz{Time: now, Valid: true},
		AuthenticatedAt: pgtype.Timestamptz{Time: now, Valid: true}, Amr: amr,
	}
	if audience == "enterprise" {
		enterpriseUser, err := queries.GetEnterpriseUserByID(ctx, userID)
		if err != nil {
			return IssuedSession{}, err
		}
		params.EnterpriseID = uuid.NullUUID{UUID: enterpriseUser.EnterpriseID, Valid: true}
		params.DepartmentID = uuid.NullUUID{UUID: enterpriseUser.DepartmentID, Valid: true}
		params.AuthorizationVersion = pgtype.Int8{Int64: enterpriseUser.AuthorizationVersion, Valid: true}
	}
	session, err := queries.CreateSession(ctx, params)
	if err != nil {
		return IssuedSession{}, err
	}
	principal, err := service.loadPrincipal(ctx, session)
	if err != nil {
		return IssuedSession{}, err
	}
	return IssuedSession{Principal: principal, Token: token, CSRFToken: csrf}, nil
}

func (service Service) loadPrincipal(ctx context.Context, session db.Session) (Principal, error) {
	principal := Principal{Session: session}
	if session.Audience == "platform" {
		user, err := service.Store.Queries.GetPlatformUser(ctx, session.UserID)
		if err != nil || user.Status != "active" {
			return Principal{}, ErrSessionRevoked
		}
		principal.PlatformUser = &user
		principal.Permissions = []string{"*"}
		return principal, nil
	}
	if !session.EnterpriseID.Valid || !session.DepartmentID.Valid || !session.AuthorizationVersion.Valid {
		return Principal{}, ErrSessionInvalid
	}
	user, err := service.Store.Queries.GetEnterpriseUser(ctx, db.GetEnterpriseUserParams{ID: session.UserID, EnterpriseID: session.EnterpriseID.UUID})
	if err != nil || user.Status != "active" {
		return Principal{}, ErrSessionRevoked
	}
	enterprise, err := service.Store.Queries.GetEnterprise(ctx, session.EnterpriseID.UUID)
	if err != nil {
		return Principal{}, ErrSessionRevoked
	}
	if enterprise.Status == "suspended" {
		return Principal{}, ErrEnterpriseSuspended
	}
	if enterprise.Status == "disabled" {
		return Principal{}, ErrEnterpriseDisabled
	}
	department, err := service.Store.Queries.GetDepartment(ctx, db.GetDepartmentParams{ID: user.DepartmentID, EnterpriseID: user.EnterpriseID})
	if err != nil || department.Status != "active" {
		return Principal{}, ErrSessionRevoked
	}
	if user.AuthorizationVersion != session.AuthorizationVersion.Int64 {
		return Principal{}, ErrAuthorizationVersion
	}
	permissions, err := service.Store.Queries.ListEffectiveUserPermissions(ctx, db.ListEffectiveUserPermissionsParams{EnterpriseID: user.EnterpriseID, UserID: user.ID, DepartmentID: user.DepartmentID})
	if err != nil {
		return Principal{}, err
	}
	scopes, err := service.Store.Queries.ListEffectiveUserDataScopes(ctx, db.ListEffectiveUserDataScopesParams{EnterpriseID: user.EnterpriseID, UserID: user.ID, DepartmentID: user.DepartmentID})
	if err != nil {
		return Principal{}, err
	}
	principal.EnterpriseUser = &user
	principal.Permissions = permissions
	principal.DataScopeIDs = scopes
	return principal, nil
}

func (service Service) lookupCredential(ctx context.Context, audience, username string) (uuid.UUID, db.PasswordCredential, error) {
	var userID uuid.UUID
	if audience == "platform" {
		user, err := service.Store.Queries.GetPlatformUserByUsername(ctx, username)
		if err != nil || user.Status != "active" {
			return uuid.Nil, db.PasswordCredential{}, ErrInvalidCredentials
		}
		userID = user.ID
	} else if audience == "enterprise" {
		user, err := service.Store.Queries.GetEnterpriseUserByUsername(ctx, username)
		if err != nil || user.Status != "active" || user.DepartmentStatus != "active" {
			return uuid.Nil, db.PasswordCredential{}, ErrInvalidCredentials
		}
		if user.EnterpriseStatus == "suspended" {
			return uuid.Nil, db.PasswordCredential{}, ErrEnterpriseSuspended
		}
		if user.EnterpriseStatus == "disabled" {
			return uuid.Nil, db.PasswordCredential{}, ErrEnterpriseDisabled
		}
		userID = user.ID
	} else {
		return uuid.Nil, db.PasswordCredential{}, ErrAudienceMismatch
	}
	credential, err := service.Store.Queries.GetPasswordCredential(ctx, db.GetPasswordCredentialParams{Audience: audience, SubjectID: userID})
	return userID, credential, err
}

func (service Service) identityStrings(ctx context.Context, audience string, userID uuid.UUID) (string, string, error) {
	if audience == "platform" {
		user, err := service.Store.Queries.GetPlatformUser(ctx, userID)
		return user.Username, user.Email.String, err
	}
	var username string
	var email pgtype.Text
	err := service.Store.Pool.QueryRow(ctx, `SELECT username, email FROM enterprise_users WHERE id=$1`, userID).Scan(&username, &email)
	return username, email.String, err
}

func (service Service) checkLoginLimit(ctx context.Context, audience, username, ip string) error {
	if service.Redis == nil || service.Redis.Raw == nil {
		return ErrLoginDependency
	}
	key := loginLimitKey(audience, username, ip)
	count, err := service.Redis.Raw.Incr(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLoginDependency, err)
	}
	if count == 1 {
		if err := service.Redis.Raw.Expire(ctx, key, 15*time.Minute).Err(); err != nil {
			return fmt.Errorf("%w: %v", ErrLoginDependency, err)
		}
	}
	if count > 5 {
		return ErrLoginRateLimited
	}
	return nil
}

func (service Service) clearLoginLimit(ctx context.Context, audience, username, ip string) error {
	if service.Redis == nil || service.Redis.Raw == nil {
		return ErrLoginDependency
	}
	if err := service.Redis.Raw.Del(ctx, loginLimitKey(audience, username, ip)).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrLoginDependency, err)
	}
	return nil
}

func loginLimitKey(audience, username, ip string) string {
	return "argus:login:" + audience + ":" + strings.ToLower(username) + ":" + ip
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now().UTC()
	}
	return time.Now().UTC()
}

func (service Service) idleTTL() time.Duration {
	if service.IdleTTL > 0 {
		return service.IdleTTL
	}
	return 30 * time.Minute
}

func (service Service) absoluteTTL() time.Duration {
	if service.AbsoluteTTL > 0 {
		return service.AbsoluteTTL
	}
	return 12 * time.Hour
}

func mustUUIDV7() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}
