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

type WebSocketGateway struct {
	Service        GatewayService
	Backends       BackendFactory
	ObjectStore    ObjectStore
	AllowedOrigins []string
	RejectNew      <-chan struct{}
	Drain          <-chan struct{}
	Sessions       *SessionTracker
	Terminations   *TerminationHub
	Logger         *slog.Logger
	Now            func() time.Time
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
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	state := ProtocolState{StartedAt: gateway.now()}
	helloCtx, helloCancel := context.WithTimeout(ctx, HelloTimeout)
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
	target, err := gateway.Service.AuthorizeConnection(ctx, sessionID, hello.Ticket)
	hello.Ticket = ""
	if err != nil {
		gateway.logFailure("authorize", err)
		closeProtocol(connection, gatewayErrorCode(err))
		return
	}
	terminationEvents, unregisterTermination := gateway.Terminations.Register(sessionID, target.Session.SessionFence)
	defer unregisterTermination()
	recording, err := gateway.Service.OpenRecording(ctx, sessionID, gateway.ObjectStore)
	if err != nil {
		gateway.logFailure("open_recording", err)
		_ = gateway.Service.Finish(ctx, sessionID, target.Session.SessionFence, "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE")
		closeProtocol(connection, "REMOTE_ACCESS_RECORDING_UNAVAILABLE")
		return
	}
	backend, err := gateway.Backends.Open(ctx, target, hello.Cols, hello.Rows)
	if err != nil {
		gateway.logFailure("open_backend", err)
		_ = gateway.Service.FinishRecording(ctx, recording, "failed")
		_ = gateway.Service.Finish(ctx, sessionID, target.Session.SessionFence, "failed", "REMOTE_ACCESS_CONNECTION_LOST")
		closeProtocol(connection, "REMOTE_ACCESS_CONNECTION_LOST")
		return
	}
	defer backend.Close(context.Background(), "gateway_closed")
	if err := gateway.Service.MarkActive(ctx, sessionID, target.Session.SessionFence); err != nil {
		_ = gateway.Service.FinishRecording(ctx, recording, "incomplete")
		closeProtocol(connection, "REMOTE_ACCESS_SESSION_INVALIDATED")
		return
	}

	writer := &websocketWriter{connection: connection}
	mode := "ssh_pty"
	if target.Protocol == "winrs" {
		mode = "winrs_line"
	}
	if err := writer.write(ctx, serverFrame{Type: "server_ready", SessionID: sessionID.String(), Mode: mode, Nonce: hello.Nonce,
		IdleTimeout: int64(target.IdleTimeout / time.Second), MaxDuration: int64(target.MaxDuration / time.Second)}); err != nil {
		_ = gateway.Service.FinishRecording(context.Background(), recording, "incomplete")
		_ = gateway.Service.Finish(context.Background(), sessionID, target.Session.SessionFence, "connection_lost", "CLIENT_DISCONNECTED")
		return
	}

	started, lastActivity, lastAuthorizationCheck := gateway.now(), gateway.now(), gateway.now()
	clientFrames := make(chan receivedClientFrame, 1)
	backendFrames := make(chan receivedBackendFrame, 1)
	go receiveWebSocketFrames(ctx, connection, clientFrames)
	go receiveBackendFrames(ctx, backend, backendFrames)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	finishStatus, finishReason := "terminated", "completed"

	for {
		select {
		case termination, ok := <-terminationEvents:
			if ok {
				finishStatus, finishReason = "invalidated", termination.Reason
				_ = writer.write(ctx, serverFrame{Type: "state", Status: "terminating", Reason: termination.Reason})
				goto finished
			}
		case <-gateway.Drain:
			finishStatus, finishReason = "terminated", "gateway_drain"
			_ = writer.write(ctx, serverFrame{Type: "state", Status: "terminating", Reason: finishReason})
			goto finished
		case <-ctx.Done():
			finishStatus, finishReason = "connection_lost", "client_disconnected"
			goto finished
		case <-ticker.C:
			now := gateway.now()
			if chunks, flushErr := recording.Recorder.FlushDue(ctx); flushErr != nil {
				finishStatus, finishReason = "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE"
				goto finished
			} else if persistErr := gateway.Service.PersistChunks(ctx, recording, chunks); persistErr != nil {
				finishStatus, finishReason = "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE"
				goto finished
			}
			if now.Sub(lastAuthorizationCheck) >= 30*time.Second {
				if gateway.Service.CheckActive(ctx, sessionID, target.Session.SessionFence, target.Session.AuthorizationVersion) != nil {
					finishStatus, finishReason = "invalidated", "authorization_revoked"
					goto finished
				}
				lastAuthorizationCheck = now
			}
			if now.Sub(lastActivity) >= target.IdleTimeout {
				finishStatus, finishReason = "expired", "idle_timeout"
				goto finished
			}
			if now.Sub(started) >= target.MaxDuration || !now.Before(target.LeaseExpiresAt) {
				finishStatus, finishReason = "expired", "maximum_duration"
				goto finished
			}
		case item := <-clientFrames:
			if item.err != nil {
				finishStatus, finishReason = "connection_lost", "client_disconnected"
				goto finished
			}
			frame, err := state.Accept(item.raw, gateway.now())
			if err != nil || item.messageType != websocket.MessageText {
				finishStatus, finishReason = "failed", "REMOTE_ACCESS_PROTOCOL_ERROR"
				_ = writer.write(ctx, serverFrame{Type: "error", Code: "REMOTE_ACCESS_PROTOCOL_ERROR", Message: "protocol error", Terminal: true})
				goto finished
			}
			lastActivity = gateway.now()
			if frame.Type == "ping" {
				continue
			}
			if frame.Type == "close" {
				finishStatus, finishReason = "terminated", "client_close"
				goto finished
			}
			if err := gateway.recordClientFrame(ctx, recording, sessionID, frame); err != nil {
				finishStatus, finishReason = "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE"
				goto finished
			}
			if err := backend.Send(ctx, frame); err != nil {
				finishStatus, finishReason = "connection_lost", "backend_send_failed"
				goto finished
			}
		case item := <-backendFrames:
			if item.err != nil {
				if errors.Is(item.err, io.EOF) {
					finishStatus, finishReason = "terminated", "remote_closed"
				} else {
					finishStatus, finishReason = "connection_lost", "backend_disconnected"
				}
				goto finished
			}
			lastActivity = gateway.now()
			if err := gateway.recordBackendFrame(ctx, recording, sessionID, item.frame); err != nil {
				finishStatus, finishReason = "failed", "REMOTE_ACCESS_RECORDING_UNAVAILABLE"
				goto finished
			}
			if item.frame.Type == "output" {
				if err := writer.write(ctx, serverFrame{Type: "output", Stream: item.frame.Stream, Data: string(item.frame.Data)}); err != nil {
					finishStatus, finishReason = "connection_lost", "client_disconnected"
					goto finished
				}
			} else if item.frame.Type == "state" {
				if err := writer.write(ctx, serverFrame{Type: "state", Status: item.frame.Status, Reason: item.frame.Reason}); err != nil {
					finishStatus, finishReason = "connection_lost", "client_disconnected"
					goto finished
				}
				if terminalState(item.frame.Status) {
					finishStatus, finishReason = normalizeTerminalStatus(item.frame.Status), item.frame.Reason
					goto finished
				}
			}
		}
	}

finished:
	if err := backend.Close(context.Background(), finishReason); err != nil {
		gateway.logFinalizationFailure("close_backend", err)
	}
	recordingStatus := "available"
	if finishStatus == "connection_lost" || finishStatus == "invalidated" {
		recordingStatus = "incomplete"
	} else if finishStatus == "failed" && finishReason == "REMOTE_ACCESS_RECORDING_UNAVAILABLE" {
		recordingStatus = "failed"
	}
	if err := gateway.Service.FinishRecording(context.Background(), recording, recordingStatus); err != nil {
		gateway.logFinalizationFailure("finish_recording", err)
	}
	if err := gateway.Service.Finish(context.Background(), sessionID, target.Session.SessionFence, finishStatus, finishReason); err != nil {
		gateway.logFinalizationFailure("finish_session", err)
	}
	_ = writer.write(context.Background(), serverFrame{Type: "state", Status: finishStatus, Reason: finishReason})
	_ = connection.Close(websocket.StatusNormalClosure, finishReason)
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

func (gateway WebSocketGateway) recordClientFrame(ctx context.Context, recording GatewayRecording, sessionID uuid.UUID, frame ClientFrame) error {
	var event RecordingEvent
	switch frame.Type {
	case "input":
		event = RecordingEvent{Time: gateway.now().Sub(recording.Started).Seconds(), Type: "i", Data: frame.Data}
		digest := sha256.Sum256([]byte(frame.Data))
		if err := gateway.Service.RecordCommandEvent(ctx, sessionID, frame.Sequence, "input", digest[:]); err != nil {
			return err
		}
	case "resize":
		event = RecordingEvent{Time: gateway.now().Sub(recording.Started).Seconds(), Type: "r", Data: fmt.Sprintf("%dx%d", frame.Cols, frame.Rows)}
		if err := gateway.Service.RecordCommandEvent(ctx, sessionID, frame.Sequence, "resize", nil); err != nil {
			return err
		}
	default:
		return nil
	}
	chunks, err := recording.Recorder.Append(ctx, event)
	if err != nil {
		return err
	}
	return gateway.Service.PersistChunks(ctx, recording, chunks)
}

func (gateway WebSocketGateway) recordBackendFrame(ctx context.Context, recording GatewayRecording, sessionID uuid.UUID, frame BackendFrame) error {
	if frame.Type != "output" && frame.Type != "state" {
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
	return gateway.Service.PersistChunks(ctx, recording, chunks)
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
