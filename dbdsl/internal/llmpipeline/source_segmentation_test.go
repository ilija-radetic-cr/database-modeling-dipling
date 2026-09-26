package llmpipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dbdsl/internal/llm"
)

func segmentationResources() []CombinedDocumentResource {
	return []CombinedDocumentResource{
		{ID: "R-001", Lines: []CombinedDocumentLine{{Number: 1, Text: "  oštećen { tekst  "}}},
		{ID: "R-002", Lines: []CombinedDocumentLine{{Number: 1, Text: "  oštećen { tekst  "}}},
	}
}

func TestSourceSegmentationAddsOnlyBackendIDs(t *testing.T) {
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{"segments":[{"type":"other","text":"  oštećen { tekst  "}]}`),
	}}
	proposal, document, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Resources: segmentationResources(), Model: "mock",
	})
	if err != nil {
		t.Fatalf("run segmentation: %v", err)
	}
	if len(proposal.Segments) != 2 {
		t.Fatalf("expected one untouched response segment per resource: %+v", proposal.Segments)
	}
	if got := proposal.Segments[0]; got.ID != "SU-001" || got.Type != "other" || got.Text != "  oštećen { tekst  " {
		t.Fatalf("backend changed the first LLM segment: %+v", got)
	}
	if proposal.Segments[1].ID != "SU-002" {
		t.Fatalf("IDs are not global and sequential: %+v", proposal.Segments)
	}
	if len(document.Sentences) != 2 || document.Sentences[0].Text != proposal.Segments[0].Text || len(document.Sentences[0].DerivedFrom) != 0 {
		t.Fatalf("combined document must be a lineage-free projection: %+v", document.Sentences)
	}
}

func TestSourceSegmentationValidatesReturnedEvidenceUnits(t *testing.T) {
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{"segments":[{"type":"sentence","text":""}]}`),
	}}
	proposal, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Resources: segmentationResources()[:1], Model: "mock",
	})
	if err == nil || len(proposal.Segments) != 0 {
		t.Fatalf("invalid LLM evidence unit was accepted: proposal=%+v err=%v", proposal.Segments, err)
	}
}

func TestSourceSegmentationRequiresLLM(t *testing.T) {
	_, _, err := RunSourceSegmentation(context.Background(), nil, SourceSegmentationOptions{
		OutDir: t.TempDir(), Resources: segmentationResources()[:1],
	})
	if err == nil {
		t.Fatal("expected missing LLM client to fail without fallback")
	}
}

func TestSourceSegmentationAcceptsDocumentedFormattingNormalization(t *testing.T) {
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{"segments":[
			{"type":"sentence","text":"Arhivirani zapis ima broj 42."},
			{"type":"sentence","text":"E-mail ostaje označen."}
		]}`),
	}}
	_, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Model: "mock", Resources: []CombinedDocumentResource{{ID: "R-001", Lines: []CombinedDocumentLine{
			{Number: 1, Text: "Arhivi-"},
			{Number: 2, Text: "rani zapis  ima broj 42."},
			{Number: 3, Text: "E-"},
			{Number: 4, Text: "mail ostaje označen."},
		}}},
	})
	if err != nil {
		t.Fatalf("documented whitespace and line-wrap hyphen normalization was rejected: %v", err)
	}
}

func TestSourceSegmentationAcceptsInterruptedSentenceReassembly(t *testing.T) {
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{"segments":[
			{"type":"sentence","text":"Korisnik čuva podatke."},
			{"type":"page_header","text":"FAKULTET"}
		]}`),
	}}
	_, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Model: "mock", Resources: []CombinedDocumentResource{{ID: "R-001", Lines: []CombinedDocumentLine{
			{Number: 1, Text: "Korisnik čuva"},
			{Number: 2, Text: "FAKULTET"},
			{Number: 3, Text: "podatke."},
		}}},
	})
	if err != nil {
		t.Fatalf("sentence interrupted by page furniture was rejected: %v", err)
	}
}

func TestSourceSegmentationAcceptsMultipleFurnitureUnitsAfterReassembledSentence(t *testing.T) {
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{"segments":[
			{"type":"sentence","text":"Korisnik čuva podatke i nastavlja."},
			{"type":"page_header","text":"HEADER"},
			{"type":"footnote","text":"fusnota"}
		]}`),
	}}
	_, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Model: "mock", Resources: []CombinedDocumentResource{{ID: "R-001", Lines: []CombinedDocumentLine{
			{Number: 1, Text: "Korisnik čuva"},
			{Number: 2, Text: "HEADER"},
			{Number: 3, Text: "podatke i"},
			{Number: 4, Text: "fusnota"},
			{Number: 5, Text: "nastavlja."},
		}}},
	})
	if err != nil {
		t.Fatalf("ordered furniture moved after an interrupted sentence was rejected: %v", err)
	}
}

func TestSourceSegmentationRejectsReorderedFurnitureUnits(t *testing.T) {
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{"segments":[
			{"type":"page_footer","text":"FOOTER-TWO"},
			{"type":"page_header","text":"HEADER-ONE"}
		]}`),
	}}
	_, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Model: "mock", Resources: []CombinedDocumentResource{{ID: "R-001", Lines: []CombinedDocumentLine{
			{Number: 1, Text: "HEADER-ONE"},
			{Number: 2, Text: "FOOTER-TWO"},
		}}},
	})
	if err == nil || !strings.Contains(err.Error(), "complete source lines in source order") {
		t.Fatalf("reordered furniture with unchanged aggregate characters must be rejected: %v", err)
	}
}

func TestSourceSegmentationRejectsInventedFurnitureAnagram(t *testing.T) {
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{"segments":[{"type":"page_header","text":"FEDCBA"}]}`),
	}}
	_, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Model: "mock", Resources: []CombinedDocumentResource{{ID: "R-001", Lines: []CombinedDocumentLine{{Number: 1, Text: "ABCDEF"}}}},
	})
	if err == nil || !strings.Contains(err.Error(), "complete source lines in source order") {
		t.Fatalf("invented furniture anagram with unchanged aggregate characters must be rejected: %v", err)
	}
}

func TestSourceSegmentationRejectsMissingInventedNegatedOrChangedContent(t *testing.T) {
	tests := map[string]string{
		"missing fact":   `{"segments":[{"type":"sentence","text":"Korisnik čuva zapis."}]}`,
		"invented fact":  `{"segments":[{"type":"sentence","text":"Korisnik ne čuva zapis i račun."}]}`,
		"lost negation":  `{"segments":[{"type":"sentence","text":"Korisnik čuva zapis 42."}]}`,
		"changed number": `{"segments":[{"type":"sentence","text":"Korisnik ne čuva zapis 24."}]}`,
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			client := &llm.MockClient{Structured: map[string]json.RawMessage{"source_segmentation": json.RawMessage(payload)}}
			_, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
				OutDir: t.TempDir(), Model: "mock", Resources: []CombinedDocumentResource{{ID: "R-001", Lines: []CombinedDocumentLine{
					{Number: 1, Text: "Korisnik ne čuva zapis 42. Drugi podatak postoji."},
				}}},
			})
			if err == nil {
				t.Fatal("semantically unfaithful segmentation was accepted")
			}
		})
	}
}

func segmentationError(t *testing.T, lines []string, response string) error {
	t.Helper()
	resource := CombinedDocumentResource{ID: "R-001"}
	for i, line := range lines {
		resource.Lines = append(resource.Lines, CombinedDocumentLine{Number: i + 1, Text: line})
	}
	client := &llm.MockClient{Structured: map[string]json.RawMessage{"source_segmentation": json.RawMessage(response)}}
	_, _, err := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{OutDir: t.TempDir(), Model: "mock", Resources: []CombinedDocumentResource{resource}})
	return err
}

func TestSourceSegmentationKeepsWordBoundaries(t *testing.T) {
	err := segmentationError(t, []string{"The equipment is now here."}, `{"segments":[{"type":"sentence","text":"The equipment is nowhere."}]}`)
	if err == nil || !strings.Contains(err.Error(), "changes the source text") {
		t.Fatalf("joining two words must be rejected: %v", err)
	}
}

func TestSourceSegmentationCannotHideWordsAsPageFurniture(t *testing.T) {
	err := segmentationError(t, []string{"Members may not approve."}, `{"segments":[{"type":"sentence","text":"Members may approve."},{"type":"page_header","text":"not"}]}`)
	if err == nil || !strings.Contains(err.Error(), "complete source lines") {
		t.Fatalf("a word taken out of a sentence as page furniture must be rejected: %v", err)
	}
}

func TestSourceSegmentationJoinsWordsBrokenAtLineEnd(t *testing.T) {
	lines := []string{"Relacija Beograd-", "Jagodina i studenti-", "demonstratori rade."}
	for _, text := range []string{"Relacija Beograd-Jagodina i studenti-demonstratori rade.", "Relacija BeogradJagodina i studentidemonstratori rade.", "Relacija Beograd- Jagodina i studenti- demonstratori rade."} {
		if err := segmentationError(t, lines, `{"segments":[{"type":"sentence","text":"`+text+`"}]}`); err != nil {
			t.Fatalf("a word broken at a line end may be joined with or without its hyphen (%q): %v", text, err)
		}
	}
}
