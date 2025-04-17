import { css, html, LitElement } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { customElement, property } from "lit/decorators.js";
import { State, TimelineMessage } from "../types";
import { marked, MarkedOptions } from "marked";
import "./sketch-tool-calls";
@customElement("sketch-timeline-message")
export class SketchTimelineMessage extends LitElement {
  @property()
  message: TimelineMessage;

  @property()
  previousMessage: TimelineMessage;

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
    .message {
      position: relative;
      margin-bottom: 5px;
      padding-left: 30px;
    }

    .message-icon {
      position: absolute;
      left: 10px;
      top: 0;
      transform: translateX(-50%);
      width: 16px;
      height: 16px;
      border-radius: 3px;
      text-align: center;
      line-height: 16px;
      color: #fff;
      font-size: 10px;
    }

    .message-content {
      position: relative;
      padding: 5px 10px;
      background: #fff;
      border-radius: 3px;
      box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
      border-left: 3px solid transparent;
    }

    /* Copy button styles */
    .message-text-container,
    .tool-result-container {
      position: relative;
    }

    .message-actions {
      position: absolute;
      top: 5px;
      right: 5px;
      z-index: 10;
      opacity: 0;
      transition: opacity 0.2s ease;
    }

    .message-text-container:hover .message-actions,
    .tool-result-container:hover .message-actions {
      opacity: 1;
    }

    .copy-button {
      background-color: rgba(255, 255, 255, 0.9);
      border: 1px solid #ddd;
      border-radius: 4px;
      color: #555;
      cursor: pointer;
      font-size: 12px;
      padding: 2px 8px;
      transition: all 0.2s ease;
    }

    .copy-button:hover {
      background-color: #f0f0f0;
      color: #333;
    }

    /* Removed arrow decoration for a more compact look */

    .message-header {
      display: flex;
      flex-wrap: wrap;
      gap: 5px;
      margin-bottom: 3px;
      font-size: 12px;
    }

    .message-timestamp {
      font-size: 10px;
      color: #888;
      font-style: italic;
      margin-left: 3px;
    }

    .message-usage {
      font-size: 10px;
      color: #888;
      margin-left: 3px;
    }

    .conversation-id {
      font-family: monospace;
      font-size: 12px;
      padding: 2px 4px;
      background-color: #f0f0f0;
      border-radius: 3px;
      margin-left: auto;
    }

    .parent-info {
      font-size: 11px;
      opacity: 0.8;
    }

    .subconversation {
      border-left: 2px solid transparent;
      padding-left: 5px;
      margin-left: 20px;
      transition: margin-left 0.3s ease;
    }

    .message-text {
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

    .tool-details {
      margin-top: 3px;
      padding-top: 3px;
      border-top: 1px dashed #e0e0e0;
      font-size: 12px;
    }

    .tool-name {
      font-size: 12px;
      font-weight: bold;
      margin-bottom: 2px;
      background: #f0f0f0;
      padding: 2px 4px;
      border-radius: 2px;
      display: flex;
      align-items: center;
      gap: 3px;
    }

    .tool-input,
    .tool-result {
      margin-top: 2px;
      padding: 3px 5px;
      background: #f7f7f7;
      border-radius: 2px;
      font-family: monospace;
      font-size: 12px;
      overflow-x: auto;
      white-space: pre;
      line-height: 1.3;
      user-select: text;
      cursor: text;
      -webkit-user-select: text;
      -moz-user-select: text;
      -ms-user-select: text;
    }

    .tool-result {
      max-height: 300px;
      overflow-y: auto;
    }

    .usage-info {
      margin-top: 10px;
      padding-top: 10px;
      border-top: 1px dashed #e0e0e0;
      font-size: 12px;
      color: #666;
    }

    /* Custom styles for IRC-like experience */
    .user .message-content {
      border-left-color: #2196f3;
    }

    .agent .message-content {
      border-left-color: #4caf50;
    }

    .tool .message-content {
      border-left-color: #ff9800;
    }

    .error .message-content {
      border-left-color: #f44336;
    }

    /* Make message type display bold but without the IRC-style markers */
    .message-type {
      font-weight: bold;
    }

    /* Commit message styling */
    .message.commit {
      background-color: #f0f7ff;
      border-left: 4px solid #0366d6;
    }

    .commits-container {
      margin-top: 10px;
      padding: 5px;
    }

    .commits-header {
      font-weight: bold;
      margin-bottom: 5px;
      color: #24292e;
    }

    .commit-boxes-row {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      margin-top: 8px;
    }

    .commit-box {
      border: 1px solid #d1d5da;
      border-radius: 4px;
      overflow: hidden;
      background-color: #ffffff;
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
      max-width: 100%;
      display: flex;
      flex-direction: column;
    }
    
    .commit-preview {
      padding: 8px 12px;
      cursor: pointer;
      font-family: monospace;
      background-color: #f6f8fa;
      border-bottom: 1px dashed #d1d5da;
    }
    
    .commit-preview:hover {
      background-color: #eef2f6;
    }
    
    .commit-hash {
      color: #0366d6;
      font-weight: bold;
    }
    
    .commit-details {
      padding: 8px 12px;
      max-height: 200px;
      overflow-y: auto;
    }
    
    .commit-details pre {
      margin: 0;
      white-space: pre-wrap;
      word-break: break-word;
    }
    
    .commit-details.is-hidden {
      display: none;
    }
    
    .pushed-branch {
      color: #28a745;
      font-weight: 500;
      margin-left: 6px;
    }
    
    .commit-diff-button {
      padding: 6px 12px;
      border: 1px solid #ccc;
      border-radius: 3px;
      background-color: #f7f7f7;
      color: #24292e;
      font-size: 12px;
      cursor: pointer;
      transition: all 0.2s ease;
      margin: 8px 12px;
      display: block;
    }
    
    .commit-diff-button:hover {
      background-color: #e7e7e7;
      border-color: #aaa;
    }
    
    /* Tool call cards */
    .tool-call-cards-container {
      display: flex;
      flex-direction: column;
      gap: 8px;
      margin-top: 8px;
    }

    /* Message type styles */

    .user .message-icon {
      background-color: #2196f3;
    }

    .agent .message-icon {
      background-color: #4caf50;
    }

    .tool .message-icon {
      background-color: #ff9800;
    }

    .error .message-icon {
      background-color: #f44336;
    }

    .end-of-turn {
      margin-bottom: 15px;
    }

    .end-of-turn::after {
      content: "End of Turn";
      position: absolute;
      left: 15px;
      bottom: -10px;
      transform: translateX(-50%);
      font-size: 10px;
      color: #666;
      background: #f0f0f0;
      padding: 1px 4px;
      border-radius: 3px;
    }

    .markdown-content {
      box-sizing: border-box;
      min-width: 200px;
      margin: 0 auto;
    }

    .markdown-content p {
      margin-block-start: 0.5em;
      margin-block-end: 0.5em;
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

  /**
   * Format timestamp for display
   */
  formatTimestamp(
    timestamp: string | number | Date | null | undefined,
    defaultValue: string = "",
  ): string {
    if (!timestamp) return defaultValue;
    try {
      const date = new Date(timestamp);
      if (isNaN(date.getTime())) return defaultValue;

      // Format: Mar 13, 2025 09:53:25 AM
      return date.toLocaleString("en-US", {
        month: "short",
        day: "numeric",
        year: "numeric",
        hour: "numeric",
        minute: "2-digit",
        second: "2-digit",
        hour12: true,
      });
    } catch (e) {
      return defaultValue;
    }
  }

  formatNumber(
    num: number | null | undefined,
    defaultValue: string = "0",
  ): string {
    if (num === undefined || num === null) return defaultValue;
    try {
      return num.toLocaleString();
    } catch (e) {
      return String(num);
    }
  }
  formatCurrency(
    num: number | string | null | undefined,
    defaultValue: string = "$0.00",
    isMessageLevel: boolean = false,
  ): string {
    if (num === undefined || num === null) return defaultValue;
    try {
      // Use 4 decimal places for message-level costs, 2 for totals
      const decimalPlaces = isMessageLevel ? 4 : 2;
      return `$${parseFloat(String(num)).toFixed(decimalPlaces)}`;
    } catch (e) {
      return defaultValue;
    }
  }

  showCommit(commitHash: string) {
    this.dispatchEvent(new CustomEvent("show-commit-diff", {bubbles: true, composed: true, detail: {commitHash}}))
  }

  render() {
    return html`
      <div
        class="message ${this.message?.type} ${this.message?.end_of_turn
          ? "end-of-turn"
          : ""}"
      >
        ${this.previousMessage?.type != this.message?.type
          ? html`<div class="message-icon">
              ${this.message?.type.toUpperCase()[0]}
            </div>`
          : ""}
        <div class="message-content">
          <div class="message-header">
            <span class="message-type">${this.message?.type}</span>
            <span class="message-timestamp">${this.formatTimestamp(this.message?.timestamp)} ${this.message?.elapsed ? html`(${(this.message?.elapsed / 1e9).toFixed(2)}s)` : ''}</span>
            ${this.message?.usage ? html`
            <span class="message-usage">
              <span title="Input tokens">In: ${this.message?.usage?.input_tokens}</span>
              ${this.message?.usage?.cache_read_input_tokens > 0 ? html`<span title="Cache tokens">[Cache: ${this.formatNumber(this.message?.usage?.cache_read_input_tokens)}]</span>` : ""}
              <span title="Output tokens">Out: ${this.message?.usage?.output_tokens}</span>
              <span title="Message cost">(${this.formatCurrency(this.message?.usage?.cost_usd)})</span>
            </span>` : ''}
          </div>
          <div class="message-text-container">
            <div class="message-actions">
              ${copyButton(this.message?.content)}
            </div>
            ${this.message?.content
              ? html`
                  <div class="message-text markdown-content">
                    ${unsafeHTML(this.renderMarkdown(this.message?.content))}
                  </div>
                `
              : ""}
          </div>
          <sketch-tool-calls
            .toolCalls=${this.message?.tool_calls}
          ></sketch-tool-calls>
          ${this.message?.commits
            ? html`
                <div class="commits-container">
                  <div class="commits-header">
                    ${this.message.commits.length} new commit${this.message.commits.length > 1 ? "s" : ""} detected
                  </div>
                  ${this.message.commits.map((commit) => {
                    return html`
                      <div class="commit-boxes-row">
                        <div class="commit-box">
                          <div class="commit-preview">
                            <span class="commit-hash">${commit.hash.substring(0, 8)}</span> 
                            ${commit.subject}
                            <span class="pushed-branch"
                              >→ pushed to ${commit.pushed_branch}</span>
                          </div>
                          <div class="commit-details is-hidden">
                            <pre>${commit.body}</pre>
                          </div>
                          <button class="commit-diff-button" @click=${() => this.showCommit(commit.hash)}>View Changes</button>
                        </div>
                      </div>
                    `;
                  })}
                </div>
              `
            : ""}
        </div>
      </div>
    `;
  }
}

function copyButton(textToCopy: string) {  
  // Add click event listener to handle copying
  const ret = html`<button class="copy-button" title="Copy text to clipboard" @click=${(e: Event) => {
    e.stopPropagation();
    const copyButton = e.currentTarget as HTMLButtonElement;
    navigator.clipboard
      .writeText(textToCopy)
      .then(() => {
        copyButton.textContent = "Copied!";
        setTimeout(() => {
          copyButton.textContent = "Copy";
        }, 2000);
      })
      .catch((err) => {
        console.error("Failed to copy text: ", err);
        copyButton.textContent = "Failed";
        setTimeout(() => {
          copyButton.textContent = "Copy";
        }, 2000);
      });
  }}>Copy</button`;

  return ret
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-timeline-message": SketchTimelineMessage;
  }
}
