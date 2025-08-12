package bubbletea

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// InputState represents the current state of the input component
type InputState int

const (
	InputStateIdle InputState = iota
	InputStateSending
	InputStateProcessing
)

// InputComponent handles user input and command processing
type InputComponent struct {
	agent        loop.CodingAgent
	ctx          context.Context
	textInput    textinput.Model
	prompt       string
	state        InputState
	width        int
	height       int
	lastSentTime time.Time

	// Styling
	promptStyle lipgloss.Style
	inputStyle  lipgloss.Style
	stateStyle  lipgloss.Style
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
		// Don't process keys if we're in sending or processing state
		if i.state == InputStateSending || i.state == InputStateProcessing {
			return i, nil
		}

		switch msg.Type {
		case tea.KeyEnter:
			// Get the current input
			input := strings.TrimSpace(i.textInput.Value())
			if input == "" {
				return i, nil
			}

			// Immediately clear input and update state
			i.textInput.SetValue("")
			i.state = InputStateSending
			i.lastSentTime = time.Now()

			// Process command or send to agent
			if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "!") {
				// Process as command
				return i, tea.Batch(
					func() tea.Msg {
						return commandMsg{command: input}
					},
					func() tea.Msg {
						// Reset state after a short delay
						time.Sleep(100 * time.Millisecond)
						return inputStateResetMsg{}
					},
				)
			} else {
				// Send as user input with immediate display
				return i, tea.Batch(
					// First, display the user message immediately
					func() tea.Msg {
						return userMessageDisplayMsg{input: input, timestamp: time.Now()}
					},
					// Then send to agent
					func() tea.Msg {
						return userInputMsg{input: input}
					},
					// Reset state after a short delay
					func() tea.Msg {
						time.Sleep(200 * time.Millisecond)
						return inputStateResetMsg{}
					},
				)
			}

		// Removed history navigation - no longer needed

		case tea.KeyCtrlC:
			// Send a cancel message and reset state
			i.state = InputStateIdle
			return i, func() tea.Msg {
				return systemMessageMsg{content: "Operation cancelled by user"}
			}

		case tea.KeyCtrlL:
			// Clear screen command
			return i, func() tea.Msg {
				return commandMsg{command: "/clear"}
			}
		}

	case tea.WindowSizeMsg:
		i.width = msg.Width
		i.textInput.Width = msg.Width - 8 // Account for prompt, padding, and border

	case inputStateResetMsg:
		// Reset input state to idle
		i.state = InputStateIdle

	case AgentResponseCompleteMsg:
		// Agent finished responding, reset state
		i.state = InputStateIdle
	}

	// Only update text input if we're in idle state
	if i.state == InputStateIdle {
		i.textInput, cmd = i.textInput.Update(msg)
	}

	return i, cmd
}

// View renders the input component
func (i *InputComponent) View() string {
	if i.width == 0 {
		return ""
	}

	// Create the input field with border and state-aware styling
	borderColor := lipgloss.Color("240")
	if i.state == InputStateSending {
		borderColor = lipgloss.Color("#00FF41") // Green when sending
	} else if i.state == InputStateProcessing {
		borderColor = lipgloss.Color("#00FFFF") // Cyan when processing
	}

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(i.width - 4)

	// Create state-aware prompt with colored symbols
	var prompt string
	switch i.state {
	case InputStateSending:
		prompt = i.promptStyle.Foreground(lipgloss.Color("#00FF41")).Render("→")
	case InputStateProcessing:
		prompt = i.promptStyle.Foreground(lipgloss.Color("#00FFFF")).Render("*")
	default:
		if i.prompt != "" {
			prompt = i.promptStyle.Render(i.prompt)
		} else {
			prompt = i.promptStyle.Render("▶")
		}
	}

	// Create input content with state indicator
	var inputContent string
	if i.state == InputStateSending {
		inputContent = fmt.Sprintf("%s %s", prompt, i.stateStyle.Render("Sending..."))
	} else if i.state == InputStateProcessing {
		inputContent = fmt.Sprintf("%s %s", prompt, i.stateStyle.Render("Processing..."))
	} else {
		inputContent = fmt.Sprintf("%s %s", prompt, i.textInput.View())
	}

	// Add helpful hints at the bottom
	hints := i.renderHints()

	return inputStyle.Render(inputContent) + "\n" + hints
}

// SetAgent sets the agent reference
func (i *InputComponent) SetAgent(agent loop.CodingAgent) {
	i.agent = agent
}

// SetContext sets the context for the component
func (i *InputComponent) SetContext(ctx context.Context) {
	i.ctx = ctx
}

// SetPrompt sets the prompt text and processing state
func (i *InputComponent) SetPrompt(prompt string, processing bool) {
	if prompt != "" {
		i.prompt = prompt + " > "
	}

	// Update state based on processing status
	if processing {
		i.state = InputStateProcessing
		i.textInput.Blur()
	} else {
		i.state = InputStateIdle
		i.textInput.Focus()
	}
}

// HandleMessage implements MessageHandler
func (i *InputComponent) HandleMessage(msg Message) tea.Cmd {
	// InputComponent only needs to update its state based on agent activity
	// All display messages should go to MessagesComponent only
	switch typedMsg := msg.(type) {
	case agentMessageMsg:
		// Agent responded, reset to idle state
		if typedMsg.message.Type == loop.AgentMessageType {
			i.state = InputStateIdle
			i.textInput.Focus()
		}
		return nil
	case toolUseMsg:
		// Agent is using tools, show processing state
		i.state = InputStateProcessing
		i.textInput.Blur()
		return nil
	case systemMessageMsg:
		// Reset to idle state on system messages
		i.state = InputStateIdle
		i.textInput.Focus()
		return nil
	}
	return nil
}



// renderHints renders helpful keyboard shortcuts
func (i *InputComponent) renderHints() string {
	if i.width < 60 {
		return "" // Don't show hints on narrow screens
	}

	hintsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8B949E")).
		Italic(true).
		PaddingLeft(2)

	hints := "Ctrl+C Cancel • Ctrl+L Clear • Tab Switch Focus"
	return hintsStyle.Render(hints)
}
