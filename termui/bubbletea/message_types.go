package bubbletea

import (
	"time"

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

// AgentResponseCompleteMsg is sent when the agent has finished responding
type AgentResponseCompleteMsg struct{}

func (m AgentResponseCompleteMsg) Type() string { return "agent_response_complete" }

// inputStateResetMsg is sent to reset the input component state
type inputStateResetMsg struct{}

func (m inputStateResetMsg) Type() string { return "input_state_reset" }

// userMessageDisplayMsg is sent to immediately display a user message
type userMessageDisplayMsg struct {
	input     string
	timestamp time.Time
}

func (m userMessageDisplayMsg) Type() string { return "user_message_display" }
