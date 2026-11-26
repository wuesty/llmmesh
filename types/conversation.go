package types


import (
	"encoding/json"
	"errors"
)

const (
	ContextConversationHistoryKey string = "conversation_history"
)

type ConversationMessage struct {
	UserMessage      string `json:"userMessage" dynamodbav:"userMessage"`
	AssistantMessage string `json:"assistantMessage" dynamodbav:"assistantMessage"`
	FeedbackReaction string `json:"feedbackReaction,omitempty" dynamodbav:"feedbackReaction,omitempty"` // "Liked", "Disliked", or empty
	Timestamp        int64  `json:"timestamp,omitempty" dynamodbav:"timestamp,omitempty"`                // Unix timestamp in milliseconds
}

func NewConversationEntry(userMsg string, assistantMsg string) *ConversationMessage {
	return &ConversationMessage{
		UserMessage:      userMsg,
		AssistantMessage: assistantMsg,
	}
}

func NewConversationEntryFromString(jsonStr string) (*ConversationMessage, error) {
	var parts []string
	err := json.Unmarshal([]byte(jsonStr), &parts)
	if err != nil {
		return nil, err
	}

	if len(parts) != 2 {
		return nil, errors.New("input must be a JSON array of exactly 2 strings: [userMessage, assistantMessage]")
	}

	return &ConversationMessage{
		UserMessage:      parts[0],
		AssistantMessage: parts[1],
	}, nil
}

func (e *ConversationMessage) ToString() (string, error) {
	if e.UserMessage == "" || e.AssistantMessage == "" {
		return "", errors.New("both user and assistant messages must be set")
	}

	parts := []string{e.UserMessage, e.AssistantMessage}
	jsonBytes, err := json.Marshal(parts)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}
