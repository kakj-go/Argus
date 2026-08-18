package action

import (
	"errors"
	"testing"
	"time"
)

func TestCardBindingStateErrorDistinguishesTerminalStates(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		status    string
		expiresAt time.Time
		want      error
	}{
		{name: "pending", status: "pending", expiresAt: now.Add(time.Minute)},
		{name: "consumed", status: "consumed", expiresAt: now.Add(time.Minute), want: ErrBindingConsumed},
		{name: "expired status", status: "expired", expiresAt: now.Add(time.Minute), want: ErrBindingExpired},
		{name: "expired time", status: "pending", expiresAt: now.Add(-time.Second), want: ErrBindingExpired},
		{name: "invalidated", status: "invalidated", expiresAt: now.Add(time.Minute), want: ErrInvalidated},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := cardBindingStateError(test.status, test.expiresAt, now)
			if !errors.Is(err, test.want) || test.want == nil && err != nil {
				t.Fatalf("cardBindingStateError() = %v, want %v", err, test.want)
			}
		})
	}
}
