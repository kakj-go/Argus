// Package artifactcheck verifies that catalog-controlled release objects are
// actually downloadable before an onboarding action is accepted.
package artifactcheck

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kakj-go/Argus/internal/tlsmaterial"
)

var ErrUnavailable = errors.New("artifact is unavailable")

// Checker is deliberately small so domain services can fail closed without
// depending on a concrete object-store implementation.
type Checker interface {
	Check(context.Context, ...string) error
}

type HTTPChecker struct {
	client    *http.Client
	probeBase *url.URL
}

// NewHTTPChecker builds a bounded HEAD probe. probeBase may point at the
// cluster-internal Artifact Store; when set, only scheme/host are replaced and
// the catalog object's path is preserved. This avoids ingress hairpin/DNS
// dependencies while still checking the exact immutable object key.
func NewHTTPChecker(caPath, probeBase string) (*HTTPChecker, error) {
	material, err := tlsmaterial.Load(tlsmaterial.Options{CABundlePath: caPath})
	if err != nil {
		return nil, fmt.Errorf("load artifact trust bundle: %w", err)
	}
	transport, err := tlsmaterial.NewHTTPTransport(material, nil)
	if err != nil {
		return nil, err
	}
	var base *url.URL
	if strings.TrimSpace(probeBase) != "" {
		parsed, err := url.Parse(probeBase)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" {
			return nil, fmt.Errorf("artifact probe base must be an HTTP(S) origin: %q", probeBase)
		}
		base = parsed
	}
	return &HTTPChecker{client: &http.Client{Transport: transport, Timeout: 10 * time.Second}, probeBase: base}, nil
}

func (checker *HTTPChecker) Check(ctx context.Context, rawURLs ...string) error {
	if checker == nil || checker.client == nil {
		return ErrUnavailable
	}
	for _, rawURL := range rawURLs {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("%w: invalid release URL", ErrUnavailable)
		}
		probeURL := *parsed
		if checker.probeBase != nil {
			probeURL.Scheme = checker.probeBase.Scheme
			probeURL.Host = checker.probeBase.Host
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodHead, probeURL.String(), nil)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		response, err := checker.client.Do(request)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		_ = response.Body.Close()
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			return fmt.Errorf("%w: %s returned HTTP %d", ErrUnavailable, parsed.Host, response.StatusCode)
		}
	}
	return nil
}
