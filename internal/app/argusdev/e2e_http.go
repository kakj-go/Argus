package argusdev

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ScenarioHTTP struct {
	BaseURL   string
	Artifacts string
	Clients   map[string]*http.Client
}

func NewScenarioHTTP(baseURL, artifacts string) *ScenarioHTTP {
	return &ScenarioHTTP{BaseURL: baseURL, Artifacts: artifacts, Clients: map[string]*http.Client{}}
}

func (h *ScenarioHTTP) Client(name string) *http.Client {
	if client := h.Clients[name]; client != nil {
		return client
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 30 * time.Second}
	h.Clients[name] = client
	return client
}

func (h *ScenarioHTTP) Reset(name string) { delete(h.Clients, name) }

func (h *ScenarioHTTP) JSON(ctx context.Context, name, clientName, method, path string, expected int, body any, headers map[string]string) (map[string]any, error) {
	value, err := h.jsonValue(ctx, name, clientName, method, path, expected, body, headers)
	if err != nil {
		result, _ := value.(map[string]any)
		return result, err
	}
	if value == nil {
		return map[string]any{}, nil
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected a JSON object", name)
	}
	return result, nil
}

func (h *ScenarioHTTP) JSONArray(ctx context.Context, name, clientName, method, path string, expected int, body any, headers map[string]string) ([]map[string]any, error) {
	value, err := h.jsonValue(ctx, name, clientName, method, path, expected, body, headers)
	if err != nil {
		return nil, err
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected a JSON array", name)
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: JSON array contains a non-object item", name)
		}
		result = append(result, object)
	}
	return result, nil
}

func (h *ScenarioHTTP) jsonValue(ctx context.Context, name, clientName, method, path string, expected int, body any, headers map[string]string) (any, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		input = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, h.BaseURL+path, input)
	if err != nil {
		return nil, err
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	if clientName != "" {
		client = h.Client(clientName)
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var result any
	if len(bytes.TrimSpace(data)) > 0 {
		_ = json.Unmarshal(data, &result)
	}
	if response.StatusCode != expected {
		redacted := redactJSON(result)
		if encoded, marshalErr := json.MarshalIndent(redacted, "", "  "); marshalErr == nil {
			_ = writePrivate(filepath.Join(h.Artifacts, name+"-response.json"), append(encoded, '\n'))
		}
		return result, fmt.Errorf("%s: expected HTTP %d, got %d: %s", name, expected, response.StatusCode, strings.TrimSpace(string(data)))
	}
	return result, nil
}

func redactJSON(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		redactValue := false
		if name, ok := current["name"].(string); ok {
			redactValue = containsSensitiveField(name)
		}
		for key, child := range current {
			if containsSensitiveField(key) || redactValue && strings.EqualFold(key, "value") {
				result[key] = "[REDACTED]"
			} else {
				result[key] = redactJSON(child)
			}
		}
		return result
	case []any:
		result := make([]any, len(current))
		for index, child := range current {
			result[index] = redactJSON(child)
		}
		return result
	default:
		return value
	}
}

func containsSensitiveField(value string) bool {
	lower := strings.ToLower(value)
	for _, field := range []string{"password", "secret", "token", "csrf", "authorization", "cookie", "private_key", "privatekey", "recovery_code"} {
		if strings.Contains(lower, field) {
			return true
		}
	}
	return false
}

func stringField(value map[string]any, path ...string) (string, error) {
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return "", fmt.Errorf("%s is not an object", strings.Join(path, "."))
		}
		current = object[key]
	}
	result, ok := current.(string)
	if !ok || result == "" {
		return "", fmt.Errorf("%s is missing", strings.Join(path, "."))
	}
	return result, nil
}

func numberField(value map[string]any, path ...string) (int64, error) {
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("%s is not an object", strings.Join(path, "."))
		}
		current = object[key]
	}
	switch number := current.(type) {
	case float64:
		return int64(number), nil
	case json.Number:
		return strconv.ParseInt(string(number), 10, 64)
	default:
		return 0, fmt.Errorf("%s is missing", strings.Join(path, "."))
	}
}

func generateTOTP(secret string, now time.Time) (string, error) {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalized)
	if err != nil {
		return "", err
	}
	counter := uint64(now.Unix() / 30)
	message := make([]byte, 8)
	binary.BigEndian.PutUint64(message, counter)
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(message)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1_000_000), nil
}
