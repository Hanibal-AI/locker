package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hanibal-AI/locker/internal/config"
)

func TestOpenAI_Target(t *testing.T) {
	p, err := New("openai", config.ProviderConfig{
		APIKey:  "sk-test",
		BaseURL: "https://api.openai.com/v1/",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	got := p.Target("/v1/chat/completions")
	want := "https://api.openai.com/v1/chat/completions"
	if got != want {
		t.Errorf("Target() = %q, want %q", got, want)
	}
}

func TestOpenAI_Authenticate(t *testing.T) {
	p, err := New("openai", config.ProviderConfig{APIKey: "sk-secret", BaseURL: "https://api.openai.com/v1"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://api.openai.com/v1/chat/completions", nil)
	p.Authenticate(req)

	want := "Bearer sk-secret"
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("Authorization header = %q, want %q", got, want)
	}
}

func TestNew_UnsupportedProvider(t *testing.T) {
	if _, err := New("cohere", config.ProviderConfig{}); err == nil {
		t.Fatal("expected an error for an unsupported provider, got nil")
	}
}
