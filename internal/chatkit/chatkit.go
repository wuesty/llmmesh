// Package chatkit holds the pieces shared by the command-line chat front-ends:
// resolving a provider, making sure an API key is present and valid (prompting
// and optionally saving to .env), and turning raw provider errors into readable
// lines. Both cmd/chat and cmd/tui build on it.
package chatkit

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/wuesty/llmmesh/config"
	"github.com/wuesty/llmmesh/llm"
	"github.com/wuesty/llmmesh/llm/anthropic"
	"github.com/wuesty/llmmesh/llm/google"
	"github.com/wuesty/llmmesh/llm/openai"
)

// NewProvider returns a client for the named provider.
func NewProvider(name string) (llm.LLMApi, error) {
	switch name {
	case "anthropic":
		return anthropic.NewAnthropicAPI(), nil
	case "openai":
		return openai.NewOpenAIAPI(), nil
	case "google", "gemini":
		return google.NewGeminiAPI(), nil
	default:
		return nil, fmt.Errorf("unknown provider %q (want: anthropic, openai, google)", name)
	}
}

// EnvVarFor maps a provider to the environment variable holding its API key.
func EnvVarFor(provider string) string {
	switch provider {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "openai":
		return "OPENAI_API_KEY"
	case "google", "gemini":
		return "GOOGLE_AI_STUDIO_KEY"
	default:
		return ""
	}
}

// ModelFor returns the model name the provider will use (for display).
func ModelFor(provider string) string {
	switch provider {
	case "anthropic":
		return config.LocalConfig.Anthropic.DefaultLLM
	case "openai":
		return config.LocalConfig.OpenAI.DefaultLLM
	case "google", "gemini":
		return config.LocalConfig.Gcp.DefaultLLM
	default:
		return ""
	}
}

// EnsureAPIKey makes sure the provider's key is available, prompting for one
// (hidden) if the environment does not already provide it. The scanner is the
// same one the caller reads from, so a piped key does not swallow later input.
// It returns the freshly entered key (empty if the key already came from the
// environment or a loaded .env file).
func EnsureAPIKey(provider string, in *bufio.Scanner) (entered string, err error) {
	envVar := EnvVarFor(provider)
	if envVar == "" {
		return "", fmt.Errorf("unknown provider %q", provider)
	}
	if os.Getenv(envVar) != "" {
		return "", nil
	}

	fmt.Printf("%s is not set. Paste your %s API key (input hidden): ", envVar, provider)
	key, err := readSecret(in)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("could not read key: %w", err)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", fmt.Errorf("no API key entered")
	}
	if err := os.Setenv(envVar, key); err != nil {
		return "", err
	}
	config.Reload() // pick up the key we just set
	return key, nil
}

// readSecret reads one line without echoing it when stdin is a terminal, and
// falls back to the shared scanner otherwise (e.g. piped input).
func readSecret(in *bufio.Scanner) (string, error) {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		return string(b), err
	}
	if !in.Scan() {
		return "", in.Err()
	}
	return in.Text(), nil
}

// saveKey persists a freshly entered key to the dotenv file (gitignored,
// owner-only), without prompting.
func saveKey(provider, key, dotEnvPath string) {
	if err := SaveDotEnv(dotEnvPath, EnvVarFor(provider), key); err != nil {
		fmt.Fprintf(os.Stderr, "could not write %s: %v\n", dotEnvPath, err)
		return
	}
	fmt.Printf("saved to %s (gitignored, owner-only).\n", dotEnvPath)
}

// SaveDotEnv writes KEY=value into path, replacing an existing line for KEY or
// appending it, and keeps the file owner-read/write only (0600).
func SaveDotEnv(path, key, value string) error {
	var lines []string
	found := false
	if data, err := os.ReadFile(path); err == nil {
		raw := strings.Split(string(data), "\n")
		if n := len(raw); n > 0 && raw[n-1] == "" {
			raw = raw[:n-1] // drop the empty element from the trailing newline
		}
		for _, ln := range raw {
			bare := strings.TrimPrefix(strings.TrimSpace(ln), "export ")
			if k, _, ok := strings.Cut(bare, "="); ok && strings.TrimSpace(k) == key {
				lines = append(lines, key+"="+value)
				found = true
			} else {
				lines = append(lines, ln)
			}
		}
	}
	if !found {
		lines = append(lines, key+"="+value)
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

// ValidateKey sends a tiny request so a bad key fails now, not after the user's
// first real message. Transient service errors (503/overload, 429, timeouts)
// are retried a few times, since they do not indicate a bad key.
func ValidateKey(api llm.LLMApi) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(800 * time.Millisecond)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		req := llm.NewAIRequest("key-check", "Reply with the single word: ok", nil, nil, nil)
		req.AddUserMessage("ping")
		_, err = api.Chat(ctx, req)
		cancel()
		if err == nil || !IsTransientErr(err) {
			return err // success, or a real failure worth surfacing now
		}
	}
	return err // still transient after retries
}

// IsTransientErr reports whether err is a temporary service condition (service
// unavailable, overloaded, rate limited, timed out) rather than a bad key.
func IsTransientErr(err error) bool {
	s := err.Error()
	return strings.Contains(s, "503") || strings.Contains(s, "UNAVAILABLE") ||
		strings.Contains(s, "high demand") || strings.Contains(s, "overloaded") ||
		strings.Contains(s, "429") || strings.Contains(s, "deadline") ||
		strings.Contains(s, "timeout") || strings.Contains(s, "temporarily")
}

// IsAuthErr reports whether err looks like an authentication failure.
func IsAuthErr(err error) bool {
	s := err.Error()
	return strings.Contains(s, "401") || strings.Contains(s, "Unauthorized") ||
		strings.Contains(s, "invalid_api_key") || strings.Contains(s, "provide an API key") ||
		strings.Contains(s, "authentication")
}

func isModelErr(err error) bool {
	s := err.Error()
	return strings.Contains(s, "404") && strings.Contains(s, "model")
}

// FriendlyErr turns a raw provider error into a short, actionable line.
func FriendlyErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "401"), strings.Contains(s, "Unauthorized"),
		strings.Contains(s, "invalid_api_key"), strings.Contains(s, "provide an API key"):
		return "unauthorized: the API key was missing or rejected. Check the key and try again."
	case strings.Contains(s, "404") && strings.Contains(s, "model"):
		return "model not found: set the model env var (ANTHROPIC_MODEL / OPENAI_MODEL / GOOGLE_MODEL) to one your account can access."
	case strings.Contains(s, "429"):
		return "rate limited: slow down and retry."
	case strings.Contains(s, "503"), strings.Contains(s, "UNAVAILABLE"),
		strings.Contains(s, "high demand"), strings.Contains(s, "overloaded"):
		return "service temporarily unavailable (high demand); try again in a moment."
	default:
		return s
	}
}

// Connect loads the dotenv file, resolves a working client, prompts for a key
// if needed, validates it, and offers to save a freshly entered key. It exits
// the process with a message if no usable client can be established.
func Connect(provider, dotEnvPath string, in *bufio.Scanner) llm.LLMApi {
	config.LoadDotEnv(dotEnvPath)

	const maxAttempts = 3
	for attempt := 1; ; attempt++ {
		entered, err := EnsureAPIKey(provider, in)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		api, err := NewProvider(provider)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if api == nil {
			fmt.Fprintf(os.Stderr, "%s client could not be created; is the API key valid?\n", provider)
			os.Exit(1)
		}

		fmt.Printf("checking %s key... ", provider)
		err = ValidateKey(api)
		if err == nil {
			fmt.Println("ok")
			if entered != "" {
				saveKey(provider, entered, dotEnvPath)
			}
			return api
		}
		switch {
		case IsAuthErr(err):
			fmt.Fprintf(os.Stderr, "rejected.\n%s\n", FriendlyErr(err))
			_ = os.Unsetenv(EnvVarFor(provider)) // force a fresh prompt next loop
			config.Reload()
			if attempt >= maxAttempts {
				fmt.Fprintln(os.Stderr, "too many attempts; exiting.")
				os.Exit(1)
			}
		case isModelErr(err):
			fmt.Fprintf(os.Stderr, "failed.\n%s\n", FriendlyErr(err))
			os.Exit(1)
		default:
			fmt.Printf("could not fully verify (%s); continuing.\n", FriendlyErr(err))
			return api
		}
	}
}
