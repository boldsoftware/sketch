package bashkit

import (
	"bytes"
	"os"
	"testing"
)

func TestPTYSupport(t *testing.T) {
	// Skip test if PTY is not supported on this platform
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	t.Run("PTY Creation", func(t *testing.T) {
		pty, err := NewPTY()
		if err != nil {
			t.Fatalf("Failed to create PTY: %v", err)
		}
		defer pty.Close()

		// Verify that master and slave are valid file descriptors
		if pty.Master == nil {
			t.Error("PTY master is nil")
		}
		if pty.Slave == nil {
			t.Error("PTY slave is nil")
		}
	})

	t.Run("PTY Winsize", func(t *testing.T) {
		pty, err := NewPTY()
		if err != nil {
			t.Fatalf("Failed to create PTY: %v", err)
		}
		defer pty.Close()

		// Test setting window size
		err = pty.SetWinsize(24, 80)
		if err != nil {
			t.Errorf("Failed to set window size: %v", err)
		}
	})

	t.Run("PTY IO", func(t *testing.T) {
		pty, err := NewPTY()
		if err != nil {
			t.Fatalf("Failed to create PTY: %v", err)
		}
		defer pty.Close()

		// Write to slave, read from master (this is the normal direction)
		testData := []byte("test data")
		_, err = pty.Slave.Write(testData)
		if err != nil {
			t.Fatalf("Failed to write to slave: %v", err)
		}

		buf := make([]byte, len(testData))
		n, err := pty.Master.Read(buf)
		if err != nil {
			t.Fatalf("Failed to read from master: %v", err)
		}

		if n != len(testData) || !bytes.Equal(buf, testData) {
			t.Errorf("Data mismatch: wrote %q, read %q", testData, buf)
		}
	})

	t.Run("PTY Close", func(t *testing.T) {
		pty, err := NewPTY()
		if err != nil {
			t.Fatalf("Failed to create PTY: %v", err)
		}

		// Close the PTY
		err = pty.Close()
		if err != nil {
			t.Errorf("Failed to close PTY: %v", err)
		}

		// Verify that master and slave are closed
		_, err = pty.Master.Write([]byte("test"))
		if err == nil {
			t.Error("Expected error writing to closed master")
		}
	})

	t.Run("CopyOutput", func(t *testing.T) {
		pty, err := NewPTY()
		if err != nil {
			t.Fatalf("Failed to create PTY: %v", err)
		}
		defer pty.Close()

		var buf bytes.Buffer
		done := make(chan struct{})

		// Start copying in background
		go func() {
			defer close(done)
			CopyOutput(&buf, pty)
		}()

		// Write some data to the slave (simulating a process output)
		testData := []byte("test output data")
		_, err = pty.Slave.Write(testData)
		if err != nil {
			t.Fatalf("Failed to write to slave: %v", err)
		}

		// Close the slave to signal EOF to the copy operation
		pty.Slave.Close()

		// Wait for copy to complete
		<-done

		// Verify the copied data
		if !bytes.Contains(buf.Bytes(), testData) {
			t.Errorf("Expected output to contain %q, got %q", testData, buf.String())
		}
	})
}

func TestIsPTYSupported(t *testing.T) {
	// This test just verifies that the function runs without error
	supported := IsPTYSupported()

	// On Linux, PTY should be supported
	if os.Getenv("CI") == "" && os.Getenv("GITHUB_ACTIONS") == "" {
		// Only check this when not running in CI, as CI environments might vary
		if _, err := os.Stat("/dev/ptmx"); err == nil {
			if !supported {
				t.Error("PTY should be supported on this platform but IsPTYSupported returned false")
			}
		}
	}
}
