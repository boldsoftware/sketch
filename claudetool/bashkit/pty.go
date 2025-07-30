//go:build unix
// +build unix

package bashkit

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

// PTY represents a pseudo-terminal.
type PTY struct {
	Master *os.File
	Slave  *os.File
}

// NewPTY creates a new pseudo-terminal.
func NewPTY() (*PTY, error) {
	// Open a PTY master
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open PTY master: %w", err)
	}

	// Get the slave PTY name
	sname, err := ptsname(master)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("failed to get PTY slave name: %w", err)
	}

	// Unlock the slave PTY
	if err := unlockpt(master); err != nil {
		master.Close()
		return nil, fmt.Errorf("failed to unlock PTY slave: %w", err)
	}

	// Open the slave PTY
	slave, err := os.OpenFile(sname, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, fmt.Errorf("failed to open PTY slave: %w", err)
	}

	return &PTY{
		Master: master,
		Slave:  slave,
	}, nil
}

// Close closes both the master and slave file descriptors.
func (pty *PTY) Close() error {
	var masterErr, slaveErr error

	if pty.Slave != nil {
		slaveErr = pty.Slave.Close()
		pty.Slave = nil
	}

	if pty.Master != nil {
		masterErr = pty.Master.Close()
		pty.Master = nil
	}

	if masterErr != nil {
		return masterErr
	}
	return slaveErr
}

// SetWinsize sets the terminal window size.
func (pty *PTY) SetWinsize(rows, cols uint16) error {
	ws := &unix.Winsize{
		Row: rows,
		Col: cols,
	}
	return unix.IoctlSetWinsize(int(pty.Master.Fd()), unix.TIOCSWINSZ, ws)
}

// ptsname returns the name of the PTY slave device.
func ptsname(f *os.File) (string, error) {
	var n uint32
	err := ioctl(f.Fd(), unix.TIOCGPTN, uintptr(unsafe.Pointer(&n)))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("/dev/pts/%d", n), nil
}

// unlockpt unlocks the slave pseudoterminal device.
func unlockpt(f *os.File) error {
	var u int32
	return ioctl(f.Fd(), unix.TIOCSPTLCK, uintptr(unsafe.Pointer(&u)))
}

// ioctl performs a system call to control device parameters.
func ioctl(fd uintptr, cmd uintptr, ptr uintptr) error {
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, cmd, ptr)
	if e != 0 {
		return fmt.Errorf("ioctl failed: %w", e)
	}
	return nil
}

// SetupPTYCommand configures a command to use a PTY.
func SetupPTYCommand(cmd *exec.Cmd, pty *PTY) {
	cmd.Stdin = pty.Slave
	cmd.Stdout = pty.Slave
	cmd.Stderr = pty.Slave

	// Set the process group ID to be the same as the process ID
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
		Setsid:  true,
		Ctty:    int(pty.Slave.Fd()),
	}
}

// IsPTYSupported checks if PTY is supported on the current platform.
func IsPTYSupported() bool {
	// Check if /dev/ptmx exists
	_, err := os.Stat("/dev/ptmx")
	return err == nil
}

// CopyOutput reads from the PTY master and writes to the provided writer.
// Uses timeout-aware reading to prevent hanging on commands like nmap.
func CopyOutput(w io.Writer, pty *PTY) {
	CopyOutputWithTimeout(w, pty, 0) // 0 means no overall timeout, but uses read timeouts
}

// CopyOutputWithTimeout reads from the PTY master with configurable timeout.
// If timeout is 0, it uses read timeouts but no overall timeout.
// If timeout > 0, it stops after that duration regardless of activity.
func CopyOutputWithTimeout(w io.Writer, pty *PTY, timeout time.Duration) {
	const readTimeout = 500 * time.Millisecond // Timeout for individual read operations
	const bufferSize = 4096                    // Buffer size for reading
	
	buffer := make([]byte, bufferSize)
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	
	for {
		// Check overall timeout
		if timeout > 0 && time.Now().After(deadline) {
			break
		}
		
		// Set read timeout on the PTY master
		if err := pty.Master.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			// If we can't set deadline, fall back to blocking read with shorter buffer
			n, err := pty.Master.Read(buffer[:256])
			if err != nil {
				if err == io.EOF {
					break
				}
				// Other errors (including closed PTY) should break the loop
				break
			}
			if n > 0 {
				w.Write(buffer[:n])
			}
			continue
		}
		
		// Read with timeout
		n, err := pty.Master.Read(buffer)
		if err != nil {
			if err == io.EOF {
				break
			}
			// Check if it's a timeout error
			if netErr, ok := err.(interface{ Timeout() bool }); ok && netErr.Timeout() {
				// Read timeout - continue trying unless we've exceeded overall timeout
				continue
			}
			// Other errors (including closed PTY) should break the loop
			break
		}
		
		if n > 0 {
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				// If we can't write to the output, stop
				break
			}
		}
	}
	
	// Clear any remaining deadline
	pty.Master.SetReadDeadline(time.Time{})
}
