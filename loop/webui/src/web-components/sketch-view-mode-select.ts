import {css, html, LitElement} from 'lit';
import {customElement, property, state} from 'lit/decorators.js';
import {DataManager, ConnectionStatus} from '../data';
import {State, TimelineMessage} from '../types';
import './sketch-container-status';

@customElement('sketch-view-mode-select')
export class SketchViewModeSelect extends LitElement {
  // Current active mode
  @property()
  activeMode: "chat" | "diff" | "charts" | "terminal" = "chat";
  // Header bar: view mode buttons

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).

  static styles = css`
/* View Mode Button Styles */
.view-mode-buttons {
  display: flex;
  gap: 8px;
  margin-right: 10px;
}

.emoji-button {
  font-size: 18px;
  width: 32px;
  height: 32px;
  display: flex;
  align-items: center;
  justify-content: center;
  background: white;
  border: 1px solid #ddd;
  border-radius: 4px;
  cursor: pointer;
  transition: all 0.2s ease;
  padding: 0;
  line-height: 1;
}

.emoji-button:hover {
  background-color: #f0f0f0;
  transform: translateY(-2px);
  box-shadow: 0 2px 4px rgba(0, 0, 0, 0.1);
}

.emoji-button.active {
  background-color: #e6f7ff;
  border-color: #1890ff;
  color: #1890ff;
}
`;
  
  constructor() {
    super();
    
    // Binding methods
    this._handleViewModeClick = this._handleViewModeClick.bind(this);
    this._handleUpdateActiveMode = this._handleUpdateActiveMode.bind(this);
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();
    
    // Listen for update-active-mode events
    this.addEventListener('update-active-mode', this._handleUpdateActiveMode as EventListener);
  }
  
  /**
   * Handle view mode button clicks
   */
  private _handleViewModeClick(mode: "chat" | "diff" | "charts" | "terminal") {
    // Dispatch a custom event to notify the app shell to change the view
    const event = new CustomEvent('view-mode-select', {
      detail: { mode },
      bubbles: true,
      composed: true
    });
    this.dispatchEvent(event);
  }
  
  /**
   * Handle updates to the active mode
   */
  private _handleUpdateActiveMode(event: CustomEvent) {
    const { mode } = event.detail;
    if (mode) {
      this.activeMode = mode;
    }
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
    
    // Remove event listeners
    this.removeEventListener('update-active-mode', this._handleUpdateActiveMode as EventListener);
  }

  render() {
    return html`
      <div class="view-mode-buttons">
        <button
          id="showConversationButton"
          class="emoji-button ${this.activeMode === 'chat' ? 'active' : ''}"
          title="Conversation View"
          @click=${() => this._handleViewModeClick('chat')}
        >
          💬
        </button>
        <button
          id="showDiffButton"
          class="emoji-button ${this.activeMode === 'diff' ? 'active' : ''}"
          title="Diff View"
          @click=${() => this._handleViewModeClick('diff')}
        >
          ±
        </button>
        <button
          id="showChartsButton"
          class="emoji-button ${this.activeMode === 'charts' ? 'active' : ''}"
          title="Charts View"
          @click=${() => this._handleViewModeClick('charts')}
        >
          📈
        </button>
        <button
          id="showTerminalButton"
          class="emoji-button ${this.activeMode === 'terminal' ? 'active' : ''}"
          title="Terminal View"
          @click=${() => this._handleViewModeClick('terminal')}
        >
          💻
        </button>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-view-mode-select": SketchViewModeSelect;
  }
}