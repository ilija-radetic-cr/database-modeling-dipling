package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dbdsl/internal/dsl"
	"dbdsl/internal/jobs"
	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

type ReviewCandidatesArtifact struct {
	Document         map[string]any                               `json:"document" yaml:"document"`
	ReviewCandidates []llmpipeline.ProjectReviewCandidateProposal `json:"review_candidates" yaml:"review_candidates"`
}

type ReviewDecisionRecord struct {
	ID                string    `json:"id" yaml:"id"`
	CandidateID       string    `json:"candidate_id" yaml:"candidate_id"`
	QuestionSnapshot  string    `json:"question_snapshot" yaml:"question_snapshot"`
	SelectedOption    string    `json:"selected_option" yaml:"selected_option"`
	AffectedAtoms     []string  `json:"affected_atoms" yaml:"affected_atoms"`
	RationaleSnapshot string    `json:"rationale_snapshot" yaml:"rationale_snapshot"`
	ReviewedBy        string    `json:"reviewed_by" yaml:"reviewed_by"`
	ReviewedAt        time.Time `json:"reviewed_at" yaml:"reviewed_at"`
	ProjectRevision   int       `json:"project_revision" yaml:"project_revision"`
	AffectedArtifacts []string  `json:"affected_artifacts" yaml:"affected_artifacts"`
	AppliedPatchID    string    `json:"applied_patch_id" yaml:"applied_patch_id"`
	ApplyStatus       string    `json:"apply_status" yaml:"apply_status"`
	ValidationResult  string    `json:"validation_result" yaml:"validation_result"`
	NewCandidateIDs   []string  `json:"new_candidate_ids" yaml:"new_candidate_ids"`
	DecisionMode      string    `json:"decision_mode,omitempty" yaml:"decision_mode,omitempty"`
	PolicyVersion     string    `json:"policy_version,omitempty" yaml:"policy_version,omitempty"`
	ActiveReviewMS    int64     `json:"active_review_ms,omitempty" yaml:"active_review_ms,omitempty"`
}

type ReviewDecisionsArtifact struct {
	Document        map[string]any         `json:"document" yaml:"document"`
	ReviewDecisions []ReviewDecisionRecord `json:"review_decisions" yaml:"review_decisions"`
}

type ApplyReviewDecisionOptions struct {
	BaseRevision    int
	SelectedOption  string
	ReviewedBy      string
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	OnProgress      jobs.StepEmitter
}

type ModelCorrectionRequest struct {
	BaseRevision   int
	ElementID      string
	CorrectionType string
	Note           string
}

// CreateModelCorrectionCandidate turns feedback from the read-only model
// projection into an auditable blocking decision. The current draft remains
// inspectable, but acceptance and generated outputs are invalidated.
// ErrModelCorrectionUnavailable is returned in the segment-based flow, where a
// model is corrected by regenerating the conceptual model.
var ErrModelCorrectionUnavailable = errors.New("model corrections through the review queue are not available in the segment-based flow; regenerate the conceptual model instead")

func (s *Store) CreateModelCorrectionCandidate(projectID string, request ModelCorrectionRequest) (int, string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, "", ErrNotFound
	}
	if request.BaseRevision > 0 && request.BaseRevision != project.CurrentRevision {
		return 0, "", ErrRevisionConflict
	}
	if !project.ModelGenerated {
		return 0, "", ErrModelNotGenerated
	}
	if project.ConceptualDescriptionPath != "" {
		// Corrections become review candidates over requirement atoms, which the
		// segment-based flow does not produce.
		return 0, "", ErrModelCorrectionUnavailable
	}
	details, found, err := s.ElementDetails(projectID, request.ElementID)
	if err != nil {
		return 0, "", err
	}
	if !found {
		return 0, "", ErrNotFound
	}
	if len(details.Evidence.RequirementAtoms) == 0 {
		return 0, "", errors.New("model correction requires requirement-atom evidence")
	}
	candidates, decisions, err := s.loadProjectReviewArtifacts(project)
	if err != nil {
		return 0, "", err
	}
	sequence := len(candidates.ReviewCandidates) + 1
	id := fmt.Sprintf("RC-CORR-%03d", sequence)
	for {
		if _, exists := findProjectReviewCandidate(candidates.ReviewCandidates, id); !exists {
			break
		}
		sequence++
		id = fmt.Sprintf("RC-CORR-%03d", sequence)
	}
	correctionType := nonEmpty(strings.TrimSpace(request.CorrectionType), "other")
	note := nonEmpty(strings.TrimSpace(request.Note), "The user requested a controlled correction from the model element drawer.")
	candidate := llmpipeline.ProjectReviewCandidateProposal{
		ID: id, DecisionKey: "model_correction:" + request.ElementID, Question: fmt.Sprintf("How should model element %s be corrected?", request.ElementID),
		Description: note, Category: "model_correction:" + correctionType, Phase: "final_model_review", Severity: "high", Blocking: true,
		AffectedSourceUnits: append([]string(nil), details.Evidence.SourceUnits...), AffectedAtoms: append([]string(nil), details.Evidence.RequirementAtoms...),
		AffectedModelCandidates: []string{request.ElementID}, MayAffect: []string{"conceptual_model", "logical_model", "validation", "lint", "dbml", "trace"},
		Options: []llmpipeline.ReviewOptionProposal{
			{ID: "revise_model", Label: "Revise the model", Rationale: note, EffectSummary: "Invalidate the current model draft and regenerate affected downstream artifacts from a reviewed patch.", Benefits: []string{"Keeps the correction traceable"}, Risks: []string{"May change related model elements"}, AffectedArtifactKinds: []string{"requirement_atoms", "conceptual_model", "logical_model"}, Recommended: true, Effects: noChangeReviewEffects(details.Evidence.RequirementAtoms, []string{"enforceability"})},
			{ID: "keep_current", Label: "Keep the current model", Rationale: "Record the concern but retain the current interpretation.", EffectSummary: "Link the decision as evidence and regenerate the accepted projection without the requested semantic change.", Benefits: []string{"Preserves the current design"}, Risks: []string{"The reported concern remains"}, AffectedArtifactKinds: []string{"review_decisions"}, Effects: noChangeReviewEffects(details.Evidence.RequirementAtoms, []string{"documentation"})},
		},
		RecommendedOptionID: "revise_model", RecommendationConfidence: "high", Warnings: []string{},
	}
	candidates.ReviewCandidates = append(candidates.ReviewCandidates, candidate)
	var atoms llmpipeline.RequirementAtomExtractionProposal
	var functional llmpipeline.FunctionalAnalysisProposal
	var crud llmpipeline.CRUDMappingProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return 0, "", err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.FunctionalAnalysisProposalPath), &functional); err != nil {
		return 0, "", err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.CRUDMappingProposalPath), &crud); err != nil {
		return 0, "", err
	}
	qa := llmpipeline.ValidateProjectReviewProposal(llmpipeline.ProjectReviewProposal{ReviewCandidates: candidates.ReviewCandidates}, mustAcceptedSourceUnits(s, projectID), atoms.RequirementAtoms, functional.FunctionalAreas, crud.Operations)
	if !qa.OK {
		return 0, "", fmt.Errorf("model correction candidate is invalid: %s", strings.Join(qa.Errors, "; "))
	}
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"review_candidates.yaml": {Value: candidates}, "review_candidate_qa.json": {Value: qa, JSON: true}, "review_decisions.yaml": {Value: decisions},
	})
	if err != nil {
		return 0, "", err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.ReviewCandidatesPath = paths["review_candidates.yaml"]
		current.ReviewCandidateQAPath = paths["review_candidate_qa.json"]
		current.ReviewDecisionsPath = paths["review_decisions.yaml"]
		current.OpenReviewIDs[id] = true
		current.FinalModelAccepted = false
		current.DBMLReady = false
		current.DBMLPath = ""
		current.TraceReportPath = ""
		current.Completed = false
		current.CompletedSnapshot = nil
		current.LifecycleStatus = "analysis_review"
		current.LastActivity = "A controlled model correction requires a review decision."
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, id, nil
}

func noChangeReviewEffects(atomIDs, dimensions []string) *llmpipeline.ReviewOptionEffects {
	updates := make([]llmpipeline.ReviewAtomUpdate, 0, len(atomIDs))
	for _, atomID := range atomIDs {
		updates = append(updates, llmpipeline.ReviewAtomUpdate{AtomID: atomID, ModelingOutcome: "no_change", PersistenceEffect: "no_change", SupportLevel: "no_change", Confidence: "no_change"})
	}
	return &llmpipeline.ReviewOptionEffects{ModelingOutcome: "no_change", PersistenceEffect: "no_change", SupportLevel: "no_change", AtomUpdates: updates, ImpactDimensions: dimensions, FollowupCandidateIDs: []string{}}
}

func (s *Store) ReviewCandidateGenerationNeedsLLM(projectID string) (bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return false, ErrNotFound
	}
	if project.RequirementAtomsProposalPath == "" {
		return false, errors.New("requirement atoms are not ready")
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return false, err
	}
	return llmpipeline.HasReviewSemanticNeed(atoms.RequirementAtoms), nil
}

func (s *Store) GenerateReviewCandidates(ctx context.Context, client llm.Client, projectID string, opts AnalysisStageOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "review_candidates", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, units, err := s.analysisStageInputs(projectID, opts.BaseRevision)
	if err != nil {
		return 0, nil, err
	}
	if project.CRUDMappingProposalPath == "" {
		return 0, nil, errors.New("CRUD mapping is not ready")
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	var functional llmpipeline.FunctionalAnalysisProposal
	var crud llmpipeline.CRUDMappingProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return 0, nil, err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.FunctionalAnalysisProposalPath), &functional); err != nil {
		return 0, nil, err
	}
	if err := readJSON(s.absoluteWorkspacePath(project.CRUDMappingProposalPath), &crud); err != nil {
		return 0, nil, err
	}
	reviewGroupSet := map[string]bool{}
	normalizedAtoms := llmpipeline.NormalizeRequirementReviewSemantics(atoms.RequirementAtoms)
	for _, atom := range normalizedAtoms {
		if atom.RequiresReview || atom.ModelingOutcome == "deferred" || atom.ModelingOutcome == "unsupported" || atom.PersistenceEffect == "unclear" {
			key := atom.ReviewGroup
			if key == "" {
				key = atom.ID
			}
			reviewGroupSet[key] = true
		}
	}
	reviewClusters := len(reviewGroupSet)
	if reviewClusters > 0 && client == nil {
		return 0, nil, errors.New("LLM client is required for review proposal")
	}
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "review_candidates", opts.MaxOutputTokens, stageBudgetInput{ReviewClusters: reviewClusters})
	emit := stageEmitter(opts.OnProgress)
	emit("propose_review_candidates", "Identifying project-specific modeling decisions.", 24, nil)
	proposal, qa, err := llmpipeline.RunProjectReview(ctx, client, llmpipeline.ProjectReviewOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: units, RequirementAtoms: atoms.RequirementAtoms,
		FunctionalAreas: functional.FunctionalAreas, Actors: functional.Actors, Operations: crud.Operations,
		Model: opts.Model, ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
	})
	if err != nil {
		return 0, nil, err
	}
	emit("validate_review_dag", "Validating review references and dependency DAG.", 72, coverageMetadata(qa.Coverage))
	accepted := ReviewCandidatesArtifact{
		Document:         map[string]any{"id": project.ID + "_review_candidates", "pipeline_version": "0.7", "status": "proposed"},
		ReviewCandidates: proposal.ReviewCandidates,
	}
	decisions := ReviewDecisionsArtifact{Document: map[string]any{"id": project.ID + "_review_decisions", "pipeline_version": "0.7"}, ReviewDecisions: []ReviewDecisionRecord{}}
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"review_candidates.proposed.json": {Value: proposal, JSON: true},
		"review_candidates.yaml":          {Value: accepted},
		"review_candidate_qa.json":        {Value: qa, JSON: true},
		"review_decisions.yaml":           {Value: decisions},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.ReviewCandidatesProposalPath = paths["review_candidates.proposed.json"]
		current.ReviewCandidatesPath = paths["review_candidates.yaml"]
		current.ReviewCandidateQAPath = paths["review_candidate_qa.json"]
		current.ReviewDecisionsPath = paths["review_decisions.yaml"]
		current.LastAppliedPatchPath = ""
		current.OpenReviewIDs = map[string]bool{}
		current.AnsweredReviews = map[string]string{}
		for _, candidate := range proposal.ReviewCandidates {
			current.OpenReviewIDs[candidate.ID] = true
		}
		if len(current.OpenReviewIDs) == 0 {
			current.LifecycleStatus = "ready_for_model_generation"
			current.LastActivity = "Analysis completed with no blocking modeling decisions."
		} else {
			current.LifecycleStatus = "analysis_review"
			current.LastActivity = fmt.Sprintf("%d project-specific review decisions require attention.", len(current.OpenReviewIDs))
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	revision, changed := current.CurrentRevision, []string{"review_candidates", "review_decisions"}
	if automatic := autoReviewSelections(proposal.ReviewCandidates); len(automatic) > 0 {
		revision, changed, err = s.ApplyProjectReviewDecisionBatch(projectID, ApplyReviewBatchOptions{BaseRevision: current.CurrentRevision, Selections: automatic, ReviewedBy: "system_policy", DecisionMode: "auto_low_risk"})
		if err != nil {
			return revision, changed, err
		}
	}
	return revision, changed, nil
}

func (s *Store) ApplyProjectReviewDecision(ctx context.Context, client llm.Client, projectID, candidateID string, opts ApplyReviewDecisionOptions) (int, []string, error) {
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "review_resolution", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if opts.BaseRevision > 0 && opts.BaseRevision != project.CurrentRevision {
		return 0, nil, ErrRevisionConflict
	}
	candidates, decisions, err := s.loadProjectReviewArtifacts(project)
	if err != nil {
		return 0, nil, err
	}
	candidate, found := findProjectReviewCandidate(candidates.ReviewCandidates, candidateID)
	if !found || !project.OpenReviewIDs[candidateID] {
		return 0, nil, ErrNotFound
	}
	resolved := map[string]bool{}
	for _, decision := range decisions.ReviewDecisions {
		if decision.ApplyStatus == "applied" {
			resolved[decision.CandidateID] = true
		}
	}
	for _, dependency := range candidate.DependsOn {
		if !resolved[dependency] {
			return 0, nil, fmt.Errorf("review dependency %s must be resolved first", dependency)
		}
	}
	option, found := findProjectReviewOption(candidate.Options, opts.SelectedOption)
	if !found {
		return 0, nil, errors.New("selected review option does not exist")
	}
	decisionID := strings.Replace(candidate.ID, "RC-", "RD-", 1)
	if decisionID == candidate.ID {
		decisionID = "RD-" + candidate.ID
	}
	for _, existing := range decisions.ReviewDecisions {
		if existing.ID == decisionID {
			return 0, nil, errors.New("review decision already exists")
		}
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return 0, nil, err
	}
	acceptedSourceUnits := mustAcceptedSourceUnits(s, projectID)
	functional, crud, functionalProposalPath, crudProposalPath, err := s.loadReviewAnalysisContext(project)
	if err != nil {
		return 0, nil, err
	}
	emit := stageEmitter(opts.OnProgress)
	emit("apply_review_decision", "Creating a minimal resolution patch.", 25, map[string]any{"candidate_id": candidate.ID})
	var patch llmpipeline.ReviewResolutionPatchProposal
	var qa llmpipeline.StageQA
	if llmpipeline.HasStructuredReviewEffects(option) {
		patch, qa = llmpipeline.BuildDeterministicReviewResolutionPatch(candidate, option, decisionID, atoms.RequirementAtoms)
		emit("apply_review_decision", "Applying declared review effects deterministically.", 45, map[string]any{"candidate_id": candidate.ID, "llm_call": false})
	} else {
		if client == nil {
			return 0, nil, errors.New("LLM client is required for legacy review resolution")
		}
		patch, qa, err = llmpipeline.RunReviewResolutionPatch(ctx, client, llmpipeline.ReviewResolutionOptions{
			OutDir: s.projectWorkspaceDir(projectID), Candidate: candidate, SelectedOption: option, DecisionID: decisionID,
			ReservedCandidateIDs: reviewCandidateIDs(candidates.ReviewCandidates), RequirementAtoms: atoms.RequirementAtoms,
			ValidSourceUnitIDs: sourceUnitIDs(acceptedSourceUnits), ValidFunctionalAreaIDs: functionalAreaIDs(functional.FunctionalAreas),
			ValidOperationIDs: operationIDs(crud.Operations),
			Model:             opts.Model, ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		})
	}
	if err != nil {
		return 0, nil, err
	}
	if renames := normalizeNewReviewCandidateIDs(candidates.ReviewCandidates, patch.NewReviewCandidates); len(renames) > 0 {
		for _, rename := range renames {
			warning := "renumbered new review candidate " + rename + " to avoid an existing ID collision"
			patch.Warnings = append(patch.Warnings, warning)
			qa.Warnings = append(qa.Warnings, warning)
		}
	}
	referenceCatalog := reviewReferenceCatalog{
		SourceUnits: sourceUnitIDs(acceptedSourceUnits), Atoms: requirementProposalIDs(atoms.RequirementAtoms),
		FunctionalAreas: functionalAreaIDs(functional.FunctionalAreas), Operations: operationIDs(crud.Operations),
		Candidates: append(reviewCandidateIDs(candidates.ReviewCandidates), reviewCandidateIDs(patch.NewReviewCandidates)...),
	}
	if repairs := normalizeNewReviewCandidateReferences(patch.NewReviewCandidates, referenceCatalog); len(repairs) > 0 {
		for _, repair := range repairs {
			warning := "normalized new review candidate reference " + repair
			patch.Warnings = append(patch.Warnings, warning)
			qa.Warnings = append(qa.Warnings, warning)
		}
	}
	patchedAtoms, semanticChange, err := applyReviewPatch(atoms, patch, decisionID)
	if err != nil {
		return 0, nil, err
	}
	var reclassifiedObligations llmpipeline.DesignObligationsFile
	var obligationQA llmpipeline.DesignObligationQA
	if project.DesignObligationsPath != "" {
		obligationArtifacts, obligationErr := s.DesignObligations(projectID)
		if obligationErr != nil {
			return 0, nil, obligationErr
		}
		reclassifiedObligations, obligationQA = llmpipeline.ReclassifyDesignObligations(obligationArtifacts.Accepted, patchedAtoms.RequirementAtoms)
		if !obligationQA.OK {
			return 0, nil, fmt.Errorf("reclassified design obligations failed QA: %s", strings.Join(obligationQA.Errors, "; "))
		}
	}
	emit("validate_review_patch", "Validating the patched analysis copy.", 68, coverageMetadata(qa.Coverage))
	atomQA := llmpipeline.ValidateRequirementAtomProposal(patchedAtoms, acceptedSourceUnits)
	if !atomQA.OK {
		return 0, nil, fmt.Errorf("patched requirement atoms failed QA: %s", strings.Join(atomQA.Errors, "; "))
	}
	newCandidates := append(append([]llmpipeline.ProjectReviewCandidateProposal{}, candidates.ReviewCandidates...), patch.NewReviewCandidates...)
	reviewQA := llmpipeline.ValidateProjectReviewProposal(llmpipeline.ProjectReviewProposal{ReviewCandidates: newCandidates}, acceptedSourceUnits, patchedAtoms.RequirementAtoms, functional.FunctionalAreas, crud.Operations)
	if !reviewQA.OK {
		return 0, nil, fmt.Errorf("review DAG after patch is invalid: %s", strings.Join(reviewQA.Errors, "; "))
	}
	newCandidateIDs := make([]string, 0, len(patch.NewReviewCandidates))
	for _, item := range patch.NewReviewCandidates {
		newCandidateIDs = append(newCandidateIDs, item.ID)
	}
	patchID := "PATCH-" + decisionID
	decisions.ReviewDecisions = append(decisions.ReviewDecisions, ReviewDecisionRecord{
		ID: decisionID, CandidateID: candidate.ID, QuestionSnapshot: candidate.Question, SelectedOption: option.ID,
		AffectedAtoms:     append([]string(nil), candidate.AffectedAtoms...),
		RationaleSnapshot: option.Rationale, ReviewedBy: nonEmpty(opts.ReviewedBy, "web_user"), ReviewedAt: time.Now(),
		ProjectRevision: project.CurrentRevision, AffectedArtifacts: patch.AffectedArtifacts, AppliedPatchID: patchID,
		ApplyStatus: "applied", ValidationResult: "passed", NewCandidateIDs: newCandidateIDs,
	})
	acceptedCandidates := ReviewCandidatesArtifact{Document: candidates.Document, ReviewCandidates: newCandidates}
	acceptedAtoms := buildRequirementAtomsArtifact(project, patchedAtoms)
	artifacts := map[string]artifactValue{
		"requirement_atoms.proposed.json": {Value: patchedAtoms, JSON: true}, "requirement_atoms.yaml": {Value: acceptedAtoms},
		"requirement_atom_qa.json": {Value: atomQA, JSON: true}, "review_candidates.yaml": {Value: acceptedCandidates},
		"review_candidate_qa.json": {Value: reviewQA, JSON: true}, "review_decisions.yaml": {Value: decisions},
		"review_resolution_patch.proposed.json": {Value: patch, JSON: true}, "review_resolution_patch_qa.json": {Value: qa, JSON: true},
	}
	if project.DesignObligationsPath != "" {
		artifacts["design_obligations.proposed.json"] = artifactValue{Value: reclassifiedObligations, JSON: true}
		artifacts["design_obligations.yaml"] = artifactValue{Value: reclassifiedObligations}
		artifacts["design_obligation_qa.json"] = artifactValue{Value: obligationQA, JSON: true}
	}
	paths, err := s.writeAnalysisRevision(project, artifacts)
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.RequirementAtomsProposalPath = paths["requirement_atoms.proposed.json"]
		current.RequirementAtomsPath = paths["requirement_atoms.yaml"]
		current.RequirementAtomQAPath = paths["requirement_atom_qa.json"]
		if path := paths["design_obligations.proposed.json"]; path != "" {
			current.DesignObligationsProposalPath = path
			current.DesignObligationsPath = paths["design_obligations.yaml"]
			current.DesignObligationQAPath = paths["design_obligation_qa.json"]
		}
		current.ReviewCandidatesPath = paths["review_candidates.yaml"]
		current.ReviewCandidateQAPath = paths["review_candidate_qa.json"]
		current.ReviewDecisionsPath = paths["review_decisions.yaml"]
		current.LastAppliedPatchPath = paths["review_resolution_patch.proposed.json"]
		delete(current.OpenReviewIDs, candidate.ID)
		current.AnsweredReviews[candidate.ID] = option.ID
		for _, item := range patch.NewReviewCandidates {
			current.OpenReviewIDs[item.ID] = true
		}
		invalidateModelArtifacts(current)
		restoreReviewAnalysisPaths(current, functionalProposalPath, crudProposalPath)
		if len(current.OpenReviewIDs) == 0 && current.FunctionalAnalysisProposalPath != "" && current.CRUDMappingProposalPath != "" {
			current.AnalysisReady = true
			current.LifecycleStatus = "ready_for_model_generation"
			if semanticChange {
				current.LastActivity = "All blocking review decisions are resolved; semantic requirement changes were validated against the review analysis snapshot."
			} else {
				current.LastActivity = "All blocking review decisions are resolved."
			}
		} else if len(current.OpenReviewIDs) == 0 {
			current.AnalysisReady = false
			current.LifecycleStatus = "sources_processed"
			current.LastActivity = "Review decisions are resolved, but downstream analysis must be regenerated."
		} else {
			current.AnalysisReady = false
			current.LifecycleStatus = "analysis_review"
			if semanticChange {
				current.LastActivity = "Review decision applied; resolve the remaining dependent decisions against the preserved analysis snapshot."
			} else {
				current.LastActivity = "Review decision applied."
			}
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"review_decisions", "review_candidates", "requirement_atoms"}, nil
}

// loadReviewAnalysisContext keeps a review DAG anchored to the FA/CRUD snapshot
// against which it was originally validated. A semantic review patch may have
// cleared the active downstream pointers in an older workspace revision even
// though dependent candidates still reference that snapshot.
func (s *Store) loadReviewAnalysisContext(project *ProjectState) (llmpipeline.FunctionalAnalysisProposal, llmpipeline.CRUDMappingProposal, string, string, error) {
	functionalPath := project.FunctionalAnalysisProposalPath
	if functionalPath == "" {
		functionalPath = s.latestRevisionArtifact(project.ID, project.CurrentRevision, "functional_analysis.proposed.json")
	}
	crudPath := project.CRUDMappingProposalPath
	if crudPath == "" {
		crudPath = s.latestRevisionArtifact(project.ID, project.CurrentRevision, "crud_mapping.proposed.json")
	}
	if functionalPath == "" || crudPath == "" {
		return llmpipeline.FunctionalAnalysisProposal{}, llmpipeline.CRUDMappingProposal{}, functionalPath, crudPath,
			errors.New("review analysis snapshot is unavailable; regenerate functional analysis, CRUD mapping, and review candidates")
	}
	var functional llmpipeline.FunctionalAnalysisProposal
	if err := readJSON(s.absoluteWorkspacePath(functionalPath), &functional); err != nil {
		return functional, llmpipeline.CRUDMappingProposal{}, functionalPath, crudPath, fmt.Errorf("load review functional-analysis snapshot: %w", err)
	}
	var crud llmpipeline.CRUDMappingProposal
	if err := readJSON(s.absoluteWorkspacePath(crudPath), &crud); err != nil {
		return functional, crud, functionalPath, crudPath, fmt.Errorf("load review CRUD snapshot: %w", err)
	}
	return functional, crud, functionalPath, crudPath, nil
}

func (s *Store) latestRevisionArtifact(projectID string, currentRevision int, name string) string {
	for revision := currentRevision; revision >= 1; revision-- {
		rel := filepath.ToSlash(filepath.Join(s.projectRevisionRel(projectID, revision), name))
		info, err := os.Stat(s.absoluteWorkspacePath(rel))
		if err == nil && !info.IsDir() {
			return rel
		}
	}
	return ""
}

func restoreReviewAnalysisPaths(project *ProjectState, functionalProposalPath, crudProposalPath string) {
	if project.FunctionalAnalysisProposalPath == "" && functionalProposalPath != "" {
		dir := filepath.Dir(functionalProposalPath)
		project.FunctionalAnalysisProposalPath = functionalProposalPath
		project.FunctionalDecompositionPath = filepath.ToSlash(filepath.Join(dir, "functional_decomposition.yaml"))
		project.FunctionalAnalysisQAPath = filepath.ToSlash(filepath.Join(dir, "functional_analysis_qa.json"))
	}
	if project.CRUDMappingProposalPath == "" && crudProposalPath != "" {
		dir := filepath.Dir(crudProposalPath)
		project.CRUDMappingProposalPath = crudProposalPath
		project.CRUDMatrixPath = filepath.ToSlash(filepath.Join(dir, "crud_matrix.yaml"))
		project.CRUDMappingQAPath = filepath.ToSlash(filepath.Join(dir, "crud_mapping_qa.json"))
	}
}

func (s *Store) loadProjectReviewArtifacts(project *ProjectState) (ReviewCandidatesArtifact, ReviewDecisionsArtifact, error) {
	if project.ReviewCandidatesPath == "" || project.ReviewDecisionsPath == "" {
		return ReviewCandidatesArtifact{}, ReviewDecisionsArtifact{}, ErrNotFound
	}
	var candidates ReviewCandidatesArtifact
	var decisions ReviewDecisionsArtifact
	if err := readYAML(s.absoluteWorkspacePath(project.ReviewCandidatesPath), &candidates); err != nil {
		return candidates, decisions, err
	}
	if err := readYAML(s.absoluteWorkspacePath(project.ReviewDecisionsPath), &decisions); err != nil {
		return candidates, decisions, err
	}
	return candidates, decisions, nil
}

func findProjectReviewCandidate(items []llmpipeline.ProjectReviewCandidateProposal, id string) (llmpipeline.ProjectReviewCandidateProposal, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return llmpipeline.ProjectReviewCandidateProposal{}, false
}
func findProjectReviewOption(items []llmpipeline.ReviewOptionProposal, id string) (llmpipeline.ReviewOptionProposal, bool) {
	for _, item := range items {
		if item.ID == id {
			return item, true
		}
	}
	return llmpipeline.ReviewOptionProposal{}, false
}

func reviewCandidateIDs(items []llmpipeline.ProjectReviewCandidateProposal) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) != "" {
			ids = append(ids, item.ID)
		}
	}
	return ids
}

func sourceUnitIDs(items []dsl.SourceUnit) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func requirementProposalIDs(items []llmpipeline.RequirementAtomProposal) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func functionalAreaIDs(items []llmpipeline.FunctionalAreaProposal) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func operationIDs(items []llmpipeline.CRUDOperationProposal) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

type reviewReferenceCatalog struct {
	SourceUnits     []string
	Atoms           []string
	FunctionalAreas []string
	Operations      []string
	Candidates      []string
}

func normalizeNewReviewCandidateReferences(proposed []llmpipeline.ProjectReviewCandidateProposal, catalog reviewReferenceCatalog) []string {
	repairs := []string{}
	for i := range proposed {
		candidate := &proposed[i]
		candidate.AffectedSourceUnits = normalizeReferenceList(candidate.ID, "affected_source_units", candidate.AffectedSourceUnits, catalog.SourceUnits, &repairs)
		candidate.AffectedAtoms = normalizeReferenceList(candidate.ID, "affected_atoms", candidate.AffectedAtoms, catalog.Atoms, &repairs)
		candidate.AffectedFunctionalAreas = normalizeReferenceList(candidate.ID, "affected_functional_areas", candidate.AffectedFunctionalAreas, catalog.FunctionalAreas, &repairs)
		candidate.AffectedOperations = normalizeReferenceList(candidate.ID, "affected_operations", candidate.AffectedOperations, catalog.Operations, &repairs)
		candidate.DependsOn = normalizeReferenceList(candidate.ID, "depends_on", candidate.DependsOn, catalog.Candidates, &repairs)
	}
	return repairs
}

func normalizeReferenceList(candidateID, field string, values, allowed []string, repairs *[]string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if replacement, ok := uniqueNearReference(normalized, allowed); ok {
			*repairs = append(*repairs, fmt.Sprintf("%s.%s %s -> %s", candidateID, field, normalized, replacement))
			normalized = replacement
		}
		if !seen[normalized] {
			out = append(out, normalized)
			seen[normalized] = true
		}
	}
	return out
}

func uniqueNearReference(value string, allowed []string) (string, bool) {
	if value == "" {
		return "", false
	}
	prefix := strings.SplitN(value, "-", 2)[0]
	best, bestDistance, tied := "", int(^uint(0)>>1), false
	for _, candidate := range allowed {
		if candidate == value {
			return "", false
		}
		if strings.SplitN(candidate, "-", 2)[0] != prefix {
			continue
		}
		distance := levenshteinDistance(value, candidate)
		if distance < bestDistance {
			best, bestDistance, tied = candidate, distance, false
		} else if distance == bestDistance {
			tied = true
		}
	}
	maxLength := len([]rune(value))
	if candidateLength := len([]rune(best)); candidateLength > maxLength {
		maxLength = candidateLength
	}
	threshold := 1
	if maxLength >= 10 {
		threshold = 2
	}
	if maxLength >= 18 {
		threshold = 3
	}
	if best == "" || tied || bestDistance > threshold {
		return "", false
	}
	return best, true
}

func levenshteinDistance(left, right string) int {
	a, b := []rune(left), []rune(right)
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			current[j] = minReviewDistance(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func minReviewDistance(values ...int) int {
	minimum := values[0]
	for _, value := range values[1:] {
		if value < minimum {
			minimum = value
		}
	}
	return minimum
}

// normalizeNewReviewCandidateIDs preserves a valid LLM-proposed ID when it is
// unused, but deterministically allocates the next RC-NNN sequence whenever a
// proposal is empty or collides with an accepted candidate. Option IDs are
// renamed with the candidate so the accepted review artifact stays coherent.
func normalizeNewReviewCandidateIDs(existing, proposed []llmpipeline.ProjectReviewCandidateProposal) []string {
	used := map[string]bool{}
	maxSequence := 0
	observe := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		used[id] = true
		if strings.HasPrefix(id, "RC-") {
			if sequence, err := strconv.Atoi(strings.TrimPrefix(id, "RC-")); err == nil && sequence > maxSequence {
				maxSequence = sequence
			}
		}
	}
	for _, item := range existing {
		observe(item.ID)
	}
	for _, item := range proposed {
		id := strings.TrimSpace(item.ID)
		if !used[id] {
			observe(id)
		}
	}
	allocate := func() string {
		for {
			maxSequence++
			id := fmt.Sprintf("RC-%03d", maxSequence)
			if !used[id] {
				used[id] = true
				return id
			}
		}
	}

	// Reset proposed IDs from the reservation pass and claim them in order so
	// duplicates within the same LLM response are handled as well.
	for _, item := range proposed {
		if id := strings.TrimSpace(item.ID); id != "" {
			delete(used, id)
		}
	}
	for _, item := range existing {
		observe(item.ID)
	}
	renamed := []string{}
	for i := range proposed {
		item := &proposed[i]
		oldID := strings.TrimSpace(item.ID)
		if oldID != "" && !used[oldID] {
			item.ID = oldID
			used[oldID] = true
			continue
		}
		newID := allocate()
		oldRecommended := item.RecommendedOptionID
		for optionIndex := range item.Options {
			oldOptionID := item.Options[optionIndex].ID
			newOptionID := fmt.Sprintf("%s-O%d", newID, optionIndex+1)
			item.Options[optionIndex].ID = newOptionID
			if oldOptionID == oldRecommended {
				item.RecommendedOptionID = newOptionID
			}
		}
		if item.RecommendedOptionID == oldRecommended {
			for _, option := range item.Options {
				if option.Recommended {
					item.RecommendedOptionID = option.ID
					break
				}
			}
		}
		item.ID = newID
		displayOldID := oldID
		if displayOldID == "" {
			displayOldID = "<empty>"
		}
		renamed = append(renamed, displayOldID+" -> "+newID)
	}
	return renamed
}

func applyReviewPatch(input llmpipeline.RequirementAtomExtractionProposal, patch llmpipeline.ReviewResolutionPatchProposal, decisionID string) (llmpipeline.RequirementAtomExtractionProposal, bool, error) {
	out := input
	out.RequirementAtoms = append([]llmpipeline.RequirementAtomProposal(nil), input.RequirementAtoms...)
	index := map[string]int{}
	for i, atom := range out.RequirementAtoms {
		index[atom.ID] = i
	}
	semantic := false
	for _, operation := range patch.Operations {
		i, ok := index[operation.TargetID]
		if !ok {
			return out, semantic, fmt.Errorf("unknown patch target %s", operation.TargetID)
		}
		atom := &out.RequirementAtoms[i]
		switch operation.Operation {
		case "link_review_decision":
			if operation.Value != decisionID {
				return out, semantic, errors.New("patch links the wrong decision")
			}
			if !containsString(atom.ReviewDecisions, decisionID) {
				atom.ReviewDecisions = append(atom.ReviewDecisions, decisionID)
			}
			atom.RequiresReview = false
		case "update_modeling_outcome":
			atom.ModelingOutcome = operation.Value
			semantic = true
		case "update_persistence_effect":
			atom.PersistenceEffect = operation.Value
			semantic = true
		case "update_support_level":
			atom.SupportLevel = operation.Value
			semantic = true
		case "update_confidence":
			atom.Confidence = operation.Value
			semantic = true
		case "mark_deferred":
			atom.ModelingOutcome = "deferred"
			semantic = true
		default:
			return out, semantic, fmt.Errorf("unsupported review patch operation %s", operation.Operation)
		}
	}
	// A decision may turn an atom into a human-approved assumption. Atom QA
	// requires such atoms to carry a warning, so record which decision made it.
	for i := range out.RequirementAtoms {
		atom := &out.RequirementAtoms[i]
		if atom.SupportLevel != "assumption" && atom.Confidence != "low" {
			continue
		}
		if !containsString(atom.ReviewDecisions, decisionID) || len(atom.Warnings) > 0 {
			continue
		}
		atom.Warnings = append(atom.Warnings, fmt.Sprintf("Assumption approved by review decision %s; not stated explicitly in the source.", decisionID))
	}
	out.RequirementAtoms = llmpipeline.NormalizeRequirementReviewSemantics(out.RequirementAtoms)
	return out, semantic, nil
}

func containsString(items []string, needle string) bool {
	for _, item := range items {
		if item == needle {
			return true
		}
	}
	return false
}

func mustAcceptedSourceUnits(s *Store, projectID string) []dsl.SourceUnit {
	artifacts, err := s.SourceUnitArtifacts(projectID)
	if err != nil {
		return nil
	}
	return artifacts.Accepted.SourceUnits
}

func (s *Store) projectReviewCandidates(projectID string) ([]ReviewCandidate, bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, false, ErrNotFound
	}
	if project.ReviewCandidatesPath == "" {
		return nil, false, nil
	}
	candidates, decisions, err := s.loadProjectReviewArtifacts(project)
	if err != nil {
		return nil, false, err
	}
	selected := map[string]string{}
	for _, item := range decisions.ReviewDecisions {
		selected[item.CandidateID] = item.SelectedOption
	}
	out := make([]ReviewCandidate, 0, len(candidates.ReviewCandidates))
	for _, item := range candidates.ReviewCandidates {
		status := "open"
		option := selected[item.ID]
		if option != "" {
			status = "answered"
		}
		options := make([]ReviewOption, 0, len(item.Options))
		for _, choice := range item.Options {
			options = append(options, ReviewOption{ID: choice.ID, Label: choice.Label, Recommended: choice.Recommended, Rationale: choice.Rationale, EffectSummary: choice.EffectSummary, Benefits: choice.Benefits, Risks: choice.Risks, AffectedArtifactKinds: choice.AffectedArtifactKinds, Effects: choice.Effects})
		}
		out = append(out, ReviewCandidate{ID: item.ID, DecisionKey: item.DecisionKey, Question: item.Question, Description: item.Description, Status: status, AffectedAtoms: item.AffectedAtoms,
			DependsOn: item.DependsOn, MayAffect: item.MayAffect, Options: options, SelectedOption: option, RecommendedID: item.RecommendedOptionID,
			Category: item.Category, Phase: item.Phase, Severity: item.Severity, Blocking: item.Blocking, AffectedSourceUnits: item.AffectedSourceUnits,
			AffectedFunctionalAreas: item.AffectedFunctionalAreas, AffectedOperations: item.AffectedOperations, AffectedModelCandidates: item.AffectedModelCandidates,
			RecommendationConfidence: item.RecommendationConfidence, Warnings: item.Warnings})
	}
	return out, true, nil
}

func (s *Store) projectReviewDecisions(projectID string) ([]ReviewDecision, bool, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return nil, false, ErrNotFound
	}
	if project.ReviewDecisionsPath == "" {
		return nil, false, nil
	}
	_, decisions, err := s.loadProjectReviewArtifacts(project)
	if err != nil {
		return nil, false, err
	}
	out := make([]ReviewDecision, 0, len(decisions.ReviewDecisions))
	for _, item := range decisions.ReviewDecisions {
		out = append(out, ReviewDecision{ID: item.ID, Question: item.QuestionSnapshot, SelectedOption: item.SelectedOption,
			AffectedAtoms: item.AffectedAtoms,
			Status:        item.ValidationResult, Rationale: item.RationaleSnapshot, ReviewedBy: item.ReviewedBy, ReviewedAt: item.ReviewedAt,
			ProjectRevision: item.ProjectRevision, AffectedArtifacts: item.AffectedArtifacts, AppliedPatchID: item.AppliedPatchID, ApplyStatus: item.ApplyStatus, NewCandidateIDs: item.NewCandidateIDs,
			DecisionMode: item.DecisionMode, PolicyVersion: item.PolicyVersion, ActiveReviewMS: item.ActiveReviewMS})
	}
	return out, true, nil
}
