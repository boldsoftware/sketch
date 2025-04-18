import { css, html, LitElement } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { repeat } from "lit/directives/repeat.js";
import { customElement, property } from "lit/decorators.js";
import { State, ToolCall } from "../types";
import { marked, MarkedOptions } from "marked";

function renderMarkdown(markdownContent: string): string {
  try {
    // Set markdown options for proper code block highlighting and safety
    const markedOptions: MarkedOptions = {
      gfm: true, // GitHub Flavored Markdown
      breaks: true, // Convert newlines to <br>
      async: false,
      // DOMPurify is recommended for production, but not included in this implementation
    };
    return marked.parse(markdownContent, markedOptions) as string;
  } catch (error) {
    console.error("Error rendering markdown:", error);
    // Fallback to plain text if markdown parsing fails
    return markdownContent;
  }
}

@customElement("sketch-tool-card")
export class SketchToolCard extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  static styles = css`
    .tool-call {
      display: flex;
      align-items: center;
      gap: 8px;
      white-space: nowrap;
    }

    .tool-call-status {
      margin-right: 4px;
      text-align: center;
    }

    .tool-call-status.spinner {
      animation: spin 1s infinite linear;
      display: inline-block;
      width: 1em;
    }

    @keyframes spin {
      0% {
        transform: rotate(0deg);
      }
      100% {
        transform: rotate(360deg);
      }
    }

    .title {
      font-style: italic;
    }

    .cancel-button {
      background: rgb(76, 175, 80);
      color: white;
      border: none;
      padding: 4px 10px;
      border-radius: 4px;
      cursor: pointer;
      font-size: 12px;
      margin: 5px;
    }

    .cancel-button:hover {
      background: rgb(200, 35, 51) !important;
    }

    .codereview-OK {
      color: green;
    }

    details {
      border-radius: 4px;
      padding: 0.25em;
      margin: 0.25em;
      display: flex;
      flex-direction: column;
      align-items: start;
    }

    details summary {
      list-style: none;
      &::before {
        cursor: hand;
        font-family: monospace;
        content: "+";
        color: white;
        background-color: darkgray;
        border-radius: 1em;
        padding-left: 0.5em;
        margin: 0.25em;
        min-width: 1em;
      }
      [open] &::before {
        content: "-";
      }
    }

    details summary:hover {
      list-style: none;
      &::before {
        background-color: gray;
      }
    }
    summary {
      display: flex;
      flex-direction: row;
      flex-wrap: nowrap;
      justify-content: flex-start;
      align-items: baseline;
    }

    summary .tool-name {
      font-family: monospace;
      color: white;
      background: rgb(124 145 160);
      border-radius: 4px;
      padding: 0.25em;
      margin: 0.25em;
      white-space: pre;
    }

    .summary-text {
      padding: 0.25em;
      display: flex;
      max-width: 50%;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    details[open] .summary-text {
      /*display: none;*/
    }

    .tool-error-message {
      font-style: italic;
      color: #aa0909;
    }
  `;

  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  _cancelToolCall = async (tool_call_id: string, button: HTMLButtonElement) => {
    console.log("cancelToolCall", tool_call_id, button);
    button.innerText = "Cancelling";
    button.disabled = true;
    try {
      const response = await fetch("cancel", {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify({
          tool_call_id: tool_call_id,
          reason: "user requested cancellation",
        }),
      });
      if (response.ok) {
        console.log("cancel", tool_call_id, response);
        button.parentElement.removeChild(button);
      } else {
        button.innerText = "Cancel";
        console.log(`error trying to cancel ${tool_call_id}: `, response);
      }
    } catch (e) {
      console.error("cancel", tool_call_id, e);
    }
  };

  render() {
    const toolCallStatus = this.toolCall?.result_message
      ? this.toolCall?.result_message.tool_error
        ? html`❌
            <span class="tool-error-message"
              >${this.toolCall?.result_message.tool_error}</span
            >`
        : ""
      : "⏳";

    const cancelButton = this.toolCall?.result_message
      ? ""
      : html`<button
          class="cancel-button"
          title="Cancel this operation"
          @click=${(e: Event) => {
            e.stopPropagation();
            const button = e.target as HTMLButtonElement;
            this._cancelToolCall(this.toolCall?.tool_call_id, button);
          }}
        >
          Cancel
        </button>`;

    const status = html`<span
      class="tool-call-status ${this.toolCall?.result_message ? "" : "spinner"}"
      >${toolCallStatus}</span
    >`;

    const ret = html`<div class="tool-call">
      <details ?open=${this.open}>
        <summary>
          <span class="tool-name">${this.toolCall?.name}</span>
          <span class="summary-text"><slot name="summary"></slot></span>
          ${status} ${cancelButton}
        </summary>
        <slot name="input"></slot>
        <slot name="result"></slot>
      </details>
    </div> `;
    if (true) {
      return ret;
    }
  }
}

@customElement("sketch-tool-card-bash")
export class SketchToolCardBash extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  static styles = css`
    pre {
      background: black;
      color: white;
      padding: 0.5em;
      border-radius: 4px;
    }
    .summary-text {
      overflow: hidden;
      text-overflow: ellipsis;
      font-family: monospace;
    }
    .input {
      display: flex;
    }
    .input pre {
      width: 100%;
      margin-bottom: 0;
      border-radius: 4px 4px 0 0;
    }
    .result pre {
      margin-top: 0;
      color: gray;
      border-radius: 0 0 4px 4px;
    }
  `;

  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  render() {
    return html`
    <sketch-tool-card .open=${this.open} .toolCall=${this.toolCall}>
    <span slot="summary" class="summary-text">${JSON.parse(this.toolCall?.input)?.command}</span>
    <div slot="input" class="input"><pre>${JSON.parse(this.toolCall?.input)?.command}</pre></div>
    ${
      this.toolCall?.result_message
        ? html` ${this.toolCall?.result_message.tool_result
            ? html`<div slot="result" class="result">
                <pre class="tool-call-result">
${this.toolCall?.result_message.tool_result}</pre
                >
              </div>`
            : ""}`
        : ""
    }</div>
    </sketch-tool-card>`;
  }
}

@customElement("sketch-tool-card-codereview")
export class SketchToolCardCodeReview extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  static styles = css``;

  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }
  render() {
    return html` <sketch-tool-card
      .open=${this.open}
      .toolCall=${this.toolCall}
    >
      <span slot="summary" class="summary-text">
        ${this.toolCall?.result_message?.tool_result == "OK" ? "✔️" : "⛔"}
      </span>
      <div slot="result">
        <pre>${this.toolCall?.result_message?.tool_result}</pre>
      </div>
    </sketch-tool-card>`;
  }
}

@customElement("sketch-tool-card-done")
export class SketchToolCardDone extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  static styles = css``;

  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  render() {
    const doneInput = JSON.parse(this.toolCall.input);
    return html` <sketch-tool-card
      .open=${this.open}
      .toolCall=${this.toolCall}
    >
      <span slot="summary" class="summary-text"> </span>
      <div slot="result">
        ${Object.keys(doneInput.checklist_items).map((key) => {
          const item = doneInput.checklist_items[key];
          let statusIcon = "⛔";
          if (item.status == "yes") {
            statusIcon = "👍";
          } else if (item.status == "not applicable") {
            statusIcon = "🤷‍♂️";
          }
          return html`<div>
            <span>${statusIcon}</span> ${key}:${item.status}
          </div>`;
        })}
      </div>
    </sketch-tool-card>`;
  }
}

@customElement("sketch-tool-card-patch")
export class SketchToolCardPatch extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  static styles = css`
    .summary-text {
      color: #555;
      font-family: monospace;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      border-radius: 3px;
    }
  `;

  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  render() {
    const patchInput = JSON.parse(this.toolCall?.input);
    return html` <sketch-tool-card
      .open=${this.open}
      .toolCall=${this.toolCall}
    >
      <span slot="summary" class="summary-text">
        ${patchInput?.path}: ${patchInput.patches.length}
        edit${patchInput.patches.length > 1 ? "s" : ""}
      </span>
      <div slot="input">
        ${patchInput.patches.map((patch) => {
          return html` Patch operation: <b>${patch.operation}</b>
            <pre>${patch.newText}</pre>`;
        })}
      </div>
      <div slot="result">
        <pre>${this.toolCall?.result_message?.tool_result}</pre>
      </div>
    </sketch-tool-card>`;
  }
}

@customElement("sketch-tool-card-think")
export class SketchToolCardThink extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  static styles = css`
    .thought-bubble {
      overflow-x: auto;
      margin-bottom: 3px;
      font-family: monospace;
      padding: 3px 5px;
      background: rgb(236, 236, 236);
      border-radius: 6px;
      user-select: text;
      cursor: text;
      -webkit-user-select: text;
      -moz-user-select: text;
      -ms-user-select: text;
      font-size: 13px;
      line-height: 1.3;
    }
    .summary-text {
      overflow: hidden;
      text-overflow: ellipsis;
      font-family: monospace;
      max-width: 50%;
    }
  `;

  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  render() {
    return html`
      <sketch-tool-card .open=${this.open} .toolCall=${this.toolCall}>
        <span slot="summary" class="summary-text"
          >${JSON.parse(this.toolCall?.input)?.thoughts}</span
        >
        <div slot="input" class="thought-bubble">
          <div class="markdown-content">
            ${unsafeHTML(
              renderMarkdown(JSON.parse(this.toolCall?.input)?.thoughts),
            )}
          </div>
        </div>
      </sketch-tool-card>
    `;
  }
}

@customElement("sketch-tool-card-title")
export class SketchToolCardTitle extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  static styles = css`
    .summary-text {
      font-style: italic;
    }
  `;
  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  render() {
    return html`
      <span class="summary-text"
        >I've set the title of this sketch to
        <b>"${JSON.parse(this.toolCall?.input)?.title}"</b></span
      >
    `;
  }
}

@customElement("sketch-tool-card-generic")
export class SketchToolCardGeneric extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  constructor() {
    super();
  }

  connectedCallback() {
    super.connectedCallback();
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  render() {
    return html` <sketch-tool-card
      .open=${this.open}
      .toolCall=${this.toolCall}
    >
      <span slot="summary" class="summary-text">${this.toolCall?.input}</span>
      <div slot="input">
        Input:
        <pre>${this.toolCall?.input}</pre>
      </div>
      <div slot="result">
        Result:
        ${this.toolCall?.result_message
          ? html` ${this.toolCall?.result_message.tool_result
              ? html`<pre>${this.toolCall?.result_message.tool_result}</pre>`
              : ""}`
          : ""}
      </div>
    </sketch-tool-card>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-tool-card": SketchToolCard;
    "sketch-tool-card-generic": SketchToolCardGeneric;
    "sketch-tool-card-bash": SketchToolCardBash;
    "sketch-tool-card-codereview": SketchToolCardCodeReview;
    "sketch-tool-card-done": SketchToolCardDone;
    "sketch-tool-card-patch": SketchToolCardPatch;
    "sketch-tool-card-think": SketchToolCardThink;
    "sketch-tool-card-title": SketchToolCardTitle;
  }
}
