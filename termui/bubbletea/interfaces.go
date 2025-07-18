package bubbletea

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"
	"sketch.dev/loop"
)

// UIComponent defines the interface for all UI components in the Bubble Tea implementation
type UIComponent interface {
	tea.Model
	// SetAgent allows components to interact with the coding agent
	SetAgent(agent loop.CodingAgent)
	// SetContext provides the component with a context for cancellation
	SetContext(ctx context.Context)
}

// Message defines a common interface for all message types
type Message interface {
	Type() string
}

// MessageHandler defines how components handle different types of agent messages
type MessageHandler interface {
	HandleMessage(msg Message) tea.Cmd
	HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd
	HandleToolUse(msg *loop.AgentMessage) tea.Cmd
	HandleError(msg *loop.AgentMessage) tea.Cmd
}

// InputHandler defines how components handle user input
type InputHandler interface {
	HandleInput(input string) tea.Cmd
}

// StateManager defines how components manage their internal state
type StateManager interface {
	GetState() interface{}
	SetState(state interface{}) error
	Reset()
}

// UIMessage defines the interface for internal message routing
type UIMessage interface {
	Message
	Type() string
}

// MessageBuffer manages batched message processing
type MessageBuffer struct {
	messages []interface{}
	capacity int
}

// NewMessageBuffer creates a new message buffer with the specified capacity
func NewMessageBuffer(capacity int) *MessageBuffer {
	return &MessageBuffer{
		messages: make([]interface{}, 0, capacity),
		capacity: capacity,
	}
}

// Add adds a message to the buffer
func (mb *MessageBuffer) Add(msg interface{}) {
	if len(mb.messages) < mb.capacity {
		mb.messages = append(mb.messages, msg)
	}
}

// Get returns all messages in the buffer
func (mb *MessageBuffer) Get() []interface{} {
	return mb.messages
}

// Size returns the number of messages in the buffer
func (mb *MessageBuffer) Size() int {
	return len(mb.messages)
}

// Clear empties the buffer
func (mb *MessageBuffer) Clear() {
	mb.messages = mb.messages[:0]
}
