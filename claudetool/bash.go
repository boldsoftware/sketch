package claudetool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"sketch.dev/claudetool/bashkit"
	"sketch.dev/llm"
)

// PermissionCallback is a function type for checking if a command is allowed to run
type PermissionCallback func(command string) error

// BashTool is a struct for executing shell commands with bash -c and optional timeout
type BashTool struct {
	// CheckPermission is called before running any command, if set
	CheckPermission PermissionCallback
}

// NewBashTool creates a new Bash tool with optional permission callback
func NewBashTool(checkPermission PermissionCallback) *llm.Tool {
	tool := &BashTool{
		CheckPermission: checkPermission,
	}

	return &llm.Tool{
		Name:        bashName,
		Description: strings.TrimSpace(bashDescription),
		InputSchema: llm.MustSchema(bashInputSchema),
		Run:         tool.Run,
	}
}

// The Bash tool executes shell commands with bash -c and optional timeout
var Bash = NewBashTool(nil)

const (
	bashName        = "bash"
	bashDescription = `
Executes a shell command using bash -c with an optional timeout, returning combined stdout and stderr.
When run with background flag, the process may keep running after the tool call returns, and
the agent can inspect the output by reading the output files. Use the background task when, for example,
starting a server to test something. Be sure to kill the process group when done.
`
	// If you modify this, update the termui template for prettier rendering.
	bashInputSchema = `
{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {
      "type": "string",
      "description": "Shell script to execute"
    },
    "timeout": {
      "type": "string",
      "description": "Timeout as a Go duration string, defaults to 40s if background is false; 10m if background is true"
    },
    "background": {
      "type": "boolean",
      "description": "If true, executes the command in the background without waiting for completion"
    }
  }
}
`
)

type bashInput struct {
	Command    string `json:"command"`
	Timeout    string `json:"timeout,omitempty"`
	Background bool   `json:"background,omitempty"`
}

type BackgroundResult struct {
	PID        int    `json:"pid"`
	StdoutFile string `json:"stdout_file"`
	StderrFile string `json:"stderr_file"`
}

func (i *bashInput) timeout() time.Duration {
	if i.Timeout != "" {
		dur, err := time.ParseDuration(i.Timeout)
		if err == nil {
			return dur
		}
	}

	// Otherwise, use different defaults based on background mode
	if i.Background {
		return 10 * time.Minute
	} else {
		return 40 * time.Second
	}
}

func (b *BashTool) Run(ctx context.Context, m json.RawMessage) ([]llm.Content, error) {
	var req bashInput
	if err := json.Unmarshal(m, &req); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bash command input: %w", err)
	}

	// do a quick permissions check (NOT a security barrier)
	err := bashkit.Check(req.Command)
	if err != nil {
		return nil, err
	}

	// Custom permission callback if set
	if b.CheckPermission != nil {
		if err := b.CheckPermission(req.Command); err != nil {
			return nil, err
		}
	}

	// If Background is set to true, use executeBackgroundBash
	if req.Background {
		result, err := executeBackgroundBash(ctx, req)
		if err != nil {
			return nil, err
		}
		// Marshal the result to JSON
		// TODO: emit XML(-ish) instead?
		output, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal background result: %w", err)
		}
		return llm.TextContent(string(output)), nil
	}

	// For foreground commands, use executeBash
	out, execErr := executeBash(ctx, req)
	if execErr != nil {
		return nil, execErr
	}
	return llm.TextContent(out), nil
}

const maxBashOutputLength = 131072

func executeBash(ctx context.Context, req bashInput) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, req.timeout())
	defer cancel()

	// Can't do the simple thing and call CombinedOutput because of the need to kill the process group.
	cmd := exec.CommandContext(execCtx, "bash", "-c", req.Command)
	cmd.Dir = WorkingDir(ctx)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set environment with SKETCH=1
	cmd.Env = append(os.Environ(), "SKETCH=1")

	var output bytes.Buffer
	cmd.Stdin = nil
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("command failed: %w", err)
	}
	proc := cmd.Process
	done := make(chan struct{})
	go func() {
		select {
		case <-execCtx.Done():
			if execCtx.Err() == context.DeadlineExceeded && proc != nil {
				// Kill the entire process group.
				syscall.Kill(-proc.Pid, syscall.SIGKILL)
			}
		case <-done:
		}
	}()

	err := cmd.Wait()
	close(done)

	longOutput := output.Len() > maxBashOutputLength
	var outstr string
	if longOutput {
		outstr = fmt.Sprintf("output too long: got %v, max is %v\ninitial bytes of output:\n%s",
			humanizeBytes(output.Len()), humanizeBytes(maxBashOutputLength),
			output.Bytes()[:1024],
		)
	} else {
		outstr = output.String()
	}

	if execCtx.Err() == context.DeadlineExceeded {
		// Get the partial output that was captured before the timeout
		partialOutput := output.String()
		// Truncate if the output is too large
		if len(partialOutput) > maxBashOutputLength {
			partialOutput = partialOutput[:maxBashOutputLength] + "\n[output truncated due to size]\n"
		}
		return "", fmt.Errorf("command timed out after %s\nCommand output (until it timed out):\n%s", req.timeout(), outstr)
	}
	if err != nil {
		return "", fmt.Errorf("command failed: %w\n%s", err, outstr)
	}

	if longOutput {
		return "", fmt.Errorf("%s", outstr)
	}

	return output.String(), nil
}

func humanizeBytes(bytes int) string {
	switch {
	case bytes < 4*1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		kb := int(math.Round(float64(bytes) / 1024.0))
		return fmt.Sprintf("%dkB", kb)
	case bytes < 1024*1024*1024:
		mb := int(math.Round(float64(bytes) / (1024.0 * 1024.0)))
		return fmt.Sprintf("%dMB", mb)
	}
	return "more than 1GB"
}

// executeBackgroundBash executes a command in the background and returns the pid and output file locations
func executeBackgroundBash(ctx context.Context, req bashInput) (*BackgroundResult, error) {
	// Create temporary directory for output files
	tmpDir, err := os.MkdirTemp("", "sketch-bg-")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create temp files for stdout and stderr
	stdoutFile := filepath.Join(tmpDir, "stdout")
	stderrFile := filepath.Join(tmpDir, "stderr")

	// Prepare the command
	cmd := exec.Command("bash", "-c", req.Command)
	cmd.Dir = WorkingDir(ctx)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set environment with SKETCH=1
	cmd.Env = append(os.Environ(), "SKETCH=1")

	// Open output files
	stdout, err := os.Create(stdoutFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout file: %w", err)
	}
	defer stdout.Close()

	stderr, err := os.Create(stderrFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr file: %w", err)
	}
	defer stderr.Close()

	// Configure command to use the files
	cmd.Stdin = nil
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start background command: %w", err)
	}

	// Start a goroutine to reap the process when it finishes
	go func() {
		cmd.Wait()
		// Process has been reaped
	}()

	// Set up timeout handling if a timeout was specified
	pid := cmd.Process.Pid
	timeout := req.timeout()
	if timeout > 0 {
		// Launch a goroutine that will kill the process after the timeout
		go func() {
			// TODO(josh): this should use a context instead of a sleep, like executeBash above,
			// to avoid goroutine leaks. Possibly should be partially unified with executeBash.
			// Sleep for the timeout duration
			time.Sleep(timeout)

			// TODO(philip): Should we do SIGQUIT and then SIGKILL in 5s?

			// Try to kill the process group
			killErr := syscall.Kill(-pid, syscall.SIGKILL)
			if killErr != nil {
				// If killing the process group fails, try to kill just the process
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}()
	}

	// Return the process ID and file paths
	return &BackgroundResult{
		PID:        cmd.Process.Pid,
		StdoutFile: stdoutFile,
		StderrFile: stderrFile,
	}, nil
}

// BashRun is the legacy function for testing compatibility
func BashRun(ctx context.Context, m json.RawMessage) ([]llm.Content, error) {
	// Use the default Bash tool which has no permission callback
	return Bash.Run(ctx, m)
}
