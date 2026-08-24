package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/kakj-go/Argus/internal/config"
	setupapi "github.com/kakj-go/Argus/internal/gen/openapi/setup"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/platform"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type SetupHandler struct {
	Config   config.Server
	Setup    platform.SetupService
	Identity identity.Service
	Token    platform.SetupTokenProvider
}

func (handler SetupHandler) GetSetupStatus(ctx context.Context, _ setupapi.GetSetupStatusRequestObject) (setupapi.GetSetupStatusResponseObject, error) {
	state, settings, err := handler.Setup.Status(ctx)
	if err != nil {
		return setupapi.GetSetupStatusdefaultJSONResponse{Body: setupError(ctx, err), StatusCode: http.StatusServiceUnavailable}, nil
	}
	response := setupapi.SetupStatus{State: setupapi.PlatformState(state.State)}
	if settings != nil {
		response.PlatformName = &settings.PlatformName
	}
	return setupapi.GetSetupStatus200JSONResponse(response), nil
}

func (handler SetupHandler) InitializePlatform(ctx context.Context, request setupapi.InitializePlatformRequestObject) (setupapi.InitializePlatformResponseObject, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok || request.Body == nil || request.Body.SuperAdmin.Password == nil {
		return setupapi.InitializePlatformdefaultJSONResponse{Body: setupError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	if err := handler.Token.Verify(metadata.Request.Header.Get("X-Argus-Setup-Token")); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, platform.ErrSetupTokenUnavailable) {
			status = http.StatusServiceUnavailable
		}
		return setupapi.InitializePlatformdefaultJSONResponse{Body: setupError(ctx, err), StatusCode: status}, nil
	}
	email := ""
	if request.Body.SuperAdmin.Email != nil {
		email = string(*request.Body.SuperAdmin.Email)
	}
	userID, err := handler.Setup.Initialize(ctx, platform.SetupInput{
		PlatformName: request.Body.PlatformName, DefaultLocale: string(request.Body.DefaultLocale),
		Timezone: request.Body.Timezone, ExternalURL: request.Body.ExternalUrl,
		Username: request.Body.SuperAdmin.Username, DisplayName: request.Body.SuperAdmin.DisplayName,
		Email: email, Password: *request.Body.SuperAdmin.Password,
	}, request.Params.IdempotencyKey)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, platform.ErrAlreadyInitialized) {
			status = http.StatusConflict
		}
		if errors.Is(err, identity.ErrWeakPassword) {
			status = http.StatusUnprocessableEntity
		}
		return setupapi.InitializePlatformdefaultJSONResponse{Body: setupError(ctx, err), StatusCode: status}, nil
	}
	return setupapi.InitializePlatform201JSONResponse{
		State: setupapi.SetupInitializeResultStateInitialized, PlatformUserId: openapi_types.UUID(userID),
	}, nil
}

func (handler SetupHandler) Login(ctx context.Context, request setupapi.LoginRequestObject) (setupapi.LoginResponseObject, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok || request.Body == nil || request.Body.Password == nil {
		return setupapi.LogindefaultJSONResponse{Body: setupError(ctx, identity.ErrInvalidCredentials), StatusCode: http.StatusBadRequest}, nil
	}
	result, err := handler.Identity.Login(ctx, string(request.Audience), request.Body.Username, *request.Body.Password, metadata.ClientIP)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, identity.ErrLoginRateLimited) {
			status = http.StatusTooManyRequests
		}
		if errors.Is(err, identity.ErrLoginDependency) {
			status = http.StatusServiceUnavailable
		}
		if errors.Is(err, identity.ErrEnterpriseSuspended) || errors.Is(err, identity.ErrEnterpriseDisabled) {
			status = http.StatusForbidden
		}
		return setupapi.LogindefaultJSONResponse{Body: setupError(ctx, err), StatusCode: status}, nil
	}
	var response setupapi.LoginResult
	if result.Challenge != nil {
		challengeID := result.Challenge.Token
		_ = response.FromLoginResult1(setupapi.LoginResult1{
			Status: setupapi.PasswordChangeRequired,
			PasswordChangeChallenge: setupapi.PasswordChangeChallenge{
				ChallengeId: &challengeID, Audience: setupapi.PasswordChangeChallengeAudience(result.Challenge.Audience), ExpiresAt: result.Challenge.ExpiresAt,
			},
		})
		return setupapi.Login200JSONResponse(response), nil
	}
	if result.MFA != nil {
		challengeID := result.MFA.Token
		_ = response.FromLoginResult2(setupapi.LoginResult2{
			Status:       setupapi.MfaRequired,
			MfaChallenge: setupapi.MfaChallenge{ChallengeId: &challengeID, Audience: setupapi.MfaChallengeAudience(result.MFA.Audience), ExpiresAt: result.MFA.ExpiresAt},
		})
		return setupapi.Login200JSONResponse(response), nil
	}
	setSessionCookies(metadata.Writer, string(request.Audience), *result.Session, handler.Config)
	authenticated := toAuthenticatedSession(*result.Session, handler.Config.PlatformMFARequired)
	_ = response.FromLoginResult0(setupapi.LoginResult0{Status: setupapi.Authenticated, AuthenticatedSession: authenticated})
	return setupapi.Login200JSONResponse(response), nil
}

func (handler SetupHandler) CompletePasswordChange(ctx context.Context, request setupapi.CompletePasswordChangeRequestObject) (setupapi.CompletePasswordChangeResponseObject, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok || request.Body == nil || request.Body.ChallengeId == nil || request.Body.TemporaryPassword == nil || request.Body.NewPassword == nil {
		return setupapi.CompletePasswordChangedefaultJSONResponse{Body: setupError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	issued, err := handler.Identity.CompletePasswordChange(ctx, string(request.Audience), *request.Body.ChallengeId, *request.Body.TemporaryPassword, *request.Body.NewPassword)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, identity.ErrWeakPassword) {
			status = http.StatusUnprocessableEntity
		}
		if errors.Is(err, identity.ErrTemporaryExpired) {
			status = http.StatusGone
		}
		return setupapi.CompletePasswordChangedefaultJSONResponse{Body: setupError(ctx, err), StatusCode: status}, nil
	}
	setSessionCookies(metadata.Writer, string(request.Audience), issued, handler.Config)
	return setupapi.CompletePasswordChange200JSONResponse(toAuthenticatedSession(issued, handler.Config.PlatformMFARequired)), nil
}

func (handler SetupHandler) GetAuthenticatedSession(ctx context.Context, request setupapi.GetAuthenticatedSessionRequestObject) (setupapi.GetAuthenticatedSessionResponseObject, error) {
	audience := string(request.Audience)
	principal, err := handler.authenticate(ctx, audience, false, "")
	if err != nil {
		return setupapi.GetAuthenticatedSessiondefaultJSONResponse{Body: setupError(ctx, err), StatusCode: authStatus(err)}, nil
	}
	metadata, ok := RequestFromContext(ctx)
	if !ok {
		return setupapi.GetAuthenticatedSessiondefaultJSONResponse{Body: setupError(ctx, identity.ErrSessionInvalid), StatusCode: http.StatusUnauthorized}, nil
	}
	csrfCookie, err := metadata.Request.Cookie(csrfCookieName(audience))
	if err != nil || handler.Identity.ValidateCSRF(principal, csrfCookie.Value) != nil {
		return setupapi.GetAuthenticatedSessiondefaultJSONResponse{Body: setupError(ctx, identity.ErrCSRFInvalid), StatusCode: http.StatusForbidden}, nil
	}
	return setupapi.GetAuthenticatedSession200JSONResponse(toAuthenticatedSession(identity.IssuedSession{Principal: principal, CSRFToken: csrfCookie.Value}, handler.Config.PlatformMFARequired)), nil
}

func (handler SetupHandler) Logout(ctx context.Context, request setupapi.LogoutRequestObject) (setupapi.LogoutResponseObject, error) {
	principal, err := handler.authenticate(ctx, string(request.Audience), true, request.Params.XCSRFToken)
	if err != nil {
		return setupapi.LogoutdefaultJSONResponse{Body: setupError(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if err := handler.Identity.Logout(ctx, principal); err != nil {
		return setupapi.LogoutdefaultJSONResponse{Body: setupError(ctx, err), StatusCode: http.StatusInternalServerError}, nil
	}
	if metadata, ok := RequestFromContext(ctx); ok {
		clearSessionCookies(metadata.Writer, string(request.Audience), handler.Config)
	}
	return setupapi.Logout204Response{}, nil
}

func (handler SetupHandler) UpdateOwnPassword(ctx context.Context, request setupapi.UpdateOwnPasswordRequestObject) (setupapi.UpdateOwnPasswordResponseObject, error) {
	principal, err := handler.authenticate(ctx, string(request.Audience), true, request.Params.XCSRFToken)
	if err != nil {
		return setupapi.UpdateOwnPassworddefaultJSONResponse{Body: setupError(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if request.Body == nil || request.Body.CurrentPassword == nil || request.Body.NewPassword == nil {
		return setupapi.UpdateOwnPassworddefaultJSONResponse{Body: setupError(ctx, errors.New("invalid request")), StatusCode: http.StatusBadRequest}, nil
	}
	err = handler.Identity.ChangePassword(ctx, principal, *request.Body.CurrentPassword, *request.Body.NewPassword, request.Body.ExpectedVersion)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, identity.ErrWeakPassword) {
			status = http.StatusUnprocessableEntity
		}
		if errors.Is(err, identity.ErrVersionConflict) {
			status = http.StatusConflict
		}
		return setupapi.UpdateOwnPassworddefaultJSONResponse{Body: setupError(ctx, err), StatusCode: status}, nil
	}
	if metadata, ok := RequestFromContext(ctx); ok {
		clearSessionCookies(metadata.Writer, string(request.Audience), handler.Config)
	}
	return setupapi.UpdateOwnPassword204Response{}, nil
}

func (handler SetupHandler) authenticate(ctx context.Context, audience string, mutation bool, csrf string) (identity.Principal, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok {
		return identity.Principal{}, identity.ErrSessionInvalid
	}
	cookie, err := metadata.Request.Cookie(sessionCookieName(audience))
	if err != nil {
		return identity.Principal{}, identity.ErrSessionInvalid
	}
	principal, err := handler.Identity.Authenticate(ctx, audience, cookie.Value)
	if err != nil {
		return identity.Principal{}, err
	}
	if requiresMFAEnrollment(principal, metadata.Request.URL.Path, handler.Config.PlatformMFARequired) {
		return identity.Principal{}, identity.ErrMFAEnrollment
	}
	if !mutation {
		return principal, nil
	}
	if !slices.Contains(handler.Config.AllowedOrigins, metadata.Request.Header.Get("Origin")) {
		return identity.Principal{}, errors.New("origin not allowed")
	}
	csrfCookie, err := metadata.Request.Cookie(csrfCookieName(audience))
	if err != nil || csrfCookie.Value != csrf {
		return identity.Principal{}, identity.ErrCSRFInvalid
	}
	if err := handler.Identity.ValidateCSRF(principal, csrf); err != nil {
		return identity.Principal{}, err
	}
	return principal, nil
}

func requiresMFAEnrollment(principal identity.Principal, path string, platformMFARequired bool) bool {
	if !platformMFARequired || principal.PlatformUser == nil || principal.PlatformUser.MfaEnabled {
		return false
	}
	return !strings.Contains(path, "/account/mfa/totp/") && !strings.HasSuffix(path, "/auth/session")
}

func toAuthenticatedSession(issued identity.IssuedSession, platformMFARequired bool) setupapi.AuthenticatedSession {
	principal := issued.Principal
	session := setupapi.Session{
		Id: principal.Session.ID.String(), Audience: setupapi.SessionAudience(principal.Session.Audience),
		UserId: principal.Session.UserID.String(), Locale: setupapi.SessionLocale(principal.Session.Locale),
		IssuedAt: principal.Session.CreatedAt.Time, ExpiresAt: principal.Session.AbsoluteExpiresAt.Time,
	}
	csrfRequired := true
	session.CsrfRequired = &csrfRequired
	userUnion := setupapi.AuthenticatedSession_User{}
	if principal.PlatformUser != nil {
		_ = session.FromSession0(setupapi.Session0{Audience: "platform"})
		_ = userUnion.FromPlatformUser(toPlatformUser(*principal.PlatformUser))
	} else if principal.EnterpriseUser != nil {
		_ = session.FromSession1(setupapi.Session1{Audience: "enterprise"})
		enterpriseID := principal.EnterpriseUser.EnterpriseID.String()
		departmentID := principal.EnterpriseUser.DepartmentID.String()
		authorizationVersion := setupapi.AuthorizationVersion(principal.EnterpriseUser.AuthorizationVersion)
		session.EnterpriseId, session.DepartmentId, session.AuthorizationVersion = &enterpriseID, &departmentID, &authorizationVersion
		_ = userUnion.FromEnterpriseUser(toEnterpriseUser(*principal.EnterpriseUser))
	}
	permissions := make([]setupapi.Permission, len(principal.Permissions))
	copy(permissions, principal.Permissions)
	csrf := issued.CSRFToken
	amr := make([]setupapi.AuthenticationMethod, len(principal.Session.Amr))
	for index, method := range principal.Session.Amr {
		amr[index] = setupapi.AuthenticationMethod(method)
	}
	mfaState := authenticatedMFAState(principal, platformMFARequired)
	result := setupapi.AuthenticatedSession{Session: session, User: userUnion, Permissions: permissions, CsrfToken: &csrf,
		Amr: amr, MfaState: mfaState, AuthenticatedAt: principal.Session.AuthenticatedAt.Time}
	if principal.Session.StepUpExpiresAt.Valid {
		result.StepUpExpiresAt = &principal.Session.StepUpExpiresAt.Time
	}
	return result
}

func authenticatedMFAState(principal identity.Principal, platformMFARequired bool) setupapi.MfaState {
	if principal.PlatformUser != nil {
		if principal.PlatformUser.MfaEnabled {
			return setupapi.MfaStateEnabled
		}
		if platformMFARequired {
			return setupapi.MfaStateEnrollmentRequired
		}
	}
	if principal.EnterpriseUser != nil && principal.EnterpriseUser.MfaEnabled {
		return setupapi.MfaStateEnabled
	}
	return setupapi.MfaStateDisabled
}

func toPlatformUser(user db.PlatformUser) setupapi.PlatformUser {
	result := setupapi.PlatformUser{Id: user.ID.String(), Username: user.Username, DisplayName: user.DisplayName,
		Role: setupapi.PlatformUserRole(user.Role), Status: setupapi.PlatformUserStatus(user.Status), MfaEnabled: user.MfaEnabled,
		Version: user.Version, CreatedAt: user.CreatedAt.Time}
	if user.Email.Valid {
		email := openapi_types.Email(user.Email.String)
		result.Email = &email
	}
	return result
}

func toEnterpriseUser(user db.EnterpriseUser) setupapi.EnterpriseUser {
	result := setupapi.EnterpriseUser{Id: user.ID.String(), EnterpriseId: user.EnterpriseID.String(), DepartmentId: user.DepartmentID.String(),
		Username: user.Username, DisplayName: user.DisplayName, Status: setupapi.EnterpriseUserStatus(user.Status), MfaEnabled: user.MfaEnabled,
		AuthorizationVersion: user.AuthorizationVersion, Version: user.Version, CreatedAt: user.CreatedAt.Time, UpdatedAt: user.UpdatedAt.Time}
	if user.Email.Valid {
		email := openapi_types.Email(user.Email.String)
		result.Email = &email
	}
	if user.LastLoginAt.Valid {
		result.LastLoginAt = &user.LastLoginAt.Time
	}
	return result
}

func setSessionCookies(writer http.ResponseWriter, audience string, issued identity.IssuedSession, cfg config.Server) {
	http.SetCookie(writer, &http.Cookie{Name: sessionCookieName(audience), Value: issued.Token, Path: "/", Secure: cfg.SecureCookies, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: issued.Principal.Session.AbsoluteExpiresAt.Time})
	http.SetCookie(writer, &http.Cookie{Name: csrfCookieName(audience), Value: issued.CSRFToken, Path: "/", Secure: cfg.SecureCookies, HttpOnly: false, SameSite: http.SameSiteStrictMode, Expires: issued.Principal.Session.AbsoluteExpiresAt.Time})
}

func clearSessionCookies(writer http.ResponseWriter, audience string, cfg config.Server) {
	for _, name := range []string{sessionCookieName(audience), csrfCookieName(audience)} {
		http.SetCookie(writer, &http.Cookie{Name: name, Value: "", Path: "/", Secure: cfg.SecureCookies, HttpOnly: name == sessionCookieName(audience), SameSite: http.SameSiteStrictMode, Expires: time.Unix(0, 0), MaxAge: -1})
	}
}

func sessionCookieName(audience string) string { return "argus_" + audience + "_session" }
func csrfCookieName(audience string) string    { return "argus_" + audience + "_csrf" }

func setupError(ctx context.Context, err error) setupapi.ApiError {
	result := setupErrorBase(ctx, err)
	logMappedError(ctx, result.Code, err)
	return result
}

func setupErrorBase(ctx context.Context, err error) setupapi.ApiError {
	code, key := "INTERNAL_ERROR", "errors.common.internal"
	retryable := false
	var message *string
	var params *map[string]setupapi.ApiError_Params_AdditionalProperties
	switch {
	case errors.Is(err, platform.ErrAlreadyInitialized):
		code, key = "SETUP_ALREADY_INITIALIZED", "errors.setup.already_initialized"
	case errors.Is(err, platform.ErrSetupTokenInvalid):
		code, key = "SETUP_TOKEN_INVALID", "errors.setup.token_invalid"
	case errors.Is(err, platform.ErrSetupTokenExpired):
		code, key = "SETUP_TOKEN_EXPIRED", "errors.setup.token_expired"
	case errors.Is(err, platform.ErrSetupTokenUnavailable):
		code, key, retryable = "SETUP_TOKEN_UNAVAILABLE", "errors.setup.token_unavailable", true
	case errors.Is(err, identity.ErrInvalidCredentials):
		code, key = "INVALID_CREDENTIALS", "errors.auth.invalid_credentials"
	case errors.Is(err, identity.ErrWeakPassword):
		code, key = "PASSWORD_WEAK", "errors.auth.password_weak"
		if rule, ok := identity.PasswordRuleFromError(err); ok {
			key, message, params = passwordPolicyErrorDetails(ctx, rule)
		}
	case errors.Is(err, identity.ErrTemporaryExpired):
		code, key = "TEMPORARY_CREDENTIAL_EXPIRED", "errors.auth.temporary_credential_expired"
	case errors.Is(err, identity.ErrLoginRateLimited):
		code, key, retryable = "LOGIN_RATE_LIMITED", "errors.auth.login_rate_limited", true
	case errors.Is(err, identity.ErrLoginDependency):
		code, key, retryable = "LOGIN_DEPENDENCY_UNAVAILABLE", "errors.auth.login_dependency_unavailable", true
	case errors.Is(err, identity.ErrAudienceMismatch):
		code, key = "SESSION_AUDIENCE_MISMATCH", "errors.auth.session_audience_mismatch"
	case errors.Is(err, identity.ErrSessionExpired):
		code, key = "SESSION_EXPIRED", "errors.auth.session_expired"
	case errors.Is(err, identity.ErrSessionRevoked):
		code, key = "SESSION_REVOKED", "errors.auth.session_revoked"
	case errors.Is(err, identity.ErrAuthorizationVersion):
		code, key = "AUTHORIZATION_VERSION_STALE", "errors.auth.authorization_version_stale"
	case errors.Is(err, identity.ErrEnterpriseSuspended):
		code, key = "ENTERPRISE_SUSPENDED", "errors.enterprise.suspended"
	case errors.Is(err, identity.ErrEnterpriseDisabled):
		code, key = "ENTERPRISE_DISABLED", "errors.enterprise.disabled"
	case errors.Is(err, identity.ErrCSRFInvalid):
		code, key = "CSRF_TOKEN_INVALID", "errors.auth.csrf_token_invalid"
	case errors.Is(err, identity.ErrVersionConflict):
		code, key, retryable = "VERSION_CONFLICT", "errors.common.version_conflict", true
	case errors.Is(err, platform.ErrEnterpriseCodeInvalid):
		code, key = "INVALID_ARGUMENT", "errors.common.invalid_argument"
	case errors.Is(err, postgres.ErrIdempotencyConflict):
		code, key = "IDEMPOTENCY_CONFLICT", "errors.common.idempotency_conflict"
	case errors.Is(err, postgres.ErrIdempotencyExpired):
		code, key = "IDEMPOTENCY_RESULT_EXPIRED", "errors.common.idempotency_result_expired"
	case errors.Is(err, identity.ErrMFARequired):
		code, key = "MFA_REQUIRED", "errors.identity.mfa_required"
	case errors.Is(err, identity.ErrMFAEnrollment):
		code, key = "MFA_ENROLLMENT_REQUIRED", "errors.identity.mfa_enrollment_required"
	case errors.Is(err, identity.ErrMFAInvalid):
		code, key = "MFA_PROOF_INVALID", "errors.identity.mfa_proof_invalid"
	case errors.Is(err, identity.ErrStepUpRequired):
		code, key = "STEP_UP_REQUIRED", "errors.identity.step_up_required"
	case errors.Is(err, identity.ErrBreakGlassDisabled):
		code, key = "BREAK_GLASS_DISABLED", "errors.identity.break_glass_disabled"
	}
	requestID := "server-generated-request"
	if metadata, ok := RequestFromContext(ctx); ok {
		requestID = metadata.RequestID
	}
	return setupapi.ApiError{Code: code, Message: message, MessageKey: key, Params: params, RequestId: requestID, Retryable: &retryable}
}

func passwordPolicyErrorDetails(ctx context.Context, rule identity.PasswordRule) (string, *string, *map[string]setupapi.ApiError_Params_AdditionalProperties) {
	key := "errors.auth.password_weak"
	messages := map[identity.PasswordRule][2]string{
		identity.PasswordRuleMinLength:      {"密码至少需要 12 个字符", "The password must contain at least 12 characters."},
		identity.PasswordRuleMaxLength:      {"密码不能超过 1024 个字符", "The password must not exceed 1024 characters."},
		identity.PasswordRuleLetterRequired: {"密码必须包含至少一个字母", "The password must contain at least one letter."},
		identity.PasswordRuleDigitRequired:  {"密码必须包含至少一个数字", "The password must contain at least one number."},
		identity.PasswordRuleCommon:         {"该密码过于常见，请使用其他密码", "This password is too common. Choose a different password."},
		identity.PasswordRuleIdentity:       {"密码不能包含用户名或邮箱账号部分", "The password must not contain the username or email account name."},
		identity.PasswordRuleReused:         {"新密码不能与当前或临时密码相同", "The new password must differ from the current or temporary password."},
	}
	localized := messages[rule][0]
	if LocaleFromContext(ctx) == "en-US" {
		localized = messages[rule][1]
	}
	values := map[string]setupapi.ApiError_Params_AdditionalProperties{
		"field": setupStringErrorParam("password"),
		"rule":  setupStringErrorParam(string(rule)),
	}
	if rule == identity.PasswordRuleMinLength {
		values["min_length"] = setupNumberErrorParam(identity.PasswordMinLength)
	}
	if rule == identity.PasswordRuleMaxLength {
		values["max_length"] = setupNumberErrorParam(identity.PasswordMaxLength)
	}
	return key, &localized, &values
}

func setupStringErrorParam(value string) setupapi.ApiError_Params_AdditionalProperties {
	var result setupapi.ApiError_Params_AdditionalProperties
	_ = result.FromApiErrorParams0(value)
	return result
}

func setupNumberErrorParam(value int) setupapi.ApiError_Params_AdditionalProperties {
	var result setupapi.ApiError_Params_AdditionalProperties
	_ = result.FromApiErrorParams1(float32(value))
	return result
}

func authStatus(err error) int {
	if errors.Is(err, identity.ErrAudienceMismatch) || errors.Is(err, identity.ErrEnterpriseSuspended) || errors.Is(err, identity.ErrEnterpriseDisabled) || errors.Is(err, identity.ErrCSRFInvalid) || errors.Is(err, identity.ErrMFAEnrollment) || errors.Is(err, identity.ErrStepUpRequired) {
		return http.StatusForbidden
	}
	if errors.Is(err, identity.ErrAuthorizationVersion) {
		return http.StatusConflict
	}
	return http.StatusUnauthorized
}
