package httpapi

import (
	"context"
	"errors"
	"time"

	"github.com/kakj-go/Argus/internal/identity"
	"github.com/kakj-go/Argus/internal/pagination"
)

type pageKey struct {
	Time time.Time
	ID   string
}

func paginate[T any](signer pagination.Signer, items []T, cursor string, limit int, binding pagination.Binding, key func(T) pageKey) ([]T, *string, bool, error) {
	if limit < 1 || limit > 200 {
		return nil, nil, false, errors.New("invalid limit")
	}
	start := 0
	if cursor != "" {
		position, err := signer.Decode(cursor, binding)
		if err != nil {
			return nil, nil, false, err
		}
		found := false
		for index, item := range items {
			value := key(item)
			if value.ID == position.ID && value.Time.Equal(position.Time) {
				start, found = index+1, true
				break
			}
		}
		if !found {
			return nil, nil, false, pagination.ErrInvalid
		}
	}
	end := start + limit
	if end > len(items) {
		end = len(items)
	}
	page := items[start:end]
	hasMore := end < len(items)
	if !hasMore || len(page) == 0 {
		return page, nil, hasMore, nil
	}
	last := key(page[len(page)-1])
	next, err := signer.Encode(binding, pagination.Position{Time: last.Time, ID: last.ID})
	if err != nil {
		return nil, nil, false, err
	}
	return page, &next, true, nil
}

func enterpriseCursorBinding(principal identity.Principal, filter any, sort string) pagination.Binding {
	return pagination.Binding{
		Audience: "enterprise", EnterpriseID: principal.EnterpriseIDValue().String(),
		SubjectType: principal.ActorType(), SubjectID: principal.ActorID(),
		AuthorizationVersion: principal.AuthorizationVersion(), FilterHash: pagination.HashFilter(filter), Sort: sort,
	}
}

func cursorValue[T ~string](value *T) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func listLimit[T ~int](value *T) int {
	if value == nil {
		return 50
	}
	return int(*value)
}

func paginationError(err error) (string, string, int) {
	switch {
	case errors.Is(err, pagination.ErrExpired):
		return "CURSOR_EXPIRED", "errors.pagination.cursor_expired", 409
	case errors.Is(err, pagination.ErrAuthorizationVersionStale):
		return "AUTHORIZATION_VERSION_STALE", "errors.auth.authorization_version_stale", 409
	default:
		return "CURSOR_INVALID", "errors.pagination.cursor_invalid", 400
	}
}

func requestID(ctx context.Context) string {
	if metadata, ok := RequestFromContext(ctx); ok {
		return metadata.RequestID
	}
	return "server-generated-request"
}

func retryablePointer(value bool) *bool { return &value }
