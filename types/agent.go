package types


import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/wuesty/llmmesh/streaming"
)

var DefaultAgent = "DefaultAgent"

type TweedyAgent interface {
	Name() string
	Description() string
	Run(context.Context, *AgentRequest, streaming.Streamer) (*AgentResponse, error)
	GetResponseDefinition() *JSONResponseDefinition
}

type AgentRequest struct {
	Input               string
	AgentThought        string
	ConversationHistory []ConversationMessage
	AssistantMessages   []AIMessage
	UserMessages        []AIMessage
	agentWorkflow       AgentWorkflowRuntime
	agentRuntimeID      string
	addRTIdToStreamer   bool
}

func NewAgentRequest(input string, agentThought string, agentWorkflow AgentWorkflowRuntime) *AgentRequest {
	id := uuid.New()

	return &AgentRequest{
		Input:               input,
		AgentThought:        agentThought,
		ConversationHistory: []ConversationMessage{},
		AssistantMessages:   []AIMessage{},
		agentWorkflow:       agentWorkflow,
		agentRuntimeID:      fmt.Sprintf("%s-%s", "default", id.String()),
		addRTIdToStreamer:   true,
	}
}

func (r *AgentRequest) SetRTIdToStreamerFlag(flag bool) {
	r.addRTIdToStreamer = flag
}

func (r *AgentRequest) GetRTIdToStreamerFlag() bool {
	return r.addRTIdToStreamer
}

// GetAgentRuntimeID	returns the runtime ID for the agent
func (r *AgentRequest) GetAgentRuntimeID() string {
	return r.agentRuntimeID
}

// SetAgentRuntimeID	resets the runtime id based on the new agent name
func (r *AgentRequest) SetAgentRuntimeID(agentName string) {
	id := uuid.New()
	r.agentRuntimeID = fmt.Sprintf("%s-%s", agentName, id.String())
}

func (r *AgentRequest) GetAgentWorkflow() AgentWorkflowRuntime {
	return r.agentWorkflow
}

func (r *AgentRequest) AddConversationEntry(message ConversationMessage) {
	r.ConversationHistory = append(r.ConversationHistory, message)
}

func (r *AgentRequest) AddConversationHistory(messages []ConversationMessage) {
	r.ConversationHistory = append(r.ConversationHistory, messages...)
}

func (r *AgentRequest) AddAssistantMessage(msg string) {
	r.AssistantMessages = append(r.AssistantMessages, AIMessage{
		Role:    AssistantRole,
		Content: msg,
	})
}

func (r *AgentRequest) AddUserMessage(msg string) {
	r.UserMessages = append(r.UserMessages, AIMessage{
		Role:    UserRole,
		Content: msg,
	})
}

type AgentResponseStatus string

const (
	AgentDone     AgentResponseStatus = "done"
	AgentCanceled AgentResponseStatus = "canceled"
	AgentError    AgentResponseStatus = "error"
)

type AgentResponse struct {
	Output string
	Status AgentResponseStatus
}

func NewAgentResponse(output string, status AgentResponseStatus) *AgentResponse {
	return &AgentResponse{Output: output, Status: status}
}
