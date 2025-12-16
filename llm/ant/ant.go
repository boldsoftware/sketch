package ant

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strings"
	"testing"
	"time"

	"sketch.dev/llm"
)

const (
	DefaultModel = Claude45Sonnet
	// See https://docs.anthropic.com/en/docs/about-claude/models/all-models for
	// current maximums. There's currently a flag to enable 128k output (output-128k-2025-02-19)
	DefaultMaxTokens = 8192
	APIKeyEnv        = "ANTHROPIC_API_KEY"
	DefaultURL       = "https://api.anthropic.com/v1/messages"
)

const (
	Claude35Sonnet = "claude-3-5-sonnet-20241022"
	Claude35Haiku  = "claude-3-5-haiku-20241022"
	Claude37Sonnet = "claude-3-7-sonnet-20250219"
	Claude4Sonnet  = "claude-sonnet-4-20250514"
	Claude45Sonnet = "claude-sonnet-4-5-20250929"
	Claude45Opus   = "claude-opus-4-5-20251101"
)

// IsClaudeModel reports whether userName is a user-friendly Claude model.
// It uses ClaudeModelName under the hood.
func IsClaudeModel(userName string) bool {
	return ClaudeModelName(userName) != ""
}

// ClaudeModelName returns the Anthropic Claude model name for userName.
// It returns an empty string if userName is not a recognized Claude model.
func ClaudeModelName(userName string) string {
	switch userName {
	case "claude", "sonnet":
		return Claude45Sonnet
	case "opus":
		return Claude45Opus
	default:
		return ""
	}
}

// TokenContextWindow returns the maximum token context window size for this service
func (s *Service) TokenContextWindow() int {
	model := s.Model
	if model == "" {
		model = DefaultModel
	}

	switch model {
	case Claude35Sonnet, Claude37Sonnet:
		return 200000
	case Claude35Haiku:
		return 200000
	case Claude4Sonnet, Claude45Sonnet, Claude45Opus:
		return 200000
	default:
		// Default for unknown models
		return 200000
	}
}

// Service provides Claude completions.
// Fields should not be altered concurrently with calling any method on Service.
type Service struct {
	HTTPC     *http.Client // defaults to http.DefaultClient if nil
	URL       string       // defaults to DefaultURL if empty
	APIKey    string       // must be non-empty
	Model     string       // defaults to DefaultModel if empty
	MaxTokens int          // defaults to DefaultMaxTokens if zero
	DumpLLM   bool         // whether to dump request/response text to files for debugging; defaults to false
}

var _ llm.Service = (*Service)(nil)

type content struct {
	// https://docs.anthropic.com/en/api/messages
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`

	// Subtly, an empty string appears in tool results often, so we have
	// to distinguish between empty string and no string.
	// Underlying error looks like one of:
	//   "messages.46.content.0.tool_result.content.0.text.text: Field required""
	//   "messages.1.content.1.tool_use.text: Extra inputs are not permitted"
	//
	// I haven't found a super great source for the API, but
	// https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/resources/messages/messages.ts
	// is somewhat acceptable but hard to read.
	Text      *string         `json:"text,omitempty"`
	MediaType string          `json:"media_type,omitempty"` // for image
	Source    json.RawMessage `json:"source,omitempty"`     // for image

	// for thinking
	Thinking  string `json:"thinking,omitempty"`
	Data      string `json:"data,omitempty"`      // for redacted_thinking or image
	Signature string `json:"signature,omitempty"` // for thinking

	// for tool_use
	ToolName  string          `json:"name,omitempty"`
	ToolInput json.RawMessage `json:"input,omitempty"`

	// for tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	ToolError bool   `json:"is_error,omitempty"`
	// note the recursive nature here; message looks like:
	// {
	//  "role": "user",
	//  "content": [
	//    {
	//      "type": "tool_result",
	//      "tool_use_id": "toolu_01A09q90qw90lq917835lq9",
	//      "content": [
	//        {"type": "text", "text": "15 degrees"},
	//        {
	//          "type": "image",
	//          "source": {
	//            "type": "base64",
	//            "media_type": "image/jpeg",
	//            "data": "/9j/4AAQSkZJRg...",
	//          }
	//        }
	//      ]
	//    }
	//  ]
	//}
	ToolResult []content `json:"content,omitempty"`

	// timing information for tool_result; not sent to Claude
	StartTime *time.Time `json:"-"`
	EndTime   *time.Time `json:"-"`

	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// message represents a message in the conversation.
type message struct {
	Role    string    `json:"role"`
	Content []content `json:"content"`
	ToolUse *toolUse  `json:"tool_use,omitempty"` // use to control whether/which tool to use
}

// toolUse represents a tool use in the message content.
type toolUse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// tool represents a tool available to Claude.
type tool struct {
	Name string `json:"name"`
	// Type is used by the text editor tool; see
	// https://docs.anthropic.com/en/docs/build-with-claude/tool-use/text-editor-tool
	Type        string          `json:"type,omitempty"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// usage represents the billing and rate-limit usage.
type usage struct {
	InputTokens              uint64  `json:"input_tokens"`
	CacheCreationInputTokens uint64  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     uint64  `json:"cache_read_input_tokens"`
	OutputTokens             uint64  `json:"output_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

func (u *usage) Add(other usage) {
	u.InputTokens += other.InputTokens
	u.CacheCreationInputTokens += other.CacheCreationInputTokens
	u.CacheReadInputTokens += other.CacheReadInputTokens
	u.OutputTokens += other.OutputTokens
	u.CostUSD += other.CostUSD
}

// response represents the response from the message API.
type response struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Model        string    `json:"model"`
	Content      []content `json:"content"`
	StopReason   string    `json:"stop_reason"`
	StopSequence *string   `json:"stop_sequence,omitempty"`
	Usage        usage     `json:"usage"`
}

type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// https://docs.anthropic.com/en/api/messages#body-system
type systemContent struct {
	Text         string          `json:"text,omitempty"`
	Type         string          `json:"type,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// request represents the request payload for creating a message.
type request struct {
	Model         string          `json:"model"`
	Messages      []message       `json:"messages"`
	ToolChoice    *toolChoice     `json:"tool_choice,omitempty"`
	MaxTokens     int             `json:"max_tokens"`
	Tools         []*tool         `json:"tools,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	System        []systemContent `json:"system,omitempty"`
	Temperature   float64         `json:"temperature,omitempty"`
	TopK          int             `json:"top_k,omitempty"`
	TopP          float64         `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`

	TokenEfficientToolUse bool `json:"-"` // DO NOT USE, broken on Anthropic's side as of 2025-02-28
}

func mapped[Slice ~[]E, E, T any](s Slice, f func(E) T) []T {
	out := make([]T, len(s))
	for i, v := range s {
		out[i] = f(v)
	}
	return out
}

func inverted[K, V cmp.Ordered](m map[K]V) map[V]K {
	inv := make(map[V]K)
	for k, v := range m {
		if _, ok := inv[v]; ok {
			panic(fmt.Errorf("inverted map has multiple keys for value %v", v))
		}
		inv[v] = k
	}
	return inv
}

var (
	fromLLMRole = map[llm.MessageRole]string{
		llm.MessageRoleAssistant: "assistant",
		llm.MessageRoleUser:      "user",
	}
	toLLMRole = inverted(fromLLMRole)

	fromLLMContentType = map[llm.ContentType]string{
		llm.ContentTypeText:             "text",
		llm.ContentTypeThinking:         "thinking",
		llm.ContentTypeRedactedThinking: "redacted_thinking",
		llm.ContentTypeToolUse:          "tool_use",
		llm.ContentTypeToolResult:       "tool_result",
	}
	toLLMContentType = inverted(fromLLMContentType)

	fromLLMToolChoiceType = map[llm.ToolChoiceType]string{
		llm.ToolChoiceTypeAuto: "auto",
		llm.ToolChoiceTypeAny:  "any",
		llm.ToolChoiceTypeNone: "none",
		llm.ToolChoiceTypeTool: "tool",
	}

	toLLMStopReason = map[string]llm.StopReason{
		"stop_sequence": llm.StopReasonStopSequence,
		"max_tokens":    llm.StopReasonMaxTokens,
		"end_turn":      llm.StopReasonEndTurn,
		"tool_use":      llm.StopReasonToolUse,
		"refusal":       llm.StopReasonRefusal,
	}
)

func fromLLMCache(c bool) json.RawMessage {
	if !c {
		return nil
	}
	return json.RawMessage(`{"type":"ephemeral"}`)
}

func fromLLMContent(c llm.Content) content {
	var toolResult []content
	if len(c.ToolResult) > 0 {
		toolResult = make([]content, len(c.ToolResult))
		for i, tr := range c.ToolResult {
			// For image content inside a tool_result, we need to map it to "image" type
			if tr.MediaType != "" && tr.MediaType == "image/jpeg" || tr.MediaType == "image/png" {
				// Format as an image for Claude
				toolResult[i] = content{
					Type: "image",
					Source: json.RawMessage(fmt.Sprintf(`{"type":"base64","media_type":"%s","data":"%s"}`,
						tr.MediaType, tr.Data)),
				}
			} else {
				toolResult[i] = fromLLMContent(tr)
			}
		}
	}

	d := content{
		ID:           c.ID,
		Type:         fromLLMContentType[c.Type],
		MediaType:    c.MediaType,
		Thinking:     c.Thinking,
		Data:         c.Data,
		Signature:    c.Signature,
		ToolName:     c.ToolName,
		ToolInput:    c.ToolInput,
		ToolUseID:    c.ToolUseID,
		ToolError:    c.ToolError,
		ToolResult:   toolResult,
		CacheControl: fromLLMCache(c.Cache),
	}
	// Anthropic API complains if Text is specified when it shouldn't be
	// or not specified when it's the empty string.
	if c.Type != llm.ContentTypeToolResult && c.Type != llm.ContentTypeToolUse {
		d.Text = &c.Text
	}
	return d
}

func fromLLMToolUse(tu *llm.ToolUse) *toolUse {
	if tu == nil {
		return nil
	}
	return &toolUse{
		ID:   tu.ID,
		Name: tu.Name,
	}
}

func fromLLMMessage(msg llm.Message) message {
	return message{
		Role:    fromLLMRole[msg.Role],
		Content: mapped(msg.Content, fromLLMContent),
		ToolUse: fromLLMToolUse(msg.ToolUse),
	}
}

func fromLLMToolChoice(tc *llm.ToolChoice) *toolChoice {
	if tc == nil {
		return nil
	}
	return &toolChoice{
		Type: fromLLMToolChoiceType[tc.Type],
		Name: tc.Name,
	}
}

func fromLLMTool(t *llm.Tool) *tool {
	return &tool{
		Name:        t.Name,
		Type:        t.Type,
		Description: t.Description,
		InputSchema: t.InputSchema,
	}
}

func fromLLMSystem(s llm.SystemContent) systemContent {
	return systemContent{
		Text:         s.Text,
		Type:         s.Type,
		CacheControl: fromLLMCache(s.Cache),
	}
}

func (s *Service) fromLLMRequest(r *llm.Request) *request {
	return &request{
		Model:      cmp.Or(s.Model, DefaultModel),
		Messages:   mapped(r.Messages, fromLLMMessage),
		MaxTokens:  cmp.Or(s.MaxTokens, DefaultMaxTokens),
		ToolChoice: fromLLMToolChoice(r.ToolChoice),
		Tools:      mapped(r.Tools, fromLLMTool),
		System:     mapped(r.System, fromLLMSystem),
	}
}

func toLLMUsage(u usage) llm.Usage {
	return llm.Usage{
		InputTokens:              u.InputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
		OutputTokens:             u.OutputTokens,
		CostUSD:                  u.CostUSD,
	}
}

func toLLMContent(c content) llm.Content {
	// Convert toolResult from []content to []llm.Content
	var toolResultContents []llm.Content
	if len(c.ToolResult) > 0 {
		toolResultContents = make([]llm.Content, len(c.ToolResult))
		for i, tr := range c.ToolResult {
			toolResultContents[i] = toLLMContent(tr)
		}
	}

	ret := llm.Content{
		ID:         c.ID,
		Type:       toLLMContentType[c.Type],
		MediaType:  c.MediaType,
		Thinking:   c.Thinking,
		Data:       c.Data,
		Signature:  c.Signature,
		ToolName:   c.ToolName,
		ToolInput:  c.ToolInput,
		ToolUseID:  c.ToolUseID,
		ToolError:  c.ToolError,
		ToolResult: toolResultContents,
	}
	if c.Text != nil {
		ret.Text = *c.Text
	}
	return ret
}

func toLLMResponse(r *response) *llm.Response {
	return &llm.Response{
		ID:           r.ID,
		Type:         r.Type,
		Role:         toLLMRole[r.Role],
		Model:        r.Model,
		Content:      mapped(r.Content, toLLMContent),
		StopReason:   toLLMStopReason[r.StopReason],
		StopSequence: r.StopSequence,
		Usage:        toLLMUsage(r.Usage),
	}
}

// Do sends a request to Anthropic.
func (s *Service) Do(ctx context.Context, ir *llm.Request) (*llm.Response, error) {
	request := s.fromLLMRequest(ir)

	var payload []byte
	var err error
	if s.DumpLLM || testing.Testing() {
		payload, err = json.MarshalIndent(request, "", " ")
	} else {
		payload, err = json.Marshal(request)
		payload = append(payload, '\n')
	}
	if err != nil {
		return nil, err
	}

	if false {
		fmt.Printf("claude request payload:\n%s\n", payload)
	}

	backoff := []time.Duration{15 * time.Second, 30 * time.Second, time.Minute}
	largerMaxTokens := false
	var partialUsage usage

	url := cmp.Or(s.URL, DefaultURL)
	httpc := cmp.Or(s.HTTPC, http.DefaultClient)

	// retry loop
	var errs error // accumulated errors across all attempts
	for attempts := 0; ; attempts++ {
		if attempts > 10 {
			return nil, fmt.Errorf("anthropic request failed after %d attempts: %w", attempts, errs)
		}
		if attempts > 0 {
			sleep := backoff[min(attempts, len(backoff)-1)] + time.Duration(rand.Int64N(int64(time.Second)))
			slog.WarnContext(ctx, "anthropic request sleep before retry", "sleep", sleep, "attempts", attempts)
			time.Sleep(sleep)
		}
		if s.DumpLLM {
			if err := llm.DumpToFile("request", url, payload); err != nil {
				slog.WarnContext(ctx, "failed to dump request to file", "error", err)
			}
		}
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			return nil, errors.Join(errs, err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", s.APIKey)
		req.Header.Set("Anthropic-Version", "2023-06-01")

		var features []string
		if request.TokenEfficientToolUse {
			features = append(features, "token-efficient-tool-use-2025-02-19")
		}
		if largerMaxTokens {
			features = append(features, "output-128k-2025-02-19")
			request.MaxTokens = 128 * 1024
		}
		if len(features) > 0 {
			req.Header.Set("anthropic-beta", strings.Join(features, ","))
		}

		resp, err := httpc.Do(req)
		if err != nil {
			// Don't retry httprr cache misses
			if strings.Contains(err.Error(), "cached HTTP response not found") {
				return nil, err
			}
			errs = errors.Join(errs, err)
			continue
		}
		buf, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			if s.DumpLLM {
				if err := llm.DumpToFile("response", "", buf); err != nil {
					slog.WarnContext(ctx, "failed to dump response to file", "error", err)
				}
			}
			var response response
			err = json.NewDecoder(bytes.NewReader(buf)).Decode(&response)
			if err != nil {
				return nil, errors.Join(errs, err)
			}
			if response.StopReason == "max_tokens" && !largerMaxTokens {
				slog.InfoContext(ctx, "anthropic_retrying_with_larger_tokens", "message", "Retrying Anthropic API call with larger max tokens size")
				// Retry with more output tokens.
				largerMaxTokens = true
				response.Usage.CostUSD = llm.CostUSDFromResponse(resp.Header)
				partialUsage = response.Usage
				continue
			}

			// Calculate and set the cost_usd field
			if largerMaxTokens {
				response.Usage.Add(partialUsage)
			}
			response.Usage.CostUSD = llm.CostUSDFromResponse(resp.Header)

			return toLLMResponse(&response), nil
		case resp.StatusCode >= 500 && resp.StatusCode < 600:
			// server error, retry
			slog.WarnContext(ctx, "anthropic_request_failed", "response", string(buf), "status_code", resp.StatusCode)
			errs = errors.Join(errs, fmt.Errorf("status %v: %s", resp.Status, buf))
			continue
		case resp.StatusCode == 429:
			// rate limited, retry
			slog.WarnContext(ctx, "anthropic_request_rate_limited", "response", string(buf))
			errs = errors.Join(errs, fmt.Errorf("status %v: %s", resp.Status, buf))
			continue
		case resp.StatusCode >= 400 && resp.StatusCode < 500:
			// some other 400, probably unrecoverable
			slog.WarnContext(ctx, "anthropic_request_failed", "response", string(buf), "status_code", resp.StatusCode)
			return nil, errors.Join(errs, fmt.Errorf("status %v: %s", resp.Status, buf))
		default:
			// ...retry, I guess?
			slog.WarnContext(ctx, "anthropic_request_failed", "response", string(buf), "status_code", resp.StatusCode)
			errs = errors.Join(errs, fmt.Errorf("status %v: %s", resp.Status, buf))
			continue
		}
	}
}

// For debugging only, Claude can definitely handle the full patch tool.
// func (s *Service) UseSimplifiedPatch() bool {
// 	return true
// }
