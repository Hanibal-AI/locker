// Package proxy implements the HTTP reverse proxy engine that forwards
// chat completion requests to an LLM provider, as described in
// Docs/roadmap.md Phase 1.1. Requests are masked and responses restored
// through internal/pii before/after forwarding (Phase 2).
package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Hanibal-AI/locker/internal/pii"
	"github.com/Hanibal-AI/locker/internal/providers"
)

const maxBodyBytes = 10 << 20 // 10MB, applied to both request and response bodies

// hopByHopHeaders are stripped before forwarding, per RFC 7230 §6.1 — they
// describe the connection itself and must not be passed transparently
// through a proxy.
var hopByHopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

// Server is the Locker reverse proxy: it exposes an OpenAI-compatible API,
// masks PII in requests and restores it in responses, and forwards to a
// single configured Provider.
type Server struct {
	provider          providers.Provider
	client            *http.Client
	allowedModels     map[string]struct{}
	pii               *pii.Engine
	streamLookback    int
	streamIdleTimeout time.Duration
}

// Options configures a Server. See Docs/roadmap.md Phase 5.1/5.3 for why
// the streaming knobs (StreamLookbackBytes, StreamIdleTimeout) exist.
type Options struct {
	Provider providers.Provider
	// RequestTimeout bounds time-to-first-byte from upstream.
	RequestTimeout time.Duration
	// AllowedModels, if non-empty, rejects any other requested model
	// before it's forwarded.
	AllowedModels []string
	// PII masks PII in every request and restores it in every response;
	// pass an Engine built from a config.PIIConfig with Disabled: true
	// (or nil) to turn this off.
	PII *pii.Engine
	// StreamLookbackBytes bounds how many trailing bytes of a streamed
	// response are held back to avoid emitting a split placeholder
	// token. 0 uses pii.DefaultPlaceholderLookback.
	StreamLookbackBytes int
	// StreamIdleTimeout closes a streaming upstream response if no bytes
	// arrive for this long. 0 disables the watchdog.
	StreamIdleTimeout time.Duration
}

// New builds a Server per opts.
func New(opts Options) *Server {
	am := make(map[string]struct{}, len(opts.AllowedModels))
	for _, m := range opts.AllowedModels {
		am[m] = struct{}{}
	}
	return &Server{
		provider: opts.Provider,
		client: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: opts.RequestTimeout,
			},
		},
		allowedModels:     am,
		pii:               opts.PII,
		streamLookback:    opts.StreamLookbackBytes,
		streamIdleTimeout: opts.StreamIdleTimeout,
	}
}

// Handler returns the http.Handler exposing Locker's routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	return mux
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "only POST is supported")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes))
	_ = r.Body.Close()
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	if len(s.allowedModels) > 0 {
		model, err := extractModel(body)
		if err != nil {
			writeError(w, http.StatusBadRequest, `request body must be valid JSON with a "model" field`)
			return
		}
		if _, ok := s.allowedModels[model]; !ok {
			writeError(w, http.StatusForbidden, fmt.Sprintf("model %q is not in the allowed_models list", model))
			return
		}
	}

	table := pii.NewTable()
	forwardBody := body
	if s.pii != nil {
		masked, err := pii.MaskJSON(body, s.pii, table)
		if err != nil {
			writeError(w, http.StatusBadRequest, "request body must be valid JSON")
			return
		}
		forwardBody = masked
	}

	upstreamReq, err := s.buildUpstreamRequest(r, forwardBody)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build upstream request")
		return
	}

	resp, err := s.doWithRetry(upstreamReq)
	if err != nil {
		log.Printf("upstream request to %s failed: %v", s.provider.Name(), err)
		writeError(w, http.StatusBadGateway, "upstream provider unreachable")
		return
	}
	defer resp.Body.Close()

	copyHeaders(w.Header(), resp.Header)

	// Unmasking a request that contained PII changes the body length
	// (e.g. "[EMAIL_1]" -> "jean.dupont@example.com"), so headers can't
	// be committed with the upstream's Content-Length before that's
	// known — doing so causes "wrote more than the declared
	// Content-Length" and broken client connections under real load
	// (found via scripts/loadtest, see Docs/roadmap.md Phase 5.2/5.5).
	if isEventStream(resp.Header) {
		// A streamed body's final length isn't known upfront either; let
		// the server chunk it instead of forwarding a now-meaningless
		// Content-Length.
		w.Header().Del("Content-Length")
		w.WriteHeader(resp.StatusCode)
		streamResponse(w, resp.Body, table, s.streamLookback, s.streamIdleTimeout)
		return
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		log.Printf("error reading upstream response body: %v", err)
		writeError(w, http.StatusBadGateway, "failed to read upstream response")
		return
	}
	if table.Len() > 0 {
		if unmasked, err := pii.UnmaskJSON(respBody, table); err != nil {
			log.Printf("error unmasking response body: %v", err)
		} else {
			respBody = unmasked
		}
	}

	w.Header().Set("Content-Length", strconv.Itoa(len(respBody)))
	w.WriteHeader(resp.StatusCode)
	if _, err := w.Write(respBody); err != nil {
		log.Printf("error writing response body: %v", err)
	}
}

func (s *Server) buildUpstreamRequest(r *http.Request, body []byte) (*http.Request, error) {
	target := s.provider.Target(r.URL.Path)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if accept := r.Header.Get("Accept"); accept != "" {
		req.Header.Set("Accept", accept)
	}
	s.provider.Authenticate(req)
	return req, nil
}

// doWithRetry retries the request on transport-level failures only — i.e.
// failures that happen before any response headers were received, so
// nothing has been written to the client yet and a retry is safe.
func (s *Server) doWithRetry(req *http.Request) (*http.Response, error) {
	const maxAttempts = 3

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptReq := req
		if attempt > 1 {
			body, err := req.GetBody()
			if err != nil {
				return nil, lastErr
			}
			attemptReq = req.Clone(req.Context())
			attemptReq.Body = body
		}

		resp, err := s.client.Do(attemptReq)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isRetryable(err) {
			break
		}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(time.Duration(attempt) * 100 * time.Millisecond):
		}
	}
	return nil, lastErr
}

func isRetryable(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func extractModel(body []byte) (string, error) {
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", err
	}
	return payload.Model, nil
}

func isEventStream(h http.Header) bool {
	return strings.Contains(h.Get("Content-Type"), "text/event-stream")
}

// streamResponse forwards body to w as soon as bytes are available,
// restoring any placeholder tokens via table (see pii.SSEUnmasker) and
// flushing after every read so Server-Sent Events reach the client with
// as little added buffering as safely possible (see Docs/roadmap.md
// Phase 1.4 and 2.4). lookback is forwarded to pii.NewSSEUnmasker. If
// idleTimeout > 0, body is force-closed (and the loop exits) when no
// bytes have arrived for that long — see Docs/roadmap.md Phase 5.3 and
// streamWatchdog.
func streamResponse(w http.ResponseWriter, body io.ReadCloser, table *pii.Table, lookback int, idleTimeout time.Duration) {
	flusher, canFlush := w.(http.Flusher)
	unmasker := pii.NewSSEUnmasker(table, lookback)
	reader := bufio.NewReader(body)
	buf := make([]byte, 4096)

	var stalled atomic.Bool
	var activity chan struct{}
	if idleTimeout > 0 {
		activity = make(chan struct{}, 1)
		stop := make(chan struct{})
		defer close(stop)
		go streamWatchdog(body, idleTimeout, activity, stop, &stalled)
	}
	notifyActivity := func() {
		if activity == nil {
			return
		}
		select {
		case activity <- struct{}{}:
		default:
		}
	}

	flushOut := func(out []byte) bool {
		if len(out) == 0 {
			return true
		}
		if _, werr := w.Write(out); werr != nil {
			return false
		}
		if canFlush {
			flusher.Flush()
		}
		return true
	}

	for {
		n, err := reader.Read(buf)
		if n > 0 {
			notifyActivity()
			if !flushOut(unmasker.Feed(buf[:n])) {
				return
			}
		}
		if err != nil {
			if err != io.EOF && !stalled.Load() {
				log.Printf("stream copy error: %v", err)
			}
			flushOut(unmasker.Flush())
			return
		}
	}
}

// streamWatchdog closes closer — forcing the blocked Read in
// streamResponse's loop to return an error — if no activity signal
// arrives within timeout. It exits without acting if stop fires first
// (the stream ended on its own). Exactly one long-lived goroutine per
// active stream; it does not spawn per-read goroutines.
func streamWatchdog(closer io.Closer, timeout time.Duration, activity <-chan struct{}, stop <-chan struct{}, stalled *atomic.Bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-stop:
			return
		case <-activity:
			if !timer.Stop() {
				<-timer.C
			}
			timer.Reset(timeout)
		case <-timer.C:
			stalled.Store(true)
			log.Printf("closing stalled streaming upstream: no data received for %s", timeout)
			_ = closer.Close()
			return
		}
	}
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHop(key) {
			continue
		}
		for _, v := range values {
			dst.Add(key, v)
		}
	}
}

func isHopByHop(header string) bool {
	for _, h := range hopByHopHeaders {
		if strings.EqualFold(h, header) {
			return true
		}
	}
	return false
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "locker_error",
		},
	})
}
