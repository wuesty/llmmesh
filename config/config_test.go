package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadDotEnvPopulatesConfig(t *testing.T) {
	path := writeEnv(t, "# a comment\nexport ANTHROPIC_API_KEY=from-file\nOPENAI_API_KEY=\"quoted\"\n\n")

	os.Unsetenv("ANTHROPIC_API_KEY")
	os.Unsetenv("OPENAI_API_KEY")
	t.Cleanup(func() {
		os.Unsetenv("ANTHROPIC_API_KEY")
		os.Unsetenv("OPENAI_API_KEY")
		Reload()
	})

	LoadDotEnv(path)

	if got := LocalConfig.Anthropic.ApiKey; got != "from-file" {
		t.Errorf("Anthropic key = %q, want %q (export prefix should be stripped)", got, "from-file")
	}
	if got := LocalConfig.OpenAI.ApiKey; got != "quoted" {
		t.Errorf("OpenAI key = %q, want %q (surrounding quotes should be stripped)", got, "quoted")
	}
}

func TestLoadDotEnvDoesNotOverrideEnv(t *testing.T) {
	path := writeEnv(t, "ANTHROPIC_API_KEY=from-file\n")

	os.Setenv("ANTHROPIC_API_KEY", "from-env")
	t.Cleanup(func() {
		os.Unsetenv("ANTHROPIC_API_KEY")
		Reload()
	})

	LoadDotEnv(path)

	if got := LocalConfig.Anthropic.ApiKey; got != "from-env" {
		t.Errorf("a real environment variable must win: got %q, want %q", got, "from-env")
	}
}

func TestLoadDotEnvFillsEmptyEnvVar(t *testing.T) {
	// An exported-but-empty variable must not block loading from the file.
	path := writeEnv(t, "ANTHROPIC_API_KEY=from-file\n")

	os.Setenv("ANTHROPIC_API_KEY", "") // present but empty
	t.Cleanup(func() {
		os.Unsetenv("ANTHROPIC_API_KEY")
		Reload()
	})

	LoadDotEnv(path)

	if got := LocalConfig.Anthropic.ApiKey; got != "from-file" {
		t.Errorf("empty env var should be filled from .env: got %q, want %q", got, "from-file")
	}
}

func TestLoadDotEnvMissingFileIsNoError(t *testing.T) {
	// Must not panic on a missing file.
	LoadDotEnv(filepath.Join(t.TempDir(), "does-not-exist.env"))
}
