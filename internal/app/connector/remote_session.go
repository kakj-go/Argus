package connector

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"sync"
	"time"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/remoteaccess/winrsline"
	"golang.org/x/crypto/ssh"
)

type localRemoteSession struct {
	open           *connectorv1.RemoteAccessOpen
	nonce          []byte
	inbound        chan *connectorv1.ConnectResponse
	cancel         context.CancelFunc
	mu             sync.Mutex
	serverSequence uint64
	clientSequence uint64
	started        bool
}

func newLocalRemoteSession(open *connectorv1.RemoteAccessOpen) (*localRemoteSession, error) {
	if open.GetMaxFrameBytes() == 0 || open.GetMaxFrameBytes() > 64<<10 || open.GetSessionFence() == 0 || open.GetTargetPort() == 0 ||
		(open.GetProtocol() != "ssh" && open.GetProtocol() != "winrs") || open.GetTerminalCols() < 20 || open.GetTerminalRows() < 5 {
		return nil, errors.New("invalid remote access session")
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return &localRemoteSession{open: open, nonce: nonce, inbound: make(chan *connectorv1.ConnectResponse, 32)}, nil
}

func (session *localRemoteSession) leaseRequest(epoch uint64) *connectorv1.ConnectRequest {
	return &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CredentialLeaseRequest{CredentialLeaseRequest: &connectorv1.CredentialLeaseRequest{
		LeaseId: session.open.GetCredentialLeaseId(), CommandId: session.open.GetSessionId(), ConnectionEpoch: epoch, RecipientNonce: session.nonce}}}
}

func (session *localRemoteSession) acceptGrant(grant *connectorv1.CredentialLeaseGrant, epoch uint64) bool {
	return grant.GetCommandId() == session.open.GetSessionId() && grant.GetLeaseId() == session.open.GetCredentialLeaseId() && grant.GetConnectionEpoch() == epoch &&
		bytes.Equal(grant.GetRecipientNonce(), session.nonce) && grant.GetExpiresAt() != nil && time.Now().Before(grant.GetExpiresAt().AsTime()) && len(grant.GetCredentialPayload()) > 0
}

func (session *localRemoteSession) deliver(frame *connectorv1.ConnectResponse) bool {
	session.mu.Lock()
	defer session.mu.Unlock()
	sequence := remoteServerStreamSequence(frame)
	if sequence != session.serverSequence+1 {
		return false
	}
	session.serverSequence = sequence
	select {
	case session.inbound <- frame:
		return true
	default:
		return false
	}
}

func (session *localRemoteSession) stop(_ string) {
	session.mu.Lock()
	if session.cancel != nil {
		session.cancel()
	}
	session.mu.Unlock()
}

func (session *localRemoteSession) run(parent context.Context, credential []byte, outgoing chan<- outboundFrame, done chan<- string) {
	defer clear(credential)
	defer func() { done <- session.open.GetStreamId() }()
	duration := time.Hour
	if session.open.GetMaxDuration() != nil && session.open.GetMaxDuration().AsDuration() <= time.Hour {
		duration = session.open.GetMaxDuration().AsDuration()
	}
	ctx, cancel := context.WithTimeout(parent, duration)
	session.mu.Lock()
	session.cancel, session.started = cancel, true
	session.mu.Unlock()
	defer cancel()
	var err error
	if session.open.GetProtocol() == "ssh" {
		err = session.runSSH(ctx, credential, outgoing)
	} else {
		err = session.runWinRS(ctx, credential, outgoing)
	}
	status, reason := connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_TERMINATED, "remote_closed"
	if err != nil {
		status, reason = connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_CONNECTION_LOST, "REMOTE_ACCESS_CONNECTION_LOST"
	}
	outgoing <- outboundFrame{value: session.stateFrame(status, reason)}
}

func (session *localRemoteSession) runSSH(ctx context.Context, credential []byte, outgoing chan<- outboundFrame) error {
	auth, err := connectorSSHAuth(credential)
	if err != nil {
		return err
	}
	configuration := &ssh.ClientConfig{User: session.open.GetUsername(), Auth: []ssh.AuthMethod{auth}, Timeout: 15 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if session.open.GetHostKeyFingerprint() == "" || ssh.FingerprintSHA256(key) != session.open.GetHostKeyFingerprint() {
				return errors.New("Host key changed")
			}
			return nil
		}}
	if configuration.User == "" {
		return errors.New("managed account username is required")
	}
	client, err := ssh.Dial("tcp", net.JoinHostPort(session.open.GetTargetHost(), fmt.Sprint(session.open.GetTargetPort())), configuration)
	if err != nil {
		return err
	}
	defer client.Close()
	remote, err := client.NewSession()
	if err != nil {
		return err
	}
	defer remote.Close()
	stdin, _ := remote.StdinPipe()
	stdout, _ := remote.StdoutPipe()
	stderr, _ := remote.StderrPipe()
	if err := remote.RequestPty("xterm-256color", int(session.open.GetTerminalRows()), int(session.open.GetTerminalCols()), ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return err
	}
	if err := remote.Shell(); err != nil {
		return err
	}
	outgoing <- outboundFrame{value: session.stateFrame(connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_ACTIVE, "")}
	outputs := make(chan connectorOutput, 8)
	go readConnectorOutput(stdout, connectorv1.RemoteAccessOutputStream_REMOTE_ACCESS_OUTPUT_STREAM_STDOUT, outputs)
	go readConnectorOutput(stderr, connectorv1.RemoteAccessOutputStream_REMOTE_ACCESS_OUTPUT_STREAM_STDERR, outputs)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case output := <-outputs:
			if output.err != nil {
				return output.err
			}
			outgoing <- outboundFrame{value: session.outputFrame(output.stream, output.data)}
		case frame := <-session.inbound:
			switch {
			case frame.GetRemoteAccessInput() != nil:
				_, err = stdin.Write(frame.GetRemoteAccessInput().GetData())
			case frame.GetRemoteAccessResize() != nil:
				resize := frame.GetRemoteAccessResize()
				err = remote.WindowChange(int(resize.GetRows()), int(resize.GetCols()))
			case frame.GetRemoteAccessClose() != nil:
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
}

func (session *localRemoteSession) runWinRS(ctx context.Context, credential []byte, outgoing chan<- outboundFrame) error {
	if session.open.GetTargetPort() != 5986 && session.open.GetTargetPort() != 443 {
		return errors.New("WINRM_TLS_REQUIRED")
	}
	address, err := netip.ParseAddr(session.open.GetTargetHost())
	if err == nil && !address.IsLoopback() && !address.IsPrivate() {
		return errors.New("Connector WinRS target is outside the Connector network")
	}
	if session.open.GetUsername() == "" {
		return errors.New("managed account username is required")
	}
	client, err := winrsline.Open(winrsline.Options{
		Host: session.open.GetTargetHost(), Port: int(session.open.GetTargetPort()),
		Username: session.open.GetUsername(), Password: string(credential), Timeout: 30 * time.Second,
	})
	if err != nil {
		return err
	}
	defer client.Close()
	outgoing <- outboundFrame{value: session.stateFrame(connectorv1.RemoteAccessStateValue_REMOTE_ACCESS_STATE_VALUE_ACTIVE, "")}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-session.inbound:
			if frame.GetRemoteAccessClose() != nil {
				return nil
			}
			if frame.GetRemoteAccessResize() != nil {
				continue
			}
			input := frame.GetRemoteAccessInput()
			if input == nil || len(input.GetData()) == 0 {
				return errors.New("invalid WinRS line")
			}
			result, err := client.ExecuteLine(ctx, string(input.GetData()))
			if err != nil {
				return err
			}
			if len(result.Stdout) > 0 {
				outgoing <- outboundFrame{value: session.outputFrame(connectorv1.RemoteAccessOutputStream_REMOTE_ACCESS_OUTPUT_STREAM_STDOUT, result.Stdout)}
			}
			if len(result.Stderr) > 0 {
				outgoing <- outboundFrame{value: session.outputFrame(connectorv1.RemoteAccessOutputStream_REMOTE_ACCESS_OUTPUT_STREAM_STDERR, result.Stderr)}
			}
		}
	}
}

type connectorOutput struct {
	stream connectorv1.RemoteAccessOutputStream
	data   []byte
	err    error
}

func readConnectorOutput(reader io.Reader, stream connectorv1.RemoteAccessOutputStream, output chan<- connectorOutput) {
	buffer := make([]byte, 32<<10)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			output <- connectorOutput{stream: stream, data: append([]byte(nil), buffer[:count]...)}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				output <- connectorOutput{err: err}
			}
			return
		}
	}
}

func (session *localRemoteSession) outputFrame(stream connectorv1.RemoteAccessOutputStream, data []byte) *connectorv1.ConnectRequest {
	session.clientSequence++
	return &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_RemoteAccessOutput{RemoteAccessOutput: &connectorv1.RemoteAccessOutput{
		StreamId: session.open.GetStreamId(), StreamSequence: session.clientSequence, Stream: stream, Data: data}}}
}

func (session *localRemoteSession) stateFrame(state connectorv1.RemoteAccessStateValue, reason string) *connectorv1.ConnectRequest {
	session.clientSequence++
	return &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_RemoteAccessState{RemoteAccessState: &connectorv1.RemoteAccessState{
		StreamId: session.open.GetStreamId(), StreamSequence: session.clientSequence, State: state, Reason: reason}}}
}

func remoteServerStreamID(frame *connectorv1.ConnectResponse) string {
	if value := frame.GetRemoteAccessInput(); value != nil {
		return value.GetStreamId()
	}
	if value := frame.GetRemoteAccessResize(); value != nil {
		return value.GetStreamId()
	}
	if value := frame.GetRemoteAccessClose(); value != nil {
		return value.GetStreamId()
	}
	return ""
}

func remoteServerStreamSequence(frame *connectorv1.ConnectResponse) uint64 {
	if value := frame.GetRemoteAccessInput(); value != nil {
		return value.GetStreamSequence()
	}
	if value := frame.GetRemoteAccessResize(); value != nil {
		return value.GetStreamSequence()
	}
	if value := frame.GetRemoteAccessClose(); value != nil {
		return value.GetStreamSequence()
	}
	return 0
}
