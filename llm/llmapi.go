package llm

import (
	"context"
	"github.com/wuesty/llmmesh/streaming"
)


type LLMApi interface {
	Chat(ctx context.Context, request *AIRequest) (*AIResponse, error)
	Stream(ctx context.Context, request *AIRequest, streamer streaming.Streamer) (*AIResponse, error)
}

type BaseLLMApi struct {
}
