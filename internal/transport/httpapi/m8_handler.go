package httpapi

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	m8api "github.com/kakj-go/Argus/internal/gen/openapi/m8api"
	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

// M8Handler owns authentication-assurance endpoints while reusing the
// established audience, cookie, origin, and CSRF rules from SetupHandler.
type M8Handler struct {
	Auth SetupHandler
}

func (handler M8Handler) CompleteMfaLogin(ctx context.Context, request m8api.CompleteMfaLoginRequestObject) (m8api.CompleteMfaLoginResponseObject, error) {
	metadata, ok := RequestFromContext(ctx)
	if !ok || request.Body == nil || request.Body.ChallengeId == nil || request.Body.Code == nil {
		return m8api.CompleteMfaLogindefaultJSONResponse{Body: m8Error(ctx, identity.ErrMFAInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	issued, err := handler.Auth.Identity.CompleteMFALogin(ctx, string(request.Audience), *request.Body.ChallengeId, *request.Body.Code)
	if err != nil {
		return m8api.CompleteMfaLogindefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	setSessionCookies(metadata.Writer, string(request.Audience), issued, handler.Auth.Config)
	return m8api.CompleteMfaLogin200JSONResponse(toM8AuthenticatedSession(issued)), nil
}

func (handler M8Handler) EnrollTotp(ctx context.Context, request m8api.EnrollTotpRequestObject) (m8api.EnrollTotpResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, string(request.Audience), true, request.Params.XCSRFToken)
	if err != nil {
		return m8api.EnrollTotpdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	enrollment, err := handler.Auth.Identity.BeginTOTPEnrollment(ctx, principal, request.Params.IdempotencyKey)
	if err != nil {
		return m8api.EnrollTotpdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: http.StatusServiceUnavailable}, nil
	}
	return m8api.EnrollTotp201JSONResponse{EnrollmentId: &enrollment.Token, Secret: &enrollment.Secret, OtpauthUri: &enrollment.URI, ExpiresAt: enrollment.ExpiresAt}, nil
}

func (handler M8Handler) VerifyTotpEnrollment(ctx context.Context, request m8api.VerifyTotpEnrollmentRequestObject) (m8api.VerifyTotpEnrollmentResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, string(request.Audience), true, request.Params.XCSRFToken)
	if err != nil {
		return m8api.VerifyTotpEnrollmentdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if request.Body == nil || request.Body.EnrollmentId == nil || request.Body.Code == nil {
		return m8api.VerifyTotpEnrollmentdefaultJSONResponse{Body: m8Error(ctx, identity.ErrMFAInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	codes, err := handler.Auth.Identity.VerifyTOTPEnrollment(ctx, principal, *request.Body.EnrollmentId, *request.Body.Code)
	if err != nil {
		return m8api.VerifyTotpEnrollmentdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	return m8api.VerifyTotpEnrollment200JSONResponse{Codes: codes.Codes, GeneratedAt: codes.GeneratedAt}, nil
}

func (handler M8Handler) RegenerateRecoveryCodes(ctx context.Context, request m8api.RegenerateRecoveryCodesRequestObject) (m8api.RegenerateRecoveryCodesResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, string(request.Audience), true, request.Params.XCSRFToken)
	if err != nil {
		return m8api.RegenerateRecoveryCodesdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if request.Body == nil || request.Body.Code == nil {
		return m8api.RegenerateRecoveryCodesdefaultJSONResponse{Body: m8Error(ctx, identity.ErrMFAInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	codes, err := handler.Auth.Identity.RegenerateRecoveryCodes(ctx, principal, *request.Body.Code)
	if err != nil {
		return m8api.RegenerateRecoveryCodesdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	return m8api.RegenerateRecoveryCodes200JSONResponse{Codes: codes.Codes, GeneratedAt: codes.GeneratedAt}, nil
}

func (handler M8Handler) DisableTotp(ctx context.Context, request m8api.DisableTotpRequestObject) (m8api.DisableTotpResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, string(request.Audience), true, request.Params.XCSRFToken)
	if err != nil {
		return m8api.DisableTotpdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if request.Body == nil || request.Body.Code == nil {
		return m8api.DisableTotpdefaultJSONResponse{Body: m8Error(ctx, identity.ErrMFAInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	if err := handler.Auth.Identity.DisableTOTP(ctx, principal, *request.Body.Code); err != nil {
		return m8api.DisableTotpdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if metadata, ok := RequestFromContext(ctx); ok {
		clearSessionCookies(metadata.Writer, string(request.Audience), handler.Auth.Config)
	}
	return m8api.DisableTotp204Response{}, nil
}

func (handler M8Handler) StepUpAuthentication(ctx context.Context, request m8api.StepUpAuthenticationRequestObject) (m8api.StepUpAuthenticationResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, string(request.Audience), true, request.Params.XCSRFToken)
	if err != nil {
		return m8api.StepUpAuthenticationdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if request.Body == nil || request.Body.Code == nil {
		return m8api.StepUpAuthenticationdefaultJSONResponse{Body: m8Error(ctx, identity.ErrMFAInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	expires, methods, err := handler.Auth.Identity.StepUp(ctx, principal, *request.Body.Code)
	if err != nil {
		return m8api.StepUpAuthenticationdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	amr := make([]m8api.AuthenticationMethod, len(methods))
	for index, method := range methods {
		amr[index] = m8api.AuthenticationMethod(method)
	}
	return m8api.StepUpAuthentication200JSONResponse{ExpiresAt: expires, Amr: amr}, nil
}

func (handler M8Handler) ListBreakGlassSessions(ctx context.Context, _ m8api.ListBreakGlassSessionsRequestObject) (m8api.ListBreakGlassSessionsResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, "enterprise", false, "")
	if err != nil {
		return m8api.ListBreakGlassSessionsdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	rows, err := handler.Auth.Identity.ListBreakGlass(ctx, principal)
	if err != nil {
		return m8api.ListBreakGlassSessionsdefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	items := make([]m8api.BreakGlassSession, len(rows))
	for index, row := range rows {
		items[index] = toM8BreakGlassSession(row)
	}
	return m8api.ListBreakGlassSessions200JSONResponse(items), nil
}

func (handler M8Handler) CreateBreakGlassSession(ctx context.Context, request m8api.CreateBreakGlassSessionRequestObject) (m8api.CreateBreakGlassSessionResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, "enterprise", true, request.Params.XCSRFToken)
	if err != nil {
		return m8api.CreateBreakGlassSessiondefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if request.Body == nil {
		return m8api.CreateBreakGlassSessiondefaultJSONResponse{Body: m8Error(ctx, identity.ErrMFAInvalid), StatusCode: http.StatusBadRequest}, nil
	}
	created, err := handler.Auth.Identity.CreateBreakGlass(ctx, principal, request.Body.Reason, request.Body.TicketRef, request.Params.IdempotencyKey)
	if err != nil {
		return m8api.CreateBreakGlassSessiondefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	return m8api.CreateBreakGlassSession201JSONResponse(toM8BreakGlassSession(created)), nil
}

func (handler M8Handler) RevokeBreakGlassSession(ctx context.Context, request m8api.RevokeBreakGlassSessionRequestObject) (m8api.RevokeBreakGlassSessionResponseObject, error) {
	principal, err := handler.Auth.authenticate(ctx, "enterprise", true, request.Params.XCSRFToken)
	if err != nil {
		return m8api.RevokeBreakGlassSessiondefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: authStatus(err)}, nil
	}
	if err := handler.Auth.Identity.RevokeBreakGlass(ctx, principal, uuid.UUID(request.Id)); err != nil {
		return m8api.RevokeBreakGlassSessiondefaultJSONResponse{Body: m8Error(ctx, err), StatusCode: http.StatusNotFound}, nil
	}
	return m8api.RevokeBreakGlassSession204Response{}, nil
}

func toM8AuthenticatedSession(issued identity.IssuedSession) m8api.AuthenticatedSession {
	principal := issued.Principal
	session := m8api.Session{Id: principal.Session.ID.String(), Audience: m8api.SessionAudience(principal.Session.Audience),
		UserId: principal.Session.UserID.String(), Locale: m8api.SessionLocale(principal.Session.Locale),
		IssuedAt: principal.Session.CreatedAt.Time, ExpiresAt: principal.Session.AbsoluteExpiresAt.Time}
	csrfRequired := true
	session.CsrfRequired = &csrfRequired
	userUnion := m8api.AuthenticatedSession_User{}
	if principal.PlatformUser != nil {
		_ = session.FromSession0(m8api.Session0{Audience: "platform"})
		user := m8api.PlatformUser{Id: principal.PlatformUser.ID.String(), Username: principal.PlatformUser.Username, DisplayName: principal.PlatformUser.DisplayName,
			Role: m8api.PlatformUserRole(principal.PlatformUser.Role), Status: m8api.PlatformUserStatus(principal.PlatformUser.Status), MfaEnabled: principal.PlatformUser.MfaEnabled,
			Version: principal.PlatformUser.Version, CreatedAt: principal.PlatformUser.CreatedAt.Time}
		if principal.PlatformUser.Email.Valid {
			email := openapi_types.Email(principal.PlatformUser.Email.String)
			user.Email = &email
		}
		_ = userUnion.FromPlatformUser(user)
	} else if principal.EnterpriseUser != nil {
		_ = session.FromSession1(m8api.Session1{Audience: "enterprise"})
		enterpriseID, departmentID := principal.EnterpriseUser.EnterpriseID.String(), principal.EnterpriseUser.DepartmentID.String()
		authorizationVersion := m8api.AuthorizationVersion(principal.EnterpriseUser.AuthorizationVersion)
		session.EnterpriseId, session.DepartmentId, session.AuthorizationVersion = &enterpriseID, &departmentID, &authorizationVersion
		user := m8api.EnterpriseUser{Id: principal.EnterpriseUser.ID.String(), EnterpriseId: enterpriseID, DepartmentId: departmentID,
			Username: principal.EnterpriseUser.Username, DisplayName: principal.EnterpriseUser.DisplayName, Status: m8api.EnterpriseUserStatus(principal.EnterpriseUser.Status),
			MfaEnabled: principal.EnterpriseUser.MfaEnabled, AuthorizationVersion: authorizationVersion, Version: principal.EnterpriseUser.Version,
			CreatedAt: principal.EnterpriseUser.CreatedAt.Time, UpdatedAt: principal.EnterpriseUser.UpdatedAt.Time}
		if principal.EnterpriseUser.Email.Valid {
			email := openapi_types.Email(principal.EnterpriseUser.Email.String)
			user.Email = &email
		}
		if principal.EnterpriseUser.LastLoginAt.Valid {
			user.LastLoginAt = &principal.EnterpriseUser.LastLoginAt.Time
		}
		_ = userUnion.FromEnterpriseUser(user)
	}
	permissions := make([]m8api.Permission, len(principal.Permissions))
	copy(permissions, principal.Permissions)
	amr := make([]m8api.AuthenticationMethod, len(principal.Session.Amr))
	for index, method := range principal.Session.Amr {
		amr[index] = m8api.AuthenticationMethod(method)
	}
	mfaState := m8api.MfaStateDisabled
	if principal.PlatformUser != nil {
		if principal.PlatformUser.MfaEnabled {
			mfaState = m8api.MfaStateEnabled
		} else {
			mfaState = m8api.MfaStateEnrollmentRequired
		}
	} else if principal.EnterpriseUser != nil && principal.EnterpriseUser.MfaEnabled {
		mfaState = m8api.MfaStateEnabled
	}
	result := m8api.AuthenticatedSession{Session: session, User: userUnion, Permissions: permissions, CsrfToken: &issued.CSRFToken,
		Amr: amr, MfaState: mfaState, AuthenticatedAt: principal.Session.AuthenticatedAt.Time}
	if principal.Session.StepUpExpiresAt.Valid {
		result.StepUpExpiresAt = &principal.Session.StepUpExpiresAt.Time
	}
	return result
}

func toM8BreakGlassSession(row db.BreakGlassSession) m8api.BreakGlassSession {
	result := m8api.BreakGlassSession{Id: openapi_types.UUID(row.ID), EnterpriseId: openapi_types.UUID(row.EnterpriseID),
		UserId: openapi_types.UUID(row.UserID), Reason: row.Reason, TicketRef: row.TicketRef, Status: m8api.BreakGlassSessionStatus(row.Status),
		ExpiresAt: row.ExpiresAt.Time, CreatedAt: row.CreatedAt.Time}
	if row.RevokedAt.Valid {
		result.RevokedAt = &row.RevokedAt.Time
	}
	return result
}

func m8Error(ctx context.Context, err error) m8api.ApiError {
	base := setupError(ctx, err)
	return m8api.ApiError{Code: base.Code, Message: base.Message, MessageKey: base.MessageKey, RequestId: base.RequestId,
		Retryable: base.Retryable, TraceId: base.TraceId}
}
