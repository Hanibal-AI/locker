// Package config loads Locker's configuration from a YAML file and/or
// environment variables, as described in Docs/roadmap.md Phase 1.3.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ProviderConfig holds the connection details for a single upstream LLM
// provider.
type ProviderConfig struct {
	APIKey  string `yaml:"api_key"`
	BaseURL string `yaml:"base_url"`
}

// Config is Locker's top-level configuration.
type Config struct {
	ListenAddr     string                    `yaml:"listen_addr"`
	RequestTimeout time.Duration             `yaml:"request_timeout"`
	Provider       string                    `yaml:"provider"`
	Providers      map[string]ProviderConfig `yaml:"providers"`
	AllowedModels  []string                  `yaml:"allowed_models"`
}

const (
	defaultListenAddr     = ":8080"
	defaultRequestTimeout = 60 * time.Second
	defaultProvider       = "openai"
	defaultOpenAIBaseURL  = "https://api.openai.com/v1"
)

// Load reads configuration from the YAML file at path, if it exists,
// applies environment variable overrides on top, fills in defaults, and
// validates the result. If path does not exist, Locker falls back to
// defaults plus environment variables only — a config.yaml file is
// convenient but never required.
func Load(path string) (*Config, error) {
	cfg := &Config{
		ListenAddr:     defaultListenAddr,
		RequestTimeout: defaultRequestTimeout,
		Provider:       defaultProvider,
		Providers:      map[string]ProviderConfig{},
	}

	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		// Expand ${VAR} / $VAR references (e.g. api_key: "${OPENAI_API_KEY}")
		// so secrets never need to be written in plaintext into the file.
		expanded := os.ExpandEnv(string(raw))
		if err := yaml.Unmarshal([]byte(expanded), cfg); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
	case os.IsNotExist(err):
		// No config file: run purely from defaults + environment variables.
	default:
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	applyEnvOverrides(cfg)

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("LOCKER_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("LOCKER_REQUEST_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RequestTimeout = d
		}
	}
	if v := os.Getenv("LOCKER_PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv("LOCKER_ALLOWED_MODELS"); v != "" {
		models := strings.Split(v, ",")
		for i := range models {
			models[i] = strings.TrimSpace(models[i])
		}
		cfg.AllowedModels = models
	}

	openai := cfg.Providers["openai"]
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		openai.APIKey = v
	}
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		openai.BaseURL = v
	}
	if openai.BaseURL == "" {
		openai.BaseURL = defaultOpenAIBaseURL
	}
	cfg.Providers["openai"] = openai
}

func (c *Config) validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("listen_addr must not be empty")
	}
	p, ok := c.Providers[c.Provider]
	if !ok {
		return fmt.Errorf("provider %q has no matching entry under providers", c.Provider)
	}
	if p.APIKey == "" {
		return fmt.Errorf("providers.%s.api_key is required (set it in config.yaml or via the corresponding *_API_KEY environment variable)", c.Provider)
	}
	return nil
}

// ActiveProvider returns the configuration of the currently selected
// provider (Config.Provider).
func (c *Config) ActiveProvider() ProviderConfig {
	return c.Providers[c.Provider]
}
