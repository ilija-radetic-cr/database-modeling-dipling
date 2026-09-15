package llmpipeline

import "testing"

func TestBuildLosslessCombinedDocumentPreservesEveryNonEmptyLine(t *testing.T) {
	resources := []CombinedDocumentResource{{ID: "R-002", Authority: "normative", Lines: []CombinedDocumentLine{
		{Number: 1, Text: "Prva činjenica."}, {Number: 2, Text: ""}, {Number: 3, Text: "Druga činjenica sa brojem 15."},
	}}}
	segments, proposal, report := BuildLosslessCombinedDocument(resources)
	if !report.OK || report.NormativeCoverage != 1 || len(segments) != 2 || len(proposal.Sentences) != 2 {
		t.Fatalf("unexpected fidelity result: segments=%d sentences=%d report=%+v", len(segments), len(proposal.Sentences), report)
	}
	if proposal.Sentences[1].Text != "Druga činjenica sa brojem 15." || proposal.Sentences[1].DerivedFrom[0].LineStart != 3 {
		t.Fatalf("line content or provenance changed: %+v", proposal.Sentences[1])
	}
}

func TestSourceUnitValidationRejectsCitationWithoutTextPreservation(t *testing.T) {
	combined := CombinedDocumentProposal{Sentences: []CombinedDocumentSentence{
		{ID: "OD-S-0001", Text: "Igra traje 90 sekundi."},
		{ID: "OD-S-0002", Text: "Čuva se broj poena."},
	}}
	proposal := SourceUnitExtractionProposal{SourceUnits: []SourceUnitProposal{{
		ID: "SU-001", ExactText: "Igra traje 90 sekundi.", NormalizedText: "Igra traje 90 sekundi.", ODSentenceIDs: []string{"OD-S-0001", "OD-S-0002"},
	}}}
	qa := ValidateSourceUnitProposal(proposal, combined, "test")
	if qa.OK {
		t.Fatalf("citation-only coverage must not pass: %+v", qa)
	}
}
