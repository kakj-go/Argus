package modelprovider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Protocol string

const (
	ProtocolChatCompletions Protocol = "chat_completions"
	ProtocolResponses       Protocol = "responses"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"parameters"`
}

type Request struct {
	Model          string
	Messages       []Message
	Tools          []Tool
	MaxTokens      int
	Temperature    float64
	ResponseSchema map[string]any
}

type Event struct {
	Type       string
	Text       string
	ToolCallID string
	ToolName   string
	Arguments  string
	Input      int64
	Output     int64
	StopReason string
}

type Provider struct {
	Protocol Protocol
	BaseURL  string
	APIKey   string
	Client   *http.Client
}

func (provider Provider) Stream(ctx context.Context, request Request, sink func(Event) error) error {
	if provider.Protocol != ProtocolChatCompletions && provider.Protocol != ProtocolResponses {
		return errors.New("unsupported model protocol")
	}
	client := provider.Client
	if client == nil {
		client = http.DefaultClient
	}
	endpoint, payload, err := provider.buildRequest(request)
	if err != nil {
		return err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	httpRequest.Header.Set("Authorization", "Bearer "+provider.APIKey)
	response, err := client.Do(httpRequest)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		return fmt.Errorf("model provider returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return provider.consumeSSE(response.Body, sink)
}

func (provider Provider) buildRequest(request Request) (string, []byte, error) {
	base, err := url.Parse(strings.TrimRight(provider.BaseURL, "/"))
	if err != nil {
		return "", nil, err
	}
	path := "/responses"
	body := map[string]any{"model": request.Model, "stream": true, "max_output_tokens": request.MaxTokens}
	if provider.Protocol == ProtocolChatCompletions {
		path = "/chat/completions"
		body = map[string]any{"model": request.Model, "messages": request.Messages, "stream": true, "max_tokens": request.MaxTokens}
		if len(request.Tools) > 0 {
			tools := make([]map[string]any, 0, len(request.Tools))
			for _, tool := range request.Tools {
				tools = append(tools, map[string]any{"type": "function", "function": tool})
			}
			body["tools"] = tools
			body["stream_options"] = map[string]any{"include_usage": true}
		}
		if request.ResponseSchema != nil {
			body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "argus_compatibility", "strict": true, "schema": request.ResponseSchema}}
		}
	} else {
		body["input"] = request.Messages
		if len(request.Tools) > 0 {
			body["tools"] = request.Tools
		}
		if request.ResponseSchema != nil {
			body["text"] = map[string]any{"format": map[string]any{"type": "json_schema", "name": "argus_compatibility", "strict": true, "schema": request.ResponseSchema}}
		}
	}
	base.Path = strings.TrimRight(base.Path, "/") + path
	payload, err := json.Marshal(body)
	return base.String(), payload, err
}

func (provider Provider) consumeSSE(reader io.Reader, sink func(Event) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	var data strings.Builder
	chatToolCalls := map[int]Event{}
	chatToolOrder := make([]int, 0)
	flush := func() error {
		if data.Len() == 0 {
			return nil
		}
		raw := strings.TrimSuffix(data.String(), "\n")
		data.Reset()
		if raw == "[DONE]" {
			return nil
		}
		return provider.decodeEventWithState([]byte(raw), chatToolCalls, &chatToolOrder, sink)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, "data:") {
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			data.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func (provider Provider) decodeEvent(raw []byte, sink func(Event) error) error {
	return provider.decodeEventWithState(raw, map[int]Event{}, new([]int), sink)
}

func (provider Provider) decodeEventWithState(raw []byte, chatToolCalls map[int]Event, chatToolOrder *[]int, sink func(Event) error) error {
	if provider.Protocol == ProtocolResponses {
		var event struct {
			Type      string `json:"type"`
			Delta     string `json:"delta"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Response  struct {
				Status string `json:"status"`
				Usage  struct {
					Input  int64 `json:"input_tokens"`
					Output int64 `json:"output_tokens"`
				} `json:"usage"`
			} `json:"response"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		switch event.Type {
		case "response.output_text.delta":
			return sink(Event{Type: "text_delta", Text: event.Delta})
		case "response.function_call_arguments.delta":
			return sink(Event{Type: "tool_call_delta", ToolCallID: event.CallID, ToolName: event.Name, Arguments: event.Delta})
		case "response.function_call_arguments.done":
			return sink(Event{Type: "tool_call_done", ToolCallID: event.CallID, ToolName: event.Name, Arguments: event.Arguments})
		case "response.completed":
			return sink(Event{Type: "completed", Input: event.Response.Usage.Input, Output: event.Response.Usage.Output, StopReason: event.Response.Status})
		case "response.incomplete":
			return sink(Event{Type: "completed", Input: event.Response.Usage.Input, Output: event.Response.Usage.Output, StopReason: "incomplete"})
		case "response.failed":
			return errors.New("model response failed")
		default:
			return nil
		}
	}
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					Index    int                              `json:"index"`
					ID       string                           `json:"id"`
					Function struct{ Name, Arguments string } `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int64 `json:"prompt_tokens"`
			CompletionTokens int64 `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return err
	}
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			if err := sink(Event{Type: "text_delta", Text: choice.Delta.Content}); err != nil {
				return err
			}
		}
		for _, call := range choice.Delta.ToolCalls {
			current, exists := chatToolCalls[call.Index]
			if !exists {
				*chatToolOrder = append(*chatToolOrder, call.Index)
			}
			if call.ID != "" {
				current.ToolCallID = call.ID
			}
			if call.Function.Name != "" {
				current.ToolName += call.Function.Name
			}
			current.Arguments += call.Function.Arguments
			current.Type = "tool_call_delta"
			chatToolCalls[call.Index] = current
			if err := sink(Event{Type: "tool_call_delta", ToolCallID: call.ID, ToolName: call.Function.Name, Arguments: call.Function.Arguments}); err != nil {
				return err
			}
		}
		if choice.FinishReason != "" {
			if choice.FinishReason == "tool_calls" {
				for _, index := range *chatToolOrder {
					call := chatToolCalls[index]
					call.Type = "tool_call_done"
					if err := sink(call); err != nil {
						return err
					}
				}
				clear(chatToolCalls)
				*chatToolOrder = (*chatToolOrder)[:0]
			}
			if err := sink(Event{Type: "completed", StopReason: choice.FinishReason}); err != nil {
				return err
			}
		}
	}
	if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
		return sink(Event{Type: "usage", Input: chunk.Usage.PromptTokens, Output: chunk.Usage.CompletionTokens})
	}
	return nil
}
