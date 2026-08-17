package runtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type exhaustionRecorder struct {
	called bool
	cause  error
}

func (*exhaustionRecorder) Handle(context.Context, Task) error { return nil }

func (handler *exhaustionRecorder) HandleExhausted(_ context.Context, _ Task, cause error) error {
	handler.called = true
	handler.cause = cause
	return nil
}

func TestStableErrorCode(t *testing.T) {
	t.Parallel()
	if got := stableErrorCode(Error{ErrorCode: "MODEL_UNAVAILABLE", Cause: errors.New("offline")}); got != "MODEL_UNAVAILABLE" {
		t.Fatalf("stableErrorCode() = %q", got)
	}
	if got := stableErrorCode(errors.New("plain")); got != "TASK_FAILED" {
		t.Fatalf("stableErrorCode() fallback = %q", got)
	}
}

func TestRetryPolicy(t *testing.T) {
	t.Parallel()
	if !retryable(errors.New("temporary")) {
		t.Fatal("plain errors should be retried")
	}
	if retryable(Error{ErrorCode: "TOOL_INPUT_INVALID", Cause: errors.New("bad input"), Permanent: true}) {
		t.Fatal("permanent coded errors must not be retried")
	}
	if got := retryDelay(1); got != time.Second {
		t.Fatalf("first retry delay = %s", got)
	}
	if got := retryDelay(20); got != time.Minute {
		t.Fatalf("retry delay should cap at one minute, got %s", got)
	}
}

func TestNotifyExhausted(t *testing.T) {
	t.Parallel()
	cause := errors.New("provider unavailable")
	handler := &exhaustionRecorder{}
	if err := notifyExhausted(context.Background(), handler, Task{}, cause); err != nil {
		t.Fatal(err)
	}
	if !handler.called || !errors.Is(handler.cause, cause) {
		t.Fatal("exhaustion handler did not receive the terminal task error")
	}
	if err := notifyExhausted(context.Background(), HandlerFunc(func(context.Context, Task) error { return nil }), Task{}, cause); err != nil {
		t.Fatal(err)
	}
}
