package llmpipeline

import (
	"context"
	"strings"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

func TestNormalizeRequirementReviewSemanticsSeparatesWarningsFromBlockers(t *testing.T) {
	atoms := NormalizeRequirementReviewSemantics([]RequirementAtomProposal{
		{ID: "RA-001", AtomType: "data_attribute", ModelingOutcome: "represented", PersistenceEffect: "required", Confidence: "high", SupportLevel: "explicit", ReviewClass: ReviewClassNonBlockingGap, ReviewTopic: "enforcement", Warnings: []string{"Format is not specified."}},
		{ID: "RA-002", AtomType: "cardinality_constraint", ModelingOutcome: "represented", PersistenceEffect: "required", Confidence: "low", SupportLevel: "explicit", ReviewClass: ReviewClassNonBlockingGap, SourceUnits: []string{"SU-002"}},
	})
	if atoms[0].RequiresReview {
		t.Fatalf("a high-confidence missing detail must remain a warning: %+v", atoms[0])
	}
	if !atoms[1].RequiresReview || atoms[1].ReviewClass != ReviewClassBlockingModelChoice || atoms[1].ReviewGroup == "" {
		t.Fatalf("a low-confidence persistent cardinality must block with a stable group: %+v", atoms[1])
	}
	if got := reviewRelevantAtoms(atoms); len(got) != 1 || got[0].ID != "RA-002" {
		t.Fatalf("review input must contain only the blocker, got %+v", got)
	}
}

func TestProjectReviewProducesOneCandidatePerBlockingGroup(t *testing.T) {
	atoms := NormalizeRequirementReviewSemantics([]RequirementAtomProposal{
		{ID: "RA-001", Statement: "Choose order cardinality.", SourceUnits: []string{"SU-001"}, ModelingOutcome: "represented", PersistenceEffect: "required", ReviewClass: ReviewClassBlockingModelChoice, ReviewTopic: "cardinality", ReviewGroup: "orders"},
		{ID: "RA-002", Statement: "Choose customer identity.", SourceUnits: []string{"SU-002"}, ModelingOutcome: "represented", PersistenceEffect: "required", ReviewClass: ReviewClassBlockingModelChoice, ReviewTopic: "identity", ReviewGroup: "customers"},
	})
	proposal, qa, err := RunProjectReview(context.Background(), llm.NewDefaultMockClient(), ProjectReviewOptions{
		OutDir: t.TempDir(), SourceUnits: []dsl.SourceUnit{{ID: "SU-001"}, {ID: "SU-002"}}, RequirementAtoms: atoms,
		FunctionalAreas: []FunctionalAreaProposal{{ID: "FA-001", Atoms: []string{"RA-001", "RA-002"}}},
		Operations:      []CRUDOperationProposal{{ID: "OP-001", FunctionalAreaID: "FA-001", RequirementAtoms: []string{"RA-001", "RA-002"}}},
		Model:           "mock-model", MaxOutputTokens: 4000,
	})
	if err != nil || !qa.OK || len(proposal.ReviewCandidates) != 2 {
		t.Fatalf("expected one valid candidate per group: candidates=%+v qa=%+v err=%v", proposal.ReviewCandidates, qa, err)
	}
}

func TestExampleRolesProduceConsistentOutcomesAndObligations(t *testing.T) {
	atoms := NormalizeRequirementReviewSemantics([]RequirementAtomProposal{
		{ID: "RA-I", AtomType: "example", ExampleRole: "illustrative_instance", ModelingOutcome: "represented", PersistenceEffect: "required"},
		{ID: "RA-S", AtomType: "example", ExampleRole: "schema_shape", ModelingOutcome: "represented", PersistenceEffect: "required"},
		{ID: "RA-D", AtomType: "example", ExampleRole: "seed_data", ModelingOutcome: "represented", PersistenceEffect: "required"},
		{ID: "RA-C", AtomType: "example", ExampleRole: "constraint_boundary", ModelingOutcome: "represented", PersistenceEffect: "required"},
	})
	if atoms[0].ModelingOutcome != "intentionally_not_in_db" || atoms[0].PersistenceEffect != "not_required" {
		t.Fatalf("illustrative example was not excluded consistently: %+v", atoms[0])
	}
	file, qa := DeriveDesignObligations(atoms)
	if !qa.OK {
		t.Fatalf("derive obligations: %+v", qa)
	}
	want := map[string]string{"RA-I": "transient", "RA-S": "attribute", "RA-D": "completeness", "RA-C": "invariant"}
	for _, obligation := range file.DesignObligations {
		if got := obligation.Kind; got != want[obligation.RequirementAtoms[0]] {
			t.Errorf("unexpected obligation for %s: %s", obligation.RequirementAtoms[0], got)
		}
	}
}

func TestRequirementReadinessFailsBeforeModelGeneration(t *testing.T) {
	atoms := []RequirementAtomProposal{{
		ID: "RA-001", AtomType: "relationship", ModelingOutcome: "represented", PersistenceEffect: "required",
		ReviewClass: ReviewClassBlockingModelChoice, ReviewTopic: "cardinality", ReviewGroup: "order-lines", SourceUnits: []string{"SU-001"},
	}}
	report := EvaluateRequirementReadiness(atoms, []DesignObligation{{ID: "DO-001", Kind: "relationship", Persistence: "required", Status: "accepted", RequirementAtoms: []string{"RA-001"}}})
	if report.OK || len(report.BlockingAtomIDs) != 1 || !strings.Contains(strings.Join(report.Errors(), " "), "RA-001") {
		t.Fatalf("blocking requirement passed readiness: %+v", report)
	}
}

func TestReviewProposalMustCoverEveryBlockingGroupExactlyOnce(t *testing.T) {
	atoms := NormalizeRequirementReviewSemantics([]RequirementAtomProposal{
		{ID: "RA-001", SourceUnits: []string{"SU-001"}, ReviewClass: ReviewClassBlockingModelChoice, ReviewTopic: "cardinality", ReviewGroup: "g1"},
		{ID: "RA-002", SourceUnits: []string{"SU-002"}, ReviewClass: ReviewClassBlockingModelChoice, ReviewTopic: "identity", ReviewGroup: "g2"},
	})
	option := ReviewOptionProposal{ID: "yes", Label: "Yes", Effects: &ReviewOptionEffects{AtomUpdates: []ReviewAtomUpdate{}}}
	proposal := ProjectReviewProposal{ReviewCandidates: []ProjectReviewCandidateProposal{{
		ID: "RC-001", DecisionKey: "g1", Question: "Choose", Blocking: true,
		AffectedAtoms: []string{"RA-001"}, AffectedSourceUnits: []string{"SU-001"},
		Options: []ReviewOptionProposal{option, {ID: "no", Label: "No", Effects: &ReviewOptionEffects{AtomUpdates: []ReviewAtomUpdate{}}}}, RecommendedOptionID: "yes",
	}}}
	qa := ValidateProjectReviewProposal(proposal, []dsl.SourceUnit{{ID: "SU-001"}, {ID: "SU-002"}}, atoms, nil, nil)
	if qa.OK || !strings.Contains(strings.Join(qa.Errors, " "), "RA-002") {
		t.Fatalf("uncovered blocking group passed review validation: %+v", qa)
	}
}
