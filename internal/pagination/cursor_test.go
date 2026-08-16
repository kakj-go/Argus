package pagination

import (
	"errors"
	"testing"
	"time"
)

func TestCursorBindsAuthorizationAndFilters(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	signer := Signer{Key: []byte("01234567890123456789012345678901"), Now: func() time.Time { return now }}
	binding := Binding{Audience: "enterprise", EnterpriseID: "e1", SubjectType: "user", SubjectID: "u1", AuthorizationVersion: 4, FilterHash: HashFilter(map[string]any{"status": "active"}), Sort: "created_at_asc"}
	token, err := signer.Encode(binding, Position{Time: now.Add(-time.Minute), ID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	position, err := signer.Decode(token, binding)
	if err != nil || position.ID != "r1" {
		t.Fatalf("decode cursor: position=%+v err=%v", position, err)
	}
	stale := binding
	stale.AuthorizationVersion++
	if _, err := signer.Decode(token, stale); !errors.Is(err, ErrAuthorizationVersionStale) {
		t.Fatalf("expected authorization stale, got %v", err)
	}
	changed := binding
	changed.FilterHash = HashFilter(map[string]any{"status": "disabled"})
	if _, err := signer.Decode(token, changed); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected invalid filter binding, got %v", err)
	}
}

func TestCursorRejectsTamperingAndExpiry(t *testing.T) {
	now := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	signer := Signer{Key: []byte("01234567890123456789012345678901"), Now: func() time.Time { return now }}
	binding := Binding{Audience: "platform", SubjectType: "platform_user", SubjectID: "u1", Sort: "created_at_desc", FilterHash: HashFilter(nil)}
	token, err := signer.Encode(binding, Position{Time: now, ID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Decode(token+"x", binding); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected tamper rejection, got %v", err)
	}
	signer.Now = func() time.Time { return now.Add(16 * time.Minute) }
	if _, err := signer.Decode(token, binding); !errors.Is(err, ErrExpired) {
		t.Fatalf("expected expiry, got %v", err)
	}
}
