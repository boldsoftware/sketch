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
	// Create a clean input field with border like Gemini CLI

	// Create the input field with border
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(i.width - 4)

	var prompt string
	if i.thinking {
		prompt = i.promptStyle.Render("⏳")
	} else {
		prompt = i.promptStyle.Render(i.prompt)
	}

	// Combine prompt and input
	inputContent := fmt.Sprintf("%s %s", prompt, i.textInput.View())

	return inputStyle.Render(inputContent)
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
	// InputComponent only needs to update its prompt state based on agent activity
	// All display messages should go to MessagesComponent only
	switch typedMsg := msg.(type) {
	case agentMessageMsg:
		// Only update prompt state, don't process the message content
		if typedMsg.message.Type == loop.AgentMessageType {
			i.SetPrompt("", false) // Agent responded, no longer thinking
		}
		return nil
	case toolUseMsg:
		// Only update thinking state, don't process the tool message
		i.SetPrompt("", true) // Agent is using tools, show thinking state
		return nil
	case systemMessageMsg:
		// Reset thinking state on system messages
		i.SetPrompt("", false)
		return nil
	}
	return nil
}
