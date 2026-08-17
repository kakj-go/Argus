package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

const (
	SoftCompactionRatio = 0.70
	HardCompactionRatio = 0.85
)

var ErrContextTooLarge = errors.New("context exceeds the model budget")

type ContextPart struct {
	Kind    string `json:"kind"`
	Content any    `json:"content"`
}

type ContextInput struct {
	ContextWindow int
	MaxOutput     int
	System        any
	ToolCatalog   any
	Checkpoint    any
	Snapshot      any
	RecentTail    any
	CurrentInput  any
}

type ContextProjection struct {
	Parts           []ContextPart `json:"parts"`
	EstimatedTokens int           `json:"estimated_tokens"`
	UsableTokens    int           `json:"usable_tokens"`
	SoftLimit       int           `json:"soft_limit"`
	HardLimit       int           `json:"hard_limit"`
	Hash            string        `json:"hash"`
}

func AssembleContext(input ContextInput) (ContextProjection, error) {
	safety := max(4096, input.ContextWindow/20)
	usable := input.ContextWindow - input.MaxOutput - safety
	if usable <= 0 {
		return ContextProjection{}, ErrContextTooLarge
	}
	parts := []ContextPart{
		{Kind: "system", Content: sanitize(input.System)},
		{Kind: "tool_catalog", Content: sanitize(input.ToolCatalog)},
		{Kind: "typed_checkpoint", Content: sanitize(input.Checkpoint)},
	}
	if input.Snapshot != nil {
		parts = append(parts, ContextPart{Kind: "snapshot", Content: sanitize(input.Snapshot)})
	}
	parts = append(parts,
		ContextPart{Kind: "recent_tail", Content: sanitize(input.RecentTail)},
		ContextPart{Kind: "current_input", Content: sanitize(input.CurrentInput)},
	)
	payload, err := json.Marshal(parts)
	if err != nil {
		return ContextProjection{}, err
	}
	estimated := estimateTokens(payload)
	if estimated > usable {
		return ContextProjection{}, ErrContextTooLarge
	}
	hash := sha256.Sum256(payload)
	return ContextProjection{
		Parts: parts, EstimatedTokens: estimated, UsableTokens: usable,
		SoftLimit: int(float64(usable) * SoftCompactionRatio), HardLimit: int(float64(usable) * HardCompactionRatio),
		Hash: hex.EncodeToString(hash[:]),
	}, nil
}

func (projection ContextProjection) NeedsSoftCompaction() bool {
	return projection.EstimatedTokens >= projection.SoftLimit
}

func (projection ContextProjection) NeedsHardCompaction() bool {
	return projection.EstimatedTokens >= projection.HardLimit
}

func estimateTokens(payload []byte) int { return max(1, (len(payload)+3)/4) }

func sanitize(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		clean := make(map[string]any, len(typed))
		for key, item := range typed {
			lower := strings.ToLower(key)
			if forbiddenContextKey(lower) {
				continue
			}
			clean[key] = sanitize(item)
		}
		return clean
	case []any:
		clean := make([]any, len(typed))
		for index, item := range typed {
			clean[index] = sanitize(item)
		}
		return clean
	case json.RawMessage:
		var decoded any
		if json.Unmarshal(typed, &decoded) == nil {
			return sanitize(decoded)
		}
		return nil
	case string:
		lower := strings.ToLower(typed)
		for _, marker := range []string{"argus__token", "argus_ak_", "remoteaccessticket", "remote_access_ticket", "begin private key"} {
			if strings.Contains(lower, marker) {
				return "[redacted]"
			}
		}
		return typed
	case nil, bool, float32, float64, int, int32, int64, uint, uint32, uint64:
		return typed
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil
		}
		var decoded any
		if json.Unmarshal(encoded, &decoded) != nil {
			return nil
		}
		return sanitize(decoded)
	}
}

func forbiddenContextKey(key string) bool {
	for _, forbidden := range []string{"argus__token", "api_key", "password", "setup_token", "session_token", "csrf", "secret", "credential", "private_param", "commit_tool", "remote_access_ticket"} {
		if strings.Contains(key, forbidden) {
			return true
		}
	}
	return false
}
