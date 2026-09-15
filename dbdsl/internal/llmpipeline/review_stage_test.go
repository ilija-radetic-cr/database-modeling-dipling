package llmpipeline

import (
	"context"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

func TestReviewStageAndResolutionPatchWithMock(t *testing.T) {
	units := []dsl.SourceUnit{{ID: "SU-001", Text: dsl.SourceUnitText{Exact: "Products exist.", Normalized: "Products exist."}}}
	atoms := []RequirementAtomProposal{{ID: "RA-001", Statement: "Products exist.", SourceUnits: []string{"SU-001"}, SupportLevel: "explicit", Confidence: "high", ModelingOutcome: "represented"}}
	areas := []FunctionalAreaProposal{{ID: "core", MainActors: []string{"system"}, Atoms: []string{"RA-001"}}}
	actors := []ActorProposal{{ID: "system"}}
	operations := []CRUDOperationProposal{{ID: "OP-001", ActorID: "system", FunctionalAreaID: "core", RequirementAtoms: []string{"RA-001"}, SourceUnits: []string{"SU-001"}}}
	mock := llm.NewDefaultMockClient()
	review, qa, err := RunProjectReview(context.Background(), mock, ProjectReviewOptions{OutDir: t.TempDir(), SourceUnits: units, RequirementAtoms: atoms, FunctionalAreas: areas, Actors: actors, Operations: operations, Model: "mock-model"})
	if err != nil || !qa.OK || len(review.ReviewCandidates) != 1 {
		t.Fatalf("review stage: proposal=%+v qa=%+v err=%v", review, qa, err)
	}
	candidate := review.ReviewCandidates[0]
	patch, patchQA, err := RunReviewResolutionPatch(context.Background(), mock, ReviewResolutionOptions{OutDir: t.TempDir(), Candidate: candidate, SelectedOption: candidate.Options[0], DecisionID: "RD-001", RequirementAtoms: atoms, Model: "mock-model"})
	if err != nil || !patchQA.OK || len(patch.Operations) != 1 {
		t.Fatalf("patch stage: patch=%+v qa=%+v err=%v", patch, patchQA, err)
	}
}

func TestReviewDependencyCycleIsRejected(t *testing.T) {
	proposal := ProjectReviewProposal{ReviewCandidates: []ProjectReviewCandidateProposal{
		{ID: "RC-001", Question: "One?", AffectedAtoms: []string{"RA-001"}, DependsOn: []string{"RC-002"}, Options: testReviewOptions(), RecommendedOptionID: "a"},
		{ID: "RC-002", Question: "Two?", AffectedAtoms: []string{"RA-001"}, DependsOn: []string{"RC-001"}, Options: testReviewOptions(), RecommendedOptionID: "a"},
	}}
	qa := ValidateProjectReviewProposal(proposal, nil, []RequirementAtomProposal{{ID: "RA-001"}}, nil, nil)
	if qa.OK {
		t.Fatalf("expected dependency cycle failure: %+v", qa)
	}
}

func TestReviewResolutionNormalizesExactDecisionOptionComposite(t *testing.T) {
	proposal := ReviewResolutionPatchProposal{Operations: []ReviewPatchOperation{
		{Operation: "link_review_decision", TargetID: "RA-001", Field: "review_decision", Value: "RD-001:RC-001-O2"},
	}}
	normalizeReviewDecisionLinks(&proposal, "RD-001", "RC-001-O2")
	if proposal.Operations[0].Value != "RD-001" || proposal.Operations[0].Field != "review_decisions" {
		t.Fatalf("composite reference was not normalized: %+v", proposal.Operations[0])
	}
	qa := ValidateReviewResolutionPatch(proposal, ProjectReviewCandidateProposal{AffectedAtoms: []string{"RA-001"}}, "RD-001", []RequirementAtomProposal{{ID: "RA-001"}})
	if !qa.OK {
		t.Fatalf("normalized proposal should pass validation: %+v", qa)
	}

	unexpected := ReviewResolutionPatchProposal{Operations: []ReviewPatchOperation{
		{Operation: "link_review_decision", TargetID: "RA-001", Field: "review_decisions", Value: "RD-999:RC-001-O2"},
	}}
	normalizeReviewDecisionLinks(&unexpected, "RD-001", "RC-001-O2")
	if ValidateReviewResolutionPatch(unexpected, ProjectReviewCandidateProposal{AffectedAtoms: []string{"RA-001"}}, "RD-001", []RequirementAtomProposal{{ID: "RA-001"}}).OK {
		t.Fatal("unexpected composite reference must remain rejected")
	}
}

func testReviewOptions() []ReviewOptionProposal {
	return []ReviewOptionProposal{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}}
}
