import { css, html, LitElement } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { createRef, Ref, ref } from "lit/directives/ref.js";

// See https://rodydavis.com/posts/lit-monaco-editor for some ideas.

// Monaco is loaded dynamically - see loadMonaco() function
declare global {
  interface Window {
    monaco?: any;
  }
}

// Monaco hash will be injected at build time
declare const __MONACO_HASH__: string;

// Load Monaco editor dynamically
let monacoLoadPromise: Promise<any> | null = null;

function loadMonaco(): Promise<any> {
  if (monacoLoadPromise) {
    return monacoLoadPromise;
  }

  if (window.monaco) {
    return Promise.resolve(window.monaco);
  }

  monacoLoadPromise = new Promise((resolve, reject) => {
    const monacoHash = (typeof __MONACO_HASH__ !== 'undefined') ? __MONACO_HASH__ : 'dev';
    const script = document.createElement('script');
    script.type = 'module';
    script.onload = () => {
      // The Monaco bundle should set window.monaco
      if (window.monaco) {
        resolve(window.monaco);
      } else {
        reject(new Error('Monaco not loaded'));
      }
    };
    script.onerror = reject;
    script.src = `./monaco-standalone-${monacoHash}.js`;
    document.head.appendChild(script);
  });

  return monacoLoadPromise;
}

// Define Monaco CSS styles as a string constant
const monacoStyles = `
  /* Import Monaco editor styles */
  @import url('./static/monaco/min/vs/editor/editor.main.css');
  
  /* Codicon font is now defined globally in sketch-app-shell.css */
  
  /* Custom Monaco styles */
  .monaco-editor {
    width: 100%;
    height: 100%;
  }
  
  // /* Custom font stack - ensure we have good monospace fonts */
  // .monaco-editor .view-lines,
  // .monaco-editor .view-line,
  // .monaco-editor-pane,
  // .monaco-editor .inputarea {
  //   font-family: "Menlo", "Monaco", "Consolas", "Courier New", monospace !important;
  //   font-size: 13px !important;
  //   font-feature-settings: "liga" 0, "calt" 0 !important;
  //   line-height: 1.5 !important;
  // }
  
  /* Ensure light theme colors */
  .monaco-editor, .monaco-editor-background, .monaco-editor .inputarea.ime-input {
    background-color: var(--monaco-editor-bg, #ffffff) !important;
  }
  
  .monaco-editor .margin {
    background-color: var(--monaco-editor-margin, #f5f5f5) !important;
  }
  
  /* Hide all scrollbars completely */
  .monaco-editor .scrollbar,
  .monaco-editor .scroll-decoration,
  .monaco-editor .invisible.scrollbar,
  .monaco-editor .slider,
  .monaco-editor .vertical.scrollbar,
  .monaco-editor .horizontal.scrollbar {
    display: none !important;
    visibility: hidden !important;
    width: 0 !important;
    height: 0 !important;
  }
  
  /* Ensure content area takes full width/height without scrollbar space */
  .monaco-editor .monaco-scrollable-element {
    /* Remove any padding/margin that might be reserved for scrollbars */
    padding-right: 0 !important;
    padding-bottom: 0 !important;
  }
`;

// Configure Monaco to use local workers with correct relative paths
// Monaco looks for this global configuration to determine how to load web workers
// @ts-ignore - MonacoEnvironment is added to the global scope at runtime
self.MonacoEnvironment = {
  getWorkerUrl: function (_moduleId, label) {
    if (label === "json") {
      return "./static/json.worker.js";
    }
    if (label === "css" || label === "scss" || label === "less") {
      return "./static/css.worker.js";
    }
    if (label === "html" || label === "handlebars" || label === "razor") {
      return "./static/html.worker.js";
    }
    if (label === "typescript" || label === "javascript") {
      return "./static/ts.worker.js";
    }
    return "./static/editor.worker.js";
  },
};

@customElement("sketch-monaco-view")
export class CodeDiffEditor extends LitElement {
  // Editable state
  @property({ type: Boolean, attribute: "editable-right" })
  editableRight?: boolean;
  private container: Ref<HTMLElement> = createRef();
  editor?: any; // monaco.editor.IStandaloneDiffEditor

  // Save state properties
  @state() private saveState: "idle" | "modified" | "saving" | "saved" = "idle";
  @state() private debounceSaveTimeout: number | null = null;
  @state() private lastSavedContent: string = "";
  @property() originalCode?: string = "// Original code here";
  @property() modifiedCode?: string = "// Modified code here";
  @property() originalFilename?: string = "original.js";
  @property() modifiedFilename?: string = "modified.js";

  /* Selected text and indicators */
  @state()
  private selectedText: string | null = null;

  @state()
  private selectionRange: {
    startLineNumber: number;
    startColumn: number;
    endLineNumber: number;
    endColumn: number;
  } | null = null;

  @state()
  private showCommentIndicator: boolean = false;

  @state()
  private indicatorPosition: { top: number; left: number } = {
    top: 0,
    left: 0,
  };

  @state()
  private showCommentBox: boolean = false;

  @state()
  private commentText: string = "";

  @state()
  private activeEditor: "original" | "modified" = "modified"; // Track which editor is active

  // Custom event to request save action from external components
  private requestSave() {
    if (!this.editableRight || this.saveState !== "modified") return;

    this.saveState = "saving";

    // Get current content from modified editor
    const modifiedContent = this.modifiedModel?.getValue() || "";

    // Create and dispatch the save event
    const saveEvent = new CustomEvent("monaco-save", {
      detail: {
        path: this.modifiedFilename,
        content: modifiedContent,
      },
      bubbles: true,
      composed: true,
    });

    this.dispatchEvent(saveEvent);
  }

  // Method to be called from parent when save is complete
  public notifySaveComplete(success: boolean) {
    if (success) {
      this.saveState = "saved";
      // Update last saved content
      this.lastSavedContent = this.modifiedModel?.getValue() || "";
      // Reset to idle after a delay
      setTimeout(() => {
        this.saveState = "idle";
      }, 2000);
    } else {
      // Return to modified state on error
      this.saveState = "modified";
    }
  }

  // Rescue people with strong save-constantly habits
  private setupKeyboardShortcuts() {
    if (!this.editor) return;
    const modifiedEditor = this.editor.getModifiedEditor();
    if (!modifiedEditor) return;

    const monaco = window.monaco;
    if (!monaco) return;
    
    modifiedEditor.addCommand(
      monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS,
      () => {
        this.requestSave();
      },
    );
  }

  // Setup content change listener for debounced save
  private setupContentChangeListener() {
    if (!this.editor || !this.editableRight) return;

    const modifiedEditor = this.editor.getModifiedEditor();
    if (!modifiedEditor || !modifiedEditor.getModel()) return;

    // Store initial content
    this.lastSavedContent = modifiedEditor.getModel()!.getValue();

    // Listen for content changes
    modifiedEditor.getModel()!.onDidChangeContent(() => {
      const currentContent = modifiedEditor.getModel()!.getValue();

      // Check if content has actually changed from last saved state
      if (currentContent !== this.lastSavedContent) {
        this.saveState = "modified";

        // Debounce save request
        if (this.debounceSaveTimeout) {
          window.clearTimeout(this.debounceSaveTimeout);
        }

        this.debounceSaveTimeout = window.setTimeout(() => {
          this.requestSave();
          this.debounceSaveTimeout = null;
        }, 1000); // 1 second debounce
      }
    });
  }

  static styles = css`
    /* Save indicator styles */
    .save-indicator {
      position: absolute;
      top: 4px;
      right: 4px;
      padding: 3px 8px;
      border-radius: 3px;
      font-size: 12px;
      font-family: system-ui, sans-serif;
      color: white;
      z-index: 100;
      opacity: 0.9;
      pointer-events: none;
      transition: opacity 0.3s ease;
    }

    .save-indicator.idle {
      background-color: #6c757d;
    }

    .save-indicator.modified {
      background-color: #f0ad4e;
    }

    .save-indicator.saving {
      background-color: #5bc0de;
    }

    .save-indicator.saved {
      background-color: #5cb85c;
    }

    /* Editor host styles */
    :host {
      --editor-width: 100%;
      --editor-height: 100%;
      display: flex;
      flex: none; /* Don't grow/shrink - size is determined by content */
      min-height: 0; /* Critical for flex layout */
      position: relative; /* Establish positioning context */
      width: 100%; /* Take full width */
      /* Height will be set dynamically by setupAutoSizing */
    }
    main {
      width: 100%;
      height: 100%; /* Fill the host element completely */
      border: 1px solid #e0e0e0;
      flex: none; /* Size determined by parent */
      min-height: 200px; /* Ensure a minimum height for the editor */
      /* Remove absolute positioning - use normal block layout */
      position: relative;
      display: block;
      box-sizing: border-box; /* Include border in width calculation */
    }

    /* Comment indicator and box styles */
    .comment-indicator {
      position: fixed;
      background-color: rgba(66, 133, 244, 0.9);
      color: white;
      border-radius: 3px;
      padding: 3px 8px;
      font-size: 12px;
      cursor: pointer;
      box-shadow: 0 2px 5px rgba(0, 0, 0, 0.2);
      z-index: 10000;
      animation: fadeIn 0.2s ease-in-out;
      display: flex;
      align-items: center;
      gap: 4px;
      pointer-events: all;
    }

    .comment-indicator:hover {
      background-color: rgba(66, 133, 244, 1);
    }

    .comment-indicator span {
      line-height: 1;
    }

    .comment-box {
      position: fixed;
      background-color: white;
      border: 1px solid #ddd;
      border-radius: 4px;
      box-shadow: 0 3px 10px rgba(0, 0, 0, 0.15);
      padding: 12px;
      z-index: 10001;
      width: 350px;
      animation: fadeIn 0.2s ease-in-out;
      max-height: 80vh;
      overflow-y: auto;
    }

    .comment-box-header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      margin-bottom: 8px;
    }

    .comment-box-header h3 {
      margin: 0;
      font-size: 14px;
      font-weight: 500;
    }

    .close-button {
      background: none;
      border: none;
      cursor: pointer;
      font-size: 16px;
      color: #666;
      padding: 2px 6px;
    }

    .close-button:hover {
      color: #333;
    }

    .selected-text-preview {
      background-color: #f5f5f5;
      border: 1px solid #eee;
      border-radius: 3px;
      padding: 8px;
      margin-bottom: 10px;
      font-family: monospace;
      font-size: 12px;
      max-height: 80px;
      overflow-y: auto;
      white-space: pre-wrap;
      word-break: break-all;
    }

    .comment-textarea {
      width: 100%;
      min-height: 80px;
      padding: 8px;
      border: 1px solid #ddd;
      border-radius: 3px;
      resize: vertical;
      font-family: inherit;
      margin-bottom: 10px;
      box-sizing: border-box;
    }

    .comment-actions {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
    }

    .comment-actions button {
      padding: 6px 12px;
      border-radius: 3px;
      cursor: pointer;
      font-size: 12px;
    }

    .cancel-button {
      background-color: transparent;
      border: 1px solid #ddd;
    }

    .cancel-button:hover {
      background-color: #f5f5f5;
    }

    .submit-button {
      background-color: #4285f4;
      color: white;
      border: none;
    }

    .submit-button:hover {
      background-color: #3367d6;
    }

    @keyframes fadeIn {
      from {
        opacity: 0;
      }
      to {
        opacity: 1;
      }
    }
  `;

  render() {
    return html`
      <style>
        ${monacoStyles}
      </style>
      <main ${ref(this.container)}></main>

      <!-- Save indicator - shown when editing -->
      ${this.editableRight
        ? html`
            <div class="save-indicator ${this.saveState}">
              ${this.saveState === "idle"
                ? "Editable"
                : this.saveState === "modified"
                  ? "Modified..."
                  : this.saveState === "saving"
                    ? "Saving..."
                    : this.saveState === "saved"
                      ? "Saved"
                      : ""}
            </div>
          `
        : ""}

      <!-- Comment indicator - shown when text is selected -->
      ${this.showCommentIndicator
        ? html`
            <div
              class="comment-indicator"
              style="top: ${this.indicatorPosition.top}px; left: ${this
                .indicatorPosition.left}px;"
              @click="${this.handleIndicatorClick}"
              @mouseenter="${() => {
                this._isHovering = true;
              }}"
              @mouseleave="${() => {
                this._isHovering = false;
              }}"
            >
              <span>💬</span>
              <span>Add comment</span>
            </div>
          `
        : ""}

      <!-- Comment box - shown when indicator is clicked -->
      ${this.showCommentBox
        ? html`
            <div
              class="comment-box"
              style="${this.calculateCommentBoxPosition()}"
              @mouseenter="${() => {
                this._isHovering = true;
              }}"
              @mouseleave="${() => {
                this._isHovering = false;
              }}"
            >
              <div class="comment-box-header">
                <h3>Add comment</h3>
                <button class="close-button" @click="${this.closeCommentBox}">
                  ×
                </button>
              </div>
              <div class="selected-text-preview">${this.selectedText}</div>
              <textarea
                class="comment-textarea"
                placeholder="Type your comment here..."
                .value="${this.commentText}"
                @input="${this.handleCommentInput}"
              ></textarea>
              <div class="comment-actions">
                <button class="cancel-button" @click="${this.closeCommentBox}">
                  Cancel
                </button>
                <button class="submit-button" @click="${this.submitComment}">
                  Add
                </button>
              </div>
            </div>
          `
        : ""}
    `;
  }

  /**
   * Calculate the optimal position for the comment box to keep it in view
   */
  private calculateCommentBoxPosition(): string {
    // Get viewport dimensions
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;

    // Default position (below indicator)
    let top = this.indicatorPosition.top + 30;
    let left = this.indicatorPosition.left;

    // Estimated box dimensions
    const boxWidth = 350;
    const boxHeight = 300;

    // Check if box would go off the right edge
    if (left + boxWidth > viewportWidth) {
      left = viewportWidth - boxWidth - 20; // Keep 20px margin
    }

    // Check if box would go off the bottom
    const bottomSpace = viewportHeight - top;
    if (bottomSpace < boxHeight) {
      // Not enough space below, try to position above if possible
      if (this.indicatorPosition.top > boxHeight) {
        // Position above the indicator
        top = this.indicatorPosition.top - boxHeight - 10;
      } else {
        // Not enough space above either, position at top of viewport with margin
        top = 10;
      }
    }

    // Ensure box is never positioned off-screen
    top = Math.max(10, top);
    left = Math.max(10, left);

    return `top: ${top}px; left: ${left}px;`;
  }

  setOriginalCode(code: string, filename?: string) {
    this.originalCode = code;
    if (filename) {
      this.originalFilename = filename;
    }

    // Update the model if the editor is initialized
    if (this.editor) {
      const model = this.editor.getOriginalEditor().getModel();
      if (model) {
        model.setValue(code);
        if (filename) {
          monaco.editor.setModelLanguage(
            model,
            this.getLanguageForFile(filename),
          );
        }
      }
    }
  }

  setModifiedCode(code: string, filename?: string) {
    this.modifiedCode = code;
    if (filename) {
      this.modifiedFilename = filename;
    }

    // Update the model if the editor is initialized
    if (this.editor) {
      const model = this.editor.getModifiedEditor().getModel();
      if (model) {
        model.setValue(code);
        if (filename) {
          monaco.editor.setModelLanguage(
            model,
            this.getLanguageForFile(filename),
          );
        }
      }
    }
  }

  private _extensionToLanguageMap: Map<string, string> | null = null;

  private getLanguageForFile(filename: string): string {
    // Get the file extension (including the dot for exact matching)
    const extension = "." + (filename.split(".").pop()?.toLowerCase() || "");

    // Build the extension-to-language map on first use
    if (!this._extensionToLanguageMap) {
      this._extensionToLanguageMap = new Map();
      const languages = monaco.languages.getLanguages();

      for (const language of languages) {
        if (language.extensions) {
          for (const ext of language.extensions) {
            // Monaco extensions already include the dot, so use them directly
            this._extensionToLanguageMap.set(ext.toLowerCase(), language.id);
          }
        }
      }
    }

    return this._extensionToLanguageMap.get(extension) || "plaintext";
  }

  /**
   * Update editor options
   */
  setOptions(value: any) { // monaco.editor.IDiffEditorConstructionOptions
    if (this.editor) {
      this.editor.updateOptions(value);
      // Re-fit content after options change
      if (this.fitEditorToContent) {
        setTimeout(() => this.fitEditorToContent!(), 50);
      }
    }
  }

  /**
   * Toggle hideUnchangedRegions feature
   */
  toggleHideUnchangedRegions(enabled: boolean) {
    if (this.editor) {
      this.editor.updateOptions({
        hideUnchangedRegions: {
          enabled: enabled,
          contextLineCount: 3,
          minimumLineCount: 3,
          revealLineCount: 10,
        },
      });
      // Re-fit content after toggling
      if (this.fitEditorToContent) {
        setTimeout(() => this.fitEditorToContent!(), 100);
      }
    }
  }

  // Models for the editor
  private originalModel?: any; // monaco.editor.ITextModel
  private modifiedModel?: any; // monaco.editor.ITextModel

  private async initializeEditor() {
    try {
      // Load Monaco dynamically
      const monaco = await loadMonaco();
      
      // Disable semantic validation globally for TypeScript/JavaScript if available
      if (monaco.languages && monaco.languages.typescript) {
        monaco.languages.typescript.typescriptDefaults.setDiagnosticsOptions({
          noSemanticValidation: true,
        });
        monaco.languages.typescript.javascriptDefaults.setDiagnosticsOptions({
          noSemanticValidation: true,
        });
      }

      // First time initialization
      if (!this.editor) {
        // Create the diff editor with auto-sizing configuration
        this.editor = monaco.editor.createDiffEditor(this.container.value!, {
          automaticLayout: false, // We'll resize manually
          readOnly: true,
          theme: "vs", // Always use light mode
          renderSideBySide: true,
          ignoreTrimWhitespace: false,
          renderOverviewRuler: false, // Disable the overview ruler
          scrollbar: {
            vertical: "hidden",
            horizontal: "hidden",
            handleMouseWheel: false, // Let outer scroller eat the wheel
            useShadows: false, // Disable scrollbar shadows
            verticalHasArrows: false, // Remove scrollbar arrows
            horizontalHasArrows: false, // Remove scrollbar arrows
            verticalScrollbarSize: 0, // Set scrollbar track width to 0
            horizontalScrollbarSize: 0, // Set scrollbar track height to 0
          },
          minimap: { enabled: false },
          overviewRulerLanes: 0,
          scrollBeyondLastLine: false,
          // Focus on the differences by hiding unchanged regions
          hideUnchangedRegions: {
            enabled: true, // Enable the feature
            contextLineCount: 3, // Show 3 lines of context around each difference
            minimumLineCount: 3, // Hide regions only when they're at least 3 lines
            revealLineCount: 10, // Show 10 lines when expanding a hidden region
          },
        });

        // Set up selection change event listeners for both editors
        this.setupSelectionChangeListeners();

        this.setupKeyboardShortcuts();

        // If this is an editable view, set the correct read-only state for each editor
        if (this.editableRight) {
          // Make sure the original editor is always read-only
          this.editor.getOriginalEditor().updateOptions({ readOnly: true });
          // Make sure the modified editor is editable
          this.editor.getModifiedEditor().updateOptions({ readOnly: false });
        }

        // Set up auto-sizing
        this.setupAutoSizing();

        // Add Monaco editor to debug global
        this.addToDebugGlobal();
      }

      // Create or update models
      this.updateModels();
      // Set up content change listener
      this.setupContentChangeListener();

      // Fix cursor positioning issues by ensuring fonts are loaded
      document.fonts.ready.then(() => {
        if (this.editor) {
          monaco.editor.remeasureFonts();
          this.fitEditorToContent();
        }
      });

      // Force layout recalculation after a short delay
      setTimeout(() => {
        if (this.editor) {
          this.fitEditorToContent();
        }
      }, 100);
    } catch (error) {
      console.error("Error initializing Monaco editor:", error);
    }
  }

  /**
   * Sets up event listeners for text selection in both editors.
   * This enables showing the comment UI when users select text and
   * manages the visibility of UI components based on user interactions.
   */
  private setupSelectionChangeListeners() {
    try {
      if (!this.editor) {
        return;
      }

      // Get both original and modified editors
      const originalEditor = this.editor.getOriginalEditor();
      const modifiedEditor = this.editor.getModifiedEditor();

      if (!originalEditor || !modifiedEditor) {
        return;
      }

      // Add selection change listener to original editor
      originalEditor.onDidChangeCursorSelection((e) => {
        this.handleSelectionChange(e, originalEditor, "original");
      });

      // Add selection change listener to modified editor
      modifiedEditor.onDidChangeCursorSelection((e) => {
        this.handleSelectionChange(e, modifiedEditor, "modified");
      });

      // Create a debounced function for mouse move handling
      let mouseMoveTimeout: number | null = null;
      const handleMouseMove = () => {
        // Clear any existing timeout
        if (mouseMoveTimeout) {
          window.clearTimeout(mouseMoveTimeout);
        }

        // If there's text selected and we're not showing the comment box, keep indicator visible
        if (this.selectedText && !this.showCommentBox) {
          this.showCommentIndicator = true;
          this.requestUpdate();
        }

        // Set a new timeout to hide the indicator after a delay
        mouseMoveTimeout = window.setTimeout(() => {
          // Only hide if we're not showing the comment box and not actively hovering
          if (!this.showCommentBox && !this._isHovering) {
            this.showCommentIndicator = false;
            this.requestUpdate();
          }
        }, 2000); // Hide after 2 seconds of inactivity
      };

      // Add mouse move listeners with debouncing
      originalEditor.onMouseMove(() => handleMouseMove());
      modifiedEditor.onMouseMove(() => handleMouseMove());

      // Track hover state over the indicator and comment box
      this._isHovering = false;

      // Use the global document click handler to detect clicks outside
      this._documentClickHandler = (e: MouseEvent) => {
        try {
          const target = e.target as HTMLElement;
          const isIndicator =
            target.matches(".comment-indicator") ||
            !!target.closest(".comment-indicator");
          const isCommentBox =
            target.matches(".comment-box") || !!target.closest(".comment-box");

          // If click is outside our UI elements
          if (!isIndicator && !isCommentBox) {
            // If we're not showing the comment box, hide the indicator
            if (!this.showCommentBox) {
              this.showCommentIndicator = false;
              this.requestUpdate();
            }
          }
        } catch (error) {
          console.error("Error in document click handler:", error);
        }
      };

      // Add the document click listener
      document.addEventListener("click", this._documentClickHandler);
    } catch (error) {
      console.error("Error setting up selection listeners:", error);
    }
  }

  // Track mouse hover state
  private _isHovering = false;

  // Store document click handler for cleanup
  private _documentClickHandler: ((e: MouseEvent) => void) | null = null;

  /**
   * Handle selection change events from either editor
   */
  private handleSelectionChange(
    e: any, // monaco.editor.ICursorSelectionChangedEvent
    editor: any, // monaco.editor.IStandaloneCodeEditor
    editorType: "original" | "modified",
  ) {
    try {
      // If we're not making a selection (just moving cursor), do nothing
      if (e.selection.isEmpty()) {
        // Don't hide indicator or box if already shown
        if (!this.showCommentBox) {
          this.selectedText = null;
          this.selectionRange = null;
          this.showCommentIndicator = false;
        }
        return;
      }

      // Get selected text
      const model = editor.getModel();
      if (!model) {
        return;
      }

      // Make sure selection is within valid range
      const lineCount = model.getLineCount();
      if (
        e.selection.startLineNumber > lineCount ||
        e.selection.endLineNumber > lineCount
      ) {
        return;
      }

      // Store which editor is active
      this.activeEditor = editorType;

      // Store selection range
      this.selectionRange = {
        startLineNumber: e.selection.startLineNumber,
        startColumn: e.selection.startColumn,
        endLineNumber: e.selection.endLineNumber,
        endColumn: e.selection.endColumn,
      };

      try {
        // Expand selection to full lines for better context
        const expandedSelection = {
          startLineNumber: e.selection.startLineNumber,
          startColumn: 1, // Start at beginning of line
          endLineNumber: e.selection.endLineNumber,
          endColumn: model.getLineMaxColumn(e.selection.endLineNumber), // End at end of line
        };

        // Get the selected text using the expanded selection
        this.selectedText = model.getValueInRange(expandedSelection);

        // Update the selection range to reflect the full lines
        this.selectionRange = {
          startLineNumber: expandedSelection.startLineNumber,
          startColumn: expandedSelection.startColumn,
          endLineNumber: expandedSelection.endLineNumber,
          endColumn: expandedSelection.endColumn,
        };
      } catch (error) {
        console.error("Error getting selected text:", error);
        return;
      }

      // If there's selected text, show the indicator
      if (this.selectedText && this.selectedText.trim() !== "") {
        // Calculate indicator position safely
        try {
          // Use the editor's DOM node as positioning context
          const editorDomNode = editor.getDomNode();
          if (!editorDomNode) {
            return;
          }

          // Get position from editor
          const position = {
            lineNumber: e.selection.endLineNumber,
            column: e.selection.endColumn,
          };

          // Use editor's built-in method for coordinate conversion
          const selectionCoords = editor.getScrolledVisiblePosition(position);

          if (selectionCoords) {
            // Get accurate DOM position for the selection end
            const editorRect = editorDomNode.getBoundingClientRect();

            // Calculate the actual screen position
            const screenLeft = editorRect.left + selectionCoords.left;
            const screenTop = editorRect.top + selectionCoords.top;

            // Store absolute screen coordinates
            this.indicatorPosition = {
              top: screenTop,
              left: screenLeft + 10, // Slight offset
            };

            // Check window boundaries to ensure the indicator stays visible
            const viewportWidth = window.innerWidth;
            const viewportHeight = window.innerHeight;

            // Keep indicator within viewport bounds
            if (this.indicatorPosition.left + 150 > viewportWidth) {
              this.indicatorPosition.left = viewportWidth - 160;
            }

            if (this.indicatorPosition.top + 40 > viewportHeight) {
              this.indicatorPosition.top = viewportHeight - 50;
            }

            // Show the indicator
            this.showCommentIndicator = true;

            // Request an update to ensure UI reflects changes
            this.requestUpdate();
          }
        } catch (error) {
          console.error("Error positioning comment indicator:", error);
        }
      }
    } catch (error) {
      console.error("Error handling selection change:", error);
    }
  }

  /**
   * Handle click on the comment indicator
   */
  private handleIndicatorClick(e: Event) {
    try {
      e.stopPropagation();
      e.preventDefault();

      this.showCommentBox = true;
      this.commentText = ""; // Reset comment text

      // Don't hide the indicator while comment box is shown
      this.showCommentIndicator = true;

      // Ensure UI updates
      this.requestUpdate();
    } catch (error) {
      console.error("Error handling indicator click:", error);
    }
  }

  /**
   * Handle changes to the comment text
   */
  private handleCommentInput(e: Event) {
    const target = e.target as HTMLTextAreaElement;
    this.commentText = target.value;
  }

  /**
   * Close the comment box
   */
  private closeCommentBox() {
    this.showCommentBox = false;
    // Also hide the indicator
    this.showCommentIndicator = false;
  }

  /**
   * Submit the comment
   */
  private submitComment() {
    try {
      if (!this.selectedText || !this.commentText) {
        return;
      }

      // Get the correct filename based on active editor
      const fileContext =
        this.activeEditor === "original"
          ? this.originalFilename || "Original file"
          : this.modifiedFilename || "Modified file";

      // Include editor info to make it clear which version was commented on
      const editorLabel =
        this.activeEditor === "original" ? "[Original]" : "[Modified]";

      // Add line number information if available
      let lineInfo = "";
      if (this.selectionRange) {
        const startLine = this.selectionRange.startLineNumber;
        const endLine = this.selectionRange.endLineNumber;
        if (startLine === endLine) {
          lineInfo = ` (line ${startLine})`;
        } else {
          lineInfo = ` (lines ${startLine}-${endLine})`;
        }
      }

      // Format the comment in a readable way
      const formattedComment = `\`\`\`\n${fileContext} ${editorLabel}${lineInfo}:\n${this.selectedText}\n\`\`\`\n\n${this.commentText}`;

      // Close UI before dispatching to prevent interaction conflicts
      this.closeCommentBox();

      // Use setTimeout to ensure the UI has updated before sending the event
      setTimeout(() => {
        try {
          // Dispatch a custom event with the comment details
          const event = new CustomEvent("monaco-comment", {
            detail: {
              fileContext,
              selectedText: this.selectedText,
              commentText: this.commentText,
              formattedComment,
              selectionRange: this.selectionRange,
              activeEditor: this.activeEditor,
            },
            bubbles: true,
            composed: true,
          });

          this.dispatchEvent(event);
        } catch (error) {
          console.error("Error dispatching comment event:", error);
        }
      }, 0);
    } catch (error) {
      console.error("Error submitting comment:", error);
      this.closeCommentBox();
    }
  }

  private updateModels() {
    try {
      // Get language based on filename
      const originalLang = this.getLanguageForFile(this.originalFilename || "");
      const modifiedLang = this.getLanguageForFile(this.modifiedFilename || "");

      // Always create new models with unique URIs based on timestamp to avoid conflicts
      const timestamp = new Date().getTime();
      // TODO: Could put filename in these URIs; unclear how they're used right now.
      const originalUri = monaco.Uri.parse(
        `file:///original-${timestamp}.${originalLang}`,
      );
      const modifiedUri = monaco.Uri.parse(
        `file:///modified-${timestamp}.${modifiedLang}`,
      );

      // Store references to old models
      const oldOriginalModel = this.originalModel;
      const oldModifiedModel = this.modifiedModel;

      // Nullify instance variables to prevent accidental use
      this.originalModel = undefined;
      this.modifiedModel = undefined;

      // Clear the editor model first to release Monaco's internal references
      if (this.editor) {
        this.editor.setModel(null);
      }

      // Now it's safe to dispose the old models
      if (oldOriginalModel) {
        oldOriginalModel.dispose();
      }

      if (oldModifiedModel) {
        oldModifiedModel.dispose();
      }

      // Create new models
      this.originalModel = monaco.editor.createModel(
        this.originalCode || "",
        originalLang,
        originalUri,
      );

      this.modifiedModel = monaco.editor.createModel(
        this.modifiedCode || "",
        modifiedLang,
        modifiedUri,
      );

      // Set the new models on the editor
      if (this.editor) {
        this.editor.setModel({
          original: this.originalModel,
          modified: this.modifiedModel,
        });

        // Set initial hideUnchangedRegions state (default to enabled/collapsed)
        this.editor.updateOptions({
          hideUnchangedRegions: {
            enabled: true, // Default to collapsed state
            contextLineCount: 3,
            minimumLineCount: 3,
            revealLineCount: 10,
          },
        });

        // Fit content after setting new models
        if (this.fitEditorToContent) {
          setTimeout(() => this.fitEditorToContent!(), 50);
        }
      }
      this.setupContentChangeListener();
    } catch (error) {
      console.error("Error updating Monaco models:", error);
    }
  }

  async updated(changedProperties: Map<string, any>) {
    // If any relevant properties changed, just update the models
    if (
      changedProperties.has("originalCode") ||
      changedProperties.has("modifiedCode") ||
      changedProperties.has("originalFilename") ||
      changedProperties.has("modifiedFilename") ||
      changedProperties.has("editableRight")
    ) {
      if (this.editor) {
        this.updateModels();

        // Force auto-sizing after model updates
        // Use a slightly longer delay to ensure layout is stable
        setTimeout(() => {
          if (this.fitEditorToContent) {
            this.fitEditorToContent();
          }
        }, 100);
      } else {
        // If the editor isn't initialized yet but we received content,
        // initialize it now
        await this.initializeEditor();
      }
    }
  }

  // Set up auto-sizing for multi-file diff view
  private setupAutoSizing() {
    if (!this.editor) return;

    const fitContent = () => {
      try {
        const originalEditor = this.editor!.getOriginalEditor();
        const modifiedEditor = this.editor!.getModifiedEditor();

        const originalHeight = originalEditor.getContentHeight();
        const modifiedHeight = modifiedEditor.getContentHeight();

        // Use the maximum height of both editors, plus some padding
        const maxHeight = Math.max(originalHeight, modifiedHeight) + 18; // 1 blank line bottom padding

        // Set both container and host height to enable proper scrolling
        if (this.container.value) {
          // Set explicit heights on both container and host
          this.container.value.style.height = `${maxHeight}px`;
          this.style.height = `${maxHeight}px`; // Update host element height

          // Emit the height change event BEFORE calling layout
          // This ensures parent containers resize first
          this.dispatchEvent(
            new CustomEvent("monaco-height-changed", {
              detail: { height: maxHeight },
              bubbles: true,
              composed: true,
            }),
          );

          // Layout after both this component and parents have updated
          setTimeout(() => {
            if (this.editor && this.container.value) {
              // Use explicit dimensions to ensure Monaco uses full available space
              // Use clientWidth instead of offsetWidth to avoid border overflow
              const width = this.container.value.clientWidth;
              this.editor.layout({
                width: width,
                height: maxHeight,
              });
            }
          }, 10);
        }
      } catch (error) {
        console.error("Error in fitContent:", error);
      }
    };

    // Store the fit function for external access
    this.fitEditorToContent = fitContent;

    // Set up listeners for content size changes
    this.editor.getOriginalEditor().onDidContentSizeChange(fitContent);
    this.editor.getModifiedEditor().onDidContentSizeChange(fitContent);

    // Initial fit
    fitContent();
  }

  private fitEditorToContent: (() => void) | null = null;

  /**
   * Set up window resize handler to ensure Monaco editor adapts to browser window changes
   */
  private setupWindowResizeHandler() {
    // Create a debounced resize handler to avoid too many layout calls
    let resizeTimeout: number | null = null;

    this._windowResizeHandler = () => {
      // Clear any existing timeout
      if (resizeTimeout) {
        window.clearTimeout(resizeTimeout);
      }

      // Debounce the resize to avoid excessive layout calls
      resizeTimeout = window.setTimeout(() => {
        if (this.editor && this.container.value) {
          // Trigger layout recalculation
          if (this.fitEditorToContent) {
            this.fitEditorToContent();
          } else {
            // Fallback: just trigger a layout with current container dimensions
            // Use clientWidth/Height instead of offsetWidth/Height to avoid border overflow
            const width = this.container.value.clientWidth;
            const height = this.container.value.clientHeight;
            this.editor.layout({ width, height });
          }
        }
      }, 100); // 100ms debounce
    };

    // Add the event listener
    window.addEventListener("resize", this._windowResizeHandler);
  }

  // Add resize observer to ensure editor resizes when container changes
  async firstUpdated() {
    // Initialize the editor
    await this.initializeEditor();

    // Set up window resize handler to ensure Monaco editor adapts to browser window changes
    this.setupWindowResizeHandler();

    // For multi-file diff, we don't use ResizeObserver since we control the size
    // Instead, we rely on auto-sizing based on content

    // If editable, set up edit mode and content change listener
    if (this.editableRight && this.editor) {
      // Ensure the original editor is read-only
      this.editor.getOriginalEditor().updateOptions({ readOnly: true });
      // Ensure the modified editor is editable
      this.editor.getModifiedEditor().updateOptions({ readOnly: false });
    }
  }

  private _resizeObserver: ResizeObserver | null = null;
  private _windowResizeHandler: (() => void) | null = null;

  /**
   * Add this Monaco editor instance to the global debug object
   * This allows inspection and debugging via browser console
   */
  private addToDebugGlobal() {
    try {
      // Initialize the debug global if it doesn't exist
      if (!(window as any).sketchDebug) {
        (window as any).sketchDebug = {
          monaco: monaco,
          editors: [],
          remeasureFonts: () => {
            monaco.editor.remeasureFonts();
            (window as any).sketchDebug.editors.forEach(
              (editor: any, index: number) => {
                if (editor && editor.layout) {
                  editor.layout();
                }
              },
            );
          },
          layoutAll: () => {
            (window as any).sketchDebug.editors.forEach(
              (editor: any, index: number) => {
                if (editor && editor.layout) {
                  editor.layout();
                }
              },
            );
          },
          getActiveEditors: () => {
            return (window as any).sketchDebug.editors.filter(
              (editor: any) => editor !== null,
            );
          },
        };
      }

      // Add this editor to the debug collection
      if (this.editor) {
        (window as any).sketchDebug.editors.push(this.editor);
      }
    } catch (error) {
      console.error("Error adding Monaco editor to debug global:", error);
    }
  }

  disconnectedCallback() {
    super.disconnectedCallback();

    try {
      // Remove editor from debug global before disposal
      if (this.editor && (window as any).sketchDebug?.editors) {
        const index = (window as any).sketchDebug.editors.indexOf(this.editor);
        if (index > -1) {
          (window as any).sketchDebug.editors[index] = null;
        }
      }

      // Clean up resources when element is removed
      if (this.editor) {
        this.editor.dispose();
        this.editor = undefined;
      }

      // Dispose models to prevent memory leaks
      if (this.originalModel) {
        this.originalModel.dispose();
        this.originalModel = undefined;
      }

      if (this.modifiedModel) {
        this.modifiedModel.dispose();
        this.modifiedModel = undefined;
      }

      // Clean up resize observer (if any)
      if (this._resizeObserver) {
        this._resizeObserver.disconnect();
        this._resizeObserver = null;
      }

      // Clear the fit function reference
      this.fitEditorToContent = null;

      // Remove document click handler if set
      if (this._documentClickHandler) {
        document.removeEventListener("click", this._documentClickHandler);
        this._documentClickHandler = null;
      }

      // Remove window resize handler if set
      if (this._windowResizeHandler) {
        window.removeEventListener("resize", this._windowResizeHandler);
        this._windowResizeHandler = null;
      }
    } catch (error) {
      console.error("Error in disconnectedCallback:", error);
    }
  }

  // disconnectedCallback implementation is defined below
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-monaco-view": CodeDiffEditor;
  }
}
