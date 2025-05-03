package browse

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"sketch.dev/llm"
)

func TestToolCreation(t *testing.T) {
	// Create browser tools instance
	tools := NewBrowseTools(context.Background())

	// Test each tool has correct name and description
	toolTests := []struct {
		tool          *llm.Tool
		expectedName  string
		shortDesc     string
		requiredProps []string
	}{
		{tools.NewNavigateTool(), "browser_navigate", "Navigate", []string{"url"}},
		{tools.NewClickTool(), "browser_click", "Click", []string{"selector"}},
		{tools.NewTypeTool(), "browser_type", "Type", []string{"selector", "text"}},
		{tools.NewWaitForTool(), "browser_wait_for", "Wait", []string{"selector"}},
		{tools.NewGetTextTool(), "browser_get_text", "Get", []string{"selector"}},
		{tools.NewEvalTool(), "browser_eval", "Evaluate", []string{"expression"}},
		{tools.NewScreenshotTool(), "browser_screenshot", "Take", nil},
		{tools.NewScrollIntoViewTool(), "browser_scroll_into_view", "Scroll", []string{"selector"}},
	}

	for _, tt := range toolTests {
		t.Run(tt.expectedName, func(t *testing.T) {
			if tt.tool.Name != tt.expectedName {
				t.Errorf("expected name %q, got %q", tt.expectedName, tt.tool.Name)
			}

			if !strings.Contains(tt.tool.Description, tt.shortDesc) {
				t.Errorf("description %q should contain %q", tt.tool.Description, tt.shortDesc)
			}

			// Verify schema has required properties
			if len(tt.requiredProps) > 0 {
				var schema struct {
					Required []string `json:"required"`
				}
				if err := json.Unmarshal(tt.tool.InputSchema, &schema); err != nil {
					t.Fatalf("failed to unmarshal schema: %v", err)
				}

				for _, prop := range tt.requiredProps {
					if !slices.Contains(schema.Required, prop) {
						t.Errorf("property %q should be required", prop)
					}
				}
			}
		})
	}
}

func TestGetAllTools(t *testing.T) {
	// Create browser tools instance
	tools := NewBrowseTools(context.Background())

	// Get all tools
	allTools := tools.GetAllTools()

	// We should have 8 tools
	if len(allTools) != 8 {
		t.Errorf("expected 8 tools, got %d", len(allTools))
	}

	// Check that each tool has the expected name prefix
	for _, tool := range allTools {
		if !strings.HasPrefix(tool.Name, "browser_") {
			t.Errorf("tool name %q does not have prefix 'browser_'", tool.Name)
		}
	}
}

// TestBrowserInitialization verifies that the browser can start correctly
func TestBrowserInitialization(t *testing.T) {
	// Skip long tests in short mode
	if testing.Short() {
		t.Skip("skipping browser initialization test in short mode")
	}

	// Create browser tools instance
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tools := NewBrowseTools(ctx)

	// Initialize the browser
	err := tools.Initialize()
	if err != nil {
		// If browser automation is not available, skip the test
		if strings.Contains(err.Error(), "browser automation not available") {
			t.Skip("Browser automation not available in this environment")
		} else {
			t.Fatalf("Failed to initialize browser: %v", err)
		}
	}

	// Clean up
	defer tools.Close()

	// Get browser context to verify it's working
	browserCtx, err := tools.GetBrowserContext()
	if err != nil {
		t.Fatalf("Failed to get browser context: %v", err)
	}

	// Try to navigate to a simple page
	var title string
	err = chromedp.Run(browserCtx,
		chromedp.Navigate("about:blank"),
		chromedp.Title(&title),
	)
	if err != nil {
		t.Fatalf("Failed to navigate to about:blank: %v", err)
	}

	t.Logf("Successfully navigated to about:blank, title: %q", title)
}

// TestNavigateTool verifies that the navigate tool works correctly
func TestNavigateTool(t *testing.T) {
	// Skip long tests in short mode
	if testing.Short() {
		t.Skip("skipping navigate tool test in short mode")
	}

	// Create browser tools instance
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tools := NewBrowseTools(ctx)
	defer tools.Close()

	// Check if browser initialization works
	if err := tools.Initialize(); err != nil {
		if strings.Contains(err.Error(), "browser automation not available") {
			t.Skip("Browser automation not available in this environment")
		}
	}

	// Get the navigate tool
	navTool := tools.NewNavigateTool()

	// Create input for the navigate tool
	input := map[string]string{"url": "https://example.com"}
	inputJSON, _ := json.Marshal(input)

	// Call the tool
	result, err := navTool.Run(ctx, json.RawMessage(inputJSON))
	if err != nil {
		t.Fatalf("Error running navigate tool: %v", err)
	}

	// Verify the response is successful
	var response struct {
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}

	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("Error unmarshaling response: %v", err)
	}

	if response.Status != "success" {
		// If browser automation is not available, skip the test
		if strings.Contains(response.Error, "browser automation not available") {
			t.Skip("Browser automation not available in this environment")
		} else {
			t.Errorf("Expected status 'success', got '%s' with error: %s", response.Status, response.Error)
		}
	}

	// Try to get the page title to verify the navigation worked
	browserCtx, err := tools.GetBrowserContext()
	if err != nil {
		// If browser automation is not available, skip the test
		if strings.Contains(err.Error(), "browser automation not available") {
			t.Skip("Browser automation not available in this environment")
		} else {
			t.Fatalf("Failed to get browser context: %v", err)
		}
	}

	var title string
	err = chromedp.Run(browserCtx, chromedp.Title(&title))
	if err != nil {
		t.Fatalf("Failed to get page title: %v", err)
	}

	t.Logf("Successfully navigated to example.com, title: %q", title)
	if title != "Example Domain" {
		t.Errorf("Expected title 'Example Domain', got '%s'", title)
	}
}

// TestScreenshotTool tests that the screenshot tool properly saves files
func TestScreenshotTool(t *testing.T) {
	// Create browser tools instance
	ctx := context.Background()
	tools := NewBrowseTools(ctx)

	// Test SaveScreenshot function directly
	testData := []byte("test image data")
	id := tools.SaveScreenshot(testData)
	if id == "" {
		t.Fatal("SaveScreenshot returned empty ID")
	}

	// Get the file path and check if the file exists
	filePath := GetScreenshotPath(id)
	_, err := os.Stat(filePath)
	if err != nil {
		t.Fatalf("Failed to find screenshot file: %v", err)
	}

	// Read the file contents
	contents, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("Failed to read screenshot file: %v", err)
	}

	// Check the file contents
	if string(contents) != string(testData) {
		t.Errorf("File contents don't match: expected %q, got %q", string(testData), string(contents))
	}

	// Clean up the test file
	os.Remove(filePath)
}
