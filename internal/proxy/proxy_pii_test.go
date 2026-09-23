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
	"github.com/Hanibal-AI/locker/internal/pii"
)

// concatSSEContent extracts and concatenates every choices[0].delta.content
// field across all "data: {...}" frames in an SSE body, the way a real
// streaming client reconstructs the full generated text.
func concatSSEContent(t *testing.T, body string) string {
	t.Helper()
	var text strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &chunk); err != nil {
			t.Fatalf("frame is not valid JSON: %v (line=%s)", err, line)
		}
		for _, c := range chunk.Choices {
			text.WriteString(c.Delta.Content)
		}
	}
	return text.String()
}

func mustPIIEngine(t *testing.T) *pii.Engine {
	t.Helper()
	e, err := pii.NewEngine(config.PIIConfig{})
	if err != nil {
		t.Fatalf("pii.NewEngine: %v", err)
	}
	return e
}

// TestPII_NonStreaming_EndToEnd proves Docs/roadmap.md Phase 2.6: a request
// containing an email is masked before it reaches the upstream provider,
// and restored in the response returned to the caller.
func TestPII_NonStreaming_EndToEnd(t *testing.T) {
	var upstreamSawBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamSawBody = string(body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"Sure, I will email [EMAIL_1] the summary."}}]}`))
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(provider, 5*time.Second, nil, mustPIIEngine(t))

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"send it to jean.dupont@example.com"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(upstreamSawBody, "jean.dupont@example.com") {
		t.Errorf("upstream received the raw email, PII was not masked before forwarding: %s", upstreamSawBody)
	}
	if !strings.Contains(upstreamSawBody, "[EMAIL_1]") {
		t.Errorf("upstream did not receive the expected placeholder: %s", upstreamSawBody)
	}
	if !strings.Contains(rec.Body.String(), "jean.dupont@example.com") {
		t.Errorf("client response was not restored to the original email: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "[EMAIL_1]") {
		t.Errorf("client response still contains a raw placeholder: %s", rec.Body.String())
	}
}

// TestPII_Streaming_EndToEnd proves the same guarantee holds for a
// streamed response, including a placeholder token split across two SSE
// chunk boundaries.
func TestPII_Streaming_EndToEnd(t *testing.T) {
	var upstreamSawBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		upstreamSawBody = string(body)

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		// Split the placeholder token itself across two separate,
		// independently-JSON-encoded SSE events — realistic if the
		// model's tokenizer splits "[EMAIL_1]" mid-token.
		for _, chunk := range []string{
			`data: {"choices":[{"index":0,"delta":{"content":"I will email [EMA"}}]}` + "\n\n",
			`data: {"choices":[{"index":0,"delta":{"content":"IL_1] right away"}}]}` + "\n\n",
			"data: [DONE]\n\n",
		} {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(provider, 5*time.Second, nil, mustPIIEngine(t))

	reqBody := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"send it to jean.dupont@example.com"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(upstreamSawBody, "jean.dupont@example.com") {
		t.Errorf("upstream received the raw email in a streaming request: %s", upstreamSawBody)
	}

	got := rec.Body.String()
	if strings.Contains(got, "[EMAIL_1]") {
		t.Errorf("streamed body still contains a raw placeholder: %s", got)
	}
	if !strings.Contains(got, "data: [DONE]") {
		t.Errorf("streamed body = %q, want it to still contain the [DONE] sentinel", got)
	}

	want := "I will email jean.dupont@example.com right away"
	if got := concatSSEContent(t, got); got != want {
		t.Errorf("reconstructed streamed content = %q, want %q (placeholder was split across two SSE events)", got, want)
	}
}
