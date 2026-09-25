package llmpipeline

import (
	"context"
	"slices"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
)

func TestOnlySourceProblemsNeedSourceReview(t *testing.T) {
	document := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{
		{ID: "OD-S-0001", Text: "Treba izvršiti neke osnovne provere polja."},
		{ID: "OD-S-0002", Text: "{ \"a\": [1, 2"},
		{ID: "OD-S-0003", Text: "Korisnik se registruje."},
	}}
	unit := func(id, od, confidence string, review bool, warnings, notes []string) SourceUnitProposal {
		text := ""
		for _, sentence := range document.Sentences {
			if sentence.ID == od {
				text = sentence.Text
			}
		}
		normalized, normalization := NormalizeSourceText(text)
		return SourceUnitProposal{ID: id, Kind: "business_rule", Relevance: "model_relevant", ExactText: text, NormalizedText: normalized,
			Normalization: normalization, ODSentenceIDs: []string{od}, Confidence: confidence, RequiresReview: review, Warnings: warnings, RequirementNotes: notes}
	}
	proposal := SourceUnitExtractionProposal{SourceUnits: []SourceUnitProposal{
		unit("SU-001", "OD-S-0001", "high", false, []string{"informational"}, []string{"Which checks apply is not stated."}),
		unit("SU-002", "OD-S-0002", "low", true, []string{"Damaged structured example."}, nil),
		unit("SU-003", "OD-S-0003", "high", false, nil, nil),
	}}
	qa := ValidateSourceUnitProposal(proposal, document, "test")
	if !slices.Equal(qa.NeedsAttention, []string{"SU-002"}) {
		t.Fatalf("only the damaged source unit needs source review, got %v (errors %v)", qa.NeedsAttention, qa.Errors)
	}
}

func TestRequirementNotesReachAtomExtraction(t *testing.T) {
	items := withRequirementNotes([]requirementSourceUnitInput{{ID: "SU-001"}, {ID: "SU-002"}}, map[string][]string{"SU-001": {"Which checks apply is not stated."}})
	if len(items[0].RequirementNotes) != 1 || items[1].RequirementNotes != nil {
		t.Fatalf("notes must attach only to their unit: %+v", items)
	}
}

func TestRequirementNotesDoNotAutomaticallyBlockExtractedAtom(t *testing.T) {
	proposal, qa, err := RunRequirementAtomExtraction(context.Background(), llm.NewDefaultMockClient(), RequirementAtomStageOptions{
		OutDir: t.TempDir(), Model: "mock-model",
		SourceUnits:     []dsl.SourceUnit{{ID: "SU-001", Kind: "requirement_sentence", Relevance: "model_relevant", Text: dsl.SourceUnitText{Exact: "Registration stores an email address."}}},
		SourceUnitNotes: map[string][]string{"SU-001": {"The email format is not specified."}},
	})
	if err != nil || !qa.OK || len(proposal.RequirementAtoms) != 1 {
		t.Fatalf("extract atom with requirement note: proposal=%+v qa=%+v err=%v", proposal, qa, err)
	}
	if proposal.RequirementAtoms[0].RequiresReview {
		t.Fatalf("a requirement note became an automatic blocker: %+v", proposal.RequirementAtoms[0])
	}
}
