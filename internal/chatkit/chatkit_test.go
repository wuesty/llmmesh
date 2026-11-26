package chatkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveDotEnvAppendsThenUpdates(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")

	if err := SaveDotEnv(path, "OPENAI_API_KEY", "first"); err != nil {
		t.Fatal(err)
	}
	if err := SaveDotEnv(path, "ANTHROPIC_API_KEY", "second"); err != nil {
		t.Fatal(err)
	}
	if err := SaveDotEnv(path, "OPENAI_API_KEY", "updated"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	if !strings.Contains(s, "OPENAI_API_KEY=updated") {
		t.Errorf("want updated OpenAI key; got:\n%s", s)
	}
	if strings.Contains(s, "OPENAI_API_KEY=first") {
		t.Errorf("old value should be replaced; got:\n%s", s)
	}
	if !strings.Contains(s, "ANTHROPIC_API_KEY=second") {
		t.Errorf("other keys must be preserved; got:\n%s", s)
	}
	if n := strings.Count(s, "OPENAI_API_KEY="); n != 1 {
		t.Errorf("want exactly one OPENAI line, got %d:\n%s", n, s)
	}
	if strings.Contains(s, "\n\n") {
		t.Errorf("no blank lines expected; got:\n%s", s)
	}
}

func TestSaveDotEnvIsOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := SaveDotEnv(path, "OPENAI_API_KEY", "x"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 600", perm)
	}
}

func TestEnvVarFor(t *testing.T) {
	cases := map[string]string{
		"anthropic": "ANTHROPIC_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"google":    "GOOGLE_AI_STUDIO_KEY",
		"gemini":    "GOOGLE_AI_STUDIO_KEY",
		"nope":      "",
	}
	for provider, want := range cases {
		if got := EnvVarFor(provider); got != want {
			t.Errorf("EnvVarFor(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestIsAuthErr(t *testing.T) {
	if !IsAuthErr(errString("POST ...: 401 Unauthorized")) {
		t.Error("401 should be an auth error")
	}
	if IsAuthErr(errString("404 model not found")) {
		t.Error("404 should not be an auth error")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
