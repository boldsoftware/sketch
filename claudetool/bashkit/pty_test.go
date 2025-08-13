//go:build unix
// +build unix

package bashkit

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPTY_Creation(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	pty, err := NewPTY()
	if err != nil {
		t.Fatalf("Failed to create PTY: %v", err)
	}
	defer pty.Close()

	if pty.Master == nil {
		t.Error("PTY master should not be nil")
	}

	if pty.Slave == nil {
		t.Error("PTY slave should not be nil")
	}
}

func TestPTY_SetWinsize(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

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

	// Test setting different size
	err = pty.SetWinsize(50, 120)
	if err != nil {
		t.Errorf("Failed to set different window size: %v", err)
	}
}

func TestPTY_Close(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	pty, err := NewPTY()
	if err != nil {
		t.Fatalf("Failed to create PTY: %v", err)
	}

	// Close should not return error
	err = pty.Close()
	if err != nil {
		t.Errorf("PTY close returned error: %v", err)
	}

	// Second close should not panic or error
	err = pty.Close()
	if err != nil {
		t.Errorf("Second PTY close returned error: %v", err)
	}
}

func TestCopyOutputWithTimeout_BasicOperation(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	pty, err := NewPTY()
	if err != nil {
		t.Fatalf("Failed to create PTY: %v", err)
	}
	defer pty.Close()

	// Write some test data to the slave
	testData := "Hello, PTY World!\n"
	go func() {
		time.Sleep(100 * time.Millisecond) // Small delay to ensure reader is ready
		pty.Slave.Write([]byte(testData))
		pty.Slave.Close() // Close to signal EOF
	}()

	// Read from master with timeout
	var output bytes.Buffer
	CopyOutputWithTimeout(&output, pty, 5*time.Second)

	result := output.String()
	if !strings.Contains(result, "Hello, PTY World!") {
		t.Errorf("Expected output to contain test data, got: %s", result)
	}
}

func TestCopyOutputWithTimeout_TimeoutHandling(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	pty, err := NewPTY()
	if err != nil {
		t.Fatalf("Failed to create PTY: %v", err)
	}
	defer pty.Close()

	// Don't write anything - should timeout
	var output bytes.Buffer
	start := time.Now()
	CopyOutputWithTimeout(&output, pty, 1*time.Second)
	elapsed := time.Since(start)

	// Should have timed out around 1 second (allow some variance)
	if elapsed < 800*time.Millisecond || elapsed > 2*time.Second {
		t.Errorf("Expected timeout around 1s, got %v", elapsed)
	}
}

func TestCopyOutputWithTimeout_IdleDetection(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	pty, err := NewPTY()
	if err != nil {
		t.Fatalf("Failed to create PTY: %v", err)
	}
	defer pty.Close()

	// Test idle detection (no output for extended period)
	var output bytes.Buffer
	start := time.Now()
	
	// Use a longer timeout but expect idle detection to kick in
	CopyOutputWithTimeout(&output, pty, 30*time.Second)
	elapsed := time.Since(start)

	// Should have stopped due to idle detection (around 10 seconds)
	// Allow some variance for system scheduling
	if elapsed < 8*time.Second || elapsed > 15*time.Second {
		t.Errorf("Expected idle timeout around 10s, got %v", elapsed)
	}
}

func TestCopyOutputWithTimeout_ProgressTracking(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	pty, err := NewPTY()
	if err != nil {
		t.Fatalf("Failed to create PTY: %v", err)
	}
	defer pty.Close()

	// Write data in chunks to test progress tracking
	testData := []string{
		"First chunk of data\n",
		"Second chunk of data\n",
		"Third chunk of data\n",
	}

	go func() {
		for i, chunk := range testData {
			time.Sleep(time.Duration(i*100) * time.Millisecond)
			pty.Slave.Write([]byte(chunk))
		}
		time.Sleep(200 * time.Millisecond)
		pty.Slave.Close() // Signal EOF
	}()

	var output bytes.Buffer
	CopyOutputWithTimeout(&output, pty, 5*time.Second)

	result := output.String()
	for _, expectedChunk := range testData {
		if !strings.Contains(result, strings.TrimSpace(expectedChunk)) {
			t.Errorf("Expected output to contain '%s', got: %s", expectedChunk, result)
		}
	}
}

func TestIsPTYSupported(t *testing.T) {
	// This should return true on Unix systems
	supported := IsPTYSupported()
	
	// On Unix systems with /dev/ptmx, this should be true
	// We can't assert a specific value since it depends on the system
	t.Logf("PTY supported: %v", supported)
}

func TestCopyOutput_ConvenienceFunction(t *testing.T) {
	if !IsPTYSupported() {
		t.Skip("PTY not supported on this platform")
	}

	pty, err := NewPTY()
	if err != nil {
		t.Fatalf("Failed to create PTY: %v", err)
	}
	defer pty.Close()

	// Test the convenience function (no overall timeout)
	testData := "Test data for convenience function\n"
	go func() {
		time.Sleep(100 * time.Millisecond)
		pty.Slave.Write([]byte(testData))
		pty.Slave.Close()
	}()

	var output bytes.Buffer
	
	// Use a channel to stop the copy operation after a reasonable time
	done := make(chan struct{})
	go func() {
		CopyOutput(&output, pty)
		close(done)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		// Good, function completed
	case <-time.After(5 * time.Second):
		t.Error("CopyOutput did not complete within reasonable time")
	}

	result := output.String()
	if !strings.Contains(result, "Test data for convenience function") {
		t.Errorf("Expected output to contain test data, got: %s", result)
	}
}