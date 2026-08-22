package remoteaccess

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

func TestGrantCannotBeExpandedByRequest(t *testing.T) {
	now := time.Now().UTC()
	user, enterprise, host, account := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	grant := Grant{EnterpriseID: enterprise, SubjectType: "user", SubjectID: user, HostIDs: []uuid.UUID{host}, ManagedAccountIDs: []uuid.UUID{account}, Protocols: []string{"ssh"}, Actions: []string{"terminal"}, ValidFrom: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour), Enabled: true}
	intent := Intent{EnterpriseID: enterprise, UserID: user, HostID: host, ManagedAccountID: account, Protocol: "ssh", Action: "terminal", AuthorizationTime: now}
	if !grant.Authorizes(intent) {
		t.Fatal("expected exact grant match")
	}
	intent.Protocol = "winrs"
	if grant.Authorizes(intent) {
		t.Fatal("request expanded protocol beyond grant")
	}
	intent.Protocol, intent.HostID = "ssh", uuid.New()
	if grant.Authorizes(intent) {
		t.Fatal("request expanded host beyond grant")
	}
}

func TestMFARequirementFailsClosed(t *testing.T) {
	_, err := MatchPolicies([]Policy{{ID: uuid.New(), Version: 1, Enabled: true, Protocols: []string{"ssh"}, RequireMFA: true}}, Intent{Protocol: "ssh"})
	if !errors.Is(err, ErrMFARequired) {
		t.Fatalf("expected MFA fail closed, got %v", err)
	}
}

type memoryTicketStore struct {
	mu       sync.Mutex
	hash     [32]byte
	binding  TicketBinding
	consumed bool
}

func (store *memoryTicketStore) CreateTicket(_ context.Context, binding TicketBinding, hash [32]byte) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.hash, store.binding = hash, binding
	return nil
}
func (store *memoryTicketStore) ConsumeTicket(_ context.Context, hash [32]byte, expected TicketBinding, now time.Time) (TicketBinding, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.hash != hash || store.binding.SessionID != expected.SessionID || store.binding.HTTPSessionID != expected.HTTPSessionID || store.binding.UserID != expected.UserID || store.binding.EnterpriseID != expected.EnterpriseID || store.binding.HostID != expected.HostID || store.binding.ManagedAccountID != expected.ManagedAccountID || store.binding.Protocol != expected.Protocol || store.binding.SessionFence != expected.SessionFence || store.binding.AuthorizationVersion != expected.AuthorizationVersion {
		return TicketBinding{}, ErrTicketBinding
	}
	if !now.Before(store.binding.ExpiresAt) {
		return TicketBinding{}, ErrTicketExpired
	}
	if store.consumed {
		return TicketBinding{}, ErrTicketConsumed
	}
	store.consumed = true
	return store.binding, nil
}

func TestTicketIsBoundAndConsumedOnce(t *testing.T) {
	now := time.Now().UTC()
	store := &memoryTicketStore{}
	issuer := TicketIssuer{Store: store, Now: func() time.Time { return now }}
	binding := TicketBinding{SessionID: uuid.New(), HTTPSessionID: uuid.New(), EnterpriseID: uuid.New(), UserID: uuid.New(), HostID: uuid.New(), ManagedAccountID: uuid.New(), LeaseID: uuid.New(), Protocol: "ssh", AuthorizationVersion: 3, SessionFence: 2}
	value, _, err := issuer.Issue(context.Background(), binding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.Consume(context.Background(), value, binding); err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.Consume(context.Background(), value, binding); !errors.Is(err, ErrTicketConsumed) {
		t.Fatalf("expected replay rejection, got %v", err)
	}
}

type memoryObjects struct{ values map[string][]byte }

func (store *memoryObjects) Put(_ context.Context, key string, value []byte) error {
	if store.values == nil {
		store.values = map[string][]byte{}
	}
	store.values[key] = append([]byte(nil), value...)
	return nil
}
func (store *memoryObjects) Get(_ context.Context, key string) ([]byte, error) {
	return store.values[key], nil
}

func TestRecordingChunkIsEncryptedAndChained(t *testing.T) {
	objects := &memoryObjects{}
	recorder := Recorder{Store: objects, RecordingID: uuid.NewString(), DEK: make([]byte, 32)}
	if _, err := recorder.Append(context.Background(), RecordingEvent{Time: 0.1, Type: "i", Data: "secret command"}); err != nil {
		t.Fatal(err)
	}
	first, err := recorder.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if string(objects.values[first.ObjectKey]) == "secret command" {
		t.Fatal("recording stored plaintext")
	}
	if _, err := recorder.Append(context.Background(), RecordingEvent{Time: 0.2, Type: "o", Data: "output"}); err != nil {
		t.Fatal(err)
	}
	second, err := recorder.Flush(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.PreviousHash != first.Hash {
		t.Fatal("hash chain did not link chunks")
	}
}

func TestRecordingEnvelopeRejectsDivergentKeyReference(t *testing.T) {
	envelope := secret.Envelope{Provider: secret.EnvelopeProviderLocal, KeyID: "recording-key", KeyVersion: 2}
	wrapped, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	record := db.RemoteAccessRecording{KeyProvider: envelope.Provider, KeyID: envelope.KeyID, KeyVersion: int32(envelope.KeyVersion), WrappedDek: wrapped}
	if _, err := recordingEnvelope(record); err != nil {
		t.Fatalf("matching recording key reference was rejected: %v", err)
	}
	record.KeyVersion++
	if _, err := recordingEnvelope(record); !errors.Is(err, ErrRecordingUnavailable) {
		t.Fatalf("divergent recording key reference was accepted: %v", err)
	}
}

func TestWebSocketProtocolRequiresHelloAndMonotonicSequence(t *testing.T) {
	now := time.Now().UTC()
	state := ProtocolState{StartedAt: now}
	hello := []byte(`{"protocol":"argus.remote_access/v1","type":"client_hello","sequence":1,"ticket":"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ","nonce":"0123456789abcdef","cols":80,"rows":24}`)
	if _, err := state.Accept(hello, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	input := []byte(`{"protocol":"argus.remote_access/v1","type":"input","sequence":2,"data":"ls\n"}`)
	if _, err := state.Accept(input, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Accept(input, now.Add(2*time.Second)); !errors.Is(err, ErrProtocol) {
		t.Fatalf("duplicate sequence accepted: %v", err)
	}
}

type flakyObjects struct {
	fail   bool
	values map[string][]byte
}

func (store *flakyObjects) Put(_ context.Context, key string, value []byte) error {
	if store.fail {
		return errors.New("ObjectStore unavailable")
	}
	if store.values == nil {
		store.values = map[string][]byte{}
	}
	store.values[key] = append([]byte(nil), value...)
	return nil
}

func (store *flakyObjects) Get(_ context.Context, key string) ([]byte, error) {
	return store.values[key], nil
}

func TestRecordingRetriesObjectStoreForThirtySeconds(t *testing.T) {
	now := time.Now().UTC()
	objects := &flakyObjects{fail: true}
	recorder := Recorder{Store: objects, RecordingID: uuid.NewString(), DEK: make([]byte, 32), Now: func() time.Time { return now }}
	if _, err := recorder.Append(context.Background(), RecordingEvent{Time: 0.1, Type: "o", Data: "output"}); err != nil {
		t.Fatal(err)
	}
	now = now.Add(RecordingFlushAfter)
	if chunks, err := recorder.FlushDue(context.Background()); err != nil || len(chunks) != 0 {
		t.Fatalf("initial outage must remain buffered: chunks=%d err=%v", len(chunks), err)
	}
	now = now.Add(RecordingStoreGrace - time.Second)
	if _, err := recorder.FlushDue(context.Background()); err != nil {
		t.Fatalf("outage inside grace window failed session: %v", err)
	}
	now = now.Add(time.Second)
	if _, err := recorder.FlushDue(context.Background()); !errors.Is(err, ErrRecordingUnavailable) {
		t.Fatalf("expected fail closed after grace window, got %v", err)
	}
}

func TestRecordingRecoversInsideObjectStoreGrace(t *testing.T) {
	now := time.Now().UTC()
	objects := &flakyObjects{fail: true}
	recorder := Recorder{Store: objects, RecordingID: uuid.NewString(), DEK: make([]byte, 32), Now: func() time.Time { return now }}
	_, _ = recorder.Append(context.Background(), RecordingEvent{Time: 0.1, Type: "o", Data: "output"})
	now = now.Add(RecordingFlushAfter)
	_, _ = recorder.FlushDue(context.Background())
	objects.fail = false
	now = now.Add(time.Second)
	chunks, err := recorder.FlushDue(context.Background())
	if err != nil || len(chunks) != 1 {
		t.Fatalf("expected buffered recording recovery: chunks=%d err=%v", len(chunks), err)
	}
}

func TestTerminationHubRejectsOldFence(t *testing.T) {
	hub := NewTerminationHub()
	sessionID := uuid.New()
	events, unregister := hub.Register(sessionID, 4)
	defer unregister()
	hub.Notify(sessionID, Termination{Fence: 4, Reason: "stale"})
	select {
	case <-events:
		t.Fatal("same fence terminated active session")
	default:
	}
	hub.Notify(sessionID, Termination{Fence: 5, Reason: "revoked"})
	select {
	case event := <-events:
		if event.Reason != "revoked" {
			t.Fatalf("unexpected reason %q", event.Reason)
		}
	case <-time.After(time.Second):
		t.Fatal("new fence did not terminate active session")
	}
}

func TestSessionTrackerWaitsForActiveWebSocketsDuringDrain(t *testing.T) {
	tracker := NewSessionTracker()
	leave, accepted := tracker.Enter()
	if !accepted {
		t.Fatal("tracker rejected a session before drain")
	}
	drained := tracker.BeginDrain()
	if _, accepted := tracker.Enter(); accepted {
		t.Fatal("tracker accepted a new session during drain")
	}
	select {
	case <-drained:
		t.Fatal("tracker drained before the active session finished")
	default:
	}
	leave()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("tracker did not release the drain wait")
	}
}

func TestSessionTrackerDrainsImmediatelyWithoutSessions(t *testing.T) {
	select {
	case <-NewSessionTracker().BeginDrain():
	case <-time.After(time.Second):
		t.Fatal("empty tracker did not drain immediately")
	}
}
