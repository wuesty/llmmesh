package types

type AgentWorkflowRuntime interface {
	AddTool(tool TweedyTool)
	GetTool(toolName string) TweedyTool
	AddAgent(agent TweedyAgent)
	GetAgent(agentName string) TweedyAgent
	Agents(f func(key string, value TweedyAgent) bool)
	Strategy() Strategy
	Name() string
}
