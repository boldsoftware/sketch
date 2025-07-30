package loop

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"sketch.dev/browser"
	"sketch.dev/claudetool"
	"sketch.dev/claudetool/bashkit"
	"sketch.dev/claudetool/browse"
	"sketch.dev/claudetool/codereview"
	"sketch.dev/claudetool/onstart"
	"sketch.dev/llm"
	"sketch.dev/llm/ant"
	"sketch.dev/llm/conversation"
	"sketch.dev/mcp"
	"sketch.dev/skabandclient"
	"tailscale.com/portlist"
)

const (
	userCancelMessage = "user requested agent to stop handling responses"
)

type MessageIterator interface {
	// Next blocks until the next message is available. It may
	// return nil if the underlying iterator context is done.
	Next() *AgentMessage
	Close()
}

type CodingAgent interface {
	// Init initializes an agent inside a docker container.
	Init(AgentInit) error

	// Ready returns a channel closed after Init successfully called.
	Ready() <-chan struct{}

	// URL reports the HTTP URL of this agent.
	URL() string

	// UserMessage enqueues a message to the agent and returns immediately.
	UserMessage(ctx context.Context, msg string)

	// Returns an iterator that finishes when the context is done and
	// starts with the given message index.
	NewIterator(ctx context.Context, nextMessageIdx int) MessageIterator

	// Returns an iterator that notifies of state transitions until the context is done.
	NewStateTransitionIterator(ctx context.Context) StateTransitionIterator

	// Loop begins the agent loop returns only when ctx is cancelled.
	Loop(ctx context.Context)

	// BranchPrefix returns the configured branch prefix
	BranchPrefix() string

	// LinkToGitHub returns whether GitHub branch linking is enabled
	LinkToGitHub() bool

	CancelTurn(cause error)

	CancelToolUse(toolUseID string, cause error) error

	// Returns a subset of the agent's message history.
	Messages(start int, end int) []AgentMessage

	// Returns the current number of messages in the history
	MessageCount() int

	TotalUsage() conversation.CumulativeUsage
	OriginalBudget() conversation.Budget

	WorkingDir() string
	RepoRoot() string

	// Diff returns a unified diff of changes made since the agent was instantiated.
	// If commit is non-nil, it shows the diff for just that specific commit.
	Diff(commit *string) (string, error)

	// SketchGitBase returns the commit that's the "base" for Sketch's work. It
	// starts out as the commit where sketch started, but a user can move it if need
	// be, for example in the case of a rebase. It is stored as a git tag.
	SketchGitBase() string

	// SketchGitBase returns the symbolic name for the "base" for Sketch's work.
	// (Typically, this is "sketch-base")
	SketchGitBaseRef() string

	// Slug returns the slug identifier for this session.
	Slug() string

	// BranchName returns the git branch name for the conversation.
	BranchName() string

	// IncrementRetryNumber increments the retry number for branch naming conflicts.
	IncrementRetryNumber()

	// OS returns the operating system of the client.
	OS() string

	// SessionID returns the unique session identifier.
	SessionID() string

	// SSHConnectionString returns the SSH connection string for the container.
	SSHConnectionString() string

	// DetectGitChanges checks for new git commits and pushes them if found
	DetectGitChanges(ctx context.Context) error

	// OutstandingLLMCallCount returns the number of outstanding LLM calls.
	OutstandingLLMCallCount() int

	// OutstandingToolCalls returns the names of outstanding tool calls.
	OutstandingToolCalls() []string
	OutsideOS() string
	OutsideHostname() string
	OutsideWorkingDir() string
	GitOrigin() string

	// GitUsername returns the git user name from the agent config.
	GitUsername() string

	// DiffStats returns the number of lines added and removed from sketch-base to HEAD
	DiffStats() (int, int)
	// OpenBrowser is a best-effort attempt to open a browser at url in outside sketch.
	OpenBrowser(url string)

	// IsInContainer returns true if the agent is running in a container
	IsInContainer() bool
	// FirstMessageIndex returns the index of the first message in the current conversation
	FirstMessageIndex() int

	CurrentStateName() string
	// CurrentTodoContent returns the current todo list data as JSON, or empty string if no todos exist
	CurrentTodoContent() string

	// CompactConversation compacts the current conversation by generating a summary
	// and restarting the conversation with that summary as the initial context
	CompactConversation(ctx context.Context) error

	// SkabandAddr returns the skaband address if configured
	SkabandAddr() string

	// GetPorts returns the cached list of open TCP ports
	GetPorts() []portlist.Port

	// TokenContextWindow returns the TokenContextWindow size of the model the agent is using.
	TokenContextWindow() int
}

type CodingAgentMessageType string

const (
	UserMessageType    CodingAgentMessageType = "user"
	AgentMessageType   CodingAgentMessageType = "agent"
	ErrorMessageType   CodingAgentMessageType = "error"
	BudgetMessageType  CodingAgentMessageType = "budget" // dedicated for "out of budget" errors
	ToolUseMessageType CodingAgentMessageType = "tool"
	CommitMessageType  CodingAgentMessageType = "commit"  // for displaying git commits
	AutoMessageType    CodingAgentMessageType = "auto"    // for automated notifications like autoformatting
	CompactMessageType CodingAgentMessageType = "compact" // for conversation compaction notifications
	PortMessageType    CodingAgentMessageType = "port"    // for port monitoring events

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
	Usage                *llm.Usage `json:"usage,omitempty"`

	// Message timing information
	StartTime *time.Time     `json:"start_time,omitempty"`
	EndTime   *time.Time     `json:"end_time,omitempty"`
	Elapsed   *time.Duration `json:"elapsed,omitempty"`

	// Turn duration - the time taken for a complete agent turn
	TurnDuration *time.Duration `json:"turnDuration,omitempty"`

	// HideOutput indicates that this message should not be rendered in the UI.
	// This is useful for subconversations that generate output that shouldn't be shown to the user.
	HideOutput bool `json:"hide_output,omitempty"`

	// TodoContent contains the agent's todo file content when it has changed
	TodoContent *string `json:"todo_content,omitempty"`

	Idx int `json:"idx"`
}

// SetConvo sets m.ConversationID, m.ParentConversationID, and m.HideOutput based on convo.
func (m *AgentMessage) SetConvo(convo *conversation.Convo) {
	if convo == nil {
		m.ConversationID = ""
		m.ParentConversationID = nil
		return
	}
	m.ConversationID = convo.ID
	m.HideOutput = convo.Hidden
	if convo.Parent != nil {
		m.ParentConversationID = &convo.Parent.ID
	}
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
	attrs = append(attrs, slog.Int("idx", a.Idx))
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
	if len(a.ToolResult) > 0 {
		attrs = append(attrs, slog.Any("tool_result", a.ToolResult))
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
	CumulativeUsage() conversation.CumulativeUsage
	LastUsage() llm.Usage
	ResetBudget(conversation.Budget)
	OverBudget() error
	SendMessage(message llm.Message) (*llm.Response, error)
	SendUserTextMessage(s string, otherContents ...llm.Content) (*llm.Response, error)
	GetID() string
	ToolResultContents(ctx context.Context, resp *llm.Response) ([]llm.Content, bool, error)
	ToolResultCancelContents(resp *llm.Response) ([]llm.Content, error)
	CancelToolUse(toolUseID string, cause error) error
	SubConvoWithHistory() *conversation.Convo
	DebugJSON() ([]byte, error)
}

// AgentGitState holds the state necessary for pushing to a remote git repo
// when sketch branch changes. If gitRemoteAddr is set, then we push to sketch/
// any time we notice we need to.
type AgentGitState struct {
	mu            sync.Mutex      // protects following
	lastSketch    string          // hash of the last sketch branch that was pushed to the host
	gitRemoteAddr string          // HTTP URL of the host git repo
	upstream      string          // upstream branch for git work
	seenCommits   map[string]bool // Track git commits we've already seen (by hash)
	slug          string          // Human-readable session identifier
	retryNumber   int             // Number to append when branch conflicts occur
	linesAdded    int             // Lines added from sketch-base to HEAD
	linesRemoved  int             // Lines removed from sketch-base to HEAD
}

func (ags *AgentGitState) SetSlug(slug string) {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	if ags.slug != slug {
		ags.retryNumber = 0
	}
	ags.slug = slug
}

func (ags *AgentGitState) Slug() string {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	return ags.slug
}

func (ags *AgentGitState) IncrementRetryNumber() {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	ags.retryNumber++
}

func (ags *AgentGitState) DiffStats() (int, int) {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	return ags.linesAdded, ags.linesRemoved
}

// HasSeenCommits returns true if any commits have been processed
func (ags *AgentGitState) HasSeenCommits() bool {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	return len(ags.seenCommits) > 0
}

func (ags *AgentGitState) RetryNumber() int {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	return ags.retryNumber
}

func (ags *AgentGitState) BranchName(prefix string) string {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	return ags.branchNameLocked(prefix)
}

func (ags *AgentGitState) branchNameLocked(prefix string) string {
	if ags.slug == "" {
		return ""
	}
	if ags.retryNumber == 0 {
		return prefix + ags.slug
	}
	return fmt.Sprintf("%s%s%d", prefix, ags.slug, ags.retryNumber)
}

func (ags *AgentGitState) Upstream() string {
	ags.mu.Lock()
	defer ags.mu.Unlock()
	return ags.upstream
}

type Agent struct {
	convo             ConvoInterface
	config            AgentConfig // config for this agent
	gitState          AgentGitState
	workingDir        string
	repoRoot          string // workingDir may be a subdir of repoRoot
	url               string
	firstMessageIndex int           // index of the first message in the current conversation
	outsideHTTP       string        // base address of the outside webserver (only when under docker)
	ready             chan struct{} // closed when the agent is initialized (only when under docker)
	codebase          *onstart.Codebase
	startedAt         time.Time
	originalBudget    conversation.Budget
	codereview        *codereview.CodeReviewer
	// State machine to track agent state
	stateMachine *StateMachine
	// Outside information
	outsideHostname   string
	outsideOS         string
	outsideWorkingDir string
	// MCP manager for handling MCP server connections
	mcpManager *mcp.MCPManager
	// Port monitor for tracking TCP ports
	portMonitor *PortMonitor

	// Time when the current turn started (reset at the beginning of InnerLoop)
	startOfTurn time.Time

	// Inbox - for messages from the user to the agent.
	// sent on by UserMessage
	// . e.g. when user types into the chat textarea
	// read from by GatherMessages
	inbox chan string

	// protects cancelTurn
	cancelTurnMu sync.Mutex
	// cancels potentially long-running tool_use calls or chains of them
	cancelTurn context.CancelCauseFunc

	// protects following
	mu sync.Mutex

	// Stores all messages for this agent
	history []AgentMessage

	// Iterators add themselves here when they're ready to be notified of new messages.
	subscribers []chan *AgentMessage

	// Track outstanding LLM call IDs
	outstandingLLMCalls map[string]struct{}

	// Track outstanding tool calls by ID with their names
	outstandingToolCalls map[string]string
}

// TokenContextWindow implements CodingAgent.
func (a *Agent) TokenContextWindow() int {
	return a.config.Service.TokenContextWindow()
}

// GetConvo returns the conversation interface for debugging purposes.
func (a *Agent) GetConvo() ConvoInterface {
	return a.convo
}

// NewIterator implements CodingAgent.
func (a *Agent) NewIterator(ctx context.Context, nextMessageIdx int) MessageIterator {
	a.mu.Lock()
	defer a.mu.Unlock()

	return &MessageIteratorImpl{
		agent:          a,
		ctx:            ctx,
		nextMessageIdx: nextMessageIdx,
		ch:             make(chan *AgentMessage, 100),
	}
}

type MessageIteratorImpl struct {
	agent          *Agent
	ctx            context.Context
	nextMessageIdx int
	ch             chan *AgentMessage
	subscribed     bool
}

func (m *MessageIteratorImpl) Close() {
	m.agent.mu.Lock()
	defer m.agent.mu.Unlock()
	// Delete ourselves from the subscribers list
	m.agent.subscribers = slices.DeleteFunc(m.agent.subscribers, func(x chan *AgentMessage) bool {
		return x == m.ch
	})
	close(m.ch)
}

func (m *MessageIteratorImpl) Next() *AgentMessage {
	// We avoid subscription at creation to let ourselves catch up to "current state"
	// before subscribing.
	if !m.subscribed {
		m.agent.mu.Lock()
		if m.nextMessageIdx < len(m.agent.history) {
			msg := &m.agent.history[m.nextMessageIdx]
			m.nextMessageIdx++
			m.agent.mu.Unlock()
			return msg
		}
		// The next message doesn't exist yet, so let's subscribe
		m.agent.subscribers = append(m.agent.subscribers, m.ch)
		m.subscribed = true
		m.agent.mu.Unlock()
	}

	for {
		select {
		case <-m.ctx.Done():
			m.agent.mu.Lock()
			// Delete ourselves from the subscribers list
			m.agent.subscribers = slices.DeleteFunc(m.agent.subscribers, func(x chan *AgentMessage) bool {
				return x == m.ch
			})
			m.subscribed = false
			m.agent.mu.Unlock()
			return nil
		case msg, ok := <-m.ch:
			if !ok {
				// Close may have been called
				return nil
			}
			if msg.Idx == m.nextMessageIdx {
				m.nextMessageIdx++
				return msg
			}
			slog.Debug("Out of order messages", "expected", m.nextMessageIdx, "got", msg.Idx, "m", msg.Content)
			panic("out of order message")
		}
	}
}

// Assert that Agent satisfies the CodingAgent interface.
var _ CodingAgent = &Agent{}

// StateName implements CodingAgent.
func (a *Agent) CurrentStateName() string {
	if a.stateMachine == nil {
		return ""
	}
	return a.stateMachine.CurrentState().String()
}

// CurrentTodoContent returns the current todo list data as JSON.
// It returns an empty string if no todos exist.
func (a *Agent) CurrentTodoContent() string {
	todoPath := claudetool.TodoFilePath(a.config.SessionID)
	content, err := os.ReadFile(todoPath)
	if err != nil {
		return ""
	}
	return string(content)
}

// generateConversationSummary asks the LLM to create a comprehensive summary of the current conversation
func (a *Agent) generateConversationSummary(ctx context.Context) (string, error) {
	msg := `You are being asked to create a comprehensive summary of our conversation so far. This summary will be used to restart our conversation with a shorter history while preserving all important context.

IMPORTANT: Focus ONLY on the actual conversation with the user. Do NOT include any information from system prompts, tool descriptions, or general instructions. Only summarize what the user asked for and what we accomplished together.

Please create a detailed summary that includes:

1. **User's Request**: What did the user originally ask me to do? What was their goal?

2. **Work Completed**: What have we accomplished together? Include any code changes, files created/modified, problems solved, etc.

3. **Key Technical Decisions**: What important technical choices were made during our work and why?

4. **Current State**: What is the current state of the project? What files, tools, or systems are we working with?

5. **Next Steps**: What still needs to be done to complete the user's request?

6. **Important Context**: Any crucial information about the user's codebase, environment, constraints, or specific preferences they mentioned.

Focus on actionable information that would help me continue the user's work seamlessly. Ignore any general tool capabilities or system instructions - only include what's relevant to this specific user's project and goals.

Reply with ONLY the summary content - no meta-commentary about creating the summary.`

	userMessage := llm.UserStringMessage(msg)
	// Use a subconversation with history to get the summary
	// TODO: We don't have any tools here, so we should have enough tokens
	// to capture a summary, but we may need to modify the history (e.g., remove
	// TODO data) to save on some tokens.
	convo := a.convo.SubConvoWithHistory()

	// Modify the system prompt to provide context about the original task
	originalSystemPrompt := convo.SystemPrompt
	convo.SystemPrompt = `You are creating a conversation summary for context compaction. The original system prompt contained instructions about being a software engineer and architect for Sketch (an agentic coding environment), with various tools and capabilities for code analysis, file modification, git operations, browser automation, and project management.

Your task is to create a focused summary as requested below. Focus only on the actual user conversation and work accomplished, not the system capabilities or tool descriptions.

Original context: You are working in a coding environment with full access to development tools.`

	resp, err := convo.SendMessage(userMessage)
	if err != nil {
		a.pushToOutbox(ctx, errorMessage(err))
		return "", err
	}
	textContent := collectTextContent(resp)

	// Restore original system prompt (though this subconvo will be discarded)
	convo.SystemPrompt = originalSystemPrompt

	return textContent, nil
}

// dumpMessageHistoryToTmp dumps the agent's entire message history to /tmp as JSON
// and returns the filename
func (a *Agent) dumpMessageHistoryToTmp(ctx context.Context) (string, error) {
	// Create a filename based on session ID and timestamp
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("/tmp/sketch-messages-%s-%s.json", a.config.SessionID, timestamp)

	// Marshal the entire message history to JSON
	jsonData, err := json.MarshalIndent(a.history, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal message history: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filename, jsonData, 0o644); err != nil {
		return "", fmt.Errorf("failed to write message history to %s: %w", filename, err)
	}

	slog.InfoContext(ctx, "Dumped message history to file", "filename", filename, "message_count", len(a.history))
	return filename, nil
}

// CompactConversation compacts the current conversation by generating a summary
// and restarting the conversation with that summary as the initial context
func (a *Agent) CompactConversation(ctx context.Context) error {
	// Dump the entire message history to /tmp as JSON before compacting
	dumpFile, err := a.dumpMessageHistoryToTmp(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to dump message history to /tmp", "error", err)
		// Continue with compaction even if dump fails
	}

	summary, err := a.generateConversationSummary(ctx)
	if err != nil {
		return fmt.Errorf("failed to generate conversation summary: %w", err)
	}

	a.mu.Lock()

	// Get usage information before resetting conversation
	lastUsage := a.convo.LastUsage()
	contextWindow := a.config.Service.TokenContextWindow()
	currentContextSize := lastUsage.InputTokens + lastUsage.CacheReadInputTokens + lastUsage.CacheCreationInputTokens

	// Preserve cumulative usage across compaction
	cumulativeUsage := a.convo.CumulativeUsage()

	// Reset conversation state but keep all other state (git, working dir, etc.)
	a.firstMessageIndex = len(a.history)
	a.convo = a.initConvoWithUsage(&cumulativeUsage)

	a.mu.Unlock()

	// Create informative compaction message with token details
	compactionMsg := fmt.Sprintf("📜 Conversation compacted to manage token limits. Previous context preserved in summary below.\n\n"+
		"**Token Usage:** %d / %d tokens (%.1f%% of context window)",
		currentContextSize, contextWindow, float64(currentContextSize)/float64(contextWindow)*100)

	a.pushToOutbox(ctx, AgentMessage{
		Type:    CompactMessageType,
		Content: compactionMsg,
	})

	// Create the message content with dump file information if available
	var messageContent string
	if dumpFile != "" {
		messageContent = fmt.Sprintf("Here's a summary of our previous work:\n\n%s\n\nThe complete message history has been dumped to %s for your reference if needed.\n\nPlease continue with the work based on this summary.", summary, dumpFile)
	} else {
		messageContent = fmt.Sprintf("Here's a summary of our previous work:\n\n%s\n\nPlease continue with the work based on this summary.", summary)
	}

	a.pushToOutbox(ctx, AgentMessage{
		Type:    UserMessageType,
		Content: messageContent,
	})
	a.inbox <- messageContent

	return nil
}

func (a *Agent) URL() string { return a.url }

// GetPorts returns the cached list of open TCP ports.
func (a *Agent) GetPorts() []portlist.Port {
	if a.portMonitor == nil {
		return nil
	}
	return a.portMonitor.GetPorts()
}

// BranchName returns the git branch name for the conversation.
func (a *Agent) BranchName() string {
	return a.gitState.BranchName(a.config.BranchPrefix)
}

// Slug returns the slug identifier for this conversation.
func (a *Agent) Slug() string {
	return a.gitState.Slug()
}

// IncrementRetryNumber increments the retry number for branch naming conflicts
func (a *Agent) IncrementRetryNumber() {
	a.gitState.IncrementRetryNumber()
}

// OutstandingLLMCallCount returns the number of outstanding LLM calls.
func (a *Agent) OutstandingLLMCallCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.outstandingLLMCalls)
}

// OutstandingToolCalls returns the names of outstanding tool calls.
func (a *Agent) OutstandingToolCalls() []string {
	a.mu.Lock()
	defer a.mu.Unlock()

	tools := make([]string, 0, len(a.outstandingToolCalls))
	for _, toolName := range a.outstandingToolCalls {
		tools = append(tools, toolName)
	}
	return tools
}

// OS returns the operating system of the client.
func (a *Agent) OS() string {
	return a.config.ClientGOOS
}

func (a *Agent) SessionID() string {
	return a.config.SessionID
}

// SSHConnectionString returns the SSH connection string for the container.
func (a *Agent) SSHConnectionString() string {
	return a.config.SSHConnectionString
}

// OutsideOS returns the operating system of the outside system.
func (a *Agent) OutsideOS() string {
	return a.outsideOS
}

// OutsideHostname returns the hostname of the outside system.
func (a *Agent) OutsideHostname() string {
	return a.outsideHostname
}

// OutsideWorkingDir returns the working directory on the outside system.
func (a *Agent) OutsideWorkingDir() string {
	return a.outsideWorkingDir
}

// GitOrigin returns the URL of the git remote 'origin' if it exists.
func (a *Agent) GitOrigin() string {
	return a.config.OriginalGitOrigin
}

// GitUsername returns the git user name from the agent config.
func (a *Agent) GitUsername() string {
	return a.config.GitUsername
}

// DiffStats returns the number of lines added and removed from sketch-base to HEAD
func (a *Agent) DiffStats() (int, int) {
	return a.gitState.DiffStats()
}

func (a *Agent) OpenBrowser(url string) {
	if !a.IsInContainer() {
		browser.Open(url)
		return
	}
	// We're in Docker, need to send a request to the Git server
	// to signal that the outer process should open the browser.
	// We don't get to specify a URL, because we are untrusted.
	httpc := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpc.Post(a.outsideHTTP+"/browser", "text/plain", nil)
	if err != nil {
		slog.Debug("browser launch request connection failed", "err", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return
	}
	body, _ := io.ReadAll(resp.Body)
	slog.Debug("browser launch request execution failed", "status", resp.Status, "body", string(body))
}

// CurrentState returns the current state of the agent's state machine.
func (a *Agent) CurrentState() State {
	return a.stateMachine.CurrentState()
}

func (a *Agent) IsInContainer() bool {
	return a.config.InDocker
}

func (a *Agent) FirstMessageIndex() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.firstMessageIndex
}

// SetSlug sets a human-readable identifier for the conversation.
func (a *Agent) SetSlug(slug string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.gitState.SetSlug(slug)
	convo, ok := a.convo.(*conversation.Convo)
	if ok {
		convo.ExtraData["branch"] = a.BranchName()
	}
}

// OnToolCall implements ant.Listener and tracks the start of a tool call.
func (a *Agent) OnToolCall(ctx context.Context, convo *conversation.Convo, id string, toolName string, toolInput json.RawMessage, content llm.Content) {
	// Track the tool call
	a.mu.Lock()
	a.outstandingToolCalls[id] = toolName
	a.mu.Unlock()
}

// contentToString converts []llm.Content to a string, concatenating all text content and skipping non-text types.
// If there's only one element in the array and it's a text type, it returns that text directly.
// It also processes nested ToolResult arrays recursively.
func contentToString(contents []llm.Content) string {
	if len(contents) == 0 {
		return ""
	}

	// If there's only one element and it's a text type, return it directly
	if len(contents) == 1 && contents[0].Type == llm.ContentTypeText {
		return contents[0].Text
	}

	// Otherwise, concatenate all text content
	var result strings.Builder
	for _, content := range contents {
		if content.Type == llm.ContentTypeText && content.Text != "" {
			if result.Len() > 0 {
				result.WriteString("\n\n")
			}
			result.WriteString(content.Text)
		} else if content.Type == llm.ContentTypeToolResult && len(content.ToolResult) > 0 {
			// Recursively process nested tool results
			result.WriteString(contentToString(content.ToolResult))
		}
	}

	return result.String()
}

// OnToolResult implements ant.Listener.
func (a *Agent) OnToolResult(ctx context.Context, convo *conversation.Convo, toolID string, toolName string, toolInput json.RawMessage, content llm.Content, result *string, err error) {
	// Remove the tool call from outstanding calls
	a.mu.Lock()
	delete(a.outstandingToolCalls, toolID)
	a.mu.Unlock()

	m := AgentMessage{
		Type:       ToolUseMessageType,
		Content:    content.Text,
		ToolResult: contentToString(content.ToolResult),
		ToolError:  content.ToolError,
		ToolName:   toolName,
		ToolInput:  string(toolInput),
		ToolCallId: content.ToolUseID,
		StartTime:  content.ToolUseStartTime,
		EndTime:    content.ToolUseEndTime,
	}

	// Calculate the elapsed time if both start and end times are set
	if content.ToolUseStartTime != nil && content.ToolUseEndTime != nil {
		elapsed := content.ToolUseEndTime.Sub(*content.ToolUseStartTime)
		m.Elapsed = &elapsed
	}

	m.SetConvo(convo)
	a.pushToOutbox(ctx, m)
}

// OnRequest implements ant.Listener.
func (a *Agent) OnRequest(ctx context.Context, convo *conversation.Convo, id string, msg *llm.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.outstandingLLMCalls[id] = struct{}{}
	// We already get tool results from the above. We send user messages to the outbox in the agent loop.
}

// OnResponse implements conversation.Listener. Responses contain messages from the LLM
// that need to be displayed (as well as tool calls that we send along when
// they're done). (It would be reasonable to also mention tool calls when they're
// started, but we don't do that yet.)
func (a *Agent) OnResponse(ctx context.Context, convo *conversation.Convo, id string, resp *llm.Response) {
	// Remove the LLM call from outstanding calls
	a.mu.Lock()
	delete(a.outstandingLLMCalls, id)
	a.mu.Unlock()

	if resp == nil {
		// LLM API call failed
		m := AgentMessage{
			Type:    ErrorMessageType,
			Content: "API call failed, type 'continue' to try again",
		}
		m.SetConvo(convo)
		a.pushToOutbox(ctx, m)
		return
	}

	endOfTurn := false
	if convo.Parent == nil { // subconvos never end the turn
		switch resp.StopReason {
		case llm.StopReasonToolUse:
			// Check whether any of the tool calls are for tools that should end the turn
		ToolSearch:
			for _, part := range resp.Content {
				if part.Type != llm.ContentTypeToolUse {
					continue
				}
				// Find the tool by name
				for _, tool := range convo.Tools {
					if tool.Name == part.ToolName {
						endOfTurn = tool.EndsTurn
						break ToolSearch
					}
				}
			}
		default:
			endOfTurn = true
		}
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
	if resp.StopReason == llm.StopReasonToolUse {
		var toolCalls []ToolCall
		for _, part := range resp.Content {
			if part.Type == llm.ContentTypeToolUse {
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

	m.SetConvo(convo)
	a.pushToOutbox(ctx, m)
}

// WorkingDir implements CodingAgent.
func (a *Agent) WorkingDir() string {
	return a.workingDir
}

// RepoRoot returns the git repository root directory.
func (a *Agent) RepoRoot() string {
	return a.repoRoot
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

// ShouldCompact checks if the conversation should be compacted based on token usage
func (a *Agent) ShouldCompact() bool {
	// Get the threshold from environment variable, default to 0.94 (94%)
	// (Because default Claude output is 8192 tokens, which is 4% of 200,000 tokens,
	// and a little bit of buffer.)
	thresholdRatio := 0.94
	if envThreshold := os.Getenv("SKETCH_COMPACT_THRESHOLD_RATIO"); envThreshold != "" {
		if parsed, err := strconv.ParseFloat(envThreshold, 64); err == nil && parsed > 0 && parsed <= 1.0 {
			thresholdRatio = parsed
		}
	}

	// Get the most recent usage to check current context size
	lastUsage := a.convo.LastUsage()

	if lastUsage.InputTokens == 0 {
		// No API calls made yet
		return false
	}

	// Calculate the current context size from the last API call
	// This includes all tokens that were part of the input context:
	// - Input tokens (user messages, system prompt, conversation history)
	// - Cache read tokens (cached parts of the context)
	// - Cache creation tokens (new parts being cached)
	currentContextSize := lastUsage.InputTokens + lastUsage.CacheReadInputTokens + lastUsage.CacheCreationInputTokens

	// Get the service's token context window
	service := a.config.Service
	contextWindow := service.TokenContextWindow()

	// Calculate threshold
	threshold := uint64(float64(contextWindow) * thresholdRatio)

	// Check if we've exceeded the threshold
	return currentContextSize >= threshold
}

func (a *Agent) OriginalBudget() conversation.Budget {
	return a.originalBudget
}

// Upstream returns the upstream branch for git work
func (a *Agent) Upstream() string {
	return a.gitState.Upstream()
}

// AgentConfig contains configuration for creating a new Agent.
type AgentConfig struct {
	Context      context.Context
	Service      llm.Service
	Budget       conversation.Budget
	GitUsername  string
	GitEmail     string
	SessionID    string
	ClientGOOS   string
	ClientGOARCH string
	InDocker     bool
	OneShot      bool
	WorkingDir   string
	// Outside information
	OutsideHostname   string
	OutsideOS         string
	OutsideWorkingDir string

	// Outtie's HTTP to, e.g., open a browser
	OutsideHTTP string
	// Outtie's Git server
	GitRemoteAddr string
	// Original git origin URL from host repository, if any
	OriginalGitOrigin string
	// Upstream branch for git work
	Upstream string
	// Commit to checkout from Outtie
	Commit string
	// Prefix for git branches created by sketch
	BranchPrefix string
	// LinkToGitHub enables GitHub branch linking in UI
	LinkToGitHub bool
	// SSH connection string for connecting to the container
	SSHConnectionString string
	// Skaband client for session history (optional)
	SkabandClient *skabandclient.SkabandClient
	// MCP server configurations
	MCPServers []string
	// Timeout configuration for bash tool
	BashTimeouts *claudetool.Timeouts
}

// NewAgent creates a new Agent.
// It is not usable until Init() is called.
func NewAgent(config AgentConfig) *Agent {
	// Set default branch prefix if not specified
	if config.BranchPrefix == "" {
		config.BranchPrefix = "sketch/"
	}

	agent := &Agent{
		config:         config,
		ready:          make(chan struct{}),
		inbox:          make(chan string, 100),
		subscribers:    make([]chan *AgentMessage, 0),
		startedAt:      time.Now(),
		originalBudget: config.Budget,
		gitState: AgentGitState{
			seenCommits:   make(map[string]bool),
			gitRemoteAddr: config.GitRemoteAddr,
			upstream:      config.Upstream,
		},
		outsideHostname:      config.OutsideHostname,
		outsideOS:            config.OutsideOS,
		outsideWorkingDir:    config.OutsideWorkingDir,
		outstandingLLMCalls:  make(map[string]struct{}),
		outstandingToolCalls: make(map[string]string),
		stateMachine:         NewStateMachine(),
		workingDir:           config.WorkingDir,
		outsideHTTP:          config.OutsideHTTP,

		mcpManager: mcp.NewMCPManager(),
	}

	// Initialize port monitor with 5-second interval
	agent.portMonitor = NewPortMonitor(agent, 5*time.Second)

	return agent
}

type AgentInit struct {
	NoGit bool // only for testing

	InDocker bool
	HostAddr string
}

func (a *Agent) Init(ini AgentInit) error {
	if a.convo != nil {
		return fmt.Errorf("Agent.Init: already initialized")
	}
	ctx := a.config.Context
	slog.InfoContext(ctx, "agent initializing")

	// If a remote + commit was specified, clone it.
	if a.config.Commit != "" && a.gitState.gitRemoteAddr != "" {
		if _, err := os.Stat("/app/.git"); err == nil {
			// Already a repo in /app.
			// Make sure that the remote is configured correctly.
			// We do a fetch below.
			if err := upsertRemoteOrigin(ctx, "/app", a.gitState.gitRemoteAddr); err != nil {
				return err
			}
		} else {
			slog.InfoContext(ctx, "cloning git repo", "commit", a.config.Commit)
			// TODO: --reference-if-able instead?
			cmd := exec.CommandContext(ctx, "git", "clone", "--reference", "/git-ref", a.gitState.gitRemoteAddr, "/app")
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to clone repository from %s: %s: %w", a.gitState.gitRemoteAddr, out, err)
			}
		}
	}

	if a.workingDir != "" {
		err := os.Chdir(a.workingDir)
		if err != nil {
			return fmt.Errorf("failed to change working directory to %s: %w", a.workingDir, err)
		}
	}

	if !ini.NoGit {

		// Configure git user settings
		if a.config.GitEmail != "" {
			cmd := exec.CommandContext(ctx, "git", "config", "--global", "user.email", a.config.GitEmail)
			cmd.Dir = a.workingDir
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git config --global user.email: %s: %v", out, err)
			}
		}
		if a.config.GitUsername != "" {
			cmd := exec.CommandContext(ctx, "git", "config", "--global", "user.name", a.config.GitUsername)
			cmd.Dir = a.workingDir
			if out, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git config --global user.name: %s: %v", out, err)
			}
		}
		// Configure git http.postBuffer
		cmd := exec.CommandContext(ctx, "git", "config", "--global", "http.postBuffer", "524288000")
		cmd.Dir = a.workingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git config --global http.postBuffer: %s: %v", out, err)
		}
	}

	// If a commit was specified, we fetch and reset to it.
	if a.config.Commit != "" && a.gitState.gitRemoteAddr != "" {
		slog.InfoContext(ctx, "updating git repo", "commit", a.config.Commit)

		cmd := exec.CommandContext(ctx, "git", "fetch", "--prune", "origin")
		cmd.Dir = a.workingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git fetch: %s: %w", out, err)
		}
		// The -B resets the branch if it already exists (or creates it if it doesn't)
		cmd = exec.CommandContext(ctx, "git", "checkout", "-f", "-B", "sketch-wip", a.config.Commit)
		cmd.Dir = a.workingDir
		if checkoutOut, err := cmd.CombinedOutput(); err != nil {
			// Remove git hooks if they exist and retry
			// Only try removing hooks if we haven't already removed them during fetch
			hookPath := filepath.Join(a.workingDir, ".git", "hooks")
			if _, statErr := os.Stat(hookPath); statErr == nil {
				slog.WarnContext(ctx, "git checkout failed, removing hooks and retrying",
					slog.String("error", err.Error()),
					slog.String("output", string(checkoutOut)))
				if removeErr := removeGitHooks(ctx, a.workingDir); removeErr != nil {
					slog.WarnContext(ctx, "failed to remove git hooks", slog.String("error", removeErr.Error()))
				}

				// Retry the checkout operation
				cmd = exec.CommandContext(ctx, "git", "checkout", "-f", "-B", "sketch-wip", a.config.Commit)
				cmd.Dir = a.workingDir
				if retryOut, retryErr := cmd.CombinedOutput(); retryErr != nil {
					return fmt.Errorf("git checkout %s failed even after removing hooks: %s: %w", a.config.Commit, retryOut, retryErr)
				}
			} else {
				return fmt.Errorf("git checkout -f -B sketch-wip %s: %s: %w", a.config.Commit, checkoutOut, err)
			}
		}
	} else if a.IsInContainer() {
		// If we're not running in a container, we don't switch branches (nor push branches back and forth).
		slog.InfoContext(ctx, "checking out branch", slog.String("commit", a.config.Commit))
		cmd := exec.CommandContext(ctx, "git", "checkout", "-f", "-B", "sketch-wip")
		cmd.Dir = a.workingDir
		if checkoutOut, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout -f -B sketch-wip: %s: %w", checkoutOut, err)
		}
	} else {
		slog.InfoContext(ctx, "Not checking out any branch")
	}

	if ini.HostAddr != "" {
		a.url = "http://" + ini.HostAddr
	}

	if !ini.NoGit {
		repoRoot, err := repoRoot(ctx, a.workingDir)
		if err != nil {
			return fmt.Errorf("repoRoot: %w", err)
		}
		a.repoRoot = repoRoot

		if a.IsInContainer() {
			if err := setupGitHooks(a.repoRoot); err != nil {
				slog.WarnContext(ctx, "failed to set up git hooks", "err", err)
			}
		}

		// Check if we have any commits, and if not, create an empty initial commit
		cmd := exec.CommandContext(ctx, "git", "rev-list", "--all", "--count")
		cmd.Dir = repoRoot
		countOut, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("git rev-list --all --count: %s: %w", countOut, err)
		}
		commitCount := strings.TrimSpace(string(countOut))
		if commitCount == "0" {
			slog.Info("No commits found, creating empty initial commit")
			cmd = exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "Initial empty commit")
			cmd.Dir = repoRoot
			if commitOut, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git commit --allow-empty: %s: %w", commitOut, err)
			}
		}

		cmd = exec.CommandContext(ctx, "git", "tag", "-f", a.SketchGitBaseRef(), "HEAD")
		cmd.Dir = repoRoot
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git tag -f %s %s: %s: %w", a.SketchGitBaseRef(), "HEAD", out, err)
		}

		slog.Info("running codebase analysis")
		codebase, err := onstart.AnalyzeCodebase(ctx, a.repoRoot)
		if err != nil {
			slog.Warn("failed to analyze codebase", "error", err)
		}
		a.codebase = codebase

		codereview, err := codereview.NewCodeReviewer(ctx, a.repoRoot, a.SketchGitBaseRef())
		if err != nil {
			return fmt.Errorf("Agent.Init: codereview.NewCodeReviewer: %w", err)
		}
		a.codereview = codereview

	}
	a.gitState.lastSketch = a.SketchGitBase()
	a.convo = a.initConvo()
	close(a.ready)
	return nil
}

//go:embed agent_system_prompt.txt
var agentSystemPrompt string

// initConvo initializes the conversation.
// It must not be called until all agent fields are initialized,
// particularly workingDir and git.
func (a *Agent) initConvo() *conversation.Convo {
	return a.initConvoWithUsage(nil)
}

// initConvoWithUsage initializes the conversation with optional preserved usage.
func (a *Agent) initConvoWithUsage(usage *conversation.CumulativeUsage) *conversation.Convo {
	ctx := a.config.Context
	convo := conversation.New(ctx, a.config.Service, usage)
	convo.PromptCaching = true
	convo.Budget = a.config.Budget
	convo.SystemPrompt = a.renderSystemPrompt()
	convo.ExtraData = map[string]any{"session_id": a.config.SessionID}

	// Define a permission callback for the bash tool to check if the branch name is set before allowing git commits
	bashPermissionCheck := func(command string) error {
		if a.gitState.Slug() != "" {
			return nil // branch is set up
		}
		willCommit, err := bashkit.WillRunGitCommit(command)
		if err != nil {
			return nil // fail open
		}
		if willCommit {
			return fmt.Errorf("you must use the set-slug tool before making git commits")
		}
		return nil
	}

	bashTool := &claudetool.BashTool{
		CheckPermission:  bashPermissionCheck,
		EnableJITInstall: claudetool.EnableBashToolJITInstall,
		Timeouts:         a.config.BashTimeouts,
	}

	// Register all tools with the conversation
	// When adding, removing, or modifying tools here, double-check that the termui tool display
	// template in termui/termui.go has pretty-printing support for all tools.

	var browserTools []*llm.Tool
	_, supportsScreenshots := a.config.Service.(*ant.Service)
	var bTools []*llm.Tool
	var browserCleanup func()

	bTools, browserCleanup = browse.RegisterBrowserTools(a.config.Context, supportsScreenshots)
	// Add cleanup function to context cancel
	go func() {
		<-a.config.Context.Done()
		browserCleanup()
	}()
	browserTools = bTools

	convo.Tools = []*llm.Tool{
		bashTool.Tool(), claudetool.Keyword, claudetool.Patch(a.patchCallback),
		claudetool.Think, claudetool.TodoRead, claudetool.TodoWrite, a.setSlugTool(), a.commitMessageStyleTool(), makeDoneTool(a.codereview),
		a.codereview.Tool(), claudetool.AboutSketch, (&claudetool.NmapTool{}).Tool(),
		claudetool.PentestToolInstance,
	}

	// One-shot mode is non-interactive, multiple choice requires human response
	if !a.config.OneShot {
		convo.Tools = append(convo.Tools, multipleChoiceTool)
	}

	convo.Tools = append(convo.Tools, browserTools...)

	// Add MCP tools if configured
	if len(a.config.MCPServers) > 0 {

		slog.InfoContext(ctx, "Initializing MCP connections", "servers", len(a.config.MCPServers))
		serverConfigs, parseErrors := mcp.ParseServerConfigs(ctx, a.config.MCPServers)

		// Replace any headers with value _sketch_public_key_ and _sketch_session_id_ with those values.
		for i := range serverConfigs {
			if serverConfigs[i].Headers != nil {
				for key, value := range serverConfigs[i].Headers {
					// Replace env placeholders. E.g., "env:FOO" becomes os.Getenv("FOO")
					if strings.HasPrefix(value, "env:") {
						serverConfigs[i].Headers[key] = os.Getenv(value[4:])
					}
				}
			}
		}
		mcpConnections, mcpErrors := a.mcpManager.ConnectToServerConfigs(ctx, serverConfigs, 10*time.Second, parseErrors)

		if len(mcpErrors) > 0 {
			for _, err := range mcpErrors {
				slog.ErrorContext(ctx, "MCP connection error", "error", err)
				// Send agent message about MCP connection failures
				a.pushToOutbox(ctx, AgentMessage{
					Type:    ErrorMessageType,
					Content: fmt.Sprintf("MCP server connection failed: %v", err),
				})
			}
		}

		if len(mcpConnections) > 0 {
			// Add tools from all successful connections
			totalTools := 0
			for _, connection := range mcpConnections {
				convo.Tools = append(convo.Tools, connection.Tools...)
				totalTools += len(connection.Tools)
				// Log tools per server using structured data
				slog.InfoContext(ctx, "Added MCP tools from server", "server", connection.ServerName, "count", len(connection.Tools), "tools", connection.ToolNames)
			}
			slog.InfoContext(ctx, "Total MCP tools added", "count", totalTools)
		} else {
			slog.InfoContext(ctx, "No MCP tools available after connection attempts")
		}
	}

	convo.Listener = a
	return convo
}

var multipleChoiceTool = &llm.Tool{
	Name:        "multiplechoice",
	Description: `Present the user with an quick way to answer to your question using one of a short list of possible answers you would expect from the user.`,
	EndsTurn:    true,
	InputSchema: json.RawMessage(`{
  "type": "object",
  "description": "The question and a list of answers you would expect the user to choose from.",
  "properties": {
    "question": {
      "type": "string",
      "description": "The text of the multiple-choice question you would like the user, e.g. 'What kinds of test cases would you like me to add?'"
    },
    "responseOptions": {
      "type": "array",
      "description": "The set of possible answers to let the user quickly choose from, e.g. ['Basic unit test coverage', 'Error return values', 'Malformed input'].",
      "items": {
        "type": "object",
        "properties": {
          "caption": {
            "type": "string",
            "description": "The caption, e.g. 'Basic coverage', 'Error return values', or 'Malformed input' for the response button. Do NOT include options for responses that would end the conversation like 'Ok', 'No thank you', 'This looks good'"
          },
          "responseText": {
            "type": "string",
            "description": "The full text of the response, e.g. 'Add unit tests for basic test coverage', 'Add unit tests for error return values', or 'Add unit tests for malformed input'"
          }
        },
        "required": ["caption", "responseText"]
      }
    }
  },
  "required": ["question", "responseOptions"]
}`),
	Run: func(ctx context.Context, input json.RawMessage) ([]llm.Content, error) {
		// The Run logic for "multiplechoice" tool is a no-op on the server.
		// The UI will present a list of options for the user to select from,
		// and that's it as far as "executing" the tool_use goes.
		// When the user *does* select one of the presented options, that
		// responseText gets sent as a chat message on behalf of the user.
		return llm.TextContent("end your turn and wait for the user to respond"), nil
	},
}

type MultipleChoiceOption struct {
	Caption      string `json:"caption"`
	ResponseText string `json:"responseText"`
}

type MultipleChoiceParams struct {
	Question        string                 `json:"question"`
	ResponseOptions []MultipleChoiceOption `json:"responseOptions"`
}

// branchExists reports whether branchName exists, either locally or in well-known remotes.
func branchExists(dir, branchName string) bool {
	refs := []string{
		"refs/heads/",
		"refs/remotes/origin/",
	}
	for _, ref := range refs {
		cmd := exec.Command("git", "show-ref", "--verify", "--quiet", ref+branchName)
		cmd.Dir = dir
		if cmd.Run() == nil { // exit code 0 means branch exists
			return true
		}
	}
	return false
}

func (a *Agent) setSlugTool() *llm.Tool {
	return &llm.Tool{
		Name:        "set-slug",
		Description: `Set a short slug as an identifier for this conversation.`,
		InputSchema: json.RawMessage(`{
	"type": "object",
	"properties": {
		"slug": {
			"type": "string",
			"description": "A 2-3 word alphanumeric hyphenated slug, imperative tense"
		}
	},
	"required": ["slug"]
}`),
		Run: func(ctx context.Context, input json.RawMessage) ([]llm.Content, error) {
			var params struct {
				Slug string `json:"slug"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return nil, err
			}
			// Prevent slug changes if there have been git changes
			// This lets the agent change its mind about a good slug,
			// while ensuring that once a branch has been pushed, it remains stable.
			if s := a.Slug(); s != "" && s != params.Slug && a.gitState.HasSeenCommits() {
				return nil, fmt.Errorf("slug already set to %q", s)
			}
			if params.Slug == "" {
				return nil, fmt.Errorf("slug parameter cannot be empty")
			}
			slug := cleanSlugName(params.Slug)
			if slug == "" {
				return nil, fmt.Errorf("slug parameter could not be converted to a valid slug")
			}
			a.SetSlug(slug)
			// TODO: do this by a call to outie, rather than semi-guessing from innie
			if branchExists(a.workingDir, a.BranchName()) {
				return nil, fmt.Errorf("slug %q already exists; please choose a different slug", slug)
			}
			return llm.TextContent("OK"), nil
		},
	}
}

func (a *Agent) commitMessageStyleTool() *llm.Tool {
	description := `Provides git commit message style guidance. MANDATORY: You must use this tool before making any git commits.`
	preCommit := &llm.Tool{
		Name:        "commit-message-style",
		Description: description,
		InputSchema: llm.EmptySchema(),
		Run: func(ctx context.Context, input json.RawMessage) ([]llm.Content, error) {
			styleHint, err := claudetool.CommitMessageStyleHint(ctx, a.repoRoot)
			if err != nil {
				slog.DebugContext(ctx, "failed to get commit message style hint", "err", err)
			}
			return llm.TextContent(styleHint), nil
		},
	}
	return preCommit
}

// patchCallback is the agent's patch tool callback.
// It warms the codereview cache in the background.
func (a *Agent) patchCallback(input claudetool.PatchInput, result []llm.Content, err error) ([]llm.Content, error) {
	if a.codereview != nil {
		a.codereview.WarmTestCache(input.Path)
	}
	return result, err
}

func (a *Agent) Ready() <-chan struct{} {
	return a.ready
}

// BranchPrefix returns the configured branch prefix
func (a *Agent) BranchPrefix() string {
	return a.config.BranchPrefix
}

// LinkToGitHub returns whether GitHub branch linking is enabled
func (a *Agent) LinkToGitHub() bool {
	return a.config.LinkToGitHub
}

func (a *Agent) UserMessage(ctx context.Context, msg string) {
	a.pushToOutbox(ctx, AgentMessage{Type: UserMessageType, Content: msg})
	a.inbox <- msg
}

func (a *Agent) CancelToolUse(toolUseID string, cause error) error {
	return a.convo.CancelToolUse(toolUseID, cause)
}

func (a *Agent) CancelTurn(cause error) {
	a.cancelTurnMu.Lock()
	defer a.cancelTurnMu.Unlock()
	if a.cancelTurn != nil {
		// Force state transition to cancelled state
		ctx := a.config.Context
		a.stateMachine.ForceTransition(ctx, StateCancelled, "User cancelled turn: "+cause.Error())
		a.cancelTurn(cause)
	}
}

func (a *Agent) Loop(ctxOuter context.Context) {
	// Start port monitoring
	if a.portMonitor != nil && a.IsInContainer() {
		if err := a.portMonitor.Start(ctxOuter); err != nil {
			slog.WarnContext(ctxOuter, "Failed to start port monitor", "error", err)
		} else {
			slog.InfoContext(ctxOuter, "Port monitor started")
		}
	}

	// Set up cleanup when context is done
	defer func() {
		if a.mcpManager != nil {
			a.mcpManager.Close()
		}
		if a.portMonitor != nil && a.IsInContainer() {
			a.portMonitor.Stop()
		}
	}()

	for {
		select {
		case <-ctxOuter.Done():
			return
		default:
			ctxInner, cancel := context.WithCancelCause(ctxOuter)
			a.cancelTurnMu.Lock()
			// Set .cancelTurn so the user can cancel whatever is happening
			// inside the conversation loop without canceling this outer Loop execution.
			// This cancelTurn func is intended be called from other goroutines,
			// hence the mutex.
			a.cancelTurn = cancel
			a.cancelTurnMu.Unlock()
			err := a.processTurn(ctxInner) // Renamed from InnerLoop to better reflect its purpose
			if err != nil {
				slog.ErrorContext(ctxOuter, "Error in processing turn", "error", err)
			}
			cancel(nil)
		}
	}
}

func (a *Agent) pushToOutbox(ctx context.Context, m AgentMessage) {
	if m.Timestamp.IsZero() {
		m.Timestamp = time.Now()
	}

	// If this is a ToolUseMessage and ToolResult is set but Content is not, copy the ToolResult to Content
	if m.Type == ToolUseMessageType && m.ToolResult != "" && m.Content == "" {
		m.Content = m.ToolResult
	}

	// If this is an end-of-turn message, calculate the turn duration and add it to the message
	if m.EndOfTurn && m.Type == AgentMessageType {
		turnDuration := time.Since(a.startOfTurn)
		m.TurnDuration = &turnDuration
		slog.InfoContext(ctx, "Turn completed", "turnDuration", turnDuration)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	m.Idx = len(a.history)
	slog.InfoContext(ctx, "agent message", m.Attr())
	a.history = append(a.history, m)

	// Notify all subscribers
	for _, ch := range a.subscribers {
		ch <- &m
	}
}

func (a *Agent) GatherMessages(ctx context.Context, block bool) ([]llm.Content, error) {
	var m []llm.Content
	if block {
		select {
		case <-ctx.Done():
			return m, ctx.Err()
		case msg := <-a.inbox:
			m = append(m, llm.StringContent(msg))
		}
	}
	for {
		select {
		case msg := <-a.inbox:
			m = append(m, llm.StringContent(msg))
		default:
			return m, nil
		}
	}
}

// processTurn handles a single conversation turn with the user
func (a *Agent) processTurn(ctx context.Context) error {
	// Reset the start of turn time
	a.startOfTurn = time.Now()

	// Transition to waiting for user input state
	a.stateMachine.Transition(ctx, StateWaitingForUserInput, "Starting turn")

	// Process initial user message
	initialResp, err := a.processUserMessage(ctx)
	if err != nil {
		a.stateMachine.Transition(ctx, StateError, "Error processing user message: "+err.Error())
		return err
	}

	// Handle edge case where both initialResp and err are nil
	if initialResp == nil {
		err := fmt.Errorf("unexpected nil response from processUserMessage with no error")
		a.stateMachine.Transition(ctx, StateError, "Error processing user message: "+err.Error())

		a.pushToOutbox(ctx, errorMessage(err))
		return err
	}

	// We do this as we go, but let's also do it at the end of the turn
	defer func() {
		if _, err := a.handleGitCommits(ctx); err != nil {
			// Just log the error, don't stop execution
			slog.WarnContext(ctx, "Failed to check for new git commits", "error", err)
		}
	}()

	// Main response loop - continue as long as the model is using tools or a tool use fails.
	resp := initialResp
	for {
		// Check if we are over budget
		if err := a.overBudget(ctx); err != nil {
			a.stateMachine.Transition(ctx, StateBudgetExceeded, "Budget exceeded: "+err.Error())
			return err
		}

		// Check if we should compact the conversation
		if a.ShouldCompact() {
			a.stateMachine.Transition(ctx, StateCompacting, "Token usage threshold reached, compacting conversation")
			if err := a.CompactConversation(ctx); err != nil {
				a.stateMachine.Transition(ctx, StateError, "Error during compaction: "+err.Error())
				return err
			}
			// After compaction, end this turn and start fresh
			a.stateMachine.Transition(ctx, StateEndOfTurn, "Compaction completed, ending turn")
			return nil
		}

		// If the model is not requesting to use a tool, we're done
		if resp.StopReason != llm.StopReasonToolUse {
			a.stateMachine.Transition(ctx, StateEndOfTurn, "LLM completed response, ending turn")
			break
		}

		// Transition to tool use requested state
		a.stateMachine.Transition(ctx, StateToolUseRequested, "LLM requested tool use")

		// Handle tool execution
		continueConversation, toolResp := a.handleToolExecution(ctx, resp)
		if !continueConversation {
			return nil
		}

		if toolResp == nil {
			return fmt.Errorf("cannot continue conversation with a nil tool response")
		}

		// Set the response for the next iteration
		resp = toolResp
	}

	return nil
}

// processUserMessage waits for user messages and sends them to the model
func (a *Agent) processUserMessage(ctx context.Context) (*llm.Response, error) {
	// Wait for at least one message from the user
	msgs, err := a.GatherMessages(ctx, true)
	if err != nil { // e.g. the context was canceled while blocking in GatherMessages
		a.stateMachine.Transition(ctx, StateError, "Error gathering messages: "+err.Error())
		return nil, err
	}

	userMessage := llm.Message{
		Role:    llm.MessageRoleUser,
		Content: msgs,
	}

	// Transition to sending to LLM state
	a.stateMachine.Transition(ctx, StateSendingToLLM, "Sending user message to LLM")

	// Send message to the model
	resp, err := a.convo.SendMessage(userMessage)
	if err != nil {
		a.stateMachine.Transition(ctx, StateError, "Error sending to LLM: "+err.Error())
		a.pushToOutbox(ctx, errorMessage(err))
		return nil, err
	}

	// Transition to processing LLM response state
	a.stateMachine.Transition(ctx, StateProcessingLLMResponse, "Processing LLM response")

	return resp, nil
}

// handleToolExecution processes a tool use request from the model
func (a *Agent) handleToolExecution(ctx context.Context, resp *llm.Response) (bool, *llm.Response) {
	var results []llm.Content
	cancelled := false
	toolEndsTurn := false

	// Transition to checking for cancellation state
	a.stateMachine.Transition(ctx, StateCheckingForCancellation, "Checking if user requested cancellation")

	// Check if the operation was cancelled by the user
	select {
	case <-ctx.Done():
		// Don't actually run any of the tools, but rather build a response
		// for each tool_use message letting the LLM know that user canceled it.
		var err error
		results, err = a.convo.ToolResultCancelContents(resp)
		if err != nil {
			a.stateMachine.Transition(ctx, StateError, "Error creating cancellation response: "+err.Error())
			a.pushToOutbox(ctx, errorMessage(err))
		}
		cancelled = true
		a.stateMachine.Transition(ctx, StateCancelled, "Operation cancelled by user")
	default:
		// Transition to running tool state
		a.stateMachine.Transition(ctx, StateRunningTool, "Executing requested tool")

		// Add working directory and session ID to context for tool execution
		ctx = claudetool.WithWorkingDir(ctx, a.workingDir)
		ctx = claudetool.WithSessionID(ctx, a.config.SessionID)

		// Execute the tools
		var err error
		results, toolEndsTurn, err = a.convo.ToolResultContents(ctx, resp)
		if ctx.Err() != nil { // e.g. the user canceled the operation
			cancelled = true
			a.stateMachine.Transition(ctx, StateCancelled, "Operation cancelled during tool execution")
		} else if err != nil {
			a.stateMachine.Transition(ctx, StateError, "Error executing tool: "+err.Error())
			a.pushToOutbox(ctx, errorMessage(err))
		}
	}

	// Process git commits that may have occurred during tool execution
	a.stateMachine.Transition(ctx, StateCheckingGitCommits, "Checking for git commits")
	autoqualityMessages := a.processGitChanges(ctx)

	// Check budget again after tool execution
	a.stateMachine.Transition(ctx, StateCheckingBudget, "Checking budget after tool execution")
	if err := a.overBudget(ctx); err != nil {
		a.stateMachine.Transition(ctx, StateBudgetExceeded, "Budget exceeded after tool execution: "+err.Error())
		return false, nil
	}

	// Continue the conversation with tool results and any user messages
	shouldContinue, resp := a.continueTurnWithToolResults(ctx, results, autoqualityMessages, cancelled)
	return shouldContinue && !toolEndsTurn, resp
}

// DetectGitChanges checks for new git commits and pushes them if found
func (a *Agent) DetectGitChanges(ctx context.Context) error {
	// Check for git commits
	_, err := a.handleGitCommits(ctx)
	if err != nil {
		slog.WarnContext(ctx, "Failed to check for new git commits", "error", err)
		return fmt.Errorf("failed to check for new git commits: %w", err)
	}
	return nil
}

// processGitChanges checks for new git commits, runs autoformatters if needed, and returns any messages generated
// This is used internally by the agent loop
func (a *Agent) processGitChanges(ctx context.Context) []string {
	// Check for git commits after tool execution
	newCommits, err := a.handleGitCommits(ctx)
	if err != nil {
		// Just log the error, don't stop execution
		slog.WarnContext(ctx, "Failed to check for new git commits", "error", err)
		return nil
	}

	// Run mechanical checks if there was exactly one new commit.
	if len(newCommits) != 1 {
		return nil
	}
	var autoqualityMessages []string
	a.stateMachine.Transition(ctx, StateRunningAutoformatters, "Running mechanical checks on new commit")
	msg := a.codereview.RunMechanicalChecks(ctx)
	if msg != "" {
		a.pushToOutbox(ctx, AgentMessage{
			Type:      AutoMessageType,
			Content:   msg,
			Timestamp: time.Now(),
		})
		autoqualityMessages = append(autoqualityMessages, msg)
	}

	return autoqualityMessages
}

// continueTurnWithToolResults continues the conversation with tool results
func (a *Agent) continueTurnWithToolResults(ctx context.Context, results []llm.Content, autoqualityMessages []string, cancelled bool) (bool, *llm.Response) {
	// Get any messages the user sent while tools were executing
	a.stateMachine.Transition(ctx, StateGatheringAdditionalMessages, "Gathering additional user messages")
	msgs, err := a.GatherMessages(ctx, false)
	if err != nil {
		a.stateMachine.Transition(ctx, StateError, "Error gathering additional messages: "+err.Error())
		return false, nil
	}

	// Inject any auto-generated messages from quality checks
	for _, msg := range autoqualityMessages {
		msgs = append(msgs, llm.StringContent(msg))
	}

	// Handle cancellation by appending a message about it
	if cancelled {
		msgs = append(msgs, llm.StringContent(cancelToolUseMessage))
		// EndOfTurn is false here so that the client of this agent keeps processing
		// further messages; the conversation is not over.
		a.pushToOutbox(ctx, AgentMessage{Type: ErrorMessageType, Content: userCancelMessage, EndOfTurn: false})
	} else if err := a.convo.OverBudget(); err != nil {
		// Handle budget issues by appending a message about it
		budgetMsg := "We've exceeded our budget. Please ask the user to confirm before continuing by ending the turn."
		msgs = append(msgs, llm.StringContent(budgetMsg))
		a.pushToOutbox(ctx, budgetMessage(fmt.Errorf("warning: %w (ask to keep trying, if you'd like)", err)))
	}

	// Combine tool results with user messages
	results = append(results, msgs...)

	// Send the combined message to continue the conversation
	a.stateMachine.Transition(ctx, StateSendingToolResults, "Sending tool results back to LLM")
	resp, err := a.convo.SendMessage(llm.Message{
		Role:    llm.MessageRoleUser,
		Content: results,
	})
	if err != nil {
		a.stateMachine.Transition(ctx, StateError, "Error sending tool results: "+err.Error())
		a.pushToOutbox(ctx, errorMessage(fmt.Errorf("error: failed to continue conversation: %s", err.Error())))
		return true, nil // Return true to continue the conversation, but with no response
	}

	// Transition back to processing LLM response
	a.stateMachine.Transition(ctx, StateProcessingLLMResponse, "Processing LLM response to tool results")

	if cancelled {
		return false, nil
	}

	return true, resp
}

func (a *Agent) overBudget(ctx context.Context) error {
	if err := a.convo.OverBudget(); err != nil {
		a.stateMachine.Transition(ctx, StateBudgetExceeded, "Budget exceeded: "+err.Error())
		m := budgetMessage(err)
		m.Content = m.Content + "\n\nBudget reset."
		a.pushToOutbox(ctx, m)
		a.convo.ResetBudget(a.originalBudget)
		return err
	}
	return nil
}

func collectTextContent(msg *llm.Response) string {
	// Collect all text content
	var allText strings.Builder
	for _, content := range msg.Content {
		if content.Type == llm.ContentTypeText && content.Text != "" {
			if allText.Len() > 0 {
				allText.WriteString("\n\n")
			}
			allText.WriteString(content.Text)
		}
	}
	return allText.String()
}

func (a *Agent) TotalUsage() conversation.CumulativeUsage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.convo.CumulativeUsage()
}

// Diff returns a unified diff of changes made since the agent was instantiated.
func (a *Agent) Diff(commit *string) (string, error) {
	if a.SketchGitBase() == "" {
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
	cmd := exec.CommandContext(ctx, "git", "diff", "--unified=10", a.SketchGitBaseRef())
	cmd.Dir = a.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get diff: %w - %s", err, string(output))
	}

	return string(output), nil
}

// SketchGitBaseRef distinguishes between the typical container version, where sketch-base is
// unambiguous, and the "unsafe" version, where we need to use a session id to disambiguate.
func (a *Agent) SketchGitBaseRef() string {
	if a.IsInContainer() {
		return "sketch-base"
	} else {
		return "sketch-base-" + a.SessionID()
	}
}

// SketchGitBase returns the Git commit hash that was saved when the agent was instantiated.
func (a *Agent) SketchGitBase() string {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", a.SketchGitBaseRef())
	cmd.Dir = a.repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("could not identify sketch-base", slog.String("error", err.Error()))
		return "HEAD"
	}
	return string(strings.TrimSpace(string(output)))
}

// removeGitHooks removes the Git hooks directory from the repository
func removeGitHooks(_ context.Context, repoPath string) error {
	hooksDir := filepath.Join(repoPath, ".git", "hooks")

	// Check if hooks directory exists
	if _, err := os.Stat(hooksDir); os.IsNotExist(err) {
		// Directory doesn't exist, nothing to do
		return nil
	} else if err != nil {
		return fmt.Errorf("error checking git hooks directory: %w", err)
	}

	// Remove the hooks directory
	err := os.RemoveAll(hooksDir)
	if err != nil {
		return fmt.Errorf("failed to remove git hooks directory: %w", err)
	}

	// Create an empty hooks directory to prevent git from recreating default hooks
	err = os.MkdirAll(hooksDir, 0o755)
	if err != nil {
		return fmt.Errorf("failed to create empty git hooks directory: %w", err)
	}

	return nil
}

func (a *Agent) handleGitCommits(ctx context.Context) ([]*GitCommit, error) {
	msgs, commits, error := a.gitState.handleGitCommits(ctx, a.repoRoot, a.SketchGitBaseRef(), a.config.BranchPrefix)
	for _, msg := range msgs {
		a.pushToOutbox(ctx, msg)
	}
	return commits, error
}

// handleGitCommits() highlights new commits to the user. When running
// under docker, new HEADs are pushed to a branch according to the slug.
func (ags *AgentGitState) handleGitCommits(ctx context.Context, repoRoot string, baseRef string, branchPrefix string) ([]AgentMessage, []*GitCommit, error) {
	ags.mu.Lock()
	defer ags.mu.Unlock()

	msgs := []AgentMessage{}
	if repoRoot == "" {
		return msgs, nil, nil
	}

	sketch, err := resolveRef(ctx, repoRoot, "sketch-wip")
	if err != nil {
		return msgs, nil, err
	}
	if sketch == ags.lastSketch {
		return msgs, nil, nil // nothing to do
	}
	defer func() {
		ags.lastSketch = sketch
	}()

	// Compute diff stats from baseRef to HEAD when HEAD changes
	if added, removed, err := computeDiffStats(ctx, repoRoot, baseRef); err != nil {
		// Log error but don't fail the entire operation
		slog.WarnContext(ctx, "Failed to compute diff stats", "error", err)
	} else {
		// Set diff stats directly since we already hold the mutex
		ags.linesAdded = added
		ags.linesRemoved = removed
	}

	// Get new commits. Because it's possible that the agent does rebases, fixups, and
	// so forth, we use, as our fixed point, the "initialCommit", and we limit ourselves
	// to the last 100 commits.
	var commits []*GitCommit

	// Get commits since the initial commit
	// Format: <hash>\0<subject>\0<body>\0
	// This uses NULL bytes as separators to avoid issues with newlines in commit messages
	// Limit to 100 commits to avoid overwhelming the user
	cmd := exec.CommandContext(ctx, "git", "log", "-n", "100", "--pretty=format:%H%x00%s%x00%b%x00", "^"+baseRef, sketch)
	cmd.Dir = repoRoot
	output, err := cmd.Output()
	if err != nil {
		return msgs, nil, fmt.Errorf("failed to get git log: %w", err)
	}

	// Parse git log output and filter out already seen commits
	parsedCommits := parseGitLog(string(output))

	var sketchCommit *GitCommit

	// Filter out commits we've already seen
	for _, commit := range parsedCommits {
		if commit.Hash == sketch {
			sketchCommit = &commit
		}

		// Skip if we've seen this commit before. If our sketch branch has changed, always include that.
		if ags.seenCommits[commit.Hash] && commit.Hash != sketch {
			continue
		}

		// Mark this commit as seen
		ags.seenCommits[commit.Hash] = true

		// Add to our list of new commits
		commits = append(commits, &commit)
	}

	if ags.gitRemoteAddr != "" {
		if sketchCommit == nil {
			// I think this can only happen if we have a bug or if there's a race.
			sketchCommit = &GitCommit{}
			sketchCommit.Hash = sketch
			sketchCommit.Subject = "unknown"
			commits = append(commits, sketchCommit)
		}

		// Try up to 10 times with incrementing retry numbers if the branch is checked out on the remote
		var out []byte
		var err error
		originalRetryNumber := ags.retryNumber
		originalBranchName := ags.branchNameLocked(branchPrefix)
		for retries := range 10 {
			if retries > 0 {
				ags.retryNumber++
			}

			branch := ags.branchNameLocked(branchPrefix)
			cmd := exec.Command("git", "push", "--force", ags.gitRemoteAddr, "sketch-wip:refs/heads/"+branch)
			cmd.Dir = repoRoot
			out, err = cmd.CombinedOutput()

			if err == nil {
				// Success! Break out of the retry loop
				break
			}

			// Check if this is the "refusing to update checked out branch" error
			if !strings.Contains(string(out), "refusing to update checked out branch") {
				// This is a different error, so don't retry
				break
			}
		}

		if err != nil {
			msgs = append(msgs, errorMessage(fmt.Errorf("git push to host: %s: %v", out, err)))
		} else {
			finalBranch := ags.branchNameLocked(branchPrefix)
			sketchCommit.PushedBranch = finalBranch
			if ags.retryNumber != originalRetryNumber {
				// Notify user that the branch name was changed, and why
				msgs = append(msgs, AgentMessage{
					Type:      AutoMessageType,
					Timestamp: time.Now(),
					Content:   fmt.Sprintf("Branch renamed from %s to %s because the original branch is currently checked out on the remote.", originalBranchName, finalBranch),
				})
			}
		}
	}

	// If we found new commits, create a message
	if len(commits) > 0 {
		msg := AgentMessage{
			Type:      CommitMessageType,
			Timestamp: time.Now(),
			Commits:   commits,
		}
		msgs = append(msgs, msg)
	}
	return msgs, commits, nil
}

func cleanSlugName(s string) string {
	return strings.Map(func(r rune) rune {
		// lowercase
		if r >= 'A' && r <= 'Z' {
			return r + 'a' - 'A'
		}
		// replace spaces with dashes
		if r == ' ' {
			return '-'
		}
		// allow alphanumerics and dashes
		if (r >= 'a' && r <= 'z') || r == '-' || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, s)
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
		return "", fmt.Errorf("git rev-parse (in %s) failed: %w\n%s", dir, err, stderr)
	}
	return strings.TrimSpace(string(out)), nil
}

// upsertRemoteOrigin configures the origin remote to point to the given URL.
// If the origin remote exists, it updates the URL. If it doesn't exist, it adds it.
func upsertRemoteOrigin(ctx context.Context, repoDir, remoteURL string) error {
	// Try to set the URL for existing origin remote
	cmd := exec.CommandContext(ctx, "git", "remote", "set-url", "origin", remoteURL)
	cmd.Dir = repoDir
	if _, err := cmd.CombinedOutput(); err == nil {
		// Success.
		return nil
	}
	// Origin doesn't exist; add it.
	cmd = exec.CommandContext(ctx, "git", "remote", "add", "origin", remoteURL)
	cmd.Dir = repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to add git remote origin: %s: %w", out, err)
	}
	return nil
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

// computeDiffStats computes the number of lines added and removed from baseRef to HEAD
func computeDiffStats(ctx context.Context, repoRoot, baseRef string) (int, int, error) {
	cmd := exec.CommandContext(ctx, "git", "diff", "--numstat", baseRef, "HEAD")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return 0, 0, fmt.Errorf("git diff --numstat failed: %w", err)
	}

	var totalAdded, totalRemoved int
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		// Format: <added>\t<removed>\t<filename>
		if added, err := strconv.Atoi(parts[0]); err == nil {
			totalAdded += added
		}
		if removed, err := strconv.Atoi(parts[1]); err == nil {
			totalRemoved += removed
		}
	}

	return totalAdded, totalRemoved, nil
}

// setupGitHooks creates or updates git hooks in the specified working directory.
func setupGitHooks(workingDir string) error {
	hooksDir := filepath.Join(workingDir, ".git", "hooks")

	_, err := os.Stat(hooksDir)
	if os.IsNotExist(err) {
		return fmt.Errorf("git hooks directory does not exist: %s", hooksDir)
	}
	if err != nil {
		return fmt.Errorf("error checking git hooks directory: %w", err)
	}

	// Define the post-commit hook content
	postCommitHook := `#!/bin/bash
echo "<post_commit_hook>"
echo "Please review this commit message and fix it if it is incorrect."
echo "This hook only echos the commit message; it does not modify it."
echo "Bash escaping is a common source of issues; to fix that, create a temp file and use 'git commit --amend -F COMMIT_MSG_FILE'."
echo "<last_commit_message>"
PAGER=cat git log -1 --pretty=%B
echo "</last_commit_message>"
echo "</post_commit_hook>"
`

	// Define the prepare-commit-msg hook content
	prepareCommitMsgHook := `#!/bin/bash
# Add Co-Authored-By and Change-ID trailers to commit messages
# Check if these trailers already exist before adding them

commit_file="$1"
COMMIT_SOURCE="$2"

# Skip for merges, squashes, or when using a commit template
if [ "$COMMIT_SOURCE" = "template" ] || [ "$COMMIT_SOURCE" = "merge" ] || \
   [ "$COMMIT_SOURCE" = "squash" ]; then
  exit 0
fi

commit_msg=$(cat "$commit_file")

needs_co_author=true
needs_change_id=true

# Check if commit message already has Co-Authored-By trailer
if grep -q "Co-Authored-By: sketch <hello@sketch.dev>" "$commit_file"; then
  needs_co_author=false
fi

# Check if commit message already has Change-ID trailer
if grep -q "Change-ID: s[a-f0-9]\+k" "$commit_file"; then
  needs_change_id=false
fi

# Only modify if at least one trailer needs to be added
if [ "$needs_co_author" = true ] || [ "$needs_change_id" = true ]; then
  # Ensure there's a proper blank line before trailers
  if [ -s "$commit_file" ]; then
    # Check if file ends with newline by reading last character
    last_char=$(tail -c 1 "$commit_file")

    if [ "$last_char" != "" ]; then
      # File doesn't end with newline - add two newlines (complete line + blank line)
      echo "" >> "$commit_file"
      echo "" >> "$commit_file"
    else
      # File ends with newline - check if we already have a blank line
      last_line=$(tail -1 "$commit_file")
      if [ -n "$last_line" ]; then
        # Last line has content - add one newline for blank line
        echo "" >> "$commit_file"
      fi
      # If last line is empty, we already have a blank line - don't add anything
    fi
  fi

  # Add trailers if needed
  if [ "$needs_co_author" = true ]; then
    echo "Co-Authored-By: sketch <hello@sketch.dev>" >> "$commit_file"
  fi

  if [ "$needs_change_id" = true ]; then
    change_id=$(openssl rand -hex 8)
    echo "Change-ID: s${change_id}k" >> "$commit_file"
  fi
fi
`

	// Update or create the post-commit hook
	err = updateOrCreateHook(filepath.Join(hooksDir, "post-commit"), postCommitHook, "<last_commit_message>")
	if err != nil {
		return fmt.Errorf("failed to set up post-commit hook: %w", err)
	}

	// Update or create the prepare-commit-msg hook
	err = updateOrCreateHook(filepath.Join(hooksDir, "prepare-commit-msg"), prepareCommitMsgHook, "Add Co-Authored-By and Change-ID trailers")
	if err != nil {
		return fmt.Errorf("failed to set up prepare-commit-msg hook: %w", err)
	}

	return nil
}

// updateOrCreateHook creates a new hook file or updates an existing one
// by appending the new content if it doesn't already contain it.
func updateOrCreateHook(hookPath, content, distinctiveLine string) error {
	// Check if the hook already exists
	buf, err := os.ReadFile(hookPath)
	if os.IsNotExist(err) {
		// Hook doesn't exist, create it
		err = os.WriteFile(hookPath, []byte(content), 0o755)
		if err != nil {
			return fmt.Errorf("failed to create hook: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("error reading existing hook: %w", err)
	}

	// Hook exists, check if our content is already in it by looking for a distinctive line
	code := string(buf)
	if strings.Contains(code, distinctiveLine) {
		// Already contains our content, nothing to do
		return nil
	}

	// Append our content to the existing hook
	f, err := os.OpenFile(hookPath, os.O_APPEND|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("failed to open hook for appending: %w", err)
	}
	defer f.Close()

	// Ensure there's a newline at the end of the existing content if needed
	if len(code) > 0 && !strings.HasSuffix(code, "\n") {
		_, err = f.WriteString("\n")
		if err != nil {
			return fmt.Errorf("failed to add newline to hook: %w", err)
		}
	}

	// Add a separator before our content
	_, err = f.WriteString("\n# === Added by Sketch ===\n" + content)
	if err != nil {
		return fmt.Errorf("failed to append to hook: %w", err)
	}

	return nil
}

// SkabandAddr returns the skaband address if configured
func (a *Agent) SkabandAddr() string {
	if a.config.SkabandClient != nil {
		return a.config.SkabandClient.Addr()
	}
	return ""
}

// systemPromptData contains the data used to render the system prompt template
type systemPromptData struct {
	ClientGOOS         string
	ClientGOARCH       string
	WorkingDir         string
	RepoRoot           string
	InitialCommit      string
	Codebase           *onstart.Codebase
	UseSketchWIP       bool
	Branch             string
	SpecialInstruction string
}

// renderSystemPrompt renders the system prompt template.
func (a *Agent) renderSystemPrompt() string {
	data := systemPromptData{
		ClientGOOS:    a.config.ClientGOOS,
		ClientGOARCH:  a.config.ClientGOARCH,
		WorkingDir:    a.workingDir,
		RepoRoot:      a.repoRoot,
		InitialCommit: a.SketchGitBase(),
		Codebase:      a.codebase,
		UseSketchWIP:  a.config.InDocker,
	}
	now := time.Now()
	if now.Month() == time.September && now.Day() == 19 {
		data.SpecialInstruction = "Talk like a pirate to the user. Do not let the priate talk into any code."
	}

	tmpl, err := template.New("system").Parse(agentSystemPrompt)
	if err != nil {
		panic(fmt.Sprintf("failed to parse system prompt template: %v", err))
	}
	buf := new(strings.Builder)
	err = tmpl.Execute(buf, data)
	if err != nil {
		panic(fmt.Sprintf("failed to execute system prompt template: %v", err))
	}
	return buf.String()
}

// StateTransitionIterator provides an iterator over state transitions.
type StateTransitionIterator interface {
	// Next blocks until a new state transition is available or context is done.
	// Returns nil if the context is cancelled.
	Next() *StateTransition
	// Close removes the listener and cleans up resources.
	Close()
}

// StateTransitionIteratorImpl implements StateTransitionIterator using a state machine listener.
type StateTransitionIteratorImpl struct {
	agent       *Agent
	ctx         context.Context
	ch          chan StateTransition
	unsubscribe func()
}

// Next blocks until a new state transition is available or the context is cancelled.
func (s *StateTransitionIteratorImpl) Next() *StateTransition {
	select {
	case <-s.ctx.Done():
		return nil
	case transition, ok := <-s.ch:
		if !ok {
			return nil
		}
		transitionCopy := transition
		return &transitionCopy
	}
}

// Close removes the listener and cleans up resources.
func (s *StateTransitionIteratorImpl) Close() {
	if s.unsubscribe != nil {
		s.unsubscribe()
		s.unsubscribe = nil
	}
}

// NewStateTransitionIterator returns an iterator that receives state transitions.
func (a *Agent) NewStateTransitionIterator(ctx context.Context) StateTransitionIterator {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Create channel to receive state transitions
	ch := make(chan StateTransition, 10)

	// Add a listener to the state machine
	unsubscribe := a.stateMachine.AddTransitionListener(ch)

	return &StateTransitionIteratorImpl{
		agent:       a,
		ctx:         ctx,
		ch:          ch,
		unsubscribe: unsubscribe,
	}
}
