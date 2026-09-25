package llmpipeline

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"dbdsl/internal/llm"
)

// List items derived from a list segment use a sub-ID (SU-011.3).
var sourceUnitIDPattern = regexp.MustCompile(`^SU-[0-9]{3,}(\.[0-9]+)?$`)

const (
	SourceUnitChunkSize             = 40
	defaultSourceUnitMaxParallelism = 3
)

type SourceUnitExtractionOptions struct {
	OutDir          string
	Document        CombinedDocumentProposal
	Model           string
	ReasoningEffort string
	Temperature     float64
	MaxOutputTokens int
	MaxParallelism  int
	PromptVersion   string
}

type sourceUnitClassification struct {
	ODSentenceID   string   `json:"od_sentence_id"`
	Kind           string   `json:"kind"`
	Section        string   `json:"section"`
	Relevance      string   `json:"relevance"`
	Tags           []string `json:"tags"`
	Confidence     string   `json:"confidence"`
	RequiresReview bool     `json:"requires_review"`
	Warnings       []string `json:"warnings"`
	// RequirementNotes record ambiguity in what the sentence requires. They do
	// not block source review; they travel to requirement extraction instead.
	RequirementNotes []string `json:"requirement_notes"`
}

type sourceUnitClassificationProposal struct {
	Classifications   []sourceUnitClassification `json:"classifications"`
	Warnings          []string                   `json:"warnings"`
	ConfidenceSummary map[string]string          `json:"confidence_summary"`
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
	chunks := chunkCombinedDocument(opts.Document, SourceUnitChunkSize)
	if len(chunks) == 1 {
		return runSourceUnitChunk(ctx, client, opts, chunks[0], 1, 1, sourceUnitInputBytes(opts.Document))
	}
	type chunkResult struct {
		classification sourceUnitClassificationProposal
		err            error
	}
	results := make([]chunkResult, len(chunks))
	maxParallelism := normalizedParallelism(opts.MaxParallelism, defaultSourceUnitMaxParallelism)
	semaphore := make(chan struct{}, maxParallelism)
	var wait sync.WaitGroup
	fullContextBytes := sourceUnitInputBytes(opts.Document)
	for index, chunk := range chunks {
		wait.Add(1)
		go func(index int, chunk CombinedDocumentProposal) {
			defer wait.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			classification, _, err := runSourceUnitClassificationChunk(ctx, client, opts, chunk, index+1, len(chunks), fullContextBytes)
			results[index] = chunkResult{classification: classification, err: err}
		}(index, chunk)
	}
	wait.Wait()
	merged := sourceUnitClassificationProposal{
		Classifications: []sourceUnitClassification{}, Warnings: []string{},
		ConfidenceSummary: map[string]string{"strategy": "bounded_chunks_with_deterministic_merge", "chunk_count": fmt.Sprint(len(chunks))},
	}
	for index, result := range results {
		if result.err != nil {
			err := fmt.Errorf("source-unit chunk %d/%d: %w", index+1, len(chunks), result.err)
			return SourceUnitExtractionProposal{}, SourceUnitQA{OK: false, DerivationStrategy: "llm_chunked", Errors: []string{err.Error()}}, err
		}
		merged.Classifications = append(merged.Classifications, result.classification.Classifications...)
		merged.Warnings = append(merged.Warnings, result.classification.Warnings...)
		if overall := result.classification.ConfidenceSummary["overall"]; overall != "" {
			merged.ConfidenceSummary[fmt.Sprintf("chunk_%03d", index+1)] = overall
		}
	}
	proposal := buildSourceUnitsFromClassification(opts.Document, merged)
	qa := ValidateSourceUnitProposal(proposal, opts.Document, "llm_chunked")
	qa.Errors = append(qa.Errors, validateSourceUnitClassifications(opts.Document, merged)...)
	sort.Strings(qa.Errors)
	qa.OK = len(qa.Errors) == 0
	_ = writeJSONFile(filepath.Join(opts.OutDir, "llm_runs", "source_unit_consolidation.json"), map[string]any{
		"strategy": "bounded_chunks_with_deterministic_merge", "chunk_size": SourceUnitChunkSize,
		"chunk_count": len(chunks), "max_parallelism": maxParallelism,
		"source_units": len(proposal.SourceUnits), "validation": qa,
	})
	if !qa.OK {
		return proposal, qa, fmt.Errorf("merged source units failed validation: %s", strings.Join(qa.Errors, "; "))
	}
	return proposal, qa, nil
}

func runSourceUnitChunk(ctx context.Context, client llm.Client, opts SourceUnitExtractionOptions, document CombinedDocumentProposal, chunkIndex, chunkCount, fullContextBytes int) (SourceUnitExtractionProposal, SourceUnitQA, error) {
	classification, qa, err := runSourceUnitClassificationChunk(ctx, client, opts, document, chunkIndex, chunkCount, fullContextBytes)
	if err != nil {
		return SourceUnitExtractionProposal{}, qa, err
	}
	proposal := buildSourceUnitsFromClassification(document, classification)
	qa = ValidateSourceUnitProposal(proposal, document, "llm")
	qa.Errors = append(qa.Errors, validateSourceUnitClassifications(document, classification)...)
	qa.OK = len(qa.Errors) == 0
	return proposal, qa, nil
}

func runSourceUnitClassificationChunk(ctx context.Context, client llm.Client, opts SourceUnitExtractionOptions, document CombinedDocumentProposal, chunkIndex, chunkCount, fullContextBytes int) (sourceUnitClassificationProposal, SourceUnitQA, error) {
	promptVersion := resolvedPromptVersion(opts.PromptVersion)
	segments := make([]map[string]string, 0, len(document.Sentences))
	for _, sentence := range document.Sentences {
		segments = append(segments, map[string]string{
			"id": sentence.ID, "type": nonEmpty(sentence.Role, sentence.Kind), "text": sentence.Text,
		})
	}
	input := mustJSON(map[string]any{
		"segments":         segments,
		"pipeline_version": PipelineVersion,
		"chunk_index":      chunkIndex,
		"chunk_count":      chunkCount,
		"output_contract":  "source_unit_classification",
		"template_version": promptVersion,
	})
	var classification sourceUnitClassificationProposal
	var qa SourceUnitQA
	chunkBudget := sourceUnitChunkBudget(opts.MaxOutputTokens, len(document.Sentences))
	metadata := map[string]string{
		"template_version":        promptVersion,
		"normalization_version":   SourceTextNormalizationVersion,
		"od_sentence_count":       fmt.Sprintf("%d", len(document.Sentences)),
		"chunk_index":             fmt.Sprint(chunkIndex),
		"chunk_count":             fmt.Sprint(chunkCount),
		"full_context_bytes":      fmt.Sprint(fullContextBytes),
		"context_policy":          "bounded_source_chunks_v1",
		"canonicalizer_version":   "pipeline_ids_v2",
		"call_gate_policy":        "semantic_need_v1",
		"budget_policy":           "adaptive_v1",
		"stage_max_output_tokens": fmt.Sprint(opts.MaxOutputTokens),
	}
	if chunkCount > 1 {
		metadata["run_key"] = fmt.Sprintf("chunk_%03d", chunkIndex)
	}
	err := runStructuredStage(ctx, client, opts.OutDir, 2, llm.Request{
		Stage:           "source_unit_extraction",
		Model:           opts.Model,
		Instructions:    sourceUnitExtractionInstructions,
		Input:           input,
		SchemaName:      "DBDSLSourceUnitClassification",
		Schema:          sourceUnitClassificationSchema(),
		ReasoningEffort: opts.ReasoningEffort,
		Temperature:     opts.Temperature,
		MaxOutputTokens: chunkBudget,
		Metadata:        metadata,
	}, &classification, func() []string {
		proposal := buildSourceUnitsFromClassification(document, classification)
		qa = ValidateSourceUnitProposal(proposal, document, "llm_chunk")
		qa.Errors = append(qa.Errors, validateSourceUnitClassifications(document, classification)...)
		qa.OK = len(qa.Errors) == 0
		return qa.Errors
	})
	if err != nil {
		return classification, qa, err
	}
	return classification, qa, nil
}

func chunkCombinedDocument(document CombinedDocumentProposal, maxSize int) []CombinedDocumentProposal {
	var chunks []CombinedDocumentProposal
	for start := 0; start < len(document.Sentences); start += maxSize {
		end := start + maxSize
		if end > len(document.Sentences) {
			end = len(document.Sentences)
		}
		chunks = append(chunks, CombinedDocumentProposal{Sentences: append([]CombinedDocumentSentence(nil), document.Sentences[start:end]...)})
	}
	return chunks
}

func sourceUnitChunkBudget(ceiling, sentences int) int {
	if ceiling <= 0 {
		ceiling = llm.DefaultMaxOutputTokens
	}
	budget := 3000 + 90*sentences
	if budget < 6000 {
		budget = 6000
	}
	if budget > ceiling {
		budget = ceiling
	}
	return budget
}

func sourceUnitInputBytes(document CombinedDocumentProposal) int {
	segments := make([]map[string]string, 0, len(document.Sentences))
	for _, sentence := range document.Sentences {
		segments = append(segments, map[string]string{
			"id": sentence.ID, "type": nonEmpty(sentence.Role, sentence.Kind), "text": sentence.Text,
		})
	}
	return len(mustJSON(map[string]any{"segments": segments}))
}

func validateSourceUnitClassifications(document CombinedDocumentProposal, proposal sourceUnitClassificationProposal) []string {
	known := map[string]bool{}
	for _, sentence := range document.Sentences {
		known[sentence.ID] = true
	}
	seen := map[string]bool{}
	var issues []string
	for _, item := range proposal.Classifications {
		if !known[item.ODSentenceID] {
			issues = append(issues, "classification references unknown OD sentence "+item.ODSentenceID)
		}
		if seen[item.ODSentenceID] {
			issues = append(issues, "duplicate classification for OD sentence "+item.ODSentenceID)
		}
		seen[item.ODSentenceID] = true
	}
	for id := range known {
		if !seen[id] {
			issues = append(issues, "missing classification for OD sentence "+id)
		}
	}
	sort.Strings(issues)
	return issues
}

func buildSourceUnitsFromClassification(document CombinedDocumentProposal, proposal sourceUnitClassificationProposal) SourceUnitExtractionProposal {
	bySentence := map[string]sourceUnitClassification{}
	for _, item := range proposal.Classifications {
		bySentence[item.ODSentenceID] = item
	}
	units := make([]SourceUnitProposal, 0, len(document.Sentences))
	for i, sentence := range document.Sentences {
		item := bySentence[sentence.ID]
		normalized, normalization := NormalizeSourceText(sentence.Text)
		confidence := nonEmpty(item.Confidence, sentence.Confidence)
		if confidence == "" {
			confidence = "medium"
		}
		warnings := append([]string(nil), item.Warnings...)
		requiresReview := item.RequiresReview || confidence == "low" || item.ODSentenceID == ""
		if item.ODSentenceID == "" {
			warnings = append(warnings, "LLM omitted classification for this source sentence.")
		}
		units = append(units, SourceUnitProposal{
			ID: fmt.Sprintf("SU-%03d", i+1), Kind: nonEmpty(item.Kind, "noise"), Section: nonEmpty(item.Section, "combined_document"),
			Relevance: nonEmpty(item.Relevance, "non_model"), Tags: item.Tags, ExactText: sentence.Text, NormalizedText: normalized, Normalization: normalization,
			ODSentenceIDs: []string{sentence.ID}, Confidence: confidence, RequiresReview: requiresReview, Warnings: warnings,
			RequirementNotes: item.RequirementNotes,
		})
	}
	return SourceUnitExtractionProposal{SourceUnits: units, Warnings: proposal.Warnings, ConfidenceSummary: proposal.ConfidenceSummary}
}

func BuildSourceUnitFallback(document CombinedDocumentProposal) (SourceUnitExtractionProposal, SourceUnitQA) {
	units := make([]SourceUnitProposal, 0, len(document.Sentences))
	for i, sentence := range document.Sentences {
		normalized, normalization := NormalizeSourceText(sentence.Text)
		kind := "requirement_sentence"
		relevance := "model_relevant"
		if CombinedDocumentUnitKind(sentence) == CombinedDocumentUnitStructural {
			kind = "heading"
			relevance = "model_supporting"
		}
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
			NormalizedText: normalized,
			Normalization:  normalization,
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
		OriginChains:       map[string][]string{},
		Errors:             []string{},
		Warnings:           append([]string(nil), proposal.Warnings...),
		NeedsAttention:     []string{},
		ReviewDecisions:    []SourceUnitReviewDecision{},
	}
	segmentContract := false
	for _, unit := range proposal.SourceUnits {
		if len(unit.SegmentIDs) > 0 {
			segmentContract = true
			break
		}
	}
	if segmentContract {
		qa.SegmentsTotal = len(document.Sentences)
	} else {
		qa.ODSentencesTotal = len(document.Sentences)
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
		if strategy == SegmentUnitStrategy {
			// Segment text is stored verbatim; there is no backend normalization.
			if unit.NormalizedText != unit.ExactText {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s text must be the segment text unchanged", unit.ID))
			}
		} else {
			expectedNormalized, expectedAudit := NormalizeSourceText(unit.ExactText)
			if unit.NormalizedText != expectedNormalized {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s normalized_text was not produced by the deterministic backend normalizer", unit.ID))
			}
			if !reflect.DeepEqual(unit.Normalization, expectedAudit) {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s normalization audit does not match exact_text", unit.ID))
			}
		}
		segmentIDs := sourceUnitSegmentIDs(unit)
		if len(segmentIDs) == 0 {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has no source-segment reference", unit.ID))
		}
		var supporting []string
		originSet := map[string]bool{}
		for _, sentenceID := range segmentIDs {
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
		// Only source problems stop the pipeline here. Informational warnings and
		// requirement ambiguity are resolved later as modeling decisions.
		if unit.RequiresReview || unit.Confidence == "low" {
			qa.NeedsAttention = append(qa.NeedsAttention, unit.ID)
		}
	}
	if len(proposal.SourceUnits) == 0 {
		qa.Errors = append(qa.Errors, "source-unit extraction produced no units")
	}
	for _, sentence := range document.Sentences {
		if !covered[sentence.ID] {
			if segmentContract {
				qa.UnreferencedSegments = append(qa.UnreferencedSegments, sentence.ID)
			} else {
				qa.UnreferencedODSentences = append(qa.UnreferencedODSentences, sentence.ID)
			}
		} else if !faithfullyCovered[sentence.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s is referenced but its source text is not preserved by any source unit", sentence.ID))
		}
	}
	sort.Strings(qa.UnreferencedSegments)
	sort.Strings(qa.UnreferencedODSentences)
	if len(qa.UnreferencedSegments)+len(qa.UnreferencedODSentences) > 0 {
		qa.Errors = append(qa.Errors, "one or more combined-document sentences are not covered")
	}
	if segmentContract {
		qa.SegmentsReferenced = len(covered)
	} else {
		qa.ODSentencesReferenced = len(covered)
	}
	qa.OK = len(qa.Errors) == 0
	return qa
}

func sourceUnitSegmentIDs(unit SourceUnitProposal) []string {
	if len(unit.SegmentIDs) > 0 {
		return unit.SegmentIDs
	}
	return unit.ODSentenceIDs
}

func containsNormalized(haystack, needle string) bool {
	normalize := func(value string) string {
		return strings.Join(strings.Fields(strings.ToLower(value)), " ")
	}
	return strings.Contains(normalize(haystack), normalize(needle))
}
