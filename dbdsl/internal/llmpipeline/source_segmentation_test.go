package llmpipeline

import (
	"context"
	"encoding/json"
	"testing"

	"dbdsl/internal/llm"
)

func segmentationResources() []CombinedDocumentResource {
	return []CombinedDocumentResource{
		{ID: "R-001", Lines: []CombinedDocumentLine{{Number: 1, Text: "Prvi dokument."}}},
		{ID: "R-002", Lines: []CombinedDocumentLine{{Number: 1, Text: "Drugi dokument."}}},
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
