package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Hanibal-AI/locker/internal/providers"
)

// stubProvider is a test double for providers.Provider that points at a
// local httptest.Server instead of a real upstream.
type stubProvider struct {
	baseURL    string
	authHeader string
}

func (p *stubProvider) Name() string { return "stub" }

func (p *stubProvider) Target(path string) string {
	return p.baseURL + strings.TrimPrefix(path, "/v1")
}

func (p *stubProvider) Authenticate(req *http.Request) {
	if p.authHeader != "" {
		req.Header.Set("Authorization", p.authHeader)
	}
}

// stubProvider's own upstream (in these tests, an httptest.Server) always
// already speaks OpenAI-compatible shape, so translation is identity —
// see providers.passthroughTranslation for the production equivalent.
func (p *stubProvider) TranslateRequest(body []byte) ([]byte, error)  { return body, nil }
func (p *stubProvider) TranslateResponse(body []byte) ([]byte, error) { return body, nil }
func (p *stubProvider) NewStreamTranslator() providers.StreamTranslator {
	return stubStreamTranslator{}
}

type stubStreamTranslator struct{}

func (stubStreamTranslator) Feed(chunk []byte) []byte { return chunk }
func (stubStreamTranslator) Flush() []byte            { return nil }

func TestHandleChatCompletions_PassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Errorf("upstream received Authorization = %q, want %q", got, "Bearer sk-test")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"model":"gpt-4o"`) {
			t.Errorf("upstream received unexpected body: %s", body)
		}
		w.Header().Set("X-Upstream-Header", "present")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[]}`))
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL, authHeader: "Bearer sk-test"}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Header().Get("X-Upstream-Header"); got != "present" {
		t.Errorf("X-Upstream-Header = %q, want forwarded value %q", got, "present")
	}
	if !strings.Contains(rec.Body.String(), "chatcmpl-1") {
		t.Errorf("response body not forwarded unchanged: %s", rec.Body.String())
	}
}

func TestHandleChatCompletions_AllowedModelsRejects(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("upstream should not be called for a disallowed model")
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second, AllowedModels: []string{"gpt-4o"}})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-3.5-turbo","messages":[]}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
}

func TestHandleChatCompletions_AllowedModelsPermits(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second, AllowedModels: []string{"gpt-4o"}})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestHandleChatCompletions_Streaming(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		for _, chunk := range []string{"data: chunk1\n\n", "data: chunk2\n\n", "data: [DONE]\n\n"} {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want %q", got, "text/event-stream")
	}
	want := "data: chunk1\n\ndata: chunk2\n\ndata: [DONE]\n\n"
	if rec.Body.String() != want {
		t.Errorf("streamed body = %q, want %q", rec.Body.String(), want)
	}
}

func TestHandleChatCompletions_UpstreamUnreachable(t *testing.T) {
	provider := &stubProvider{baseURL: "http://127.0.0.1:1"} // reserved, always refused
	server := New(Options{Provider: provider, RequestTimeout: 2 * time.Second})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o"}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
}

func TestHandleChatCompletions_MethodNotAllowed(t *testing.T) {
	server := New(Options{Provider: &stubProvider{baseURL: "http://example.invalid"}, RequestTimeout: 5 * time.Second})

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/completions", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestHealthz(t *testing.T) {
	server := New(Options{Provider: &stubProvider{baseURL: "http://example.invalid"}, RequestTimeout: 5 * time.Second})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
