package claudetool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestBashRun(t *testing.T) {
	// Test basic functionality
	t.Run("Basic Command", func(t *testing.T) {
		input := json.RawMessage(`{"command":"echo 'Hello, world!'"}`)

		result, err := BashRun(context.Background(), input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expected := "Hello, world!\n"
		resultStr := ContentToString(result)
		if resultStr != expected {
			t.Errorf("Expected %q, got %q", expected, resultStr)
		}
	})

	// Test with arguments
	t.Run("Command With Arguments", func(t *testing.T) {
		input := json.RawMessage(`{"command":"echo -n foo && echo -n bar"}`)

		result, err := BashRun(context.Background(), input)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expected := "foobar"
		resultStr := ContentToString(result)
		if resultStr != expected {
			t.Errorf("Expected %q, got %q", expected, resultStr)
		}
	})

	// Test with timeout parameter
	t.Run("With Timeout", func(t *testing.T) {
		inputObj := struct {
			Command string `json:"command"`
			Timeout string `json:"timeout"`
		}{
			Command: "sleep 0.1 && echo 'Completed'",
			Timeout: "5s",
		}
		inputJSON, err := json.Marshal(inputObj)
		if err != nil {
			t.Fatalf("Failed to marshal input: %v", err)
		}

		result, err := BashRun(context.Background(), inputJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		expected := "Completed\n"
		resultStr := ContentToString(result)
		if resultStr != expected {
			t.Errorf("Expected %q, got %q", expected, resultStr)
		}
	})

	// Test command timeout
	t.Run("Command Timeout", func(t *testing.T) {
		inputObj := struct {
			Command string `json:"command"`
			Timeout string `json:"timeout"`
		}{
			Command: "sleep 0.5 && echo 'Should not see this'",
			Timeout: "100ms",
		}
		inputJSON, err := json.Marshal(inputObj)
		if err != nil {
			t.Fatalf("Failed to marshal input: %v", err)
		}

		_, err = BashRun(context.Background(), inputJSON)
		if err == nil {
			t.Errorf("Expected timeout error, got none")
		} else if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("Expected timeout error, got: %v", err)
		}
	})

	// Test command that fails
	t.Run("Failed Command", func(t *testing.T) {
		input := json.RawMessage(`{"command":"exit 1"}`)

		_, err := BashRun(context.Background(), input)
		if err == nil {
			t.Errorf("Expected error for failed command, got none")
		}
	})

	// Test invalid input
	t.Run("Invalid JSON Input", func(t *testing.T) {
		input := json.RawMessage(`{"command":123}`) // Invalid JSON (command must be string)

		_, err := BashRun(context.Background(), input)
		if err == nil {
			t.Errorf("Expected error for invalid input, got none")
		}
	})
}

func TestExecuteBash(t *testing.T) {
	ctx := context.Background()

	// Test successful command
	t.Run("Successful Command", func(t *testing.T) {
		req := bashInput{
			Command: "echo 'Success'",
			Timeout: "5s",
		}

		output, err := executeBash(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		want := "Success\n"
		if output != want {
			t.Errorf("Expected %q, got %q", want, output)
		}
	})

	// Test SKETCH=1 environment variable is set
	t.Run("SKETCH Environment Variable", func(t *testing.T) {
		req := bashInput{
			Command: "echo $SKETCH",
			Timeout: "5s",
		}

		output, err := executeBash(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		want := "1\n"
		if output != want {
			t.Errorf("Expected SKETCH=1, got %q", output)
		}
	})

	// Test command with output to stderr
	t.Run("Command with stderr", func(t *testing.T) {
		req := bashInput{
			Command: "echo 'Error message' >&2 && echo 'Success'",
			Timeout: "5s",
		}

		output, err := executeBash(ctx, req)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		want := "Error message\nSuccess\n"
		if output != want {
			t.Errorf("Expected %q, got %q", want, output)
		}
	})

	// Test command that fails with stderr
	t.Run("Failed Command with stderr", func(t *testing.T) {
		req := bashInput{
			Command: "echo 'Error message' >&2 && exit 1",
			Timeout: "5s",
		}

		_, err := executeBash(ctx, req)
		if err == nil {
			t.Errorf("Expected error for failed command, got none")
		} else if !strings.Contains(err.Error(), "Error message") {
			t.Errorf("Expected stderr in error message, got: %v", err)
		}
	})

	// Test timeout
	t.Run("Command Timeout", func(t *testing.T) {
		req := bashInput{
			Command: "sleep 1 && echo 'Should not see this'",
			Timeout: "100ms",
		}

		start := time.Now()
		_, err := executeBash(ctx, req)
		elapsed := time.Since(start)

		// Command should time out after ~100ms, not wait for full 1 second
		if elapsed >= 1*time.Second {
			t.Errorf("Command did not respect timeout, took %v", elapsed)
		}

		if err == nil {
			t.Errorf("Expected timeout error, got none")
		} else if !strings.Contains(err.Error(), "timed out") {
			t.Errorf("Expected timeout error, got: %v", err)
		}
	})
}

func TestBackgroundBash(t *testing.T) {
	// Test basic background execution
	t.Run("Basic Background Command", func(t *testing.T) {
		inputObj := struct {
			Command    string `json:"command"`
			Background bool   `json:"background"`
		}{
			Command:    "echo 'Hello from background' $SKETCH",
			Background: true,
		}
		inputJSON, err := json.Marshal(inputObj)
		if err != nil {
			t.Fatalf("Failed to marshal input: %v", err)
		}

		result, err := BashRun(context.Background(), inputJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Parse the returned JSON
		var bgResult BackgroundResult
		resultStr := ContentToString(result)
		if err := json.Unmarshal([]byte(resultStr), &bgResult); err != nil {
			t.Fatalf("Failed to unmarshal background result: %v", err)
		}

		// Verify we got a valid PID
		if bgResult.PID <= 0 {
			t.Errorf("Invalid PID returned: %d", bgResult.PID)
		}

		// Verify output files exist
		if _, err := os.Stat(bgResult.StdoutFile); os.IsNotExist(err) {
			t.Errorf("Stdout file doesn't exist: %s", bgResult.StdoutFile)
		}
		if _, err := os.Stat(bgResult.StderrFile); os.IsNotExist(err) {
			t.Errorf("Stderr file doesn't exist: %s", bgResult.StderrFile)
		}

		// Wait for the command output to be written to file
		waitForFile(t, bgResult.StdoutFile)

		// Check file contents
		stdoutContent, err := os.ReadFile(bgResult.StdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}
		expected := "Hello from background 1\n"
		if string(stdoutContent) != expected {
			t.Errorf("Expected stdout content %q, got %q", expected, string(stdoutContent))
		}

		// Clean up
		os.Remove(bgResult.StdoutFile)
		os.Remove(bgResult.StderrFile)
		os.Remove(filepath.Dir(bgResult.StdoutFile))
	})

	// Test background command with stderr output
	t.Run("Background Command with stderr", func(t *testing.T) {
		inputObj := struct {
			Command    string `json:"command"`
			Background bool   `json:"background"`
		}{
			Command:    "echo 'Output to stdout' && echo 'Output to stderr' >&2",
			Background: true,
		}
		inputJSON, err := json.Marshal(inputObj)
		if err != nil {
			t.Fatalf("Failed to marshal input: %v", err)
		}

		result, err := BashRun(context.Background(), inputJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Parse the returned JSON
		var bgResult BackgroundResult
		resultStr := ContentToString(result)
		if err := json.Unmarshal([]byte(resultStr), &bgResult); err != nil {
			t.Fatalf("Failed to unmarshal background result: %v", err)
		}

		// Wait for the command output to be written to files
		waitForFile(t, bgResult.StdoutFile)
		waitForFile(t, bgResult.StderrFile)

		// Check stdout content
		stdoutContent, err := os.ReadFile(bgResult.StdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}
		expectedStdout := "Output to stdout\n"
		if string(stdoutContent) != expectedStdout {
			t.Errorf("Expected stdout content %q, got %q", expectedStdout, string(stdoutContent))
		}

		// Check stderr content
		stderrContent, err := os.ReadFile(bgResult.StderrFile)
		if err != nil {
			t.Fatalf("Failed to read stderr file: %v", err)
		}
		expectedStderr := "Output to stderr\n"
		if string(stderrContent) != expectedStderr {
			t.Errorf("Expected stderr content %q, got %q", expectedStderr, string(stderrContent))
		}

		// Clean up
		os.Remove(bgResult.StdoutFile)
		os.Remove(bgResult.StderrFile)
		os.Remove(filepath.Dir(bgResult.StdoutFile))
	})

	// Test background command running without waiting
	t.Run("Background Command Running", func(t *testing.T) {
		// Create a script that will continue running after we check
		inputObj := struct {
			Command    string `json:"command"`
			Background bool   `json:"background"`
		}{
			Command:    "echo 'Running in background' && sleep 5",
			Background: true,
		}
		inputJSON, err := json.Marshal(inputObj)
		if err != nil {
			t.Fatalf("Failed to marshal input: %v", err)
		}

		// Start the command in the background
		result, err := BashRun(context.Background(), inputJSON)
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		// Parse the returned JSON
		var bgResult BackgroundResult
		resultStr := ContentToString(result)
		if err := json.Unmarshal([]byte(resultStr), &bgResult); err != nil {
			t.Fatalf("Failed to unmarshal background result: %v", err)
		}

		// Wait for the command output to be written to file
		waitForFile(t, bgResult.StdoutFile)

		// Check stdout content
		stdoutContent, err := os.ReadFile(bgResult.StdoutFile)
		if err != nil {
			t.Fatalf("Failed to read stdout file: %v", err)
		}
		expectedStdout := "Running in background\n"
		if string(stdoutContent) != expectedStdout {
			t.Errorf("Expected stdout content %q, got %q", expectedStdout, string(stdoutContent))
		}

		// Verify the process is still running
		process, _ := os.FindProcess(bgResult.PID)
		err = process.Signal(syscall.Signal(0))
		if err != nil {
			// Process not running, which is unexpected
			t.Error("Process is not running")
		} else {
			// Expected: process should be running
			t.Log("Process correctly running in background")
			// Kill it for cleanup
			process.Kill()
		}

		// Clean up
		os.Remove(bgResult.StdoutFile)
		os.Remove(bgResult.StderrFile)
		os.Remove(filepath.Dir(bgResult.StdoutFile))
	})
}

func TestBashTimeout(t *testing.T) {
	// Test default timeout values
	t.Run("Default Timeout Values", func(t *testing.T) {
		// Test foreground default timeout
		foreground := bashInput{
			Command:    "echo 'test'",
			Background: false,
		}
		fgTimeout := foreground.timeout()
		expectedFg := 40 * time.Second
		if fgTimeout != expectedFg {
			t.Errorf("Expected foreground default timeout to be %v, got %v", expectedFg, fgTimeout)
		}

		// Test background default timeout
		background := bashInput{
			Command:    "echo 'test'",
			Background: true,
		}
		bgTimeout := background.timeout()
		expectedBg := 10 * time.Minute
		if bgTimeout != expectedBg {
			t.Errorf("Expected background default timeout to be %v, got %v", expectedBg, bgTimeout)
		}

		// Test explicit timeout overrides defaults
		explicit := bashInput{
			Command:    "echo 'test'",
			Background: true,
			Timeout:    "5s",
		}
		explicitTimeout := explicit.timeout()
		expectedExplicit := 5 * time.Second
		if explicitTimeout != expectedExplicit {
			t.Errorf("Expected explicit timeout to be %v, got %v", expectedExplicit, explicitTimeout)
		}
	})
}

// waitForFile waits for a file to exist and be non-empty or times out
func waitForFile(t *testing.T, filepath string) {
	timeout := time.After(5 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Timed out waiting for file to exist and have contents: %s", filepath)
			return
		case <-tick.C:
			info, err := os.Stat(filepath)
			if err == nil && info.Size() > 0 {
				return // File exists and has content
			}
		}
	}
}

// waitForProcessDeath waits for a process to no longer exist or times out
func waitForProcessDeath(t *testing.T, pid int) {
	timeout := time.After(5 * time.Second)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Timed out waiting for process %d to exit", pid)
			return
		case <-tick.C:
			process, _ := os.FindProcess(pid)
			err := process.Signal(syscall.Signal(0))
			if err != nil {
				// Process doesn't exist
				return
			}
		}
	}
}
