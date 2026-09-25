package llmpipeline

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
)

// List items derived from a list segment use a sub-ID (SU-011.3).
var sourceUnitIDPattern = regexp.MustCompile(`^SU-[0-9]{3,}(\.[0-9]+)?$`)

const (
	SourceUnitChunkSize             = 40
	defaultSourceUnitMaxParallelism = 3
)

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
