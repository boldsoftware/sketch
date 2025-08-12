package bubbletea

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProgressTracker manages multiple concurrent progress indicators
type ProgressTracker struct {
	width  int
	height int

	// Active operations
	operations map[string]*Operation
	
	// Progress bars
	progressBars map[string]progress.Model
	
	// Spinners for indeterminate operations
	spinners map[string]spinner.Model

	// Styles
	headerStyle    lipgloss.Style
	operationStyle lipgloss.Style
	progressStyle  lipgloss.Style
	completeStyle  lipgloss.Style
	errorStyle     lipgloss.Style
	borderStyle    lipgloss.Style
}

// Operation represents a tracked operation
type Operation struct {
	ID          string
	Name        string
	Description string
	Progress    float64 // 0.0 to 1.0, -1 for indeterminate
	Status      OperationStatus
	StartTime   time.Time
	EndTime     *time.Time
	Error       string
	Details     []string
}

type OperationStatus int

const (
	StatusRunning OperationStatus = iota
	StatusComplete
	StatusError
	StatusCancelled
)

// NewProgressTracker creates a new progress tracker
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		operations:   make(map[string]*Operation),
		progressBars: make(map[string]progress.Model),
		spinners:     make(map[string]spinner.Model),
		headerStyle: lipgloss.NewStyle().
			Foreground(CyberBlue).
			Bold(true).
			Underline(true),
		operationStyle: lipgloss.NewStyle().
			Foreground(HackerGreen).
			Bold(true),
		progressStyle: lipgloss.NewStyle().
			Foreground(TerminalGreen),
		completeStyle: lipgloss.NewStyle().
			Foreground(HackerGreen).
			Bold(true),
		errorStyle: lipgloss.NewStyle().
			Foreground(WarningRed).
			Bold(true),
		borderStyle: lipgloss.NewStyle().
			Foreground(DarkGreen).
			Background(DarkBg),
	}
}

func (p *ProgressTracker) Init() tea.Cmd {
	var cmds []tea.Cmd
	
	// Initialize all spinners
	for _, spinner := range p.spinners {
		cmds = append(cmds, spinner.Tick)
	}
	
	return tea.Batch(cmds...)
}

func (p *ProgressTracker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
		p.height = msg.Height / 3 // Use 1/3 of screen for progress

		// Update progress bar widths
		for id, bar := range p.progressBars {
			bar.Width = p.width - 20 // Account for labels and padding
			p.progressBars[id] = bar
		}

	case StartOperationMsg:
		p.startOperation(msg)
		if msg.Progress < 0 {
			// Indeterminate operation, add spinner
			spinner := p.createSpinner()
			p.spinners[msg.ID] = spinner
			cmds = append(cmds, spinner.Tick)
		} else {
			// Determinate operation, add progress bar
			bar := p.createProgressBar()
			p.progressBars[msg.ID] = bar
		}

	case UpdateOperationMsg:
		if op, exists := p.operations[msg.ID]; exists {
			op.Progress = msg.Progress
			op.Description = msg.Description
			if len(msg.Details) > 0 {
				op.Details = append(op.Details, msg.Details...)
				// Keep only last 5 details
				if len(op.Details) > 5 {
					op.Details = op.Details[len(op.Details)-5:]
				}
			}

			// Update progress bar if it exists
			if bar, hasBar := p.progressBars[msg.ID]; hasBar {
				cmds = append(cmds, bar.SetPercent(msg.Progress))
			}
		}

	case CompleteOperationMsg:
		if op, exists := p.operations[msg.ID]; exists {
			op.Status = StatusComplete
			op.Progress = 1.0
			now := time.Now()
			op.EndTime = &now
			
			// Remove spinner/progress bar after delay
			cmds = append(cmds, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
				return RemoveOperationMsg{ID: msg.ID}
			}))
		}

	case ErrorOperationMsg:
		if op, exists := p.operations[msg.ID]; exists {
			op.Status = StatusError
			op.Error = msg.Error
			now := time.Now()
			op.EndTime = &now
			
			// Remove after longer delay for errors
			cmds = append(cmds, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
				return RemoveOperationMsg{ID: msg.ID}
			}))
		}

	case RemoveOperationMsg:
		delete(p.operations, msg.ID)
		delete(p.progressBars, msg.ID)
		delete(p.spinners, msg.ID)

	case NmapScanProgressMsg:
		// Handle nmap-specific progress updates
		p.handleNmapProgress(msg)

	case BashCommandProgressMsg:
		// Handle bash command progress
		p.handleBashProgress(msg)
	}

	// Update all progress bars
	for id, bar := range p.progressBars {
		if op, exists := p.operations[id]; exists && op.Status == StatusRunning {
			var cmd tea.Cmd
			model, cmd := bar.Update(msg)
			p.progressBars[id] = model.(progress.Model)
			cmds = append(cmds, cmd)
		}
	}

	// Update all spinners
	for id, spin := range p.spinners {
		if op, exists := p.operations[id]; exists && op.Status == StatusRunning {
			var cmd tea.Cmd
			spin, cmd = spin.Update(msg)
			p.spinners[id] = spin
			cmds = append(cmds, cmd)
		}
	}

	return p, tea.Batch(cmds...)
}

func (p *ProgressTracker) View() string {
	if len(p.operations) == 0 {
		return ""
	}

	var content strings.Builder

	// Header
	content.WriteString(p.borderStyle.Render("╭" + strings.Repeat("─", p.width-2) + "╮\n"))
	content.WriteString(p.borderStyle.Render("│ "))
	content.WriteString(p.headerStyle.Render("🚀 ACTIVE OPERATIONS"))
	padding := p.width - len("🚀 ACTIVE OPERATIONS") - 4
	content.WriteString(strings.Repeat(" ", padding))
	content.WriteString(p.borderStyle.Render("│\n"))
	content.WriteString(p.borderStyle.Render("├" + strings.Repeat("─", p.width-2) + "┤\n"))

	// Operations
	for id, op := range p.operations {
		content.WriteString(p.renderOperation(id, op))
	}

	// Footer
	content.WriteString(p.borderStyle.Render("╰" + strings.Repeat("─", p.width-2) + "╯"))

	return content.String()
}

func (p *ProgressTracker) renderOperation(id string, op *Operation) string {
	var content strings.Builder

	// Operation header
	content.WriteString(p.borderStyle.Render("│ "))
	
	// Status icon and name
	statusIcon := p.getStatusIcon(op.Status)
	content.WriteString(p.operationStyle.Render(fmt.Sprintf("%s %s", statusIcon, op.Name)))
	content.WriteString("\n")

	// Progress indicator
	content.WriteString(p.borderStyle.Render("│ "))
	if op.Progress < 0 {
		// Indeterminate progress with spinner
		if spinner, exists := p.spinners[id]; exists {
			content.WriteString(p.progressStyle.Render(fmt.Sprintf("  %s %s", spinner.View(), op.Description)))
		}
	} else {
		// Determinate progress with bar
		if bar, exists := p.progressBars[id]; exists {
			content.WriteString(p.progressStyle.Render(fmt.Sprintf("  %s %.0f%%", bar.View(), op.Progress*100)))
		}
	}
	content.WriteString("\n")

	// Duration
	duration := p.getOperationDuration(op)
	content.WriteString(p.borderStyle.Render("│ "))
	content.WriteString(p.progressStyle.Render(fmt.Sprintf("  ⏱️  %s", duration)))
	content.WriteString("\n")

	// Recent details (if any)
	if len(op.Details) > 0 {
		content.WriteString(p.borderStyle.Render("│ "))
		lastDetail := op.Details[len(op.Details)-1]
		if len(lastDetail) > p.width-10 {
			lastDetail = lastDetail[:p.width-13] + "..."
		}
		content.WriteString(p.progressStyle.Render(fmt.Sprintf("  [MSG] %s", lastDetail)))
		content.WriteString("\n")
	}

	// Error message (if any)
	if op.Error != "" {
		content.WriteString(p.borderStyle.Render("│ "))
		errorMsg := op.Error
		if len(errorMsg) > p.width-10 {
			errorMsg = errorMsg[:p.width-13] + "..."
		}
		content.WriteString(p.errorStyle.Render(fmt.Sprintf("  [ERR] %s", errorMsg)))
		content.WriteString("\n")
	}

	return content.String()
}

func (p *ProgressTracker) getStatusIcon(status OperationStatus) string {
	switch status {
	case StatusRunning:
		return "[RUN]"
	case StatusComplete:
		return "[OK]"
	case StatusError:
		return "[ERR]"
	case StatusCancelled:
		return "[STOP]"
	default:
		return "[?]"
	}
}

func (p *ProgressTracker) getOperationDuration(op *Operation) string {
	var endTime time.Time
	if op.EndTime != nil {
		endTime = *op.EndTime
	} else {
		endTime = time.Now()
	}
	
	duration := endTime.Sub(op.StartTime)
	if duration < time.Minute {
		return fmt.Sprintf("%.1fs", duration.Seconds())
	} else if duration < time.Hour {
		return fmt.Sprintf("%.1fm", duration.Minutes())
	} else {
		return fmt.Sprintf("%.1fh", duration.Hours())
	}
}

func (p *ProgressTracker) createProgressBar() progress.Model {
	bar := progress.New(progress.WithDefaultGradient())
	bar.Width = p.width - 20
	return bar
}

func (p *ProgressTracker) createSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = lipgloss.NewStyle().Foreground(HackerGreen)
	return s
}

func (p *ProgressTracker) startOperation(msg StartOperationMsg) {
	op := &Operation{
		ID:          msg.ID,
		Name:        msg.Name,
		Description: msg.Description,
		Progress:    msg.Progress,
		Status:      StatusRunning,
		StartTime:   time.Now(),
		Details:     []string{},
	}
	p.operations[msg.ID] = op
}

func (p *ProgressTracker) handleNmapProgress(msg NmapScanProgressMsg) {
	// Update nmap scan progress based on output parsing
	if op, exists := p.operations[msg.ScanID]; exists {
		op.Progress = msg.Progress
		op.Details = append(op.Details, msg.CurrentTarget)
	}
}

func (p *ProgressTracker) handleBashProgress(msg BashCommandProgressMsg) {
	// Update bash command progress
	if op, exists := p.operations[msg.CommandID]; exists {
		op.Progress = msg.Progress
		if msg.Output != "" {
			op.Details = append(op.Details, msg.Output)
		}
	}
}

// Helper functions for starting operations
func (p *ProgressTracker) StartNmapScan(target string) tea.Cmd {
	return func() tea.Msg {
		return StartOperationMsg{
			ID:          fmt.Sprintf("nmap_%d", time.Now().UnixNano()),
			Name:        "Nmap Scan",
			Description: fmt.Sprintf("Scanning %s", target),
			Progress:    0.0,
		}
	}
}

func (p *ProgressTracker) StartBashCommand(command string) tea.Cmd {
	return func() tea.Msg {
		return StartOperationMsg{
			ID:          fmt.Sprintf("bash_%d", time.Now().UnixNano()),
			Name:        "Bash Command",
			Description: command,
			Progress:    -1, // Indeterminate
		}
	}
}

// Progress message types
type StartOperationMsg struct {
	ID          string
	Name        string
	Description string
	Progress    float64 // -1 for indeterminate
}

type UpdateOperationMsg struct {
	ID          string
	Progress    float64
	Description string
	Details     []string
}

type CompleteOperationMsg struct {
	ID string
}

type ErrorOperationMsg struct {
	ID    string
	Error string
}

type RemoveOperationMsg struct {
	ID string
}

type NmapScanProgressMsg struct {
	ScanID        string
	Progress      float64
	CurrentTarget string
	PortsFound    int
}

type BashCommandProgressMsg struct {
	CommandID string
	Progress  float64
	Output    string
}
