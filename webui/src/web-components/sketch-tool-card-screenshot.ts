import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { ToolCall } from "../types";

@customElement("sketch-tool-card-screenshot")
export class SketchToolCardScreenshot extends LitElement {
  @property()
  toolCall: ToolCall;

  @property()
  open: boolean;

  @state()
  imageLoaded: boolean = false;

  @state()
  loadError: boolean = false;

  static styles = css`
    .summary-text {
      font-style: italic;
      padding: 0.5em;
    }

    .screenshot-container {
      margin: 10px 0;
      display: flex;
      flex-direction: column;
      align-items: center;
    }

    .screenshot {
      max-width: 100%;
      max-height: 500px;
      border-radius: 4px;
      box-shadow: 0 2px 6px rgba(0, 0, 0, 0.2);
      border: 1px solid #ddd;
    }

    .loading-indicator {
      margin: 20px;
      color: #666;
      font-style: italic;
    }

    .error-message {
      color: #d32f2f;
      font-style: italic;
      margin: 10px 0;
    }

    .screenshot-info {
      margin-top: 8px;
      font-size: 12px;
      color: #666;
    }

    .selector-info {
      padding: 4px 8px;
      background-color: #f5f5f5;
      border-radius: 4px;
      font-family: monospace;
      margin: 5px 0;
      display: inline-block;
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
    // Parse the input to get selector
    let selector = "";
    try {
      if (this.toolCall?.input) {
        const input = JSON.parse(this.toolCall.input);
        selector = input.selector || "(full page)";
      }
    } catch (e) {
      console.error("Error parsing screenshot input:", e);
    }

    // Get the screenshot ID from the result
    let screenshotId = "";
    let hasResult = false;
    if (this.toolCall?.result_message?.tool_result) {
      try {
        const result = JSON.parse(this.toolCall.result_message.tool_result);
        screenshotId = result.id;
        hasResult = true;
      } catch (e) {
        console.error("Error parsing screenshot result:", e);
      }
    }

    // Construct the URL for the screenshot (using relative URL without leading slash)
    const screenshotUrl = screenshotId ? `screenshot/${screenshotId}` : "";

    return html`
      <sketch-tool-card .open=${this.open} .toolCall=${this.toolCall}>
        <span slot="summary" class="summary-text">
          Screenshot of ${selector}
        </span>
        <div slot="input" class="selector-info">
          ${selector !== "(full page)" ? `Taking screenshot of element: ${selector}` : `Taking full page screenshot`}
        </div>
        <div slot="result">
          ${hasResult
            ? html`
                <div class="screenshot-container">
                  ${!this.imageLoaded && !this.loadError
                    ? html`<div class="loading-indicator">Loading screenshot...</div>`
                    : ""}
                  ${this.loadError
                    ? html`<div class="error-message">Failed to load screenshot</div>`
                    : html`
                        <img
                          class="screenshot"
                          src="${screenshotUrl}"
                          @load=${() => (this.imageLoaded = true)}
                          @error=${() => (this.loadError = true)}
                          ?hidden=${!this.imageLoaded}
                        />
                        ${this.imageLoaded
                          ? html`<div class="screenshot-info">Screenshot ID: ${screenshotId}</div>`
                          : ""}
                      `}
                </div>
              `
            : ""}
        </div>
      </sketch-tool-card>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-tool-card-screenshot": SketchToolCardScreenshot;
  }
}
