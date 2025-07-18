package bubbletea

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// BubbleTeaApp is the main application model that implements tea.Model
type BubbleTeaApp struct {
	agent   loop.CodingAgent
	httpURL string
	ctx     context.Context

	// Components
	chatView  UIComponent
	inputView UIComponent
	statusBar UIComponent

	// Message routing and queue management
	messageQueue *MessageQueue
	router       *MessageRouter

	// State management
	mu             sync.Mutex
	pushedBranches map[string]struct{}
	currentSlug    string
	titlePushed    bool

	// Agent state tracking
	currentState     string
	stateTransitions []string
	iteratorActive   bool

	// UI state
	width  int
	height int
	ready  bool
}

// BubbleTeaUI wraps the BubbleTeaApp and provides the external interface
type BubbleTeaUI struct {
	app     *BubbleTeaApp
	program *tea.Program
	agent   loop.CodingAgent // Direct reference to agent for easier access
}

// New creates a new BubbleTeaUI instance
func New(agent loop.CodingAgent, httpURL string) *BubbleTeaUI {
	app := &BubbleTeaApp{
		agent:          agent,
		httpURL:        httpURL,
		pushedBranches: make(map[string]struct{}),
		messageQueue:   NewMessageQueue(1000), // Buffer up to 1000 messages
		router:         NewMessageRouter(),
	}
	return &BubbleTeaUI{
		app:   app,
		agent: agent, // Store direct reference to agent
	}
}

// Run starts the Bubble Tea UI
func (ui *BubbleTeaUI) Run(ctx context.Context) error {
	// Store context for component access
	ui.app.ctx = ctx

	// Set up terminal title
	ui.pushTerminalTitle()
	ui.setTerminalTitle("sketch")

	// Ensure terminal state is restored on exit
	defer func() {
		ui.popTerminalTitle()
	}()

	// Initialize components
	if err := ui.initializeComponents(); err != nil {
		return fmt.Errorf("failed to initialize components: %w", err)
	}

	// Create a cancellable context for the program
	programCtx, cancelProgram := context.WithCancel(ctx)
	defer cancelProgram()

	// Set up context for all components
	ui.app.chatView.SetContext(programCtx)
	ui.app.inputView.SetContext(programCtx)
	ui.app.statusBar.SetContext(programCtx)

	// Create and start the program with the BubbleTeaApp as the model
	// Use AltScreen for full-screen mode and enable mouse support for scrolling
	ui.program = tea.NewProgram(
		ui.app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Set up graceful shutdown on context cancellation
	go func() {
		<-ctx.Done()
		// Give a short grace period for cleanup
		time.Sleep(100 * time.Millisecond)
		ui.program.Quit()
	}()

	// Start message processing in background
	go ui.processAgentMessages(programCtx)

	// Start state transition monitoring
	go ui.processStateTransitions(programCtx)

	// Set up panic recovery
	defer func() {
		if r := recover(); r != nil {
			// Log the panic
			fmt.Printf("Recovered from panic in Bubble Tea UI: %v\n", r)
			// Ensure terminal state is restored
			ui.popTerminalTitle()
		}
	}()

	// Run the program and handle errors
	model, err := ui.program.Run()
	if err != nil {
		return fmt.Errorf("bubble tea program error: %w", err)
	}

	// Check if the final model state indicates any issues
	if finalApp, ok := model.(*BubbleTeaApp); ok && finalApp != nil {
		// Perform any final cleanup or state checks
	}

	return nil
}

// initializeComponents sets up all UI components
func (ui *BubbleTeaUI) initializeComponents() error {
	// Create components if they don't exist
	if ui.app.chatView == nil {
		ui.app.chatView = NewMessagesComponent()
	}

	if ui.app.inputView == nil {
		ui.app.inputView = NewInputComponent()
	}

	if ui.app.statusBar == nil {
		ui.app.statusBar = NewStatusComponent()
	}

	// Set up agent and context references
	ui.app.chatView.SetAgent(ui.app.agent)
	ui.app.inputView.SetAgent(ui.app.agent)
	ui.app.statusBar.SetAgent(ui.app.agent)

	// Set up message routing
	ui.app.router.RegisterHandler("agent_message", ui.app.chatView.(MessageHandler))
	ui.app.router.RegisterHandler("tool_use", ui.app.chatView.(MessageHandler))
	ui.app.router.RegisterHandler("system_message", ui.app.chatView.(MessageHandler))

	// Set up input component with URL
	if inputComp, ok := ui.app.inputView.(*InputComponent); ok {
		inputComp.SetPrompt(ui.app.httpURL, false)
	}

	return nil
}

// processAgentMessages handles incoming messages from the agent
func (ui *BubbleTeaUI) processAgentMessages(ctx context.Context) {
	// Mark iterator as active
	ui.app.mu.Lock()
	ui.app.iteratorActive = true
	ui.app.mu.Unlock()

	// Create message iterator starting from index 0
	messageIterator := ui.app.agent.NewIterator(ctx, 0)

	defer func() {
		// Cleanup iterator and mark as inactive
		ui.cleanupIterator(messageIterator)
		ui.app.mu.Lock()
		ui.app.iteratorActive = false
		ui.app.mu.Unlock()
	}()

	// Create a buffer for batching messages
	messageBuffer := NewMessageBuffer(10)

	// Create a ticker for batched processing
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	// Process messages from the iterator
	for {
		select {
		case <-ctx.Done():
			// Context cancelled, exit gracefully
			return

		case <-ticker.C:
			// Process any buffered messages
			if messageBuffer.Size() > 0 {
				messages := messageBuffer.Get()
				messageBuffer.Clear()

				// Send all messages to the UI program
				for _, msg := range messages {
					if ui.program != nil {
						ui.program.Send(msg)
					}
				}
			}

		default:
			// Try to get next message from iterator
			resp := messageIterator.Next()
			if resp == nil {
				// Iterator exhausted, exit gracefully
				return
			}

			// Skip hidden messages
			if resp.HideOutput {
				continue
			}

			// Track state transitions if this is a state-related message
			ui.trackStateTransition(resp)

			// Buffer the message for batched processing
			messageBuffer.Add(agentMessageMsg{resp})

			// If buffer is getting full, process immediately
			if messageBuffer.Size() >= 5 {
				messages := messageBuffer.Get()
				messageBuffer.Clear()

				// Send all messages to the UI program
				for _, msg := range messages {
					if ui.program != nil {
						ui.program.Send(msg)
					}
				}
			}
		}
	}
}

// cleanupIterator performs cleanup for the message iterator
func (ui *BubbleTeaUI) cleanupIterator(iterator loop.MessageIterator) {
	if iterator != nil {
		iterator.Close()
	}
}

// trackStateTransition tracks state changes from agent messages
func (ui *BubbleTeaUI) trackStateTransition(msg *loop.AgentMessage) {
	ui.app.mu.Lock()
	defer ui.app.mu.Unlock()

	// Track state transitions based on message type and content
	switch msg.Type {
	case loop.AgentMessageType:
		// Agent is actively responding
		if ui.app.currentState != "responding" {
			ui.app.currentState = "responding"
			ui.app.stateTransitions = append(ui.app.stateTransitions, "responding")
		}
	case loop.ToolUseMessageType:
		// Agent is using tools
		if ui.app.currentState != "tool_use" {
			ui.app.currentState = "tool_use"
			ui.app.stateTransitions = append(ui.app.stateTransitions, "tool_use")
		}
	case loop.UserMessageType:
		// User is providing input
		if ui.app.currentState != "user_input" {
			ui.app.currentState = "user_input"
			ui.app.stateTransitions = append(ui.app.stateTransitions, "user_input")
		}
	case loop.ErrorMessageType:
		// Error state
		if ui.app.currentState != "error" {
			ui.app.currentState = "error"
			ui.app.stateTransitions = append(ui.app.stateTransitions, "error")
		}
	default:
		// Default idle state
		if ui.app.currentState != "idle" {
			ui.app.currentState = "idle"
			ui.app.stateTransitions = append(ui.app.stateTransitions, "idle")
		}
	}

	// Limit state transition history to prevent memory growth
	if len(ui.app.stateTransitions) > 100 {
		ui.app.stateTransitions = ui.app.stateTransitions[len(ui.app.stateTransitions)-100:]
	}
}

// processStateTransitions handles state machine transitions
func (ui *BubbleTeaUI) processStateTransitions(ctx context.Context) {
	// Create state transition iterator
	stateIterator := ui.app.agent.NewStateTransitionIterator(ctx)
	if stateIterator == nil {
		return
	}

	defer func() {
		// Cleanup iterator
		stateIterator.Close()
	}()

	// Process state transitions
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Continue processing
		}

		// Get next state transition
		transition := stateIterator.Next()
		if transition == nil {
			// Iterator exhausted or context cancelled
			return
		}

		// Update app state based on transition
		ui.app.mu.Lock()
		ui.app.currentState = transition.To.String()
		ui.app.stateTransitions = append(ui.app.stateTransitions, transition.To.String())

		// Limit state transition history
		if len(ui.app.stateTransitions) > 100 {
			ui.app.stateTransitions = ui.app.stateTransitions[len(ui.app.stateTransitions)-100:]
		}
		ui.app.mu.Unlock()

		// Update status component if available
		if ui.app.statusBar != nil {
			if statusComp, ok := ui.app.statusBar.(*StatusComponent); ok {
				statusComp.UpdateState(transition.To.String())
			}
		}
	}
}

// RestoreOldState cleans up the UI (compatibility with old interface)
func (ui *BubbleTeaUI) RestoreOldState() error {
	// Restore terminal title
	ui.popTerminalTitle()

	if ui.program != nil {
		ui.program.Quit()
	}
	return nil
}

// Terminal title management methods

// pushTerminalTitle pushes the current terminal title onto the title stack
func (ui *BubbleTeaUI) pushTerminalTitle() {
	fmt.Printf("\033[22;0t")
	ui.app.mu.Lock()
	ui.app.titlePushed = true
	ui.app.mu.Unlock()
}

// popTerminalTitle pops the terminal title from the title stack
func (ui *BubbleTeaUI) popTerminalTitle() {
	ui.app.mu.Lock()
	titlePushed := ui.app.titlePushed
	ui.app.mu.Unlock()

	if titlePushed {
		fmt.Printf("\033[23;0t")
		ui.app.mu.Lock()
		ui.app.titlePushed = false
		ui.app.mu.Unlock()
	}
}

// setTerminalTitle sets the terminal title
func (ui *BubbleTeaUI) setTerminalTitle(title string) {
	fmt.Printf("\033]0;%s\007", title)
}

// updateTitleWithSlug updates the terminal title with slug
func (ui *BubbleTeaUI) updateTitleWithSlug(slug string) {
	ui.app.mu.Lock()
	defer ui.app.mu.Unlock()

	ui.app.currentSlug = slug
	title := "sketch"
	if slug != "" {
		title = fmt.Sprintf("sketch: %s", slug)
	}
	ui.setTerminalTitle(title)
}

// Message types are now defined in message_types.go

// MessageQueue manages buffered message processing
type MessageQueue struct {
	mu       sync.RWMutex
	messages []Message
	maxSize  int
}

// NewMessageQueue creates a new message queue with specified capacity
func NewMessageQueue(maxSize int) *MessageQueue {
	return &MessageQueue{
		messages: make([]Message, 0, maxSize),
		maxSize:  maxSize,
	}
}

// Enqueue adds a message to the queue (thread-safe)
func (mq *MessageQueue) Enqueue(msg Message) bool {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	if len(mq.messages) >= mq.maxSize {
		// Remove oldest message to make room
		mq.messages = mq.messages[1:]
	}

	mq.messages = append(mq.messages, msg)
	return true
}

// Dequeue removes and returns the oldest message (thread-safe)
func (mq *MessageQueue) Dequeue() (Message, bool) {
	mq.mu.Lock()
	defer mq.mu.Unlock()

	if len(mq.messages) == 0 {
		return nil, false
	}

	msg := mq.messages[0]
	mq.messages = mq.messages[1:]
	return msg, true
}

// Len returns the current queue length (thread-safe)
func (mq *MessageQueue) Len() int {
	mq.mu.RLock()
	defer mq.mu.RUnlock()
	return len(mq.messages)
}

// Clear empties the queue (thread-safe)
func (mq *MessageQueue) Clear() {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	mq.messages = mq.messages[:0]
}

// MessageRouter handles routing messages to appropriate components
type MessageRouter struct {
	mu       sync.RWMutex
	handlers map[string][]MessageHandler
}

// NewMessageRouter creates a new message router
func NewMessageRouter() *MessageRouter {
	return &MessageRouter{
		handlers: make(map[string][]MessageHandler),
	}
}

// RegisterHandler registers a handler for a specific message type
func (mr *MessageRouter) RegisterHandler(messageType string, handler MessageHandler) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	if mr.handlers[messageType] == nil {
		mr.handlers[messageType] = make([]MessageHandler, 0)
	}
	mr.handlers[messageType] = append(mr.handlers[messageType], handler)
}

// RouteMessage routes a message to all registered handlers for its type
func (mr *MessageRouter) RouteMessage(msg UIMessage) []tea.Cmd {
	mr.mu.RLock()
	defer mr.mu.RUnlock()

	handlers, exists := mr.handlers[msg.Type()]
	if !exists {
		return nil
	}

	var cmds []tea.Cmd

	// Route to appropriate handlers based on message type
	switch typedMsg := msg.(type) {
	case agentMessageMsg:
		for _, handler := range handlers {
			if cmd := handler.HandleAgentMessage(typedMsg.message); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case toolUseMsg:
		for _, handler := range handlers {
			if cmd := handler.HandleToolUse(typedMsg.message); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	case systemMessageMsg:
		// Convert system message to agent message for handling
		agentMsg := &loop.AgentMessage{
			Type:    loop.ErrorMessageType,
			Content: typedMsg.content,
		}
		for _, handler := range handlers {
			if cmd := handler.HandleError(agentMsg); cmd != nil {
				cmds = append(cmds, cmd)
			}
		}
	}

	return cmds
}

// UnregisterHandler removes a handler for a specific message type
func (mr *MessageRouter) UnregisterHandler(messageType string, handler MessageHandler) {
	mr.mu.Lock()
	defer mr.mu.Unlock()

	handlers, exists := mr.handlers[messageType]
	if !exists {
		return
	}

	// Remove the handler from the slice
	for i, h := range handlers {
		if h == handler {
			mr.handlers[messageType] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}
}

// tea.Model interface implementation for BubbleTeaApp

// Init initializes the BubbleTeaApp model
func (app *BubbleTeaApp) Init() tea.Cmd {
	// Initialize any startup commands
	return nil
}

// Update handles all messages and updates the model state
func (app *BubbleTeaApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return app.handleKeyPress(msg)
	case agentMessageMsg:
		return app.handleAgentMessage(msg)
	case userInputMsg:
		return app.handleUserInput(msg)
	case commandMsg:
		return app.handleCommand(msg)
	case tea.WindowSizeMsg:
		return app.handleWindowResize(msg)
	}

	return app, nil
}

// View renders the main UI
func (app *BubbleTeaApp) View() string {
	if !app.ready {
		return "Initializing Bubble Tea UI..."
	}

	// Create a layout with all components
	var layout strings.Builder

	// Header with URL and help text
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("12")).
		Bold(true).
		PaddingLeft(1).
		PaddingRight(1).
		Width(app.width).
		Render(fmt.Sprintf("🌐 %s/\n💬 type 'help' for help", app.httpURL))

	layout.WriteString(header)
	layout.WriteString("\n\n")

	// Main chat view (if initialized)
	if app.chatView != nil {
		chatContent := app.chatView.View()
		layout.WriteString(chatContent)
		layout.WriteString("\n")
	}

	// Status bar (if initialized)
	if app.statusBar != nil {
		statusContent := app.statusBar.View()
		layout.WriteString(statusContent)
		layout.WriteString("\n")
	}

	// Input component (if initialized)
	if app.inputView != nil {
		inputContent := app.inputView.View()
		layout.WriteString(inputContent)
	}

	return layout.String()
}

// handleKeyPress processes keyboard input
func (app *BubbleTeaApp) handleKeyPress(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return app, tea.Quit
	}
	return app, nil
}

// handleAgentMessage processes messages from the coding agent
func (app *BubbleTeaApp) handleAgentMessage(msg agentMessageMsg) (tea.Model, tea.Cmd) {
	// Add message to queue for processing
	app.messageQueue.Enqueue(msg)

	// Route message to appropriate handlers
	var cmds []tea.Cmd

	// Determine message type and route accordingly
	switch msg.message.Type {
	case loop.ToolUseMessageType:
		toolMsg := toolUseMsg{message: msg.message}
		if routedCmds := app.router.RouteMessage(toolMsg); routedCmds != nil {
			cmds = append(cmds, routedCmds...)
		}
	default:
		// Route as regular agent message
		if routedCmds := app.router.RouteMessage(msg); routedCmds != nil {
			cmds = append(cmds, routedCmds...)
		}
	}

	// Return batch command if we have multiple commands
	if len(cmds) > 1 {
		return app, tea.Batch(cmds...)
	} else if len(cmds) == 1 {
		return app, cmds[0]
	}

	return app, nil
}

// handleUserInput processes user text input
func (app *BubbleTeaApp) handleUserInput(msg userInputMsg) (tea.Model, tea.Cmd) {
	// Send input to the agent
	if app.ctx != nil {
		app.agent.UserMessage(app.ctx, msg.input)
	}
	return app, nil
}

// handleCommand processes special commands
func (app *BubbleTeaApp) handleCommand(msg commandMsg) (tea.Model, tea.Cmd) {
	// Handle special commands like help, budget, etc.
	// This will be implemented in subsequent tasks
	return app, nil
}

// handleWindowResize handles terminal window resize events
func (app *BubbleTeaApp) handleWindowResize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	app.width = msg.Width
	app.height = msg.Height
	app.ready = true

	// Update component sizes based on new window dimensions
	// This will be implemented when we add proper layout management
	return app, nil
}

// Placeholder component constructors - these will be implemented in subsequent tasks
func NewMessagesComponent() UIComponent {
	// Placeholder implementation
	return &placeholderComponent{name: "Messages"}
}

func NewInputComponent() UIComponent {
	// Placeholder implementation
	return &placeholderComponent{name: "Input"}
}

// Placeholder component for testing
type placeholderComponent struct {
	name  string
	agent loop.CodingAgent
	ctx   context.Context
}

func (p *placeholderComponent) Init() tea.Cmd {
	return nil
}

func (p *placeholderComponent) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return p, nil
}

func (p *placeholderComponent) View() string {
	return fmt.Sprintf("[%s Component Placeholder]", p.name)
}

func (p *placeholderComponent) SetAgent(agent loop.CodingAgent) {
	p.agent = agent
}

func (p *placeholderComponent) SetContext(ctx context.Context) {
	p.ctx = ctx
}

// Implement MessageHandler interface for placeholder
func (p *placeholderComponent) HandleAgentMessage(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

func (p *placeholderComponent) HandleToolUse(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

func (p *placeholderComponent) HandleError(msg *loop.AgentMessage) tea.Cmd {
	return nil
}

// InputComponent extension methods
func (p *placeholderComponent) SetPrompt(url string, thinking bool) {
	// Placeholder implementation
}

// StatusComponent extension methods
func (p *placeholderComponent) UpdateState(state string) {
	// Placeholder implementation
}
