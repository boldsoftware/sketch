# Design Document

## Overview

This design outlines the migration of Sketch's terminal UI from the low-level `golang.org/x/term` package to the Bubble Tea framework. The new architecture will maintain all existing functionality while providing better separation of concerns, improved maintainability, and enhanced user experience through Bubble Tea's component-based model-view-update (MVU) pattern.

The design preserves the existing `TermUI` interface for backward compatibility while completely reimplementing the internal architecture using Bubble Tea components. This allows for a seamless migration without breaking existing integrations.

## Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Main Application                         │
└─────────────────────┬───────────────────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────────────────┐
│                    TermUI (Public API)                         │
│  - New() *TermUI                                               │
│  - Run(ctx) error                                              │
│  - HandleToolUse(*AgentMessage)                                │
│  - AppendChatMessage(chatMessage)                              │
│  - AppendSystemMessage(string, ...any)                        │
│  - RestoreOldState() error                                     │
└─────────────────────┬───────────────────────────────────────────┘
                      │
┌─────────────────────▼───────────────────────────────────────────┐
│                 BubbleTeaApp (Internal)                        │
│  - Implements tea.Model interface                              │
│  - Coordinates all UI components                               │
│  - Manages application state and message routing               │
└─────────────────────┬───────────────────────────────────────────┘
                      │
        ┌─────────────┼─────────────┐
        │             │             │
┌───────▼──────┐ ┌────▼────┐ ┌──────▼──────┐
│   Messages   │ │  Input  │ │   Status    │
│  Component   │ │Component│ │  Component  │
└──────────────┘ └─────────┘ └─────────────┘
```

### Component Architecture

The Bubble Tea implementation will be organized into several key components:

1. **BubbleTeaApp**: Main application model that coordinates all components
2. **MessagesComponent**: Handles display of chat messages, tool outputs, and system messages
3. **InputComponent**: Manages user input, command history, and special command processing
4. **StatusComponent**: Displays real-time status information (budget, state, outstanding operations)
5. **ToolTemplateRenderer**: Renders tool usage with appropriate formatting and emojis

## Components and Interfaces

### BubbleTeaApp Model

The main Bubble Tea application model that implements the `tea.Model` interface:

```go
type BubbleTeaApp struct {
    // Core dependencies
    agent   loop.CodingAgent
    httpURL string
    
    // UI Components
    messages *MessagesComponent
    input    *InputComponent
    status   *StatusComponent
    
    // State management
    width, height    int
    terminalTitle    string
    titlePushed      bool
    currentSlug      string
    pushedBranches   map[string]struct{}
    
    // Message processing
    messageQueue     chan UIMessage
    systemQueue      chan string
    
    // Lifecycle management
    ctx              context.Context
    cancel           context.CancelFunc
    messageWaitGroup sync.WaitGroup
    
    // Agent integration
    agentIterator    loop.MessageIterator
    stateIterator    loop.StateTransitionIterator
    
    // Terminal state
    originalTermState *term.State
}

type UIMessage struct {
    Type    UIMessageType
    Content interface{}
}

type UIMessageType int

const (
    UIChatMessage UIMessageType = iota
    UISystemMessage
    UIAgentStateChange
    UITerminalResize
    UIShutdown
)
```

### MessagesComponent

Handles the display and scrolling of all message types:

```go
type MessagesComponent struct {
    // Message storage and display
    messages     []DisplayMessage
    viewport     viewport.Model
    
    // Rendering configuration
    toolRenderer *ToolTemplateRenderer
    
    // Styling
    userStyle    lipgloss.Style
    agentStyle   lipgloss.Style
    systemStyle  lipgloss.Style
    errorStyle   lipgloss.Style
    toolStyle    lipgloss.Style
}

type DisplayMessage struct {
    Type      MessageDisplayType
    Content   string
    Timestamp time.Time
    Sender    string
    Thinking  bool
    
    // Tool-specific fields
    ToolName   string
    ToolInput  string
    ToolResult string
    ToolError  bool
    
    // Git commit fields
    Commits []*loop.GitCommit
    
    // Styling hints
    Style lipgloss.Style
}

type MessageDisplayType int

const (
    DisplayChat MessageDisplayType = iota
    DisplaySystem
    DisplayTool
    DisplayCommit
    DisplayError
    DisplayBudget
    DisplayAuto
    DisplayCompact
    DisplayPort
)
```

### InputComponent

Manages user input with command history and special command processing:

```go
type InputComponent struct {
    // Input handling
    textInput    textinput.Model
    
    // Command history
    history      []string
    historyIndex int
    
    // Command processing
    commandProcessor *CommandProcessor
    
    // State
    prompt       string
    thinking     bool
    multiLine    bool
    
    // Styling
    promptStyle  lipgloss.Style
    inputStyle   lipgloss.Style
}

type CommandProcessor struct {
    agent   loop.CodingAgent
    httpURL string
}
```

### StatusComponent

Displays real-time status information:

```go
type StatusComponent struct {
    // Status information
    budget           conversation.Budget
    usage            conversation.CumulativeUsage
    agentState       string
    outstandingCalls []string
    
    // Display configuration
    showBudget       bool
    showState        bool
    showOperations   bool
    
    // Styling
    budgetStyle      lipgloss.Style
    stateStyle       lipgloss.Style
    operationsStyle  lipgloss.Style
}
```

### ToolTemplateRenderer

Renders tool usage with the existing template system:

```go
type ToolTemplateRenderer struct {
    template *template.Template
    
    // Emoji and formatting configuration
    emojiMap map[string]string
    
    // Styling
    toolStyle    lipgloss.Style
    errorStyle   lipgloss.Style
}
```

## Data Models

### Message Flow Architecture

The system processes messages through several channels and queues:

1. **Agent Messages**: Received via `loop.MessageIterator` from the agent
2. **User Input**: Captured by the input component and processed
3. **System Messages**: Generated internally for status updates and errors
4. **State Transitions**: Received via `loop.StateTransitionIterator` for agent state changes

### Message Processing Pipeline

```
Agent Iterator ──┐
                 │
State Iterator ──┼──► Message Router ──► Component Updates ──► UI Render
                 │
User Input ──────┘
```

### State Management

The application maintains several types of state:

1. **UI State**: Component dimensions, focus, scroll positions
2. **Message State**: Message history, display formatting, filtering
3. **Agent State**: Current agent status, outstanding operations, budget information
4. **Terminal State**: Title, dimensions, raw terminal state for restoration

## Error Handling

### Error Categories

1. **Terminal Errors**: TTY detection, raw mode setup, state restoration
2. **Agent Communication Errors**: Iterator failures, message parsing errors
3. **Rendering Errors**: Template execution, styling application
4. **Input Processing Errors**: Command parsing, shell execution failures

### Error Recovery Strategies

1. **Graceful Degradation**: Fall back to simpler rendering when complex operations fail
2. **Error Display**: Show errors prominently in the message area with clear formatting
3. **State Recovery**: Attempt to restore terminal state even when other operations fail
4. **Logging**: Comprehensive error logging for debugging while maintaining user experience

### Error Handling Implementation

```go
type ErrorHandler struct {
    logger *slog.Logger
}

func (e *ErrorHandler) HandleTerminalError(err error) tea.Cmd
func (e *ErrorHandler) HandleAgentError(err error) tea.Cmd
func (e *ErrorHandler) HandleRenderError(err error) tea.Cmd
func (e *ErrorHandler) HandleInputError(err error) tea.Cmd
```

## Testing Strategy

### Unit Testing

1. **Component Testing**: Test individual components (Messages, Input, Status) in isolation
2. **Message Processing**: Test message routing, formatting, and display logic
3. **Command Processing**: Test special command handling and shell integration
4. **Template Rendering**: Test tool template system with various tool types

### Integration Testing

1. **Agent Integration**: Test with mock agent implementations
2. **Terminal Integration**: Test terminal state management and restoration
3. **Concurrent Operations**: Test message processing under concurrent load
4. **Error Scenarios**: Test error handling and recovery

### Testing Architecture

```go
// Test utilities
type MockAgent struct {
    messages chan *loop.AgentMessage
    states   chan loop.StateTransition
}

type TestTerminal struct {
    input  chan string
    output []string
}

// Component test helpers
func NewTestMessagesComponent() *MessagesComponent
func NewTestInputComponent() *InputComponent
func NewTestStatusComponent() *StatusComponent
```

### Test Coverage Goals

- **Component Logic**: 90%+ coverage for component update and view logic
- **Message Processing**: 95%+ coverage for message routing and formatting
- **Command Processing**: 90%+ coverage for special commands and shell integration
- **Error Handling**: 85%+ coverage for error scenarios and recovery

## Performance Considerations

### Rendering Optimization

1. **Lazy Rendering**: Only render visible messages in the viewport
2. **Message Caching**: Cache rendered message content to avoid re-computation
3. **Efficient Updates**: Use Bubble Tea's efficient update model to minimize re-renders
4. **Memory Management**: Implement message history limits to prevent unbounded growth

### Concurrency Management

1. **Non-blocking UI**: Ensure UI updates never block on agent operations
2. **Message Queuing**: Use buffered channels to handle message bursts
3. **Graceful Shutdown**: Implement proper cleanup for all goroutines and resources

### Memory Usage

1. **Message History Limits**: Implement configurable limits for message retention
2. **Component State**: Minimize component state size and complexity
3. **Resource Cleanup**: Proper cleanup of iterators, channels, and terminal state

## Migration Strategy

### Phase 1: Core Infrastructure

1. Implement basic Bubble Tea application structure
2. Create component interfaces and basic implementations
3. Implement message routing and basic display
4. Ensure terminal state management works correctly

### Phase 2: Feature Parity

1. Implement all message types and tool template rendering
2. Add special command processing and shell integration
3. Implement status display and real-time updates
4. Add agent state integration and display

### Phase 3: Enhancement and Polish

1. Add enhanced input features (command completion, multi-line support)
2. Implement advanced styling and theming
3. Add performance optimizations and memory management
4. Comprehensive testing and bug fixes

### Backward Compatibility

The migration maintains the existing `TermUI` public interface:

```go
// Existing interface preserved
func New(agent loop.CodingAgent, httpURL string) *TermUI
func (ui *TermUI) Run(ctx context.Context) error
func (ui *TermUI) HandleToolUse(resp *loop.AgentMessage)
func (ui *TermUI) AppendChatMessage(msg chatMessage)
func (ui *TermUI) AppendSystemMessage(fmtString string, args ...any)
func (ui *TermUI) RestoreOldState() error
```

The internal implementation will be completely replaced with Bubble Tea components while maintaining identical external behavior.

## Dependencies

### New Dependencies

- `github.com/charmbracelet/bubbletea`: Core Bubble Tea framework
- `github.com/charmbracelet/lipgloss`: Styling and layout
- `github.com/charmbracelet/bubbles/textinput`: Input component
- `github.com/charmbracelet/bubbles/viewport`: Scrollable message display

### Existing Dependencies (Preserved)

- `golang.org/x/term`: Terminal state management and detection
- `sketch.dev/loop`: Agent interface and message types
- `github.com/fatih/color`: Color support (may be replaced by lipgloss)
- Standard library packages for templates, JSON, context management

### Dependency Management

All new dependencies are well-maintained, actively developed, and have stable APIs. The migration will include proper dependency version pinning and compatibility testing.