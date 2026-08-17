package resource

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
)

const (
	maxLabels          = 64
	maxNormalizedBytes = 4096
)

var (
	userLabelKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	systemLabelKeyPattern = regexp.MustCompile(`^argus\.io/[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)
	labelValuePattern     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]{0,61}[a-z0-9])?$`)
	ErrInvalidLabels      = errors.New("invalid labels")
)

type normalizedLabels struct {
	Keys   []string
	Values map[string]string
}

func NormalizeUserLabels(labels map[string]string) ([]byte, []byte, error) {
	return normalizeLabels(labels, false)
}

func NormalizeStoredLabels(labels map[string]string) ([]byte, []byte, error) {
	return normalizeLabels(labels, true)
}

func DecodeLabels(raw []byte) (map[string]string, error) {
	if len(raw) == 0 {
		return map[string]string{}, nil
	}
	var labels map[string]string
	if err := json.Unmarshal(raw, &labels); err != nil {
		return nil, ErrInvalidLabels
	}
	return labels, nil
}

func MergeSystemLabels(user, current map[string]string) map[string]string {
	merged := make(map[string]string, len(user)+len(current))
	for key, value := range user {
		merged[key] = value
	}
	for key, value := range current {
		if strings.HasPrefix(key, "argus.io/") {
			merged[key] = value
		}
	}
	return merged
}

func normalizeLabels(labels map[string]string, allowSystem bool) ([]byte, []byte, error) {
	if len(labels) > maxLabels {
		return nil, nil, ErrInvalidLabels
	}
	ordered := normalizedLabels{Keys: make([]string, 0, len(labels)), Values: labels}
	for key, value := range labels {
		validKey := userLabelKeyPattern.MatchString(key)
		if allowSystem {
			validKey = validKey || systemLabelKeyPattern.MatchString(key)
		}
		if !validKey || len(key) > 72 || !labelValuePattern.MatchString(value) || len(value) > 63 {
			return nil, nil, ErrInvalidLabels
		}
		ordered.Keys = append(ordered.Keys, key)
	}
	sort.Strings(ordered.Keys)
	encoded := []byte{'{'}
	for i, key := range ordered.Keys {
		if i > 0 {
			encoded = append(encoded, ',')
		}
		keyJSON, _ := json.Marshal(key)
		valueJSON, _ := json.Marshal(ordered.Values[key])
		encoded = append(encoded, keyJSON...)
		encoded = append(encoded, ':')
		encoded = append(encoded, valueJSON...)
	}
	encoded = append(encoded, '}')
	if len(encoded) > maxNormalizedBytes {
		return nil, nil, ErrInvalidLabels
	}
	hash := sha256.Sum256(encoded)
	return encoded, hash[:], nil
}
