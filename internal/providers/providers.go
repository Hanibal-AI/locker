// Package providers implements the LLM provider adapters behind a common
// interface, as described in Docs/roadmap.md Phase 1.2 and Phase 6.
package providers

import (
	"fmt"
	"net/http"

	"github.com/Hanibal-AI/locker/internal/config"
)

// Provider adapts an incoming OpenAI-compatible request/response pair to
// a specific upstream LLM provider: where to send it, how to
// authenticate, and — for providers whose native API isn't already
// OpenAI-shaped (e.g. Anthropic) — how to translate between the two
// shapes. See Docs/roadmap.md Phase 6.3 for the contract a new provider
// must satisfy, and CONTRIBUTING.md for a walkthrough.
//
// Locker's own request masking (internal/pii) and response unmasking
// always operate on the OpenAI-compatible shape, before TranslateRequest
// and after TranslateResponse/StreamTranslator — a Provider never needs
// to know anything about PII masking, and internal/pii never needs to
// know anything about a specific provider's native shape.
type Provider interface {
	// Name identifies the provider (e.g. "openai").
	Name() string
	// Target returns the upstream URL for a given incoming request path
	// (e.g. "/v1/chat/completions").
	Target(path string) string
	// Authenticate sets whatever headers the upstream provider requires
	// (API key, organization header, etc.) on the outbound request.
	Authenticate(req *http.Request)
	// TranslateRequest converts an OpenAI-compatible chat-completion
	// request body (already PII-masked) into whatever shape this
	// provider's native API expects. A provider whose native API is
	// already OpenAI-compatible (OpenAI, Mistral) returns body unchanged.
	TranslateRequest(body []byte) ([]byte, error)
	// TranslateResponse converts a non-streaming native response body
	// back into OpenAI-compatible shape, before PII unmasking.
	TranslateResponse(body []byte) ([]byte, error)
	// NewStreamTranslator returns a fresh, stateful translator for one
	// streaming response, converting this provider's native
	// Server-Sent Events into OpenAI-compatible chat-completion-chunk
	// events before PII unmasking. Called once per streaming request.
	NewStreamTranslator() StreamTranslator
}

// StreamTranslator converts one provider's native SSE byte stream into
// an OpenAI-compatible chat-completion-chunk SSE byte stream, chunk by
// chunk, mirroring the Feed/Flush shape of pii.SSEUnmasker so the two
// compose directly in the proxy's read loop. It is stateful (a provider
// may need to track things like which text is still open across events)
// and scoped to a single response — never reused across requests.
type StreamTranslator interface {
	// Feed consumes a raw chunk of bytes read from the upstream and
	// returns the OpenAI-compatible bytes translated from it so far.
	// Implementations that buffer partial frames return less than they
	// were given and catch up on a later call.
	Feed(chunk []byte) []byte
	// Flush returns any remaining translated output at end of stream.
	Flush() []byte
}

// New builds the Provider for the given name from configuration.
func New(name string, cfg config.ProviderConfig) (Provider, error) {
	switch name {
	case "openai":
		return newOpenAI(cfg), nil
	case "anthropic":
		return newAnthropic(cfg), nil
	case "mistral":
		return newMistral(cfg), nil
	default:
		return nil, fmt.Errorf("provider %q is not supported (supported: openai, anthropic, mistral)", name)
	}
}
