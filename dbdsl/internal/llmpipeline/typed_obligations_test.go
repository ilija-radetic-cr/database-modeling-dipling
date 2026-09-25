package llmpipeline

import "testing"

func TestTypedAtomsMapToObligationsWithoutKeywordNoise(t *testing.T) {
	cases := map[string]string{
		"relationship":           "relationship",
		"cardinality_constraint": "cardinality",
		"uniqueness_constraint":  "key",
		"state_transition":       "lifecycle",
		"ui_behavior":            "transient",
	}
	for atomType, want := range cases {
		atom := RequirementAtomProposal{ID: "RA-1", Statement: "Each line must have exactly one carrier and only active status.",
			AtomType: atomType, ModelingRelevance: "direct_db", ModelingOutcome: "represented", PersistenceEffect: "required", SupportLevel: "explicit"}
		got := classifyObligations(atom)
		if len(got) != 1 || got[0].kind != want {
			t.Fatalf("%s: got %+v, want single %s obligation", atomType, got, want)
		}
	}
}

func TestUntypedAtomsKeepKeywordFallback(t *testing.T) {
	atom := RequirementAtomProposal{ID: "RA-1", Statement: "The player score is stored for every attempt.",
		AtomType: "line_data", ModelingRelevance: "direct_db", ModelingOutcome: "represented", PersistenceEffect: "required", SupportLevel: "explicit"}
	if got := classifyObligations(atom); got[0].kind != "event_history" {
		t.Fatalf("legacy keyword classification changed: %+v", got)
	}
}
