import { css, html, LitElement } from "lit";
import { customElement, property } from "lit/decorators.js";
import { State } from "../types";

@customElement("sketch-container-status")
export class SketchContainerStatus extends LitElement {
  // Header bar: Container status details

  @property()
  state: State;

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
    .info-card {
      background: #f9f9f9;
      border-radius: 8px;
      padding: 15px;
      margin-bottom: 20px;
      box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
      display: none; /* Hidden in the combined layout */
    }

    .info-grid {
      display: flex;
      flex-wrap: wrap;
      gap: 8px;
      background: #f9f9f9;
      border-radius: 4px;
      padding: 4px 10px;
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
      flex: 1;
    }

    .info-item {
      display: flex;
      align-items: center;
      white-space: nowrap;
      margin-right: 10px;
      font-size: 13px;
    }

    .info-label {
      font-size: 11px;
      color: #555;
      margin-right: 3px;
      font-weight: 500;
    }

    .info-value {
      font-size: 11px;
      font-weight: 600;
    }

    .cost {
      color: #2e7d32;
    }

    .info-item a {
      --tw-text-opacity: 1;
      color: rgb(37 99 235 / var(--tw-text-opacity, 1));
      text-decoration: inherit;
    }
  `;

  constructor() {
    super();
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();
    // register event listeners
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
    // unregister event listeners
  }

  render() {
    return html`
      <div class="info-grid">
        <div class="info-item">
          <a href="logs">Logs</a>
        </div>
        <div class="info-item">
          <a href="download">Download</a>
        </div>
        <div class="info-item">
          <span id="hostname" class="info-value">${this.state?.hostname}</span>
        </div>
        <div class="info-item">
          <span id="workingDir" class="info-value"
            >${this.state?.working_dir}</span
          >
        </div>
        <div class="info-item">
          <span class="info-label">Commit:</span>
          <span id="initialCommit" class="info-value"
            >${this.state?.initial_commit?.substring(0, 8)}</span
          >
        </div>
        <div class="info-item">
          <span class="info-label">Msgs:</span>
          <span id="messageCount" class="info-value"
            >${this.state?.message_count}</span
          >
        </div>
        <div class="info-item">
          <span class="info-label">In:</span>
          <span id="inputTokens" class="info-value"
            >${this.state?.total_usage?.input_tokens}</span
          >
        </div>
        <div class="info-item">
          <span class="info-label">Cache Read:</span>
          <span id="cacheReadInputTokens" class="info-value"
            >${this.state?.total_usage?.cache_read_input_tokens}</span
          >
        </div>
        <div class="info-item">
          <span class="info-label">Cache Create:</span>
          <span id="cacheCreationInputTokens" class="info-value"
            >${this.state?.total_usage?.cache_creation_input_tokens}</span
          >
        </div>
        <div class="info-item">
          <span class="info-label">Out:</span>
          <span id="outputTokens" class="info-value"
            >${this.state?.total_usage?.output_tokens}</span
          >
        </div>
        <div class="info-item">
          <span class="info-label">Cost:</span>
          <span id="totalCost" class="info-value cost">$${(this.state?.total_usage?.total_cost_usd || 0).toFixed(2)}</span>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-container-status": SketchContainerStatus;
  }
}
