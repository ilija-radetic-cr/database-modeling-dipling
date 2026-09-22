package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dbdsl/internal/dsl"
	"dbdsl/internal/generate"
	"dbdsl/internal/jobs"
	"dbdsl/internal/lint"
	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
	"dbdsl/internal/quality"
	"dbdsl/internal/validate"
)

type ModelStageOptions struct {
	BaseRevision    int
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	OnProgress      jobs.StepEmitter
}

type ConceptualModelArtifacts struct {
	Proposed   llmpipeline.ConceptualModelProposal  `json:"proposed"`
	Accepted   *llmpipeline.ConceptualModelProposal `json:"accepted,omitempty"`
	IsAccepted bool                                 `json:"is_accepted"`
	Diff       map[string]any                       `json:"diff,omitempty"`
	QA         llmpipeline.StageQA                  `json:"qa"`
}

func (s *Store) ConceptualOptimizationPreview(projectID string) (map[string]int, error) {
	project, units, atoms, functional, crud, decisions, err := s.modelStageInputs(projectID, 0)
	if err != nil {
		return nil, err
	}
	obligations, err := s.DesignObligations(project.ID)
	if err != nil {
		return nil, err
	}
	refreshed, qa := llmpipeline.ReclassifyDesignObligations(obligations.Accepted, atoms.RequirementAtoms)
	if !qa.OK {
		return nil, fmt.Errorf("design-obligation preview failed: %s", strings.Join(qa.Errors, "; "))
	}
	return llmpipeline.ConceptualInputMetrics(llmpipeline.ConceptualModelOptions{
		SourceUnits: units, RequirementAtoms: atoms.RequirementAtoms, FunctionalAreas: functional.FunctionalAreas,
		Actors: functional.Actors, Operations: crud.Operations, ReviewDecisions: reviewDecisionIDs(decisions),
		ReviewDecisionContext: conceptualReviewDecisionInputs(decisions), DesignObligations: refreshed.DesignObligations,
	}), nil
}

func (s *Store) GenerateConceptualModel(ctx context.Context, client llm.Client, projectID string, opts ModelStageOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "conceptual_model", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, units, atoms, functional, crud, decisions, err := s.modelStageInputs(projectID, opts.BaseRevision)
	if err != nil {
		return 0, nil, err
	}
	resolvedProfile := defaultLLMExecutionProfile(opts.Model)
	if project.LLMExecutionProfile != nil {
		resolvedProfile = *project.LLMExecutionProfile
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for conceptual modeling")
	}
	obligationArtifacts, err := s.DesignObligations(projectID)
	if err != nil {
		return 0, nil, errors.New("design obligations are not ready; rerun the v0.7 requirement stage")
	}
	previousObligations := obligationArtifacts.Accepted
	refreshedObligations, refreshedObligationQA := llmpipeline.ReclassifyDesignObligations(previousObligations, atoms.RequirementAtoms)
	if !refreshedObligationQA.OK {
		return 0, nil, fmt.Errorf("design-obligation compatibility migration failed: %s", strings.Join(refreshedObligationQA.Errors, "; "))
	}
	requiredObligations := 0
	for _, obligation := range refreshedObligations.DesignObligations {
		if obligation.Status != "not_required" && obligation.Persistence != "not_required" {
			requiredObligations++
		}
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "conceptual_model", opts.MaxOutputTokens, stageBudgetInput{RequiredObligations: requiredObligations})
	emit := stageEmitter(opts.OnProgress)
	migrationReport := designObligationMigrationReport(previousObligations, refreshedObligations)
	emit("propose_conceptual_model", "Proposing a source-backed conceptual model from v0.7.1 obligations.", 22, map[string]any{"required_obligations": migrationReport["required_after"], "not_required_obligations": migrationReport["not_required_after"]})
	decisionIDs := reviewDecisionIDs(decisions)
	controls := s.resolveLLMExecutionControls(projectID)
	proposal, qa, err := llmpipeline.RunConceptualModel(ctx, client, llmpipeline.ConceptualModelOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: units, RequirementAtoms: atoms.RequirementAtoms,
		FunctionalAreas: functional.FunctionalAreas, Actors: functional.Actors, Operations: crud.Operations,
		ReviewDecisions: decisionIDs, ReviewDecisionContext: conceptualReviewDecisionInputs(decisions),
		DesignObligations: refreshedObligations.DesignObligations,
		Model:             opts.Model, ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		MaxParallelism: controls.MaxParallelism, MaxRepairAttempts: controls.MaxRepairAttempts,
		PromptVersion: controls.PromptVersion,
		OnChunkProgress: func(phase string, completed, total int) {
			if total <= 0 {
				return
			}
			progress := 22 + completed*42/total
			message := fmt.Sprintf("Generated conceptual-model chunk %d/%d.", completed, total)
			if phase == "repair" {
				progress = 64 + completed*6/total
				message = fmt.Sprintf("Applied conceptual delta-repair chunk %d/%d.", completed, total)
			}
			emit("propose_conceptual_model", message, progress, map[string]any{"phase": phase, "completed_chunks": completed, "total_chunks": total})
		},
	})
	if err != nil {
		return 0, nil, err
	}
	emit("validate_conceptual_model", "Validating conceptual evidence and cardinalities.", 72, coverageMetadata(qa.Coverage))
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"conceptual_model.proposed.json":          {Value: proposal, JSON: true},
		"conceptual_model_qa.json":                {Value: qa, JSON: true},
		"conceptual_model_diff.json":              {Value: conceptualModelDiff(s, project, proposal), JSON: true},
		"design_obligations.proposed.json":        {Value: refreshedObligations, JSON: true},
		"design_obligations.yaml":                 {Value: refreshedObligations},
		"design_obligation_qa.json":               {Value: refreshedObligationQA, JSON: true},
		"design_obligation_migration_report.json": {Value: migrationReport, JSON: true},
		"llm_execution_profile.json":              {Value: map[string]any{"profile": resolvedProfile, "resolved_stage": map[string]any{"stage": "conceptual_model", "model": opts.Model, "reasoning_effort": opts.ReasoningEffort, "max_output_tokens": opts.MaxOutputTokens}}, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		invalidateLogicalArtifacts(current)
		current.ConceptualModelProposalPath = paths["conceptual_model.proposed.json"]
		current.ConceptualModelAcceptedPath = ""
		current.ConceptualModelQAPath = paths["conceptual_model_qa.json"]
		current.ConceptualModelDiffPath = paths["conceptual_model_diff.json"]
		current.DesignObligationsProposalPath = paths["design_obligations.proposed.json"]
		current.DesignObligationsPath = paths["design_obligations.yaml"]
		current.DesignObligationQAPath = paths["design_obligation_qa.json"]
		if current.LLMExecutionProfile == nil {
			profile := resolvedProfile
			current.LLMExecutionProfile = &profile
		}
		current.LifecycleStatus = "conceptual_review"
		current.LastActivity = "Conceptual proposal passed deterministic checks and awaits explicit acceptance."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"conceptual_model"}, nil
}

func designObligationMigrationReport(before, after llmpipeline.DesignObligationsFile) map[string]any {
	count := func(file llmpipeline.DesignObligationsFile, persistence string) int {
		total := 0
		for _, item := range file.DesignObligations {
			if item.Persistence == persistence {
				total++
			}
		}
		return total
	}
	changed := []map[string]string{}
	byID := map[string]llmpipeline.DesignObligation{}
	for _, item := range before.DesignObligations {
		byID[item.ID] = item
	}
	for _, item := range after.DesignObligations {
		old := byID[item.ID]
		if old.Persistence != item.Persistence || old.Status != item.Status {
			changed = append(changed, map[string]string{"id": item.ID, "from_persistence": old.Persistence, "to_persistence": item.Persistence, "from_status": old.Status, "to_status": item.Status, "rationale": item.Rationale})
		}
	}
	return map[string]any{
		"policy_version": "design_obligations/v0.7.1", "total": len(after.DesignObligations),
		"required_before": count(before, "required"), "required_after": count(after, "required"),
		"not_required_before": count(before, "not_required"), "not_required_after": count(after, "not_required"),
		"changed": changed,
	}
}

func (s *Store) GenerateLogicalModel(ctx context.Context, client llm.Client, projectID string, opts ModelStageOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "logical_model", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, units, atoms, functional, crud, decisions, err := s.modelStageInputs(projectID, opts.BaseRevision)
	if err != nil {
		return 0, nil, err
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for logical projection")
	}
	if project.ConceptualModelAcceptedPath == "" {
		return 0, nil, errors.New("accepted conceptual model is not ready")
	}
	obligationArtifacts, err := s.DesignObligations(projectID)
	if err != nil {
		return 0, nil, errors.New("design obligations are not ready; rerun the v0.7 requirement stage")
	}
	var conceptual llmpipeline.ConceptualModelProposal
	if err := readJSON(s.absoluteWorkspacePath(project.ConceptualModelAcceptedPath), &conceptual); err != nil {
		return 0, nil, err
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "logical_model", opts.MaxOutputTokens, stageBudgetInput{
		Entities: len(conceptual.EntityConcepts), Relationships: len(conceptual.Relationships), Constraints: len(conceptual.ConstraintConcepts),
	})
	emit := stageEmitter(opts.OnProgress)
	previousProposal, validationErrors := s.failedLogicalRepairContext(project)
	message := "Projecting the conceptual model into DB-DSL."
	if len(validationErrors) > 0 {
		message = fmt.Sprintf("Repairing the previous logical projection against %d validation errors.", len(validationErrors))
	}
	emit("project_logical_model", message, 18, map[string]any{"repair_mode": len(validationErrors) > 0, "validation_errors": len(validationErrors)})
	decisionIDs := reviewDecisionIDs(decisions)
	controls := s.resolveLLMExecutionControls(projectID)
	patch, patchQA, err := llmpipeline.RunLogicalProjection(ctx, client, llmpipeline.LogicalProjectionOptions{
		OutDir: s.projectWorkspaceDir(projectID), ConceptualModel: conceptual, SourceUnits: units,
		RequirementAtoms: atoms.RequirementAtoms, ReviewDecisions: decisionIDs,
		ReviewDecisionContext: conceptualReviewDecisionInputs(decisions), Model: opts.Model,
		DesignObligations: obligationArtifacts.Accepted.DesignObligations,
		PreviousProposal:  previousProposal, ValidationErrors: validationErrors,
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		MaxParallelism: controls.MaxParallelism, PromptVersion: controls.PromptVersion,
		OnChunkProgress: func(completed, total int) {
			if total > 1 {
				emit("project_logical_model", fmt.Sprintf("Generated logical-projection chunk %d/%d.", completed, total), 18+completed*24/total, map[string]any{"completed_chunks": completed, "total_chunks": total})
			}
		},
	})
	if err != nil {
		return 0, nil, err
	}
	dslDecisions := make([]dsl.V05ReviewDecision, 0, len(decisions.ReviewDecisions))
	for _, decision := range decisions.ReviewDecisions {
		dslDecisions = append(dslDecisions, dsl.V05ReviewDecision{ID: decision.ID, Question: decision.QuestionSnapshot, AffectedAtoms: decision.AffectedAtoms,
			Decision: map[string]any{"status": "accepted", "selected_option": decision.SelectedOption, "rationale": decision.RationaleSnapshot, "reviewed_by": decision.ReviewedBy, "reviewed_at": decision.ReviewedAt}})
	}
	artifacts, err := llmpipeline.BuildLogicalArtifacts(project.Name, units, atoms, functional, crud, patch, dslDecisions)
	if err != nil {
		return 0, nil, err
	}
	emit("write_logical_bundle", "Writing an isolated logical-model revision.", 48, coverageMetadata(patchQA.Coverage))
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"source_units.yaml":             {Value: mustSourceUnitFile(s, projectID)},
		"requirement_atoms.yaml":        {Value: artifacts.RequirementAtoms},
		"functional_decomposition.yaml": {Value: artifacts.FunctionalDecomposition},
		"crud_matrix.yaml":              {Value: artifacts.CRUDMatrix},
		"review_decisions.yaml":         {Value: artifacts.ReviewDecisions},
		"dbdsl_patch.proposed.json":     {Value: patch, JSON: true},
		"db_model.dsl.yaml":             {Value: artifacts.Model},
	})
	if err != nil {
		return 0, nil, err
	}
	combined, err := s.CombinedDocument(projectID)
	if err != nil {
		return 0, nil, err
	}
	taskRel := filepath.ToSlash(filepath.Join(filepath.Dir(paths["db_model.dsl.yaml"]), "TASK.md"))
	if err := writeAtomic(s.absoluteWorkspacePath(taskRel), []byte(combined.Markdown)); err != nil {
		return 0, nil, err
	}
	modelPath := s.absoluteWorkspacePath(paths["db_model.dsl.yaml"])
	emit("validate", "Running structural DB-DSL validation.", 64, nil)
	validation := validate.ValidateFile(modelPath)
	lintResult := lint.Result{Version: lint.VersionV05}
	if validation.OK() {
		emit("lint", "Running semantic lint checks.", 76, nil)
		lintResult = lint.LintFile(modelPath)
	}
	qualityReport := quality.BuildReport(modelPath, false)
	reportPaths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"validation_report.json": {Value: map[string]any{"ok": validation.OK(), "errors": validation.Errors}, JSON: true},
		"lint_report.json":       {Value: lintResult, JSON: true},
		"quality_report.json":    {Value: qualityReport, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	if !validation.OK() {
		return 0, nil, fmt.Errorf("logical DB-DSL validation failed: %s", strings.Join(validation.Errors, "; "))
	}
	if lintResult.HasErrors() {
		return 0, nil, errors.New("logical DB-DSL has blocking lint errors")
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		invalidateLogicalArtifacts(current)
		current.LogicalPatchProposalPath = paths["dbdsl_patch.proposed.json"]
		current.ModelPath = modelPath
		current.BundlePath = filepath.Dir(modelPath)
		current.TaskPath = s.absoluteWorkspacePath(taskRel)
		current.ValidationReportPath = reportPaths["validation_report.json"]
		current.LintReportPath = reportPaths["lint_report.json"]
		current.QualityReportPath = reportPaths["quality_report.json"]
		current.ModelGenerated = true
		current.DBMLReady = false
		current.FinalModelAccepted = false
		current.LifecycleStatus = "model_generated"
		current.LastActivity = "Logical DB-DSL draft generated and validated; final model review is required."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"logical_model", "validation", "lint", "quality"}, nil
}

func (s *Store) modelStageInputs(projectID string, baseRevision int) (*ProjectState, []dsl.SourceUnit, llmpipeline.RequirementAtomExtractionProposal, llmpipeline.FunctionalAnalysisProposal, llmpipeline.CRUDMappingProposal, ReviewDecisionsArtifact, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, nil, llmpipeline.RequirementAtomExtractionProposal{}, llmpipeline.FunctionalAnalysisProposal{}, llmpipeline.CRUDMappingProposal{}, ReviewDecisionsArtifact{}, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return nil, nil, llmpipeline.RequirementAtomExtractionProposal{}, llmpipeline.FunctionalAnalysisProposal{}, llmpipeline.CRUDMappingProposal{}, ReviewDecisionsArtifact{}, ErrRevisionConflict
	}
	if !project.AnalysisReady || len(project.OpenReviewIDs) > 0 {
		return nil, nil, llmpipeline.RequirementAtomExtractionProposal{}, llmpipeline.FunctionalAnalysisProposal{}, llmpipeline.CRUDMappingProposal{}, ReviewDecisionsArtifact{}, errors.New("analysis is not ready or blocking review decisions remain")
	}
	units := mustAcceptedSourceUnits(s, projectID)
	if len(units) == 0 {
		return nil, nil, llmpipeline.RequirementAtomExtractionProposal{}, llmpipeline.FunctionalAnalysisProposal{}, llmpipeline.CRUDMappingProposal{}, ReviewDecisionsArtifact{}, errors.New("source units are not ready")
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	var functional llmpipeline.FunctionalAnalysisProposal
	var crud llmpipeline.CRUDMappingProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return nil, nil, atoms, functional, crud, ReviewDecisionsArtifact{}, err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.FunctionalAnalysisProposalPath), &functional); err != nil {
		return nil, nil, atoms, functional, crud, ReviewDecisionsArtifact{}, err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.CRUDMappingProposalPath), &crud); err != nil {
		return nil, nil, atoms, functional, crud, ReviewDecisionsArtifact{}, err
	}
	var decisions ReviewDecisionsArtifact
	if project.ReviewDecisionsPath != "" {
		if err := readYAML(s.absoluteWorkspacePath(project.ReviewDecisionsPath), &decisions); err != nil {
			return nil, nil, atoms, functional, crud, decisions, err
		}
	}
	return project, units, atoms, functional, crud, decisions, nil
}

func (s *Store) ConceptualModel(projectID string) (ConceptualModelArtifacts, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return ConceptualModelArtifacts{}, ErrNotFound
	}
	if project.ConceptualModelProposalPath == "" || project.ConceptualModelQAPath == "" {
		return ConceptualModelArtifacts{}, ErrNotFound
	}
	var out ConceptualModelArtifacts
	if err := readJSON(s.absoluteWorkspacePath(project.ConceptualModelProposalPath), &out.Proposed); err != nil {
		return out, err
	}
	if project.ConceptualModelAcceptedPath != "" {
		var accepted llmpipeline.ConceptualModelProposal
		if err := readJSON(s.absoluteWorkspacePath(project.ConceptualModelAcceptedPath), &accepted); err != nil {
			return out, err
		}
		out.Accepted = &accepted
		out.IsAccepted = true
	}
	if err := readJSON(s.absoluteWorkspacePath(project.ConceptualModelQAPath), &out.QA); err != nil {
		return out, err
	}
	if project.ConceptualModelDiffPath != "" {
		_ = readJSON(s.absoluteWorkspacePath(project.ConceptualModelDiffPath), &out.Diff)
	}
	return out, nil
}

func (s *Store) AcceptConceptualModel(projectID string, baseRevision int) (int, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return 0, ErrRevisionConflict
	}
	artifacts, err := s.ConceptualModel(projectID)
	if err != nil {
		return 0, err
	}
	if !artifacts.QA.OK || len(artifacts.QA.Errors) > 0 {
		return 0, errors.New("conceptual model has blocking QA issues")
	}
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"conceptual_model.accepted.json": {Value: artifacts.Proposed, JSON: true},
	})
	if err != nil {
		return 0, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.ConceptualModelAcceptedPath = paths["conceptual_model.accepted.json"]
		current.LifecycleStatus = "ready_for_model_generation"
		current.LastActivity = "Conceptual model explicitly accepted."
		return nil
	})
	if err != nil {
		return 0, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, nil
}

func conceptualModelDiff(s *Store, project *ProjectState, proposal llmpipeline.ConceptualModelProposal) map[string]any {
	previousEntities, previousRelationships := []string{}, []string{}
	if project.ConceptualModelAcceptedPath != "" {
		var previous llmpipeline.ConceptualModelProposal
		if readJSON(s.absoluteWorkspacePath(project.ConceptualModelAcceptedPath), &previous) == nil {
			for _, entity := range previous.EntityConcepts {
				previousEntities = append(previousEntities, entity.ID)
			}
			for _, relationship := range previous.Relationships {
				previousRelationships = append(previousRelationships, relationship.ID)
			}
		}
	}
	currentEntities, currentRelationships := []string{}, []string{}
	for _, entity := range proposal.EntityConcepts {
		currentEntities = append(currentEntities, entity.ID)
	}
	for _, relationship := range proposal.Relationships {
		currentRelationships = append(currentRelationships, relationship.ID)
	}
	return map[string]any{
		"pipeline_version": "0.7",
		"previous":         map[string]any{"entities": uniqueSorted(previousEntities), "relationships": uniqueSorted(previousRelationships)},
		"proposed":         map[string]any{"entities": uniqueSorted(currentEntities), "relationships": uniqueSorted(currentRelationships)},
		"graph_diff": map[string]any{
			"entities":      diffConceptIDs(previousEntities, currentEntities),
			"relationships": diffConceptIDs(previousRelationships, currentRelationships),
		},
		"review_required": true,
	}
}

func diffConceptIDs(previous, current []string) map[string][]string {
	previousSet, currentSet := map[string]bool{}, map[string]bool{}
	for _, id := range previous {
		previousSet[id] = true
	}
	for _, id := range current {
		currentSet[id] = true
	}
	added, removed, unchanged := []string{}, []string{}, []string{}
	for id := range currentSet {
		if previousSet[id] {
			unchanged = append(unchanged, id)
		} else {
			added = append(added, id)
		}
	}
	for id := range previousSet {
		if !currentSet[id] {
			removed = append(removed, id)
		}
	}
	return map[string][]string{"added": uniqueSorted(added), "removed": uniqueSorted(removed), "unchanged": uniqueSorted(unchanged)}
}

func reviewDecisionIDs(decisions ReviewDecisionsArtifact) []string {
	out := make([]string, 0, len(decisions.ReviewDecisions))
	for _, item := range decisions.ReviewDecisions {
		if item.ApplyStatus == "applied" {
			out = append(out, item.ID)
		}
	}
	return out
}

func conceptualReviewDecisionInputs(decisions ReviewDecisionsArtifact) []llmpipeline.ConceptualReviewDecisionInput {
	out := make([]llmpipeline.ConceptualReviewDecisionInput, 0, len(decisions.ReviewDecisions))
	for _, item := range decisions.ReviewDecisions {
		if item.ApplyStatus != "applied" {
			continue
		}
		out = append(out, llmpipeline.ConceptualReviewDecisionInput{
			ID: item.ID, Question: item.QuestionSnapshot, SelectedOptionID: item.SelectedOption,
			Rationale: item.RationaleSnapshot, AffectedAtoms: append([]string(nil), item.AffectedAtoms...),
		})
	}
	return out
}

func (s *Store) failedLogicalRepairContext(project *ProjectState) (*llmpipeline.PatchProposal, []string) {
	if project.SemanticVerificationPath != "" && project.LogicalPatchProposalPath != "" {
		var semantic SemanticVerificationReport
		if readJSON(s.absoluteWorkspacePath(project.SemanticVerificationPath), &semantic) == nil && !semantic.OK {
			var proposal llmpipeline.PatchProposal
			if readJSON(s.absoluteWorkspacePath(project.LogicalPatchProposalPath), &proposal) == nil && len(proposal.Operations) > 0 {
				issues := []string{}
				for _, issue := range semantic.Issues {
					if issue.Blocking {
						issues = append(issues, fmt.Sprintf("%s %s: %s", issue.Code, issue.ObligationID, issue.Message))
					}
				}
				if len(issues) > 0 {
					return &proposal, issues
				}
			}
		}
	}
	revisionDir := s.absoluteWorkspacePath(s.projectRevisionRel(project.ID, project.CurrentRevision+1))
	var report struct {
		OK     bool     `json:"ok"`
		Errors []string `json:"errors"`
	}
	if err := readJSON(filepath.Join(revisionDir, "validation_report.json"), &report); err != nil || report.OK || len(report.Errors) == 0 {
		return nil, nil
	}
	var proposal llmpipeline.PatchProposal
	if err := readJSON(filepath.Join(revisionDir, "dbdsl_patch.proposed.json"), &proposal); err != nil || len(proposal.Operations) == 0 {
		return nil, nil
	}
	return &proposal, append([]string(nil), report.Errors...)
}

func mustSourceUnitFile(s *Store, projectID string) dsl.V05SourceUnitsFile {
	artifacts, _ := s.SourceUnitArtifacts(projectID)
	return artifacts.Accepted
}

func invalidateLogicalArtifacts(project *ProjectState) {
	project.LogicalPatchProposalPath = ""
	project.ValidationReportPath = ""
	project.LintReportPath = ""
	project.QualityReportPath = ""
	project.ObligationRealizationsPath = ""
	project.SemanticVerificationPath = ""
	project.InvariantReportPath = ""
	project.DBMLPath = ""
	project.TraceReportPath = ""
	project.FinalModelAccepted = false
	project.ModelGenerated = false
	project.DBMLReady = false
	project.ModelPath = ""
	project.BundlePath = ""
	project.TaskPath = ""
}

func (s *Store) AcceptFinalModel(projectID string, baseRevision int) (int, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return 0, ErrRevisionConflict
	}
	if !project.ModelGenerated {
		return 0, ErrModelNotGenerated
	}
	semanticReport, err := s.SemanticVerification(projectID)
	if err != nil {
		return 0, errors.New("semantic verification is required before final model acceptance")
	}
	if !semanticReport.OK || semanticReport.BlockingIssues > 0 {
		return 0, errors.New("blocking semantic obligation issues remain")
	}
	report := qualityForProject(project)
	if report.Summary.ValidationErrors > 0 || report.Summary.BlockingIssues > 0 {
		return 0, errors.New("blocking model quality issues remain")
	}
	paths, modelPath, err := s.writeAcceptedBundleRevision(project)
	if err != nil {
		return 0, err
	}
	validation := validate.ValidateFile(modelPath)
	if !validation.OK() {
		return 0, fmt.Errorf("accepted DB-DSL validation failed: %s", strings.Join(validation.Errors, "; "))
	}
	lintResult := lint.LintFile(modelPath)
	if lintResult.HasErrors() {
		return 0, errors.New("accepted DB-DSL has blocking lint errors")
	}
	qualityReport := quality.BuildReport(modelPath, false)
	qualityReport.Summary.SemanticVerificationStatus = "passed"
	qualityReport.Summary.SemanticBlockingIssues = semanticReport.BlockingIssues
	reportPaths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"validation_report.json": {Value: map[string]any{"ok": true, "errors": validation.Errors}, JSON: true},
		"lint_report.json":       {Value: lintResult, JSON: true},
		"quality_report.json":    {Value: qualityReport, JSON: true},
	})
	if err != nil {
		return 0, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		s.applyAcceptedBundlePaths(current, paths, modelPath)
		current.ValidationReportPath = reportPaths["validation_report.json"]
		current.LintReportPath = reportPaths["lint_report.json"]
		current.QualityReportPath = reportPaths["quality_report.json"]
		current.FinalModelAccepted = true
		current.DBMLReady = false
		current.LifecycleStatus = "model_generated"
		current.LastActivity = "Final logical model accepted."
		return nil
	})
	if err != nil {
		return 0, err
	}
	project, _ = s.Project(projectID)
	return project.CurrentRevision, nil
}

func (s *Store) GenerateFinalOutputs(projectID string, baseRevision int, emit jobs.StepEmitter) (int, []string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return 0, nil, ErrRevisionConflict
	}
	if !project.FinalModelAccepted {
		return 0, nil, errors.New("final model review has not been accepted")
	}
	semanticReport, err := s.SemanticVerification(projectID)
	if err != nil || !semanticReport.OK || semanticReport.BlockingIssues > 0 {
		return 0, nil, errors.New("semantic verification must pass before final outputs")
	}
	emit = stageEmitter(emit)
	paths, modelPath, err := s.writeAcceptedBundleRevision(project)
	if err != nil {
		return 0, nil, err
	}
	emit("generate_dbml", "Generating DBML from accepted DB-DSL.", 30, nil)
	dbml, err := generate.DBMLFile(modelPath)
	if err != nil {
		return 0, nil, err
	}
	emit("generate_trace", "Generating traceability report from the same DB-DSL revision.", 58, nil)
	trace, err := generate.TraceFile(modelPath)
	if err != nil {
		return 0, nil, err
	}
	validation := validate.ValidateFile(modelPath)
	if !validation.OK() {
		return 0, nil, fmt.Errorf("accepted DB-DSL validation failed before final outputs: %s", strings.Join(validation.Errors, "; "))
	}
	lintResult := lint.LintFile(modelPath)
	if lintResult.HasErrors() {
		return 0, nil, errors.New("accepted DB-DSL has blocking lint errors before final outputs")
	}
	qualityReport := quality.BuildReport(modelPath, true)
	qualityReport.Summary.SemanticVerificationStatus = "passed"
	qualityReport.Summary.SemanticBlockingIssues = semanticReport.BlockingIssues
	reportPaths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"validation_report.json": {Value: map[string]any{"ok": true, "errors": validation.Errors}, JSON: true},
		"lint_report.json":       {Value: lintResult, JSON: true},
		"quality_report.json":    {Value: qualityReport, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	revisionRel := s.projectRevisionRel(projectID, project.CurrentRevision+1)
	dbmlRel := filepath.ToSlash(filepath.Join(revisionRel, "model.dbml"))
	traceRel := filepath.ToSlash(filepath.Join(revisionRel, "traceability_report.md"))
	if err := writeAtomic(s.absoluteWorkspacePath(dbmlRel), []byte(dbml)); err != nil {
		return 0, nil, err
	}
	if err := writeAtomic(s.absoluteWorkspacePath(traceRel), []byte(trace)); err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		s.applyAcceptedBundlePaths(current, paths, modelPath)
		current.ValidationReportPath = reportPaths["validation_report.json"]
		current.LintReportPath = reportPaths["lint_report.json"]
		current.QualityReportPath = reportPaths["quality_report.json"]
		current.DBMLPath = dbmlRel
		current.TraceReportPath = traceRel
		current.DBMLReady = true
		current.LifecycleStatus = "ready_for_dbml"
		current.LastActivity = "DBML and traceability report generated from the accepted model."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"dbml", "trace", "quality"}, nil
}

func (s *Store) writeAcceptedBundleRevision(project *ProjectState) (map[string]string, string, error) {
	bundle, err := dsl.LoadV05Bundle(project.ModelPath)
	if err != nil {
		return nil, "", err
	}
	document := *bundle.Document
	document.Model.Status = "accepted"
	document.Model.Name = strings.TrimSuffix(document.Model.Name, " LLM Draft")
	if document.Model.Description == "Offline LLM-assisted v0.5 logical database model draft. Validate, lint and review before treating as final." {
		document.Model.Description = "LLM-assisted v0.5 logical database model accepted after validation, lint and explicit human review."
	}
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"source_units.yaml":             {Value: bundle.SourceUnits},
		"requirement_atoms.yaml":        {Value: bundle.RequirementAtoms},
		"functional_decomposition.yaml": {Value: bundle.FunctionalDecomposition},
		"crud_matrix.yaml":              {Value: bundle.CRUDMatrix},
		"review_decisions.yaml":         {Value: bundle.ReviewDecisions},
		"db_model.dsl.yaml":             {Value: &document},
	})
	if err != nil {
		return nil, "", err
	}
	taskSource := project.TaskPath
	if taskSource == "" {
		taskSource = project.CombinedDocumentPath
	}
	task, err := os.ReadFile(s.absoluteWorkspacePath(taskSource))
	if err != nil {
		return nil, "", fmt.Errorf("read accepted model task: %w", err)
	}
	taskRel := filepath.ToSlash(filepath.Join(s.projectRevisionRel(project.ID, project.CurrentRevision+1), "TASK.md"))
	if err := writeAtomic(s.absoluteWorkspacePath(taskRel), task); err != nil {
		return nil, "", err
	}
	paths["TASK.md"] = taskRel
	return paths, s.absoluteWorkspacePath(paths["db_model.dsl.yaml"]), nil
}

func (s *Store) applyAcceptedBundlePaths(project *ProjectState, paths map[string]string, modelPath string) {
	project.ModelPath = modelPath
	project.BundlePath = filepath.Dir(modelPath)
	project.TaskPath = s.absoluteWorkspacePath(paths["TASK.md"])
	project.SourceUnitsPath = paths["source_units.yaml"]
	project.RequirementAtomsPath = paths["requirement_atoms.yaml"]
	project.FunctionalDecompositionPath = paths["functional_decomposition.yaml"]
	project.CRUDMatrixPath = paths["crud_matrix.yaml"]
	project.ReviewDecisionsPath = paths["review_decisions.yaml"]
	project.ModelGenerated = true
}

// RefreshModelQuality executes the deterministic validation/lint/quality gate
// against the current logical-model revision. It never repairs or mutates the
// accepted model implicitly.
func (s *Store) RefreshModelQuality(projectID string, baseRevision int, emit jobs.StepEmitter) (int, []string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return 0, nil, ErrRevisionConflict
	}
	if !project.ModelGenerated || project.ModelPath == "" {
		return 0, nil, ErrModelNotGenerated
	}
	emit = stageEmitter(emit)
	emit("validate", "Running structural DB-DSL validation.", 28, nil)
	validation := validate.ValidateFile(project.ModelPath)
	lintResult := lint.Result{Version: lint.VersionV05}
	if validation.OK() {
		emit("lint", "Running semantic lint checks.", 58, nil)
		lintResult = lint.LintFile(project.ModelPath)
	}
	emit("quality", "Building the deterministic quality report.", 78, nil)
	qualityReport := quality.BuildReport(project.ModelPath, project.DBMLReady)
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"validation_report.json": {Value: map[string]any{"ok": validation.OK(), "errors": validation.Errors}, JSON: true},
		"lint_report.json":       {Value: lintResult, JSON: true},
		"quality_report.json":    {Value: qualityReport, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.ValidationReportPath = paths["validation_report.json"]
		current.LintReportPath = paths["lint_report.json"]
		current.QualityReportPath = paths["quality_report.json"]
		current.LastActivity = "Validation, lint and quality reports refreshed."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	if !validation.OK() {
		return current.CurrentRevision, []string{"validation", "lint", "quality"}, fmt.Errorf("logical DB-DSL validation failed: %s", strings.Join(validation.Errors, "; "))
	}
	return current.CurrentRevision, []string{"validation", "lint", "quality"}, nil
}
