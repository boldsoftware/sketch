package bubbletea

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// AnimatedStatusComponent provides animated status indicators for different operations
type AnimatedStatusComponent struct {
	*StatusComponent // Embed the base status component
	width  int
	height int
	agent  loop.CodingAgent

	// Spinners for different operations
	commandSpinner  spinner.Model
	toolSpinner     spinner.Model
	messageSpinner  spinner.Model
	scanSpinner     spinner.Model

	// Current operation states
	commandRunning  bool
	toolExecuting   bool
	messageSending  bool
	scanInProgress  bool

	// Operation details
	currentCommand string
	currentTool    string
	scanProgress   float64
	scanTarget     string

	// Styles
	activeStyle   lipgloss.Style
	inactiveStyle lipgloss.Style
	progressStyle lipgloss.Style
}

// NewAnimatedStatusComponent creates a new animated status component
func NewAnimatedStatusComponent() *AnimatedStatusComponent {
	// Create different spinners for different operations
	cmdSpinner := spinner.New()
	cmdSpinner.Spinner = spinner.Points
	cmdSpinner.Style = lipgloss.NewStyle().Foreground(HackerGreen)

	toolSpinner := spinner.New()
	toolSpinner.Spinner = spinner.Globe
	toolSpinner.Style = lipgloss.NewStyle().Foreground(CyberBlue)

	msgSpinner := spinner.New()
	msgSpinner.Spinner = spinner.Dot
	msgSpinner.Style = lipgloss.NewStyle().Foreground(TerminalGreen)

	scanSpinner := spinner.New()
	scanSpinner.Spinner = spinner.Meter
	scanSpinner.Style = lipgloss.NewStyle().Foreground(WarningRed)

	return &AnimatedStatusComponent{
		StatusComponent: NewStatusComponent().(*StatusComponent), // Initialize embedded component
		commandSpinner: cmdSpinner,
		toolSpinner:    toolSpinner,
		messageSpinner: msgSpinner,
		scanSpinner:    scanSpinner,
		activeStyle: lipgloss.NewStyle().
			Foreground(HackerGreen).
			Bold(true),
		inactiveStyle: lipgloss.NewStyle().
			Foreground(MutedText),
		progressStyle: lipgloss.NewStyle().
			Foreground(CyberBlue).
			Background(DarkBg),
	}
}

func (a *AnimatedStatusComponent) Init() tea.Cmd {
	return tea.Batch(
		a.commandSpinner.Tick,
		a.toolSpinner.Tick,
		a.messageSpinner.Tick,
		a.scanSpinner.Tick,
	)
}

func (a *AnimatedStatusComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = 1 // Status bar is single line

	case CommandStartMsg:
		a.commandRunning = true
		a.currentCommand = msg.Command

	case CommandEndMsg:
		a.commandRunning = false
		a.currentCommand = ""

	case ToolStartMsg:
		a.toolExecuting = true
		a.currentTool = msg.ToolName

	case ToolEndMsg:
		a.toolExecuting = false
		a.currentTool = ""

	case MessageSendStartMsg:
		a.messageSending = true

	case MessageSendEndMsg:
		a.messageSending = false

	case ScanStartMsg:
		a.scanInProgress = true
		a.scanTarget = msg.Target
		a.scanProgress = 0.0

	case ScanProgressMsg:
		a.scanProgress = msg.Progress

	case ScanEndMsg:
		a.scanInProgress = false
		a.scanTarget = ""
		a.scanProgress = 0.0
	}

	// Update spinners
	var cmd tea.Cmd
	a.commandSpinner, cmd = a.commandSpinner.Update(msg)
	cmds = append(cmds, cmd)

	a.toolSpinner, cmd = a.toolSpinner.Update(msg)
	cmds = append(cmds, cmd)

	a.messageSpinner, cmd = a.messageSpinner.Update(msg)
	cmds = append(cmds, cmd)

	a.scanSpinner, cmd = a.scanSpinner.Update(msg)
	cmds = append(cmds, cmd)

	return a, tea.Batch(cmds...)
}

func (a *AnimatedStatusComponent) View() string {
	if a.width == 0 {
		return ""
	}

	var indicators []string

	// Command execution indicator
	if a.commandRunning {
		cmdText := fmt.Sprintf("%s CMD: %s", a.commandSpinner.View(), a.truncateText(a.currentCommand, 20))
		indicators = append(indicators, a.activeStyle.Render(cmdText))
	}

	// Tool execution indicator
	if a.toolExecuting {
		toolText := fmt.Sprintf("%s TOOL: %s", a.toolSpinner.View(), a.currentTool)
		indicators = append(indicators, a.activeStyle.Render(toolText))
	}

	// Message sending indicator
	if a.messageSending {
		msgText := fmt.Sprintf("%s SENDING", a.messageSpinner.View())
		indicators = append(indicators, a.activeStyle.Render(msgText))
	}

	// Scan progress indicator
	if a.scanInProgress {
		progressBar := a.renderProgressBar(a.scanProgress, 20)
		scanText := fmt.Sprintf("%s SCAN: %s %s %.0f%%", 
			a.scanSpinner.View(), 
			a.truncateText(a.scanTarget, 15),
			progressBar,
			a.scanProgress*100)
		indicators = append(indicators, a.activeStyle.Render(scanText))
	}

	// If no operations are active, show idle state
	if len(indicators) == 0 {
		indicators = append(indicators, a.inactiveStyle.Render("⚡ READY"))
	}

	// Join indicators and pad to width
	content := strings.Join(indicators, " │ ")
	if len(content) > a.width {
		content = a.truncateText(content, a.width-3) + "..."
	}

	// Create border
	borderStyle := lipgloss.NewStyle().
		Foreground(DarkGreen).
		Background(DarkBg)

	return borderStyle.Render(fmt.Sprintf("═%s%s═", 
		content, 
		strings.Repeat(" ", max(0, a.width-len(content)-2))))
}

func (a *AnimatedStatusComponent) renderProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	empty := width - filled
	
	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	return a.progressStyle.Render(fmt.Sprintf("[%s]", bar))
}

func (a *AnimatedStatusComponent) truncateText(text string, maxLen int) string {
	if len(text) <= maxLen {
		return text
	}
	return text[:maxLen-3] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Message types for animated status
type CommandStartMsg struct {
	Command string
}

type CommandEndMsg struct{}

type ToolStartMsg struct {
	ToolName string
}

type ToolEndMsg struct{}

type MessageSendStartMsg struct{}

type MessageSendEndMsg struct{}

type ScanStartMsg struct {
	Target string
}

type ScanProgressMsg struct {
	Progress float64 // 0.0 to 1.0
}

type ScanEndMsg struct{}

// SetAgent sets the agent for this component
func (a *AnimatedStatusComponent) SetAgent(agent loop.CodingAgent) {
	a.agent = agent
	// Also set on embedded component if it has SetAgent method
	if a.StatusComponent != nil {
		a.StatusComponent.SetAgent(agent)
	}
}

// SetContext sets the context for the component
func (a *AnimatedStatusComponent) SetContext(ctx context.Context) {
	// Delegate to the embedded StatusComponent
	a.StatusComponent.SetContext(ctx)
}
