package llmpipeline

import (
	"errors"
	"strings"
	"unicode"

	"dbdsl/internal/dsl"
)

const SourceTextNormalizationVersion = "source_text_normalizer_v1"

// NormalizeSourceText creates the only accepted normalized representation of
// source evidence. It deliberately performs formatting-only transformations;
// it never translates, paraphrases, or changes source tokens.
func NormalizeSourceText(exact string) (string, dsl.SourceTextNormalization) {
	current := exact
	operations := []dsl.SourceNormalizationOperation{}
	apply := func(kind, next string) {
		if next == current {
			return
		}
		operations = append(operations, dsl.SourceNormalizationOperation{Kind: kind, Before: current, After: next})
		current = next
	}

	apply("trim_boundary_whitespace", strings.TrimSpace(current))
	apply("compact_whitespace", strings.Join(strings.Fields(current), " "))
	apply("normalize_punctuation_spacing", normalizePunctuationSpacing(current))

	audit := dsl.SourceTextNormalization{
		Version:        SourceTextNormalizationVersion,
		Strategy:       "backend_deterministic",
		ExactHash:      textHash(exact),
		NormalizedHash: textHash(current),
		Changed:        exact != current,
		Operations:     operations,
	}
	return current, audit
}

// ValidateSourceTextNormalization rejects free-form paraphrases. A caller may
// submit a candidate for optimistic UI workflows, but it must exactly equal the
// backend-owned deterministic result.
func ValidateSourceTextNormalization(exact, candidate string) (dsl.SourceTextNormalization, error) {
	expected, audit := NormalizeSourceText(exact)
	if candidate != expected {
		return audit, errors.New("normalized_text must equal the deterministic backend normalization of exact_text")
	}
	return audit, nil
}

func normalizePunctuationSpacing(value string) string {
	input := []rune(value)
	out := make([]rune, 0, len(input))
	for _, current := range input {
		if isClosingPunctuation(current) && len(out) > 0 && out[len(out)-1] == ' ' {
			out = out[:len(out)-1]
		}
		if current == ' ' && len(out) > 0 && isOpeningPunctuation(out[len(out)-1]) {
			continue
		}
		out = append(out, current)
	}
	return string(out)
}

func isClosingPunctuation(r rune) bool {
	switch r {
	case '.', ',', ';', ':', '!', '?', '%', ')', ']', '}':
		return true
	default:
		return unicode.Is(unicode.Pe, r)
	}
}

func isOpeningPunctuation(r rune) bool {
	switch r {
	case '(', '[', '{':
		return true
	default:
		return unicode.Is(unicode.Ps, r)
	}
}
