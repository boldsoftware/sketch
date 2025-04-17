import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { PropertyValues } from "lit";
import { DataManager, ConnectionStatus } from "../data";
import { State, TimelineMessage, ToolCall } from "../types";
import "./sketch-container-status";
import "./sketch-view-mode-select";
import "./sketch-network-status";
import "./sketch-timeline";
import "./sketch-chat-input";
import "./sketch-diff-view";
import "./sketch-charts";
import "./sketch-terminal";
import { SketchDiffView } from "./sketch-diff-view";
import { View } from "vega";

type ViewMode = "chat" | "diff" | "charts" | "terminal";

@customElement("sketch-app-shell")
export class SketchAppShell extends LitElement {
  // Current view mode (chat, diff, charts, terminal)
  @state()
  viewMode: "chat" | "diff" | "charts" | "terminal" = "chat";

  // Current commit hash for diff view
  @state()
  currentCommitHash: string = "";

  // Reference to the diff view component
  private diffViewRef?: HTMLElement;

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
    :host {
      display: block;
      font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
        Roboto, sans-serif;
      color: #333;
      line-height: 1.4;
      min-height: 100vh;
      width: 100%;
      position: relative;
      overflow-x: hidden;
    }

    /* Top banner with combined elements */
    .top-banner {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 5px 20px;
      margin-bottom: 0;
      border-bottom: 1px solid #eee;
      gap: 10px;
      position: fixed;
      top: 0;
      left: 0;
      right: 0;
      background: white;
      z-index: 100;
      box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
      max-width: 100%;
    }

    .banner-title {
      font-size: 18px;
      font-weight: 600;
      margin: 0;
      min-width: 6em;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .chat-title {
      margin: 0;
      padding: 0;
      color: rgba(82, 82, 82, 0.85);
      font-size: 16px;
      font-weight: normal;
      font-style: italic;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    /* View mode container styles - mirroring timeline.css structure */
    .view-container {
      max-width: 1200px;
      margin: 0 auto;
      margin-top: 65px; /* Space for the top banner */
      margin-bottom: 90px; /* Increased space for the chat input */
      position: relative;
      padding-bottom: 15px; /* Additional padding to prevent clipping */
      padding-top: 15px; /* Add padding at top to prevent content touching the header */
    }

    /* Allow the container to expand to full width in diff mode */
    .view-container.diff-active {
      max-width: 100%;
    }

    /* Individual view styles */
    .chat-view,
    .diff-view,
    .chart-view,
    .terminal-view {
      display: none; /* Hidden by default */
      width: 100%;
    }

    /* Active view styles - these will be applied via JavaScript */
    .view-active {
      display: flex;
      flex-direction: column;
    }

    .title-container {
      display: flex;
      flex-direction: column;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 33%;
    }

    .refresh-control {
      display: flex;
      align-items: center;
      margin-bottom: 0;
      flex-wrap: nowrap;
      white-space: nowrap;
      flex-shrink: 0;
    }

    .refresh-button {
      background: #4caf50;
      color: white;
      border: none;
      padding: 4px 10px;
      border-radius: 4px;
      cursor: pointer;
      font-size: 12px;
      margin-right: 5px;
    }

    .stop-button:hover {
      background-color: #c82333 !important;
    }

    .poll-updates {
      display: flex;
      align-items: center;
      margin: 0 5px;
      font-size: 12px;
    }
  `;

  // Header bar: Network connection status details
  @property()
  connectionStatus: ConnectionStatus = "disconnected";

  @property()
  connectionErrorMessage: string = "";

  @property()
  messageStatus: string = "";

  // Chat messages
  @property()
  messages: TimelineMessage[] = [];

  @property()
  chatMessageText: string = "";

  @property()
  title: string = "";

  private dataManager = new DataManager();

  @property()
  containerState: State = { title: "", os: "", total_usage: {} };

  // Track if this is the first load of messages
  @state()
  private isFirstLoad: boolean = true;

  // Track if we should scroll to the bottom
  @state()
  private shouldScrollToBottom: boolean = true;

  // Mutation observer to detect when new messages are added
  private mutationObserver: MutationObserver | null = null;

  constructor() {
    super();

    // Binding methods to this
    this._handleViewModeSelect = this._handleViewModeSelect.bind(this);
    this._handleDiffComment = this._handleDiffComment.bind(this);
    this._handleShowCommitDiff = this._handleShowCommitDiff.bind(this);
    this._handlePopState = this._handlePopState.bind(this);
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();

    // Initialize client-side nav history.
    const url = new URL(window.location.href);
    const mode = url.searchParams.get("view") || "chat";
    window.history.replaceState({ mode }, "", url.toString());

    this.toggleViewMode(mode as ViewMode, false);
    // Add popstate event listener to handle browser back/forward navigation
    window.addEventListener(
      "popstate",
      this._handlePopState as EventListener);

    // Add event listeners
    window.addEventListener(
      "view-mode-select",
      this._handleViewModeSelect as EventListener
    );
    window.addEventListener(
      "diff-comment",
      this._handleDiffComment as EventListener
    );
    window.addEventListener(
      "show-commit-diff",
      this._handleShowCommitDiff as EventListener
    );

    // register event listeners
    this.dataManager.addEventListener(
      "dataChanged",
      this.handleDataChanged.bind(this)
    );
    this.dataManager.addEventListener(
      "connectionStatusChanged",
      this.handleConnectionStatusChanged.bind(this)
    );

    // Initialize the data manager
    this.dataManager.initialize();
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
    window.removeEventListener(
      "popstate",
      this._handlePopState as EventListener);

    // Remove event listeners
    window.removeEventListener(
      "view-mode-select",
      this._handleViewModeSelect as EventListener
    );
    window.removeEventListener(
      "diff-comment",
      this._handleDiffComment as EventListener
    );
    window.removeEventListener(
      "show-commit-diff",
      this._handleShowCommitDiff as EventListener
    );

    // unregister data manager event listeners
    this.dataManager.removeEventListener(
      "dataChanged",
      this.handleDataChanged.bind(this)
    );
    this.dataManager.removeEventListener(
      "connectionStatusChanged",
      this.handleConnectionStatusChanged.bind(this)
    );

    // Disconnect mutation observer if it exists
    if (this.mutationObserver) {
      console.log("Auto-scroll: Disconnecting mutation observer");
      this.mutationObserver.disconnect();
      this.mutationObserver = null;
    }
  }

  updateUrlForViewMode(
    mode: "chat" | "diff" | "charts" | "terminal"
  ): void {
    // Get the current URL without search parameters
    const url = new URL(window.location.href);

    // Clear existing parameters
    url.search = "";

    // Only add view parameter if not in default chat view
    if (mode !== "chat") {
      url.searchParams.set("view", mode);
      const diffView = this.shadowRoot?.querySelector(".diff-view") as SketchDiffView;

      // If in diff view and there's a commit hash, include that too
      if (mode === "diff" && diffView.commitHash) {
        url.searchParams.set("commit", diffView.commitHash);
      }
    }

    // Update the browser history without reloading the page
    window.history.pushState({ mode }, "", url.toString());
  }

  _handlePopState(event) {
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
    const mode = event.detail.mode as "chat" | "diff" | "charts" | "terminal";
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

  /**
   * Handle diff comment event
   */
  private _handleDiffComment(event: CustomEvent) {
    const { comment } = event.detail;
    if (!comment) return;

    // Find the chat input textarea
    const chatInput = this.shadowRoot?.querySelector("sketch-chat-input");
    if (chatInput) {
      // Update the chat input content using property
      const currentContent = chatInput.getAttribute("content") || "";
      const newContent = currentContent
        ? `${currentContent}\n\n${comment}`
        : comment;
      chatInput.setAttribute("content", newContent);

      // Dispatch an event to update the textarea value in the chat input component
      const updateEvent = new CustomEvent("update-content", {
        detail: { content: newContent },
        bubbles: true,
        composed: true,
      });
      chatInput.dispatchEvent(updateEvent);

      // Switch back to chat view
      this.toggleViewMode("chat", true);
    }
  }

  /**
   * Listen for commit diff event
   * @param commitHash The commit hash to show diff for
   */
  public showCommitDiff(commitHash: string): void {
    // Store the commit hash
    this.currentCommitHash = commitHash;

    // Switch to diff view
    this.toggleViewMode("diff",  true);

    // Wait for DOM update to complete
    this.updateComplete.then(() => {
      // Get the diff view component
      const diffView = this.shadowRoot?.querySelector("sketch-diff-view");
      if (diffView) {
        // Call the showCommitDiff method
        (diffView as any).showCommitDiff(commitHash);
      }
    });
  }

  /**
   * Toggle between different view modes: chat, diff, charts, terminal
   */
  public toggleViewMode(mode: ViewMode, updateHistory: boolean): void {
    // Don't do anything if the mode is already active
    if (this.viewMode === mode) return;

    // Update the view mode
    this.viewMode = mode;

    if (updateHistory) {
      // Update URL with the current view mode
      this.updateUrlForViewMode(mode);
    }

    // Wait for DOM update to complete
    this.updateComplete.then(() => {
      // Update active view
      const viewContainer = this.shadowRoot?.querySelector(".view-container");
      const chatView = this.shadowRoot?.querySelector(".chat-view");
      const diffView = this.shadowRoot?.querySelector(".diff-view");
      const chartView = this.shadowRoot?.querySelector(".chart-view");
      const terminalView = this.shadowRoot?.querySelector(".terminal-view");

      // Remove active class from all views
      chatView?.classList.remove("view-active");
      diffView?.classList.remove("view-active");
      chartView?.classList.remove("view-active");
      terminalView?.classList.remove("view-active");

      // Add/remove diff-active class on view container
      if (mode === "diff") {
        viewContainer?.classList.add("diff-active");
      } else {
        viewContainer?.classList.remove("diff-active");
      }

      // Add active class to the selected view
      switch (mode) {
        case "chat":
          chatView?.classList.add("view-active");
          break;
        case "diff":
          diffView?.classList.add("view-active");
          // Load diff content if we have a diff view
          const diffViewComp =
            this.shadowRoot?.querySelector("sketch-diff-view");
          if (diffViewComp && this.currentCommitHash) {
            (diffViewComp as any).showCommitDiff(this.currentCommitHash);
          } else if (diffViewComp) {
            (diffViewComp as any).loadDiffContent();
          }
          break;
        case "charts":
          chartView?.classList.add("view-active");
          break;
        case "terminal":
          terminalView?.classList.add("view-active");
          break;
      }

      // Update view mode buttons
      const viewModeSelect = this.shadowRoot?.querySelector(
        "sketch-view-mode-select"
      );
      if (viewModeSelect) {
        const event = new CustomEvent("update-active-mode", {
          detail: { mode },
          bubbles: true,
          composed: true,
        });
        viewModeSelect.dispatchEvent(event);
      }

      // FIXME: This is a hack to get vega chart in sketch-charts.ts to work properly
      // When the chart is in the background, its container has a width of 0, so vega
      // renders width 0 and only changes that width on a resize event.
      // See https://github.com/vega/react-vega/issues/85#issuecomment-1826421132
      window.dispatchEvent(new Event("resize"));
    });
  }

  mergeAndDedupe(
    arr1: TimelineMessage[],
    arr2: TimelineMessage[]
  ): TimelineMessage[] {
    const mergedArray = [...arr1, ...arr2];
    const seenIds = new Set<number>();
    const toolCallResults = new Map<string, TimelineMessage>();

    let ret: TimelineMessage[] = mergedArray
      .filter((msg) => {
        if (msg.type == "tool") {
          toolCallResults.set(msg.tool_call_id, msg);
          return false;
        }
        if (seenIds.has(msg.idx)) {
          return false; // Skip if idx is already seen
        }

        seenIds.add(msg.idx);
        return true;
      })
      .sort((a: TimelineMessage, b: TimelineMessage) => a.idx - b.idx);

    // Attach any tool_call result messages to the original message's tool_call object.
    ret.forEach((msg) => {
      msg.tool_calls?.forEach((toolCall) => {
        if (toolCallResults.has(toolCall.tool_call_id)) {
          toolCall.result_message = toolCallResults.get(toolCall.tool_call_id);
        }
      });
    });
    return ret;
  }

  private handleDataChanged(eventData: {
    state: State;
    newMessages: TimelineMessage[];
    isFirstFetch?: boolean;
  }): void {
    const { state, newMessages, isFirstFetch } = eventData;

    // Check if this is the first data fetch or if there are new messages
    if (isFirstFetch) {
      console.log("Auto-scroll: First data fetch, will scroll to bottom");
      this.isFirstLoad = true;
      this.shouldScrollToBottom = true;
      this.messageStatus = "Initial messages loaded";
    } else if (newMessages && newMessages.length > 0) {
      console.log(`Auto-scroll: Received ${newMessages.length} new messages`);
      this.messageStatus = "Updated just now";
      // Check if we should scroll before updating messages
      this.shouldScrollToBottom = this.checkShouldScroll();
    } else {
      this.messageStatus = "No new messages";
    }

    // Update state if we received it
    if (state) {
      this.containerState = state;
      this.title = state.title;
    }

    // Create a copy of the current messages before updating
    const oldMessageCount = this.messages.length;

    // Update messages
    this.messages = this.mergeAndDedupe(this.messages, newMessages);

    // Log information about the message update
    if (this.messages.length > oldMessageCount) {
      console.log(
        `Auto-scroll: Messages updated from ${oldMessageCount} to ${this.messages.length}, shouldScroll=${this.shouldScrollToBottom}`
      );
    }
  }

  private handleConnectionStatusChanged(
    status: ConnectionStatus,
    errorMessage?: string
  ): void {
    this.connectionStatus = status;
    this.connectionErrorMessage = errorMessage || "";
  }

  async _sendChat(e: CustomEvent) {
    console.log("app shell: _sendChat", e);
    const message = e.detail.message?.trim();
    if (message == "") {
      return;
    }
    try {
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
      // Clear the input after successfully sending the message.
      this.chatMessageText = "";

      // Reset data manager state to force a full refresh after sending a message
      // This ensures we get all messages in the correct order
      // Use private API for now - TODO: add a resetState() method to DataManager
      (this.dataManager as any).nextFetchIndex = 0;
      (this.dataManager as any).currentFetchStartIndex = 0;

      // Always scroll to bottom after sending a message
      console.log("Auto-scroll: User sent a message, forcing scroll to bottom");
      this.shouldScrollToBottom = true;

      // // If in diff view, switch to conversation view
      // if (this.viewMode === "diff") {
      //   await this.toggleViewMode("chat");
      // }

      // Refresh the timeline data to show the new message
      await this.dataManager.fetchData();

      // Force multiple scroll attempts to ensure the user message is visible
      // This addresses potential timing issues with DOM updates
      const forceScrollAttempts = () => {
        console.log("Auto-scroll: Forcing scroll after user message");
        this.shouldScrollToBottom = true;

        // Update the timeline component's scroll state
        const timeline = this.shadowRoot?.querySelector(
          "sketch-timeline"
        ) as any;
        if (timeline && timeline.setShouldScrollToLatest) {
          timeline.setShouldScrollToLatest(true);
          timeline.scrollToLatest();
        } else {
          this.scrollToBottom();
        }
      };

      // Make multiple scroll attempts with different timings
      // This ensures we catch the DOM after various update stages
      setTimeout(forceScrollAttempts, 100);
      setTimeout(forceScrollAttempts, 300);
      setTimeout(forceScrollAttempts, 600);
    } catch (error) {
      console.error("Error sending chat message:", error);
      const statusText = document.getElementById("statusText");
      if (statusText) {
        statusText.textContent = "Error sending message";
      }
    }
  }

  render() {
    return html`
      <div class="top-banner">
        <div class="title-container">
          <h1 class="banner-title">sketch</h1>
          <h2 id="chatTitle" class="chat-title">${this.title}</h2>
        </div>

        <sketch-container-status
          .state=${this.containerState}
        ></sketch-container-status>

        <div class="refresh-control">
          <sketch-view-mode-select></sketch-view-mode-select>

          <button id="stopButton" class="refresh-button stop-button">
            Stop
          </button>

          <div class="poll-updates">
            <input type="checkbox" id="pollToggle" checked />
            <label for="pollToggle">Poll</label>
          </div>

          <sketch-network-status
            message=${this.messageStatus}
            connection=${this.connectionStatus}
            error=${this.connectionErrorMessage}
          ></sketch-network-status>
        </div>
      </div>

      <div class="view-container">
        <div class="chat-view ${this.viewMode === "chat" ? "view-active" : ""}">
          <sketch-timeline .messages=${this.messages}></sketch-timeline>
        </div>

        <div class="diff-view ${this.viewMode === "diff" ? "view-active" : ""}">
          <sketch-diff-view
            .commitHash=${this.currentCommitHash}
          ></sketch-diff-view>
        </div>

        <div
          class="chart-view ${this.viewMode === "charts" ? "view-active" : ""}"
        >
          <sketch-charts .messages=${this.messages}></sketch-charts>
        </div>

        <div
          class="terminal-view ${this.viewMode === "terminal"
            ? "view-active"
            : ""}"
        >
          <sketch-terminal></sketch-terminal>
        </div>
      </div>

      <sketch-chat-input
        .content=${this.chatMessageText}
        @send-chat="${this._sendChat}"
      ></sketch-chat-input>
    `;
  }

  /**
   * Check if the page should scroll to the bottom based on current view position
   * @returns Boolean indicating if we should scroll to the bottom
   */
  private checkShouldScroll(): boolean {
    // If we're not in chat view, don't auto-scroll
    if (this.viewMode !== "chat") {
      return false;
    }

    // More generous threshold - if we're within 500px of the bottom, auto-scroll
    // This ensures we start scrolling sooner when new messages appear
    const scrollPosition = window.scrollY;
    const windowHeight = window.innerHeight;
    const documentHeight = document.body.scrollHeight;
    const distanceFromBottom = documentHeight - (scrollPosition + windowHeight);
    const threshold = 500; // Increased threshold to be more responsive

    return distanceFromBottom <= threshold;
  }

  /**
   * Scroll to the bottom of the timeline
   */
  private scrollToBottom(): void {
    if (!this.checkShouldScroll()) {
      return;
    }

    this.scrollTo({ top: this.scrollHeight, behavior: "smooth" });
  }

  /**
   * Called after the component's properties have been updated
   */
  updated(changedProperties: PropertyValues): void {
    // If messages have changed, scroll to bottom if needed
    if (changedProperties.has("messages") && this.messages.length > 0) {
      setTimeout(() => this.scrollToBottom(), 50);
    }
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
      50
    );

    const pollToggleCheckbox = this.renderRoot?.querySelector("#pollToggle") as HTMLInputElement;
    pollToggleCheckbox?.addEventListener("change", () => {
      this.dataManager.setPollingEnabled(pollToggleCheckbox.checked);
      if (!pollToggleCheckbox.checked) {
        this.connectionStatus = "disabled";
        this.messageStatus = "Polling stopped";
      } else {
        this.messageStatus = "Polling for updates...";
      }
    });
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-app-shell": SketchAppShell;
  }
}
