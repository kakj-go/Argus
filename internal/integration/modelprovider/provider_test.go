package modelprovider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProviderStreamsBothProtocols(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		protocol Protocol
		stream   string
	}{
		{
			name:     "chat completions aggregates tool arguments",
			protocol: ProtocolChatCompletions,
			stream: strings.Join([]string{
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call-1","function":{"name":"host.list","arguments":"{\"limit\":"}}]},"finish_reason":""}]}`,
				``,
				`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"10}"}}]},"finish_reason":"tool_calls"}]}`,
				``,
				`data: {"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":4}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"),
		},
		{
			name:     "responses maps usage",
			protocol: ProtocolResponses,
			stream: strings.Join([]string{
				`data: {"type":"response.output_text.delta","delta":"done"}`,
				``,
				`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":9,"output_tokens":3}}}`,
				``,
			}, "\n"),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, test.stream)
			}))
			defer server.Close()
			var events []Event
			provider := Provider{Protocol: test.protocol, BaseURL: server.URL, Client: server.Client()}
			err := provider.Stream(context.Background(), Request{Model: "replay", MaxTokens: 32}, func(event Event) error {
				events = append(events, event)
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if test.protocol == ProtocolChatCompletions {
				assertEvent(t, events, "tool_call_done", func(event Event) bool {
					return event.ToolCallID == "call-1" && event.ToolName == "host.list" && event.Arguments == `{"limit":10}`
				})
				assertEvent(t, events, "usage", func(event Event) bool { return event.Input == 12 && event.Output == 4 })
			} else {
				assertEvent(t, events, "completed", func(event Event) bool { return event.Input == 9 && event.Output == 3 })
			}
		})
	}
}

func assertEvent(t *testing.T, events []Event, eventType string, match func(Event) bool) {
	t.Helper()
	for _, event := range events {
		if event.Type == eventType && match(event) {
			return
		}
	}
	t.Fatalf("missing matching %s event in %#v", eventType, events)
}

func TestResponsesIncompleteIsAStableCompletedEvent(t *testing.T) {
	t.Parallel()
	provider := Provider{Protocol: ProtocolResponses}
	var received Event
	err := provider.decodeEvent([]byte(`{"type":"response.incomplete","response":{"status":"incomplete","usage":{"input_tokens":17,"output_tokens":5}}}`), func(event Event) error {
		received = event
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if received.Type != "completed" || received.StopReason != "incomplete" || received.Input != 17 || received.Output != 5 {
		t.Fatalf("unexpected normalized event: %#v", received)
	}
}
