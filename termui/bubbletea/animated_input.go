package bubbletea

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// AnimatedInputComponent provides an enhanced input component with animations
type AnimatedInputComponent struct {
	agent loop.CodingAgent
	ctx   context.Context

	textInput    textinput.Model
	history      []string
	historyIndex int
	prompt       string
	width        int
	height       int

	// Animation states
	thinking      bool
	sending       bool
	thinkSpinner  spinner.Model
	sendSpinner   spinner.Model

	// Typing animation
	typingAnimation bool
	typingText      string
	typingIndex     int
	typingSpeed     time.Duration

	// Styles
	promptStyle    lipgloss.Style
	inputStyle     lipgloss.Style
	thinkingStyle  lipgloss.Style
	sendingStyle   lipgloss.Style
	borderStyle    lipgloss.Style
}

// NewAnimatedInputComponent creates a new animated input component
func NewAnimatedInputComponent() *AnimatedInputComponent {
	ti := textinput.New()
	ti.Placeholder = "Type your pentesting command or /path/to/file..."
	ti.Focus()
	ti.CharLimit = 2000
	ti.Width = 80

	// Create spinners
	thinkSpinner := spinner.New()
	thinkSpinner.Spinner = spinner.Ellipsis
	thinkSpinner.Style = lipgloss.NewStyle().Foreground(CyberBlue)

	sendSpinner := spinner.New()
	sendSpinner.Spinner = spinner.Points
	sendSpinner.Style = lipgloss.NewStyle().Foreground(HackerGreen)

	return &AnimatedInputComponent{
		textInput:    ti,
		history:      []string{},
		historyIndex: -1,
		prompt:       "🎯",
		typingSpeed:  50 * time.Millisecond,
		promptStyle: lipgloss.NewStyle().
			Foreground(HackerGreen).
			Bold(true),
		inputStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#1a1a1a")).
			Padding(0, 1),
		thinkingStyle: lipgloss.NewStyle().
			Foreground(CyberBlue).
			Italic(true),
		sendingStyle: lipgloss.NewStyle().
			Foreground(HackerGreen).
			Bold(true),
		borderStyle: lipgloss.NewStyle().
			Foreground(DarkGreen).
			Background(DarkBg),
		thinkSpinner: thinkSpinner,
		sendSpinner:  sendSpinner,
	}
}

func (a *AnimatedInputComponent) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		a.thinkSpinner.Tick,
		a.sendSpinner.Tick,
	)
}

func (a *AnimatedInputComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = 3 // Input area height
		a.textInput.Width = a.width - 10 // Account for prompt and padding

	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if !a.thinking && !a.sending && a.textInput.Value() != "" {
				input := strings.TrimSpace(a.textInput.Value())
				a.addToHistory(input)
				a.textInput.SetValue("")
				a.sending = true
				
				// Send message start animation
				cmds = append(cmds, func() tea.Msg {
					return MessageSendStartMsg{}
				})
				
				// Simulate sending delay then process
				cmds = append(cmds, tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
					return ProcessInputMsg{Input: input}
				}))
				
				return a, tea.Batch(cmds...)
			}

		case "up":
			if a.historyIndex < len(a.history)-1 {
				a.historyIndex++
				a.textInput.SetValue(a.history[len(a.history)-1-a.historyIndex])
				a.textInput.CursorEnd()
			}
			return a, nil

		case "down":
			if a.historyIndex > 0 {
				a.historyIndex--
				a.textInput.SetValue(a.history[len(a.history)-1-a.historyIndex])
				a.textInput.CursorEnd()
			} else if a.historyIndex == 0 {
				a.historyIndex = -1
				a.textInput.SetValue("")
			}
			return a, nil

		case "ctrl+c":
			if a.thinking || a.sending {
				a.thinking = false
				a.sending = false
				cmds = append(cmds, func() tea.Msg {
					return MessageSendEndMsg{}
				})
			}
		}

	case ProcessInputMsg:
		a.sending = false
		a.thinking = true
		cmds = append(cmds, func() tea.Msg {
			return MessageSendEndMsg{}
		})
		// Send the actual user input message to the main UI
		cmds = append(cmds, func() tea.Msg {
			return userInputMsg{input: msg.Input}
		})

	case ThinkingEndMsg:
		a.thinking = false

	case AgentResponseCompleteMsg:
		// Agent has finished responding, reset thinking state
		a.thinking = false
		a.sending = false

	case TypingAnimationMsg:
		if a.typingAnimation && a.typingIndex < len(a.typingText) {
			a.typingIndex++
			cmds = append(cmds, tea.Tick(a.typingSpeed, func(t time.Time) tea.Msg {
				return TypingAnimationMsg{}
			}))
		} else {
			a.typingAnimation = false
		}

	case StartTypingMsg:
		a.typingText = msg.Text
		a.typingIndex = 0
		a.typingAnimation = true
		cmds = append(cmds, tea.Tick(a.typingSpeed, func(t time.Time) tea.Msg {
			return TypingAnimationMsg{}
		}))
	}

	// Update text input if not in special states
	if !a.thinking && !a.sending {
		var cmd tea.Cmd
		a.textInput, cmd = a.textInput.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Update spinners
	var cmd tea.Cmd
	a.thinkSpinner, cmd = a.thinkSpinner.Update(msg)
	cmds = append(cmds, cmd)

	a.sendSpinner, cmd = a.sendSpinner.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a *AnimatedInputComponent) View() string {
	if a.width == 0 {
		return ""
	}

	// Clean minimalistic design with clear input visibility
	var lines []string

	// Main input line with clear prompt and input field
	var inputLine string
	if a.sending {
		inputLine = a.sendingStyle.Render(fmt.Sprintf("%s Sending...", a.sendSpinner.View()))
	} else if a.thinking {
		inputLine = a.thinkingStyle.Render(fmt.Sprintf("%s Processing...", a.thinkSpinner.View()))
	} else {
		// Clear, visible input with contrasting colors
		promptPart := a.promptStyle.Render(a.prompt + " ")
		inputPart := a.inputStyle.Render(a.textInput.View())
		inputLine = promptPart + inputPart
	}

	// Add subtle border around input
	borderTop := lipgloss.NewStyle().
		Foreground(DarkGreen).
		Render("┌" + strings.Repeat("─", a.width-2) + "┐")
	
	borderBottom := lipgloss.NewStyle().
		Foreground(DarkGreen).
		Render("└" + strings.Repeat("─", a.width-2) + "┘")

	// Input line with proper padding
	inputWithBorder := lipgloss.NewStyle().
		Foreground(DarkGreen).
		Render("│ ") + inputLine
	
	// Pad to full width
	currentLen := len("│ ") + len(inputLine)
	padding := max(0, a.width-currentLen-1)
	inputWithBorder += strings.Repeat(" ", padding) + 
		lipgloss.NewStyle().Foreground(DarkGreen).Render("│")

	lines = append(lines, borderTop)
	lines = append(lines, inputWithBorder)
	
	// Optional status line (only when relevant)
	if a.sending || a.thinking || len(a.history) > 0 {
		var statusText string
		if a.sending {
			statusText = "⚡ Sending to AI"
		} else if a.thinking {
			statusText = "🧠 AI thinking"
		} else {
			statusText = fmt.Sprintf("↑↓ History (%d)", len(a.history))
		}
		
		statusStyle := lipgloss.NewStyle().
			Foreground(MutedText).
			Italic(true)
		
		statusLine := lipgloss.NewStyle().
			Foreground(DarkGreen).
			Render("│ ") + statusStyle.Render(statusText)
		
		statusLen := len("│ ") + len(statusText)
		statusPadding := max(0, a.width-statusLen-1)
		statusLine += strings.Repeat(" ", statusPadding) + 
			lipgloss.NewStyle().Foreground(DarkGreen).Render("│")
		
		lines = append(lines, statusLine)
	}
	
	lines = append(lines, borderBottom)

	return strings.Join(lines, "\n")
}

func (a *AnimatedInputComponent) SetAgent(agent loop.CodingAgent) {
	a.agent = agent
}

func (a *AnimatedInputComponent) SetContext(ctx context.Context) {
	a.ctx = ctx
}

func (a *AnimatedInputComponent) addToHistory(input string) {
	a.history = append(a.history, input)
	if len(a.history) > 100 { // Keep last 100 commands
		a.history = a.history[1:]
	}
	a.historyIndex = -1
}

func (a *AnimatedInputComponent) SetPrompt(url string, thinking bool) {
	a.thinking = thinking
	if thinking {
		a.prompt = "🤔"
	} else {
		a.prompt = "🎯"
	}
}

// Implement MessageHandler interface
func (a *AnimatedInputComponent) HandleMessage(msg Message) tea.Cmd {
	return nil
}

func (a *AnimatedInputComponent) HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

func (a *AnimatedInputComponent) HandleToolUse(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

func (a *AnimatedInputComponent) HandleError(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

// Animation message types
type ProcessInputMsg struct {
	Input string
}

type ThinkingEndMsg struct{}

type TypingAnimationMsg struct{}

type StartTypingMsg struct {
	Text string
}
