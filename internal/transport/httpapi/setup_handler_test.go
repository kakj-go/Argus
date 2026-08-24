package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/platform"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestRequiresMFAEnrollment(t *testing.T) {
	unenrolled := identity.Principal{PlatformUser: &db.PlatformUser{MfaEnabled: false}}
	enrolled := identity.Principal{PlatformUser: &db.PlatformUser{MfaEnabled: true}}

	tests := []struct {
		name      string
		principal identity.Principal
		path      string
		required  bool
		want      bool
	}{
		{name: "disabled by default", principal: unenrolled, path: "/api/v1/platform/enterprises", required: false, want: false},
		{name: "required for platform operation", principal: unenrolled, path: "/api/v1/platform/enterprises", required: true, want: true},
		{name: "enrollment remains available", principal: unenrolled, path: "/api/v1/platform/account/mfa/totp/enroll", required: true, want: false},
		{name: "session remains available", principal: unenrolled, path: "/api/v1/platform/auth/session", required: true, want: false},
		{name: "enrolled platform user", principal: enrolled, path: "/api/v1/platform/enterprises", required: true, want: false},
		{name: "enterprise user unaffected", principal: identity.Principal{EnterpriseUser: &db.EnterpriseUser{}}, path: "/api/v1/enterprise/departments", required: true, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := requiresMFAEnrollment(test.principal, test.path, test.required); got != test.want {
				t.Fatalf("requiresMFAEnrollment() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAuthenticatedMFAStateHonorsPlatformRequirement(t *testing.T) {
	unenrolled := identity.Principal{PlatformUser: &db.PlatformUser{MfaEnabled: false}}
	if got := authenticatedMFAState(unenrolled, false); got != "disabled" {
		t.Fatalf("optional platform MFA state = %q", got)
	}
	if got := authenticatedMFAState(unenrolled, true); got != "enrollment_required" {
		t.Fatalf("required platform MFA state = %q", got)
	}

	enrolled := identity.Principal{PlatformUser: &db.PlatformUser{MfaEnabled: true}}
	if got := authenticatedMFAState(enrolled, false); got != "enabled" {
		t.Fatalf("enabled platform MFA state = %q", got)
	}
}

func TestSetupErrorMapsInvalidEnterpriseCode(t *testing.T) {
	response := setupError(context.Background(), platform.ErrEnterpriseCodeInvalid)
	if response.Code != "INVALID_ARGUMENT" {
		t.Fatalf("code = %q, want INVALID_ARGUMENT", response.Code)
	}
	if response.MessageKey != "errors.common.invalid_argument" {
		t.Fatalf("message key = %q, want errors.common.invalid_argument", response.MessageKey)
	}
}

func TestSetupErrorPublishesSafePasswordPolicyDetails(t *testing.T) {
	ctx := context.WithValue(context.Background(), localeContextKey{}, "en-US")
	response := setupError(ctx, identity.PasswordPolicyError{Rule: identity.PasswordRuleIdentity})
	if response.Code != "PASSWORD_WEAK" || response.MessageKey != "errors.auth.password_weak" {
		t.Fatalf("password error = %#v", response)
	}
	if response.Message == nil || *response.Message != "The password must not contain the username or email account name." {
		t.Fatalf("message = %#v", response.Message)
	}
	if response.Params == nil {
		t.Fatal("password error params are missing")
	}
	rule, err := (*response.Params)["rule"].AsApiErrorParams0()
	if err != nil || rule != "contains_identity" {
		t.Fatalf("rule = %q, err=%v", rule, err)
	}
}

func TestMappedErrorsLogStableCodeWithoutPublishingBusinessErrorText(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx := context.WithValue(context.Background(), requestLoggerContextKey{}, logger)

	response := enterpriseIdentityError(ctx, identity.ErrDepartmentNotEmpty)
	if response.Code != "DEPARTMENT_NOT_EMPTY" {
		t.Fatalf("code = %q", response.Code)
	}
	logLine := output.String()
	if !strings.Contains(logLine, `"error_code":"DEPARTMENT_NOT_EMPTY"`) {
		t.Fatalf("log = %q", logLine)
	}
	if strings.Contains(logLine, identity.ErrDepartmentNotEmpty.Error()) {
		t.Fatalf("expected business rejection log to omit raw error text: %q", logLine)
	}
}

func TestInternalErrorsKeepDetailsOnlyInControlledServerLog(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil))
	ctx := context.WithValue(context.Background(), requestLoggerContextKey{}, logger)
	internal := errors.New("database connection failed")

	response := setupError(ctx, internal)
	if response.Code != "INTERNAL_ERROR" || response.Message != nil || response.Params != nil {
		t.Fatalf("unsafe internal response = %#v", response)
	}
	logLine := output.String()
	for _, fragment := range []string{`"error_code":"INTERNAL_ERROR"`, `"error":"database connection failed"`} {
		if !strings.Contains(logLine, fragment) {
			t.Fatalf("log %q does not contain %q", logLine, fragment)
		}
	}
}
