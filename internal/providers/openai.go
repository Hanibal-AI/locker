package providers

import (
	"net/http"
	"strings"

	"github.com/Hanibal-AI/locker/internal/config"
)

type openAIProvider struct {
	baseURL string
	apiKey  string
}

func newOpenAI(cfg config.ProviderConfig) *openAIProvider {
	return &openAIProvider{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
	}
}

func (p *openAIProvider) Name() string { return "openai" }

// Target maps Locker's incoming path (e.g. "/v1/chat/completions") onto
// OpenAI's API, whose base URL already includes "/v1".
func (p *openAIProvider) Target(path string) string {
	return p.baseURL + strings.TrimPrefix(path, "/v1")
}

func (p *openAIProvider) Authenticate(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
}
