package claudetool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"sketch.dev/claudetool/bashkit"
	"sketch.dev/llm"
	"sketch.dev/llm/conversation"
)

// PermissionCallback is a function type for checking if a command is allowed to run
type PermissionCallback func(command string) error

// BashTool specifies a llm.Tool for executing shell commands.
type BashTool struct {
	// CheckPermission is called before running any command, if set
	CheckPermission PermissionCallback
	// EnableJITInstall enables just-in-time tool installation for missing commands
	EnableJITInstall bool
	// Timeouts holds the configurable timeout values (uses defaults if nil)
	Timeouts *Timeouts
}

const (
	EnableBashToolJITInstall = true
	NoBashToolJITInstall     = false

	DefaultFastTimeout       = 30 * time.Second
	DefaultSlowTimeout       = 15 * time.Minute
	DefaultBackgroundTimeout = 24 * time.Hour
)

// Timeouts holds the configurable timeout values for bash commands.
type Timeouts struct {
	Fast       time.Duration // regular commands (e.g., ls, echo, simple scripts)
	Slow       time.Duration // commands that may reasonably take longer (e.g., downloads, builds, tests)
	Background time.Duration // background commands (e.g., servers, long-running processes)
}

// Fast returns t's fast timeout, or DefaultFastTimeout if t is nil.
func (t *Timeouts) fast() time.Duration {
	if t == nil {
		return DefaultFastTimeout
	}
	return t.Fast
}

// Slow returns t's slow timeout, or DefaultSlowTimeout if t is nil.
func (t *Timeouts) slow() time.Duration {
	if t == nil {
		return DefaultSlowTimeout
	}
	return t.Slow
}

// Background returns t's background timeout, or DefaultBackgroundTimeout if t is nil.
func (t *Timeouts) background() time.Duration {
	if t == nil {
		return DefaultBackgroundTimeout
	}
	return t.Background
}

// Tool returns an llm.Tool based on b.
func (b *BashTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        bashName,
		Description: strings.TrimSpace(bashDescription),
		InputSchema: llm.MustSchema(bashInputSchema),
		Run:         b.Run,
	}
}

const (
	bashName        = "bash"
	bashDescription = `
Executes shell commands via bash -c, returning combined stdout/stderr.

With background=true, returns immediately while process continues running
with output redirected to files. Kill process group when done.
Use background for servers/demos that need to stay running.

MUST set slow_ok=true for potentially slow commands: builds, downloads,
installs, tests, or any other substantive operation.

Set pty=true to run commands in a pseudo-terminal environment, which is required
for interactive commands or programs that need terminal-like behavior.
`
	// If you modify this, update the termui template for prettier rendering.
	bashInputSchema = `
{
  "type": "object",
  "required": ["command"],
  "properties": {
    "command": {
      "type": "string",
      "description": "Shell to execute"
    },
    "slow_ok": {
      "type": "boolean",
      "description": "Use extended timeout"
    },
    "background": {
      "type": "boolean",
      "description": "Execute in background"
    },
    "pty": {
      "type": "boolean",
      "description": "Use pseudo-terminal (PTY) for execution"
    }
  }
}
`
)

type bashInput struct {
	Command    string `json:"command"`
	SlowOK     bool   `json:"slow_ok,omitempty"`
	Background bool   `json:"background,omitempty"`
	PTY        bool   `json:"pty,omitempty"`
}

type BackgroundResult struct {
	PID        int    `json:"pid"`
	StdoutFile string `json:"stdout_file"`
	StderrFile string `json:"stderr_file"`
}

func (i *bashInput) timeout(t *Timeouts) time.Duration {
	switch {
	case i.Background:
		return t.background()
	case i.SlowOK:
		return t.slow()
	default:
		return t.fast()
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

	// Check for missing tools and try to install them if needed, best effort only
	if b.EnableJITInstall {
		err := b.checkAndInstallMissingTools(ctx, req.Command)
		if err != nil {
			slog.DebugContext(ctx, "failed to auto-install missing tools", "error", err)
		}
	}

	timeout := req.timeout(b.Timeouts)

	// If Background is set to true, use executeBackgroundBash
	if req.Background {
		result, err := executeBackgroundBash(ctx, req, timeout)
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
	out, execErr := executeBash(ctx, req, timeout)
	if execErr != nil {
		return nil, execErr
	}
	return llm.TextContent(out), nil
}

const maxBashOutputLength = 131072

func executeBash(ctx context.Context, req bashInput, timeout time.Duration) (string, error) {
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Can't do the simple thing and call CombinedOutput because of the need to kill the process group.
	cmd := exec.CommandContext(execCtx, "bash", "-c", req.Command)
	cmd.Dir = WorkingDir(ctx)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Set environment with SKETCH=1
	cmd.Env = append(os.Environ(), "SKETCH=1")

	var output bytes.Buffer
	var pty *bashkit.PTY
	var err error

	// Use PTY if requested and supported
	if req.PTY {
		// Check if PTY is supported on this platform
		if !bashkit.IsPTYSupported() {
			slog.WarnContext(ctx, "PTY requested but not supported on this platform, falling back to pipes")
		} else {
			// Create a new PTY
			pty, err = bashkit.NewPTY()
			if err != nil {
				return "", fmt.Errorf("failed to create PTY: %w", err)
			}
			defer pty.Close()

			// Set terminal size to a reasonable default (80x24)
			if err := pty.SetWinsize(24, 80); err != nil {
				slog.WarnContext(ctx, "failed to set PTY window size", "error", err)
			}

			// Configure command to use the PTY
			bashkit.SetupPTYCommand(cmd, pty)

			// Set up a goroutine to copy output from the PTY master to our buffer
			outputDone := make(chan struct{})
			go func() {
				defer close(outputDone)
				bashkit.CopyOutput(&output, pty)
			}()

			defer func() {
				// Wait for output copying to complete after command finishes
				select {
				case <-outputDone:
				case <-time.After(100 * time.Millisecond):
					// If we don't get all output within a reasonable time, continue anyway
				}
			}()
		}
	}

	// If PTY wasn't requested or failed to initialize, use standard pipes
	if pty == nil {
		cmd.Stdin = nil
		cmd.Stdout = &output
		cmd.Stderr = &output
	}

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

	err = cmd.Wait()
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
		return "", fmt.Errorf("command timed out after %s\nCommand output (until it timed out):\n%s", timeout, outstr)
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
func executeBackgroundBash(ctx context.Context, req bashInput, timeout time.Duration) (*BackgroundResult, error) {
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

	// Use PTY if requested and supported
	var pty *bashkit.PTY
	if req.PTY {
		// Check if PTY is supported on this platform
		if !bashkit.IsPTYSupported() {
			slog.WarnContext(ctx, "PTY requested but not supported on this platform, falling back to pipes")
		} else {
			// Create a new PTY
			pty, err = bashkit.NewPTY()
			if err != nil {
				return nil, fmt.Errorf("failed to create PTY: %w", err)
			}

			// Set terminal size to a reasonable default (80x24)
			if err := pty.SetWinsize(24, 80); err != nil {
				slog.WarnContext(ctx, "failed to set PTY window size", "error", err)
			}

			// Configure command to use the PTY
			bashkit.SetupPTYCommand(cmd, pty)

			// Set up a goroutine to copy output from the PTY master to our files
			go func() {
				defer pty.Close()

				// Copy from PTY master to stdout file
				io.Copy(stdout, pty.Master)
			}()
		}
	}

	// If PTY wasn't requested or failed to initialize, use standard pipes
	if pty == nil {
		cmd.Stdin = nil
		cmd.Stdout = stdout
		cmd.Stderr = stderr
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		if pty != nil {
			pty.Close()
		}
		return nil, fmt.Errorf("failed to start background command: %w", err)
	}

	// Start a goroutine to reap the process when it finishes
	go func() {
		cmd.Wait()
		// Process has been reaped
	}()

	// Set up timeout handling if a timeout was specified
	pid := cmd.Process.Pid
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

// checkAndInstallMissingTools analyzes a bash command and attempts to automatically install any missing tools.
func (b *BashTool) checkAndInstallMissingTools(ctx context.Context, command string) error {
	commands, err := bashkit.ExtractCommands(command)
	if err != nil {
		return err
	}

	autoInstallMu.Lock()
	defer autoInstallMu.Unlock()

	var missing []string
	for _, cmd := range commands {
		if doNotAttemptToolInstall[cmd] {
			continue
		}
		_, err := exec.LookPath(cmd)
		if err == nil {
			doNotAttemptToolInstall[cmd] = true // spare future LookPath calls
			continue
		}
		missing = append(missing, cmd)
	}

	if len(missing) == 0 {
		return nil
	}

	err = b.installTools(ctx, missing)
	if err != nil {
		return err
	}
	for _, cmd := range missing {
		doNotAttemptToolInstall[cmd] = true // either it's installed or it's not--either way, we're done with it
	}
	return nil
}

// Command safety check cache to avoid repeated LLM calls
var (
	autoInstallMu           sync.Mutex
	doNotAttemptToolInstall = make(map[string]bool) // set to true if the tool should not be auto-installed
)

// installTools installs missing tools.
func (b *BashTool) installTools(ctx context.Context, missing []string) error {
	slog.InfoContext(ctx, "installTools subconvo", "tools", missing)

	info := conversation.ToolCallInfoFromContext(ctx)
	if info.Convo == nil {
		return fmt.Errorf("no conversation context available for tool installation")
	}
	subConvo := info.Convo.SubConvo()
	subConvo.Hidden = true
	subBash := &BashTool{EnableJITInstall: NoBashToolJITInstall}

	done := false
	doneTool := &llm.Tool{
		Name:        "done",
		Description: "Call this tool once when finished processing all commands, providing the installation status for each.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "results": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "command_name": {
            "type": "string",
            "description": "The name of the command"
          },
          "installed": {
            "type": "boolean",
            "description": "Whether the command was installed"
          }
        },
        "required": ["command_name", "installed"]
      }
    }
  },
  "required": ["results"]
}`),
		Run: func(ctx context.Context, input json.RawMessage) ([]llm.Content, error) {
			type InstallResult struct {
				CommandName string `json:"command_name"`
				Installed   bool   `json:"installed"`
			}
			type DoneInput struct {
				Results []InstallResult `json:"results"`
			}
			var doneInput DoneInput
			err := json.Unmarshal(input, &doneInput)
			results := doneInput.Results
			if err != nil {
				slog.WarnContext(ctx, "failed to parse install results", "raw", string(input), "error", err)
			} else {
				slog.InfoContext(ctx, "auto-tool installation complete", "results", results)
			}
			done = true
			return llm.TextContent(""), nil
		},
	}

	subConvo.Tools = []*llm.Tool{
		subBash.Tool(),
		doneTool,
	}

	const autoinstallSystemPrompt = `The assistant powers an entirely automated auto-installer tool.

The user will provide a list of commands that were not found on the system.

The assistant's task:

First, decide whether each command is mainstream and safe for automatic installation in a development environment. Skip any commands that could cause security issues, legal problems, or consume excessive resources.

For each appropriate command:

1. Detect the system's package manager and install the command using standard repositories only (no source builds, no curl|bash installs).
2. Make a minimal verification attempt (package manager success is sufficient).
3. If installation fails after reasonable attempts, mark as failed and move on.

Once all commands have been processed, call the "done" tool with the status of each command.
`

	subConvo.SystemPrompt = autoinstallSystemPrompt

	cmds := new(strings.Builder)
	cmds.WriteString("<commands>\n")
	for _, cmd := range missing {
		cmds.WriteString("<command>")
		cmds.WriteString(cmd)
		cmds.WriteString("</command>\n")
	}
	cmds.WriteString("</commands>\n")

	resp, err := subConvo.SendUserTextMessage(cmds.String())
	if err != nil {
		return err
	}

	for !done {
		if resp.StopReason != llm.StopReasonToolUse {
			return fmt.Errorf("subagent finished without calling tool")
		}

		ctxWithWorkDir := WithWorkingDir(ctx, WorkingDir(ctx))
		results, _, err := subConvo.ToolResultContents(ctxWithWorkDir, resp)
		if err != nil {
			return err
		}

		resp, err = subConvo.SendMessage(llm.Message{
			Role:    llm.MessageRoleUser,
			Content: results,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
