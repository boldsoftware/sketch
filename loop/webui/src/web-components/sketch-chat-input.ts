import { css, html, LitElement, PropertyValues } from "lit";
import { customElement, property, query } from "lit/decorators.js";
import { DataManager, ConnectionStatus } from "../data";
import { State, TimelineMessage } from "../types";
import "./sketch-container-status";

@customElement("sketch-chat-input")
export class SketchChatInput extends LitElement {
  @property()
  content: string = "";

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
    /* Chat styles - exactly matching timeline.css */
    .chat-container {
      position: fixed;
      bottom: 0;
      left: 0;
      width: 100%;
      background: #f0f0f0;
      padding: 15px;
      box-shadow: 0 -2px 10px rgba(0, 0, 0, 0.1);
      z-index: 1000;
      min-height: 40px; /* Ensure minimum height */
    }

    .chat-input-wrapper {
      display: flex;
      max-width: 1200px;
      margin: 0 auto;
      gap: 10px;
    }

    #chatInput {
      flex: 1;
      padding: 12px;
      border: 1px solid #ddd;
      border-radius: 4px;
      resize: none;
      font-family: monospace;
      font-size: 12px;
      min-height: 40px;
      max-height: 120px;
      background: #f7f7f7;
    }

    #sendChatButton {
      background-color: #2196f3;
      color: white;
      border: none;
      border-radius: 4px;
      padding: 0 20px;
      cursor: pointer;
      font-weight: 600;
    }

    #sendChatButton:hover {
      background-color: #0d8bf2;
    }
  `;

  constructor() {
    super();

    // Binding methods
    this._handleUpdateContent = this._handleUpdateContent.bind(this);
  }

  /**
   * Handle update-content event
   */
  private _handleUpdateContent(event: CustomEvent) {
    const { content } = event.detail;
    if (content !== undefined) {
      this.content = content;

      // Update the textarea value directly, otherwise it won't update until next render
      const textarea = this.shadowRoot?.querySelector(
        "#chatInput"
      ) as HTMLTextAreaElement;
      if (textarea) {
        textarea.value = content;
      }
    }
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();

    // Listen for update-content events
    this.addEventListener(
      "update-content",
      this._handleUpdateContent as EventListener
    );
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();

    // Remove event listeners
    this.removeEventListener(
      "update-content",
      this._handleUpdateContent as EventListener
    );
  }

  sendChatMessage() {
    const event = new CustomEvent("send-chat", {
      detail: { message: this.content },
      bubbles: true,
      composed: true,
    });
    this.dispatchEvent(event);
    this.content = ""; // Clear the input after sending
  }

  adjustChatSpacing() {
    console.log("TODO: adjustChatSpacing");
  }

  _sendChatClicked() {
    this.sendChatMessage();
    this.chatInput.focus(); // Refocus the input after sending
  }

  _chatInputKeyDown(event: KeyboardEvent) {
    // Send message if Enter is pressed without Shift key
    if (event.key === "Enter" && !event.shiftKey) {
      event.preventDefault(); // Prevent default newline
      this.sendChatMessage();
    }
  }

  _chatInputChanged(event) {
    this.content = event.target.value;
    requestAnimationFrame(() => this.adjustChatSpacing());
  }

  @query("#chatInput")
  private chatInput: HTMLTextAreaElement;

  protected firstUpdated(): void {
    if (this.chatInput) {
      this.chatInput.focus();
    }
  }

  render() {
    return html`
      <div class="chat-container">
        <div class="chat-input-wrapper">
          <textarea
            id="chatInput"
            placeholder="Type your message here and press Enter to send..."
            autofocus
            @keydown="${this._chatInputKeyDown}"
            @input="${this._chatInputChanged}"
            .value=${this.content || ""}
          ></textarea>
          <button @click="${this._sendChatClicked}" id="sendChatButton">
            Send
          </button>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-chat-input": SketchChatInput;
  }
}
