package llmpipeline

import (
	"context"
	"testing"

	"dbdsl/internal/llm"
)

func TestRunSourceUnitExtractionBuildsCompleteOriginQA(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{{
		ID:         "OD-S-001",
		Text:       "Products have names.",
		Confidence: "high",
		DerivedFrom: []CombinedDocumentOrigin{{
			ResourceID: "R-001", LineStart: 3, LineEnd: 3, ExactText: "Products have names.",
		}},
	}}}
	proposal, qa, err := RunSourceUnitExtraction(context.Background(), llm.NewDefaultMockClient(), SourceUnitExtractionOptions{
		OutDir: t.TempDir(), Document: document, Model: "mock-model",
	})
	if err != nil {
		t.Fatalf("extract source units: %v", err)
	}
	if len(proposal.SourceUnits) != 1 || !qa.OK || qa.ODSentencesReferenced != 1 {
		t.Fatalf("unexpected source-unit result: proposal=%+v qa=%+v", proposal, qa)
	}
	if got := qa.OriginChains["SU-001"]; len(got) != 1 || got[0] != "R-001#L3-L3" {
		t.Fatalf("unexpected origin chain: %v", got)
	}
}

func TestValidateSourceUnitProposalRejectsInventedTextAndUnknownOD(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{{ID: "OD-S-001", Text: "Products have names."}}}
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

func TestSourceUnitFallbackPreservesCoverage(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{
		{ID: "OD-S-001", Text: "Products exist.", Confidence: "high"},
		{ID: "OD-S-002", Text: "Each product has a name.", Confidence: "low"},
	}}
	proposal, qa := BuildSourceUnitFallback(document)
	if !qa.OK || len(proposal.SourceUnits) != 2 || qa.ODSentencesReferenced != 2 {
		t.Fatalf("unexpected fallback: proposal=%+v qa=%+v", proposal, qa)
	}
	if len(qa.NeedsAttention) != 1 || qa.NeedsAttention[0] != "SU-002" {
		t.Fatalf("expected low-confidence unit to need attention: %+v", qa)
	}
}
