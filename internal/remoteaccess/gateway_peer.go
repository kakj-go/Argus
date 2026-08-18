package remoteaccess

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	remotev1 "github.com/kakj-go/Argus/internal/gen/proto/argus/remoteaccess/v1"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	redisstore "github.com/kakj-go/Argus/internal/storage/redis"
)

type ConnectorOwnerResolver struct {
	Store *postgres.Store
	Redis *redisstore.Client
}

type RouteResolver interface {
	Resolve(context.Context, uuid.UUID, int64) (string, error)
	SaveRoute(context.Context, ConnectionTarget, string) error
	DeleteRoute(context.Context, ConnectionTarget)
}

func (resolver ConnectorOwnerResolver) Resolve(ctx context.Context, connectorID uuid.UUID, epoch int64) (string, error) {
	if connectorID == uuid.Nil || epoch < 1 || resolver.Store == nil {
		return "", ErrSessionUnavailable
	}
	if resolver.Redis != nil {
		if entry, err := resolver.Redis.GetConnectorRegistry(ctx, connectorID.String()); err == nil && entry.ConnectionEpoch == epoch {
			return entry.GatewayInstanceID, nil
		}
	}
	session, err := resolver.Store.Queries.GetConnectorSession(ctx, db.GetConnectorSessionParams{ConnectorID: connectorID, ConnectionEpoch: epoch})
	if err != nil || session.GatewayInstanceID == "" {
		return "", ErrSessionUnavailable
	}
	return session.GatewayInstanceID, nil
}

func (resolver ConnectorOwnerResolver) SaveRoute(ctx context.Context, target ConnectionTarget, owner string) error {
	if resolver.Store == nil || owner == "" {
		return ErrSessionUnavailable
	}
	_, err := resolver.Store.Queries.UpsertRemoteAccessRoute(ctx, db.UpsertRemoteAccessRouteParams{
		SessionID: target.Session.ID, GatewayInstance: owner, ConnectorID: target.ConnectorID,
		ConnectorEpoch: pgtype.Int8{Int64: target.ConnectionEpoch, Valid: target.ConnectionEpoch > 0},
		SessionFence:   target.Session.SessionFence, LeaseExpiresAt: timestamp(target.LeaseExpiresAt),
	})
	return err
}

func (resolver ConnectorOwnerResolver) DeleteRoute(ctx context.Context, target ConnectionTarget) {
	if resolver.Store != nil {
		_, _ = resolver.Store.Queries.DeleteRemoteAccessRoute(ctx, db.DeleteRemoteAccessRouteParams{
			SessionID: target.Session.ID, SessionFence: target.Session.SessionFence,
		})
	}
}

type GatewayPeerOpener interface {
	Open(context.Context, string, ConnectionTarget, int, int) (BackendSession, error)
}

type DistributedConnectorFactory struct {
	InstanceID string
	Local      BackendFactory
	Resolver   RouteResolver
	Peers      GatewayPeerOpener
}

func (factory DistributedConnectorFactory) Open(ctx context.Context, target ConnectionTarget, cols, rows int) (BackendSession, error) {
	if factory.InstanceID == "" || factory.Local == nil || factory.Peers == nil || !target.ConnectorID.Valid {
		return nil, ErrSessionUnavailable
	}
	owner, err := factory.Resolver.Resolve(ctx, target.ConnectorID.UUID, target.ConnectionEpoch)
	if err != nil {
		return nil, err
	}
	var session BackendSession
	if owner == factory.InstanceID {
		session, err = factory.Local.Open(ctx, target, cols, rows)
	} else {
		session, err = factory.Peers.Open(ctx, owner, target, cols, rows)
	}
	if err != nil {
		return nil, err
	}
	if err := factory.Resolver.SaveRoute(ctx, target, factory.InstanceID); err != nil {
		_ = session.Close(context.Background(), "route_persist_failed")
		return nil, err
	}
	return &routedBackendSession{BackendSession: session, resolver: factory.Resolver, target: target}, nil
}

type routedBackendSession struct {
	BackendSession
	resolver RouteResolver
	target   ConnectionTarget
	once     sync.Once
}

func (session *routedBackendSession) Close(ctx context.Context, reason string) error {
	err := session.BackendSession.Close(ctx, reason)
	session.once.Do(func() { session.resolver.DeleteRoute(context.Background(), session.target) })
	return err
}

type GatewayPeerDialer struct {
	TLSConfig      *tls.Config
	HeadlessSuffix string
	Port           string
	Resolver       GatewayPeerEndpointResolver
	Logger         *slog.Logger
}

type GatewayPeerEndpointResolver interface {
	ResolveEndpoint(context.Context, string, string) (string, error)
}

func NewGatewayPeerDialer(certificatePath, privateKeyPath, caPath, serverName, headlessSuffix, port string) (*GatewayPeerDialer, error) {
	configuration, err := loadPeerTLS(certificatePath, privateKeyPath, caPath, serverName, false)
	if err != nil {
		return nil, err
	}
	if headlessSuffix == "" || port == "" {
		return nil, errors.New("Gateway peer DNS suffix and port are required")
	}
	return &GatewayPeerDialer{TLSConfig: configuration, HeadlessSuffix: strings.TrimPrefix(headlessSuffix, "."), Port: port}, nil
}

func (dialer *GatewayPeerDialer) Open(ctx context.Context, owner string, target ConnectionTarget, cols, rows int) (BackendSession, error) {
	if dialer == nil || dialer.TLSConfig == nil || owner == "" || !validSize(cols, rows) {
		return nil, ErrSessionUnavailable
	}
	endpoint := fmt.Sprintf("%s.%s:%s", owner, dialer.HeadlessSuffix, dialer.Port)
	if dialer.Resolver != nil {
		var err error
		endpoint, err = dialer.Resolver.ResolveEndpoint(ctx, owner, dialer.Port)
		if err != nil {
			dialer.logFailure("resolve", err)
			return nil, err
		}
	}
	connection, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(credentials.NewTLS(dialer.TLSConfig.Clone())),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxFrameBytes), grpc.MaxCallSendMsgSize(MaxFrameBytes)))
	if err != nil {
		return nil, err
	}
	stream, err := remotev1.NewGatewayPeerServiceClient(connection).OpenRemoteAccess(ctx)
	if err != nil {
		dialer.logFailure("open", err)
		_ = connection.Close()
		return nil, err
	}
	request := &remotev1.OpenRemoteAccessRequest{Sequence: 1, Frame: &remotev1.OpenRemoteAccessRequest_Open{Open: &remotev1.GatewayPeerOpen{
		SessionId: target.Session.ID.String(), SessionFence: uint64(target.Session.SessionFence), ConnectorId: target.ConnectorID.UUID.String(),
		ConnectionEpoch: uint64(target.ConnectionEpoch), CredentialLeaseId: target.CredentialLeaseID.String(), TerminalCols: uint32(cols), TerminalRows: uint32(rows),
	}}}
	if err := stream.Send(request); err != nil {
		dialer.logFailure("send", err)
		_ = connection.Close()
		return nil, err
	}
	first, err := stream.Recv()
	if err != nil || first.GetSequence() != 1 || first.GetReady() == nil {
		dialer.logFailure("receive_ready", err)
		_ = connection.Close()
		return nil, ErrSessionUnavailable
	}
	return &gatewayPeerSession{connection: connection, stream: stream, clientSequence: 1, serverSequence: 1}, nil
}

func (dialer *GatewayPeerDialer) logFailure(stage string, err error) {
	if dialer == nil || dialer.Logger == nil {
		return
	}
	if err == nil {
		dialer.Logger.Warn("Gateway peer handshake returned an invalid frame", "stage", stage)
		return
	}
	value := status.Convert(err)
	dialer.Logger.Warn("Gateway peer handshake failed", "stage", stage, "code", value.Code().String(), "error", value.Message())
}

type gatewayPeerSession struct {
	connection     *grpc.ClientConn
	stream         remotev1.GatewayPeerService_OpenRemoteAccessClient
	mu             sync.Mutex
	clientSequence uint64
	serverSequence uint64
	closed         bool
}

func (session *gatewayPeerSession) Send(_ context.Context, frame ClientFrame) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return io.EOF
	}
	session.clientSequence++
	request := &remotev1.OpenRemoteAccessRequest{Sequence: session.clientSequence}
	switch frame.Type {
	case "input":
		request.Frame = &remotev1.OpenRemoteAccessRequest_Input{Input: &remotev1.GatewayPeerInput{Data: []byte(frame.Data)}}
	case "resize":
		request.Frame = &remotev1.OpenRemoteAccessRequest_Resize{Resize: &remotev1.GatewayPeerResize{Cols: uint32(frame.Cols), Rows: uint32(frame.Rows)}}
	case "close":
		request.Frame = &remotev1.OpenRemoteAccessRequest_Close{Close: &remotev1.GatewayPeerClose{Reason: frame.Reason}}
	default:
		return ErrProtocol
	}
	return session.stream.Send(request)
}

func (session *gatewayPeerSession) Receive(_ context.Context) (BackendFrame, error) {
	response, err := session.stream.Recv()
	if err != nil {
		return BackendFrame{}, err
	}
	if response.GetSequence() != session.serverSequence+1 || response.GetReady() != nil {
		return BackendFrame{}, ErrProtocol
	}
	session.serverSequence = response.GetSequence()
	if output := response.GetOutput(); output != nil {
		if output.GetStream() != "stdout" && output.GetStream() != "stderr" {
			return BackendFrame{}, ErrProtocol
		}
		return BackendFrame{Type: "output", Stream: output.GetStream(), Data: append([]byte(nil), output.GetData()...)}, nil
	}
	if state := response.GetState(); state != nil {
		return BackendFrame{Type: "state", Status: state.GetStatus(), Reason: state.GetReason()}, nil
	}
	return BackendFrame{}, ErrProtocol
}

func (session *gatewayPeerSession) Close(_ context.Context, reason string) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil
	}
	session.closed = true
	session.clientSequence++
	err := session.stream.Send(&remotev1.OpenRemoteAccessRequest{Sequence: session.clientSequence,
		Frame: &remotev1.OpenRemoteAccessRequest_Close{Close: &remotev1.GatewayPeerClose{Reason: reason}}})
	_ = session.stream.CloseSend()
	closeErr := session.connection.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return closeErr
}

type GatewayPeerServer struct {
	remotev1.UnimplementedGatewayPeerServiceServer
	Service    GatewayService
	Local      BackendFactory
	InstanceID string
	Logger     *slog.Logger
}

func (server GatewayPeerServer) OpenRemoteAccess(stream remotev1.GatewayPeerService_OpenRemoteAccessServer) error {
	if server.Service.Store == nil || server.Local == nil || server.InstanceID == "" {
		return status.Error(codes.Unavailable, "Gateway peer unavailable")
	}
	first, err := stream.Recv()
	if err != nil || first.GetSequence() != 1 || first.GetOpen() == nil {
		server.logFailure("receive_open", err)
		return status.Error(codes.InvalidArgument, "invalid Gateway peer open")
	}
	open := first.GetOpen()
	sessionID, sessionErr := uuid.Parse(open.GetSessionId())
	connectorID, connectorErr := uuid.Parse(open.GetConnectorId())
	credentialLeaseID, leaseErr := uuid.Parse(open.GetCredentialLeaseId())
	if sessionErr != nil || connectorErr != nil || leaseErr != nil || open.GetSessionFence() < 1 || open.GetConnectionEpoch() < 1 ||
		!validSize(int(open.GetTerminalCols()), int(open.GetTerminalRows())) {
		return status.Error(codes.InvalidArgument, "invalid Gateway peer binding")
	}
	owner, err := server.Service.Store.Queries.GetConnectorSession(stream.Context(), db.GetConnectorSessionParams{
		ConnectorID: connectorID, ConnectionEpoch: int64(open.GetConnectionEpoch()),
	})
	if err != nil || owner.GatewayInstanceID != server.InstanceID {
		server.logFailure("verify_owner", err)
		return status.Error(codes.FailedPrecondition, "Gateway no longer owns Connector")
	}
	target, err := server.Service.ResolvePeerTarget(stream.Context(), sessionID, int64(open.GetSessionFence()), connectorID,
		int64(open.GetConnectionEpoch()), credentialLeaseID)
	if err != nil {
		server.logFailure("resolve_target", err)
		return status.Error(codes.PermissionDenied, "remote access binding is invalid")
	}
	backend, err := server.Local.Open(stream.Context(), target, int(open.GetTerminalCols()), int(open.GetTerminalRows()))
	if err != nil {
		server.logFailure("open_connector", err)
		return status.Error(codes.Unavailable, "Connector stream unavailable")
	}
	defer backend.Close(context.Background(), "peer_closed")
	mode := "ssh_pty"
	if target.Protocol == "winrs" {
		mode = "winrs_line"
	}
	if err := stream.Send(&remotev1.OpenRemoteAccessResponse{Sequence: 1,
		Frame: &remotev1.OpenRemoteAccessResponse_Ready{Ready: &remotev1.GatewayPeerReady{Mode: mode}}}); err != nil {
		return err
	}
	requests := make(chan peerRequest, 1)
	backendFrames := make(chan receivedBackendFrame, 1)
	go receivePeerRequests(stream, requests)
	go receiveBackendFrames(stream.Context(), backend, backendFrames)
	clientSequence, serverSequence := uint64(1), uint64(1)
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case item := <-requests:
			if item.err != nil {
				return item.err
			}
			if item.request.GetSequence() != clientSequence+1 {
				return status.Error(codes.InvalidArgument, "Gateway peer sequence mismatch")
			}
			clientSequence = item.request.GetSequence()
			frame, closeStream, err := peerClientFrame(item.request)
			if err != nil {
				return status.Error(codes.InvalidArgument, "invalid Gateway peer frame")
			}
			if closeStream {
				return nil
			}
			if err := backend.Send(stream.Context(), frame); err != nil {
				return status.Error(codes.Unavailable, "Connector stream send failed")
			}
		case item := <-backendFrames:
			if item.err != nil {
				if errors.Is(item.err, io.EOF) {
					return nil
				}
				return item.err
			}
			serverSequence++
			if err := stream.Send(peerServerFrame(serverSequence, item.frame)); err != nil {
				return err
			}
			if item.frame.Type == "state" && terminalState(item.frame.Status) {
				return nil
			}
		}
	}
}

func (server GatewayPeerServer) logFailure(stage string, err error) {
	if server.Logger == nil {
		return
	}
	if err == nil {
		server.Logger.Warn("Gateway peer request failed", "stage", stage)
		return
	}
	value := status.Convert(err)
	server.Logger.Warn("Gateway peer request failed", "stage", stage, "code", value.Code().String(), "error", value.Message())
}

type peerRequest struct {
	request *remotev1.OpenRemoteAccessRequest
	err     error
}

func receivePeerRequests(stream remotev1.GatewayPeerService_OpenRemoteAccessServer, output chan<- peerRequest) {
	for {
		request, err := stream.Recv()
		select {
		case output <- peerRequest{request: request, err: err}:
		case <-stream.Context().Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func peerClientFrame(request *remotev1.OpenRemoteAccessRequest) (ClientFrame, bool, error) {
	if input := request.GetInput(); input != nil && len(input.GetData()) > 0 && len(input.GetData()) <= MaxFrameBytes {
		return ClientFrame{Type: "input", Data: string(input.GetData())}, false, nil
	}
	if resize := request.GetResize(); resize != nil && validSize(int(resize.GetCols()), int(resize.GetRows())) {
		return ClientFrame{Type: "resize", Cols: int(resize.GetCols()), Rows: int(resize.GetRows())}, false, nil
	}
	if closeFrame := request.GetClose(); closeFrame != nil {
		return ClientFrame{Type: "close", Reason: closeFrame.GetReason()}, true, nil
	}
	return ClientFrame{}, false, ErrProtocol
}

func peerServerFrame(sequence uint64, frame BackendFrame) *remotev1.OpenRemoteAccessResponse {
	response := &remotev1.OpenRemoteAccessResponse{Sequence: sequence}
	if frame.Type == "output" {
		response.Frame = &remotev1.OpenRemoteAccessResponse_Output{Output: &remotev1.GatewayPeerOutput{Stream: frame.Stream, Data: append([]byte(nil), frame.Data...)}}
	} else {
		response.Frame = &remotev1.OpenRemoteAccessResponse_State{State: &remotev1.GatewayPeerState{Status: frame.Status, Reason: frame.Reason}}
	}
	return response
}

func LoadGatewayPeerServerTLS(certificatePath, privateKeyPath, caPath, authorizedPeerName string) (*tls.Config, error) {
	return loadPeerTLS(certificatePath, privateKeyPath, caPath, authorizedPeerName, true)
}

func loadPeerTLS(certificatePath, privateKeyPath, caPath, peerName string, server bool) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certificatePath, privateKeyPath)
	if err != nil {
		return nil, err
	}
	bundle, err := os.ReadFile(caPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundle) || peerName == "" {
		return nil, errors.New("Gateway peer CA bundle and peer name are required")
	}
	configuration := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}}
	if !server {
		configuration.RootCAs, configuration.ServerName = roots, peerName
		return configuration, nil
	}
	configuration.ClientAuth, configuration.ClientCAs = tls.RequireAndVerifyClientCert, roots
	configuration.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("Gateway peer certificate is missing")
		}
		if err := state.PeerCertificates[0].VerifyHostname(peerName); err != nil {
			return fmt.Errorf("unauthorized Gateway peer: %w", err)
		}
		return nil
	}
	return configuration, nil
}
