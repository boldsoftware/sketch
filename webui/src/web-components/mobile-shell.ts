import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { ConnectionStatus, DataManager } from "../data";
import { AgentMessage, State } from "../types";
import { aggregateAgentMessages } from "./aggregateAgentMessages";

import "./mobile-title";
import "./mobile-chat";
import "./mobile-chat-input";

@customElement("mobile-shell")
export class MobileShell extends LitElement {
  private dataManager = new DataManager();

  @state()
  state: State | null = null;

  @property({ attribute: false })
  messages: AgentMessage[] = [];

  @state()
  connectionStatus: ConnectionStatus = "disconnected";

  static styles = css`
    :host {
      display: flex;
      flex-direction: column;
      /* Use dynamic viewport height for better iOS support */
      height: 100dvh;
      /* Fallback for browsers that don't support dvh */
      height: 100vh;
      /* iOS Safari custom property fallback */
      height: calc(var(--vh, 1vh) * 100);
      /* Additional iOS Safari fix */
      min-height: 100vh;
      min-height: -webkit-fill-available;
      width: 100vw;
      background-color: #ffffff;
      font-family:
        -apple-system, BlinkMacSystemFont, "Segoe UI", "Roboto", sans-serif;
    }

    .mobile-container {
      display: flex;
      flex-direction: column;
      height: 100%;
      overflow: hidden;
    }

    mobile-title {
      flex-shrink: 0;
    }

    mobile-chat {
      flex: 1;
      overflow: hidden;
    }

    mobile-chat-input {
      flex-shrink: 0;
    }
  `;

  connectedCallback() {
    super.connectedCallback();
    this.setupDataManager();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    // Remove event listeners
    this.dataManager.removeEventListener(
      "dataChanged",
      this.handleDataChanged.bind(this),
    );
    this.dataManager.removeEventListener(
      "connectionStatusChanged",
      this.handleConnectionStatusChanged.bind(this),
    );
  }

  private setupDataManager() {
    // Add event listeners
    this.dataManager.addEventListener(
      "dataChanged",
      this.handleDataChanged.bind(this),
    );
    this.dataManager.addEventListener(
      "connectionStatusChanged",
      this.handleConnectionStatusChanged.bind(this),
    );

    // Initialize the data manager - it will automatically connect to /stream?from=0
    this.dataManager.initialize();
  }

  private handleDataChanged(eventData: {
    state: State;
    newMessages: AgentMessage[];
  }) {
    const { state, newMessages } = eventData;

    if (state) {
      this.state = state;
    }

    // Update messages using the same pattern as main app shell
    this.messages = aggregateAgentMessages(this.messages, newMessages);
  }

  private handleConnectionStatusChanged(
    status: ConnectionStatus,
    errorMessage?: string,
  ) {
    this.connectionStatus = status;
  }

  private handleSendMessage = async (
    event: CustomEvent<{ message: string }>,
  ) => {
    const message = event.detail.message.trim();
    if (!message) {
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
        console.error("Failed to send message:", response.statusText);
      }
    } catch (error) {
      console.error("Error sending message:", error);
    }
  };

  render() {
    const isThinking =
      this.state?.outstanding_llm_calls > 0 ||
      (this.state?.outstanding_tool_calls?.length ?? 0) > 0;

    return html`
      <div class="mobile-container">
        <mobile-title
          .connectionStatus=${this.connectionStatus}
          .isThinking=${isThinking}
          .skabandAddr=${this.state?.skaband_addr}
          .slug=${this.state?.slug}
        ></mobile-title>

        <mobile-chat
          .messages=${this.messages}
          .isThinking=${isThinking}
        ></mobile-chat>

        <mobile-chat-input
          .disabled=${this.connectionStatus !== "connected"}
          @send-message=${this.handleSendMessage}
        ></mobile-chat-input>
      </div>
    `;
  }
}
