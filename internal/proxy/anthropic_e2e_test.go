package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hanibal-AI/locker/internal/config"
	"github.com/Hanibal-AI/locker/internal/providers"
)

// mustProvider builds a real providers.Provider (not a test stub),
// pointed at upstreamURL, so these tests exercise the actual translation
// code in internal/providers.
func mustProvider(t *testing.T, name, upstreamURL string) providers.Provider {
	t.Helper()
	p, err := providers.New(name, config.ProviderConfig{APIKey: "sk-test", BaseURL: upstreamURL})
	if err != nil {
		t.Fatalf("providers.New(%q): %v", name, err)
	}
	return p
}

// TestAnthropic_NonStreaming_EndToEnd proves Docs/roadmap.md Phase 6.4:
// the exact same OpenAI-shaped client request/response contract works
// against Anthropic (a genuinely different native API shape), with PII
// masking still holding — the upstream never sees the raw email, and the
// client always gets an OpenAI-shaped response with it restored.
func TestAnthropic_NonStreaming_EndToEnd(t *testing.T) {
	var upstreamSawBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("upstream path = %q, want %q (Anthropic's native endpoint)", r.URL.Path, "/v1/messages")
		}
		if got := r.Header.Get("x-api-key"); got != "sk-test" {
			t.Errorf("x-api-key = %q, want %q", got, "sk-test")
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header missing")
		}
		upstreamSawBody, _ = io.ReadAll(r.Body)

		// A native Anthropic response, echoing the (masked) placeholder
		// back — exactly the shape a real Claude response has.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "msg_1", "type": "message", "role": "assistant",
			"model": "claude-3-5-sonnet-20241022",
			"content": [{"type":"text","text":"Sure, I will email [EMAIL_1] the report."}],
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 20, "output_tokens": 10}
		}`))
	}))
	defer upstream.Close()

	server := New(Options{
		Provider:       mustProvider(t, "anthropic", upstream.URL),
		RequestTimeout: 5 * time.Second,
		PII:            mustPIIEngine(t),
	})

	// The exact same OpenAI-shaped request body a client would send to
	// the "openai" provider — nothing Anthropic-specific about it.
	reqBody := `{"model":"claude-3-5-sonnet-20241022","messages":[` +
		`{"role":"system","content":"You are a helpful assistant."},` +
		`{"role":"user","content":"email jean.dupont@example.com the report"}` +
		`]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	// Upstream must have received Anthropic's native shape: masked
	// content, system pulled out of messages, max_tokens defaulted.
	var upstreamReq struct {
		System    string `json:"system"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(upstreamSawBody, &upstreamReq); err != nil {
		t.Fatalf("upstream body is not valid JSON: %v (body=%s)", err, upstreamSawBody)
	}
	if upstreamReq.System != "You are a helpful assistant." {
		t.Errorf("upstream system = %q, want the extracted system prompt", upstreamReq.System)
	}
	if upstreamReq.MaxTokens == 0 {
		t.Error("upstream max_tokens = 0, want a default to have been applied")
	}
	if len(upstreamReq.Messages) != 1 || strings.Contains(upstreamReq.Messages[0].Content, "jean.dupont@example.com") {
		t.Errorf("upstream messages = %+v, want just the masked user message (no raw email, no system role)", upstreamReq.Messages)
	}
	if !strings.Contains(upstreamReq.Messages[0].Content, "[EMAIL_1]") {
		t.Errorf("upstream did not receive the expected placeholder: %+v", upstreamReq.Messages)
	}

	// The client must see an OpenAI-shaped response with PII restored.
	var clientResp struct {
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &clientResp); err != nil {
		t.Fatalf("client response is not valid OpenAI-shaped JSON: %v (body=%s)", err, rec.Body.String())
	}
	if len(clientResp.Choices) != 1 {
		t.Fatalf("client response choices = %+v, want exactly 1", clientResp.Choices)
	}
	want := "Sure, I will email jean.dupont@example.com the report."
	if clientResp.Choices[0].Message.Content != want {
		t.Errorf("client message content = %q, want %q", clientResp.Choices[0].Message.Content, want)
	}
	if clientResp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want %q", clientResp.Choices[0].FinishReason, "stop")
	}
}

// TestAnthropic_Streaming_EndToEnd is the streaming counterpart: native
// Anthropic SSE events (content_block_delta, message_delta, message_stop)
// must be translated into OpenAI-shaped chunks, with PII restored and the
// "[DONE]" sentinel present, exactly like the "openai" provider.
func TestAnthropic_Streaming_EndToEnd(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "jean.dupont@example.com") {
			t.Errorf("upstream received the raw email in a streaming request: %s", body)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, frame := range []string{
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\"}}\n\n",
			"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Sure, emailing [EMA\"}}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"IL_1] now.\"}}\n\n",
			"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
			"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":8}}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		} {
			_, _ = w.Write([]byte(frame))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	server := New(Options{
		Provider:       mustProvider(t, "anthropic", upstream.URL),
		RequestTimeout: 5 * time.Second,
		PII:            mustPIIEngine(t),
	})

	reqBody := `{"model":"claude-3-5-sonnet-20241022","stream":true,"messages":[{"role":"user","content":"email jean.dupont@example.com now"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}

	got := rec.Body.String()
	if strings.Contains(got, "[EMAIL_1]") {
		t.Errorf("streamed body still contains a raw placeholder: %s", got)
	}
	if !strings.HasSuffix(strings.TrimRight(got, "\n"), "data: [DONE]") {
		t.Errorf("streamed body does not end with the [DONE] sentinel: %s", got)
	}

	want := "Sure, emailing jean.dupont@example.com now."
	if reconstructed := concatSSEContent(t, got); reconstructed != want {
		t.Errorf("reconstructed streamed content = %q, want %q", reconstructed, want)
	}
}

// TestMistral_EndToEnd proves Docs/roadmap.md Phase 6.4's third leg: the
// same OpenAI-shaped client request also routes correctly through the
// "mistral" provider, with PII masking intact — Mistral's native shape
// already matches OpenAI's, so this is the identity-translation path.
func TestMistral_EndToEnd(t *testing.T) {
	var upstreamSawBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamSawBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"cmpl-1","choices":[{"message":{"role":"assistant","content":"Sure, I will email [EMAIL_1] the report."}}]}`))
	}))
	defer upstream.Close()

	server := New(Options{
		Provider:       mustProvider(t, "mistral", upstream.URL),
		RequestTimeout: 5 * time.Second,
		PII:            mustPIIEngine(t),
	})

	reqBody := `{"model":"mistral-large-latest","messages":[{"role":"user","content":"email jean.dupont@example.com the report"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(string(upstreamSawBody), "jean.dupont@example.com") {
		t.Errorf("upstream received the raw email: %s", upstreamSawBody)
	}
	if !strings.Contains(string(upstreamSawBody), "[EMAIL_1]") {
		t.Errorf("upstream did not receive the expected placeholder: %s", upstreamSawBody)
	}
	if !strings.Contains(rec.Body.String(), "jean.dupont@example.com") {
		t.Errorf("client response was not restored: %s", rec.Body.String())
	}
}
