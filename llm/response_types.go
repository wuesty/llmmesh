package llm

import "github.com/wuesty/llmmesh/types"


type AIResponseStatus string

const (
	Done     AIResponseStatus = "done"
	Tools    AIResponseStatus = "tools"
	Unknown  AIResponseStatus = "unknown"
	Canceled AIResponseStatus = "canceled"
)

// AIResponse		Response from the LLM
type AIResponse struct {
	Content       string
	Messages      []types.AIMessage
	ToolsRequired bool
	ToolCalls     []types.ToolCall
	ToolMessages  []types.AIMessage
	Status        AIResponseStatus
}

// NewAIResponse  creates a new AIRequest
func NewAIResponse() *AIResponse {
	return &AIResponse{
		Content:       "",
		Messages:      make([]types.AIMessage, 0),
		ToolMessages:  make([]types.AIMessage, 0),
		ToolsRequired: false,
		ToolCalls:     make([]types.ToolCall, 0),
		Status:        Canceled,
	}
}

// AddMessage		Adds a Message to the Response
func (ar *AIResponse) AddMessage(m types.AIMessage) {
	ar.Messages = append(ar.Messages, m)
}

// AddToolCall		Adds a ToolCall to the Response
func (ar *AIResponse) AddToolCall(tool types.ToolCall) {
	ar.ToolCalls = append(ar.ToolCalls, tool)
}

// AddToolMessage	Adds a ToolMessage to the Response
func (ar *AIResponse) AddToolMessage(tool types.AIMessage) {
	ar.ToolMessages = append(ar.ToolMessages, tool)
}
