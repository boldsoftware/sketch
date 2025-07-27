package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// https://ai.google.dev/api/generate-content#request-body
type Request struct {
	Contents          []Content         `json:"contents"`
	Tools             []Tool            `json:"tools,omitempty"`
	SystemInstruction *Content          `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig `json:"generationConfig,omitempty"`
	CachedContent     string            `json:"cachedContent,omitempty"` // format: "cachedContents/{name}"
	// ToolConfig has been left out because it does not appear to be useful.
}

// https://ai.google.dev/api/generate-content#response-body
type Response struct {
	Candidates []Candidate `json:"candidates"`
	headers    http.Header // captured HTTP response headers
}

// Header returns the HTTP response headers.
func (r *Response) Header() http.Header {
	return r.headers
}

type Candidate struct {
	Content Content `json:"content"`
}

type Content struct {
	Parts []Part `json:"parts"`
	Role  string `json:"role,omitempty"`
}

// Part is a part of the content.
// This is a union data structure, only one-of the fields can be set.
type Part struct {
	Text                string               `json:"text,omitempty"`
	FunctionCall        *FunctionCall        `json:"functionCall,omitempty"`
	FunctionResponse    *FunctionResponse    `json:"functionResponse,omitempty"`
	ExecutableCode      *ExecutableCode      `json:"executableCode,omitempty"`
	CodeExecutionResult *CodeExecutionResult `json:"codeExecutionResult,omitempty"`
	// TODO inlineData
	// TODO fileData
}

type FunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type FunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type ExecutableCode struct {
	Language Language `json:"language"`
	Code     string   `json:"code"`
}

type Language int

const (
	LanguageUnspecified Language = 0
	LanguagePython      Language = 1 // python >= 3.10 with numpy and simpy
)

type CodeExecutionResult struct {
	Outcome Outcome `json:"outcome"`
	Output  string  `json:"output"`
}

type Outcome int

const (
	OutcomeUnspecified      Outcome = 0
	OutcomeOK               Outcome = 1
	OutcomeFailed           Outcome = 2
	OutcomeDeadlineExceeded Outcome = 3
)

// https://ai.google.dev/api/generate-content#v1beta.GenerationConfig
type GenerationConfig struct {
	ResponseMimeType string  `json:"responseMimeType,omitempty"` // text/plain, application/json, or text/x.enum
	ResponseSchema   *Schema `json:"responseSchema,omitempty"`   // for JSON
}

// https://ai.google.dev/api/caching#Tool
type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations"`
	CodeExecution        *struct{}             `json:"codeExecution,omitempty"` // if present, enables the model to execute code
	// TODO googleSearchRetrieval https://ai.google.dev/api/caching#GoogleSearchRetrieval
}

// https://ai.google.dev/api/caching#FunctionDeclaration
type FunctionDeclaration struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  Schema `json:"parameters"`
}

// https://ai.google.dev/api/caching#Schema
type Schema struct {
	Type        DataType          `json:"type"`
	Format      string            `json:"string,omitempty"` // for NUMBER type: float, double for INTEGER type: int32, int64 for STRING type: enum
	Description string            `json:"description,omitempty"`
	Nullable    *bool             `json:"nullable,omitempty"`
	Enum        []string          `json:"enum,omitempty"`
	MaxItems    string            `json:"maxItems,omitempty"`   // for ARRAY
	MinItems    string            `json:"minItems,omitempty"`   // for ARRAY
	Properties  map[string]Schema `json:"properties,omitempty"` // for OBJECT
	Required    []string          `json:"required,omitempty"`   // for OBJECT
	Items       *Schema           `json:"items,omitempty"`      // for ARRAY
}

type DataType int

const (
	DataTypeUNSPECIFIED = DataType(0) // Not specified, should not be used.
	DataTypeSTRING      = DataType(1)
	DataTypeNUMBER      = DataType(2)
	DataTypeINTEGER     = DataType(3)
	DataTypeBOOLEAN     = DataType(4)
	DataTypeARRAY       = DataType(5)
	DataTypeOBJECT      = DataType(6)
)

const defaultEndpoint = "https://generativelanguage.googleapis.com/v1beta"

type Model struct {
	Model    string // e.g. "models/gemini-1.5-flash"
	APIKey   string
	HTTPC    *http.Client // if nil, http.DefaultClient is used
	Endpoint string       // if empty, DefaultEndpoint is used
}

func (m Model) GenerateContent(ctx context.Context, req *Request) (*Response, error) {
	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", m.endpoint(), m.Model, m.APIKey)
	fmt.Printf("DEBUG: Making Gemini request to URL: %s\n", url)
	fmt.Printf("DEBUG: API Key length: %d\n", len(m.APIKey))
	fmt.Printf("DEBUG: Model: %s\n", m.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Add("Content-Type", "application/json")
	httpResp, err := m.httpc().Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("GenerateContent: do: %w", err)
	}
	defer httpResp.Body.Close()
	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("GenerateContent: reading response body: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GenerateContent: HTTP status: %d, %s", httpResp.StatusCode, string(body))
	}
	var res Response
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("GenerateContent: unmarshaling response: %w, %s", err, string(body))
	}
	res.headers = httpResp.Header
	return &res, nil
}

func (m Model) endpoint() string {
	if m.Endpoint != "" {
		return m.Endpoint
	}
	return defaultEndpoint
}

func (m Model) httpc() *http.Client {
	if m.HTTPC != nil {
		return m.HTTPC
	}
	return http.DefaultClient
}
