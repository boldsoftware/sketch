package bubbletea

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrorCategory defines the type of error for appropriate handling
type ErrorCategory string

const (
	// Error categories
	TerminalError    ErrorCategory = "terminal"
	AgentError       ErrorCategory = "agent"
	RenderError      ErrorCategory = "render"
	InputError       ErrorCategory = "input"
	FileServingError ErrorCategory = "file_serving"
	NetworkError     ErrorCategory = "network"
	InternalError    ErrorCategory = "internal"
)

// ErrorHandler manages error handling and recovery
type ErrorHandler struct {
	logger *slog.Logger
	styles map[string]lipgloss.Style
}

// NewErrorHandler creates a new error handler
func NewErrorHandler() *ErrorHandler {
	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Create styles for error messages
	styles := map[string]lipgloss.Style{
		"error": lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true),
		"warning": lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true),
		"info": lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")),
		"recovery": lipgloss.NewStyle().
			Foreground(lipgloss.Color("35")).
			Italic(true),
	}

	return &ErrorHandler{
		logger: logger,
		styles: styles,
	}
}

// HandleError handles an error with the specified category
func (e *ErrorHandler) HandleError(err error, category ErrorCategory) tea.Cmd {
	if err == nil {
		return nil
	}

	// Log the error
	e.logger.Error("Error occurred",
		"category", category,
		"error", err.Error(),
		"stack", string(debug.Stack()))

	// Create error message based on category
	var msg string
	switch category {
	case TerminalError:
		msg = fmt.Sprintf("Terminal error: %s", err.Error())
	case AgentError:
		msg = fmt.Sprintf("Agent error: %s", err.Error())
	case RenderError:
		msg = fmt.Sprintf("Rendering error: %s", err.Error())
	case InputError:
		msg = fmt.Sprintf("Input error: %s", err.Error())
	case FileServingError:
		msg = fmt.Sprintf("File serving error: %s", err.Error())
	case NetworkError:
		msg = fmt.Sprintf("Network error: %s", err.Error())
	default:
		msg = fmt.Sprintf("Internal error: %s", err.Error())
	}

	// Return a command to display the error message
	return func() tea.Msg {
		return errorMsg{
			message:  msg,
			category: string(category),
		}
	}
}

// HandleTerminalError handles terminal-specific errors
func (e *ErrorHandler) HandleTerminalError(err error) tea.Cmd {
	return e.HandleError(err, TerminalError)
}

// HandleAgentError handles agent-specific errors
func (e *ErrorHandler) HandleAgentError(err error) tea.Cmd {
	return e.HandleError(err, AgentError)
}

// HandleRenderError handles rendering errors
func (e *ErrorHandler) HandleRenderError(err error) tea.Cmd {
	return e.HandleError(err, RenderError)
}

// HandleInputError handles input processing errors
func (e *ErrorHandler) HandleInputError(err error) tea.Cmd {
	return e.HandleError(err, InputError)
}

// HandleFileServingError handles file serving errors
func (e *ErrorHandler) HandleFileServingError(err error) tea.Cmd {
	return e.HandleError(err, FileServingError)
}

// HandleNetworkError handles network-related errors
func (e *ErrorHandler) HandleNetworkError(err error) tea.Cmd {
	return e.HandleError(err, NetworkError)
}

// FormatErrorMessage formats an error message with appropriate styling
func (e *ErrorHandler) FormatErrorMessage(msg string, category string) string {
	prefix := "❌ Error: "

	switch category {
	case string(TerminalError):
		prefix = "❌ Terminal Error: "
	case string(AgentError):
		prefix = "❌ Agent Error: "
	case string(RenderError):
		prefix = "❌ Rendering Error: "
	case string(InputError):
		prefix = "❌ Input Error: "
	case string(FileServingError):
		prefix = "❌ File Serving Error: "
	case string(NetworkError):
		prefix = "❌ Network Error: "
	}

	return e.styles["error"].Render(prefix) + msg
}

// errorMsg is a message type for error notifications
type errorMsg struct {
	message  string
	category string
}
