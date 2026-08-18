package agent

import (
	"testing"

	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestChooseCompactionBoundaryUsesCompleteTurn(t *testing.T) {
	t.Parallel()
	events := []db.ConversationEvent{{Sequence: 1, EventType: "user_message"}, {Sequence: 2, EventType: "tool_call_requested"}, {Sequence: 3, EventType: "tool_call_result"}, {Sequence: 4, EventType: "assistant_message"}, {Sequence: 5, EventType: "user_message"}, {Sequence: 6, EventType: "assistant_message"}}
	through, kept, ok := ChooseCompactionBoundary(events)
	if !ok || through != 4 || kept != 5 {
		t.Fatalf("boundary = %d/%d, %v", through, kept, ok)
	}
}

func TestChooseCompactionBoundaryRejectsIncompleteGroups(t *testing.T) {
	t.Parallel()
	events := []db.ConversationEvent{{Sequence: 1, EventType: "user_message"}, {Sequence: 2, EventType: "tool_call_requested"}, {Sequence: 3, EventType: "tool_call_result"}, {Sequence: 4, EventType: "pending_action_created"}}
	if _, _, ok := ChooseCompactionBoundary(events); ok {
		t.Fatal("incomplete event groups must not be compacted")
	}
}

func TestOnlyHardLimitCompactionMayResumeOrFailRun(t *testing.T) {
	t.Parallel()
	if !requiresCompactionResume("hard_limit") {
		t.Fatal("hard-limit compaction must own waiting_system recovery")
	}
	for _, reason := range []string{"soft_limit", "manual", "compaction_completed"} {
		if requiresCompactionResume(reason) {
			t.Fatalf("%s compaction must not mutate the Run lifecycle", reason)
		}
	}
}
