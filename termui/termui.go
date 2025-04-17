package termui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"text/template"
	"time"

	"github.com/fatih/color"
	"golang.org/x/term"
	"sketch.dev/loop"
)

var (
	// toolUseTemplTxt defines how tool invocations appear in the terminal UI.
	// Keep this template in sync with the tools defined in claudetool package
	// and registered in loop/agent.go.
	// Add formatting for new tools as they are created.
	// TODO: should this be part of tool definition to make it harder to forget to set up?
	toolUseTemplTxt = `{{if .msg.ToolError}}🙈 {{end -}}
{{if eq .msg.ToolName "think" -}}
 🧠 {{.input.thoughts -}}
{{else if eq .msg.ToolName "keyword_search" -}}
 🔍 {{ .input.query}}: {{.input.keywords -}}
{{else if eq .msg.ToolName "bash" -}}
 🖥️  {{ .input.command -}}
{{else if eq .msg.ToolName "patch" -}}
 ⌨️  {{.input.path -}}
{{else if eq .msg.ToolName "done" -}}
{{/* nothing to show here, the agent will write more in its next message */}}
{{else if eq .msg.ToolName "title" -}}
 🏷️  {{.input.title -}}
{{else if eq .msg.ToolName "str_replace_editor" -}}
 ✏️  {{.input.file_path -}}
{{else if eq .msg.ToolName "codereview" -}}
 🐛  Running automated code review, may be slow
{{else -}}
 🛠️ {{ .msg.ToolName}}: {{.msg.ToolInput -}}
{{end -}}
`
	toolUseTmpl = template.Must(template.New("tool_use").Parse(toolUseTemplTxt))
)

type termUI struct {
	stdin  *os.File
	stdout *os.File
	stderr *os.File

	agent   loop.CodingAgent
	httpURL string

	trm *term.Terminal

	// the chatMsgCh channel is for "conversation" messages, like responses to user input
	// from the LLM, or output from executing slash-commands issued by the user.
	chatMsgCh chan chatMessage

	// the log channel is for secondary messages, like logging, errors, and debug information
	// from local and remove subproceses.
	termLogCh chan string

	// protects following
	mu       sync.Mutex
	oldState *term.State
	// Tracks branches that were pushed during the session
	pushedBranches map[string]struct{}
}

type chatMessage struct {
	idx      int
	sender   string
	content  string
	thinking bool
}

func New(agent loop.CodingAgent, httpURL string) *termUI {
	return &termUI{
		agent:          agent,
		stdin:          os.Stdin,
		stdout:         os.Stdout,
		stderr:         os.Stderr,
		httpURL:        httpURL,
		chatMsgCh:      make(chan chatMessage, 1),
		termLogCh:      make(chan string, 1),
		pushedBranches: make(map[string]struct{}),
	}
}

func (ui *termUI) Run(ctx context.Context) error {
	fmt.Println(`🎨 Welcome to Sketch`)
	fmt.Println(`🌐 ` + ui.httpURL + `/`)
	fmt.Println(`📍 Initial Commit: ` + ui.agent.InitialCommit())
	fmt.Println(`💬 type 'help' for help`)
	fmt.Println()

	// Start up the main terminal UI:
	if err := ui.initializeTerminalUI(ctx); err != nil {
		return err
	}
	go ui.receiveMessagesLoop(ctx)
	if err := ui.inputLoop(ctx); err != nil {
		return err
	}
	return nil
}

func (ui *termUI) LogToolUse(resp loop.AgentMessage) {
	inputData := map[string]any{}
	if err := json.Unmarshal([]byte(resp.ToolInput), &inputData); err != nil {
		ui.AppendSystemMessage("error: %v", err)
		return
	}
	buf := bytes.Buffer{}
	if err := toolUseTmpl.Execute(&buf, map[string]any{"msg": resp, "input": inputData, "output": resp.ToolResult}); err != nil {
		ui.AppendSystemMessage("error: %v", err)
		return
	}
	ui.AppendSystemMessage("%s\n", buf.String())
}

func (ui *termUI) receiveMessagesLoop(ctx context.Context) {
	bold := color.New(color.Bold).SprintFunc()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		resp := ui.agent.WaitForMessage(ctx)
		// Typically a user message will start the thinking and a (top-level
		// conversation) end of turn will stop it.
		thinking := !(resp.EndOfTurn && resp.ParentConversationID == nil)

		switch resp.Type {
		case loop.AgentMessageType:
			ui.AppendChatMessage(chatMessage{thinking: thinking, idx: resp.Idx, sender: "🕴️", content: resp.Content})
		case loop.ToolUseMessageType:
			ui.LogToolUse(resp)
		case loop.ErrorMessageType:
			ui.AppendSystemMessage("❌ %s", resp.Content)
		case loop.BudgetMessageType:
			ui.AppendSystemMessage("💰 %s", resp.Content)
		case loop.AutoMessageType:
			ui.AppendSystemMessage("🧐 %s", resp.Content)
		case loop.UserMessageType:
			ui.AppendChatMessage(chatMessage{thinking: thinking, idx: resp.Idx, sender: "👤️", content: resp.Content})
		case loop.CommitMessageType:
			// Display each commit in the terminal
			for _, commit := range resp.Commits {
				if commit.PushedBranch != "" {
					ui.AppendSystemMessage("🔄 new commit: [%s] %s pushed to %s", commit.Hash[:8], commit.Subject, bold(commit.PushedBranch))

					// Track the pushed branch in our map
					ui.mu.Lock()
					ui.pushedBranches[commit.PushedBranch] = struct{}{}
					ui.mu.Unlock()
				} else {
					ui.AppendSystemMessage("🔄 new commit: [%s] %s", commit.Hash[:8], commit.Subject)
				}
			}
		default:
			ui.AppendSystemMessage("❌ Unexpected Message Type %s %v", resp.Type, resp)
		}
	}
}

func (ui *termUI) inputLoop(ctx context.Context) error {
	for {
		line, err := ui.trm.ReadLine()
		if errors.Is(err, io.EOF) {
			ui.AppendSystemMessage("\n")
			line = "exit"
		} else if err != nil {
			return err
		}

		line = strings.TrimSpace(line)

		switch line {
		case "?", "help":
			ui.AppendSystemMessage(`Available commands:
- help, ?         : Show this help message
- budget          : Show original budget
- usage, cost     : Show current token usage and cost
- stop, cancel, abort : Cancel the current operation
- exit, quit, q   : Exit the application
- ! <command>     : Execute shell command (e.g. !ls -la)`)
		case "budget":
			originalBudget := ui.agent.OriginalBudget()
			ui.AppendSystemMessage("💰 Budget summary:")
			if originalBudget.MaxResponses > 0 {
				ui.AppendSystemMessage("- Max responses: %d", originalBudget.MaxResponses)
			}
			if originalBudget.MaxWallTime > 0 {
				ui.AppendSystemMessage("- Max wall time: %v", originalBudget.MaxWallTime)
			}
			ui.AppendSystemMessage("- Max total cost: %0.2f", originalBudget.MaxDollars)
		case "usage", "cost":
			totalUsage := ui.agent.TotalUsage()
			ui.AppendSystemMessage("💰 Current usage summary:")
			ui.AppendSystemMessage("- Input tokens: %d [Cache: read=%d, creation=%d]", totalUsage.InputTokens, totalUsage.CacheReadInputTokens, totalUsage.CacheCreationInputTokens)
			ui.AppendSystemMessage("- Output tokens: %d", totalUsage.OutputTokens)
			ui.AppendSystemMessage("- Responses: %d", totalUsage.Responses)
			ui.AppendSystemMessage("- Wall time: %s", totalUsage.WallTime().Round(time.Second))
			ui.AppendSystemMessage("- Total cost: $%0.2f", totalUsage.TotalCostUSD)
		case "bye", "exit", "q", "quit":
			ui.trm.SetPrompt("")
			// Display final usage stats
			totalUsage := ui.agent.TotalUsage()
			ui.AppendSystemMessage("💰 Final usage summary:")
			ui.AppendSystemMessage("- Input tokens: %d [Cache: read=%d, creation=%d]", totalUsage.InputTokens, totalUsage.CacheReadInputTokens, totalUsage.CacheCreationInputTokens)
			ui.AppendSystemMessage("- Output tokens: %d", totalUsage.OutputTokens)
			ui.AppendSystemMessage("- Responses: %d", totalUsage.Responses)
			ui.AppendSystemMessage("- Wall time: %s", totalUsage.WallTime().Round(time.Second))
			ui.AppendSystemMessage("- Total cost: $%0.2f", totalUsage.TotalCostUSD)

			// Display pushed branches
			ui.mu.Lock()
			if len(ui.pushedBranches) > 0 {
				// Convert map keys to a slice for display
				branches := make([]string, 0, len(ui.pushedBranches))
				for branch := range ui.pushedBranches {
					branches = append(branches, branch)
				}

				if len(branches) == 1 {
					ui.AppendSystemMessage("\n🔄 Branch pushed during session: %s", branches[0])
					ui.AppendSystemMessage("🍒 To add those changes to your branch: git cherry-pick %s..%s", ui.agent.InitialCommit(), branches[0])
				} else {
					ui.AppendSystemMessage("\n🔄 Branches pushed during session:")
					for _, branch := range branches {
						ui.AppendSystemMessage("- %s", branch)
					}
					ui.AppendSystemMessage("\n🍒 To add all those changes to your branch:")
					for _, branch := range branches {
						ui.AppendSystemMessage("git cherry-pick %s..%s", ui.agent.InitialCommit(), branch)
					}
				}
			}
			ui.mu.Unlock()

			ui.AppendSystemMessage("\n👋 Goodbye!")
			return nil
		case "stop", "cancel", "abort":
			ui.agent.CancelInnerLoop(fmt.Errorf("user canceled the operation"))
		case "panic":
			panic("user forced a panic")
		default:
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "!") {
				// Execute as shell command
				line = line[1:] // remove the '!' prefix
				sendToLLM := strings.HasPrefix(line, "!")
				if sendToLLM {
					line = line[1:] // remove the second '!'
				}

				// Create a cmd and run it
				// TODO: ui.trm contains a mutex inside its write call.
				// It is potentially safe to attach ui.trm directly to this
				// cmd object's Stdout/Stderr and stream the output.
				// That would make a big difference for, e.g. wget.
				cmd := exec.Command("bash", "-c", line)
				out, err := cmd.CombinedOutput()
				ui.AppendSystemMessage("%s", out)
				if err != nil {
					ui.AppendSystemMessage("❌ Command error: %v", err)
				}
				if sendToLLM {
					// Send the command and its output to the agent
					message := fmt.Sprintf("I ran the command: `%s`\nOutput:\n```\n%s```", line, out)
					if err != nil {
						message += fmt.Sprintf("\n\nError: %v", err)
					}
					ui.agent.UserMessage(ctx, message)
				}
				continue
			}

			// Send it to the LLM
			// chatMsg := chatMessage{sender: "you", content: line}
			// ui.sendChatMessage(chatMsg)
			ui.agent.UserMessage(ctx, line)
		}
	}
}

func (ui *termUI) updatePrompt(thinking bool) {
	var t string

	if thinking {
		// Emoji don't seem to work here? Messes up my terminal.
		t = "*"
	}
	p := fmt.Sprintf("%s/ %s($%0.2f/%0.2f)%s> ",
		ui.httpURL, ui.agent.WorkingDir(), ui.agent.TotalUsage().TotalCostUSD, ui.agent.OriginalBudget().MaxDollars, t)
	ui.trm.SetPrompt(p)
}

func (ui *termUI) initializeTerminalUI(ctx context.Context) error {
	ui.mu.Lock()
	defer ui.mu.Unlock()

	if !term.IsTerminal(int(ui.stdin.Fd())) {
		return fmt.Errorf("this command requires terminal I/O")
	}

	oldState, err := term.MakeRaw(int(ui.stdin.Fd()))
	if err != nil {
		return err
	}
	ui.oldState = oldState
	ui.trm = term.NewTerminal(ui.stdin, "")
	width, height, err := term.GetSize(int(ui.stdin.Fd()))
	if err != nil {
		return fmt.Errorf("Error getting terminal size: %v\n", err)
	}
	ui.trm.SetSize(width, height)
	// Handle terminal resizes...
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		for {
			<-sig
			newWidth, newHeight, err := term.GetSize(int(ui.stdin.Fd()))
			if err != nil {
				continue
			}
			if newWidth != width || newHeight != height {
				width, height = newWidth, newHeight
				ui.trm.SetSize(width, height)
			}
		}
	}()

	ui.updatePrompt(false)

	// This is the only place where we should call fe.trm.Write:
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-ui.chatMsgCh:
				// Sometimes claude doesn't say anything when it runs tools.
				// No need to output anything in that case.
				if strings.TrimSpace(msg.content) == "" {
					break
				}
				s := fmt.Sprintf("%s  %s\n", msg.sender, msg.content)
				// Update prompt before writing, because otherwise it doesn't redraw the prompt.
				ui.updatePrompt(msg.thinking)
				ui.trm.Write([]byte(s))
			case logLine := <-ui.termLogCh:
				b := []byte(logLine + "\n")
				ui.trm.Write(b)
			}
		}
	}()

	return nil
}

func (ui *termUI) RestoreOldState() error {
	ui.mu.Lock()
	defer ui.mu.Unlock()
	return term.Restore(int(ui.stdin.Fd()), ui.oldState)
}

// AppendChatMessage is for showing responses the user's request, conversational dialog etc
func (ui *termUI) AppendChatMessage(msg chatMessage) {
	ui.chatMsgCh <- msg
}

// AppendSystemMessage is for debug information, errors and such that are not part of the "conversation" per se,
// but still need to be shown to the user.
func (ui *termUI) AppendSystemMessage(fmtString string, args ...any) {
	ui.termLogCh <- fmt.Sprintf(fmtString, args...)
}
