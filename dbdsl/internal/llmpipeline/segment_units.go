package llmpipeline

import "dbdsl/internal/dsl"

// SegmentUnitStrategy marks source units derived one-to-one from LLM segments,
// without a second LLM classification pass.
const SegmentUnitStrategy = "segments_v1"

var pageNoiseSegmentTypes = map[string]bool{"page_header": true, "page_footer": true, "page_number": true}

// BuildSourceUnitsFromSegments serializes the segments into the source-unit
// contract: ID, type and text, unchanged. IDs were already assigned by
// RunSourceSegmentation.
func BuildSourceUnitsFromSegments(document CombinedDocumentProposal) (SourceUnitExtractionProposal, SourceUnitQA) {
	units := make([]SourceUnitProposal, 0, len(document.Sentences))
	for _, segment := range document.Sentences {
		units = append(units, SourceUnitProposal{
			ID: segment.ID, Kind: segment.Role, Tags: []string{},
			ExactText: segment.Text, NormalizedText: segment.Text, Normalization: unnormalizedText(),
			SegmentIDs: []string{segment.ID}, Warnings: []string{},
		})
	}
	proposal := SourceUnitExtractionProposal{
		SourceUnits:       units,
		Warnings:          []string{},
		ConfidenceSummary: map[string]string{"strategy": SegmentUnitStrategy},
	}
	return proposal, ValidateSourceUnitProposal(proposal, document, SegmentUnitStrategy)
}

// SegmentTextNormalization marks source-unit text taken verbatim from the
// segmentation response: the backend does not normalize it.
const SegmentTextNormalization = "none"

func unnormalizedText() dsl.SourceTextNormalization {
	return dsl.SourceTextNormalization{Version: SegmentTextNormalization, Strategy: SegmentTextNormalization, Operations: []dsl.SourceNormalizationOperation{}}
}
