package modelprovider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var ErrEndpointNotAllowed = errors.New("model endpoint is not allowed")

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type PublicEndpointPolicy struct {
	Resolver    Resolver
	Dialer      net.Dialer
	DeniedCIDRs []netip.Prefix
}

func (policy PublicEndpointPolicy) Validate(ctx context.Context, rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return nil, ErrEndpointNotAllowed
	}
	addresses, err := policy.lookup(ctx, parsed.Hostname())
	if err != nil || len(addresses) == 0 {
		return nil, fmt.Errorf("%w: DNS resolution failed", ErrEndpointNotAllowed)
	}
	for _, address := range addresses {
		if !policy.allowedHost(parsed.Hostname(), address) {
			return nil, fmt.Errorf("%w: address %s", ErrEndpointNotAllowed, address)
		}
	}
	return parsed, nil
}

func (policy PublicEndpointPolicy) Client() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	configureE2EReplayTLS(transport)
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := policy.lookup(ctx, host)
		if err != nil || len(addresses) == 0 {
			return nil, ErrEndpointNotAllowed
		}
		for _, candidate := range addresses {
			if !policy.allowedHost(host, candidate) {
				return nil, ErrEndpointNotAllowed
			}
		}
		return policy.Dialer.DialContext(ctx, network, net.JoinHostPort(addresses[0].String(), port))
	}
	return &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many model endpoint redirects")
			}
			_, err := policy.Validate(request.Context(), request.URL.String())
			return err
		},
	}
}

func (policy PublicEndpointPolicy) allowedHost(host string, address netip.Addr) bool {
	return isE2EReplayHost(host) || policy.allowed(address)
}

func (policy PublicEndpointPolicy) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return []netip.Addr{address.Unmap()}, nil
	}
	resolver := policy.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return resolver.LookupNetIP(ctx, "ip", host)
}

func (policy PublicEndpointPolicy) allowed(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() || address.IsUnspecified() {
		return false
	}
	for _, prefix := range append(defaultDeniedPrefixes(), policy.DeniedCIDRs...) {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func defaultDeniedPrefixes() []netip.Prefix {
	values := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "fc00::/7", "fe80::/10", "ff00::/8",
	}
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		prefixes = append(prefixes, netip.MustParsePrefix(value))
	}
	return prefixes
}
