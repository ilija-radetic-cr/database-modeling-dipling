package llmpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"dbdsl/internal/llm"
)

type scriptedClient struct {
	responses    []string
	instructions []string
}

func (c *scriptedClient) GenerateStructured(_ context.Context, req llm.Request) (llm.Response, error) {
	c.instructions = append(c.instructions, req.Instructions)
	if len(c.responses) == 0 {
		return llm.Response{}, errors.New("no scripted response")
	}
	next := c.responses[0]
	c.responses = c.responses[1:]
	return llm.Response{Raw: next, Parsed: json.RawMessage(next)}, nil
}

func (c *scriptedClient) GenerateText(context.Context, llm.TextRequest) (llm.TextResponse, error) {
	return llm.TextResponse{}, errors.New("unused")
}

func TestRunStructuredStageRetriesOnceWithValidationFeedback(t *testing.T) {
	client := &scriptedClient{responses: []string{`{"covered":["RA-1"]}`, `{"covered":["RA-1","RA-2"]}`}}
	var target struct{ Covered []string }
	validate := func() []string {
		if len(target.Covered) < 2 {
			return []string{"functional analysis does not cover atoms: RA-2"}
		}
		return nil
	}
	req := llm.Request{Stage: "functional_analysis", Instructions: "base", Input: "{}", Metadata: map[string]string{}}
	if err := runStructuredStage(context.Background(), client, t.TempDir(), 1, req, &target, validate); err != nil {
		t.Fatalf("stage should succeed after feedback retry: %v", err)
	}
	if len(client.instructions) != 2 || !strings.Contains(client.instructions[1], "does not cover atoms: RA-2") {
		t.Fatalf("retry did not receive validation feedback: %q", client.instructions)
	}
}

func TestQuotaErrorsAreNotRetried(t *testing.T) {
	if retryableStructuredError(errors.New("OpenAI request returned 429 Too Many Requests: You have no credits remaining")) {
		t.Fatal("billing errors must not be retried")
	}
	if !retryableStructuredError(errors.New("OpenAI request returned 429 Too Many Requests: rate limit")) {
		t.Fatal("rate limits must stay retryable")
	}
}
