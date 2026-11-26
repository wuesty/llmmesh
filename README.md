# llmmesh

A small, provider-agnostic LLM client for Go. One interface, three backends
(**Anthropic**, **OpenAI**, **Google AI Studio**), streaming and non-streaming, with
OpenTelemetry tracing on every call.

It was extracted from a production agent runtime (Armchair, a fantasy-football assistant that ran
live through the 2025 NFL season) and trimmed down to the provider-integration layer so it stands on
its own. It carries no application code, no secrets, and no business logic, just the part that talks
to the model providers.

## Why it exists

Each provider's SDK has its own request shape, tool-call format, and streaming semantics. llmmesh
hides those differences behind one interface so the caller writes a request once and runs it against
any provider:

```go
type LLMApi interface {
    Chat(ctx context.Context, request *AIRequest) (*AIResponse, error)
    Stream(ctx context.Context, request *AIRequest, streamer streaming.Streamer) (*AIResponse, error)
}
```

`AIRequest` carries system/user/assistant messages, conversation history, and optional tool
definitions; `AIResponse` carries the text, any tool calls the model wants to make, and a status.
Each provider package translates to and from its SDK's native types and normalizes tool calls into a
common `ToolCall` shape.

## Layout

```
llm/              the interface, request/response types, and constants
  anthropic/      Anthropic implementation  (github.com/anthropics/anthropic-sdk-go)
  openai/         OpenAI implementation     (github.com/openai/openai-go)
  google/         Google AI Studio impl     (google.golang.org/genai)
types/            shared message / tool / conversation types
streaming/        the Streamer interface used by Stream()
utils/            small helpers (errors, string set, id list)
config/           reads API keys and default models from the environment (+ .env)
internal/chatkit/ shared key handling / provider setup for the CLIs
cmd/example/      a runnable one-prompt example
cmd/chat/         a line-based streaming REPL
cmd/tui/          a full-screen Bubble Tea chat (streaming, pinned input)
```

## Configuration

Keys and default model names come from the environment (see `config/config.go`). You can also keep
them in a `.env` file in the working directory; the chat REPL loads it on startup (real environment
variables take precedence), and when you enter a key at its prompt it offers to save it back to `.env`
(written `0600`, and `.env` is gitignored). Example `.env`:

```
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
OPENAI_MODEL=gpt-4o-mini
```

Environment variables read:

| Variable | Purpose | Default |
|----------|---------|---------|
| `ANTHROPIC_API_KEY` | Anthropic key | (required for Anthropic) |
| `ANTHROPIC_MODEL` | model override | `claude-sonnet-5-5` |
| `OPENAI_API_KEY` | OpenAI key | (required for OpenAI) |
| `OPENAI_MODEL` | model override | `gpt-4o-mini` |
| `GOOGLE_AI_STUDIO_KEY` | Google AI Studio key | (required for Google) |
| `GOOGLE_MODEL` | model override | `gemini-3.8-flash` |

## Run it

**Full-screen TUI** (`cmd/tui`) — a Claude-Code-style chat built with Bubble Tea: scrolling history,
input pinned at the bottom, the active model in a yellow status bar, tokens streaming in live:

```sh
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/tui -provider anthropic
# Enter sends · Ctrl+C quits · PgUp/PgDn or mouse wheel scroll
```

In-chat commands: `/provider <name>` switches provider, `/model <name>` switches model,
`/key [provider]` sets a provider's key with masked input (saved to `.env`), `/help` lists them.

**One-shot prompt** (`cmd/example`):

```sh
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/example -provider anthropic -prompt "Explain a mesh VPN in one sentence."

export OPENAI_API_KEY=sk-...
go run ./cmd/example -provider openai -prompt "Same question, different model."
```

**Streaming chat REPL** (`cmd/chat`) — prints the reply token by token as it streams, and keeps
conversation history across turns:

```sh
export ANTHROPIC_API_KEY=sk-ant-...
go run ./cmd/chat -provider anthropic
# type a message, Enter to send; Ctrl-D, "exit", or "quit" to leave
```

If the provider's key is not in the environment, the chat prompts for one (input hidden) instead of
failing a request. The key is validated with a tiny request before the session starts, so a bad key
re-prompts immediately rather than erroring on your first message. Library logs are silenced so only
the streamed reply shows, and provider errors are rendered as a single readable line.

Across all three, the same `Stream(ctx, req, streamer)` call drives a one-shot print, a console REPL,
and a live TUI — only the `Streamer` differs: `consoleStreamer` writes deltas to stdout, while
`chanStreamer` forwards them onto a channel the Bubble Tea program drains as UI updates.

## Use it as a library

```go
import (
    "context"
    "github.com/wuesty/llmmesh/llm"
    "github.com/wuesty/llmmesh/llm/anthropic"
)

api := anthropic.NewAnthropicAPI() // or openai.NewOpenAIAPI(), google.NewGeminiAPI()

req := llm.NewAIRequest("my-run", "You are a concise assistant.", nil, nil, nil)
req.AddUserMessage("Say hello.")

resp, err := api.Chat(context.Background(), req)
// resp.Content holds the reply; resp.ToolCalls holds any requested tool calls.
```

## Notes

- Tool use works through the `AIRequest`/`AgentWorkflowRuntime` types; the example keeps it to a
  plain chat for clarity. See `llm/*/api.go` `buildToolList` / `toolCallFrom*` for the translation.
- OpenTelemetry spans are emitted per call; wire up an exporter if you want to see them.
