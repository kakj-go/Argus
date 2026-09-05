package connector

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	connectorcore "github.com/kakj-go/Argus/internal/connector"
	commonv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/common/v1"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/tlsmaterial"
	"github.com/kakj-go/Argus/internal/trustbundle"
)

var errIdentityRotated = errors.New("Connector identity rotated")

type connectorClient struct {
	store  localStore
	logger *slog.Logger
}

type receivedFrame struct {
	value *connectorv1.ConnectResponse
	err   error
}

type outboundFrame struct {
	value *connectorv1.ConnectRequest
	stop  bool
}

type completedCommand struct {
	command *connectorv1.ConnectorCommand
	outcome commandOutcome
}

type pendingLease struct {
	command *connectorv1.ConnectorCommand
	nonce   []byte
}

type pendingTunnelLease struct {
	desired *connectorv1.TelemetryTunnelDesired
	nonce   []byte
}

func (client connectorClient) run(ctx context.Context) error {
	if err := client.store.ensure(); err != nil {
		return err
	}
	backoff := time.Second
	for {
		err := client.runSession(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errIdentityRotated) {
			backoff = time.Second
			continue
		}
		client.logger.Warn("Connector session ended; reconnecting", "error", err, "backoff", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (client connectorClient) runSession(ctx context.Context) error {
	identity, err := client.store.loadIdentity()
	if err != nil {
		return fmt.Errorf("load Connector identity: %w", err)
	}
	transport, address, err := client.transportCredentials(identity.GatewayEndpoint)
	if err != nil {
		return err
	}
	connection, err := grpc.NewClient(address, grpc.WithTransportCredentials(transport),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(connectorcore.MaxMessageBytes), grpc.MaxCallSendMsgSize(connectorcore.MaxMessageBytes)))
	if err != nil {
		return err
	}
	defer connection.Close()
	stream, err := connectorv1.NewConnectorControlServiceClient(connection).Connect(ctx)
	if err != nil {
		return err
	}
	clientNonce := make([]byte, 32)
	if _, err := rand.Read(clientNonce); err != nil {
		return err
	}
	if err := stream.Send(&connectorv1.ConnectRequest{Sequence: 1, Frame: &connectorv1.ConnectRequest_Hello{Hello: &connectorv1.ConnectorHello{
		ProtocolVersion: connectorcore.ProtocolVersion, InstanceId: identity.InstanceID, SoftwareVersion: softwareVersion,
		Capabilities: identity.Capabilities, ClientNonce: clientNonce, TrustBundleEpoch: uint64(identity.TrustBundleEpoch),
		TrustBundleSha256: identity.TrustBundleSHA256, TrustBundleCaFingerprints: identity.TrustCAFingerprints}}}); err != nil {
		return err
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	welcome := first.GetWelcome()
	if first.GetSequence() != 1 || welcome == nil || welcome.GetProtocolVersion() != connectorcore.ProtocolVersion ||
		welcome.GetConnectionEpoch() == 0 || welcome.GetMaxMessageBytes() > connectorcore.MaxMessageBytes || len(welcome.GetServerNonce()) < 16 {
		return errors.New("Connector welcome is invalid")
	}
	if welcome.GetHeartbeatInterval() == nil {
		return errors.New("Connector heartbeat interval is missing")
	}
	heartbeat := welcome.GetHeartbeatInterval().AsDuration()
	if heartbeat < 5*time.Second || heartbeat > 5*time.Minute {
		return errors.New("Connector heartbeat interval is invalid")
	}
	client.logger.Info("Connector session established", "connector_id", identity.ConnectorID, "connection_epoch", welcome.GetConnectionEpoch())
	tunnels := newMemberTunnelSupervisor(connectorTelemetryTunnelLimit, connectorTunnelBytesPerSecond())
	defer tunnels.CloseAll()
	return client.sessionLoop(ctx, stream, identity, welcome, heartbeat, tunnels)
}

func (client connectorClient) sessionLoop(ctx context.Context, stream connectorv1.ConnectorControlService_ConnectClient,
	identity identityState, welcome *connectorv1.ConnectorWelcome, heartbeat time.Duration, tunnels *memberTunnelSupervisor) error {
	received := make(chan receivedFrame, 1)
	go receiveServerFrames(stream, received)
	outgoing := make(chan outboundFrame, 64)
	completed := make(chan completedCommand, 32)
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	results, err := client.store.loadResults()
	if err != nil {
		return err
	}
	clientSequence, serverSequence := uint64(1), uint64(1)
	epoch := welcome.GetConnectionEpoch()
	pending := map[string]pendingLease{}
	pendingTunnels := map[string]pendingTunnelLease{}
	remoteSessions := map[string]*localRemoteSession{}
	remotePending := map[string]*localRemoteSession{}
	remoteDone := make(chan string, 16)
	var active atomic.Int32
	var activeRemote atomic.Int32
	var rotationKey []byte
	var stopAfterAcknowledgement uint64
	if welcome.GetCertificateRotationRequested() || certificateNeedsRotation(client.store) {
		rotationKey, err = client.requestRotation(identity, epoch, outgoing)
		if err != nil {
			return err
		}
	}
	send := func(item outboundFrame) error {
		clientSequence++
		item.value.Sequence = clientSequence
		if err := stream.Send(item.value); err != nil {
			return err
		}
		if item.stop {
			stopAfterAcknowledgement = clientSequence
		}
		return nil
	}
	for {
		heartbeatChannel := ticker.C
		outgoingChannel := (<-chan outboundFrame)(outgoing)
		if stopAfterAcknowledgement != 0 {
			heartbeatChannel = nil
			outgoingChannel = nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeatChannel:
			// A Connector control stream can remain open longer than the 24-hour
			// client certificate lifetime. Re-evaluate rotation on every heartbeat
			// so a long-lived stream moves to the next issuer during CA overlap
			// instead of waiting for a reconnect that may happen after retirement.
			if len(rotationKey) == 0 && certificateNeedsRotation(client.store) {
				rotationKey, err = client.requestRotation(identity, epoch, outgoing)
				if err != nil {
					return err
				}
			}
			if err := send(outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_Heartbeat{Heartbeat: &connectorv1.ConnectorHeartbeat{
				ConnectionEpoch: epoch, SentAt: timestamppb.Now(), ActiveCommands: uint32(active.Load()),
				ActiveRemoteAccessStreams: uint32(activeRemote.Load()), TelemetryTunnels: tunnels.Snapshot(),
				TrustBundleEpoch: uint64(identity.TrustBundleEpoch), TrustBundleSha256: identity.TrustBundleSHA256,
				TrustBundleCaFingerprints: identity.TrustCAFingerprints}}}}); err != nil {
				return err
			}
		case item := <-outgoingChannel:
			if err := send(item); err != nil {
				return err
			}
		case execution := <-completed:
			active.Add(-1)
			record := outcomeRecord(execution.command, execution.outcome)
			if err := client.store.saveResult(record); err != nil {
				return err
			}
			results[execution.command.GetCommandId()] = record
			outgoing <- outboundFrame{value: commandResultFrame(execution.command, epoch, execution.outcome), stop: execution.outcome.stop}
		case streamID := <-remoteDone:
			if session, ok := remoteSessions[streamID]; ok {
				session.stop("completed")
				delete(remoteSessions, streamID)
				delete(remotePending, session.open.GetSessionId())
				activeRemote.Add(-1)
			}
		case receivedItem := <-received:
			if receivedItem.err != nil {
				return receivedItem.err
			}
			message := receivedItem.value
			if message.GetSequence() != serverSequence+1 || message.GetWelcome() != nil || message.GetRemoteAccessData() != nil {
				return errors.New("Connector server sequence or frame is invalid")
			}
			serverSequence = message.GetSequence()
			switch {
			case message.GetAcknowledge() != nil:
				acknowledged := message.GetAcknowledge().GetClientSequence()
				if acknowledged > clientSequence {
					return errors.New("Connector acknowledgement is invalid")
				}
				if stopSequenceAcknowledged(stopAfterAcknowledgement, acknowledged) {
					client.removeIdentity()
					return nil
				}
			case message.GetClose() != nil:
				return fmt.Errorf("Connector stream closed: %s", message.GetClose().GetError().GetCode())
			case message.GetCommandReconcileRequest() != nil:
				outgoing <- outboundFrame{value: reconcileFrame(message.GetCommandReconcileRequest(), epoch, results)}
			case message.GetCredentialLeaseGrant() != nil:
				grant := message.GetCredentialLeaseGrant()
				if value, ok := pendingTunnels[grant.GetCommandId()]; ok {
					if grant.GetConnectionEpoch() != epoch || grant.GetLeaseId() != value.desired.GetCredentialLeaseId() ||
						!strings.EqualFold(hex.EncodeToString(grant.GetRecipientNonce()), hex.EncodeToString(value.nonce)) ||
						grant.GetExpiresAt() == nil || time.Now().After(grant.GetExpiresAt().AsTime()) {
						return errors.New("Connector tunnel credential lease grant is invalid")
					}
					delete(pendingTunnels, grant.GetCommandId())
					credential := append([]byte(nil), grant.GetCredentialPayload()...)
					applyErr := tunnels.Apply(ctx, value.desired, credential)
					clear(credential)
					if applyErr != nil {
						outgoing <- outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_TelemetryTunnelStatusSet{
							TelemetryTunnelStatusSet: &connectorv1.TelemetryTunnelStatusSet{Tunnels: []*connectorv1.TelemetryTunnelStatus{{
								TunnelId: value.desired.GetTunnelId(), Epoch: value.desired.GetEpoch(), Fence: value.desired.GetFence(),
								Status: "down", DropReason: "tunnel_quota_exceeded",
							}}}}}}
					}
					continue
				}
				if value, ok := pending[grant.GetCommandId()]; ok {
					if grant.GetConnectionEpoch() != epoch || grant.GetLeaseId() != value.command.GetCredentialLeaseId() ||
						!strings.EqualFold(hex.EncodeToString(grant.GetRecipientNonce()), hex.EncodeToString(value.nonce)) || grant.GetExpiresAt() == nil || time.Now().After(grant.GetExpiresAt().AsTime()) {
						return errors.New("Connector credential lease grant is invalid")
					}
					delete(pending, grant.GetCommandId())
					credential := append([]byte(nil), grant.GetCredentialPayload()...)
					active.Add(1)
					go executeConnectorCommand(ctx, value.command, credential, completed)
					continue
				}
				remote, ok := remotePending[grant.GetCommandId()]
				if !ok || !remote.acceptGrant(grant, epoch) {
					return errors.New("Connector remote access credential lease grant is invalid")
				}
				delete(remotePending, grant.GetCommandId())
				activeRemote.Add(1)
				go remote.run(ctx, append([]byte(nil), grant.GetCredentialPayload()...), outgoing, remoteDone)
			case message.GetCertificateRotationGrant() != nil:
				if len(rotationKey) == 0 {
					return errors.New("unexpected Connector certificate rotation grant")
				}
				if err := client.acceptRotation(identity, message.GetCertificateRotationGrant(), rotationKey); err != nil {
					return err
				}
				return errIdentityRotated
			case message.GetTrustBundleUpdate() != nil:
				updated, acknowledgement, err := client.acceptTrustBundle(identity, message.GetTrustBundleUpdate())
				if err != nil {
					return err
				}
				identity = updated
				outgoing <- outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_TrustBundleAcknowledge{
					TrustBundleAcknowledge: acknowledgement}}}
			case message.GetCommand() != nil:
				command := message.GetCommand()
				if command.GetConnectionEpoch() != epoch || command.GetCommandId() == "" || command.GetTypedPayload() == nil {
					return errors.New("Connector command is invalid")
				}
				outgoing <- outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_Acknowledge{Acknowledge: &connectorv1.ClientAcknowledge{ServerSequence: serverSequence}}}}
				if record, ok := findRecordedResult(results, command); ok {
					outgoing <- outboundFrame{value: recordedResultFrame(command, epoch, record)}
					continue
				}
				running, err := emptyCommandResult(command.GetCommandType())
				if err != nil {
					return err
				}
				outgoing <- outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CommandResult{CommandResult: &connectorv1.CommandResult{
					CommandId: command.GetCommandId(), ConnectionEpoch: epoch, Status: "running", TypedResult: running,
					ResultSchemaVersion: "argus.connector_result/v1"}}}}
				if command.GetCredentialLeaseId() != "" {
					nonce := make([]byte, 32)
					if _, err := rand.Read(nonce); err != nil {
						return err
					}
					pending[command.GetCommandId()] = pendingLease{command: command, nonce: nonce}
					outgoing <- outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CredentialLeaseRequest{CredentialLeaseRequest: &connectorv1.CredentialLeaseRequest{
						LeaseId: command.GetCredentialLeaseId(), CommandId: command.GetCommandId(), ConnectionEpoch: epoch, RecipientNonce: nonce}}}}
				} else {
					active.Add(1)
					go executeConnectorCommand(ctx, command, nil, completed)
				}
			case message.GetTelemetryTunnelDesiredSet() != nil:
				desiredSet := message.GetTelemetryTunnelDesiredSet()
				if !desiredSet.GetFullSnapshot() || len(desiredSet.GetTunnels()) > connectorTelemetryTunnelLimit {
					return errors.New("Connector telemetry tunnel snapshot is invalid")
				}
				tunnels.ReconcileSnapshot(desiredSet.GetTunnels())
				desiredIDs := make(map[string]struct{}, len(desiredSet.GetTunnels()))
				for _, desired := range desiredSet.GetTunnels() {
					if err := validateTunnelDesired(desired); err != nil {
						return err
					}
					desiredIDs[desired.GetTunnelId()] = struct{}{}
					if tunnels.Has(desired.GetTunnelId(), desired.GetEpoch(), desired.GetFence()) {
						continue
					}
					operationRef := connectorTunnelOperationRef(desired)
					if pendingValue, ok := pendingTunnels[operationRef]; ok &&
						pendingValue.desired.GetCredentialLeaseId() == desired.GetCredentialLeaseId() {
						continue
					}
					nonce := make([]byte, 32)
					if _, err := rand.Read(nonce); err != nil {
						return err
					}
					pendingTunnels[operationRef] = pendingTunnelLease{desired: proto.Clone(desired).(*connectorv1.TelemetryTunnelDesired), nonce: nonce}
					outgoing <- outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CredentialLeaseRequest{
						CredentialLeaseRequest: &connectorv1.CredentialLeaseRequest{LeaseId: desired.GetCredentialLeaseId(),
							CommandId: operationRef, ConnectionEpoch: epoch, RecipientNonce: nonce}}}}
				}
				for operationRef, value := range pendingTunnels {
					if _, ok := desiredIDs[value.desired.GetTunnelId()]; ok {
						continue
					}
					clear(value.nonce)
					delete(pendingTunnels, operationRef)
				}
			case message.GetRemoteAccessOpen() != nil:
				open := message.GetRemoteAccessOpen()
				if open.GetConnectionEpoch() != epoch || open.GetStreamId() == "" || open.GetSessionId() == "" || open.GetCredentialLeaseId() == "" ||
					open.GetExpiresAt() == nil || time.Now().After(open.GetExpiresAt().AsTime()) || remoteSessions[open.GetStreamId()] != nil {
					return errors.New("Connector remote access open frame is invalid")
				}
				remote, err := newLocalRemoteSession(open)
				if err != nil {
					return err
				}
				remoteSessions[open.GetStreamId()] = remote
				remotePending[open.GetSessionId()] = remote
				outgoing <- outboundFrame{value: remote.leaseRequest(epoch)}
			case message.GetRemoteAccessInput() != nil || message.GetRemoteAccessResize() != nil || message.GetRemoteAccessClose() != nil:
				streamID := remoteServerStreamID(message)
				remote := remoteSessions[streamID]
				if remote == nil || !remote.deliver(message) {
					return errors.New("Connector remote access stream frame is invalid")
				}
			default:
				return errors.New("unsupported Connector server frame")
			}
		}
	}
}

func connectorTunnelOperationRef(desired *connectorv1.TelemetryTunnelDesired) string {
	return fmt.Sprintf("telemetry_tunnel:%s:%d:%d", desired.GetTunnelId(), desired.GetEpoch(), desired.GetFence())
}

func connectorTunnelBytesPerSecond() int64 {
	const defaultBytesPerSecond = 64 * 1024 * 1024
	value, err := strconv.ParseInt(os.Getenv("ARGUS_CONNECTOR_TUNNEL_BYTES_PER_SECOND"), 10, 64)
	if err != nil || value <= 0 {
		return defaultBytesPerSecond
	}
	return value
}

func stopSequenceAcknowledged(stopSequence, acknowledged uint64) bool {
	return stopSequence != 0 && acknowledged >= stopSequence
}

func receiveServerFrames(stream connectorv1.ConnectorControlService_ConnectClient, output chan<- receivedFrame) {
	for {
		value, err := stream.Recv()
		output <- receivedFrame{value: value, err: err}
		if err != nil {
			return
		}
	}
}

func executeConnectorCommand(ctx context.Context, command *connectorv1.ConnectorCommand, credential []byte, output chan<- completedCommand) {
	defer clear(credential)
	outcome := (commandExecutor{}).execute(ctx, command, credential)
	if outcome.code != "" {
		slog.Warn("Connector command failed", "command_id", command.GetCommandId(), "command_type", command.GetCommandType(), "error_code", outcome.code)
	}
	output <- completedCommand{command: command, outcome: outcome}
}

func (client connectorClient) transportCredentials(endpoint string) (credentials.TransportCredentials, string, error) {
	parsed, err := parseGatewayEndpoint(endpoint)
	if err != nil {
		return nil, "", err
	}
	material, err := tlsmaterial.Load(tlsmaterial.Options{
		CertificatePath: filepath.Join(client.store.directory, certFile), PrivateKeyPath: filepath.Join(client.store.directory, keyFile),
		CABundlePath: filepath.Join(client.store.directory, caFile), Usage: x509.ExtKeyUsageClientAuth,
	})
	if err != nil {
		return nil, "", err
	}
	tlsCredentials, err := tlsmaterial.ClientCredentials(material, parsed.Hostname())
	if err != nil {
		return nil, "", err
	}
	address := parsed.Host
	// E2E/debug override: dial a pinned address (for example the public
	// load-balancer IP) while TLS keeps verifying the endpoint hostname.
	if override := os.Getenv("ARGUS_CONNECTOR_DIAL_ADDRESS"); override != "" {
		address = override
	}
	return tlsCredentials, address, nil
}

func parseGatewayEndpoint(value string) (*url.URL, error) {
	if !strings.Contains(value, "://") {
		value = "grpcs://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "grpcs" || parsed.Hostname() == "" || parsed.Port() == "" || parsed.Path != "" {
		return nil, errors.New("Connector gateway endpoint must be grpcs://host:port")
	}
	return parsed, nil
}

func emptyCommandResult(commandType string) (*anypb.Any, error) {
	var value proto.Message
	switch commandType {
	case "host_connection_probe":
		value = &connectorv1.HostConnectionProbeResult{}
	case "kubernetes_connection_probe":
		value = &connectorv1.KubernetesConnectionProbeResult{}
	case "kubernetes_resource_query":
		value = &connectorv1.KubernetesResourceQueryResult{}
	case "kubernetes_pod_logs":
		value = &connectorv1.KubernetesPodLogsResult{}
	case "connector_uninstall":
		value = &connectorv1.ConnectorUninstallResult{}
	case "collector_management":
		value = &connectorv1.CollectorManagementResult{}
	default:
		return nil, errors.New("unsupported Connector command result type")
	}
	return anypb.New(value)
}

func commandResultFrame(command *connectorv1.ConnectorCommand, epoch uint64, outcome commandOutcome) *connectorv1.ConnectRequest {
	status := "succeeded"
	errorStatus := (*commonv1.ErrorStatus)(nil)
	result := outcome.result
	if outcome.code != "" {
		status = "failed"
		errorStatus = &commonv1.ErrorStatus{Code: outcome.code, MessageKey: "errors.connector.command_failed", Retryable: false}
		result, _ = emptyCommandResult(command.GetCommandType())
	}
	encoded, _ := proto.Marshal(result)
	digest := sha256.Sum256(encoded)
	return &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CommandResult{CommandResult: &connectorv1.CommandResult{CommandId: command.GetCommandId(),
		ConnectionEpoch: epoch, Status: status, ResultHash: hex.EncodeToString(digest[:]), Error: errorStatus,
		TypedResult: result, ResultSchemaVersion: "argus.connector_result/v1"}}}
}

func outcomeRecord(command *connectorv1.ConnectorCommand, outcome commandOutcome) commandRecord {
	status := "succeeded"
	result := outcome.result
	if outcome.code != "" {
		status = "failed"
		result, _ = emptyCommandResult(command.GetCommandType())
	}
	encoded, _ := proto.Marshal(result)
	return commandRecord{CommandID: command.GetCommandId(), IdempotencyKey: command.GetIdempotencyKey(), Status: status,
		ResultTypeURL: result.GetTypeUrl(), Result: encoded, ErrorCode: outcome.code}
}

func findRecordedResult(values map[string]commandRecord, command *connectorv1.ConnectorCommand) (commandRecord, bool) {
	if value, ok := values[command.GetCommandId()]; ok {
		return value, true
	}
	for _, value := range values {
		if command.GetIdempotencyKey() != "" && value.IdempotencyKey == command.GetIdempotencyKey() {
			return value, true
		}
	}
	return commandRecord{}, false
}

func recordedResultFrame(command *connectorv1.ConnectorCommand, epoch uint64, record commandRecord) *connectorv1.ConnectRequest {
	var typed anypb.Any
	if proto.Unmarshal(record.Result, &typed) != nil || typed.TypeUrl == "" {
		typed.TypeUrl, typed.Value = record.ResultTypeURL, nil
	}
	var errorStatus *commonv1.ErrorStatus
	if record.ErrorCode != "" {
		errorStatus = &commonv1.ErrorStatus{Code: record.ErrorCode, MessageKey: "errors.connector.command_failed", Retryable: false}
	}
	return &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CommandResult{CommandResult: &connectorv1.CommandResult{CommandId: command.GetCommandId(), ConnectionEpoch: epoch,
		Status: record.Status, ResultHash: record.ResultHash, Error: errorStatus, TypedResult: &typed, ResultSchemaVersion: "argus.connector_result/v1"}}}
}

func reconcileFrame(request *connectorv1.CommandReconcileRequest, epoch uint64, values map[string]commandRecord) *connectorv1.ConnectRequest {
	result := &connectorv1.CommandReconcileResult{ConnectionEpoch: epoch}
	for _, commandID := range request.GetCommandIds() {
		if value, ok := values[commandID]; ok {
			item := &connectorv1.ReconciledCommand{CommandId: commandID, Status: value.Status, ResultHash: value.ResultHash}
			if value.ErrorCode != "" {
				item.Error = &commonv1.ErrorStatus{Code: value.ErrorCode, MessageKey: "errors.connector.command_failed", Retryable: false}
			}
			result.Commands = append(result.Commands, item)
		}
	}
	return &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CommandReconcileResult{CommandReconcileResult: result}}
}

func certificateNeedsRotation(store localStore) bool {
	certificatePEM, _, _, err := store.identityMaterial()
	if err != nil {
		return false
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		return false
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	threshold := certificate.NotBefore.Add(certificate.NotAfter.Sub(certificate.NotBefore) * 2 / 3)
	return time.Now().After(threshold)
}

func (client connectorClient) requestRotation(identity identityState, epoch uint64, outgoing chan<- outboundFrame) ([]byte, error) {
	id, err := uuid.Parse(identity.ConnectorID)
	if err != nil {
		return nil, err
	}
	key, csr, err := connectorcore.GenerateCSR(id)
	if err != nil {
		return nil, err
	}
	keyPEM, err := connectorcore.MarshalPrivateKey(key)
	if err != nil {
		return nil, err
	}
	outgoing <- outboundFrame{value: &connectorv1.ConnectRequest{Frame: &connectorv1.ConnectRequest_CertificateRotationRequest{CertificateRotationRequest: &connectorv1.CertificateRotationRequest{
		ConnectionEpoch: epoch, CsrPem: csr}}}}
	return keyPEM, nil
}

func (client connectorClient) acceptRotation(identity identityState, grant *connectorv1.CertificateRotationGrant, keyPEM []byte) error {
	if grant.GetConnectionEpoch() == 0 || len(grant.GetCertificatePem()) == 0 || grant.GetNotAfter() == nil {
		return errors.New("Connector certificate rotation grant is invalid")
	}
	id, err := uuid.Parse(identity.ConnectorID)
	if err != nil {
		return err
	}
	_, _, caBundle, err := client.store.identityMaterial()
	if err != nil {
		return err
	}
	if err := validateIssuedIdentity(id, keyPEM, grant.GetCertificatePem(), caBundle); err != nil {
		return err
	}
	identity.CertificateExpiresAt = grant.GetNotAfter().AsTime()
	return client.store.saveIdentity(identity, keyPEM, grant.GetCertificatePem(), caBundle)
}

func (client connectorClient) acceptTrustBundle(identity identityState, update *connectorv1.TrustBundleUpdate) (identityState, *connectorv1.TrustBundleAcknowledge, error) {
	if update == nil || update.GetEpoch() < uint64(identity.TrustBundleEpoch) || update.GetEpoch() == 0 ||
		update.GetStartedAt() == nil || !update.GetStartedAt().IsValid() {
		return identity, nil, errors.New("Connector Trust Bundle update metadata is invalid")
	}
	if update.GetState() != trustbundle.StateStable && update.GetState() != trustbundle.StatePreparing &&
		update.GetState() != trustbundle.StateOverlapping && update.GetState() != trustbundle.StateRetiring {
		return identity, nil, errors.New("Connector Trust Bundle update state is invalid")
	}
	if (update.GetState() == trustbundle.StateOverlapping || update.GetState() == trustbundle.StateRetiring) &&
		(update.GetRetireAt() == nil || !update.GetRetireAt().IsValid()) {
		return identity, nil, errors.New("Connector Trust Bundle retirement deadline is invalid")
	}
	material, err := trustbundle.Parse(update.GetBundlePem(), time.Now().UTC())
	if err != nil {
		return identity, nil, err
	}
	expectedFingerprints := append(append([]string{}, update.GetCurrentCaFingerprints()...), update.GetNextCaFingerprints()...)
	expected := trustbundle.Bundle{Epoch: int64(update.GetEpoch()), Material: material}
	if !expected.Matches(trustbundle.Acknowledgement{Epoch: int64(update.GetEpoch()), SHA256: update.GetBundleSha256(), Fingerprints: expectedFingerprints}) ||
		len(update.GetCurrentCaFingerprints()) == 0 {
		return identity, nil, errors.New("Connector Trust Bundle update digest or fingerprints are invalid")
	}
	identity.TrustBundleEpoch = int64(update.GetEpoch())
	identity.TrustBundleSHA256 = material.SHA256
	identity.TrustCAFingerprints = material.Fingerprints
	if err := client.store.saveTrustBundle(identity, material.PEM); err != nil {
		return identity, nil, err
	}
	return identity, &connectorv1.TrustBundleAcknowledge{Epoch: update.GetEpoch(), BundleSha256: material.SHA256,
		CaFingerprints: material.Fingerprints}, nil
}

func (client connectorClient) removeIdentity() {
	for _, name := range []string{identityFile, keyFile, certFile, caFile, resultsFile} {
		_ = os.Remove(filepath.Join(client.store.directory, name))
	}
}
