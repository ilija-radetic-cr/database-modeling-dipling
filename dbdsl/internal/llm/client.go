package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultModel           = "gpt-5.6-sol"
	DefaultReasoningEffort = "medium"
	DefaultMaxOutputTokens = 12000
	DefaultOpenAIEndpoint  = "https://api.openai.com/v1/responses"
	DefaultHTTPTimeout     = 10 * time.Minute
)

type Client interface {
	GenerateStructured(ctx context.Context, req Request) (Response, error)
	GenerateText(ctx context.Context, req TextRequest) (TextResponse, error)
}

type Request struct {
	Stage           string
	Model           string
	Instructions    string
	Input           string
	SchemaName      string
	Schema          map[string]any
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
	Metadata        map[string]string
}

type TextRequest struct {
	Stage           string
	Model           string
	Instructions    string
	Input           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
	Metadata        map[string]string
}

type Response struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Raw      string          `json:"raw"`
	Text     string          `json:"text"`
	Parsed   json.RawMessage `json:"parsed"`
	Usage    Usage           `json:"usage"`
}

type TextResponse struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Raw      string `json:"raw"`
	Text     string `json:"text"`
	Usage    Usage  `json:"usage"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type RequestLog struct {
	Stage           string            `json:"stage"`
	Model           string            `json:"model"`
	Provider        string            `json:"provider"`
	ReasoningEffort string            `json:"reasoning_effort"`
	Temperature     float64           `json:"temperature"`
	MaxOutputTokens int               `json:"max_output_tokens"`
	SchemaName      string            `json:"schema_name,omitempty"`
	Instructions    string            `json:"instructions"`
	Input           string            `json:"input"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

func (r Request) SanitizedLog(provider string) RequestLog {
	return RequestLog{
		Stage:           r.Stage,
		Model:           nonEmpty(r.Model, DefaultModel),
		Provider:        provider,
		ReasoningEffort: nonEmpty(r.ReasoningEffort, DefaultReasoningEffort),
		Temperature:     r.Temperature,
		MaxOutputTokens: defaultMaxTokens(r.MaxOutputTokens),
		SchemaName:      r.SchemaName,
		Instructions:    r.Instructions,
		Input:           r.Input,
		Metadata:        r.Metadata,
	}
}

func (r TextRequest) SanitizedLog(provider string) RequestLog {
	return RequestLog{
		Stage:           r.Stage,
		Model:           nonEmpty(r.Model, DefaultModel),
		Provider:        provider,
		ReasoningEffort: nonEmpty(r.ReasoningEffort, DefaultReasoningEffort),
		Temperature:     r.Temperature,
		MaxOutputTokens: defaultMaxTokens(r.MaxOutputTokens),
		Instructions:    r.Instructions,
		Input:           r.Input,
		Metadata:        r.Metadata,
	}
}

type OpenAIClient struct {
	APIKey     string
	Endpoint   string
	HTTPClient *http.Client
}

func NewOpenAIClientFromEnv() (*OpenAIClient, error) {
	key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if key == "" {
		return nil, errors.New("OPENAI_API_KEY is required for real LLM calls; use --mock for offline tests")
	}
	return &OpenAIClient{
		APIKey:   key,
		Endpoint: normalizeEndpoint(os.Getenv("DBDSL_LLM_BASE_URL")),
		HTTPClient: &http.Client{
			Timeout: openAIHTTPTimeoutFromEnv(),
		},
	}, nil
}

func openAIHTTPTimeoutFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("DBDSL_LLM_HTTP_TIMEOUT"))
	if raw == "" {
		return DefaultHTTPTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return DefaultHTTPTimeout
	}
	return timeout
}

func (c *OpenAIClient) GenerateStructured(ctx context.Context, req Request) (Response, error) {
	if req.SchemaName == "" {
		return Response{}, errors.New("schema name is required for structured LLM calls")
	}
	if req.Schema == nil {
		return Response{}, errors.New("schema is required for structured LLM calls")
	}

	body, err := c.do(ctx, buildStructuredPayload(req))
	if err != nil {
		return Response{}, err
	}
	text, usage, err := extractOutputText(body)
	response := Response{
		Provider: "openai",
		Model:    nonEmpty(req.Model, DefaultModel),
		Raw:      string(body),
		Text:     strings.TrimSpace(text),
		Usage:    usage,
	}
	if err != nil {
		return response, err
	}
	trimmed := response.Text
	if !json.Valid([]byte(trimmed)) {
		return response, fmt.Errorf("structured LLM response is not valid JSON: %s", snippet(trimmed, 240))
	}
	response.Parsed = json.RawMessage(trimmed)
	return response, nil
}

func (c *OpenAIClient) GenerateText(ctx context.Context, req TextRequest) (TextResponse, error) {
	body, err := c.do(ctx, buildTextPayload(req))
	if err != nil {
		return TextResponse{}, err
	}
	text, usage, err := extractOutputText(body)
	response := TextResponse{
		Provider: "openai",
		Model:    nonEmpty(req.Model, DefaultModel),
		Raw:      string(body),
		Text:     strings.TrimSpace(text),
		Usage:    usage,
	}
	if err != nil {
		return response, err
	}
	return response, nil
}

func (c *OpenAIClient) do(ctx context.Context, payload map[string]any) ([]byte, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultOpenAIEndpoint
	}
	apiKey := strings.TrimSpace(c.APIKey)
	if apiKey == "" {
		return nil, errors.New("OpenAI API key is required")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal OpenAI request: %w", err)
	}
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create OpenAI request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("OpenAI request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("read OpenAI response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenAI request returned %s: %s", resp.Status, openAIErrorMessage(body))
	}
	return body, nil
}

func buildStructuredPayload(req Request) map[string]any {
	payload := basePayload(req.Model, req.Instructions, req.Input, req.ReasoningEffort, req.Temperature, req.MaxOutputTokens)
	payload["text"] = map[string]any{
		"format": map[string]any{
			"type":        "json_schema",
			"name":        req.SchemaName,
			"description": "Structured proposal for DB-DSL assisted database modeling.",
			"strict":      true,
			"schema":      req.Schema,
		},
		"verbosity": "low",
	}
	return payload
}

func buildTextPayload(req TextRequest) map[string]any {
	payload := basePayload(req.Model, req.Instructions, req.Input, req.ReasoningEffort, req.Temperature, req.MaxOutputTokens)
	payload["text"] = map[string]any{
		"format": map[string]any{"type": "text"},
	}
	return payload
}

func basePayload(model, instructions, input, reasoningEffort string, temperature float64, maxOutputTokens int) map[string]any {
	payload := map[string]any{
		"model":             nonEmpty(model, DefaultModel),
		"instructions":      instructions,
		"input":             input,
		"store":             false,
		"max_output_tokens": defaultMaxTokens(maxOutputTokens),
		"reasoning": map[string]any{
			"effort": nonEmpty(reasoningEffort, DefaultReasoningEffort),
		},
	}
	if temperature != 0 && supportsTemperature(nonEmpty(model, DefaultModel)) {
		payload["temperature"] = temperature
	}
	return payload
}

func normalizeEndpoint(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return DefaultOpenAIEndpoint
	}
	value = strings.TrimRight(value, "/")
	if strings.HasSuffix(value, "/responses") {
		return value
	}
	if strings.HasSuffix(value, "/v1") {
		return value + "/responses"
	}
	return value + "/v1/responses"
}

func extractOutputText(body []byte) (string, Usage, error) {
	var envelope struct {
		OutputText string `json:"output_text"`
		Status     string `json:"status"`
		Error      *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
		IncompleteDetails *struct {
			Reason string `json:"reason"`
		} `json:"incomplete_details"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
		Usage Usage `json:"usage"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return "", Usage{}, fmt.Errorf("parse OpenAI response JSON: %w", err)
	}
	if envelope.Error != nil && envelope.Error.Message != "" {
		return "", envelope.Usage, fmt.Errorf("OpenAI response error: %s", envelope.Error.Message)
	}
	var parts []string
	if envelope.OutputText != "" {
		parts = append(parts, envelope.OutputText)
	} else {
		for _, item := range envelope.Output {
			for _, content := range item.Content {
				if content.Type == "output_text" && content.Text != "" {
					parts = append(parts, content.Text)
				}
			}
		}
	}
	text := strings.Join(parts, "\n")
	if envelope.Status != "" && envelope.Status != "completed" {
		detail := ""
		if envelope.IncompleteDetails != nil && envelope.IncompleteDetails.Reason != "" {
			detail = fmt.Sprintf(": %s", envelope.IncompleteDetails.Reason)
		}
		return text, envelope.Usage, fmt.Errorf("OpenAI response has status %s%s", envelope.Status, detail)
	}
	if len(parts) == 0 {
		return "", envelope.Usage, errors.New("OpenAI response did not contain output_text")
	}
	return text, envelope.Usage, nil
}

func openAIErrorMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	return snippet(string(body), 500)
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func defaultMaxTokens(value int) int {
	if value <= 0 {
		return DefaultMaxOutputTokens
	}
	return value
}

func supportsTemperature(model string) bool {
	return !strings.HasPrefix(model, "gpt-5")
}

func snippet(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
