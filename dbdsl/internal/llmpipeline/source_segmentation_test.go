package llmpipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"dbdsl/internal/llm"
)

func segmentationFixture() ([]CombinedDocumentResource, []SourceSegment, CombinedDocumentProposal) {
	resources := []CombinedDocumentResource{{
		ID: "R-001", FileType: "text", Authority: "normative",
		Lines: []CombinedDocumentLine{
			{Number: 1, Text: "Sistem čuva podatke o"},
			{Number: 2, Text: "korisnicima."},
			{Number: 3, Text: "UNIVERZITET U BEOGRADU"},
		},
	}}
	segments, fallback, _ := BuildLosslessCombinedDocument(resources)
	return resources, segments, fallback
}

func TestSourceSegmentationGroupsExactSpansAndKeepsLayoutAuditable(t *testing.T) {
	resources, segments, fallback := segmentationFixture()
	proposal, document, qa := RunSourceSegmentation(context.Background(), llm.NewDefaultMockClient(), SourceSegmentationOptions{
		OutDir: t.TempDir(), Resources: resources, Segments: segments, Fallback: fallback,
		Model: "mock", MaxParallelism: 1,
	})
	if !qa.OK || proposal.FallbackUsed || !proposal.LLMAssisted {
		t.Fatalf("expected validated LLM-assisted segmentation: proposal=%+v qa=%+v", proposal, qa)
	}
	if len(proposal.Groups) != 2 || proposal.Groups[0].Role != "sentence" || proposal.Groups[1].Role != "layout_noise" {
		t.Fatalf("expected one semantic group and one retained layout group: %+v", proposal.Groups)
	}
	if len(document.Sentences) != 1 || document.Sentences[0].Text != "Sistem čuva podatke o korisnicima." {
		t.Fatalf("hard-wrapped sentence was not reconstructed from candidates: %+v", document.Sentences)
	}
	origins := document.Sentences[0].DerivedFrom
	if len(origins) != 2 || origins[0].SourceSegmentID == "" || origins[1].EndByte != len("korisnicima.") {
		t.Fatalf("exact segment and byte lineage was not preserved: %+v", origins)
	}
	report := BuildSourceFidelityReportFromSegmentation(segments, proposal)
	if !report.OK || report.NormativeCoverage != 1 || report.DispositionCounts["layout"] != 1 {
		t.Fatalf("layout must remain covered without entering the semantic document: %+v", report)
	}
}

func TestSourceSegmentationFallsBackWithoutLLM(t *testing.T) {
	resources, segments, fallback := segmentationFixture()
	proposal, document, qa := RunSourceSegmentation(context.Background(), nil, SourceSegmentationOptions{
		OutDir: t.TempDir(), Resources: resources, Segments: segments, Fallback: fallback,
	})
	if !qa.OK || !qa.FallbackUsed || !proposal.FallbackUsed || proposal.Strategy != "deterministic_fallback" {
		t.Fatalf("expected a valid nonblocking deterministic fallback: proposal=%+v qa=%+v", proposal, qa)
	}
	if len(document.Sentences) == 0 || !strings.Contains(strings.Join(document.Warnings, " "), "deterministic fallback") {
		t.Fatalf("fallback must be explicit in the combined artifact: %+v", document)
	}
	if document.Sentences[0].DerivedFrom[0].SourceSegmentID == "" {
		t.Fatalf("fallback must retain exact segment and byte lineage: %+v", document.Sentences[0].DerivedFrom)
	}
}

func TestSourceSegmentationRejectsInventedCandidateReference(t *testing.T) {
	resources, segments, fallback := segmentationFixture()
	client := &llm.MockClient{Structured: map[string]json.RawMessage{
		"source_segmentation": json.RawMessage(`{
			"classifications":[
				{"candidate_id":"INVENTED","role":"sentence","boundary":"start","join_to_candidate_id":"","confidence":"high","requires_review":false,"warnings":[]}
			],
			"warnings":[],"confidence_summary":{"overall":"high"}
		}`),
	}}
	proposal, _, qa := RunSourceSegmentation(context.Background(), client, SourceSegmentationOptions{
		OutDir: t.TempDir(), Resources: resources, Segments: segments, Fallback: fallback, Model: "mock",
	})
	if !qa.OK || !proposal.FallbackUsed || !strings.Contains(proposal.FallbackReason, "failed") {
		t.Fatalf("invalid LLM IDs must be rejected and safely fall back: proposal=%+v qa=%+v", proposal, qa)
	}
}
