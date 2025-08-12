package bubbletea

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/dustin/go-humanize"
	"sketch.dev/loop"
)

// CommandProcessor handles special command processing
type CommandProcessor struct {
	agent   loop.CodingAgent
	httpURL string
	ctx     context.Context
}

// NewCommandProcessor creates a new command processor
func NewCommandProcessor(agent loop.CodingAgent, httpURL string, ctx context.Context) *CommandProcessor {
	return &CommandProcessor{
		agent:   agent,
		httpURL: httpURL,
		ctx:     ctx,
	}
}

// ProcessCommand processes a special command and returns appropriate tea.Cmd
func (cp *CommandProcessor) ProcessCommand(input string) (bool, tea.Cmd) {
	input = strings.TrimSpace(input)

	// Check if this is a special command
	switch input {
	case "?", "help":
		return true, cp.handleHelpCommand()
	case "budget":
		return true, cp.handleBudgetCommand()
	case "usage", "cost":
		return true, cp.handleUsageCommand()
	case "browser", "open", "b":
		return true, cp.handleBrowserCommand()
	case "stop", "cancel", "abort":
		return true, cp.handleStopCommand()
	case "bye", "exit", "q", "quit":
		return true, cp.handleExitCommand()
	case "panic":
		return true, cp.handlePanicCommand()
	default:
		// Check for shell commands
		if strings.HasPrefix(input, "!") {
			return true, cp.handleShellCommand(input)
		}

		// Not a special command
		return false, nil
	}
}

// handleHelpCommand shows the help message
func (cp *CommandProcessor) handleHelpCommand() tea.Cmd {
	return func() tea.Msg {
		content := `General use:
Use chat to ask sketch to tackle a task or answer a question about this repo.

Special commands:
- help, ?             : Show this help message
- budget              : Show original budget
- usage, cost         : Show current token usage and cost
- browser, open, b    : Open current conversation in browser
- stop, cancel, abort : Cancel the current operation
- exit, quit, q       : Exit sketch
- ! <command>         : Execute a shell command (e.g. !ls -la)`

		return systemMessageMsg{content: content}
	}
}

// handleBudgetCommand shows the budget information
func (cp *CommandProcessor) handleBudgetCommand() tea.Cmd {
	return func() tea.Msg {
		if cp.agent == nil {
			return systemMessageMsg{content: "[ERR] No agent available"}
		}

		originalBudget := cp.agent.OriginalBudget()
		content := fmt.Sprintf("[BUD] Budget summary:\n- Max total cost: $%.2f", originalBudget.MaxDollars)

		return systemMessageMsg{content: content}
	}
}

// handleUsageCommand shows current usage statistics
func (cp *CommandProcessor) handleUsageCommand() tea.Cmd {
	return func() tea.Msg {
		if cp.agent == nil {
			return systemMessageMsg{content: "[ERR] No agent available"}
		}

		totalUsage := cp.agent.TotalUsage()
		content := fmt.Sprintf(`[USD] Current usage summary:
- Input tokens: %s
- Output tokens: %s
- Responses: %d
- Wall time: %s
- Total cost: $%.2f`,
			humanize.Comma(int64(totalUsage.TotalInputTokens())),
			humanize.Comma(int64(totalUsage.OutputTokens)),
			totalUsage.Responses,
			totalUsage.WallTime().Round(time.Second),
			totalUsage.TotalCostUSD)

		return systemMessageMsg{content: content}
	}
}

// handleBrowserCommand opens the browser
func (cp *CommandProcessor) handleBrowserCommand() tea.Cmd {
	return func() tea.Msg {
		if cp.httpURL != "" {
			content := fmt.Sprintf("🌐 Opening %s in browser", cp.httpURL)
			if cp.agent != nil {
				go cp.agent.OpenBrowser(cp.httpURL)
			}
			return systemMessageMsg{content: content}
		} else {
			return systemMessageMsg{content: "[ERR] No web URL available for this session"}
		}
	}
}

// handleStopCommand cancels the current operation
func (cp *CommandProcessor) handleStopCommand() tea.Cmd {
	return func() tea.Msg {
		if cp.agent != nil {
			cp.agent.CancelTurn(fmt.Errorf("user canceled the operation"))
		}
		return systemMessageMsg{content: "🛑 Operation cancelled"}
	}
}

// handleExitCommand handles graceful shutdown
func (cp *CommandProcessor) handleExitCommand() tea.Cmd {
	return func() tea.Msg {
		var content strings.Builder

		if cp.agent != nil {
			// Display final usage stats
			totalUsage := cp.agent.TotalUsage()
			content.WriteString(fmt.Sprintf(`💰 Final usage summary:
- Input tokens: %s
- Output tokens: %s
- Responses: %d
- Wall time: %s
- Total cost: $%.2f`,
				humanize.Comma(int64(totalUsage.TotalInputTokens())),
				humanize.Comma(int64(totalUsage.OutputTokens)),
				totalUsage.Responses,
				totalUsage.WallTime().Round(time.Second),
				totalUsage.TotalCostUSD))
		}

		content.WriteString("\n\n👋 Goodbye!")

		// Return a quit command along with the message
		return tea.Batch(
			func() tea.Msg { return systemMessageMsg{content: content.String()} },
			tea.Quit,
		)()
	}
}

// handlePanicCommand forces a panic for debugging
func (cp *CommandProcessor) handlePanicCommand() tea.Cmd {
	return func() tea.Msg {
		panic("user forced a panic")
	}
}

// handleShellCommand executes shell commands
func (cp *CommandProcessor) handleShellCommand(input string) tea.Cmd {
	return func() tea.Msg {
		// Remove the '!' prefix
		command := input[1:]
		sendToLLM := strings.HasPrefix(command, "!")
		if sendToLLM {
			command = command[1:] // remove the second '!'
		}

		// Execute the command
		cmd := exec.Command("bash", "-c", command)
		out, err := cmd.CombinedOutput()

		var content strings.Builder
		content.WriteString(string(out))

		if err != nil {
			content.WriteString(fmt.Sprintf("\n[ERR] Command error: %v", err))
		}

		// Send to LLM if requested
		if sendToLLM && cp.agent != nil && cp.ctx != nil {
			message := fmt.Sprintf("I ran the command: `%s`\nOutput:\n```\n%s```", command, out)
			if err != nil {
				message += fmt.Sprintf("\n\nError: %v", err)
			}
			go cp.agent.UserMessage(cp.ctx, message)
		}

		return systemMessageMsg{content: content.String()}
	}
}

// UpdateContext updates the context for the command processor
func (cp *CommandProcessor) UpdateContext(ctx context.Context) {
	cp.ctx = ctx
}

// UpdateAgent updates the agent reference
func (cp *CommandProcessor) UpdateAgent(agent loop.CodingAgent) {
	cp.agent = agent
}

// UpdateHTTPURL updates the HTTP URL
func (cp *CommandProcessor) UpdateHTTPURL(httpURL string) {
	cp.httpURL = httpURL
}
