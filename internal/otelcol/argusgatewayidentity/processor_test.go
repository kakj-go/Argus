package argusgatewayidentity

import (
	"context"
	"testing"

	"go.opentelemetry.io/collector/client"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

type testAuthData map[string]any

func (data testAuthData) GetAttribute(key string) any { return data[key] }

func (data testAuthData) GetAttributeNames() []string {
	result := make([]string, 0, len(data))
	for key := range data {
		result = append(result, key)
	}
	return result
}

func TestIdentityFromContextRequiresCompleteAuthenticatedPeer(t *testing.T) {
	if _, err := identityFromContext(context.Background()); err == nil {
		t.Fatal("missing downstream authentication accepted")
	}
	ctx := client.NewContext(context.Background(), client.Info{Auth: testAuthData{
		"argus.telemetry.collector_id": "collector-id",
	}})
	if _, err := identityFromContext(ctx); err == nil {
		t.Fatal("incomplete downstream authentication accepted")
	}
	ctx = client.NewContext(context.Background(), client.Info{Auth: testAuthData{
		"argus.telemetry.collector_id":       "collector-id",
		"argus.telemetry.certificate_serial": "00ab",
	}})
	identity, err := identityFromContext(ctx)
	if err != nil || identity.collectorID != "collector-id" || identity.serial != "00ab" {
		t.Fatalf("authenticated downstream identity rejected: %#v %v", identity, err)
	}
}

func TestOverwriteRemovesForgedArgusAttributes(t *testing.T) {
	attributes := pcommon.NewMap()
	attributes.PutStr("argus.enterprise.id", "forged")
	attributes.PutStr("argus.downstream.collector.id", "also-forged")
	attributes.PutStr("service.name", "checkout")
	overwrite(attributes, downstreamIdentity{collectorID: "trusted-collector", serial: "00ff"})

	if value, ok := attributes.Get("argus.enterprise.id"); ok {
		t.Fatalf("forged enterprise identity remains: %q", value.Str())
	}
	if value, ok := attributes.Get("argus.downstream.collector.id"); !ok || value.Str() != "trusted-collector" {
		t.Fatalf("trusted Collector identity missing: %#v %v", value, ok)
	}
	if value, ok := attributes.Get("argus.downstream.certificate.serial"); !ok || value.Str() != "00ff" {
		t.Fatalf("trusted certificate serial missing: %#v %v", value, ok)
	}
	if value, ok := attributes.Get("service.name"); !ok || value.Str() != "checkout" {
		t.Fatalf("non-Argus attribute was removed: %#v %v", value, ok)
	}
}
