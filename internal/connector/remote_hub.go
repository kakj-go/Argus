package connector

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/common/v1"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/remoteaccess"
)

type RemoteAccessHub struct {
	mu          sync.Mutex
	connections map[uuid.UUID]remoteConnectorConnection
	streams     map[string]*connectorRemoteSession
}

type remoteConnectorConnection struct {
	epoch    int64
	outbound chan<- *connectorv1.ConnectResponse
}

type connectorRemoteSession struct {
	hub              *RemoteAccessHub
	connectorID      uuid.UUID
	epoch            int64
	streamID         string
	events           chan remoteaccess.BackendFrame
	outboundSequence uint64
	inboundSequence  uint64
	closed           bool
}

func NewRemoteAccessHub() *RemoteAccessHub {
	return &RemoteAccessHub{connections: map[uuid.UUID]remoteConnectorConnection{}, streams: map[string]*connectorRemoteSession{}}
}

func (hub *RemoteAccessHub) Register(connectorID uuid.UUID, epoch int64, outbound chan<- *connectorv1.ConnectResponse) func() {
	hub.mu.Lock()
	hub.connections[connectorID] = remoteConnectorConnection{epoch: epoch, outbound: outbound}
	hub.mu.Unlock()
	return func() {
		hub.mu.Lock()
		current, ok := hub.connections[connectorID]
		if ok && current.epoch == epoch {
			delete(hub.connections, connectorID)
		}
		for id, session := range hub.streams {
			if session.connectorID == connectorID && session.epoch == epoch {
				session.closed = true
				select {
				case session.events <- remoteaccess.BackendFrame{Type: "state", Status: "connection_lost", Reason: "connector_disconnected"}:
				default:
				}
				delete(hub.streams, id)
			}
		}
		hub.mu.Unlock()
	}
}

func (hub *RemoteAccessHub) Open(ctx context.Context, target remoteaccess.ConnectionTarget, cols, rows int) (remoteaccess.BackendSession, error) {
	if !target.ConnectorID.Valid || target.ConnectionEpoch < 1 {
		return nil, remoteaccess.ErrSessionUnavailable
	}
	streamID := uuid.NewString()
	hub.mu.Lock()
	connection, ok := hub.connections[target.ConnectorID.UUID]
	if !ok || connection.epoch != target.ConnectionEpoch {
		hub.mu.Unlock()
		return nil, remoteaccess.ErrSessionUnavailable
	}
	session := &connectorRemoteSession{hub: hub, connectorID: target.ConnectorID.UUID, epoch: target.ConnectionEpoch, streamID: streamID,
		events: make(chan remoteaccess.BackendFrame, 16)}
	hub.streams[streamID] = session
	hub.mu.Unlock()
	open := &connectorv1.RemoteAccessOpen{StreamId: streamID, SessionId: target.Session.ID.String(), ManagedAccountId: target.ManagedAccountID.String(),
		Protocol: target.Protocol, ExpiresAt: timestamppb.New(target.LeaseExpiresAt), MaxFrameBytes: remoteaccess.MaxFrameBytes,
		ConnectionEpoch: uint64(target.ConnectionEpoch), SessionFence: uint64(target.Session.SessionFence), TargetHost: target.Address, TargetPort: uint32(target.Port),
		HostKeyFingerprint: target.PinnedHostKey, CredentialLeaseId: target.CredentialLeaseID.String(), TerminalCols: uint32(cols), TerminalRows: uint32(rows),
		IdleTimeout: durationpb.New(target.IdleTimeout), MaxDuration: durationpb.New(target.MaxDuration)}
	open.Username = target.Username
	if err := hub.enqueue(ctx, connection, &connectorv1.ConnectResponse{Frame: &connectorv1.ConnectResponse_RemoteAccessOpen{RemoteAccessOpen: open}}); err != nil {
		hub.remove(streamID)
		return nil, err
	}
	return session, nil
}

func (hub *RemoteAccessHub) Deliver(connectorID uuid.UUID, epoch int64, request *connectorv1.ConnectRequest) error {
	streamID, sequence, frame, err := remoteConnectorFrame(request)
	if err != nil {
		return err
	}
	hub.mu.Lock()
	session, ok := hub.streams[streamID]
	if !ok || session.connectorID != connectorID || session.epoch != epoch || session.closed || sequence != session.inboundSequence+1 {
		hub.mu.Unlock()
		return ErrCommandState
	}
	session.inboundSequence = sequence
	if frame.Type == "state" && remoteTerminalState(frame.Status) {
		session.closed = true
		delete(hub.streams, streamID)
	}
	hub.mu.Unlock()
	select {
	case session.events <- frame:
		return nil
	default:
		return errors.New("remote access stream backpressure")
	}
}

func (session *connectorRemoteSession) Send(ctx context.Context, frame remoteaccess.ClientFrame) error {
	session.hub.mu.Lock()
	if session.closed {
		session.hub.mu.Unlock()
		return io.EOF
	}
	connection, ok := session.hub.connections[session.connectorID]
	if !ok || connection.epoch != session.epoch {
		session.hub.mu.Unlock()
		return io.EOF
	}
	session.outboundSequence++
	sequence := session.outboundSequence
	session.hub.mu.Unlock()
	response := &connectorv1.ConnectResponse{}
	switch frame.Type {
	case "input":
		response.Frame = &connectorv1.ConnectResponse_RemoteAccessInput{RemoteAccessInput: &connectorv1.RemoteAccessInput{StreamId: session.streamID, StreamSequence: sequence, Data: []byte(frame.Data)}}
	case "resize":
		response.Frame = &connectorv1.ConnectResponse_RemoteAccessResize{RemoteAccessResize: &connectorv1.RemoteAccessResize{StreamId: session.streamID, StreamSequence: sequence, Cols: uint32(frame.Cols), Rows: uint32(frame.Rows)}}
	case "close":
		response.Frame = &connectorv1.ConnectResponse_RemoteAccessClose{RemoteAccessClose: &connectorv1.RemoteAccessClose{StreamId: session.streamID,
			Close: &commonv1.StreamClose{Reason: commonv1.CloseReason_CLOSE_REASON_NORMAL}, StreamSequence: sequence}}
	default:
		return remoteaccess.ErrProtocol
	}
	return session.hub.enqueue(ctx, connection, response)
}

func (session *connectorRemoteSession) Receive(ctx context.Context) (remoteaccess.BackendFrame, error) {
	select {
	case frame := <-session.events:
		return frame, nil
	case <-ctx.Done():
		return remoteaccess.BackendFrame{}, ctx.Err()
	}
}

func (session *connectorRemoteSession) Close(ctx context.Context, reason string) error {
	session.hub.mu.Lock()
	if session.closed {
		session.hub.mu.Unlock()
		return nil
	}
	session.closed = true
	delete(session.hub.streams, session.streamID)
	connection, ok := session.hub.connections[session.connectorID]
	session.hub.mu.Unlock()
	if !ok {
		return nil
	}
	return session.hub.enqueue(ctx, connection, &connectorv1.ConnectResponse{Frame: &connectorv1.ConnectResponse_RemoteAccessClose{RemoteAccessClose: &connectorv1.RemoteAccessClose{
		StreamId: session.streamID, Close: &commonv1.StreamClose{Reason: commonv1.CloseReason_CLOSE_REASON_NORMAL,
			Error: &commonv1.ErrorStatus{Code: "REMOTE_ACCESS_CLOSED", MessageKey: reason}}, StreamSequence: session.outboundSequence + 1}}})
}

func (hub *RemoteAccessHub) enqueue(ctx context.Context, connection remoteConnectorConnection, frame *connectorv1.ConnectResponse) error {
	select {
	case connection.outbound <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(5 * time.Second):
		return errors.New("remote access Connector queue timeout")
	}
}

func (hub *RemoteAccessHub) remove(streamID string) {
	hub.mu.Lock()
	delete(hub.streams, streamID)
	hub.mu.Unlock()
}

func remoteConnectorFrame(request *connectorv1.ConnectRequest) (string, uint64, remoteaccess.BackendFrame, error) {
	if output := request.GetRemoteAccessOutput(); output != nil {
		stream := "stdout"
		if output.GetStream() == connectorv1.RemoteAccessOutputStream_REMOTE_ACCESS_OUTPUT_STREAM_STDERR {
			stream = "stderr"
		} else if output.GetStream() != connectorv1.RemoteAccessOutputStream_REMOTE_ACCESS_OUTPUT_STREAM_STDOUT {
			return "", 0, remoteaccess.BackendFrame{}, ErrCommandState
		}
		return output.GetStreamId(), output.GetStreamSequence(), remoteaccess.BackendFrame{Type: "output", Stream: stream, Data: append([]byte(nil), output.GetData()...)}, nil
	}
	if state := request.GetRemoteAccessState(); state != nil {
		return state.GetStreamId(), state.GetStreamSequence(), remoteaccess.BackendFrame{Type: "state", Status: remoteStateName(state.GetState()), Reason: state.GetReason()}, nil
	}
	if closeFrame := request.GetRemoteAccessClose(); closeFrame != nil {
		reason := "connector_closed"
		if closeFrame.GetClose() != nil && closeFrame.GetClose().GetError() != nil {
			reason = closeFrame.GetClose().GetError().GetCode()
		}
		return closeFrame.GetStreamId(), closeFrame.GetStreamSequence(), remoteaccess.BackendFrame{Type: "state", Status: "connection_lost", Reason: reason}, nil
	}
	return "", 0, remoteaccess.BackendFrame{}, ErrCommandState
}

func remoteStateName(value connectorv1.RemoteAccessStateValue) string {
	switch value {
	case connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_CONNECTING:
		return "connecting"
	case connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_ACTIVE:
		return "active"
	case connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_TERMINATING:
		return "terminating"
	case connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_TERMINATED:
		return "terminated"
	case connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_FAILED:
		return "failed"
	case connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_CONNECTION_LOST:
		return "connection_lost"
	case connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_INVALIDATED:
		return "invalidated"
	default:
		return "failed"
	}
}

func remoteTerminalState(status string) bool {
	return status == "terminated" || status == "failed" || status == "connection_lost" || status == "invalidated"
}
