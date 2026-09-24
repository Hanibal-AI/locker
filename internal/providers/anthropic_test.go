package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hanibal-AI/locker/internal/config"
)

func mustAnthropic(t *testing.T) *anthropicProvider {
	t.Helper()
	return newAnthropic(config.ProviderConfig{APIKey: "sk-ant-test", BaseURL: "https://api.anthropic.com"})
}

func TestAnthropic_Target(t *testing.T) {
	p := mustAnthropic(t)
	got := p.Target("/v1/chat/completions")
	want := "https://api.anthropic.com/v1/messages"
	if got != want {
		t.Errorf("Target() = %q, want %q", got, want)
	}
}

func TestAnthropic_Authenticate(t *testing.T) {
	p := mustAnthropic(t)
	req := httptest.NewRequest(http.MethodPost, "https://api.anthropic.com/v1/messages", nil)
	p.Authenticate(req)

	if got := req.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Errorf("x-api-key = %q, want %q", got, "sk-ant-test")
	}
	if got := req.Header.Get("anthropic-version"); got != anthropicVersion {
		t.Errorf("anthropic-version = %q, want %q", got, anthropicVersion)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("Authorization = %q, want empty (Anthropic uses x-api-key, not Bearer)", got)
	}
}

func TestAnthropic_TranslateRequest_ExtractsSystemMessage(t *testing.T) {
	p := mustAnthropic(t)
	in := `{"model":"claude-3-5-sonnet-20241022","messages":[
		{"role":"system","content":"You are helpful."},
		{"role":"user","content":"Hello"}
	],"max_tokens":100,"stream":true,"temperature":0.5}`

	out, err := p.TranslateRequest([]byte(in))
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}

	var got struct {
		Model       string  `json:"model"`
		System      string  `json:"system"`
		MaxTokens   int     `json:"max_tokens"`
		Stream      bool    `json:"stream"`
		Temperature float64 `json:"temperature"`
		Messages    []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (body=%s)", err, out)
	}

	if got.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("Model = %q, want unchanged", got.Model)
	}
	if got.System != "You are helpful." {
		t.Errorf("System = %q, want %q", got.System, "You are helpful.")
	}
	if got.MaxTokens != 100 {
		t.Errorf("MaxTokens = %d, want 100", got.MaxTokens)
	}
	if !got.Stream {
		t.Error("Stream = false, want true")
	}
	if got.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", got.Temperature)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" || got.Messages[0].Content != "Hello" {
		t.Errorf("Messages = %+v, want just the user message (system pulled out)", got.Messages)
	}
}

func TestAnthropic_TranslateRequest_DefaultsMaxTokens(t *testing.T) {
	p := mustAnthropic(t)
	in := `{"model":"claude-3-5-sonnet-20241022","messages":[{"role":"user","content":"hi"}]}`

	out, err := p.TranslateRequest([]byte(in))
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}

	var got struct {
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.MaxTokens != defaultAnthropicMaxTokens {
		t.Errorf("MaxTokens = %d, want default %d", got.MaxTokens, defaultAnthropicMaxTokens)
	}
}

func TestAnthropic_TranslateRequest_InvalidJSON(t *testing.T) {
	p := mustAnthropic(t)
	if _, err := p.TranslateRequest([]byte(`not json`)); err == nil {
		t.Fatal("expected an error for invalid request JSON, got nil")
	}
}

func TestAnthropic_TranslateResponse(t *testing.T) {
	p := mustAnthropic(t)
	in := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"model": "claude-3-5-sonnet-20241022",
		"content": [{"type":"text","text":"Hello there!"}],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 25}
	}`

	out, err := p.TranslateResponse([]byte(in))
	if err != nil {
		t.Fatalf("TranslateResponse: %v", err)
	}

	var got struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Index   int `json:"index"`
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v (body=%s)", err, out)
	}

	if got.ID != "msg_123" || got.Object != "chat.completion" || got.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("top-level fields = %+v", got)
	}
	if len(got.Choices) != 1 {
		t.Fatalf("Choices = %+v, want exactly 1", got.Choices)
	}
	c := got.Choices[0]
	if c.Message.Role != "assistant" || c.Message.Content != "Hello there!" {
		t.Errorf("Message = %+v, want role=assistant content=%q", c.Message, "Hello there!")
	}
	if c.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q (end_turn mapped)", c.FinishReason, "stop")
	}
	if got.Usage.PromptTokens != 10 || got.Usage.CompletionTokens != 25 || got.Usage.TotalTokens != 35 {
		t.Errorf("Usage = %+v", got.Usage)
	}
}

func TestAnthropic_TranslateResponse_ConcatenatesMultipleTextBlocks(t *testing.T) {
	p := mustAnthropic(t)
	in := `{"id":"msg_1","content":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}],"stop_reason":"end_turn","usage":{}}`

	out, err := p.TranslateResponse([]byte(in))
	if err != nil {
		t.Fatalf("TranslateResponse: %v", err)
	}
	var got struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if got.Choices[0].Message.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", got.Choices[0].Message.Content, "Hello world")
	}
}

func TestMapAnthropicStopReason(t *testing.T) {
	tests := map[string]string{
		"end_turn":      "stop",
		"stop_sequence": "stop",
		"max_tokens":    "length",
		"tool_use":      "tool_use", // unrecognized: passed through
	}
	for in, want := range tests {
		if got := mapAnthropicStopReason(in); got != want {
			t.Errorf("mapAnthropicStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}
