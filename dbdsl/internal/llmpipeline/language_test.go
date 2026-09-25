package llmpipeline

import (
	"testing"
)

func TestDetectSourceLanguage(t *testing.T) {
	cases := map[string]string{
		"Реализовати веб систем који служи за претрагу, резервацију и куповину аутобуских карата.":                   LanguageSerbianCyrillic,
		"Napraviti sledeću mini internet aplikaciju za piceriju. Korisnici treba da imaju mogućnost unošenja imena.": LanguageSerbianLatin,
		"Napraviti sledecu aplikaciju za piceriju gde korisnik moze da se prijavi i da je sistem dostupan.":          LanguageSerbianLatin,
		"Build a web application where customers can order pizzas and administrators accept orders.":                 LanguageEnglish,
	}
	for text, want := range cases {
		if got := DetectSourceLanguage(text); got != want {
			t.Errorf("%q: got %s, want %s", text[:30], got, want)
		}
	}
}

func TestMapperTransliteratesSerbianLabels(t *testing.T) {
	model := ConceptualModelProposal{EntityConcepts: []ConceptualEntityProposal{{
		ID: "ENT-PORUDZBINA", Label: "Поруџбина", Kind: "regular", Evidence: mapperEvidence("RA-1"),
		Attributes: []ConceptualAttributeProposal{
			{ID: "ATTR-CENA", Label: "Укупна цена", Required: true, Evidence: mapperEvidence("RA-1")},
			{ID: "ATTR-DATUM", Label: "Datum porudžbine", Evidence: mapperEvidence("RA-1")},
		},
	}}}
	patch, _, err := MapConceptualToLogical(model, LogicalMappingOptions{Language: LanguageSerbianCyrillic})
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entity := patch.Operations[0].Entity
	if entity.TableName != "porudzbina" {
		t.Errorf("Serbian table names are transliterated, not pluralized: %s", entity.TableName)
	}
	if entity.Attributes[0].ID != "ukupna_cena" || entity.Attributes[0].Type != "money" {
		t.Errorf("Cyrillic attribute: %+v", entity.Attributes[0])
	}
	if entity.Attributes[1].ID != "datum_porudzbine" || entity.Attributes[1].Type != "date" {
		t.Errorf("Latin attribute: %+v", entity.Attributes[1])
	}
}
