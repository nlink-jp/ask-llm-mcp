// Package config loads ask-llm-mcp configuration from a TOML file with
// environment-variable overrides.
//
// Schema:
//
//	[llm]  base_url / model / api_key / request_timeout /
//	       system_prompt / temperature / max_tokens
//	[log]  level
//
// Precedence, highest first:
//
//	ASK_LLM_* env  >  OPENAI_* env  >  config file  >  built-in defaults
//
// The default config path is ~/.config/ask-llm-mcp/config.toml. Pass an
// explicit path to Load(), or pass "" to use the default. Running
// several servers with different --config files (each pointing at a
// different model/endpoint) is the intended way to expose more than one
// backend to an MCP client.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
)

// Built-in defaults applied when neither the config file nor an env var
// supplies a value. Exported so tests can reference them by name.
const (
	// DefaultBaseURL is the LM Studio OpenAI-compatible endpoint.
	DefaultBaseURL = "http://localhost:1234/v1"
	// DefaultRequestTimeout is the per-request timeout in seconds; long,
	// to accommodate heavy local models.
	DefaultRequestTimeout = 180
	// DefaultLogLevel is the slog level name used when unset.
	DefaultLogLevel = "info"
)

// Config is the root configuration, populated by Load() from the on-disk
// TOML and merged with env-var overrides.
type Config struct {
	LLM LLMConfig `toml:"llm"`
	Log LogConfig `toml:"log"`
}

// LLMConfig holds the OpenAI-compatible endpoint and generation knobs.
// Temperature and MaxTokens are pointers so "unset" (nil, omitted from
// the request) is distinguishable from an explicit zero value.
type LLMConfig struct {
	BaseURL        string   `toml:"base_url"`
	Model          string   `toml:"model"`
	APIKey         string   `toml:"api_key"`
	RequestTimeout int      `toml:"request_timeout"`
	SystemPrompt   string   `toml:"system_prompt"`
	Temperature    *float64 `toml:"temperature"`
	MaxTokens      *int     `toml:"max_tokens"`
}

// LogConfig holds the log level (applied to stderr; stdout is owned by
// the MCP transport).
type LogConfig struct {
	Level string `toml:"level"`
}

// Load reads ask-llm-mcp's configuration. If path is empty, the default
// path under ~/.config/ask-llm-mcp/config.toml is used; if no file
// exists there, Load proceeds with built-in defaults so a fresh install
// can still run when ASK_LLM_MODEL (or OPENAI_MODEL) is exported.
//
// TOML decoding is strict: any unknown field is a hard error so silent
// typos fail fast (feedback_strict_json_decode.md). Env-var overrides
// are applied AFTER the TOML decode so they always take precedence. A
// missing model after all resolution sources is a hard error.
func Load(path string) (*Config, error) {
	cfg := defaults()

	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, ".config", "ask-llm-mcp", "config.toml")
		}
	}
	if path != "" {
		if _, err := os.Stat(path); err == nil {
			meta, err := toml.DecodeFile(path, cfg)
			if err != nil {
				return nil, fmt.Errorf("parse config %s: %w", path, err)
			}
			if undecoded := meta.Undecoded(); len(undecoded) > 0 {
				return nil, fmt.Errorf("parse config %s: unknown fields: %v", path, undecoded)
			}
		}
	}

	applyEnvOverrides(cfg)

	if cfg.LLM.Model == "" {
		return nil, fmt.Errorf("model is required: set [llm].model in config or ASK_LLM_MODEL / OPENAI_MODEL env var")
	}

	return cfg, nil
}

// defaults returns a Config populated with built-in values. Kept as a
// constructor (not a package-level var) so tests always start from a
// fresh, mutation-free baseline.
func defaults() *Config {
	return &Config{
		LLM: LLMConfig{
			BaseURL:        DefaultBaseURL,
			RequestTimeout: DefaultRequestTimeout,
		},
		Log: LogConfig{
			Level: DefaultLogLevel,
		},
	}
}

// applyEnvOverrides walks the precedence table. Tool-specific variables
// (ASK_LLM_*) always win over the generic OpenAI fallbacks; numeric
// inputs that fail to parse fall through so a malformed env var never
// silently overrides a good config value.
func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("ASK_LLM_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	} else if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		cfg.LLM.BaseURL = v
	}

	if v := os.Getenv("ASK_LLM_MODEL"); v != "" {
		cfg.LLM.Model = v
	} else if v := os.Getenv("OPENAI_MODEL"); v != "" {
		cfg.LLM.Model = v
	}

	if v := os.Getenv("ASK_LLM_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	} else if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		cfg.LLM.APIKey = v
	}

	if v := os.Getenv("ASK_LLM_SYSTEM_PROMPT"); v != "" {
		cfg.LLM.SystemPrompt = v
	}
	if v := os.Getenv("ASK_LLM_LOG_LEVEL"); v != "" {
		cfg.Log.Level = v
	}

	parseIntEnv("ASK_LLM_REQUEST_TIMEOUT", &cfg.LLM.RequestTimeout)
	parseFloatPtrEnv("ASK_LLM_TEMPERATURE", &cfg.LLM.Temperature)
	parseIntPtrEnv("ASK_LLM_MAX_TOKENS", &cfg.LLM.MaxTokens)
}

// parseIntEnv reads name as an integer and writes it into *dst when the
// env var is set AND parses cleanly. A blank or non-integer value leaves
// *dst untouched.
func parseIntEnv(name string, dst *int) {
	v := os.Getenv(name)
	if v == "" {
		return
	}
	if n, err := strconv.Atoi(v); err == nil {
		*dst = n
	}
}

// parseIntPtrEnv is like parseIntEnv but targets an optional (*int)
// field: a clean parse allocates and sets the pointer.
func parseIntPtrEnv(name string, dst **int) {
	v := os.Getenv(name)
	if v == "" {
		return
	}
	if n, err := strconv.Atoi(v); err == nil {
		*dst = &n
	}
}

// parseFloatPtrEnv is the float64 counterpart of parseIntPtrEnv.
func parseFloatPtrEnv(name string, dst **float64) {
	v := os.Getenv(name)
	if v == "" {
		return
	}
	if f, err := strconv.ParseFloat(v, 64); err == nil {
		*dst = &f
	}
}
