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
		}, &response, func(bool) []string { return validateSourceSegmentationResponse(response, document) })
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

func validateSourceSegmentationResponse(response sourceSegmentationResponse, document string) []string {
	var issues []string
	for index, segment := range response.Segments {
		if strings.TrimSpace(segment.Text) == "" {
			issues = append(issues, fmt.Sprintf("segment %d has empty text", index+1))
		}
	}
	issues = append(issues, validateSegmentationFidelity(document, response.Segments)...)
	return issues
}

// validateSegmentationFidelity is the deterministic boundary after the LLM,
// checked on words and physical lines, not on a pool of characters:
//   - a page header, footer, number or footnote must be one or more complete
//     source lines, in source order; only such lines may be left out of the
//     sentences they interrupt, so no word of a sentence can hide in them;
//   - all other segments, in order, must give exactly the words of the other
//     lines, in order. Whitespace may change; the one allowed edit is joining a
//     word broken by a hyphen at the end of a line, with or without the hyphen.
func validateSegmentationFidelity(document string, segments []sourceSegmentationResponseSegment) []string {
	lines := sourceLines(document)
	consumed := make([]bool, len(lines))
	cursor := 0
	for index, segment := range segments {
		if !isInterruptingSegment(segment.Type) || strings.TrimSpace(segment.Text) == "" {
			continue
		}
		start, end, ok := matchWholeLines(lines, consumed, cursor, compactContent(segment.Text))
		if !ok {
			return []string{fmt.Sprintf("segment %d (%s) is not one or more complete source lines in source order; only whole lines can be page furniture or a footnote", index+1, segment.Type)}
		}
		for line := start; line <= end; line++ {
			consumed[line] = true
		}
		cursor = end + 1
	}

	type word struct {
		text     string
		joinable bool // ends a line with a hyphen and a content line follows
	}
	var source []word
	for index, line := range lines {
		if consumed[index] {
			continue
		}
		for position, token := range line {
			joinable := position == len(line)-1 && strings.HasSuffix(token, "-") && nextContentLine(lines, consumed, index) >= 0
			source = append(source, word{text: token, joinable: joinable})
		}
	}
	var returned []string
	var owner []int
	for index, segment := range segments {
		if isInterruptingSegment(segment.Type) {
			continue
		}
		for _, token := range strings.Fields(segment.Text) {
			returned = append(returned, token)
			owner = append(owner, index+1)
		}
	}
	i, j := 0, 0
	for i < len(source) && j < len(returned) {
		switch {
		case source[i].text == returned[j]:
			i, j = i+1, j+1
		case source[i].joinable && i+1 < len(source) &&
			(returned[j] == source[i].text+source[i+1].text || returned[j] == strings.TrimSuffix(source[i].text, "-")+source[i+1].text):
			i, j = i+2, j+1
		default:
			return []string{fmt.Sprintf("segment %d changes the source text: expected %q, found %q; copy the words exactly", owner[j], wordsAround(source, i, func(w word) string { return w.text }), wordsAround(returned, j, func(w string) string { return w }))}
		}
	}
	switch {
	case i < len(source):
		return []string{fmt.Sprintf("segments leave out source text starting at %q", wordsAround(source, i, func(w word) string { return w.text }))}
	case j < len(returned):
		return []string{fmt.Sprintf("segment %d adds text that is not in the source: %q", owner[j], wordsAround(returned, j, func(w string) string { return w }))}
	}
	return nil
}

// sourceLines splits the document into lines of words, leaving out empty lines.
func sourceLines(document string) [][]string {
	var lines [][]string
	for _, line := range strings.Split(strings.ReplaceAll(document, "\r\n", "\n"), "\n") {
		if words := strings.Fields(line); len(words) > 0 {
			lines = append(lines, words)
		}
	}
	return lines
}

func nextContentLine(lines [][]string, consumed []bool, after int) int {
	for index := after + 1; index < len(lines); index++ {
		if !consumed[index] {
			return index
		}
	}
	return -1
}

// matchWholeLines finds, from the cursor on, a run of complete unconsumed lines
// whose text without whitespace is the candidate. A hyphen that ends a line in
// the run may be dropped, as a word broken across the lines is joined.
func matchWholeLines(lines [][]string, consumed []bool, cursor int, candidate []rune) (int, int, bool) {
	if len(candidate) == 0 {
		return 0, 0, false
	}
	for start := cursor; start < len(lines); start++ {
		if consumed[start] {
			continue
		}
		position := 0
		for end := start; end < len(lines) && !consumed[end]; end++ {
			line := []rune(strings.Join(lines[end], ""))
			matched := true
			for k, r := range line {
				if position < len(candidate) && candidate[position] == r {
					position++
					continue
				}
				if r == '-' && k == len(line)-1 && end+1 < len(lines) {
					continue
				}
				matched = false
				break
			}
			if !matched {
				break
			}
			if position == len(candidate) {
				return start, end, true
			}
		}
	}
	return 0, 0, false
}

func compactContent(value string) []rune {
	return []rune(strings.Join(strings.Fields(value), ""))
}

// wordsAround shows up to six words from index, for a readable message.
func wordsAround[T any](items []T, index int, text func(T) string) string {
	end := index + 6
	if end > len(items) {
		end = len(items)
	}
	parts := []string{}
	for _, item := range items[index:end] {
		parts = append(parts, text(item))
	}
	return strings.Join(parts, " ")
}

func isInterruptingSegment(kind string) bool {
	switch kind {
	case "page_header", "page_footer", "page_number", "footnote":
		return true
	default:
		return false
	}
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
