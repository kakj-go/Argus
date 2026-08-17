package model

import (
	"context"
	"errors"
	"strings"

	"github.com/kakj-go/Argus/internal/integration/modelprovider"
)

type ProviderTester struct {
	Policy modelprovider.PublicEndpointPolicy
}

func (tester ProviderTester) Test(ctx context.Context, input Input) CompatibilityResult {
	checks := []CompatibilityCheck{{Name: "basic", Status: "failed"}, {Name: "streaming", Status: "failed"},
		{Name: "tool_calling", Status: "failed"}, {Name: "structured_output", Status: "failed"}}
	if _, err := tester.Policy.Validate(ctx, input.BaseURL); err != nil {
		for index := range checks {
			checks[index].ErrorCode = "MODEL_ENDPOINT_NOT_ALLOWED"
		}
		return CompatibilityResult{Checks: checks}
	}
	provider := modelprovider.Provider{Protocol: modelprovider.Protocol(input.Protocol), BaseURL: input.BaseURL, APIKey: input.APIKey, Client: tester.Policy.Client()}
	base := modelprovider.Request{Model: input.ProviderModelID, Messages: []modelprovider.Message{{Role: "user", Content: "Reply with OK."}}, MaxTokens: min(int(input.MaxOutput), 64)}
	var text, completed bool
	err := provider.Stream(ctx, base, func(event modelprovider.Event) error {
		if event.Type == "text_delta" && strings.TrimSpace(event.Text) != "" {
			text = true
		}
		if event.Type == "completed" {
			completed = true
		}
		return nil
	})
	if err == nil && text {
		checks[0].Status = "passed"
	} else {
		checks[0].ErrorCode = "MODEL_COMPATIBILITY_FAILED"
	}
	if err == nil && completed {
		checks[1].Status = "passed"
	} else {
		checks[1].ErrorCode = "MODEL_COMPATIBILITY_FAILED"
	}

	toolRequest := base
	toolRequest.Messages = []modelprovider.Message{{Role: "user", Content: "Call the compatibility_probe tool."}}
	toolRequest.Tools = []modelprovider.Tool{{Name: "compatibility_probe", Schema: map[string]any{"type": "object", "additionalProperties": false}}}
	called := false
	err = provider.Stream(ctx, toolRequest, func(event modelprovider.Event) error {
		if event.Type == "tool_call_done" {
			called = true
		}
		return nil
	})
	if err == nil && called {
		checks[2].Status = "passed"
	} else {
		checks[2].ErrorCode = "MODEL_COMPATIBILITY_FAILED"
	}

	structured := base
	structured.Messages = []modelprovider.Message{{Role: "user", Content: "Return JSON with ok=true."}}
	structured.ResponseSchema = map[string]any{"type": "object", "properties": map[string]any{"ok": map[string]any{"type": "boolean"}}, "required": []string{"ok"}, "additionalProperties": false}
	var structuredText bool
	err = provider.Stream(ctx, structured, func(event modelprovider.Event) error {
		if event.Type == "text_delta" {
			structuredText = true
		}
		return nil
	})
	if err == nil && structuredText {
		checks[3].Status = "passed"
	} else {
		checks[3].ErrorCode = "MODEL_COMPATIBILITY_FAILED"
	}
	compatible := true
	for _, check := range checks {
		compatible = compatible && check.Status == "passed"
	}
	return CompatibilityResult{Compatible: compatible, Checks: checks}
}

func providerErrorCode(err error) string {
	if errors.Is(err, modelprovider.ErrEndpointNotAllowed) {
		return "MODEL_ENDPOINT_NOT_ALLOWED"
	}
	return "MODEL_COMPATIBILITY_FAILED"
}
