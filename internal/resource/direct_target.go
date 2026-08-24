package resource

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

var ErrDirectTargetDenied = errors.New("direct target denied")

type IPResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type DirectTargetValidator struct {
	Resolver    IPResolver
	DeniedCIDRs []netip.Prefix
}

func ParseDeniedCIDRs(values []string) ([]netip.Prefix, error) {
	result := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("parse denied CIDR %q: %w", value, err)
		}
		result = append(result, prefix.Masked())
	}
	return result, nil
}

func (validator DirectTargetValidator) Resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if host == "" {
		return nil, ErrDirectTargetDenied
	}
	if address, err := netip.ParseAddr(host); err == nil {
		address = address.Unmap()
		if validator.denied(address) {
			return nil, ErrDirectTargetDenied
		}
		return []netip.Addr{address}, nil
	}
	resolver := validator.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, ErrDirectTargetDenied
	}
	result := make([]netip.Addr, 0, len(addresses))
	seen := map[netip.Addr]bool{}
	for _, address := range addresses {
		address = address.Unmap()
		if validator.denied(address) {
			return nil, ErrDirectTargetDenied
		}
		if !seen[address] {
			seen[address] = true
			result = append(result, address)
		}
	}
	return result, nil
}

func (validator DirectTargetValidator) ResolveHTTPS(ctx context.Context, rawURL string) (*url.URL, []netip.Addr, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, nil, ErrDirectTargetDenied
	}
	addresses, err := validator.Resolve(ctx, parsed.Hostname())
	if err != nil {
		return nil, nil, err
	}
	return parsed, addresses, nil
}

func (validator DirectTargetValidator) Revalidate(ctx context.Context, host string, allowed []netip.Addr) error {
	current, err := validator.Resolve(ctx, host)
	if err != nil {
		return err
	}
	allowedSet := make(map[netip.Addr]bool, len(allowed))
	for _, address := range allowed {
		allowedSet[address.Unmap()] = true
	}
	for _, address := range current {
		if !allowedSet[address.Unmap()] {
			return ErrDirectTargetDenied
		}
	}
	return nil
}

func (validator DirectTargetValidator) denied(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return true
	}
	for _, metadata := range []netip.Prefix{
		netip.MustParsePrefix("169.254.169.254/32"),
		netip.MustParsePrefix("100.100.100.200/32"),
		netip.MustParsePrefix("fd00:ec2::254/128"),
	} {
		if metadata.Contains(address) {
			return true
		}
	}
	for _, prefix := range validator.DeniedCIDRs {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}
