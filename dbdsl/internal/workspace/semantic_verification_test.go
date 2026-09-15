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
