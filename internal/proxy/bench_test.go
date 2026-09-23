package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// benchPrompt approximates a realistic prompt containing PII, so these
// benchmarks measure the cost of the full pipeline (RegEx + NER masking,
// request forwarding, response unmasking) rather than an empty-input
// best case.
const benchPrompt = `{"model":"gpt-4o","messages":[{"role":"user","content":` +
	`"Hi, I work with Martin at Renault in Boulogne. Please email jean.dupont@example.com ` +
	`and cc our SIRET 12345678901237. The deadline is 2024-03-12."}]}`

func benchServer(b *testing.B, streaming bool) (*Server, func()) {
	b.Helper()
	var upstream *httptest.Server
	if streaming {
		upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher := w.(http.Flusher)
			for _, chunk := range []string{
				`data: {"choices":[{"index":0,"delta":{"content":"Sure, I will email [EMAIL_1] and loop in [PERSON_1]."}}]}` + "\n\n",
				"data: [DONE]\n\n",
			} {
				_, _ = w.Write([]byte(chunk))
				flusher.Flush()
			}
		}))
	} else {
		upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"Sure, I will email [EMAIL_1] and loop in [PERSON_1]."}}]}`))
		}))
	}

	provider := &stubProvider{baseURL: upstream.URL}
	server := New(Options{
		Provider:       provider,
		RequestTimeout: 5 * time.Second,
		PII:            mustPIIEngine(b),
	})
	return server, upstream.Close
}

// BenchmarkNonStreaming measures one full request/response cycle through
// the complete pipeline (mask request -> forward -> unmask response) for
// a non-streaming chat completion. See Docs/roadmap.md Phase 5.2.
func BenchmarkNonStreaming(b *testing.B) {
	server, closeUpstream := benchServer(b, false)
	defer closeUpstream()
	handler := server.Handler()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(benchPrompt))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkNonStreaming_Parallel approximates concurrent request load.
func BenchmarkNonStreaming_Parallel(b *testing.B) {
	server, closeUpstream := benchServer(b, false)
	defer closeUpstream()
	handler := server.Handler()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(benchPrompt))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	})
}

// BenchmarkStreaming measures one full streaming request/response cycle,
// including SSE unmasking.
func BenchmarkStreaming(b *testing.B) {
	server, closeUpstream := benchServer(b, true)
	defer closeUpstream()
	handler := server.Handler()

	streamPrompt := strings.Replace(benchPrompt, `"model":"gpt-4o"`, `"model":"gpt-4o","stream":true`, 1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamPrompt))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

// BenchmarkStreaming_Parallel approximates concurrent streaming sessions.
func BenchmarkStreaming_Parallel(b *testing.B) {
	server, closeUpstream := benchServer(b, true)
	defer closeUpstream()
	handler := server.Handler()

	streamPrompt := strings.Replace(benchPrompt, `"model":"gpt-4o"`, `"model":"gpt-4o","stream":true`, 1)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamPrompt))
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}
	})
}
