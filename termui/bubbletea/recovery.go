package bubbletea

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

// RecoveryManager handles recovery from panics and errors
type RecoveryManager struct {
	// Original terminal state for restoration
	originalState *term.State
	// Error handler for categorized errors
	errorHandler *ErrorHandler
	// Context for cancellation
	ctx context.Context
	// Cancel function for cleanup
	cancel context.CancelFunc
	// Wait group for pending operations
	wg sync.WaitGroup
	// Recovery attempts counter
	recoveryAttempts int
	// Maximum recovery attempts
	maxRecoveryAttempts int
	// Recovery backoff duration
	backoffDuration time.Duration
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(originalState *term.State) *RecoveryManager {
	ctx, cancel := context.WithCancel(context.Background())

	return &RecoveryManager{
		originalState:       originalState,
		errorHandler:        NewErrorHandler(),
		ctx:                 ctx,
		cancel:              cancel,
		maxRecoveryAttempts: 3,
		backoffDuration:     500 * time.Millisecond,
	}
}

// RecoverFromPanic recovers from a panic and returns a tea.Cmd
func (r *RecoveryManager) RecoverFromPanic() tea.Cmd {
	if err := recover(); err != nil {
		// Increment recovery attempts
		r.recoveryAttempts++

		// Log the panic
		stack := debug.Stack()
		r.errorHandler.logger.Error("Recovered from panic",
			"error", err,
			"stack", string(stack),
			"attempt", r.recoveryAttempts)

		// Restore terminal state if available
		if r.originalState != nil {
			term.Restore(int(os.Stdin.Fd()), r.originalState)
		}

		// Check if we've exceeded maximum recovery attempts
		if r.recoveryAttempts > r.maxRecoveryAttempts {
			// Cancel context and exit
			r.cancel()
			return tea.Quit
		}

		// Return a command to display the error and continue
		return func() tea.Msg {
			// Add backoff delay
			time.Sleep(r.backoffDuration * time.Duration(r.recoveryAttempts))

			return errorMsg{
				message:  fmt.Sprintf("Recovered from panic: %v", err),
				category: string(InternalError),
			}
		}
	}

	return nil
}

// SafeExecute executes a function with panic recovery
func (r *RecoveryManager) SafeExecute(fn func() error) error {
	var err error

	defer func() {
		if panicErr := recover(); panicErr != nil {
			stack := debug.Stack()
			r.errorHandler.logger.Error("Panic in SafeExecute",
				"error", panicErr,
				"stack", string(stack))

			err = fmt.Errorf("panic: %v", panicErr)
		}
	}()

	err = fn()
	return err
}

// WaitForPendingOperations waits for all pending operations to complete
func (r *RecoveryManager) WaitForPendingOperations(timeout time.Duration) {
	// Create a channel for timeout
	done := make(chan struct{})

	// Wait for operations in a goroutine
	go func() {
		r.wg.Wait()
		close(done)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		// All operations completed
		return
	case <-time.After(timeout):
		// Timeout occurred
		r.errorHandler.logger.Warn("Timeout waiting for pending operations")
		return
	}
}

// RestoreTerminalState restores the original terminal state
func (r *RecoveryManager) RestoreTerminalState() error {
	if r.originalState != nil {
		return term.Restore(int(os.Stdin.Fd()), r.originalState)
	}
	return nil
}

// AddPendingOperation adds a pending operation to the wait group
func (r *RecoveryManager) AddPendingOperation() {
	r.wg.Add(1)
}

// CompletePendingOperation completes a pending operation
func (r *RecoveryManager) CompletePendingOperation() {
	r.wg.Done()
}

// Cleanup performs cleanup operations
func (r *RecoveryManager) Cleanup() {
	// Cancel context
	r.cancel()

	// Wait for pending operations with timeout
	r.WaitForPendingOperations(5 * time.Second)

	// Restore terminal state
	r.RestoreTerminalState()
}

// WithRecovery wraps a tea.Cmd with panic recovery
func (r *RecoveryManager) WithRecovery(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}

	return func() tea.Msg {
		defer r.RecoverFromPanic()
		return cmd()
	}
}

// SafeUpdate wraps a model's Update method with panic recovery
func (r *RecoveryManager) SafeUpdate(model tea.Model, msg tea.Msg) (tea.Model, tea.Cmd) {
	var m tea.Model
	var cmd tea.Cmd

	defer func() {
		if panicCmd := r.RecoverFromPanic(); panicCmd != nil {
			cmd = panicCmd
		}
	}()

	m, cmd = model.Update(msg)
	return m, cmd
}

// SafeView wraps a model's View method with panic recovery
func (r *RecoveryManager) SafeView(model tea.Model) string {
	var view string

	defer func() {
		if r.RecoverFromPanic() != nil {
			view = "Error rendering view. Please try again."
		}
	}()

	view = model.View()
	return view
}
