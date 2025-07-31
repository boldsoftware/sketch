import { html } from "lit";
import { property, state } from "lit/decorators.js";
import { ConnectionStatus, DataManager } from "../data";
import { AgentMessage, State, Usage } from "../types";
import { aggregateAgentMessages } from "./aggregateAgentMessages";
import { SketchTailwindElement } from "./sketch-tailwind-element";
import { ThemeService } from "./theme-service";

import "./sketch-chat-input";
import "./sketch-container-status";

import "./sketch-diff2-view";
import { SketchDiff2View } from "./sketch-diff2-view";
import { DefaultGitDataService } from "./git-data-service";
import "./sketch-monaco-view";
import "./sketch-call-status";
import "./sketch-push-button";
import "./sketch-terminal";
import "./sketch-timeline";
import "./sketch-view-mode-select";
import "./sketch-theme-toggle";
import "./sketch-todo-panel";

import { createRef } from "lit/directives/ref.js";
import { SketchChatInput } from "./sketch-chat-input";

type ViewMode = "chat" | "diff2" | "terminal";

// Base class for sketch app shells - contains shared logic
export abstract class SketchAppShellBase extends SketchTailwindElement {
  // Current view mode (chat, diff, terminal)
  @state()
  viewMode: ViewMode = "chat";

  // Current commit hash for diff view
  @state()
  currentCommitHash: string = "";

  // Last commit information
  @state()
  @state()
  latestUsage: Usage | null = null;

  // Reference to the container status element
  containerStatusElement: any = null;

  // Note: CSS styles have been converted to Tailwind classes applied directly to HTML elements
  // since this component now extends SketchTailwindElement which disables shadow DOM

  // Override createRenderRoot to apply host styles for proper sizing while still using light DOM
  createRenderRoot() {
    // Use light DOM like SketchTailwindElement but still apply host styles
    const style = document.createElement("style");
    style.textContent = `
      sketch-app-shell {
        display: block;
        width: 100%;
        height: 100vh;
        max-width: 100%;
        box-sizing: border-box;
        overflow: hidden;
      }
    `;

    // Add the style to the document head if not already present
    if (!document.head.querySelector("style[data-sketch-app-shell]")) {
      style.setAttribute("data-sketch-app-shell", "");
      document.head.appendChild(style);
    }

    return this;
  }

  // Header bar: Network connection status details
  @property()
  connectionStatus: ConnectionStatus = "disconnected";

  // Track if the last commit info has been copied
  @state()
  // lastCommitCopied moved to sketch-container-status

  // Track notification preferences
  @state()
  notificationsEnabled: boolean = false;

  // Track if the window is focused to control notifications
  @state()
  private _windowFocused: boolean = document.hasFocus();

  // Track if the todo panel should be visible
  @state()
  protected _todoPanelVisible: boolean = false;

  // Store scroll position for the chat view to preserve it when switching tabs
  @state()
  private _chatScrollPosition: number = 0;

  // ResizeObserver for tracking chat input height changes
  private chatInputResizeObserver: ResizeObserver | null = null;

  @property()
  connectionErrorMessage: string = "";

  // Chat messages
  @property({ attribute: false })
  messages: AgentMessage[] = [];

  @property()
  set slug(value: string) {
    const oldValue = this._slug;
    this._slug = value;
    this.requestUpdate("slug", oldValue);
    // Update document title when slug property changes
    this.updateDocumentTitle();
  }

  get slug(): string {
    return this._slug;
  }

  private _slug: string = "";

  dataManager = new DataManager();

  @property({ attribute: false })
  containerState: State = {
    state_version: 2,
    slug: "",
    os: "",
    message_count: 0,
    hostname: "",
    working_dir: "",
    initial_commit: "",
    outstanding_llm_calls: 0,
    outstanding_tool_calls: [],
    session_id: "",
    ssh_available: false,
    ssh_error: "",
    in_container: false,
    first_message_index: 0,
    diff_lines_added: 0,
    diff_lines_removed: 0,
  };

  // Mutation observer to detect when new messages are added
  private mutationObserver: MutationObserver | null = null;

  constructor() {
    super();

    // Reference to the container status element
    this.containerStatusElement = null;

    // Binding methods to this
    this._handleViewModeSelect = this._handleViewModeSelect.bind(this);
    this._handlePopState = this._handlePopState.bind(this);
    this._handleShowCommitDiff = this._handleShowCommitDiff.bind(this);
    this._handlePushRebaseRequest = this._handlePushRebaseRequest.bind(this);
    this._handleStopClick = this._handleStopClick.bind(this);
    this._handleEndClick = this._handleEndClick.bind(this);
    this._handleNotificationsToggle =
      this._handleNotificationsToggle.bind(this);
    this._handleWindowFocus = this._handleWindowFocus.bind(this);
    this._handleWindowBlur = this._handleWindowBlur.bind(this);

    // Load notification preference from localStorage
    try {
      const savedPref = localStorage.getItem("sketch-notifications-enabled");
      if (savedPref !== null) {
        this.notificationsEnabled = savedPref === "true";
      }
    } catch (error) {
      console.error("Error loading notification preference:", error);
    }
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();

    // Initialize theme system
    ThemeService.getInstance().initializeTheme();

    // Get reference to the container status element
    setTimeout(() => {
      this.containerStatusElement = this.querySelector("#container-status");
    }, 0);

    // Initialize client-side nav history.
    const url = new URL(window.location.href);
    const mode = url.searchParams.get("view") || "chat";
    window.history.replaceState({ mode }, "", url.toString());

    this.toggleViewMode(mode as ViewMode, false);
    // Add popstate event listener to handle browser back/forward navigation
    window.addEventListener("popstate", this._handlePopState);

    // Add event listeners
    window.addEventListener("view-mode-select", this._handleViewModeSelect);
    window.addEventListener("show-commit-diff", this._handleShowCommitDiff);

    // Add window focus/blur listeners for controlling notifications
    window.addEventListener("focus", this._handleWindowFocus);
    window.addEventListener("blur", this._handleWindowBlur);
    window.addEventListener(
      "push-rebase-request",
      this._handlePushRebaseRequest,
    );

    // register event listeners
    this.dataManager.addEventListener(
      "dataChanged",
      this.handleDataChanged.bind(this),
    );
    this.dataManager.addEventListener(
      "connectionStatusChanged",
      this.handleConnectionStatusChanged.bind(this),
    );

    // Set initial document title
    this.updateDocumentTitle();

    // Initialize the data manager
    this.dataManager.initialize();

    // Process existing messages for commit info
    if (this.messages && this.messages.length > 0) {
      // Update last commit info via container status component
      setTimeout(() => {
        if (this.containerStatusElement) {
          this.containerStatusElement.updateLastCommitInfo(this.messages);
        }
      }, 100);
    }

    // Check if todo panel should be visible on initial load
    this.checkTodoPanelVisibility();

    // Set up ResizeObserver for chat input to update todo panel height
    this.setupChatInputObserver();
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener("popstate", this._handlePopState);

    // Remove event listeners
    window.removeEventListener("view-mode-select", this._handleViewModeSelect);
    window.removeEventListener("show-commit-diff", this._handleShowCommitDiff);
    window.removeEventListener("focus", this._handleWindowFocus);
    window.removeEventListener("blur", this._handleWindowBlur);
    window.removeEventListener(
      "push-rebase-request",
      this._handlePushRebaseRequest,
    );

    // unregister data manager event listeners
    this.dataManager.removeEventListener(
      "dataChanged",
      this.handleDataChanged.bind(this),
    );
    this.dataManager.removeEventListener(
      "connectionStatusChanged",
      this.handleConnectionStatusChanged.bind(this),
    );

    // Disconnect mutation observer if it exists
    if (this.mutationObserver) {
      this.mutationObserver.disconnect();
      this.mutationObserver = null;
    }

    // Disconnect chat input resize observer if it exists
    if (this.chatInputResizeObserver) {
      this.chatInputResizeObserver.disconnect();
      this.chatInputResizeObserver = null;
    }
  }

  updateUrlForViewMode(mode: ViewMode): void {
    // Get the current URL without search parameters
    const url = new URL(window.location.href);

    // Clear existing parameters
    url.search = "";

    // Only add view parameter if not in default chat view
    if (mode !== "chat") {
      url.searchParams.set("view", mode);
      const diff2View = this.querySelector(
        "sketch-diff2-view",
      ) as SketchDiff2View;

      // If in diff2 view and there's a commit hash, include that too
      if (mode === "diff2" && diff2View?.commit) {
        url.searchParams.set("commit", diff2View.commit);
      }
    }

    // Update the browser history without reloading the page
    window.history.pushState({ mode }, "", url.toString());
  }

  private _handlePopState(event: PopStateEvent) {
    if (event.state && event.state.mode) {
      this.toggleViewMode(event.state.mode, false);
    } else {
      this.toggleViewMode("chat", false);
    }
  }

  /**
   * Handle view mode selection event
   */
  private _handleViewModeSelect(event: CustomEvent) {
    const mode = event.detail.mode as "chat" | "diff2" | "terminal";
    this.toggleViewMode(mode, true);
  }

  /**
   * Handle show commit diff event
   */
  private _handleShowCommitDiff(event: CustomEvent) {
    const { commitHash } = event.detail;
    if (commitHash) {
      this.showCommitDiff(commitHash);
    }
  }

  private _handleDiffComment(_event: CustomEvent) {
    // Empty stub required by the event binding in the template
    // Actual handling occurs at global level in sketch-chat-input component
  }
  /**
   * Listen for commit diff event
   * @param commitHash The commit hash to show diff for
   */
  private showCommitDiff(commitHash: string): void {
    // Store the commit hash
    this.currentCommitHash = commitHash;

    this.toggleViewMode("diff2", true);

    this.updateComplete.then(() => {
      const diff2View = this.querySelector("sketch-diff2-view");
      if (diff2View) {
        (diff2View as SketchDiff2View).refreshDiffView();
      }
    });
  }

  /**
   * Toggle between different view modes: chat, diff2, terminal
   */
  private toggleViewMode(mode: ViewMode, updateHistory: boolean): void {
    // Don't do anything if the mode is already active
    if (this.viewMode === mode) return;

    // Store scroll position if we're leaving the chat view
    if (this.viewMode === "chat" && this.scrollContainerRef.value) {
      // Only store scroll position if we actually have meaningful content
      const scrollTop = this.scrollContainerRef.value.scrollTop;
      const scrollHeight = this.scrollContainerRef.value.scrollHeight;
      const clientHeight = this.scrollContainerRef.value.clientHeight;

      // Store position only if we have scrollable content and have actually scrolled
      if (scrollHeight > clientHeight && scrollTop > 0) {
        this._chatScrollPosition = scrollTop;
      }
    }

    // Update the view mode
    this.viewMode = mode;

    if (updateHistory) {
      // Update URL with the current view mode
      this.updateUrlForViewMode(mode);
    }

    // Wait for DOM update to complete
    this.updateComplete.then(() => {
      // Handle scroll position restoration for chat view
      if (
        mode === "chat" &&
        this.scrollContainerRef.value &&
        this._chatScrollPosition > 0
      ) {
        // Use requestAnimationFrame to ensure DOM is ready
        requestAnimationFrame(() => {
          if (this.scrollContainerRef.value) {
            // Double-check that we're still in chat mode and the container is available
            if (
              this.viewMode === "chat" &&
              this.scrollContainerRef.value.isConnected
            ) {
              this.scrollContainerRef.value.scrollTop =
                this._chatScrollPosition;
            }
          }
        });
      }

      // Handle diff2 view specific logic
      if (mode === "diff2") {
        // Refresh git/recentlog when Monaco diff view is opened
        // This ensures branch information is always up-to-date, as branches can change frequently
        const diff2ViewComp = this.querySelector("sketch-diff2-view");
        if (diff2ViewComp) {
          (diff2ViewComp as SketchDiff2View).refreshDiffView();
        }
      }

      // Update view mode buttons
      const viewModeSelect = this.querySelector("sketch-view-mode-select");
      if (viewModeSelect) {
        const event = new CustomEvent("update-active-mode", {
          detail: { mode },
          bubbles: true,
          composed: true,
        });
        viewModeSelect.dispatchEvent(event);
      }
    });
  }

  /**
   * Updates the document title based on current slug and connection status
   */
  private updateDocumentTitle(): void {
    let docTitle = `sk: ${this.slug || "untitled"}`;

    // Add red circle emoji if disconnected
    if (this.connectionStatus === "disconnected") {
      docTitle += " 🔴";
    }

    document.title = docTitle;
  }

  // Check and request notification permission if needed
  private async checkNotificationPermission(): Promise<boolean> {
    // Check if the Notification API is supported
    if (!("Notification" in window)) {
      console.log("This browser does not support notifications");
      return false;
    }

    // Check if permission is already granted
    if (Notification.permission === "granted") {
      return true;
    }

    // If permission is not denied, request it
    if (Notification.permission !== "denied") {
      const permission = await Notification.requestPermission();
      return permission === "granted";
    }

    return false;
  }

  // Handle notifications toggle click
  private _handleNotificationsToggle(): void {
    this.notificationsEnabled = !this.notificationsEnabled;

    // If enabling notifications, check permissions
    if (this.notificationsEnabled) {
      this.checkNotificationPermission();
    }

    // Save preference to localStorage
    try {
      localStorage.setItem(
        "sketch-notifications-enabled",
        String(this.notificationsEnabled),
      );
    } catch (error) {
      console.error("Error saving notification preference:", error);
    }
  }

  // Handle window focus event
  private _handleWindowFocus(): void {
    this._windowFocused = true;
  }

  // Handle window blur event
  private _handleWindowBlur(): void {
    this._windowFocused = false;
  }

  // Get the last user or agent message (ignore system messages like commit, error, etc.)
  // For example, when Sketch notices a new commit, it'll send a message,
  // but it's still idle!
  private getLastUserOrAgentMessage(): AgentMessage | null {
    for (let i = this.messages.length - 1; i >= 0; i--) {
      const message = this.messages[i];
      if (message.type === "user" || message.type === "agent") {
        return message;
      }
    }
    return null;
  }

  // Show notification for message with EndOfTurn=true
  private async showEndOfTurnNotification(
    message: AgentMessage,
  ): Promise<void> {
    // Don't show notifications if they're disabled
    if (!this.notificationsEnabled) return;

    // Don't show notifications if the window is focused
    if (this._windowFocused) return;

    // Check if we have permission to show notifications
    const hasPermission = await this.checkNotificationPermission();
    if (!hasPermission) return;

    // Only show notifications for agent messages with end_of_turn=true and no parent_conversation_id
    if (
      message.type !== "agent" ||
      !message.end_of_turn ||
      message.parent_conversation_id
    )
      return;

    // Create a title that includes the sketch slug
    const notificationTitle = `Sketch: ${this.slug || "untitled"}`;

    // Extract the beginning of the message content (first 100 chars)
    const messagePreview = message.content
      ? message.content.substring(0, 100) +
        (message.content.length > 100 ? "..." : "")
      : "Agent has completed its turn";

    // Create and show the notification
    try {
      new Notification(notificationTitle, {
        body: messagePreview,
        icon: "https://sketch.dev/favicon.ico", // Use sketch.dev favicon for notification
      });
    } catch (error) {
      console.error("Error showing notification:", error);
    }
  }

  // Check if todo panel should be visible based on latest todo content from messages or state
  private checkTodoPanelVisibility(): void {
    // Find the latest todo content from messages first
    let latestTodoContent = "";
    for (let i = this.messages.length - 1; i >= 0; i--) {
      const message = this.messages[i];
      if (message.todo_content !== undefined) {
        latestTodoContent = message.todo_content || "";
        break;
      }
    }

    // If no todo content found in messages, check the current state
    if (latestTodoContent === "" && this.containerState?.todo_content) {
      latestTodoContent = this.containerState.todo_content;
    }

    // Parse the todo data to check if there are any actual todos
    let hasTodos = false;
    if (latestTodoContent.trim()) {
      try {
        const todoData = JSON.parse(latestTodoContent);
        hasTodos = todoData.items && todoData.items.length > 0;
      } catch {
        // Invalid JSON, treat as no todos
        hasTodos = false;
      }
    }

    this._todoPanelVisible = hasTodos;

    // Update todo panel content if visible
    if (hasTodos) {
      const todoPanel = this.querySelector("sketch-todo-panel") as any;
      if (todoPanel && todoPanel.updateTodoContent) {
        todoPanel.updateTodoContent(latestTodoContent);
      }
    }
  }

  private handleDataChanged(eventData: {
    state: State;
    newMessages: AgentMessage[];
  }): void {
    const { state, newMessages } = eventData;

    // Update state if we received it
    if (state) {
      // Ensure we're using the latest call status to prevent indicators from being stuck
      if (
        state.outstanding_llm_calls === 0 &&
        state.outstanding_tool_calls.length === 0
      ) {
        // Force reset containerState calls when nothing is reported as in progress
        state.outstanding_llm_calls = 0;
        state.outstanding_tool_calls = [];
      }

      this.containerState = state;
      this.slug = state.slug || "";

      // Update document title when sketch slug changes
      this.updateDocumentTitle();
    }

    // Update messages
    const oldMessageCount = this.messages.length;
    this.messages = aggregateAgentMessages(this.messages, newMessages);

    // If new messages were added and we're in chat view, reset stored scroll position
    // so the timeline can auto-scroll to bottom for new content
    if (this.messages.length > oldMessageCount && this.viewMode === "chat") {
      // Only reset if we were near the bottom (indicating user wants to follow new messages)
      if (this.scrollContainerRef.value) {
        const scrollTop = this.scrollContainerRef.value.scrollTop;
        const scrollHeight = this.scrollContainerRef.value.scrollHeight;
        const clientHeight = this.scrollContainerRef.value.clientHeight;
        const isNearBottom = scrollTop + clientHeight >= scrollHeight - 50; // 50px tolerance

        if (isNearBottom) {
          this._chatScrollPosition = 0; // Reset stored position to allow auto-scroll
        }
      }
    }

    // Process new messages to find commit messages
    // Update last commit info via container status component
    if (this.containerStatusElement) {
      this.containerStatusElement.updateLastCommitInfo(newMessages);
    }

    // Check for agent messages with end_of_turn=true and show notifications
    if (newMessages && newMessages.length > 0) {
      for (const message of newMessages) {
        if (
          message.type === "agent" &&
          message.end_of_turn &&
          !message.parent_conversation_id
        ) {
          this.showEndOfTurnNotification(message);
          break; // Only show one notification per batch of messages
        }
      }
      const msgsWithUsage = newMessages.filter((msg) => msg.usage);
      if (msgsWithUsage.length > 0) {
        this.latestUsage =
          msgsWithUsage[msgsWithUsage.length - 1]?.usage || null;
      }
    }

    // Check if todo panel should be visible after agent loop iteration
    this.checkTodoPanelVisibility();

    // Ensure chat input observer is set up when new data comes in
    if (!this.chatInputResizeObserver) {
      this.setupChatInputObserver();
    }
  }

  private handleConnectionStatusChanged(
    status: ConnectionStatus,
    errorMessage?: string,
  ): void {
    this.connectionStatus = status;
    this.connectionErrorMessage = errorMessage || "";

    // Update document title when connection status changes
    this.updateDocumentTitle();
  }

  private async _handleStopClick(): Promise<void> {
    try {
      const response = await fetch("cancel", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ reason: "user requested cancellation" }),
      });

      if (!response.ok) {
        const errorData = await response.text();
        throw new Error(
          `Failed to stop operation: ${response.status} - ${errorData}`,
        );
      }

      // Stop request sent
    } catch (error) {
      console.error("Error stopping operation:", error);
    }
  }

  private async _handleEndClick(event?: Event): Promise<void> {
    if (event) {
      event.preventDefault();
      event.stopPropagation();
    }

    // Show confirmation dialog
    const confirmed = window.confirm(
      "Ending the session will shut down the underlying container. Are you sure?",
    );
    if (!confirmed) return;

    try {
      const response = await fetch("end", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ reason: "user requested end of session" }),
      });

      if (!response.ok) {
        const errorData = await response.text();
        throw new Error(
          `Failed to end session: ${response.status} - ${errorData}`,
        );
      }

      // After successful response, redirect to messages view
      // Extract the session ID from the URL
      await this.navigateToMessagesArchiveView();

      // End request sent - connection will be closed by server
    } catch (error) {
      console.error("Error ending session:", error);
    }
  }

  async navigateToMessagesArchiveView(): Promise<void> {
    const currentUrl = window.location.href;
    // The URL pattern should be like https://sketch.dev/s/cs71-8qa6-1124-aw79/
    const urlParts = currentUrl.split("/");
    let sessionId = "";

    // Find the session ID in the URL (should be after /s/)
    for (let i = 0; i < urlParts.length; i++) {
      if (urlParts[i] === "s" && i + 1 < urlParts.length) {
        sessionId = urlParts[i + 1];
        break;
      }
    }

    if (sessionId) {
      // Create the messages URL
      const messagesUrl = `/messages/${sessionId}`;
      // Redirect to messages view
      window.location.href = messagesUrl;
    }
  }

  async _handlePushRebaseRequest(e: CustomEvent) {
    const chatInput = this.querySelector(
      "sketch-chat-input",
    ) as SketchChatInput;
    if (chatInput) {
      if (chatInput.content && chatInput.content.trim() !== "") {
        chatInput.content += "\n\n";
      }
      chatInput.content += e.detail.message;
      chatInput.focus();
      // Adjust textarea height to accommodate new content
      requestAnimationFrame(() => {
        if (chatInput.adjustChatSpacing) {
          chatInput.adjustChatSpacing();
        }
      });
    }
  }

  async _sendChat(e: CustomEvent) {
    console.log("app shell: _sendChat", e);
    e.preventDefault();
    e.stopPropagation();
    const message = e.detail.message?.trim();
    if (message == "") {
      return;
    }
    try {
      // Always switch to chat view when sending a message so user can see processing
      if (this.viewMode !== "chat") {
        this.toggleViewMode("chat", true);
      }

      // Send the message to the server
      const response = await fetch("chat", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({ message }),
      });

      if (!response.ok) {
        const errorData = await response.text();
        throw new Error(`Server error: ${response.status} - ${errorData}`);
      }
    } catch (error) {
      console.error("Error sending chat message:", error);
      const statusText = document.getElementById("statusText");
      if (statusText) {
        statusText.textContent = "Error sending message";
      }
    }
  }

  protected scrollContainerRef = createRef<HTMLElement>();

  /**
   * Set up ResizeObserver to monitor chat input height changes
   */
  private setupChatInputObserver(): void {
    // Wait for DOM to be ready
    this.updateComplete.then(() => {
      const chatInputElement = this.querySelector("#chat-input");
      if (chatInputElement && !this.chatInputResizeObserver) {
        this.chatInputResizeObserver = new ResizeObserver((entries) => {
          for (const entry of entries) {
            this.updateTodoPanelHeight(entry.contentRect.height);
          }
        });

        this.chatInputResizeObserver.observe(chatInputElement);

        // Initial height calculation
        const rect = chatInputElement.getBoundingClientRect();
        this.updateTodoPanelHeight(rect.height);
      }
    });
  }

  /**
   * Update the CSS custom property that controls todo panel bottom position
   */
  private updateTodoPanelHeight(chatInputHeight: number): void {
    // Add some padding (20px) between todo panel and chat input
    const bottomOffset = chatInputHeight;

    // Update the CSS custom property on the host element
    this.style.setProperty("--chat-input-height", `${bottomOffset}px`);
  }

  // Abstract method to be implemented by subclasses
  abstract render(): any;

  // Protected helper methods for subclasses to render common UI elements
  protected renderTopBanner() {
    return html`
      <!-- Top banner: flex row, space between, border bottom, shadow -->
      <div
        id="top-banner"
        class="flex self-stretch justify-between items-center px-5 pr-8 mb-0 border-b border-gray-200 dark:border-neutral-700 gap-5 bg-white dark:bg-neutral-800 shadow-md w-full h-12"
      >
        <!-- Title container -->
        <div
          class="flex flex-col whitespace-nowrap overflow-hidden text-ellipsis max-w-[30%] md:max-w-1/2 sm:max-w-[60%] py-1.5"
        >
          <h1
            class="text-lg md:text-base sm:text-sm font-semibold m-0 min-w-24 whitespace-nowrap overflow-hidden text-ellipsis"
          >
            ${this.containerState?.skaband_addr
              ? html`<a
                  href="${this.containerState.skaband_addr}"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="text-inherit no-underline transition-opacity duration-200 ease-in-out flex items-center gap-2 hover:opacity-80 hover:underline"
                >
                  <img
                    src="${this.containerState.skaband_addr}/sketch.dev.png"
                    alt="sketch"
                    class="w-5 h-5 md:w-[18px] md:h-[18px] sm:w-4 sm:h-4 rounded-sm"
                  />
                  sketch
                </a>`
              : html`sketch`}
          </h1>
          <h2
            class="m-0 p-0 text-gray-600 dark:text-neutral-400 text-sm font-normal italic whitespace-nowrap overflow-hidden text-ellipsis"
          >
            ${this.slug}
          </h2>
        </div>

        <!-- Container status info moved above tabs -->
        <sketch-container-status
          .latestUsage=${this.latestUsage}
          .state=${this.containerState}
          id="container-status"
        ></sketch-container-status>

        <!-- Last Commit section moved to sketch-container-status -->

        <!-- Views section with tabs -->
        <sketch-view-mode-select
          .diffLinesAdded=${this.containerState?.diff_lines_added || 0}
          .diffLinesRemoved=${this.containerState?.diff_lines_removed || 0}
        ></sketch-view-mode-select>

        <!-- Control buttons and status -->
        <div
          class="flex items-center mb-0 flex-nowrap whitespace-nowrap flex-shrink-0 gap-4 pl-4 mr-12"
        >
          <button
            id="stopButton"
            class="bg-red-600 hover:bg-red-700 disabled:bg-red-300 disabled:cursor-not-allowed disabled:opacity-70 text-white border-none px-1.5 py-1 xl:px-2.5 rounded cursor-pointer text-xs mr-1.5 flex items-center gap-1.5 transition-colors"
            ?disabled=${(this.containerState?.outstanding_llm_calls || 0) ===
              0 &&
            (this.containerState?.outstanding_tool_calls || []).length === 0}
            title="Ask the agent to pause its work, may take a moment"
          >
            <svg
              class="w-4 h-4"
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <rect x="6" y="6" width="12" height="12" />
            </svg>
            <span class="max-sm:hidden sm:max-xl:hidden">Stop</span>
          </button>
          <button
            id="endButton"
            class="bg-gray-600 hover:bg-gray-700 disabled:bg-gray-400 disabled:cursor-not-allowed disabled:opacity-70 text-white border-none px-1.5 py-1 xl:px-2.5 rounded cursor-pointer text-xs mr-1.5 flex items-center gap-1.5 transition-colors"
            @click=${this._handleEndClick}
            title="End the session and shut down the container, may cause data loss"
          >
            <svg
              class="w-4 h-4"
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path d="M18 6L6 18" />
              <path d="M6 6l12 12" />
            </svg>
            <span class="max-sm:hidden sm:max-xl:hidden">End</span>
          </button>

          <sketch-theme-toggle></sketch-theme-toggle>

          <div
            class="flex items-center text-xs mr-2.5 cursor-pointer"
            @click=${this._handleNotificationsToggle}
            title="${this.notificationsEnabled
              ? "Disable"
              : "Enable"} notifications when the agent completes its turn"
          >
            <div
              class="w-5 h-5 relative inline-flex items-center justify-center"
            >
              <!-- Bell SVG icon -->
              <svg
                xmlns="http://www.w3.org/2000/svg"
                width="16"
                height="16"
                fill="currentColor"
                viewBox="0 0 16 16"
                class="${!this.notificationsEnabled ? "relative z-10" : ""}"
              >
                <path
                  d="M8 16a2 2 0 0 0 2-2H6a2 2 0 0 0 2 2zM8 1.918l-.797.161A4.002 4.002 0 0 0 4 6c0 .628-.134 2.197-.459 3.742-.16.767-.376 1.566-.663 2.258h10.244c-.287-.692-.502-1.49-.663-2.258C12.134 8.197 12 6.628 12 6a4.002 4.002 0 0 0-3.203-3.92L8 1.917zM14.22 12c.223.447.481.801.78 1H1c.299-.199.557-.553.78-1C2.68 10.2 3 6.88 3 6c0-2.42 1.72-4.44 4.005-4.901a1 1 0 1 1 1.99 0A5.002 5.002 0 0 1 13 6c0 .88.32 4.2 1.22 6z"
                />
              </svg>
              ${!this.notificationsEnabled
                ? html`<div
                    class="absolute w-0.5 h-6 bg-red-600 rotate-45 origin-center"
                  ></div>`
                : ""}
            </div>
          </div>

          <sketch-call-status
            .agentState=${this.containerState?.agent_state}
            .llmCalls=${this.containerState?.outstanding_llm_calls || 0}
            .toolCalls=${this.containerState?.outstanding_tool_calls || []}
            .isIdle=${(() => {
              const lastUserOrAgentMessage = this.getLastUserOrAgentMessage();
              return lastUserOrAgentMessage
                ? lastUserOrAgentMessage.end_of_turn &&
                    !lastUserOrAgentMessage.parent_conversation_id
                : true;
            })()}
            .isDisconnected=${this.connectionStatus === "disconnected"}
          ></sketch-call-status>
        </div>
      </div>
    `;
  }

  protected renderMainViews() {
    return html`
      <!-- Chat View -->
      <div
        class="chat-view ${this.viewMode === "chat"
          ? "view-active flex flex-col"
          : "hidden"} w-full h-full"
      >
        <div
          class="${this._todoPanelVisible && this.viewMode === "chat"
            ? "mr-[400px] xl:mr-[350px] lg:mr-[300px] w-[calc(100%-400px)] xl:w-[calc(100%-350px)] lg:w-[calc(100%-300px)]"
            : "mr-0"} flex-1 flex flex-col w-full h-full transition-[margin-right] duration-200 ease-in-out"
        >
          <sketch-timeline
            .messages=${this.messages}
            .scrollContainer=${this.scrollContainerRef}
            .agentState=${this.containerState?.agent_state}
            .llmCalls=${this.containerState?.outstanding_llm_calls || 0}
            .toolCalls=${this.containerState?.outstanding_tool_calls || []}
            .firstMessageIndex=${this.containerState?.first_message_index || 0}
            .state=${this.containerState}
            .dataManager=${this.dataManager}
          ></sketch-timeline>
        </div>
      </div>

      <!-- Todo panel positioned outside the main flow - only visible in chat view -->
      <div
        class="${this._todoPanelVisible && this.viewMode === "chat"
          ? "block"
          : "hidden"} fixed top-12 right-4 max-lg:hidden xl:w-[350px] lg:w-[300px] z-[100] transition-[bottom] duration-200 ease-in-out bg-gradient-to-b from-gray-50 to-gray-50/20 dark:from-neutral-800 dark:to-neutral-800/20 border-l border-gray-300 dark:border-neutral-600"
        style="bottom: var(--chat-input-height, 90px);"
      >
        <sketch-todo-panel
          .visible=${this._todoPanelVisible && this.viewMode === "chat"}
        ></sketch-todo-panel>
      </div>
      <!-- Diff2 View -->
      <div
        class="diff2-view ${this.viewMode === "diff2"
          ? "view-active flex-1 overflow-hidden min-h-0 flex flex-col h-full"
          : "hidden"} w-full h-full"
      >
        <sketch-diff2-view
          .commit=${this.currentCommitHash}
          .gitService=${new DefaultGitDataService()}
          @diff-comment="${this._handleDiffComment}"
        ></sketch-diff2-view>
      </div>

      <!-- Terminal View -->
      <div
        class="terminal-view ${this.viewMode === "terminal"
          ? "view-active flex flex-col"
          : "hidden"} w-full h-full"
      >
        <sketch-terminal></sketch-terminal>
      </div>
    `;
  }

  protected renderChatInput() {
    return html`
      <!-- Chat input fixed at bottom -->
      <div
        id="chat-input"
        class="self-end w-full shadow-[0_-2px_10px_rgba(0,0,0,0.1)]"
      >
        <sketch-chat-input @send-chat="${this._sendChat}"></sketch-chat-input>
      </div>
    `;
  }

  /**
   * Lifecycle callback when component is first connected to DOM
   */
  firstUpdated(): void {
    if (this.viewMode !== "chat") {
      return;
    }

    // Initial scroll to bottom when component is first rendered
    setTimeout(
      () => this.scrollTo({ top: this.scrollHeight, behavior: "smooth" }),
      50,
    );

    // Setup stop button
    const stopButton = this.renderRoot?.querySelector(
      "#stopButton",
    ) as HTMLButtonElement;
    stopButton?.addEventListener("click", async () => {
      try {
        const response = await fetch("cancel", {
          method: "POST",
          headers: {
            "Content-Type": "application/json",
          },
          body: JSON.stringify({ reason: "User clicked stop button" }),
        });
        if (!response.ok) {
          console.error("Failed to cancel:", await response.text());
        }
      } catch (error) {
        console.error("Error cancelling operation:", error);
      }
    });

    // Process any existing messages to find commit information
    if (this.messages && this.messages.length > 0) {
      // Update last commit info via container status component
      if (this.containerStatusElement) {
        this.containerStatusElement.updateLastCommitInfo(this.messages);
      }
    }

    // Set up chat input height observer for todo panel
    this.setupChatInputObserver();
  }
}

// Export the ViewMode type for use in subclasses
export type { ViewMode };
