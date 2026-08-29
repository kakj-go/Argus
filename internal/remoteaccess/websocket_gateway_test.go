package remoteaccess

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

func TestOutputBacklogKeepsMostRecentChunks(t *testing.T) {
	backlog := &outputBacklog{}
	backlog.append("stdout", bytes.Repeat([]byte("a"), maxBacklogBytes/2))
	backlog.append("stdout", bytes.Repeat([]byte("b"), maxBacklogBytes/2))
	backlog.append("stdout", []byte("newest"))
	chunks := backlog.snapshot()
	if len(chunks) != 2 {
		t.Fatalf("expected the overflow to drop the oldest chunk, got %d chunks", len(chunks))
	}
	if !bytes.HasPrefix(chunks[0].Data, []byte("b")) || !bytes.Equal(chunks[1].Data, []byte("newest")) {
		t.Fatal("backlog must keep the most recent output for replay")
	}
	if again := backlog.snapshot(); len(again) != 2 {
		t.Fatalf("snapshot must retain the scrollback for a later re-attach, got %d chunks", len(again))
	}
}

func TestSessionParksTakeStopsLoop(t *testing.T) {
	parks := NewSessionParks()
	id := uuid.New()
	if parks.Has(id) {
		t.Fatal("empty registry must not report a park")
	}
	parked := &ParkedSession{stop: make(chan struct{}), done: make(chan struct{})}
	parks.Put(id, parked)
	if !parks.Has(id) {
		t.Fatal("registry must report the parked session")
	}
	loopExited := make(chan struct{})
	go func() {
		<-parked.stop
		close(parked.done)
		close(loopExited)
	}()
	taken := parks.Take(id)
	if taken != parked {
		t.Fatal("Take must return the parked session")
	}
	if parks.Has(id) {
		t.Fatal("Take must remove the session from the registry")
	}
	if parks.Take(id) != nil {
		t.Fatal("second Take must return nil")
	}
	<-loopExited
}
