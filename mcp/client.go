package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
	"sketch.dev/llm"
)

const (
	// DefaultMCPConnectionTimeout is the default timeout for connecting to MCP servers
	DefaultMCPConnectionTimeout = 120 * time.Second

	// DefaultMCPToolTimeout is the default timeout for executing MCP tool calls
	DefaultMCPToolTimeout = 120 * time.Second
)

// ServerConfig represents the configuration for an MCP server
type ServerConfig struct {
	Name    string            `json:"name,omitempty"`
	Type    string            `json:"type,omitempty"`    // "stdio", "http", "sse"
	URL     string            `json:"url,omitempty"`     // for http/sse
	Command string            `json:"command,omitempty"` // for stdio
	Args    []string          `json:"args,omitempty"`    // for stdio
	Env     map[string]string `json:"env,omitempty"`     // for stdio
	Headers map[string]string `json:"headers,omitempty"` // for http/sse
}

// MCPManager manages multiple MCP server connections
type MCPManager struct {
	mu      sync.RWMutex
	clients map[string]*MCPClientWrapper
}

// MCPClientWrapper wraps an MCP client connection
type MCPClientWrapper struct {
	name   string
	config ServerConfig
	client *client.Client
	tools  []*llm.Tool
}

// MCPServerConnection represents a successful MCP server connection with its tools
type MCPServerConnection struct {
	ServerName string
	Tools      []*llm.Tool
	ToolNames  []string // Original tool names without server prefix
}

// NewMCPManager creates a new MCP manager
func NewMCPManager() *MCPManager {
	return &MCPManager{
		clients: make(map[string]*MCPClientWrapper),
	}
}

// ParseServerConfigs parses JSON configuration strings into ServerConfig structs
func ParseServerConfigs(ctx context.Context, configs []string) ([]ServerConfig, []error) {
	if len(configs) == 0 {
		return nil, nil
	}

	var serverConfigs []ServerConfig
	var errors []error

	for i, configStr := range configs {
		var config ServerConfig
		if err := json.Unmarshal([]byte(configStr), &config); err != nil {
			slog.ErrorContext(ctx, "Failed to parse MCP server config", "config", configStr, "error", err)
			errors = append(errors, fmt.Errorf("config %d: %w", i, err))
			continue
		}
		// Require a name
		if config.Name == "" {
			errors = append(errors, fmt.Errorf("config %d: name is required", i))
			continue
		}
		serverConfigs = append(serverConfigs, config)
	}

	return serverConfigs, errors
}

// ConnectToServerConfigs connects to multiple parsed MCP server configs in parallel
func (m *MCPManager) ConnectToServerConfigs(ctx context.Context, serverConfigs []ServerConfig, timeout time.Duration, existingErrors []error) ([]MCPServerConnection, []error) {
	if len(serverConfigs) == 0 {
		return nil, existingErrors
	}

	slog.InfoContext(ctx, "Connecting to MCP servers", "count", len(serverConfigs), "timeout", timeout)

	// Connect to servers in parallel using sync.WaitGroup
	type result struct {
		tools         []*llm.Tool
		err           error
		serverName    string
		originalTools []string // Original tool names without server prefix
	}

	results := make(chan result, len(serverConfigs))
	// Create a timeout context only for the connection establishment goroutines
	connectionCtx, connectionCancel := context.WithTimeout(context.Background(), timeout)
	defer connectionCancel()

	for _, config := range serverConfigs {
		go func(cfg ServerConfig) {
			slog.InfoContext(ctx, "Connecting to MCP server", "server", cfg.Name, "type", cfg.Type, "url", cfg.URL, "command", cfg.Command)
			// Pass both the long-running context (ctx) and the connection timeout context
			tools, originalToolNames, err := m.connectToServerWithNames(ctx, connectionCtx, cfg)
			results <- result{
				tools:         tools,
				err:           err,
				serverName:    cfg.Name,
				originalTools: originalToolNames,
			}
		}(config)
	}

	// Collect results
	var connections []MCPServerConnection
	errors := make([]error, 0, len(existingErrors))
	errors = append(errors, existingErrors...)

NextServer:
	for range len(serverConfigs) {
		select {
		case res := <-results:
			if res.err != nil {
				slog.ErrorContext(ctx, "Failed to connect to MCP server", "server", res.serverName, "error", res.err)
				errors = append(errors, fmt.Errorf("MCP server %q: %w", res.serverName, res.err))
			} else {
				connection := MCPServerConnection{
					ServerName: res.serverName,
					Tools:      res.tools,
					ToolNames:  res.originalTools,
				}
				connections = append(connections, connection)
				slog.InfoContext(ctx, "Successfully connected to MCP server", "server", res.serverName, "tools", len(res.tools), "tool_names", res.originalTools)
			}
		case <-connectionCtx.Done():
			errors = append(errors, fmt.Errorf("timeout connecting to MCP servers"))
			break NextServer
		}
	}

	return connections, errors
}

// connectToServerWithNames connects to a single MCP server and returns tools with original names
func (m *MCPManager) connectToServerWithNames(longRunningCtx context.Context, connectionCtx context.Context, config ServerConfig) ([]*llm.Tool, []string, error) {
	tools, err := m.connectToServer(longRunningCtx, connectionCtx, config)
	if err != nil {
		return nil, nil, err
	}

	// Extract original tool names (remove server prefix)
	originalNames := make([]string, len(tools))
	for i, tool := range tools {
		// Tool names are in format "servername_toolname"
		parts := strings.SplitN(tool.Name, "_", 2)
		if len(parts) == 2 {
			originalNames[i] = parts[1]
		} else {
			originalNames[i] = tool.Name // fallback if no prefix
		}
	}

	return tools, originalNames, nil
}

// connectToServer connects to a single MCP server
// longRunningCtx: context for the ongoing MCP client lifecycle (SSE streams)
// connectionCtx: context with timeout for connection establishment only
func (m *MCPManager) connectToServer(longRunningCtx context.Context, connectionCtx context.Context, config ServerConfig) ([]*llm.Tool, error) {
	var mcpClient *client.Client
	var err error

	// Convert environment variables to []string format
	var envVars []string
	for k, v := range config.Env {
		envVars = append(envVars, k+"="+v)
	}

	switch config.Type {
	case "stdio", "":
		if config.Command == "" {
			return nil, fmt.Errorf("command is required for stdio transport")
		}
		mcpClient, err = client.NewStdioMCPClient(config.Command, envVars, config.Args...)
		// TODO: Get the transport, cast it to *transport.Stdio, and start a goroutine to pipe stderr from the subprocess
		// to our subprocess, but with each line prefixed with the server name.
	case "http":
		if config.URL == "" {
			return nil, fmt.Errorf("URL is required for HTTP transport")
		}
		// Use streamable HTTP client for HTTP transport
		var httpOptions []transport.StreamableHTTPCOption
		if len(config.Headers) > 0 {
			httpOptions = append(httpOptions, transport.WithHTTPHeaders(config.Headers))
		}
		mcpClient, err = client.NewStreamableHttpClient(config.URL, httpOptions...)
	case "sse":
		if config.URL == "" {
			return nil, fmt.Errorf("URL is required for SSE transport")
		}
		var sseOptions []transport.ClientOption
		if len(config.Headers) > 0 {
			sseOptions = append(sseOptions, transport.WithHeaders(config.Headers))
		}
		mcpClient, err = client.NewSSEMCPClient(config.URL, sseOptions...)
	default:
		return nil, fmt.Errorf("unsupported MCP transport type: %s", config.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create MCP client: %w", err)
	}

	// Start the client with the long-running context for SSE streams
	if err := mcpClient.Start(longRunningCtx); err != nil {
		return nil, fmt.Errorf("failed to start MCP client: %w", err)
	}

	// Initialize the client with connection timeout context
	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			Capabilities:    mcp.ClientCapabilities{},
			ClientInfo: mcp.Implementation{
				Name:    "sketch",
				Version: "1.0.0",
			},
		},
	}
	if _, err := mcpClient.Initialize(connectionCtx, initReq); err != nil {
		return nil, fmt.Errorf("failed to initialize MCP client: %w", err)
	}

	// Get available tools with connection timeout context
	toolsReq := mcp.ListToolsRequest{}
	toolsResp, err := mcpClient.ListTools(connectionCtx, toolsReq)
	if err != nil {
		return nil, fmt.Errorf("failed to list tools: %w", err)
	}

	// Convert MCP tools to llm.Tool
	llmTools, err := m.convertMCPTools(config.Name, mcpClient, toolsResp.Tools)
	if err != nil {
		return nil, fmt.Errorf("failed to convert tools: %w", err)
	}

	// Store the client
	clientWrapper := &MCPClientWrapper{
		name:   config.Name,
		config: config,
		client: mcpClient,
		tools:  llmTools,
	}

	m.mu.Lock()
	m.clients[config.Name] = clientWrapper
	m.mu.Unlock()

	return llmTools, nil
}

// convertMCPTools converts MCP tools to llm.Tool format
func (m *MCPManager) convertMCPTools(serverName string, mcpClient *client.Client, mcpTools []mcp.Tool) ([]*llm.Tool, error) {
	var llmTools []*llm.Tool

	for _, mcpTool := range mcpTools {
		// Convert the input schema
		schemaBytes, err := json.Marshal(mcpTool.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal input schema for tool %s: %w", mcpTool.Name, err)
		}

		llmTool := &llm.Tool{
			Name:        fmt.Sprintf("%s_%s", serverName, mcpTool.Name),
			Description: mcpTool.Description,
			InputSchema: json.RawMessage(schemaBytes),
			Run: func(toolName string, client *client.Client) func(ctx context.Context, input json.RawMessage) llm.ToolOut {
				return func(ctx context.Context, input json.RawMessage) llm.ToolOut {
					result, err := m.executeMCPTool(ctx, client, toolName, input)
					if err != nil {
						return llm.ErrorToolOut(err)
					}
					// Convert result to llm.Content
					return llm.ToolOut{LLMContent: []llm.Content{llm.StringContent(fmt.Sprintf("%v", result))}}
				}
			}(mcpTool.Name, mcpClient),
		}

		llmTools = append(llmTools, llmTool)
	}

	return llmTools, nil
}

// executeMCPTool executes an MCP tool call
func (m *MCPManager) executeMCPTool(ctx context.Context, mcpClient *client.Client, toolName string, input json.RawMessage) (any, error) {
	// Add timeout for tool execution
	// TODO: Expose the timeout as a tool call argument.
	ctxWithTimeout, cancel := context.WithTimeout(ctx, DefaultMCPToolTimeout)
	defer cancel()

	// Parse input arguments
	var args map[string]any
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, fmt.Errorf("failed to parse tool arguments: %w", err)
		}
	}

	// Call the MCP tool
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}
	resp, err := mcpClient.CallTool(ctxWithTimeout, req)
	if err != nil {
		return nil, fmt.Errorf("MCP tool call failed: %w", err)
	}

	// Return the content from the response
	return resp.Content, nil
}

// Close closes all MCP client connections
func (m *MCPManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, clientWrapper := range m.clients {
		if clientWrapper.client != nil {
			clientWrapper.client.Close()
		}
	}
	m.clients = make(map[string]*MCPClientWrapper)
}
