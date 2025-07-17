package bashkit

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
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
func CopyOutput(w io.Writer, pty *PTY) {
	io.Copy(w, pty.Master)
}
