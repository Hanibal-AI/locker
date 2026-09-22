// Package providers implements the LLM provider adapters behind a common
// interface, as described in Docs/roadmap.md Phase 1.2. Only OpenAI is
// implemented in this phase; Anthropic and Mistral follow in Phase 6.
package providers

import (
	"fmt"
	"net/http"

	"github.com/Hanibal-AI/locker/internal/config"
)

// Provider adapts an incoming OpenAI-compatible request to a specific
// upstream LLM provider: where to send it, and how to authenticate.
type Provider interface {
	// Name identifies the provider (e.g. "openai").
	Name() string
	// Target returns the upstream URL for a given incoming request path
	// (e.g. "/v1/chat/completions").
	Target(path string) string
	// Authenticate sets whatever headers the upstream provider requires
	// (API key, organization header, etc.) on the outbound request.
	Authenticate(req *http.Request)
}

// New builds the Provider for the given name from configuration.
func New(name string, cfg config.ProviderConfig) (Provider, error) {
	switch name {
	case "openai":
		return newOpenAI(cfg), nil
	default:
		return nil, fmt.Errorf("provider %q is not supported yet (see Docs/roadmap.md Phase 6)", name)
	}
}
