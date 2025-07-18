package bubbletea

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// InputComponent handles user input and command processing
type InputComponent struct {
	agent        loop.CodingAgent
	ctx          context.Context
	textInput    textinput.Model
	history      []string
	historyIndex int
	prompt       string
	thinking     bool
	multiLine    bool
	width        int
	height       int

	// Styling
	promptStyle lipgloss.Style
	inputStyle  lipgloss.Style
}

// Init initializes the input component
func (i *InputComponent) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages for the input component
func (i *InputComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			if i.thinking {
				// Ignore input while thinking
				return i, nil
			}

			// Get the current input
			input := i.textInput.Value()
			if input == "" {
				return i, nil
			}

			// Add to history
			i.history = append(i.history, input)
			if len(i.history) > 100 {
				i.history = i.history[len(i.history)-100:]
			}
			i.historyIndex = len(i.history)

			// Clear input
			i.textInput.SetValue("")

			// Process command or send to agent
			if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "!") {
				// Process as command
				return i, func() tea.Msg {
					return commandMsg{command: input}
				}
			} else {
				// Send as user input
				return i, func() tea.Msg {
					return userInputMsg{input: input}
				}
			}

		case tea.KeyUp:
			// Navigate history up
			if i.historyIndex > 0 {
				i.historyIndex--
				i.textInput.SetValue(i.history[i.historyIndex])
			}

		case tea.KeyDown:
			// Navigate history down
			if i.historyIndex < len(i.history)-1 {
				i.historyIndex++
				i.textInput.SetValue(i.history[i.historyIndex])
			} else if i.historyIndex == len(i.history)-1 {
				i.historyIndex = len(i.history)
				i.textInput.SetValue("")
			}

		case tea.KeyCtrlC:
			// Send a cancel message instead of directly calling Cancel
			return i, func() tea.Msg {
				return systemMessageMsg{content: "Operation cancelled by user"}
			}
		}
	case tea.WindowSizeMsg:
		i.width = msg.Width
		i.textInput.Width = msg.Width - 4 // Account for prompt and padding
	}

	// Update text input
	i.textInput, cmd = i.textInput.Update(msg)
	return i, cmd
}

// View renders the input component
func (i *InputComponent) View() string {
	var prompt string
	if i.thinking {
		prompt = i.promptStyle.Render("⏳ ")
	} else {
		prompt = i.promptStyle.Render(i.prompt)
	}

	return fmt.Sprintf("%s%s", prompt, i.textInput.View())
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
	if prompt != "" {
		i.prompt = prompt + " > "
	}
	i.thinking = thinking

	// Disable input while thinking
	if thinking {
		i.textInput.Blur()
	} else {
		i.textInput.Focus()
	}
}

// HandleMessage implements MessageHandler
func (i *InputComponent) HandleMessage(msg Message) tea.Cmd {
	// Route message based on type
	switch typedMsg := msg.(type) {
	case agentMessageMsg:
		return i.HandleAgentMessage(typedMsg.message)
	case toolUseMsg:
		return i.HandleToolUse(typedMsg.message)
	case systemMessageMsg:
		// Convert system message to error message for handling
		agentMsg := &loop.AgentMessage{
			Type:    loop.ErrorMessageType,
			Content: typedMsg.content,
		}
		return i.HandleError(agentMsg)
	}
	return nil
}

// HandleAgentMessage handles agent messages
func (i *InputComponent) HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd {
	// Update thinking state based on message type
	if msg.Type == loop.AgentMessageType {
		i.SetPrompt("", false) // Agent responded, no longer thinking
	}
	return nil
}

// HandleToolUse handles tool use messages
func (i *InputComponent) HandleToolUse(msg *loop.AgentMessage) tea.Cmd {
	// Update thinking state based on tool use
	i.SetPrompt("", true) // Agent is using tools, show thinking state
	return nil
}

// HandleError handles error messages
func (i *InputComponent) HandleError(msg *loop.AgentMessage) tea.Cmd {
	// Reset thinking state on error
	i.SetPrompt("", false)
	return nil
}
