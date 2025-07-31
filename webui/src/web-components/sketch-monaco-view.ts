/* eslint-disable no-async-promise-executor, @typescript-eslint/ban-ts-comment */
import { html } from "lit";
import { customElement, property, state } from "lit/decorators.js";
import { createRef, Ref, ref } from "lit/directives/ref.js";
import { SketchTailwindElement } from "./sketch-tailwind-element.js";

// See https://rodydavis.com/posts/lit-monaco-editor for some ideas.

import type * as monaco from "monaco-editor";

// Monaco is loaded dynamically - see loadMonaco() function
declare global {
  interface Window {
    monaco?: typeof monaco;
  }
}

// Monaco hash will be injected at build time
declare const __MONACO_HASH__: string;

// Load Monaco editor dynamically
let monacoLoadPromise: Promise<any> | null = null;

function loadMonaco(): Promise<typeof monaco> {
  if (monacoLoadPromise) {
    return monacoLoadPromise;
  }

  if (window.monaco) {
    return Promise.resolve(window.monaco);
  }

  monacoLoadPromise = new Promise(async (resolve, reject) => {
    try {
      // Check if we're in development mode
      const isDev = __MONACO_HASH__ === "dev";

      if (isDev) {
        // In development mode, import Monaco directly
        const monaco = await import("monaco-editor");
        window.monaco = monaco;
        resolve(monaco);
      } else {
        // In production mode, load from external bundle
        const monacoHash = __MONACO_HASH__;

        // Try to load the external Monaco bundle
        const script = document.createElement("script");
        script.onload = () => {
          // The Monaco bundle should set window.monaco
          if (window.monaco) {
            resolve(window.monaco);
          } else {
            reject(new Error("Monaco not loaded from external bundle"));
          }
        };
        script.onerror = (error) => {
          console.warn("Failed to load external Monaco bundle:", error);
          reject(new Error("Monaco external bundle failed to load"));
        };

        // Don't set type="module" since we're using IIFE format
        script.src = `./static/monaco-standalone-${monacoHash}.js`;
        document.head.appendChild(script);
      }
    } catch (error) {
      reject(error);
    }
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

  /* Ensure light theme colors */
  .monaco-editor, .monaco-editor-background, .monaco-editor .inputarea.ime-input {
    background-color: var(--monaco-editor-bg, #ffffff) !important;
  }

  .monaco-editor .margin {
    background-color: var(--monaco-editor-margin, #f5f5f5) !important;
  }

  /* Glyph decoration styles - only show on hover */
  .comment-glyph-decoration {
    width: 16px !important;
    height: 18px !important;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.2s ease;
  }

  .comment-glyph-decoration:before {
    content: '💬';
    font-size: 12px;
    line-height: 18px;
    width: 16px;
    height: 18px;
    display: block;
    text-align: center;
  }

  .comment-glyph-decoration.hover-visible {
    opacity: 1;
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
export class CodeDiffEditor extends SketchTailwindElement {
  // Editable state
  @property({ type: Boolean, attribute: "editable-right" })
  editableRight?: boolean;

  // Inline diff mode (for mobile)
  @property({ type: Boolean, attribute: "inline" })
  inline?: boolean;
  private container: Ref<HTMLElement> = createRef();
  editor?: monaco.editor.IStandaloneDiffEditor;

  // Save state properties
  @state() private saveState: "idle" | "modified" | "saving" | "saved" = "idle";
  @state() private debounceSaveTimeout: number | null = null;
  @state() private lastSavedContent: string = "";
  @property() originalCode?: string = "// Original code here";
  @property() modifiedCode?: string = "// Modified code here";
  @property() originalFilename?: string = "original.js";
  @property() modifiedFilename?: string = "modified.js";

  // Comment system state
  @state() private showCommentBox: boolean = false;
  @state() private commentText: string = "";
  @state() private selectedLines: {
    startLine: number;
    endLine: number;
    editorType: "original" | "modified";
    text: string;
  } | null = null;
  @state() private commentBoxPosition: { top: number; left: number } = {
    top: 0,
    left: 0,
  };
  @state() private isDragging: boolean = false;
  @state() private dragStartLine: number | null = null;
  @state() private dragStartEditor: "original" | "modified" | null = null;

  // Track visible glyphs to ensure proper cleanup
  private visibleGlyphs: Set<string> = new Set();

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

      // Update glyph decorations when content changes
      setTimeout(() => {
        if (this.editor && this.modifiedModel) {
          this.addGlyphDecorationsToEditor(
            this.editor.getModifiedEditor(),
            this.modifiedModel,
            "modified",
          );
        }
      }, 50);
    });
  }

  render() {
    // Set host element styles for layout (equivalent to :host styles)
    this.style.cssText = `
      --editor-width: 100%;
      --editor-height: 100%;
      display: flex;
      flex: none;
      min-height: 0;
      position: relative;
      width: 100%;
    `;

    return html`
      <style>
        ${monacoStyles}

        /* Custom animation for comment box fade-in */
        @keyframes fadeIn {
          from {
            opacity: 0;
          }
          to {
            opacity: 1;
          }
        }
        .animate-fade-in {
          animation: fadeIn 0.2s ease-in-out;
        }
      </style>

      <main
        ${ref(this.container)}
        class="w-full h-full border border-gray-300 dark:border-neutral-600 flex-none min-h-[200px] relative block box-border"
      ></main>

      <!-- Save indicator - shown when editing -->
      ${this.editableRight
        ? html`
            <div
              class="absolute top-1 right-1 px-2 py-0.5 rounded text-xs font-sans text-white z-[100] opacity-90 pointer-events-none transition-opacity duration-300 ${this
                .saveState === "idle"
                ? "bg-gray-500"
                : this.saveState === "modified"
                  ? "bg-yellow-500"
                  : this.saveState === "saving"
                    ? "bg-blue-400"
                    : this.saveState === "saved"
                      ? "bg-green-500"
                      : "bg-gray-500"}"
            >
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

      <!-- Comment box - shown when glyph is clicked -->
      ${this.showCommentBox
        ? html`
            <div
              class="fixed bg-white dark:bg-neutral-800 border border-gray-300 dark:border-neutral-600 rounded shadow-lg p-3 z-[10001] w-[600px] animate-fade-in max-h-[80vh] overflow-y-auto"
              style="top: ${this.commentBoxPosition.top}px; left: ${this
                .commentBoxPosition.left}px;"
            >
              <div class="flex justify-between items-center mb-2">
                <h3 class="m-0 text-sm font-medium text-gray-900 dark:text-neutral-100">Add comment</h3>
                <button
                  class="bg-none border-none cursor-pointer text-base text-gray-600 dark:text-neutral-400 px-1.5 py-0.5 hover:text-gray-800 dark:hover:text-neutral-200"
                  @click="${this.closeCommentBox}"
                >
                  ×
                </button>
              </div>
              ${this.selectedLines
                ? html`
                    <div
                      class="bg-gray-100 dark:bg-neutral-700 border border-gray-200 dark:border-neutral-600 rounded p-2 mb-2.5 font-mono text-xs text-gray-900 dark:text-neutral-100 overflow-y-auto whitespace-pre-wrap break-all leading-relaxed ${this.getPreviewCssClass() ===
                      "small-selection"
                        ? ""
                        : "max-h-[280px]"}"
                    >
                      ${this.selectedLines.text}
                    </div>
                  `
                : ""}
              <textarea
                class="w-full min-h-[80px] p-2 border border-gray-300 dark:border-neutral-600 rounded resize-y font-inherit mb-2.5 box-border bg-white dark:bg-neutral-800 text-gray-900 dark:text-neutral-100"
                placeholder="Type your comment here..."
                .value="${this.commentText}"
                @input="${this.handleCommentInput}"
                @keydown="${this.handleCommentKeydown}"
              ></textarea>
              <div class="flex justify-end gap-2">
                <button
                  class="px-3 py-1.5 rounded cursor-pointer text-xs bg-transparent border border-gray-300 dark:border-neutral-600 text-gray-700 dark:text-neutral-300 hover:bg-gray-100 dark:hover:bg-neutral-700"
                  @click="${this.closeCommentBox}"
                >
                  Cancel
                </button>
                <button
                  class="px-3 py-1.5 rounded cursor-pointer text-xs bg-blue-600 text-white border-none hover:bg-blue-700"
                  @click="${this.submitComment}"
                >
                  Add
                </button>
              </div>
            </div>
          `
        : ""}
    `;
  }

  /**
   * Handle changes to the comment text
   */
  private handleCommentInput(e: Event) {
    const target = e.target as HTMLTextAreaElement;
    this.commentText = target.value;
  }

  /**
   * Handle keyboard shortcuts in the comment textarea
   */
  private handleCommentKeydown(e: KeyboardEvent) {
    // Check for Command+Enter (Mac) or Ctrl+Enter (other platforms)
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      this.submitComment();
    }
  }

  /**
   * Get CSS class for selected text preview based on number of lines
   */
  private getPreviewCssClass(): string {
    if (!this.selectedLines) {
      return "large-selection";
    }

    // Count the number of lines in the selected text
    const lineCount = this.selectedLines.text.split("\n").length;

    // If 10 lines or fewer, show all content; otherwise, limit height
    return lineCount <= 10 ? "small-selection" : "large-selection";
  }

  /**
   * Close the comment box
   */
  private closeCommentBox() {
    this.showCommentBox = false;
    this.commentText = "";
    this.selectedLines = null;
  }

  /**
   * Submit the comment
   */
  private submitComment() {
    try {
      if (!this.selectedLines || !this.commentText.trim()) {
        return;
      }

      // Store references before closing the comment box
      const selectedLines = this.selectedLines;
      const commentText = this.commentText;

      // Get the correct filename based on active editor
      const fileContext =
        selectedLines.editorType === "original"
          ? this.originalFilename || "Original file"
          : this.modifiedFilename || "Modified file";

      // Include editor info to make it clear which version was commented on
      const editorLabel =
        selectedLines.editorType === "original" ? "[Original]" : "[Modified]";

      // Add line number information
      let lineInfo = "";
      if (selectedLines.startLine === selectedLines.endLine) {
        lineInfo = ` (line ${selectedLines.startLine})`;
      } else {
        lineInfo = ` (lines ${selectedLines.startLine}-${selectedLines.endLine})`;
      }

      // Format the comment in a readable way
      const formattedComment = `\`\`\`\n${fileContext} ${editorLabel}${lineInfo}:\n${selectedLines.text}\n\`\`\`\n\n${commentText}`;

      // Close UI before dispatching to prevent interaction conflicts
      this.closeCommentBox();

      // Use setTimeout to ensure the UI has updated before sending the event
      setTimeout(() => {
        try {
          // Dispatch a custom event with the comment details
          const event = new CustomEvent("monaco-comment", {
            detail: {
              fileContext,
              selectedText: selectedLines.text,
              commentText: commentText,
              formattedComment,
              selectionRange: {
                startLineNumber: selectedLines.startLine,
                startColumn: 1,
                endLineNumber: selectedLines.endLine,
                endColumn: 1,
              },
              activeEditor: selectedLines.editorType,
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

  /**
   * Calculate the optimal position for the comment box to keep it in view
   */
  private calculateCommentBoxPosition(
    lineNumber: number,
    editorType: "original" | "modified",
  ): { top: number; left: number } {
    try {
      if (!this.editor) {
        return { top: 100, left: 100 };
      }

      const targetEditor =
        editorType === "original"
          ? this.editor.getOriginalEditor()
          : this.editor.getModifiedEditor();
      if (!targetEditor) {
        return { top: 100, left: 100 };
      }

      // Get position from editor
      const position = {
        lineNumber: lineNumber,
        column: 1,
      };

      // Use editor's built-in method for coordinate conversion
      const coords = targetEditor.getScrolledVisiblePosition(position);

      if (coords) {
        // Get accurate DOM position
        const editorDomNode = targetEditor.getDomNode();
        if (editorDomNode) {
          const editorRect = editorDomNode.getBoundingClientRect();

          // Calculate the actual screen position
          let screenLeft = editorRect.left + coords.left + 20; // Offset to the right
          let screenTop = editorRect.top + coords.top;

          // Get viewport dimensions
          const viewportWidth = window.innerWidth;
          const viewportHeight = window.innerHeight;

          // Estimated box dimensions (updated for wider box)
          const boxWidth = 600;
          const boxHeight = 400;

          // Check if box would go off the right edge
          if (screenLeft + boxWidth > viewportWidth) {
            screenLeft = viewportWidth - boxWidth - 20; // Keep 20px margin
          }

          // Check if box would go off the bottom
          if (screenTop + boxHeight > viewportHeight) {
            screenTop = Math.max(10, viewportHeight - boxHeight - 10);
          }

          // Ensure box is never positioned off-screen
          screenTop = Math.max(10, screenTop);
          screenLeft = Math.max(10, screenLeft);

          return { top: screenTop, left: screenLeft };
        }
      }
    } catch (error) {
      console.error("Error calculating comment box position:", error);
    }

    return { top: 100, left: 100 };
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
          window.monaco!.editor.setModelLanguage(
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
          window.monaco!.editor.setModelLanguage(
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
      const languages = window.monaco!.languages.getLanguages();

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
   * Setup glyph decorations for both editors
   */
  private setupGlyphDecorations() {
    if (!this.editor || !window.monaco) {
      return;
    }

    const originalEditor = this.editor.getOriginalEditor();
    const modifiedEditor = this.editor.getModifiedEditor();

    if (originalEditor && this.originalModel) {
      this.addGlyphDecorationsToEditor(
        originalEditor,
        this.originalModel,
        "original",
      );
      this.setupHoverBehavior(originalEditor);
    }

    if (modifiedEditor && this.modifiedModel) {
      this.addGlyphDecorationsToEditor(
        modifiedEditor,
        this.modifiedModel,
        "modified",
      );
      this.setupHoverBehavior(modifiedEditor);
    }
  }

  /**
   * Add glyph decorations to a specific editor
   */
  private addGlyphDecorationsToEditor(
    editor: monaco.editor.IStandaloneCodeEditor,
    model: monaco.editor.ITextModel,
    editorType: "original" | "modified",
  ) {
    if (!window.monaco) {
      return;
    }

    // Clear existing decorations
    if (editorType === "original" && this.originalDecorations) {
      this.originalDecorations.clear();
    } else if (editorType === "modified" && this.modifiedDecorations) {
      this.modifiedDecorations.clear();
    }

    // Create decorations for every line
    const lineCount = model.getLineCount();
    const decorations: monaco.editor.IModelDeltaDecoration[] = [];

    for (let lineNumber = 1; lineNumber <= lineCount; lineNumber++) {
      decorations.push({
        range: new window.monaco.Range(lineNumber, 1, lineNumber, 1),
        options: {
          isWholeLine: false,
          glyphMarginClassName: `comment-glyph-decoration comment-glyph-${editorType}-${lineNumber}`,
          glyphMarginHoverMessage: { value: "Comment line" },
          stickiness:
            window.monaco.editor.TrackedRangeStickiness
              .NeverGrowsWhenTypingAtEdges,
        },
      });
    }

    // Create or update decorations collection
    if (editorType === "original") {
      this.originalDecorations =
        editor.createDecorationsCollection(decorations);
    } else {
      this.modifiedDecorations =
        editor.createDecorationsCollection(decorations);
    }
  }

  /**
   * Setup hover and click behavior for glyph decorations
   */
  private setupHoverBehavior(editor: monaco.editor.IStandaloneCodeEditor) {
    if (!editor) {
      return;
    }

    let currentHoveredLine: number | null = null;
    const editorType =
      this.editor?.getOriginalEditor() === editor ? "original" : "modified";

    // Listen for mouse move events in the editor
    editor.onMouseMove((e) => {
      if (e.target.position) {
        const lineNumber = e.target.position.lineNumber;

        // Handle real-time drag preview updates
        if (
          this.isDragging &&
          this.dragStartLine !== null &&
          this.dragStartEditor === editorType &&
          this.showCommentBox
        ) {
          const startLine = Math.min(this.dragStartLine, lineNumber);
          const endLine = Math.max(this.dragStartLine, lineNumber);
          this.updateSelectedLinesPreview(startLine, endLine, editorType);
        }

        // Handle hover glyph visibility (only when not dragging)
        if (!this.isDragging) {
          // If we're hovering over a different line, update visibility
          if (currentHoveredLine !== lineNumber) {
            // Hide previous line's glyph
            if (currentHoveredLine !== null) {
              this.toggleGlyphVisibility(currentHoveredLine, false);
            }

            // Show current line's glyph
            this.toggleGlyphVisibility(lineNumber, true);
            currentHoveredLine = lineNumber;
          }
        }
      }
    });

    // Listen for mouse down events for click-to-comment and drag selection
    editor.onMouseDown((e) => {
      if (
        e.target.type ===
        window.monaco?.editor.MouseTargetType.GUTTER_GLYPH_MARGIN
      ) {
        if (e.target.position) {
          const lineNumber = e.target.position.lineNumber;

          // Prevent default Monaco behavior
          e.event.preventDefault();
          e.event.stopPropagation();

          // Check if there's an existing selection in this editor
          const selection = editor.getSelection();
          if (selection && !selection.isEmpty()) {
            // Use the existing selection
            const startLine = selection.startLineNumber;
            const endLine = selection.endLineNumber;
            this.showCommentForSelection(
              startLine,
              endLine,
              editorType,
              selection,
            );
          } else {
            // Start drag selection or show comment for clicked line
            this.isDragging = true;
            this.dragStartLine = lineNumber;
            this.dragStartEditor = editorType;

            // If it's just a click (not drag), show comment box immediately
            this.showCommentForLines(lineNumber, lineNumber, editorType);
          }
        }
      }
    });

    // Listen for mouse up events to end drag selection
    editor.onMouseUp((e) => {
      if (this.isDragging) {
        if (
          e.target.position &&
          this.dragStartLine !== null &&
          this.dragStartEditor === editorType
        ) {
          const endLine = e.target.position.lineNumber;
          const startLine = Math.min(this.dragStartLine, endLine);
          const finalEndLine = Math.max(this.dragStartLine, endLine);

          // Update the final selection (if comment box is not already shown)
          if (!this.showCommentBox) {
            this.showCommentForLines(startLine, finalEndLine, editorType);
          } else {
            // Just update the final selection since preview was already being updated
            this.updateSelectedLinesPreview(
              startLine,
              finalEndLine,
              editorType,
            );
          }
        }

        // Reset drag state
        this.isDragging = false;
        this.dragStartLine = null;
        this.dragStartEditor = null;
      }
    });

    // // Listen for mouse leave events
    // editor.onMouseLeave(() => {
    //   if (currentHoveredLine !== null) {
    //     this.toggleGlyphVisibility(currentHoveredLine, false);
    //     currentHoveredLine = null;
    //   }
    // });
  }

  /**
   * Update the selected lines preview during drag operations
   */
  private updateSelectedLinesPreview(
    startLine: number,
    endLine: number,
    editorType: "original" | "modified",
  ) {
    try {
      if (!this.editor) {
        return;
      }

      const targetModel =
        editorType === "original" ? this.originalModel : this.modifiedModel;

      if (!targetModel) {
        return;
      }

      // Get the text for the selected lines
      const lines: string[] = [];
      for (let i = startLine; i <= endLine; i++) {
        if (i <= targetModel.getLineCount()) {
          lines.push(targetModel.getLineContent(i));
        }
      }

      const selectedText = lines.join("\n");

      // Update the selected lines state
      this.selectedLines = {
        startLine,
        endLine,
        editorType,
        text: selectedText,
      };

      // Request update to refresh the preview
      this.requestUpdate();
    } catch (error) {
      console.error("Error updating selected lines preview:", error);
    }
  }

  /**
   * Show comment box for a Monaco editor selection
   */
  private showCommentForSelection(
    startLine: number,
    endLine: number,
    editorType: "original" | "modified",
    selection: monaco.Selection,
  ) {
    try {
      if (!this.editor) {
        return;
      }

      const targetModel =
        editorType === "original" ? this.originalModel : this.modifiedModel;

      if (!targetModel) {
        return;
      }

      // Get the exact selected text from the Monaco selection
      const selectedText = targetModel.getValueInRange(selection);

      // Set the selected lines state
      this.selectedLines = {
        startLine,
        endLine,
        editorType,
        text: selectedText,
      };

      // Calculate and set comment box position
      this.commentBoxPosition = this.calculateCommentBoxPosition(
        startLine,
        editorType,
      );

      // Reset comment text and show the box
      this.commentText = "";
      this.showCommentBox = true;

      // Clear any visible glyphs since we're showing the comment box
      this.clearAllVisibleGlyphs();

      // Request update to render the comment box
      this.requestUpdate();
    } catch (error) {
      console.error("Error showing comment for selection:", error);
    }
  }

  /**
   * Show comment box for a range of lines
   */
  private showCommentForLines(
    startLine: number,
    endLine: number,
    editorType: "original" | "modified",
  ) {
    try {
      if (!this.editor) {
        return;
      }

      const targetEditor =
        editorType === "original"
          ? this.editor.getOriginalEditor()
          : this.editor.getModifiedEditor();
      const targetModel =
        editorType === "original" ? this.originalModel : this.modifiedModel;

      if (!targetEditor || !targetModel) {
        return;
      }

      // Get the text for the selected lines
      const lines: string[] = [];
      for (let i = startLine; i <= endLine; i++) {
        if (i <= targetModel.getLineCount()) {
          lines.push(targetModel.getLineContent(i));
        }
      }

      const selectedText = lines.join("\n");

      // Set the selected lines state
      this.selectedLines = {
        startLine,
        endLine,
        editorType,
        text: selectedText,
      };

      // Calculate and set comment box position
      this.commentBoxPosition = this.calculateCommentBoxPosition(
        startLine,
        editorType,
      );

      // Reset comment text and show the box
      this.commentText = "";
      this.showCommentBox = true;

      // Clear any visible glyphs since we're showing the comment box
      this.clearAllVisibleGlyphs();

      // Request update to render the comment box
      this.requestUpdate();
    } catch (error) {
      console.error("Error showing comment for lines:", error);
    }
  }

  /**
   * Clear all currently visible glyphs
   */
  private clearAllVisibleGlyphs() {
    try {
      this.visibleGlyphs.forEach((glyphId) => {
        const element = this.container.value?.querySelector(`.${glyphId}`);
        if (element) {
          element.classList.remove("hover-visible");
        }
      });
      this.visibleGlyphs.clear();
    } catch (error) {
      console.error("Error clearing visible glyphs:", error);
    }
  }

  /**
   * Toggle the visibility of a glyph decoration for a specific line
   */
  private toggleGlyphVisibility(lineNumber: number, visible: boolean) {
    try {
      // If making visible, clear all existing visible glyphs first
      if (visible) {
        this.clearAllVisibleGlyphs();
      }

      // Find all glyph decorations for this line in both editors
      const selectors = [
        `comment-glyph-original-${lineNumber}`,
        `comment-glyph-modified-${lineNumber}`,
      ];

      selectors.forEach((glyphId) => {
        const element = this.container.value?.querySelector(`.${glyphId}`);
        if (element) {
          if (visible) {
            element.classList.add("hover-visible");
            this.visibleGlyphs.add(glyphId);
          } else {
            element.classList.remove("hover-visible");
            this.visibleGlyphs.delete(glyphId);
          }
        }
      });
    } catch (error) {
      console.error("Error toggling glyph visibility:", error);
    }
  }

  /**
   * Update editor options
   */
  setOptions(value: monaco.editor.IDiffEditorConstructionOptions) {
    if (this.editor) {
      this.editor.updateOptions(value);
      // Re-fit content after options change with scroll preservation
      if (this.fitEditorToContent) {
        setTimeout(() => {
          // Preserve scroll positions during options change
          const originalScrollTop =
            this.editor!.getOriginalEditor().getScrollTop();
          const modifiedScrollTop =
            this.editor!.getModifiedEditor().getScrollTop();

          this.fitEditorToContent!();

          // Restore scroll positions
          requestAnimationFrame(() => {
            this.editor!.getOriginalEditor().setScrollTop(originalScrollTop);
            this.editor!.getModifiedEditor().setScrollTop(modifiedScrollTop);
          });
        }, 50);
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
      // Re-fit content after toggling with scroll preservation
      if (this.fitEditorToContent) {
        setTimeout(() => {
          // Preserve scroll positions during toggle
          const originalScrollTop =
            this.editor!.getOriginalEditor().getScrollTop();
          const modifiedScrollTop =
            this.editor!.getModifiedEditor().getScrollTop();

          this.fitEditorToContent!();

          // Restore scroll positions
          requestAnimationFrame(() => {
            this.editor!.getOriginalEditor().setScrollTop(originalScrollTop);
            this.editor!.getModifiedEditor().setScrollTop(modifiedScrollTop);
          });
        }, 100);
      }
    }
  }

  // Models for the editor
  private originalModel?: monaco.editor.ITextModel;
  private modifiedModel?: monaco.editor.ITextModel;

  // Decoration collections for glyph decorations
  private originalDecorations?: monaco.editor.IEditorDecorationsCollection;
  private modifiedDecorations?: monaco.editor.IEditorDecorationsCollection;

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
        // Ensure the container ref is available
        if (!this.container.value) {
          throw new Error(
            "Container element not available - component may not be fully rendered",
          );
        }

        // Create the diff editor with auto-sizing configuration
        this.editor = monaco.editor.createDiffEditor(this.container.value, {
          automaticLayout: false, // We'll resize manually
          readOnly: true,
          theme: "vs", // Always use light mode
          renderSideBySide: !this.inline,
          ignoreTrimWhitespace: false,
          // Enable glyph margin for both editors to show decorations
          glyphMargin: true,
          scrollbar: {
            // Ideally we'd handle the mouse wheel for the horizontal scrollbar,
            // but there doesn't seem to be that option. Setting
            // alwaysConsumeMousewheel false and handleMouseWheel true didn't
            // work for me.
            handleMouseWheel: false,
          },
          renderOverviewRuler: false, // Disable overview ruler
          scrollBeyondLastLine: false,
          // Focus on the differences by hiding unchanged regions
          hideUnchangedRegions: {
            enabled: true, // Enable the feature
            contextLineCount: 5, // Show 3 lines of context around each difference
            minimumLineCount: 3, // Hide regions only when they're at least 3 lines
            revealLineCount: 10, // Show 10 lines when expanding a hidden region
          },
        });

        this.setupKeyboardShortcuts();

        // If this is an editable view, set the correct read-only state for each editor
        if (this.editableRight) {
          // Make sure the original editor is always read-only
          this.editor
            .getOriginalEditor()
            .updateOptions({ readOnly: true, glyphMargin: true });
          // Make sure the modified editor is editable
          this.editor
            .getModifiedEditor()
            .updateOptions({ readOnly: false, glyphMargin: true });
        } else {
          // Ensure glyph margin is enabled on both editors even in read-only mode
          this.editor.getOriginalEditor().updateOptions({ glyphMargin: true });
          this.editor.getModifiedEditor().updateOptions({ glyphMargin: true });
        }

        // Set up auto-sizing
        this.setupAutoSizing();

        // Add Monaco editor to debug global
        this.addToDebugGlobal();
      }

      // Create or update models
      this.updateModels();
      // Add glyph decorations after models are set
      this.setupGlyphDecorations();
      // Set up content change listener
      this.setupContentChangeListener();

      // Fix cursor positioning issues by ensuring fonts are loaded
      document.fonts.ready.then(() => {
        if (this.editor) {
          // Preserve scroll positions during font remeasuring
          const originalScrollTop = this.editor
            .getOriginalEditor()
            .getScrollTop();
          const modifiedScrollTop = this.editor
            .getModifiedEditor()
            .getScrollTop();

          monaco.editor.remeasureFonts();

          if (this.fitEditorToContent) {
            this.fitEditorToContent();
          }

          // Restore scroll positions after font remeasuring
          requestAnimationFrame(() => {
            this.editor!.getOriginalEditor().setScrollTop(originalScrollTop);
            this.editor!.getModifiedEditor().setScrollTop(modifiedScrollTop);
          });
        }
      });

      // Force layout recalculation after a short delay with scroll preservation
      setTimeout(() => {
        if (this.editor && this.fitEditorToContent) {
          // Preserve scroll positions
          const originalScrollTop = this.editor
            .getOriginalEditor()
            .getScrollTop();
          const modifiedScrollTop = this.editor
            .getModifiedEditor()
            .getScrollTop();

          this.fitEditorToContent();

          // Restore scroll positions
          requestAnimationFrame(() => {
            this.editor!.getOriginalEditor().setScrollTop(originalScrollTop);
            this.editor!.getModifiedEditor().setScrollTop(modifiedScrollTop);
          });
        }
      }, 100);
    } catch (error) {
      console.error("Error initializing Monaco editor:", error);
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
      const originalUri = window.monaco!.Uri.parse(
        `file:///original-${timestamp}.${originalLang}`,
      );
      const modifiedUri = window.monaco!.Uri.parse(
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
      this.originalModel = window.monaco!.editor.createModel(
        this.originalCode || "",
        originalLang,
        originalUri,
      );

      this.modifiedModel = window.monaco!.editor.createModel(
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

        // Fit content after setting new models with scroll preservation
        if (this.fitEditorToContent) {
          setTimeout(() => {
            // Preserve scroll positions when fitting content after model changes
            const originalScrollTop =
              this.editor!.getOriginalEditor().getScrollTop();
            const modifiedScrollTop =
              this.editor!.getModifiedEditor().getScrollTop();

            this.fitEditorToContent!();

            // Restore scroll positions
            requestAnimationFrame(() => {
              this.editor!.getOriginalEditor().setScrollTop(originalScrollTop);
              this.editor!.getModifiedEditor().setScrollTop(modifiedScrollTop);
            });
          }, 50);
        }

        // Add glyph decorations after setting new models
        setTimeout(() => this.setupGlyphDecorations(), 100);
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
        // Use a slightly longer delay to ensure layout is stable with scroll preservation
        setTimeout(() => {
          if (this.fitEditorToContent && this.editor) {
            // Preserve scroll positions during model update layout
            const originalScrollTop = this.editor
              .getOriginalEditor()
              .getScrollTop();
            const modifiedScrollTop = this.editor
              .getModifiedEditor()
              .getScrollTop();

            this.fitEditorToContent();

            // Restore scroll positions
            requestAnimationFrame(() => {
              this.editor!.getOriginalEditor().setScrollTop(originalScrollTop);
              this.editor!.getModifiedEditor().setScrollTop(modifiedScrollTop);
            });
          }
        }, 100);
      } else {
        // If the editor isn't initialized yet but we received content,
        // ensure we're connected before initializing
        await this.ensureConnectedToDocument();
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
          // Trigger layout recalculation with scroll preservation
          if (this.fitEditorToContent) {
            // Preserve scroll positions during window resize
            const originalScrollTop = this.editor
              .getOriginalEditor()
              .getScrollTop();
            const modifiedScrollTop = this.editor
              .getModifiedEditor()
              .getScrollTop();

            this.fitEditorToContent();

            // Restore scroll positions
            requestAnimationFrame(() => {
              this.editor!.getOriginalEditor().setScrollTop(originalScrollTop);
              this.editor!.getModifiedEditor().setScrollTop(modifiedScrollTop);
            });
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
    // Ensure we're connected to the document before Monaco initialization
    await this.ensureConnectedToDocument();

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

  /**
   * Ensure this component and its container are properly connected to the document.
   * Monaco editor requires the container to be in the document for proper initialization.
   */
  private async ensureConnectedToDocument(): Promise<void> {
    // Wait for our own render to complete
    await this.updateComplete;

    // Verify the container ref is available
    if (!this.container.value) {
      throw new Error("Container element not available after updateComplete");
    }

    // Check if we're connected to the document
    if (!this.isConnected) {
      throw new Error("Component is not connected to the document");
    }

    // Verify the container is also in the document
    if (!this.container.value.isConnected) {
      throw new Error("Container element is not connected to the document");
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
          monaco: window.monaco!,
          editors: [],
          remeasureFonts: () => {
            window.monaco!.editor.remeasureFonts();
            (window as any).sketchDebug.editors.forEach(
              (editor: any, _index: number) => {
                if (editor && editor.layout) {
                  editor.layout();
                }
              },
            );
          },
          layoutAll: () => {
            (window as any).sketchDebug.editors.forEach(
              (editor: any, _index: number) => {
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

      // Clean up decorations
      if (this.originalDecorations) {
        this.originalDecorations.clear();
        this.originalDecorations = undefined;
      }

      if (this.modifiedDecorations) {
        this.modifiedDecorations.clear();
        this.modifiedDecorations = undefined;
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

      // Remove window resize handler if set
      if (this._windowResizeHandler) {
        window.removeEventListener("resize", this._windowResizeHandler);
        this._windowResizeHandler = null;
      }

      // Clear visible glyphs tracking
      this.visibleGlyphs.clear();
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
