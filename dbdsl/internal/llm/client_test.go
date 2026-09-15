package llm

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildStructuredPayloadDoesNotContainSecrets(t *testing.T) {
	payload := buildStructuredPayload(Request{
		Stage:        "requirement_extraction",
		Model:        "test-model",
		Instructions: "Return JSON.",
		Input:        "input",
		SchemaName:   "TestSchema",
		Schema: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"required":             []string{},
			"additionalProperties": false,
		},
	})
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	text := string(data)
	for _, needle := range []string{"Authorization", "OPENAI_API_KEY", "sk-"} {
		if strings.Contains(text, needle) {
			t.Fatalf("payload contains secret-like value %q: %s", needle, text)
		}
	}
}

func TestBuildStructuredPayloadOmitsUnsupportedTemperature(t *testing.T) {
	payload := buildStructuredPayload(Request{
		Stage:        "requirement_extraction",
		Model:        "gpt-5.6-sol",
		Instructions: "Return JSON.",
		Input:        "input",
		SchemaName:   "TestSchema",
		Schema:       map[string]any{"type": "object"},
		Temperature:  0.2,
	})
	if _, ok := payload["temperature"]; ok {
		t.Fatalf("gpt-5 family payload should not include temperature")
	}
}

func TestExtractOutputTextFromNestedResponse(t *testing.T) {
	body := []byte(`{
	  "status": "completed",
	  "output": [
	    {
	      "type": "message",
	      "content": [
	        {"type": "output_text", "text": "{\"ok\":true}"}
	      ]
	    }
	  ],
	  "usage": {"input_tokens": 10, "output_tokens": 5, "total_tokens": 15}
	}`)
	text, usage, err := extractOutputText(body)
	if err != nil {
		t.Fatalf("extractOutputText failed: %v", err)
	}
	if text != `{"ok":true}` {
		t.Fatalf("unexpected output text %q", text)
	}
	if usage.TotalTokens != 15 {
		t.Fatalf("expected usage total 15, got %d", usage.TotalTokens)
	}
}

func TestExtractOutputTextReportsIncompleteReasonAndPreservesPartialOutput(t *testing.T) {
	body := []byte(`{
	  "status": "incomplete",
	  "incomplete_details": {"reason": "max_output_tokens"},
	  "output": [{
	    "type": "message",
	    "content": [{"type": "output_text", "text": "{\"partial\":"}]
	  }],
	  "usage": {"input_tokens": 100, "output_tokens": 12000, "total_tokens": 12100}
	}`)
	text, usage, err := extractOutputText(body)
	if err == nil || !strings.Contains(err.Error(), "status incomplete: max_output_tokens") {
		t.Fatalf("incomplete response error = %v", err)
	}
	if text != `{"partial":` {
		t.Fatalf("partial output = %q", text)
	}
	if usage.OutputTokens != 12000 {
		t.Fatalf("output tokens = %d, want 12000", usage.OutputTokens)
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	cases := map[string]string{
		"":                                  DefaultOpenAIEndpoint,
		"https://api.openai.com":            "https://api.openai.com/v1/responses",
		"https://api.openai.com/v1":         "https://api.openai.com/v1/responses",
		"https://example.test/v1/responses": "https://example.test/v1/responses",
	}
	for input, want := range cases {
		if got := normalizeEndpoint(input); got != want {
			t.Fatalf("normalizeEndpoint(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestOpenAIHTTPTimeoutFromEnv(t *testing.T) {
	t.Setenv("DBDSL_LLM_HTTP_TIMEOUT", "")
	if got := openAIHTTPTimeoutFromEnv(); got != 10*time.Minute {
		t.Fatalf("default timeout = %s, want 10m", got)
	}

	t.Setenv("DBDSL_LLM_HTTP_TIMEOUT", "90s")
	if got := openAIHTTPTimeoutFromEnv(); got != 90*time.Second {
		t.Fatalf("configured timeout = %s, want 90s", got)
	}

	for _, invalid := range []string{"invalid", "0s", "-1s"} {
		t.Setenv("DBDSL_LLM_HTTP_TIMEOUT", invalid)
		if got := openAIHTTPTimeoutFromEnv(); got != 10*time.Minute {
			t.Fatalf("timeout for %q = %s, want safe default 10m", invalid, got)
		}
	}
}
