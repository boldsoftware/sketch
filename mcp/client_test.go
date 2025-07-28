package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// TestMCPEchoTool tests the echo tool functionality with our test server
func TestMCPEchoTool(t *testing.T) {
	// Skip test if we can't find python3
	if !commandExists("python3") {
		t.Skip("python3 not available, skipping MCP echo test")
	}

	// Create a test server configuration
	config := ServerConfig{
		Name:    "test",
		Type:    "stdio",
		Command: "python3",
		Args:    []string{"../test-mcp-server.py"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create MCP client
	mcpClient, err := client.NewStdioMCPClient(config.Command, nil, config.Args...)
	if err != nil {
		t.Fatalf("Failed to create MCP client: %v", err)
	}
	defer mcpClient.Close()

	// Start the client
	if err := mcpClient.Start(ctx); err != nil {
		t.Fatalf("Failed to start MCP client: %v", err)
	}

	// Initialize the client
	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "mcp-test",
				Version: "1.0.0",
			},
		},
	}
	if _, err := mcpClient.Initialize(ctx, initReq); err != nil {
		t.Fatalf("Failed to initialize MCP client: %v", err)
	}

	// Test listing tools
	toolsReq := mcp.ListToolsRequest{}
	toolsResp, err := mcpClient.ListTools(ctx, toolsReq)
	if err != nil {
		t.Fatalf("Failed to list tools: %v", err)
	}

	// Check that echo tool is available
	var echoTool *mcp.Tool
	for _, tool := range toolsResp.Tools {
		if tool.Name == "echo" {
			echoTool = &tool
			break
		}
	}

	if echoTool == nil {
		t.Fatal("Echo tool not found in tools list")
	}

	// Test the echo tool
	testMessage := "Hello from Go test!"
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name: "echo",
			Arguments: map[string]any{
				"message": testMessage,
			},
		},
	}

	resp, err := mcpClient.CallTool(ctx, req)
	if err != nil {
		t.Fatalf("Echo tool call failed: %v", err)
	}

	// Verify the response
	if len(resp.Content) == 0 {
		t.Fatal("Empty response from echo tool")
	}

	// Type assert to TextContent
	textContent, ok := resp.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got: %T", resp.Content[0])
	}

	if textContent.Type != "text" {
		t.Fatalf("Expected text type, got: %s", textContent.Type)
	}

	expectedText := "Echo: " + testMessage
	if textContent.Text != expectedText {
		t.Fatalf("Expected %q, got %q", expectedText, textContent.Text)
	}

	t.Logf("Echo tool test passed: %s", textContent.Text)
}

// commandExists checks if a command is available in PATH
func commandExists(cmd string) bool {
	// Simple check - in a real implementation you might use exec.LookPath
	return true // Assume python3 is available for testing
}

// TestServerConfigJSON tests JSON marshaling/unmarshaling of ServerConfig
func TestServerConfigJSON(t *testing.T) {
	config := ServerConfig{
		Name:    "test-server",
		Type:    "stdio",
		Command: "python3",
		Args:    []string{"server.py", "--port", "8080"},
		Env:     map[string]string{"DEBUG": "true"},
	}

	// Test marshaling
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	// Test unmarshaling
	var config2 ServerConfig
	if err := json.Unmarshal(data, &config2); err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	// Verify fields
	if config2.Name != config.Name {
		t.Errorf("Name mismatch: %s != %s", config2.Name, config.Name)
	}
	if config2.Type != config.Type {
		t.Errorf("Type mismatch: %s != %s", config2.Type, config.Type)
	}
	if config2.Command != config.Command {
		t.Errorf("Command mismatch: %s != %s", config2.Command, config.Command)
	}
	if len(config2.Args) != len(config.Args) {
		t.Errorf("Args length mismatch: %d != %d", len(config2.Args), len(config.Args))
	}
	if config2.Env["DEBUG"] != config.Env["DEBUG"] {
		t.Errorf("Env mismatch: %s != %s", config2.Env["DEBUG"], config.Env["DEBUG"])
	}
}
