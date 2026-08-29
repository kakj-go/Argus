package remoteaccess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

type BackendFrame struct {
	Type   string
	Stream string
	Data   []byte
	Status string
	Reason string
}

type BackendSession interface {
	Send(context.Context, ClientFrame) error
	Receive(context.Context) (BackendFrame, error)
	Close(context.Context, string) error
}

type BackendFactory interface {
	Open(context.Context, ConnectionTarget, int, int) (BackendSession, error)
}

// maxBacklogBytes bounds the detached-session output replay buffer so a
// parked session cannot grow without limit while nobody is attached.
const maxBacklogBytes = 256 << 10

type WebSocketGateway struct {
	Service        GatewayService
	Backends       BackendFactory
	ObjectStore    ObjectStore
	AllowedOrigins []string
	RejectNew      <-chan struct{}
	Drain          <-chan struct{}
	Sessions       *SessionTracker
	Terminations   *TerminationHub
	// Parks holds sessions whose browser disconnected while the remote PTY
	// keeps running; a fresh ticket can re-attach them on this instance.
	Parks  *SessionParks
	Logger *slog.Logger
	Now    func() time.Time
}

// SessionParks is the instance-local registry of detached live sessions.
type SessionParks struct {
	mu     sync.Mutex
	parked map[uuid.UUID]*ParkedSession
}

func NewSessionParks() *SessionParks {
	return &SessionParks{parked: map[uuid.UUID]*ParkedSession{}}
}

func (parks *SessionParks) Has(id uuid.UUID) bool {
	if parks == nil {
		return false
	}
	parks.mu.Lock()
	defer parks.mu.Unlock()
	_, ok := parks.parked[id]
	return ok
}

// Take removes a parked session and stops its park loop before returning it,
// guaranteeing the caller becomes the only consumer of the backend frames.
func (parks *SessionParks) Take(id uuid.UUID) *ParkedSession {
	if parks == nil {
		return nil
	}
	parks.mu.Lock()
	session, ok := parks.parked[id]
	delete(parks.parked, id)
	parks.mu.Unlock()
	if session == nil {
		return nil
	}
	close(session.stop)
	<-session.done
	_ = ok
	return session
}

func (parks *SessionParks) Put(id uuid.UUID, session *ParkedSession) {
	parks.mu.Lock()
	parks.parked[id] = session
	parks.mu.Unlock()
}

func (parks *SessionParks) Remove(id uuid.UUID) {
	parks.mu.Lock()
	delete(parks.parked, id)
	parks.mu.Unlock()
}

// outputBacklog is the session-scrollback ring: every output chunk (attached
// or parked) is appended so a re-attached browser can repaint the terminal
// after a page refresh, bounded by bytes.
type outputBacklog struct {
	mu     sync.Mutex
	chunks []BackendFrame
	bytes  int
}

func (backlog *outputBacklog) append(stream string, data []byte) {
	backlog.mu.Lock()
	defer backlog.mu.Unlock()
	backlog.chunks = append(backlog.chunks, BackendFrame{Type: "output", Stream: stream, Data: append([]byte(nil), data...)})
	backlog.bytes += len(data)
	for backlog.bytes > maxBacklogBytes && len(backlog.chunks) > 1 {
		backlog.bytes -= len(backlog.chunks[0].Data)
		backlog.chunks = backlog.chunks[1:]
	}
}

func (backlog *outputBacklog) snapshot() []BackendFrame {
	backlog.mu.Lock()
	defer backlog.mu.Unlock()
	chunks := make([]BackendFrame, len(backlog.chunks))
	copy(chunks, backlog.chunks)
	return chunks
}

// ParkedSession couples a detached engine with the lifecycle channels of its
// park goroutine.
type ParkedSession struct {
	engine *sessionEngine
	stop   chan struct{}
	done   chan struct{}
}

// sessionEngine owns one live remote session end to end: the backend PTY, the
// recording, protocol validation state and the idle/max-duration timers. The
// attached relay and the detached park loop are two phases of the same engine,
// so a browser refresh parks the session and a later attach resumes it without
// losing the backend or the scrollback.
type sessionEngine struct {
	gateway              WebSocketGateway
	sessionID            uuid.UUID
	target               ConnectionTarget
	recording            *GatewayRecording
	backend              BackendSession
	backendFrames        <-chan receivedBackendFrame
	terminationEvents    <-chan Termination
	unregisterCompletion func()
	sessionCtx           context.Context
	sessionCancel        context.CancelFunc
	state                *ProtocolState
	started              time.Time
	lastActivity         time.Time
	lastAuthCheck        time.Time
	// auditSequence numbers command-audit events per session (marker=1). The
	// WebSocket sequence restarts on every re-attach, so auditing by frame
	// sequence would collide with earlier connections under the
	// (session_id, sequence) unique constraint.
	auditSequence uint64
	backlog       *outputBacklog
	finishFence   int64
	finished      bool
}

type SessionTracker struct {
	mu       sync.Mutex
	active   int
	draining bool
	drained  chan struct{}
}

func NewSessionTracker() *SessionTracker {
	return &SessionTracker{drained: make(chan struct{})}
}

func (tracker *SessionTracker) Enter() (func(), bool) {
	if tracker == nil {
		return func() {}, true
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.draining {
		return nil, false
	}
	tracker.active++
	return tracker.leave, true
}

func (tracker *SessionTracker) BeginDrain() <-chan struct{} {
	if tracker == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if !tracker.draining {
		tracker.draining = true
		if tracker.active == 0 {
			close(tracker.drained)
		}
	}
	return tracker.drained
}

func (tracker *SessionTracker) leave() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.active > 0 {
		tracker.active--
	}
	if tracker.draining && tracker.active == 0 {
		select {
		case <-tracker.drained:
		default:
			close(tracker.drained)
		}
	}
}

type serverFrame struct {
	Protocol    string `json:"protocol"`
	Type        string `json:"type"`
	Sequence    uint64 `json:"sequence"`
	SessionID   string `json:"session_id,omitempty"`
	Mode        string `json:"mode,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
	IdleTimeout int64  `json:"idle_timeout_seconds,omitempty"`
	MaxDuration int64  `json:"max_duration_seconds,omitempty"`
	Stream      string `json:"stream,omitempty"`
	Data        string `json:"data,omitempty"`
	Status      string `json:"status,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Code        string `json:"code,omitempty"`
	Message     string `json:"message,omitempty"`
	Terminal    bool   `json:"terminal,omitempty"`
}

func (gateway WebSocketGateway) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	leave, accepted := gateway.Sessions.Enter()
	if !accepted {
		http.Error(response, "remote access Gateway is draining", http.StatusServiceUnavailable)
		return
	}
	defer leave()
	select {
	case <-gateway.RejectNew:
		http.Error(response, "remote access Gateway is draining", http.StatusServiceUnavailable)
		return
	default:
	}
	if request.Method != http.MethodGet || gateway.Service.Store == nil || gateway.Backends == nil || gateway.ObjectStore == nil {
		http.Error(response, "remote access unavailable", http.StatusServiceUnavailable)
		return
	}
	if !gateway.originAllowed(request.Header.Get("Origin")) {
		http.Error(response, "origin denied", http.StatusForbidden)
		return
	}
	sessionID, err := sessionIDFromPath(request.URL.Path)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	connection, err := websocket.Accept(response, request, &websocket.AcceptOptions{OriginPatterns: gateway.originPatterns()})
	if err != nil {
		return
	}
	connection.SetReadLimit(MaxFrameBytes)
	defer connection.CloseNow()
	gateway.serve(request.Context(), connection, sessionID)
}

func (gateway WebSocketGateway) serve(parent context.Context, connection *websocket.Conn, sessionID uuid.UUID) {
	requestCtx, requestCancel := context.WithCancel(parent)
	defer requestCancel()
	state := ProtocolState{StartedAt: gateway.now()}
	helloCtx, helloCancel := context.WithTimeout(requestCtx, HelloTimeout)
	messageType, raw, err := connection.Read(helloCtx)
	helloCancel()
	if err != nil || messageType != websocket.MessageText {
		closeProtocol(connection, "REMOTE_ACCESS_PROTOCOL_ERROR")
		return
	}
	hello, err := state.Accept(raw, gateway.now())
	if err != nil {
		closeProtocol(connection, "REMOTE_ACCESS_PROTOCOL_ERROR")
		return
	}
	// A parked session on this instance turns the ticket into a re-attach:
	// the PTY backend is reused instead of opening a second shell.
	reattach := gateway.Parks.Has(sessionID)
	target, err := gateway.Service.AuthorizeConnection(requestCtx, sessionID, hello.Ticket, reattach)
	hello.Ticket = ""
	if err != nil {
		gateway.logFailure("authorize", err)
		closeProtocol(connection, gatewayErrorCode(err))
		return
	}

	var engine *sessionEngine
	if reattach {
		parked := gateway.Parks.Take(sessionID)
		if parked == nil || parked.engine.finished {
			// The park vanished between Has and Take (expired or taken by a
			// concurrent attach); converge the row instead of leaving it stale.
			_ = gateway.Service.Finish(context.Background(), sessionID, target.Session.SessionFence, "connection_lost", "REMOTE_ACCESS_BACKEND_LOST")
			closeProtocol(connection, "REMOTE_ACCESS_CONNECTION_LOST")
			return
		}
		engine = parked.engine
		// The fresh WebSocket starts a new protocol sequence space; the
		// engine must not keep counting frames from the previous connection.
		engine.state = &state
	} else {
		engine, err = gateway.startSession(requestCtx, sessionID, target, &state, hello.Cols, hello.Rows)
		if err != nil {
			closeProtocol(connection, "REMOTE_ACCESS_CONNECTION_LOST")
			return
		}
	}

	writer := &websocketWriter{connection: connection}
	mode := "ssh_pty"
	if target.Protocol == "winrs" {
		mode = "winrs_line"
	}
	if err := writer.write(requestCtx, serverFrame{Type: "server_ready", SessionID: sessionID.String(), Mode: mode, Nonce: hello.Nonce,
		IdleTimeout: int64(target.IdleTimeout / time.Second), MaxDuration: int64(target.MaxDuration / time.Second)}); err != nil {
		gateway.finishRecording(context.Background(), engine.recording, "incomplete")
		engine.finalize("connection_lost", "CLIENT_DISCONNECTED")
		return
	}
	// Replay the session scrollback so the refreshed browser repaints; the
	// ring keeps accumulating so a later refresh replays again.
	for _, chunk := range engine.backlog.snapshot() {
		if err := writer.write(requestCtx, serverFrame{Type: "output", Stream: chunk.Stream, Data: string(chunk.Data)}); err != nil {
			engine.park()
			return
		}
	}

	clientFrames := make(chan receivedClientFrame, 1)
	go receiveWebSocketFrames(requestCtx, connection, clientFrames)
	finishStatus, finishReason := engine.relay(requestCtx, requestCancel, writer, clientFrames)
	_ = connection.Close(websocket.StatusNormalClosure, finishReason)
	if finishStatus != "" {
		_ = writer.write(context.Background(), serverFrame{Type: "state", Status: finishStatus, Reason: finishReason})
		engine.finalize(finishStatus, finishReason)
	}
}

// startSession performs the first-attach setup: recording, command audit,
// backend open and activation. On entry the session row is "connecting".
func (gateway WebSocketGateway) startSession(ctx context.Context, sessionID uuid.UUID, target ConnectionTarget, state *ProtocolState, cols, rows int) (*sessionEngine, error) {
	terminationEvents, unregisterTermination := gateway.Terminations.Register(sessionID, target.Session.SessionFence)
	var recording *GatewayRecording
	if target.Session.RecordingMode != "disabled" {
		opened, openErr := gateway.Service.OpenRecording(ctx, sessionID, gateway.ObjectStore)
		if openErr != nil {
			gateway.logFailure("open_recording", openErr)
			if target.Session.RecordingMode != "optional" {
				_ = gateway.Service.Finish(ctx, sessionID, target.Session.SessionFence, "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE")
				return nil, ErrRecordingUnavailable
			}
		} else {
			recording = &opened
		}
	}
	if target.Session.CommandAuditMode != "disabled" {
		if auditErr := gateway.Service.InitializeCommandAudit(ctx, sessionID); auditErr != nil {
			gateway.logFailure("initialize_command_audit", auditErr)
			if target.Session.CommandAuditMode == "required" || gateway.Service.RecordCommandAuditDegradation(ctx, sessionID) != nil {
				_ = gateway.Service.Finish(ctx, sessionID, target.Session.SessionFence, "failed", "REMOTE_ACCESS_COMMAND_AUDIT_UNAVAILABLE")
				return nil, ErrCommandAuditUnavailable
			}
		}
	}
	// The PTY backend must outlive the browser request: the gRPC stream is
	// bound to this detached context so a page refresh parks the session
	// instead of tearing the remote shell down with the HTTP request.
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	backend, err := gateway.Backends.Open(sessionCtx, target, cols, rows)
	if err != nil {
		sessionCancel()
		gateway.logFailure("open_backend", err)
		gateway.finishRecording(ctx, recording, "failed")
		_ = gateway.Service.Finish(ctx, sessionID, target.Session.SessionFence, "failed", "REMOTE_ACCESS_CONNECTION_LOST")
		return nil, err
	}
	if err := gateway.Service.MarkActive(ctx, sessionID, target.Session.SessionFence); err != nil {
		sessionCancel()
		_ = backend.Close(context.Background(), "gateway_closed")
		gateway.finishRecording(ctx, recording, "incomplete")
		return nil, err
	}
	now := gateway.now()
	engine := &sessionEngine{gateway: gateway, sessionID: sessionID, target: target, recording: recording, backend: backend,
		terminationEvents: terminationEvents, unregisterCompletion: unregisterTermination,
		sessionCtx: sessionCtx, sessionCancel: sessionCancel, state: state,
		started: now, lastActivity: now, lastAuthCheck: now, auditSequence: 1,
		backlog: &outputBacklog{}, finishFence: target.Session.SessionFence}
	engine.backendFrames = receiveBackendFramesDetached(sessionCtx, backend)
	return engine, nil
}

func receiveBackendFramesDetached(ctx context.Context, backend BackendSession) <-chan receivedBackendFrame {
	backendFrames := make(chan receivedBackendFrame, 1)
	go func() {
		defer close(backendFrames)
		for {
			frame, err := backend.Receive(ctx)
			select {
			case backendFrames <- receivedBackendFrame{frame: frame, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return backendFrames
}

// receiveBackendFrames serves the gateway peer bridge, which never parks: the
// peer stream ending is terminal for the whole bridge.
func receiveBackendFrames(ctx context.Context, backend BackendSession, output chan<- receivedBackendFrame) {
	for {
		frame, err := backend.Receive(ctx)
		select {
		case output <- receivedBackendFrame{frame: frame, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

// relay drives the attached phase. An empty return means the session was
// parked (browser disconnected); the backend keeps running.
func (engine *sessionEngine) relay(requestCtx context.Context, requestCancel context.CancelFunc, writer *websocketWriter, clientFrames <-chan receivedClientFrame) (string, string) {
	gateway := engine.gateway
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case termination, ok := <-engine.terminationEvents:
			if ok {
				engine.finishFence = termination.Fence
				_ = writer.write(requestCtx, serverFrame{Type: "state", Status: "terminating", Reason: termination.Reason})
				return "invalidated", termination.Reason
			}
		case <-gateway.Drain:
			_ = writer.write(requestCtx, serverFrame{Type: "state", Status: "terminating", Reason: "gateway_drain"})
			return "terminated", "gateway_drain"
		case <-requestCtx.Done():
			requestCancel()
			engine.park()
			return "", ""
		case <-ticker.C:
			if status, reason, done := engine.tickChecks(requestCtx); done {
				return status, reason
			}
		case item := <-clientFrames:
			if item.err != nil {
				requestCancel()
				engine.park()
				return "", ""
			}
			frame, err := engine.state.Accept(item.raw, gateway.now())
			if errors.Is(err, ErrChannelUnavailable) {
				engine.lastActivity = gateway.now()
				_ = writer.write(requestCtx, serverFrame{Type: "error", Code: "REMOTE_ACCESS_CHANNEL_NOT_AVAILABLE", Message: "channel not available", Terminal: false})
				continue
			}
			if err != nil || item.messageType != websocket.MessageText {
				_ = writer.write(requestCtx, serverFrame{Type: "error", Code: "REMOTE_ACCESS_PROTOCOL_ERROR", Message: "protocol error", Terminal: true})
				return "failed", "REMOTE_ACCESS_PROTOCOL_ERROR"
			}
			if businessActivity(frame.Type) {
				engine.lastActivity = gateway.now()
			}
			if frame.Type == "ping" {
				continue
			}
			if frame.Type == "close" {
				return "terminated", "client_close"
			}
			engine.auditSequence++
			if err := gateway.auditClientFrame(requestCtx, engine.sessionID, engine.target.Session.CommandAuditMode, engine.auditSequence, frame); err != nil {
				return "failed", "REMOTE_ACCESS_COMMAND_AUDIT_UNAVAILABLE"
			}
			if err := gateway.recordClientFrame(requestCtx, engine.recording, frame); err != nil {
				if engine.target.Session.RecordingMode != "optional" {
					return "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE"
				}
				gateway.finishRecording(context.Background(), engine.recording, "failed")
				engine.recording = nil
			}
			if err := engine.backend.Send(requestCtx, frame); err != nil {
				return "connection_lost", "backend_send_failed"
			}
		case item, ok := <-engine.backendFrames:
			if !ok || item.err != nil {
				if ok && errors.Is(item.err, io.EOF) || !ok {
					return "terminated", "remote_closed"
				}
				return "connection_lost", "backend_disconnected"
			}
			if businessActivity(item.frame.Type) {
				engine.lastActivity = gateway.now()
			}
			if err := gateway.recordBackendFrame(engine.sessionCtx, engine.recording, item.frame); err != nil {
				if engine.target.Session.RecordingMode != "optional" {
					return "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE"
				}
				gateway.finishRecording(context.Background(), engine.recording, "failed")
				engine.recording = nil
			}
			if item.frame.Type == "output" {
				if err := writer.write(requestCtx, serverFrame{Type: "output", Stream: item.frame.Stream, Data: string(item.frame.Data)}); err != nil {
					requestCancel()
					engine.park()
					return "", ""
				}
				// The scrollback ring covers the whole session so any later
				// re-attach repaints what this browser already saw.
				engine.backlog.append(item.frame.Stream, item.frame.Data)
			} else if item.frame.Type == "state" {
				if err := writer.write(requestCtx, serverFrame{Type: "state", Status: item.frame.Status, Reason: item.frame.Reason}); err != nil {
					requestCancel()
					engine.park()
					return "", ""
				}
				if terminalState(item.frame.Status) {
					return normalizeTerminalStatus(item.frame.Status), item.frame.Reason
				}
			}
		}
	}
}

// park hands the engine to the registry and starts the detached loop that
// keeps recording, buffers output and enforces idle/max-duration timeouts.
func (engine *sessionEngine) park() {
	parked := &ParkedSession{engine: engine, stop: make(chan struct{}), done: make(chan struct{})}
	engine.gateway.Parks.Put(engine.sessionID, parked)
	go engine.parkLoop(parked)
}

func (engine *sessionEngine) parkLoop(parked *ParkedSession) {
	defer close(parked.done)
	gateway := engine.gateway
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-parked.stop:
			return
		case termination, ok := <-engine.terminationEvents:
			if ok {
				engine.finishFence = termination.Fence
				gateway.Parks.Remove(engine.sessionID)
				engine.finalize("invalidated", termination.Reason)
				return
			}
		case <-gateway.Drain:
			gateway.Parks.Remove(engine.sessionID)
			engine.finalize("terminated", "gateway_drain")
			return
		case <-ticker.C:
			if status, reason, done := engine.tickChecks(engine.sessionCtx); done {
				gateway.Parks.Remove(engine.sessionID)
				engine.finalize(status, reason)
				return
			}
		case item, ok := <-engine.backendFrames:
			if !ok || item.err != nil {
				gateway.Parks.Remove(engine.sessionID)
				if ok && errors.Is(item.err, io.EOF) {
					engine.finalize("terminated", "remote_closed")
				} else if ok {
					engine.finalize("connection_lost", "backend_disconnected")
				} else {
					engine.finalize("terminated", "remote_closed")
				}
				return
			}
			if businessActivity(item.frame.Type) {
				engine.lastActivity = gateway.now()
			}
			if err := gateway.recordBackendFrame(engine.sessionCtx, engine.recording, item.frame); err != nil {
				if engine.target.Session.RecordingMode != "optional" {
					gateway.Parks.Remove(engine.sessionID)
					engine.finalize("failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE")
					return
				}
				gateway.finishRecording(context.Background(), engine.recording, "failed")
				engine.recording = nil
			}
			if item.frame.Type == "output" {
				engine.backlog.append(item.frame.Stream, item.frame.Data)
			} else if item.frame.Type == "state" && terminalState(item.frame.Status) {
				gateway.Parks.Remove(engine.sessionID)
				engine.finalize(normalizeTerminalStatus(item.frame.Status), item.frame.Reason)
				return
			}
		}
	}
}

// tickChecks runs the shared per-second housekeeping: recording flush,
// authorization recheck, idle timeout and maximum duration.
func (engine *sessionEngine) tickChecks(ctx context.Context) (string, string, bool) {
	gateway := engine.gateway
	now := gateway.now()
	if engine.recording != nil {
		chunks, flushErr := engine.recording.Recorder.FlushDue(ctx)
		if flushErr == nil {
			flushErr = gateway.Service.PersistChunks(ctx, *engine.recording, chunks)
		}
		if flushErr != nil {
			if engine.target.Session.RecordingMode != "optional" {
				return "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE", true
			}
			gateway.finishRecording(context.Background(), engine.recording, "failed")
			engine.recording = nil
		}
	}
	if now.Sub(engine.lastAuthCheck) >= 30*time.Second {
		if gateway.Service.CheckActive(ctx, engine.sessionID, engine.target.Session.SessionFence, engine.target.Session.AuthorizationVersion) != nil {
			return "invalidated", "authorization_revoked", true
		}
		engine.lastAuthCheck = now
	}
	if idleTimeoutReached(now, engine.lastActivity, engine.target.IdleTimeout) {
		return "expired", "idle_timeout", true
	}
	if now.Sub(engine.started) >= engine.target.MaxDuration || !now.Before(engine.target.LeaseExpiresAt) {
		return "expired", "maximum_duration", true
	}
	return "", "", false
}

// finalize converges the session exactly once: backend close, recording
// completion and durable status transition.
func (engine *sessionEngine) finalize(status, reason string) {
	if engine.finished {
		return
	}
	engine.finished = true
	if engine.unregisterCompletion != nil {
		engine.unregisterCompletion()
	}
	if err := engine.backend.Close(context.Background(), reason); err != nil {
		engine.gateway.logFinalizationFailure("close_backend", err)
	}
	recordingStatus := "available"
	if status == "connection_lost" || status == "invalidated" {
		recordingStatus = "incomplete"
	} else if status == "failed" && reason == "REMOTE_ACCESS_RECORDING_UNAVAILABLE" {
		recordingStatus = "failed"
	}
	engine.gateway.finishRecording(context.Background(), engine.recording, recordingStatus)
	if err := engine.gateway.Service.FinishFenceTolerant(context.Background(), engine.sessionID, engine.finishFence, status, reason); err != nil {
		engine.gateway.logFinalizationFailure("finish_session", err)
	}
	engine.sessionCancel()
}

func (gateway WebSocketGateway) logFailure(stage string, err error) {
	if gateway.Logger != nil {
		gateway.Logger.Warn("Remote access WebSocket setup failed", "stage", stage, "error", err)
	}
}

func (gateway WebSocketGateway) logFinalizationFailure(stage string, err error) {
	if gateway.Logger != nil {
		gateway.Logger.Error("Remote access WebSocket finalization failed", "stage", stage, "error", err)
	}
}

// auditClientFrame records an input/resize event; sequence is the engine-scoped
// audit counter, monotonically increasing across re-attach boundaries.
func (gateway WebSocketGateway) auditClientFrame(ctx context.Context, sessionID uuid.UUID, mode string, sequence uint64, frame ClientFrame) error {
	if mode == "disabled" || (frame.Type != "input" && frame.Type != "resize") {
		return nil
	}
	var eventType string
	var commandHash []byte
	if frame.Type == "input" {
		eventType = "input"
		digest := sha256.Sum256([]byte(frame.Data))
		commandHash = digest[:]
	} else {
		eventType = "resize"
	}
	if err := gateway.Service.RecordCommandEvent(ctx, sessionID, sequence, eventType, commandHash); err != nil {
		if mode != "optional" {
			return err
		}
		return gateway.Service.RecordCommandAuditDegradation(ctx, sessionID)
	}
	return nil
}

func (gateway WebSocketGateway) recordClientFrame(ctx context.Context, recording *GatewayRecording, frame ClientFrame) error {
	if recording == nil {
		return nil
	}
	var event RecordingEvent
	switch frame.Type {
	case "input":
		event = RecordingEvent{Time: gateway.now().Sub(recording.Started).Seconds(), Type: "i", Data: frame.Data}
	case "resize":
		event = RecordingEvent{Time: gateway.now().Sub(recording.Started).Seconds(), Type: "r", Data: fmt.Sprintf("%dx%d", frame.Cols, frame.Rows)}
	default:
		return nil
	}
	chunks, err := recording.Recorder.Append(ctx, event)
	if err != nil {
		return err
	}
	return gateway.Service.PersistChunks(ctx, *recording, chunks)
}

func (gateway WebSocketGateway) recordBackendFrame(ctx context.Context, recording *GatewayRecording, frame BackendFrame) error {
	if recording == nil || (frame.Type != "output" && frame.Type != "state") {
		return nil
	}
	eventType, data := "o", any(string(frame.Data))
	if frame.Type == "state" {
		eventType, data = "m", map[string]string{"status": frame.Status, "reason": frame.Reason}
	}
	chunks, err := recording.Recorder.Append(ctx, RecordingEvent{Time: gateway.now().Sub(recording.Started).Seconds(), Type: eventType, Data: data})
	if err != nil {
		return err
	}
	return gateway.Service.PersistChunks(ctx, *recording, chunks)
}

func (gateway WebSocketGateway) finishRecording(ctx context.Context, recording *GatewayRecording, status string) {
	if recording == nil {
		return
	}
	if err := gateway.Service.FinishRecording(ctx, *recording, status); err != nil {
		gateway.logFinalizationFailure("finish_recording", err)
	}
}

type websocketWriter struct {
	connection *websocket.Conn
	mu         sync.Mutex
	sequence   uint64
}

func (writer *websocketWriter) write(ctx context.Context, frame serverFrame) error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.sequence++
	frame.Protocol, frame.Sequence = WebSocketProtocol, writer.sequence
	value, err := json.Marshal(frame)
	if err != nil || len(value) > MaxFrameBytes {
		return ErrProtocol
	}
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return writer.connection.Write(writeCtx, websocket.MessageText, value)
}

type receivedClientFrame struct {
	messageType websocket.MessageType
	raw         []byte
	err         error
}

func receiveWebSocketFrames(ctx context.Context, connection *websocket.Conn, output chan<- receivedClientFrame) {
	for {
		messageType, raw, err := connection.Read(ctx)
		select {
		case output <- receivedClientFrame{messageType: messageType, raw: raw, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

type receivedBackendFrame struct {
	frame BackendFrame
	err   error
}

func (gateway WebSocketGateway) originAllowed(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" {
		return false
	}
	for _, allowed := range gateway.AllowedOrigins {
		if strings.EqualFold(strings.TrimRight(allowed, "/"), parsed.Scheme+"://"+parsed.Host) {
			return true
		}
	}
	return false
}

func (gateway WebSocketGateway) originPatterns() []string {
	patterns := make([]string, 0, len(gateway.AllowedOrigins))
	for _, raw := range gateway.AllowedOrigins {
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			patterns = append(patterns, parsed.Host)
		}
	}
	return patterns
}

func (gateway WebSocketGateway) now() time.Time {
	if gateway.Now != nil {
		return gateway.Now().UTC()
	}
	return time.Now().UTC()
}

func sessionIDFromPath(path string) (uuid.UUID, error) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != "sessions" {
		return uuid.Nil, ErrProtocol
	}
	return uuid.Parse(parts[2])
}

func closeProtocol(connection *websocket.Conn, code string) {
	_ = connection.Close(websocket.StatusPolicyViolation, code)
}

func gatewayErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrTicketConsumed):
		return "REMOTE_ACCESS_TICKET_CONSUMED"
	case errors.Is(err, ErrTicketBinding):
		return "REMOTE_ACCESS_SCOPE_DENIED"
	case errors.Is(err, ErrSessionUnavailable):
		return "REMOTE_ACCESS_CONNECTION_LOST"
	default:
		return "REMOTE_ACCESS_TICKET_EXPIRED"
	}
}

func terminalState(status string) bool {
	return status == "terminated" || status == "failed" || status == "connection_lost" || status == "invalidated" || status == "expired"
}

func normalizeTerminalStatus(status string) string {
	if terminalState(status) {
		return status
	}
	return "failed"
}
