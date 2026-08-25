package argusdev

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRetryImageBuildSucceedsOnFirstAttempt(t *testing.T) {
	attempts := 0
	err := retryImageBuild(context.Background(), []time.Duration{0, 0}, func(context.Context) error {
		attempts++
		return nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryImageBuildRetriesThenSucceeds(t *testing.T) {
	transient := errors.New("registry metadata EOF")
	attempts := 0
	var retried []int
	err := retryImageBuild(context.Background(), []time.Duration{0, 0}, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return transient
		}
		return nil
	}, func(attempt int, err error, _ time.Duration) {
		if !errors.Is(err, transient) {
			t.Fatalf("retry error = %v", err)
		}
		retried = append(retried, attempt)
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 || len(retried) != 2 || retried[0] != 1 || retried[1] != 2 {
		t.Fatalf("attempts = %d, retries = %v", attempts, retried)
	}
}

func TestRetryImageBuildReturnsFinalError(t *testing.T) {
	want := errors.New("registry unavailable")
	attempts := 0
	err := retryImageBuild(context.Background(), []time.Duration{0, 0}, func(context.Context) error {
		attempts++
		return want
	}, nil)
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("error = %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestRetryImageBuildHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	err := retryImageBuild(ctx, []time.Duration{time.Hour, time.Hour}, func(context.Context) error {
		attempts++
		cancel()
		return errors.New("cancelled build")
	}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestRetryImageBuildDoesNotRetryCapabilityErrors(t *testing.T) {
	attempts := 0
	err := retryImageBuild(context.Background(), []time.Duration{0, 0}, func(context.Context) error {
		attempts++
		return fmt.Errorf("%w: docker was not found", errCapability)
	}, nil)
	if !errors.Is(err, errCapability) {
		t.Fatalf("error = %v, want capability error", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestE2EWorkerDeploymentsFollowInstallProfile(t *testing.T) {
	tests := []struct {
		profile string
		want    []string
	}{
		{profile: "evaluation", want: []string{"argus-worker"}},
		{profile: "local-hardening", want: []string{
			"argus-worker-agent",
			"argus-worker-action",
			"argus-worker-compaction",
			"argus-worker-sandbox",
		}},
	}
	for _, test := range tests {
		t.Run(test.profile, func(t *testing.T) {
			if got := e2eWorkerDeployments(test.profile); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("e2eWorkerDeployments(%q) = %v, want %v", test.profile, got, test.want)
			}
		})
	}
}
