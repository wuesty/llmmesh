package types

import "time"

type PromptGenerator interface {
	Generate() (string, error)
}

// Prompt represents a text-based prompt
type Prompt struct {
	Text      string    `json:"text"`
	UpdatedAt time.Time `json:"updatedAt"`
}
