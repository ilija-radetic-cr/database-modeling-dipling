package llmpipeline

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

var lowerSnakeIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type BaselineOptions struct {
	TaskPath        string
	OutDir          string
	Target          string
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
}

type stageReport struct {
	OK       bool     `json:"ok"`
	Stage    string   `json:"stage"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

type RunSummary struct {
	Version          int          `json:"version"`
	Stage            string       `json:"stage"`
	Status           string       `json:"status"`
	Provider         string       `json:"provider"`
	Model            string       `json:"model"`
	TemplateVersion  string       `json:"template_version,omitempty"`
	SchemaName       string       `json:"schema_name,omitempty"`
	InputHash        string       `json:"input_hash"`
	ReasoningEffort  string       `json:"reasoning_effort"`
	MaxOutputTokens  int          `json:"max_output_tokens"`
	Usage            llm.Usage    `json:"usage"`
	StartedAt        time.Time    `json:"started_at"`
	CompletedAt      time.Time    `json:"completed_at,omitempty"`
	DurationMS       int64        `json:"duration_ms,omitempty"`
	ValidationOK     bool         `json:"validation_ok"`
	ValidationScope  string       `json:"validation_scope,omitempty"`
	Errors           []string     `json:"errors"`
	Cached           bool         `json:"cached,omitempty"`
	CacheKey         string       `json:"cache_key,omitempty"`
	ContextBytes     int          `json:"context_bytes,omitempty"`
	RetryCount       int          `json:"retry_count,omitempty"`
	CallReason       string       `json:"call_reason,omitempty"`
	IssueID          string       `json:"issue_id,omitempty"`
	FullContextBytes int          `json:"full_context_bytes,omitempty"`
	ContextReduction float64      `json:"context_reduction_ratio,omitempty"`
	BudgetPolicy     string       `json:"budget_policy,omitempty"`
	WastedTokens     int          `json:"wasted_tokens,omitempty"`
	Attempts         []RunAttempt `json:"attempts,omitempty"`
	CallGatePolicy   string       `json:"call_gate_policy,omitempty"`
}

type RunAttempt struct {
	Number          int       `json:"number"`
	Kind            string    `json:"kind"`
	MaxOutputTokens int       `json:"max_output_tokens"`
	Usage           llm.Usage `json:"usage"`
	DurationMS      int64     `json:"duration_ms"`
	Error           string    `json:"error,omitempty"`
}

type ContextManifest struct {
	Version              int                 `json:"version"`
	Stage                string              `json:"stage"`
	CallReason           string              `json:"call_reason"`
	IssueID              string              `json:"issue_id,omitempty"`
	InputHash            string              `json:"input_hash"`
	ContextBytes         int                 `json:"context_bytes"`
	FullContextBytes     int                 `json:"full_context_bytes"`
	ContextReduction     float64             `json:"context_reduction_ratio"`
	BudgetPolicy         string              `json:"budget_policy"`
	MaxOutputTokens      int                 `json:"max_output_tokens"`
	TemplateVersion      string              `json:"template_version,omitempty"`
	CanonicalizerVersion string              `json:"canonicalizer_version"`
	CallGatePolicy       string              `json:"call_gate_policy"`
	Included             map[string][]string `json:"included"`
}

type sourceUnitInput struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Section    string   `json:"section"`
	Relevance  string   `json:"relevance"`
	Tags       []string `json:"tags,omitempty"`
	Exact      string   `json:"exact"`
	Normalized string   `json:"normalized,omitempty"`
}

func RunBaseline(ctx context.Context, client llm.Client, opts BaselineOptions) error {
	if client == nil {
		return errors.New("LLM client is required")
	}
	opts = normalizeBaselineOptions(opts)
	if opts.TaskPath == "" || opts.OutDir == "" {
		return errors.New("task path and output directory are required")
	}
	if opts.Target != "dbml" && opts.Target != "sql" {
		return errors.New("baseline target must be dbml or sql")
	}
	taskBytes, err := os.ReadFile(opts.TaskPath)
	if err != nil {
		return fmt.Errorf("read task: %w", err)
	}
	instructions := baselineDBMLInstructions
	if opts.Target == "sql" {
		instructions = baselineSQLInstructions
	}
	req := llm.TextRequest{
		Stage:           "baseline",
		Model:           opts.Model,
		Instructions:    instructions,
		Input:           string(taskBytes),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": promptTemplateVersion,
			"target":           opts.Target,
		},
	}
	runDir := stageRunDir(opts.OutDir, 1, "baseline_"+opts.Target)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	if err := writeTextFile(filepath.Join(runDir, "prompt.md"), instructions); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(runDir, "request.json"), req.SanitizedLog(providerName(client))); err != nil {
		return err
	}
	resp, err := client.GenerateText(ctx, req)
	if err != nil {
		_ = writeJSONFile(filepath.Join(runDir, "validation_report.json"), stageReport{OK: false, Stage: req.Stage, Errors: []string{err.Error()}})
		return err
	}
	if err := writeTextFile(filepath.Join(runDir, "response.raw.txt"), resp.Raw); err != nil {
		return err
	}
	artifactName := "baseline.dbml"
	if opts.Target == "sql" {
		artifactName = "baseline.sql"
	}
	if err := writeTextFile(filepath.Join(opts.OutDir, artifactName), resp.Text); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(runDir, "validation_report.json"), stageReport{OK: true, Stage: req.Stage})
}

func runStructuredStage[T any](ctx context.Context, client llm.Client, outDir string, number int, req llm.Request, target *T, validateTarget func() []string) error {
	runName := req.Stage
	if key := strings.TrimSpace(req.Metadata["run_key"]); key != "" {
		runName += "_" + strings.NewReplacer("/", "_", "\\", "_", "..", "_").Replace(key)
	}
	req.Input = compactLLMInput(req.Input)
	runDir := stageRunDir(outDir, number, runName)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	if err := writeTextFile(filepath.Join(runDir, "prompt.md"), req.Instructions); err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(runDir, "request.json"), req.SanitizedLog(providerName(client))); err != nil {
		return err
	}
	started := time.Now()
	cacheKey := structuredCacheKey(req)
	callReason := nonEmpty(req.Metadata["call_reason"], "semantic_generation")
	fullContextBytes := metadataInt(req.Metadata, "full_context_bytes", len(req.Input))
	contextReduction := reductionRatio(len(req.Input), fullContextBytes)
	summary := RunSummary{Version: 1, Stage: req.Stage, Status: "running", Provider: providerName(client), Model: nonEmpty(req.Model, llm.DefaultModel),
		TemplateVersion: req.Metadata["template_version"], SchemaName: req.SchemaName, InputHash: textHash(req.Input),
		ReasoningEffort: nonEmpty(req.ReasoningEffort, llm.DefaultReasoningEffort), MaxOutputTokens: req.MaxOutputTokens, StartedAt: started, Errors: []string{},
		CacheKey: cacheKey, ContextBytes: len(req.Input), FullContextBytes: fullContextBytes, ContextReduction: contextReduction,
		CallReason: callReason, IssueID: req.Metadata["issue_id"], BudgetPolicy: nonEmpty(req.Metadata["budget_policy"], "adaptive_v1"),
		ValidationScope: nonEmpty(req.Metadata["validation_scope"], "structured_schema"),
		CallGatePolicy:  nonEmpty(req.Metadata["call_gate_policy"], "semantic_need_v1"), Attempts: []RunAttempt{}}
	_ = writeJSONFile(filepath.Join(runDir, "run.json"), summary)
	_ = writeJSONFile(filepath.Join(runDir, "context_manifest.json"), buildContextManifest(req, callReason, fullContextBytes))
	cachePath := filepath.Join(outDir, "llm_cache", strings.TrimPrefix(cacheKey, "sha256:")+".json")
	cacheEnabled := req.Metadata["cache_policy"] != "full_validation_only" || req.Metadata["validation_scope"] == "full_dbdsl_v05"
	if cacheEnabled {
		if cached, cacheErr := os.ReadFile(cachePath); cacheErr == nil && json.Valid(cached) {
			if json.Unmarshal(cached, target) == nil {
				errors := []string{}
				if validateTarget != nil {
					errors = validateTarget()
				}
				if len(errors) == 0 {
					summary.Cached = true
					_ = writeTextFile(filepath.Join(runDir, "response.raw.txt"), "cache_hit")
					_ = writeTextFile(filepath.Join(runDir, "response.parsed.json"), string(cached))
					_ = writeJSONFile(filepath.Join(runDir, "validation_report.json"), stageReport{OK: true, Stage: req.Stage})
					finishRunSummary(runDir, &summary, "completed", true, llm.Usage{}, nil)
					return nil
				}
			}
		}
	}
	var zero T
	*target = zero
	attemptStarted := time.Now()
	initialAttempts := 2
	if req.Metadata["retry_policy"] == "none" {
		initialAttempts = 1
	}
	resp, err := generateStructuredAttempt(ctx, client, req, initialAttempts)
	summary.Attempts = append(summary.Attempts, runAttempt(1, "initial", req.MaxOutputTokens, resp.Usage, attemptStarted, err))
	if initialAttempts > 1 && err != nil && retryableStructuredError(err) && ctx.Err() == nil {
		summary.RetryCount = 1
		if resp.Raw != "" {
			_ = writeTextFile(filepath.Join(runDir, "response.attempt_001.raw.txt"), resp.Raw)
		}
		if resp.Text != "" {
			_ = writeTextFile(filepath.Join(runDir, "response.attempt_001.partial.txt"), resp.Text)
		}
		retryReq := req
		if strings.Contains(strings.ToLower(err.Error()), "json") {
			retryReq.Instructions += "\nThe previous response was incomplete or invalid JSON. Return one complete object conforming exactly to the supplied schema."
		}
		if strings.Contains(strings.ToLower(err.Error()), "incomplete") || strings.Contains(strings.ToLower(err.Error()), "max_output") {
			retryReq.MaxOutputTokens = bumpedRetryBudget(req.MaxOutputTokens, metadataInt(req.Metadata, "stage_max_output_tokens", defaultStageMaxOutputTokens(req.Stage, req.MaxOutputTokens)))
		}
		attemptStarted = time.Now()
		resp, err = generateStructuredAttempt(ctx, client, retryReq, 1)
		summary.Attempts = append(summary.Attempts, runAttempt(2, "structured_retry", retryReq.MaxOutputTokens, resp.Usage, attemptStarted, err))
	}
	if err != nil {
		if resp.Raw != "" {
			_ = writeTextFile(filepath.Join(runDir, "response.raw.txt"), resp.Raw)
		}
		if resp.Text != "" {
			_ = writeTextFile(filepath.Join(runDir, "response.partial.txt"), resp.Text)
		}
		finishRunSummary(runDir, &summary, "failed", false, resp.Usage, []string{err.Error()})
		_ = writeJSONFile(filepath.Join(runDir, "validation_report.json"), stageReport{OK: false, Stage: req.Stage, Errors: []string{err.Error()}})
		return err
	}
	if err := writeTextFile(filepath.Join(runDir, "response.raw.txt"), resp.Raw); err != nil {
		return err
	}
	if err := writeTextFile(filepath.Join(runDir, "response.parsed.json"), string(resp.Parsed)); err != nil {
		return err
	}
	if err := json.Unmarshal(resp.Parsed, target); err != nil {
		finishRunSummary(runDir, &summary, "failed", false, resp.Usage, []string{err.Error()})
		_ = writeJSONFile(filepath.Join(runDir, "validation_report.json"), stageReport{OK: false, Stage: req.Stage, Errors: []string{err.Error()}})
		return fmt.Errorf("parse %s response: %w", req.Stage, err)
	}
	if validateTarget != nil && validationRetryStages[req.Stage] && ctx.Err() == nil {
		// One feedback round: the model gets the exact backend errors instead of
		// failing the whole job on a single missed reference or coverage gap.
		if errors := validateTarget(); len(errors) > 0 {
			_ = writeTextFile(filepath.Join(runDir, "response.attempt_invalid.raw.txt"), resp.Raw)
			retryReq := req
			retryReq.Instructions += "\nYour previous response failed backend validation with these errors:\n- " + strings.Join(errors, "\n- ") +
				"\nReturn one complete corrected object that fixes every listed error and keeps all other content consistent."
			firstUsage := resp.Usage
			attemptStarted = time.Now()
			retryResp, retryErr := generateStructuredAttempt(ctx, client, retryReq, 1)
			summary.Attempts = append(summary.Attempts, runAttempt(len(summary.Attempts)+1, "validation_retry", retryReq.MaxOutputTokens, retryResp.Usage, attemptStarted, retryErr))
			summary.RetryCount++
			if retryErr == nil {
				var retried T
				if json.Unmarshal(retryResp.Parsed, &retried) == nil {
					*target = retried
					resp = retryResp
					resp.Usage = addUsage(firstUsage, retryResp.Usage)
					_ = writeTextFile(filepath.Join(runDir, "response.raw.txt"), resp.Raw)
					_ = writeTextFile(filepath.Join(runDir, "response.parsed.json"), string(resp.Parsed))
				}
			}
		}
	}
	if validateTarget != nil {
		if errors := validateTarget(); len(errors) > 0 {
			finishRunSummary(runDir, &summary, "failed", false, resp.Usage, errors)
			_ = writeJSONFile(filepath.Join(runDir, "validation_report.json"), stageReport{OK: false, Stage: req.Stage, Errors: errors})
			return fmt.Errorf("%s proposal failed preflight validation: %s", req.Stage, strings.Join(errors, "; "))
		}
	}
	if err := writeJSONFile(filepath.Join(runDir, "validation_report.json"), stageReport{OK: true, Stage: req.Stage}); err != nil {
		finishRunSummary(runDir, &summary, "failed", false, resp.Usage, []string{err.Error()})
		return err
	}
	finishRunSummary(runDir, &summary, "completed", true, resp.Usage, nil)
	if cacheEnabled {
		if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err == nil {
			_ = writeTextFile(cachePath, string(resp.Parsed))
		}
	}
	return nil
}

// validationRetryStages get one error-feedback retry inside runStructuredStage.
// Conceptual and logical stages are excluded because they run their own
// scoped repair loops over the full model.
var validationRetryStages = map[string]bool{
	"source_segmentation": true, "conceptual_description": true,
}

func addUsage(a, b llm.Usage) llm.Usage {
	return llm.Usage{InputTokens: a.InputTokens + b.InputTokens, OutputTokens: a.OutputTokens + b.OutputTokens, TotalTokens: a.TotalTokens + b.TotalTokens}
}

func retryableStructuredError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	// Billing and quota errors are permanent until the account changes; retrying only delays the failure.
	for _, marker := range []string{"no credits", "insufficient_quota", "exceeded your current quota", "billing"} {
		if strings.Contains(message, marker) {
			return false
		}
	}
	for _, marker := range []string{"timeout", "deadline exceeded", "temporarily", "connection reset", "eof", "429", "502", "503", "504", "not valid json", "incomplete"} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func generateStructuredAttempt(ctx context.Context, client llm.Client, req llm.Request, attemptsRemaining int) (llm.Response, error) {
	if attemptsRemaining <= 1 {
		return client.GenerateStructured(ctx, req)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return client.GenerateStructured(ctx, req)
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return llm.Response{}, context.DeadlineExceeded
	}
	budget := remaining / time.Duration(attemptsRemaining)
	attemptCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	return client.GenerateStructured(attemptCtx, req)
}

func structuredCacheKey(req llm.Request) string {
	payload, _ := json.Marshal(map[string]any{
		"stage": req.Stage, "model": nonEmpty(req.Model, llm.DefaultModel), "instructions": req.Instructions,
		"input": req.Input, "schema_name": req.SchemaName, "schema": req.Schema,
		"reasoning_effort": nonEmpty(req.ReasoningEffort, llm.DefaultReasoningEffort),
		"temperature":      req.Temperature, "max_output_tokens": req.MaxOutputTokens,
		"template_version": req.Metadata["template_version"],
		"context_policy":   nonEmpty(req.Metadata["context_policy"], "minimal_context_v1"), "call_gate_policy": nonEmpty(req.Metadata["call_gate_policy"], "semantic_need_v1"),
		"canonicalizer_version": req.Metadata["canonicalizer_version"], "risk_policy": req.Metadata["risk_policy"],
		"compiler_version": req.Metadata["compiler_version"], "verifier_version": req.Metadata["verifier_version"],
	})
	return textHash(string(payload))
}

func finishRunSummary(runDir string, summary *RunSummary, status string, validationOK bool, usage llm.Usage, errors []string) {
	summary.Status = status
	summary.ValidationOK = validationOK
	if len(summary.Attempts) == 0 {
		summary.Usage = usage
	} else {
		summary.Usage = aggregateAttemptUsage(summary.Attempts)
		for _, attempt := range summary.Attempts {
			if attempt.Error != "" {
				summary.WastedTokens += attempt.Usage.TotalTokens
			}
		}
		if status == "failed" {
			summary.WastedTokens = summary.Usage.TotalTokens
		}
	}
	summary.Errors = append([]string(nil), errors...)
	summary.CompletedAt = time.Now()
	summary.DurationMS = summary.CompletedAt.Sub(summary.StartedAt).Milliseconds()
	_ = writeJSONFile(filepath.Join(runDir, "run.json"), summary)
}

func runAttempt(number int, kind string, maxOutputTokens int, usage llm.Usage, started time.Time, err error) RunAttempt {
	attempt := RunAttempt{Number: number, Kind: kind, MaxOutputTokens: maxOutputTokens, Usage: usage, DurationMS: time.Since(started).Milliseconds()}
	if err != nil {
		attempt.Error = err.Error()
	}
	return attempt
}

func aggregateAttemptUsage(attempts []RunAttempt) llm.Usage {
	var usage llm.Usage
	for _, attempt := range attempts {
		usage.InputTokens += attempt.Usage.InputTokens
		usage.OutputTokens += attempt.Usage.OutputTokens
		usage.TotalTokens += attempt.Usage.TotalTokens
	}
	return usage
}

func bumpedRetryBudget(current, ceiling int) int {
	if current <= 0 {
		current = llm.DefaultMaxOutputTokens
	}
	if ceiling < current {
		ceiling = current
	}
	bumped := current + current/2
	if bumped > ceiling {
		return ceiling
	}
	return bumped
}

func defaultStageMaxOutputTokens(stage string, fallback int) int {
	switch stage {
	case "source_segmentation", "source_unit_extraction", "requirement_atom_extraction":
		return 16000
	case "functional_analysis":
		return 12000
	case "crud_mapping":
		return 16000
	case "review_candidate_proposal":
		return 14000
	case "conceptual_model":
		return 32000
	case "logical_projection":
		return 40000
	default:
		if fallback > 0 {
			return fallback
		}
		return 20000
	}
}

func metadataInt(metadata map[string]string, key string, fallback int) int {
	if metadata == nil {
		return fallback
	}
	var value int
	if _, err := fmt.Sscan(metadata[key], &value); err != nil || value <= 0 {
		return fallback
	}
	return value
}

func reductionRatio(sent, full int) float64 {
	if full <= 0 || sent >= full {
		return 0
	}
	return float64(full-sent) / float64(full)
}

func buildContextManifest(req llm.Request, callReason string, fullContextBytes int) ContextManifest {
	return ContextManifest{
		Version: 1, Stage: req.Stage, CallReason: callReason, IssueID: req.Metadata["issue_id"], InputHash: textHash(req.Input),
		ContextBytes: len(req.Input), FullContextBytes: fullContextBytes, ContextReduction: reductionRatio(len(req.Input), fullContextBytes),
		BudgetPolicy: nonEmpty(req.Metadata["budget_policy"], "adaptive_v1"), MaxOutputTokens: req.MaxOutputTokens,
		TemplateVersion: req.Metadata["template_version"], CanonicalizerVersion: nonEmpty(req.Metadata["canonicalizer_version"], "pipeline_ids_v2"),
		CallGatePolicy: nonEmpty(req.Metadata["call_gate_policy"], "semantic_need_v1"),
		Included:       summarizeContextInput(req.Input),
	}
}

func summarizeContextInput(input string) map[string][]string {
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(input), &root) != nil {
		return map[string][]string{}
	}
	out := map[string][]string{}
	for key, raw := range root {
		var items []map[string]any
		if json.Unmarshal(raw, &items) != nil {
			continue
		}
		ids := make([]string, 0, len(items))
		for _, item := range items {
			if id, ok := item["id"].(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			out[key] = ids
		}
	}
	return out
}

func textHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum[:])
}

type artifacts struct {
	ReviewDecisions dsl.ReviewDecisionsFile
	Model           dsl.Document
}

type conversionDiagnostics struct {
	Warnings []string
}

// buildArtifacts turns a validated patch into a DB-DSL v0.6 bundle: the model
// document plus the review-decision record; evidence cites source units only.
func buildArtifacts(taskName string, sourceUnits []dsl.SourceUnit, patch PatchProposal, diagnostics *conversionDiagnostics) (artifacts, error) {
	if len(sourceUnits) == 0 {
		return artifacts{}, errors.New("no source units to trace the model against")
	}
	model := dsl.Document{
		DSL: dsl.DSLMeta{Name: "DB-DSL", Version: "0.6"},
		Model: dsl.ModelInfo{
			ID:          slug(strings.TrimSuffix(taskName, filepath.Ext(taskName))),
			Name:        llmDraftName(strings.TrimSuffix(taskName, filepath.Ext(taskName))),
			DomainSlice: slug(strings.TrimSuffix(taskName, filepath.Ext(taskName))),
			Status:      "llm_draft_requires_review",
			Description: "LLM-assisted v0.6 logical database model draft. Validate, lint and review before treating as final.",
		},
		Source: dsl.SourceInfo{
			PipelineVersion:     PipelineVersion,
			TaskTextFile:        "TASK.md",
			SourceUnitsFile:     "source_units.yaml",
			ReviewDecisionsFile: "review_decisions.yaml",
			ReviewState:         "llm_draft_requires_review",
			DerivationStrategy:  "segment_description_v1",
		},
		ImportSpecs:   []dsl.ImportSpec{},
		StateMachines: []dsl.StateMachine{},
		DerivedViews:  []dsl.DerivedView{},
		FileSpecs:     []dsl.FileSpec{},
	}

	if err := applyPatch(&model, patch, diagnostics); err != nil {
		return artifacts{}, err
	}
	normalizeModelReferences(&model, diagnostics)

	return artifacts{
		ReviewDecisions: dsl.ReviewDecisionsFile{
			Document: map[string]any{
				"id":                  model.Model.ID + "_review_decisions",
				"title":               model.Model.Name + " review decisions",
				"pipeline_version":    PipelineVersion,
				"generation_strategy": "segment_description_v1",
			},
			ReviewState: map[string]any{
				"status":                           "llm_draft_requires_review",
				"all_required_reviews_resolved":    true,
				"unresolved_requires_review_flags": 0,
			},
			ReviewDecisions: []dsl.ReviewDecision{},
			CoverageChecks: []map[string]any{{
				"id":     "no_resolved_decisions_claimed",
				"status": "passed",
				"note":   "The pipeline does not fabricate resolved human review decisions.",
			}},
		},
		Model: model,
	}, nil
}

func applyPatch(model *dsl.Document, patch PatchProposal, diagnostics *conversionDiagnostics) error {
	seenEntities := map[string]bool{}
	for _, operation := range patch.Operations {
		if operation.Operation == "add_entity" {
			if operation.Entity == nil {
				return errors.New("add_entity operation is missing entity")
			}
			entity := convertEntity(*operation.Entity, diagnostics)
			if entity.ID == "" {
				return errors.New("add_entity operation produced empty entity id")
			}
			if seenEntities[entity.ID] {
				return fmt.Errorf("duplicate entity id %s in patch", entity.ID)
			}
			seenEntities[entity.ID] = true
			model.Entities = append(model.Entities, entity)
		}
	}
	if len(model.Entities) == 0 {
		return errors.New("DB-DSL patch produced no entities")
	}
	for _, operation := range patch.Operations {
		switch operation.Operation {
		case "add_entity":
			continue
		case "add_relationship":
			if operation.Relationship == nil {
				return errors.New("add_relationship operation is missing relationship")
			}
			model.Relationships = append(model.Relationships, convertRelationship(*operation.Relationship, diagnostics))
		case "add_constraint":
			if operation.Constraint == nil {
				return errors.New("add_constraint operation is missing constraint")
			}
			model.Constraints = append(model.Constraints, convertConstraint(*operation.Constraint, diagnostics))
		case "add_state_machine":
			if operation.StateMachine == nil {
				return errors.New("add_state_machine operation is missing state_machine")
			}
			model.StateMachines = append(model.StateMachines, convertStateMachine(*operation.StateMachine, diagnostics))
		case "add_derived_view":
			if operation.DerivedView == nil {
				return errors.New("add_derived_view operation is missing derived_view")
			}
			model.DerivedViews = append(model.DerivedViews, convertDerivedView(*operation.DerivedView, diagnostics))
		case "add_file_spec":
			if operation.FileSpec == nil {
				return errors.New("add_file_spec operation is missing file_spec")
			}
			model.FileSpecs = append(model.FileSpecs, convertFileSpec(*operation.FileSpec, diagnostics))
		case "add_import_spec":
			if operation.ImportSpec == nil {
				return errors.New("add_import_spec operation is missing import_spec")
			}
			model.ImportSpecs = append(model.ImportSpecs, convertImportSpec(*operation.ImportSpec, diagnostics))
		case "add_index":
			if operation.Index == nil {
				return errors.New("add_index operation is missing index")
			}
			model.Indexes = append(model.Indexes, dsl.Index{
				ID: operation.Index.ID, Owner: operation.Index.Owner, Fields: operation.Index.Fields,
				Description: nonEmpty(operation.Index.Description, "Search index."),
				Evidence:    convertEvidence(operation.Index.Evidence, diagnostics),
			})
		case "remove_operation":
			return errors.New("remove_operation is repair-only and must be merged before artifact conversion")
		default:
			return fmt.Errorf("unsupported patch operation %s", operation.Operation)
		}
	}
	return nil
}

func convertEntity(proposal EntityProposal, diagnostics *conversionDiagnostics) dsl.Entity {
	entityEvidence := convertEvidence(proposal.Evidence, diagnostics)
	kind := nonEmpty(proposal.Kind, "regular")
	attrs := make([]dsl.Attribute, 0, len(proposal.Attributes))
	for _, attr := range proposal.Attributes {
		attrID := normalizeAttributeID(proposal.ID, attr, diagnostics)
		if shouldDropGeneratedIDAttribute(kind, proposal.ID, attr, attrID) {
			diagnostics.add(fmt.Sprintf("dropped generated primary key attribute %s.%s because %s entities receive an id in DBML generation", proposal.ID, attr.ID, kind))
			continue
		}
		required := attr.Required
		evidence := convertEvidence(attr.Evidence, diagnostics)
		if len(evidence.SourceUnits) == 0 {
			evidence = entityEvidence
		}
		attrs = append(attrs, dsl.Attribute{
			ID:          attrID,
			Label:       nonEmpty(attr.Label, titleFromID(attrID)),
			Description: nonEmpty(attr.Description, "LLM proposed attribute."),
			Type:        nonEmpty(attr.Type, "string"),
			Required:    &required,
			Precision:   attr.Precision,
			Scale:       attr.Scale,
			Default:     attr.Default,
			SourceField: attr.SourceField,
			EnumValues:  attr.EnumValues,
			Notes:       attr.Notes,
			Evidence:    evidence,
		})
	}
	if len(attrs) == 0 {
		diagnostics.add("entity " + proposal.ID + " had no scalar attributes; no unsupported fallback attribute was fabricated")
	}
	return dsl.Entity{
		ID:          proposal.ID,
		Label:       nonEmpty(proposal.Label, titleFromID(proposal.ID)),
		Description: nonEmpty(proposal.Description, "LLM proposed entity."),
		TableName:   nonEmpty(proposal.TableName, pluralSnake(proposal.ID)),
		Kind:        kind,
		Evidence:    entityEvidence,
		Attributes:  attrs,
	}
}

func normalizeAttributeID(entityID string, attr AttributeProposal, diagnostics *conversionDiagnostics) string {
	if lowerSnakeIdentifierPattern.MatchString(attr.ID) {
		return attr.ID
	}
	normalized := bestAttributeID(attr)
	if normalized == "" {
		normalized = "attribute"
	}
	if attr.ID == "" {
		diagnostics.add(fmt.Sprintf("normalized empty attribute id on %s to %s", entityID, normalized))
	} else if normalized != attr.ID {
		diagnostics.add(fmt.Sprintf("normalized attribute %s.%s id to %s", entityID, attr.ID, normalized))
	}
	return normalized
}

func bestAttributeID(attr AttributeProposal) string {
	for _, candidate := range []string{attr.SourceField, attr.Label, attr.ID} {
		normalized := attributeSlug(candidate)
		if normalized != "" {
			return normalized
		}
	}
	return ""
}

func shouldDropGeneratedIDAttribute(entityKind, entityID string, attr AttributeProposal, attrID string) bool {
	if entityKind == "association" {
		return false
	}
	attrRef := normalizeRef(attrID)
	if attrRef == "id" {
		return true
	}
	if attr.Type != "id" || strings.TrimSpace(attr.SourceField) != "" {
		return false
	}
	if attrRef == normalizeRef(entityID)+"_id" {
		return true
	}
	return strings.HasSuffix(attrRef, "_id")
}

func convertRelationship(proposal RelationshipProposal, diagnostics *conversionDiagnostics) dsl.Relationship {
	required := proposal.Required
	fkRequired := proposal.FKRequired
	identifying := proposal.Identifying
	return dsl.Relationship{
		ID:          proposal.ID,
		Label:       nonEmpty(proposal.Label, titleFromID(proposal.ID)),
		Description: nonEmpty(proposal.Description, "LLM proposed relationship."),
		From:        proposal.From,
		To:          proposal.To,
		Cardinality: nonEmpty(proposal.Cardinality, "many_to_one"),
		Required:    &required,
		FKRequired:  &fkRequired,
		OnDelete:    proposal.OnDelete,
		Identifying: &identifying,
		Through:     proposal.Through,
		Notes:       proposal.Notes,
		Evidence:    convertEvidence(proposal.Evidence, diagnostics),
	}
}

func convertConstraint(proposal ConstraintProposal, diagnostics *conversionDiagnostics) dsl.Constraint {
	return dsl.Constraint{
		ID:          proposal.ID,
		Type:        proposal.Type,
		Owner:       proposal.Owner,
		Field:       proposal.Field,
		Fields:      proposal.Fields,
		Value:       proposal.Value,
		Min:         proposal.Min,
		Max:         proposal.Max,
		Pattern:     proposal.Pattern,
		Expression:  proposal.Expression,
		Description: nonEmpty(proposal.Description, "LLM proposed constraint."),
		Evidence:    convertEvidence(proposal.Evidence, diagnostics),
	}
}

func convertStateMachine(proposal StateMachineProposal, diagnostics *conversionDiagnostics) dsl.StateMachine {
	return dsl.StateMachine{
		ID:          proposal.ID,
		Owner:       proposal.Owner,
		Field:       proposal.Field,
		States:      proposal.States,
		Initial:     proposal.Initial,
		Terminal:    proposal.Terminal,
		Transitions: proposal.Transitions,
		Notes:       proposal.Notes,
		Evidence:    convertEvidence(proposal.Evidence, diagnostics),
	}
}

func convertDerivedView(proposal DerivedViewProposal, diagnostics *conversionDiagnostics) dsl.DerivedView {
	return dsl.DerivedView{
		ID:          proposal.ID,
		Label:       nonEmpty(proposal.Label, titleFromID(proposal.ID)),
		Description: nonEmpty(proposal.Description, "LLM proposed derived view."),
		Kind:        nonEmpty(proposal.Kind, "projection"),
		Sources:     proposal.Sources,
		Persistence: nonEmpty(proposal.Persistence, "virtual"),
		Metrics:     proposal.Metrics,
		Filters:     proposal.Filters,
		Notes:       proposal.Notes,
		Evidence:    convertEvidence(proposal.Evidence, diagnostics),
	}
}

func convertFileSpec(proposal FileSpecProposal, diagnostics *conversionDiagnostics) dsl.FileSpec {
	return dsl.FileSpec{
		ID:                proposal.ID,
		Owner:             proposal.Owner,
		Field:             proposal.Field,
		AllowedExtensions: proposal.AllowedExtensions,
		MaxSizeMB:         proposal.MaxSizeMB,
		MIMETypes:         proposal.MIMETypes,
		Storage:           nonEmpty(proposal.Storage, "external_reference"),
		Notes:             proposal.Notes,
		Evidence:          convertEvidence(proposal.Evidence, diagnostics),
	}
}

func convertImportSpec(proposal ImportSpecProposal, diagnostics *conversionDiagnostics) dsl.ImportSpec {
	mappings := make([]dsl.ImportMapping, 0, len(proposal.Mappings))
	for _, mapping := range proposal.Mappings {
		mappings = append(mappings, dsl.ImportMapping{
			SourcePath: mapping.SourcePath,
			Target:     mapping.Target,
			Notes:      mapping.Notes,
		})
	}
	return dsl.ImportSpec{
		ID:          proposal.ID,
		Label:       nonEmpty(proposal.Label, titleFromID(proposal.ID)),
		Description: nonEmpty(proposal.Description, "LLM proposed import spec."),
		Format:      proposal.Format,
		Source: dsl.ImportSource{
			Fragment:    proposal.Source.Fragment,
			SourceID:    proposal.Source.SourceID,
			File:        proposal.Source.File,
			SourceUnits: proposal.Source.SourceUnits,
		},
		Root:     proposal.Root,
		Mappings: mappings,
		Evidence: convertEvidence(proposal.Evidence, diagnostics),
	}
}

func convertEvidence(proposal EvidenceProposal, diagnostics *conversionDiagnostics) dsl.Evidence {
	evidence := dsl.Evidence{
		SourceUnits:     append([]string(nil), proposal.SourceUnits...),
		ReviewDecisions: append([]string(nil), proposal.ReviewDecisions...),
		SupportLevel:    nonEmpty(proposal.SupportLevel, "inferred"),
		Confidence:      nonEmpty(proposal.Confidence, "medium"),
		Notes:           append([]string(nil), proposal.Notes...),
	}
	if evidence.SupportLevel == "assumption" && len(evidence.ReviewDecisions) == 0 {
		evidence.SupportLevel = "inferred"
		evidence.Notes = append(evidence.Notes, "support_level downgraded from assumption because no resolved review decision was provided")
		diagnostics.add("assumption evidence without resolved review decision was downgraded to inferred")
	}
	return evidence
}

func normalizeModelReferences(model *dsl.Document, diagnostics *conversionDiagnostics) {
	entityByID := map[string]dsl.Entity{}
	for _, entity := range model.Entities {
		entityByID[entity.ID] = entity
	}
	for i := range model.Constraints {
		constraint := &model.Constraints[i]
		if constraint.Owner == "" || constraint.Owner == "model" {
			continue
		}
		entity, ok := entityByID[constraint.Owner]
		if !ok {
			if normalizedOwner, found := findEntityID(entityByID, constraint.Owner); found {
				diagnostics.add(fmt.Sprintf("normalized constraint %s owner from %s to %s", constraint.ID, constraint.Owner, normalizedOwner))
				constraint.Owner = normalizedOwner
				entity = entityByID[normalizedOwner]
				ok = true
			}
		}
		if !ok {
			continue
		}
		if constraint.Field != "" {
			if normalizedField, found := findAttributeID(entity, constraint.Field); found && normalizedField != constraint.Field {
				diagnostics.add(fmt.Sprintf("normalized constraint %s field from %s to %s", constraint.ID, constraint.Field, normalizedField))
				constraint.Field = normalizedField
			}
		}
		for fieldIndex, field := range constraint.Fields {
			if normalizedField, found := findAttributeID(entity, field); found && normalizedField != field {
				diagnostics.add(fmt.Sprintf("normalized constraint %s fields[%d] from %s to %s", constraint.ID, fieldIndex, field, normalizedField))
				constraint.Fields[fieldIndex] = normalizedField
			}
		}
	}
	applyRequiredConstraintsToAttributes(model, diagnostics)
}

func applyRequiredConstraintsToAttributes(model *dsl.Document, diagnostics *conversionDiagnostics) {
	for _, constraint := range model.Constraints {
		if constraint.Type != "required" || constraint.Owner == "" || constraint.Owner == "model" || constraint.Field == "" {
			continue
		}
		attribute := modelAttribute(model, constraint.Owner, constraint.Field)
		if attribute == nil || (attribute.Required != nil && *attribute.Required) {
			continue
		}
		required := true
		attribute.Required = &required
		diagnostics.add(fmt.Sprintf("set attribute %s.%s required=true from required constraint %s", constraint.Owner, constraint.Field, constraint.ID))
	}
}

func modelAttribute(model *dsl.Document, entityID, attributeID string) *dsl.Attribute {
	for entityIndex := range model.Entities {
		if model.Entities[entityIndex].ID != entityID {
			continue
		}
		for attrIndex := range model.Entities[entityIndex].Attributes {
			if model.Entities[entityIndex].Attributes[attrIndex].ID == attributeID {
				return &model.Entities[entityIndex].Attributes[attrIndex]
			}
		}
	}
	return nil
}

func findEntityID(entityByID map[string]dsl.Entity, ref string) (string, bool) {
	var matches []string
	refNorm := normalizeRef(ref)
	for id, entity := range entityByID {
		if normalizeRef(id) == refNorm || normalizeRef(entity.TableName) == refNorm || normalizeRef(entity.Label) == refNorm {
			matches = append(matches, id)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func findAttributeID(entity dsl.Entity, ref string) (string, bool) {
	var matches []string
	refNorm := normalizeRef(ref)
	refLoose := looseSlug(ref)
	for _, attribute := range entity.Attributes {
		attributeNorm := normalizeRef(attribute.ID)
		attributeLoose := looseSlug(attribute.ID)
		if attributeNorm == refNorm ||
			normalizeRef(attribute.SourceField) == refNorm ||
			normalizeRef(attribute.Label) == refNorm ||
			strings.HasSuffix(attributeNorm, "_"+refNorm) ||
			strings.HasSuffix(refNorm, "_"+attributeNorm) ||
			(refLoose != "" && attributeLoose != "" && attributeLoose == refLoose) ||
			(refLoose != "" && attributeLoose != "" && strings.HasSuffix(refLoose, "_"+attributeLoose)) ||
			(refLoose != "" && looseSlug(attribute.SourceField) == refLoose) ||
			(refLoose != "" && looseSlug(attribute.Label) == refLoose) {
			matches = append(matches, attribute.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], true
	}
	return "", false
}

func normalizeRef(value string) string {
	return strings.Trim(slug(value), "_")
}

func attributeSlug(value string) string {
	if strings.ContainsAny(value, "-_ .:/\\") {
		return looseSlug(value)
	}
	return stableSlug(value)
}

func looseSlug(value string) string {
	value = strings.TrimSpace(value)
	var out []rune
	lastUnderscore := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+'a'-'A')
			lastUnderscore = false
		case r >= '0' && r <= '9':
			out = append(out, r)
			lastUnderscore = false
		default:
			if !lastUnderscore && len(out) > 0 {
				out = append(out, '_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(string(out), "_")
}

func normalizeBaselineOptions(opts BaselineOptions) BaselineOptions {
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}
	if opts.Target == "" {
		opts.Target = "dbml"
	}
	return opts
}

func stageRunDir(outDir string, number int, stage string) string {
	base := filepath.Join(outDir, "llm_runs", fmt.Sprintf("%03d_%s", number, stage))
	if _, err := os.Stat(base); os.IsNotExist(err) {
		return base
	}
	for attempt := 2; ; attempt++ {
		candidate := fmt.Sprintf("%s_attempt_%03d", base, attempt)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

func providerName(client llm.Client) string {
	switch client.(type) {
	case *llm.OpenAIClient:
		return "openai"
	case *llm.MockClient:
		return "mock"
	default:
		return "unknown"
	}
}

// compactLLMInput removes indentation and empty values ("" / [] / {} / null)
// from a JSON request payload. Empty fields carry no information for the model
// but are paid for as input tokens on every call.
func compactLLMInput(input string) string {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return input
	}
	pruned := pruneEmptyJSON(value)
	if pruned == nil {
		return input
	}
	var out strings.Builder
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(pruned) != nil {
		return input
	}
	return strings.TrimSpace(out.String())
}

func pruneEmptyJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			if pruned := pruneEmptyJSON(item); pruned != nil {
				out[key] = pruned
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			if pruned := pruneEmptyJSON(item); pruned != nil {
				out = append(out, pruned)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return typed
	case nil:
		return nil
	default:
		return typed
	}
}

func (d *conversionDiagnostics) add(message string) {
	if d != nil {
		d.Warnings = append(d.Warnings, message)
	}
}

func appendUnique(values []string, value string) []string {
	if value == "" || contains(values, value) {
		return values
	}
	return append(values, value)
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func writeTextFile(path, value string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(value), 0o644)
}

func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeTextFile(path, string(data))
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func llmDraftName(title string) string {
	title = strings.TrimSpace(title)
	lower := strings.ToLower(title)
	switch {
	case title == "":
		return "LLM Draft"
	case strings.HasSuffix(lower, "llm draft"):
		return title
	case strings.HasSuffix(lower, "llm"):
		return title + " Draft"
	default:
		return title + " LLM Draft"
	}
}

func titleFromID(id string) string {
	id = strings.ReplaceAll(id, "_", " ")
	if id == "" {
		return "Untitled"
	}
	parts := strings.Fields(id)
	for i, part := range parts {
		if len(part) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func pluralSnake(id string) string {
	name := slug(id)
	if strings.HasSuffix(name, "s") {
		return name
	}
	return name + "s"
}

func slug(value string) string {
	result := stableSlug(value)
	if result == "" {
		return fmt.Sprintf("llm_model_%d", time.Now().Unix())
	}
	return result
}

func stableSlug(value string) string {
	value = strings.TrimSpace(value)
	var out []rune
	lastUnderscore := false
	for i, r := range value {
		if i > 0 && r >= 'A' && r <= 'Z' && !lastUnderscore {
			out = append(out, '_')
		}
		switch {
		case r >= 'a' && r <= 'z':
			out = append(out, r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+'a'-'A')
			lastUnderscore = false
		case r >= '0' && r <= '9':
			out = append(out, r)
			lastUnderscore = false
		default:
			if !lastUnderscore && len(out) > 0 {
				out = append(out, '_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(string(out), "_")
}
