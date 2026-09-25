package llmpipeline

import (
	"dbdsl/internal/dsl"
	"testing"
)

func TestValidateConceptualModelAllowsEntityToFileConceptRelationship(t *testing.T) {
	evidence := EvidenceProposal{
		SourceUnits:  []string{"SU-001"},
		SupportLevel: "explicit", Confidence: "high",
	}
	proposal := ConceptualModelProposal{
		EntityConcepts: []ConceptualEntityProposal{{
			ID: "E-REGISTRATION", Label: "Registration", Kind: "regular", Evidence: evidence,
		}},
		FileConcepts: []PlanElementProposal{{
			ID: "FC-001", Label: "Profile image", Kind: "profile_image",
			SourceUnits: []string{"SU-001"},
		}},
		Relationships: []ConceptualRelationshipProposal{{
			ID: "R-REG-IMAGE", From: "E-REGISTRATION", To: "FC-001",
			Cardinality: "one_to_one", Evidence: evidence,
		}},
	}
	units := []dsl.SourceUnit{{ID: "SU-001"}}

	qa := ValidateConceptualModel(proposal, units, nil)
	if !qa.OK || len(qa.Errors) != 0 {
		t.Fatalf("entity-to-file relationship must be valid: %+v", qa)
	}

	proposal.Relationships[0].From = "E-UNKNOWN"
	qa = ValidateConceptualModel(proposal, units, nil)
	if qa.OK || len(qa.Errors) != 1 || qa.Errors[0] != "R-REG-IMAGE references unknown relationship endpoint" {
		t.Fatalf("unknown endpoint must remain invalid: %+v", qa)
	}
}
