package bubbletea

import (
	"context"
	"encoding/json"
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
		// Enhanced keyboard navigation for better UX
		switch msg.String() {
		case "up", "k":
			m.viewport.ScrollUp(1)
			return m, nil // Don't pass to input component
		case "down", "j":
			m.viewport.ScrollDown(1)
			return m, nil // Don't pass to input component
		case "pgup", "ctrl+b":
			m.viewport.PageUp()
			return m, nil
		case "pgdn", "ctrl+f", "space":
			m.viewport.PageDown()
			return m, nil
		case "ctrl+u":
			m.viewport.HalfPageUp()
			return m, nil
		case "ctrl+d":
			m.viewport.HalfPageDown()
			return m, nil
		case "home", "g":
			m.viewport.GotoTop()
			return m, nil
		case "end", "G":
			m.viewport.GotoBottom()
			return m, nil
		}
	}

	// Update viewport with mouse support
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

// renderUserMessage renders a user message in hacker style
func (m *MessagesComponent) renderUserMessage(msg DisplayMessage) string {
	var content strings.Builder

	// User message header with timestamp and hacker styling
	timestamp := msg.Timestamp.Format("15:04:05")
	header := fmt.Sprintf("╔══ [%s] 👤 You ══╗", timestamp)
	content.WriteString(m.userStyle.Render(header))
	content.WriteString("\n")

	// Message content with proper wrapping and border
	messageText := m.wrapText(msg.Content, m.width-6)
	lines := strings.Split(messageText, "\n")
	for _, line := range lines {
		content.WriteString(m.userStyle.Render("║ "))
		content.WriteString(line)
		content.WriteString("\n")
	}
	content.WriteString(m.userStyle.Render("╚════════════════════════════════════════════════════════════════════════════════╝"))
	content.WriteString("\n")

	return content.String()
}

// renderAgentMessage renders an agent message in hacker style
func (m *MessagesComponent) renderAgentMessage(msg DisplayMessage) string {
	var content strings.Builder

	// Agent message header with timestamp and hacker styling
	timestamp := msg.Timestamp.Format("15:04:05")
	header := fmt.Sprintf("╔══ [%s] Kifaru ══╗", timestamp)
	content.WriteString(m.agentStyle.Render(header))
	content.WriteString("\n")

	// Message content with proper wrapping and border
	messageText := m.wrapText(msg.Content, m.width-6)
	lines := strings.Split(messageText, "\n")
	for _, line := range lines {
		content.WriteString(m.agentStyle.Render("║ "))
		content.WriteString(line)
		content.WriteString("\n")
	}
	content.WriteString(m.agentStyle.Render("╚════════════════════════════════════════════════════════════════════════════════╝"))
	content.WriteString("\n")

	return content.String()
}

// renderToolMessage renders a tool use message with enhanced formatting for bash commands
func (m *MessagesComponent) renderToolMessage(msg DisplayMessage) string {
	var content strings.Builder

	// Tool message header with timestamp and hacker styling
	timestamp := msg.Timestamp.Format("15:04:05")
	header := fmt.Sprintf("╔══ [%s] 🛠️ %s ══╗", timestamp, strings.ToUpper(msg.ToolName))
	content.WriteString(m.systemStyle.Render(header))
	content.WriteString("\n")

	// Enhanced input section for specialized tools
	if msg.ToolInput != "" {
		if m.isBashTool(msg.ToolName) {
			m.renderBashToolInput(&content, msg.ToolInput)
		} else if m.isPentestTool(msg.ToolName) {
			m.renderPentestToolInput(&content, msg.ToolInput)
		} else {
			// Generic tool input rendering
			m.renderGenericToolInput(&content, msg.ToolInput)
		}
	}

	// Enhanced result section
	if msg.ToolError {
		m.renderToolError(&content, msg.ToolResult)
	} else if msg.ToolResult != "" {
		if m.isBashTool(msg.ToolName) {
			m.renderBashToolResult(&content, msg.ToolResult)
		} else if m.isPentestTool(msg.ToolName) {
			m.renderPentestToolResult(&content, msg.ToolResult)
		} else {
			m.renderGenericToolResult(&content, msg.ToolResult)
		}
	}

	content.WriteString(m.systemStyle.Render("╚════════════════════════════════════════════════════════════════════════════════╝"))
	content.WriteString("\n")

	return content.String()
}

// renderErrorMessage renders an error message with hacker theme styling
func (m *MessagesComponent) renderErrorMessage(msg DisplayMessage) string {
	var content strings.Builder

	// Error message header with timestamp and hacker styling
	timestamp := msg.Timestamp.Format("15:04:05")
	header := fmt.Sprintf("╔══ [%s] ❌ Error ══╗", timestamp)
	content.WriteString(m.errorStyle.Render(header))
	content.WriteString("\n")

	// Message content with proper wrapping and border
	messageText := m.wrapText(msg.Content, m.width-6)
	lines := strings.Split(messageText, "\n")
	for _, line := range lines {
		content.WriteString(m.errorStyle.Render("║ "))
		content.WriteString(line)
		content.WriteString("\n")
	}
	content.WriteString(m.errorStyle.Render("╚════════════════════════════════════════════════════════════════════════════════╝"))
	content.WriteString("\n")

	return content.String()
}

// renderSystemMessage renders a system message with hacker theme styling
func (m *MessagesComponent) renderSystemMessage(msg DisplayMessage) string {
	var content strings.Builder

	// System message header with timestamp and hacker styling
	timestamp := msg.Timestamp.Format("15:04:05")
	header := fmt.Sprintf("╔══ [%s] ℹ️ System ══╗", timestamp)
	content.WriteString(m.systemStyle.Render(header))
	content.WriteString("\n")

	// Message content with proper wrapping and border
	messageText := m.wrapText(msg.Content, m.width-6)
	lines := strings.Split(messageText, "\n")
	for _, line := range lines {
		content.WriteString(m.systemStyle.Render("║ "))
		content.WriteString(line)
		content.WriteString("\n")
	}
	content.WriteString(m.systemStyle.Render("╚════════════════════════════════════════════════════════════════════════════════╝"))
	content.WriteString("\n")

	return content.String()
}

// renderCommitMessage renders a git commit message with hacker theme styling
func (m *MessagesComponent) renderCommitMessage(msg DisplayMessage) string {
	var content strings.Builder

	// Commit message header with timestamp and hacker styling
	timestamp := msg.Timestamp.Format("15:04:05")
	header := fmt.Sprintf("╔══ [%s] 📝 Git ══╗", timestamp)
	content.WriteString(m.systemStyle.Render(header))
	content.WriteString("\n")

	// Render each commit with proper wrapping and border
	for _, commit := range msg.Commits {
		content.WriteString(m.systemStyle.Render("║ "))
		content.WriteString(m.userStyle.Render(commit.Hash[:7]))
		content.WriteString(" ")
		content.WriteString(commit.Subject)
		content.WriteString("\n")

		if commit.PushedBranch != "" {
			branchLine := fmt.Sprintf("Pushed to branch: %s", commit.PushedBranch)
			content.WriteString(m.systemStyle.Render("║ "))
			content.WriteString(branchLine)
			content.WriteString("\n")
		}
	}

	content.WriteString(m.systemStyle.Render("╚════════════════════════════════════════════════════════════════════════════════╝"))
	content.WriteString("\n")

	return content.String()
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

// isBashTool checks if the tool is a bash-related tool that needs special formatting
func (m *MessagesComponent) isBashTool(toolName string) bool {
	return toolName == "bash" || toolName == "shell" || toolName == "command"
}

// isPentestTool checks if the tool is the pentest tool that needs special formatting
func (m *MessagesComponent) isPentestTool(toolName string) bool {
	return toolName == "pentest"
}

// renderBashToolInput renders bash tool input with enhanced formatting
func (m *MessagesComponent) renderBashToolInput(content *strings.Builder, input string) {
	// Try to parse JSON input for bash tool
	var bashInput map[string]interface{}
	if err := json.Unmarshal([]byte(input), &bashInput); err == nil {
		// Successfully parsed JSON, extract command
		if command, ok := bashInput["command"].(string); ok {
			// Command section header
			cmdHeaderStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00FFFF")).
				Bold(true)
			content.WriteString(m.systemStyle.Render("║ "))
			content.WriteString(cmdHeaderStyle.Render("💻 Command:"))
			content.WriteString("\n")

			// Command with syntax highlighting style
			cmdStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#39FF14")).
				Background(lipgloss.Color("#1A1A1A")).
				Padding(0, 1)
			cmdLines := strings.Split(command, "\n")
			for _, line := range cmdLines {
				content.WriteString(m.systemStyle.Render("║ "))
				content.WriteString(cmdStyle.Render("$ " + line))
				content.WriteString("\n")
			}

			// Add separator
			content.WriteString(m.systemStyle.Render("╠"))
			content.WriteString(m.systemStyle.Render(strings.Repeat("═", 78)))
			content.WriteString(m.systemStyle.Render("╣"))
			content.WriteString("\n")
		}
	} else {
		// Fallback to generic input rendering
		m.renderGenericToolInput(content, input)
	}
}

// renderPentestToolInput renders pentest tool input with action-based formatting
func (m *MessagesComponent) renderPentestToolInput(content *strings.Builder, input string) {
	// Try to parse JSON input for pentest tool
	var pentestInput map[string]interface{}
	if err := json.Unmarshal([]byte(input), &pentestInput); err == nil {
		// Successfully parsed JSON, extract action and data
		if action, ok := pentestInput["action"].(string); ok {
			// Action section header
			actionHeaderStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00FFFF")).
				Bold(true)
			content.WriteString(m.systemStyle.Render("║ "))
			content.WriteString(actionHeaderStyle.Render("🎯 Action:"))
			content.WriteString("\n")

			// Action with highlighting
			actionStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF6B35")).
				Background(lipgloss.Color("#1A1A1A")).
				Padding(0, 1).
				Bold(true)
			content.WriteString(m.systemStyle.Render("║ "))
			content.WriteString(actionStyle.Render(strings.ToUpper(action)))
			content.WriteString("\n")

			// Data section if present
			if data, ok := pentestInput["data"]; ok && data != nil {
				// Data header
				dataHeaderStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color("#00FF41")).
					Bold(true)
				content.WriteString(m.systemStyle.Render("║ "))
				content.WriteString(dataHeaderStyle.Render("📊 Data:"))
				content.WriteString("\n")

				// Format data as JSON with indentation
				if dataBytes, err := json.MarshalIndent(data, "", "  "); err == nil {
					dataStyle := lipgloss.NewStyle().
						Foreground(lipgloss.Color("#C9D1D9")).
						Background(lipgloss.Color("#0D1117"))
					dataLines := strings.Split(string(dataBytes), "\n")
					for _, line := range dataLines {
						if strings.TrimSpace(line) != "" {
							content.WriteString(m.systemStyle.Render("║ "))
							content.WriteString(dataStyle.Render(line))
							content.WriteString("\n")
						}
					}
				}
			}

			// Add separator
			content.WriteString(m.systemStyle.Render("╠"))
			content.WriteString(m.systemStyle.Render(strings.Repeat("═", 78)))
			content.WriteString(m.systemStyle.Render("╣"))
			content.WriteString("\n")
		}
	} else {
		// Fallback to generic input rendering
		m.renderGenericToolInput(content, input)
	}
}

// renderGenericToolInput renders generic tool input
func (m *MessagesComponent) renderGenericToolInput(content *strings.Builder, input string) {
	inputHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FFFF")).
		Bold(true)
	content.WriteString(m.systemStyle.Render("║ "))
	content.WriteString(inputHeaderStyle.Render("📥 Input:"))
	content.WriteString("\n")

	inputText := m.wrapText(input, m.width-6)
	lines := strings.Split(inputText, "\n")
	for _, line := range lines {
		content.WriteString(m.systemStyle.Render("║ "))
		content.WriteString(line)
		content.WriteString("\n")
	}

	// Add separator
	content.WriteString(m.systemStyle.Render("╠"))
	content.WriteString(m.systemStyle.Render(strings.Repeat("═", 78)))
	content.WriteString(m.systemStyle.Render("╣"))
	content.WriteString("\n")
}

// renderBashToolResult renders bash tool result with enhanced formatting
func (m *MessagesComponent) renderBashToolResult(content *strings.Builder, result string) {
	// Output section header
	outputHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF41")).
		Bold(true)
	content.WriteString(m.systemStyle.Render("║ "))
	content.WriteString(outputHeaderStyle.Render("📤 Output:"))
	content.WriteString("\n")

	// Style for command output
	outputStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C9D1D9")).
		Background(lipgloss.Color("#0D1117"))

	// Process output line by line for better formatting
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		// Wrap long lines
		if len(line) > m.width-6 {
			wrappedLines := m.wrapText(line, m.width-6)
			for _, wrappedLine := range strings.Split(wrappedLines, "\n") {
				content.WriteString(m.systemStyle.Render("║ "))
				content.WriteString(outputStyle.Render(wrappedLine))
				content.WriteString("\n")
			}
		} else {
			content.WriteString(m.systemStyle.Render("║ "))
			content.WriteString(outputStyle.Render(line))
			content.WriteString("\n")
		}
	}
}

// renderPentestToolResult renders pentest tool result with specialized formatting
func (m *MessagesComponent) renderPentestToolResult(content *strings.Builder, result string) {
	// Result section header
	resultHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF41")).
		Bold(true)
	content.WriteString(m.systemStyle.Render("║ "))
	content.WriteString(resultHeaderStyle.Render("✅ Result:"))
	content.WriteString("\n")

	// Try to parse result as JSON for better formatting
	var resultData interface{}
	if err := json.Unmarshal([]byte(result), &resultData); err == nil {
		// Successfully parsed JSON, format with indentation
		if formattedBytes, err := json.MarshalIndent(resultData, "", "  "); err == nil {
			resultStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00FF41")).
				Background(lipgloss.Color("#0D1117"))

			lines := strings.Split(string(formattedBytes), "\n")
			for _, line := range lines {
				if strings.TrimSpace(line) != "" {
					content.WriteString(m.systemStyle.Render("║ "))
					content.WriteString(resultStyle.Render(line))
					content.WriteString("\n")
				}
			}
		} else {
			// JSON parsing succeeded but formatting failed, show raw
			m.renderRawResult(content, result)
		}
	} else {
		// Not JSON, render as plain text with highlighting for key information
		m.renderRawResult(content, result)
	}
}

// renderRawResult renders raw text result with basic formatting
func (m *MessagesComponent) renderRawResult(content *strings.Builder, result string) {
	resultStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C9D1D9")).
		Background(lipgloss.Color("#0D1117"))

	lines := strings.Split(result, "\n")
	for _, line := range lines {
		// Wrap long lines
		if len(line) > m.width-6 {
			wrappedLines := m.wrapText(line, m.width-6)
			for _, wrappedLine := range strings.Split(wrappedLines, "\n") {
				content.WriteString(m.systemStyle.Render("║ "))
				content.WriteString(resultStyle.Render(wrappedLine))
				content.WriteString("\n")
			}
		} else {
			content.WriteString(m.systemStyle.Render("║ "))
			content.WriteString(resultStyle.Render(line))
			content.WriteString("\n")
		}
	}
}

// renderGenericToolResult renders generic tool result
func (m *MessagesComponent) renderGenericToolResult(content *strings.Builder, result string) {
	resultHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FF41")).
		Bold(true)
	content.WriteString(m.systemStyle.Render("║ "))
	content.WriteString(resultHeaderStyle.Render("📤 Result:"))
	content.WriteString("\n")

	resultText := m.wrapText(result, m.width-6)
	lines := strings.Split(resultText, "\n")
	for _, line := range lines {
		content.WriteString(m.systemStyle.Render("║ "))
		content.WriteString(line)
		content.WriteString("\n")
	}
}

// renderToolError renders tool error with enhanced formatting
func (m *MessagesComponent) renderToolError(content *strings.Builder, errorMsg string) {
	errorHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0040")).
		Bold(true)
	content.WriteString(m.errorStyle.Render("║ "))
	content.WriteString(errorHeaderStyle.Render("❌ Error:"))
	content.WriteString("\n")

	errorText := m.wrapText(errorMsg, m.width-6)
	lines := strings.Split(errorText, "\n")
	for _, line := range lines {
		content.WriteString(m.errorStyle.Render("║ "))
		content.WriteString(line)
		content.WriteString("\n")
	}
}
