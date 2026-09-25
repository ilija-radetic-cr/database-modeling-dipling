package llmpipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"dbdsl/internal/llm"
)

type transientStructuredClient struct{ calls int }

func (c *transientStructuredClient) GenerateStructured(context.Context, llm.Request) (llm.Response, error) {
	c.calls++
	if c.calls == 1 {
		return llm.Response{}, errors.New("temporary 503 response")
	}
	payload := json.RawMessage(`{"value":"ok"}`)
	return llm.Response{Provider: "test", Model: "test", Raw: string(payload), Text: string(payload), Parsed: payload}, nil
}

type deadlineThenSuccessClient struct{ calls int }

func (c *deadlineThenSuccessClient) GenerateStructured(ctx context.Context, _ llm.Request) (llm.Response, error) {
	c.calls++
	if c.calls == 1 {
		<-ctx.Done()
		return llm.Response{}, ctx.Err()
	}
	payload := json.RawMessage(`{"value":"ok"}`)
	return llm.Response{Provider: "test", Model: "test", Raw: string(payload), Text: string(payload), Parsed: payload}, nil
}

func (c *deadlineThenSuccessClient) GenerateText(context.Context, llm.TextRequest) (llm.TextResponse, error) {
	return llm.TextResponse{}, errors.New("unused")
}

func (c *transientStructuredClient) GenerateText(context.Context, llm.TextRequest) (llm.TextResponse, error) {
	return llm.TextResponse{}, errors.New("unused")
}

func TestStructuredStageRetriesOneTransientFailure(t *testing.T) {
	client := &transientStructuredClient{}
	outDir := t.TempDir()
	var target struct {
		Value string `json:"value"`
	}
	err := runStructuredStage(context.Background(), client, outDir, 12, llm.Request{
		Stage: "retry_test", Model: "test", Instructions: "test", Input: `{}`, SchemaName: "RetryTest",
		Schema: object(map[string]any{"value": str()}), Metadata: map[string]string{"template_version": "test"},
	}, &target, nil)
	if err != nil || target.Value != "ok" || client.calls != 2 {
		t.Fatalf("transient retry failed: target=%+v calls=%d err=%v", target, client.calls, err)
	}
	var summary RunSummary
	readJSONTestFile(t, outDir+"/llm_runs/012_retry_test/run.json", &summary)
	if summary.RetryCount != 1 || !summary.ValidationOK || len(summary.Attempts) != 2 || summary.CallGatePolicy != "semantic_need_v1" {
		t.Fatalf("retry was not recorded: %+v", summary)
	}
	var manifest ContextManifest
	readJSONTestFile(t, outDir+"/llm_runs/012_retry_test/context_manifest.json", &manifest)
	if manifest.CallReason != "semantic_generation" || manifest.CallGatePolicy != "semantic_need_v1" || manifest.InputHash == "" {
		t.Fatalf("context manifest is incomplete: %+v", manifest)
	}
}

func TestStructuredStageReservesDeadlineForRetry(t *testing.T) {
	client := &deadlineThenSuccessClient{}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	var target struct {
		Value string `json:"value"`
	}
	err := runStructuredStage(ctx, client, t.TempDir(), 12, llm.Request{
		Stage: "deadline_retry_test", Model: "test", Instructions: "test", Input: `{}`, SchemaName: "RetryTest",
		Schema: object(map[string]any{"value": str()}), Metadata: map[string]string{"template_version": "test"},
	}, &target, nil)
	if err != nil || target.Value != "ok" || client.calls != 2 {
		t.Fatalf("deadline-aware retry failed: target=%+v calls=%d err=%v", target, client.calls, err)
	}
}

func readJSONTestFile(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
}
