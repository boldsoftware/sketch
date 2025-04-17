import {css, html, LitElement} from 'lit';
import {repeat} from 'lit/directives/repeat.js';
import {customElement, property} from 'lit/decorators.js';
import {State, TimelineMessage} from '../types';
import './sketch-timeline-message'

@customElement('sketch-timeline')
export class SketchTimeline extends LitElement {
  @property()
  messages: TimelineMessage[] = [];

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
  /* Hide views initially to prevent flash of content */
  .timeline-container .timeline,
  .timeline-container .diff-view,
  .timeline-container .chart-view,
  .timeline-container .terminal-view {
    visibility: hidden;
  }
  
  /* Will be set by JavaScript once we know which view to display */
  .timeline-container.view-initialized .timeline,
  .timeline-container.view-initialized .diff-view,
  .timeline-container.view-initialized .chart-view,
  .timeline-container.view-initialized .terminal-view {
    visibility: visible;
  }

  .timeline-container {
    width: 100%;
    position: relative;
  }
  
  /* Timeline styles that should remain unchanged */
  .timeline {
    position: relative;
    margin: 10px 0;
    scroll-behavior: smooth;
  }
  
  .timeline::before {
    content: "";
    position: absolute;
    top: 0;
    bottom: 0;
    left: 15px;
    width: 2px;
    background: #e0e0e0;
    border-radius: 1px;
  }
  
  /* Hide the timeline vertical line when there are no messages */
  .timeline.empty::before {
    display: none;
  }  
  `;

  constructor() {
    super();
    
    // Binding methods
    this._handleShowCommitDiff = this._handleShowCommitDiff.bind(this);
  }
  
  /**
   * Handle showCommitDiff event
   */
  private _handleShowCommitDiff(event: CustomEvent) {
    const { commitHash } = event.detail;
    if (commitHash) {
      // Bubble up the event to the app shell
      const newEvent = new CustomEvent('show-commit-diff', {
        detail: { commitHash },
        bubbles: true,
        composed: true
      });
      this.dispatchEvent(newEvent);
    }
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();
    
    // Listen for showCommitDiff events from the renderer
    document.addEventListener('showCommitDiff', this._handleShowCommitDiff as EventListener);
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
    
    // Remove event listeners
    document.removeEventListener('showCommitDiff', this._handleShowCommitDiff as EventListener);
  }

  messageKey(message: TimelineMessage): string {
    // If the message has tool calls, and any of the tool_calls get a response, we need to
    // re-render that message.
    const toolCallResponses = message.tool_calls?.filter((tc)=>tc.result_message).map((tc)=>tc.tool_call_id).join('-');
    return `message-${message.idx}-${toolCallResponses}`;
  }

  render() {
    return html`
    <div class="timeline-container">
      ${repeat(this.messages, this.messageKey, (message, index) => {        
        let previousMessage: TimelineMessage;
        if (index > 0) {
          previousMessage = this.messages[index-1];
        } 
        return html`<sketch-timeline-message .message=${message} .previousMessage=${previousMessage}></sketch-timeline-message>`;
      })}
    </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-timeline": SketchTimeline;
  }
}