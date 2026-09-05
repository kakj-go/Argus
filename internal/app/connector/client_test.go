package connector

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kakj-go/Argus/internal/collectormanager"
	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/telemetrybinding"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEnrollmentEndpointRequiresHTTPSOutsideLoopback(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		want      string
		wantError bool
	}{
		{name: "https base", input: "https://control.example.test", want: "https://control.example.test/api/v1/connectors/enroll"},
		{name: "existing path", input: "https://control.example.test/api/v1/connectors/enroll", want: "https://control.example.test/api/v1/connectors/enroll"},
		{name: "loopback http", input: "http://127.0.0.1:8080", want: "http://127.0.0.1:8080/api/v1/connectors/enroll"},
		{name: "remote http", input: "http://control.example.test", wantError: true},
		{name: "missing host", input: "https://", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := enrollmentEndpoint(test.input)
			if test.wantError {
				if err == nil {
					t.Fatalf("expected %q to be rejected", test.input)
				}
				return
			}
			if err != nil || value != test.want {
				t.Fatalf("endpoint=%q err=%v, want %q", value, err, test.want)
			}
		})
	}
}

func TestPinnedEnrollmentAddressOnlyOverridesDialing(t *testing.T) {
	transport := pinnedAddressTransport("127.0.0.1:8443")
	if transport.DialContext == nil || transport.TLSClientConfig != nil {
		t.Fatalf("pinned base transport must leave TLS identity to tlsmaterial: %#v", transport)
	}
}

func TestParseGatewayEndpointRequiresGRPCSTarget(t *testing.T) {
	for _, value := range []string{"gateway.example.test:9443", "grpcs://gateway.example.test:9443"} {
		parsed, err := parseGatewayEndpoint(value)
		if err != nil || parsed.Host != "gateway.example.test:9443" {
			t.Fatalf("parse %q: endpoint=%v err=%v", value, parsed, err)
		}
	}
	for _, value := range []string{"grpc://gateway.example.test:9443", "grpcs://gateway.example.test", "grpcs://gateway.example.test:9443/path"} {
		if _, err := parseGatewayEndpoint(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestLocalStoreUsesPrivateAtomicFilesAndPrunesResults(t *testing.T) {
	store := localStore{directory: filepath.Join(t.TempDir(), "connector")}
	identity := identityState{ConnectorID: "018f47e2-9a4c-7b31-8acd-02a2475e8d2f", Role: "bastion", InstanceID: "instance-1",
		Name: "bastion-1", GatewayEndpoint: "grpcs://gateway.example.test:9443", CertificateExpiresAt: time.Now().Add(time.Hour),
		Capabilities: []string{"host.connection_probe"}}
	if err := store.saveIdentity(identity, []byte("key"), []byte("certificate"), []byte("ca")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{identityFile, keyFile, certFile, caFile} {
		info, err := os.Stat(filepath.Join(store.directory, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o, want 600", name, info.Mode().Perm())
		}
		if _, err := os.Stat(filepath.Join(store.directory, name+".tmp")); !os.IsNotExist(err) {
			t.Fatalf("temporary file for %s was not removed", name)
		}
	}
	seed, err := json.Marshal(map[string]commandRecord{
		"old": {CommandID: "old", UpdatedAt: time.Now().Add(-25 * time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicPrivateWrite(filepath.Join(store.directory, resultsFile), seed); err != nil {
		t.Fatal(err)
	}
	if err := store.saveResult(commandRecord{CommandID: "current", IdempotencyKey: "idem-1", Status: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	results, err := store.loadResults()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := results["old"]; ok {
		t.Fatal("expired command result was retained")
	}
	if _, ok := findRecordedResult(results, &connectorv1.ConnectorCommand{CommandId: "retry", IdempotencyKey: "idem-1"}); !ok {
		t.Fatal("idempotency-key retry did not reuse the recorded result")
	}
}

func TestCertificateNeedsRotationAtTwoThirdsTTL(t *testing.T) {
	writeCertificate := func(t *testing.T, notBefore, notAfter time.Time) localStore {
		t.Helper()
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: notBefore, NotAfter: notAfter}
		encoded, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		store := localStore{directory: t.TempDir()}
		if err := store.ensure(); err != nil {
			t.Fatal(err)
		}
		certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: encoded})
		for name, value := range map[string][]byte{certFile: certificate, keyFile: []byte("key"), caFile: []byte("ca")} {
			if err := atomicPrivateWrite(filepath.Join(store.directory, name), value); err != nil {
				t.Fatal(err)
			}
		}
		return store
	}
	now := time.Now()
	if certificateNeedsRotation(writeCertificate(t, now.Add(-10*time.Minute), now.Add(50*time.Minute))) {
		t.Fatal("fresh certificate requested rotation before two thirds of its TTL")
	}
	if !certificateNeedsRotation(writeCertificate(t, now.Add(-50*time.Minute), now.Add(10*time.Minute))) {
		t.Fatal("certificate did not request rotation after two thirds of its TTL")
	}
}

func TestCommandExecutorRejectsInvalidTypesAndAcceptsUninstall(t *testing.T) {
	expires := timestamppb.New(time.Now().Add(time.Minute))
	unknown, err := anypb.New(&connectorv1.ConnectorUninstall{})
	if err != nil {
		t.Fatal(err)
	}
	outcome := (commandExecutor{}).execute(context.Background(), &connectorv1.ConnectorCommand{
		CommandId: "command-1", CommandType: "arbitrary_shell", ExpiresAt: expires, TypedPayload: unknown,
	}, nil)
	if outcome.code != "CONNECTOR_COMMAND_FAILED" || outcome.stop {
		t.Fatalf("unexpected unknown-command outcome: %+v", outcome)
	}
	uninstall, err := anypb.New(&connectorv1.ConnectorUninstall{})
	if err != nil {
		t.Fatal(err)
	}
	outcome = (commandExecutor{}).execute(context.Background(), &connectorv1.ConnectorCommand{
		CommandId: "command-2", CommandType: "connector_uninstall", ExpiresAt: expires, TypedPayload: uninstall,
	}, nil)
	if outcome.code != "" || !outcome.stop || outcome.result == nil {
		t.Fatalf("unexpected uninstall outcome: %+v", outcome)
	}
	expired := (commandExecutor{}).execute(context.Background(), &connectorv1.ConnectorCommand{
		CommandId: "command-3", CommandType: "connector_uninstall", ExpiresAt: timestamppb.New(time.Now().Add(-time.Second)), TypedPayload: uninstall,
	}, nil)
	if expired.code != "CONNECTOR_COMMAND_INVALID" {
		t.Fatalf("expired command code=%q", expired.code)
	}
}

func TestCollectorManagementUsesConvergenceTimeout(t *testing.T) {
	if got := timeoutForCommand("host_connection_probe"); got != 45*time.Second {
		t.Fatalf("host command timeout=%s, want 45s", got)
	}
	if got := timeoutForCommand("collector_management"); got != 3*time.Minute {
		t.Fatalf("Collector command timeout=%s, want 3m", got)
	}
}

func TestCollectorManagementFailureCodesAreStableAndSanitized(t *testing.T) {
	for name, test := range map[string]struct {
		err  error
		code string
	}{
		"invalid":  {collectormanager.ErrInvalidCommand, "COLLECTOR_COMMAND_INVALID"},
		"artifact": {collectormanager.ErrArtifactInvalid, "COLLECTOR_ARTIFACT_INVALID"},
		"evidence": {telemetrybinding.ErrInvalidNodeEvidence, "COLLECTOR_NODE_EVIDENCE_INVALID"},
		"timeout":  {context.DeadlineExceeded, "COLLECTOR_HEALTH_CHECK_FAILED"},
	} {
		t.Run(name, func(t *testing.T) {
			if got := collectorManagementFailureCode(test.err); got != test.code {
				t.Fatalf("failure code=%q, want %q", got, test.code)
			}
		})
	}
}

func TestStopSequenceRequiresAcknowledgementOfUninstallResult(t *testing.T) {
	for _, test := range []struct {
		name         string
		stopSequence uint64
		acknowledged uint64
		want         bool
	}{
		{name: "not stopping", stopSequence: 0, acknowledged: 12, want: false},
		{name: "earlier frame", stopSequence: 12, acknowledged: 11, want: false},
		{name: "uninstall result", stopSequence: 12, acknowledged: 12, want: true},
		{name: "later cumulative acknowledgement", stopSequence: 12, acknowledged: 13, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := stopSequenceAcknowledged(test.stopSequence, test.acknowledged); got != test.want {
				t.Fatalf("stopSequenceAcknowledged(%d, %d)=%v, want %v", test.stopSequence, test.acknowledged, got, test.want)
			}
		})
	}
}
