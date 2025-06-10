import { State, AgentMessage } from "../types";
import { LitElement, css, html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { formatNumber } from "../utils";

@customElement("sketch-container-status")
export class SketchContainerStatus extends LitElement {
  // Header bar: Container status details

  @property()
  state: State;

  @state()
  showDetails: boolean = false;

  @state()
  lastCommit: { hash: string; pushedBranch?: string } | null = null;

  @state()
  lastCommitCopied: boolean = false;

  // See https://lit.dev/docs/components/styles/ for how lit-element handles CSS.
  // Note that these styles only apply to the scope of this web component's
  // shadow DOM node, so they won't leak out or collide with CSS declared in
  // other components or the containing web page (...unless you want it to do that).
  static styles = css`
    /* Last commit display styling */
    .last-commit {
      display: flex;
      flex-direction: column;
      padding: 3px 8px;
      cursor: pointer;
      position: relative;
      margin: 4px 0;
      transition: color 0.2s ease;
    }

    .last-commit:hover {
      color: #0366d6;
    }

    /* Pulse animation for new commits */
    @keyframes pulse {
      0% {
        transform: scale(1);
        opacity: 1;
      }
      50% {
        transform: scale(1.05);
        opacity: 0.8;
      }
      100% {
        transform: scale(1);
        opacity: 1;
      }
    }

    .pulse {
      animation: pulse 1.5s ease-in-out;
      background-color: rgba(38, 132, 255, 0.1);
      border-radius: 3px;
    }

    .last-commit-title {
      color: #666;
      font-family: system-ui, sans-serif;
      font-size: 11px;
      font-weight: 500;
      line-height: 1.2;
    }

    .last-commit-hash {
      font-family: monospace;
      font-size: 12px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    /* Styles for the last commit in main grid */
    .last-commit-column {
      justify-content: flex-start;
    }

    .info-label {
      color: #666;
      font-family: system-ui, sans-serif;
      font-size: 11px;
      font-weight: 500;
    }

    .last-commit-main {
      cursor: pointer;
      position: relative;
      padding-top: 0;
    }

    .last-commit-main:hover {
      color: #0366d6;
    }

    .main-grid-commit {
      font-family: monospace;
      font-size: 12px;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
    }

    .commit-hash-indicator {
      color: #666;
    }

    .commit-branch-indicator {
      color: #28a745;
    }

    .no-commit-indicator {
      color: #999;
      font-style: italic;
      font-size: 12px;
    }

    .copied-indicator {
      position: absolute;
      top: 0;
      left: 0;
      background: rgba(0, 0, 0, 0.7);
      color: white;
      padding: 2px 6px;
      border-radius: 3px;
      font-size: 11px;
      pointer-events: none;
      z-index: 10;
    }

    .copy-icon {
      margin-left: 4px;
      opacity: 0.7;
    }

    .copy-icon svg {
      vertical-align: middle;
    }

    .last-commit-main:hover .copy-icon {
      opacity: 1;
    }

    .info-container {
      display: flex;
      align-items: center;
      position: relative;
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

    .info-expanded {
      position: absolute;
      top: 100%;
      right: 0;
      z-index: 10;
      min-width: 400px;
      background: white;
      border-radius: 8px;
      padding: 10px 15px;
      box-shadow: 0 6px 16px rgba(0, 0, 0, 0.1);
      margin-top: 5px;
      display: none;
    }

    .info-expanded.active {
      display: block;
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
      word-break: break-all;
    }

    [title] {
      cursor: default;
    }

    .info-item a {
      --tw-text-opacity: 1;
      color: rgb(37 99 235 / var(--tw-text-opacity, 1));
      text-decoration: inherit;
    }

    .info-toggle {
      margin-left: 8px;
      width: 24px;
      height: 24px;
      border-radius: 50%;
      display: flex;
      align-items: center;
      justify-content: center;
      background: #f0f0f0;
      border: 1px solid #ddd;
      cursor: pointer;
      font-weight: bold;
      font-style: italic;
      color: #555;
      transition: all 0.2s ease;
    }

    .info-toggle:hover {
      background: #e0e0e0;
    }

    .info-toggle.active {
      background: #4a90e2;
      color: white;
      border-color: #3a80d2;
    }

    .main-info-grid {
      display: grid;
      grid-template-columns: 1fr 1fr;
      gap: 10px;
      width: 100%;
    }

    .info-column {
      display: flex;
      flex-direction: column;
      gap: 2px;
    }

    .detailed-info-grid {
      display: grid;
      grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
      gap: 8px;
      margin-top: 10px;
    }

    .ssh-section {
      margin-top: 10px;
      padding-top: 10px;
      border-top: 1px solid #eee;
    }

    .ssh-command {
      display: flex;
      align-items: center;
      margin-bottom: 8px;
      gap: 10px;
    }

    .ssh-command-text {
      font-family: monospace;
      font-size: 12px;
      background: #f5f5f5;
      padding: 4px 8px;
      border-radius: 4px;
      border: 1px solid #e0e0e0;
      flex-grow: 1;
    }

    .copy-button {
      background: #f0f0f0;
      border: 1px solid #ddd;
      border-radius: 4px;
      padding: 3px 6px;
      font-size: 11px;
      cursor: pointer;
      transition: all 0.2s;
    }

    .copy-button:hover {
      background: #e0e0e0;
    }

    .ssh-warning {
      background: #fff3e0;
      border-left: 3px solid #ff9800;
      padding: 8px 12px;
      margin-top: 8px;
      font-size: 12px;
      color: #e65100;
    }

    .vscode-link {
      color: white;
      text-decoration: none;
      background-color: #0066b8;
      padding: 4px 8px;
      border-radius: 4px;
      display: flex;
      align-items: center;
      gap: 6px;
      font-size: 12px;
      transition: all 0.2s ease;
    }

    .vscode-link:hover {
      background-color: #005091;
    }

    .vscode-icon {
      width: 16px;
      height: 16px;
    }

    .github-link {
      color: #2962ff;
      text-decoration: none;
    }

    .github-link:hover {
      text-decoration: underline;
    }

    .commit-info-container {
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .commit-info-container .copy-icon {
      opacity: 0.7;
      display: flex;
      align-items: center;
    }

    .commit-info-container .copy-icon svg {
      vertical-align: middle;
    }

    .commit-info-container:hover .copy-icon {
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
      width: 16px;
      height: 16px;
    }
  `;

  constructor() {
    super();
    this._toggleInfoDetails = this._toggleInfoDetails.bind(this);

    // Close the info panel when clicking outside of it
    document.addEventListener("click", (event) => {
      if (this.showDetails && !this.contains(event.target as Node)) {
        this.showDetails = false;
        this.requestUpdate();
      }
    });
  }

  /**
   * Toggle the display of detailed information
   */
  private _toggleInfoDetails(event: Event) {
    event.stopPropagation();
    this.showDetails = !this.showDetails;
    this.requestUpdate();
  }

  /**
   * Update the last commit information based on messages
   */
  public updateLastCommitInfo(newMessages: AgentMessage[]): void {
    if (!newMessages || newMessages.length === 0) return;

    // Process messages in chronological order (latest last)
    for (const message of newMessages) {
      if (
        message.type === "commit" &&
        message.commits &&
        message.commits.length > 0
      ) {
        // Get the first commit from the list
        const commit = message.commits[0];
        if (commit) {
          // Check if the commit hash has changed
          const hasChanged =
            !this.lastCommit || this.lastCommit.hash !== commit.hash;

          this.lastCommit = {
            hash: commit.hash,
            pushedBranch: commit.pushed_branch,
          };
          this.lastCommitCopied = false;

          // Add pulse animation if the commit changed
          if (hasChanged) {
            // Find the last commit element
            setTimeout(() => {
              const lastCommitEl =
                this.shadowRoot?.querySelector(".last-commit-main");
              if (lastCommitEl) {
                // Add the pulse class
                lastCommitEl.classList.add("pulse");

                // Remove the pulse class after animation completes
                setTimeout(() => {
                  lastCommitEl.classList.remove("pulse");
                }, 1500);
              }
            }, 0);
          }
        }
      }
    }
  }

  /**
   * Copy commit info to clipboard when clicked
   */
  private copyCommitInfo(event: MouseEvent): void {
    event.preventDefault();
    event.stopPropagation();

    if (!this.lastCommit) return;

    const textToCopy =
      this.lastCommit.pushedBranch || this.lastCommit.hash.substring(0, 8);

    navigator.clipboard
      .writeText(textToCopy)
      .then(() => {
        this.lastCommitCopied = true;
        // Reset the copied state after 1.5 seconds
        setTimeout(() => {
          this.lastCommitCopied = false;
        }, 1500);
      })
      .catch((err) => {
        console.error("Failed to copy commit info:", err);
      });
  }

  formatHostname() {
    // Only display outside hostname
    const outsideHostname = this.state?.outside_hostname;

    if (!outsideHostname) {
      return this.state?.hostname;
    }

    return outsideHostname;
  }

  formatWorkingDir() {
    // Only display outside working directory
    const outsideWorkingDir = this.state?.outside_working_dir;

    if (!outsideWorkingDir) {
      return this.state?.working_dir;
    }

    return outsideWorkingDir;
  }

  getHostnameTooltip() {
    const outsideHostname = this.state?.outside_hostname;
    const insideHostname = this.state?.inside_hostname;

    if (
      !outsideHostname ||
      !insideHostname ||
      outsideHostname === insideHostname
    ) {
      return "";
    }

    return `Outside: ${outsideHostname}, Inside: ${insideHostname}`;
  }

  getWorkingDirTooltip() {
    const outsideWorkingDir = this.state?.outside_working_dir;
    const insideWorkingDir = this.state?.inside_working_dir;

    if (
      !outsideWorkingDir ||
      !insideWorkingDir ||
      outsideWorkingDir === insideWorkingDir
    ) {
      return "";
    }

    return `Outside: ${outsideWorkingDir}, Inside: ${insideWorkingDir}`;
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

  copyToClipboard(text: string) {
    navigator.clipboard
      .writeText(text)
      .then(() => {
        // Could add a temporary success indicator here
      })
      .catch((err) => {
        console.error("Could not copy text: ", err);
      });
  }

  getSSHHostname() {
    return `sketch-${this.state?.session_id}`;
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

  renderSSHSection() {
    // Only show SSH section if we're in a Docker container and have session ID
    if (!this.state?.session_id) {
      return html``;
    }

    const sshHost = this.getSSHHostname();
    const sshCommand = `ssh ${sshHost}`;
    const vscodeCommand = `code --remote ssh-remote+root@${sshHost} /app -n`;
    const vscodeURL = `vscode://vscode-remote/ssh-remote+root@${sshHost}/app?windowId=_blank`;

    if (!this.state?.ssh_available) {
      return html`
        <div class="ssh-section">
          <h3>Connect to Container</h3>
          <div class="ssh-warning">
            SSH connections are not available:
            ${this.state?.ssh_error || "SSH configuration is missing"}
          </div>
        </div>
      `;
    }

    return html`
      <div class="ssh-section">
        <h3>Connect to Container</h3>
        <div class="ssh-command">
          <div class="ssh-command-text">${sshCommand}</div>
          <button
            class="copy-button"
            @click=${() => this.copyToClipboard(sshCommand)}
          >
            Copy
          </button>
        </div>
        <div class="ssh-command">
          <div class="ssh-command-text">${vscodeCommand}</div>
          <button
            class="copy-button"
            @click=${() => this.copyToClipboard(vscodeCommand)}
          >
            Copy
          </button>
        </div>
        <div class="ssh-command">
          <a href="${vscodeURL}" class="vscode-link" title="${vscodeURL}">
            <svg
              class="vscode-icon"
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="none"
              stroke="white"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <path
                d="M16.5 9.4 7.55 4.24a.35.35 0 0 0-.41.01l-1.23.93a.35.35 0 0 0-.14.29v13.04c0 .12.07.23.17.29l1.24.93c.13.1.31.09.43-.01L16.5 14.6l-6.39 4.82c-.16.12-.38.12-.55.01l-1.33-1.01a.35.35 0 0 1-.14-.28V5.88c0-.12.07-.23.18-.29l1.23-.93c.14-.1.32-.1.46 0l6.54 4.92-6.54 4.92c-.14.1-.32.1-.46 0l-1.23-.93a.35.35 0 0 1-.18-.29V5.88c0-.12.07-.23.17-.29l1.33-1.01c.16-.12.39-.11.55.01l6.39 4.81z"
              />
            </svg>
            <span>Open in VSCode</span>
          </a>
        </div>
      </div>
    `;
  }

  render() {
    return html`
      <div class="info-container">
        <!-- Main visible info in two columns - github/hostname/dir and last commit -->
        <div class="main-info-grid">
          <!-- First column: GitHub repo (or hostname) and working dir -->
          <div class="info-column">
            <div class="info-item">
              ${(() => {
                const github = this.formatGitHubRepo(this.state?.git_origin);
                if (github) {
                  return html`
                    <a
                      href="${github.url}"
                      target="_blank"
                      rel="noopener noreferrer"
                      class="github-link"
                      title="${this.state?.git_origin}"
                    >
                      ${github.formatted}
                    </a>
                  `;
                } else {
                  return html`
                    <span
                      id="hostname"
                      class="info-value"
                      title="${this.getHostnameTooltip()}"
                    >
                      ${this.formatHostname()}
                    </span>
                  `;
                }
              })()}
            </div>
            <div class="info-item">
              <span
                id="workingDir"
                class="info-value"
                title="${this.getWorkingDirTooltip()}"
              >
                ${this.formatWorkingDir()}
              </span>
            </div>
          </div>

          <!-- Second column: Last Commit -->
          <div class="info-column last-commit-column">
            <div class="info-item">
              <span class="info-label">Last Commit</span>
            </div>
            <div
              class="info-item last-commit-main"
              @click=${(e: MouseEvent) => this.copyCommitInfo(e)}
              title="Click to copy"
            >
              ${this.lastCommit
                ? this.lastCommit.pushedBranch
                  ? (() => {
                      const githubLink = this.getGitHubBranchLink(
                        this.lastCommit.pushedBranch,
                      );
                      return html`
                        <div class="commit-info-container">
                          <span
                            class="commit-branch-indicator main-grid-commit"
                            title="Click to copy: ${this.lastCommit
                              .pushedBranch}"
                            @click=${(e) => this.copyCommitInfo(e)}
                            >${this.lastCommit.pushedBranch}</span
                          >
                          <span class="copy-icon">
                            ${this.lastCommitCopied
                              ? html`<svg
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
                                </svg>`
                              : html`<svg
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
                                </svg>`}
                          </span>
                          ${githubLink
                            ? html`<a
                                href="${githubLink}"
                                target="_blank"
                                rel="noopener noreferrer"
                                class="octocat-link"
                                title="Open ${this.lastCommit
                                  .pushedBranch} on GitHub"
                                @click=${(e) => e.stopPropagation()}
                              >
                                <svg
                                  class="octocat-icon"
                                  viewBox="0 0 16 16"
                                  width="16"
                                  height="16"
                                >
                                  <path
                                    fill="currentColor"
                                    d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.013 8.013 0 0016 8c0-4.42-3.58-8-8-8z"
                                  />
                                </svg>
                              </a>`
                            : ""}
                        </div>
                      `;
                    })()
                  : html`<span class="commit-hash-indicator main-grid-commit"
                      >${this.lastCommit.hash.substring(0, 8)}</span
                    >`
                : html`<span class="no-commit-indicator">N/A</span>`}
              <span class="copy-icon">
                ${this.lastCommitCopied
                  ? html`<svg
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
                    </svg>`
                  : html`<svg
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
                    </svg>`}
              </span>
            </div>
          </div>
        </div>

        <!-- Info toggle button -->
        <button
          class="info-toggle ${this.showDetails ? "active" : ""}"
          @click=${this._toggleInfoDetails}
          title="Show/hide details"
        >
          i
        </button>

        <!-- Expanded info panel -->
        <div class="info-expanded ${this.showDetails ? "active" : ""}">
          <!-- Last Commit section moved to main grid -->

          <div class="detailed-info-grid">
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
              <span class="info-label">Session ID:</span>
              <span id="sessionId" class="info-value"
                >${this.state?.session_id || "N/A"}</span
              >
            </div>
            <div class="info-item">
              <span class="info-label">Hostname:</span>
              <span
                id="hostnameDetail"
                class="info-value"
                title="${this.getHostnameTooltip()}"
              >
                ${this.formatHostname()}
              </span>
            </div>
            ${this.state?.agent_state
              ? html`
                  <div class="info-item">
                    <span class="info-label">Agent State:</span>
                    <span id="agentState" class="info-value"
                      >${this.state?.agent_state}</span
                    >
                  </div>
                `
              : ""}
            <div class="info-item">
              <span class="info-label">Input tokens:</span>
              <span id="inputTokens" class="info-value"
                >${formatNumber(
                  (this.state?.total_usage?.input_tokens || 0) +
                    (this.state?.total_usage?.cache_read_input_tokens || 0) +
                    (this.state?.total_usage?.cache_creation_input_tokens || 0),
                )}</span
              >
            </div>
            <div class="info-item">
              <span class="info-label">Output tokens:</span>
              <span id="outputTokens" class="info-value"
                >${formatNumber(this.state?.total_usage?.output_tokens)}</span
              >
            </div>
            ${(this.state?.total_usage?.total_cost_usd || 0) > 0
              ? html`
                  <div class="info-item">
                    <span class="info-label">Total cost:</span>
                    <span id="totalCost" class="info-value cost"
                      >$${(this.state?.total_usage?.total_cost_usd).toFixed(
                        2,
                      )}</span
                    >
                  </div>
                `
              : ""}
            <div
              class="info-item"
              style="grid-column: 1 / -1; margin-top: 5px; border-top: 1px solid #eee; padding-top: 5px;"
            >
              <a href="logs">Logs</a> (<a href="download">Download</a>)
            </div>
          </div>

          <!-- SSH Connection Information -->
          ${this.renderSSHSection()}
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
