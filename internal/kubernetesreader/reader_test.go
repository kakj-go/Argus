package kubernetesreader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"

	connectorv1 "github.com/kakj-go/Argus/internal/gen/proto/argus/connector/v1"
	"github.com/kakj-go/Argus/internal/resource"
)

type mutableResolver struct {
	addresses []netip.Addr
}

func (resolver *mutableResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return resolver.addresses, nil
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (body *trackedBody) Close() error {
	body.closed = true
	return nil
}

func TestRevalidatingTransportRejectsDNSRebindingAfterResponse(t *testing.T) {
	original := netip.MustParseAddr("8.8.8.8")
	resolver := &mutableResolver{addresses: []netip.Addr{original}}
	body := &trackedBody{Reader: strings.NewReader("ok")}
	transport := revalidatingTransport{
		validator: resource.DirectTargetValidator{Resolver: resolver},
		host:      "api.example.test",
		addresses: []netip.Addr{original},
		base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			resolver.addresses = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
			return &http.Response{StatusCode: http.StatusOK, Body: body}, nil
		}),
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.test/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, resource.ErrDirectTargetDenied) {
		t.Fatalf("expected DNS rebinding rejection, got %v", err)
	}
	if !body.closed {
		t.Fatal("expected rejected response body to be closed")
	}
}

func TestRevalidatingTransportRejectsChangedDNSBeforeRequest(t *testing.T) {
	original := netip.MustParseAddr("8.8.8.8")
	called := false
	transport := revalidatingTransport{
		validator: resource.DirectTargetValidator{Resolver: &mutableResolver{addresses: []netip.Addr{netip.MustParseAddr("1.1.1.1")}}},
		host:      "api.example.test",
		addresses: []netip.Addr{original},
		base: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return nil, errors.New("unexpected request")
		}),
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://api.example.test/api", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := transport.RoundTrip(request); !errors.Is(err, resource.ErrDirectTargetDenied) {
		t.Fatalf("expected DNS rebinding rejection, got %v", err)
	}
	if called {
		t.Fatal("expected request to be rejected before network dispatch")
	}
}

func TestKubernetesReaderRejectsRedirects(t *testing.T) {
	if err := rejectRedirect(nil, nil); !errors.Is(err, resource.ErrDirectTargetDenied) {
		t.Fatalf("expected redirect rejection, got %v", err)
	}
}

func TestUnmarshalConnectorResultAcceptsStoredTypedProjection(t *testing.T) {
	stored, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(&connectorv1.KubernetesResourceQueryResult{
		ResourcesJson: [][]byte{[]byte(`{"metadata":{"name":"default"}}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	var result connectorv1.KubernetesResourceQueryResult
	if err := unmarshalConnectorResult(stored, &result); err != nil {
		t.Fatalf("stored typed projection was rejected: %v", err)
	}
	if len(result.ResourcesJson) != 1 || !strings.Contains(string(result.ResourcesJson[0]), `"name":"default"`) {
		t.Fatalf("stored typed projection was not restored: %q", result.ResourcesJson)
	}
}

func TestUnmarshalConnectorResultRejectsLegacyAnyEnvelope(t *testing.T) {
	legacy := []byte(`{"@type":"type.googleapis.com/argus.connector.v1.KubernetesResourceQueryResult","resources_json":[]}`)
	var result connectorv1.KubernetesResourceQueryResult
	if err := unmarshalConnectorResult(legacy, &result); !errors.Is(err, resource.ErrKubernetesUnavailable) {
		t.Fatalf("legacy Any envelope was accepted: %v", err)
	}
}
