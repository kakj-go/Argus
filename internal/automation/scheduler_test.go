package automation

import (
	"context"
	"testing"
	"time"

	"github.com/kakj-go/Argus/internal/mcp"
)

func TestCronParserRequiresFiveFields(t *testing.T) {
	if _, err := cronParser.Parse("0 * * * *"); err != nil {
		t.Fatalf("five-field cron rejected: %v", err)
	}
	if _, err := cronParser.Parse("0 0 * * * *"); err == nil {
		t.Fatal("six-field cron with seconds must be rejected")
	}
}

func TestAutomationToolValidationRejectsCommitAndHiddenTools(t *testing.T) {
	for _, test := range []struct {
		name     string
		toolID   string
		metadata mcp.Metadata
	}{
		{name: "commit", toolID: "host.update.commit", metadata: mcp.Metadata{Risk: "write", Visibility: mcp.Visible}},
		{name: "hidden preview", toolID: "host.update.preview", metadata: mcp.Metadata{Risk: "write", Visibility: mcp.Hidden}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateAutomationTool(test.toolID, test.metadata); err == nil {
				t.Fatal("unsafe Automation tool must be rejected")
			}
		})
	}
	if err := validateAutomationTool("host.update.preview", mcp.Metadata{Risk: "write", Visibility: mcp.Visible}); err != nil {
		t.Fatalf("governed Preview tool rejected: %v", err)
	}
	if err := validateAutomationTool("host.list", mcp.Metadata{Risk: "read", Visibility: mcp.Visible}); err != nil {
		t.Fatalf("read Tool rejected: %v", err)
	}
}

func TestValidateRejectsUnknownIANATimezoneBeforeStorage(t *testing.T) {
	_, err := (Service{}).Validate(context.Background(), newID(), Input{
		Cron: "0 * * * *", Timezone: "Mars/Olympus_Mons",
	})
	if err != ErrUnavailable {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestCollapseDueSchedulesKeepsOnlyLatestFifteenMinuteMisfire(t *testing.T) {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := cronParser.Parse("*/5 * * * *")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 17, 8, 17, 0, 0, time.UTC)
	due := now.Add(-32 * time.Minute)
	scheduledFor, next := collapseDueSchedules(schedule, location, due, now)
	if age := now.Sub(scheduledFor); age < 0 || age > 5*time.Minute {
		t.Fatalf("expected latest due occurrence, age=%s", age)
	}
	if !next.After(now) {
		t.Fatalf("next occurrence must be in the future: %s", next)
	}
}

func TestCronScheduleRemainsMonotonicAcrossDSTFallback(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	schedule, err := cronParser.Parse("30 1 * * *")
	if err != nil {
		t.Fatal(err)
	}
	first := schedule.Next(time.Date(2026, time.October, 31, 0, 0, 0, 0, location)).UTC()
	second := schedule.Next(first.In(location)).UTC()
	if !second.After(first) {
		t.Fatalf("DST schedule did not advance: first=%s second=%s", first, second)
	}
}
