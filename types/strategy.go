package types

import (
	"context"
	"github.com/wuesty/llmmesh/streaming"
)

type Strategy interface {
	Run(ctx context.Context, input *AgentRequest, streamer streaming.Streamer) ([]string, string, error)
	SetWorkflow(workflow AgentWorkflowRuntime)
}
