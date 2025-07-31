import { html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { SketchTailwindElement } from "./sketch-tailwind-element.js";
import "./sketch-monaco-view";
import "./sketch-diff-range-picker";
import "./sketch-diff-empty-view";
import { GitDiffFile, GitDataService } from "./git-data-service";
import { DiffRange } from "./sketch-diff-range-picker";

/**
 * A component that displays diffs using Monaco editor with range and file pickers
 */
@customElement("sketch-diff2-view")
export class SketchDiff2View extends SketchTailwindElement {
  /**
   * Handles comment events from the Monaco editor and forwards them to the chat input
   * using the same event format as the original diff view for consistency.
   */
  private handleMonacoComment(event: CustomEvent) {
    try {
      // Validate incoming data
      if (!event.detail || !event.detail.formattedComment) {
        console.error("Invalid comment data received");
        return;
      }

      // Create and dispatch event using the standardized format
      const commentEvent = new CustomEvent("diff-comment", {
        detail: { comment: event.detail.formattedComment },
        bubbles: true,
        composed: true,
      });

      this.dispatchEvent(commentEvent);
    } catch (error) {
      console.error("Error handling Monaco comment:", error);
    }
  }

  /**
   * Handle height change events from the Monaco editor
   */
  private handleMonacoHeightChange(event: CustomEvent) {
    try {
      // Get the monaco view that emitted the event
      const monacoView = event.target as HTMLElement;
      if (!monacoView) return;

      // Find the parent Monaco editor container (now using Tailwind classes)
      const fileDiffEditor = monacoView.closest(
        ".flex.flex-col.w-full.min-h-\\[200px\\].flex-1",
      ) as HTMLElement;
      if (!fileDiffEditor) return;

      // Get the new height from the event
      const newHeight = event.detail.height;

      // Only update if the height actually changed to avoid unnecessary layout
      const currentHeight = fileDiffEditor.style.height;
      const newHeightStr = `${newHeight}px`;

      if (currentHeight !== newHeightStr) {
        // Update the container height to match monaco's height
        fileDiffEditor.style.height = newHeightStr;

        // Remove any previous min-height constraint that might interfere
        fileDiffEditor.style.minHeight = "auto";

        // IMPORTANT: Tell Monaco to relayout after its container size changed
        // Monaco has automaticLayout: false, so it won't detect container changes
        setTimeout(() => {
          const monacoComponent = monacoView as any;
          if (monacoComponent && monacoComponent.editor) {
            // Force layout with explicit dimensions to ensure Monaco fills the space
            const editorWidth = fileDiffEditor.offsetWidth;
            monacoComponent.editor.layout({
              width: editorWidth,
              height: newHeight,
            });
          }
        }, 0);
      }
    } catch (error) {
      console.error("Error handling Monaco height change:", error);
    }
  }

  /**
   * Handle save events from the Monaco editor
   */
  private async handleMonacoSave(event: CustomEvent) {
    try {
      // Validate incoming data
      if (
        !event.detail ||
        !event.detail.path ||
        event.detail.content === undefined
      ) {
        console.error("Invalid save data received");
        return;
      }

      const { path, content } = event.detail;

      // Get Monaco view component
      const monacoView = this.querySelector("sketch-monaco-view");
      if (!monacoView) {
        console.error("Monaco view not found");
        return;
      }

      try {
        await this.gitService?.saveFileContent(path, content);
        console.log(`File saved: ${path}`);
        (monacoView as any).notifySaveComplete(true);
      } catch (error) {
        const errorMessage =
          error instanceof Error ? error.message : String(error);
        alert(`Failed to save changes to ${path}:\n\n${errorMessage}`);
        (monacoView as any).notifySaveComplete(false);
      }
    } catch (error) {
      console.error("Error handling save:", error);
    }
  }
  @property({ type: String })
  initialCommit: string = "";

  // The commit to show - used when showing a specific commit from timeline
  @property({ type: String })
  commit: string = "";

  @property({ type: String })
  selectedFilePath: string = "";

  @state()
  private files: GitDiffFile[] = [];

  @state()
  private currentRange: DiffRange = { type: "range", from: "", to: "HEAD" };

  @state()
  private fileContents: Map<
    string,
    { original: string; modified: string; editable: boolean }
  > = new Map();

  @state()
  private fileExpandStates: Map<string, boolean> = new Map();

  @state()
  private loading: boolean = false;

  @state()
  private error: string | null = null;

  @state()
  private selectedFile: string = ""; // Empty string means "All files"

  @state()
  private viewMode: "all" | "single" = "all";

  @state()
  private untrackedFiles: string[] = [];

  @state()
  private showUntrackedPopup: boolean = false;

  @property({ attribute: false, type: Object })
  gitService!: GitDataService;

  // The gitService must be passed from parent to ensure proper dependency injection

  constructor() {
    super();
    console.log("SketchDiff2View initialized");

    // Fix for monaco-aria-container positioning and hide scrollbars globally
    // Add a global style to ensure proper positioning of aria containers and hide scrollbars
    const styleElement = document.createElement("style");
    styleElement.textContent = `
      .monaco-aria-container {
        position: absolute !important;
        top: 0 !important;
        left: 0 !important;
        width: 1px !important;
        height: 1px !important;
        overflow: hidden !important;
        clip: rect(1px, 1px, 1px, 1px) !important;
        white-space: nowrap !important;
        margin: 0 !important;
        padding: 0 !important;
        border: 0 !important;
        z-index: -1 !important;
      }

      /* Aggressively hide all Monaco scrollbar elements */
      .monaco-editor .scrollbar,
      .monaco-editor .scroll-decoration,
      .monaco-editor .invisible.scrollbar,
      .monaco-editor .slider,
      .monaco-editor .vertical.scrollbar,
      .monaco-editor .horizontal.scrollbar,
      .monaco-diff-editor .scrollbar,
      .monaco-diff-editor .scroll-decoration,
      .monaco-diff-editor .invisible.scrollbar,
      .monaco-diff-editor .slider,
      .monaco-diff-editor .vertical.scrollbar,
      .monaco-diff-editor .horizontal.scrollbar {
        display: none !important;
        visibility: hidden !important;
        width: 0 !important;
        height: 0 !important;
        opacity: 0 !important;
      }

      /* Target the specific scrollbar classes that Monaco uses */
      .monaco-scrollable-element > .scrollbar,
      .monaco-scrollable-element > .scroll-decoration,
      .monaco-scrollable-element .slider {
        display: none !important;
        visibility: hidden !important;
        width: 0 !important;
        height: 0 !important;
      }

      /* Remove scrollbar space/padding from content area */
      .monaco-editor .monaco-scrollable-element,
      .monaco-diff-editor .monaco-scrollable-element {
        padding-right: 0 !important;
        padding-bottom: 0 !important;
        margin-right: 0 !important;
        margin-bottom: 0 !important;
      }

      /* Ensure the diff content takes full width without scrollbar space */
      .monaco-diff-editor .editor.modified,
      .monaco-diff-editor .editor.original {
        margin-right: 0 !important;
        padding-right: 0 !important;
      }
  `;
    document.head.appendChild(styleElement);
  }

  // Override createRenderRoot to apply host styles for proper sizing while still using light DOM
  createRenderRoot() {
    // Use light DOM like SketchTailwindElement but still apply host styles
    const style = document.createElement("style");
    style.textContent = `
      sketch-diff2-view {
        height: -webkit-fill-available;
      }
    `;

    // Add the style to the document head if not already present
    if (!document.head.querySelector("style[data-sketch-diff2-view]")) {
      style.setAttribute("data-sketch-diff2-view", "");
      document.head.appendChild(style);
    }

    return this;
  }

  connectedCallback() {
    super.connectedCallback();
    // Initialize with default range and load data
    // Get base commit if not set
    if (
      this.currentRange.type === "range" &&
      !("from" in this.currentRange && this.currentRange.from)
    ) {
      this.gitService
        .getBaseCommitRef()
        .then((baseRef) => {
          this.currentRange = { type: "range", from: baseRef, to: "HEAD" };
          this.loadDiffData();
        })
        .catch((error) => {
          console.error("Error getting base commit ref:", error);
          // Use default range
          this.loadDiffData();
        });
    } else {
      this.loadDiffData();
    }

    // Add click listener to close popup when clicking outside
    document.addEventListener("click", this.handleDocumentClick.bind(this));
  }

  disconnectedCallback() {
    super.disconnectedCallback();
    document.removeEventListener("click", this.handleDocumentClick.bind(this));
  }

  handleDocumentClick(event: Event) {
    if (this.showUntrackedPopup) {
      const target = event.target as HTMLElement;
      // Check if click is outside the popup and button
      if (!target.closest(".relative")) {
        this.showUntrackedPopup = false;
      }
    }
  }

  // Toggle hideUnchangedRegions setting for a specific file
  private toggleFileExpansion(filePath: string) {
    const currentState = this.fileExpandStates.get(filePath) ?? false;
    const newState = !currentState;
    this.fileExpandStates.set(filePath, newState);

    // Apply to the specific Monaco view component for this file
    const monacoView = this.querySelector(
      `sketch-monaco-view[data-file-path="${filePath}"]`,
    );
    if (monacoView) {
      (monacoView as any).toggleHideUnchangedRegions(!newState); // inverted because true means "hide unchanged"
    }

    // Force a re-render to update the button state
    this.requestUpdate();
  }

  render() {
    return html`
      <div class="px-4 py-2 border-b border-gray-300 dark:border-neutral-600 bg-gray-100 dark:bg-neutral-800 flex-shrink-0">
        <div class="flex flex-col gap-3">
          <div class="w-full flex items-center gap-3">
            <sketch-diff-range-picker
              class="flex-1 min-w-[400px]"
              .gitService="${this.gitService}"
              @range-change="${this.handleRangeChange}"
            ></sketch-diff-range-picker>
            ${this.renderUntrackedFilesNotification()}
            <div class="flex-1"></div>
            ${this.renderFileSelector()}
          </div>
        </div>
      </div>

      <div class="flex-1 overflow-auto flex flex-col min-h-0 relative h-full">
        ${this.renderDiffContent()}
      </div>
    `;
  }

  renderFileSelector() {
    const fileCount = this.files.length;

    return html`
      <div class="flex items-center gap-2">
        <select
          class="min-w-[200px] px-3 py-2 border border-gray-400 dark:border-neutral-600 rounded bg-white dark:bg-neutral-800 text-sm text-gray-900 dark:text-neutral-100 cursor-pointer focus:outline-none focus:border-blue-500 focus:ring-2 focus:ring-blue-200 disabled:bg-gray-100 dark:disabled:bg-neutral-700 disabled:text-gray-500 dark:disabled:text-neutral-500 disabled:cursor-not-allowed"
          .value="${this.selectedFile}"
          @change="${this.handleFileSelection}"
          ?disabled="${fileCount === 0}"
        >
          <option value="">All files (${fileCount})</option>
          ${this.files.map(
            (file) => html`
              <option value="${file.path}">
                ${this.getFileDisplayName(file)}
              </option>
            `,
          )}
        </select>
        ${this.selectedFile ? this.renderSingleFileExpandButton() : ""}
      </div>
    `;
  }

  renderUntrackedFilesNotification() {
    if (!this.untrackedFiles || this.untrackedFiles.length === 0) {
      return "";
    }

    const fileCount = this.untrackedFiles.length;
    const fileCountText =
      fileCount === 1 ? "1 untracked file" : `${fileCount} untracked files`;

    return html`
      <div class="relative">
        <button
          class="flex items-center gap-2 px-3 py-1.5 text-sm bg-gray-100 dark:bg-neutral-700 text-gray-700 dark:text-neutral-300 border border-gray-300 dark:border-neutral-600 rounded hover:bg-gray-200 dark:hover:bg-neutral-600 focus:outline-none focus:ring-2 focus:ring-blue-500"
          @click="${this.toggleUntrackedFilesPopup}"
          type="button"
        >
          ${fileCount} untracked
          <svg
            class="w-4 h-4"
            fill="none"
            stroke="currentColor"
            viewBox="0 0 24 24"
          >
            <path
              stroke-linecap="round"
              stroke-linejoin="round"
              stroke-width="2"
              d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
            />
          </svg>
        </button>

        ${this.showUntrackedPopup
          ? html`
              <div
                class="absolute top-full left-0 mt-2 w-80 bg-white dark:bg-neutral-800 border border-gray-300 dark:border-neutral-600 rounded-lg shadow-lg z-50"
              >
                <div class="p-4">
                  <div class="flex items-start gap-3 mb-3">
                    <svg
                      class="w-5 h-5 text-blue-600 flex-shrink-0 mt-0.5"
                      fill="none"
                      stroke="currentColor"
                      viewBox="0 0 24 24"
                    >
                      <path
                        stroke-linecap="round"
                        stroke-linejoin="round"
                        stroke-width="2"
                        d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z"
                      />
                    </svg>
                    <div class="flex-1">
                      <div class="font-medium text-gray-900 dark:text-neutral-100 mb-1">
                        ${fileCountText}
                      </div>
                      <div class="text-sm text-gray-600 dark:text-neutral-400 mb-3">
                        These files are not tracked by git. They will be lost if
                        the session ends now. The agent typically does not add
                        files to git until it is ready for feedback.
                      </div>
                    </div>
                  </div>

                  <div class="max-h-32 overflow-y-auto">
                    <div class="text-sm text-gray-700 dark:text-neutral-300">
                      ${this.untrackedFiles.map(
                        (file) => html`
                          <div
                            class="py-1 px-2 hover:bg-gray-100 dark:hover:bg-neutral-700 rounded font-mono text-xs"
                          >
                            ${file}
                          </div>
                        `,
                      )}
                    </div>
                  </div>
                </div>
              </div>
            `
          : ""}
      </div>
    `;
  }

  renderDiffContent() {
    if (this.loading) {
      return html`<div class="flex items-center justify-center h-full">
        Loading diff...
      </div>`;
    }

    if (this.error) {
      return html`<div class="text-red-600 dark:text-red-400 p-4">${this.error}</div>`;
    }

    if (this.files.length === 0) {
      return html`<sketch-diff-empty-view></sketch-diff-empty-view>`;
    }

    // Render single file view if a specific file is selected
    if (this.selectedFile && this.viewMode === "single") {
      return this.renderSingleFileView();
    }

    // Render multi-file view
    return html`
      <div class="flex flex-col w-full min-h-full">
        ${this.files.map((file, index) => this.renderFileDiff(file, index))}
      </div>
    `;
  }

  /**
   * Load diff data for the current range
   */
  async loadDiffData() {
    this.loading = true;
    this.error = null;

    try {
      // Initialize files as empty array if undefined
      if (!this.files) {
        this.files = [];
      }

      // Load diff data for the range
      this.files = await this.gitService.getDiff(
        this.currentRange.from,
        this.currentRange.to,
      );

      // Ensure files is always an array, even when API returns null
      if (!this.files) {
        this.files = [];
      }

      // Load untracked files for notification
      try {
        this.untrackedFiles = await this.gitService.getUntrackedFiles();
      } catch (error) {
        console.error("Error loading untracked files:", error);
        this.untrackedFiles = [];
      }

      // Load content for all files
      if (this.files.length > 0) {
        // Initialize expand states for new files (default to collapsed)
        this.files.forEach((file) => {
          if (!this.fileExpandStates.has(file.path)) {
            this.fileExpandStates.set(file.path, false); // false = collapsed (hide unchanged regions)
          }
        });
        await this.loadAllFileContents();
      } else {
        // No files to display - reset the view to initial state
        this.selectedFilePath = "";
        this.selectedFile = "";
        this.viewMode = "all";
        this.fileContents.clear();
        this.fileExpandStates.clear();
      }
    } catch (error) {
      console.error("Error loading diff data:", error);
      this.error = `Error loading diff data: ${error.message}`;
      // Ensure files is an empty array when an error occurs
      this.files = [];
      // Reset the view to initial state
      this.selectedFilePath = "";
      this.selectedFile = "";
      this.viewMode = "all";
      this.fileContents.clear();
      this.fileExpandStates.clear();
    } finally {
      this.loading = false;
      // Request a re-render.
      // Empirically, without this line, diffs are visibly slow to load.
      this.requestUpdate();
    }
  }

  /**
   * Load content for all files in the diff
   */
  async loadAllFileContents() {
    this.loading = true;
    this.error = null;
    this.fileContents.clear();

    try {
      let isUnstagedChanges = false;

      // Determine the commits to compare based on the current range
      const _fromCommit = this.currentRange.from;
      const toCommit = this.currentRange.to;
      // Check if this is an unstaged changes view
      isUnstagedChanges = toCommit === "";

      // Load content for all files
      const promises = this.files.map(async (file) => {
        try {
          let originalCode = "";
          let modifiedCode = "";
          let editable = isUnstagedChanges;

          // Load the original code based on file status
          if (file.status !== "A") {
            // For modified, renamed, or deleted files: load original content
            originalCode = await this.gitService.getFileContent(
              file.old_hash || "",
            );
          }

          // For modified code, always use working copy when editable
          if (editable) {
            try {
              // Always use working copy when editable, regardless of diff status
              modifiedCode = await this.gitService.getWorkingCopyContent(
                file.path,
              );
            } catch (error) {
              if (file.status === "D") {
                // For deleted files, silently use empty content
                console.warn(
                  `Could not get working copy for deleted file ${file.path}, using empty content`,
                );
                modifiedCode = "";
              } else {
                // For any other file status, propagate the error
                console.error(
                  `Failed to get working copy for ${file.path}:`,
                  error,
                );
                throw error;
              }
            }
          } else {
            // For non-editable view, use git content based on file status
            if (file.status === "D") {
              // Deleted file: empty modified
              modifiedCode = "";
            } else {
              // Added/modified/renamed: use the content from git
              modifiedCode = await this.gitService.getFileContent(
                file.new_hash || "",
              );
            }
          }

          // Don't make deleted files editable
          if (file.status === "D") {
            editable = false;
          }

          this.fileContents.set(file.path, {
            original: originalCode,
            modified: modifiedCode,
            editable,
          });
        } catch (error) {
          console.error(`Error loading content for file ${file.path}:`, error);
          // Store empty content for failed files to prevent blocking
          this.fileContents.set(file.path, {
            original: "",
            modified: "",
            editable: false,
          });
        }
      });

      await Promise.all(promises);
    } catch (error) {
      console.error("Error loading file contents:", error);
      this.error = `Error loading file contents: ${error.message}`;
    } finally {
      this.loading = false;
    }
  }

  /**
   * Handle range change event from the range picker
   */
  handleRangeChange(event: CustomEvent) {
    const { range } = event.detail;
    console.log("Range changed:", range);
    this.currentRange = range;

    // Load diff data for the new range
    this.loadDiffData();
  }

  /**
   * Render a single file diff section
   */
  renderFileDiff(file: GitDiffFile, index: number) {
    const content = this.fileContents.get(file.path);
    if (!content) {
      return html`
        <div
          class="flex flex-col border-b-4 border-gray-300 dark:border-neutral-600 mb-0 last:border-b-0"
        >
          <div
            class="bg-gray-100 dark:bg-neutral-800 border-b border-gray-300 dark:border-neutral-600 px-4 py-2 font-medium text-sm text-gray-800 dark:text-neutral-200 sticky top-0 z-10 shadow-sm flex justify-between items-center"
          >
            ${this.renderFileHeader(file)}
          </div>
          <div class="flex items-center justify-center h-full">
            Loading ${file.path}...
          </div>
        </div>
      `;
    }

    return html`
      <div
        class="flex flex-col border-b-4 border-gray-300 dark:border-neutral-600 mb-0 last:border-b-0"
      >
        <div
          class="bg-gray-100 border-b border-gray-300 px-4 py-2 font-medium text-sm text-gray-800 sticky top-0 z-10 shadow-sm flex justify-between items-center"
        >
          ${this.renderFileHeader(file)}
        </div>
        <div class="flex flex-col w-full min-h-[200px] flex-1">
          <sketch-monaco-view
            class="flex flex-col w-full min-h-[200px] flex-1"
            .originalCode="${content.original}"
            .modifiedCode="${content.modified}"
            .originalFilename="${file.path}"
            .modifiedFilename="${file.path}"
            ?readOnly="${!content.editable}"
            ?editable-right="${content.editable}"
            @monaco-comment="${this.handleMonacoComment}"
            @monaco-save="${this.handleMonacoSave}"
            @monaco-height-changed="${this.handleMonacoHeightChange}"
            data-file-index="${index}"
            data-file-path="${file.path}"
          ></sketch-monaco-view>
        </div>
      </div>
    `;
  }

  /**
   * Render file header with status and path info
   */
  renderFileHeader(file: GitDiffFile) {
    const statusClasses = this.getFileStatusTailwindClasses(file.status);
    const statusText = this.getFileStatusText(file.status);
    const changesInfo = this.getChangesInfo(file);
    const pathInfo = this.getPathInfo(file);

    const isExpanded = this.fileExpandStates.get(file.path) ?? false;

    return html`
      <div class="flex items-center gap-2">
        <span
          class="inline-block px-1.5 py-0.5 rounded text-xs font-bold mr-2 ${statusClasses}"
        >
          ${statusText}
        </span>
        <span class="font-mono font-normal text-gray-600">${pathInfo}</span>
        ${changesInfo
          ? html`<span class="ml-2 text-xs text-gray-600">${changesInfo}</span>`
          : ""}
      </div>
      <div class="flex items-center">
        <button
          class="bg-transparent border border-gray-300 rounded px-2 py-1 text-sm cursor-pointer whitespace-nowrap transition-colors duration-200 flex items-center justify-center min-w-8 min-h-8 hover:bg-gray-200"
          @click="${() => this.toggleFileExpansion(file.path)}"
          title="${isExpanded
            ? "Collapse: Hide unchanged regions to focus on changes"
            : "Expand: Show all lines including unchanged regions"}"
        >
          ${isExpanded ? this.renderCollapseIcon() : this.renderExpandAllIcon()}
        </button>
      </div>
    `;
  }

  /**
   * Get Tailwind CSS classes for file status
   */
  getFileStatusTailwindClasses(status: string): string {
    switch (status.toUpperCase()) {
      case "A":
        return "bg-green-100 text-green-800";
      case "M":
        return "bg-yellow-100 text-yellow-800";
      case "D":
        return "bg-red-100 text-red-800";
      case "R":
      case "C":
      default:
        if (status.toUpperCase().startsWith("R")) {
          return "bg-cyan-100 text-cyan-800";
        }
        if (status.toUpperCase().startsWith("C")) {
          return "bg-indigo-100 text-indigo-800";
        }
        return "bg-yellow-100 text-yellow-800";
    }
  }

  /**
   * Get display text for file status
   */
  getFileStatusText(status: string): string {
    switch (status.toUpperCase()) {
      case "A":
        return "Added";
      case "M":
        return "Modified";
      case "D":
        return "Deleted";
      case "R":
      case "C":
      default:
        if (status.toUpperCase().startsWith("R")) {
          return "Renamed";
        }
        if (status.toUpperCase().startsWith("C")) {
          return "Copied";
        }
        return "Modified";
    }
  }

  /**
   * Get changes information (+/-) for display
   */
  getChangesInfo(file: GitDiffFile): string {
    const additions = file.additions || 0;
    const deletions = file.deletions || 0;

    if (additions === 0 && deletions === 0) {
      return "";
    }

    const parts = [];
    if (additions > 0) {
      parts.push(`+${additions}`);
    }
    if (deletions > 0) {
      parts.push(`-${deletions}`);
    }

    return `(${parts.join(", ")})`;
  }

  /**
   * Get path information for display, handling renames
   */
  getPathInfo(file: GitDiffFile): string {
    if (file.old_path && file.old_path !== "") {
      // For renames, show old_path → new_path
      return `${file.old_path} → ${file.path}`;
    }
    // For regular files, just show the path
    return file.path;
  }

  /**
   * Render expand all icon (dotted line with arrows pointing away)
   */
  renderExpandAllIcon() {
    return html`
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <!-- Dotted line in the middle -->
        <line
          x1="2"
          y1="8"
          x2="14"
          y2="8"
          stroke="currentColor"
          stroke-width="1"
          stroke-dasharray="2,1"
        />
        <!-- Large arrow pointing up -->
        <path d="M8 2 L5 6 L11 6 Z" fill="currentColor" />
        <!-- Large arrow pointing down -->
        <path d="M8 14 L5 10 L11 10 Z" fill="currentColor" />
      </svg>
    `;
  }

  /**
   * Render collapse icon (arrows pointing towards dotted line)
   */
  renderCollapseIcon() {
    return html`
      <svg width="16" height="16" viewBox="0 0 16 16" fill="currentColor">
        <!-- Dotted line in the middle -->
        <line
          x1="2"
          y1="8"
          x2="14"
          y2="8"
          stroke="currentColor"
          stroke-width="1"
          stroke-dasharray="2,1"
        />
        <!-- Large arrow pointing down towards line -->
        <path d="M8 6 L5 2 L11 2 Z" fill="currentColor" />
        <!-- Large arrow pointing up towards line -->
        <path d="M8 10 L5 14 L11 14 Z" fill="currentColor" />
      </svg>
    `;
  }

  /**
   * Handle file selection change from the dropdown
   */
  handleFileSelection(event: Event) {
    const selectElement = event.target as HTMLSelectElement;
    const selectedValue = selectElement.value;

    this.selectedFile = selectedValue;
    this.viewMode = selectedValue ? "single" : "all";

    // Force re-render
    this.requestUpdate();
  }

  toggleUntrackedFilesPopup() {
    this.showUntrackedPopup = !this.showUntrackedPopup;
  }

  /**
   * Get display name for file in the selector
   */
  getFileDisplayName(file: GitDiffFile): string {
    const status = this.getFileStatusText(file.status);
    const pathInfo = this.getPathInfo(file);
    return `${status}: ${pathInfo}`;
  }

  /**
   * Render expand/collapse button for single file view in header
   */
  renderSingleFileExpandButton() {
    if (!this.selectedFile) return "";

    const isExpanded = this.fileExpandStates.get(this.selectedFile) ?? false;

    return html`
      <button
        class="bg-transparent border border-gray-300 rounded px-1.5 py-1.5 text-sm cursor-pointer whitespace-nowrap transition-colors duration-200 flex items-center justify-center min-w-8 min-h-8 hover:bg-gray-200"
        @click="${() => this.toggleFileExpansion(this.selectedFile)}"
        title="${isExpanded
          ? "Collapse: Hide unchanged regions to focus on changes"
          : "Expand: Show all lines including unchanged regions"}"
      >
        ${isExpanded ? this.renderCollapseIcon() : this.renderExpandAllIcon()}
      </button>
    `;
  }

  /**
   * Render single file view with full-screen Monaco editor
   */
  renderSingleFileView() {
    const selectedFileData = this.files.find(
      (f) => f.path === this.selectedFile,
    );
    if (!selectedFileData) {
      return html`<div class="text-red-600 p-4">Selected file not found</div>`;
    }

    const content = this.fileContents.get(this.selectedFile);
    if (!content) {
      return html`<div class="flex items-center justify-center h-full">
        Loading ${this.selectedFile}...
      </div>`;
    }

    return html`
      <div class="flex-1 flex flex-col h-full min-h-0">
        <sketch-monaco-view
          class="flex-1 w-full h-full min-h-0"
          .originalCode="${content.original}"
          .modifiedCode="${content.modified}"
          .originalFilename="${selectedFileData.path}"
          .modifiedFilename="${selectedFileData.path}"
          ?readOnly="${!content.editable}"
          ?editable-right="${content.editable}"
          @monaco-comment="${this.handleMonacoComment}"
          @monaco-save="${this.handleMonacoSave}"
          data-file-path="${selectedFileData.path}"
        ></sketch-monaco-view>
      </div>
    `;
  }

  /**
   * Refresh the diff view by reloading commits and diff data
   *
   * This is called when the Monaco diff tab is activated to ensure:
   * 1. Branch information from git/recentlog is current (branches can change frequently)
   * 2. The diff content is synchronized with the latest repository state
   * 3. Users always see up-to-date information without manual refresh
   */
  refreshDiffView() {
    // First refresh the range picker to get updated branch information
    const rangePicker = this.querySelector("sketch-diff-range-picker");
    if (rangePicker) {
      (rangePicker as any).loadCommits();
    }

    if (this.commit) {
      // Convert single commit to range (commit^ to commit)
      this.currentRange = {
        type: "range",
        from: `${this.commit}^`,
        to: this.commit,
      };
    }

    // Then reload diff data based on the current range
    this.loadDiffData();
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-diff2-view": SketchDiff2View;
  }
}
