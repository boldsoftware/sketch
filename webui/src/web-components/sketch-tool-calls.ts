import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { repeat } from "lit/directives/repeat.js";
import { ToolCall } from "../types";
import "./sketch-tool-card";
import "./sketch-tool-card-screenshot";

@customElement("sketch-tool-calls")
export class SketchToolCalls extends LitElement {
  @property()
  toolCalls: ToolCall[] = [];

  @property()
  open: boolean = false;

  @state()
  expanded: boolean = false;

  static styles = css`
    /* Tool calls container styles */
    .tool-calls-container {
      margin-top: 8px;
      padding-top: 4px;
    }

    /* Card container */
    .tool-call-card {
      display: flex;
      flex-direction: column;
      background-color: rgba(255, 255, 255, 0.6);
      border-radius: 6px;
      margin-bottom: 6px;
      overflow: hidden;
      cursor: pointer;
      border-left: 2px solid rgba(0, 0, 0, 0.1);
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
    }

    /* Status indicators for tool calls */
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

    .tool-call-cards-container {
      display: block;
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

  cardForToolCall(toolCall: ToolCall, open: boolean) {
    switch (toolCall.name) {
      case "bash":
        return html`<sketch-tool-card-bash
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-bash>`;
      case "codereview":
        return html`<sketch-tool-card-codereview
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-codereview>`;
      case "done":
        return html`<sketch-tool-card-done
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-done>`;
      case "multiplechoice":
        return html`<sketch-tool-card-multiple-choice
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-multiple-choice>`;
      case "patch":
        return html`<sketch-tool-card-patch
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-patch>`;
      case "think":
        return html`<sketch-tool-card-think
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-think>`;
      case "title":
        return html`<sketch-tool-card-title
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-title>`;
      case "browser_screenshot":
        return html`<sketch-tool-card-screenshot
          .open=${open}
          .toolCall=${toolCall}
        ></sketch-tool-card-screenshot>`;
    }
    return html`<sketch-tool-card-generic
      .open=${open}
      .toolCall=${toolCall}
    ></sketch-tool-card-generic>`;
  }

  // toolUseKey return value should change, if the toolCall gets a response.
  toolUseKey(toolCall: ToolCall): string {
    if (!toolCall.result_message) {
      return toolCall.tool_call_id;
    }
    return `${toolCall.tool_call_id}-${toolCall.result_message.idx}`;
  }

  render() {
    if (!this.toolCalls || this.toolCalls.length === 0) {
      return html``;
    }

    return html`<div class="tool-calls-container">
      <div class="tool-call-cards-container">
        ${repeat(this.toolCalls, this.toolUseKey, (toolCall, idx) => {
          let shouldOpen = false;
          // Always expand screenshot tool calls, expand last tool call if this.open is true
          if (
            toolCall.name === "browser_screenshot" ||
            (idx == this.toolCalls?.length - 1 && this.open)
          ) {
            shouldOpen = true;
          }
          return html`<div
            id="${toolCall.tool_call_id}"
            class="tool-call-card ${toolCall.name}"
          >
            ${this.cardForToolCall(toolCall, shouldOpen)}
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
