package connector

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/common/v1"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/resource"
	"github.com/kakj-go/Argus/internal/secret"
	"github.com/kakj-go/Argus/internal/storage/postgres/db"
)

const (
	ProtocolVersion      = "argus.connector/v1"
	MaxMessageBytes      = 1 << 20
	MaxInflightCommands  = 16
	defaultDispatchEvery = 500 * time.Millisecond
)

type Gateway struct {
	connectorv1.UnimplementedConnectorControlServiceServer
	Service           Service
	Credentials       secret.Service
	HeartbeatInterval time.Duration
	DispatchInterval  time.Duration
	Dispatch          *DispatchHub
	RemoteAccess      *RemoteAccessHub
	Drain             <-chan struct{}
}

type activeCommand struct {
	acknowledged bool
	expiresAt    time.Time
}

func (gateway Gateway) Connect(stream connectorv1.ConnectorControlService_ConnectServer) error {
	identity, connectorRecord, err := gateway.trustedIdentity(stream.Context())
	if err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	hello := first.GetHello()
	if first.GetSequence() != 1 || hello == nil || hello.GetProtocolVersion() != ProtocolVersion || hello.GetInstanceId() != connectorRecord.InstanceID || len(hello.GetClientNonce()) < 16 || len(hello.GetClientNonce()) > 64 {
		return gateway.close(stream, 1, commonv1.CloseReason_CLOSE_REASON_PROTOCOL_ERROR, "CONNECTOR_PROTOCOL_ERROR")
	}
	opened, err := gateway.Service.OpenSession(stream.Context(), identity, hello.GetCapabilities())
	if err != nil {
		return gateway.close(stream, 1, commonv1.CloseReason_CLOSE_REASON_AUTHORIZATION_REVOKED, "CONNECTOR_FENCED")
	}
	epoch := opened.ConnectionEpoch
	defer gateway.Service.Disconnect(context.Background(), identity, epoch)
	serverNonce := make([]byte, 32)
	if _, err := rand.Read(serverNonce); err != nil {
		return err
	}
	heartbeat := gateway.heartbeatInterval()
	serverSequence := uint64(1)
	if err := stream.Send(&connectorv1.ConnectResponse{Sequence: serverSequence, Frame: &connectorv1.ConnectResponse_Welcome{Welcome: &connectorv1.ConnectorWelcome{
		ProtocolVersion: ProtocolVersion, ConnectionEpoch: uint64(epoch), ServerTime: timestamppb.Now(), HeartbeatInterval: durationpb.New(heartbeat),
		MaxMessageBytes: MaxMessageBytes, MaxInflightCommands: MaxInflightCommands, ServerNonce: serverNonce,
		CertificateRotationRequested: opened.CertificateRotationRequestedAt.Valid}}}); err != nil {
		return err
	}
	serverSequence++
	if err := gateway.sendReconcile(stream, identity, epoch, &serverSequence); err != nil {
		return err
	}

	received := make(chan receiveResult, 1)
	remoteOutbound := make(chan *connectorv1.ConnectResponse, 32)
	unregisterRemote := func() {}
	if gateway.RemoteAccess != nil {
		unregisterRemote = gateway.RemoteAccess.Register(identity.ConnectorID, epoch, remoteOutbound)
	}
	defer unregisterRemote()
	go receiveConnectorFrames(stream, received)
	dispatchTicker := time.NewTicker(gateway.dispatchInterval())
	defer dispatchTicker.Stop()
	heartbeatDeadline := time.NewTimer(3 * heartbeat)
	defer heartbeatDeadline.Stop()
	lastClientSequence := uint64(1)
	pendingAcknowledgements := map[uint64]string{}
	activeCommands := map[string]activeCommand{}
	dispatchWakeup, unregisterDispatch := gateway.Dispatch.Register(identity.ConnectorID)
	defer unregisterDispatch()
	defer gateway.markUncertain(identity, epoch, activeCommands)
	dispatchCommands := func() error {
		removeExpiredActiveCommands(time.Now(), activeCommands, pendingAcknowledgements)
		available := MaxInflightCommands - int32(len(activeCommands))
		if available <= 0 {
			return nil
		}
		commands, err := gateway.Service.ListCommands(stream.Context(), identity, epoch, available)
		if err != nil {
			return err
		}
		for _, command := range commands {
			frame, err := typedCommand(command)
			if err != nil {
				_, _ = gateway.Service.TransitionCommand(stream.Context(), identity, epoch, command.CommandID, "failed", nil, "CONNECTOR_COMMAND_SCHEMA_INVALID")
				continue
			}
			if _, err := gateway.Service.TransitionCommand(stream.Context(), identity, epoch, command.CommandID, "dispatched", nil, ""); err != nil {
				continue
			}
			if err := stream.Send(&connectorv1.ConnectResponse{Sequence: serverSequence, Frame: &connectorv1.ConnectResponse_Command{Command: frame}}); err != nil {
				_, _ = gateway.Service.TransitionCommand(context.Background(), identity, epoch, command.CommandID, "delivery_unknown", nil, "CONNECTOR_DISCONNECTED")
				delete(activeCommands, command.CommandID)
				return err
			}
			pendingAcknowledgements[serverSequence] = command.CommandID
			activeCommands[command.CommandID] = activeCommand{expiresAt: command.ExpiresAt.Time}
			serverSequence++
		}
		return nil
	}
	for {
		select {
		case <-gateway.Drain:
			return gateway.close(stream, serverSequence, commonv1.CloseReason_CLOSE_REASON_SERVER_DRAIN, "CONNECTOR_GATEWAY_DRAINING")
		case <-stream.Context().Done():
			return stream.Context().Err()
		case <-heartbeatDeadline.C:
			return gateway.close(stream, serverSequence, commonv1.CloseReason_CLOSE_REASON_SESSION_EXPIRED, "CONNECTOR_HEARTBEAT_TIMEOUT")
		case item := <-received:
			if item.err != nil {
				if errors.Is(item.err, io.EOF) {
					return nil
				}
				return item.err
			}
			message := item.message
			if message.GetSequence() != lastClientSequence+1 || message.GetHello() != nil || message.GetRemoteAccessData() != nil {
				return gateway.close(stream, serverSequence, commonv1.CloseReason_CLOSE_REASON_PROTOCOL_ERROR, "CONNECTOR_SEQUENCE_INVALID")
			}
			lastClientSequence = message.GetSequence()
			if leaseRequest := message.GetCredentialLeaseRequest(); leaseRequest != nil {
				grant, err := gateway.fulfillCredentialLease(stream.Context(), identity, epoch, leaseRequest)
				if err != nil {
					return gateway.close(stream, serverSequence, commonv1.CloseReason_CLOSE_REASON_AUTHORIZATION_REVOKED, "CREDENTIAL_LEASE_INVALID")
				}
				if err := stream.Send(&connectorv1.ConnectResponse{Sequence: serverSequence, Frame: &connectorv1.ConnectResponse_CredentialLeaseGrant{CredentialLeaseGrant: grant}}); err != nil {
					return err
				}
				// grpc-go may retain the message after Send returns. The Connector owns
				// zeroing the received copy after the command has completed.
				serverSequence++
			}
			if rotationRequest := message.GetCertificateRotationRequest(); rotationRequest != nil {
				if rotationRequest.GetConnectionEpoch() != uint64(epoch) {
					return gateway.close(stream, serverSequence, commonv1.CloseReason_CLOSE_REASON_AUTHORIZATION_REVOKED, "CONNECTOR_FENCED")
				}
				certificate, err := gateway.Service.RotateCertificate(stream.Context(), identity, epoch, rotationRequest.GetCsrPem())
				if err != nil {
					return gateway.close(stream, serverSequence, commonv1.CloseReason_CLOSE_REASON_AUTHORIZATION_REVOKED, "CONNECTOR_CERTIFICATE_ROTATION_FAILED")
				}
				grant := &connectorv1.CertificateRotationGrant{ConnectionEpoch: uint64(epoch), CertificatePem: []byte(certificate.PEM),
					CaBundlePem: []byte(certificate.CABundlePEM), NotAfter: timestamppb.New(certificate.NotAfter)}
				if err := stream.Send(&connectorv1.ConnectResponse{Sequence: serverSequence, Frame: &connectorv1.ConnectResponse_CertificateRotationGrant{
					CertificateRotationGrant: grant}}); err != nil {
					return err
				}
				serverSequence++
			}
			if err := gateway.handleFrame(stream.Context(), identity, epoch, message, pendingAcknowledgements, activeCommands); err != nil {
				return gateway.close(stream, serverSequence, commonv1.CloseReason_CLOSE_REASON_PROTOCOL_ERROR, "CONNECTOR_FRAME_INVALID")
			}
			if message.GetHeartbeat() != nil {
				if !heartbeatDeadline.Stop() {
					select {
					case <-heartbeatDeadline.C:
					default:
					}
				}
				heartbeatDeadline.Reset(3 * heartbeat)
			}
			if err := stream.Send(&connectorv1.ConnectResponse{Sequence: serverSequence, Frame: &connectorv1.ConnectResponse_Acknowledge{
				Acknowledge: &connectorv1.ServerAcknowledge{ClientSequence: lastClientSequence}}}); err != nil {
				return err
			}
			serverSequence++
		case <-dispatchTicker.C:
			if err := dispatchCommands(); err != nil {
				return err
			}
		case <-dispatchWakeup:
			if err := dispatchCommands(); err != nil {
				return err
			}
		case frame := <-remoteOutbound:
			frame.Sequence = serverSequence
			if err := stream.Send(frame); err != nil {
				return err
			}
			serverSequence++
		}
	}
}

type receiveResult struct {
	message *connectorv1.ConnectRequest
	err     error
}

func receiveConnectorFrames(stream connectorv1.ConnectorControlService_ConnectServer, output chan<- receiveResult) {
	for {
		message, err := stream.Recv()
		output <- receiveResult{message: message, err: err}
		if err != nil {
			return
		}
	}
}

func (gateway Gateway) handleFrame(ctx context.Context, identity TrustedIdentity, epoch int64, request *connectorv1.ConnectRequest, acknowledgements map[uint64]string, active map[string]activeCommand) error {
	switch {
	case request.GetHeartbeat() != nil:
		if request.GetHeartbeat().GetConnectionEpoch() != uint64(epoch) {
			return ErrConnectorFenced
		}
		return gateway.Service.Heartbeat(ctx, identity, epoch)
	case request.GetAcknowledge() != nil:
		sequence := request.GetAcknowledge().GetServerSequence()
		commandID, ok := acknowledgements[sequence]
		if !ok {
			return ErrCommandState
		}
		if _, err := gateway.Service.TransitionCommand(ctx, identity, epoch, commandID, "acknowledged", nil, ""); err != nil {
			return err
		}
		delete(acknowledgements, sequence)
		command := active[commandID]
		command.acknowledged = true
		active[commandID] = command
		return nil
	case request.GetCommandResult() != nil:
		result := request.GetCommandResult()
		if result.GetConnectionEpoch() != uint64(epoch) || len(result.GetResult()) != 0 || result.GetTypedResult() == nil || result.GetResultSchemaVersion() == "" {
			return ErrCommandState
		}
		command, err := gateway.Service.Store.Queries.GetConnectorCommand(ctx, db.GetConnectorCommandParams{CommandID: result.GetCommandId(), ConnectorID: identity.ConnectorID, ConnectionEpoch: epoch})
		if err != nil || !resultTypeAllowed(command.CommandType, result.GetTypedResult()) {
			return ErrCommandState
		}
		value, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(result.GetTypedResult())
		if err != nil || len(value) > MaxMessageBytes {
			return ErrCommandState
		}
		errorCode := ""
		if result.GetError() != nil {
			errorCode = result.GetError().GetCode()
		}
		next := normalizeResultStatus(result.GetStatus())
		if command.CommandType == "connector_uninstall" && next == "succeeded" && !validConnectorUninstallResult(result.GetTypedResult()) {
			return ErrCommandState
		}
		if _, err = gateway.Service.TransitionCommand(ctx, identity, epoch, result.GetCommandId(), next, value, errorCode); err != nil {
			return err
		}
		if next != "running" {
			delete(active, result.GetCommandId())
		}
		return gateway.completeConnectionTest(ctx, command, next, result.GetTypedResult(), errorCode)
	case request.GetCommandReconcileResult() != nil:
		result := request.GetCommandReconcileResult()
		if result.GetConnectionEpoch() != uint64(epoch) {
			return ErrConnectorFenced
		}
		for _, item := range result.GetCommands() {
			next := normalizeResultStatus(item.GetStatus())
			if next != "succeeded" && next != "failed" {
				continue
			}
			errorCode := ""
			if item.GetError() != nil {
				errorCode = item.GetError().GetCode()
			}
			command, err := gateway.Service.Store.Queries.GetConnectorCommand(ctx, db.GetConnectorCommandParams{CommandID: item.GetCommandId(), ConnectorID: identity.ConnectorID, ConnectionEpoch: epoch})
			if err != nil {
				continue
			}
			next = reconciledCommandStatus(command.CommandType, next)
			command, err = gateway.Service.TransitionCommand(ctx, identity, epoch, item.GetCommandId(), next, nil, errorCode)
			if err == nil && (command.CommandType == "host_connection_probe" || command.CommandType == "kubernetes_connection_probe") {
				_ = gateway.completeConnectionTest(ctx, command, "result_unknown", nil, "CONNECTOR_RESULT_NOT_REPLAYABLE")
			}
		}
		return nil
	case request.GetCredentialLeaseRequest() != nil:
		return nil
	case request.GetRemoteAccessOutput() != nil || request.GetRemoteAccessState() != nil || request.GetRemoteAccessClose() != nil:
		if gateway.RemoteAccess == nil {
			return ErrCommandState
		}
		return gateway.RemoteAccess.Deliver(identity.ConnectorID, epoch, request)
	case request.GetCertificateRotationRequest() != nil:
		return nil
	default:
		return ErrCommandState
	}
}

func (gateway Gateway) fulfillCredentialLease(ctx context.Context, identity TrustedIdentity, epoch int64, request *connectorv1.CredentialLeaseRequest) (*connectorv1.CredentialLeaseGrant, error) {
	if gateway.Credentials.Store == nil || request.GetConnectionEpoch() != uint64(epoch) || len(request.GetRecipientNonce()) < 16 || len(request.GetRecipientNonce()) > 64 {
		return nil, secret.ErrInvalidLease
	}
	leaseID, err := uuid.Parse(request.GetLeaseId())
	if err != nil {
		return nil, secret.ErrInvalidLease
	}
	command, err := gateway.Service.Store.Queries.GetConnectorCommand(ctx, db.GetConnectorCommandParams{CommandID: request.GetCommandId(), ConnectorID: identity.ConnectorID, ConnectionEpoch: epoch})
	commandID := request.GetCommandId()
	if err == nil {
		if !command.CredentialLeaseID.Valid || command.CredentialLeaseID.UUID != leaseID || (command.Status != "acknowledged" && command.Status != "running") {
			return nil, secret.ErrInvalidLease
		}
	} else {
		lease, leaseErr := gateway.Service.Store.Queries.GetCredentialLease(ctx, db.GetCredentialLeaseParams{ID: leaseID, EnterpriseID: identity.EnterpriseID})
		if leaseErr != nil || lease.TargetResourceType != "remote_access_session" || lease.OperationRef != commandID ||
			lease.RecipientType != "connector" || lease.RecipientID != identity.ConnectorID.String() || lease.Status != "active" {
			return nil, secret.ErrInvalidLease
		}
	}
	issued, err := gateway.Credentials.FulfillLease(ctx, identity.EnterpriseID, leaseID, "connector", identity.ConnectorID.String())
	if err != nil {
		return nil, err
	}
	return &connectorv1.CredentialLeaseGrant{LeaseId: leaseID.String(), CommandId: commandID, ConnectionEpoch: uint64(epoch),
		CredentialPayload: issued.Value, ExpiresAt: timestamppb.New(issued.Lease.ExpiresAt.Time), RecipientNonce: request.GetRecipientNonce()}, nil
}

func (gateway Gateway) completeConnectionTest(ctx context.Context, command db.ConnectorCommand, status string, typed *anypb.Any, errorCode string) error {
	if command.CommandType != "host_connection_probe" && command.CommandType != "kubernetes_connection_probe" {
		return nil
	}
	testID, err := uuid.Parse(command.OperationRef)
	if err != nil {
		return ErrCommandState
	}
	result := resource.ConnectionTestResult{}
	if status == "succeeded" {
		if command.CommandType == "host_connection_probe" {
			var value connectorv1.HostConnectionProbeResult
			if typed.UnmarshalTo(&value) != nil {
				return ErrCommandState
			}
			result.ResolvedIPs, result.HostKeyFingerprint, result.RemoteVersion, result.LatencyMS = value.ResolvedIps, value.HostKeyFingerprint, value.RemoteVersion, int64(value.LatencyMillis)
		} else {
			var value connectorv1.KubernetesConnectionProbeResult
			if typed.UnmarshalTo(&value) != nil {
				return ErrCommandState
			}
			result.RemoteVersion = value.ServerVersion
		}
	}
	encoded, _ := json.Marshal(result)
	_, err = gateway.Service.Store.Queries.CompleteConnectionTest(ctx, db.CompleteConnectionTestParams{ID: testID, EnterpriseID: command.EnterpriseID,
		Status: status, Result: encoded, ErrorCode: pgtype.Text{String: errorCode, Valid: errorCode != ""}})
	return err
}

func (gateway Gateway) trustedIdentity(ctx context.Context) (TrustedIdentity, db.Connector, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return TrustedIdentity{}, db.Connector{}, ErrConnectorFenced
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) != 1 {
		return TrustedIdentity{}, db.Connector{}, ErrConnectorFenced
	}
	certificate := tlsInfo.State.PeerCertificates[0]
	connectorID, err := connectorIDFromCertificate(certificate)
	if err != nil {
		return TrustedIdentity{}, db.Connector{}, err
	}
	value, err := gateway.Service.Store.Queries.GetConnectorByID(ctx, connectorID)
	if err != nil {
		return TrustedIdentity{}, db.Connector{}, ErrConnectorFenced
	}
	identity := TrustedIdentity{ConnectorID: connectorID, EnterpriseID: value.EnterpriseID, SerialNumber: certificate.SerialNumber.String()}
	return identity, value, nil
}

func connectorIDFromCertificate(certificate *x509.Certificate) (uuid.UUID, error) {
	if certificate == nil || len(certificate.URIs) != 1 || certificate.URIs[0].Scheme != "spiffe" || certificate.URIs[0].Host != "argus.io" {
		return uuid.Nil, ErrConnectorFenced
	}
	parts := strings.Split(strings.Trim(certificate.URIs[0].Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "connector" {
		return uuid.Nil, ErrConnectorFenced
	}
	value, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, ErrConnectorFenced
	}
	return value, nil
}

func typedCommand(command db.ConnectorCommand) (*connectorv1.ConnectorCommand, error) {
	var payload proto.Message
	switch command.CommandType {
	case "host_connection_probe":
		var plan struct {
			Address        string `json:"address"`
			Port           uint32 `json:"port"`
			Platform       string `json:"platform"`
			Username       string `json:"username"`
			ConnectionMode string `json:"connection_mode"`
		}
		if err := json.Unmarshal(command.Payload, &plan); err != nil || plan.Address == "" || plan.Port == 0 {
			return nil, ErrCommandState
		}
		protocol := "ssh"
		if strings.Contains(plan.ConnectionMode, "winrm") || (plan.ConnectionMode == "via_bastion" && plan.Platform == "windows") {
			protocol = "winrm"
		}
		payload = &connectorv1.HostConnectionProbe{Address: plan.Address, Port: plan.Port, Protocol: protocol, Username: plan.Username}
	case "kubernetes_connection_probe":
		var plan struct {
			Address string `json:"address"`
		}
		if err := json.Unmarshal(command.Payload, &plan); err != nil || plan.Address == "" {
			return nil, ErrCommandState
		}
		payload = &connectorv1.KubernetesConnectionProbe{ApiServer: plan.Address}
	case "kubernetes_resource_query":
		var query connectorv1.KubernetesResourceQuery
		if err := json.Unmarshal(command.Payload, &query); err != nil || query.ClusterId == "" || query.ResourceType == "" {
			return nil, ErrCommandState
		}
		payload = &query
	case "kubernetes_pod_logs":
		var query connectorv1.KubernetesPodLogsQuery
		if err := json.Unmarshal(command.Payload, &query); err != nil || query.ClusterId == "" || query.Namespace == "" || query.Pod == "" {
			return nil, ErrCommandState
		}
		payload = &query
	case "connector_uninstall":
		var request connectorv1.ConnectorUninstall
		if err := json.Unmarshal(command.Payload, &request); err != nil || request.ConnectorId == "" || request.ExpectedConnectionEpoch == 0 {
			return nil, ErrCommandState
		}
		payload = &request
	default:
		return nil, ErrCommandState
	}
	typed, err := anypb.New(payload)
	if err != nil {
		return nil, err
	}
	frame := &connectorv1.ConnectorCommand{CommandId: command.CommandID, ConnectionEpoch: uint64(command.ConnectionEpoch), CommandType: command.CommandType,
		PayloadHash: hex.EncodeToString(command.PayloadHash), ExpiresAt: timestamppb.New(command.ExpiresAt.Time), IdempotencyKey: command.IdempotencyKey,
		TypedPayload: typed, PayloadSchemaVersion: command.PayloadSchemaVersion, OperationRef: command.OperationRef}
	if command.CredentialLeaseID.Valid {
		frame.CredentialLeaseId = command.CredentialLeaseID.UUID.String()
	}
	return frame, nil
}

func resultTypeAllowed(commandType string, value *anypb.Any) bool {
	allowed := map[string]string{
		"host_connection_probe":       "type.googleapis.com/argus.connector.v1.HostConnectionProbeResult",
		"kubernetes_connection_probe": "type.googleapis.com/argus.connector.v1.KubernetesConnectionProbeResult",
		"kubernetes_resource_query":   "type.googleapis.com/argus.connector.v1.KubernetesResourceQueryResult",
		"kubernetes_pod_logs":         "type.googleapis.com/argus.connector.v1.KubernetesPodLogsResult",
		"connector_uninstall":         "type.googleapis.com/argus.connector.v1.ConnectorUninstallResult",
	}
	return value != nil && value.TypeUrl == allowed[commandType]
}

func validConnectorUninstallResult(value *anypb.Any) bool {
	var result connectorv1.ConnectorUninstallResult
	return value != nil && value.UnmarshalTo(&result) == nil && result.GetIdentityRemoved() && result.GetServiceStopped()
}

func reconciledCommandStatus(commandType, reported string) string {
	if commandType == "connector_uninstall" && reported == "succeeded" {
		return "result_unknown"
	}
	return reported
}

func (gateway Gateway) sendReconcile(stream connectorv1.ConnectorControlService_ConnectServer, identity TrustedIdentity, epoch int64, sequence *uint64) error {
	commands, err := gateway.Service.ListUncertainCommands(stream.Context(), identity, 64)
	if err != nil || len(commands) == 0 {
		return err
	}
	ids := make([]string, 0, len(commands))
	for _, command := range commands {
		ids = append(ids, command.CommandID)
	}
	if err := stream.Send(&connectorv1.ConnectResponse{Sequence: *sequence, Frame: &connectorv1.ConnectResponse_CommandReconcileRequest{
		CommandReconcileRequest: &connectorv1.CommandReconcileRequest{CommandIds: ids, ConnectionEpoch: uint64(epoch)}}}); err != nil {
		return err
	}
	*sequence++
	return nil
}

func (gateway Gateway) markUncertain(identity TrustedIdentity, epoch int64, values map[string]activeCommand) {
	for commandID, command := range values {
		status := "delivery_unknown"
		if command.acknowledged {
			status = "result_unknown"
		}
		_, _ = gateway.Service.TransitionCommand(context.Background(), identity, epoch, commandID, status, nil, "CONNECTOR_DISCONNECTED")
	}
}

func removeExpiredActiveCommands(now time.Time, active map[string]activeCommand, acknowledgements map[uint64]string) {
	for commandID, command := range active {
		if command.expiresAt.After(now) {
			continue
		}
		delete(active, commandID)
		for sequence, pendingCommandID := range acknowledgements {
			if pendingCommandID == commandID {
				delete(acknowledgements, sequence)
			}
		}
	}
}

func (gateway Gateway) close(stream connectorv1.ConnectorControlService_ConnectServer, sequence uint64, reason commonv1.CloseReason, code string) error {
	_ = stream.Send(&connectorv1.ConnectResponse{Sequence: sequence, Frame: &connectorv1.ConnectResponse_Close{Close: &commonv1.StreamClose{
		Reason: reason, Error: &commonv1.ErrorStatus{Code: code, MessageKey: "errors.connector.protocol", Retryable: false}}}})
	return fmt.Errorf("connector stream closed: %s", code)
}

func (gateway Gateway) heartbeatInterval() time.Duration {
	if gateway.HeartbeatInterval <= 0 {
		return 30 * time.Second
	}
	return gateway.HeartbeatInterval
}

func (gateway Gateway) dispatchInterval() time.Duration {
	if gateway.DispatchInterval <= 0 {
		return defaultDispatchEvery
	}
	return gateway.DispatchInterval
}

func normalizeResultStatus(value string) string {
	switch strings.ToLower(value) {
	case "succeeded", "success":
		return "succeeded"
	case "failed", "failure":
		return "failed"
	case "running":
		return "running"
	default:
		return "result_unknown"
	}
}

func LoadServerTLS(certPath, keyPath, caPath string) (*tls.Config, error) {
	certificate, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, err
	}
	caPEM, err := osReadFile(caPath)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, errors.New("connector client CA bundle is invalid")
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}, nil
}

var osReadFile = func(path string) ([]byte, error) { return os.ReadFile(path) }
