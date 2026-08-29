package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	openapi_types "github.com/oapi-codegen/runtime/types"

	remoteaccessapi "github.com/kakj-go/Argus/internal/gen/openapi/remoteaccessapi"
	"github.com/kakj-go/Argus/internal/remoteaccess"
)

func TestRemoteAccessAdminPermissions(t *testing.T) {
	for _, permission := range []string{
		"remote_access.grant.read",
		"remote_access.rule.manage",
		"remote_access.session.terminate",
		"remote_access.recording.read",
	} {
		if !remoteAccessAdminPermission(permission) {
			t.Fatalf("%s must require enterprise_admin", permission)
		}
	}
	for _, permission := range []string{
		"remote_access.request",
		"remote_access.session.create",
		"remote_access.session.approve",
	} {
		if remoteAccessAdminPermission(permission) {
			t.Fatalf("%s must remain available to its workflow or request actor", permission)
		}
	}
}

func TestRemoteAccessTransitionsPreserveAuthenticationErrors(t *testing.T) {
	handler := RemoteAccessHandler{}
	ctx := context.Background()
	id := openapi_types.UUID(uuid.Nil)

	tests := []struct {
		name string
		run  func() error
	}{
		{name: "grant", run: func() error {
			_, err := handler.transitionGrant(ctx, id, "", 0, "", remoteaccess.GovernanceDisabled)
			return err
		}},
		{name: "rule", run: func() error {
			_, err := handler.transitionRule(ctx, id, remoteaccessapi.DisableRemoteAccessRuleParams{}, remoteaccess.GovernanceDisabled)
			return err
		}},
		{name: "workflow", run: func() error {
			_, err := handler.transitionWorkflow(ctx, id, "", 0, "", remoteaccess.GovernanceDisabled)
			return err
		}},
		{name: "session profile", run: func() error {
			_, err := handler.transitionProfile(ctx, id, "", 0, "", remoteaccess.GovernanceDisabled)
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.run()
			if err == nil {
				t.Fatal("expected authentication error")
			}
			if got := remoteAccessError(ctx, err).Code; got != "SESSION_INVALID" {
				t.Fatalf("error code = %q, want SESSION_INVALID", got)
			}
			if got := remoteAccessStatus(err); got != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", got, http.StatusUnauthorized)
			}
		})
	}
}

func TestRemoteAccessAuthStatus(t *testing.T) {
	tests := []struct {
		code string
		want int
	}{
		{code: "AUTHORIZATION_VERSION_STALE", want: http.StatusConflict},
		{code: "SESSION_EXPIRED", want: http.StatusUnauthorized},
		{code: "SESSION_REVOKED", want: http.StatusUnauthorized},
		{code: "SESSION_INVALID", want: http.StatusUnauthorized},
		{code: "AUTHORIZATION_DENIED", want: http.StatusForbidden},
		{code: "CSRF_INVALID", want: http.StatusForbidden},
	}
	for _, test := range tests {
		if got := remoteAccessAuthStatus(remoteaccessapi.ApiError{Code: test.code}); got != test.want {
			t.Fatalf("%s status = %d, want %d", test.code, got, test.want)
		}
	}
}

func TestRemoteAccessErrorMapsMissingSession(t *testing.T) {
	if got := remoteAccessError(context.Background(), pgx.ErrNoRows); got.Code != "RESOURCE_NOT_FOUND" {
		t.Fatalf("error code = %q, want RESOURCE_NOT_FOUND", got.Code)
	}
	if got := remoteAccessStatus(pgx.ErrNoRows); got != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", got, http.StatusNotFound)
	}
}

func TestRecordingEventPageSerializesEmptyEventsAsArray(t *testing.T) {
	// 翻页拉到末尾时 service 会返回零事件页；契约要求 events 是数组而不是 null，
	// 否则前端 flatMap 会把 null 混进事件列表导致渲染崩溃。
	page := toRecordingEventPage(remoteaccess.RecordingEventPage{})
	encoded, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal recording event page: %v", err)
	}
	if !strings.Contains(string(encoded), `"events":[]`) {
		t.Fatalf("empty events page must serialize events as [], got %s", encoded)
	}

	page = toRecordingEventPage(remoteaccess.RecordingEventPage{Events: []remoteaccess.RecordingEvent{{Time: 1.5, Type: "o", Data: "uptime"}}})
	encoded, err = json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal recording event page: %v", err)
	}
	if !strings.Contains(string(encoded), `"time":1.5`) || !strings.Contains(string(encoded), `"data":"uptime"`) {
		t.Fatalf("events page lost event payload: %s", encoded)
	}
}
