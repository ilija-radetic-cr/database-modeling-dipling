package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"dbdsl/internal/llm"
)

type SourceSegmentationOptions struct {
	OutDir          string
	Resources       []CombinedDocumentResource
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	PromptVersion   string
}

type sourceSegmentationResponse struct {
	Segments []sourceSegmentationResponseSegment `json:"segments"`
}

type sourceSegmentationResponseSegment struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func RunSourceSegmentation(ctx context.Context, client llm.Client, opts SourceSegmentationOptions) (SourceSegmentationProposal, CombinedDocumentProposal, error) {
	if client == nil {
		return SourceSegmentationProposal{}, CombinedDocumentProposal{}, errors.New("LLM client is required for source segmentation")
	}
	if opts.OutDir == "" {
		return SourceSegmentationProposal{}, CombinedDocumentProposal{}, errors.New("output directory is required")
	}
	if len(opts.Resources) == 0 {
		return SourceSegmentationProposal{}, CombinedDocumentProposal{}, errors.New("at least one source resource is required")
	}
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}

	proposal := SourceSegmentationProposal{Segments: []SourceSegmentationSegment{}}
	for resourceIndex, resource := range opts.Resources {
		document := resourceText(resource)
		input := "<document>\n" + document + "\n</document>"
		var response sourceSegmentationResponse
		err := runStructuredStage(ctx, client, opts.OutDir, 1, llm.Request{
			Stage:           "source_segmentation",
			Model:           opts.Model,
			Instructions:    sourceSegmentationInstructions,
			Input:           input,
			SchemaName:      "DBDSLSourceSegmentation",
			Schema:          sourceSegmentationSchema(),
			ReasoningEffort: opts.ReasoningEffort,
			MaxOutputTokens: opts.MaxOutputTokens,
			Metadata: map[string]string{
				"template_version":        resolvedPromptVersion(opts.PromptVersion),
				"run_key":                 resource.ID,
				"resource_index":          fmt.Sprint(resourceIndex + 1),
				"resource_count":          fmt.Sprint(len(opts.Resources)),
				"full_context_bytes":      fmt.Sprint(len(input)),
				"context_policy":          "complete_resource_v1",
				"canonicalizer_version":   "backend_source_unit_ids_v1",
				"call_gate_policy":        "always_v1",
				"budget_policy":           "adaptive_v1",
				"stage_max_output_tokens": fmt.Sprint(opts.MaxOutputTokens),
				"retry_policy":            "none",
			},
		}, &response, func() []string { return validateSourceSegmentationResponse(response) })
		if err != nil {
			return SourceSegmentationProposal{}, CombinedDocumentProposal{}, fmt.Errorf("segment resource %s: %w", resource.ID, err)
		}
		// The backend only numbers the segments. Their text is stored exactly as
		// the LLM returned it; nothing is normalized, split, linked or classified.
		for _, segment := range response.Segments {
			proposal.Segments = append(proposal.Segments, SourceSegmentationSegment{
				ID: fmt.Sprintf("SU-%03d", len(proposal.Segments)+1), Type: segment.Type, Text: segment.Text,
			})
		}
	}

	return proposal, combinedDocumentFromSegments(proposal), nil
}

func validateSourceSegmentationResponse(response sourceSegmentationResponse) []string {
	var issues []string
	for index, segment := range response.Segments {
		if strings.TrimSpace(segment.Text) == "" {
			issues = append(issues, fmt.Sprintf("segment %d has empty text", index+1))
		}
	}
	return issues
}

func resourceText(resource CombinedDocumentResource) string {
	lines := make([]string, 0, len(resource.Lines))
	for _, line := range resource.Lines {
		lines = append(lines, line.Text)
	}
	return strings.Join(lines, "\n")
}

func combinedDocumentFromSegments(proposal SourceSegmentationProposal) CombinedDocumentProposal {
	units := make([]CombinedDocumentSentence, 0, len(proposal.Segments))
	for _, segment := range proposal.Segments {
		kind := CombinedDocumentUnitStructural
		if segment.Type == "sentence" {
			kind = CombinedDocumentUnitSentence
		}
		units = append(units, CombinedDocumentSentence{
			ID: segment.ID, Kind: kind, Role: segment.Type, Text: segment.Text, NormalizedText: segment.Text,
			Tags: []string{}, Warnings: []string{},
		})
	}
	return CombinedDocumentProposal{Sentences: units, Warnings: []string{}, ConfidenceSummary: map[string]string{}}
}
