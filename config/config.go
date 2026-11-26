// Package config loads llmmesh provider settings from the environment.
//
// The provider packages read their API keys and default model names from
// config.LocalConfig.<Provider>.<Field>. This package populates those fields
// from environment variables, so the library is self-contained and ships no
// application coupling and no secrets. Override any default by exporting the
// matching variable before the process starts.
//
// Model defaults are overridable; set them to a model your account can access.
//
//	ANTHROPIC_API_KEY       (required to use the Anthropic provider)
//	ANTHROPIC_MODEL         (default: claude-sonnet-5-5)
//	OPENAI_API_KEY          (required to use the OpenAI provider)
//	OPENAI_MODEL            (default: gpt-4o-mini)
//	GOOGLE_AI_STUDIO_KEY    (required to use the Google provider)
//	GOOGLE_MODEL            (default: gemini-3.8-flash)
package config

import (
	"bufio"
	"os"
	"strings"
)

type AnthropicConfig struct {
	ApiKey     string
	DefaultLLM string
}

type OpenAIConfig struct {
	ApiKey     string
	DefaultLLM string
}

type GcpConfig struct {
	AIStudioKey string
	DefaultLLM  string
}

type Config struct {
	Anthropic AnthropicConfig
	OpenAI    OpenAIConfig
	Gcp       GcpConfig
}

// LocalConfig is populated once from the environment at package initialization.
var LocalConfig = load()

// Reload re-reads the environment into LocalConfig. Useful when a key is set at
// runtime (for example, prompted from the user) after this package initialized.
func Reload() { LocalConfig = load() }

// SetModel overrides the model a provider will use, at runtime. Providers read
// their model on each call, so this takes effect on the next request.
func SetModel(provider, model string) {
	switch provider {
	case "anthropic":
		LocalConfig.Anthropic.DefaultLLM = model
	case "openai":
		LocalConfig.OpenAI.DefaultLLM = model
	case "google", "gemini":
		LocalConfig.Gcp.DefaultLLM = model
	}
}

// LoadDotEnv reads KEY=VALUE lines from path into the process environment and
// refreshes LocalConfig. Existing environment variables take precedence (a real
// env var is never overwritten by the file). A missing file is not an error.
// Lines may be blank, "# comments", or optionally prefixed with "export".
func LoadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k == "" {
			continue
		}
		// Fill from the file when the variable is missing or empty; a real,
		// non-empty environment variable still takes precedence.
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	LocalConfig = load()
}

func load() Config {
	return Config{
		Anthropic: AnthropicConfig{
			ApiKey:     os.Getenv("ANTHROPIC_API_KEY"),
			DefaultLLM: envOr("ANTHROPIC_MODEL", "claude-sonnet-5-5"),
		},
		OpenAI: OpenAIConfig{
			ApiKey:     os.Getenv("OPENAI_API_KEY"),
			DefaultLLM: envOr("OPENAI_MODEL", "gpt-4o-mini"),
		},
		Gcp: GcpConfig{
			AIStudioKey: os.Getenv("GOOGLE_AI_STUDIO_KEY"),
			DefaultLLM:  envOr("GOOGLE_MODEL", "gemini-3.8-flash"),
		},
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
