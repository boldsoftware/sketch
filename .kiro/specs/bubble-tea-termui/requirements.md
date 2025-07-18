# Requirements Document

## Introduction

This feature involves migrating the existing terminal UI implementation from the low-level `golang.org/x/term` package to the Bubble Tea framework. The current terminal UI provides a chat-like interface for interacting with Sketch's AI coding agent, displaying various message types (user, agent, tool use, git commits, errors, budget, auto-formatting, port monitoring), handling user input with special commands, and managing complex terminal state including concurrent message processing. The migration should preserve all existing functionality while leveraging Bubble Tea's component-based architecture for better maintainability, enhanced user experience, improved terminal handling, and better separation of concerns between UI rendering and business logic.

## Requirements

### Requirement 1

**User Story:** As a developer using Sketch, I want the terminal UI to maintain all current functionality after the Bubble Tea migration, so that my workflow remains uninterrupted.

#### Acceptance Criteria

1. WHEN the application starts THEN the terminal UI SHALL display the HTTP URL and help message exactly as before
2. WHEN I type commands like "help", "budget", "usage", "browser", "exit" THEN the system SHALL respond with identical functionality to the current implementation
3. WHEN I execute shell commands with "!" prefix THEN the system SHALL execute them and optionally send results to the LLM with "!!" prefix
4. WHEN I send messages to the AI agent THEN the system SHALL display agent responses with proper formatting and emoji indicators
5. WHEN the agent uses tools THEN the system SHALL display tool usage with appropriate icons and formatting using the existing template system
6. WHEN git commits are made THEN the system SHALL display commit information with GitHub links when available
7. WHEN I exit the application THEN the system SHALL display usage statistics and pushed branch information exactly as before

### Requirement 2

**User Story:** As a developer, I want the terminal UI to have improved visual organization and responsiveness, so that I can better track conversations and system status.

#### Acceptance Criteria

1. WHEN messages are displayed THEN the system SHALL organize them into distinct visual sections (chat messages, system messages, status information)
2. WHEN the agent is thinking THEN the system SHALL provide clear visual indicators in the prompt and interface
3. WHEN multiple message types are received simultaneously THEN the system SHALL handle them without visual conflicts or race conditions
4. WHEN the terminal is resized THEN the system SHALL adapt the layout gracefully without losing content
5. WHEN long messages are displayed THEN the system SHALL handle text wrapping and scrolling appropriately
6. WHEN tool execution is in progress THEN the system SHALL provide visual feedback about ongoing operations

### Requirement 3

**User Story:** As a developer, I want enhanced input handling and command completion, so that I can interact more efficiently with the terminal interface.

#### Acceptance Criteria

1. WHEN I type commands THEN the system SHALL provide input validation and visual feedback
2. WHEN I use arrow keys THEN the system SHALL support command history navigation
3. WHEN I press Tab THEN the system SHALL provide command completion for built-in commands
4. WHEN I type long messages THEN the system SHALL support multi-line input with proper visual indicators
5. WHEN I press Ctrl+C THEN the system SHALL gracefully cancel current operations without terminating the application
6. WHEN I use keyboard shortcuts THEN the system SHALL support common terminal shortcuts for navigation and editing

### Requirement 4

**User Story:** As a developer, I want the terminal UI to provide better status information and real-time updates, so that I can monitor system state and resource usage effectively.

#### Acceptance Criteria

1. WHEN the application is running THEN the system SHALL display current cost and budget information in a status bar
2. WHEN network operations are in progress THEN the system SHALL show connection status and activity indicators
3. WHEN the agent state changes THEN the system SHALL update status indicators in real-time
4. WHEN tool calls are outstanding THEN the system SHALL display a list of pending operations
5. WHEN errors occur THEN the system SHALL highlight them distinctly from normal messages
6. WHEN the session has been running for extended periods THEN the system SHALL display session duration and activity metrics

### Requirement 5

**User Story:** As a developer, I want the terminal UI architecture to be maintainable and extensible, so that future enhancements can be implemented efficiently.

#### Acceptance Criteria

1. WHEN new message types are added THEN the system SHALL support them through a pluggable component architecture
2. WHEN new commands are implemented THEN the system SHALL integrate them without modifying core UI logic
3. WHEN UI themes or styling changes are needed THEN the system SHALL support them through configuration
4. WHEN debugging is required THEN the system SHALL provide clear separation between UI state and business logic
5. WHEN testing is performed THEN the system SHALL support unit testing of individual UI components
6. WHEN performance optimization is needed THEN the system SHALL allow profiling and optimization of rendering components

### Requirement 6

**User Story:** As a developer, I want the terminal UI to handle concurrent operations safely, so that the interface remains stable under heavy load.

#### Acceptance Criteria

1. WHEN multiple goroutines send messages simultaneously THEN the system SHALL handle them without race conditions
2. WHEN the agent iterator provides rapid message updates THEN the system SHALL process them without blocking the UI
3. WHEN user input occurs during agent processing THEN the system SHALL queue inputs appropriately
4. WHEN the application shuts down THEN the system SHALL wait for all pending operations to complete gracefully
5. WHEN context cancellation occurs THEN the system SHALL clean up resources and restore terminal state properly
6. WHEN system signals are received THEN the system SHALL handle them appropriately without corrupting the display

### Requirement 7

**User Story:** As a developer, I want the terminal UI to properly handle all agent message types and state transitions, so that I have complete visibility into the agent's operations.

#### Acceptance Criteria

1. WHEN the agent sends UserMessageType messages THEN the system SHALL display them with user emoji (🦸) and proper formatting
2. WHEN the agent sends AgentMessageType messages THEN the system SHALL display them with agent emoji (🕴️) and handle thinking indicators
3. WHEN the agent sends ToolUseMessageType messages THEN the system SHALL render them using the comprehensive tool template system with appropriate icons
4. WHEN the agent sends CommitMessageType messages THEN the system SHALL display git commit information with hash, subject, pushed branch, and GitHub links
5. WHEN the agent sends ErrorMessageType messages THEN the system SHALL highlight them with error styling (❌) and proper visibility
6. WHEN the agent sends BudgetMessageType messages THEN the system SHALL display budget information with money emoji (💰) and cost details
7. WHEN the agent sends AutoMessageType messages THEN the system SHALL show automated notifications (🧐) for formatting and code review
8. WHEN the agent sends CompactMessageType messages THEN the system SHALL display conversation compaction notifications (📜)
9. WHEN the agent sends PortMessageType messages THEN the system SHALL show port monitoring events (🔌)

### Requirement 8

**User Story:** As a developer, I want the terminal UI to integrate with the agent's state machine, so that I can understand what the agent is currently doing.

#### Acceptance Criteria

1. WHEN the agent transitions to StateWaitingForUserInput THEN the system SHALL update the prompt to indicate readiness for input
2. WHEN the agent transitions to StateSendingToLLM THEN the system SHALL show loading indicators for LLM communication
3. WHEN the agent transitions to StateProcessingLLMResponse THEN the system SHALL indicate that the agent is processing the response
4. WHEN the agent transitions to StateToolUseRequested THEN the system SHALL show that tools are being prepared for execution
5. WHEN the agent transitions to StateRunningTool THEN the system SHALL display which tools are currently executing
6. WHEN the agent transitions to StateCheckingGitCommits THEN the system SHALL indicate git operations are in progress
7. WHEN the agent transitions to StateCancelled THEN the system SHALL clearly show that operations were cancelled
8. WHEN the agent transitions to StateError THEN the system SHALL prominently display error states
9. WHEN the agent transitions to StateBudgetExceeded THEN the system SHALL show budget warnings and options

### Requirement 9

**User Story:** As a developer, I want the terminal UI to support all existing special commands and shell integration, so that my current workflow is preserved.

#### Acceptance Criteria

1. WHEN I type "help" or "?" THEN the system SHALL display the complete help message with all available commands
2. WHEN I type "budget" THEN the system SHALL show the original budget configuration with max cost
3. WHEN I type "usage" or "cost" THEN the system SHALL display detailed token usage, response count, wall time, and total cost
4. WHEN I type "browser", "open", or "b" THEN the system SHALL attempt to open the conversation URL in a browser
5. WHEN I type "stop", "cancel", or "abort" THEN the system SHALL cancel the current agent operation
6. WHEN I type "bye", "exit", "q", or "quit" THEN the system SHALL display final statistics and gracefully shut down
7. WHEN I type "!command" THEN the system SHALL execute the shell command and display output
8. WHEN I type "!!command" THEN the system SHALL execute the shell command and send results to the LLM
9. WHEN I type "panic" THEN the system SHALL trigger a panic for debugging purposes

### Requirement 10

**User Story:** As a developer, I want the terminal UI to properly handle the comprehensive tool template system, so that all tool executions are clearly represented.

#### Acceptance Criteria

1. WHEN the "think" tool is used THEN the system SHALL display the brain emoji (🧠) with thoughts content
2. WHEN the "bash" tool is used THEN the system SHALL show terminal emoji (🖥️) with command, background (🥷), and slow (🐢) indicators
3. WHEN the "patch" tool is used THEN the system SHALL display keyboard emoji (⌨️) with the file path
4. WHEN browser tools are used THEN the system SHALL show appropriate emojis for navigate (🌐), click (🖱️), type (⌨️), wait (⏳), etc.
5. WHEN the "codereview" tool is used THEN the system SHALL display bug emoji (🐛) with slow operation warning
6. WHEN the "multiplechoice" tool is used THEN the system SHALL show question (📝) with formatted response options
7. WHEN the "set-slug" tool is used THEN the system SHALL display snail emoji (🐌) with the slug value
8. WHEN unknown tools are used THEN the system SHALL fall back to generic tool emoji (🛠️) with tool name and input
9. WHEN tool errors occur THEN the system SHALL display the wavy line indicator (〰️) to show errors

### Requirement 11

**User Story:** As a developer, I want the terminal UI to handle terminal title management and window state, so that I have proper context about my session.

#### Acceptance Criteria

1. WHEN the application starts THEN the system SHALL set the terminal title to "sketch"
2. WHEN a slug is set THEN the system SHALL update the terminal title to "sketch: {slug}"
3. WHEN the application starts THEN the system SHALL push the current terminal title onto the title stack
4. WHEN the application exits THEN the system SHALL restore the original terminal title from the stack
5. WHEN terminal resize events occur THEN the system SHALL handle SIGWINCH signals and update display dimensions
6. WHEN the terminal is not a TTY THEN the system SHALL provide appropriate error messages
7. WHEN terminal state restoration fails THEN the system SHALL handle errors gracefully without crashing