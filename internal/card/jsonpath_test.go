package card

import (
	"errors"
	"reflect"
	"testing"
)

func TestRestrictedJSONPath(t *testing.T) {
	t.Parallel()
	value := map[string]any{"items": []any{map[string]any{"name": "first"}, map[string]any{"name": "second"}}}
	got, err := EvaluateJSONPath(value, "$.items[*].name")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []any{"first", "second"}) {
		t.Fatalf("unexpected value: %#v", got)
	}
	for _, path := range []string{"$..items", "$[?(@.id)]", "$['items']", "$.items[01]", "$.items[-1]", "$.items[0].*"} {
		if _, err := ParseJSONPath(path); !errors.Is(err, ErrJSONPathInvalid) {
			t.Fatalf("path %q error = %v", path, err)
		}
	}
}
