package proxy

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestResilience_ContentLengthCorrectAfterUnmasking is a regression test
// for a real bug found via scripts/loadtest (Docs/roadmap.md Phase 5.2):
// unmasking a response changes its byte length (e.g. "[EMAIL_1]" ->
// "jean.dupont@example.com"), so a Content-Length copied from the
// upstream's (masked-length) response is wrong once the body is
// unmasked. Over a real HTTP connection (unlike httptest.NewRecorder,
// which doesn't enforce wire-protocol correctness) this broke the
// connection under load. This test uses a real client/server pair so it
// actually exercises HTTP framing.
func TestResilience_ContentLengthCorrectAfterUnmasking(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := []byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"contact [EMAIL_1] please"}}]}`)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second, PII: mustPIIEngine(t)})

	// A real server, not httptest.NewRecorder, so Go's net/http actually
	// validates Content-Length against what's written.
	frontend := httptest.NewServer(server.Handler())
	defer frontend.Close()

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"email jean.dupont@example.com about this"}]}`
	resp, err := http.Post(frontend.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body failed (likely a Content-Length mismatch): %v", err)
	}
	if !strings.Contains(string(got), "jean.dupont@example.com") {
		t.Errorf("response body = %s, want the restored email", got)
	}
}

// TestResilience_MalformedJSON ensures a request body that isn't valid
// JSON is rejected cleanly (400), not a panic or hang.
func TestResilience_MalformedJSON(t *testing.T) {
	server := New(Options{
		Provider:       &stubProvider{baseURL: "http://example.invalid"},
		RequestTimeout: 5 * time.Second,
		PII:            mustPIIEngine(t),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{not valid json`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestResilience_OversizedPayload ensures a request body larger than
// maxBodyBytes is rejected rather than exhausting memory.
func TestResilience_OversizedPayload(t *testing.T) {
	server := New(Options{
		Provider:       &stubProvider{baseURL: "http://example.invalid"},
		RequestTimeout: 5 * time.Second,
		PII:            mustPIIEngine(t),
	})

	huge := bytes.Repeat([]byte("a"), maxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(huge))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	// The truncated body is no longer valid JSON, so this is rejected as
	// a bad request rather than forwarded upstream.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// TestResilience_UpstreamDown ensures an unreachable upstream produces a
// clean 502, not a hang or a panic — see also
// TestHandleChatCompletions_UpstreamUnreachable in proxy_test.go.
func TestResilience_UpstreamDown(t *testing.T) {
	server := New(Options{
		Provider:       &stubProvider{baseURL: "http://127.0.0.1:1"},
		RequestTimeout: 2 * time.Second,
		PII:            mustPIIEngine(t),
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("handler hung instead of failing fast on an unreachable upstream")
	}

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusBadGateway, rec.Body.String())
	}
}

// TestResilience_UpstreamMalformedJSONResponse ensures a non-JSON (or
// otherwise unparsable) upstream response doesn't crash the unmask step
// — it falls back to forwarding the raw body.
func TestResilience_UpstreamMalformedJSONResponse(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second, PII: mustPIIEngine(t)})

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"email jean.dupont@example.com"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler hung on a malformed upstream response")
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if rec.Body.String() != "not json at all" {
		t.Errorf("body = %q, want the raw upstream body forwarded as a fallback", rec.Body.String())
	}
}
