package mcp

import (
	"context"
	"errors"
	"testing"
)

func TestCommitToolsAreHiddenAndCallerBound(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	execute := func(context.Context, Call) (Result, error) { return Result{}, nil }
	if err := registry.Register(Metadata{ID: "host.create.preview", Visibility: Visible, ExecutionMode: Sequential, MaxResultBytes: 1024, Execute: execute}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Metadata{ID: "host.create.commit", Visibility: Visible, ExecutionMode: ParallelSafe, MaxResultBytes: 1024, Execute: execute}); err != nil {
		t.Fatal(err)
	}
	if len(registry.ModelCatalog()) != 1 || registry.ModelCatalog()[0].ID != "host.create.preview" {
		t.Fatalf("unexpected model catalog: %#v", registry.ModelCatalog())
	}
	if _, err := registry.Call(context.Background(), Call{ToolID: "host.create.commit", Caller: "model"}); err != ErrCommitCaller {
		t.Fatalf("commit call error = %v", err)
	}
	if err := registry.ValidatePairs(); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryEnforcesAuthorizationAndInputValidation(t *testing.T) {
	t.Parallel()
	registry := NewRegistry()
	authorized := false
	err := registry.Register(Metadata{ID: "host.get", Visibility: Visible, ExecutionMode: Sequential, MaxResultBytes: 1024,
		InputSchema: map[string]any{"type": "object", "additionalProperties": false},
		Authorize: func(context.Context, Call) error {
			if !authorized {
				return errors.New("denied")
			}
			return nil
		},
		Validate: func(input map[string]any) error {
			if input["host_id"] == "" || input["host_id"] == nil {
				return errors.New("host_id required")
			}
			return nil
		},
		Execute: func(context.Context, Call) (Result, error) {
			return Result{Structured: map[string]any{"ok": true}}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = registry.Call(context.Background(), Call{ToolID: "host.get", Input: map[string]any{"host_id": "id"}}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("authorization error = %v", err)
	}
	authorized = true
	if _, err = registry.Call(context.Background(), Call{ToolID: "host.get", Input: map[string]any{}}); !errors.Is(err, ErrInputInvalid) {
		t.Fatalf("validation error = %v", err)
	}
	if _, err = registry.Call(context.Background(), Call{ToolID: "host.get", Input: map[string]any{"host_id": "id"}}); err != nil {
		t.Fatal(err)
	}
	catalog := registry.ModelCatalog()
	if len(catalog) != 1 || catalog[0].InputSchema == nil || catalog[0].Authorize != nil || catalog[0].Validate != nil {
		t.Fatalf("unsafe model catalog: %#v", catalog)
	}
}
