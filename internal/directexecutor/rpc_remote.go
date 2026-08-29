package directexecutor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	directv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/directexecutor/v1"
	"github.com/kakj-go/Argus/internal/remoteaccess/winrsline"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

type sshRemoteOutput struct {
	stream string
	data   []byte
	err    error
}

func (server RPCServer) OpenRemoteAccess(stream directv1.DirectExecutorService_OpenRemoteAccessServer) error {
	if server.Logger != nil {
		server.Logger.Debug("Direct Executor accepted remote access stream")
	}
	if server.Executor == nil || server.Executor.Store == nil || server.Executor.Secrets.Store == nil {
		return status.Error(codes.Unavailable, "Direct Executor is unavailable")
	}
	first, err := stream.Recv()
	if err != nil {
		if server.Logger != nil {
			server.Logger.Warn("Direct Executor failed to receive remote access open frame", "code", status.Code(err).String())
		}
		return err
	}
	open := first.GetOpen()
	if first.GetSequence() != 1 || open == nil || open.GetSessionFence() == 0 || open.GetTerminalCols() < 20 || open.GetTerminalRows() < 5 {
		return status.Error(codes.InvalidArgument, "invalid remote access open frame")
	}
	sessionID, leaseID, err := parseRemoteIDs(open)
	if err != nil {
		return status.Error(codes.InvalidArgument, "invalid remote access identifiers")
	}
	target, err := server.Executor.Store.Queries.GetRemoteAccessSessionTarget(stream.Context(), sessionID)
	if err != nil || target.SessionFence != int64(open.GetSessionFence()) || target.Status != "connecting" ||
		(target.ConnectionMode != "direct_ssh" && target.ConnectionMode != "direct_winrm") || target.ManagedAccountID.String() != open.GetManagedAccountId() ||
		target.HostID.String() != open.GetHostId() || target.Protocol != open.GetProtocol() || target.Address != open.GetTargetHost() || target.Port != int32(open.GetTargetPort()) {
		return status.Error(codes.PermissionDenied, "remote access target mismatch")
	}
	issued, err := server.Executor.Secrets.FulfillLease(stream.Context(), target.EnterpriseID, leaseID, "direct_executor", server.Executor.InstanceID)
	if err != nil {
		if server.Logger != nil {
			server.Logger.Warn("Direct Executor rejected remote access credential lease", "protocol", target.Protocol)
		}
		return status.Error(codes.PermissionDenied, "credential lease rejected")
	}
	defer clear(issued.Value)
	addresses, err := server.Executor.Validator.Resolve(stream.Context(), target.Address)
	if err != nil {
		return status.Error(codes.PermissionDenied, "remote target denied")
	}
	ctx, cancel := context.WithTimeout(stream.Context(), boundedRemoteDuration(open.GetMaxDurationSeconds()))
	defer cancel()
	if target.Protocol == "ssh" {
		return server.openSSH(ctx, stream, target, open, addresses, issued.Value)
	}
	if target.Protocol == "winrs" {
		if target.Port != 5986 && target.Port != 443 {
			return status.Error(codes.PermissionDenied, "WINRM_TLS_REQUIRED")
		}
		return server.openWinRS(ctx, stream, target, open, addresses, issued.Value)
	}
	return status.Error(codes.InvalidArgument, "unsupported remote access protocol")
}

func (server RPCServer) openSSH(ctx context.Context, stream directv1.DirectExecutorService_OpenRemoteAccessServer, target db.GetRemoteAccessSessionTargetRow,
	open *directv1.RemoteAccessOpen, addresses []netip.Addr, credential []byte) error {
	auth, err := sshAuth(credential)
	if err != nil {
		return status.Error(codes.PermissionDenied, "invalid SSH credential")
	}
	hostKey := func(_ string, _ net.Addr, key ssh.PublicKey) error {
		if target.PinnedHostKey == "" || ssh.FingerprintSHA256(key) != target.PinnedHostKey {
			return errors.New("SSH host key changed")
		}
		return nil
	}
	connection, err := dialFixed(ctx, addresses[0], target.Port)
	if err != nil {
		return status.Error(codes.Unavailable, "SSH connection failed")
	}
	defer connection.Close()
	if err := server.Executor.Validator.Revalidate(ctx, target.Address, addresses); err != nil {
		return status.Error(codes.PermissionDenied, "remote target changed")
	}
	configuration := &ssh.ClientConfig{User: target.Username, Auth: []ssh.AuthMethod{auth}, HostKeyCallback: hostKey, Timeout: 15 * time.Second}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, net.JoinHostPort(target.Hostname, fmt.Sprint(target.Port)), configuration)
	if err != nil {
		return status.Error(codes.Unavailable, "SSH handshake failed")
	}
	client := ssh.NewClient(clientConnection, channels, requests)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return status.Error(codes.Unavailable, "SSH session failed")
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return err
	}
	if err := session.RequestPty("xterm-256color", int(open.GetTerminalRows()), int(open.GetTerminalCols()), ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return status.Error(codes.Unavailable, "SSH PTY rejected")
	}
	if err := session.Shell(); err != nil {
		return status.Error(codes.Unavailable, "SSH shell rejected")
	}

	outputs := make(chan sshRemoteOutput, 8)
	go readSSHOutput(stdout, "stdout", outputs)
	go readSSHOutput(stderr, "stderr", outputs)
	go func() { outputs <- sshRemoteOutput{err: session.Wait()} }()
	if err := stream.Send(&directv1.OpenRemoteAccessResponse{Sequence: 1, Frame: &directv1.OpenRemoteAccessResponse_Ready{Ready: &directv1.RemoteAccessReady{Mode: "ssh_pty"}}}); err != nil {
		return err
	}
	return bridgeSSHWithOutputs(ctx, stream, session, stdin, outputs, 1, 1)
}

// bridgeSSHWithOutputs bridges SSH I/O with already-started output readers
func bridgeSSHWithOutputs(ctx context.Context, stream directv1.DirectExecutorService_OpenRemoteAccessServer, session *ssh.Session, stdin io.WriteCloser, outputs <-chan sshRemoteOutput, serverSequence, clientSequence uint64) error {
	requests := make(chan *directv1.OpenRemoteAccessRequest, 1)
	go readRemoteRequests(stream, requests)
	for {
		select {
		case <-ctx.Done():
			_ = session.Close()
			return ctx.Err()
		case request, ok := <-requests:
			if !ok || request.GetSequence() != clientSequence+1 || request.GetOpen() != nil {
				return status.Error(codes.InvalidArgument, "remote access sequence invalid")
			}
			clientSequence = request.GetSequence()
			switch {
			case request.GetInput() != nil:
				if len(request.GetInput().GetData()) > maxRPCMessageBytes {
					return status.Error(codes.ResourceExhausted, "remote input too large")
				}
				if _, err := stdin.Write(request.GetInput().GetData()); err != nil {
					return err
				}
			case request.GetResize() != nil:
				resize := request.GetResize()
				if resize.GetCols() < 20 || resize.GetRows() < 5 || session.WindowChange(int(resize.GetRows()), int(resize.GetCols())) != nil {
					return status.Error(codes.InvalidArgument, "invalid terminal size")
				}
			case request.GetClose() != nil:
				_ = session.Close()
				return nil
			default:
				return status.Error(codes.InvalidArgument, "unsupported remote access frame")
			}
		case value := <-outputs:
			if value.err != nil {
				serverSequence++
				_ = stream.Send(&directv1.OpenRemoteAccessResponse{Sequence: serverSequence, Frame: &directv1.OpenRemoteAccessResponse_State{State: &directv1.RemoteAccessState{Status: "terminated", Reason: "remote_closed"}}})
				return nil
			}
			if len(value.data) > 0 {
				serverSequence++
				if err := stream.Send(&directv1.OpenRemoteAccessResponse{Sequence: serverSequence, Frame: &directv1.OpenRemoteAccessResponse_Output{Output: &directv1.RemoteAccessOutput{Stream: value.stream, Data: value.data}}}); err != nil {
					return err
				}
			}
		}
	}
}

func (server RPCServer) openWinRS(ctx context.Context, stream directv1.DirectExecutorService_OpenRemoteAccessServer, target db.GetRemoteAccessSessionTargetRow,
	_ *directv1.RemoteAccessOpen, addresses []netip.Addr, credential []byte) error {
	if err := server.Executor.Validator.Revalidate(ctx, target.Address, addresses); err != nil {
		return status.Error(codes.PermissionDenied, "remote target changed")
	}
	client, err := winrsline.Open(winrsline.Options{
		Host: target.Address, Port: int(target.Port), Username: target.Username, Password: string(credential),
		Dial: func(_, _ string) (net.Conn, error) {
			return net.DialTimeout("tcp", net.JoinHostPort(addresses[0].String(), fmt.Sprint(target.Port)), 15*time.Second)
		},
	})
	if err != nil {
		if server.Logger != nil {
			server.Logger.Warn("Direct Executor could not create WinRS shell", "error", err)
		}
		return status.Error(codes.Unavailable, "WinRS client failed")
	}
	defer client.Close()
	if err := stream.Send(&directv1.OpenRemoteAccessResponse{Sequence: 1, Frame: &directv1.OpenRemoteAccessResponse_Ready{Ready: &directv1.RemoteAccessReady{Mode: "winrs_line"}}}); err != nil {
		return err
	}
	requests := make(chan *directv1.OpenRemoteAccessRequest, 1)
	go readRemoteRequests(stream, requests)
	serverSequence, clientSequence := uint64(1), uint64(1)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case request, ok := <-requests:
			if !ok || request.GetSequence() != clientSequence+1 || request.GetOpen() != nil {
				return status.Error(codes.InvalidArgument, "remote access sequence invalid")
			}
			clientSequence = request.GetSequence()
			if request.GetClose() != nil {
				return nil
			}
			if request.GetResize() != nil {
				continue
			}
			input := request.GetInput()
			if input == nil || len(input.GetData()) == 0 || len(input.GetData()) > maxRPCMessageBytes {
				return status.Error(codes.InvalidArgument, "WinRS line input invalid")
			}
			command := string(input.GetData())
			if command == "" {
				continue
			}
			result, err := client.ExecuteLine(ctx, command)
			if err != nil {
				return status.Error(codes.Unavailable, "WinRS command failed")
			}
			for _, output := range []struct {
				stream string
				data   []byte
			}{{"stdout", result.Stdout}, {"stderr", result.Stderr}} {
				if len(output.data) == 0 {
					continue
				}
				serverSequence++
				if err := stream.Send(&directv1.OpenRemoteAccessResponse{Sequence: serverSequence, Frame: &directv1.OpenRemoteAccessResponse_Output{Output: &directv1.RemoteAccessOutput{Stream: output.stream, Data: output.data}}}); err != nil {
					return err
				}
			}
		}
	}
}

func parseRemoteIDs(open *directv1.RemoteAccessOpen) (uuid.UUID, uuid.UUID, error) {
	sessionID, err := uuid.Parse(open.GetSessionId())
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	leaseID, err := uuid.Parse(open.GetCredentialLeaseId())
	return sessionID, leaseID, err
}

func readRemoteRequests(stream directv1.DirectExecutorService_OpenRemoteAccessServer, output chan<- *directv1.OpenRemoteAccessRequest) {
	defer close(output)
	for {
		request, err := stream.Recv()
		if err != nil {
			return
		}
		output <- request
	}
}

func readSSHOutput(reader io.Reader, stream string, output chan<- sshRemoteOutput) {
	buffer := make([]byte, 32<<10)
	for {
		count, err := reader.Read(buffer)
		if count > 0 {
			output <- sshRemoteOutput{stream: stream, data: append([]byte(nil), buffer[:count]...)}
		}
		if err != nil {
			return
		}
	}
}

func boundedRemoteDuration(seconds uint32) time.Duration {
	if seconds < 60 || seconds > 3600 {
		return time.Hour
	}
	return time.Duration(seconds) * time.Second
}
