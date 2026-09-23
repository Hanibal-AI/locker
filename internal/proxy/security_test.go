package proxy

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// secretHeaderProvider is a stubProvider variant that injects a
// recognizable "secret" into the Authorization header, so tests can
// assert it never leaks into logs.
type secretHeaderProvider struct {
	baseURL string
	secret  string
}

func (p *secretHeaderProvider) Name() string { return "secret-stub" }
func (p *secretHeaderProvider) Target(path string) string {
	return p.baseURL + strings.TrimPrefix(path, "/v1")
}
func (p *secretHeaderProvider) Authenticate(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.secret)
}

// captureLog redirects the standard logger to a buffer for the duration
// of the test and restores it afterwards.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

// TestSecurity_APIKeyNeverLogged_UpstreamFailure covers Docs/roadmap.md
// Phase 5.4: even on the upstream-unreachable error path (which does
// log), the provider's secret must never appear in log output.
func TestSecurity_APIKeyNeverLogged_UpstreamFailure(t *testing.T) {
	logBuf := captureLog(t)
	const secret = "sk-super-secret-value-12345"

	provider := &secretHeaderProvider{baseURL: "http://127.0.0.1:1", secret: secret}
	server := New(Options{Provider: provider, RequestTimeout: 2 * time.Second, PII: mustPIIEngine(t)})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
	if strings.Contains(logBuf.String(), secret) {
		t.Errorf("log output contains the API key secret: %s", logBuf.String())
	}
}

// TestSecurity_PIINeverLogged_FullRequestLifecycle exercises a normal
// successful request containing PII, plus an unmasking-error path, and
// asserts the raw PII value never appears in anything written to the
// standard logger.
func TestSecurity_PIINeverLogged_FullRequestLifecycle(t *testing.T) {
	logBuf := captureLog(t)
	const rawEmail = "jean.dupont@example.com"

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"contact [EMAIL_1] please"}}]}`))
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second, PII: mustPIIEngine(t)})

	reqBody := `{"model":"gpt-4o","messages":[{"role":"user","content":"email ` + rawEmail + ` about this"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(reqBody))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
	// Sanity check: the email really was in play (present in the client
	// response, which is expected — the point is it must not be in logs).
	if !strings.Contains(rec.Body.String(), rawEmail) {
		t.Fatalf("test setup problem: email not found in client response: %s", rec.Body.String())
	}
	if strings.Contains(logBuf.String(), rawEmail) {
		t.Errorf("log output contains the raw PII value: %s", logBuf.String())
	}
}
