package types


type RoleType string

const (
	SystemRole         RoleType = "system"
	UserRole           RoleType = "user"
	AssistantRole      RoleType = "assistant"
	ToolRole           RoleType = "tool"
	ConversationRole   RoleType = "conversation"
	ContextQuestionKey string   = "question"
)

// AIMessage Type
type AIMessage struct {
	Role    RoleType
	Content string
}
