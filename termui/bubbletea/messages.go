package bubbletea

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// MessageType defines the type of message for internal routing
type MessageType string

const (
	// Message types for internal routing
	AgentMessageType    MessageType = "agent_message"
	UserInputType       MessageType = "user_input"
	CommandType         MessageType = "command"
	SystemMessageType   MessageType = "system_message"
	ToolUseType         MessageType = "tool_use"
	StateTransitionType MessageType = "state_transition"
)

// MessagesComponent displays chat messages and tool outputs
type MessagesComponent struct {
	agent        loop.CodingAgent
	ctx          context.Context
	viewport     viewport.Model
	messages     []DisplayMessage
	toolRenderer *ToolTemplateRenderer
	width        int
	height       int

	// Styling
	userStyle   lipgloss.Style
	agentStyle  lipgloss.Style
	systemStyle lipgloss.Style
	errorStyle  lipgloss.Style
}

// DisplayMessage represents a message to be displayed
type DisplayMessage struct {
	Type      loop.CodingAgentMessageType
	Content   string
	Timestamp time.Time
	Sender    string
	Thinking  bool

	// Tool-specific fields
	ToolName   string
	ToolInput  string
	ToolResult string
	ToolError  bool

	// Git commit fields
	Commits []*loop.GitCommit
}

// Init initializes the messages component
func (m *MessagesComponent) Init() tea.Cmd {
	return nil
}

// Update handles messages for the messages component
func (m *MessagesComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height - 4 // Reserve space for status and input
		m.viewport.Width = m.width
		m.viewport.Height = m.height
		m.updateViewportContent()

	case tea.KeyMsg:
		// Handle keyboard navigation
		switch msg.String() {
		case "up", "k":
			m.viewport.ScrollUp(1)
		case "down", "j":
			m.viewport.ScrollDown(1)
		case "pgup":
			m.viewport.PageUp()
		case "pgdn":
			m.viewport.PageDown()
		case "ctrl+u":
			m.viewport.HalfPageUp()
		case "ctrl+d":
			m.viewport.HalfPageDown()
		case "home":
			m.viewport.GotoTop()
		case "end":
			m.viewport.GotoBottom()
		}
	}

	// Update viewport
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// View renders the messages component
func (m *MessagesComponent) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	return m.viewport.View()
}

// SetAgent sets the agent reference
func (m *MessagesComponent) SetAgent(agent loop.CodingAgent) {
	m.agent = agent
}

// SetContext sets the context for the component
func (m *MessagesComponent) SetContext(ctx context.Context) {
	m.ctx = ctx
}

// updateViewportContent updates the viewport content with rendered messages
func (m *MessagesComponent) updateViewportContent() {
	var content strings.Builder

	for _, msg := range m.messages {
		content.WriteString(m.renderMessage(msg))
		content.WriteString("\n\n")
	}

	m.viewport.SetContent(content.String())

	// Auto-scroll to bottom if we're already at the bottom
	if m.viewport.AtBottom() {
		m.viewport.GotoBottom()
	}
}

// renderMessage renders a single message
func (m *MessagesComponent) renderMessage(msg DisplayMessage) string {
	switch msg.Type {
	case loop.UserMessageType:
		return m.renderUserMessage(msg)
	case loop.AgentMessageType:
		return m.renderAgentMessage(msg)
	case loop.ToolUseMessageType:
		return m.renderToolMessage(msg)
	case loop.ErrorMessageType:
		return m.renderErrorMessage(msg)
	case loop.CommitMessageType:
		return m.renderCommitMessage(msg)
	default:
		return m.renderSystemMessage(msg)
	}
}

// renderUserMessage renders a user message
func (m *MessagesComponent) renderUserMessage(msg DisplayMessage) string {
	return m.userStyle.Render("🦸 You:") + " " + msg.Content
}

// renderAgentMessage renders an agent message
func (m *MessagesComponent) renderAgentMessage(msg DisplayMessage) string {
	prefix := "🕴️ Agent:"
	if msg.Thinking {
		prefix = "⏳ Agent (thinking):"
	}
	return m.agentStyle.Render(prefix) + " " + msg.Content
}

// renderToolMessage renders a tool use message
func (m *MessagesComponent) renderToolMessage(msg DisplayMessage) string {
	if m.toolRenderer != nil {
		// Convert to loop.AgentMessage for rendering
		agentMsg := &loop.AgentMessage{
			Type:       loop.ToolUseMessageType,
			ToolName:   msg.ToolName,
			ToolInput:  msg.ToolInput,
			ToolResult: msg.ToolResult,
			ToolError:  msg.ToolError,
		}
		return m.toolRenderer.RenderTool(agentMsg)
	}

	// Fallback rendering
	var result strings.Builder
	result.WriteString(fmt.Sprintf("🛠️ %s: %s\n", msg.ToolName, msg.ToolInput))
	if msg.ToolError {
		result.WriteString(m.errorStyle.Render("❌ Error: " + msg.ToolResult))
	} else {
		result.WriteString(msg.ToolResult)
	}
	return result.String()
}

// renderErrorMessage renders an error message
func (m *MessagesComponent) renderErrorMessage(msg DisplayMessage) string {
	return m.errorStyle.Render("❌ Error: " + msg.Content)
}

// renderSystemMessage renders a system message
func (m *MessagesComponent) renderSystemMessage(msg DisplayMessage) string {
	return m.systemStyle.Render("ℹ️ " + msg.Content)
}

// renderCommitMessage renders a git commit message
func (m *MessagesComponent) renderCommitMessage(msg DisplayMessage) string {
	var result strings.Builder
	result.WriteString(m.systemStyle.Render("📝 Git Commits:"))
	result.WriteString("\n")

	for _, commit := range msg.Commits {
		result.WriteString(fmt.Sprintf("  %s %s\n",
			m.userStyle.Render(commit.Hash[:7]),
			commit.Subject))

		if commit.PushedBranch != "" {
			result.WriteString(m.systemStyle.Render(
				fmt.Sprintf("  Pushed to branch: %s\n", commit.PushedBranch)))
		}
	}

	return result.String()
}

// AddMessage adds a message to the display
func (m *MessagesComponent) AddMessage(msg DisplayMessage) {
	m.messages = append(m.messages, msg)
	m.updateViewportContent()
}

// HandleMessage implements MessageHandler
func (m *MessagesComponent) HandleMessage(msg Message) tea.Cmd {
	// Route message based on type
	switch typedMsg := msg.(type) {
	case agentMessageMsg:
		return m.HandleAgentMessage(typedMsg.message)
	case toolUseMsg:
		return m.HandleToolUse(typedMsg.message)
	case systemMessageMsg:
		// Convert system message to error message for handling
		agentMsg := &loop.AgentMessage{
			Type:    loop.ErrorMessageType,
			Content: typedMsg.content,
		}
		return m.HandleError(agentMsg)
	}
	return nil
}

// HandleAgentMessage handles agent messages
func (m *MessagesComponent) HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd {
	// Convert to DisplayMessage
	displayMsg := DisplayMessage{
		Type:      msg.Type,
		Content:   msg.Content,
		Timestamp: msg.Timestamp,
		Thinking:  false,
		Commits:   msg.Commits,
	}

	m.AddMessage(displayMsg)
	return nil
}

// HandleToolUse handles tool use messages
func (m *MessagesComponent) HandleToolUse(msg *loop.AgentMessage) tea.Cmd {
	// Convert to DisplayMessage
	displayMsg := DisplayMessage{
		Type:       loop.ToolUseMessageType,
		ToolName:   msg.ToolName,
		ToolInput:  msg.ToolInput,
		ToolResult: msg.ToolResult,
		ToolError:  msg.ToolError,
		Timestamp:  msg.Timestamp,
	}

	m.AddMessage(displayMsg)
	return nil
}

// HandleError handles error messages
func (m *MessagesComponent) HandleError(msg *loop.AgentMessage) tea.Cmd {
	// Convert to DisplayMessage
	displayMsg := DisplayMessage{
		Type:      loop.ErrorMessageType,
		Content:   msg.Content,
		Timestamp: msg.Timestamp,
	}

	m.AddMessage(displayMsg)
	return nil
}

// key is a helper struct for viewport key mapping
type key struct {
	key tea.KeyType
}
