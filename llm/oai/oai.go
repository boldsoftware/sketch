package oai

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
	"sketch.dev/llm"
)

const (
	DefaultMaxTokens = 8192

	OpenAIURL    = "https://api.openai.com/v1"
	FireworksURL = "https://api.fireworks.ai/inference/v1"
	CerebrasURL  = "https://api.cerebras.ai/v1"
	LlamaCPPURL  = "http://host.docker.internal:1234/v1"
	TogetherURL  = "https://api.together.xyz/v1"
	GeminiURL    = "https://generativelanguage.googleapis.com/v1beta/openai/"
	MistralURL   = "https://api.mistral.ai/v1"
	MoonshotURL  = "https://api.moonshot.ai/v1"

	// Environment variable names for API keys
	OpenAIAPIKeyEnv    = "OPENAI_API_KEY"
	FireworksAPIKeyEnv = "FIREWORKS_API_KEY"
	CerebrasAPIKeyEnv  = "CEREBRAS_API_KEY"
	TogetherAPIKeyEnv  = "TOGETHER_API_KEY"
	GeminiAPIKeyEnv    = "GEMINI_API_KEY"
	MistralAPIKeyEnv   = "MISTRAL_API_KEY"
	MoonshotAPIKeyEnv  = "MOONSHOT_API_KEY"
)

type Model struct {
	UserName           string // provided by the user to identify this model (e.g. "gpt4.1")
	ModelName          string // provided to the service provide to specify which model to use (e.g. "gpt-4.1-2025-04-14")
	URL                string
	APIKeyEnv          string // environment variable name for the API key
	IsReasoningModel   bool   // whether this model is a reasoning model (e.g. O3, O4-mini)
	UseSimplifiedPatch bool   // whether to use the simplified patch input schema; defaults to false
}

var (
	DefaultModel = GPT41

	GPT41 = Model{
		UserName:  "gpt4.1",
		ModelName: "gpt-4.1-2025-04-14",
		URL:       OpenAIURL,
		APIKeyEnv: OpenAIAPIKeyEnv,
	}

	GPT4o = Model{
		UserName:  "gpt4o",
		ModelName: "gpt-4o-2024-08-06",
		URL:       OpenAIURL,
		APIKeyEnv: OpenAIAPIKeyEnv,
	}

	GPT4oMini = Model{
		UserName:  "gpt4o-mini",
		ModelName: "gpt-4o-mini-2024-07-18",
		URL:       OpenAIURL,
		APIKeyEnv: OpenAIAPIKeyEnv,
	}

	GPT41Mini = Model{
		UserName:  "gpt4.1-mini",
		ModelName: "gpt-4.1-mini-2025-04-14",
		URL:       OpenAIURL,
		APIKeyEnv: OpenAIAPIKeyEnv,
	}

	GPT41Nano = Model{
		UserName:  "gpt4.1-nano",
		ModelName: "gpt-4.1-nano-2025-04-14",
		URL:       OpenAIURL,
		APIKeyEnv: OpenAIAPIKeyEnv,
	}

	O3 = Model{
		UserName:         "o3",
		ModelName:        "o3-2025-04-16",
		URL:              OpenAIURL,
		APIKeyEnv:        OpenAIAPIKeyEnv,
		IsReasoningModel: true,
	}

	O4Mini = Model{
		UserName:         "o4-mini",
		ModelName:        "o4-mini-2025-04-16",
		URL:              OpenAIURL,
		APIKeyEnv:        OpenAIAPIKeyEnv,
		IsReasoningModel: true,
	}

	Gemini25Flash = Model{
		UserName:  "gemini-flash-2.5",
		ModelName: "gemini-2.5-flash-preview-04-17",
		URL:       GeminiURL,
		APIKeyEnv: GeminiAPIKeyEnv,
	}

	Gemini25Pro = Model{
		UserName:  "gemini-pro-2.5",
		ModelName: "gemini-2.5-pro-preview-03-25",
		URL:       GeminiURL,
		// GRRRR. Really??
		// Input is: $1.25, prompts <= 200k tokens, $2.50, prompts > 200k tokens
		// Output is: $10.00, prompts <= 200k tokens, $15.00, prompts > 200k
		// Caching is: $0.31, prompts <= 200k tokens, $0.625, prompts > 200k, $4.50 / 1,000,000 tokens per hour
		// Whatever that means. Are we caching? I have no idea.
		// How do you always manage to be the annoying one, Google?
		// I'm not complicating things just for you.
		APIKeyEnv: GeminiAPIKeyEnv,
	}

	Gemini3Pro = Model{
		UserName:  "gemini-pro-3",
		ModelName: "gemini-3-pro-preview",
		URL:       GeminiURL,
		APIKeyEnv: GeminiAPIKeyEnv,
	}

	TogetherDeepseekV3 = Model{
		UserName:  "together-deepseek-v3",
		ModelName: "deepseek-ai/DeepSeek-V3",
		URL:       TogetherURL,
		APIKeyEnv: TogetherAPIKeyEnv,
	}

	TogetherDeepseekR1 = Model{
		UserName:  "together-deepseek-r1",
		ModelName: "deepseek-ai/DeepSeek-R1",
		URL:       TogetherURL,
		APIKeyEnv: TogetherAPIKeyEnv,
	}

	TogetherLlama4Maverick = Model{
		UserName:  "together-llama4-maverick",
		ModelName: "meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8",
		URL:       TogetherURL,
		APIKeyEnv: TogetherAPIKeyEnv,
	}

	FireworksLlama4Maverick = Model{
		UserName:  "fireworks-llama4-maverick",
		ModelName: "accounts/fireworks/models/llama4-maverick-instruct-basic",
		URL:       FireworksURL,
		APIKeyEnv: FireworksAPIKeyEnv,
	}

	TogetherLlama3_3_70B = Model{
		UserName:  "together-llama3-70b",
		ModelName: "meta-llama/Llama-3.3-70B-Instruct-Turbo",
		URL:       TogetherURL,
		APIKeyEnv: TogetherAPIKeyEnv,
	}

	TogetherMistralSmall = Model{
		UserName:  "together-mistral-small",
		ModelName: "mistralai/Mistral-Small-24B-Instruct-2501",
		URL:       TogetherURL,
		APIKeyEnv: TogetherAPIKeyEnv,
	}

	TogetherQwen3 = Model{
		UserName:  "together-qwen3",
		ModelName: "Qwen/Qwen3-235B-A22B-fp8-tput",
		URL:       TogetherURL,
		APIKeyEnv: TogetherAPIKeyEnv,
	}

	TogetherGemma2 = Model{
		UserName:  "together-gemma2",
		ModelName: "google/gemma-2-27b-it",
		URL:       TogetherURL,
		APIKeyEnv: TogetherAPIKeyEnv,
	}

	LlamaCPP = Model{
		UserName:  "llama.cpp",
		ModelName: "llama.cpp local model",
		URL:       LlamaCPPURL,
		APIKeyEnv: "NONE",
	}

	FireworksDeepseekV3 = Model{
		UserName:  "fireworks-deepseek-v3",
		ModelName: "accounts/fireworks/models/deepseek-v3-0324",
		URL:       FireworksURL,
		APIKeyEnv: FireworksAPIKeyEnv,
	}

	MoonshotKimiK2 = Model{
		UserName:  "moonshot-kimi-k2",
		ModelName: "moonshot-v1-auto",
		URL:       MoonshotURL,
		APIKeyEnv: MoonshotAPIKeyEnv,
	}

	MistralMedium = Model{
		UserName:  "mistral-medium-3",
		ModelName: "mistral-medium-latest",
		URL:       MistralURL,
		APIKeyEnv: MistralAPIKeyEnv,
	}

	DevstralSmall = Model{
		UserName:  "devstral-small",
		ModelName: "devstral-small-latest",
		URL:       MistralURL,
		APIKeyEnv: MistralAPIKeyEnv,
	}

	Qwen3CoderFireworks = Model{
		UserName:           "qwen3-coder-fireworks",
		ModelName:          "accounts/fireworks/models/qwen3-coder-480b-a35b-instruct",
		URL:                FireworksURL,
		APIKeyEnv:          FireworksAPIKeyEnv,
		UseSimplifiedPatch: true,
	}

	Qwen3CoderCerebras = Model{
		UserName:  "qwen3-coder-cerebras",
		ModelName: "qwen-3-coder-480b",
		URL:       CerebrasURL,
		APIKeyEnv: CerebrasAPIKeyEnv,
	}

	Qwen3Coder30Fireworks = Model{
		UserName:           "qwen3-coder-30-fireworks",
		ModelName:          "accounts/fireworks/models/qwen3-30b-a3b",
		URL:                FireworksURL,
		APIKeyEnv:          FireworksAPIKeyEnv,
		UseSimplifiedPatch: true,
	}

	ZaiGLM45CoderFireworks = Model{
		UserName:  "zai-glm45-fireworks",
		ModelName: "accounts/fireworks/models/glm-4p5",
		URL:       FireworksURL,
		APIKeyEnv: FireworksAPIKeyEnv,
	}

	GPTOSS20B = Model{
		UserName:  "gpt-oss-20b",
		ModelName: "accounts/fireworks/models/gpt-oss-20b",
		URL:       FireworksURL,
		APIKeyEnv: FireworksAPIKeyEnv,
	}

	GPTOSS120B = Model{
		UserName:  "gpt-oss-120b",
		ModelName: "accounts/fireworks/models/gpt-oss-120b",
		URL:       FireworksURL,
		APIKeyEnv: FireworksAPIKeyEnv,
	}

	GPT5 = Model{
		UserName:  "gpt5",
		ModelName: "gpt-5",
		URL:       OpenAIURL,
		APIKeyEnv: OpenAIAPIKeyEnv,
	}

	GPT5Mini = Model{
		UserName:  "gpt5mini",
		ModelName: "gpt-5-mini",
		URL:       OpenAIURL,
		APIKeyEnv: OpenAIAPIKeyEnv,
	}

	// Skaband-specific model names.
	// Provider details (URL and APIKeyEnv) are handled by skaband
	Qwen = Model{
		UserName:           "qwen",
		ModelName:          "qwen", // skaband will map this to the actual provider model
		UseSimplifiedPatch: true,
	}
	GLM = Model{
		UserName:  "glm",
		ModelName: "glm", // skaband will map this to the actual provider model
	}
)

// Service provides chat completions.
// Fields should not be altered concurrently with calling any method on Service.
type Service struct {
	HTTPC     *http.Client // defaults to http.DefaultClient if nil
	APIKey    string       // optional, if not set will try to load from env var
	Model     Model        // defaults to DefaultModel if zero value
	ModelURL  string       // optional, overrides Model.URL
	MaxTokens int          // defaults to DefaultMaxTokens if zero
	Org       string       // optional - organization ID
	DumpLLM   bool         // whether to dump request/response text to files for debugging; defaults to false
}

var _ llm.Service = (*Service)(nil)

// ModelsRegistry is a registry of all known models with their user-friendly names.
var ModelsRegistry = []Model{
	GPT41,
	GPT41Mini,
	GPT41Nano,
	GPT4o,
	GPT4oMini,
	GPT5,
	GPT5Mini,
	O3,
	O4Mini,
	Gemini25Flash,
	Gemini25Pro,
	Gemini3Pro,
	TogetherDeepseekV3,
	TogetherDeepseekR1,
	TogetherLlama4Maverick,
	TogetherLlama3_3_70B,
	TogetherMistralSmall,
	TogetherQwen3,
	TogetherGemma2,
	LlamaCPP,
	FireworksDeepseekV3,
	MoonshotKimiK2,
	FireworksLlama4Maverick,
	MistralMedium,
	DevstralSmall,
	Qwen3CoderFireworks,
	Qwen3Coder30Fireworks,
	Qwen3CoderCerebras,
	ZaiGLM45CoderFireworks,
	GPTOSS120B,
	GPTOSS20B,
	// Skaband-supported models
	Qwen,
	GLM,
}

// ListModels returns a list of all available models with their user-friendly names.
func ListModels() []string {
	var names []string
	for _, model := range ModelsRegistry {
		if model.UserName != "" {
			names = append(names, model.UserName)
		}
	}
	return names
}

// ModelByUserName returns a model by its user-friendly name.
// Returns nil if no model with the given name is found.
func ModelByUserName(name string) Model {
	for _, model := range ModelsRegistry {
		if model.UserName == name {
			return model
		}
	}
	return Model{}
}

func (m Model) IsZero() bool {
	return m == Model{}
}

var (
	fromLLMRole = map[llm.MessageRole]string{
		llm.MessageRoleAssistant: "assistant",
		llm.MessageRoleUser:      "user",
	}
	fromLLMToolChoiceType = map[llm.ToolChoiceType]string{
		llm.ToolChoiceTypeAuto: "auto",
		llm.ToolChoiceTypeAny:  "any",
		llm.ToolChoiceTypeNone: "none",
		llm.ToolChoiceTypeTool: "function", // OpenAI uses "function" instead of "tool"
	}
	toLLMRole = map[string]llm.MessageRole{
		"assistant": llm.MessageRoleAssistant,
		"user":      llm.MessageRoleUser,
	}
	toLLMStopReason = map[string]llm.StopReason{
		"stop":           llm.StopReasonStopSequence,
		"length":         llm.StopReasonMaxTokens,
		"tool_calls":     llm.StopReasonToolUse,
		"function_call":  llm.StopReasonToolUse,      // Map both to ToolUse
		"content_filter": llm.StopReasonStopSequence, // No direct equivalent
	}
)

// fromLLMContent converts llm.Content to the format expected by OpenAI.
func fromLLMContent(c llm.Content) (string, []openai.ToolCall) {
	switch c.Type {
	case llm.ContentTypeText:
		return c.Text, nil
	case llm.ContentTypeToolUse:
		// For OpenAI, tool use is sent as a null content with tool_calls in the message
		return "", []openai.ToolCall{
			{
				Type: openai.ToolTypeFunction,
				ID:   c.ID, // Use the content ID if provided
				Function: openai.FunctionCall{
					Name:      c.ToolName,
					Arguments: string(c.ToolInput),
				},
			},
		}
	case llm.ContentTypeToolResult:
		// Tool results in OpenAI are sent as a separate message with tool_call_id
		// OpenAI doesn't support multiple content items or images in tool results
		// Combine all text content into a single string
		var resultText string
		if len(c.ToolResult) > 0 {
			// Collect all text from content objects
			texts := make([]string, 0, len(c.ToolResult))
			for _, result := range c.ToolResult {
				if result.Text != "" {
					texts = append(texts, result.Text)
				}
			}
			resultText = strings.Join(texts, "\n")
		}
		return resultText, nil
	default:
		// For thinking or other types, convert to text
		return c.Text, nil
	}
}

// fromLLMMessage converts llm.Message to OpenAI ChatCompletionMessage format
func fromLLMMessage(msg llm.Message) []openai.ChatCompletionMessage {
	// For OpenAI, we need to handle tool results differently than regular messages
	// Each tool result becomes its own message with role="tool"

	var messages []openai.ChatCompletionMessage

	// Check if this is a regular message or contains tool results
	var regularContent []llm.Content
	var toolResults []llm.Content

	for _, c := range msg.Content {
		if c.Type == llm.ContentTypeToolResult {
			toolResults = append(toolResults, c)
		} else {
			regularContent = append(regularContent, c)
		}
	}

	// Process tool results as separate messages, but first
	for _, tr := range toolResults {
		// Convert toolresult array to a string for OpenAI
		// Collect all text from content objects
		var texts []string
		for _, result := range tr.ToolResult {
			if strings.TrimSpace(result.Text) != "" {
				texts = append(texts, result.Text)
			}
		}
		toolResultContent := strings.Join(texts, "\n")

		// OpenAI doesn't have an explicit error field for tool results, so add it directly to the content.
		if tr.ToolError {
			if toolResultContent != "" {
				toolResultContent = "error: " + toolResultContent
			} else {
				toolResultContent = "error: tool execution failed"
			}
		}

		m := openai.ChatCompletionMessage{
			Role:       "tool",
			Content:    cmp.Or(toolResultContent, " "), // Use empty space if empty to avoid omitempty issues
			ToolCallID: tr.ToolUseID,
		}
		messages = append(messages, m)
	}
	// Process regular content second
	if len(regularContent) > 0 {
		m := openai.ChatCompletionMessage{
			Role: fromLLMRole[msg.Role],
		}

		// For assistant messages that contain tool calls
		var toolCalls []openai.ToolCall
		var textContent string

		for _, c := range regularContent {
			content, tools := fromLLMContent(c)
			if len(tools) > 0 {
				toolCalls = append(toolCalls, tools...)
			} else if content != "" {
				if textContent != "" {
					textContent += "\n"
				}
				textContent += content
			}
		}

		m.Content = textContent
		m.ToolCalls = toolCalls

		messages = append(messages, m)
	}

	return messages
}

// requiresMaxCompletionTokens returns true if the model requires max_completion_tokens instead of max_tokens.
func (m Model) requiresMaxCompletionTokens() bool {
	// Reasoning models always use max_completion_tokens
	if m.IsReasoningModel {
		return true
	}

	// GPT-5 series models also require max_completion_tokens
	switch m.ModelName {
	case "gpt-5", "gpt-5-mini":
		return true
	default:
		return false
	}
}

// fromLLMToolChoice converts llm.ToolChoice to the format expected by OpenAI.
func fromLLMToolChoice(tc *llm.ToolChoice) any {
	if tc == nil {
		return nil
	}

	if tc.Type == llm.ToolChoiceTypeTool && tc.Name != "" {
		return openai.ToolChoice{
			Type: openai.ToolTypeFunction,
			Function: openai.ToolFunction{
				Name: tc.Name,
			},
		}
	}

	// For non-specific tool choice, just use the string
	return fromLLMToolChoiceType[tc.Type]
}

// fromLLMTool converts llm.Tool to the format expected by OpenAI.
func fromLLMTool(t *llm.Tool) openai.Tool {
	return openai.Tool{
		Type: openai.ToolTypeFunction,
		Function: &openai.FunctionDefinition{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.InputSchema,
		},
	}
}

// fromLLMSystem converts llm.SystemContent to an OpenAI system message.
func fromLLMSystem(systemContent []llm.SystemContent) []openai.ChatCompletionMessage {
	if len(systemContent) == 0 {
		return nil
	}

	// Combine all system content into a single system message
	var systemText string
	for i, content := range systemContent {
		if i > 0 && systemText != "" && content.Text != "" {
			systemText += "\n"
		}
		systemText += content.Text
	}

	if systemText == "" {
		return nil
	}

	return []openai.ChatCompletionMessage{
		{
			Role:    "system",
			Content: systemText,
		},
	}
}

// toRawLLMContent converts a raw content string from OpenAI to llm.Content.
func toRawLLMContent(content string) llm.Content {
	return llm.Content{
		Type: llm.ContentTypeText,
		Text: content,
	}
}

// toToolCallLLMContent converts a tool call from OpenAI to llm.Content.
func toToolCallLLMContent(toolCall openai.ToolCall) llm.Content {
	// Generate a content ID if needed
	id := toolCall.ID
	if id == "" {
		// Create a deterministic ID based on the function name if no ID is provided
		id = "tc_" + toolCall.Function.Name
	}

	return llm.Content{
		ID:        id,
		Type:      llm.ContentTypeToolUse,
		ToolName:  toolCall.Function.Name,
		ToolInput: json.RawMessage(toolCall.Function.Arguments),
	}
}

// toToolResultLLMContent converts a tool result message from OpenAI to llm.Content.
func toToolResultLLMContent(msg openai.ChatCompletionMessage) llm.Content {
	return llm.Content{
		Type:      llm.ContentTypeToolResult,
		ToolUseID: msg.ToolCallID,
		ToolResult: []llm.Content{{
			Type: llm.ContentTypeText,
			Text: msg.Content,
		}},
		ToolError: false, // OpenAI doesn't specify errors explicitly; error information is parsed from content
	}
}

// toLLMContents converts message content from OpenAI to []llm.Content.
func toLLMContents(msg openai.ChatCompletionMessage) []llm.Content {
	var contents []llm.Content

	// If this is a tool response, handle it separately
	if msg.Role == "tool" && msg.ToolCallID != "" {
		return []llm.Content{toToolResultLLMContent(msg)}
	}

	// If there's text content, add it
	if msg.Content != "" {
		contents = append(contents, toRawLLMContent(msg.Content))
	}

	// If there are tool calls, add them
	for _, tc := range msg.ToolCalls {
		contents = append(contents, toToolCallLLMContent(tc))
	}

	// If empty, add an empty text content
	if len(contents) == 0 {
		contents = append(contents, llm.Content{
			Type: llm.ContentTypeText,
			Text: "",
		})
	}

	return contents
}

// toLLMUsage converts usage information from OpenAI to llm.Usage.
func (s *Service) toLLMUsage(au openai.Usage, headers http.Header) llm.Usage {
	// fmt.Printf("raw usage: %+v / %v / %v\n", au, au.PromptTokensDetails, au.CompletionTokensDetails)
	in := uint64(au.PromptTokens)
	var inc uint64
	if au.PromptTokensDetails != nil {
		inc = uint64(au.PromptTokensDetails.CachedTokens)
	}
	out := uint64(au.CompletionTokens)
	u := llm.Usage{
		InputTokens:              in,
		CacheReadInputTokens:     inc,
		CacheCreationInputTokens: in,
		OutputTokens:             out,
	}
	u.CostUSD = llm.CostUSDFromResponse(headers)
	return u
}

// toLLMResponse converts the OpenAI response to llm.Response.
func (s *Service) toLLMResponse(r *openai.ChatCompletionResponse) *llm.Response {
	// fmt.Printf("Raw response\n")
	// enc := json.NewEncoder(os.Stdout)
	// enc.SetIndent("", "  ")
	// enc.Encode(r)
	// fmt.Printf("\n")

	if len(r.Choices) == 0 {
		return &llm.Response{
			ID:    r.ID,
			Model: r.Model,
			Role:  llm.MessageRoleAssistant,
			Usage: s.toLLMUsage(r.Usage, r.Header()),
		}
	}

	// Process the primary choice
	choice := r.Choices[0]

	return &llm.Response{
		ID:         r.ID,
		Model:      r.Model,
		Role:       toRoleFromString(choice.Message.Role),
		Content:    toLLMContents(choice.Message),
		StopReason: toStopReason(string(choice.FinishReason)),
		Usage:      s.toLLMUsage(r.Usage, r.Header()),
	}
}

// toRoleFromString converts a role string to llm.MessageRole.
func toRoleFromString(role string) llm.MessageRole {
	if role == "tool" || role == "system" || role == "function" {
		return llm.MessageRoleAssistant // Map special roles to assistant for consistency
	}
	if mr, ok := toLLMRole[role]; ok {
		return mr
	}
	return llm.MessageRoleUser // Default to user if unknown
}

// toStopReason converts a finish reason string to llm.StopReason.
func toStopReason(reason string) llm.StopReason {
	if sr, ok := toLLMStopReason[reason]; ok {
		return sr
	}
	return llm.StopReasonStopSequence // Default
}

// TokenContextWindow returns the maximum token context window size for this service
func (s *Service) TokenContextWindow() int {
	// TODO: move TokenContextWindow information to Model struct

	model := cmp.Or(s.Model, DefaultModel)

	// OpenAI models generally have 128k context windows
	// Some newer models have larger windows, but 128k is a safe default
	switch model.ModelName {
	case "gpt-4.1-2025-04-14", "gpt-4.1-mini-2025-04-14", "gpt-4.1-nano-2025-04-14":
		return 200000 // 200k for newer GPT-4.1 models
	case "gpt-4o-2024-08-06", "gpt-4o-mini-2024-07-18":
		return 128000 // 128k for GPT-4o models
	case "o3-2025-04-16", "o3-mini-2025-04-16":
		return 200000 // 200k for O3 models
	case "accounts/fireworks/models/qwen3-coder-480b-a35b-instruct":
		return 256000 // 256k native context for Qwen3-Coder
	case "glm", "zai-glm45-fireworks":
		return 128000
	case "qwen", "qwen3-coder-cerebras", "qwen3-coder-fireworks":
		return 256000 // 256k native context for Qwen3-Coder
	case "gpt-oss-20b", "gpt-oss-120b":
		return 128000
	case "gpt-5", "gpt-5-mini":
		return 256000
	default:
		// Default for unknown models
		return 128000
	}
}

// Do sends a request to OpenAI using the go-openai package.
func (s *Service) Do(ctx context.Context, ir *llm.Request) (*llm.Response, error) {
	// Configure the OpenAI client
	httpc := cmp.Or(s.HTTPC, http.DefaultClient)
	model := cmp.Or(s.Model, DefaultModel)

	// TODO: do this one during Service setup? maybe with a constructor instead?
	config := openai.DefaultConfig(s.APIKey)
	if modelURLOverride := cmp.Or(s.ModelURL, model.URL); modelURLOverride != "" {
		config.BaseURL = modelURLOverride
	}
	if s.Org != "" {
		config.OrgID = s.Org
	}
	config.HTTPClient = httpc

	client := openai.NewClientWithConfig(config)

	// Start with system messages if provided
	var allMessages []openai.ChatCompletionMessage
	if len(ir.System) > 0 {
		sysMessages := fromLLMSystem(ir.System)
		allMessages = append(allMessages, sysMessages...)
	}

	// Add regular and tool messages
	for _, msg := range ir.Messages {
		msgs := fromLLMMessage(msg)
		allMessages = append(allMessages, msgs...)
	}

	// Convert tools
	var tools []openai.Tool
	for _, t := range ir.Tools {
		tools = append(tools, fromLLMTool(t))
	}

	// Create the OpenAI request
	req := openai.ChatCompletionRequest{
		Model:      model.ModelName,
		Messages:   allMessages,
		Tools:      tools,
		ToolChoice: fromLLMToolChoice(ir.ToolChoice), // TODO: make fromLLMToolChoice return an error when a perfect translation is not possible
	}
	if model.requiresMaxCompletionTokens() {
		req.MaxCompletionTokens = cmp.Or(s.MaxTokens, DefaultMaxTokens)
	} else {
		req.MaxTokens = cmp.Or(s.MaxTokens, DefaultMaxTokens)
	}
	// Dump request if enabled
	if s.DumpLLM {
		if reqJSON, err := json.MarshalIndent(req, "", "  "); err == nil {
			// Construct the chat completions URL
			baseURL := cmp.Or(model.URL, OpenAIURL)
			url := baseURL + "/chat/completions"
			if err := llm.DumpToFile("request", url, reqJSON); err != nil {
				slog.WarnContext(ctx, "failed to dump openai request to file", "error", err)
			}
		}
	}

	// Retry mechanism
	backoff := []time.Duration{1 * time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second}

	// retry loop
	var errs error // accumulated errors across all attempts
	for attempts := 0; ; attempts++ {
		if attempts > 10 {
			return nil, fmt.Errorf("openai request failed after %d attempts: %w", attempts, errs)
		}
		if attempts > 0 {
			sleep := backoff[min(attempts, len(backoff)-1)] + time.Duration(rand.Int64N(int64(time.Second)))
			slog.WarnContext(ctx, "openai request sleep before retry", "sleep", sleep, "attempts", attempts)
			time.Sleep(sleep)
		}

		resp, err := client.CreateChatCompletion(ctx, req)

		// Handle successful response
		if err == nil {
			// Dump response if enabled
			if s.DumpLLM {
				if respJSON, jsonErr := json.MarshalIndent(resp, "", "  "); jsonErr == nil {
					if dumpErr := llm.DumpToFile("response", "", respJSON); dumpErr != nil {
						slog.WarnContext(ctx, "failed to dump openai response to file", "error", dumpErr)
					}
				}
			}
			return s.toLLMResponse(&resp), nil
		}

		// Handle errors
		// Check for TLS "bad record MAC" errors and retry once
		if strings.Contains(err.Error(), "tls: bad record MAC") && attempts == 0 {
			slog.WarnContext(ctx, "tls bad record MAC error, retrying once", "error", err.Error())
			errs = errors.Join(errs, fmt.Errorf("TLS error (attempt %d): %w", attempts+1, err))
			continue
		}

		var apiErr *openai.APIError
		if ok := errors.As(err, &apiErr); !ok {
			// Not an OpenAI API error, return immediately with accumulated errors
			return nil, errors.Join(errs, err)
		}

		switch {
		case apiErr.HTTPStatusCode >= 500:
			// Server error, try again with backoff
			slog.WarnContext(ctx, "openai_request_failed", "error", apiErr.Error(), "status_code", apiErr.HTTPStatusCode)
			errs = errors.Join(errs, fmt.Errorf("status %d: %s", apiErr.HTTPStatusCode, apiErr.Error()))
			continue

		case apiErr.HTTPStatusCode == 429:
			// Rate limited, accumulate error and retry
			slog.WarnContext(ctx, "openai_request_rate_limited", "error", apiErr.Error())
			errs = errors.Join(errs, fmt.Errorf("status %d (rate limited): %s", apiErr.HTTPStatusCode, apiErr.Error()))
			continue

		case apiErr.HTTPStatusCode >= 400 && apiErr.HTTPStatusCode < 500:
			// Client error, probably unrecoverable
			slog.WarnContext(ctx, "openai_request_failed", "error", apiErr.Error(), "status_code", apiErr.HTTPStatusCode)
			return nil, errors.Join(errs, fmt.Errorf("status %d: %s", apiErr.HTTPStatusCode, apiErr.Error()))

		default:
			// Other error, accumulate and retry
			slog.WarnContext(ctx, "openai_request_failed", "error", apiErr.Error(), "status_code", apiErr.HTTPStatusCode)
			errs = errors.Join(errs, fmt.Errorf("status %d: %s", apiErr.HTTPStatusCode, apiErr.Error()))
			continue
		}
	}
}

func (s *Service) UseSimplifiedPatch() bool {
	return s.Model.UseSimplifiedPatch
}
