import { css, html, LitElement } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { repeat } from "lit/directives/repeat.js";
import { customElement, property } from "lit/decorators.js";
import { State, ToolCall } from "../types";
import { marked, MarkedOptions } from "marked";

@customElement("sketch-tool-calls")
export class SketchToolCalls extends LitElement {
  @property()
  toolCalls: ToolCall[] = [];

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
    /* Tool calls container styles */
    .tool-calls-container {
      /* Removed dotted border */
    }

    .tool-calls-toggle {
      cursor: pointer;
      background-color: #f0f0f0;
      padding: 5px 10px;
      border: none;
      border-radius: 4px;
      text-align: left;
      font-size: 12px;
      margin-top: 5px;
      color: #555;
      font-weight: 500;
    }

    .tool-calls-toggle:hover {
      background-color: #e0e0e0;
    }

    .tool-calls-details {
      margin-top: 10px;
      transition: max-height 0.3s ease;
    }

    .tool-calls-details.collapsed {
      max-height: 0;
      overflow: hidden;
      margin-top: 0;
    }

    .tool-call {
      background: #f9f9f9;
      border-radius: 4px;
      padding: 10px;
      margin-bottom: 10px;
      border-left: 3px solid #4caf50;
    }

    .tool-call-header {
      margin-bottom: 8px;
      font-size: 14px;
      padding: 2px 0;
    }

    /* Compact tool display styles */
    .tool-compact-line {
      font-family: monospace;
      font-size: 12px;
      line-height: 1.4;
      padding: 4px 6px;
      background: #f8f8f8;
      border-radius: 3px;
      position: relative;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 100%;
      display: flex;
      align-items: center;
    }

    .tool-result-inline {
      font-family: monospace;
      color: #0066bb;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      max-width: 400px;
      display: inline-block;
      vertical-align: middle;
    }

    .copy-inline-button {
      font-size: 10px;
      padding: 2px 4px;
      margin-left: 8px;
      background: #eee;
      border: none;
      border-radius: 3px;
      cursor: pointer;
      opacity: 0.7;
    }

    .copy-inline-button:hover {
      opacity: 1;
      background: #ddd;
    }

    .tool-input.compact,
    .tool-result.compact {
      margin: 2px 0;
      padding: 4px;
      font-size: 12px;
    }

    /* Removed old compact container CSS */

    /* Ultra-compact tool call box styles */
    .tool-calls-header {
      /* Empty header - just small spacing */
    }

    .tool-call-boxes-row {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-bottom: 8px;
    }

    .tool-call-wrapper {
      display: flex;
      flex-direction: column;
      margin-bottom: 4px;
    }

    .tool-call-box {
      display: inline-flex;
      align-items: center;
      background: #f0f0f0;
      border-radius: 4px;
      padding: 3px 8px;
      font-size: 12px;
      cursor: pointer;
      max-width: 320px;
      position: relative;
      border: 1px solid #ddd;
      transition: background-color 0.2s;
    }

    .tool-call-box:hover {
      background-color: #e8e8e8;
    }

    .tool-call-box.expanded {
      background-color: #e0e0e0;
      border-bottom-left-radius: 0;
      border-bottom-right-radius: 0;
      border-bottom: 1px solid #ccc;
    }

    .tool-call-input {
      color: #666;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      font-family: monospace;
      font-size: 11px;
    }

    .tool-call-card {
      display: flex;
      flex-direction: column;
      background-color: white;
      overflow: hidden;
      cursor: pointer;
    }

    /* Compact view (default) */
    .tool-call-compact-view {
      display: flex;
      align-items: center;
      gap: 8px;
      font-size: 0.9em;
      white-space: nowrap;
      overflow: visible; /* Don't hide overflow, we'll handle text truncation per element */
      position: relative; /* For positioning the expand icon */
    }

    /* Expanded view (hidden by default) */
    .tool-call-card.collapsed .tool-call-expanded-view {
      display: none;
    }

    .tool-call-expanded-view {
      display: flex;
      flex-direction: column;
      border-top: 1px solid #eee;
    }

    .tool-call-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      padding: 6px 10px;
      background-color: #f0f0f0;
      border-bottom: 1px solid #ddd;
      font-weight: bold;
    }

    .tool-call-name {
      color: gray;
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

    .tool-call-time {
      margin-left: 8px;
      font-size: 0.85em;
      color: #666;
      font-weight: normal;
    }

    .tool-call-input-preview {
      color: #555;
      font-family: var(--monospace-font);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      max-width: 30%;
      background-color: rgba(240, 240, 240, 0.5);
      padding: 2px 5px;
      border-radius: 3px;
      font-size: 0.9em;
    }

    .tool-call-result-preview {
      color: #28a745;
      font-family: var(--monospace-font);
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      max-width: 40%;
      background-color: rgba(240, 248, 240, 0.5);
      padding: 2px 5px;
      border-radius: 3px;
      font-size: 0.9em;
    }

    .tool-call-expand-icon {
      position: absolute;
      right: 10px;
      font-size: 0.8em;
      color: #888;
    }

    .tool-call-input {
      padding: 6px 10px;
      border-bottom: 1px solid #eee;
      font-family: var(--monospace-font);
      font-size: 0.9em;
      white-space: pre-wrap;
      word-break: break-all;
      background-color: #f5f5f5;
    }

    .tool-call-result {
      padding: 6px 10px;
      font-family: var(--monospace-font);
      font-size: 0.9em;
      white-space: pre-wrap;
      max-height: 300px;
      overflow-y: auto;
    }

    .tool-call-result pre {
      margin: 0;
      white-space: pre-wrap;
    }

    @keyframes spin {
      0% {
        transform: rotate(0deg);
      }
      100% {
        transform: rotate(360deg);
      }
    }

    /* Standalone tool messages (legacy/disconnected) */
    .tool-details.standalone .tool-header {
      border-radius: 4px;
      background-color: #fff3cd;
      border-color: #ffeeba;
    }

    .tool-details.standalone .tool-warning {
      margin-left: 10px;
      font-size: 0.85em;
      color: #856404;
      font-style: italic;
    }

    /* Tool call expanded view with sections */
    .tool-call-section {
      border-bottom: 1px solid #eee;
    }

    .tool-call-section:last-child {
      border-bottom: none;
    }

    .tool-call-section-label {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 8px 10px;
      background-color: #f5f5f5;
      font-weight: bold;
      font-size: 0.9em;
    }

    .tool-call-section-content {
      padding: 0;
    }

    .tool-call-copy-btn {
      background-color: #f0f0f0;
      border: 1px solid #ddd;
      border-radius: 4px;
      padding: 2px 8px;
      font-size: 0.8em;
      cursor: pointer;
      transition: background-color 0.2s;
    }

    .tool-call-copy-btn:hover {
      background-color: #e0e0e0;
    }

    /* Override for tool call input in expanded view */
    .tool-call-section-content .tool-call-input {
      margin: 0;
      padding: 8px 10px;
      border: none;
      background-color: #fff;
      max-height: 300px;
      overflow-y: auto;
    }

    .tool-call-card .tool-call-input-preview,
    .tool-call-card .tool-call-result-preview {
      font-family: monospace;
      background: black;
      padding: 1em;
    }
    .tool-call-input-preview {
      color: white;
    }
    .tool-call-result-preview {
      color: gray;
    }

    .tool-call-card.title {
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

    .thought-bubble {
      position: relative;
      background-color: #eee;
      border-radius: 8px;
      padding: 0.5em;
      box-shadow: 0 0 10px rgba(0, 0, 0, 0.2);
      margin-left: 24px;
      margin-top: 24px;
      margin-bottom: 12px;
      max-width: 30%;
      white-space: pre;
    }
    
    .thought-bubble .preview {
      white-space: nowrap;
      text-overflow: ellipsis;
      overflow: hidden;
    }

    .thought-bubble:before {
      content: '';
      position: absolute;
      top: -8px;
      left: -8px;
      width: 15px;
      height: 15px;
      background-color: #eee;
      border-radius: 50%;
      box-shadow: 0 0 10px rgba(0, 0, 0, 0.1);
    }
    
    .thought-bubble:after {
      content: '';
      position: absolute;
      top: -16px;
      left: -16px;
      width: 8px;
      height: 8px;
      background-color: #eee;
      border-radius: 50%;
      box-shadow: 0 0 10px rgba(0, 0, 0, 0.1);
    }
    

    .patch-input-preview {
      color: #555;
      font-family: monospace;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      max-width: 30%;
      background-color: rgba(240, 240, 240, 0.5);
      padding: 2px 5px;
      border-radius: 3px;
      font-size: 0.9em;
    }

    .codereview-OK {
      color: green;
    }
  `;

  constructor() {
    super();
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
  }

  renderMarkdown(markdownContent: string): string {
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

  toolCard(toolCall: ToolCall) {
    const toolCallStatus = toolCall.result_message
      ? toolCall.result_message.tool_error
        ? "❌"
        : ""
      : "⏳";

    const cancelButton = toolCall.result_message
      ? ""
      : html`<button
          class="cancel-button"
          title="Cancel this operation"
          @click=${(e: Event) => {
            e.stopPropagation();
            const button = e.target as HTMLButtonElement;
            this._cancelToolCall(toolCall.tool_call_id, button);
          }}
        >
          Cancel
        </button>`;

    const status = html`<span
      class="tool-call-status ${toolCall.result_message ? "" : "spinner"}"
      >${toolCallStatus}</span
    >`;

    switch (toolCall.name) {
      case "title":
        const titleInput = JSON.parse(toolCall.input);
        return html`
        <div class="tool-call-compact-view">
          I've set the title of this sketch to <b>"${titleInput.title}"</b>
        </div>`;
      case "bash":
        const bashInput = JSON.parse(toolCall.input);
        return html`
        <div class="tool-call-compact-view">
          ${status}
          <span class="tool-call-name">${toolCall.name}</span>
          <pre class="tool-call-input-preview">${bashInput.command}</pre>
          ${toolCall.result_message
            ? html`
                ${toolCall.result_message.tool_result
                  ? html`
                      <pre class="tool-call-result-preview">
${toolCall.result_message.tool_result}</pre>`
                  : ""}`
            : cancelButton}
        </div>`;
      case "codereview":
        return html`
        <div class="tool-call-compact-view">
          ${status}
          <span class="tool-call-name">${toolCall.name}</span>
          ${cancelButton}
          <code class="codereview-preview codereview-${toolCall.result_message?.tool_result}">${toolCall.result_message?.tool_result == 'OK' ? '✔️': '⛔ ' + toolCall.result_message?.tool_result}</code>
        </div>`;
      case "think":
        const thinkInput = JSON.parse(toolCall.input);
        return html`
        <div class="tool-call-compact-view">
          ${status}
          <span class="tool-call-name">${toolCall.name}</span>
          <div class="thought-bubble"><div class="preview">${thinkInput.thoughts}</div></div>
          ${cancelButton}
        </div>`;
      case "patch":
        const patchInput = JSON.parse(toolCall.input);
        return html`
        <div class="tool-call-compact-view">
          ${status}
          <span class="tool-call-name">${toolCall.name}</span>
          <div class="patch-input-preview"><span class="patch-path">${patchInput.path}</span>: ${patchInput.patches.length} edit${patchInput.patches.length > 1 ? 's': ''}</div>
          ${cancelButton}
        </div>`;
      case "done":
        const doneInput = JSON.parse(toolCall.input);
        return html`
        <div class="tool-call-compact-view">
          ${status}
          <span class="tool-call-name">${toolCall.name}</span>
          <div class="done-input-preview">
            ${Object.keys(doneInput.checklist_items).map((key) => {
              const item = doneInput.checklist_items[key];
              let statusIcon = '⛔';
              if (item.status == 'yes') {
                statusIcon = '👍';
              } else if (item.status =='not applicable') {
                statusIcon = '🤷‍♂️';
              }
              return html`<div><span>${statusIcon}</span> ${key}:${item.status}</div>`;
            })}
          </div>
          ${cancelButton}
        </div>`;

      default: // Generic tool card:
        return html`
      <div class="tool-call-compact-view">
        ${status}
        <span class="tool-call-name">${toolCall.name}</span>
        <code class="tool-call-input-preview">${toolCall.input}</code>
        ${cancelButton}
        <code class="tool-call-result-preview">${toolCall.result_message?.tool_result}</code>
      </div>
      ${toolCall.result_message?.tool_result}
    `;
    }
  }
  render() {
    return html`
    <div class="tool-calls-container">
      <div class="tool-calls-header"></div>
      <div class="tool-call-cards-container">
        ${this.toolCalls?.map((toolCall) => {
          return html`<div class="tool-call-card ${toolCall.name}">
            ${this.toolCard(toolCall)}
          </div>`;
        })}
      </div>
    </div>`;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-tool-calls": SketchToolCalls;
  }
}
