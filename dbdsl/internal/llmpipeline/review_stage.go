package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

type ProjectReviewOptions struct {
	OutDir           string
	SourceUnits      []dsl.SourceUnit
	RequirementAtoms []RequirementAtomProposal
	FunctionalAreas  []FunctionalAreaProposal
	Actors           []ActorProposal
	Operations       []CRUDOperationProposal
	Model            string
	ReasoningEffort  string
	MaxOutputTokens  int
}

type ReviewResolutionOptions struct {
	OutDir                 string
	Candidate              ProjectReviewCandidateProposal
	SelectedOption         ReviewOptionProposal
	DecisionID             string
	ReservedCandidateIDs   []string
	ValidSourceUnitIDs     []string
	ValidFunctionalAreaIDs []string
	ValidOperationIDs      []string
	RequirementAtoms       []RequirementAtomProposal
	Model                  string
	ReasoningEffort        string
	MaxOutputTokens        int
}

func RunProjectReview(ctx context.Context, client llm.Client, opts ProjectReviewOptions) (ProjectReviewProposal, StageQA, error) {
	if opts.OutDir == "" || len(opts.RequirementAtoms) == 0 || len(opts.FunctionalAreas) == 0 {
		return ProjectReviewProposal{}, StageQA{}, errors.New("output directory and validated analysis are required")
	}
	atoms := reviewRelevantAtoms(opts.RequirementAtoms)
	if len(atoms) == 0 {
		proposal := ProjectReviewProposal{ReviewCandidates: []ProjectReviewCandidateProposal{}, Warnings: []string{}, ConfidenceSummary: map[string]string{"strategy": "deterministic_no_semantic_need"}}
		qa := ValidateProjectReviewProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Operations)
		_ = writeJSONFile(filepath.Join(opts.OutDir, "llm_runs", "review_candidate_call_gate.json"), map[string]any{
			"policy": "semantic_need_v1", "provider_call": false, "reason": "no_review_relevant_atoms", "validation": qa,
		})
		return proposal, qa, nil
	}
	if client == nil {
		return ProjectReviewProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	atomIDs := map[string]bool{}
	sourceIDs := map[string]bool{}
	for _, atom := range atoms {
		atomIDs[atom.ID] = true
		for _, id := range atom.SourceUnits {
			sourceIDs[id] = true
		}
	}
	units := make([]dsl.SourceUnit, 0, len(sourceIDs))
	for _, unit := range opts.SourceUnits {
		if sourceIDs[unit.ID] {
			units = append(units, unit)
		}
	}
	areas := filterReviewAreas(opts.FunctionalAreas, atomIDs)
	operations := filterReviewOperations(opts.Operations, atomIDs)
	input := mustJSON(map[string]any{
		"source_units": sourceUnitInputs(units), "requirement_atoms": atoms,
		"functional_areas": areas, "actors": opts.Actors, "crud_operations": operations,
		"output_contract": "review_candidate_proposal", "pipeline_version": "0.7", "template_version": promptTemplateVersion,
	})
	fullInput := mustJSON(map[string]any{
		"source_units": sourceUnitInputs(opts.SourceUnits), "requirement_atoms": opts.RequirementAtoms,
		"functional_areas": opts.FunctionalAreas, "actors": opts.Actors, "crud_operations": opts.Operations,
	})
	var proposal ProjectReviewProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 6, llm.Request{
		Stage: "review_candidate_proposal", Model: opts.Model, Instructions: projectReviewInstructions,
		Input: input, SchemaName: "DBDSLProjectReview", Schema: projectReviewSchema(), ReasoningEffort: opts.ReasoningEffort,
		MaxOutputTokens: opts.MaxOutputTokens, Metadata: map[string]string{"template_version": promptTemplateVersion, "full_context_bytes": fmt.Sprint(len(fullInput)), "context_policy": "minimal_context_v1", "canonicalizer_version": "pipeline_ids_v2", "risk_policy": "review_risk_value_v1"},
	}, &proposal, func() []string {
		canonicalizeProjectReviewIDs(&proposal)
		qa = ValidateProjectReviewProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Operations)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	canonicalizeProjectReviewIDs(&proposal)
	qa = ValidateProjectReviewProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Operations)
	return proposal, qa, nil
}

func reviewRelevantAtoms(atoms []RequirementAtomProposal) []RequirementAtomProposal {
	out := make([]RequirementAtomProposal, 0, len(atoms))
	for _, atom := range atoms {
		if atom.RequiresReview || atom.ModelingOutcome == "deferred" || atom.ModelingOutcome == "unsupported" || atom.PersistenceEffect == "unclear" || atom.SupportLevel == "assumption" || atom.Confidence == "low" {
			out = append(out, atom)
		}
	}
	return out
}

func HasReviewSemanticNeed(atoms []RequirementAtomProposal) bool {
	return len(reviewRelevantAtoms(atoms)) > 0
}

func filterReviewAreas(areas []FunctionalAreaProposal, atomIDs map[string]bool) []FunctionalAreaProposal {
	var out []FunctionalAreaProposal
	for _, area := range areas {
		for _, id := range area.Atoms {
			if atomIDs[id] {
				out = append(out, area)
				break
			}
		}
	}
	return out
}

func filterReviewOperations(operations []CRUDOperationProposal, atomIDs map[string]bool) []CRUDOperationProposal {
	var out []CRUDOperationProposal
	for _, operation := range operations {
		for _, id := range operation.RequirementAtoms {
			if atomIDs[id] {
				out = append(out, operation)
				break
			}
		}
	}
	return out
}

func HasStructuredReviewEffects(option ReviewOptionProposal) bool {
	return option.Effects != nil
}

func BuildDeterministicReviewResolutionPatch(candidate ProjectReviewCandidateProposal, option ReviewOptionProposal, decisionID string, atoms []RequirementAtomProposal) (ReviewResolutionPatchProposal, StageQA) {
	patch := ReviewResolutionPatchProposal{
		Operations: []ReviewPatchOperation{}, AffectedArtifacts: []string{"review_decisions", "requirement_atoms"},
		Explanation: "Applied declared review-option effects deterministically.", NewReviewCandidates: []ProjectReviewCandidateProposal{},
		ValidationExpectations: []string{"all affected atoms link the selected decision"}, Warnings: []string{},
	}
	updates := map[string]ReviewAtomUpdate{}
	if option.Effects != nil {
		for _, update := range option.Effects.AtomUpdates {
			updates[update.AtomID] = update
		}
	}
	for _, atomID := range candidate.AffectedAtoms {
		patch.Operations = append(patch.Operations, ReviewPatchOperation{Operation: "link_review_decision", TargetID: atomID, Field: "review_decisions", Value: decisionID})
		if option.Effects == nil {
			continue
		}
		modelingOutcome, persistenceEffect, supportLevel, confidence := option.Effects.ModelingOutcome, option.Effects.PersistenceEffect, option.Effects.SupportLevel, ""
		if update, ok := updates[atomID]; ok {
			modelingOutcome, persistenceEffect, supportLevel, confidence = update.ModelingOutcome, update.PersistenceEffect, update.SupportLevel, update.Confidence
		}
		if value := modelingOutcome; value != "" && value != "no_change" {
			patch.Operations = append(patch.Operations, ReviewPatchOperation{Operation: "update_modeling_outcome", TargetID: atomID, Field: "modeling_outcome", Value: value})
		}
		if value := persistenceEffect; value != "" && value != "no_change" {
			patch.Operations = append(patch.Operations, ReviewPatchOperation{Operation: "update_persistence_effect", TargetID: atomID, Field: "persistence_effect", Value: value})
		}
		if value := supportLevel; value != "" && value != "no_change" {
			patch.Operations = append(patch.Operations, ReviewPatchOperation{Operation: "update_support_level", TargetID: atomID, Field: "support_level", Value: value})
		}
		if value := confidence; value != "" && value != "no_change" {
			patch.Operations = append(patch.Operations, ReviewPatchOperation{Operation: "update_confidence", TargetID: atomID, Field: "confidence", Value: value})
		}
	}
	qa := ValidateReviewResolutionPatch(patch, candidate, decisionID, atoms)
	return patch, qa
}

func canonicalizeProjectReviewIDs(proposal *ProjectReviewProposal) {
	sort.SliceStable(proposal.ReviewCandidates, func(i, j int) bool {
		left := nonEmpty(proposal.ReviewCandidates[i].DecisionKey, proposal.ReviewCandidates[i].Question)
		right := nonEmpty(proposal.ReviewCandidates[j].DecisionKey, proposal.ReviewCandidates[j].Question)
		return strings.ToLower(left) < strings.ToLower(right)
	})
	candidateMap := map[string]string{}
	for i := range proposal.ReviewCandidates {
		old := proposal.ReviewCandidates[i].ID
		canonical := fmt.Sprintf("RC-%03d", i+1)
		candidateMap[old] = canonical
		proposal.ReviewCandidates[i].ID = canonical
		if strings.TrimSpace(proposal.ReviewCandidates[i].DecisionKey) == "" {
			proposal.ReviewCandidates[i].DecisionKey = canonical
		}
	}
	for i := range proposal.ReviewCandidates {
		candidate := &proposal.ReviewCandidates[i]
		sort.SliceStable(candidate.Options, func(i, j int) bool {
			return strings.ToLower(candidate.Options[i].Label) < strings.ToLower(candidate.Options[j].Label)
		})
		optionMap := map[string]string{}
		for j := range candidate.Options {
			old := candidate.Options[j].ID
			canonical := fmt.Sprintf("%s-O%d", candidate.ID, j+1)
			optionMap[old] = canonical
			candidate.Options[j].ID = canonical
		}
		if canonical, ok := optionMap[candidate.RecommendedOptionID]; ok {
			candidate.RecommendedOptionID = canonical
		}
		for j, dependency := range candidate.DependsOn {
			if canonical, ok := candidateMap[dependency]; ok {
				candidate.DependsOn[j] = canonical
			}
		}
		for j := range candidate.Options {
			if candidate.Options[j].Effects == nil {
				continue
			}
			for k, followup := range candidate.Options[j].Effects.FollowupCandidateIDs {
				if canonical, ok := candidateMap[followup]; ok {
					candidate.Options[j].Effects.FollowupCandidateIDs[k] = canonical
				}
			}
		}
	}
}

func RunReviewResolutionPatch(ctx context.Context, client llm.Client, opts ReviewResolutionOptions) (ReviewResolutionPatchProposal, StageQA, error) {
	if client == nil {
		return ReviewResolutionPatchProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || opts.Candidate.ID == "" || opts.SelectedOption.ID == "" || opts.DecisionID == "" {
		return ReviewResolutionPatchProposal{}, StageQA{}, errors.New("review candidate, option, decision ID and output directory are required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	input := mustJSON(map[string]any{
		"candidate": opts.Candidate, "selected_option": opts.SelectedOption, "decision_id": opts.DecisionID,
		"reserved_review_candidate_ids": opts.ReservedCandidateIDs,
		"valid_reference_ids": map[string]any{
			"source_units":      opts.ValidSourceUnitIDs,
			"requirement_atoms": requirementAtomIDs(opts.RequirementAtoms),
			"functional_areas":  opts.ValidFunctionalAreaIDs,
			"operations":        opts.ValidOperationIDs,
			"review_candidates": opts.ReservedCandidateIDs,
		},
		"requirement_atoms": opts.RequirementAtoms, "output_contract": "review_resolution_patch",
		"pipeline_version": "0.7", "template_version": promptTemplateVersion,
	})
	var proposal ReviewResolutionPatchProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 7, llm.Request{
		Stage: "review_resolution_patch", Model: opts.Model, Instructions: reviewResolutionPatchInstructions,
		Input: input, SchemaName: "DBDSLReviewResolutionPatch", Schema: reviewResolutionPatchSchema(),
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{"template_version": promptTemplateVersion, "candidate_id": opts.Candidate.ID, "decision_id": opts.DecisionID},
	}, &proposal, func() []string {
		normalizeReviewDecisionLinks(&proposal, opts.DecisionID, opts.SelectedOption.ID)
		qa = ValidateReviewResolutionPatch(proposal, opts.Candidate, opts.DecisionID, opts.RequirementAtoms)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa = ValidateReviewResolutionPatch(proposal, opts.Candidate, opts.DecisionID, opts.RequirementAtoms)
	return proposal, qa, nil
}

func requirementAtomIDs(items []RequirementAtomProposal) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

// normalizeReviewDecisionLinks repairs a narrow, meaning-preserving formatting
// variation observed in real structured output. Some models copy both the
// decision and selected-option IDs into the value (for example
// "RD-001:RC-001-O2"). The selected option is already persisted separately, so
// the canonical requirement-atom reference must remain the exact decision ID.
// No arbitrary or semantically different reference is accepted here.
func normalizeReviewDecisionLinks(proposal *ReviewResolutionPatchProposal, decisionID, selectedOptionID string) {
	composite := decisionID + ":" + selectedOptionID
	for i := range proposal.Operations {
		operation := &proposal.Operations[i]
		if operation.Operation != "link_review_decision" || operation.Value != composite {
			continue
		}
		operation.Value = decisionID
		operation.Field = "review_decisions"
		proposal.Warnings = append(proposal.Warnings, "normalized composite review-decision reference to canonical decision_id")
	}
}

func ValidateProjectReviewProposal(proposal ProjectReviewProposal, units []dsl.SourceUnit, atoms []RequirementAtomProposal, areas []FunctionalAreaProposal, operations []CRUDOperationProposal) StageQA {
	qa := newStageQA(proposal.Warnings)
	sourceIDs, atomIDs, areaIDs, operationIDs := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, item := range units {
		sourceIDs[item.ID] = true
	}
	for _, item := range atoms {
		atomIDs[item.ID] = true
	}
	for _, item := range areas {
		areaIDs[item.ID] = true
	}
	for _, item := range operations {
		operationIDs[item.ID] = true
	}
	candidates := map[string]ProjectReviewCandidateProposal{}
	decisionKeys := map[string]string{}
	for _, candidate := range proposal.ReviewCandidates {
		if candidate.ID == "" || candidates[candidate.ID].ID != "" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate review candidate id %q", candidate.ID))
		}
		candidates[candidate.ID] = candidate
		decisionKey := candidate.DecisionKey
		if decisionKey == "" {
			// Artifacts created before v0.7.2 did not persist decision_key. Treat the
			// candidate ID as its stable legacy key while keeping the v0.7.2 output
			// schema strict for all newly generated proposals.
			decisionKey = candidate.ID
		}
		if previous := decisionKeys[decisionKey]; previous != "" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s duplicates decision_key from %s", candidate.ID, previous))
		} else {
			decisionKeys[decisionKey] = candidate.ID
		}
		if strings.TrimSpace(candidate.Question) == "" || len(candidate.Options) < 2 || len(candidate.Options) > 3 {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s must have a question and two or three options", candidate.ID))
		}
		optionIDs := map[string]bool{}
		recommendedFound := false
		for _, option := range candidate.Options {
			if option.ID == "" || optionIDs[option.ID] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s has empty or duplicate option id %q", candidate.ID, option.ID))
			}
			optionIDs[option.ID] = true
			if option.ID == candidate.RecommendedOptionID {
				recommendedFound = true
			}
			if option.Effects == nil {
				// Legacy candidates remain readable. The deterministic batch endpoint
				// separately rejects options without v0.7.2 structured effects.
				continue
			}
			for _, update := range option.Effects.AtomUpdates {
				if !atomIDs[update.AtomID] {
					qa.Errors = append(qa.Errors, fmt.Sprintf("%s option %s updates unknown atom %s", candidate.ID, option.ID, update.AtomID))
				}
				if !containsString(candidate.AffectedAtoms, update.AtomID) {
					qa.Errors = append(qa.Errors, fmt.Sprintf("%s option %s updates atom %s outside candidate scope", candidate.ID, option.ID, update.AtomID))
				}
			}
		}
		if !recommendedFound {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s recommended option does not exist", candidate.ID))
		}
		for _, id := range candidate.AffectedSourceUnits {
			if !sourceIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown source unit %s", candidate.ID, id))
			}
		}
		for _, id := range candidate.AffectedAtoms {
			if !atomIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown atom %s", candidate.ID, id))
			}
		}
		for _, id := range candidate.AffectedFunctionalAreas {
			if !areaIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown area %s", candidate.ID, id))
			}
		}
		for _, id := range candidate.AffectedOperations {
			if !operationIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown operation %s", candidate.ID, id))
			}
		}
	}
	for _, candidate := range proposal.ReviewCandidates {
		for _, dependency := range candidate.DependsOn {
			if dependency == candidate.ID {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s depends on itself", candidate.ID))
			} else if candidates[dependency].ID == "" {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s depends on unknown candidate %s", candidate.ID, dependency))
			}
		}
		for _, option := range candidate.Options {
			if option.Effects == nil {
				continue
			}
			if option.Effects.RequiresFollowup && len(option.Effects.FollowupCandidateIDs) == 0 {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s option %s requires follow-up but declares no followup_candidate_ids", candidate.ID, option.ID))
			}
			for _, followup := range option.Effects.FollowupCandidateIDs {
				if followup == candidate.ID || candidates[followup].ID == "" {
					qa.Errors = append(qa.Errors, fmt.Sprintf("%s option %s references invalid follow-up candidate %s", candidate.ID, option.ID, followup))
				}
			}
		}
	}
	if cycle := reviewDependencyCycle(candidates); len(cycle) > 0 {
		qa.Errors = append(qa.Errors, "review dependency cycle: "+strings.Join(cycle, " -> "))
	}
	qa.Coverage["review_candidates"] = len(proposal.ReviewCandidates)
	for _, candidate := range proposal.ReviewCandidates {
		if candidate.Blocking {
			qa.Coverage["blocking_candidates"]++
		}
	}
	qa.OK = len(qa.Errors) == 0
	return qa
}

func containsString(items []string, wanted string) bool {
	for _, item := range items {
		if item == wanted {
			return true
		}
	}
	return false
}

func ValidateReviewResolutionPatch(proposal ReviewResolutionPatchProposal, candidate ProjectReviewCandidateProposal, decisionID string, atoms []RequirementAtomProposal) StageQA {
	qa := newStageQA(proposal.Warnings)
	atomIDs := map[string]bool{}
	affected := map[string]bool{}
	for _, atom := range atoms {
		atomIDs[atom.ID] = true
	}
	for _, id := range candidate.AffectedAtoms {
		affected[id] = true
	}
	linked := map[string]bool{}
	for _, operation := range proposal.Operations {
		if !atomIDs[operation.TargetID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("patch references unknown atom %s", operation.TargetID))
		}
		if len(affected) > 0 && !affected[operation.TargetID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("patch targets atom %s outside candidate scope", operation.TargetID))
		}
		if operation.Operation == "link_review_decision" {
			if operation.Value != decisionID {
				qa.Errors = append(qa.Errors, fmt.Sprintf("patch links unexpected decision %s", operation.Value))
			}
			linked[operation.TargetID] = true
		}
	}
	for id := range affected {
		if !linked[id] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("patch does not link decision to affected atom %s", id))
		}
	}
	if len(proposal.Operations) == 0 && len(affected) > 0 {
		qa.Errors = append(qa.Errors, "review patch contains no operations")
	}
	qa.Coverage["patch_operations"] = len(proposal.Operations)
	qa.Coverage["affected_atoms"] = len(affected)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func reviewDependencyCycle(candidates map[string]ProjectReviewCandidateProposal) []string {
	state := map[string]int{}
	stack := []string{}
	var visit func(string) []string
	visit = func(id string) []string {
		if state[id] == 1 {
			for i, item := range stack {
				if item == id {
					return append(append([]string{}, stack[i:]...), id)
				}
			}
			return []string{id, id}
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		stack = append(stack, id)
		for _, dependency := range candidates[id].DependsOn {
			if cycle := visit(dependency); len(cycle) > 0 {
				return cycle
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		return nil
	}
	for id := range candidates {
		if cycle := visit(id); len(cycle) > 0 {
			return cycle
		}
	}
	return nil
}
