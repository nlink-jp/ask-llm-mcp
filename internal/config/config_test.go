package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearAllEnv unsets every env var Load() consults so a test starts from
// a clean baseline regardless of the developer's shell.
func clearAllEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"ASK_LLM_BASE_URL", "ASK_LLM_MODEL", "ASK_LLM_API_KEY",
		"ASK_LLM_REQUEST_TIMEOUT", "ASK_LLM_SYSTEM_PROMPT",
		"ASK_LLM_TEMPERATURE", "ASK_LLM_MAX_TOKENS", "ASK_LLM_LOG_LEVEL",
		"OPENAI_BASE_URL", "OPENAI_API_KEY", "OPENAI_MODEL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_MissingModel_IsError(t *testing.T) {
	clearAllEnv(t)
	_, err := Load(filepath.Join(t.TempDir(), "nonexistent.toml"))
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
	if !strings.Contains(err.Error(), "model is required") {
		t.Fatalf("expected model-required error, got: %v", err)
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	clearAllEnv(t)
	t.Setenv("ASK_LLM_MODEL", "some-model")

	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.BaseURL != DefaultBaseURL {
		t.Errorf("base_url default: want %q, got %q", DefaultBaseURL, cfg.LLM.BaseURL)
	}
	if cfg.LLM.RequestTimeout != DefaultRequestTimeout {
		t.Errorf("timeout default: want %d, got %d", DefaultRequestTimeout, cfg.LLM.RequestTimeout)
	}
	if cfg.Log.Level != DefaultLogLevel {
		t.Errorf("log level default: want %q, got %q", DefaultLogLevel, cfg.Log.Level)
	}
	if cfg.LLM.Temperature != nil || cfg.LLM.MaxTokens != nil {
		t.Errorf("temperature/max_tokens should be nil when unset: %v %v", cfg.LLM.Temperature, cfg.LLM.MaxTokens)
	}
}

func TestLoad_EnvOnly_AskLLMOverOpenAI(t *testing.T) {
	clearAllEnv(t)
	t.Setenv("ASK_LLM_MODEL", "ask-wins")
	t.Setenv("OPENAI_MODEL", "openai-loses")
	t.Setenv("ASK_LLM_BASE_URL", "http://ask:1/v1")
	t.Setenv("OPENAI_BASE_URL", "http://openai:2/v1")
	t.Setenv("OPENAI_API_KEY", "sk-from-openai") // no ASK_LLM_API_KEY, so this wins

	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Model != "ask-wins" {
		t.Errorf("model: want ask-wins, got %q", cfg.LLM.Model)
	}
	if cfg.LLM.BaseURL != "http://ask:1/v1" {
		t.Errorf("base_url: want ask, got %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.APIKey != "sk-from-openai" {
		t.Errorf("api_key fallback: want sk-from-openai, got %q", cfg.LLM.APIKey)
	}
}

func TestLoad_FileThenEnv(t *testing.T) {
	clearAllEnv(t)

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[llm]
base_url = "http://file:1234/v1"
model = "file-model"
api_key = "sk-file"
request_timeout = 300
system_prompt = "be terse"
temperature = 0.2
max_tokens = 512

[log]
level = "debug"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.BaseURL != "http://file:1234/v1" || cfg.LLM.Model != "file-model" {
		t.Fatalf("file values not loaded: %+v", cfg.LLM)
	}
	if cfg.LLM.APIKey != "sk-file" || cfg.LLM.RequestTimeout != 300 {
		t.Fatalf("file api_key/timeout wrong: %+v", cfg.LLM)
	}
	if cfg.LLM.SystemPrompt != "be terse" {
		t.Fatalf("system_prompt: got %q", cfg.LLM.SystemPrompt)
	}
	if cfg.LLM.Temperature == nil || *cfg.LLM.Temperature != 0.2 {
		t.Fatalf("temperature: want 0.2, got %v", cfg.LLM.Temperature)
	}
	if cfg.LLM.MaxTokens == nil || *cfg.LLM.MaxTokens != 512 {
		t.Fatalf("max_tokens: want 512, got %v", cfg.LLM.MaxTokens)
	}
	if cfg.Log.Level != "debug" {
		t.Fatalf("log level: want debug, got %q", cfg.Log.Level)
	}

	// Env override on top of file.
	t.Setenv("ASK_LLM_MODEL", "env-model")
	t.Setenv("ASK_LLM_BASE_URL", "http://env:9/v1")
	t.Setenv("ASK_LLM_MAX_TOKENS", "99")
	cfg2, err := Load(path)
	if err != nil {
		t.Fatalf("Load with env: %v", err)
	}
	if cfg2.LLM.Model != "env-model" || cfg2.LLM.BaseURL != "http://env:9/v1" {
		t.Fatalf("env override failed: %+v", cfg2.LLM)
	}
	if cfg2.LLM.MaxTokens == nil || *cfg2.LLM.MaxTokens != 99 {
		t.Fatalf("env max_tokens override: got %v", cfg2.LLM.MaxTokens)
	}
	// Non-overridden file values survive.
	if cfg2.LLM.APIKey != "sk-file" || cfg2.LLM.RequestTimeout != 300 {
		t.Fatalf("file values lost after env override: %+v", cfg2.LLM)
	}
}

func TestLoad_StrictDecode_RejectsUnknownField(t *testing.T) {
	clearAllEnv(t)
	t.Setenv("ASK_LLM_MODEL", "any") // satisfy the model requirement

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(`
[llm]
model = "m"
typoooo_field = "would-silently-fail-non-strict"
`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := Load(path)
	if err == nil {
		t.Fatal("expected strict-decode error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown fields") {
		t.Fatalf("expected unknown-fields error, got: %v", err)
	}
}

func TestParseIntEnv_BadValueLeavesDefault(t *testing.T) {
	clearAllEnv(t)
	t.Setenv("ASK_LLM_MODEL", "m")
	t.Setenv("ASK_LLM_REQUEST_TIMEOUT", "not-a-number")

	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.RequestTimeout != DefaultRequestTimeout {
		t.Fatalf("timeout: want default %d, got %d", DefaultRequestTimeout, cfg.LLM.RequestTimeout)
	}
}

func TestEnvTemperatureParsedAsFloat(t *testing.T) {
	clearAllEnv(t)
	t.Setenv("ASK_LLM_MODEL", "m")
	t.Setenv("ASK_LLM_TEMPERATURE", "0.7")

	cfg, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LLM.Temperature == nil || *cfg.LLM.Temperature != 0.7 {
		t.Fatalf("temperature from env: want 0.7, got %v", cfg.LLM.Temperature)
	}
}
