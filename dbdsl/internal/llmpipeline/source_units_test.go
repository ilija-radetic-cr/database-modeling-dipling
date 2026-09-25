package llmpipeline

import (
	"testing"
)

func TestValidateSourceUnitProposalRejectsInventedTextAndUnknownOD(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{{ID: "OD-S-0001", Text: "Products have names."}}}
	proposal := SourceUnitExtractionProposal{SourceUnits: []SourceUnitProposal{{
		ID: "SU-001", Kind: "requirement_sentence", Relevance: "model_relevant",
		ExactText: "Products have secret prices.", NormalizedText: "Products have secret prices.",
		ODSentenceIDs: []string{"OD-S-999"}, Confidence: "high",
	}}}
	qa := ValidateSourceUnitProposal(proposal, document, "llm")
	if qa.OK || len(qa.Errors) < 2 {
		t.Fatalf("expected unknown reference and coverage errors, got %+v", qa)
	}
}
