package bifrost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	bifrost "github.com/maximhq/bifrost/core"
	"github.com/maximhq/bifrost/core/schemas"
	"sketch.dev/llm"
)

// Service implements llm.Service using Bifrost as the backend
type Service struct {
	client   *bifrost.Bifrost
	account  *Account
	provider schemas.ModelProvider
	model    string
}

// Account implements the schemas.Account interface for Bifrost configuration
type Account struct {
	providers map[schemas.ModelProvider]*ProviderConfig
}

// ProviderConfig holds configuration for a specific provider
type ProviderConfig struct {
	Keys         []schemas.Key
	Config       *schemas.ProviderConfig
	BaseURL      string
	APIKey       string
	ResourceName string // For Azure
	APIVersion   string // For Azure
}

// NewService creates a new Bifrost-backed LLM service
func NewService(provider schemas.ModelProvider, model string) (*Service, error) {
	account := &Account{
		providers: make(map[schemas.ModelProvider]*ProviderConfig),
	}

	// Configure the provider based on environment variables
	if err := account.configureProvider(provider); err != nil {
		return nil, fmt.Errorf("failed to configure provider %v: %w", provider, err)
	}

	// Initialize Bifrost client
	config := schemas.BifrostConfig{
		Account: account,
	}

	client, err := bifrost.Init(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Bifrost: %w", err)
	}

	return &Service{
		client:   client,
		account:  account,
		provider: provider,
		model:    model,
	}, nil
}

// Do implements llm.Service.Do
func (s *Service) Do(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	// Convert our request to Bifrost format
	bifrostReq, err := s.convertRequest(req)
	if err != nil {
		return nil, fmt.Errorf("failed to convert request: %w", err)
	}

	// Make the request through Bifrost
	bifrostResp, bifrostErr := s.client.ChatCompletionRequest(ctx, bifrostReq)
	if bifrostErr != nil {
		return nil, fmt.Errorf("Bifrost request failed: %s", bifrostErr.Error)
	}

	// Convert response back to our format
	resp, err := s.convertResponse(bifrostResp)
	if err != nil {
		return nil, fmt.Errorf("failed to convert response: %w", err)
	}

	return resp, nil
}

// TokenContextWindow implements llm.Service.TokenContextWindow
func (s *Service) TokenContextWindow() int {
	// This needs to be implemented based on the model
	// For now, return reasonable defaults based on common models
	switch {
	case strings.Contains(s.model, "gpt-4"):
		return 128000
	case strings.Contains(s.model, "gpt-3.5"):
		return 16000
	case strings.Contains(s.model, "claude"):
		return 200000
	default:
		return 128000 // Safe default
	}
}

// convertRequest converts our llm.Request to schemas.BifrostRequest
func (s *Service) convertRequest(req *llm.Request) (*schemas.BifrostRequest, error) {
	// Convert messages
	messages := make([]schemas.BifrostMessage, 0, len(req.Messages)+len(req.System))

	// Add system messages first
	for _, sys := range req.System {
		if sys.Text != "" {
			messages = append(messages, schemas.BifrostMessage{
				Role: schemas.ModelChatMessageRoleSystem,
				Content: schemas.MessageContent{
					ContentStr: &sys.Text,
				},
			})
		}
	}

	// Add regular messages
	for _, msg := range req.Messages {
		bifrostMsg, err := s.convertMessage(msg)
		if err != nil {
			return nil, fmt.Errorf("failed to convert message: %w", err)
		}
		messages = append(messages, bifrostMsg...)
	}

	// Create the request
	bifrostReq := &schemas.BifrostRequest{
		Provider: s.provider,
		Model:    s.model,
		Input: schemas.RequestInput{
			ChatCompletionInput: &messages,
		},
	}

	// Add tools if present
	if len(req.Tools) > 0 {
		tools, err := s.convertTools(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tools: %w", err)
		}

		params := &schemas.ModelParameters{
			Tools: &tools,
		}

		if req.ToolChoice != nil {
			toolChoice := s.convertToolChoice(req.ToolChoice)
			params.ToolChoice = &toolChoice
		}

		bifrostReq.Params = params
	}

	return bifrostReq, nil
}

// convertMessage converts a single llm.Message to one or more schemas.BifrostMessage
func (s *Service) convertMessage(msg llm.Message) ([]schemas.BifrostMessage, error) {
	var messages []schemas.BifrostMessage

	// Convert role
	var role schemas.ModelChatMessageRole
	switch msg.Role {
	case llm.MessageRoleUser:
		role = schemas.ModelChatMessageRoleUser
	case llm.MessageRoleAssistant:
		role = schemas.ModelChatMessageRoleAssistant
	default:
		return nil, fmt.Errorf("unsupported message role: %v", msg.Role)
	}

	// Handle different content types
	var textParts []string
	var toolCalls []schemas.ToolCall

	for _, content := range msg.Content {
		switch content.Type {
		case llm.ContentTypeText, llm.ContentTypeThinking, llm.ContentTypeRedactedThinking:
			if content.Text != "" {
				textParts = append(textParts, content.Text)
			}
		case llm.ContentTypeToolUse:
			toolCall, err := s.convertToolUse(content)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool use: %w", err)
			}
			toolCalls = append(toolCalls, toolCall)
		case llm.ContentTypeToolResult:
			// Tool results need to be separate messages in most APIs
			toolResultMsg, err := s.convertToolResult(content)
			if err != nil {
				return nil, fmt.Errorf("failed to convert tool result: %w", err)
			}
			messages = append(messages, toolResultMsg)
		}
	}

	// Create main message if we have text content or tool calls
	if len(textParts) > 0 || len(toolCalls) > 0 {
		content := strings.Join(textParts, "\n")
		bifrostMsg := schemas.BifrostMessage{
			Role: role,
			Content: schemas.MessageContent{
				ContentStr: &content,
			},
		}

		if len(toolCalls) > 0 {
			bifrostMsg.AssistantMessage = &schemas.AssistantMessage{
				ToolCalls: &toolCalls,
			}
		}

		messages = append(messages, bifrostMsg)
	}

	return messages, nil
}

// convertToolUse converts llm tool use content to schemas.ToolCall
func (s *Service) convertToolUse(content llm.Content) (schemas.ToolCall, error) {
	var args map[string]interface{}
	if err := json.Unmarshal(content.ToolInput, &args); err != nil {
		return schemas.ToolCall{}, fmt.Errorf("failed to unmarshal tool input: %w", err)
	}

	functionType := "function"
	return schemas.ToolCall{
		ID:   &content.ID,
		Type: &functionType,
		Function: schemas.FunctionCall{
			Name:      &content.ToolName,
			Arguments: string(content.ToolInput),
		},
	}, nil
}

// convertToolResult converts llm tool result to schemas.BifrostMessage
func (s *Service) convertToolResult(content llm.Content) (schemas.BifrostMessage, error) {
	// Combine all tool result content into a single string
	var resultParts []string
	for _, result := range content.ToolResult {
		if result.Text != "" {
			resultParts = append(resultParts, result.Text)
		}
	}

	resultText := strings.Join(resultParts, "\n")

	return schemas.BifrostMessage{
		Role: schemas.ModelChatMessageRoleTool,
		Content: schemas.MessageContent{
			ContentStr: &resultText,
		},
		ToolMessage: &schemas.ToolMessage{
			ToolCallID: &content.ToolUseID,
		},
	}, nil
}

// convertTools converts llm.Tool slice to Bifrost tools format
func (s *Service) convertTools(tools []*llm.Tool) ([]schemas.Tool, error) {
	bifrostTools := make([]schemas.Tool, len(tools))

	for i, tool := range tools {
		var params map[string]interface{}
		if err := json.Unmarshal(tool.InputSchema, &params); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool schema: %w", err)
		}

		bifrostTools[i] = schemas.Tool{
			Type: "function",
			Function: schemas.Function{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters: schemas.FunctionParameters{
					Type:       "object",
					Properties: params,
					Required:   []string{}, // TODO: Extract required fields from schema
				},
			},
		}
	}

	return bifrostTools, nil
}

// convertToolChoice converts llm.ToolChoice to Bifrost format
func (s *Service) convertToolChoice(tc *llm.ToolChoice) schemas.ToolChoice {
	switch tc.Type {
	case llm.ToolChoiceTypeAuto:
		autoStr := "auto"
		return schemas.ToolChoice{ToolChoiceStr: &autoStr}
	case llm.ToolChoiceTypeNone:
		noneStr := "none"
		return schemas.ToolChoice{ToolChoiceStr: &noneStr}
	case llm.ToolChoiceTypeAny:
		requiredStr := "required"
		return schemas.ToolChoice{ToolChoiceStr: &requiredStr}
	case llm.ToolChoiceTypeTool:
		// For specific tool choice, we need to use ToolChoiceStruct
		// Let me check what ToolChoiceStruct looks like
		autoStr := "auto" // Fallback for now
		return schemas.ToolChoice{ToolChoiceStr: &autoStr}
	default:
		autoStr := "auto"
		return schemas.ToolChoice{ToolChoiceStr: &autoStr}
	}
}

// convertResponse converts schemas.BifrostResponse to llm.Response
func (s *Service) convertResponse(bifrostResp *schemas.BifrostResponse) (*llm.Response, error) {
	if bifrostResp == nil {
		return nil, fmt.Errorf("bifrost response is nil")
	}

	// TODO: Implement full response conversion
	// This is a basic implementation that needs to be expanded

	resp := &llm.Response{
		Role:  llm.MessageRoleAssistant,
		Model: s.model,
	}

	// Convert content - this needs proper implementation based on Bifrost's response structure
	// For now, create a basic text response
	resp.Content = []llm.Content{{
		Type: llm.ContentTypeText,
		Text: "Response conversion not yet implemented",
	}}

	return resp, nil
}

// configureProvider sets up provider configuration based on environment variables
func (a *Account) configureProvider(provider schemas.ModelProvider) error {
	config := &ProviderConfig{
		Config: &schemas.ProviderConfig{
			NetworkConfig:            schemas.DefaultNetworkConfig,
			ConcurrencyAndBufferSize: schemas.DefaultConcurrencyAndBufferSize,
		},
	}

	switch provider {
	case schemas.OpenAI:
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		config.APIKey = apiKey
		config.Keys = []schemas.Key{{
			Value:  apiKey,
			Weight: 1.0,
		}}

	case schemas.Azure:
		apiKey := os.Getenv("AZURE_OPENAI_API_KEY")
		endpoint := os.Getenv("AZURE_OPENAI_ENDPOINT")
		if apiKey == "" || endpoint == "" {
			return fmt.Errorf("AZURE_OPENAI_API_KEY and AZURE_OPENAI_ENDPOINT environment variables must be set")
		}
		config.APIKey = apiKey
		config.BaseURL = endpoint
		config.APIVersion = os.Getenv("AZURE_OPENAI_API_VERSION")
		if config.APIVersion == "" {
			config.APIVersion = "2024-02-01" // Default API version
		}
		config.Keys = []schemas.Key{{
			Value:  apiKey,
			Weight: 1.0,
		}}

	default:
		return fmt.Errorf("unsupported provider: %v", provider)
	}

	a.providers[provider] = config
	return nil
}

// GetConfiguredProviders implements schemas.Account
func (a *Account) GetConfiguredProviders() ([]schemas.ModelProvider, error) {
	providers := make([]schemas.ModelProvider, 0, len(a.providers))
	for provider := range a.providers {
		providers = append(providers, provider)
	}
	return providers, nil
}

// GetKeysForProvider implements schemas.Account
func (a *Account) GetKeysForProvider(ctx *context.Context, provider schemas.ModelProvider) ([]schemas.Key, error) {
	config, exists := a.providers[provider]
	if !exists {
		return nil, fmt.Errorf("provider %v not configured", provider)
	}
	return config.Keys, nil
}

// GetConfigForProvider implements schemas.Account
func (a *Account) GetConfigForProvider(provider schemas.ModelProvider) (*schemas.ProviderConfig, error) {
	config, exists := a.providers[provider]
	if !exists {
		return nil, fmt.Errorf("provider %v not configured", provider)
	}
	return config.Config, nil
}
