package llm

import (
	"sync"
	"github.com/wuesty/llmmesh/types"
)


type ToolCall struct {
	ID                string
	Type              string
	FunctionName      string
	FunctionArguments string
}

// AIRequest		AI Request Type
type AIRequest struct {
	AgentRuntimeID       string
	Prompt               string
	SystemMessages       []types.AIMessage
	UserMessages         []types.AIMessage
	AssistantMessages    []types.AIMessage
	ConversationMessages []types.ConversationMessage
	ToolCalls            []ToolCall
	ToolResults          *sync.Map
	ToolsNames           []string
	agentWorkflow        types.AgentWorkflowRuntime
	JSONResponseFormat   *types.JSONResponseDefinition
}

// NewAIRequest  creates a new AIRequest
func NewAIRequest(agentRuntimeID string, basePrompt string, agentWorkflow types.AgentWorkflowRuntime, toolNames []string, jsonReponseFormat *types.JSONResponseDefinition) *AIRequest {
	initialToolResults := new(sync.Map)
	initialToolCalls := make([]ToolCall, 0)
	initialSystemMessages := make([]types.AIMessage, 0)
	initialUserMessages := make([]types.AIMessage, 0)
	initialAssistantMessages := make([]types.AIMessage, 0)
	initialConversationMessages := make([]types.ConversationMessage, 0)

	//	Return the AI Request
	return &AIRequest{
		AgentRuntimeID:       agentRuntimeID,
		Prompt:               basePrompt,
		SystemMessages:       initialSystemMessages,
		UserMessages:         initialUserMessages,
		AssistantMessages:    initialAssistantMessages,
		ConversationMessages: initialConversationMessages,
		ToolCalls:            initialToolCalls,
		ToolResults:          initialToolResults,
		ToolsNames:           toolNames,
		agentWorkflow:        agentWorkflow,
		JSONResponseFormat:   jsonReponseFormat,
	}
}

// GetAgentWorkflow   Returns the agent workflow
func (ar *AIRequest) GetAgentWorkflow() types.AgentWorkflowRuntime {
	return ar.agentWorkflow
}

// AddSystemMessage		Adds a system Message to the request
func (ar *AIRequest) AddSystemMessage(message string) {
	if message != "" {
		ar.SystemMessages = append(ar.SystemMessages, types.AIMessage{Role: types.SystemRole, Content: message})
	}
}

// AddUserMessage 		Adds a User Message ot the user request
func (ar *AIRequest) AddUserMessage(message string) {
	if message != "" {
		ar.UserMessages = append(ar.UserMessages, types.AIMessage{Role: types.UserRole, Content: message})
	}
}

// AddUserMessages	Adds all the given user message to the request
func (ar *AIRequest) AddUserMessages(messages []types.AIMessage) {
	ar.UserMessages = append(ar.UserMessages, messages...)
}

// AddAssistantMessage  Adds an assistant Message to the request
func (ar *AIRequest) AddAssistantMessage(message string) {
	if message != "" {
		ar.AssistantMessages = append(ar.AssistantMessages, types.AIMessage{Role: types.AssistantRole, Content: message})
	}
}

// AddAssistantMessages Adds an assistant Message to the request
func (ar *AIRequest) AddAssistantMessages(messages []types.AIMessage) {
	ar.AssistantMessages = append(ar.AssistantMessages, messages...)

}

// AddToolResult   Adds a Tool Result to the request
func (ar *AIRequest) AddToolResult(toolResult types.ToolResult) {
	ar.ToolResults.Store(toolResult.Id, &toolResult)
}

// GetToolResult   Returns the tool
func (ar *AIRequest) GetToolResult(toolID string) *types.ToolResult {
	tr, ok := ar.ToolResults.Load(toolID)
	if !ok {
		return nil
	}
	return tr.(*types.ToolResult)
}

// AddToolCall    Adds a Tool Call to the requst
func (ar *AIRequest) AddToolCall(toolCall ToolCall) {
	ar.ToolCalls = append(ar.ToolCalls, toolCall)
}

// AddConversationHistory Add History to the request
func (ar *AIRequest) AddConversationHistory(conversationHistory []types.ConversationMessage) {
	ar.ConversationMessages = append(ar.ConversationMessages, conversationHistory...)
}
