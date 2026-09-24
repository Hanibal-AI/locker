package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Hanibal-AI/locker/internal/config"
)

// anthropicVersion is the API version Locker was built and tested
// against. Anthropic requires this header on every request.
const anthropicVersion = "2023-06-01"

// defaultAnthropicMaxTokens is used when a client's OpenAI-shaped request
// doesn't set max_tokens — OpenAI treats it as optional, but Anthropic's
// native API rejects a request without it.
const defaultAnthropicMaxTokens = 4096

// anthropicProvider translates between Locker's OpenAI-compatible surface
// and Anthropic's native Messages API, which has a different request
// shape (top-level "system" instead of a "system" message role, a
// required "max_tokens"), a different response shape (a "content" array
// of typed blocks instead of "choices[].message.content"), and a
// different streaming event model (content_block_delta / message_delta /
// message_stop instead of choices[].delta.content). See
// anthropic_stream.go for the streaming half.
//
// Scope: only text content blocks are translated (see TranslateResponse
// and the stream translator) — tool use / multi-modal content is not
// handled in this phase and is silently dropped from the OpenAI-shaped
// output.
type anthropicProvider struct {
	baseURL string
	apiKey  string
}

func newAnthropic(cfg config.ProviderConfig) *anthropicProvider {
	return &anthropicProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
	}
}

func (p *anthropicProvider) Name() string { return "anthropic" }

// Target ignores path (Locker only ever exposes one route,
// "/v1/chat/completions") and always points at Anthropic's native
// Messages endpoint.
func (p *anthropicProvider) Target(string) string {
	return p.baseURL + "/v1/messages"
}

// Authenticate uses Anthropic's own scheme: an "x-api-key" header plus a
// required API version header, not "Authorization: Bearer".
func (p *anthropicProvider) Authenticate(req *http.Request) {
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)
}

// TranslateRequest converts an OpenAI-shaped chat-completion request
// into Anthropic's native Messages request shape:
//   - any "system"-role message(s) are pulled out of "messages" into a
//     top-level "system" string (joined, if there were several);
//   - "max_tokens" is defaulted if the client didn't set one;
//   - "model", "stream", "temperature", "top_p", and "stop" pass through
//     unchanged when present; any other OpenAI-only field (e.g. "n",
//     "presence_penalty") is dropped rather than sent to an API that
//     doesn't understand it.
func (p *anthropicProvider) TranslateRequest(body []byte) ([]byte, error) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, fmt.Errorf("anthropic: invalid request JSON: %w", err)
	}

	out := map[string]any{}
	for _, field := range []string{"model", "stream", "temperature", "top_p"} {
		if v, ok := req[field]; ok {
			out[field] = v
		}
	}
	if stop, ok := req["stop"]; ok {
		out["stop_sequences"] = stop
	}
	if maxTokens, ok := req["max_tokens"]; ok {
		out["max_tokens"] = maxTokens
	} else {
		out["max_tokens"] = defaultAnthropicMaxTokens
	}

	rawMessages, _ := req["messages"].([]any)
	var systemParts []string
	messages := make([]map[string]any, 0, len(rawMessages))
	for _, m := range rawMessages {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)
		if role == "system" {
			systemParts = append(systemParts, content)
			continue
		}
		messages = append(messages, map[string]any{"role": role, "content": content})
	}
	if len(systemParts) > 0 {
		out["system"] = strings.Join(systemParts, "\n\n")
	}
	out["messages"] = messages

	return json.Marshal(out)
}

// TranslateResponse converts a non-streaming Anthropic Messages response
// into OpenAI-compatible chat-completion shape.
func (p *anthropicProvider) TranslateResponse(body []byte) ([]byte, error) {
	var resp struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("anthropic: invalid response JSON: %w", err)
	}

	var text strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}

	out := map[string]any{
		"id":     resp.ID,
		"object": "chat.completion",
		"model":  resp.Model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": text.String(),
				},
				"finish_reason": mapAnthropicStopReason(resp.StopReason),
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     resp.Usage.InputTokens,
			"completion_tokens": resp.Usage.OutputTokens,
			"total_tokens":      resp.Usage.InputTokens + resp.Usage.OutputTokens,
		},
	}
	return json.Marshal(out)
}

// mapAnthropicStopReason maps Anthropic's stop_reason values onto
// OpenAI's finish_reason vocabulary. An unrecognized value passes through
// unchanged rather than being silently dropped.
func mapAnthropicStopReason(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	default:
		return reason
	}
}
