package modelprovider

import (
	"context"
	"net/netip"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (resolver staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return resolver[host], nil
}

func TestPublicEndpointPolicy(t *testing.T) {
	t.Parallel()
	policy := PublicEndpointPolicy{Resolver: staticResolver{
		"public.example":  {netip.MustParseAddr("8.8.8.8")},
		"private.example": {netip.MustParseAddr("10.0.0.2")},
	}}
	if _, err := policy.Validate(context.Background(), "https://public.example/v1"); err != nil {
		t.Fatalf("public endpoint rejected: %v", err)
	}
	for _, endpoint := range []string{"http://public.example/v1", "https://private.example/v1", "https://127.0.0.1/v1", "https://169.254.169.254/latest"} {
		if _, err := policy.Validate(context.Background(), endpoint); err == nil {
			t.Fatalf("endpoint %q was accepted", endpoint)
		}
	}
}
