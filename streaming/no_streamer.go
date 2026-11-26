package streaming

// NoStreamer	 Struct
type NoStreamer struct{}

func (n NoStreamer) Stream(message *AgentAsyncMessage) {}

func (n NoStreamer) CloseChannel() {}

func (n NoStreamer) StreamPlan(planData string) {}

func (n NoStreamer) StreamAudio(audio *AgentAsyncMessage) error { return nil }

func (n NoStreamer) Flush() {}

func (n NoStreamer) ClearAudio() {}

func (n NoStreamer) IsConnected() bool                             { return true }
func (n NoStreamer) AddActiveAgent(agentRuntimeID string)          {}
func (n NoStreamer) IsAgentActive(agentRuntimeID string) bool      { return true }
func (n NoStreamer) RemoveActiveAgent(agentRuntimeID string) error { return nil }
func (n NoStreamer) DeactivateRuntimeIDCheck()                     {}
func (n NoStreamer) ReactivateRuntimeIDCheck()                     {}
