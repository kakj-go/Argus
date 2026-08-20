package identity

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestTOTPWindowAndCounter(t *testing.T) {
	secret := "JBSWY3DPEHPK3PXP"
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	for _, offset := range []time.Duration{-30 * time.Second, 0, 30 * time.Second} {
		code := totpAt(secret, now.Add(offset))
		counter, ok := matchingTOTPCounter(secret, code, now)
		if !ok || counter != now.Add(offset).Unix()/30 {
			t.Fatalf("TOTP offset %s rejected or returned wrong counter", offset)
		}
	}
	if verifyTOTP(secret, totpAt(secret, now.Add(-60*time.Second)), now) {
		t.Fatal("TOTP outside the permitted window was accepted")
	}
	if verifyTOTP(secret, "12345", now) {
		t.Fatal("short TOTP was accepted")
	}
}

func TestRequireStepUpExpires(t *testing.T) {
	now := time.Date(2026, time.August, 19, 12, 0, 0, 0, time.UTC)
	service := Service{Now: func() time.Time { return now }}
	principal := Principal{Session: db.Session{StepUpExpiresAt: pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true}}}
	if err := service.RequireStepUp(principal); err != nil {
		t.Fatalf("valid step-up rejected: %v", err)
	}
	principal.Session.StepUpExpiresAt.Time = now
	if err := service.RequireStepUp(principal); !errors.Is(err, ErrStepUpRequired) {
		t.Fatalf("expired step-up returned %v", err)
	}
}

func TestRecoveryCodesAreUniqueAndNormalized(t *testing.T) {
	codes, hashes, err := newRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("unexpected recovery code count: %d/%d", len(codes), len(hashes))
	}
	seen := map[string]bool{}
	for _, code := range codes {
		normalized := normalizeRecoveryCode(code)
		if len(normalized) != 16 || seen[normalized] {
			t.Fatalf("invalid or duplicate recovery code %q", code)
		}
		seen[normalized] = true
	}
}
