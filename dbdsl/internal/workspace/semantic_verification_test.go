package workspace

import (
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llmpipeline"
)

func TestSemanticVerifierRejectsEvidenceWithoutCompatibleRealization(t *testing.T) {
	model := dsl.Document{Entities: []dsl.Entity{{ID: "game", Evidence: dsl.Evidence{RequirementAtoms: []string{"RA-1"}}}}}
	obligations := []llmpipeline.DesignObligation{{ID: "DO-1", Kind: "derived_view", Persistence: "derived", Risk: "high", RequirementAtoms: []string{"RA-1"}, Statement: "Prikaži rang listu."}}
	report := verifySemanticObligations(model, obligations, "sha256:test")
	if report.OK || report.BlockingIssues != 1 || report.Issues[0].Code != "evidence_without_semantic_realization" {
		t.Fatalf("semantic citation must not substitute for realization: %+v", report)
	}
}

func TestSemanticVerifierRejectsGenericFactContainer(t *testing.T) {
	model := dsl.Document{Entities: []dsl.Entity{{ID: "game_fact", Attributes: []dsl.Attribute{{ID: "fact_kind"}, {ID: "fact_value"}}}}}
	report := verifySemanticObligations(model, nil, "sha256:test")
	if report.OK || report.BlockingIssues != 1 || report.Issues[0].Code != "weakly_typed_model" {
		t.Fatalf("weakly typed container must block acceptance: %+v", report)
	}
}

func TestSemanticRepairExclusionReclassifiesObligationDeterministically(t *testing.T) {
	obligation := llmpipeline.DesignObligation{
		ID: "DO-1", Statement: "A report is displayed.", Kind: "derived_view", Persistence: "derived", Risk: "high",
		RequirementAtoms: []string{"RA-1"}, SourceUnits: []string{"SU-1"}, VerificationTarget: "Identify the persistent facts.",
	}
	candidate := buildSemanticRepairCandidate("RC-SEM-001", SemanticIssue{
		ID: "SEM-001", Severity: "high", Code: "unrealized_design_obligation", ObligationID: "DO-1", Message: obligation.Statement, Blocking: true,
	}, obligation)
	if len(candidate.Options) != 2 || candidate.RecommendedOptionID != "repair_model" {
		t.Fatalf("unexpected semantic repair candidate: %+v", candidate)
	}
	exclusion := candidate.Options[1]
	patch, qa := llmpipeline.BuildDeterministicReviewResolutionPatch(candidate, exclusion, "RD-SEM-001", []llmpipeline.RequirementAtomProposal{{
		ID: "RA-1", Statement: obligation.Statement, SourceUnits: []string{"SU-1"}, ModelingOutcome: "represented", PersistenceEffect: "derived_basis",
	}})
	if !qa.OK {
		t.Fatalf("semantic exclusion patch failed QA: %+v", qa)
	}
	patched, semanticChange, err := applyReviewPatch(llmpipeline.RequirementAtomExtractionProposal{RequirementAtoms: []llmpipeline.RequirementAtomProposal{{
		ID: "RA-1", Statement: obligation.Statement, SourceUnits: []string{"SU-1"}, ModelingOutcome: "represented", PersistenceEffect: "derived_basis",
	}}}, patch, "RD-SEM-001")
	if err != nil || !semanticChange {
		t.Fatalf("apply semantic exclusion: semantic=%v err=%v", semanticChange, err)
	}
	updated, updatedQA := llmpipeline.ReclassifyDesignObligations(llmpipeline.DesignObligationsFile{DesignObligations: []llmpipeline.DesignObligation{obligation}}, patched.RequirementAtoms)
	if !updatedQA.OK || updated.DesignObligations[0].Status != "not_required" || updated.DesignObligations[0].Persistence != "not_required" {
		t.Fatalf("semantic exclusion was not reflected in design obligations: obligations=%+v qa=%+v", updated, updatedQA)
	}
}
