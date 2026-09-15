package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"dbdsl/internal/llm"
)

var combinedDocumentIDPattern = regexp.MustCompile(`^OD-S-[0-9]{3,}$`)

type CombinedDocumentOptions struct {
	OutDir          string
	Resources       []CombinedDocumentResource
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
}

func RunCombinedDocument(ctx context.Context, client llm.Client, opts CombinedDocumentOptions) (CombinedDocumentProposal, error) {
	if client == nil {
		return CombinedDocumentProposal{}, errors.New("LLM client is required")
	}
	opts = normalizeCombinedDocumentOptions(opts)
	if opts.OutDir == "" {
		return CombinedDocumentProposal{}, errors.New("output directory is required")
	}
	if len(opts.Resources) == 0 {
		return CombinedDocumentProposal{}, errors.New("at least one ready resource is required")
	}

	input := mustJSON(map[string]any{
		"resources":        opts.Resources,
		"pipeline_version": "0.7",
		"output_contract":  "combined_document",
		"template_version": promptTemplateVersion,
	})
	var proposal CombinedDocumentProposal
	if err := runStructuredStage(ctx, client, opts.OutDir, 1, llm.Request{
		Stage:           "combined_document",
		Model:           opts.Model,
		Instructions:    combinedDocumentInstructions,
		Input:           input,
		SchemaName:      "DBDSLCombinedDocument",
		Schema:          combinedDocumentSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version": promptTemplateVersion,
			"resource_count":   fmt.Sprintf("%d", len(opts.Resources)),
		},
	}, &proposal, func() []string {
		return validateCombinedDocumentProposal(proposal, opts.Resources)
	}); err != nil {
		return CombinedDocumentProposal{}, err
	}
	return proposal, nil
}

func normalizeCombinedDocumentOptions(opts CombinedDocumentOptions) CombinedDocumentOptions {
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}
	return opts
}

func validateCombinedDocumentProposal(proposal CombinedDocumentProposal, resources []CombinedDocumentResource) []string {
	var out []string
	if len(proposal.Sentences) == 0 {
		out = append(out, "combined document produced no sentences")
	}
	lineCounts := map[string]int{}
	for _, resource := range resources {
		if strings.TrimSpace(resource.ID) == "" {
			out = append(out, "resource with empty id")
			continue
		}
		lineCounts[resource.ID] = resource.LineCount
	}
	seen := map[string]bool{}
	for _, sentence := range proposal.Sentences {
		if !combinedDocumentIDPattern.MatchString(sentence.ID) {
			out = append(out, fmt.Sprintf("invalid combined sentence id %q", sentence.ID))
		}
		if seen[sentence.ID] {
			out = append(out, fmt.Sprintf("duplicate combined sentence id %q", sentence.ID))
		}
		seen[sentence.ID] = true
		if strings.TrimSpace(sentence.Text) == "" {
			out = append(out, fmt.Sprintf("%s has empty text", sentence.ID))
		}
		if len(sentence.DerivedFrom) == 0 {
			out = append(out, fmt.Sprintf("%s has no lineage", sentence.ID))
		}
		for _, origin := range sentence.DerivedFrom {
			lineCount, ok := lineCounts[origin.ResourceID]
			if !ok {
				out = append(out, fmt.Sprintf("%s references unknown resource %s", sentence.ID, origin.ResourceID))
				continue
			}
			if origin.LineStart <= 0 || origin.LineEnd <= 0 || origin.LineStart > origin.LineEnd {
				out = append(out, fmt.Sprintf("%s has invalid line span for %s", sentence.ID, origin.ResourceID))
				continue
			}
			if lineCount > 0 && origin.LineEnd > lineCount {
				out = append(out, fmt.Sprintf("%s references line %d beyond %s line count %d", sentence.ID, origin.LineEnd, origin.ResourceID, lineCount))
			}
			if strings.TrimSpace(origin.ExactText) == "" {
				out = append(out, fmt.Sprintf("%s has empty exact_text for %s", sentence.ID, origin.ResourceID))
			}
		}
	}
	return out
}
