package directexecutor

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"

	directv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/directexecutor/v1"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const maxRPCMessageBytes = 64 * 1024

type Dispatcher struct {
	connection *grpc.ClientConn
	client     directv1.DirectExecutorServiceClient
}

func NewDispatcher(endpoint, serverName, certificatePath, privateKeyPath, caPath string) (*Dispatcher, error) {
	tlsConfig, err := loadClientTLS(certificatePath, privateKeyPath, caPath, serverName)
	if err != nil {
		return nil, err
	}
	connection, err := grpc.NewClient(strings.TrimPrefix(endpoint, "grpcs://"),
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
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

func LoadServerTLS(certificatePath, privateKeyPath, caPath string, authorizedClientNames []string) (*tls.Config, error) {
	certificate, roots, err := loadTLSMaterial(certificatePath, privateKeyPath, caPath)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    roots,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return errors.New("Direct Executor client certificate is missing")
			}
			return authorizeClientCertificate(state.PeerCertificates[0], authorizedClientNames)
		},
	}, nil
}

func authorizeClientCertificate(certificate *x509.Certificate, authorizedClientNames []string) error {
	for _, name := range authorizedClientNames {
		if name != "" && certificate.VerifyHostname(name) == nil {
			return nil
		}
	}
	return fmt.Errorf("unauthorized Direct Executor client identity")
}

func loadClientTLS(certificatePath, privateKeyPath, caPath, serverName string) (*tls.Config, error) {
	certificate, roots, err := loadTLSMaterial(certificatePath, privateKeyPath, caPath)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName, RootCAs: roots, Certificates: []tls.Certificate{certificate}}, nil
}

func loadTLSMaterial(certificatePath, privateKeyPath, caPath string) (tls.Certificate, *x509.CertPool, error) {
	certificate, err := tls.LoadX509KeyPair(certificatePath, privateKeyPath)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	bundle, err := os.ReadFile(caPath)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(bundle) {
		return tls.Certificate{}, nil, errors.New("Direct Executor CA bundle contains no certificates")
	}
	return certificate, roots, nil
}
