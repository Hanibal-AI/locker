package providers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Hanibal-AI/locker/internal/config"
)

func TestMistral_Target(t *testing.T) {
	p, err := New("mistral", config.ProviderConfig{APIKey: "sk-test", BaseURL: "https://api.mistral.ai/v1/"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	got := p.Target("/v1/chat/completions")
	want := "https://api.mistral.ai/v1/chat/completions"
	if got != want {
		t.Errorf("Target() = %q, want %q", got, want)
	}
}

func TestMistral_Authenticate(t *testing.T) {
	p, err := New("mistral", config.ProviderConfig{APIKey: "sk-secret", BaseURL: "https://api.mistral.ai/v1"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "https://api.mistral.ai/v1/chat/completions", nil)
	p.Authenticate(req)

	if got := req.Header.Get("Authorization"); got != "Bearer sk-secret" {
		t.Errorf("Authorization header = %q, want %q", got, "Bearer sk-secret")
	}
}

// TestMistral_Passthrough proves Mistral's already-OpenAI-compatible
// shape needs no translation at all.
func TestMistral_Passthrough(t *testing.T) {
	p, err := New("mistral", config.ProviderConfig{APIKey: "sk-test", BaseURL: "https://api.mistral.ai/v1"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	body := []byte(`{"model":"mistral-large-latest","messages":[{"role":"user","content":"hi"}]}`)
	got, err := p.TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("TranslateRequest changed the body: got %s, want unchanged %s", got, body)
	}

	respBody := []byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"hi"}}]}`)
	gotResp, err := p.TranslateResponse(respBody)
	if err != nil {
		t.Fatalf("TranslateResponse: %v", err)
	}
	if string(gotResp) != string(respBody) {
		t.Errorf("TranslateResponse changed the body: got %s, want unchanged %s", gotResp, respBody)
	}

	tr := p.NewStreamTranslator()
	chunk := []byte(`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n")
	if got := tr.Feed(chunk); string(got) != string(chunk) {
		t.Errorf("StreamTranslator.Feed changed the chunk: got %s, want unchanged %s", got, chunk)
	}
}
