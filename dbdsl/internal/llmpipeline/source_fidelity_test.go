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

func TestBuildLosslessCombinedDocumentSplitsSentencesWithoutLosingLineCoverage(t *testing.T) {
	resources := []CombinedDocumentResource{{ID: "R-001", FileType: "text", Authority: "normative", Lines: []CombinedDocumentLine{
		{Number: 1, Text: "Prva rečenica. Druga rečenica!"},
	}}}
	segments, proposal, report := BuildLosslessCombinedDocument(resources)
	if len(segments) != 1 || len(proposal.Sentences) != 2 {
		t.Fatalf("expected one physical segment and two OD sentences: segments=%+v units=%+v", segments, proposal.Sentences)
	}
	if proposal.Sentences[0].Kind != CombinedDocumentUnitSentence || proposal.Sentences[1].Text != "Druga rečenica!" {
		t.Fatalf("unexpected sentence segmentation: %+v", proposal.Sentences)
	}
	if !report.OK || len(report.Dispositions) != 1 || len(report.Dispositions[0].ODIDs) != 2 {
		t.Fatalf("one line must be covered by both sentences: %+v", report)
	}
}

func TestBuildLosslessCombinedDocumentMergesHardWrappedSentence(t *testing.T) {
	resources := []CombinedDocumentResource{{ID: "R-001", FileType: "text", Lines: []CombinedDocumentLine{
		{Number: 1, Text: "Sistem čuva podatke o"},
		{Number: 2, Text: "korisnicima."},
	}}}
	segments, proposal, report := BuildLosslessCombinedDocument(resources)
	if len(proposal.Sentences) != 1 || proposal.Sentences[0].Text != "Sistem čuva podatke o korisnicima." {
		t.Fatalf("hard-wrapped sentence was not merged: %+v", proposal.Sentences)
	}
	unit := proposal.Sentences[0]
	if unit.Transformation != "merged" || unit.DerivedFrom[0].LineStart != 1 || unit.DerivedFrom[0].LineEnd != 2 {
		t.Fatalf("unexpected multi-line provenance: %+v", unit)
	}
	if !report.OK || len(segments) != 2 || report.Dispositions[0].ODIDs[0] != unit.ID || report.Dispositions[1].ODIDs[0] != unit.ID {
		t.Fatalf("both physical lines must map to the merged sentence: %+v", report)
	}
}

func TestBuildLosslessCombinedDocumentHandlesAbbreviationsVersionsAndClosers(t *testing.T) {
	resources := []CombinedDocumentResource{{ID: "R-001", FileType: "text", Lines: []CombinedDocumentLine{
		{Number: 1, Text: `Npr. korisnik unosi verziju 3.5.x i vrednost 2.5. Sistem odgovara "uspešno." Sledeća rečenica?`},
	}}}
	_, proposal, _ := BuildLosslessCombinedDocument(resources)
	if len(proposal.Sentences) != 3 {
		t.Fatalf("expected three sentences, got %+v", proposal.Sentences)
	}
	if proposal.Sentences[0].Text != "Npr. korisnik unosi verziju 3.5.x i vrednost 2.5." {
		t.Fatalf("abbreviation, version, or decimal was split incorrectly: %q", proposal.Sentences[0].Text)
	}
	if proposal.Sentences[1].Text != `Sistem odgovara "uspešno."` {
		t.Fatalf("closing quote was not retained: %q", proposal.Sentences[1].Text)
	}
}

func TestBuildLosslessCombinedDocumentPreservesStructuralUnits(t *testing.T) {
	resources := []CombinedDocumentResource{{ID: "R-001", FileType: "markdown", Lines: []CombinedDocumentLine{
		{Number: 1, Text: "# Pravila"},
		{Number: 2, Text: "- Proizvod ima naziv. Cena je obavezna."},
		{Number: 3, Text: "- Kratka oznaka"},
	}}}
	_, proposal, report := BuildLosslessCombinedDocument(resources)
	if len(proposal.Sentences) != 4 {
		t.Fatalf("expected heading, two list sentences, and a structural fragment: %+v", proposal.Sentences)
	}
	if proposal.Sentences[0].Kind != CombinedDocumentUnitStructural || proposal.Sentences[0].Text != "# Pravila" {
		t.Fatalf("heading was not preserved structurally: %+v", proposal.Sentences[0])
	}
	if proposal.Sentences[1].Kind != CombinedDocumentUnitSentence || proposal.Sentences[1].Text != "- Proizvod ima naziv." {
		t.Fatalf("list marker was not preserved on the first sentence: %+v", proposal.Sentences[1])
	}
	if proposal.Sentences[3].Kind != CombinedDocumentUnitStructural {
		t.Fatalf("unterminated list item must be structural: %+v", proposal.Sentences[3])
	}
	if !report.OK || len(report.Dispositions[1].ODIDs) != 2 {
		t.Fatalf("multi-sentence list line lost fidelity: %+v", report)
	}
}

func TestBuildLosslessCombinedDocumentKeepsStructuredFormatsLineBased(t *testing.T) {
	resources := []CombinedDocumentResource{{ID: "R-001", FileType: "json", Lines: []CombinedDocumentLine{
		{Number: 1, Text: `{`},
		{Number: 2, Text: `"name": "Primer. Sa tačkom."`},
		{Number: 3, Text: `}`},
	}}}
	_, proposal, report := BuildLosslessCombinedDocument(resources)
	if len(proposal.Sentences) != 3 || !report.OK {
		t.Fatalf("structured format must retain one OD unit per non-empty line: %+v %+v", proposal, report)
	}
	for _, unit := range proposal.Sentences {
		if unit.Kind != CombinedDocumentUnitStructural {
			t.Fatalf("structured-format line classified as a sentence: %+v", unit)
		}
	}
}

func TestBuildLosslessCombinedDocumentUsesStableResourceAndIDOrder(t *testing.T) {
	resources := []CombinedDocumentResource{
		{ID: "R-002", FileType: "text", Lines: []CombinedDocumentLine{{Number: 1, Text: "Drugi resurs."}}},
		{ID: "R-001", FileType: "text", Lines: []CombinedDocumentLine{{Number: 1, Text: "Prvi resurs."}}},
	}
	_, proposal, _ := BuildLosslessCombinedDocument(resources)
	if proposal.Sentences[0].ID != "OD-S-0001" || proposal.Sentences[0].Text != "Prvi resurs." || proposal.Sentences[1].ID != "OD-S-0002" {
		t.Fatalf("resource or OD ID order is unstable: %+v", proposal.Sentences)
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
