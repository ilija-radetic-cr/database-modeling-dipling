package llmpipeline

import "testing"

func TestDeriveDesignObligationsRecognizesSerbianGameplaySemantics(t *testing.T) {
	atoms := []RequirementAtomProposal{{
		ID: "RA-0001", Statement: "Sistem nasumično generiše kombinaciju i mora da sačuva rezultat i broj poena.",
		ModelingRelevance: "application_logic", SourceUnits: []string{"SU-001"},
	}}
	file, qa := DeriveDesignObligations(atoms)
	if !qa.OK {
		t.Fatalf("obligation QA failed: %+v", qa)
	}
	kinds := map[string]bool{}
	for _, obligation := range file.DesignObligations {
		kinds[obligation.Kind] = true
	}
	for _, required := range []string{"generated_snapshot", "event_history", "invariant"} {
		if !kinds[required] {
			t.Fatalf("missing %s obligation: %+v", required, file.DesignObligations)
		}
	}
}

func TestConvertEntityDoesNotFabricateFallbackName(t *testing.T) {
	diagnostics := &conversionDiagnostics{}
	entity := convertEntity(EntityProposal{ID: "round", Label: "Round", Kind: "regular"}, nil, diagnostics)
	if len(entity.Attributes) != 0 {
		t.Fatalf("unexpected fabricated attributes: %+v", entity.Attributes)
	}
}
