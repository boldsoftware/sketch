import { TimelineMessage } from "./types";
import { formatNumber } from "./utils";

/**
 * Event types for data manager
 */
export type DataManagerEventType = 'dataChanged' | 'connectionStatusChanged';

/**
 * Connection status types
 */
export type ConnectionStatus = 'connected' | 'disconnected' | 'disabled';

/**
 * State interface
 */
export interface TimelineState {
  hostname?: string;
  working_dir?: string;
  initial_commit?: string;
  message_count?: number;
  title?: string;
  total_usage?: {
    input_tokens: number;
    output_tokens: number;
    cache_read_input_tokens: number;
    cache_creation_input_tokens: number;
    total_cost_usd: number;
  };
}

/**
 * DataManager - Class to manage timeline data, fetching, and polling
 */
export class DataManager {
  // State variables
  private lastMessageCount: number = 0;
  private nextFetchIndex: number = 0;
  private currentFetchStartIndex: number = 0;
  private currentPollController: AbortController | null = null;
  private isFetchingMessages: boolean = false;
  private isPollingEnabled: boolean = true;
  private isFirstLoad: boolean = true;
  private connectionStatus: ConnectionStatus = "disabled";
  private messages: TimelineMessage[] = [];
  private timelineState: TimelineState | null = null;
  
  // Event listeners
  private eventListeners: Map<DataManagerEventType, Array<(...args: any[]) => void>> = new Map();

  constructor() {
    // Initialize empty arrays for each event type
    this.eventListeners.set('dataChanged', []);
    this.eventListeners.set('connectionStatusChanged', []);
  }

  /**
   * Initialize the data manager and fetch initial data
   */
  public async initialize(): Promise<void> {
    try {
      // Initial data fetch
      await this.fetchData();
      // Start polling for updates only if initial fetch succeeds
      this.startPolling();
    } catch (error) {
      console.error("Initial data fetch failed, will retry via polling", error);
      // Still start polling to recover
      this.startPolling();
    }
  }

  /**
   * Get all messages
   */
  public getMessages(): TimelineMessage[] {
    return this.messages;
  }

  /**
   * Get the current state
   */
  public getState(): TimelineState | null {
    return this.timelineState;
  }

  /**
   * Get the connection status
   */
  public getConnectionStatus(): ConnectionStatus {
    return this.connectionStatus;
  }

  /**
   * Get the isFirstLoad flag
   */
  public getIsFirstLoad(): boolean {
    return this.isFirstLoad;
  }

  /**
   * Get the currentFetchStartIndex
   */
  public getCurrentFetchStartIndex(): number {
    return this.currentFetchStartIndex;
  }

  /**
   * Add an event listener
   */
  public addEventListener(event: DataManagerEventType, callback: (...args: any[]) => void): void {
    const listeners = this.eventListeners.get(event) || [];
    listeners.push(callback);
    this.eventListeners.set(event, listeners);
  }

  /**
   * Remove an event listener
   */
  public removeEventListener(event: DataManagerEventType, callback: (...args: any[]) => void): void {
    const listeners = this.eventListeners.get(event) || [];
    const index = listeners.indexOf(callback);
    if (index !== -1) {
      listeners.splice(index, 1);
      this.eventListeners.set(event, listeners);
    }
  }

  /**
   * Emit an event
   */
  private emitEvent(event: DataManagerEventType, ...args: any[]): void {
    const listeners = this.eventListeners.get(event) || [];
    listeners.forEach(callback => callback(...args));
  }

  /**
   * Set polling enabled/disabled state
   */
  public setPollingEnabled(enabled: boolean): void {
    this.isPollingEnabled = enabled;
    
    if (enabled) {
      this.startPolling();
    } else {
      this.stopPolling();
    }
  }

  /**
   * Start polling for updates
   */
  public startPolling(): void {
    this.stopPolling(); // Stop any existing polling
    
    // Start long polling
    this.longPoll();
  }

  /**
   * Stop polling for updates
   */
  public stopPolling(): void {
    // Abort any ongoing long poll request
    if (this.currentPollController) {
      this.currentPollController.abort();
      this.currentPollController = null;
    }
    
    // If polling is disabled by user, set connection status to disabled
    if (!this.isPollingEnabled) {
      this.updateConnectionStatus("disabled");
    }
  }

  /**
   * Update the connection status
   */
  private updateConnectionStatus(status: ConnectionStatus): void {
    if (this.connectionStatus !== status) {
      this.connectionStatus = status;
      this.emitEvent('connectionStatusChanged', status);
    }
  }

  /**
   * Long poll for updates
   */
  private async longPoll(): Promise<void> {
    // Abort any existing poll request
    if (this.currentPollController) {
      this.currentPollController.abort();
      this.currentPollController = null;
    }

    // If polling is disabled, don't start a new poll
    if (!this.isPollingEnabled) {
      return;
    }

    let timeoutId: number | undefined;

    try {
      // Create a new abort controller for this request
      this.currentPollController = new AbortController();
      const signal = this.currentPollController.signal;

      // Get the URL with the current message count
      const pollUrl = `state?poll=true&seen=${this.lastMessageCount}`;

      // Make the long poll request
      // Use explicit timeout to handle stalled connections (120s)
      const controller = new AbortController();
      timeoutId = window.setTimeout(() => controller.abort(), 120000);

      interface CustomFetchOptions extends RequestInit {
        [Symbol.toStringTag]?: unknown;
      }

      const fetchOptions: CustomFetchOptions = {
        signal: controller.signal,
        // Use the original signal to allow manual cancellation too
        get [Symbol.toStringTag]() {
          if (signal.aborted) controller.abort();
          return "";
        },
      };

      try {
        const response = await fetch(pollUrl, fetchOptions);
        // Clear the timeout since we got a response
        clearTimeout(timeoutId);

        // Parse the JSON response
        const _data = await response.json();

        // If we got here, data has changed, so fetch the latest data
        await this.fetchData();

        // Start a new long poll (if polling is still enabled)
        if (this.isPollingEnabled) {
          this.longPoll();
        }
      } catch (error) {
        // Handle fetch errors inside the inner try block
        clearTimeout(timeoutId);
        throw error; // Re-throw to be caught by the outer catch block
      }
    } catch (error: unknown) {
      // Clean up timeout if we're handling an error
      if (timeoutId) clearTimeout(timeoutId);

      // Don't log or treat manual cancellations as errors
      const isErrorWithName = (
        err: unknown,
      ): err is { name: string; message?: string } =>
        typeof err === "object" && err !== null && "name" in err;

      if (
        isErrorWithName(error) &&
        error.name === "AbortError" &&
        this.currentPollController?.signal.aborted
      ) {
        console.log("Polling cancelled by user");
        return;
      }

      // Handle different types of errors with specific messages
      let errorMessage = "Not connected";

      if (isErrorWithName(error)) {
        if (error.name === "AbortError") {
          // This was our timeout abort
          errorMessage = "Connection timeout - not connected";
          console.error("Long polling timeout");
        } else if (error.name === "SyntaxError") {
          // JSON parsing error
          errorMessage = "Invalid response from server - not connected";
          console.error("JSON parsing error:", error);
        } else if (
          error.name === "TypeError" &&
          error.message?.includes("NetworkError")
        ) {
          // Network connectivity issues
          errorMessage = "Network connection lost - not connected";
          console.error("Network error during polling:", error);
        } else {
          // Generic error
          console.error("Long polling error:", error);
        }
      }

      // Disable polling on error
      this.isPollingEnabled = false;

      // Update connection status to disconnected
      this.updateConnectionStatus("disconnected");

      // Emit an event that we're disconnected with the error message
      this.emitEvent('connectionStatusChanged', this.connectionStatus, errorMessage);
    }
  }

  /**
   * Fetch timeline data
   */
  public async fetchData(): Promise<void> {    
    // If we're already fetching messages, don't start another fetch
    if (this.isFetchingMessages) {
      console.log("Already fetching messages, skipping request");
      return;
    }

    this.isFetchingMessages = true;

    try {
      // Fetch state first
      const stateResponse = await fetch("state");
      const state = await stateResponse.json();
      this.timelineState = state;

      // Check if new messages are available
      if (
        state.message_count === this.lastMessageCount &&
        this.lastMessageCount > 0
      ) {
        // No new messages, early return
        this.isFetchingMessages = false;
        this.emitEvent('dataChanged', { state, newMessages: [] });
        return;
      }

      // Fetch messages with a start parameter
      this.currentFetchStartIndex = this.nextFetchIndex;
      const messagesResponse = await fetch(
        `messages?start=${this.nextFetchIndex}`,
      );
      const newMessages = await messagesResponse.json() || [];

      // Store messages in our array
      if (this.nextFetchIndex === 0) {
        // If this is the first fetch, replace the entire array
        this.messages = [...newMessages];
      } else {
        // Otherwise append the new messages
        this.messages = [...this.messages, ...newMessages];
      }

      // Update connection status to connected
      this.updateConnectionStatus("connected");

      // Update the last message index for next fetch
      if (newMessages && newMessages.length > 0) {
        this.nextFetchIndex += newMessages.length;
      }

      // Update the message count
      this.lastMessageCount = state?.message_count ?? 0;

      // Mark that we've completed first load
      if (this.isFirstLoad) {
        this.isFirstLoad = false;
      }

      // Emit an event that data has changed
      this.emitEvent('dataChanged', { state, newMessages, isFirstFetch: this.nextFetchIndex === newMessages.length });
    } catch (error) {
      console.error("Error fetching data:", error);

      // Update connection status to disconnected
      this.updateConnectionStatus("disconnected");

      // Emit an event that we're disconnected
      this.emitEvent('connectionStatusChanged', this.connectionStatus, "Not connected");
    } finally {
      this.isFetchingMessages = false;
    }
  }
}
