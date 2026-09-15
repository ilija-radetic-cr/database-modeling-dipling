package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"dbdsl/internal/llm"
)

var sourceUnitIDPattern = regexp.MustCompile(`^SU-[0-9]{3,}$`)

type SourceUnitExtractionOptions struct {
	OutDir          string
	Document        CombinedDocumentProposal
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
}

func RunSourceUnitExtraction(ctx context.Context, client llm.Client, opts SourceUnitExtractionOptions) (SourceUnitExtractionProposal, SourceUnitQA, error) {
	if client == nil {
		return SourceUnitExtractionProposal{}, SourceUnitQA{}, errors.New("LLM client is required")
	}
	if opts.OutDir == "" {
		return SourceUnitExtractionProposal{}, SourceUnitQA{}, errors.New("output directory is required")
	}
	if len(opts.Document.Sentences) == 0 {
		return SourceUnitExtractionProposal{}, SourceUnitQA{}, errors.New("combined document has no sentences")
	}
	opts.Model = nonEmpty(opts.Model, llm.DefaultModel)
	opts.ReasoningEffort = nonEmpty(opts.ReasoningEffort, llm.DefaultReasoningEffort)
	if opts.MaxOutputTokens <= 0 {
		opts.MaxOutputTokens = llm.DefaultMaxOutputTokens
	}
	input := mustJSON(map[string]any{
		"combined_document": opts.Document,
		"pipeline_version":  "0.7",
		"output_contract":   "source_unit_extraction",
		"template_version":  promptTemplateVersion,
	})
	var proposal SourceUnitExtractionProposal
	var qa SourceUnitQA
	err := runStructuredStage(ctx, client, opts.OutDir, 2, llm.Request{
		Stage:           "source_unit_extraction",
		Model:           opts.Model,
		Instructions:    sourceUnitExtractionInstructions,
		Input:           input,
		SchemaName:      "DBDSLSourceUnitExtraction",
		Schema:          sourceUnitExtractionSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: opts.MaxOutputTokens,
		Metadata: map[string]string{
			"template_version":  promptTemplateVersion,
			"od_sentence_count": fmt.Sprintf("%d", len(opts.Document.Sentences)),
		},
	}, &proposal, func() []string {
		qa = ValidateSourceUnitProposal(proposal, opts.Document, "llm")
		return qa.Errors
	})
	if err != nil {
		return proposal, qa, err
	}
	qa = ValidateSourceUnitProposal(proposal, opts.Document, "llm")
	return proposal, qa, nil
}

func BuildSourceUnitFallback(document CombinedDocumentProposal) (SourceUnitExtractionProposal, SourceUnitQA) {
	units := make([]SourceUnitProposal, 0, len(document.Sentences))
	for i, sentence := range document.Sentences {
		kind := "requirement_sentence"
		relevance := "model_relevant"
		requiresReview := sentence.Confidence == "low" || len(sentence.Warnings) > 0
		warnings := append([]string(nil), sentence.Warnings...)
		if requiresReview && len(warnings) == 0 {
			warnings = append(warnings, "Combined-document sentence has low confidence.")
		}
		units = append(units, SourceUnitProposal{
			ID:             fmt.Sprintf("SU-%03d", i+1),
			Kind:           kind,
			Section:        "combined_document",
			Relevance:      relevance,
			Tags:           []string{"deterministic_fallback"},
			ExactText:      sentence.Text,
			NormalizedText: sentence.Text,
			ODSentenceIDs:  []string{sentence.ID},
			Confidence:     nonEmpty(sentence.Confidence, "medium"),
			RequiresReview: requiresReview,
			Warnings:       warnings,
		})
	}
	proposal := SourceUnitExtractionProposal{
		SourceUnits:       units,
		Warnings:          []string{"LLM source-unit extraction was unavailable; deterministic fallback was used."},
		ConfidenceSummary: map[string]string{"overall": "deterministic fallback"},
	}
	return proposal, ValidateSourceUnitProposal(proposal, document, "deterministic_fallback")
}

func ValidateSourceUnitProposal(proposal SourceUnitExtractionProposal, document CombinedDocumentProposal, strategy string) SourceUnitQA {
	qa := SourceUnitQA{
		DerivationStrategy: strategy,
		ODSentencesTotal:   len(document.Sentences),
		OriginChains:       map[string][]string{},
		Errors:             []string{},
		Warnings:           append([]string(nil), proposal.Warnings...),
		NeedsAttention:     []string{},
		ReviewDecisions:    []SourceUnitReviewDecision{},
	}
	sentences := map[string]CombinedDocumentSentence{}
	for _, sentence := range document.Sentences {
		sentences[sentence.ID] = sentence
	}
	seenUnits := map[string]bool{}
	covered := map[string]bool{}
	faithfullyCovered := map[string]bool{}
	for _, unit := range proposal.SourceUnits {
		if !sourceUnitIDPattern.MatchString(unit.ID) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("invalid source unit id %q", unit.ID))
		}
		if seenUnits[unit.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("duplicate source unit id %q", unit.ID))
		}
		seenUnits[unit.ID] = true
		if strings.TrimSpace(unit.ExactText) == "" || strings.TrimSpace(unit.NormalizedText) == "" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has empty exact or normalized text", unit.ID))
		}
		if len(unit.ODSentenceIDs) == 0 {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has no OD sentence reference", unit.ID))
		}
		var supporting []string
		originSet := map[string]bool{}
		for _, sentenceID := range unit.ODSentenceIDs {
			sentence, ok := sentences[sentenceID]
			if !ok {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown OD sentence %s", unit.ID, sentenceID))
				continue
			}
			covered[sentenceID] = true
			supporting = append(supporting, sentence.Text)
			if containsNormalized(unit.ExactText, sentence.Text) || containsNormalized(unit.NormalizedText, sentence.Text) {
				faithfullyCovered[sentenceID] = true
			}
			for _, origin := range sentence.DerivedFrom {
				originSet[fmt.Sprintf("%s#L%d-L%d", origin.ResourceID, origin.LineStart, origin.LineEnd)] = true
			}
		}
		if len(supporting) > 0 && !containsNormalized(strings.Join(supporting, " "), unit.ExactText) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s exact_text is not supported by referenced OD sentences", unit.ID))
		}
		for origin := range originSet {
			qa.OriginChains[unit.ID] = append(qa.OriginChains[unit.ID], origin)
		}
		sort.Strings(qa.OriginChains[unit.ID])
		if unit.Confidence == "low" && (!unit.RequiresReview || len(unit.Warnings) == 0) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s low confidence requires review and a warning", unit.ID))
		}
		if unit.RequiresReview || len(unit.Warnings) > 0 {
			qa.NeedsAttention = append(qa.NeedsAttention, unit.ID)
		}
	}
	if len(proposal.SourceUnits) == 0 {
		qa.Errors = append(qa.Errors, "source-unit extraction produced no units")
	}
	for _, sentence := range document.Sentences {
		if !covered[sentence.ID] {
			qa.UnreferencedODSentences = append(qa.UnreferencedODSentences, sentence.ID)
		} else if !faithfullyCovered[sentence.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s is referenced but its source text is not preserved by any source unit", sentence.ID))
		}
	}
	sort.Strings(qa.UnreferencedODSentences)
	if len(qa.UnreferencedODSentences) > 0 {
		qa.Errors = append(qa.Errors, "one or more combined-document sentences are not covered")
	}
	qa.ODSentencesReferenced = len(covered)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func containsNormalized(haystack, needle string) bool {
	normalize := func(value string) string {
		return strings.Join(strings.Fields(strings.ToLower(value)), " ")
	}
	return strings.Contains(normalize(haystack), normalize(needle))
}
