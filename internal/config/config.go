// Package config loads Locker's configuration from a YAML file and/or
// environment variables, as described in Docs/roadmap.md Phase 1.3.
package config

import (
	"fmt"
	"os"
	"strconv"
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

// PIIRule defines a custom RegEx detection rule, added on top of the
// built-in ones (email, phone, iban, credit_card, siren, siret), with no
// validation formula — shape match is enough. See Docs/roadmap.md
// Phase 2.1.
type PIIRule struct {
	Name        string `yaml:"name"`
	Pattern     string `yaml:"pattern"`
	Placeholder string `yaml:"placeholder"`
}

// NERConfig controls PII Detection Pipeline Layer 2 (internal/pii/ner):
// recognition of unstructured entities (names, organizations, locations,
// dates) that RegEx alone cannot catch.
type NERConfig struct {
	// Disabled turns Layer 2 off while leaving Layer 1 (RegEx) active.
	// Zero value (false) keeps it on by default.
	Disabled bool `yaml:"disabled"`
}

// PIIConfig controls the PII detection/masking pipeline (internal/pii).
type PIIConfig struct {
	// Disabled turns PII masking off entirely (both layers). Zero value
	// (false) keeps it on by default.
	Disabled bool `yaml:"disabled"`
	// EnabledRules restricts which built-in Layer 1 rules run. Empty
	// means all of them: "email", "phone", "iban", "credit_card",
	// "siren", "siret".
	EnabledRules []string  `yaml:"enabled_rules"`
	CustomRules  []PIIRule `yaml:"custom_rules"`
	NER          NERConfig `yaml:"ner"`
	// StreamLookbackBytes bounds how many trailing bytes of a streamed
	// response Locker holds back to avoid emitting a split placeholder
	// token — a latency/accuracy tradeoff (Docs/roadmap.md Phase 5.1): a
	// larger value tolerates longer placeholder tokens (e.g. long custom
	// rule names) at the cost of a slightly larger per-flush buffer.
	// Zero/unset uses pii.DefaultPlaceholderLookback.
	StreamLookbackBytes int `yaml:"stream_lookback_bytes"`
}

// Config is Locker's top-level configuration.
type Config struct {
	ListenAddr     string                    `yaml:"listen_addr"`
	RequestTimeout time.Duration             `yaml:"request_timeout"`
	Provider       string                    `yaml:"provider"`
	Providers      map[string]ProviderConfig `yaml:"providers"`
	AllowedModels  []string                  `yaml:"allowed_models"`
	PII            PIIConfig                 `yaml:"pii"`
	// StreamIdleTimeout closes a streaming upstream response if no bytes
	// arrive for this long, so a stalled provider can't hold a Locker
	// goroutine and client connection open forever (Docs/roadmap.md
	// Phase 5.3). Zero disables the watchdog.
	StreamIdleTimeout time.Duration `yaml:"stream_idle_timeout"`
}

const (
	defaultListenAddr        = ":8080"
	defaultRequestTimeout    = 60 * time.Second
	defaultProvider          = "openai"
	defaultStreamIdleTimeout = 90 * time.Second
)

// providerDefaults describes how a built-in provider's API key and base
// URL are picked up from the environment when not set in config.yaml.
// See Docs/roadmap.md Phase 6.3 ("how to add a provider"): adding a new
// provider here plus a case in providers.New (internal/providers) is the
// whole integration surface — no other package needs to change.
type providerDefaults struct {
	envAPIKey  string
	envBaseURL string
	baseURL    string
}

var knownProviders = map[string]providerDefaults{
	"openai": {
		envAPIKey:  "OPENAI_API_KEY",
		envBaseURL: "OPENAI_BASE_URL",
		baseURL:    "https://api.openai.com/v1",
	},
	"anthropic": {
		envAPIKey:  "ANTHROPIC_API_KEY",
		envBaseURL: "ANTHROPIC_BASE_URL",
		baseURL:    "https://api.anthropic.com",
	},
	"mistral": {
		envAPIKey:  "MISTRAL_API_KEY",
		envBaseURL: "MISTRAL_BASE_URL",
		baseURL:    "https://api.mistral.ai/v1",
	},
}

// Load reads configuration from the YAML file at path, if it exists,
// applies environment variable overrides on top, fills in defaults, and
// validates the result. If path does not exist, Locker falls back to
// defaults plus environment variables only — a config.yaml file is
// convenient but never required.
//
// Every error path here is deliberately loud: Load never silently
// ignores a malformed value (an invalid duration, an unparsable bool) and
// falls back to a default instead — see Docs/roadmap.md Phase 5.3
// ("config validation errors fail fast and loud at startup, no silent
// misconfiguration").
func Load(path string) (*Config, error) {
	cfg := &Config{
		ListenAddr:        defaultListenAddr,
		RequestTimeout:    defaultRequestTimeout,
		Provider:          defaultProvider,
		Providers:         map[string]ProviderConfig{},
		StreamIdleTimeout: defaultStreamIdleTimeout,
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

	if err := applyEnvOverrides(cfg); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func applyEnvOverrides(cfg *Config) error {
	if v := os.Getenv("LOCKER_LISTEN_ADDR"); v != "" {
		cfg.ListenAddr = v
	}
	if v := os.Getenv("LOCKER_REQUEST_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("LOCKER_REQUEST_TIMEOUT=%q is not a valid duration: %w", v, err)
		}
		cfg.RequestTimeout = d
	}
	if v := os.Getenv("LOCKER_STREAM_IDLE_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("LOCKER_STREAM_IDLE_TIMEOUT=%q is not a valid duration: %w", v, err)
		}
		cfg.StreamIdleTimeout = d
	}
	if v := os.Getenv("LOCKER_PROVIDER"); v != "" {
		cfg.Provider = v
	}
	if v := os.Getenv("LOCKER_ALLOWED_MODELS"); v != "" {
		cfg.AllowedModels = splitTrim(v)
	}
	if v := os.Getenv("LOCKER_PII_DISABLED"); v != "" {
		b, err := parseBoolStrict(v)
		if err != nil {
			return fmt.Errorf("LOCKER_PII_DISABLED=%q is not a valid boolean: %w", v, err)
		}
		cfg.PII.Disabled = b
	}
	if v := os.Getenv("LOCKER_PII_NER_DISABLED"); v != "" {
		b, err := parseBoolStrict(v)
		if err != nil {
			return fmt.Errorf("LOCKER_PII_NER_DISABLED=%q is not a valid boolean: %w", v, err)
		}
		cfg.PII.NER.Disabled = b
	}
	if v := os.Getenv("LOCKER_PII_ENABLED_RULES"); v != "" {
		cfg.PII.EnabledRules = splitTrim(v)
	}
	if v := os.Getenv("LOCKER_PII_STREAM_LOOKBACK_BYTES"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("LOCKER_PII_STREAM_LOOKBACK_BYTES=%q is not a valid non-negative integer", v)
		}
		cfg.PII.StreamLookbackBytes = n
	}

	for name, pd := range knownProviders {
		entry := cfg.Providers[name]
		if v := os.Getenv(pd.envAPIKey); v != "" {
			entry.APIKey = v
		}
		if v := os.Getenv(pd.envBaseURL); v != "" {
			entry.BaseURL = v
		}
		if entry.BaseURL == "" {
			entry.BaseURL = pd.baseURL
		}
		cfg.Providers[name] = entry
	}
	return nil
}

// parseBoolStrict accepts only "true" or "false" (case-insensitive) — no
// permissive "1"/"0"/"yes" aliases — so a typo in an env var value fails
// loudly instead of silently taking the "false" branch.
func parseBoolStrict(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf(`must be "true" or "false"`)
	}
}

func splitTrim(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
	if c.PII.StreamLookbackBytes < 0 {
		return fmt.Errorf("pii.stream_lookback_bytes must not be negative, got %d", c.PII.StreamLookbackBytes)
	}
	if c.RequestTimeout < 0 {
		return fmt.Errorf("request_timeout must not be negative, got %s", c.RequestTimeout)
	}
	if c.StreamIdleTimeout < 0 {
		return fmt.Errorf("stream_idle_timeout must not be negative, got %s", c.StreamIdleTimeout)
	}
	return nil
}

// ActiveProvider returns the configuration of the currently selected
// provider (Config.Provider).
func (c *Config) ActiveProvider() ProviderConfig {
	return c.Providers[c.Provider]
}
