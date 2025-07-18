# Implementation Plan

- [x] 1. Set up project structure and dependencies

  - Add Bubble Tea framework dependencies to go.mod
  - Create internal package structure for components
  - Set up basic project organization with proper imports
  - _Requirements: 1.3, 5.1_

- [x] 2. Implement core Bubble Tea application model

  - [x] 2.1 Create BubbleTeaApp struct with tea.Model interface

    - Implement Init() tea.Cmd method for application initialization
    - Implement Update(tea.Msg) (tea.Model, tea.Cmd) method for message handling
    - Implement View() string method for rendering
    - _Requirements: 1.1, 6.1_

  - [x] 2.2 Implement message routing and queue management

    - Create UIMessage types and routing logic
    - Implement buffered channels for message processing
    - Add concurrent message handling without race conditions
    - _Requirements: 6.1, 6.2_

  - [x] 2.3 Integrate with agent iterator and state machine
    - Connect to loop.MessageIterator for agent messages
    - Connect to loop.StateTransitionIterator for state changes
    - Implement proper iterator lifecycle management
    - _Requirements: 8.1, 8.2, 8.3_

- [x] 3. Create MessagesComponent for chat display

  - [x] 3.1 Implement basic message display structure

    - Create DisplayMessage struct with all message types
    - Implement viewport.Model integration for scrolling
    - Add message storage and retrieval logic
    - _Requirements: 7.1, 7.2, 7.3_

  - [x] 3.2 Implement comprehensive message type rendering

    - Add rendering for UserMessageType with user emoji (🦸)
    - Add rendering for AgentMessageType with agent emoji (🕴️) and thinking indicators
    - Add rendering for ErrorMessageType with error styling (❌)
    - Add rendering for BudgetMessageType with money emoji (💰)
    - Add rendering for AutoMessageType with auto emoji (🧐)
    - Add rendering for CompactMessageType with scroll emoji (📜)
    - Add rendering for PortMessageType with port emoji (🔌)
    - _Requirements: 7.1, 7.2, 7.3, 7.4, 7.5, 7.6, 7.7, 7.8, 7.9_

  - [x] 3.3 Implement git commit message rendering
    - Add rendering for CommitMessageType with commit details
    - Display commit hash, subject, and pushed branch information
    - Integrate GitHub link generation and display
    - _Requirements: 7.4_

- [x] 4. Create ToolTemplateRenderer for tool display

  - [x] 4.1 Port existing tool template system to Bubble Tea

    - Migrate toolUseTemplTxt template to new renderer
    - Implement template execution with proper error handling
    - Add emoji mapping and formatting logic
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9_

  - [x] 4.2 Implement comprehensive tool rendering support
    - Add rendering for "think" tool with brain emoji (🧠)
    - Add rendering for "bash" tool with terminal emoji (🖥️) and indicators
    - Add rendering for "patch" tool with keyboard emoji (⌨️)
    - Add rendering for browser tools with appropriate emojis
    - Add rendering for "codereview" tool with bug emoji (🐛)
    - Add rendering for "multiplechoice" tool with question emoji (📝)
    - Add rendering for "set-slug" tool with snail emoji (🐌)
    - Add fallback rendering for unknown tools with generic emoji (🛠️)
    - Add error indicator rendering with wavy line (〰️)
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5, 10.6, 10.7, 10.8, 10.9_

- [ ] 5. Fix compilation issues and type errors

  - [x] 5.1 Fix loop package type references

    - Update DisplayMessage.Type to use loop.CodingAgentMessageType instead of loop.MessageType
    - Update DisplayMessage.Commits to use []loop.GitCommit instead of []loop.Commit
    - Fix all message type constants to match loop package definitions
    - _Requirements: 1.1, 7.1_

  - [x] 5.2 Fix deprecated viewport methods

    - Replace viewport.LineUp with viewport.ScrollUp
    - Replace viewport.LineDown with viewport.ScrollDown
    - Replace viewport.HalfViewUp with viewport.HalfPageUp
    - Replace viewport.HalfViewDown with viewport.HalfPageDown
    - _Requirements: 2.1_

  - [ ] 5.3 Fix BubbleTeaUI agent access
    - Add agent field to BubbleTeaUI struct or fix agent access pattern
    - Ensure proper agent reference passing between components
    - _Requirements: 1.1, 8.1_

- [x] 6. Create InputComponent for user interaction

  - [x] 6.1 Implement basic input handling with textinput.Model

    - Set up textinput component with proper styling
    - Implement input capture and processing
    - Add prompt display and management
    - _Requirements: 3.1, 3.4_

  - [x] 6.2 Implement command history and navigation

    - Add command history storage and retrieval
    - Implement arrow key navigation through history
    - Add history persistence and management
    - _Requirements: 3.2_

  - [x] 6.3 Implement special command processing

    - Add CommandProcessor struct for command handling
    - Implement help command with complete command list
    - Implement budget command with budget display
    - Implement usage/cost command with detailed statistics
    - Implement browser/open command with URL opening
    - Implement stop/cancel/abort command with operation cancellation
    - Implement exit/quit command with graceful shutdown
    - Implement shell command execution with ! and !! prefixes
    - Implement panic command for debugging
    - _Requirements: 9.1, 9.2, 9.3, 9.4, 9.5, 9.6, 9.7, 9.8, 9.9_

  - [x] 6.4 Add advanced input features
    - Implement Tab completion for built-in commands
    - Add multi-line input support with visual indicators
    - Implement Ctrl+C handling for operation cancellation
    - Add keyboard shortcuts for common operations
    - _Requirements: 3.3, 3.5, 3.6_

- [ ] 7. Create StatusComponent for real-time information

  - [x] 7.1 Implement budget and usage display

    - Add real-time budget information display
    - Show current cost vs maximum budget
    - Display token usage statistics
    - _Requirements: 4.1_

  - [x] 7.2 Implement agent state display

    - Show current agent state from state machine
    - Display state transition information
    - Add visual indicators for different states
    - _Requirements: 4.2, 8.1, 8.2, 8.3, 8.4, 8.5, 8.6, 8.7, 8.8, 8.9_

  - [x] 7.3 Implement operation monitoring
    - Display outstanding LLM calls
    - Show pending tool executions
    - Add network activity indicators
    - Display session duration and metrics
    - _Requirements: 4.3, 4.4, 4.5, 4.6_

- [x] 8. Implement terminal state management

  - [x] 8.1 Port terminal title management

    - Implement terminal title setting and restoration
    - Add title stack management (push/pop)
    - Handle slug-based title updates
    - _Requirements: 11.1, 11.2, 11.3, 11.4_

  - [x] 8.2 Implement terminal resize handling

    - Add SIGWINCH signal handling
    - Update component dimensions on resize
    - Ensure proper layout recalculation
    - _Requirements: 11.5_

  - [x] 8.3 Add terminal state validation and error handling
    - Implement TTY detection and validation
    - Add graceful error handling for non-TTY environments
    - Ensure proper terminal state restoration on exit
    - _Requirements: 11.6, 11.7_

- [x] 9. Integrate components into main TermUI interface

  - [x] 9.1 Implement TermUI wrapper for backward compatibility

    - Create TermUI struct that wraps BubbleTeaApp
    - Implement New() constructor with identical signature
    - Ensure all public methods maintain identical behavior
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7_

  - [x] 9.2 Implement message bridging between old and new APIs

    - Bridge HandleToolUse() calls to new component system
    - Bridge AppendChatMessage() calls to MessagesComponent
    - Bridge AppendSystemMessage() calls to MessagesComponent
    - Ensure thread-safe message passing
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7_

  - [x] 9.3 Implement Run() method with Bubble Tea program
    - Set up tea.Program with proper options
    - Handle context cancellation and cleanup
    - Ensure graceful shutdown with pending message processing
    - Implement proper error handling and recovery
    - _Requirements: 6.4, 6.5, 6.6_

- [x] 10. Add styling and visual enhancements

  - [x] 10.1 Implement comprehensive styling system

    - Create lipgloss styles for all message types
    - Add color schemes and visual hierarchy
    - Implement responsive layout adjustments
    - _Requirements: 2.1, 2.2, 2.3_

  - [x] 10.2 Add visual feedback and indicators

    - Implement thinking indicators and loading states
    - Add progress indicators for long operations
    - Create visual separators and organization
    - _Requirements: 2.1, 2.2, 2.4, 2.5_

  - [x] 10.3 Implement enhanced message organization
    - Add visual grouping for related messages
    - Implement message filtering and search capabilities
    - Add timestamp display and formatting
    - _Requirements: 2.1, 2.3, 2.6_

- [ ] 11. Implement error handling and recovery

  - [x] 11.1 Create comprehensive error handling system

    - Implement ErrorHandler with categorized error types
    - Add graceful degradation for rendering failures
    - Implement error display with clear user messaging
    - _Requirements: 4.5_

  - [ ] 11.2 Add recovery mechanisms
    - Implement state recovery for component failures
    - Add automatic retry logic for transient errors
    - Ensure terminal state restoration even during errors
    - _Requirements: 6.5, 6.6_

- [ ] 12. Add performance optimizations

  - [ ] 12.1 Implement efficient rendering

    - Add lazy rendering for large message histories
    - Implement message caching to avoid re-computation
    - Add viewport optimization for smooth scrolling
    - _Requirements: 2.4, 2.5_

  - [ ] 12.2 Implement memory management
    - Add configurable message history limits
    - Implement efficient message storage and cleanup
    - Add resource cleanup for iterators and channels
    - _Requirements: 6.4_

- [ ] 13. Create comprehensive test suite

  - [ ] 13.1 Implement unit tests for all components

    - Create tests for BubbleTeaApp model logic
    - Add tests for MessagesComponent rendering and state
    - Create tests for InputComponent command processing
    - Add tests for StatusComponent display logic
    - Create tests for ToolTemplateRenderer formatting
    - _Requirements: 5.5_

  - [ ] 13.2 Implement integration tests

    - Create mock agent implementations for testing
    - Add tests for message flow and routing
    - Implement tests for concurrent operation handling
    - Add tests for error scenarios and recovery
    - _Requirements: 5.5, 6.1, 6.2_

  - [ ] 13.3 Add performance and stress tests
    - Create tests for high message volume scenarios
    - Add tests for memory usage and cleanup
    - Implement tests for terminal resize and state management
    - _Requirements: 6.1, 6.2, 6.4_

- [ ] 14. Final integration and validation

  - [ ] 14.1 Perform end-to-end testing with real agent

    - Test complete workflow with actual agent interactions
    - Validate all message types and tool executions
    - Ensure proper state transitions and display
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7_

  - [ ] 14.2 Validate backward compatibility

    - Ensure existing integrations continue to work
    - Test all public API methods for identical behavior
    - Validate terminal state management and cleanup
    - _Requirements: 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7_

  - [ ] 14.3 Performance validation and optimization
    - Profile memory usage and rendering performance
    - Optimize any performance bottlenecks discovered
    - Validate smooth operation under various load conditions
    - _Requirements: 2.4, 2.5, 6.1, 6.2_
