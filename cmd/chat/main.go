// Command chat is an interactive, streaming chat REPL over any llmmesh provider.
//
// It prints the model's reply as it streams, token by token, and keeps
// conversation history across turns so the model has context. Key handling
// (prompt, validation, optional .env save) lives in internal/chatkit.
//
// Usage:
//
//	export ANTHROPIC_API_KEY=...        # or OPENAI_API_KEY / GOOGLE_AI_STUDIO_KEY
//	go run ./cmd/chat -provider anthropic
//
// Type a message and press Enter to send. Ctrl-D, "exit", or "quit" ends it.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/wuesty/llmmesh/internal/chatkit"
	"github.com/wuesty/llmmesh/llm"
	"github.com/wuesty/llmmesh/streaming"
	"github.com/wuesty/llmmesh/types"
)

const dotEnvPath = ".env"

// consoleStreamer prints output deltas to stdout as they arrive and keeps a
// copy of the full text so the caller can store it in history. It embeds
// streaming.NoStreamer so it only overrides the one method it cares about.
type consoleStreamer struct {
	streaming.NoStreamer
	buf strings.Builder
}

func (c *consoleStreamer) Stream(m *streaming.AgentAsyncMessage) {
	if m == nil || m.Type != streaming.OutputMessageType {
		return
	}
	if s, ok := m.Output.(string); ok && s != "" {
		fmt.Print(s)
		os.Stdout.Sync()
		c.buf.WriteString(s)
	}
}

func (c *consoleStreamer) text() string { return c.buf.String() }

func main() {
	provider := flag.String("provider", "anthropic", "anthropic | openai | google")
	system := flag.String("system", "You are a concise, helpful assistant.", "system prompt")
	flag.Parse()

	// The CLI is the presentation layer: it reports errors itself, so silence
	// the library's own slog output to keep the chat clean.
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	api := chatkit.Connect(*provider, dotEnvPath, in)

	fmt.Printf("llmmesh chat  provider=%s  (Ctrl-D, \"exit\", or \"quit\" to leave)\n", *provider)

	// history holds completed turns newest-first; the providers reverse-iterate it.
	var history []types.ConversationMessage

	for {
		fmt.Print("\nyou> ")
		if !in.Scan() {
			fmt.Println()
			break // Ctrl-D / EOF
		}
		userMsg := strings.TrimSpace(in.Text())
		if userMsg == "" {
			continue
		}
		if userMsg == "exit" || userMsg == "quit" {
			break
		}

		req := llm.NewAIRequest("chat", *system, nil, nil, nil)
		req.AddConversationHistory(history)
		req.AddUserMessage(userMsg)

		fmt.Print("ai > ")
		cs := &consoleStreamer{}
		if _, err := api.Stream(context.Background(), req, cs); err != nil {
			fmt.Fprintf(os.Stderr, "\n[error] %s\n", chatkit.FriendlyErr(err))
			continue
		}
		fmt.Println()

		history = append([]types.ConversationMessage{
			{UserMessage: userMsg, AssistantMessage: cs.text()},
		}, history...)
	}
}
