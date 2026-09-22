package llmpipeline

import "testing"

func TestNormalizeSourceTextAppliesOnlyAuditedFormattingChanges(t *testing.T) {
	exact := "  Proizvod   mora da ima naziv .  "
	normalized, audit := NormalizeSourceText(exact)
	if normalized != "Proizvod mora da ima naziv." {
		t.Fatalf("normalized text = %q", normalized)
	}
	if audit.Version != SourceTextNormalizationVersion || audit.Strategy != "backend_deterministic" || !audit.Changed {
		t.Fatalf("unexpected normalization audit: %+v", audit)
	}
	if audit.ExactHash == "" || audit.NormalizedHash == "" || audit.ExactHash == audit.NormalizedHash {
		t.Fatalf("normalization hashes do not describe the change: %+v", audit)
	}
	wantKinds := []string{"trim_boundary_whitespace", "compact_whitespace", "normalize_punctuation_spacing"}
	if len(audit.Operations) != len(wantKinds) {
		t.Fatalf("operations = %+v", audit.Operations)
	}
	for i, want := range wantKinds {
		if audit.Operations[i].Kind != want {
			t.Fatalf("operation %d = %q, want %q", i, audit.Operations[i].Kind, want)
		}
	}
}

func TestNormalizeSourceTextPreservesSerbianMeaningBearingTokens(t *testing.T) {
	exact := "Korisnik ne sme imati više od 2 naloga."
	normalized, audit := NormalizeSourceText(exact)
	if normalized != exact || audit.Changed || len(audit.Operations) != 0 {
		t.Fatalf("meaning-bearing text changed: normalized=%q audit=%+v", normalized, audit)
	}
}

func TestValidateSourceTextNormalizationRejectsParaphraseAndTranslation(t *testing.T) {
	exact := "Proizvod mora imati naziv."
	for _, candidate := range []string{
		"Svaki proizvod mora imati naziv.",
		"Product must have a name.",
		"Proizvod može imati naziv.",
	} {
		if _, err := ValidateSourceTextNormalization(exact, candidate); err == nil {
			t.Fatalf("unsafe candidate %q was accepted", candidate)
		}
	}
}
