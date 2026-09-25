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
	"sort"
	"strings"
	"time"

	"dbdsl/internal/dsl"
	"dbdsl/internal/generate"
	"dbdsl/internal/lint"
	"dbdsl/internal/llm"
	"dbdsl/internal/scaffold"
	"dbdsl/internal/validate"

	"gopkg.in/yaml.v3"
)

var lowerSnakeIdentifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type PlanOptions struct {
	TaskPath          string
	OutDir            string
	ModelID           string
	Name              string
	Model             string
	ReasoningEffort   string
	Temperature       float64
	MaxOutputTokens   int
	MaxRepairAttempts int
}

type PlanResult struct {
	OutDir           string
	ModelPath        string
	DBMLPath         string
	TracePath        string
	ValidationReport validate.Result
	LintReport       lint.Result
	GeneratedDBML    bool
}

type RepairOptions struct {
	BundleDir       string
	Issue           string
	OutDir          string
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
}

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

func RunPlan(ctx context.Context, client llm.Client, opts PlanOptions) (PlanResult, error) {
	if client == nil {
		return PlanResult{}, errors.New("LLM client is required")
	}
	opts = normalizePlanOptions(opts)
	if opts.TaskPath == "" || opts.OutDir == "" {
		return PlanResult{}, errors.New("task path and output directory are required")
	}

	taskTitle := strings.TrimSuffix(filepath.Base(opts.TaskPath), filepath.Ext(opts.TaskPath))
	if strings.TrimSpace(opts.Name) != "" {
		taskTitle = strings.TrimSpace(opts.Name)
	}
	taskBytes, err := os.ReadFile(opts.TaskPath)
	if err != nil {
		return PlanResult{}, fmt.Errorf("read task: %w", err)
	}
	if _, err := scaffold.BundleFromTask(opts.TaskPath, opts.OutDir, scaffold.Options{ModelID: opts.ModelID, Name: opts.Name}); err != nil {
		return PlanResult{}, fmt.Errorf("create deterministic source scaffold: %w", err)
	}
	modelPath := filepath.Join(opts.OutDir, "db_model.dsl.yaml")
	sourceBundle, err := dsl.LoadV05Bundle(modelPath)
	if err != nil {
		return PlanResult{}, fmt.Errorf("load source scaffold: %w", err)
	}

	sourceUnits := sourceBundle.SourceUnits.SourceUnits
	sourceIDs := sourceUnitIDSet(sourceUnits)
	sourceInput := sourceUnitInputs(sourceUnits)

	extractionInput := mustJSON(map[string]any{
		"task_file":        filepath.Base(opts.TaskPath),
		"source_units":     sourceInput,
		"pipeline_version": "0.5",
		"output_contract":  "requirement_extraction",
		"template_version": promptTemplateVersion,
	})
	var extraction RequirementExtractionProposal
	if err := runStructuredStage(ctx, client, opts.OutDir, 1, llm.Request{
		Stage:           "requirement_extraction",
		Model:           opts.Model,
		Instructions:    requirementExtractionInstructions,
		Input:           extractionInput,
		SchemaName:      "DBDSLRequirementExtraction",
		Schema:          requirementExtractionSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": promptTemplateVersion,
			"task_file":        filepath.Base(opts.TaskPath),
		},
	}, &extraction, func() []string {
		return validateExtractionProposal(extraction, sourceIDs)
	}); err != nil {
		return PlanResult{}, err
	}
	if err := writeJSONFile(filepath.Join(opts.OutDir, "requirement_atoms.proposed.json"), extraction); err != nil {
		return PlanResult{}, err
	}

	atomIDs := atomIDSet(extraction.RequirementAtoms)
	modelPlanInput := mustJSON(map[string]any{
		"source_units":           sourceInput,
		"requirement_extraction": extraction,
		"output_contract":        "model_plan",
		"template_version":       promptTemplateVersion,
	})
	var modelPlan ModelPlanProposal
	if err := runStructuredStage(ctx, client, opts.OutDir, 2, llm.Request{
		Stage:           "model_plan",
		Model:           opts.Model,
		Instructions:    modelPlanInstructions,
		Input:           modelPlanInput,
		SchemaName:      "DBDSLModelPlan",
		Schema:          modelPlanSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": promptTemplateVersion,
			"task_file":        filepath.Base(opts.TaskPath),
		},
	}, &modelPlan, func() []string {
		return validateModelPlanProposal(modelPlan, sourceIDs, atomIDs)
	}); err != nil {
		return PlanResult{}, err
	}
	if err := writeJSONFile(filepath.Join(opts.OutDir, "model_plan.proposed.json"), modelPlan); err != nil {
		return PlanResult{}, err
	}

	patchInput := mustJSON(map[string]any{
		"source_units":           sourceInput,
		"requirement_extraction": extraction,
		"model_plan":             modelPlan,
		"output_contract":        "dbdsl_patch",
		"template_version":       promptTemplateVersion,
	})
	var patch PatchProposal
	if err := runStructuredStage(ctx, client, opts.OutDir, 3, llm.Request{
		Stage:           "dbdsl_patch",
		Model:           opts.Model,
		Instructions:    patchInstructions,
		Input:           patchInput,
		SchemaName:      "DBDSLPatch",
		Schema:          patchSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": promptTemplateVersion,
			"task_file":        filepath.Base(opts.TaskPath),
		},
	}, &patch, func() []string {
		return validatePatchProposal(patch, sourceIDs, atomIDs)
	}); err != nil {
		return PlanResult{}, err
	}
	if err := writeJSONFile(filepath.Join(opts.OutDir, "dbdsl_patch.proposed.json"), patch); err != nil {
		return PlanResult{}, err
	}

	diagnostics := &conversionDiagnostics{}
	artifacts, err := buildArtifacts(string(taskBytes), taskTitle, sourceUnits, extraction, patch, diagnostics)
	if err != nil {
		return PlanResult{}, err
	}
	if err := writeArtifacts(opts.OutDir, artifacts); err != nil {
		return PlanResult{}, err
	}

	validationReport := validate.ValidateFile(modelPath)
	lintReport := lint.Result{Version: lint.VersionV05}
	if validationReport.OK() {
		lintReport = lint.LintFile(modelPath)
	}
	if opts.MaxRepairAttempts > 0 {
		if issueID, issueText := firstRepairIssue(validationReport, lintReport); issueText != "" {
			if err := writeRepairSuggestion(ctx, client, opts.OutDir, opts, modelPath, issueID, issueText, validationReport, lintReport); err != nil {
				diagnostics.add("repair suggestion failed: " + err.Error())
			}
		}
	}
	if err := writeJSONFile(filepath.Join(opts.OutDir, "validation_report.json"), map[string]any{
		"ok":       validationReport.OK(),
		"errors":   validationReport.Errors,
		"warnings": diagnostics.Warnings,
	}); err != nil {
		return PlanResult{}, err
	}
	if err := writeJSONFile(filepath.Join(opts.OutDir, "lint_report.json"), lintReport); err != nil {
		return PlanResult{}, err
	}

	result := PlanResult{
		OutDir:           opts.OutDir,
		ModelPath:        modelPath,
		ValidationReport: validationReport,
		LintReport:       lintReport,
	}
	if validationReport.OK() && !lintReport.HasErrors() {
		dbml, err := generate.DBMLFile(modelPath)
		if err != nil {
			return result, fmt.Errorf("generate DBML from LLM bundle: %w", err)
		}
		trace, err := generate.TraceFile(modelPath)
		if err != nil {
			return result, fmt.Errorf("generate trace report from LLM bundle: %w", err)
		}
		result.DBMLPath = filepath.Join(opts.OutDir, "model.dbml")
		result.TracePath = filepath.Join(opts.OutDir, "traceability_report.md")
		if err := os.WriteFile(result.DBMLPath, []byte(dbml), 0o644); err != nil {
			return result, fmt.Errorf("write DBML: %w", err)
		}
		if err := os.WriteFile(result.TracePath, []byte(trace), 0o644); err != nil {
			return result, fmt.Errorf("write trace report: %w", err)
		}
		result.GeneratedDBML = true
	}
	return result, nil
}

func RunRepair(ctx context.Context, client llm.Client, opts RepairOptions) error {
	if client == nil {
		return errors.New("LLM client is required")
	}
	opts = normalizeRepairOptions(opts)
	if opts.BundleDir == "" || opts.Issue == "" || opts.OutDir == "" {
		return errors.New("bundle directory, issue, and output directory are required")
	}
	modelPath := filepath.Join(opts.BundleDir, "db_model.dsl.yaml")
	validationReport := validate.ValidateFile(modelPath)
	lintReport := lint.Result{Version: lint.VersionV05}
	if validationReport.OK() {
		lintReport = lint.LintFile(modelPath)
	}
	issueText := findIssueText(opts.Issue, validationReport, lintReport)
	if issueText == "" {
		return fmt.Errorf("issue %s was not found in validation/lint reports", opts.Issue)
	}
	modelBytes, err := os.ReadFile(modelPath)
	if err != nil {
		return fmt.Errorf("read model: %w", err)
	}
	input := mustJSON(map[string]any{
		"issue_id":          opts.Issue,
		"issue_text":        issueText,
		"db_model_fragment": string(modelBytes),
		"validation_report": validationReport,
		"lint_report":       lintReport,
	})
	var repair RepairProposal
	if err := runStructuredStage(ctx, client, opts.OutDir, 1, llm.Request{
		Stage:           "repair",
		Model:           opts.Model,
		Instructions:    repairInstructions,
		Input:           input,
		SchemaName:      "DBDSLRepair",
		Schema:          repairSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": promptTemplateVersion,
			"issue_id":         opts.Issue,
		},
	}, &repair, func() []string { return nil }); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(opts.OutDir, "repair.proposed.json"), repair)
}

func writeRepairSuggestion(ctx context.Context, client llm.Client, outDir string, opts PlanOptions, modelPath, issueID, issueText string, validationReport validate.Result, lintReport lint.Result) error {
	modelBytes, err := os.ReadFile(modelPath)
	if err != nil {
		return fmt.Errorf("read model for repair suggestion: %w", err)
	}
	input := mustJSON(map[string]any{
		"issue_id":          issueID,
		"issue_text":        issueText,
		"db_model_fragment": string(modelBytes),
		"validation_report": validationReport,
		"lint_report":       lintReport,
		"auto_apply":        false,
	})
	var repair RepairProposal
	if err := runStructuredStage(ctx, client, outDir, 4, llm.Request{
		Stage:           "repair",
		Model:           opts.Model,
		Instructions:    repairInstructions,
		Input:           input,
		SchemaName:      "DBDSLRepair",
		Schema:          repairSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": promptTemplateVersion,
			"issue_id":         issueID,
			"auto_apply":       "false",
		},
	}, &repair, func() []string { return nil }); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(outDir, "repair.proposed.json"), repair)
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
	"source_segmentation": true, "source_unit_extraction": true, "requirement_atom_extraction": true,
	"functional_analysis": true, "crud_mapping": true, "review_candidate_proposal": true,
	"conceptual_description": true,
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
	RequirementAtoms        dsl.V05RequirementAtomsFile
	FunctionalDecomposition dsl.V05FunctionalDecompositionFile
	CRUDMatrix              dsl.V05CRUDMatrixFile
	ReviewDecisions         dsl.V05ReviewDecisionsFile
	Model                   dsl.Document
}

type conversionDiagnostics struct {
	Warnings []string
}

func buildArtifacts(taskText, taskName string, sourceUnits []dsl.SourceUnit, extraction RequirementExtractionProposal, patch PatchProposal, diagnostics *conversionDiagnostics) (artifacts, error) {
	_ = taskText
	atoms := convertRequirementAtoms(extraction.RequirementAtoms)
	if len(atoms) == 0 {
		return artifacts{}, errors.New("requirement extraction produced no atoms")
	}
	atomByID := map[string]dsl.RequirementAtom{}
	for _, atom := range atoms {
		atomByID[atom.ID] = atom
	}
	areas := convertFunctionalAreas(extraction.FunctionalAreas, atoms)
	actors := convertActors(extraction.Actors)
	actors = ensureReferencedActors(actors, areas, extraction.Operations)
	operations := convertOperations(extraction.Operations, atoms, sourceUnits, areas, actors)

	model := dsl.Document{
		DSL: dsl.DSLMeta{Name: "DB-DSL", Version: "0.5"},
		Model: dsl.ModelInfo{
			ID:          slug(strings.TrimSuffix(taskName, filepath.Ext(taskName))),
			Name:        llmDraftName(strings.TrimSuffix(taskName, filepath.Ext(taskName))),
			DomainSlice: slug(strings.TrimSuffix(taskName, filepath.Ext(taskName))),
			Status:      "llm_draft_requires_review",
			Description: "Offline LLM-assisted v0.5 logical database model draft. Validate, lint and review before treating as final.",
		},
		Source: dsl.SourceInfo{
			PipelineVersion:             "0.5",
			TaskTextFile:                "TASK.md",
			SourceUnitsFile:             "source_units.yaml",
			RequirementAtomsFile:        "requirement_atoms.yaml",
			FunctionalDecompositionFile: "functional_decomposition.yaml",
			CRUDMatrixFile:              "crud_matrix.yaml",
			ReviewDecisionsFile:         "review_decisions.yaml",
			ReviewState:                 "llm_draft_requires_review",
			DerivationStrategy:          "llm_assisted_offline_v0",
		},
		ImportSpecs:   []dsl.ImportSpec{},
		StateMachines: []dsl.StateMachine{},
		DerivedViews:  []dsl.DerivedView{},
		FileSpecs:     []dsl.FileSpec{},
	}

	if err := applyPatch(&model, patch, atomByID, diagnostics); err != nil {
		return artifacts{}, err
	}
	normalizeModelReferences(&model, diagnostics)
	fillModelImpacts(atoms, model)
	for i := range atoms {
		atomByID[atoms[i].ID] = atoms[i]
	}
	matrix := buildCRUDMatrix(actors, operations, model.Entities)

	return artifacts{
		RequirementAtoms: dsl.V05RequirementAtomsFile{
			Document: map[string]any{
				"id":                  model.Model.ID + "_requirement_atoms",
				"title":               model.Model.Name + " requirement atoms",
				"pipeline_version":    "0.5",
				"source_units_file":   "source_units.yaml",
				"generation_strategy": "llm_assisted_offline_v0",
			},
			RequirementAtoms: atoms,
			CoverageChecks: []map[string]any{{
				"id":     "llm_atoms_reference_source_units",
				"status": "passed",
				"note":   "Preflight checked source unit references before YAML emission.",
			}},
		},
		FunctionalDecomposition: dsl.V05FunctionalDecompositionFile{
			Document: map[string]any{
				"id":                      model.Model.ID + "_functional_decomposition",
				"title":                   model.Model.Name + " functional decomposition",
				"pipeline_version":        "0.5",
				"source_units_file":       "source_units.yaml",
				"requirement_atoms_file":  "requirement_atoms.yaml",
				"crud_matrix_file":        "crud_matrix.yaml",
				"generation_strategy":     "llm_assisted_offline_v0",
				"requires_domain_review":  true,
				"domain_modeling_quality": "llm_draft",
				"recommended_next_action": "Inspect review candidates, validation/lint reports and generated DBML before accepting.",
			},
			FunctionalAreas: areas,
			CoverageSummary: map[string]any{
				"total_atoms":         len(atoms),
				"represented_atoms":   countRepresentedAtoms(atoms),
				"llm_review_required": true,
				"unresolved_warnings": len(extraction.Warnings) + len(patch.Warnings),
			},
		},
		CRUDMatrix: dsl.V05CRUDMatrixFile{
			Document: map[string]any{
				"id":                            model.Model.ID + "_crud_matrix",
				"title":                         model.Model.Name + " CRUD matrix",
				"description":                   "CRUD matrix synthesized from LLM operations and model evidence.",
				"pipeline_version":              "0.5",
				"source_units_file":             "source_units.yaml",
				"requirement_atoms_file":        "requirement_atoms.yaml",
				"functional_decomposition_file": "functional_decomposition.yaml",
			},
			Notation: map[string]string{
				"C": "create/insert",
				"R": "read/select",
				"U": "update",
				"D": "delete",
			},
			Actors:     actors,
			Operations: operations,
			Matrix:     matrix,
			CoverageChecks: []map[string]any{{
				"id":     "all_entities_have_crud_rows",
				"status": "passed",
				"note":   "Each emitted entity has one CRUD matrix row.",
			}},
		},
		ReviewDecisions: dsl.V05ReviewDecisionsFile{
			Document: map[string]any{
				"id":                     model.Model.ID + "_review_decisions",
				"title":                  model.Model.Name + " review decisions",
				"pipeline_version":       "0.5",
				"requirement_atoms_file": "requirement_atoms.yaml",
				"generation_strategy":    "llm_assisted_offline_v0",
				"review_candidates_note": "Unresolved LLM review candidates are retained in proposed JSON logs, not committed as resolved decisions.",
			},
			ReviewState: map[string]any{
				"status":                           "llm_draft_requires_review",
				"all_required_reviews_resolved":    true,
				"unresolved_requires_review_flags": 0,
			},
			ReviewDecisions: []dsl.V05ReviewDecision{},
			CoverageChecks: []map[string]any{{
				"id":     "no_resolved_decisions_claimed",
				"status": "passed",
				"note":   "The offline v0 pipeline does not fabricate resolved human review decisions.",
			}},
		},
		Model: model,
	}, nil
}

func writeArtifacts(outDir string, a artifacts) error {
	files := map[string]any{
		"requirement_atoms.yaml":        a.RequirementAtoms,
		"functional_decomposition.yaml": a.FunctionalDecomposition,
		"crud_matrix.yaml":              a.CRUDMatrix,
		"review_decisions.yaml":         a.ReviewDecisions,
		"db_model.dsl.yaml":             a.Model,
	}
	for name, value := range files {
		data, err := yaml.Marshal(value)
		if err != nil {
			return fmt.Errorf("marshal %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func convertRequirementAtoms(proposals []RequirementAtomProposal) []dsl.RequirementAtom {
	atoms := make([]dsl.RequirementAtom, 0, len(proposals))
	for i, proposal := range proposals {
		id := proposal.ID
		if id == "" {
			id = fmt.Sprintf("LLM-RA-%03d", i+1)
		}
		area := nonEmpty(proposal.FunctionalArea, "core_model")
		pattern := nonEmpty(proposal.FunctionalPattern, "domain_modeling")
		outcome := nonEmpty(proposal.ModelingOutcome, "represented")
		atoms = append(atoms, dsl.RequirementAtom{
			ID:                id,
			Statement:         nonEmpty(proposal.Statement, "LLM extracted requirement."),
			Subject:           proposal.Subject,
			Predicate:         proposal.Predicate,
			Object:            proposal.Object,
			Quantifier:        proposal.Quantifier,
			Condition:         proposal.Condition,
			TemporalSemantics: proposal.TemporalSemantics,
			Ownership:         proposal.Ownership,
			AtomType:          nonEmpty(proposal.AtomType, "data_requirement"),
			ModelingRelevance: nonEmpty(proposal.ModelingRelevance, "direct_db"),
			SourceUnits:       proposal.SourceUnits,
			FunctionalArea:    area,
			FunctionalPattern: pattern,
			SupportLevel:      nonEmpty(proposal.SupportLevel, "inferred"),
			Confidence:        nonEmpty(proposal.Confidence, "medium"),
			RequiresReview:    proposal.RequiresReview,
			ReviewClass:       proposal.ReviewClass,
			ReviewTopic:       proposal.ReviewTopic,
			ReviewGroup:       proposal.ReviewGroup,
			ReviewDecisions:   append([]string(nil), proposal.ReviewDecisions...),
			ModelingOutcome:   dsl.RequirementOutcome{Status: outcome},
		})
	}
	return atoms
}

func convertFunctionalAreas(proposals []FunctionalAreaProposal, atoms []dsl.RequirementAtom) []dsl.FunctionalArea {
	areasByID := map[string]dsl.FunctionalArea{}
	for _, proposal := range proposals {
		if proposal.ID == "" {
			continue
		}
		areasByID[proposal.ID] = dsl.FunctionalArea{
			ID:            proposal.ID,
			Label:         nonEmpty(proposal.Label, titleFromID(proposal.ID)),
			Purpose:       nonEmpty(proposal.Purpose, "Group related database modeling requirements."),
			MainActors:    append([]string(nil), proposal.MainActors...),
			Atoms:         append([]string(nil), proposal.Atoms...),
			ModelingFocus: append([]string(nil), proposal.ModelingFocus...),
		}
	}
	for _, atom := range atoms {
		area := areasByID[atom.FunctionalArea]
		if area.ID == "" {
			area = dsl.FunctionalArea{
				ID:            atom.FunctionalArea,
				Label:         titleFromID(atom.FunctionalArea),
				Purpose:       "Synthesized functional area for LLM extracted requirements.",
				MainActors:    []string{"system"},
				ModelingFocus: []string{},
			}
		}
		if !contains(area.Atoms, atom.ID) {
			area.Atoms = append(area.Atoms, atom.ID)
		}
		if len(area.MainActors) == 0 {
			area.MainActors = []string{"system"}
		}
		areasByID[area.ID] = area
	}
	areas := make([]dsl.FunctionalArea, 0, len(areasByID))
	for _, area := range areasByID {
		sort.Strings(area.Atoms)
		areas = append(areas, area)
	}
	sort.Slice(areas, func(i, j int) bool { return areas[i].ID < areas[j].ID })
	return areas
}

func convertActors(proposals []ActorProposal) []dsl.CRUDActor {
	actorsByID := map[string]dsl.CRUDActor{}
	for _, proposal := range proposals {
		if proposal.ID == "" {
			continue
		}
		actorsByID[proposal.ID] = dsl.CRUDActor{
			ID:          proposal.ID,
			Label:       nonEmpty(proposal.Label, titleFromID(proposal.ID)),
			Description: nonEmpty(proposal.Description, "Actor identified by the LLM proposal."),
		}
	}
	if len(actorsByID) == 0 {
		actorsByID["system"] = dsl.CRUDActor{ID: "system", Label: "System", Description: "System actor used by the offline LLM pipeline."}
	}
	actors := make([]dsl.CRUDActor, 0, len(actorsByID))
	for _, actor := range actorsByID {
		actors = append(actors, actor)
	}
	sort.Slice(actors, func(i, j int) bool { return actors[i].ID < actors[j].ID })
	return actors
}

func ensureReferencedActors(actors []dsl.CRUDActor, areas []dsl.FunctionalArea, operations []OperationProposal) []dsl.CRUDActor {
	actorsByID := map[string]dsl.CRUDActor{}
	for _, actor := range actors {
		actorsByID[actor.ID] = actor
	}
	for _, area := range areas {
		for _, actorID := range area.MainActors {
			if actorID == "" || actorsByID[actorID].ID != "" {
				continue
			}
			actorsByID[actorID] = dsl.CRUDActor{
				ID:          actorID,
				Label:       titleFromID(actorID),
				Description: "Actor referenced by an LLM functional area.",
			}
		}
	}
	for _, operation := range operations {
		if operation.Actor == "" || actorsByID[operation.Actor].ID != "" {
			continue
		}
		actorsByID[operation.Actor] = dsl.CRUDActor{
			ID:          operation.Actor,
			Label:       titleFromID(operation.Actor),
			Description: "Actor referenced by an LLM CRUD operation.",
		}
	}
	out := make([]dsl.CRUDActor, 0, len(actorsByID))
	for _, actor := range actorsByID {
		out = append(out, actor)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func convertOperations(proposals []OperationProposal, atoms []dsl.RequirementAtom, sourceUnits []dsl.SourceUnit, areas []dsl.FunctionalArea, actors []dsl.CRUDActor) []dsl.CRUDOperation {
	actorIDs := map[string]bool{}
	for _, actor := range actors {
		actorIDs[actor.ID] = true
	}
	areaIDs := map[string]bool{}
	for _, area := range areas {
		areaIDs[area.ID] = true
	}
	atomIDs := make([]string, 0, len(atoms))
	sourceIDs := make([]string, 0, len(sourceUnits))
	sourceByAtom := map[string][]string{}
	for _, atom := range atoms {
		atomIDs = append(atomIDs, atom.ID)
		sourceByAtom[atom.ID] = atom.SourceUnits
	}
	for _, sourceUnit := range sourceUnits {
		sourceIDs = append(sourceIDs, sourceUnit.ID)
	}
	var operations []dsl.CRUDOperation
	for _, proposal := range proposals {
		if proposal.ID == "" {
			continue
		}
		sourceAtoms := proposal.SourceAtoms
		if len(sourceAtoms) == 0 {
			sourceAtoms = atomIDs
		}
		sourceUnitsForOp := proposal.SourceUnits
		if len(sourceUnitsForOp) == 0 {
			sourceUnitsForOp = unionSourcesForAtoms(sourceAtoms, sourceByAtom)
		}
		if len(sourceUnitsForOp) == 0 {
			sourceUnitsForOp = sourceIDs
		}
		actor := proposal.Actor
		if !actorIDs[actor] {
			actor = actors[0].ID
		}
		area := proposal.FunctionalArea
		if !areaIDs[area] {
			area = areas[0].ID
		}
		operations = append(operations, dsl.CRUDOperation{
			ID:                proposal.ID,
			Label:             nonEmpty(proposal.Label, titleFromID(proposal.ID)),
			FunctionalArea:    area,
			FunctionalPattern: nonEmpty(proposal.FunctionalPattern, "domain_modeling"),
			Actor:             actor,
			SourceAtoms:       sourceAtoms,
			SourceUnits:       sourceUnitsForOp,
			Description:       nonEmpty(proposal.Description, "LLM proposed operation."),
		})
	}
	if len(operations) == 0 {
		operations = append(operations, dsl.CRUDOperation{
			ID:                "inspect_llm_draft",
			Label:             "Inspect LLM draft",
			FunctionalArea:    areas[0].ID,
			FunctionalPattern: "review",
			Actor:             actors[0].ID,
			SourceAtoms:       atomIDs,
			SourceUnits:       sourceIDs,
			Description:       "Inspect LLM-generated model draft.",
		})
	}
	return operations
}

func applyPatch(model *dsl.Document, patch PatchProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) error {
	seenEntities := map[string]bool{}
	for _, operation := range patch.Operations {
		if operation.Operation == "add_entity" {
			if operation.Entity == nil {
				return errors.New("add_entity operation is missing entity")
			}
			entity := convertEntity(*operation.Entity, atomByID, diagnostics)
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
			model.Relationships = append(model.Relationships, convertRelationship(*operation.Relationship, atomByID, diagnostics))
		case "add_constraint":
			if operation.Constraint == nil {
				return errors.New("add_constraint operation is missing constraint")
			}
			model.Constraints = append(model.Constraints, convertConstraint(*operation.Constraint, atomByID, diagnostics))
		case "add_state_machine":
			if operation.StateMachine == nil {
				return errors.New("add_state_machine operation is missing state_machine")
			}
			model.StateMachines = append(model.StateMachines, convertStateMachine(*operation.StateMachine, atomByID, diagnostics))
		case "add_derived_view":
			if operation.DerivedView == nil {
				return errors.New("add_derived_view operation is missing derived_view")
			}
			model.DerivedViews = append(model.DerivedViews, convertDerivedView(*operation.DerivedView, atomByID, diagnostics))
		case "add_file_spec":
			if operation.FileSpec == nil {
				return errors.New("add_file_spec operation is missing file_spec")
			}
			model.FileSpecs = append(model.FileSpecs, convertFileSpec(*operation.FileSpec, atomByID, diagnostics))
		case "add_import_spec":
			if operation.ImportSpec == nil {
				return errors.New("add_import_spec operation is missing import_spec")
			}
			model.ImportSpecs = append(model.ImportSpecs, convertImportSpec(*operation.ImportSpec, atomByID, diagnostics))
		case "remove_operation":
			return errors.New("remove_operation is repair-only and must be merged before artifact conversion")
		default:
			return fmt.Errorf("unsupported patch operation %s", operation.Operation)
		}
	}
	return nil
}

func convertEntity(proposal EntityProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.Entity {
	entityEvidence := convertEvidence(proposal.Evidence, atomByID, diagnostics)
	kind := nonEmpty(proposal.Kind, "regular")
	attrs := make([]dsl.Attribute, 0, len(proposal.Attributes))
	for _, attr := range proposal.Attributes {
		attrID := normalizeAttributeID(proposal.ID, attr, diagnostics)
		if shouldDropGeneratedIDAttribute(kind, proposal.ID, attr, attrID) {
			diagnostics.add(fmt.Sprintf("dropped generated primary key attribute %s.%s because %s entities receive an id in DBML generation", proposal.ID, attr.ID, kind))
			continue
		}
		required := attr.Required
		evidence := convertEvidence(attr.Evidence, atomByID, diagnostics)
		if len(evidence.SourceUnits) == 0 && len(evidence.RequirementAtoms) == 0 {
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

func convertRelationship(proposal RelationshipProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.Relationship {
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
		Evidence:    convertEvidence(proposal.Evidence, atomByID, diagnostics),
	}
}

func convertConstraint(proposal ConstraintProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.Constraint {
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
		Evidence:    convertEvidence(proposal.Evidence, atomByID, diagnostics),
	}
}

func convertStateMachine(proposal StateMachineProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.StateMachine {
	return dsl.StateMachine{
		ID:          proposal.ID,
		Owner:       proposal.Owner,
		Field:       proposal.Field,
		States:      proposal.States,
		Initial:     proposal.Initial,
		Terminal:    proposal.Terminal,
		Transitions: proposal.Transitions,
		Notes:       proposal.Notes,
		Evidence:    convertEvidence(proposal.Evidence, atomByID, diagnostics),
	}
}

func convertDerivedView(proposal DerivedViewProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.DerivedView {
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
		Evidence:    convertEvidence(proposal.Evidence, atomByID, diagnostics),
	}
}

func convertFileSpec(proposal FileSpecProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.FileSpec {
	return dsl.FileSpec{
		ID:                proposal.ID,
		Owner:             proposal.Owner,
		Field:             proposal.Field,
		AllowedExtensions: proposal.AllowedExtensions,
		MaxSizeMB:         proposal.MaxSizeMB,
		MIMETypes:         proposal.MIMETypes,
		Storage:           nonEmpty(proposal.Storage, "external_reference"),
		Notes:             proposal.Notes,
		Evidence:          convertEvidence(proposal.Evidence, atomByID, diagnostics),
	}
}

func convertImportSpec(proposal ImportSpecProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.ImportSpec {
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
		Evidence: convertEvidence(proposal.Evidence, atomByID, diagnostics),
	}
}

func convertEvidence(proposal EvidenceProposal, atomByID map[string]dsl.RequirementAtom, diagnostics *conversionDiagnostics) dsl.Evidence {
	evidence := dsl.Evidence{
		SourceUnits:      append([]string(nil), proposal.SourceUnits...),
		RequirementAtoms: append([]string(nil), proposal.RequirementAtoms...),
		ReviewDecisions:  append([]string(nil), proposal.ReviewDecisions...),
		SupportLevel:     nonEmpty(proposal.SupportLevel, "inferred"),
		Confidence:       nonEmpty(proposal.Confidence, "medium"),
		Notes:            append([]string(nil), proposal.Notes...),
	}
	if len(evidence.SourceUnits) == 0 && len(evidence.RequirementAtoms) > 0 {
		evidence.SourceUnits = unionSourcesForAtoms(evidence.RequirementAtoms, atomSources(atomByID))
	}
	if evidence.SupportLevel == "assumption" && len(evidence.ReviewDecisions) == 0 {
		evidence.SupportLevel = "inferred"
		evidence.Notes = append(evidence.Notes, "support_level downgraded from assumption because no resolved review decision was provided")
		diagnostics.add("assumption evidence without resolved review decision was downgraded to inferred")
	}
	return evidence
}

func fillModelImpacts(atoms []dsl.RequirementAtom, model dsl.Document) {
	impacts := map[string]*dsl.RequirementModelImpacts{}
	for i := range atoms {
		impacts[atoms[i].ID] = &dsl.RequirementModelImpacts{}
	}
	for _, entity := range model.Entities {
		for _, atomID := range entity.Evidence.RequirementAtoms {
			if impacts[atomID] != nil {
				impacts[atomID].Entities = appendUnique(impacts[atomID].Entities, entity.ID)
			}
		}
		for _, attribute := range entity.Attributes {
			ref := entity.ID + "." + attribute.ID
			for _, atomID := range attribute.Evidence.RequirementAtoms {
				if impacts[atomID] != nil {
					impacts[atomID].Attributes = appendUnique(impacts[atomID].Attributes, ref)
				}
			}
		}
	}
	for _, relationship := range model.Relationships {
		for _, atomID := range relationship.Evidence.RequirementAtoms {
			if impacts[atomID] != nil {
				impacts[atomID].Relationships = appendUnique(impacts[atomID].Relationships, relationship.ID)
			}
		}
	}
	for _, constraint := range model.Constraints {
		for _, atomID := range constraint.Evidence.RequirementAtoms {
			if impacts[atomID] != nil {
				impacts[atomID].Constraints = appendUnique(impacts[atomID].Constraints, constraint.ID)
			}
		}
	}
	for _, spec := range model.ImportSpecs {
		for _, atomID := range spec.Evidence.RequirementAtoms {
			if impacts[atomID] != nil {
				impacts[atomID].ImportSpecs = appendUnique(impacts[atomID].ImportSpecs, spec.ID)
			}
		}
	}
	for _, machine := range model.StateMachines {
		for _, atomID := range machine.Evidence.RequirementAtoms {
			if impacts[atomID] != nil {
				impacts[atomID].StateMachines = appendUnique(impacts[atomID].StateMachines, machine.ID)
			}
		}
	}
	for _, view := range model.DerivedViews {
		for _, atomID := range view.Evidence.RequirementAtoms {
			if impacts[atomID] != nil {
				impacts[atomID].DerivedViews = appendUnique(impacts[atomID].DerivedViews, view.ID)
			}
		}
	}
	for _, spec := range model.FileSpecs {
		for _, atomID := range spec.Evidence.RequirementAtoms {
			if impacts[atomID] != nil {
				impacts[atomID].FileSpecs = appendUnique(impacts[atomID].FileSpecs, spec.ID)
			}
		}
	}
	for i := range atoms {
		if impact := impacts[atoms[i].ID]; impact != nil {
			sortImpact(impact)
			atoms[i].ModelImpacts = *impact
		}
	}
}

func buildCRUDMatrix(actors []dsl.CRUDActor, operations []dsl.CRUDOperation, entities []dsl.Entity) []dsl.CRUDRow {
	rows := make([]dsl.CRUDRow, 0, len(entities))
	for _, entity := range entities {
		ops := map[string][]string{}
		entityAtoms := set(entity.Evidence.RequirementAtoms...)
		for _, operation := range operations {
			if intersectsSet(entityAtoms, operation.SourceAtoms) {
				ops[operation.ID] = inferActions(operation.ID, operation.Label)
			}
		}
		if len(ops) == 0 && len(operations) > 0 {
			ops[operations[0].ID] = []string{"R"}
		}
		rows = append(rows, dsl.CRUDRow{
			Entity:     entity.ID,
			Table:      entity.TableName,
			Operations: ops,
			Rationale:  "CRUD row synthesized from LLM operation and model evidence overlap.",
		})
	}
	return rows
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

func validateExtractionProposal(proposal RequirementExtractionProposal, sourceIDs map[string]bool) []string {
	var errors []string
	seenAtoms := map[string]bool{}
	for _, atom := range proposal.RequirementAtoms {
		if atom.ID == "" {
			errors = append(errors, "requirement atom id is required")
		}
		if seenAtoms[atom.ID] {
			errors = append(errors, "duplicate requirement atom id "+atom.ID)
		}
		seenAtoms[atom.ID] = true
		errors = append(errors, validateSourceRefs("requirement atom "+atom.ID, atom.SourceUnits, sourceIDs)...)
	}
	for _, op := range proposal.Operations {
		errors = append(errors, validateSourceRefs("operation "+op.ID, op.SourceUnits, sourceIDs)...)
		for _, atomID := range op.SourceAtoms {
			if atomID != "" && !seenAtoms[atomID] {
				errors = append(errors, "operation "+op.ID+" references unknown atom "+atomID)
			}
		}
	}
	for _, review := range proposal.ReviewCandidates {
		for _, atomID := range review.AffectedAtoms {
			if atomID != "" && !seenAtoms[atomID] {
				errors = append(errors, "review candidate "+review.ID+" references unknown atom "+atomID)
			}
		}
	}
	return errors
}

func validateModelPlanProposal(proposal ModelPlanProposal, sourceIDs, atomIDs map[string]bool) []string {
	var errors []string
	check := func(label string, elements []PlanElementProposal) {
		for _, element := range elements {
			errors = append(errors, validateSourceRefs(label+" "+element.ID, element.SourceUnits, sourceIDs)...)
			errors = append(errors, validateAtomRefs(label+" "+element.ID, element.RequirementAtoms, atomIDs)...)
		}
	}
	check("candidate entity", proposal.CandidateEntities)
	check("candidate relationship", proposal.CandidateRelationships)
	check("candidate constraint", proposal.CandidateConstraints)
	check("candidate state machine", proposal.CandidateStateMachines)
	check("candidate derived view", proposal.CandidateDerivedViews)
	check("candidate file spec", proposal.CandidateFileSpecs)
	return errors
}

func validatePatchProposal(proposal PatchProposal, sourceIDs, atomIDs map[string]bool) []string {
	var errors []string
	for _, op := range proposal.Operations {
		switch op.Operation {
		case "add_entity":
			if op.Entity == nil {
				errors = append(errors, "add_entity operation is missing entity")
				continue
			}
			errors = append(errors, validateEvidenceRefs("entity "+op.Entity.ID, op.Entity.Evidence, sourceIDs, atomIDs)...)
			for _, attr := range op.Entity.Attributes {
				errors = append(errors, validateEvidenceRefs("attribute "+op.Entity.ID+"."+attr.ID, attr.Evidence, sourceIDs, atomIDs)...)
			}
		case "add_relationship":
			if op.Relationship == nil {
				errors = append(errors, "add_relationship operation is missing relationship")
				continue
			}
			errors = append(errors, validateEvidenceRefs("relationship "+op.Relationship.ID, op.Relationship.Evidence, sourceIDs, atomIDs)...)
		case "add_constraint":
			if op.Constraint == nil {
				errors = append(errors, "add_constraint operation is missing constraint")
				continue
			}
			errors = append(errors, validateEvidenceRefs("constraint "+op.Constraint.ID, op.Constraint.Evidence, sourceIDs, atomIDs)...)
		case "add_state_machine":
			if op.StateMachine == nil {
				errors = append(errors, "add_state_machine operation is missing state_machine")
				continue
			}
			errors = append(errors, validateEvidenceRefs("state_machine "+op.StateMachine.ID, op.StateMachine.Evidence, sourceIDs, atomIDs)...)
		case "add_derived_view":
			if op.DerivedView == nil {
				errors = append(errors, "add_derived_view operation is missing derived_view")
				continue
			}
			errors = append(errors, validateEvidenceRefs("derived_view "+op.DerivedView.ID, op.DerivedView.Evidence, sourceIDs, atomIDs)...)
		case "add_file_spec":
			if op.FileSpec == nil {
				errors = append(errors, "add_file_spec operation is missing file_spec")
				continue
			}
			errors = append(errors, validateEvidenceRefs("file_spec "+op.FileSpec.ID, op.FileSpec.Evidence, sourceIDs, atomIDs)...)
		case "add_import_spec":
			if op.ImportSpec == nil {
				errors = append(errors, "add_import_spec operation is missing import_spec")
				continue
			}
			errors = append(errors, validateEvidenceRefs("import_spec "+op.ImportSpec.ID, op.ImportSpec.Evidence, sourceIDs, atomIDs)...)
		case "remove_operation":
			if op.TargetOperation == "" || op.TargetID == "" {
				errors = append(errors, "remove_operation requires target_operation and target_id")
			} else if !strings.HasPrefix(op.TargetOperation, "add_") {
				errors = append(errors, "remove_operation target_operation must identify an add operation")
			}
		default:
			errors = append(errors, "unsupported patch operation "+op.Operation)
		}
	}
	return errors
}

func validateEvidenceRefs(label string, evidence EvidenceProposal, sourceIDs, atomIDs map[string]bool) []string {
	var errors []string
	errors = append(errors, validateSourceRefs(label, evidence.SourceUnits, sourceIDs)...)
	errors = append(errors, validateAtomRefs(label, evidence.RequirementAtoms, atomIDs)...)
	return errors
}

func validateSourceRefs(label string, refs []string, known map[string]bool) []string {
	var errors []string
	for _, ref := range refs {
		if ref == "" {
			errors = append(errors, label+" references empty source unit")
			continue
		}
		if !known[ref] {
			errors = append(errors, label+" references unknown source unit "+ref)
		}
	}
	return errors
}

func validateAtomRefs(label string, refs []string, known map[string]bool) []string {
	var errors []string
	for _, ref := range refs {
		if ref == "" {
			errors = append(errors, label+" references empty requirement atom")
			continue
		}
		if !known[ref] {
			errors = append(errors, label+" references unknown requirement atom "+ref)
		}
	}
	return errors
}

func findIssueText(issue string, validationReport validate.Result, lintReport lint.Result) string {
	for i, err := range validationReport.Errors {
		id := fmt.Sprintf("validation:%d", i+1)
		if issue == id || strings.Contains(err, issue) {
			return err
		}
	}
	for i, item := range lintReport.Issues {
		id := fmt.Sprintf("lint:%s:%d", item.Code, i+1)
		if issue == id || issue == item.Code || strings.Contains(item.Message, issue) {
			return fmt.Sprintf("[%s] %s %s: %s", item.Severity, item.Code, item.Element, item.Message)
		}
	}
	return ""
}

func firstRepairIssue(validationReport validate.Result, lintReport lint.Result) (string, string) {
	if len(validationReport.Errors) > 0 {
		return "validation:1", validationReport.Errors[0]
	}
	for i, item := range lintReport.Issues {
		if item.Severity == lint.SeverityError {
			return fmt.Sprintf("lint:%s:%d", item.Code, i+1), fmt.Sprintf("[%s] %s %s: %s", item.Severity, item.Code, item.Element, item.Message)
		}
	}
	return "", ""
}

func normalizePlanOptions(opts PlanOptions) PlanOptions {
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}
	if opts.MaxRepairAttempts < 0 {
		opts.MaxRepairAttempts = 0
	}
	return opts
}

func normalizeRepairOptions(opts RepairOptions) RepairOptions {
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}
	return opts
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

func sourceUnitInputs(units []dsl.SourceUnit) []sourceUnitInput {
	out := make([]sourceUnitInput, 0, len(units))
	for _, unit := range units {
		out = append(out, sourceUnitInput{
			ID:         unit.ID,
			Kind:       unit.Kind,
			Section:    unit.Section,
			Relevance:  unit.Relevance,
			Tags:       unit.Tags,
			Exact:      unit.Text.Exact,
			Normalized: normalizedIfDifferent(unit.Text.Exact, unit.Text.Normalized),
		})
	}
	return out
}

// normalizedIfDifferent avoids sending the same sentence twice; the backend
// normalizer changes only whitespace/punctuation, so most units are identical.
func normalizedIfDifferent(exact, normalized string) string {
	if strings.TrimSpace(exact) == strings.TrimSpace(normalized) {
		return ""
	}
	return normalized
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

func sourceUnitIDSet(units []dsl.SourceUnit) map[string]bool {
	out := map[string]bool{}
	for _, unit := range units {
		out[unit.ID] = true
	}
	return out
}

func atomIDSet(atoms []RequirementAtomProposal) map[string]bool {
	out := map[string]bool{}
	for _, atom := range atoms {
		out[atom.ID] = true
	}
	return out
}

func atomSources(atoms map[string]dsl.RequirementAtom) map[string][]string {
	out := map[string][]string{}
	for id, atom := range atoms {
		out[id] = atom.SourceUnits
	}
	return out
}

func unionSourcesForAtoms(atomIDs []string, sourceByAtom map[string][]string) []string {
	seen := map[string]bool{}
	for _, atomID := range atomIDs {
		for _, sourceID := range sourceByAtom[atomID] {
			seen[sourceID] = true
		}
	}
	out := make([]string, 0, len(seen))
	for sourceID := range seen {
		out = append(out, sourceID)
	}
	sort.Strings(out)
	return out
}

func inferActions(id, label string) []string {
	text := strings.ToLower(id + " " + label)
	for _, token := range []string{"create", "add", "insert", "register", "submit", "import"} {
		if strings.Contains(text, token) {
			return []string{"C", "R"}
		}
	}
	for _, token := range []string{"manage", "update", "edit", "approve", "process", "confirm"} {
		if strings.Contains(text, token) {
			return []string{"C", "R", "U"}
		}
	}
	for _, token := range []string{"delete", "remove", "cancel"} {
		if strings.Contains(text, token) {
			return []string{"R", "U", "D"}
		}
	}
	return []string{"R"}
}

func countRepresentedAtoms(atoms []dsl.RequirementAtom) int {
	count := 0
	for _, atom := range atoms {
		if atom.ModelingOutcome.Status == "represented" {
			count++
		}
	}
	return count
}

func (d *conversionDiagnostics) add(message string) {
	if d != nil {
		d.Warnings = append(d.Warnings, message)
	}
}

func intersectsSet(values map[string]bool, candidates []string) bool {
	for _, candidate := range candidates {
		if values[candidate] {
			return true
		}
	}
	return false
}

func set(values ...string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
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

func sortImpact(impact *dsl.RequirementModelImpacts) {
	sort.Strings(impact.Entities)
	sort.Strings(impact.Attributes)
	sort.Strings(impact.Relationships)
	sort.Strings(impact.Constraints)
	sort.Strings(impact.ImportSpecs)
	sort.Strings(impact.StateMachines)
	sort.Strings(impact.DerivedViews)
	sort.Strings(impact.FileSpecs)
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

func mustJSON(value any) string {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(data)
}

func mustCompactJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
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
