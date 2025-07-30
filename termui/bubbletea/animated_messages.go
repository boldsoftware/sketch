package bubbletea

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// AnimatedMessagesComponent extends MessagesComponent with animations
type AnimatedMessagesComponent struct {
	*MessagesComponent // Embed the original component

	// Animation states
	thinking         bool
	typing           bool
	toolExecuting    bool
	loadingMessage   string

	// Spinners for different states
	thinkSpinner     spinner.Model
	typeSpinner      spinner.Model
	toolSpinner      spinner.Model

	// Typing animation
	typingText       string
	typingIndex      int
	typingSpeed      time.Duration
	showCursor       bool
	cursorBlink      time.Duration

	// Loading dots animation
	loadingDots      int
	loadingMaxDots   int

	// Progress tracking
	commandProgress  float64
	scanProgress     float64
	currentOperation string

	// Animation styles
	thinkingStyle    lipgloss.Style
	typingStyle      lipgloss.Style
	loadingStyle     lipgloss.Style
	progressStyle    lipgloss.Style
}

// NewAnimatedMessagesComponent creates a new animated messages component
func NewAnimatedMessagesComponent() *AnimatedMessagesComponent {
	baseComponent := NewMessagesComponent()
	
	// Create different spinners for different operations
	thinkSpinner := spinner.New()
	thinkSpinner.Spinner = spinner.Ellipsis
	thinkSpinner.Style = lipgloss.NewStyle().Foreground(CyberBlue)

	typeSpinner := spinner.New()
	typeSpinner.Spinner = spinner.Dot
	typeSpinner.Style = lipgloss.NewStyle().Foreground(HackerGreen)

	toolSpinner := spinner.New()
	toolSpinner.Spinner = spinner.Globe
	toolSpinner.Style = lipgloss.NewStyle().Foreground(WarningRed)

	return &AnimatedMessagesComponent{
		MessagesComponent: baseComponent.(*MessagesComponent),
		typingSpeed:       80 * time.Millisecond,
		cursorBlink:       500 * time.Millisecond,
		loadingMaxDots:    3,
		thinkSpinner:      thinkSpinner,
		typeSpinner:       typeSpinner,
		toolSpinner:       toolSpinner,
		thinkingStyle: lipgloss.NewStyle().
			Foreground(CyberBlue).
			Italic(true),
		typingStyle: lipgloss.NewStyle().
			Foreground(HackerGreen).
			Bold(true),
		loadingStyle: lipgloss.NewStyle().
			Foreground(TerminalGreen).
			Italic(true),
		progressStyle: lipgloss.NewStyle().
			Foreground(CyberBlue).
			Background(DarkBg),
	}
}

func (a *AnimatedMessagesComponent) Init() tea.Cmd {
	return tea.Batch(
		a.MessagesComponent.Init(),
		a.thinkSpinner.Tick,
		a.typeSpinner.Tick,
		a.toolSpinner.Tick,
		a.tickCursor(),
		a.tickLoadingDots(),
	)
}

func (a *AnimatedMessagesComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle animation-specific messages first
	switch msg := msg.(type) {
	case AgentThinkingMsg:
		a.thinking = msg.Thinking
		a.loadingMessage = msg.Message

	case TypingStartMsg:
		a.typing = true
		a.typingText = msg.Text
		a.typingIndex = 0
		cmds = append(cmds, a.tickTyping())

	case TypingTickMsg:
		if a.typing && a.typingIndex < len(a.typingText) {
			a.typingIndex++
			cmds = append(cmds, a.tickTyping())
		} else {
			a.typing = false
		}

	case TypingEndMsg:
		a.typing = false
		a.typingText = ""
		a.typingIndex = 0

	case ToolExecutionMsg:
		a.toolExecuting = msg.Executing
		a.currentOperation = msg.Operation
		a.commandProgress = msg.Progress

	case ScanProgressUpdateMsg:
		a.scanProgress = msg.Progress
		a.currentOperation = msg.Operation

	case CursorBlinkMsg:
		a.showCursor = !a.showCursor
		cmds = append(cmds, a.tickCursor())

	case LoadingDotsMsg:
		a.loadingDots = (a.loadingDots + 1) % (a.loadingMaxDots + 1)
		cmds = append(cmds, a.tickLoadingDots())
	}

	// Update spinners
	var cmd tea.Cmd
	a.thinkSpinner, cmd = a.thinkSpinner.Update(msg)
	cmds = append(cmds, cmd)

	a.typeSpinner, cmd = a.typeSpinner.Update(msg)
	cmds = append(cmds, cmd)

	a.toolSpinner, cmd = a.toolSpinner.Update(msg)
	cmds = append(cmds, cmd)

	// Update base component
	baseModel, cmd := a.MessagesComponent.Update(msg)
	a.MessagesComponent = baseModel.(*MessagesComponent)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a *AnimatedMessagesComponent) View() string {
	baseView := a.MessagesComponent.View()
	
	// If no animations are active, return base view
	if !a.thinking && !a.typing && !a.toolExecuting {
		return baseView
	}

	// Add clean, minimalistic animation indicators
	var animations []string

	// Clean thinking indicator
	if a.thinking {
		thinkingStyle := lipgloss.NewStyle().
			Foreground(CyberBlue).
			Italic(true)
			
		thinkingText := fmt.Sprintf("%s Thinking...", a.thinkSpinner.View())
		animations = append(animations, thinkingStyle.Render(thinkingText))
	}

	// Clean typing indicator
	if a.typing {
		typingStyle := lipgloss.NewStyle().
			Foreground(HackerGreen).
			Italic(true)
			
		displayText := a.typingText[:a.typingIndex]
		cursor := ""
		if a.showCursor {
			cursor = "▋"
		}
		
		typingText := fmt.Sprintf("%s %s%s", a.typeSpinner.View(), displayText, cursor)
		animations = append(animations, typingStyle.Render(typingText))
	}

	// Clean tool execution indicator
	if a.toolExecuting {
		toolStyle := lipgloss.NewStyle().
			Foreground(WarningRed).
			Bold(true)
			
		progressBar := a.renderProgressBar(a.commandProgress, 20)
		toolText := fmt.Sprintf("%s %s %s", 
			a.toolSpinner.View(), 
			a.currentOperation,
			progressBar)
			
		animations = append(animations, toolStyle.Render(toolText))
	}

	// Combine base view with clean animations
	if len(animations) > 0 {
		return baseView + "\n" + strings.Join(animations, "\n") + "\n"
	}

	return baseView
}

func (a *AnimatedMessagesComponent) renderAnimationBox(title, content string, style lipgloss.Style) string {
	width := a.width - 4
	if width < 20 {
		width = 20
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(DarkGreen).
		Background(DarkBg).
		Padding(0, 1).
		Width(width)

	titleStyle := lipgloss.NewStyle().
		Foreground(CyberBlue).
		Bold(true)

	header := titleStyle.Render(title)
	body := style.Render(content)

	return boxStyle.Render(fmt.Sprintf("%s\n%s", header, body))
}

func (a *AnimatedMessagesComponent) renderProgressBar(progress float64, width int) string {
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}

	filled := int(progress * float64(width))
	empty := width - filled
	
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	percentage := fmt.Sprintf("%.0f%%", progress*100)
	
	return a.progressStyle.Render(fmt.Sprintf("[%s] %s", bar, percentage))
}

// Animation tick functions
func (a *AnimatedMessagesComponent) tickTyping() tea.Cmd {
	return tea.Tick(a.typingSpeed, func(t time.Time) tea.Msg {
		return TypingTickMsg{}
	})
}

func (a *AnimatedMessagesComponent) tickCursor() tea.Cmd {
	return tea.Tick(a.cursorBlink, func(t time.Time) tea.Msg {
		return CursorBlinkMsg{}
	})
}

func (a *AnimatedMessagesComponent) tickLoadingDots() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return LoadingDotsMsg{}
	})
}

// Enhanced message handling with animations
func (a *AnimatedMessagesComponent) HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd {
	// Start typing animation for agent responses
	if msg.Content != "" {
		return tea.Batch(
			a.MessagesComponent.HandleAgentMessage(msg),
			func() tea.Msg {
				return TypingStartMsg{Text: msg.Content}
			},
		)
	}
	return a.MessagesComponent.HandleAgentMessage(msg)
}

func (a *AnimatedMessagesComponent) HandleToolUse(msg *loop.AgentMessage) tea.Cmd {
	// Start tool execution animation
	cmds := []tea.Cmd{
		a.MessagesComponent.HandleToolUse(msg),
		func() tea.Msg {
			return ToolExecutionMsg{
				Executing: true,
				Operation: fmt.Sprintf("Executing %s", msg.ToolName),
				Progress:  0.0,
			}
		},
	}

	// Simulate progress for different tool types
	if msg.ToolName == "bash" {
		cmds = append(cmds, a.simulateBashProgress())
	} else if msg.ToolName == "pentest" {
		cmds = append(cmds, a.simulatePentestProgress())
	}

	return tea.Batch(cmds...)
}

func (a *AnimatedMessagesComponent) simulateBashProgress() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		// Simulate command execution progress
		progress := 0.1 + (0.8 * float64(time.Now().UnixNano() % 1000000000) / 1000000000.0)
		return ToolExecutionMsg{
			Executing: true,
			Operation: "Running bash command",
			Progress:  progress,
		}
	})
}

func (a *AnimatedMessagesComponent) simulatePentestProgress() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		progress := 0.05 + (0.9 * float64(time.Now().UnixNano() % 1000000000) / 1000000000.0)
		return ToolExecutionMsg{
			Executing: true,
			Operation: "Processing pentest data",
			Progress:  progress,
		}
	})
}

// StartThinking starts the thinking animation
func (a *AnimatedMessagesComponent) StartThinking(message string) tea.Cmd {
	return func() tea.Msg {
		return AgentThinkingMsg{
			Thinking: true,
			Message:  message,
		}
	}
}

// StopThinking stops the thinking animation
func (a *AnimatedMessagesComponent) StopThinking() tea.Cmd {
	return func() tea.Msg {
		return AgentThinkingMsg{
			Thinking: false,
			Message:  "",
		}
	}
}

// StartToolExecution starts tool execution animation
func (a *AnimatedMessagesComponent) StartToolExecution(operation string) tea.Cmd {
	return func() tea.Msg {
		return ToolExecutionMsg{
			Executing: true,
			Operation: operation,
			Progress:  0.0,
		}
	}
}

// StopToolExecution stops tool execution animation
func (a *AnimatedMessagesComponent) StopToolExecution() tea.Cmd {
	return func() tea.Msg {
		return ToolExecutionMsg{
			Executing: false,
			Operation: "",
			Progress:  0.0,
		}
	}
}

// Animation message types
type AgentThinkingMsg struct {
	Thinking bool
	Message  string
}

type TypingStartMsg struct {
	Text string
}

type TypingTickMsg struct{}

type TypingEndMsg struct{}

type ToolExecutionMsg struct {
	Executing bool
	Operation string
	Progress  float64
}

type ScanProgressUpdateMsg struct {
	Progress  float64
	Operation string
}

type CursorBlinkMsg struct{}

type LoadingDotsMsg struct{}

// MessageHandler interface methods
func (a *AnimatedMessagesComponent) HandleMessage(msg Message) tea.Cmd {
	// Delegate to base component
	return a.MessagesComponent.HandleMessage(msg)
}

func (a *AnimatedMessagesComponent) HandleError(msg *loop.AgentMessage) tea.Cmd {
	// Handle error messages with animation
	return tea.Batch(
		a.MessagesComponent.HandleError(msg),
		func() tea.Msg {
			return AgentThinkingMsg{
				Thinking: false,
				Message:  "",
			}
		},
		func() tea.Msg {
			return ToolExecutionMsg{
				Executing: false,
				Operation: "",
				Progress:  0.0,
			}
		},
	)
}

// SetContext sets the context for the AnimatedMessagesComponent
func (a *AnimatedMessagesComponent) SetContext(ctx context.Context) {
	a.MessagesComponent.SetContext(ctx)
}
