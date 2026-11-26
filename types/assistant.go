package types

type AssistantResponse struct {
	Answer   string `json:"answer" jsonschema_description:"The answer to the question "`
	Question string `json:"question" jsonschema_description:"Question back to the user for clarification"`
}

type AssistantInput struct {
	Question string `json:"question"`
	Data     string `json:"data"`
}
