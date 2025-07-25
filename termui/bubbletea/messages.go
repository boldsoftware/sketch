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

	// Message caching and memory management
	messageCache   map[int]string // Cache rendered messages by index
	maxHistorySize int            // Maximum number of messages to keep in history

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

	// Implement lazy rendering for large message histories
	// Only render messages that are likely to be visible or close to the viewport
	// This significantly improves performance with large message histories

	// Calculate the number of messages that can fit in the viewport
	// Use a conservative estimate of 5 lines per message
	messagesPerViewport := m.height / 5
	if messagesPerViewport < 5 {
		messagesPerViewport = 5 // Minimum number of messages to render
	}

	// Add a buffer for scrolling
	bufferSize := messagesPerViewport * 2

	// Determine which messages to render
	startIdx := 0
	if len(m.messages) > bufferSize {
		// If we have more messages than can fit in the buffer,
		// only render the most recent ones
		startIdx = len(m.messages) - bufferSize
	}

	// Render only the messages that are likely to be visible
	for i := startIdx; i < len(m.messages); i++ {
		content.WriteString(m.renderMessage(m.messages[i]))
		content.WriteString("\n\n")
	}

	// Create a temporary string builder for the indicator
	var fullContent strings.Builder

	// If we're not showing all messages, add an indicator at the beginning
	if startIdx > 0 {
		indicator := fmt.Sprintf("[ %d earlier messages not shown ]\n\n", startIdx)
		fullContent.WriteString(indicator)
	}

	// Add the main content
	fullContent.WriteString(content.String())

	m.viewport.SetContent(fullContent.String())

	// Auto-scroll to bottom if we're already at the bottom
	if m.viewport.AtBottom() {
		m.viewport.GotoBottom()
	}
}

// renderMessage renders a single message
func (m *MessagesComponent) renderMessage(msg DisplayMessage) string {
	// Check if we have a cached version of this message
	msgIndex := -1
	for i, m := range m.messages {
		if &m == &msg {
			msgIndex = i
			break
		}
	}

	// If we found the message index and it's in the cache, return the cached version
	if msgIndex >= 0 {
		if cached, ok := m.messageCache[msgIndex]; ok {
			return cached
		}
	}

	// Otherwise, render the message
	var rendered string
	switch msg.Type {
	case loop.UserMessageType:
		rendered = m.renderUserMessage(msg)
	case loop.AgentMessageType:
		rendered = m.renderAgentMessage(msg)
	case loop.ToolUseMessageType:
		rendered = m.renderToolMessage(msg)
	case loop.ErrorMessageType:
		rendered = m.renderErrorMessage(msg)
	case loop.CommitMessageType:
		rendered = m.renderCommitMessage(msg)
	default:
		rendered = m.renderSystemMessage(msg)
	}

	// Cache the rendered message if we have an index
	if msgIndex >= 0 {
		m.messageCache[msgIndex] = rendered
	}

	return rendered
}

// renderUserMessage renders a user message in modern chat style
func (m *MessagesComponent) renderUserMessage(msg DisplayMessage) string {
	// Create message content with proper wrapping
	content := m.wrapText(msg.Content, m.width-4) // Account for padding only

	// User header with modern styling - no borders, just color-coded title
	header := m.userStyle.Render("▶ You")

	// Create clean message layout without borders
	messageContent := header + "\n" + content

	// Add subtle padding and margin for readability
	messageStyle := lipgloss.NewStyle().
		PaddingLeft(2).
		PaddingRight(2).
		PaddingBottom(1).
		MarginBottom(1)

	return messageStyle.Render(messageContent)
}

// renderAgentMessage renders an agent message in modern chat style
func (m *MessagesComponent) renderAgentMessage(msg DisplayMessage) string {
	// Create message content with proper wrapping
	content := m.wrapText(msg.Content, m.width-4) // Account for padding only

	// Agent header with modern styling - no borders, just color-coded title
	agentLabel := "▶ Agent"
	if msg.Thinking {
		agentLabel = "⏳ Agent (thinking)"
	}
	header := m.agentStyle.Render(agentLabel)

	// Create clean message layout without borders
	messageContent := header + "\n" + content

	// Add subtle padding and margin for readability
	messageStyle := lipgloss.NewStyle().
		PaddingLeft(2).
		PaddingRight(2).
		PaddingBottom(1).
		MarginBottom(1)

	return messageStyle.Render(messageContent)
}

// renderToolMessage renders a tool use message in modern chat style
func (m *MessagesComponent) renderToolMessage(msg DisplayMessage) string {
	var content strings.Builder

	// Tool header with modern styling - no borders, just color-coded title
	toolLabel := fmt.Sprintf("🛠️ %s", msg.ToolName)
	header := m.systemStyle.Render(toolLabel)
	content.WriteString(header)
	content.WriteString("\n")

	// Tool input with proper wrapping
	if msg.ToolInput != "" {
		inputContent := m.wrapText(msg.ToolInput, m.width-4)
		inputStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")).
			Italic(true)
		content.WriteString("Input: " + inputStyle.Render(inputContent))
		content.WriteString("\n")
	}

	// Tool result or error
	if msg.ToolError {
		errorContent := m.wrapText(msg.ToolResult, m.width-4)
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
		content.WriteString("❌ Error: " + errorStyle.Render(errorContent))
	} else if msg.ToolResult != "" {
		resultContent := m.wrapText(msg.ToolResult, m.width-4)
		resultStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("252"))
		content.WriteString("Result: " + resultStyle.Render(resultContent))
	}

	// Add subtle padding and margin for readability - no borders
	messageStyle := lipgloss.NewStyle().
		PaddingLeft(2).
		PaddingRight(2).
		PaddingBottom(1).
		MarginBottom(1)

	return messageStyle.Render(content.String())
}

// renderErrorMessage renders an error message in modern chat style
func (m *MessagesComponent) renderErrorMessage(msg DisplayMessage) string {
	// Create message content with proper wrapping
	content := m.wrapText(msg.Content, m.width-4) // Account for padding only

	// Error header with modern styling - no borders, just color-coded title
	header := m.errorStyle.Render("❌ Error")

	// Create clean message layout without borders
	messageContent := header + "\n" + content

	// Add subtle padding and margin for readability
	messageStyle := lipgloss.NewStyle().
		PaddingLeft(2).
		PaddingRight(2).
		PaddingBottom(1).
		MarginBottom(1)

	return messageStyle.Render(messageContent)
}

// renderSystemMessage renders a system message in modern chat style
func (m *MessagesComponent) renderSystemMessage(msg DisplayMessage) string {
	// Create message content with proper wrapping
	content := m.wrapText(msg.Content, m.width-4) // Account for padding only

	// System header with modern styling - no borders, just color-coded title
	header := m.systemStyle.Render("ℹ️ System")

	// Create clean message layout without borders
	messageContent := header + "\n" + content

	// Add subtle padding and margin for readability
	messageStyle := lipgloss.NewStyle().
		PaddingLeft(2).
		PaddingRight(2).
		PaddingBottom(1).
		MarginBottom(1)

	return messageStyle.Render(messageContent)
}

// renderCommitMessage renders a git commit message in modern chat style
func (m *MessagesComponent) renderCommitMessage(msg DisplayMessage) string {
	var content strings.Builder

	// Commit header with modern styling - no borders, just color-coded title
	header := m.systemStyle.Render("📝 Git Commits")
	content.WriteString(header)
	content.WriteString("\n")

	for _, commit := range msg.Commits {
		content.WriteString(fmt.Sprintf("%s %s\n",
			m.userStyle.Render(commit.Hash[:7]),
			commit.Subject))

		if commit.PushedBranch != "" {
			content.WriteString(fmt.Sprintf("Pushed to branch: %s\n", commit.PushedBranch))
		}
		content.WriteString("\n")
	}

	// Add subtle padding and margin for readability - no borders
	messageStyle := lipgloss.NewStyle().
		PaddingLeft(2).
		PaddingRight(2).
		PaddingBottom(1).
		MarginBottom(1)

	return messageStyle.Render(content.String())
}

// AddMessage adds a message to the display
func (m *MessagesComponent) AddMessage(msg DisplayMessage) {
	// Initialize message cache if needed
	if m.messageCache == nil {
		m.messageCache = make(map[int]string)
	}

	// Initialize max history size if not set
	if m.maxHistorySize <= 0 {
		m.maxHistorySize = 1000 // Default to 1000 messages
	}

	// Add the message to the history
	m.messages = append(m.messages, msg)

	// Enforce message history limit
	if len(m.messages) > m.maxHistorySize {
		// Remove oldest messages
		excess := len(m.messages) - m.maxHistorySize
		m.messages = m.messages[excess:]

		// Clear cache entries for removed messages
		for i := 0; i < excess; i++ {
			delete(m.messageCache, i)
		}

		// Reindex cache keys
		newCache := make(map[int]string)
		for k, v := range m.messageCache {
			if k >= excess {
				newCache[k-excess] = v
			}
		}
		m.messageCache = newCache
	}

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
	case userInputMsg:
		return m.HandleUserInput(typedMsg.input)
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

// HandleUserInput handles user input messages
func (m *MessagesComponent) HandleUserInput(input string) tea.Cmd {
	// Convert to DisplayMessage
	displayMsg := DisplayMessage{
		Type:      loop.UserMessageType,
		Content:   input,
		Timestamp: time.Now(),
		Sender:    "You",
	}

	m.AddMessage(displayMsg)
	return nil
}

// wrapText wraps text to fit within the specified width
func (m *MessagesComponent) wrapText(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	currentLine := words[0]
	for _, word := range words[1:] {
		if len(currentLine)+1+len(word) <= width {
			currentLine += " " + word
		} else {
			result.WriteString(currentLine)
			result.WriteString("\n")
			currentLine = word
		}
	}
	result.WriteString(currentLine)

	return result.String()
}

// key is a helper struct for viewport key mapping
type key struct {
	key tea.KeyType
}
