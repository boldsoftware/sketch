package bubbletea

import (
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ErrorHandler manages error handling and recovery
type ErrorHandler struct {
	logger *slog.Logger
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(logger *slog.Logger) *ErrorHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ErrorHandler{
		logger: logger,
	}
}

// HandleTerminalError handles terminal-related errors
func (e *ErrorHandler) HandleTerminalError(err error) tea.Cmd {
	e.logger.Error("Terminal error", "error", err)
	return func() tea.Msg {
		return systemMessageMsg{content: fmt.Sprintf("Terminal error: %v", err)}
	}
}

// HandleAgentError handles agent communication errors
func (e *ErrorHandler) HandleAgentError(err error) tea.Cmd {
	e.logger.Error("Agent error", "error", err)
	return func() tea.Msg {
		return systemMessageMsg{content: fmt.Sprintf("Agent error: %v", err)}
	}
}

// HandleRenderError handles rendering errors
func (e *ErrorHandler) HandleRenderError(err error) tea.Cmd {
	e.logger.Error("Render error", "error", err)
	return func() tea.Msg {
		return systemMessageMsg{content: fmt.Sprintf("Render error: %v", err)}
	}
}

// HandleInputError handles input processing errors
func (e *ErrorHandler) HandleInputError(err error) tea.Cmd {
	e.logger.Error("Input error", "error", err)
	return func() tea.Msg {
		return systemMessageMsg{content: fmt.Sprintf("Input error: %v", err)}
	}
}

// RecoverFromPanic recovers from panics and returns a command to display the error
func (e *ErrorHandler) RecoverFromPanic() tea.Cmd {
	if r := recover(); r != nil {
		stack := debug.Stack()
		e.logger.Error("Recovered from panic",
			"error", r,
			"stack", string(stack))

		return func() tea.Msg {
			return systemMessageMsg{content: fmt.Sprintf("Recovered from panic: %v", r)}
		}
	}
	return nil
}

// WithRecovery wraps a command with panic recovery
func (e *ErrorHandler) WithRecovery(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}

	return func() tea.Msg {
		defer func() {
			if r := recover(); r != nil {
				e.logger.Error("Recovered from panic in command", "error", r)
			}
		}()

		return cmd()
	}
}

// RetryWithBackoff retries a command with exponential backoff
func (e *ErrorHandler) RetryWithBackoff(cmd tea.Cmd, maxRetries int) tea.Cmd {
	return func() tea.Msg {
		var lastErr error
		for i := 0; i < maxRetries; i++ {
			// Try to execute the command
			msg := cmd()

			// Check if the message is an error
			if errMsg, ok := msg.(errorMsg); ok {
				lastErr = errMsg.err
				// Wait with exponential backoff
				backoff := time.Duration(1<<uint(i)) * 100 * time.Millisecond
				if backoff > 5*time.Second {
					backoff = 5 * time.Second
				}
				e.logger.Info("Retrying command after error",
					"attempt", i+1,
					"maxRetries", maxRetries,
					"backoff", backoff,
					"error", lastErr)
				time.Sleep(backoff)
				continue
			}

			// If not an error, return the message
			return msg
		}

		// If we've exhausted retries, return the last error
		e.logger.Error("Command failed after retries",
			"maxRetries", maxRetries,
			"error", lastErr)
		return systemMessageMsg{content: fmt.Sprintf("Operation failed after %d retries: %v", maxRetries, lastErr)}
	}
}

// errorMsg represents an error message
type errorMsg struct {
	err error
}

func (e errorMsg) Error() string {
	return e.err.Error()
}
