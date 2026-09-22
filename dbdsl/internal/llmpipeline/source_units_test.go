package llmpipeline

import (
	"context"
	"encoding/json"
	"testing"

	"dbdsl/internal/llm"
)

func TestRunSourceUnitExtractionBuildsCompleteOriginQA(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{{
		ID:         "OD-S-0001",
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

func TestSourceUnitLLMOutputContainsClassificationNotLosslessText(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{{ID: "OD-S-0001", Text: "Products   have names .", Confidence: "high"}}}
	outDir := t.TempDir()
	proposal, _, err := RunSourceUnitExtraction(context.Background(), llm.NewDefaultMockClient(), SourceUnitExtractionOptions{OutDir: outDir, Document: document, Model: "mock-model"})
	if err != nil {
		t.Fatalf("extract source units: %v", err)
	}
	var response map[string]any
	readJSONTestFile(t, outDir+"/llm_runs/002_source_unit_extraction/response.parsed.json", &response)
	encoded, _ := json.Marshal(response)
	var decoded struct {
		Classifications []map[string]any `json:"classifications"`
	}
	if json.Unmarshal(encoded, &decoded) != nil || len(decoded.Classifications) != 1 {
		t.Fatalf("unexpected classification response: %s", encoded)
	}
	if _, exists := decoded.Classifications[0]["exact_text"]; exists {
		t.Fatal("LLM classification must not echo backend-owned exact_text")
	}
	if _, exists := decoded.Classifications[0]["normalized_text"]; exists {
		t.Fatal("LLM classification must not author backend-owned normalized_text")
	}
	if proposal.SourceUnits[0].ExactText != document.Sentences[0].Text || proposal.SourceUnits[0].ID != "SU-001" {
		t.Fatalf("backend did not restore stable lossless source unit: %+v", proposal.SourceUnits[0])
	}
	if proposal.SourceUnits[0].NormalizedText != "Products have names." || proposal.SourceUnits[0].Normalization.Version != SourceTextNormalizationVersion {
		t.Fatalf("backend did not deterministically normalize source text: %+v", proposal.SourceUnits[0])
	}
}

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

func TestSourceUnitFallbackPreservesCoverage(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{
		{ID: "OD-S-0001", Text: "Products exist.", Confidence: "high"},
		{ID: "OD-S-0002", Text: "Each product has a name.", Confidence: "low"},
	}}
	proposal, qa := BuildSourceUnitFallback(document)
	if !qa.OK || len(proposal.SourceUnits) != 2 || qa.ODSentencesReferenced != 2 {
		t.Fatalf("unexpected fallback: proposal=%+v qa=%+v", proposal, qa)
	}
	if len(qa.NeedsAttention) != 1 || qa.NeedsAttention[0] != "SU-002" {
		t.Fatalf("expected low-confidence unit to need attention: %+v", qa)
	}
}

func TestSourceUnitExtractionReceivesStructuralKind(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{{
		ID: "OD-S-0001", Kind: CombinedDocumentUnitStructural, Text: "# Rules", Confidence: "high",
	}}}
	proposal, qa, err := RunSourceUnitExtraction(context.Background(), llm.NewDefaultMockClient(), SourceUnitExtractionOptions{
		OutDir: t.TempDir(), Document: document, Model: "mock-model",
	})
	if err != nil {
		t.Fatalf("extract structural source unit: %v", err)
	}
	if !qa.OK || len(proposal.SourceUnits) != 1 || proposal.SourceUnits[0].Kind != "heading" || proposal.SourceUnits[0].Relevance != "model_supporting" {
		t.Fatalf("structural kind was not propagated to classification: proposal=%+v qa=%+v", proposal, qa)
	}

	fallback, fallbackQA := BuildSourceUnitFallback(document)
	if !fallbackQA.OK || fallback.SourceUnits[0].Kind != "heading" || fallback.SourceUnits[0].Relevance != "model_supporting" {
		t.Fatalf("structural fallback was not classified safely: proposal=%+v qa=%+v", fallback, fallbackQA)
	}
}
