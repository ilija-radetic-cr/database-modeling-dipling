package llmpipeline

import (
	"strings"
	"unicode"
)

// Output languages the pipeline writes human-readable LLM text in. IDs, enum
// values, JSON keys and column names stay ASCII regardless of the language.
const (
	LanguageSerbianCyrillic = "sr-Cyrl"
	LanguageSerbianLatin    = "sr-Latn"
	LanguageEnglish         = "en"
)

// DetectSourceLanguage classifies task text by script and by frequent Serbian
// function words. It only needs to separate the three supported outputs.
func DetectSourceLanguage(text string) string {
	cyrillic, latin, serbianLatin := 0, 0, 0
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Cyrillic, r):
			cyrillic++
		case strings.ContainsRune("čćšžđČĆŠŽĐ", r):
			serbianLatin++
			latin++
		case unicode.IsLetter(r) && r < unicode.MaxLatin1:
			latin++
		}
	}
	if cyrillic > 0 && cyrillic*2 >= latin {
		return LanguageSerbianCyrillic
	}
	if latin == 0 {
		return LanguageEnglish
	}
	words := strings.Fields(strings.ToLower(text))
	serbianWords := 0
	for _, word := range words {
		switch strings.Trim(word, ".,;:()\"'!?") {
		case "je", "i", "se", "da", "za", "na", "od", "koji", "koja", "koje", "treba", "može", "moze", "korisnik", "sistem", "sistema", "ili", "su", "u":
			serbianWords++
		}
	}
	if serbianLatin > 0 || (len(words) > 0 && serbianWords*100/len(words) >= 8) {
		return LanguageSerbianLatin
	}
	return LanguageEnglish
}

type outputLanguageKey struct{}
