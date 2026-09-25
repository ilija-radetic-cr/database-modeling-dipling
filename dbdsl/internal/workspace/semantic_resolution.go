package workspace

import (
	"errors"
	"fmt"
	"strings"

	"dbdsl/internal/llmpipeline"
)

// CreateSemanticRepairCandidates converts every unresolved blocking semantic
// finding into an auditable human decision. The current model remains
// inspectable until a decision is applied; applying a decision invalidates the
// downstream model and returns the project to the normal projection pipeline.
func (s *Store) CreateSemanticRepairCandidates(projectID string, baseRevision int) (int, []string, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if baseRevision > 0 && baseRevision != project.CurrentRevision {
		return 0, nil, ErrRevisionConflict
	}
	if !project.ModelGenerated {
		return 0, nil, ErrModelNotGenerated
	}
	report, err := s.SemanticVerification(projectID)
	if err != nil {
		return 0, nil, err
	}
	if report.OK {
		return 0, nil, errors.New("semantic verification has no blocking issues")
	}
	// A report can fail only on unrealized non-blocking obligations; those must
	// still be resolvable, otherwise the project can neither be repaired nor accepted.
	includeNonBlocking := report.BlockingIssues == 0
	obligationArtifacts, err := s.DesignObligations(projectID)
	if err != nil {
		return 0, nil, err
	}
	obligationByID := map[string]llmpipeline.DesignObligation{}
	for _, obligation := range obligationArtifacts.Accepted.DesignObligations {
		obligationByID[obligation.ID] = obligation
	}
	candidates, decisions, err := s.loadProjectReviewArtifacts(project)
	if err != nil {
		return 0, nil, err
	}
	existingByKey := map[string]string{}
	for _, candidate := range candidates.ReviewCandidates {
		existingByKey[candidate.DecisionKey] = candidate.ID
	}
	sequence := len(candidates.ReviewCandidates) + 1
	createdIDs := []string{}
	allIDs := []string{}
	for _, issue := range report.Issues {
		if (!issue.Blocking && !includeNonBlocking) || issue.ObligationID == "" {
			continue
		}
		decisionKey := "semantic_obligation:" + issue.ObligationID
		if existingID := existingByKey[decisionKey]; existingID != "" {
			allIDs = append(allIDs, existingID)
			continue
		}
		obligation, found := obligationByID[issue.ObligationID]
		if !found {
			return 0, nil, fmt.Errorf("semantic issue %s references unknown obligation %s", issue.ID, issue.ObligationID)
		}
		id := fmt.Sprintf("RC-SEM-%03d", sequence)
		for {
			if _, exists := findProjectReviewCandidate(candidates.ReviewCandidates, id); !exists {
				break
			}
			sequence++
			id = fmt.Sprintf("RC-SEM-%03d", sequence)
		}
		candidate := buildSemanticRepairCandidate(id, issue, obligation)
		candidates.ReviewCandidates = append(candidates.ReviewCandidates, candidate)
		existingByKey[decisionKey] = id
		createdIDs = append(createdIDs, id)
		allIDs = append(allIDs, id)
		sequence++
	}
	if len(createdIDs) == 0 {
		return project.CurrentRevision, allIDs, nil
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
	qa := llmpipeline.ValidateProjectReviewProposal(
		llmpipeline.ProjectReviewProposal{ReviewCandidates: candidates.ReviewCandidates},
		mustAcceptedSourceUnits(s, projectID), atoms.RequirementAtoms, functional.FunctionalAreas, crud.Operations,
	)
	if !qa.OK {
		return 0, nil, fmt.Errorf("semantic repair candidates are invalid: %s", strings.Join(qa.Errors, "; "))
	}
	paths, err := s.writeAnalysisRevision(project, map[string]artifactValue{
		"review_candidates.yaml":   {Value: candidates},
		"review_candidate_qa.json": {Value: qa, JSON: true},
		"review_decisions.yaml":    {Value: decisions},
	})
	if err != nil {
		return 0, nil, err
	}
	err = s.withProject(projectID, project.CurrentRevision, func(current *ProjectState) error {
		current.ReviewCandidatesPath = paths["review_candidates.yaml"]
		current.ReviewCandidateQAPath = paths["review_candidate_qa.json"]
		current.ReviewDecisionsPath = paths["review_decisions.yaml"]
		for _, id := range createdIDs {
			current.OpenReviewIDs[id] = true
		}
		current.FinalModelAccepted = false
		current.DBMLReady = false
		current.DBMLPath = ""
		current.TraceReportPath = ""
		current.LifecycleStatus = "analysis_review"
		current.LastActivity = fmt.Sprintf("%d blocking semantic obligations require auditable repair decisions.", len(createdIDs))
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	current, _ := s.Project(projectID)
	return current.CurrentRevision, allIDs, nil
}

func buildSemanticRepairCandidate(id string, issue SemanticIssue, obligation llmpipeline.DesignObligation) llmpipeline.ProjectReviewCandidateProposal {
	repairRationale := fmt.Sprintf(
		"Repair the conceptual/logical model so %s is realized as %s. Verification target: %s",
		obligation.ID, obligation.Kind, obligation.VerificationTarget,
	)
	excludeRationale := fmt.Sprintf(
		"A human reviewer confirms that %s has no persistent database consequence; retain the decision and classify its requirement atoms outside DB scope.",
		obligation.ID,
	)
	return llmpipeline.ProjectReviewCandidateProposal{
		ID: id, DecisionKey: "semantic_obligation:" + obligation.ID,
		Question:    fmt.Sprintf("How should blocking semantic obligation %s be resolved?", obligation.ID),
		Description: fmt.Sprintf("%s reports %s: %s", issue.ID, issue.Code, issue.Message),
		Category:    "semantic_obligation_repair", Phase: "semantic_verification", Severity: issue.Severity, Blocking: true,
		AffectedSourceUnits:     append([]string(nil), obligation.SourceUnits...),
		AffectedAtoms:           append([]string(nil), obligation.RequirementAtoms...),
		AffectedModelCandidates: append([]string(nil), issue.ModelElements...),
		MayAffect:               []string{"design_obligations", "conceptual_model", "logical_model", "semantic_verification", "dbml", "trace"},
		Options: []llmpipeline.ReviewOptionProposal{
			{
				ID: "repair_model", Label: "Repair the model", Rationale: repairRationale,
				EffectSummary:         "Regenerate the conceptual and logical model with an explicit compatible realization.",
				Benefits:              []string{"Preserves the source-backed obligation", "Makes the semantic repair auditable"},
				Risks:                 []string{"May add or change model elements"},
				AffectedArtifactKinds: []string{"conceptual_model", "logical_model", "semantic_verification"},
				Recommended:           true,
				Effects:               noChangeReviewEffects(obligation.RequirementAtoms, []string{"enforceability", "persistence"}),
			},
			{
				ID: "exclude_obligation", Label: "Outside DB scope", Rationale: excludeRationale,
				EffectSummary:         "Classify the linked requirement atoms as intentionally absent from the persistent schema and reclassify their design obligations.",
				Benefits:              []string{"Removes false-positive schema obligations with an explicit human decision"},
				Risks:                 []string{"Incorrect use can omit required persistent data"},
				AffectedArtifactKinds: []string{"requirement_atoms", "design_obligations", "semantic_verification"},
				Effects:               semanticExclusionEffects(obligation.RequirementAtoms, obligation.Kind),
			},
		},
		RecommendedOptionID: "repair_model", RecommendationConfidence: "high", Warnings: []string{},
	}
}

func semanticExclusionEffects(atomIDs []string, kind string) *llmpipeline.ReviewOptionEffects {
	updates := make([]llmpipeline.ReviewAtomUpdate, 0, len(atomIDs))
	for _, atomID := range atomIDs {
		updates = append(updates, llmpipeline.ReviewAtomUpdate{
			AtomID: atomID, ModelingOutcome: "intentionally_not_in_db", PersistenceEffect: "not_required",
			SupportLevel: "no_change", Confidence: "no_change",
		})
	}
	return &llmpipeline.ReviewOptionEffects{
		ModelingOutcome: "intentionally_not_in_db", PersistenceEffect: "not_required", SupportLevel: "no_change",
		AtomUpdates: updates, ImpactDimensions: semanticImpactDimensions(kind), FollowupCandidateIDs: []string{},
	}
}

func semanticImpactDimensions(kind string) []string {
	if kind == "derived_view" {
		return []string{"derived_data", "persistence"}
	}
	return []string{"enforceability", "persistence"}
}
