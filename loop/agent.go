package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"sketch.dev/ant"
	"sketch.dev/claudetool"
)

const (
	userCancelMessage = "user requested agent to stop handling responses"
)

type CodingAgent interface {
	// Init initializes an agent inside a docker container.
	Init(AgentInit) error

	// Ready returns a channel closed after Init successfully called.
	Ready() <-chan struct{}

	// URL reports the HTTP URL of this agent.
	URL() string

	// UserMessage enqueues a message to the agent and returns immediately.
	UserMessage(ctx context.Context, msg string)

	// WaitForMessage blocks until the agent has a response to give.
	// Use AgentMessage.EndOfTurn to help determine if you want to
	// drain the agent.
	WaitForMessage(ctx context.Context) AgentMessage

	// Loop begins the agent loop returns only when ctx is cancelled.
	Loop(ctx context.Context)

	CancelInnerLoop(cause error)

	CancelToolUse(toolUseID string, cause error) error

	// Returns a subset of the agent's message history.
	Messages(start int, end int) []AgentMessage

	// Returns the current number of messages in the history
	MessageCount() int

	TotalUsage() ant.CumulativeUsage
	OriginalBudget() ant.Budget

	// WaitForMessageCount returns when the agent has at more than clientMessageCount messages or the context is done.
	WaitForMessageCount(ctx context.Context, greaterThan int)

	WorkingDir() string

	// Diff returns a unified diff of changes made since the agent was instantiated.
	// If commit is non-nil, it shows the diff for just that specific commit.
	Diff(commit *string) (string, error)

	// InitialCommit returns the Git commit hash that was saved when the agent was instantiated.
	InitialCommit() string

	// Title returns the current title of the conversation.
	Title() string

	// OS returns the operating system of the client.
	OS() string
}

type CodingAgentMessageType string

const (
	UserMessageType    CodingAgentMessageType = "user"
	AgentMessageType   CodingAgentMessageType = "agent"
	ErrorMessageType   CodingAgentMessageType = "error"
	BudgetMessageType  CodingAgentMessageType = "budget" // dedicated for "out of budget" errors
	ToolUseMessageType CodingAgentMessageType = "tool"
	CommitMessageType  CodingAgentMessageType = "commit" // for displaying git commits
	AutoMessageType    CodingAgentMessageType = "auto"   // for automated notifications like autoformatting

	cancelToolUseMessage = "Stop responding to my previous message. Wait for me to ask you something else before attempting to use any more tools."
)

type AgentMessage struct {
	Type CodingAgentMessageType `json:"type"`
	// EndOfTurn indicates that the AI is done working and is ready for the next user input.
	EndOfTurn bool `json:"end_of_turn"`

	Content    string `json:"content"`
	ToolName   string `json:"tool_name,omitempty"`
	ToolInput  string `json:"input,omitempty"`
	ToolResult string `json:"tool_result,omitempty"`
	ToolError  bool   `json:"tool_error,omitempty"`
	ToolCallId string `json:"tool_call_id,omitempty"`

	// ToolCalls is a list of all tool calls requested in this message (name and input pairs)
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// ToolResponses is a list of all responses to tool calls requested in this message (name and input pairs)
	ToolResponses []AgentMessage `json:"toolResponses,omitempty"`

	// Commits is a list of git commits for a commit message
	Commits []*GitCommit `json:"commits,omitempty"`

	Timestamp            time.Time  `json:"timestamp"`
	ConversationID       string     `json:"conversation_id"`
	ParentConversationID *string    `json:"parent_conversation_id,omitempty"`
	Usage                *ant.Usage `json:"usage,omitempty"`

	// Message timing information
	StartTime *time.Time     `json:"start_time,omitempty"`
	EndTime   *time.Time     `json:"end_time,omitempty"`
	Elapsed   *time.Duration `json:"elapsed,omitempty"`

	// Turn duration - the time taken for a complete agent turn
	TurnDuration *time.Duration `json:"turnDuration,omitempty"`

	Idx int `json:"idx"`
}

// GitCommit represents a single git commit for a commit message
type GitCommit struct {
	Hash         string `json:"hash"`                    // Full commit hash
	Subject      string `json:"subject"`                 // Commit subject line
	Body         string `json:"body"`                    // Full commit message body
	PushedBranch string `json:"pushed_branch,omitempty"` // If set, this commit was pushed to this branch
}

// ToolCall represents a single tool call within an agent message
type ToolCall struct {
	Name          string        `json:"name"`
	Input         string        `json:"input"`
	ToolCallId    string        `json:"tool_call_id"`
	ResultMessage *AgentMessage `json:"result_message,omitempty"`
	Args          string        `json:"args,omitempty"`
	Result        string        `json:"result,omitempty"`
}

func (a *AgentMessage) Attr() slog.Attr {
	var attrs []any = []any{
		slog.String("type", string(a.Type)),
	}
	if a.EndOfTurn {
		attrs = append(attrs, slog.Bool("end_of_turn", a.EndOfTurn))
	}
	if a.Content != "" {
		attrs = append(attrs, slog.String("content", a.Content))
	}
	if a.ToolName != "" {
		attrs = append(attrs, slog.String("tool_name", a.ToolName))
	}
	if a.ToolInput != "" {
		attrs = append(attrs, slog.String("tool_input", a.ToolInput))
	}
	if a.Elapsed != nil {
		attrs = append(attrs, slog.Int64("elapsed", a.Elapsed.Nanoseconds()))
	}
	if a.TurnDuration != nil {
		attrs = append(attrs, slog.Int64("turnDuration", a.TurnDuration.Nanoseconds()))
	}
	if a.ToolResult != "" {
		attrs = append(attrs, slog.String("tool_result", a.ToolResult))
	}
	if a.ToolError {
		attrs = append(attrs, slog.Bool("tool_error", a.ToolError))
	}
	if len(a.ToolCalls) > 0 {
		toolCallAttrs := make([]any, 0, len(a.ToolCalls))
		for i, tc := range a.ToolCalls {
			toolCallAttrs = append(toolCallAttrs, slog.Group(
				fmt.Sprintf("tool_call_%d", i),
				slog.String("name", tc.Name),
				slog.String("input", tc.Input),
			))
		}
		attrs = append(attrs, slog.Group("tool_calls", toolCallAttrs...))
	}
	if a.ConversationID != "" {
		attrs = append(attrs, slog.String("convo_id", a.ConversationID))
	}
	if a.ParentConversationID != nil {
		attrs = append(attrs, slog.String("parent_convo_id", *a.ParentConversationID))
	}
	if a.Usage != nil && !a.Usage.IsZero() {
		attrs = append(attrs, a.Usage.Attr())
	}
	// TODO: timestamp, convo ids, idx?
	return slog.Group("agent_message", attrs...)
}

func errorMessage(err error) AgentMessage {
	// It's somewhat unknowable whether error messages are "end of turn" or not, but it seems like the best approach.
	if os.Getenv(("DEBUG")) == "1" {
		return AgentMessage{Type: ErrorMessageType, Content: err.Error() + " Stacktrace: " + string(debug.Stack()), EndOfTurn: true}
	}

	return AgentMessage{Type: ErrorMessageType, Content: err.Error(), EndOfTurn: true}
}

func budgetMessage(err error) AgentMessage {
	return AgentMessage{Type: BudgetMessageType, Content: err.Error(), EndOfTurn: true}
}

// ConvoInterface defines the interface for conversation interactions
type ConvoInterface interface {
	CumulativeUsage() ant.CumulativeUsage
	ResetBudget(ant.Budget)
	OverBudget() error
	SendMessage(message ant.Message) (*ant.MessageResponse, error)
	SendUserTextMessage(s string, otherContents ...ant.Content) (*ant.MessageResponse, error)
	ToolResultContents(ctx context.Context, resp *ant.MessageResponse) ([]ant.Content, error)
	ToolResultCancelContents(resp *ant.MessageResponse) ([]ant.Content, error)
	CancelToolUse(toolUseID string, cause error) error
}

type Agent struct {
	convo          ConvoInterface
	config         AgentConfig // config for this agent
	workingDir     string
	repoRoot       string // workingDir may be a subdir of repoRoot
	url            string
	lastHEAD       string        // hash of the last HEAD that was pushed to the host (only when under docker)
	initialCommit  string        // hash of the Git HEAD when the agent was instantiated or Init()
	gitRemoteAddr  string        // HTTP URL of the host git repo (only when under docker)
	ready          chan struct{} // closed when the agent is initialized (only when under docker)
	startedAt      time.Time
	originalBudget ant.Budget
	title          string
	codereview     *claudetool.CodeReviewer

	// Time when the current turn started (reset at the beginning of InnerLoop)
	startOfTurn time.Time

	// Inbox - for messages from the user to the agent.
	// sent on by UserMessage
	// . e.g. when user types into the chat textarea
	// read from by GatherMessages
	inbox chan string

	// Outbox
	// sent on by pushToOutbox
	//  via OnToolResult and OnResponse callbacks
	// read from by WaitForMessage
	// 	called by termui inside its repl loop.
	outbox chan AgentMessage

	// protects cancelInnerLoop
	cancelInnerLoopMu sync.Mutex
	// cancels potentially long-running tool_use calls or chains of them
	cancelInnerLoop context.CancelCauseFunc

	// protects following
	mu sync.Mutex

	// Stores all messages for this agent
	history []AgentMessage

	listeners []chan struct{}

	// Track git commits we've already seen (by hash)
	seenCommits map[string]bool
}

func (a *Agent) URL() string { return a.url }

// Title returns the current title of the conversation.
// If no title has been set, returns an empty string.
func (a *Agent) Title() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

// OS returns the operating system of the client.
func (a *Agent) OS() string {
	return a.config.ClientGOOS
}

// SetTitle sets the title of the conversation.
func (a *Agent) SetTitle(title string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.title = title
	// Notify all listeners that the state has changed
	for _, ch := range a.listeners {
		close(ch)
	}
	a.listeners = a.listeners[:0]
}

// OnToolResult implements ant.Listener.
func (a *Agent) OnToolResult(ctx context.Context, convo *ant.Convo, toolName string, toolInput json.RawMessage, content ant.Content, result *string, err error) {
	m := AgentMessage{
		Type:       ToolUseMessageType,
		Content:    content.Text,
		ToolResult: content.ToolResult,
		ToolError:  content.ToolError,
		ToolName:   toolName,
		ToolInput:  string(toolInput),
		ToolCallId: content.ToolUseID,
		StartTime:  content.StartTime,
		EndTime:    content.EndTime,
	}

	// Calculate the elapsed time if both start and end times are set
	if content.StartTime != nil && content.EndTime != nil {
		elapsed := content.EndTime.Sub(*content.StartTime)
		m.Elapsed = &elapsed
	}

	m.ConversationID = convo.ID
	if convo.Parent != nil {
		m.ParentConversationID = &convo.Parent.ID
	}
	a.pushToOutbox(ctx, m)
}

// OnRequest implements ant.Listener.
func (a *Agent) OnRequest(ctx context.Context, convo *ant.Convo, msg *ant.Message) {
	// No-op.
	// We already get tool results from the above. We send user messages to the outbox in the agent loop.
}

// OnResponse implements ant.Listener. Responses contain messages from the LLM
// that need to be displayed (as well as tool calls that we send along when
// they're done). (It would be reasonable to also mention tool calls when they're
// started, but we don't do that yet.)
func (a *Agent) OnResponse(ctx context.Context, convo *ant.Convo, resp *ant.MessageResponse) {
	endOfTurn := false
	if resp.StopReason != ant.StopReasonToolUse {
		endOfTurn = true
	}
	m := AgentMessage{
		Type:      AgentMessageType,
		Content:   collectTextContent(resp),
		EndOfTurn: endOfTurn,
		Usage:     &resp.Usage,
		StartTime: resp.StartTime,
		EndTime:   resp.EndTime,
	}

	// Extract any tool calls from the response
	if resp.StopReason == ant.StopReasonToolUse {
		var toolCalls []ToolCall
		for _, part := range resp.Content {
			if part.Type == "tool_use" {
				toolCalls = append(toolCalls, ToolCall{
					Name:       part.ToolName,
					Input:      string(part.ToolInput),
					ToolCallId: part.ID,
				})
			}
		}
		m.ToolCalls = toolCalls
	}

	// Calculate the elapsed time if both start and end times are set
	if resp.StartTime != nil && resp.EndTime != nil {
		elapsed := resp.EndTime.Sub(*resp.StartTime)
		m.Elapsed = &elapsed
	}

	m.ConversationID = convo.ID
	if convo.Parent != nil {
		m.ParentConversationID = &convo.Parent.ID
	}
	a.pushToOutbox(ctx, m)
}

// WorkingDir implements CodingAgent.
func (a *Agent) WorkingDir() string {
	return a.workingDir
}

// MessageCount implements CodingAgent.
func (a *Agent) MessageCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.history)
}

// Messages implements CodingAgent.
func (a *Agent) Messages(start int, end int) []AgentMessage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.history[start:end])
}

func (a *Agent) OriginalBudget() ant.Budget {
	return a.originalBudget
}

// AgentConfig contains configuration for creating a new Agent.
type AgentConfig struct {
	Context          context.Context
	AntURL           string
	APIKey           string
	HTTPC            *http.Client
	Budget           ant.Budget
	GitUsername      string
	GitEmail         string
	SessionID        string
	ClientGOOS       string
	ClientGOARCH     string
	UseAnthropicEdit bool
}

// NewAgent creates a new Agent.
// It is not usable until Init() is called.
func NewAgent(config AgentConfig) *Agent {
	agent := &Agent{
		config:         config,
		ready:          make(chan struct{}),
		inbox:          make(chan string, 100),
		outbox:         make(chan AgentMessage, 100),
		startedAt:      time.Now(),
		originalBudget: config.Budget,
		seenCommits:    make(map[string]bool),
	}
	return agent
}

type AgentInit struct {
	WorkingDir string
	NoGit      bool // only for testing

	InDocker      bool
	Commit        string
	GitRemoteAddr string
	HostAddr      string
}

func (a *Agent) Init(ini AgentInit) error {
	ctx := a.config.Context
	if ini.InDocker {
		cmd := exec.CommandContext(ctx, "git", "stash")
		cmd.Dir = ini.WorkingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git stash: %s: %v", out, err)
		}
		cmd = exec.CommandContext(ctx, "git", "remote", "add", "sketch-host", ini.GitRemoteAddr)
		cmd.Dir = ini.WorkingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git remote add: %s: %v", out, err)
		}
		cmd = exec.CommandContext(ctx, "git", "fetch", "sketch-host")
		cmd.Dir = ini.WorkingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git fetch: %s: %w", out, err)
		}
		cmd = exec.CommandContext(ctx, "git", "checkout", "-f", ini.Commit)
		cmd.Dir = ini.WorkingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout %s: %s: %w", ini.Commit, out, err)
		}

		// Disable git hooks that might require unavailable commands (like branchless)
		hooksDir := fmt.Sprintf("%s/.git/hooks", ini.WorkingDir)
		if _, err := os.Stat(hooksDir); err == nil {
			// Rename hooks directory to disable all hooks
			backupDir := fmt.Sprintf("%s/.git/hooks.backup", ini.WorkingDir)
			if err := os.Rename(hooksDir, backupDir); err != nil {
				slog.WarnContext(ctx, "failed to rename git hooks directory",
					slog.String("error", err.Error()))
				// If we can't rename the directory, try to remove specific problematic hooks
				for _, hook := range []string{"reference-transaction", "post-checkout", "post-commit"} {
					hookPath := fmt.Sprintf("%s/%s", hooksDir, hook)
					if _, err := os.Stat(hookPath); err == nil {
						os.Remove(hookPath)
					}
				}
			} else {
				// Create an empty hooks directory
				os.Mkdir(hooksDir, 0755)
			}
		}

		a.lastHEAD = ini.Commit
		a.gitRemoteAddr = ini.GitRemoteAddr
		a.initialCommit = ini.Commit
		if ini.HostAddr != "" {
			a.url = "http://" + ini.HostAddr
		}
	}
	a.workingDir = ini.WorkingDir

	if !ini.NoGit {
		repoRoot, err := repoRoot(ctx, a.workingDir)
		if err != nil {
			return fmt.Errorf("repoRoot: %w", err)
		}
		a.repoRoot = repoRoot

		commitHash, err := resolveRef(ctx, a.repoRoot, "HEAD")
		if err != nil {
			return fmt.Errorf("resolveRef: %w", err)
		}
		a.initialCommit = commitHash

		codereview, err := claudetool.NewCodeReviewer(ctx, a.repoRoot, a.initialCommit)
		if err != nil {
			return fmt.Errorf("Agent.Init: claudetool.NewCodeReviewer: %w", err)
		}
		a.codereview = codereview
	}
	a.lastHEAD = a.initialCommit
	a.convo = a.initConvo()
	close(a.ready)
	return nil
}

// initConvo initializes the conversation.
// It must not be called until all agent fields are initialized,
// particularly workingDir and git.
func (a *Agent) initConvo() *ant.Convo {
	ctx := a.config.Context
	convo := ant.NewConvo(ctx, a.config.APIKey)
	if a.config.HTTPC != nil {
		convo.HTTPC = a.config.HTTPC
	}
	if a.config.AntURL != "" {
		convo.URL = a.config.AntURL
	}
	convo.PromptCaching = true
	convo.Budget = a.config.Budget

	var editPrompt string
	if a.config.UseAnthropicEdit {
		editPrompt = "Then use the str_replace_editor tool to make those edits. For short complete file replacements, you may use the bash tool with cat and heredoc stdin."
	} else {
		editPrompt = "Then use the patch tool to make those edits. Combine all edits to any given file into a single patch tool call."
	}

	convo.SystemPrompt = fmt.Sprintf(`
You are an expert coding assistant and architect, with a specialty in Go.
You are assisting the user to achieve their goals.

Start by asking concise clarifying questions as needed.
Once the intent is clear, work autonomously.

Call the title tool early in the conversation to provide a brief summary of
what the chat is about.

Break down the overall goal into a series of smaller steps.
(The first step is often: "Make a plan.")
Then execute each step using tools.
Update the plan if you have encountered problems or learned new information.

When in doubt about a step, follow this broad workflow:

- Think about how the current step fits into the overall plan.
- Do research. Good tool choices: bash, think, keyword_search
- Make edits.
- Repeat.

To make edits reliably and efficiently, first think about the intent of the edit,
and what set of patches will achieve that intent.
%s

For renames or refactors, consider invoking gopls (via bash).

The done tool provides a checklist of items you MUST verify and
review before declaring that you are done. Before executing
the done tool, run all the tools the done tool checklist asks
for, including creating a git commit. Do not forget to run tests.

<platform>
%s/%s
</platform>
<pwd>
%v
</pwd>
<git_root>
%v
</git_root>
`, editPrompt, a.config.ClientGOOS, a.config.ClientGOARCH, a.workingDir, a.repoRoot)

	// Register all tools with the conversation
	// When adding, removing, or modifying tools here, double-check that the termui tool display
	// template in termui/termui.go has pretty-printing support for all tools.
	convo.Tools = []*ant.Tool{
		claudetool.Bash, claudetool.Keyword,
		claudetool.Think, a.titleTool(), makeDoneTool(a.codereview, a.config.GitUsername, a.config.GitEmail),
		a.codereview.Tool(),
	}
	if a.config.UseAnthropicEdit {
		convo.Tools = append(convo.Tools, claudetool.AnthropicEditTool)
	} else {
		convo.Tools = append(convo.Tools, claudetool.Patch)
	}
	convo.Listener = a
	return convo
}

func (a *Agent) titleTool() *ant.Tool {
	// titleTool creates the title tool that sets the conversation title.
	title := &ant.Tool{
		Name:        "title",
		Description: `Use this tool early in the conversation, BEFORE MAKING ANY GIT COMMITS, to summarize what the chat is about briefly.`,
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"title": {
			"type": "string",
			"description": "A brief title summarizing what this chat is about"
		}
	},
	"required": ["title"]
}`),
		Run: func(ctx context.Context, input json.RawMessage) (string, error) {
			var params struct {
				Title string `json:"title"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return "", err
			}
			a.SetTitle(params.Title)
			return fmt.Sprintf("Title set to: %s", params.Title), nil
		},
	}
	return title
}

func (a *Agent) Ready() <-chan struct{} {
	return a.ready
}

func (a *Agent) UserMessage(ctx context.Context, msg string) {
	a.pushToOutbox(ctx, AgentMessage{Type: UserMessageType, Content: msg})
	a.inbox <- msg
}

func (a *Agent) WaitForMessage(ctx context.Context) AgentMessage {
	// TODO: Should this drain any outbox messages in case there are multiple?
	select {
	case msg := <-a.outbox:
		return msg
	case <-ctx.Done():
		return errorMessage(ctx.Err())
	}
}

func (a *Agent) CancelToolUse(toolUseID string, cause error) error {
	return a.convo.CancelToolUse(toolUseID, cause)
}

func (a *Agent) CancelInnerLoop(cause error) {
	a.cancelInnerLoopMu.Lock()
	defer a.cancelInnerLoopMu.Unlock()
	if a.cancelInnerLoop != nil {
		a.cancelInnerLoop(cause)
	}
}

func (a *Agent) Loop(ctxOuter context.Context) {
	for {
		select {
		case <-ctxOuter.Done():
			return
		default:
			ctxInner, cancel := context.WithCancelCause(ctxOuter)
			a.cancelInnerLoopMu.Lock()
			// Set .cancelInnerLoop so the user can cancel whatever is happening
			// inside InnerLoop(ctxInner) without canceling this outer Loop execution.
			// This CancelInnerLoop func is intended be called from other goroutines,
			// hence the mutex.
			a.cancelInnerLoop = cancel
			a.cancelInnerLoopMu.Unlock()
			a.InnerLoop(ctxInner)
			cancel(nil)
		}
	}
}

func (a *Agent) pushToOutbox(ctx context.Context, m AgentMessage) {
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}

	// If this is an end-of-turn message, calculate the turn duration and add it to the message
	if m.EndOfTurn && m.Type == AgentMessageType {
		turnDuration := time.Since(a.startOfTurn)
		m.TurnDuration = &turnDuration
		slog.InfoContext(ctx, "Turn completed", "turnDuration", turnDuration)
	}

	slog.InfoContext(ctx, "agent message", m.Attr())

	a.mu.Lock()
	defer a.mu.Unlock()
	m.Idx = len(a.history)
	a.history = append(a.history, m)
	a.outbox <- m

	// Notify all listeners:
	for _, ch := range a.listeners {
		close(ch)
	}
	a.listeners = a.listeners[:0]
}

func (a *Agent) GatherMessages(ctx context.Context, block bool) ([]ant.Content, error) {
	var m []ant.Content
	if block {
		select {
		case <-ctx.Done():
			return m, ctx.Err()
		case msg := <-a.inbox:
			m = append(m, ant.Content{Type: "text", Text: msg})
		}
	}
	for {
		select {
		case msg := <-a.inbox:
			m = append(m, ant.Content{Type: "text", Text: msg})
		default:
			return m, nil
		}
	}
}

func (a *Agent) InnerLoop(ctx context.Context) {
	// Reset the start of turn time
	a.startOfTurn = time.Now()

	// Wait for at least one message from the user.
	msgs, err := a.GatherMessages(ctx, true)
	if err != nil { // e.g. the context was canceled while blocking in GatherMessages
		return
	}
	// We do this as we go, but let's also do it at the end of the turn
	defer func() {
		if _, err := a.handleGitCommits(ctx); err != nil {
			// Just log the error, don't stop execution
			slog.WarnContext(ctx, "Failed to check for new git commits", "error", err)
		}
	}()

	userMessage := ant.Message{
		Role:    "user",
		Content: msgs,
	}
	// convo.SendMessage does the actual network call to send this to anthropic. This blocks until the response is ready.
	// TODO: pass ctx to SendMessage, and figure out how to square that ctx with convo's own .Ctx.  Who owns the scope of this call?
	resp, err := a.convo.SendMessage(userMessage)
	if err != nil {
		a.pushToOutbox(ctx, errorMessage(err))
		return
	}
	for {
		// TODO: here and below where we check the budget,
		// we should review the UX: is it clear what happened?
		// is it clear how to resume?
		// should we let the user set a new budget?
		if err := a.overBudget(ctx); err != nil {
			return
		}
		if resp.StopReason != ant.StopReasonToolUse {
			break
		}
		var results []ant.Content
		cancelled := false
		select {
		case <-ctx.Done():
			// Don't actually run any of the tools, but rather build a response
			// for each tool_use message letting the LLM know that user canceled it.
			results, err = a.convo.ToolResultCancelContents(resp)
			if err != nil {
				a.pushToOutbox(ctx, errorMessage(err))
			}
			cancelled = true
		default:
			ctx = claudetool.WithWorkingDir(ctx, a.workingDir)
			// fall-through, when the user has not canceled the inner loop:
			results, err = a.convo.ToolResultContents(ctx, resp)
			if ctx.Err() != nil { // e.g. the user canceled the operation
				cancelled = true
			} else if err != nil {
				a.pushToOutbox(ctx, errorMessage(err))
			}
		}

		// Check for git commits. Currently we do this here, after we collect
		// tool results, since that's when we know commits could have happened.
		// We could instead do this when the turn ends, but I think it makes sense
		// to do this as we go.
		newCommits, err := a.handleGitCommits(ctx)
		if err != nil {
			// Just log the error, don't stop execution
			slog.WarnContext(ctx, "Failed to check for new git commits", "error", err)
		}
		var autoqualityMessages []string
		if len(newCommits) == 1 {
			formatted := a.codereview.Autoformat(ctx)
			if len(formatted) > 0 {
				msg := fmt.Sprintf(`
I ran autoformatters and they updated these files:

%s

Please amend your latest git commit with these changes and then continue with what you were doing.`,
					strings.Join(formatted, "\n"),
				)[1:]
				a.pushToOutbox(ctx, AgentMessage{
					Type:      AutoMessageType,
					Content:   msg,
					Timestamp: time.Now(),
				})
				autoqualityMessages = append(autoqualityMessages, msg)
			}
		}

		if err := a.overBudget(ctx); err != nil {
			return
		}

		// Include, along with the tool results (which must go first for whatever reason),
		// any messages that the user has sent along while the tool_use was executing concurrently.
		msgs, err = a.GatherMessages(ctx, false)
		if err != nil {
			return
		}
		// Inject any auto-generated messages from quality checks.
		for _, msg := range autoqualityMessages {
			msgs = append(msgs, ant.Content{Type: "text", Text: msg})
		}
		if cancelled {
			msgs = append(msgs, ant.Content{Type: "text", Text: cancelToolUseMessage})
			// EndOfTurn is false here so that the client of this agent keeps processing
			// messages from WaitForMessage() and gets the response from the LLM (usually
			// something like "okay, I'll wait further instructions", but the user should
			// be made aware of it regardless).
			a.pushToOutbox(ctx, AgentMessage{Type: ErrorMessageType, Content: userCancelMessage, EndOfTurn: false})
		} else if err := a.convo.OverBudget(); err != nil {
			budgetMsg := "We've exceeded our budget. Please ask the user to confirm before continuing by ending the turn."
			msgs = append(msgs, ant.Content{Type: "text", Text: budgetMsg})
			a.pushToOutbox(ctx, budgetMessage(fmt.Errorf("warning: %w (ask to keep trying, if you'd like)", err)))
		}
		results = append(results, msgs...)
		resp, err = a.convo.SendMessage(ant.Message{
			Role:    "user",
			Content: results,
		})
		if err != nil {
			a.pushToOutbox(ctx, errorMessage(fmt.Errorf("error: failed to continue conversation: %s", err.Error())))
			break
		}
		if cancelled {
			return
		}
	}
}

func (a *Agent) overBudget(ctx context.Context) error {
	if err := a.convo.OverBudget(); err != nil {
		m := budgetMessage(err)
		m.Content = m.Content + "\n\nBudget reset."
		a.pushToOutbox(ctx, budgetMessage(err))
		a.convo.ResetBudget(a.originalBudget)
		return err
	}
	return nil
}

func collectTextContent(msg *ant.MessageResponse) string {
	// Collect all text content
	var allText strings.Builder
	for _, content := range msg.Content {
		if content.Type == "text" && content.Text != "" {
			if allText.Len() > 0 {
				allText.WriteString("\n\n")
			}
			allText.WriteString(content.Text)
		}
	}
	return allText.String()
}

func (a *Agent) TotalUsage() ant.CumulativeUsage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.convo.CumulativeUsage()
}

// WaitForMessageCount returns when the agent has at more than clientMessageCount messages or the context is done.
func (a *Agent) WaitForMessageCount(ctx context.Context, greaterThan int) {
	for a.MessageCount() <= greaterThan {
		a.mu.Lock()
		ch := make(chan struct{})
		// Deletion happens when we notify.
		a.listeners = append(a.listeners, ch)
		a.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-ch:
			continue
		}
	}
}

// Diff returns a unified diff of changes made since the agent was instantiated.
func (a *Agent) Diff(commit *string) (string, error) {
	if a.initialCommit == "" {
		return "", fmt.Errorf("no initial commit reference available")
	}

	// Find the repository root
	ctx := context.Background()

	// If a specific commit hash is provided, show just that commit's changes
	if commit != nil && *commit != "" {
		// Validate that the commit looks like a valid git SHA
		if !isValidGitSHA(*commit) {
			return "", fmt.Errorf("invalid git commit SHA format: %s", *commit)
		}

		// Get the diff for just this commit
		cmd := exec.CommandContext(ctx, "git", "show", "--unified=10", *commit)
		cmd.Dir = a.repoRoot
		output, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to get diff for commit %s: %w - %s", *commit, err, string(output))
		}
		return string(output), nil
	}

	// Otherwise, get the diff between the initial commit and the current state using exec.Command
	cmd := exec.CommandContext(ctx, "git", "diff", "--unified=10", a.initialCommit)
	cmd.Dir = a.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w - %s", err, string(output))
	}

	return string(output), nil
}

// InitialCommit returns the Git commit hash that was saved when the agent was instantiated.
func (a *Agent) InitialCommit() string {
	return a.initialCommit
}

// handleGitCommits() highlights new commits to the user. When running
// under docker, new HEADs are pushed to a branch according to the title.
func (a *Agent) handleGitCommits(ctx context.Context) ([]*GitCommit, error) {
	if a.repoRoot == "" {
		return nil, nil
	}

	head, err := resolveRef(ctx, a.repoRoot, "HEAD")
	if err != nil {
		return nil, err
	}
	if head == a.lastHEAD {
		return nil, nil // nothing to do
	}
	defer func() {
		a.lastHEAD = head
	}()

	// Get new commits. Because it's possible that the agent does rebases, fixups, and
	// so forth, we use, as our fixed point, the "initialCommit", and we limit ourselves
	// to the last 100 commits.
	var commits []*GitCommit

	// Get commits since the initial commit
	// Format: <hash>\0<subject>\0<body>\0
	// This uses NULL bytes as separators to avoid issues with newlines in commit messages
	// Limit to 100 commits to avoid overwhelming the user
	cmd := exec.CommandContext(ctx, "git", "log", "-n", "100", "--pretty=format:%H%x00%s%x00%b%x00", "^"+a.initialCommit, head)
	cmd.Dir = a.repoRoot
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get git log: %w", err)
	}

	// Parse git log output and filter out already seen commits
	parsedCommits := parseGitLog(string(output))

	var headCommit *GitCommit

	// Filter out commits we've already seen
	for _, commit := range parsedCommits {
		if commit.Hash == head {
			headCommit = &commit
		}

		// Skip if we've seen this commit before. If our head has changed, always include that.
		if a.seenCommits[commit.Hash] && commit.Hash != head {
			continue
		}

		// Mark this commit as seen
		a.seenCommits[commit.Hash] = true

		// Add to our list of new commits
		commits = append(commits, &commit)
	}

	if a.gitRemoteAddr != "" {
		if headCommit == nil {
			// I think this can only happen if we have a bug or if there's a race.
			headCommit = &GitCommit{}
			headCommit.Hash = head
			headCommit.Subject = "unknown"
			commits = append(commits, headCommit)
		}

		cleanTitle := titleToBranch(a.title)
		if cleanTitle == "" {
			cleanTitle = a.config.SessionID
		}
		branch := "sketch/" + cleanTitle

		// TODO: I don't love the force push here. We could see if the push is a fast-forward, and,
		// if it's not, we could make a backup with a unique name (perhaps append a timestamp) and
		// then use push with lease to replace.
		cmd = exec.Command("git", "push", "--force", a.gitRemoteAddr, "HEAD:refs/heads/"+branch)
		cmd.Dir = a.workingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			a.pushToOutbox(ctx, errorMessage(fmt.Errorf("git push to host: %s: %v", out, err)))
		} else {
			headCommit.PushedBranch = branch
		}
	}

	// If we found new commits, create a message
	if len(commits) > 0 {
		msg := AgentMessage{
			Type:      CommitMessageType,
			Timestamp: time.Now(),
			Commits:   commits,
		}
		a.pushToOutbox(ctx, msg)
	}
	return commits, nil
}

func titleToBranch(s string) string {
	// Convert to lowercase
	s = strings.ToLower(s)

	// Replace spaces with hyphens
	s = strings.ReplaceAll(s, " ", "-")

	// Remove any character that isn't a-z or hyphen
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// parseGitLog parses the output of git log with format '%H%x00%s%x00%b%x00'
// and returns an array of GitCommit structs.
func parseGitLog(output string) []GitCommit {
	var commits []GitCommit

	// No output means no commits
	if len(output) == 0 {
		return commits
	}

	// Split by NULL byte
	parts := strings.Split(output, "\x00")

	// Process in triplets (hash, subject, body)
	for i := 0; i < len(parts); i++ {
		// Skip empty parts
		if parts[i] == "" {
			continue
		}

		// This should be a hash
		hash := strings.TrimSpace(parts[i])

		// Make sure we have at least a subject part available
		if i+1 >= len(parts) {
			break // No more parts available
		}

		// Get the subject
		subject := strings.TrimSpace(parts[i+1])

		// Get the body if available
		body := ""
		if i+2 < len(parts) {
			body = strings.TrimSpace(parts[i+2])
		}

		// Skip to the next triplet
		i += 2

		commits = append(commits, GitCommit{
			Hash:    hash,
			Subject: subject,
			Body:    body,
		})
	}

	return commits
}

func repoRoot(ctx context.Context, dir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	stderr := new(strings.Builder)
	cmd.Stderr = stderr
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\n%s", err, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

func resolveRef(ctx context.Context, dir, refName string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", refName)
	stderr := new(strings.Builder)
	cmd.Stderr = stderr
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse failed: %w\n%s", err, stderr)
	}
	// TODO: validate that out is valid hex
	return strings.TrimSpace(string(out)), nil
}

// isValidGitSHA validates if a string looks like a valid git SHA hash.
// Git SHAs are hexadecimal strings of at least 4 characters but typically 7, 8, or 40 characters.
func isValidGitSHA(sha string) bool {
	// Git SHA must be a hexadecimal string with at least 4 characters
	if len(sha) < 4 || len(sha) > 40 {
		return false
	}

	// Check if the string only contains hexadecimal characters
	for _, char := range sha {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
			return false
		}
	}

	return true
}
