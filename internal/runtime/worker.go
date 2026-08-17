package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	DefaultLease = 30 * time.Second
	DefaultPoll  = 500 * time.Millisecond
)

type Task struct {
	db.RuntimeTask
}

type Handler interface {
	Handle(context.Context, Task) error
}

type ExhaustionHandler interface {
	HandleExhausted(context.Context, Task, error) error
}

type HandlerFunc func(context.Context, Task) error

func (handler HandlerFunc) Handle(ctx context.Context, task Task) error { return handler(ctx, task) }

type Processor struct {
	Store  *postgres.Store
	Queue  string
	Owner  string
	Lease  time.Duration
	Poll   time.Duration
	Handle Handler
	Logger *slog.Logger
}

func (processor Processor) Run(ctx context.Context) error {
	if processor.Store == nil || processor.Handle == nil || processor.Queue == "" || processor.Owner == "" {
		return errors.New("runtime processor requires store, handler, queue, and owner")
	}
	if processor.Lease <= 0 {
		processor.Lease = DefaultLease
	}
	if processor.Poll <= 0 {
		processor.Poll = DefaultPoll
	}
	if processor.Logger == nil {
		processor.Logger = slog.Default()
	}
	ticker := time.NewTicker(processor.Poll)
	defer ticker.Stop()
	for {
		handled, err := processor.runOne(ctx)
		if err != nil {
			processor.Logger.Error("runtime task processing failed", "queue", processor.Queue, "error", err)
		}
		if handled {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (processor Processor) runOne(ctx context.Context) (bool, error) {
	claimed, err := processor.Store.Queries.ClaimRuntimeTask(ctx, db.ClaimRuntimeTaskParams{
		Queue: processor.Queue, LeaseOwner: pgtype.Text{String: processor.Owner, Valid: true}, Column3: interval(processor.Lease),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	running, err := processor.Store.Queries.StartRuntimeTask(ctx, db.StartRuntimeTaskParams{
		ID: claimed.ID, LeaseOwner: claimed.LeaseOwner, FenceToken: claimed.FenceToken,
	})
	if err != nil {
		return true, err
	}
	taskCtx, cancelTask := context.WithCancel(ctx)
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		interval := max(processor.Lease/3, time.Second)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-taskCtx.Done():
				return
			case <-ticker.C:
				_, renewErr := processor.Store.Queries.RenewRuntimeTaskLease(taskCtx, db.RenewRuntimeTaskLeaseParams{
					ID: running.ID, LeaseOwner: running.LeaseOwner, FenceToken: running.FenceToken, Column4: intervalValue(processor.Lease),
				})
				if renewErr != nil {
					cancelTask()
					return
				}
			}
		}
	}()
	err = processor.Handle.Handle(taskCtx, Task{RuntimeTask: running})
	cancelTask()
	<-renewDone
	if err != nil && running.Attempt < running.MaxAttempts && retryable(err) {
		_, requeueErr := processor.Store.Queries.RequeueRuntimeTask(ctx, db.RequeueRuntimeTaskParams{
			ID: running.ID, LeaseOwner: running.LeaseOwner, FenceToken: running.FenceToken,
			LastErrorCode: pgtype.Text{String: stableErrorCode(err), Valid: true},
			AvailableAt:   pgtype.Timestamptz{Time: time.Now().UTC().Add(retryDelay(running.Attempt)), Valid: true},
		})
		if requeueErr != nil {
			return true, requeueErr
		}
		return true, nil
	}
	status := "succeeded"
	errorCode := pgtype.Text{}
	if err != nil {
		if exhaustionErr := notifyExhausted(ctx, processor.Handle, Task{RuntimeTask: running}, err); exhaustionErr != nil {
			return true, exhaustionErr
		}
		status = "failed"
		errorCode = pgtype.Text{String: stableErrorCode(err), Valid: true}
	}
	_, finishErr := processor.Store.Queries.FinishRuntimeTask(ctx, db.FinishRuntimeTaskParams{
		ID: running.ID, LeaseOwner: running.LeaseOwner, FenceToken: running.FenceToken, Status: status, LastErrorCode: errorCode,
	})
	if finishErr != nil {
		return true, finishErr
	}
	return true, err
}

func notifyExhausted(ctx context.Context, handler Handler, task Task, cause error) error {
	value, ok := handler.(ExhaustionHandler)
	if !ok {
		return nil
	}
	return value.HandleExhausted(ctx, task, cause)
}

func interval(duration time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: duration.Microseconds(), Valid: true}
}

func intervalValue(duration time.Duration) pgtype.Interval { return interval(duration) }

func stableErrorCode(err error) string {
	type coded interface{ Code() string }
	var value coded
	if errors.As(err, &value) && value.Code() != "" {
		return value.Code()
	}
	return "TASK_FAILED"
}

type Error struct {
	ErrorCode string
	Cause     error
	Permanent bool
}

func (err Error) Error() string { return fmt.Sprintf("%s: %v", err.ErrorCode, err.Cause) }
func (err Error) Unwrap() error { return err.Cause }
func (err Error) Code() string  { return err.ErrorCode }

func retryable(err error) bool {
	var coded Error
	return !errors.As(err, &coded) || !coded.Permanent
}

func retryDelay(attempt int32) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Second << min(attempt-1, 6)
	return min(delay, time.Minute)
}
