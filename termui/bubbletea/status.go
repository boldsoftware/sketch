package bubbletea

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// StatusComponent displays real-time status information
type StatusComponent struct {
	agent        loop.CodingAgent
	ctx          context.Context
	width        int
	currentState string

	// Status information
	outstandingCalls []string
	startTime        time.Time

	// Styling
	stateStyle      lipgloss.Style
	budgetStyle     lipgloss.Style
	operationsStyle lipgloss.Style
}

// NewStatusComponent creates a new status component
func NewStatusComponent() UIComponent {
	return &StatusComponent{
		startTime: time.Now(),
		stateStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FF41")).
			Bold(true),
		budgetStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FFFF")).
			Bold(true),
		operationsStyle: lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8B949E")).
			Italic(true),
	}
}

// Init initializes the status component
func (s *StatusComponent) Init() tea.Cmd {
	return nil
}

// Update handles messages for the status component
func (s *StatusComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width = msg.Width
	}
	return s, nil
}

// View renders the status component
func (s *StatusComponent) View() string {
	if s.width == 0 {
		return ""
	}

	// Create a clean status bar like Gemini CLI
	leftStatus := ""
	rightStatus := ""

	// Left side: current state and pending operations
	if s.currentState != "" {
		leftStatus = s.currentState
	} else {
		leftStatus = "ready"
	}

	if len(s.outstandingCalls) > 0 {
		leftStatus += fmt.Sprintf(" (%s)", strings.Join(s.outstandingCalls, ", "))
	}

	// Right side: session info
	duration := time.Since(s.startTime).Round(time.Second)
	rightStatus = fmt.Sprintf("session: %s", duration)

	// Create the status bar with hacker theme styling
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C9D1D9")).
		Background(lipgloss.Color("#0D1117")).
		Padding(0, 1).
		Width(s.width).
		Border(lipgloss.NormalBorder(), true, false, false, false).
		BorderForeground(lipgloss.Color("#008F11"))

	// Calculate spacing to push right status to the right
	leftStyled := s.stateStyle.Render(leftStatus)
	rightStyled := s.operationsStyle.Render(rightStatus)

	// Calculate the number of spaces needed
	contentWidth := lipgloss.Width(leftStyled) + lipgloss.Width(rightStyled)
	spacesNeeded := s.width - contentWidth - 4 // Account for padding
	if spacesNeeded < 1 {
		spacesNeeded = 1
	}

	spaces := strings.Repeat(" ", spacesNeeded)
	statusContent := leftStyled + spaces + rightStyled

	return statusStyle.Render(statusContent)
}

// SetAgent sets the agent reference
func (s *StatusComponent) SetAgent(agent loop.CodingAgent) {
	s.agent = agent
}

// SetContext sets the context for the component
func (s *StatusComponent) SetContext(ctx context.Context) {
	s.ctx = ctx

	// Start a goroutine to update outstanding calls
	go s.monitorOutstandingCalls(ctx)
}

// UpdateState updates the current state display
func (s *StatusComponent) UpdateState(state string) {
	s.currentState = state
}

// monitorOutstandingCalls periodically updates the list of outstanding calls
func (s *StatusComponent) monitorOutstandingCalls(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.agent != nil {
				s.outstandingCalls = s.agent.OutstandingToolCalls()
			}
		}
	}
}

// HandleMessage implements MessageHandler
func (s *StatusComponent) HandleMessage(msg Message) tea.Cmd {
	// Route message based on type
	switch m := msg.(type) {
	case agentMessageMsg:
		return s.HandleAgentMessage(m.message)
	case toolUseMsg:
		return s.HandleToolUse(m.message)
	case systemMessageMsg:
		// Convert system message to error message for handling
		agentMsg := &loop.AgentMessage{
			Type:    loop.ErrorMessageType,
			Content: m.content,
		}
		return s.HandleError(agentMsg)
	}
	return nil
}

// HandleAgentMessage handles agent messages
func (s *StatusComponent) HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

// HandleToolUse handles tool use messages
func (s *StatusComponent) HandleToolUse(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

// HandleError handles error messages
func (s *StatusComponent) HandleError(msg *loop.AgentMessage) tea.Cmd {
	return nil
}
