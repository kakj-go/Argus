package httpapi

import (
	"testing"
	"time"

	"github.com/kakj-go/Argus/internal/pagination"
)

func TestPaginateUsesSignedCursorAndBinding(t *testing.T) {
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	signer := pagination.Signer{Key: []byte("01234567890123456789012345678901"), Now: func() time.Time { return now }}
	items := []struct {
		id   string
		time time.Time
	}{
		{id: "one", time: now.Add(-3 * time.Minute)},
		{id: "two", time: now.Add(-2 * time.Minute)},
		{id: "three", time: now.Add(-1 * time.Minute)},
	}
	key := func(value struct {
		id   string
		time time.Time
	}) pageKey {
		return pageKey{ID: value.id, Time: value.time}
	}
	binding := pagination.Binding{Audience: "enterprise", EnterpriseID: "enterprise-1", SubjectType: "user", SubjectID: "user-1", AuthorizationVersion: 3, FilterHash: pagination.HashFilter(nil), Sort: "created_at_desc"}

	first, cursor, hasMore, err := paginate(signer, items, "", 2, binding, key)
	if err != nil || !hasMore || cursor == nil || len(first) != 2 || first[0].id != "one" || first[1].id != "two" {
		t.Fatalf("first page mismatch: items=%v cursor=%v has_more=%v err=%v", first, cursor, hasMore, err)
	}
	second, next, hasMore, err := paginate(signer, items, *cursor, 2, binding, key)
	if err != nil || hasMore || next != nil || len(second) != 1 || second[0].id != "three" {
		t.Fatalf("second page mismatch: items=%v cursor=%v has_more=%v err=%v", second, next, hasMore, err)
	}
	if _, _, _, err := paginate(signer, items, *cursor, 2, pagination.Binding{Audience: "enterprise", EnterpriseID: "other", SubjectType: "user", SubjectID: "user-1", AuthorizationVersion: 3, FilterHash: pagination.HashFilter(nil), Sort: "created_at_desc"}, key); err == nil {
		t.Fatal("cursor must not be reusable across enterprise bindings")
	}
}
