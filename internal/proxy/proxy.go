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
	"strings"
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
	provider      providers.Provider
	client        *http.Client
	allowedModels map[string]struct{}
	pii           *pii.Engine
}

// New builds a Server that forwards to provider, bounding time-to-first-byte
// from upstream by requestTimeout. If allowedModels is non-empty, requests
// for any other model are rejected before being forwarded. piiEngine masks
// PII in every request and restores it in every response; pass an Engine
// built from a config.PIIConfig with Disabled: true to turn this off.
func New(provider providers.Provider, requestTimeout time.Duration, allowedModels []string, piiEngine *pii.Engine) *Server {
	am := make(map[string]struct{}, len(allowedModels))
	for _, m := range allowedModels {
		am[m] = struct{}{}
	}
	return &Server{
		provider: provider,
		client: &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: requestTimeout,
			},
		},
		allowedModels: am,
		pii:           piiEngine,
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
	w.WriteHeader(resp.StatusCode)

	if isEventStream(resp.Header) {
		streamResponse(w, resp.Body, table)
		return
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		log.Printf("error reading upstream response body: %v", err)
		return
	}
	if table.Len() > 0 {
		if unmasked, err := pii.UnmaskJSON(respBody, table); err != nil {
			log.Printf("error unmasking response body: %v", err)
		} else {
			respBody = unmasked
		}
	}
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
// restoring any placeholder tokens via table (see pii.StreamUnmasker) and
// flushing after every read so Server-Sent Events reach the client with
// as little added buffering as safely possible (see Docs/roadmap.md
// Phase 1.4 and 2.4).
func streamResponse(w http.ResponseWriter, body io.Reader, table *pii.Table) {
	flusher, canFlush := w.(http.Flusher)
	unmasker := pii.NewSSEUnmasker(table)
	reader := bufio.NewReader(body)
	buf := make([]byte, 4096)

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
			if !flushOut(unmasker.Feed(buf[:n])) {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("stream copy error: %v", err)
			}
			flushOut(unmasker.Flush())
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
