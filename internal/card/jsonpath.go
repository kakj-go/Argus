package card

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

var ErrJSONPathInvalid = errors.New("restricted JSONPath is invalid")

type pathStep struct {
	property string
	index    int
	wildcard bool
}

// ParseJSONPath accepts only $, property access, numeric array indexes, and [*].
func ParseJSONPath(path string) ([]pathStep, error) {
	if path == "$" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "$") || len(path) > 512 {
		return nil, ErrJSONPathInvalid
	}
	steps := make([]pathStep, 0, 8)
	for offset := 1; offset < len(path); {
		switch path[offset] {
		case '.':
			offset++
			start := offset
			for offset < len(path) && (unicode.IsLetter(rune(path[offset])) || unicode.IsDigit(rune(path[offset])) || path[offset] == '_') {
				offset++
			}
			if start == offset || !isIdentifierStart(path[start]) {
				return nil, ErrJSONPathInvalid
			}
			steps = append(steps, pathStep{property: path[start:offset], index: -1})
		case '[':
			end := strings.IndexByte(path[offset:], ']')
			if end < 0 {
				return nil, ErrJSONPathInvalid
			}
			end += offset
			value := path[offset+1 : end]
			if value == "*" {
				steps = append(steps, pathStep{index: -1, wildcard: true})
			} else {
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || (len(value) > 1 && value[0] == '0') {
					return nil, ErrJSONPathInvalid
				}
				steps = append(steps, pathStep{index: index})
			}
			offset = end + 1
		default:
			return nil, ErrJSONPathInvalid
		}
		if len(steps) > 32 {
			return nil, ErrJSONPathInvalid
		}
	}
	return steps, nil
}

func EvaluateJSONPath(value any, path string) (any, error) {
	steps, err := ParseJSONPath(path)
	if err != nil {
		return nil, err
	}
	values := []any{value}
	for _, step := range steps {
		next := make([]any, 0, len(values))
		for _, current := range values {
			switch {
			case step.property != "":
				object, ok := current.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("%w: property %q requires object", ErrJSONPathInvalid, step.property)
				}
				item, exists := object[step.property]
				if !exists {
					return nil, fmt.Errorf("%w: property %q not found", ErrJSONPathInvalid, step.property)
				}
				next = append(next, item)
			case step.wildcard:
				items, ok := current.([]any)
				if !ok {
					return nil, fmt.Errorf("%w: wildcard requires array", ErrJSONPathInvalid)
				}
				next = append(next, items...)
			default:
				items, ok := current.([]any)
				if !ok || step.index >= len(items) {
					return nil, fmt.Errorf("%w: array index %d is unavailable", ErrJSONPathInvalid, step.index)
				}
				next = append(next, items[step.index])
			}
		}
		values = next
	}
	if len(values) == 1 {
		return values[0], nil
	}
	return values, nil
}

func isIdentifierStart(value byte) bool {
	return value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z'
}
