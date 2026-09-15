package llmpipeline

import (
	"fmt"
	"sort"
	"strings"
)

// BuildLosslessCombinedDocument creates a one-to-one OD view over every
// non-empty extracted source line. Semantic merging remains the responsibility
// of source-unit extraction; the OD layer is deliberately lossless.
func BuildLosslessCombinedDocument(resources []CombinedDocumentResource) ([]SourceSegment, CombinedDocumentProposal, SourceFidelityReport) {
	ordered := append([]CombinedDocumentResource(nil), resources...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	segments := []SourceSegment{}
	sentences := []CombinedDocumentSentence{}
	dispositions := []SourceSegmentDisposition{}
	normative := 0
	for _, resource := range ordered {
		segmentNo := 0
		for _, line := range resource.Lines {
			text := strings.TrimSpace(line.Text)
			if text == "" {
				continue
			}
			segmentNo++
			segmentID := fmt.Sprintf("%s-S-%04d", resource.ID, segmentNo)
			odID := fmt.Sprintf("OD-S-%04d", len(sentences)+1)
			authority := strings.TrimSpace(resource.Authority)
			if authority == "" {
				authority = "normative"
			}
			segments = append(segments, SourceSegment{
				ID: segmentID, ResourceID: resource.ID, LineStart: line.Number,
				LineEnd: line.Number, Text: text, Authority: authority,
			})
			sentences = append(sentences, CombinedDocumentSentence{
				ID: odID, Text: text,
				DerivedFrom:    []CombinedDocumentOrigin{{ResourceID: resource.ID, LineStart: line.Number, LineEnd: line.Number, ExactText: text}},
				Transformation: "copied", Confidence: "high", Warnings: []string{},
			})
			dispositions = append(dispositions, SourceSegmentDisposition{SegmentID: segmentID, Status: "retained", ODIDs: []string{odID}})
			if authority == "normative" {
				normative++
			}
		}
	}
	coverage := 1.0
	if normative == 0 {
		coverage = 1.0
	}
	report := SourceFidelityReport{
		OK: true, PipelineVersion: "0.7", SegmentsTotal: len(segments),
		NormativeSegments: normative, NormativeCovered: normative, NormativeCoverage: coverage,
		DispositionCounts:   map[string]int{"retained": len(segments)},
		UncoveredSegmentIDs: []string{}, NeedsAttention: []string{}, Dispositions: dispositions,
		Errors: []string{}, Warnings: []string{},
	}
	proposal := CombinedDocumentProposal{
		Sentences: sentences, Warnings: []string{},
		ConfidenceSummary: map[string]string{"overall": "deterministic lossless source-line projection"},
	}
	if len(segments) == 0 {
		report.OK = false
		report.Errors = append(report.Errors, "no non-empty source segments were produced")
	}
	return segments, proposal, report
}
