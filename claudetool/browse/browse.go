// Package browse provides browser automation tools for the agent
package browse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/google/uuid"
	"sketch.dev/llm"
)

// ScreenshotDir is the directory where screenshots are stored
const ScreenshotDir = "/tmp/sketch-screenshots"

// BrowseTools contains all browser tools and manages a shared browser instance
type BrowseTools struct {
	ctx              context.Context
	cancel           context.CancelFunc
	browserCtx       context.Context
	browserCtxCancel context.CancelFunc
	mux              sync.Mutex
	initOnce         sync.Once
	initialized      bool
	initErr          error
	// Map to track screenshots by ID and their creation time
	screenshots      map[string]time.Time
	screenshotsMutex sync.Mutex
}

// NewBrowseTools creates a new set of browser automation tools
func NewBrowseTools(ctx context.Context) *BrowseTools {
	ctx, cancel := context.WithCancel(ctx)

	// Ensure the screenshot directory exists
	if err := os.MkdirAll(ScreenshotDir, 0755); err != nil {
		log.Printf("Failed to create screenshot directory: %v", err)
	}

	b := &BrowseTools{
		ctx:         ctx,
		cancel:      cancel,
		screenshots: make(map[string]time.Time),
	}

	return b
}

// Initialize starts the browser if it's not already running
func (b *BrowseTools) Initialize() error {
	b.mux.Lock()
	defer b.mux.Unlock()

	b.initOnce.Do(func() {
		// ChromeDP.ExecPath has a list of common places to find Chrome...
		opts := chromedp.DefaultExecAllocatorOptions[:]
		allocCtx, _ := chromedp.NewExecAllocator(b.ctx, opts...)
		browserCtx, browserCancel := chromedp.NewContext(
			allocCtx,
			chromedp.WithLogf(log.Printf),
		)

		b.browserCtx = browserCtx
		b.browserCtxCancel = browserCancel

		// Ensure the browser starts
		if err := chromedp.Run(browserCtx); err != nil {
			b.initErr = fmt.Errorf("failed to start browser (please apt get chromium or equivalent): %w", err)
			return
		}
		b.initialized = true
	})

	return b.initErr
}

// Close shuts down the browser
func (b *BrowseTools) Close() {
	b.mux.Lock()
	defer b.mux.Unlock()

	if b.browserCtxCancel != nil {
		b.browserCtxCancel()
		b.browserCtxCancel = nil
	}

	if b.cancel != nil {
		b.cancel()
	}

	b.initialized = false
	log.Println("Browser closed")
}

// GetBrowserContext returns the context for browser operations
func (b *BrowseTools) GetBrowserContext() (context.Context, error) {
	if err := b.Initialize(); err != nil {
		return nil, err
	}
	return b.browserCtx, nil
}

// All tools return this as a response when successful
type baseResponse struct {
	Status string `json:"status,omitempty"`
}

func successResponse() string {
	return `{"status":"success"}`
}

func errorResponse(err error) string {
	return fmt.Sprintf(`{"status":"error","error":"%s"}`, err.Error())
}

// NavigateTool definition
type navigateInput struct {
	URL string `json:"url"`
}

// NewNavigateTool creates a tool for navigating to URLs
func (b *BrowseTools) NewNavigateTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_navigate",
		Description: "Navigate the browser to a specific URL and wait for page to load",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"url": {
					"type": "string",
					"description": "The URL to navigate to"
				}
			},
			"required": ["url"]
		}`),
		Run: b.navigateRun,
	}
}

func (b *BrowseTools) navigateRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input navigateInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	err = chromedp.Run(browserCtx,
		chromedp.Navigate(input.URL),
		chromedp.WaitReady("body"),
	)
	if err != nil {
		return errorResponse(err), nil
	}

	return successResponse(), nil
}

// ClickTool definition
type clickInput struct {
	Selector    string `json:"selector"`
	WaitVisible bool   `json:"wait_visible,omitempty"`
}

// NewClickTool creates a tool for clicking elements
func (b *BrowseTools) NewClickTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_click",
		Description: "Click the first element matching a CSS selector",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selector": {
					"type": "string",
					"description": "CSS selector for the element to click"
				},
				"wait_visible": {
					"type": "boolean",
					"description": "Wait for the element to be visible before clicking"
				}
			},
			"required": ["selector"]
		}`),
		Run: b.clickRun,
	}
}

func (b *BrowseTools) clickRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input clickInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	actions := []chromedp.Action{
		chromedp.WaitReady(input.Selector),
	}

	if input.WaitVisible {
		actions = append(actions, chromedp.WaitVisible(input.Selector))
	}

	actions = append(actions, chromedp.Click(input.Selector))

	err = chromedp.Run(browserCtx, actions...)
	if err != nil {
		return errorResponse(err), nil
	}

	return successResponse(), nil
}

// TypeTool definition
type typeInput struct {
	Selector string `json:"selector"`
	Text     string `json:"text"`
	Clear    bool   `json:"clear,omitempty"`
}

// NewTypeTool creates a tool for typing into input elements
func (b *BrowseTools) NewTypeTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_type",
		Description: "Type text into an input or textarea element",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selector": {
					"type": "string",
					"description": "CSS selector for the input element"
				},
				"text": {
					"type": "string",
					"description": "Text to type into the element"
				},
				"clear": {
					"type": "boolean",
					"description": "Clear the input field before typing"
				}
			},
			"required": ["selector", "text"]
		}`),
		Run: b.typeRun,
	}
}

func (b *BrowseTools) typeRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input typeInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	actions := []chromedp.Action{
		chromedp.WaitReady(input.Selector),
		chromedp.WaitVisible(input.Selector),
	}

	if input.Clear {
		actions = append(actions, chromedp.Clear(input.Selector))
	}

	actions = append(actions, chromedp.SendKeys(input.Selector, input.Text))

	err = chromedp.Run(browserCtx, actions...)
	if err != nil {
		return errorResponse(err), nil
	}

	return successResponse(), nil
}

// WaitForTool definition
type waitForInput struct {
	Selector  string `json:"selector"`
	TimeoutMS int    `json:"timeout_ms,omitempty"`
}

// NewWaitForTool creates a tool for waiting for elements
func (b *BrowseTools) NewWaitForTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_wait_for",
		Description: "Wait for an element to be present in the DOM",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selector": {
					"type": "string",
					"description": "CSS selector for the element to wait for"
				},
				"timeout_ms": {
					"type": "integer",
					"description": "Maximum time to wait in milliseconds (default: 30000)"
				}
			},
			"required": ["selector"]
		}`),
		Run: b.waitForRun,
	}
}

func (b *BrowseTools) waitForRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input waitForInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	timeout := 30000 // default timeout 30 seconds
	if input.TimeoutMS > 0 {
		timeout = input.TimeoutMS
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	timeoutCtx, cancel := context.WithTimeout(browserCtx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	err = chromedp.Run(timeoutCtx, chromedp.WaitReady(input.Selector))
	if err != nil {
		return errorResponse(err), nil
	}

	return successResponse(), nil
}

// GetTextTool definition
type getTextInput struct {
	Selector string `json:"selector"`
}

type getTextOutput struct {
	Text string `json:"text"`
}

// NewGetTextTool creates a tool for getting text from elements
func (b *BrowseTools) NewGetTextTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_get_text",
		Description: "Get the innerText of an element",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selector": {
					"type": "string",
					"description": "CSS selector for the element to get text from"
				}
			},
			"required": ["selector"]
		}`),
		Run: b.getTextRun,
	}
}

func (b *BrowseTools) getTextRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input getTextInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	var text string
	err = chromedp.Run(browserCtx,
		chromedp.WaitReady(input.Selector),
		chromedp.Text(input.Selector, &text),
	)
	if err != nil {
		return errorResponse(err), nil
	}

	output := getTextOutput{Text: text}
	result, err := json.Marshal(output)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to marshal response: %w", err)), nil
	}

	return string(result), nil
}

// EvalTool definition
type evalInput struct {
	Expression string `json:"expression"`
}

type evalOutput struct {
	Result any `json:"result"`
}

// NewEvalTool creates a tool for evaluating JavaScript
func (b *BrowseTools) NewEvalTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_eval",
		Description: "Evaluate JavaScript in the browser context",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"expression": {
					"type": "string",
					"description": "JavaScript expression to evaluate"
				}
			},
			"required": ["expression"]
		}`),
		Run: b.evalRun,
	}
}

func (b *BrowseTools) evalRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input evalInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	var result any
	err = chromedp.Run(browserCtx, chromedp.Evaluate(input.Expression, &result))
	if err != nil {
		return errorResponse(err), nil
	}

	output := evalOutput{Result: result}
	response, err := json.Marshal(output)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to marshal response: %w", err)), nil
	}

	return string(response), nil
}

// ScreenshotTool definition
type screenshotInput struct {
	Selector string `json:"selector,omitempty"`
	Format   string `json:"format,omitempty"`
}

type screenshotOutput struct {
	ID string `json:"id"`
}

// NewScreenshotTool creates a tool for taking screenshots
func (b *BrowseTools) NewScreenshotTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_screenshot",
		Description: "Take a screenshot of the page or a specific element",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selector": {
					"type": "string",
					"description": "CSS selector for the element to screenshot (optional)"	
				},
				"format": {
					"type": "string",
					"description": "Output format ('base64' or 'png'), defaults to 'base64'",
					"enum": ["base64", "png"]
				}
			}
		}`),
		Run: b.screenshotRun,
	}
}

func (b *BrowseTools) screenshotRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input screenshotInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	var buf []byte
	var actions []chromedp.Action

	if input.Selector != "" {
		// Take screenshot of specific element
		actions = append(actions,
			chromedp.WaitReady(input.Selector),
			chromedp.Screenshot(input.Selector, &buf, chromedp.NodeVisible),
		)
	} else {
		// Take full page screenshot
		actions = append(actions, chromedp.CaptureScreenshot(&buf))
	}

	err = chromedp.Run(browserCtx, actions...)
	if err != nil {
		return errorResponse(err), nil
	}

	// Save the screenshot and get its ID
	id := b.SaveScreenshot(buf)
	if id == "" {
		return errorResponse(fmt.Errorf("failed to save screenshot")), nil
	}

	// Return the ID in the response
	output := screenshotOutput{ID: id}
	response, err := json.Marshal(output)
	if err != nil {
		return errorResponse(fmt.Errorf("failed to marshal response: %w", err)), nil
	}

	return string(response), nil
}

// ScrollIntoViewTool definition
type scrollIntoViewInput struct {
	Selector string `json:"selector"`
}

// NewScrollIntoViewTool creates a tool for scrolling elements into view
func (b *BrowseTools) NewScrollIntoViewTool() *llm.Tool {
	return &llm.Tool{
		Name:        "browser_scroll_into_view",
		Description: "Scroll an element into view if it's not visible",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"selector": {
					"type": "string",
					"description": "CSS selector for the element to scroll into view"
				}
			},
			"required": ["selector"]
		}`),
		Run: b.scrollIntoViewRun,
	}
}

func (b *BrowseTools) scrollIntoViewRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input scrollIntoViewInput
	if err := json.Unmarshal(m, &input); err != nil {
		return errorResponse(fmt.Errorf("invalid input: %w", err)), nil
	}

	browserCtx, err := b.GetBrowserContext()
	if err != nil {
		return errorResponse(err), nil
	}

	script := fmt.Sprintf(`
		const el = document.querySelector('%s');
		if (el) {
			el.scrollIntoView({behavior: 'smooth', block: 'center'});
			return true;
		}
		return false;
	`, input.Selector)

	var result bool
	err = chromedp.Run(browserCtx,
		chromedp.WaitReady(input.Selector),
		chromedp.Evaluate(script, &result),
	)
	if err != nil {
		return errorResponse(err), nil
	}

	if !result {
		return errorResponse(fmt.Errorf("element not found: %s", input.Selector)), nil
	}

	return successResponse(), nil
}

// GetAllTools returns all browser tools
func (b *BrowseTools) GetAllTools() []*llm.Tool {
	return []*llm.Tool{
		b.NewNavigateTool(),
		b.NewClickTool(),
		b.NewTypeTool(),
		b.NewWaitForTool(),
		b.NewGetTextTool(),
		b.NewEvalTool(),
		b.NewScreenshotTool(),
		b.NewScrollIntoViewTool(),
	}
}

// SaveScreenshot saves a screenshot to disk and returns its ID
func (b *BrowseTools) SaveScreenshot(data []byte) string {
	// Generate a unique ID
	id := uuid.New().String()

	// Save the file
	filePath := filepath.Join(ScreenshotDir, id+".png")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		log.Printf("Failed to save screenshot: %v", err)
		return ""
	}

	// Track this screenshot
	b.screenshotsMutex.Lock()
	b.screenshots[id] = time.Now()
	b.screenshotsMutex.Unlock()

	return id
}

// GetScreenshotPath returns the full path to a screenshot by ID
func GetScreenshotPath(id string) string {
	return filepath.Join(ScreenshotDir, id+".png")
}
