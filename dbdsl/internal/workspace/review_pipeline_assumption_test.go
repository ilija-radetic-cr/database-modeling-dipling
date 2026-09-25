package workspace

import (
	"testing"

	"dbdsl/internal/llmpipeline"
)

func TestApplyReviewPatchDocumentsDecisionAssumption(t *testing.T) {
	input := llmpipeline.RequirementAtomExtractionProposal{RequirementAtoms: []llmpipeline.RequirementAtomProposal{{
		ID: "RA-0076", Statement: "Admins import urban lines.", SupportLevel: "explicit", Confidence: "high",
	}}}
	patch := llmpipeline.ReviewResolutionPatchProposal{Operations: []llmpipeline.ReviewPatchOperation{
		{Operation: "link_review_decision", TargetID: "RA-0076", Field: "review_decisions", Value: "RD-007"},
		{Operation: "update_support_level", TargetID: "RA-0076", Field: "support_level", Value: "assumption"},
	}}
	out, _, err := applyReviewPatch(input, patch, "RD-007")
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	atom := out.RequirementAtoms[0]
	if len(atom.Warnings) != 1 || !containsString(atom.ReviewDecisions, "RD-007") {
		t.Fatalf("assumption was not documented: %+v", atom)
	}
	if len(input.RequirementAtoms[0].Warnings) != 0 {
		t.Fatalf("input atoms were mutated")
	}
}
