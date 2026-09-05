package directexecutor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	directv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/directexecutor/v1"
	"github.com/kakj-go/Argus/internal/storage/postgres"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
	"github.com/kakj-go/Argus/internal/tlsmaterial"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

const maxRPCMessageBytes = 64 * 1024

type Dispatcher struct {
	connection *grpc.ClientConn
	client     directv1.DirectExecutorServiceClient
}

func NewDispatcher(endpoint, serverName, certificatePath, privateKeyPath, caPath string) (*Dispatcher, error) {
	material, err := tlsmaterial.Load(tlsmaterial.Options{CertificatePath: certificatePath, PrivateKeyPath: privateKeyPath,
		CABundlePath: caPath, Usage: x509.ExtKeyUsageClientAuth})
	if err != nil {
		return nil, err
	}
	tlsCredentials, err := tlsmaterial.ClientCredentials(material, serverName)
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(strings.TrimPrefix(endpoint, "grpcs://"),
		grpc.WithTransportCredentials(tlsCredentials),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxRPCMessageBytes), grpc.MaxCallSendMsgSize(maxRPCMessageBytes)))
	if err != nil {
		return nil, err
	}
	return &Dispatcher{connection: connection, client: directv1.NewDirectExecutorServiceClient(connection)}, nil
}

func (dispatcher *Dispatcher) Close() error { return dispatcher.connection.Close() }

func (dispatcher *Dispatcher) DispatchConnectionTest(ctx context.Context, test db.ConnectionTest) error {
	_, err := dispatcher.client.DispatchConnectionTest(ctx, &directv1.DispatchConnectionTestRequest{ConnectionTestId: test.ID.String()})
	return err
}

func (dispatcher *Dispatcher) DispatchCollectorManagement(ctx context.Context, operation db.TelemetryCollectorOperation) error {
	_, err := dispatcher.client.DispatchCollectorManagement(ctx, &directv1.DispatchCollectorManagementRequest{OperationId: operation.ID.String()})
	return err
}

func (dispatcher *Dispatcher) OpenRemoteAccess(ctx context.Context) (directv1.DirectExecutorService_OpenRemoteAccessClient, error) {
	return dispatcher.client.OpenRemoteAccess(ctx)
}

type RPCServer struct {
	directv1.UnimplementedDirectExecutorServiceServer
	Executor *Executor
	Context  context.Context
	Logger   *slog.Logger
}

func (server RPCServer) DispatchConnectionTest(_ context.Context, request *directv1.DispatchConnectionTestRequest) (*directv1.DispatchConnectionTestResponse, error) {
	if server.Executor == nil || server.Executor.Store == nil {
		return nil, status.Error(codes.Unavailable, "Direct Executor is unavailable")
	}
	id, err := uuid.Parse(request.GetConnectionTestId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid connection test ID")
	}
	if !server.Executor.reserveOne() {
		return nil, status.Error(codes.ResourceExhausted, "Direct Executor is at capacity")
	}
	test, err := server.Executor.Store.Queries.ClaimDirectConnectionTest(server.Context, id)
	if errors.Is(err, pgx.ErrNoRows) {
		server.Executor.release(1)
		return &directv1.DispatchConnectionTestResponse{Status: directv1.DispatchStatus_DISPATCH_STATUS_ALREADY_HANDLED}, nil
	}
	if err != nil {
		server.Executor.release(1)
		return nil, status.Error(codes.Unavailable, "claim connection test")
	}
	go func() {
		defer server.Executor.release(1)
		server.Executor.execute(server.Context, test)
	}()
	return &directv1.DispatchConnectionTestResponse{Status: directv1.DispatchStatus_DISPATCH_STATUS_ACCEPTED}, nil
}

func (server RPCServer) DispatchCollectorManagement(_ context.Context, request *directv1.DispatchCollectorManagementRequest) (*directv1.DispatchCollectorManagementResponse, error) {
	if server.Executor == nil || server.Executor.Store == nil {
		return nil, status.Error(codes.Unavailable, "Direct Executor is unavailable")
	}
	id, err := uuid.Parse(request.GetOperationId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid Collector operation ID")
	}
	if !server.Executor.reserveOne() {
		return nil, status.Error(codes.ResourceExhausted, "Direct Executor is at capacity")
	}
	operation, err := server.Executor.Store.Queries.ClaimTelemetryCollectorOperation(server.Context, db.ClaimTelemetryCollectorOperationParams{
		ID: id, LeaseOwner: pgtype.Text{String: server.Executor.InstanceID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		server.Executor.release(1)
		return &directv1.DispatchCollectorManagementResponse{Status: directv1.DispatchStatus_DISPATCH_STATUS_ALREADY_HANDLED}, nil
	}
	if err != nil {
		server.Executor.release(1)
		return nil, status.Error(codes.Unavailable, "claim Collector operation")
	}
	go func() {
		defer server.Executor.release(1)
		server.Executor.executeCollectorOperation(server.Context, operation)
	}()
	return &directv1.DispatchCollectorManagementResponse{Status: directv1.DispatchStatus_DISPATCH_STATUS_ACCEPTED}, nil
}

func LoadServerTLS(certificatePath, privateKeyPath, caPath string, store *postgres.Store, authorizedClientURIs []string) (*tls.Config, error) {
	material, err := tlsmaterial.Load(tlsmaterial.Options{CertificatePath: certificatePath, PrivateKeyPath: privateKeyPath,
		CABundlePath: caPath, Usage: x509.ExtKeyUsageServerAuth})
	if err != nil || store == nil || len(authorizedClientURIs) == 0 {
		if err == nil {
			err = errors.New("Direct Executor service identity registry is not configured")
		}
		return nil, err
	}
	return material.ServerConfig(tls.RequireAndVerifyClientCert, func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return errors.New("Direct Executor client certificate is missing")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return trustbundle.VerifyServiceCertificate(ctx, store.Queries, state.PeerCertificates[0], authorizedClientURIs)
	})
}
