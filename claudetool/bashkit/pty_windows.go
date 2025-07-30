//go:build windows
// +build windows

package bashkit

import (
	"io"
	"os"
	"os/exec"
	"time"
)

// PTY represents a pseudo-terminal on Windows (stub implementation).
// Windows doesn't have true PTY support, so this provides a compatible interface
// that falls back to standard pipes with enhanced buffering.
type PTY struct {
	Master *os.File // Will be nil on Windows
	Slave  *os.File // Will be nil on Windows
}

// NewPTY creates a new pseudo-terminal (stub for Windows).
// Returns an error since Windows doesn't support true PTY.
func NewPTY() (*PTY, error) {
	return nil, &os.PathError{
		Op:   "NewPTY",
		Path: "PTY",
		Err:  os.ErrNotExist,
	}
}

// SetWinsize sets the window size of the PTY (stub for Windows).
func (p *PTY) SetWinsize(rows, cols int) error {
	return &os.PathError{
		Op:   "SetWinsize",
		Path: "PTY",
		Err:  os.ErrNotExist,
	}
}

// Close closes the PTY (stub for Windows).
func (p *PTY) Close() error {
	return nil
}

// IsPTYSupported returns whether PTY is supported on this platform.
func IsPTYSupported() bool {
	return false // Windows doesn't support true PTY
}

// SetupPTYCommand configures a command to use the PTY (stub for Windows).
// Since PTY isn't supported, this does nothing.
func SetupPTYCommand(cmd *exec.Cmd, pty *PTY) {
	// No-op on Windows since PTY isn't supported
}

// CopyOutput reads from the PTY master and writes to the provided writer.
// On Windows, this is a no-op since PTY isn't supported.
func CopyOutput(w io.Writer, pty *PTY) {
	// No-op on Windows
}

// CopyOutputWithTimeout reads from the PTY master with configurable timeout.
// On Windows, this is a no-op since PTY isn't supported.
func CopyOutputWithTimeout(w io.Writer, pty *PTY, timeout time.Duration) {
	// No-op on Windows
}
