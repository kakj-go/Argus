package sandboxsmoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Options struct {
	BaseURL      string
	APIKey       string
	SandboxImage string
	Timeout      time.Duration
	PollInterval time.Duration
	HTTPClient   *http.Client
}

type lifecycleStatus struct {
	State   string `json:"state"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

type sandboxInfo struct {
	ID     string          `json:"id"`
	Status lifecycleStatus `json:"status"`
}

type endpointInfo struct {
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
}

func Run(ctx context.Context, options Options) error {
	if strings.TrimSpace(options.BaseURL) == "" || strings.TrimSpace(options.APIKey) == "" {
		return fmt.Errorf("OpenSandbox base URL and API key are required")
	}
	if options.SandboxImage == "" {
		options.SandboxImage = "busybox:1.37.0"
	}
	if options.Timeout <= 0 {
		options.Timeout = 4 * time.Minute
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 2 * time.Second
	}
	if options.HTTPClient == nil {
		options.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	baseURL := strings.TrimRight(options.BaseURL, "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	requestBody := map[string]any{
		"image":          map[string]string{"uri": options.SandboxImage},
		"timeout":        int(options.Timeout.Seconds()),
		"resourceLimits": map[string]string{"cpu": "100m", "memory": "128Mi"},
		"entrypoint":     []string{"tail", "-f", "/dev/null"},
		"metadata":       map[string]string{"argus.io/purpose": "installation-verification"},
	}
	var sandbox sandboxInfo
	if err := doJSON(ctx, options, http.MethodPost, baseURL+"/sandboxes", requestBody, nil, &sandbox); err != nil {
		return fmt.Errorf("create sandbox: %w", err)
	}
	if sandbox.ID == "" {
		return fmt.Errorf("create sandbox: response did not include an ID")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = doJSON(cleanupCtx, options, http.MethodDelete, baseURL+"/sandboxes/"+url.PathEscape(sandbox.ID), nil, nil, nil)
	}()

	deadline := time.Now().Add(options.Timeout)
	var lastProbeErr error
	for {
		if err := doJSON(ctx, options, http.MethodGet, baseURL+"/sandboxes/"+url.PathEscape(sandbox.ID), nil, nil, &sandbox); err != nil {
			return fmt.Errorf("read sandbox %s: %w", sandbox.ID, err)
		}
		switch sandbox.Status.State {
		case "Running":
			if err := probeExecd(ctx, options, baseURL, sandbox.ID); err == nil {
				return nil
			} else {
				lastProbeErr = err
			}
		case "Failed", "Terminated":
			return fmt.Errorf("sandbox %s entered %s: %s %s", sandbox.ID, sandbox.Status.State, sandbox.Status.Reason, sandbox.Status.Message)
		}
		if time.Now().After(deadline) {
			if lastProbeErr != nil {
				return fmt.Errorf("sandbox %s reached Running but execd did not become ready before timeout: %w", sandbox.ID, lastProbeErr)
			}
			return fmt.Errorf("sandbox %s did not reach Running before timeout; last state %s", sandbox.ID, sandbox.Status.State)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(options.PollInterval):
		}
	}
}

func probeExecd(ctx context.Context, options Options, baseURL, sandboxID string) error {
	endpointURL := fmt.Sprintf("%s/sandboxes/%s/endpoints/44772?use_server_proxy=true", baseURL, url.PathEscape(sandboxID))
	var endpoint endpointInfo
	if err := doJSON(ctx, options, http.MethodGet, endpointURL, nil, nil, &endpoint); err != nil {
		return fmt.Errorf("resolve execd endpoint: %w", err)
	}
	if endpoint.Endpoint == "" {
		return fmt.Errorf("resolve execd endpoint: response did not include an endpoint")
	}
	if !strings.HasPrefix(endpoint.Endpoint, "http://") && !strings.HasPrefix(endpoint.Endpoint, "https://") {
		endpoint.Endpoint = "http://" + endpoint.Endpoint
	}
	headers := endpoint.Headers
	if headers == nil {
		headers = map[string]string{}
	}
	if headers["X-EXECD-ACCESS-TOKEN"] == "" {
		headers["X-EXECD-ACCESS-TOKEN"] = options.APIKey
	}
	if err := doJSON(ctx, options, http.MethodGet, strings.TrimRight(endpoint.Endpoint, "/")+"/ping", nil, headers, nil); err != nil {
		return fmt.Errorf("probe execd: %w", err)
	}
	return nil
}

func doJSON(ctx context.Context, options Options, method, endpoint string, body any, headers map[string]string, output any) error {
	var requestBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return err
		}
		requestBody = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return err
	}
	request.Header.Set("OPEN-SANDBOX-API-KEY", options.APIKey)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := options.HTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("HTTP %s: %s", response.Status, strings.TrimSpace(string(data)))
	}
	if output != nil && len(data) > 0 {
		if err := json.Unmarshal(data, output); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
