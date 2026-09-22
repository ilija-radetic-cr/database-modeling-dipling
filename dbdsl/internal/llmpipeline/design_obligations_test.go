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

func TestDesignObligationPolicyRespectsOutcomeAndExampleRole(t *testing.T) {
	atoms := []RequirementAtomProposal{
		{ID: "RA-0001", Statement: "The button opens the login route.", ModelingRelevance: "ui_only", ModelingOutcome: "intentionally_not_in_db", SourceUnits: []string{"SU-001"}},
		{ID: "RA-0002", Statement: "Example product has color red.", ModelingRelevance: "direct_db", ModelingOutcome: "represented", SupportLevel: "example_based", ExampleRole: "illustrative_instance", SourceUnits: []string{"SU-002"}},
		{ID: "RA-0003", Statement: "Username is unique.", ModelingRelevance: "direct_db", ModelingOutcome: "represented", PersistenceEffect: "required", SourceUnits: []string{"SU-003"}},
		{ID: "RA-0004", Statement: "Password must contain an uppercase letter.", ModelingRelevance: "application_logic", ModelingOutcome: "requires_app_logic", SourceUnits: []string{"SU-004"}},
	}
	file, qa := DeriveDesignObligations(atoms)
	if !qa.OK {
		t.Fatalf("obligation QA failed: %+v", qa)
	}
	byAtom := map[string][]DesignObligation{}
	for _, item := range file.DesignObligations {
		byAtom[item.RequirementAtoms[0]] = append(byAtom[item.RequirementAtoms[0]], item)
	}
	for _, atomID := range []string{"RA-0001", "RA-0002"} {
		if byAtom[atomID][0].Persistence != "not_required" || byAtom[atomID][0].Status != "not_required" {
			t.Fatalf("%s must be excluded from persistent obligations: %+v", atomID, byAtom[atomID])
		}
	}
	if byAtom["RA-0003"][0].Persistence != "required" {
		t.Fatalf("explicit durable requirement was excluded: %+v", byAtom["RA-0003"])
	}
	if byAtom["RA-0004"][0].Kind != "security" || byAtom["RA-0004"][0].Persistence != "required" {
		t.Fatalf("password invariant must remain a required security obligation: %+v", byAtom["RA-0004"])
	}
}

func TestReclassifyDesignObligationsPreservesIDs(t *testing.T) {
	atoms := []RequirementAtomProposal{{
		ID: "RA-0001", Statement: "Illustrative JSON color is red.", ModelingRelevance: "direct_db",
		ModelingOutcome: "represented", SupportLevel: "example_based", ExampleRole: "illustrative_instance", SourceUnits: []string{"SU-001"},
	}}
	legacy := DesignObligationsFile{Document: map[string]any{"pipeline_version": "0.7"}, DesignObligations: []DesignObligation{{
		ID: "DO-0312", Statement: atoms[0].Statement, Kind: "attribute", Persistence: "required", SourceUnits: []string{"SU-001"},
		RequirementAtoms: []string{"RA-0001"}, VerificationTarget: "legacy", Risk: "medium", Status: "accepted",
	}}}
	updated, qa := ReclassifyDesignObligations(legacy, atoms)
	if !qa.OK {
		t.Fatalf("reclassified obligations failed QA: %+v", qa)
	}
	item := updated.DesignObligations[0]
	if item.ID != "DO-0312" || item.Persistence != "not_required" || item.Status != "not_required" {
		t.Fatalf("compatibility migration did not preserve identity/disposition: %+v", item)
	}
}
