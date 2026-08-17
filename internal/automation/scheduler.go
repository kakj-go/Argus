package automation

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/robfig/cron/v3"

	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type Scheduler struct {
	Store  *postgres.Store
	Poll   time.Duration
	Logger *slog.Logger
}

func (scheduler Scheduler) Run(ctx context.Context) error {
	if scheduler.Poll <= 0 {
		scheduler.Poll = time.Second
	}
	if scheduler.Logger == nil {
		scheduler.Logger = slog.Default()
	}
	ticker := time.NewTicker(scheduler.Poll)
	defer ticker.Stop()
	for {
		if err := scheduler.scan(ctx); err != nil {
			scheduler.Logger.Error("automation schedule scan failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (scheduler Scheduler) scan(ctx context.Context) error {
	return scheduler.Store.InTx(ctx, func(q *db.Queries) error {
		now := time.Now().UTC()
		items, err := q.ClaimDueAutomations(ctx, db.ClaimDueAutomationsParams{NextRunAt: pgtype.Timestamptz{Time: now, Valid: true}, Limit: 50})
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := scheduler.scheduleOne(ctx, q, item, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (scheduler Scheduler) scheduleOne(ctx context.Context, q *db.Queries, item db.Automation, now time.Time) error {
	location, err := time.LoadLocation(item.Timezone)
	if err != nil {
		return err
	}
	schedule, err := cronParser.Parse(item.Cron)
	if err != nil {
		return err
	}
	scheduledFor, next := collapseDueSchedules(schedule, location, item.NextRunAt.Time, now)
	if _, err := q.GetActiveAutomationRun(ctx, db.GetActiveAutomationRunParams{AutomationID: item.ID, EnterpriseID: item.EnterpriseID}); err == nil {
		_, err = q.CreateAutomationRun(ctx, db.CreateAutomationRunParams{ID: newID(), AutomationID: item.ID, EnterpriseID: item.EnterpriseID, AutomationRevision: item.Revision,
			ScheduledFor: pgtype.Timestamptz{Time: scheduledFor, Valid: true}, Status: "skipped"})
		if err == nil {
			_, err = q.AdvanceAutomation(ctx, db.AdvanceAutomationParams{ID: item.ID, EnterpriseID: item.EnterpriseID, NextRunAt: pgtype.Timestamptz{Time: next, Valid: true}})
		}
		return err
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if now.Sub(scheduledFor) > 15*time.Minute {
		_, err = q.CreateAutomationRun(ctx, db.CreateAutomationRunParams{ID: newID(), AutomationID: item.ID, EnterpriseID: item.EnterpriseID, AutomationRevision: item.Revision,
			ScheduledFor: pgtype.Timestamptz{Time: scheduledFor, Valid: true}, Status: "skipped"})
		if err != nil {
			return err
		}
		_, err = q.AdvanceAutomation(ctx, db.AdvanceAutomationParams{ID: item.ID, EnterpriseID: item.EnterpriseID, NextRunAt: pgtype.Timestamptz{Time: next, Valid: true}})
		return err
	}
	run, err := q.CreateAutomationRun(ctx, db.CreateAutomationRunParams{ID: newID(), AutomationID: item.ID, EnterpriseID: item.EnterpriseID, AutomationRevision: item.Revision,
		ScheduledFor: pgtype.Timestamptz{Time: scheduledFor, Valid: true}, Status: "pending"})
	if err != nil {
		return err
	}
	payload, _ := json.Marshal(TaskPayload{RunID: run.ID, EnterpriseID: run.EnterpriseID})
	task, err := q.CreateRuntimeTask(ctx, db.CreateRuntimeTaskParams{ID: newID(), EnterpriseID: uuid.NullUUID{UUID: item.EnterpriseID, Valid: true},
		Queue: "automation", Payload: payload, MaxAttempts: 5, AvailableAt: pgtype.Timestamptz{Time: now, Valid: true}})
	if err != nil {
		return err
	}
	_, err = q.UpdateAutomationRun(ctx, db.UpdateAutomationRunParams{ID: run.ID, EnterpriseID: run.EnterpriseID, Status: "pending", TaskID: uuid.NullUUID{UUID: task.ID, Valid: true}})
	if err != nil {
		return err
	}
	_, err = q.AdvanceAutomation(ctx, db.AdvanceAutomationParams{ID: item.ID, EnterpriseID: item.EnterpriseID, NextRunAt: pgtype.Timestamptz{Time: next, Valid: true}})
	return err
}

func collapseDueSchedules(schedule cron.Schedule, location *time.Location, dueAt, now time.Time) (time.Time, time.Time) {
	scheduledFor := dueAt.UTC()
	next := schedule.Next(dueAt.In(location)).UTC()
	for !next.After(now) {
		scheduledFor = next
		next = schedule.Next(next.In(location)).UTC()
	}
	return scheduledFor, next
}
