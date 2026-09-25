package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dbdsl/internal/llmpipeline"
)

type LLMRunSummary struct {
	ID string `json:"id"`
	llmpipeline.RunSummary
}

type LLMRunDetails struct {
	Summary    LLMRunSummary `json:"summary"`
	Request    any           `json:"request,omitempty"`
	Parsed     any           `json:"parsed_response,omitempty"`
	Validation any           `json:"validation,omitempty"`
	Context    any           `json:"context_manifest,omitempty"`
	Raw        string        `json:"raw_response,omitempty"`
}

type LLMOptimizationStageMetrics struct {
	Runs                 int     `json:"runs"`
	ProviderCalls        int     `json:"provider_calls"`
	CacheHits            int     `json:"cache_hits"`
	Retries              int     `json:"retries"`
	InputTokens          int     `json:"input_tokens"`
	OutputTokens         int     `json:"output_tokens"`
	TotalTokens          int     `json:"total_tokens"`
	WastedTokens         int     `json:"wasted_tokens"`
	UnknownUsageAttempts int     `json:"unknown_usage_attempts"`
	ContextBytes         int     `json:"context_bytes"`
	FullContextBytes     int     `json:"full_context_bytes"`
	ContextBytesSaved    int     `json:"context_bytes_saved"`
	ReductionRatio       float64 `json:"context_reduction_ratio"`
}

type LLMOptimizationReport struct {
	Version                   int                                    `json:"version"`
	ProjectID                 string                                 `json:"project_id"`
	PolicyVersion             string                                 `json:"policy_version"`
	BudgetPolicy              string                                 `json:"budget_policy"`
	ContextPolicy             string                                 `json:"context_policy"`
	CallGatePolicy            string                                 `json:"call_gate_policy"`
	RiskPolicy                string                                 `json:"risk_policy"`
	Totals                    LLMOptimizationStageMetrics            `json:"totals"`
	ByStage                   map[string]LLMOptimizationStageMetrics `json:"by_stage"`
	CallsByReason             map[string]int                         `json:"calls_by_reason"`
	AvoidedReviewCalls        int                                    `json:"avoided_review_resolution_calls"`
	AvoidedGenerationCalls    int                                    `json:"avoided_generation_calls"`
	AutoAppliedDecisions      int                                    `json:"auto_applied_decisions"`
	BatchedManualDecisions    int                                    `json:"batched_manual_decisions"`
	ActiveReviewMS            int64                                  `json:"active_review_ms"`
	UnresolvedReviewQuestions int                                    `json:"unresolved_review_questions"`
	RequirementWarnings       int                                    `json:"requirement_warnings"`
	BlockingRequirementAtoms  int                                    `json:"blocking_requirement_atoms"`
	ReviewGroups              int                                    `json:"review_groups"`
}

func (s *Store) LLMRuns(projectID string) ([]LLMRunSummary, error) {
	if _, ok := s.Project(projectID); !ok {
		return nil, ErrNotFound
	}
	root := filepath.Join(s.projectWorkspaceDir(projectID), "llm_runs")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []LLMRunSummary{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]LLMRunSummary, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		var summary llmpipeline.RunSummary
		if readJSON(filepath.Join(root, entry.Name(), "run.json"), &summary) != nil {
			continue
		}
		if summary.Errors == nil {
			summary.Errors = []string{}
		}
		items = append(items, LLMRunSummary{ID: entry.Name(), RunSummary: summary})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].StartedAt.After(items[j].StartedAt) })
	return items, nil
}

func (s *Store) LLMRun(projectID, runID string, includeRaw bool) (LLMRunDetails, error) {
	if strings.TrimSpace(runID) == "" || filepath.Base(runID) != runID || strings.Contains(runID, "..") {
		return LLMRunDetails{}, errors.New("invalid run id")
	}
	items, err := s.LLMRuns(projectID)
	if err != nil {
		return LLMRunDetails{}, err
	}
	var selected *LLMRunSummary
	for i := range items {
		if items[i].ID == runID {
			selected = &items[i]
			break
		}
	}
	if selected == nil {
		return LLMRunDetails{}, ErrNotFound
	}
	details := LLMRunDetails{Summary: *selected}
	runDir := filepath.Join(s.projectWorkspaceDir(projectID), "llm_runs", runID)
	details.Request = readJSONValue(filepath.Join(runDir, "request.json"))
	details.Parsed = readJSONValue(filepath.Join(runDir, "response.parsed.json"))
	details.Validation = readJSONValue(filepath.Join(runDir, "validation_report.json"))
	details.Context = readJSONValue(filepath.Join(runDir, "context_manifest.json"))
	if includeRaw {
		details.Raw = readString(filepath.Join(runDir, "response.raw.txt"))
	}
	return details, nil
}

func (s *Store) LLMOptimizationReport(projectID string) (LLMOptimizationReport, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return LLMOptimizationReport{}, ErrNotFound
	}
	profile := defaultLLMExecutionProfile("")
	if project.LLMExecutionProfile != nil {
		profile = *project.LLMExecutionProfile
	}
	report := LLMOptimizationReport{
		Version: 1, ProjectID: projectID, PolicyVersion: profile.PolicyVersion,
		BudgetPolicy: profile.BudgetPolicy, ContextPolicy: profile.ContextPolicy,
		CallGatePolicy: profile.CallGatePolicy, RiskPolicy: profile.RiskPolicy,
		ByStage: map[string]LLMOptimizationStageMetrics{}, CallsByReason: map[string]int{},
		UnresolvedReviewQuestions: len(project.OpenReviewIDs),
	}
	runs, err := s.LLMRuns(projectID)
	if err != nil {
		return LLMOptimizationReport{}, err
	}
	for _, run := range runs {
		stage := report.ByStage[run.Stage]
		stage.Runs++
		calls := len(run.Attempts)
		if run.Cached {
			stage.CacheHits++
			calls = 0
		} else if calls == 0 {
			// Backward-compatible estimate for run records written before attempts
			// were persisted in v0.7.2.
			calls = 1 + run.RetryCount
		}
		stage.ProviderCalls += calls
		stage.Retries += run.RetryCount
		stage.InputTokens += run.Usage.InputTokens
		stage.OutputTokens += run.Usage.OutputTokens
		stage.TotalTokens += run.Usage.TotalTokens
		stage.WastedTokens += run.WastedTokens
		for _, attempt := range run.Attempts {
			if attempt.Error != "" && attempt.Usage.TotalTokens == 0 {
				stage.UnknownUsageAttempts++
			}
		}
		stage.ContextBytes += run.ContextBytes
		full := run.FullContextBytes
		if full < run.ContextBytes {
			full = run.ContextBytes
		}
		stage.FullContextBytes += full
		stage.ContextBytesSaved += full - run.ContextBytes
		report.ByStage[run.Stage] = stage
		reason := strings.TrimSpace(run.CallReason)
		if reason == "" {
			reason = "legacy_unspecified"
		}
		report.CallsByReason[reason] += calls
	}
	for name, stage := range report.ByStage {
		if stage.FullContextBytes > 0 {
			stage.ReductionRatio = float64(stage.ContextBytesSaved) / float64(stage.FullContextBytes)
		}
		report.ByStage[name] = stage
		report.Totals.Runs += stage.Runs
		report.Totals.ProviderCalls += stage.ProviderCalls
		report.Totals.CacheHits += stage.CacheHits
		report.Totals.Retries += stage.Retries
		report.Totals.InputTokens += stage.InputTokens
		report.Totals.OutputTokens += stage.OutputTokens
		report.Totals.TotalTokens += stage.TotalTokens
		report.Totals.WastedTokens += stage.WastedTokens
		report.Totals.UnknownUsageAttempts += stage.UnknownUsageAttempts
		report.Totals.ContextBytes += stage.ContextBytes
		report.Totals.FullContextBytes += stage.FullContextBytes
		report.Totals.ContextBytesSaved += stage.ContextBytesSaved
	}
	if report.Totals.FullContextBytes > 0 {
		report.Totals.ReductionRatio = float64(report.Totals.ContextBytesSaved) / float64(report.Totals.FullContextBytes)
	}
	if project.ReviewDecisionsPath != "" {
		var artifact ReviewDecisionsArtifact
		if err := readYAML(s.absoluteWorkspacePath(project.ReviewDecisionsPath), &artifact); err == nil {
			for _, decision := range artifact.ReviewDecisions {
				switch decision.DecisionMode {
				case "auto_low_risk":
					report.AutoAppliedDecisions++
					report.AvoidedReviewCalls++
				case "manual_batch":
					report.BatchedManualDecisions++
					report.AvoidedReviewCalls++
				}
				report.ActiveReviewMS += decision.ActiveReviewMS
			}
		}
	}
	if project.RequirementAtomsProposalPath != "" {
		var atoms llmpipeline.RequirementAtomExtractionProposal
		if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err == nil {
			groups := map[string]bool{}
			for _, atom := range llmpipeline.NormalizeRequirementReviewSemantics(atoms.RequirementAtoms) {
				if atom.ReviewClass != llmpipeline.ReviewClassNone {
					report.RequirementWarnings++
				}
				if atom.RequiresReview {
					report.BlockingRequirementAtoms++
					groups[atom.ReviewGroup] = true
				}
			}
			report.ReviewGroups = len(groups)
		}
	}
	if _, err := os.Stat(filepath.Join(s.projectWorkspaceDir(projectID), "llm_runs", "review_candidate_call_gate.json")); err == nil {
		report.AvoidedGenerationCalls++
	}
	return report, nil
}

func (report LLMOptimizationReport) Markdown() string {
	return fmt.Sprintf("# LLM optimization report\n\nProject: `%s`\n\n- Provider calls: %d\n- Provider calls avoided by semantic gates: %d\n- Cache hits: %d\n- Retries: %d\n- Tokens: %d total (%d input, %d output)\n- Wasted tokens: %d\n- Failed attempts with unavailable provider usage: %d\n- Context reduction: %.1f%% (%d bytes avoided)\n- Requirement warnings: %d\n- Blocking requirement atoms: %d\n- Review groups: %d\n- Review-resolution calls avoided: %d\n- Auto-applied low-risk decisions: %d\n- Manually batched decisions: %d\n- Active review time: %d ms\n- Open review questions: %d\n",
		report.ProjectID, report.Totals.ProviderCalls, report.AvoidedGenerationCalls, report.Totals.CacheHits, report.Totals.Retries,
		report.Totals.TotalTokens, report.Totals.InputTokens, report.Totals.OutputTokens, report.Totals.WastedTokens,
		report.Totals.UnknownUsageAttempts, report.Totals.ReductionRatio*100, report.Totals.ContextBytesSaved,
		report.RequirementWarnings, report.BlockingRequirementAtoms, report.ReviewGroups, report.AvoidedReviewCalls,
		report.AutoAppliedDecisions, report.BatchedManualDecisions, report.ActiveReviewMS, report.UnresolvedReviewQuestions)
}

func readJSONValue(path string) any {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var value any
	if json.Unmarshal(data, &value) != nil {
		return nil
	}
	return value
}
