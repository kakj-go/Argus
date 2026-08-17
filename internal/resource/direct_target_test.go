package resource

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

type staticResolver map[string][]netip.Addr

func (resolver staticResolver) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	return resolver[host], nil
}

func TestDirectTargetRejectsInternalAndMetadataAddresses(t *testing.T) {
	validator := DirectTargetValidator{Resolver: staticResolver{
		"private.example":  {netip.MustParseAddr("10.0.0.8")},
		"metadata.example": {netip.MustParseAddr("169.254.169.254")},
		"mixed.example":    {netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("fd00::1")},
	}}
	for _, host := range []string{"127.0.0.1", "::1", "private.example", "metadata.example", "mixed.example"} {
		if _, err := validator.Resolve(context.Background(), host); !errors.Is(err, ErrDirectTargetDenied) {
			t.Fatalf("expected %s to be denied, got %v", host, err)
		}
	}
}

func TestDirectTargetDetectsDNSRebinding(t *testing.T) {
	resolver := staticResolver{"public.example": {netip.MustParseAddr("8.8.8.8")}}
	validator := DirectTargetValidator{Resolver: resolver}
	allowed, err := validator.Resolve(context.Background(), "public.example")
	if err != nil {
		t.Fatal(err)
	}
	resolver["public.example"] = []netip.Addr{netip.MustParseAddr("1.1.1.1")}
	if err := validator.Revalidate(context.Background(), "public.example", allowed); !errors.Is(err, ErrDirectTargetDenied) {
		t.Fatalf("expected DNS rebinding rejection, got %v", err)
	}
}

func TestDirectKubernetesRequiresHTTPS(t *testing.T) {
	validator := DirectTargetValidator{Resolver: staticResolver{"api.example": {netip.MustParseAddr("8.8.8.8")}}}
	if _, _, err := validator.ResolveHTTPS(context.Background(), "http://api.example"); !errors.Is(err, ErrDirectTargetDenied) {
		t.Fatalf("expected HTTP rejection, got %v", err)
	}
	if _, addresses, err := validator.ResolveHTTPS(context.Background(), "https://api.example:6443"); err != nil || len(addresses) != 1 {
		t.Fatalf("expected public HTTPS target, addresses=%v err=%v", addresses, err)
	}
}
