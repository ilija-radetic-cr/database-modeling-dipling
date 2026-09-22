package llmpipeline

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type combinedSourceBlock struct {
	lines      []CombinedDocumentLine
	structural bool
}

type combinedTextUnit struct {
	text       string
	start      int
	end        int
	structural bool
}

type combinedLineRange struct {
	number int
	start  int
	end    int
}

// BuildLosslessCombinedDocument keeps physical source lines as the fidelity
// layer and deterministically projects them into sentence or structural OD
// units. The two layers intentionally have different granularities.
func BuildLosslessCombinedDocument(resources []CombinedDocumentResource) ([]SourceSegment, CombinedDocumentProposal, SourceFidelityReport) {
	ordered := append([]CombinedDocumentResource(nil), resources...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	segments := []SourceSegment{}
	sentences := []CombinedDocumentSentence{}
	segmentIndex := map[string]int{}
	normative := 0
	for _, resource := range ordered {
		lines := append([]CombinedDocumentLine(nil), resource.Lines...)
		sort.SliceStable(lines, func(i, j int) bool { return lines[i].Number < lines[j].Number })
		segmentNo := 0
		authority := strings.TrimSpace(resource.Authority)
		if authority == "" {
			authority = "normative"
		}
		for _, line := range lines {
			text := strings.TrimSpace(line.Text)
			if text == "" {
				continue
			}
			segmentNo++
			segmentID := fmt.Sprintf("%s-S-%04d", resource.ID, segmentNo)
			segmentIndex[resourceLineKey(resource.ID, line.Number)] = len(segments)
			segments = append(segments, SourceSegment{
				ID: segmentID, ResourceID: resource.ID, LineStart: line.Number,
				LineEnd: line.Number, Text: text, Authority: authority,
			})
			if authority == "normative" {
				normative++
			}
		}

		for _, block := range combinedSourceBlocks(resource.FileType, lines) {
			joined, ranges := joinCombinedBlock(block.lines)
			units := splitCombinedText(joined, block.structural)
			for _, unit := range units {
				lineStart, lineEnd := unitLineSpan(unit, ranges)
				if lineStart == 0 || lineEnd == 0 {
					continue
				}
				kind := CombinedDocumentUnitSentence
				if unit.structural {
					kind = CombinedDocumentUnitStructural
				}
				transformation := "copied"
				if lineStart != lineEnd {
					transformation = "merged"
				}
				odID := fmt.Sprintf("OD-S-%04d", len(sentences)+1)
				sentences = append(sentences, CombinedDocumentSentence{
					ID: odID, Kind: kind, Text: unit.text,
					DerivedFrom: []CombinedDocumentOrigin{{
						ResourceID: resource.ID, LineStart: lineStart, LineEnd: lineEnd, ExactText: unit.text,
					}},
					Transformation: transformation, Confidence: "high", Warnings: []string{},
				})
			}
		}
	}

	odIDsBySegment := make([][]string, len(segments))
	for _, sentence := range sentences {
		for _, origin := range sentence.DerivedFrom {
			for line := origin.LineStart; line <= origin.LineEnd; line++ {
				if index, ok := segmentIndex[resourceLineKey(origin.ResourceID, line)]; ok {
					odIDsBySegment[index] = appendUniqueString(odIDsBySegment[index], sentence.ID)
				}
			}
		}
	}
	dispositions := make([]SourceSegmentDisposition, 0, len(segments))
	uncovered := []string{}
	normativeCovered := 0
	counts := map[string]int{}
	for index, segment := range segments {
		status := "retained"
		if len(odIDsBySegment[index]) == 0 {
			status = "uncovered"
			uncovered = append(uncovered, segment.ID)
		} else if segment.Authority == "normative" {
			normativeCovered++
		}
		counts[status]++
		dispositions = append(dispositions, SourceSegmentDisposition{
			SegmentID: segment.ID, Status: status, ODIDs: odIDsBySegment[index],
		})
	}
	coverage := 1.0
	if normative > 0 {
		coverage = float64(normativeCovered) / float64(normative)
	}
	report := SourceFidelityReport{
		OK: len(segments) > 0 && normativeCovered == normative, PipelineVersion: PipelineVersion, SegmentsTotal: len(segments),
		NormativeSegments: normative, NormativeCovered: normativeCovered, NormativeCoverage: coverage,
		DispositionCounts:   counts,
		UncoveredSegmentIDs: uncovered, NeedsAttention: []string{}, Dispositions: dispositions,
		Errors: []string{}, Warnings: []string{},
	}
	proposal := CombinedDocumentProposal{
		Sentences: sentences, Warnings: []string{},
		ConfidenceSummary: map[string]string{"overall": "deterministic sentence segmentation with lossless source-line provenance"},
	}
	if len(segments) == 0 {
		report.Errors = append(report.Errors, "no non-empty source segments were produced")
	}
	if normativeCovered != normative {
		report.Errors = append(report.Errors, "one or more normative source segments are not covered by OD units")
	}
	return segments, proposal, report
}

func resourceLineKey(resourceID string, line int) string {
	return fmt.Sprintf("%s\x00%d", resourceID, line)
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func combinedSourceBlocks(fileType string, lines []CombinedDocumentLine) []combinedSourceBlock {
	structuredFormat := fileType == "json" || fileType == "csv" || fileType == "xml"
	var blocks []combinedSourceBlock
	var paragraph []CombinedDocumentLine
	flush := func() {
		if len(paragraph) > 0 {
			blocks = append(blocks, combinedSourceBlock{lines: paragraph})
			paragraph = nil
		}
	}
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			flush()
			continue
		}
		if structuredFormat || isHeadingLine(text) {
			flush()
			blocks = append(blocks, combinedSourceBlock{lines: []CombinedDocumentLine{line}, structural: true})
			continue
		}
		if isListLine(text) {
			flush()
			blocks = append(blocks, combinedSourceBlock{lines: []CombinedDocumentLine{line}})
			continue
		}
		paragraph = append(paragraph, line)
	}
	flush()
	return blocks
}

func isHeadingLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "#") {
		return true
	}
	if len(trimmed) >= 3 && strings.Trim(trimmed, "-_*= ") == "" {
		return true
	}
	return false
}

func isListLine(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, prefix := range []string{"- ", "* ", "+ ", "• "} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	runes := []rune(trimmed)
	i := 0
	for i < len(runes) && unicode.IsDigit(runes[i]) {
		i++
	}
	return i > 0 && i+1 < len(runes) && (runes[i] == '.' || runes[i] == ')') && unicode.IsSpace(runes[i+1])
}

func joinCombinedBlock(lines []CombinedDocumentLine) (string, []combinedLineRange) {
	var b strings.Builder
	ranges := make([]combinedLineRange, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		start := b.Len()
		b.WriteString(text)
		ranges = append(ranges, combinedLineRange{number: line.Number, start: start, end: b.Len()})
	}
	return b.String(), ranges
}

func splitCombinedText(text string, forceStructural bool) []combinedTextUnit {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if forceStructural {
		return []combinedTextUnit{{text: text, start: 0, end: len(text), structural: true}}
	}
	var units []combinedTextUnit
	start := 0
	for index, current := range text {
		if current != '.' && current != '?' && current != '!' {
			continue
		}
		if current == '.' && protectedSentencePeriod(text, index) {
			continue
		}
		end := index + utf8.RuneLen(current)
		for end < len(text) {
			r, size := utf8.DecodeRuneInString(text[end:])
			if r == '.' || r == '?' || r == '!' || isClosingSentenceRune(r) {
				end += size
				continue
			}
			break
		}
		if unit, ok := trimmedCombinedUnit(text, start, end, false); ok {
			units = append(units, unit)
		}
		start = end
	}
	if unit, ok := trimmedCombinedUnit(text, start, len(text), true); ok {
		units = append(units, unit)
	}
	return units
}

func trimmedCombinedUnit(text string, start, end int, structural bool) (combinedTextUnit, bool) {
	for start < end {
		r, size := utf8.DecodeRuneInString(text[start:end])
		if !unicode.IsSpace(r) {
			break
		}
		start += size
	}
	for end > start {
		r, size := utf8.DecodeLastRuneInString(text[start:end])
		if !unicode.IsSpace(r) {
			break
		}
		end -= size
	}
	if start >= end {
		return combinedTextUnit{}, false
	}
	return combinedTextUnit{text: text[start:end], start: start, end: end, structural: structural}, true
}

func protectedSentencePeriod(text string, index int) bool {
	previous, _ := utf8.DecodeLastRuneInString(text[:index])
	immediateNext, _ := utf8.DecodeRuneInString(text[index+1:])
	next, _ := nextNonSpaceRune(text, index+1)
	if unicode.IsDigit(previous) && (unicode.IsDigit(immediateNext) || unicode.IsLetter(immediateNext)) {
		return true
	}
	token := strings.ToLower(periodToken(text, index))
	if token == "" {
		return false
	}
	if strings.Count(token, ".") > 0 && containsLetter(token) && next != utf8.RuneError {
		return true
	}
	if titleAbbreviations[token] && next != utf8.RuneError {
		return true
	}
	if sentenceAbbreviations[token] && unicode.IsLower(next) {
		return true
	}
	return len([]rune(token)) == 1 && unicode.IsLetter(previous) && next != utf8.RuneError
}

func containsLetter(value string) bool {
	for _, r := range value {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

var titleAbbreviations = map[string]bool{
	"dr": true, "mr": true, "mrs": true, "ms": true, "prof": true, "doc": true,
}

var sentenceAbbreviations = map[string]bool{
	"npr": true, "tj": true, "tzv": true, "itd": true, "itp": true, "sl": true,
	"br": true, "str": true, "god": true, "etc": true, "e.g": true, "i.e": true,
}

func periodToken(text string, index int) string {
	start := index
	for start > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:start])
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' {
			break
		}
		start -= size
	}
	return strings.Trim(text[start:index], ".")
}

func nextNonSpaceRune(text string, start int) (rune, int) {
	for start < len(text) {
		r, size := utf8.DecodeRuneInString(text[start:])
		if !unicode.IsSpace(r) {
			return r, start
		}
		start += size
	}
	return utf8.RuneError, len(text)
}

func isClosingSentenceRune(r rune) bool {
	switch r {
	case '\'', '"', '’', '”', '»', ')', ']', '}':
		return true
	default:
		return false
	}
}

func unitLineSpan(unit combinedTextUnit, ranges []combinedLineRange) (int, int) {
	start, end := 0, 0
	lastOffset := unit.end - 1
	for _, line := range ranges {
		if start == 0 && unit.start < line.end && unit.end > line.start {
			start = line.number
		}
		if lastOffset >= line.start && unit.start < line.end {
			end = line.number
		}
	}
	if end == 0 {
		end = start
	}
	return start, end
}
