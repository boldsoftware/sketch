import { css, html, LitElement, render } from "lit";
import { unsafeHTML } from "lit/directives/unsafe-html.js";
import { customElement, property, state } from "lit/decorators.js";
import { AgentMessage, State } from "../types";
import { marked, MarkedOptions, Renderer, Tokens } from "marked";
import type mermaid from "mermaid";
import DOMPurify from "dompurify";

// Mermaid is loaded dynamically - see loadMermaid() function
declare global {
  interface Window {
    mermaid?: typeof mermaid;
  }
}

// Mermaid hash will be injected at build time
declare const __MERMAID_HASH__: string;

// Load Mermaid dynamically
let mermaidLoadPromise: Promise<any> | null = null;

function loadMermaid(): Promise<typeof mermaid> {
  if (mermaidLoadPromise) {
    return mermaidLoadPromise;
  }

  if (window.mermaid) {
    return Promise.resolve(window.mermaid);
  }

  mermaidLoadPromise = new Promise((resolve, reject) => {
    // Get the Mermaid hash from build-time constant
    const mermaidHash = __MERMAID_HASH__;

    // Try to load the external Mermaid bundle
    const script = document.createElement("script");
    script.onload = () => {
      // The Mermaid bundle should set window.mermaid
      if (window.mermaid) {
        resolve(window.mermaid);
      } else {
        reject(new Error("Mermaid not loaded from external bundle"));
      }
    };
    script.onerror = (error) => {
      console.warn("Failed to load external Mermaid bundle:", error);
      reject(new Error("Mermaid external bundle failed to load"));
    };

    // Don't set type="module" since we're using IIFE format
    script.src = `./static/mermaid-standalone-${mermaidHash}.js`;
    document.head.appendChild(script);
  });

  return mermaidLoadPromise;
}
import "./sketch-tool-calls";
@customElement("sketch-timeline-message")
export class SketchTimelineMessage extends LitElement {
  @property()
  message: AgentMessage;

  @property()
  state: State;

  @property()
  previousMessage: AgentMessage;

  @property()
  open: boolean = false;

  @property()
  firstMessageIndex: number = 0;

  @state()
  showInfo: boolean = false;

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
    .message {
      position: relative;
      margin-bottom: 6px;
      display: flex;
      flex-direction: column;
      width: 100%;
    }

    .message-container {
      display: flex;
      position: relative;
      width: 100%;
    }

    .message-metadata-left {
      flex: 0 0 80px;
      padding: 3px 5px;
      text-align: right;
      font-size: 11px;
      color: #777;
      align-self: flex-start;
    }

    .message-metadata-right {
      flex: 0 0 80px;
      padding: 3px 5px;
      text-align: left;
      font-size: 11px;
      color: #777;
      align-self: flex-start;
    }

    .message-bubble-container {
      flex: 1;
      display: flex;
      max-width: calc(100% - 160px);
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .user .message-bubble-container {
      justify-content: flex-end;
    }

    .agent .message-bubble-container,
    .tool .message-bubble-container,
    .error .message-bubble-container {
      justify-content: flex-start;
    }

    .message-content {
      position: relative;
      padding: 6px 10px;
      border-radius: 12px;
      box-shadow: 0 1px 2px rgba(0, 0, 0, 0.1);
      max-width: 100%;
      width: fit-content;
      min-width: min-content;
      overflow-wrap: break-word;
      word-break: break-word;
    }

    /* User message styling */
    .user .message-content {
      background-color: #2196f3;
      color: white;
      border-bottom-right-radius: 5px;
    }

    /* Agent message styling */
    .agent .message-content,
    .tool .message-content,
    .error .message-content {
      background-color: #f1f1f1;
      color: black;
      border-bottom-left-radius: 5px;
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

    .message-actions {
      display: flex;
      gap: 6px;
    }

    .copy-icon,
    .info-icon {
      background-color: transparent;
      border: none;
      color: rgba(0, 0, 0, 0.6);
      cursor: pointer;
      padding: 3px;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      width: 24px;
      height: 24px;
      transition: all 0.15s ease;
    }

    .user .copy-icon,
    .user .info-icon {
      color: rgba(255, 255, 255, 0.8);
    }

    .copy-icon:hover,
    .info-icon:hover {
      background-color: rgba(0, 0, 0, 0.08);
    }

    .user .copy-icon:hover,
    .user .info-icon:hover {
      background-color: rgba(255, 255, 255, 0.15);
    }

    /* Message metadata styling */
    .message-type {
      font-weight: bold;
      font-size: 11px;
    }

    .message-timestamp {
      display: block;
      font-size: 10px;
      color: #888;
      margin-top: 2px;
    }

    .message-duration {
      display: block;
      font-size: 10px;
      color: #888;
      margin-top: 2px;
    }

    .message-usage {
      display: block;
      font-size: 10px;
      color: #888;
      margin-top: 3px;
    }

    .conversation-id {
      font-family: monospace;
      font-size: 12px;
      padding: 2px 4px;
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
      margin-bottom: 0;
      font-family: sans-serif;
      padding: 2px 0;
      user-select: text;
      cursor: text;
      -webkit-user-select: text;
      -moz-user-select: text;
      -ms-user-select: text;
      font-size: 14px;
      line-height: 1.35;
      text-align: left;
    }

    /* Style for code blocks within messages */
    .message-text pre,
    .message-text code {
      font-family: monospace;
      background: rgba(0, 0, 0, 0.05);
      border-radius: 4px;
      padding: 2px 4px;
      overflow-x: auto;
      max-width: 100%;
      white-space: pre-wrap; /* Allow wrapping for very long lines */
      word-break: break-all; /* Break words at any character */
      box-sizing: border-box; /* Include padding in width calculation */
    }

    /* Code block container styles */
    .code-block-container {
      position: relative;
      margin: 8px 0;
      border-radius: 6px;
      overflow: hidden;
      background: rgba(0, 0, 0, 0.05);
    }

    .user .code-block-container {
      background: rgba(255, 255, 255, 0.2);
    }

    .code-block-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 4px 8px;
      background: rgba(0, 0, 0, 0.1);
      font-size: 12px;
    }

    .user .code-block-header {
      background: rgba(255, 255, 255, 0.2);
      color: white;
    }

    .code-language {
      font-family: monospace;
      font-size: 11px;
      font-weight: 500;
    }

    .code-copy-button {
      background: transparent;
      border: none;
      color: inherit;
      cursor: pointer;
      padding: 2px;
      border-radius: 3px;
      display: flex;
      align-items: center;
      justify-content: center;
      opacity: 0.7;
      transition: all 0.15s ease;
    }

    .code-copy-button:hover {
      opacity: 1;
      background: rgba(0, 0, 0, 0.1);
    }

    .user .code-copy-button:hover {
      background: rgba(255, 255, 255, 0.2);
    }

    .code-block-container pre {
      margin: 0;
      padding: 8px;
      background: transparent;
    }

    .code-block-container code {
      background: transparent;
      padding: 0;
      display: block;
      width: 100%;
    }

    .user .message-text pre,
    .user .message-text code {
      background: rgba(255, 255, 255, 0.2);
      color: white;
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

    /* Compact message styling - distinct visual separation */
    .compact {
      background: linear-gradient(135deg, #fff3cd 0%, #ffeaa7 100%);
      border: 2px solid #fd7e14;
      border-radius: 12px;
      margin: 20px 0;
      padding: 0;
    }

    .compact .message-content {
      border-left: 4px solid #fd7e14;
      background: rgba(253, 126, 20, 0.05);
      font-weight: 500;
    }

    .compact .message-text {
      color: #8b4513;
      font-size: 13px;
      line-height: 1.4;
    }

    .compact::before {
      content: "📚 CONVERSATION EPOCH";
      display: block;
      text-align: center;
      font-size: 11px;
      font-weight: bold;
      color: #8b4513;
      background: #fd7e14;
      color: white;
      padding: 4px 8px;
      margin: 0;
      border-radius: 8px 8px 0 0;
      letter-spacing: 1px;
    }

    /* Pre-compaction messages get a subtle diagonal stripe background */
    .pre-compaction {
      background: repeating-linear-gradient(
        45deg,
        #ffffff,
        #ffffff 10px,
        #f8f8f8 10px,
        #f8f8f8 20px
      );
      opacity: 0.85;
      border-left: 3px solid #ddd;
    }

    .pre-compaction .message-content {
      background: rgba(255, 255, 255, 0.7);
      backdrop-filter: blur(1px);
      color: #333; /* Ensure dark text for readability */
    }

    .pre-compaction .message-text {
      color: #333; /* Ensure dark text in message content */
    }

    /* Make message type display bold but without the IRC-style markers */
    .message-type {
      font-weight: bold;
    }

    /* Commit message styling */
    .commits-container {
      margin-top: 10px;
    }

    .commit-notification {
      background-color: #e8f5e9;
      color: #2e7d32;
      font-weight: 500;
      font-size: 12px;
      padding: 6px 10px;
      border-radius: 10px;
      margin-bottom: 8px;
      text-align: center;
      box-shadow: 0 1px 2px rgba(0, 0, 0, 0.05);
    }

    .commit-card {
      background-color: #f5f5f5;
      border-radius: 8px;
      overflow: hidden;
      margin-bottom: 6px;
      box-shadow: 0 1px 2px rgba(0, 0, 0, 0.08);
      padding: 6px 8px;
      display: flex;
      align-items: center;
      gap: 8px;
    }

    .commit-hash {
      color: #0366d6;
      font-weight: bold;
      font-family: monospace;
      cursor: pointer;
      text-decoration: none;
      background-color: rgba(3, 102, 214, 0.08);
      padding: 2px 5px;
      border-radius: 4px;
    }

    .commit-hash:hover {
      background-color: rgba(3, 102, 214, 0.15);
    }

    .commit-branch {
      color: #28a745;
      font-weight: 500;
      cursor: pointer;
      font-family: monospace;
      background-color: rgba(40, 167, 69, 0.08);
      padding: 2px 5px;
      border-radius: 4px;
    }

    .commit-branch:hover {
      background-color: rgba(40, 167, 69, 0.15);
    }

    .commit-branch-container {
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .commit-branch-container .copy-icon {
      opacity: 0.7;
      display: flex;
      align-items: center;
    }

    .commit-branch-container .copy-icon svg {
      vertical-align: middle;
    }

    .commit-branch-container:hover .copy-icon {
      opacity: 1;
    }

    .octocat-link {
      color: #586069;
      text-decoration: none;
      display: flex;
      align-items: center;
      transition: color 0.2s ease;
    }

    .octocat-link:hover {
      color: #0366d6;
    }

    .octocat-icon {
      width: 14px;
      height: 14px;
    }

    .commit-subject {
      font-size: 13px;
      color: #333;
      flex-grow: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .commit-diff-button {
      padding: 3px 8px;
      border: none;
      border-radius: 4px;
      background-color: #0366d6;
      color: white;
      font-size: 11px;
      cursor: pointer;
      transition: all 0.2s ease;
      display: block;
      margin-left: auto;
    }

    .commit-diff-button:hover {
      background-color: #0256b4;
    }

    /* Tool call cards */
    .tool-call-cards-container {
      display: flex;
      flex-direction: column;
      gap: 8px;
      margin-top: 8px;
    }

    /* Error message specific styling */
    .error .message-content {
      background-color: #ffebee;
      border-left: 3px solid #f44336;
    }

    .end-of-turn {
      margin-bottom: 15px;
    }

    .end-of-turn-indicator {
      display: block;
      font-size: 11px;
      color: #777;
      padding: 2px 0;
      margin-top: 8px;
      text-align: right;
      font-style: italic;
    }

    .user .end-of-turn-indicator {
      color: rgba(255, 255, 255, 0.7);
    }

    /* Message info panel styling */
    .message-info-panel {
      margin-top: 8px;
      padding: 8px;
      background-color: rgba(0, 0, 0, 0.03);
      border-radius: 6px;
      font-size: 12px;
      transition: all 0.2s ease;
      border-left: 2px solid rgba(0, 0, 0, 0.1);
    }

    .user .message-info-panel {
      background-color: rgba(255, 255, 255, 0.15);
      border-left: 2px solid rgba(255, 255, 255, 0.2);
    }

    .info-row {
      margin-bottom: 3px;
      display: flex;
    }

    .info-label {
      font-weight: bold;
      margin-right: 5px;
      min-width: 60px;
    }

    .info-value {
      flex: 1;
    }

    .conversation-id {
      font-family: monospace;
      font-size: 10px;
      word-break: break-all;
    }

    .markdown-content {
      box-sizing: border-box;
      min-width: 200px;
      margin: 0 auto;
    }

    .markdown-content p {
      margin-block-start: 0.3em;
      margin-block-end: 0.3em;
    }

    .markdown-content p:first-child {
      margin-block-start: 0;
    }

    .markdown-content p:last-child {
      margin-block-end: 0;
    }

    /* Styling for markdown elements */
    .markdown-content a {
      color: inherit;
      text-decoration: underline;
    }

    .user .markdown-content a {
      color: #fff;
      text-decoration: underline;
    }

    .markdown-content ul,
    .markdown-content ol {
      padding-left: 1.5em;
      margin: 0.5em 0;
    }

    .markdown-content blockquote {
      border-left: 3px solid rgba(0, 0, 0, 0.2);
      padding-left: 1em;
      margin-left: 0.5em;
      font-style: italic;
    }

    .user .markdown-content blockquote {
      border-left: 3px solid rgba(255, 255, 255, 0.4);
    }

    /* Mermaid diagram styling */
    .mermaid-container {
      margin: 1em 0;
      padding: 0.5em;
      background-color: #f8f8f8;
      border-radius: 4px;
      overflow-x: auto;
    }

    .mermaid {
      text-align: center;
    }

    /* Print styles for message components */
    @media print {
      .message {
        page-break-inside: avoid;
        margin-bottom: 12px;
      }

      .message-container {
        page-break-inside: avoid;
      }

      /* Hide copy buttons and interactive elements during printing */
      .copy-icon,
      .info-icon,
      .commit-diff-button {
        display: none !important;
      }

      /* Ensure code blocks print properly */
      .message-content pre {
        white-space: pre-wrap;
        word-wrap: break-word;
        page-break-inside: avoid;
        background: #f8f8f8 !important;
        border: 1px solid #ddd !important;
        padding: 8px !important;
      }

      /* Ensure tool calls section prints properly */
      .tool-calls-section {
        page-break-inside: avoid;
      }

      /* Simplify message metadata for print */
      .message-metadata-left {
        font-size: 10px;
      }

      /* Ensure content doesn't break poorly */
      .message-content {
        orphans: 3;
        widows: 3;
      }

      /* Hide floating messages during print */
      .floating-message {
        display: none !important;
      }
    }
  `;

  // Track mermaid diagrams that need rendering
  private mermaidDiagrams = new Map();

  constructor() {
    super();
    // Mermaid will be initialized lazily when first needed
  }

  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();
  }

  // After the component is updated and rendered, render any mermaid diagrams
  updated(changedProperties: Map<string, unknown>) {
    super.updated(changedProperties);
    this.renderMermaidDiagrams();
    this.setupCodeBlockCopyButtons();
  }

  // Render mermaid diagrams after the component is updated
  renderMermaidDiagrams() {
    // Add a small delay to ensure the DOM is fully rendered
    setTimeout(async () => {
      // Find all mermaid containers in our shadow root
      const containers = this.shadowRoot?.querySelectorAll(".mermaid");
      if (!containers || containers.length === 0) return;

      try {
        // Load mermaid dynamically
        const mermaidLib = await loadMermaid();

        // Initialize mermaid with specific config (only once per load)
        mermaidLib.initialize({
          startOnLoad: false,
          suppressErrorRendering: true,
          theme: "default",
          securityLevel: "loose", // Allows more flexibility but be careful with user-generated content
          fontFamily: "monospace",
        });

        // Process each mermaid diagram
        containers.forEach((container) => {
          const id = container.id;
          const code = container.textContent || "";
          if (!code || !id) return; // Use return for forEach instead of continue

          try {
            // Clear any previous content
            container.innerHTML = code;

            // Render the mermaid diagram using promise
            mermaidLib
              .render(`${id}-svg`, code)
              .then(({ svg }) => {
                container.innerHTML = svg;
              })
              .catch((err) => {
                console.error("Error rendering mermaid diagram:", err);
                // Show the original code as fallback
                container.innerHTML = `<pre>${code}</pre>`;
              });
          } catch (err) {
            console.error("Error processing mermaid diagram:", err);
            // Show the original code as fallback
            container.innerHTML = `<pre>${code}</pre>`;
          }
        });
      } catch (err) {
        console.error("Error loading mermaid:", err);
        // Show the original code as fallback for all diagrams
        containers.forEach((container) => {
          const code = container.textContent || "";
          container.innerHTML = `<pre>${code}</pre>`;
        });
      }
    }, 100); // Small delay to ensure DOM is ready
  }

  // Setup code block copy buttons after component is updated
  setupCodeBlockCopyButtons() {
    setTimeout(() => {
      // Find all copy buttons in code blocks
      const copyButtons =
        this.shadowRoot?.querySelectorAll(".code-copy-button");
      if (!copyButtons || copyButtons.length === 0) return;

      // Add click event listener to each button
      copyButtons.forEach((button) => {
        button.addEventListener("click", (e) => {
          e.stopPropagation();
          const codeId = (button as HTMLElement).dataset.codeId;
          if (!codeId) return;

          const codeElement = this.shadowRoot?.querySelector(`#${codeId}`);
          if (!codeElement) return;

          const codeText = codeElement.textContent || "";
          const buttonRect = button.getBoundingClientRect();

          // Copy code to clipboard
          navigator.clipboard
            .writeText(codeText)
            .then(() => {
              // Show success indicator
              const originalHTML = button.innerHTML;
              button.innerHTML = `
                <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M20 6L9 17l-5-5"></path>
                </svg>
              `;

              // Display floating message
              this.showFloatingMessage("Copied!", buttonRect, "success");

              // Reset button after delay
              setTimeout(() => {
                button.innerHTML = originalHTML;
              }, 2000);
            })
            .catch((err) => {
              console.error("Failed to copy code:", err);
              this.showFloatingMessage("Failed to copy!", buttonRect, "error");
            });
        });
      });
    }, 100); // Small delay to ensure DOM is ready
  }

  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
  }

  renderMarkdown(markdownContent: string): string {
    try {
      // Create a custom renderer
      const renderer = new Renderer();
      const originalCodeRenderer = renderer.code.bind(renderer);

      // Override the code renderer to handle mermaid diagrams and add copy buttons
      renderer.code = function ({ text, lang, escaped }: Tokens.Code): string {
        if (lang === "mermaid") {
          // Generate a unique ID for this diagram
          const id = `mermaid-diagram-${Math.random().toString(36).substring(2, 10)}`;

          // Just create the container and mermaid div - we'll render it in the updated() lifecycle method
          return `<div class="mermaid-container">
                   <div class="mermaid" id="${id}">${text}</div>
                 </div>`;
        }

        // For regular code blocks, call the original renderer to get properly escaped HTML
        const originalCodeHtml = originalCodeRenderer({ text, lang, escaped });

        // Extract the code content from the original HTML to add our custom wrapper
        // The original renderer returns: <pre><code class="language-x">escapedText</code></pre>
        const codeMatch = originalCodeHtml.match(
          /<pre><code[^>]*>([\s\S]*?)<\/code><\/pre>/,
        );
        if (!codeMatch) {
          // Fallback to original if we can't parse it
          return originalCodeHtml;
        }

        const escapedText = codeMatch[1];
        const id = `code-block-${Math.random().toString(36).substring(2, 10)}`;
        const langClass = lang ? ` class="language-${lang}"` : "";

        return `<div class="code-block-container">
                 <div class="code-block-header">
                   ${lang ? `<span class="code-language">${lang}</span>` : ""}
                   <button class="code-copy-button" title="Copy code" data-code-id="${id}">
                     <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                       <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
                       <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
                     </svg>
                   </button>
                 </div>
                 <pre><code id="${id}"${langClass}>${escapedText}</code></pre>
               </div>`;
      };

      // Set markdown options for proper code block highlighting
      const markedOptions: MarkedOptions = {
        gfm: true, // GitHub Flavored Markdown
        breaks: true, // Convert newlines to <br>
        async: false,
        renderer: renderer,
      };

      // Parse markdown and sanitize the output HTML with DOMPurify
      const htmlOutput = marked.parse(markdownContent, markedOptions) as string;
      return DOMPurify.sanitize(htmlOutput, {
        // Allow common HTML elements that are safe
        ALLOWED_TAGS: [
          "p",
          "br",
          "strong",
          "em",
          "b",
          "i",
          "u",
          "s",
          "code",
          "pre",
          "h1",
          "h2",
          "h3",
          "h4",
          "h5",
          "h6",
          "ul",
          "ol",
          "li",
          "blockquote",
          "a",
          "div",
          "span", // For mermaid diagrams and code blocks
          "svg",
          "g",
          "path",
          "rect",
          "circle",
          "text",
          "line",
          "polygon", // For mermaid SVG
          "button", // For code copy buttons
        ],
        ALLOWED_ATTR: [
          "href",
          "title",
          "target",
          "rel", // For links
          "class",
          "id", // For styling and functionality
          "data-*", // For code copy buttons
          // SVG attributes for mermaid diagrams
          "viewBox",
          "width",
          "height",
          "xmlns",
          "fill",
          "stroke",
          "stroke-width",
          "d",
          "x",
          "y",
          "x1",
          "y1",
          "x2",
          "y2",
          "cx",
          "cy",
          "r",
          "rx",
          "ry",
          "points",
          "transform",
          "text-anchor",
          "font-size",
          "font-family",
        ],
        // Allow data attributes for functionality
        ALLOW_DATA_ATTR: true,
        // Keep whitespace for code formatting
        KEEP_CONTENT: true,
      });
    } catch (error) {
      console.error("Error rendering markdown:", error);
      // Fallback to sanitized plain text if markdown parsing fails
      return DOMPurify.sanitize(markdownContent);
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

  // Format duration from nanoseconds to a human-readable string
  _formatDuration(nanoseconds: number | null | undefined): string {
    if (!nanoseconds) return "0s";

    const seconds = nanoseconds / 1e9;

    if (seconds < 60) {
      return `${seconds.toFixed(1)}s`;
    } else if (seconds < 3600) {
      const minutes = Math.floor(seconds / 60);
      const remainingSeconds = seconds % 60;
      return `${minutes}min ${remainingSeconds.toFixed(0)}s`;
    } else {
      const hours = Math.floor(seconds / 3600);
      const remainingSeconds = seconds % 3600;
      const minutes = Math.floor(remainingSeconds / 60);
      return `${hours}h ${minutes}min`;
    }
  }

  showCommit(commitHash: string) {
    this.dispatchEvent(
      new CustomEvent("show-commit-diff", {
        bubbles: true,
        composed: true,
        detail: { commitHash },
      }),
    );
  }

  _toggleInfo(e: Event) {
    e.stopPropagation();
    this.showInfo = !this.showInfo;
  }

  copyToClipboard(text: string, event: Event) {
    const element = event.currentTarget as HTMLElement;
    const rect = element.getBoundingClientRect();

    navigator.clipboard
      .writeText(text)
      .then(() => {
        this.showFloatingMessage("Copied!", rect, "success");
      })
      .catch((err) => {
        console.error("Failed to copy text: ", err);
        this.showFloatingMessage("Failed to copy!", rect, "error");
      });
  }

  showFloatingMessage(
    message: string,
    targetRect: DOMRect,
    type: "success" | "error",
  ) {
    // Create floating message element
    const floatingMsg = document.createElement("div");
    floatingMsg.textContent = message;
    floatingMsg.className = `floating-message ${type}`;

    // Position it near the clicked element
    // Position just above the element
    const top = targetRect.top - 30;
    const left = targetRect.left + targetRect.width / 2 - 40;

    floatingMsg.style.position = "fixed";
    floatingMsg.style.top = `${top}px`;
    floatingMsg.style.left = `${left}px`;
    floatingMsg.style.zIndex = "9999";

    // Add to document body
    document.body.appendChild(floatingMsg);

    // Animate in
    floatingMsg.style.opacity = "0";
    floatingMsg.style.transform = "translateY(10px)";

    setTimeout(() => {
      floatingMsg.style.opacity = "1";
      floatingMsg.style.transform = "translateY(0)";
    }, 10);

    // Remove after animation
    setTimeout(() => {
      floatingMsg.style.opacity = "0";
      floatingMsg.style.transform = "translateY(-10px)";

      setTimeout(() => {
        document.body.removeChild(floatingMsg);
      }, 300);
    }, 1500);
  }

  // Format GitHub repository URL to org/repo format
  formatGitHubRepo(url) {
    if (!url) return null;

    // Common GitHub URL patterns
    const patterns = [
      // HTTPS URLs
      /https:\/\/github\.com\/([^/]+)\/([^/\s.]+)(?:\.git)?/,
      // SSH URLs
      /git@github\.com:([^/]+)\/([^/\s.]+)(?:\.git)?/,
      // Git protocol
      /git:\/\/github\.com\/([^/]+)\/([^/\s.]+)(?:\.git)?/,
    ];

    for (const pattern of patterns) {
      const match = url.match(pattern);
      if (match) {
        return {
          formatted: `${match[1]}/${match[2]}`,
          url: `https://github.com/${match[1]}/${match[2]}`,
          owner: match[1],
          repo: match[2],
        };
      }
    }

    return null;
  }

  // Generate GitHub branch URL if linking is enabled
  getGitHubBranchLink(branchName) {
    if (!this.state?.link_to_github || !branchName) {
      return null;
    }

    const github = this.formatGitHubRepo(this.state?.git_origin);
    if (!github) {
      return null;
    }

    return `https://github.com/${github.owner}/${github.repo}/tree/${branchName}`;
  }

  render() {
    // Calculate if this is an end of turn message with no parent conversation ID
    const isEndOfTurn =
      this.message?.end_of_turn && !this.message?.parent_conversation_id;

    const isPreCompaction =
      this.message?.idx !== undefined &&
      this.message.idx < this.firstMessageIndex;

    return html`
      <div
        class="message ${this.message?.type} ${isEndOfTurn
          ? "end-of-turn"
          : ""} ${isPreCompaction ? "pre-compaction" : ""}"
      >
        <div class="message-container">
          <!-- Left area (empty for simplicity) -->
          <div class="message-metadata-left"></div>

          <!-- Message bubble -->
          <div class="message-bubble-container">
            <div class="message-content">
              <div class="message-text-container">
                <div class="message-actions">
                  ${copyButton(this.message?.content)}
                  <button
                    class="info-icon"
                    title="Show message details"
                    @click=${this._toggleInfo}
                  >
                    <svg
                      xmlns="http://www.w3.org/2000/svg"
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      stroke-width="2"
                      stroke-linecap="round"
                      stroke-linejoin="round"
                    >
                      <circle cx="12" cy="12" r="10"></circle>
                      <line x1="12" y1="16" x2="12" y2="12"></line>
                      <line x1="12" y1="8" x2="12.01" y2="8"></line>
                    </svg>
                  </button>
                </div>
                ${this.message?.content
                  ? html`
                      <div class="message-text markdown-content">
                        ${unsafeHTML(
                          this.renderMarkdown(this.message?.content),
                        )}
                      </div>
                    `
                  : ""}

                <!-- End of turn indicator inside the bubble -->
                ${isEndOfTurn && this.message?.elapsed
                  ? html`
                      <div class="end-of-turn-indicator">
                        end of turn
                        (${this._formatDuration(this.message?.elapsed)})
                      </div>
                    `
                  : ""}

                <!-- Info panel that can be toggled -->
                ${this.showInfo
                  ? html`
                      <div class="message-info-panel">
                        <div class="info-row">
                          <span class="info-label">Type:</span>
                          <span class="info-value">${this.message?.type}</span>
                        </div>
                        <div class="info-row">
                          <span class="info-label">Time:</span>
                          <span class="info-value"
                            >${this.formatTimestamp(
                              this.message?.timestamp,
                              "",
                            )}</span
                          >
                        </div>
                        ${this.message?.elapsed
                          ? html`
                              <div class="info-row">
                                <span class="info-label">Duration:</span>
                                <span class="info-value"
                                  >${this._formatDuration(
                                    this.message?.elapsed,
                                  )}</span
                                >
                              </div>
                            `
                          : ""}
                        ${this.message?.usage
                          ? html`
                              <div class="info-row">
                                <span class="info-label">Tokens:</span>
                                <span class="info-value">
                                  ${this.message?.usage
                                    ? html`
                                        <div>
                                          Input:
                                          ${this.formatNumber(
                                            this.message?.usage?.input_tokens ||
                                              0,
                                          )}
                                        </div>
                                        ${this.message?.usage
                                          ?.cache_creation_input_tokens
                                          ? html`
                                              <div>
                                                Cache creation:
                                                ${this.formatNumber(
                                                  this.message?.usage
                                                    ?.cache_creation_input_tokens,
                                                )}
                                              </div>
                                            `
                                          : ""}
                                        ${this.message?.usage
                                          ?.cache_read_input_tokens
                                          ? html`
                                              <div>
                                                Cache read:
                                                ${this.formatNumber(
                                                  this.message?.usage
                                                    ?.cache_read_input_tokens,
                                                )}
                                              </div>
                                            `
                                          : ""}
                                        <div>
                                          Output:
                                          ${this.formatNumber(
                                            this.message?.usage?.output_tokens,
                                          )}
                                        </div>
                                        <div>
                                          Cost:
                                          ${this.formatCurrency(
                                            this.message?.usage?.cost_usd,
                                          )}
                                        </div>
                                      `
                                    : "N/A"}
                                </span>
                              </div>
                            `
                          : ""}
                        ${this.message?.conversation_id
                          ? html`
                              <div class="info-row">
                                <span class="info-label">Conversation ID:</span>
                                <span class="info-value conversation-id"
                                  >${this.message?.conversation_id}</span
                                >
                              </div>
                            `
                          : ""}
                      </div>
                    `
                  : ""}
              </div>

              <!-- Tool calls - only shown for agent messages -->
              ${this.message?.type === "agent"
                ? html`
                    <sketch-tool-calls
                      .toolCalls=${this.message?.tool_calls}
                      .open=${this.open}
                    ></sketch-tool-calls>
                  `
                : ""}

              <!-- Commits section (redesigned as bubbles) -->
              ${this.message?.commits
                ? html`
                    <div class="commits-container">
                      <div class="commit-notification">
                        ${this.message.commits.length} new
                        commit${this.message.commits.length > 1 ? "s" : ""}
                        detected
                      </div>
                      ${this.message.commits.map((commit) => {
                        return html`
                          <div class="commit-card">
                            <span
                              class="commit-hash"
                              title="Click to copy: ${commit.hash}"
                              @click=${(e) =>
                                this.copyToClipboard(
                                  commit.hash.substring(0, 8),
                                  e,
                                )}
                            >
                              ${commit.hash.substring(0, 8)}
                            </span>
                            ${commit.pushed_branch
                              ? (() => {
                                  const githubLink = this.getGitHubBranchLink(
                                    commit.pushed_branch,
                                  );
                                  return html`
                                    <div class="commit-branch-container">
                                      <span
                                        class="commit-branch pushed-branch"
                                        title="Click to copy: ${commit.pushed_branch}"
                                        @click=${(e) =>
                                          this.copyToClipboard(
                                            commit.pushed_branch,
                                            e,
                                          )}
                                        >${commit.pushed_branch}</span
                                      >
                                      <span
                                        class="copy-icon"
                                        @click=${(e) => {
                                          e.stopPropagation();
                                          this.copyToClipboard(
                                            commit.pushed_branch,
                                            e,
                                          );
                                        }}
                                      >
                                        <svg
                                          xmlns="http://www.w3.org/2000/svg"
                                          width="14"
                                          height="14"
                                          viewBox="0 0 24 24"
                                          fill="none"
                                          stroke="currentColor"
                                          stroke-width="2"
                                          stroke-linecap="round"
                                          stroke-linejoin="round"
                                        >
                                          <rect
                                            x="9"
                                            y="9"
                                            width="13"
                                            height="13"
                                            rx="2"
                                            ry="2"
                                          ></rect>
                                          <path
                                            d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"
                                          ></path>
                                        </svg>
                                      </span>
                                      ${githubLink
                                        ? html`
                                            <a
                                              href="${githubLink}"
                                              target="_blank"
                                              rel="noopener noreferrer"
                                              class="octocat-link"
                                              title="Open ${commit.pushed_branch} on GitHub"
                                              @click=${(e) =>
                                                e.stopPropagation()}
                                            >
                                              <svg
                                                class="octocat-icon"
                                                viewBox="0 0 16 16"
                                                width="14"
                                                height="14"
                                              >
                                                <path
                                                  fill="currentColor"
                                                  d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"
                                                />
                                              </svg>
                                            </a>
                                          `
                                        : ""}
                                    </div>
                                  `;
                                })()
                              : ``}
                            <span class="commit-subject"
                              >${commit.subject}</span
                            >
                            <button
                              class="commit-diff-button"
                              @click=${() => this.showCommit(commit.hash)}
                            >
                              View Diff
                            </button>
                          </div>
                        `;
                      })}
                    </div>
                  `
                : ""}
            </div>
          </div>

          <!-- Right side (empty for consistency) -->
          <div class="message-metadata-right"></div>
        </div>
      </div>
    `;
  }
}

function copyButton(textToCopy: string) {
  // Use an icon of overlapping rectangles for copy
  const buttonClass = "copy-icon";

  // SVG for copy icon (two overlapping rectangles)
  const copyIcon = html`<svg
    xmlns="http://www.w3.org/2000/svg"
    width="16"
    height="16"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
  >
    <rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect>
    <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path>
  </svg>`;

  // SVG for success check mark
  const successIcon = html`<svg
    xmlns="http://www.w3.org/2000/svg"
    width="16"
    height="16"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="2"
    stroke-linecap="round"
    stroke-linejoin="round"
  >
    <path d="M20 6L9 17l-5-5"></path>
  </svg>`;

  const ret = html`<button
    class="${buttonClass}"
    title="Copy to clipboard"
    @click=${(e: Event) => {
      e.stopPropagation();
      const copyButton = e.currentTarget as HTMLButtonElement;
      const originalInnerHTML = copyButton.innerHTML;
      navigator.clipboard
        .writeText(textToCopy)
        .then(() => {
          copyButton.innerHTML = "";
          const successElement = document.createElement("div");
          copyButton.appendChild(successElement);
          render(successIcon, successElement);
          setTimeout(() => {
            copyButton.innerHTML = originalInnerHTML;
          }, 2000);
        })
        .catch((err) => {
          console.error("Failed to copy text: ", err);
          setTimeout(() => {
            copyButton.innerHTML = originalInnerHTML;
          }, 2000);
        });
    }}
  >
    ${copyIcon}
  </button>`;

  return ret;
}

// Create global styles for floating messages
const floatingMessageStyles = document.createElement("style");
floatingMessageStyles.textContent = `
  .floating-message {
    background-color: rgba(0, 0, 0, 0.8);
    color: white;
    padding: 5px 10px;
    border-radius: 4px;
    font-size: 12px;
    font-family: system-ui, sans-serif;
    box-shadow: 0 2px 5px rgba(0, 0, 0, 0.2);
    pointer-events: none;
    transition: opacity 0.3s ease, transform 0.3s ease;
  }

  .floating-message.success {
    background-color: rgba(40, 167, 69, 0.9);
  }

  .floating-message.error {
    background-color: rgba(220, 53, 69, 0.9);
  }

  /* Style for code, pre elements, and tool components to ensure proper wrapping/truncation */
  pre, code, sketch-tool-calls, sketch-tool-card, sketch-tool-card-bash {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 100%;
  }
  
  /* Special rule for the message content container */
  .message-content {
    max-width: 100% !important;
    overflow: hidden !important;
  }
  
  /* Ensure tool call containers don't overflow */
  ::slotted(sketch-tool-calls) {
    max-width: 100%;
    width: 100%;
    overflow-wrap: break-word;
    word-break: break-word;
  }
`;
document.head.appendChild(floatingMessageStyles);

declare global {
  interface HTMLElementTagNameMap {
    "sketch-timeline-message": SketchTimelineMessage;
  }
}
