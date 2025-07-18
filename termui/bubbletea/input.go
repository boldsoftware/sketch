package bubbletea

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// InputComponent handles user input
type InputComponent struct {
	agent      loop.CodingAgent
	ctx        context.Context
	width      int
	prompt     string
	input      string
	cursor     int
	thinking   bool
	history    []string
	historyIdx int

	// Styling
	promptStyle lipgloss.Style
	inputStyle  lipgloss.Style
}

// Init initializes the input component
func (i *InputComponent) Init() tea.Cmd {
	return nil
}

// Update handles messages for the input component
func (i *InputComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return i.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		i.width = msg.Width
	}
	return i, nil
}

// View renders the input component
func (i *InputComponent) View() string {
	if i.thinking {
		return i.promptStyle.Render("⏳ Thinking...") + " " + i.inputStyle.Render("(Press Ctrl+C to cancel)")
	}

	// Build the input line with cursor
	var inputLine strings.Builder
	inputLine.WriteString(i.input[:i.cursor])
	inputLine.WriteString("█") // Cursor
	if i.cursor < len(i.input) {
		inputLine.WriteString(i.input[i.cursor:])
	}

	return i.promptStyle.Render(i.prompt) + " " + i.inputStyle.Render(inputLine.String())
}

// SetAgent sets the agent reference
func (i *InputComponent) SetAgent(agent loop.CodingAgent) {
	i.agent = agent
}

// SetContext sets the context for the component
func (i *InputComponent) SetContext(ctx context.Context) {
	i.ctx = ctx
}

// SetPrompt sets the prompt text and thinking state
func (i *InputComponent) SetPrompt(prompt string, thinking bool) {
	i.prompt = prompt
	i.thinking = thinking
}

// handleKeyPress processes keyboard input
func (i *InputComponent) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Ignore input while thinking
	if i.thinking {
		switch msg.String() {
		case "ctrl+c":
			// Cancel current operation
			if i.agent != nil {
				i.agent.CancelTurn(nil)
			}
			i.thinking = false
		}
		return i, nil
	}

	switch msg.String() {
	case "enter":
		if i.input == "" {
			return i, nil
		}

		// Add to history
		i.history = append(i.history, i.input)
		i.historyIdx = len(i.history)

		// Send input to agent
		if i.agent != nil && i.ctx != nil {
			input := i.input
			i.input = ""
			i.cursor = 0
			i.thinking = true

			// Return a command to send the input to the agent
			return i, func() tea.Msg {
				i.agent.UserMessage(i.ctx, input)
				return userInputMsg{input: input}
			}
		}

		// Clear input
		i.input = ""
		i.cursor = 0

	case "ctrl+c":
		// Cancel or exit
		return i, tea.Quit

	case "backspace":
		if i.cursor > 0 {
			i.input = i.input[:i.cursor-1] + i.input[i.cursor:]
			i.cursor--
		}

	case "delete":
		if i.cursor < len(i.input) {
			i.input = i.input[:i.cursor] + i.input[i.cursor+1:]
		}

	case "left":
		if i.cursor > 0 {
			i.cursor--
		}

	case "right":
		if i.cursor < len(i.input) {
			i.cursor++
		}

	case "up":
		// Navigate history
		if len(i.history) > 0 && i.historyIdx > 0 {
			i.historyIdx--
			i.input = i.history[i.historyIdx]
			i.cursor = len(i.input)
		}

	case "down":
		// Navigate history
		if i.historyIdx < len(i.history)-1 {
			i.historyIdx++
			i.input = i.history[i.historyIdx]
			i.cursor = len(i.input)
		} else if i.historyIdx == len(i.history)-1 {
			// At the end of history, clear input
			i.historyIdx = len(i.history)
			i.input = ""
			i.cursor = 0
		}

	case "home":
		i.cursor = 0

	case "end":
		i.cursor = len(i.input)

	default:
		// Handle regular character input
		if len(msg.Runes) == 1 {
			i.input = i.input[:i.cursor] + string(msg.Runes) + i.input[i.cursor:]
			i.cursor++
		}
	}

	return i, nil
}

// HandleMessage implements MessageHandler
func (i *InputComponent) HandleMessage(msg Message) tea.Cmd {
	return nil
}

// HandleAgentMessage handles agent messages
func (i *InputComponent) HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd {
	// When agent is done, update thinking state
	if msg.EndOfTurn {
		i.thinking = false
	}
	return nil
}

// HandleToolUse handles tool use messages
func (i *InputComponent) HandleToolUse(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

// HandleError handles error messages
func (i *InputComponent) HandleError(msg *loop.AgentMessage) tea.Cmd {
	// On error, stop thinking
	i.thinking = false
	return nil
}
