package types

type PlanResponse struct {
	Plan Plan `json:"plan" jsonschema_description:"The overall plan including the main question and execution steps"`
}

type Plan struct {
	Question         string `json:"question" jsonschema_description:"The question the plan is designed to answer"`
	VoiceDescription string `json:"voice_description" jsonschema_description:"What somebody would tell the user they were doing for them while the plan was being executed."`
	Steps            []Step `json:"steps" jsonschema_description:"A list of steps required to carry out the plan"`
}

type Step struct {
	Agent   string `json:"agent" jsonschema_description:"The agent responsible for executing this step"`
	Command string `json:"command" jsonschema_description:"The command or instruction given to the agent"`
}

type StepResult struct {
	Result   string `json:"result" jsonschema_description:"The result of the command or instruction given to the agent (if it produced a result)"`
	Question string `json:"question" jsonschema_description:"A question back to the user (this happens if the agent needs clarification and cannot produce a result)"`
}

type PlanExecutionResponse struct {
	Data string `json:"data" jsonschema_description:"Detailed data to support your answer"`
}
