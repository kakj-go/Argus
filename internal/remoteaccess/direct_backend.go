package remoteaccess

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	directv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/directexecutor/v1"
	"google.golang.org/grpc/status"
)

const defaultDirectHandshakeTimeout = 15 * time.Second

type DirectStreamOpener interface {
	OpenRemoteAccess(context.Context) (directv1.DirectExecutorService_OpenRemoteAccessClient, error)
}

type DirectBackendFactory struct {
	Opener           DirectStreamOpener
	HandshakeTimeout time.Duration
	Logger           *slog.Logger
}

func (factory DirectBackendFactory) Open(ctx context.Context, target ConnectionTarget, cols, rows int) (BackendSession, error) {
	if factory.Opener == nil || (target.ConnectionMode != "direct_ssh" && target.ConnectionMode != "direct_winrm") {
		return nil, ErrSessionUnavailable
	}
	streamContext, cancel := context.WithCancel(ctx)
	stream, err := factory.Opener.OpenRemoteAccess(streamContext)
	if err != nil {
		cancel()
		factory.logHandshakeFailure("open", err)
		return nil, err
	}
	open := &directv1.RemoteAccessOpen{SessionId: target.Session.ID.String(), SessionFence: uint64(target.Session.SessionFence), HostId: target.HostID.String(),
		ManagedAccountId: target.ManagedAccountID.String(), CredentialLeaseId: target.CredentialLeaseID.String(), Protocol: target.Protocol,
		TargetHost: target.Address, TargetPort: uint32(target.Port), HostKeyFingerprint: target.PinnedHostKey, TerminalCols: uint32(cols), TerminalRows: uint32(rows),
		IdleTimeoutSeconds: uint32(target.IdleTimeout.Seconds()), MaxDurationSeconds: uint32(target.MaxDuration.Seconds())}
	if err := stream.Send(&directv1.OpenRemoteAccessRequest{Sequence: 1, Frame: &directv1.OpenRemoteAccessRequest_Open{Open: open}}); err != nil {
		cancel()
		_ = stream.CloseSend()
		factory.logHandshakeFailure("send", err)
		return nil, err
	}
	timeout := factory.HandshakeTimeout
	if timeout <= 0 {
		timeout = defaultDirectHandshakeTimeout
	}
	type receiveResult struct {
		response *directv1.OpenRemoteAccessResponse
		err      error
	}
	received := make(chan receiveResult, 1)
	go func() {
		response, receiveErr := stream.Recv()
		received <- receiveResult{response: response, err: receiveErr}
	}()
	var first *directv1.OpenRemoteAccessResponse
	select {
	case result := <-received:
		first, err = result.response, result.err
	case <-time.After(timeout):
		err = context.DeadlineExceeded
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err != nil || first.GetSequence() != 1 || first.GetReady() == nil {
		cancel()
		_ = stream.CloseSend()
		factory.logHandshakeFailure("receive_ready", err)
		return nil, ErrSessionUnavailable
	}
	return &directBackendSession{stream: stream, cancel: cancel, clientSequence: 1, serverSequence: 1}, nil
}

func (factory DirectBackendFactory) logHandshakeFailure(stage string, err error) {
	if factory.Logger == nil {
		return
	}
	if err == nil {
		factory.Logger.Warn("Direct Executor remote access handshake returned an invalid frame", "stage", stage)
		return
	}
	grpcStatus := status.Convert(err)
	factory.Logger.Warn("Direct Executor remote access handshake failed", "stage", stage,
		"code", grpcStatus.Code().String(), "error", grpcStatus.Message())
}

type directBackendSession struct {
	stream         directv1.DirectExecutorService_OpenRemoteAccessClient
	cancel         context.CancelFunc
	mu             sync.Mutex
	clientSequence uint64
	serverSequence uint64
	closed         bool
}

func (session *directBackendSession) Send(ctx context.Context, frame ClientFrame) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return io.EOF
	}
	session.clientSequence++
	request := &directv1.OpenRemoteAccessRequest{Sequence: session.clientSequence}
	switch frame.Type {
	case "input":
		request.Frame = &directv1.OpenRemoteAccessRequest_Input{Input: &directv1.RemoteAccessInput{Data: []byte(frame.Data)}}
	case "resize":
		request.Frame = &directv1.OpenRemoteAccessRequest_Resize{Resize: &directv1.RemoteAccessResize{Cols: uint32(frame.Cols), Rows: uint32(frame.Rows)}}
	case "close":
		request.Frame = &directv1.OpenRemoteAccessRequest_Close{Close: &directv1.RemoteAccessClose{Reason: frame.Reason}}
	default:
		return ErrProtocol
	}
	return session.stream.Send(request)
}

func (session *directBackendSession) Receive(_ context.Context) (BackendFrame, error) {
	response, err := session.stream.Recv()
	if err != nil {
		return BackendFrame{}, err
	}
	if response.GetSequence() != session.serverSequence+1 || response.GetReady() != nil {
		return BackendFrame{}, ErrProtocol
	}
	session.serverSequence = response.GetSequence()
	if output := response.GetOutput(); output != nil {
		stream := output.GetStream()
		if stream != "stdout" && stream != "stderr" {
			return BackendFrame{}, ErrProtocol
		}
		return BackendFrame{Type: "output", Stream: stream, Data: append([]byte(nil), output.GetData()...)}, nil
	}
	if state := response.GetState(); state != nil {
		return BackendFrame{Type: "state", Status: state.GetStatus(), Reason: state.GetReason()}, nil
	}
	return BackendFrame{}, ErrProtocol
}

func (session *directBackendSession) Close(ctx context.Context, reason string) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil
	}
	session.closed = true
	session.clientSequence++
	err := session.stream.Send(&directv1.OpenRemoteAccessRequest{Sequence: session.clientSequence,
		Frame: &directv1.OpenRemoteAccessRequest_Close{Close: &directv1.RemoteAccessClose{Reason: reason}}})
	closeErr := session.stream.CloseSend()
	session.cancel()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return closeErr
}

type RoutedBackendFactory struct {
	Direct    BackendFactory
	Connector BackendFactory
}

func (factory RoutedBackendFactory) Open(ctx context.Context, target ConnectionTarget, cols, rows int) (BackendSession, error) {
	if target.ConnectionMode == "direct_ssh" || target.ConnectionMode == "direct_winrm" {
		if factory.Direct == nil {
			return nil, ErrSessionUnavailable
		}
		return factory.Direct.Open(ctx, target, cols, rows)
	}
	if factory.Connector == nil {
		return nil, ErrSessionUnavailable
	}
	return factory.Connector.Open(ctx, target, cols, rows)
}
