package llmpipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConceptualDescriptionIssuesFollowTheContract(t *testing.T) {
	units := acceptedUnits(t)
	if issues := ConceptualDescriptionIssues(testDescription(), units); len(issues) != 0 {
		t.Fatalf("the reference description must satisfy the contract: %v", issues)
	}
	d := testDescription()
	d.Things[0].Evidence.Segments = append(d.Things[0].Evidence.Segments, "SU-004–SU-006")
	d.Excluded = append(d.Excluded, DescriptionExcluded{Segment: "SU-009", Reason: "presentation"})
	d.Things[2].IdentifiedBy = DescriptionRefs{"datum", "projekat.revizija"}
	d.Rules[0].AppliesTo = []string{"zahtev.stanje", "igra.korisnik", "igra.nepostoji"}
	d.Things[0].Links = append(d.Things[0].Links, DescriptionLink{To: "igra", PerThis: "0..N", PerOther: "1"})
	issues := strings.Join(ConceptualDescriptionIssues(d, units), "\n")
	for _, want := range []string{
		`cites "SU-004–SU-006", which is not a segment ID`,
		"segment SU-009 is both excluded and cited",
		`identified_by "projekat.revizija"`,
		`"igra.nepostoji" does not resolve`,
		"between igra and korisnik is stated from both sides",
	} {
		if !strings.Contains(issues, want) {
			t.Fatalf("missing issue %q in:\n%s", want, issues)
		}
	}
	for _, accepted := range []string{`"zahtev.stanje"`, `"igra.korisnik"`} {
		if strings.Contains(issues, accepted) {
			t.Fatalf("%s follows the reference grammar and must not be reported:\n%s", accepted, issues)
		}
	}
}

func TestSanitizeNeverTurnsAPartialReferenceIntoAKey(t *testing.T) {
	units := acceptedUnits(t)
	d := testDescription()
	d.Things[2].IdentifiedBy = DescriptionRefs{"datum", "nepostoji"}
	d.Rules[1].AppliesTo = []string{"korisnik.email", "korisnik.nepostoji"}
	d.Rules[0].AppliesTo = []string{"igra", "igra.nepostoji"}
	d.Excluded = append(d.Excluded, DescriptionExcluded{Segment: "SU-009", Reason: "presentation"}, DescriptionExcluded{Segment: "SU-001–SU-002", Reason: "other"})
	d.Things[1].States = []string{"пристигao", "прихваћен"}
	d.Things[1].Transitions = []DescriptionTransition{{From: "пристигao", To: "прихваћен"}}
	d.Queries = []DescriptionQuery{{ID: "najskoriji_polасci", Description: "Поласци.", Criteria: []string{"igra.datum"}}}
	qa := ValidateConceptualDescription(&d, units)
	if !qa.OK {
		t.Fatalf("repairable problems must not fail the stage: %v", qa.Errors)
	}
	if len(d.Things[2].IdentifiedBy) != 0 || len(d.Rules[1].AppliesTo) != 0 {
		t.Fatalf("an identity or uniqueness rule with an unresolved part must be dropped whole: %v %v", d.Things[2].IdentifiedBy, d.Rules[1].AppliesTo)
	}
	if strings.Join(d.Rules[0].AppliesTo, ",") != "igra" {
		t.Fatalf("only the unresolved reference of an ordinary rule is dropped: %v", d.Rules[0].AppliesTo)
	}
	for _, item := range d.Excluded {
		if item.Segment == "SU-009" || strings.Contains(item.Segment, "–") {
			t.Fatalf("cited or malformed segments must leave excluded: %+v", d.Excluded)
		}
	}
	// Everything is written in ASCII Serbian, which also settles words the
	// model wrote half in Cyrillic, half in Latin, the same way everywhere.
	if d.Things[1].States[0] != "pristigao" || d.Things[1].Transitions[0].From != "pristigao" || d.Queries[0].ID != "najskoriji_polasci" || d.Things[1].Name != "Zahtev za registraciju" {
		t.Fatalf("the description is not written in ASCII Serbian everywhere: %v %+v %s %q", d.Things[1].States, d.Things[1].Transitions, d.Queries[0].ID, d.Things[1].Name)
	}
}

func TestConceptualDescriptionGetsOneFeedbackRoundThenRepairs(t *testing.T) {
	units := acceptedUnits(t)
	broken := testDescription()
	broken.Excluded = append(broken.Excluded, DescriptionExcluded{Segment: "SU-001–SU-002", Reason: "other"})
	first, _ := json.Marshal(broken)
	client := &scriptedClient{responses: []string{string(first), string(first)}}
	description, qa, err := RunConceptualDescription(context.Background(), client, ConceptualDescriptionOptions{OutDir: t.TempDir(), SourceUnits: units, Model: "mock"})
	if err != nil || !qa.OK {
		t.Fatalf("a problem left after the feedback round is repaired, not fatal: err=%v qa=%+v", err, qa)
	}
	if len(client.instructions) != 2 || !strings.Contains(client.instructions[1], `excluded lists "SU-001–SU-002"`) {
		t.Fatalf("the model must get the exact issue once: %d calls", len(client.instructions))
	}
	for _, item := range description.Excluded {
		if strings.Contains(item.Segment, "–") {
			t.Fatalf("the kept description must be repaired: %+v", description.Excluded)
		}
	}
}
