package bubbletea

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// RecoveryManager handles recovery from panics and transient errors
type RecoveryManager struct {
	// Mutex for thread safety
	mu sync.Mutex

	// Recovery state
	recoveryAttempts map[string]int
	lastRecoveryTime map[string]time.Time
	maxRetries       int

	// Context for cancellation
	ctx context.Context

	// Error handler for logging and displaying errors
	errorHandler *ErrorHandler
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(ctx context.Context, errorHandler *ErrorHandler) *RecoveryManager {
	return &RecoveryManager{
		recoveryAttempts: make(map[string]int),
		lastRecoveryTime: make(map[string]time.Time),
		maxRetries:       3,
		ctx:              ctx,
		errorHandler:     errorHandler,
	}
}

// SafeExecute executes a function with panic recovery
func (rm *RecoveryManager) SafeExecute(id string, fn func() error) error {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			if rm.errorHandler != nil {
				rm.errorHandler.logger.Error("Recovered from panic",
					"id", id,
					"error", r,
					"stack", string(stack))
			}

			// Record recovery attempt
			rm.mu.Lock()
			rm.recoveryAttempts[id]++
			rm.lastRecoveryTime[id] = time.Now()
			rm.mu.Unlock()
		}
	}()

	return fn()
}

// SafeCommand wraps a tea.Cmd with panic recovery
func (rm *RecoveryManager) SafeCommand(id string, cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}

	return func() tea.Msg {
		var result tea.Msg

		err := rm.SafeExecute(id, func() error {
			result = cmd()
			return nil
		})

		if err != nil {
			return systemMessageMsg{content: fmt.Sprintf("Error executing command %s: %v", id, err)}
		}

		return result
	}
}

// RetryWithBackoff retries a function with exponential backoff
func (rm *RecoveryManager) RetryWithBackoff(id string, fn func() error) error {
	rm.mu.Lock()
	attempts := rm.recoveryAttempts[id]
	rm.mu.Unlock()

	// Check if we've exceeded max retries
	if attempts >= rm.maxRetries {
		return fmt.Errorf("exceeded maximum retry attempts (%d) for %s", rm.maxRetries, id)
	}

	// Calculate backoff duration
	backoff := time.Duration(1<<uint(attempts)) * 100 * time.Millisecond
	if backoff > 5*time.Second {
		backoff = 5 * time.Second
	}

	// Record attempt
	rm.mu.Lock()
	rm.recoveryAttempts[id]++
	rm.lastRecoveryTime[id] = time.Now()
	rm.mu.Unlock()

	// Wait for backoff period
	select {
	case <-rm.ctx.Done():
		return rm.ctx.Err()
	case <-time.After(backoff):
		// Continue with retry
	}

	// Execute function
	return fn()
}

// ResetRetryCount resets the retry count for a specific ID
func (rm *RecoveryManager) ResetRetryCount(id string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	delete(rm.recoveryAttempts, id)
	delete(rm.lastRecoveryTime, id)
}

// GetRetryCount returns the current retry count for a specific ID
func (rm *RecoveryManager) GetRetryCount(id string) int {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	return rm.recoveryAttempts[id]
}

// RestoreComponentState attempts to restore a component's state after failure
func (rm *RecoveryManager) RestoreComponentState(component UIComponent) tea.Cmd {
	// Get component name for logging
	componentName := fmt.Sprintf("%T", component)

	// Log recovery attempt
	if rm.errorHandler != nil {
		rm.errorHandler.logger.Info("Attempting to restore component state",
			"component", componentName)
	}

	// Reset component if it implements StateManager
	if stateManager, ok := component.(StateManager); ok {
		stateManager.Reset()
	}

	// Return command to update UI with recovery message
	return func() tea.Msg {
		return systemMessageMsg{content: fmt.Sprintf("Restored %s component after error", componentName)}
	}
}
