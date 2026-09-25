package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	// Description is the rich LLM description the model was derived from in
	// the segment-based flow.
	Description *llmpipeline.ConceptualDescription `json:"description,omitempty"`
}

// GenerateConceptualModel is the second LLM interaction of the segment-based
// flow: the numbered source units go to the conceptual prompt, and the returned
// description is transformed deterministically into the conceptual model.
// Requirement atoms, functional analysis, CRUD mapping and review questions are
// not inputs any more; generateConceptualModelFromAnalysis keeps the former flow.
func (s *Store) GenerateConceptualModel(ctx context.Context, client llm.Client, projectID string, opts ModelStageOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "conceptual_model", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if opts.BaseRevision > 0 && opts.BaseRevision != project.CurrentRevision {
		return 0, nil, ErrRevisionConflict
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for conceptual modeling")
	}
	sourceArtifacts, err := s.SourceUnitArtifacts(projectID)
	if err != nil || len(sourceArtifacts.Accepted.SourceUnits) == 0 {
		return 0, nil, errors.New("source units are not ready")
	}
	if len(sourceArtifacts.QA.NeedsAttention) > 0 {
		return 0, nil, errors.New("source units need review before conceptual modeling")
	}
	units := sourceArtifacts.Accepted.SourceUnits
	resolvedProfile := defaultLLMExecutionProfile(opts.Model)
	if project.LLMExecutionProfile != nil {
		resolvedProfile = *project.LLMExecutionProfile
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "conceptual_model", opts.MaxOutputTokens, stageBudgetInput{Units: len(units)})
	emit := stageEmitter(opts.OnProgress)
	emit("propose_conceptual_model", "Describing what the system must remember from the numbered segments.", 22, map[string]any{"source_units": len(units)})
	controls := s.resolveLLMExecutionControls(projectID)
	description, descriptionQA, err := llmpipeline.RunConceptualDescription(ctx, client, llmpipeline.ConceptualDescriptionOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: units, Model: opts.Model, ReasoningEffort: opts.ReasoningEffort,
		MaxOutputTokens: opts.MaxOutputTokens, PromptVersion: controls.PromptVersion,
	})
	if err != nil {
		return 0, nil, err
	}
	emit("validate_conceptual_model", "Transforming the description into the conceptual model and validating it.", 72, coverageMetadata(descriptionQA.Coverage))
	proposal := llmpipeline.ConceptualDescriptionToModel(description, units)
	qa := llmpipeline.ValidateConceptualModel(proposal, units, nil)
	// The conceptual validator repeats the proposal's own warnings; each is shown once.
	qa.Warnings = distinctStrings(descriptionQA.Warnings, proposal.Warnings, qa.Warnings)
	for key, value := range descriptionQA.Coverage {
		qa.Coverage["description_"+key] = value
	}
	// The proposal is written even when QA fails, so the conceptual review can
	// show what the model returned instead of discarding a paid generation.
	emit("write_conceptual_model", "Writing the description and the derived conceptual model.", 88, nil)
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"conceptual_description.json":    {Value: description, JSON: true},
		"conceptual_model.proposed.json": {Value: proposal, JSON: true},
		"conceptual_model_qa.json":       {Value: qa, JSON: true},
		"conceptual_model_diff.json":     {Value: conceptualModelDiff(s, project, proposal), JSON: true},
		"llm_execution_profile.json":     {Value: map[string]any{"profile": resolvedProfile, "resolved_stage": map[string]any{"stage": "conceptual_model", "model": opts.Model, "reasoning_effort": opts.ReasoningEffort, "max_output_tokens": opts.MaxOutputTokens}}, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		invalidateLogicalArtifacts(current)
		current.ConceptualDescriptionPath = paths["conceptual_description.json"]
		current.ConceptualModelProposalPath = paths["conceptual_model.proposed.json"]
		current.ConceptualModelAcceptedPath = ""
		current.ConceptualModelQAPath = paths["conceptual_model_qa.json"]
		current.ConceptualModelDiffPath = paths["conceptual_model_diff.json"]
		if current.LLMExecutionProfile == nil {
			profile := resolvedProfile
			current.LLMExecutionProfile = &profile
		}
		current.LifecycleStatus = "conceptual_review"
		current.LastActivity = "Conceptual model derived from the segment description; it awaits explicit acceptance."
		if !qa.OK {
			current.LastActivity = "Conceptual model derived from the segment description has QA errors; regenerate it."
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"conceptual_model"}, nil
}

// LogicalMappingReport returns the audit trail written next to the current
// logical patch; LLM projections record only their strategy.
func (s *Store) LogicalMappingReport(projectID string) (map[string]any, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, ErrNotFound
	}
	if project.LogicalPatchProposalPath == "" {
		return nil, ErrNotFound
	}
	path := filepath.Join(filepath.Dir(s.absoluteWorkspacePath(project.LogicalPatchProposalPath)), "logical_mapping_report.json")
	report := map[string]any{}
	if err := readJSON(path, &report); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return report, nil
}

func mappingReportValue(report *llmpipeline.LogicalMappingReport) any {
	if report == nil {
		return map[string]any{"strategy": "llm"}
	}
	return report
}

func (s *Store) GenerateLogicalModel(ctx context.Context, client llm.Client, projectID string, opts ModelStageOptions) (int, []string, error) {
	_, _ = ctx, client // The logical projection is deterministic; no LLM call is made.
	project, units, conceptual, err := s.segmentFlowLogicalInputs(projectID, opts.BaseRevision)
	if err != nil {
		return 0, nil, err
	}
	emit := stageEmitter(opts.OnProgress)
	emit("project_logical_model", "Mapping the accepted conceptual model into DB-DSL with deterministic rules (no LLM).", 18, map[string]any{"strategy": "deterministic", "rule_version": llmpipeline.LogicalMappingRuleVersion})
	dslDecisions := []dsl.ReviewDecision{}
	sourceUnitFile := mustSourceUnitFile(s, projectID)
	validateCandidate := func(candidate llmpipeline.PatchProposal) []string {
		artifacts, buildErr := llmpipeline.BuildLogicalArtifacts(project.Name, units, candidate, dslDecisions)
		if buildErr != nil {
			return []string{"build logical artifacts: " + buildErr.Error()}
		}
		bundle := &dsl.Bundle{Document: &artifacts.Model, SourceUnits: &sourceUnitFile, ReviewDecisions: &artifacts.ReviewDecisions}
		return validate.ValidateV06Bundle(bundle).Errors
	}
	// Labels of the segment flow are written in the document's language, but
	// attribute names are ASCII and would read as English.
	mapped, report, mapErr := llmpipeline.MapConceptualToLogical(conceptual, llmpipeline.LogicalMappingOptions{
		Language: s.detectSourceLanguage(projectID),
	})
	if mapErr != nil {
		return 0, nil, mapErr
	}
	if validationErrors := validateCandidate(mapped); len(validationErrors) > 0 {
		return 0, nil, fmt.Errorf("deterministic logical mapping failed DB-DSL validation: %s", strings.Join(validationErrors, "; "))
	}
	patch, mappingReport := mapped, &report
	patchQA := llmpipeline.StageQA{OK: true, Errors: []string{}, Warnings: report.Warnings, Coverage: map[string]int{
		"patch_operations": len(mapped.Operations), "entities": report.Entities, "relationships": report.Relationships,
		"constraints": report.Constraints, "state_machines": report.StateMachines, "derived_views": report.DerivedViews,
	}}
	artifacts, err := llmpipeline.BuildLogicalArtifacts(project.Name, units, patch, dslDecisions)
	if err != nil {
		return 0, nil, err
	}
	artifacts.Model.Source.DerivationStrategy = llmpipeline.DescriptionTransformVersion + "+" + llmpipeline.LogicalMappingRuleVersion
	emit("write_logical_bundle", "Writing an isolated logical-model revision.", 48, coverageMetadata(patchQA.Coverage))
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"source_units.yaml":           {Value: sourceUnitFile},
		"review_decisions.yaml":       {Value: artifacts.ReviewDecisions},
		"dbdsl_patch.proposed.json":   {Value: patch, JSON: true},
		"db_model.dsl.yaml":           {Value: artifacts.Model},
		"logical_mapping_report.json": {Value: mappingReportValue(mappingReport), JSON: true},
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
	lintResult := lint.Result{Version: lint.VersionV06}
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

// segmentFlowLogicalInputs loads what the logical stage needs in the
// segment-based flow: accepted source units, the accepted conceptual model and
// the description it came from, plus the evidence files derived from them.
func (s *Store) segmentFlowLogicalInputs(projectID string, baseRevision int) (*ProjectState, []dsl.SourceUnit, llmpipeline.ConceptualModelProposal, error) {
	var conceptual llmpipeline.ConceptualModelProposal
	project, ok := s.Project(projectID)
	if !ok {
		return nil, nil, conceptual, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return nil, nil, conceptual, ErrRevisionConflict
	}
	if project.ConceptualModelAcceptedPath == "" {
		return nil, nil, conceptual, errors.New("accepted conceptual model is not ready")
	}
	units := mustAcceptedSourceUnits(s, projectID)
	if len(units) == 0 {
		return nil, nil, conceptual, errors.New("source units are not ready")
	}
	if err := readJSON(s.absoluteWorkspacePath(project.ConceptualModelAcceptedPath), &conceptual); err != nil {
		return nil, nil, conceptual, err
	}
	if qa := llmpipeline.ValidateConceptualModel(conceptual, units, nil); !qa.OK {
		return nil, nil, conceptual, fmt.Errorf("accepted conceptual model is not ready for logical mapping: %s", strings.Join(qa.Errors, "; "))
	}
	return project, units, conceptual, nil
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
	normalizeConceptualModelCollections(&out.Proposed)
	if project.ConceptualModelAcceptedPath != "" {
		var accepted llmpipeline.ConceptualModelProposal
		if err := readJSON(s.absoluteWorkspacePath(project.ConceptualModelAcceptedPath), &accepted); err != nil {
			return out, err
		}
		normalizeConceptualModelCollections(&accepted)
		out.Accepted = &accepted
		out.IsAccepted = true
	}
	if err := readJSON(s.absoluteWorkspacePath(project.ConceptualModelQAPath), &out.QA); err != nil {
		return out, err
	}
	if project.ConceptualModelDiffPath != "" {
		_ = readJSON(s.absoluteWorkspacePath(project.ConceptualModelDiffPath), &out.Diff)
	}
	if project.ConceptualDescriptionPath != "" {
		var description llmpipeline.ConceptualDescription
		if err := readJSON(s.absoluteWorkspacePath(project.ConceptualDescriptionPath), &description); err == nil {
			out.Description = &description
		}
	}
	return out, nil
}

func normalizeConceptualModelCollections(model *llmpipeline.ConceptualModelProposal) {
	if model.EntityConcepts == nil {
		model.EntityConcepts = []llmpipeline.ConceptualEntityProposal{}
	}
	if model.Relationships == nil {
		model.Relationships = []llmpipeline.ConceptualRelationshipProposal{}
	}
	if model.ConstraintConcepts == nil {
		model.ConstraintConcepts = []llmpipeline.ConceptualConstraintProposal{}
	}
	if model.LifecycleConcepts == nil {
		model.LifecycleConcepts = []llmpipeline.PlanElementProposal{}
	}
	if model.DerivedConcepts == nil {
		model.DerivedConcepts = []llmpipeline.PlanElementProposal{}
	}
	if model.FileConcepts == nil {
		model.FileConcepts = []llmpipeline.PlanElementProposal{}
	}
	if model.ImportConcepts == nil {
		model.ImportConcepts = []llmpipeline.PlanElementProposal{}
	}
	if model.UnresolvedReviewIDs == nil {
		model.UnresolvedReviewIDs = []string{}
	}
	if model.Warnings == nil {
		model.Warnings = []string{}
	}
	if model.ConfidenceSummary == nil {
		model.ConfidenceSummary = map[string]string{}
	}
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
		"pipeline_version": llmpipeline.PipelineVersion,
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

func mustSourceUnitFile(s *Store, projectID string) dsl.SourceUnitsFile {
	artifacts, _ := s.SourceUnitArtifacts(projectID)
	return artifacts.Accepted
}

func invalidateLogicalArtifacts(project *ProjectState) {
	project.LogicalPatchProposalPath = ""
	project.ValidationReportPath = ""
	project.LintReportPath = ""
	project.QualityReportPath = ""
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
	bundle, err := dsl.LoadV06Bundle(project.ModelPath)
	if err != nil {
		return nil, "", err
	}
	document := *bundle.Document
	document.Model.Status = "accepted"
	document.Model.Name = strings.TrimSuffix(document.Model.Name, " LLM Draft")
	if document.Model.Description == "LLM-assisted v0.6 logical database model draft. Validate, lint and review before treating as final." {
		document.Model.Description = "LLM-assisted v0.6 logical database model accepted after validation, lint and explicit human review."
	}
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"source_units.yaml":     {Value: bundle.SourceUnits},
		"review_decisions.yaml": {Value: bundle.ReviewDecisions},
		"db_model.dsl.yaml":     {Value: &document},
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
	// ReviewDecisionsPath stays on the workbench decision records: the bundle's
	// review_decisions.yaml is a DB-DSL export with a different schema, and
	// pointing here at it made any later logical rerun read empty decisions.
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
	lintResult := lint.Result{Version: lint.VersionV06}
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

// distinctStrings concatenates the lists, keeping the first occurrence of each value.
func distinctStrings(lists ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, list := range lists {
		for _, value := range list {
			if !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	return out
}
