package tunnelruntime

import (
	"testing"
	"time"
)

func TestBackoffSequenceAndCap(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	for index, expected := range want {
		if actual := Backoff(index + 1); actual != expected {
			t.Fatalf("Backoff(%d) = %s, want %s", index+1, actual, expected)
		}
	}
}

func TestCountersReturnHeartbeatDeltas(t *testing.T) {
	var counters Counters
	counters.AddBytes(10)
	counters.AddThrottled()
	if bytes, limits := counters.Delta(); bytes != 10 || limits != 1 {
		t.Fatalf("first delta = %d/%d", bytes, limits)
	}
	counters.AddBytes(4)
	if bytes, limits := counters.Delta(); bytes != 4 || limits != 0 {
		t.Fatalf("second delta = %d/%d", bytes, limits)
	}
}
