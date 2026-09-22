package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func clearProviderEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"LOCKER_LISTEN_ADDR", "LOCKER_REQUEST_TIMEOUT", "LOCKER_PROVIDER", "LOCKER_ALLOWED_MODELS", "OPENAI_API_KEY", "OPENAI_BASE_URL"} {
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
