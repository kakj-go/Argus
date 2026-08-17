package sandbox

import (
	"testing"
	"time"
)

func TestSandboxReservationHonorsMonthlyQuota(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		limit, used, request int64
		want                 int64
		allowed              bool
	}{
		{name: "full request", limit: 600, used: 100, request: 60, want: 60, allowed: true},
		{name: "bounded by remainder", limit: 600, used: 570, request: 60, want: 30, allowed: true},
		{name: "exhausted", limit: 600, used: 600, request: 60, allowed: false},
		{name: "disabled quota", limit: 0, request: 60, allowed: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, allowed := sandboxReservationSeconds(test.limit, test.used, test.request)
			if got != test.want || allowed != test.allowed {
				t.Fatalf("reservation = (%d, %t), want (%d, %t)", got, allowed, test.want, test.allowed)
			}
		})
	}
}

func TestMonthStartUsesUTC(t *testing.T) {
	t.Parallel()
	location := time.FixedZone("UTC+8", 8*60*60)
	value := time.Date(2026, time.August, 1, 1, 30, 0, 0, location)
	want := time.Date(2026, time.July, 1, 0, 0, 0, 0, time.UTC)
	if got := monthStart(value); !got.Equal(want) {
		t.Fatalf("monthStart() = %s, want %s", got, want)
	}
}
