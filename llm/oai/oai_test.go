package oai

import (
	"testing"
)

func TestOpenAIURL_UsesCustomLiteLLMProxy(t *testing.T) {
	expectedURL := "https://lite.bastco.org/v1"
	
	if OpenAIURL != expectedURL {
		t.Errorf("Expected OpenAI URL to be %s, got %s", expectedURL, OpenAIURL)
	}
}

func TestOpenAIModels_UseCustomURL(t *testing.T) {
	expectedURL := "https://lite.bastco.org/v1"
	
	// Test all OpenAI models use the custom URL
	openaiModels := []Model{
		GPT41,
		GPT4o,
		GPT4oMini,
		GPT41Mini,
		GPT41Nano,
		O3,
		O4Mini,
	}
	
	for _, model := range openaiModels {
		if model.URL != expectedURL {
			t.Errorf("Model %s should use custom LiteLLM URL %s, got %s", 
				model.UserName, expectedURL, model.URL)
		}
	}
}

func TestNonOpenAIModels_UseOriginalURLs(t *testing.T) {
	// Test that non-OpenAI models still use their original URLs
	testCases := []struct {
		model       Model
		expectedURL string
	}{
		{Gemini25Flash, GeminiURL},
		{Gemini25Pro, GeminiURL},
		{TogetherDeepseekV3, TogetherURL},
		{FireworksDeepseekV3, FireworksURL},
		{MistralMedium, MistralURL},
		{MoonshotKimiK2, MoonshotURL},
		{LlamaCPP, LlamaCPPURL},
	}
	
	for _, tc := range testCases {
		if tc.model.URL != tc.expectedURL {
			t.Errorf("Model %s should use URL %s, got %s", 
				tc.model.UserName, tc.expectedURL, tc.model.URL)
		}
	}
}

func TestModelByUserName_FindsOpenAIModels(t *testing.T) {
	testCases := []string{
		"gpt4.1",
		"gpt4o",
		"gpt4o-mini",
		"gpt4.1-mini",
		"gpt4.1-nano",
		"o3",
		"o4-mini",
	}
	
	for _, userName := range testCases {
		model := ModelByUserName(userName)
		if model == nil {
			t.Errorf("Model %s should be found in registry", userName)
			continue
		}
		
		if model.URL != OpenAIURL {
			t.Errorf("Model %s should use custom LiteLLM URL %s, got %s", 
				userName, OpenAIURL, model.URL)
		}
	}
}

func TestListModels_IncludesOpenAIModels(t *testing.T) {
	models := ListModels()
	
	expectedOpenAIModels := []string{
		"gpt4.1",
		"gpt4o", 
		"gpt4o-mini",
		"gpt4.1-mini",
		"gpt4.1-nano",
		"o3",
		"o4-mini",
	}
	
	for _, expected := range expectedOpenAIModels {
		found := false
		for _, model := range models {
			if model == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected model %s not found in models list", expected)
		}
	}
}

func TestDefaultModel_UsesCustomURL(t *testing.T) {
	if DefaultModel.URL != OpenAIURL {
		t.Errorf("Default model should use custom LiteLLM URL %s, got %s", 
			OpenAIURL, DefaultModel.URL)
	}
}

func TestAPIKeyEnvironmentVariables(t *testing.T) {
	// Test that OpenAI models use the correct API key environment variable
	openaiModels := []Model{
		GPT41,
		GPT4o,
		GPT4oMini,
		GPT41Mini,
		GPT41Nano,
		O3,
		O4Mini,
	}
	
	for _, model := range openaiModels {
		if model.APIKeyEnv != OpenAIAPIKeyEnv {
			t.Errorf("Model %s should use API key env %s, got %s", 
				model.UserName, OpenAIAPIKeyEnv, model.APIKeyEnv)
		}
	}
}

func TestReasoningModels_Identification(t *testing.T) {
	// Test that reasoning models are correctly identified
	reasoningModels := []Model{O3, O4Mini}
	nonReasoningModels := []Model{GPT41, GPT4o, GPT4oMini, GPT41Mini, GPT41Nano}
	
	for _, model := range reasoningModels {
		if !model.IsReasoningModel {
			t.Errorf("Model %s should be identified as a reasoning model", model.UserName)
		}
	}
	
	for _, model := range nonReasoningModels {
		if model.IsReasoningModel {
			t.Errorf("Model %s should not be identified as a reasoning model", model.UserName)
		}
	}
}