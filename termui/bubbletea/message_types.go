package bubbletea

import (
	"sketch.dev/loop"
)

// Custom message types for Bubble Tea
type agentMessageMsg struct {
	message *loop.AgentMessage
}

func (m agentMessageMsg) Type() string { return string(AgentMessageType) }

type userInputMsg struct {
	input string
}

func (m userInputMsg) Type() string { return string(UserInputType) }

type commandMsg struct {
	command string
}

func (m commandMsg) Type() string { return string(CommandType) }

type systemMessageMsg struct {
	content string
}

func (m systemMessageMsg) Type() string { return string(SystemMessageType) }

type toolUseMsg struct {
	message *loop.AgentMessage
}

func (m toolUseMsg) Type() string { return string(ToolUseType) }

type stateTransitionMsg struct {
	transition *loop.StateTransition
}

func (m stateTransitionMsg) Type() string { return string(StateTransitionType) }
