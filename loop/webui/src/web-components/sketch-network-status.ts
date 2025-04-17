import {css, html, LitElement} from 'lit';
import {customElement, property} from 'lit/decorators.js';
import {DataManager, ConnectionStatus} from '../data';
import {State, TimelineMessage} from '../types';
import './sketch-container-status';

@customElement('sketch-network-status')
export class SketchNetworkStatus extends LitElement {
  // Header bar: view mode buttons

  @property()
  connection: string;
  @property()
  message: string;
  @property()
  error: string;
  
  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).

  static styles = css`
.status-container {
  display: flex;
  align-items: center;
}

.polling-indicator {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  margin-right: 4px;
  background-color: #ccc;
}

.polling-indicator.active {
  background-color: #4caf50;
  animation: pulse 1.5s infinite;
}

.polling-indicator.error {
  background-color: #f44336;
  animation: pulse 1.5s infinite;
}

@keyframes pulse {
  0% {
    opacity: 1;
  }
  50% {
    opacity: 0.5;
  }
  100% {
    opacity: 1;
  }
}

.status-text {
  font-size: 11px;
  color: #666;
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

  indicator() {
    if (this.connection === "disabled") {
      return '';
    }
    return this.connection === "connected" ? "active": "error";
  }

  render() {
    return html`
        <div class="status-container">
          <span id="pollingIndicator" class="polling-indicator ${this.indicator()}"></span>
          <span id="statusText" class="status-text">${this.error || this.message}</span>
        </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-network-status": SketchNetworkStatus;
  }
}