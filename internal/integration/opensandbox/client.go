package opensandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiKeyHeader = "OPEN-SANDBOX-API-KEY"

type Sandbox struct {
	ID        string            `json:"id"`
	Status    Status            `json:"status"`
	ExpiresAt time.Time         `json:"expiresAt"`
	CreatedAt time.Time         `json:"createdAt"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type Status struct {
	State   string `json:"state"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type CreateRequest struct {
	Image          ImageSpec         `json:"image"`
	Timeout        int               `json:"timeout"`
	ResourceLimits map[string]string `json:"resourceLimits"`
	Metadata       map[string]string `json:"metadata"`
	NetworkPolicy  map[string]any    `json:"networkPolicy,omitempty"`
}

type ImageSpec struct {
	URI string `json:"uri"`
}

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

type APIError struct {
	StatusCode int
}

func (err APIError) Error() string {
	return fmt.Sprintf("OpenSandbox lifecycle returned %d", err.StatusCode)
}

func IsNotFound(err error) bool {
	var apiError APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound
}

func NewClient(endpoint, apiKey string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("invalid OpenSandbox endpoint")
	}
	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/v1") {
		path += "/v1"
	}
	parsed.Path = path
	return &Client{baseURL: parsed.String(), apiKey: apiKey, http: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (client *Client) Health(ctx context.Context) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(client.baseURL, "/v1")+"/health", nil)
	if err != nil {
		return err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("OpenSandbox health returned %d", response.StatusCode)
	}
	return nil
}

func (client *Client) Create(ctx context.Context, input CreateRequest) (Sandbox, error) {
	var result Sandbox
	err := client.request(ctx, http.MethodPost, "/sandboxes", input, &result)
	return result, err
}

func (client *Client) Get(ctx context.Context, id string) (Sandbox, error) {
	var result Sandbox
	err := client.request(ctx, http.MethodGet, "/sandboxes/"+url.PathEscape(id), nil, &result)
	return result, err
}

func (client *Client) List(ctx context.Context) ([]Sandbox, error) {
	var envelope struct {
		Items []Sandbox `json:"items"`
	}
	err := client.request(ctx, http.MethodGet, "/sandboxes", nil, &envelope)
	return envelope.Items, err
}

func (client *Client) Renew(ctx context.Context, id string, expiresAt time.Time) (Sandbox, error) {
	var result Sandbox
	err := client.request(ctx, http.MethodPost, "/sandboxes/"+url.PathEscape(id)+"/renew-expiration", map[string]any{"expiresAt": expiresAt.UTC()}, &result)
	return result, err
}

func (client *Client) Delete(ctx context.Context, id string) error {
	return client.request(ctx, http.MethodDelete, "/sandboxes/"+url.PathEscape(id), nil, nil)
}

func (client *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, client.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.apiKey != "" {
		request.Header.Set(apiKeyHeader, client.apiKey)
	}
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.CopyN(io.Discard, response.Body, 4096)
		return APIError{StatusCode: response.StatusCode}
	}
	if output == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output)
}
