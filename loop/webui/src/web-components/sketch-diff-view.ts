import {css, html, LitElement, unsafeCSS} from 'lit';
import {customElement, property, state} from 'lit/decorators.js';
import * as Diff2Html from "diff2html";

@customElement('sketch-diff-view')
export class SketchDiffView extends LitElement {
  // Current commit hash being viewed
  @property({type: String})
  commitHash: string = "";

  // Selected line in the diff for commenting
  @state()
  private selectedDiffLine: string | null = null;

  // View format (side-by-side or line-by-line)
  @state()
  private viewFormat: "side-by-side" | "line-by-line" = "side-by-side";

  static styles = css`
    .diff-view {
      flex: 1;
      display: flex;
      flex-direction: column;
      overflow: hidden;
      height: 100%;
    }
    
    .diff-container {
      height: 100%;
      overflow: auto;
      flex: 1;
      padding: 0 1rem;
    }
    
    #diff-view-controls {
      display: flex;
      justify-content: flex-end;
      padding: 10px;
      background: #f8f8f8;
      border-bottom: 1px solid #eee;
    }
    
    .diff-view-format {
      display: flex;
      gap: 10px;
    }
    
    .diff-view-format label {
      cursor: pointer;
    }
    
    .diff2html-content {
      font-family: var(--monospace-font);
      position: relative;
    }
    
    /* Comment box styles */
    .diff-comment-box {
      position: fixed;
      bottom: 80px;
      right: 20px;
      width: 400px;
      background-color: white;
      border: 1px solid #ddd;
      border-radius: 4px;
      box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
      padding: 16px;
      z-index: 1000;
    }
    
    .diff-comment-box h3 {
      margin-top: 0;
      margin-bottom: 10px;
      font-size: 16px;
    }
    
    .selected-line {
      margin-bottom: 10px;
      font-size: 14px;
    }
    
    .selected-line pre {
      padding: 6px;
      background: #f5f5f5;
      border: 1px solid #eee;
      border-radius: 3px;
      margin: 5px 0;
      max-height: 100px;
      overflow: auto;
      font-family: var(--monospace-font);
      font-size: 13px;
      white-space: pre-wrap;
    }
    
    #diffCommentInput {
      width: 100%;
      height: 100px;
      padding: 8px;
      border: 1px solid #ddd;
      border-radius: 4px;
      resize: vertical;
      font-family: inherit;
      margin-bottom: 10px;
    }
    
    .diff-comment-buttons {
      display: flex;
      justify-content: flex-end;
      gap: 8px;
    }
    
    .diff-comment-buttons button {
      padding: 6px 12px;
      border-radius: 4px;
      border: 1px solid #ddd;
      background: white;
      cursor: pointer;
    }
    
    .diff-comment-buttons button:hover {
      background: #f5f5f5;
    }
    
    .diff-comment-buttons button#submitDiffComment {
      background: #1a73e8;
      color: white;
      border-color: #1a73e8;
    }
    
    .diff-comment-buttons button#submitDiffComment:hover {
      background: #1967d2;
    }
    
    /* Styles for the comment button on diff lines */
    .d2h-gutter-comment-button {
      position: absolute;
      right: 0;
      top: 50%;
      transform: translateY(-50%);
      visibility: hidden;
      background: rgba(255, 255, 255, 0.8);
      border-radius: 50%;
      width: 16px;
      height: 16px;
      line-height: 13px;
      text-align: center;
      font-size: 14px;
      cursor: pointer;
      color: #666;
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
    }
    
    tr:hover .d2h-gutter-comment-button {
      visibility: visible;
    }
    
    .d2h-gutter-comment-button:hover {
      background: white;
      color: #333;
      box-shadow: 0 1px 3px rgba(0, 0, 0, 0.2);
    }
  `;
  
  constructor() {
    super();
  }
  
  // See https://lit.dev/docs/components/lifecycle/
  connectedCallback() {
    super.connectedCallback();
    
    // Load the diff2html CSS if needed
    this.loadDiff2HtmlCSS();
  }
  
  // Load diff2html CSS into the shadow DOM
  private async loadDiff2HtmlCSS() {
    try {
      // Check if diff2html styles are already loaded
      const styleId = 'diff2html-styles';
      if (this.shadowRoot?.getElementById(styleId)) {
        return; // Already loaded
      }
      
      // Fetch the diff2html CSS
      const response = await fetch('static/diff2html.min.css');
      
      if (!response.ok) {
        console.error(`Failed to load diff2html CSS: ${response.status} ${response.statusText}`);
        return;
      }
      
      const cssText = await response.text();
      
      // Create a style element and append to shadow DOM
      const style = document.createElement('style');
      style.id = styleId;
      style.textContent = cssText;
      this.shadowRoot?.appendChild(style);
      
      console.log('diff2html CSS loaded into shadow DOM');
    } catch (error) {
      console.error('Error loading diff2html CSS:', error);
    }
  }
  
  // See https://lit.dev/docs/components/lifecycle/
  disconnectedCallback() {
    super.disconnectedCallback();
  }
  
  // Method called to load diff content
  async loadDiffContent() {
    // Wait for the component to be rendered
    await this.updateComplete;
    
    const diff2htmlContent = this.shadowRoot?.getElementById("diff2htmlContent");
    if (!diff2htmlContent) return;
    
    try {
      // Show loading state
      diff2htmlContent.innerHTML = "Loading enhanced diff...";
      
      // Build the diff URL - include commit hash if specified
      const diffUrl = this.commitHash ? `diff?commit=${this.commitHash}` : "diff";
      
      // Fetch the diff from the server
      const response = await fetch(diffUrl);
      
      if (!response.ok) {
        throw new Error(
          `Server returned ${response.status}: ${response.statusText}`,
        );
      }
      
      const diffText = await response.text();
      
      if (!diffText || diffText.trim() === "") {
        diff2htmlContent.innerHTML =
          "<span style='color: #666; font-style: italic;'>No changes detected since conversation started.</span>";
        return;
      }
      
      // Render the diff using diff2html
      const diffHtml = Diff2Html.html(diffText, {
        outputFormat: this.viewFormat,
        drawFileList: true,
        matching: "lines",
        renderNothingWhenEmpty: false,
        colorScheme: "light" as any, // Force light mode to match the rest of the UI
      });
      
      // Insert the generated HTML
      diff2htmlContent.innerHTML = diffHtml;
      
      // Add CSS styles to ensure we don't have double scrollbars
      const d2hFiles = diff2htmlContent.querySelectorAll(".d2h-file-wrapper");
      d2hFiles.forEach((file) => {
        const contentElem = file.querySelector(".d2h-files-diff");
        if (contentElem) {
          // Remove internal scrollbar - the outer container will handle scrolling
          (contentElem as HTMLElement).style.overflow = "visible";
          (contentElem as HTMLElement).style.maxHeight = "none";
        }
      });
      
      // Add click event handlers to each code line for commenting
      this.setupDiffLineComments();
      
    } catch (error) {
      console.error("Error loading diff2html content:", error);
      const errorMessage =
        error instanceof Error ? error.message : "Unknown error";
      diff2htmlContent.innerHTML = `<span style='color: #dc3545;'>Error loading enhanced diff: ${errorMessage}</span>`;
    }
  }
  
  // Handle view format changes
  private handleViewFormatChange(event: Event) {
    const input = event.target as HTMLInputElement;
    if (input.checked) {
      this.viewFormat = input.value as "side-by-side" | "line-by-line";
      this.loadDiffContent();
    }
  }
  
  /**
   * Setup handlers for diff code lines to enable commenting
   */
  private setupDiffLineComments(): void {
    const diff2htmlContent = this.shadowRoot?.getElementById("diff2htmlContent");
    if (!diff2htmlContent) return;
    
    console.log("Setting up diff line comments");
    
    // Add plus buttons to each code line
    this.addCommentButtonsToCodeLines();
    
    // Use event delegation for handling clicks on plus buttons
    diff2htmlContent.addEventListener("click", (event) => {
      const target = event.target as HTMLElement;
      
      // Only respond to clicks on the plus button
      if (target.classList.contains("d2h-gutter-comment-button")) {
        // Find the parent row first
        const row = target.closest("tr");
        if (!row) return;
        
        // Then find the code line in that row
        const codeLine = row.querySelector(".d2h-code-side-line") || row.querySelector(".d2h-code-line");
        if (!codeLine) return;
        
        // Get the line text content
        const lineContent = codeLine.querySelector(".d2h-code-line-ctn");
        if (!lineContent) return;
        
        const lineText = lineContent.textContent?.trim() || "";
        
        // Get file name to add context
        const fileHeader = codeLine
          .closest(".d2h-file-wrapper")
          ?.querySelector(".d2h-file-name");
        const fileName = fileHeader
          ? fileHeader.textContent?.trim()
          : "Unknown file";
        
        // Get line number if available
        const lineNumElem = codeLine
          .closest("tr")
          ?.querySelector(".d2h-code-side-linenumber");
        const lineNum = lineNumElem ? lineNumElem.textContent?.trim() : "";
        const lineInfo = lineNum ? `Line ${lineNum}: ` : "";
        
        // Format the line for the comment box with file context and line number
        const formattedLine = `${fileName} ${lineInfo}${lineText}`;
        
        console.log("Comment button clicked for line: ", formattedLine);
        
        // Open the comment box with this line
        this.openDiffCommentBox(formattedLine);
        
        // Prevent event from bubbling up
        event.stopPropagation();
      }
    });
  }
  
  /**
   * Add plus buttons to each table row in the diff for commenting
   */
  private addCommentButtonsToCodeLines(): void {
    const diff2htmlContent = this.shadowRoot?.getElementById("diff2htmlContent");
    if (!diff2htmlContent) return;
    
    // Target code lines first, then find their parent rows
    const codeLines = diff2htmlContent.querySelectorAll(
      ".d2h-code-side-line, .d2h-code-line"
    );
    
    // Create a Set to store unique rows to avoid duplicates
    const rowsSet = new Set<HTMLElement>();
    
    // Get all rows that contain code lines
    codeLines.forEach(line => {
      const row = line.closest('tr');
      if (row) rowsSet.add(row as HTMLElement);
    });
    
    // Convert Set back to array for processing
    const codeRows = Array.from(rowsSet);
    
    codeRows.forEach((row) => {
      const rowElem = row as HTMLElement;
      
      // Skip info lines without actual code (e.g., "file added")
      if (rowElem.querySelector(".d2h-info")) {
        return;
      }
      
      // Find the code line number element (first TD in the row)
      const lineNumberCell = rowElem.querySelector(
        ".d2h-code-side-linenumber, .d2h-code-linenumber"
      );
      
      if (!lineNumberCell) return;
      
      // Create the plus button
      const plusButton = document.createElement("span");
      plusButton.className = "d2h-gutter-comment-button";
      plusButton.innerHTML = "+";
      plusButton.title = "Add a comment on this line";
      
      // Add button to the line number cell for proper positioning
      (lineNumberCell as HTMLElement).style.position = "relative"; // Ensure positioning context
      lineNumberCell.appendChild(plusButton);
    });
  }
  
  /**
   * Open the comment box for a selected diff line
   */
  private openDiffCommentBox(lineText: string): void {
    // Make sure the comment box div exists
    const commentBoxId = "diffCommentBox";
    let commentBox = this.shadowRoot?.getElementById(commentBoxId);
    
    // If it doesn't exist, create it
    if (!commentBox) {
      commentBox = document.createElement("div");
      commentBox.id = commentBoxId;
      commentBox.className = "diff-comment-box";
      
      // Create the comment box contents
      commentBox.innerHTML = `
        <h3>Add a comment</h3>
        <div class="selected-line">
          Line:
          <pre id="selectedLine"></pre>
        </div>
        <textarea
          id="diffCommentInput"
          placeholder="Enter your comment about this line..."
        ></textarea>
        <div class="diff-comment-buttons">
          <button id="cancelDiffComment">Cancel</button>
          <button id="submitDiffComment">Add Comment</button>
        </div>
      `;
      
      this.shadowRoot?.appendChild(commentBox);
    }
    
    // Store the selected line
    this.selectedDiffLine = lineText;
    
    // Display the line in the comment box
    const selectedLine = this.shadowRoot?.getElementById("selectedLine");
    if (selectedLine) {
      selectedLine.textContent = lineText;
    }
    
    // Reset the comment input
    const commentInput = this.shadowRoot?.getElementById(
      "diffCommentInput"
    ) as HTMLTextAreaElement;
    if (commentInput) {
      commentInput.value = "";
    }
    
    // Show the comment box
    if (commentBox) {
      commentBox.style.display = "block";
    }
    
    // Add event listeners for submit and cancel buttons
    const submitButton = this.shadowRoot?.getElementById("submitDiffComment");
    if (submitButton) {
      submitButton.onclick = () => this.submitDiffComment();
    }
    
    const cancelButton = this.shadowRoot?.getElementById("cancelDiffComment");
    if (cancelButton) {
      cancelButton.onclick = () => this.closeDiffCommentBox();
    }
    
    // Focus on the comment input
    if (commentInput) {
      commentInput.focus();
    }
  }
  
  /**
   * Close the diff comment box without submitting
   */
  private closeDiffCommentBox(): void {
    const commentBox = this.shadowRoot?.getElementById("diffCommentBox");
    if (commentBox) {
      commentBox.style.display = "none";
    }
    this.selectedDiffLine = null;
  }
  
  /**
   * Submit a comment on a diff line
   */
  private submitDiffComment(): void {
    const commentInput = this.shadowRoot?.getElementById(
      "diffCommentInput"
    ) as HTMLTextAreaElement;
    
    if (!commentInput) return;
    
    const comment = commentInput.value.trim();
    
    // Validate inputs
    if (!this.selectedDiffLine || !comment) {
      alert("Please select a line and enter a comment.");
      return;
    }
    
    // Format the comment in a readable way
    const formattedComment = `\`\`\`\n${this.selectedDiffLine}\n\`\`\`\n\n${comment}`;
    
    // Dispatch a custom event with the formatted comment
    const event = new CustomEvent('diff-comment', {
      detail: { comment: formattedComment },
      bubbles: true,
      composed: true
    });
    this.dispatchEvent(event);
    
    // Close only the comment box but keep the diff view open
    this.closeDiffCommentBox();
  }
  
  // Clear the current state
  public clearState(): void {
    this.commitHash = "";
  }
  
  // Show diff for a specific commit
  public showCommitDiff(commitHash: string): void {
    // Store the commit hash
    this.commitHash = commitHash;
    // Load the diff content
    this.loadDiffContent();
  }
  
  render() {
    return html`
      <div class="diff-view">
        <div class="diff-container">
          <div id="diff-view-controls">
            <div class="diff-view-format">
              <label>
                <input 
                  type="radio" 
                  name="diffViewFormat" 
                  value="side-by-side" 
                  ?checked=${this.viewFormat === "side-by-side"}
                  @change=${this.handleViewFormatChange}
                > Side-by-side
              </label>
              <label>
                <input 
                  type="radio" 
                  name="diffViewFormat" 
                  value="line-by-line"
                  ?checked=${this.viewFormat === "line-by-line"}
                  @change=${this.handleViewFormatChange}
                > Line-by-line
              </label>
            </div>
          </div>
          <div id="diff2htmlContent" class="diff2html-content"></div>
        </div>
      </div>
    `;
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "sketch-diff-view": SketchDiffView;
  }
}
