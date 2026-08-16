package authorization

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sort"
)

type Resource struct {
	EnterpriseID string
	Type         string
	ID           string
	Labels       map[string]string
}

type Scope struct {
	ID                  string
	EnterpriseID        string
	ResourceTypes       []string
	ExplicitResourceIDs []string
	LabelSelector       json.RawMessage
	Status              string
}

type selector struct {
	SchemaVersion string        `json:"schema_version"`
	Requirements  []requirement `json:"requirements"`
}

type requirement struct {
	Key      string   `json:"key"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

func NormalizeSelector(raw json.RawMessage) (json.RawMessage, []byte, error) {
	if len(raw) == 0 || string(raw) == "null" {
		sum := sha256.Sum256([]byte("null"))
		return nil, sum[:], nil
	}
	var value selector
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, nil, err
	}
	if value.SchemaVersion != "argus.label_selector/v1" || len(value.Requirements) == 0 || len(value.Requirements) > 16 {
		return nil, nil, errors.New("invalid label selector")
	}
	sort.Slice(value.Requirements, func(i, j int) bool { return value.Requirements[i].Key < value.Requirements[j].Key })
	seen := map[string]bool{}
	totalValues := 0
	for i := range value.Requirements {
		requirement := &value.Requirements[i]
		if seen[requirement.Key] {
			return nil, nil, errors.New("duplicate label selector key")
		}
		seen[requirement.Key] = true
		sort.Strings(requirement.Values)
		totalValues += len(requirement.Values)
		switch requirement.Operator {
		case "eq":
			if len(requirement.Values) != 1 {
				return nil, nil, errors.New("eq requires one value")
			}
		case "in":
			if len(requirement.Values) == 0 || len(requirement.Values) > 32 {
				return nil, nil, errors.New("invalid in values")
			}
		case "exists", "not_exists":
			if len(requirement.Values) != 0 {
				return nil, nil, errors.New("existence operator cannot include values")
			}
		default:
			return nil, nil, errors.New("unsupported selector operator")
		}
	}
	if totalValues > 128 {
		return nil, nil, errors.New("selector has too many values")
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	sum := sha256.Sum256(normalized)
	return normalized, sum[:], nil
}

func ScopeMatches(scope Scope, resource Resource) bool {
	if scope.Status != "active" || scope.EnterpriseID != resource.EnterpriseID || !contains(scope.ResourceTypes, resource.Type) {
		return false
	}
	if contains(scope.ExplicitResourceIDs, resource.ID) {
		return true
	}
	if len(scope.LabelSelector) == 0 || string(scope.LabelSelector) == "null" {
		return false
	}
	var value selector
	if json.Unmarshal(scope.LabelSelector, &value) != nil {
		return false
	}
	for _, requirement := range value.Requirements {
		label, exists := resource.Labels[requirement.Key]
		switch requirement.Operator {
		case "eq", "in":
			if !exists || !contains(requirement.Values, label) {
				return false
			}
		case "exists":
			if !exists {
				return false
			}
		case "not_exists":
			if exists {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func AnyScopeMatches(scopes []Scope, resource Resource) (bool, []string) {
	var matched []string
	for _, scope := range scopes {
		if ScopeMatches(scope, resource) {
			matched = append(matched, scope.ID)
		}
	}
	return len(matched) > 0, matched
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
