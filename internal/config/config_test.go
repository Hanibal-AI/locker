package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"LOCKER_LISTEN_ADDR", "LOCKER_REQUEST_TIMEOUT", "LOCKER_PROVIDER",
		"LOCKER_ALLOWED_MODELS", "OPENAI_API_KEY", "OPENAI_BASE_URL",
		"LOCKER_STREAM_IDLE_TIMEOUT", "LOCKER_PII_DISABLED",
		"LOCKER_PII_NER_DISABLED", "LOCKER_PII_ENABLED_RULES",
		"LOCKER_PII_STREAM_LOOKBACK_BYTES",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_EnvOnly_NoFile(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ListenAddr != defaultListenAddr {
		t.Errorf("ListenAddr = %q, want default %q", cfg.ListenAddr, defaultListenAddr)
	}
	if cfg.Provider != defaultProvider {
		t.Errorf("Provider = %q, want %q", cfg.Provider, defaultProvider)
	}
	if got := cfg.ActiveProvider().APIKey; got != "sk-test" {
		t.Errorf("APIKey = %q, want %q", got, "sk-test")
	}
	if got := cfg.ActiveProvider().BaseURL; got != defaultOpenAIBaseURL {
		t.Errorf("BaseURL = %q, want %q", got, defaultOpenAIBaseURL)
	}
}

func TestLoad_MissingAPIKey_Fails(t *testing.T) {
	clearProviderEnv(t)

	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected an error when no API key is configured, got nil")
	}
}

func TestLoad_YAMLFile_WithEnvExpansion(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-from-env")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `
listen_addr: ":9090"
request_timeout: 5s
provider: openai
providers:
  openai:
    api_key: "${OPENAI_API_KEY}"
    base_url: "https://api.openai.com/v1"
allowed_models:
  - gpt-4o
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, ":9090")
	}
	if cfg.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout = %v, want 5s", cfg.RequestTimeout)
	}
	if got := cfg.ActiveProvider().APIKey; got != "sk-from-env" {
		t.Errorf("APIKey = %q, want expanded value %q", got, "sk-from-env")
	}
	if len(cfg.AllowedModels) != 1 || cfg.AllowedModels[0] != "gpt-4o" {
		t.Errorf("AllowedModels = %v, want [gpt-4o]", cfg.AllowedModels)
	}
}

func TestLoad_EnvOverridesFile(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-env-override")
	t.Setenv("LOCKER_LISTEN_ADDR", ":7070")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `
listen_addr: ":9090"
provider: openai
providers:
  openai:
    api_key: "sk-from-file"
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ListenAddr != ":7070" {
		t.Errorf("ListenAddr = %q, want env override %q", cfg.ListenAddr, ":7070")
	}
	if got := cfg.ActiveProvider().APIKey; got != "sk-env-override" {
		t.Errorf("APIKey = %q, want env override %q", got, "sk-env-override")
	}
}

func TestLoad_UnsupportedProvider_Fails(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("LOCKER_PROVIDER", "anthropic")

	_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err == nil {
		t.Fatal("expected an error for a provider with no matching entry, got nil")
	}
}

// TestLoad_MalformedEnvVars_FailFastAndLoud covers Docs/roadmap.md
// Phase 5.3: a malformed env var must be a hard error, never a silent
// fallback to whatever default/previous value was already set.
func TestLoad_MalformedEnvVars_FailFastAndLoud(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"invalid request timeout duration", map[string]string{"LOCKER_REQUEST_TIMEOUT": "not-a-duration"}},
		{"invalid stream idle timeout duration", map[string]string{"LOCKER_STREAM_IDLE_TIMEOUT": "soon"}},
		{"invalid pii disabled bool", map[string]string{"LOCKER_PII_DISABLED": "flase"}},
		{"invalid pii ner disabled bool", map[string]string{"LOCKER_PII_NER_DISABLED": "yes"}},
		{"invalid stream lookback bytes", map[string]string{"LOCKER_PII_STREAM_LOOKBACK_BYTES": "sixty-four"}},
		{"negative stream lookback bytes", map[string]string{"LOCKER_PII_STREAM_LOOKBACK_BYTES": "-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearProviderEnv(t)
			t.Setenv("OPENAI_API_KEY", "sk-test")
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			_, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
			if err == nil {
				t.Fatalf("Load with %v = nil error, want a loud failure instead of a silent fallback", tt.env)
			}
		})
	}
}

func TestLoad_ValidEnvVars_Applied(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")
	t.Setenv("LOCKER_REQUEST_TIMEOUT", "5s")
	t.Setenv("LOCKER_STREAM_IDLE_TIMEOUT", "30s")
	t.Setenv("LOCKER_PII_DISABLED", "true")
	t.Setenv("LOCKER_PII_NER_DISABLED", "TRUE")
	t.Setenv("LOCKER_PII_STREAM_LOOKBACK_BYTES", "128")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.RequestTimeout != 5*time.Second {
		t.Errorf("RequestTimeout = %v, want 5s", cfg.RequestTimeout)
	}
	if cfg.StreamIdleTimeout != 30*time.Second {
		t.Errorf("StreamIdleTimeout = %v, want 30s", cfg.StreamIdleTimeout)
	}
	if !cfg.PII.Disabled {
		t.Error("PII.Disabled = false, want true")
	}
	if !cfg.PII.NER.Disabled {
		t.Error("PII.NER.Disabled = false, want true (case-insensitive TRUE)")
	}
	if cfg.PII.StreamLookbackBytes != 128 {
		t.Errorf("PII.StreamLookbackBytes = %d, want 128", cfg.PII.StreamLookbackBytes)
	}
}

func TestLoad_DefaultStreamIdleTimeout(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.StreamIdleTimeout != defaultStreamIdleTimeout {
		t.Errorf("StreamIdleTimeout = %v, want default %v", cfg.StreamIdleTimeout, defaultStreamIdleTimeout)
	}
}

func TestLoad_NegativeStreamLookbackInYAML_Fails(t *testing.T) {
	clearProviderEnv(t)
	t.Setenv("OPENAI_API_KEY", "sk-test")

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `
provider: openai
providers:
  openai:
    api_key: "sk-from-file"
pii:
  stream_lookback_bytes: -5
`
	if err := os.WriteFile(path, []byte(yamlContent), 0o600); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for a negative pii.stream_lookback_bytes, got nil")
	}
}
