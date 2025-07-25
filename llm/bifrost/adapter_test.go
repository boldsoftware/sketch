package bifrost

import (
	"context"
	"os"
	"testing"

	"github.com/maximhq/bifrost/core/schemas"
	"sketch.dev/llm"
)

func TestNewService(t *testing.T) {
	// Skip if no API key is available
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}

	service, err := NewService(schemas.OpenAI, "gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	if service == nil {
		t.Fatal("Service is nil")
	}

	// Test that it implements llm.Service
	var _ llm.Service = service
}

func TestTokenContextWindow(t *testing.T) {
	service := &Service{model: "gpt-4"}

	contextWindow := service.TokenContextWindow()
	if contextWindow <= 0 {
		t.Errorf("Expected positive context window, got %d", contextWindow)
	}

	// Test different models
	testCases := []struct {
		model    string
		expected int
	}{
		{"gpt-4", 128000},
		{"gpt-3.5-turbo", 16000},
		{"claude-3", 200000},
		{"unknown-model", 128000},
	}

	for _, tc := range testCases {
		service.model = tc.model
		result := service.TokenContextWindow()
		if result != tc.expected {
			t.Errorf("Model %s: expected %d, got %d", tc.model, tc.expected, result)
		}
	}
}

func TestConvertRequest(t *testing.T) {
	service := &Service{
		provider: schemas.OpenAI,
		model:    "gpt-3.5-turbo",
	}

	// Test basic request conversion
	req := &llm.Request{
		Messages: []llm.Message{
			{
				Role: llm.MessageRoleUser,
				Content: []llm.Content{
					{
						Type: llm.ContentTypeText,
						Text: "Hello, world!",
					},
				},
			},
		},
		System: []llm.SystemContent{
			{
				Text: "You are a helpful assistant.",
			},
		},
	}

	bifrostReq, err := service.convertRequest(req)
	if err != nil {
		t.Fatalf("Failed to convert request: %v", err)
	}

	if bifrostReq == nil {
		t.Fatal("Converted request is nil")
	}

	if bifrostReq.Provider != schemas.OpenAI {
		t.Errorf("Expected provider OpenAI, got %v", bifrostReq.Provider)
	}

	if bifrostReq.Model != "gpt-3.5-turbo" {
		t.Errorf("Expected model gpt-3.5-turbo, got %s", bifrostReq.Model)
	}

	if bifrostReq.Input.ChatCompletionInput == nil {
		t.Fatal("ChatCompletionInput is nil")
	}

	messages := *bifrostReq.Input.ChatCompletionInput
	if len(messages) != 2 { // system + user message
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}

	// Check system message
	if messages[0].Role != schemas.ModelChatMessageRoleSystem {
		t.Errorf("Expected system role, got %v", messages[0].Role)
	}

	// Check user message
	if messages[1].Role != schemas.ModelChatMessageRoleUser {
		t.Errorf("Expected user role, got %v", messages[1].Role)
	}
}

func TestConvertTools(t *testing.T) {
	service := &Service{}

	tools := []*llm.Tool{
		{
			Name:        "test_function",
			Description: "A test function",
			InputSchema: llm.MustSchema(`{
				"type": "object",
				"properties": {
					"param1": {
						"type": "string",
						"description": "First parameter"
					}
				},
				"required": ["param1"]
			}`),
		},
	}

	bifrostTools, err := service.convertTools(tools)
	if err != nil {
		t.Fatalf("Failed to convert tools: %v", err)
	}

	if len(bifrostTools) != 1 {
		t.Errorf("Expected 1 tool, got %d", len(bifrostTools))
	}

	tool := bifrostTools[0]
	if tool.Type != "function" {
		t.Errorf("Expected type 'function', got %s", tool.Type)
	}

	if tool.Function.Name != "test_function" {
		t.Errorf("Expected name 'test_function', got %s", tool.Function.Name)
	}

	if tool.Function.Description != "A test function" {
		t.Errorf("Expected description 'A test function', got %s", tool.Function.Description)
	}
}

func TestConvertToolChoice(t *testing.T) {
	service := &Service{}

	testCases := []struct {
		input    *llm.ToolChoice
		expected interface{}
	}{
		{
			&llm.ToolChoice{Type: llm.ToolChoiceTypeAuto},
			"auto",
		},
		{
			&llm.ToolChoice{Type: llm.ToolChoiceTypeNone},
			"none",
		},
		{
			&llm.ToolChoice{Type: llm.ToolChoiceTypeAny},
			"required",
		},
		{
			&llm.ToolChoice{Type: llm.ToolChoiceTypeTool, Name: "specific_function"},
			map[string]interface{}{
				"type": "function",
				"function": map[string]string{
					"name": "specific_function",
				},
			},
		},
	}

	for i, tc := range testCases {
		result := service.convertToolChoice(tc.input)

		// For simple string cases
		if str, ok := tc.expected.(string); ok {
			if result.ToolChoiceStr == nil || *result.ToolChoiceStr != str {
				t.Errorf("Test case %d: expected %s, got %v", i, str, result)
			}
			continue
		}

		// For map cases, we'd need deeper comparison
		// For now, just check it's not nil
		if result.ToolChoiceStr == nil && result.ToolChoiceStruct == nil {
			t.Errorf("Test case %d: expected non-nil result", i)
		}
	}
}

func TestAccountInterface(t *testing.T) {
	account := &Account{
		providers: make(map[schemas.ModelProvider]*ProviderConfig),
	}

	// Test that it implements schemas.Account
	var _ schemas.Account = account

	// Test with no providers configured
	providers, err := account.GetConfiguredProviders()
	if err != nil {
		t.Errorf("GetConfiguredProviders failed: %v", err)
	}

	if len(providers) != 0 {
		t.Errorf("Expected 0 providers, got %d", len(providers))
	}

	// Test getting keys for non-existent provider
	ctx := context.Background()
	_, err = account.GetKeysForProvider(&ctx, schemas.OpenAI)
	if err == nil {
		t.Error("Expected error for non-configured provider")
	}
}

// TestIntegrationBasic tests basic integration if environment is set up
func TestIntegrationBasic(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}

	service, err := NewService(schemas.OpenAI, "gpt-3.5-turbo")
	if err != nil {
		t.Fatalf("Failed to create service: %v", err)
	}

	req := &llm.Request{
		Messages: []llm.Message{
			{
				Role: llm.MessageRoleUser,
				Content: []llm.Content{
					{
						Type: llm.ContentTypeText,
						Text: "Say 'Hello, Bifrost integration test!'",
					},
				},
			},
		},
	}

	ctx := context.Background()
	resp, err := service.Do(ctx, req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if len(resp.Content) == 0 {
		t.Error("Response has no content")
	}

	t.Logf("Response: %+v", resp)
}
