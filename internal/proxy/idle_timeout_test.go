package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestStreamIdleTimeout_ClosesStalledUpstream proves Docs/roadmap.md
// Phase 5.3: an upstream that sends some data then goes silent mid-stream
// must not hold the connection open forever. Locker closes it after
// StreamIdleTimeout and the client still gets what was sent so far.
func TestStreamIdleTimeout_ClosesStalledUpstream(t *testing.T) {
	unblock := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)

		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))
		flusher.Flush()

		// Stall indefinitely — simulates a hung upstream. The test itself
		// controls when (or whether) this handler goroutine unblocks, so
		// it never leaks past the test.
		<-unblock
	}))
	defer func() {
		close(unblock)
		upstream.Close()
	}()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{
		Provider:          provider,
		RequestTimeout:    5 * time.Second,
		StreamIdleTimeout: 100 * time.Millisecond,
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(rec, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after the upstream stalled — idle timeout watchdog did not fire")
	}

	if !strings.Contains(rec.Body.String(), "partial") {
		t.Errorf("expected the data sent before the stall to reach the client, got %q", rec.Body.String())
	}
}

// TestStreamIdleTimeout_Disabled_DoesNotCloseAFastStream is a control:
// with the watchdog off (the default in these tests via zero value), a
// normal fast stream is unaffected.
func TestStreamIdleTimeout_ZeroDisablesWatchdog(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n"))
		flusher.Flush()
	}))
	defer upstream.Close()

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{Provider: provider, RequestTimeout: 5 * time.Second, StreamIdleTimeout: 0})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","stream":true}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "hi") {
		t.Errorf("expected the stream to complete normally with the watchdog disabled, got %q", rec.Body.String())
	}
}
