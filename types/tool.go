package types


import (
	"context"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	openaisdk "github.com/openai/openai-go"
	"google.golang.org/genai"

	"github.com/wuesty/llmmesh/streaming"
)

type TweedyTool interface {
	Execute(ctx context.Context, call ToolCall, workflow AgentWorkflowRuntime, streamer streaming.Streamer) (*ToolResult, error)
	Name() string
	GetOpenAIDefinition() openaisdk.ChatCompletionToolParam
	GetGoogleDefinition() *genai.FunctionDeclaration
	GetAnthropicDefinition() anthropicsdk.ToolParam
}

// ToolCall	 Struct for Generic ToolCall
type ToolCall struct {
	Name      string
	Id        string
	Arguments map[string]interface{}
}

// ToolResult Struct for Generic ToolResult
type ToolResult struct {
	Name    string
	Id      string
	Content string
}
