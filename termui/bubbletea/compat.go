package bubbletea

import (
	"context"
	"fmt"

	"sketch.dev/loop"
)

// CompatibilityWrapper provides backward compatibility with the existing TermUI interface
type CompatibilityWrapper struct {
	bubbleUI *BubbleTeaUI
}

// NewCompatibilityWrapper creates a wrapper that maintains the old TermUI interface
func NewCompatibilityWrapper(agent loop.CodingAgent, httpURL string) *CompatibilityWrapper {
	ui := New(agent, httpURL)

	// Initialize all components
	ui.app.chatView = NewMessagesComponent()
	ui.app.inputView = NewInputComponent()
	ui.app.statusBar = NewStatusComponent()

	// Set up component references
	ui.app.chatView.SetAgent(agent)
	ui.app.inputView.SetAgent(agent)
	ui.app.statusBar.SetAgent(agent)

	return &CompatibilityWrapper{
		bubbleUI: ui,
	}
}

// Run starts the UI (compatible with old TermUI.Run signature)
func (c *CompatibilityWrapper) Run(ctx context.Context) error {
	return c.bubbleUI.Run(ctx)
}

// RestoreOldState cleans up the UI (compatible with old TermUI.RestoreOldState signature)
func (c *CompatibilityWrapper) RestoreOldState() error {
	return c.bubbleUI.RestoreOldState()
}

// HandleToolUse processes tool usage messages (compatible with old TermUI.HandleToolUse signature)
func (c *CompatibilityWrapper) HandleToolUse(resp *loop.AgentMessage) {
	// For now, we'll handle this through the message processing system
	// This will be properly integrated in subsequent tasks
	if c.bubbleUI.program != nil {
		c.bubbleUI.program.Send(agentMessageMsg{resp})
	}
}

// AppendChatMessage adds a chat message (compatible with old TermUI interface)
func (c *CompatibilityWrapper) AppendChatMessage(sender, content string, thinking bool) {
	// This will be implemented when we add the chat component
	// For now, we'll create a mock agent message
	msg := &loop.AgentMessage{
		Type:    loop.AgentMessageType,
		Content: content,
	}
	if c.bubbleUI.program != nil {
		c.bubbleUI.program.Send(agentMessageMsg{msg})
	}
}

// AppendSystemMessage adds a system message (compatible with old TermUI interface)
func (c *CompatibilityWrapper) AppendSystemMessage(fmtString string, args ...any) {
	content := fmt.Sprintf(fmtString, args...)
	msg := &loop.AgentMessage{
		Type:    loop.ErrorMessageType, // Using ErrorMessageType for system messages
		Content: content,
	}
	if c.bubbleUI.program != nil {
		c.bubbleUI.program.Send(agentMessageMsg{msg})
	}
}
