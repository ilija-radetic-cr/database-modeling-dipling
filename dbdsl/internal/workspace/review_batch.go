package workspace

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"dbdsl/internal/llmpipeline"
)

const reviewRiskPolicyVersion = "review_risk_value_v1"

type ReviewSelection struct {
	CandidateID      string `json:"candidate_id"`
	SelectedOptionID string `json:"selected_option_id"`
}

type ApplyReviewBatchOptions struct {
	BaseRevision   int
	Selections     []ReviewSelection
	ReviewedBy     string
	DecisionMode   string
	ActiveReviewMS int64
}

func (s *Store) ApplyProjectReviewDecisionBatch(projectID string, opts ApplyReviewBatchOptions) (int, []string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if opts.BaseRevision > 0 && opts.BaseRevision != project.CurrentRevision {
		return 0, nil, ErrRevisionConflict
	}
	if len(opts.Selections) == 0 {
		return 0, nil, errors.New("at least one review selection is required")
	}
	candidates, decisions, err := s.loadProjectReviewArtifacts(project)
	if err != nil {
		return 0, nil, err
	}
	var atoms llmpipeline.RequirementAtomExtractionProposal
	if err := readJSON(s.absoluteWorkspacePath(project.RequirementAtomsProposalPath), &atoms); err != nil {
		return 0, nil, err
	}
	acceptedUnits := mustAcceptedSourceUnits(s, projectID)
	functional, crud, functionalProposalPath, crudProposalPath, err := s.loadReviewAnalysisContext(project)
	if err != nil {
		return 0, nil, err
	}
	selectionByCandidate := map[string]string{}
	for _, selection := range opts.Selections {
		if selection.CandidateID == "" || selection.SelectedOptionID == "" {
			return 0, nil, errors.New("candidate_id and selected_option_id are required")
		}
		if _, duplicate := selectionByCandidate[selection.CandidateID]; duplicate {
			return 0, nil, fmt.Errorf("duplicate selection for %s", selection.CandidateID)
		}
		selectionByCandidate[selection.CandidateID] = selection.SelectedOptionID
	}
	resolved := map[string]bool{}
	for _, decision := range decisions.ReviewDecisions {
		if decision.ApplyStatus == "applied" {
			resolved[decision.CandidateID] = true
		}
	}
	pending := append([]ReviewSelection(nil), opts.Selections...)
	combinedPatch := llmpipeline.ReviewResolutionPatchProposal{
		Operations: []llmpipeline.ReviewPatchOperation{}, AffectedArtifacts: []string{"review_decisions", "requirement_atoms"},
		Explanation: "Applied structured review decisions as one deterministic batch.", NewReviewCandidates: []llmpipeline.ProjectReviewCandidateProposal{},
		ValidationExpectations: []string{"all selected decisions link their affected atoms"}, Warnings: []string{},
	}
	semanticChange := false
	ordered := make([]ReviewSelection, 0, len(pending))
	for len(pending) > 0 {
		progress := false
		next := make([]ReviewSelection, 0, len(pending))
		for _, selection := range pending {
			candidate, found := findProjectReviewCandidate(candidates.ReviewCandidates, selection.CandidateID)
			if !found || !project.OpenReviewIDs[candidate.ID] {
				return 0, nil, fmt.Errorf("review candidate %s is not open", selection.CandidateID)
			}
			ready := true
			for _, dependency := range candidate.DependsOn {
				if !resolved[dependency] {
					ready = false
					break
				}
			}
			if !ready {
				next = append(next, selection)
				continue
			}
			option, found := findProjectReviewOption(candidate.Options, selection.SelectedOptionID)
			if !found {
				return 0, nil, fmt.Errorf("selected option %s does not exist for %s", selection.SelectedOptionID, candidate.ID)
			}
			if !llmpipeline.HasStructuredReviewEffects(option) {
				return 0, nil, fmt.Errorf("%s option %s has no v0.7.2 structured effects", candidate.ID, option.ID)
			}
			decisionID := strings.Replace(candidate.ID, "RC-", "RD-", 1)
			if decisionID == candidate.ID {
				decisionID = "RD-" + candidate.ID
			}
			patch, patchQA := llmpipeline.BuildDeterministicReviewResolutionPatch(candidate, option, decisionID, atoms.RequirementAtoms)
			if !patchQA.OK {
				return 0, nil, fmt.Errorf("review patch for %s failed QA: %s", candidate.ID, strings.Join(patchQA.Errors, "; "))
			}
			patched, changed, patchErr := applyReviewPatch(atoms, patch, decisionID)
			if patchErr != nil {
				return 0, nil, patchErr
			}
			atoms = patched
			semanticChange = semanticChange || changed
			combinedPatch.Operations = append(combinedPatch.Operations, patch.Operations...)
			decisionActiveMS := int64(0)
			if len(ordered) == 0 {
				decisionActiveMS = opts.ActiveReviewMS
			}
			decisions.ReviewDecisions = append(decisions.ReviewDecisions, ReviewDecisionRecord{
				ID: decisionID, CandidateID: candidate.ID, QuestionSnapshot: candidate.Question, SelectedOption: option.ID,
				AffectedAtoms: append([]string(nil), candidate.AffectedAtoms...), RationaleSnapshot: option.Rationale,
				ReviewedBy: nonEmpty(opts.ReviewedBy, "web_user"), ReviewedAt: time.Now(), ProjectRevision: project.CurrentRevision,
				AffectedArtifacts: append([]string(nil), patch.AffectedArtifacts...), AppliedPatchID: "BATCH-" + decisionID,
				ApplyStatus: "applied", ValidationResult: "passed", NewCandidateIDs: []string{},
				DecisionMode: nonEmpty(opts.DecisionMode, "manual_batch"), PolicyVersion: reviewRiskPolicyVersion, ActiveReviewMS: decisionActiveMS,
			})
			resolved[candidate.ID] = true
			ordered = append(ordered, selection)
			progress = true
		}
		if !progress {
			return 0, nil, errors.New("selected review decisions have unresolved dependencies outside the batch")
		}
		pending = next
	}
	atomQA := llmpipeline.ValidateRequirementAtomProposal(atoms, acceptedUnits)
	if !atomQA.OK {
		return 0, nil, fmt.Errorf("batched requirement atoms failed QA: %s", strings.Join(atomQA.Errors, "; "))
	}
	reviewQA := llmpipeline.ValidateProjectReviewProposal(llmpipeline.ProjectReviewProposal{ReviewCandidates: candidates.ReviewCandidates}, acceptedUnits, atoms.RequirementAtoms, functional.FunctionalAreas, crud.Operations)
	if !reviewQA.OK {
		return 0, nil, fmt.Errorf("review DAG after batch is invalid: %s", strings.Join(reviewQA.Errors, "; "))
	}
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"requirement_atoms.proposed.json": {Value: atoms, JSON: true}, "requirement_atoms.yaml": {Value: buildRequirementAtomsArtifact(project, atoms)},
		"requirement_atom_qa.json": {Value: atomQA, JSON: true}, "review_candidates.yaml": {Value: candidates},
		"review_candidate_qa.json": {Value: reviewQA, JSON: true}, "review_decisions.yaml": {Value: decisions},
		"review_resolution_patch.proposed.json": {Value: combinedPatch, JSON: true},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.RequirementAtomsProposalPath = paths["requirement_atoms.proposed.json"]
		current.RequirementAtomsPath = paths["requirement_atoms.yaml"]
		current.RequirementAtomQAPath = paths["requirement_atom_qa.json"]
		current.ReviewCandidatesPath = paths["review_candidates.yaml"]
		current.ReviewCandidateQAPath = paths["review_candidate_qa.json"]
		current.ReviewDecisionsPath = paths["review_decisions.yaml"]
		current.LastAppliedPatchPath = paths["review_resolution_patch.proposed.json"]
		for _, selection := range ordered {
			delete(current.OpenReviewIDs, selection.CandidateID)
			current.AnsweredReviews[selection.CandidateID] = selection.SelectedOptionID
		}
		invalidateModelArtifacts(current)
		restoreReviewAnalysisPaths(current, functionalProposalPath, crudProposalPath)
		if len(current.OpenReviewIDs) == 0 {
			current.AnalysisReady = true
			current.LifecycleStatus = "ready_for_model_generation"
			current.LastActivity = fmt.Sprintf("Applied %d review decisions in one deterministic batch.", len(ordered))
			if semanticChange {
				current.LastActivity += " Semantic requirement changes passed QA."
			}
		} else {
			current.AnalysisReady = false
			current.LifecycleStatus = "analysis_review"
			current.LastActivity = fmt.Sprintf("Applied %d review decisions; %d remain open.", len(ordered), len(current.OpenReviewIDs))
		}
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, []string{"review_decisions", "review_candidates", "requirement_atoms"}, nil
}

func autoReviewSelections(candidates []llmpipeline.ProjectReviewCandidateProposal) []ReviewSelection {
	highImpact := map[string]bool{"identity": true, "key": true, "cardinality": true, "ownership": true, "lifecycle": true, "history": true, "persistence": true, "security": true, "enforceability": true}
	var selections []ReviewSelection
	for _, candidate := range candidates {
		if candidate.Blocking || candidate.Severity != "low" || candidate.RecommendationConfidence != "high" || len(candidate.DependsOn) > 0 {
			continue
		}
		option, found := findProjectReviewOption(candidate.Options, candidate.RecommendedOptionID)
		if !found || option.Effects == nil || option.Effects.RequiresFollowup || len(option.Effects.FollowupCandidateIDs) > 0 {
			continue
		}
		risky := false
		for _, dimension := range option.Effects.ImpactDimensions {
			if highImpact[dimension] {
				risky = true
				break
			}
		}
		if !risky {
			selections = append(selections, ReviewSelection{CandidateID: candidate.ID, SelectedOptionID: option.ID})
		}
	}
	return selections
}
