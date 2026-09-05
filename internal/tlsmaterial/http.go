package tlsmaterial

import (
	"crypto/tls"
	"net/http"
	"sync"
)

// HTTPTransport reloads the CA Bundle (and optional client identity) before a
// request opens a connection. A changed valid snapshot atomically replaces the
// underlying Transport; invalid projected updates leave the previous one live.
type HTTPTransport struct {
	material *Material
	base     *http.Transport
	mu       sync.Mutex
	hash     [32]byte
	current  *http.Transport
}

func NewHTTPTransport(material *Material, base *http.Transport) (*HTTPTransport, error) {
	if _, err := material.validSnapshot(); err != nil {
		return nil, err
	}
	if base == nil {
		base = http.DefaultTransport.(*http.Transport)
	}
	transport := &HTTPTransport{material: material, base: base.Clone()}
	current := material.latestSnapshot()
	transport.current = transport.transportFor(current)
	transport.hash = current.hash
	return transport, nil
}

func (transport *HTTPTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	current := transport.material.latestSnapshot()
	transport.mu.Lock()
	if transport.current == nil || transport.hash != current.hash {
		previous := transport.current
		transport.current = transport.transportFor(current)
		transport.hash = current.hash
		if previous != nil {
			previous.CloseIdleConnections()
		}
	}
	active := transport.current
	transport.mu.Unlock()
	return active.RoundTrip(request)
}

func (transport *HTTPTransport) CloseIdleConnections() {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.current != nil {
		transport.current.CloseIdleConnections()
	}
}

func (transport *HTTPTransport) transportFor(current *snapshot) *http.Transport {
	result := transport.base.Clone()
	configuration := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: current.roots}
	if current.certificate != nil {
		configuration.Certificates = []tls.Certificate{*current.certificate}
	}
	result.TLSClientConfig = configuration
	return result
}
