package gem

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"sketch.dev/llm"
	"sketch.dev/llm/gem/gemini"
)

const (
	DefaultModel    = "gemini-3-pro-preview"
	GeminiAPIKeyEnv = "GEMINI_API_KEY"
)

// Service provides Gemini completions.
// Fields should not be altered concurrently with calling any method on Service.
type Service struct {
	HTTPC   *http.Client // defaults to http.DefaultClient if nil
	URL     string       // Gemini API URL, uses the gemini package default if empty
	APIKey  string       // must be non-empty
	Model   string       // defaults to DefaultModel if empty
	DumpLLM bool         // whether to dump request/response text to files for debugging; defaults to false
}

var _ llm.Service = (*Service)(nil)

// These maps convert between Sketch's llm package and Gemini API formats
var fromLLMRole = map[llm.MessageRole]string{
	llm.MessageRoleAssistant: "model",
	llm.MessageRoleUser:      "user",
}

// convertToolSchemas converts Sketch's llm.Tool schemas to Gemini's schema format
func convertToolSchemas(tools []*llm.Tool) ([]gemini.FunctionDeclaration, error) {
	if len(tools) == 0 {
		return nil, nil
	}

	var decls []gemini.FunctionDeclaration
	for _, tool := range tools {
		// Parse the schema from raw JSON
		var schemaJSON map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schemaJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal tool %s schema: %w", tool.Name, err)
		}
		decls = append(decls, gemini.FunctionDeclaration{
			Name:        tool.Name,
			Description: tool.Description,
			Parameters:  convertJSONSchemaToGeminiSchema(schemaJSON),
		})
	}

	return decls, nil
}

// convertJSONSchemaToGeminiSchema converts a JSON schema to Gemini's schema format
func convertJSONSchemaToGeminiSchema(schemaJSON map[string]any) gemini.Schema {
	schema := gemini.Schema{}

	// Set the type based on the JSON schema type
	if typeVal, ok := schemaJSON["type"].(string); ok {
		switch typeVal {
		case "string":
			schema.Type = gemini.DataTypeSTRING
		case "number":
			schema.Type = gemini.DataTypeNUMBER
		case "integer":
			schema.Type = gemini.DataTypeINTEGER
		case "boolean":
			schema.Type = gemini.DataTypeBOOLEAN
		case "array":
			schema.Type = gemini.DataTypeARRAY
		case "object":
			schema.Type = gemini.DataTypeOBJECT
		default:
			schema.Type = gemini.DataTypeSTRING // Default to string for unknown types
		}
	}

	// Set description if available
	if desc, ok := schemaJSON["description"].(string); ok {
		schema.Description = desc
	}

	// Handle enum values
	if enumValues, ok := schemaJSON["enum"].([]any); ok {
		schema.Enum = make([]string, len(enumValues))
		for i, v := range enumValues {
			if strVal, ok := v.(string); ok {
				schema.Enum[i] = strVal
			} else {
				// Convert non-string values to string
				valBytes, _ := json.Marshal(v)
				schema.Enum[i] = string(valBytes)
			}
		}
	}

	// Handle object properties
	if properties, ok := schemaJSON["properties"].(map[string]any); ok && schema.Type == gemini.DataTypeOBJECT {
		schema.Properties = make(map[string]gemini.Schema)
		for propName, propSchema := range properties {
			if propSchemaMap, ok := propSchema.(map[string]any); ok {
				schema.Properties[propName] = convertJSONSchemaToGeminiSchema(propSchemaMap)
			}
		}
	}

	// Handle required properties
	if required, ok := schemaJSON["required"].([]any); ok {
		schema.Required = make([]string, len(required))
		for i, r := range required {
			if strVal, ok := r.(string); ok {
				schema.Required[i] = strVal
			}
		}
	}

	// Handle array items
	if items, ok := schemaJSON["items"].(map[string]any); ok && schema.Type == gemini.DataTypeARRAY {
		itemSchema := convertJSONSchemaToGeminiSchema(items)
		schema.Items = &itemSchema
	}

	// Handle minimum/maximum items for arrays
	if minItems, ok := schemaJSON["minItems"].(float64); ok {
		schema.MinItems = fmt.Sprintf("%d", int(minItems))
	}
	if maxItems, ok := schemaJSON["maxItems"].(float64); ok {
		schema.MaxItems = fmt.Sprintf("%d", int(maxItems))
	}

	return schema
}

// buildGeminiRequest converts Sketch's llm.Request to Gemini's request format
func (s *Service) buildGeminiRequest(req *llm.Request) (*gemini.Request, error) {
	gemReq := &gemini.Request{}

	// Add system instruction if provided
	if len(req.System) > 0 {
		// Combine all system messages into a single system instruction
		systemText := ""
		for i, sys := range req.System {
			if i > 0 && systemText != "" && sys.Text != "" {
				systemText += "\n"
			}
			systemText += sys.Text
		}

		if systemText != "" {
			gemReq.SystemInstruction = &gemini.Content{
				Parts: []gemini.Part{{Text: systemText}},
			}
		}
	}

	// Convert messages to Gemini content format
	for _, msg := range req.Messages {
		// Set the role based on the message role
		role, ok := fromLLMRole[msg.Role]
		if !ok {
			return nil, fmt.Errorf("unsupported message role: %v", msg.Role)
		}

		content := gemini.Content{
			Role: role,
		}

		// Store tool usage information to correlate tool uses with responses
		toolNameToID := make(map[string]string)

		// First pass: collect tool use IDs for correlation
		for _, c := range msg.Content {
			if c.Type == llm.ContentTypeToolUse && c.ID != "" {
				toolNameToID[c.ToolName] = c.ID
			}
		}

		// Map each content item to Gemini's format
		for _, c := range msg.Content {
			switch c.Type {
			case llm.ContentTypeText, llm.ContentTypeThinking, llm.ContentTypeRedactedThinking:
				// Simple text content
				content.Parts = append(content.Parts, gemini.Part{
					Text: c.Text,
				})
			case llm.ContentTypeToolUse:
				// Tool use becomes a function call
				var args map[string]any
				if err := json.Unmarshal(c.ToolInput, &args); err != nil {
					return nil, fmt.Errorf("failed to unmarshal tool input: %w", err)
				}

				// Make sure we have a valid ID for this tool use
				if c.ID == "" {
					c.ID = fmt.Sprintf("gemini_tool_%s_%d", c.ToolName, time.Now().UnixNano())
				}

				// Save the ID for this tool name for future correlation
				toolNameToID[c.ToolName] = c.ID

				slog.DebugContext(context.Background(), "gemini_preparing_tool_use",
					"tool_name", c.ToolName,
					"tool_id", c.ID,
					"input", string(c.ToolInput))

				content.Parts = append(content.Parts, gemini.Part{
					FunctionCall: &gemini.FunctionCall{
						Name: c.ToolName,
						Args: args,
					},
					ThoughtSignature: c.Signature,
				})
			case llm.ContentTypeToolResult:
				// Tool result becomes a function response
				// Create a map for the response
				response := map[string]any{
					"error": c.ToolError,
				}

				// Handle tool results: Gemini only supports string results
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
				response["result"] = resultText

				// Determine the function name to use - this is critical
				funcName := ""

				// First try to find the function name from a stored toolUseID if we have one
				if c.ToolUseID != "" {
					// Try to derive the tool name from the previous tools we've seen
					for name, id := range toolNameToID {
						if id == c.ToolUseID {
							funcName = name
							break
						}
					}
				}

				// Fallback options if we couldn't find the tool name
				if funcName == "" {
					// Try the tool name directly
					if c.ToolName != "" {
						funcName = c.ToolName
					} else {
						// Last resort fallback
						funcName = "default_tool"
					}
				}

				slog.DebugContext(context.Background(), "gemini_preparing_tool_result",
					"tool_use_id", c.ToolUseID,
					"mapped_func_name", funcName,
					"result_count", len(c.ToolResult))

				content.Parts = append(content.Parts, gemini.Part{
					FunctionResponse: &gemini.FunctionResponse{
						Name:     funcName,
						Response: response,
					},
				})
			}
		}

		gemReq.Contents = append(gemReq.Contents, content)
	}

	// Handle tools/functions
	if len(req.Tools) > 0 {
		// Convert tool schemas
		decls, err := convertToolSchemas(req.Tools)
		if err != nil {
			return nil, fmt.Errorf("failed to convert tool schemas: %w", err)
		}
		if len(decls) > 0 {
			gemReq.Tools = []gemini.Tool{{FunctionDeclarations: decls}}
		}
	}

	return gemReq, nil
}

// convertGeminiResponsesToContent converts a Gemini response to llm.Content
func convertGeminiResponseToContent(res *gemini.Response) []llm.Content {
	if res == nil || len(res.Candidates) == 0 || len(res.Candidates[0].Content.Parts) == 0 {
		return []llm.Content{{
			Type: llm.ContentTypeText,
			Text: "",
		}}
	}

	var contents []llm.Content

	// Process each part in the first candidate's content
	for i, part := range res.Candidates[0].Content.Parts {
		// Log the part type for debugging
		slog.DebugContext(context.Background(), "processing_gemini_part",
			"index", i,
			"has_text", part.Text != "",
			"has_function_call", part.FunctionCall != nil,
			"has_function_response", part.FunctionResponse != nil)

		if part.Text != "" {
			// Simple text response
			contents = append(contents, llm.Content{
				Type: llm.ContentTypeText,
				Text: part.Text,
			})
		} else if part.FunctionCall != nil {
			// Function call (tool use)
			args, err := json.Marshal(part.FunctionCall.Args)
			if err != nil {
				// If we can't marshal, use empty args
				slog.DebugContext(context.Background(), "gemini_failed_to_markshal_args",
					"tool_name", part.FunctionCall.Name,
					"args", string(args),
					"err", err.Error(),
				)
				args = []byte("{}")
			}

			// Generate a unique ID for this tool use that includes the function name
			// to make it easier to correlate with responses
			toolID := fmt.Sprintf("gemini_tool_%s_%d", part.FunctionCall.Name, time.Now().UnixNano())

			contents = append(contents, llm.Content{
				ID:        toolID,
				Type:      llm.ContentTypeToolUse,
				ToolName:  part.FunctionCall.Name,
				ToolInput: json.RawMessage(args),
				Signature: part.ThoughtSignature,
			})

			slog.DebugContext(context.Background(), "gemini_tool_call",
				"tool_id", toolID,
				"tool_name", part.FunctionCall.Name,
				"args", string(args))
		} else if part.FunctionResponse != nil {
			// We shouldn't normally get function responses from the model, but just in case
			respData, _ := json.Marshal(part.FunctionResponse.Response)
			slog.DebugContext(context.Background(), "unexpected_function_response",
				"name", part.FunctionResponse.Name,
				"response", string(respData))
		}
	}

	// If no content was added, add an empty text content
	if len(contents) == 0 {
		slog.DebugContext(context.Background(), "empty_gemini_response", "adding_empty_text", true)
		contents = append(contents, llm.Content{
			Type: llm.ContentTypeText,
			Text: "",
		})
	}

	return contents
}

// Gemini doesn't provide usage info directly, so we need to estimate it
// ensureToolIDs makes sure all tool uses have proper IDs
func ensureToolIDs(contents []llm.Content) {
	for i, content := range contents {
		if content.Type == llm.ContentTypeToolUse && content.ID == "" {
			// Generate a stable ID using the tool name and timestamp
			contents[i].ID = fmt.Sprintf("gemini_tool_%s_%d", content.ToolName, time.Now().UnixNano())
			slog.DebugContext(context.Background(), "assigned_missing_tool_id",
				"tool_name", content.ToolName,
				"new_id", contents[i].ID)
		}
	}
}

func calculateUsage(req *gemini.Request, res *gemini.Response) llm.Usage {
	// Very rough estimation of token counts
	var inputTokens uint64
	var outputTokens uint64

	// Count system tokens
	if req.SystemInstruction != nil {
		for _, part := range req.SystemInstruction.Parts {
			if part.Text != "" {
				// Very rough estimation: 1 token per 4 characters
				inputTokens += uint64(len(part.Text)) / 4
			}
		}
	}

	// Count input tokens
	for _, content := range req.Contents {
		for _, part := range content.Parts {
			if part.Text != "" {
				inputTokens += uint64(len(part.Text)) / 4
			} else if part.FunctionCall != nil {
				// Estimate function call tokens
				argBytes, _ := json.Marshal(part.FunctionCall.Args)
				inputTokens += uint64(len(part.FunctionCall.Name)+len(argBytes)) / 4
			} else if part.FunctionResponse != nil {
				// Estimate function response tokens
				resBytes, _ := json.Marshal(part.FunctionResponse.Response)
				inputTokens += uint64(len(part.FunctionResponse.Name)+len(resBytes)) / 4
			}
		}
	}

	// Count output tokens
	if res != nil && len(res.Candidates) > 0 {
		for _, part := range res.Candidates[0].Content.Parts {
			if part.Text != "" {
				outputTokens += uint64(len(part.Text)) / 4
			} else if part.FunctionCall != nil {
				// Estimate function call tokens
				argBytes, _ := json.Marshal(part.FunctionCall.Args)
				outputTokens += uint64(len(part.FunctionCall.Name)+len(argBytes)) / 4
			}
		}
	}

	return llm.Usage{
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}
}

// TokenContextWindow returns the maximum token context window size for this service
func (s *Service) TokenContextWindow() int {
	model := s.Model
	if model == "" {
		model = DefaultModel
	}

	// Gemini models generally have large context windows
	switch model {
	case "gemini-3-pro-preview":
		return 1000000 // 1M tokens for Gemini 3 Pro
	case "gemini-2.5-pro-preview-03-25":
		return 1000000 // 1M tokens for Gemini 2.5 Pro
	case "gemini-2.0-flash-exp":
		return 1000000 // 1M tokens for Gemini 2.0 Flash
	case "gemini-1.5-pro", "gemini-1.5-pro-latest":
		return 2000000 // 2M tokens for Gemini 1.5 Pro
	case "gemini-1.5-flash", "gemini-1.5-flash-latest":
		return 1000000 // 1M tokens for Gemini 1.5 Flash
	default:
		// Default for unknown models
		return 1000000
	}
}

// Do sends a request to Gemini.
func (s *Service) Do(ctx context.Context, ir *llm.Request) (*llm.Response, error) {
	// Log the incoming request for debugging
	slog.DebugContext(ctx, "gemini_request",
		"message_count", len(ir.Messages),
		"tool_count", len(ir.Tools),
		"system_count", len(ir.System))

	// Log tool-related information if any tools are present
	if len(ir.Tools) > 0 {
		var toolNames []string
		for _, tool := range ir.Tools {
			toolNames = append(toolNames, tool.Name)
		}
		slog.DebugContext(ctx, "gemini_tools", "tools", toolNames)
	}

	// Log details about the messages being sent
	for i, msg := range ir.Messages {
		contentTypes := make([]string, len(msg.Content))
		for j, c := range msg.Content {
			contentTypes[j] = c.Type.String()

			// Log tool-related content with more details
			if c.Type == llm.ContentTypeToolUse {
				slog.DebugContext(ctx, "gemini_tool_use",
					"message_idx", i,
					"content_idx", j,
					"tool_name", c.ToolName,
					"tool_input", string(c.ToolInput))
			} else if c.Type == llm.ContentTypeToolResult {
				slog.DebugContext(ctx, "gemini_tool_result",
					"message_idx", i,
					"content_idx", j,
					"tool_use_id", c.ToolUseID,
					"tool_error", c.ToolError,
					"result_count", len(c.ToolResult))
			}
		}
		slog.DebugContext(ctx, "gemini_message",
			"idx", i,
			"role", msg.Role.String(),
			"content_types", contentTypes)
	}
	// Build the Gemini request
	gemReq, err := s.buildGeminiRequest(ir)
	if err != nil {
		return nil, fmt.Errorf("failed to build Gemini request: %w", err)
	}

	// Log the structured Gemini request for debugging
	if reqJSON, err := json.MarshalIndent(gemReq, "", "  "); err == nil {
		slog.DebugContext(ctx, "gemini_request_json", "request", string(reqJSON))
		if s.DumpLLM {
			// Construct the same URL that the Gemini client will use
			endpoint := cmp.Or(s.URL, "https://generativelanguage.googleapis.com/v1beta")
			url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", endpoint, cmp.Or(s.Model, DefaultModel), s.APIKey)
			if err := llm.DumpToFile("request", url, reqJSON); err != nil {
				slog.WarnContext(ctx, "failed to dump gemini request to file", "error", err)
			}
		}
	}

	// Create a Gemini model instance
	model := gemini.Model{
		Model:    "models/" + cmp.Or(s.Model, DefaultModel),
		Endpoint: s.URL,
		APIKey:   s.APIKey,
		HTTPC:    cmp.Or(s.HTTPC, http.DefaultClient),
	}

	// Send the request to Gemini with retry logic
	startTime := time.Now()
	endTime := startTime // Initialize endTime
	var gemRes *gemini.Response

	// Retry mechanism for handling server errors and rate limiting
	backoff := []time.Duration{1 * time.Second, 3 * time.Second, 5 * time.Second, 10 * time.Second}
	for attempts := 0; attempts <= len(backoff); attempts++ {
		gemApiErr := error(nil)
		gemRes, gemApiErr = model.GenerateContent(ctx, gemReq)
		endTime = time.Now()

		if gemApiErr == nil {
			// Successful response
			// Log the structured Gemini response
			if resJSON, err := json.MarshalIndent(gemRes, "", "  "); err == nil {
				slog.DebugContext(ctx, "gemini_response_json", "response", string(resJSON))
				if s.DumpLLM {
					if err := llm.DumpToFile("response", "", resJSON); err != nil {
						slog.WarnContext(ctx, "failed to dump gemini response to file", "error", err)
					}
				}
			}
			break
		}

		if attempts == len(backoff) {
			// We've exhausted all retry attempts
			return nil, fmt.Errorf("gemini: API error after %d attempts: %w", attempts, gemApiErr)
		}

		// Check if the error is retryable (e.g., server error or rate limiting)
		if strings.Contains(gemApiErr.Error(), "429") || strings.Contains(gemApiErr.Error(), "5") {
			// Rate limited or server error - wait and retry
			random := time.Duration(rand.Int63n(int64(time.Second)))
			sleep := backoff[attempts] + random
			slog.WarnContext(ctx, "gemini_request_retry", "error", gemApiErr.Error(), "attempt", attempts+1, "sleep", sleep)
			time.Sleep(sleep)
			continue
		}

		// Non-retryable error
		return nil, fmt.Errorf("gemini: API error: %w", gemApiErr)
	}

	content := convertGeminiResponseToContent(gemRes)

	ensureToolIDs(content)

	usage := calculateUsage(gemReq, gemRes)
	usage.CostUSD = llm.CostUSDFromResponse(gemRes.Header())

	stopReason := llm.StopReasonEndTurn
	for _, part := range content {
		if part.Type == llm.ContentTypeToolUse {
			stopReason = llm.StopReasonToolUse
			slog.DebugContext(ctx, "gemini_tool_use_detected",
				"setting_stop_reason", "llm.StopReasonToolUse",
				"tool_name", part.ToolName)
			break
		}
	}

	return &llm.Response{
		Role:       llm.MessageRoleAssistant,
		Model:      s.Model,
		Content:    content,
		StopReason: stopReason,
		Usage:      usage,
		StartTime:  &startTime,
		EndTime:    &endTime,
	}, nil
}
