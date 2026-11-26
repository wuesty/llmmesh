// Command example sends a single prompt to a chosen provider and prints the reply.
//
// It shows the whole point of llmmesh: the same request and the same call work
// across providers; only the constructor differs.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=...        # or OPENAI_API_KEY / GOOGLE_AI_STUDIO_KEY
//	go run ./cmd/example -provider anthropic -prompt "Say hello in one sentence."
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/wuesty/llmmesh/llm"
	"github.com/wuesty/llmmesh/llm/anthropic"
	"github.com/wuesty/llmmesh/llm/google"
	"github.com/wuesty/llmmesh/llm/openai"
)

func newProvider(name string) (llm.LLMApi, error) {
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

func main() {
	provider := flag.String("provider", "anthropic", "anthropic | openai | google")
	prompt := flag.String("prompt", "Say hello in one short sentence.", "user prompt")
	system := flag.String("system", "You are a concise assistant.", "system prompt")
	flag.Parse()

	api, err := newProvider(*provider)
	if err != nil {
		log.Fatal(err)
	}
	if api == nil {
		log.Fatalf("%s client could not be created; is the API key set?", *provider)
	}

	// The same request shape feeds any provider. NewAIRequest's third argument
	// is an optional agent-workflow (for tool use); nil here means "no tools".
	req := llm.NewAIRequest("example", *system, nil, nil, nil)
	req.AddUserMessage(*prompt)

	resp, err := api.Chat(context.Background(), req)
	if err != nil {
		log.Fatalf("chat failed: %v", err)
	}
	fmt.Println(resp.Content)
}
