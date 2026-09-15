package llmpipeline

import (
	"context"
	"errors"
	"fmt"
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
	if client == nil {
		return ProjectReviewProposal{}, StageQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" || len(opts.RequirementAtoms) == 0 || len(opts.FunctionalAreas) == 0 {
		return ProjectReviewProposal{}, StageQA{}, errors.New("output directory and validated analysis are required")
	}
	normalizeAnalysisOptions(&opts.Model, &opts.ReasoningEffort, &opts.MaxOutputTokens)
	input := mustJSON(map[string]any{
		"source_units": sourceUnitInputs(opts.SourceUnits), "requirement_atoms": opts.RequirementAtoms,
		"functional_areas": opts.FunctionalAreas, "actors": opts.Actors, "crud_operations": opts.Operations,
		"output_contract": "review_candidate_proposal", "pipeline_version": "0.7", "template_version": promptTemplateVersion,
	})
	var proposal ProjectReviewProposal
	var qa StageQA
	err := runStructuredStage(ctx, client, opts.OutDir, 6, llm.Request{
		Stage: "review_candidate_proposal", Model: opts.Model, Instructions: projectReviewInstructions,
		Input: input, SchemaName: "DBDSLProjectReview", Schema: projectReviewSchema(), ReasoningEffort: opts.ReasoningEffort,
		MaxOutputTokens: opts.MaxOutputTokens, Metadata: map[string]string{"template_version": promptTemplateVersion},
	}, &proposal, func() []string {
		qa = ValidateProjectReviewProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Operations)
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa = ValidateProjectReviewProposal(proposal, opts.SourceUnits, opts.RequirementAtoms, opts.FunctionalAreas, opts.Operations)
	return proposal, qa, nil
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
	for _, candidate := range proposal.ReviewCandidates {
		if candidate.ID == "" || candidates[candidate.ID].ID != "" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate review candidate id %q", candidate.ID))
		}
		candidates[candidate.ID] = candidate
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
