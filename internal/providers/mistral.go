package providers

import (
	"net/http"
	"strings"

	"github.com/Hanibal-AI/locker/internal/config"
)

// Mistral's chat completions API is already OpenAI-compatible in shape
// (messages, choices, streaming delta.content), so this adapter only
// needs routing and authentication — see passthroughTranslation.
type mistralProvider struct {
	passthroughTranslation
	baseURL string
	apiKey  string
}

func newMistral(cfg config.ProviderConfig) *mistralProvider {
	return &mistralProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
	}
}

func (p *mistralProvider) Name() string { return "mistral" }

// Target maps Locker's incoming path (e.g. "/v1/chat/completions") onto
// Mistral's API, whose base URL already includes "/v1".
func (p *mistralProvider) Target(path string) string {
	return p.baseURL + strings.TrimPrefix(path, "/v1")
}

func (p *mistralProvider) Authenticate(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
}
