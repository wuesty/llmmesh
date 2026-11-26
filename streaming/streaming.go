package streaming

import (
	"github.com/google/uuid"
	"time"
)

type AsyncMessageType int32

const (
	PlanMessageType       AsyncMessageType = 0
	StepUpdateMessageType                  = 1
	OutputMessageType                      = 2
	CompletedMessageType                   = 3
	ErrorMessageType                       = 4
	FlushMessageType                       = 5
	AudioMessageType                       = 6
	AgentFinishedType                      = 7
)

type StepUpdateState string

const (
	StartedState   StepUpdateState = "STARTED"
	CompletedState                 = "COMPLETED"
)

type StepUpdateMessage struct {
	Id              string          `json:"id"`
	Text            string          `json:"text"`
	StepUpdateState StepUpdateState `json:"step_update_state"`
}

type AsyncErrorMessage struct {
	Message string `json:"message"`
	Cause   string `json:"cause"`
}

type AgentAsyncMessage struct {
	AgentRuntimeID string            `json:"agent_name"`
	Type           AsyncMessageType  `json:"type,omitempty"`
	Plan           interface{}       `json:"plan,omitempty"`
	StepUpdate     StepUpdateMessage `json:"step_update,omitempty"`
	Output         interface{}       `json:"output,omitempty"`
	Error          AsyncErrorMessage `json:"error,omitempty"`
	Timestamp      time.Time         `json:"timestamp,omitempty"`
}

type Streamer interface {
	Stream(message *AgentAsyncMessage)
	CloseChannel()
	StreamPlan(planData string)
	StreamAudio(message *AgentAsyncMessage) error
	Flush()
	ClearAudio()
	IsConnected() bool
	//	For Asynchronous execution
	AddActiveAgent(agentRuntimeID string)
	IsAgentActive(agentRuntimeID string) bool
	RemoveActiveAgent(agentRuntimeID string) error
	DeactivateRuntimeIDCheck()
	ReactivateRuntimeIDCheck()
}

func SendError(errorMessage *AgentAsyncMessage, streamer Streamer) {
	start, stop := NewStepUpdate(errorMessage.AgentRuntimeID, "Error")
	streamer.Stream(start)
	streamer.Stream(errorMessage)
	streamer.Stream(stop)
}

func SendOutput(errorMessage *AgentAsyncMessage, streamer Streamer) {
	start, stop := NewStepUpdate(errorMessage.AgentRuntimeID, "Output")
	streamer.Stream(start)
	streamer.Stream(errorMessage)
	streamer.Stream(stop)
}

func NewStepUpdate(agentRuntimeID string, title string) (*AgentAsyncMessage, *AgentAsyncMessage) {
	stepId := uuid.New().String()
	return &AgentAsyncMessage{
			AgentRuntimeID: agentRuntimeID,
			Type:           StepUpdateMessageType,
			Output:         "",
			StepUpdate: StepUpdateMessage{
				Id:              stepId,
				Text:            title,
				StepUpdateState: StartedState,
			},
		}, &AgentAsyncMessage{
			AgentRuntimeID: agentRuntimeID,
			Type:           StepUpdateMessageType,
			Output:         "",
			StepUpdate: StepUpdateMessage{
				Id:              stepId,
				Text:            title,
				StepUpdateState: CompletedState,
			},
		}
}

func NewError(agentRuntimeID string, message string, err error) *AgentAsyncMessage {
	return &AgentAsyncMessage{
		AgentRuntimeID: agentRuntimeID,
		Type:           ErrorMessageType,
		Output:         "",
		Error: AsyncErrorMessage{
			Message: message,
			Cause:   err.Error(),
		},
	}
}

func NewCompletionMessage(agentRuntimeID string) *AgentAsyncMessage {
	return &AgentAsyncMessage{
		AgentRuntimeID: agentRuntimeID,
		Output:         "",
		Type:           CompletedMessageType,
	}
}

func NewOutputMessage(agentRuntimeID string, content string) *AgentAsyncMessage {
	return &AgentAsyncMessage{
		AgentRuntimeID: agentRuntimeID,
		Type:           OutputMessageType,
		Output:         content,
	}
}

func NewAudioMessage(agentRuntimeID string, audio []byte) *AgentAsyncMessage {
	return &AgentAsyncMessage{
		AgentRuntimeID: agentRuntimeID,
		Type:           OutputMessageType,
		Output:         audio,
	}
}

func NewPlanMessage(agentRuntimeID string, content string) *AgentAsyncMessage {
	return &AgentAsyncMessage{
		AgentRuntimeID: agentRuntimeID,
		Type:           PlanMessageType,
		Output:         content,
	}
}

func NewAgentFinishedMessage(agentRuntimeID string) *AgentAsyncMessage {
	return &AgentAsyncMessage{
		AgentRuntimeID: agentRuntimeID,
		Type:           AgentFinishedType,
		Output:         "done",
	}
}
