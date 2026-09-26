package llmpipeline

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// A thing whose states no property holds gets a state attribute in the
// conceptual model, so its lifecycle never names a field the entity lacks, and
// "thing.stanje" in a rule refers to that attribute.
func TestLifecycleWithoutStatePropertyGetsStateAttribute(t *testing.T) {
	units := acceptedUnits(t)
	description := testDescription()
	description.Rules = append(description.Rules, DescriptionRule{
		ID: "odluka", Kind: "approval", Statement: "Администратор одлучује о захтеву.",
		AppliesTo: []string{"zahtev.stanje"}, Evidence: DescriptionEvidence{Segments: []string{"SU-007"}, Mode: "direct"},
	})
	model := ConceptualDescriptionToModel(description, units)
	var lifecycle PlanElementProposal
	for _, item := range model.LifecycleConcepts {
		if item.Owner == "ENT-ZAHTEV" {
			lifecycle = item
		}
	}
	var state *ConceptualAttributeProposal
	for _, entity := range model.EntityConcepts {
		for i, attribute := range entity.Attributes {
			if entity.ID == "ENT-ZAHTEV" && attribute.Name == lifecycle.Field {
				state = &entity.Attributes[i]
			}
		}
	}
	if lifecycle.Field != "stanje" || state == nil || !reflect.DeepEqual(state.EnumValues, description.Things[1].States) || !state.Required {
		t.Fatalf("lifecycle field %q has no matching state attribute: %+v", lifecycle.Field, state)
	}
	for _, constraint := range model.ConstraintConcepts {
		if constraint.ID == "CON-ODLUKA" && !reflect.DeepEqual(constraint.Targets, []string{state.ID}) {
			t.Fatalf("rule on zahtev.stanje does not target the state attribute: %+v", constraint.Targets)
		}
	}
	if qa := ValidateConceptualModel(model, units, nil); !qa.OK {
		t.Fatalf("derived model with a state attribute is invalid: %v", qa.Errors)
	}
	model.LifecycleConcepts[0].Field = "nepostoji"
	if qa := ValidateConceptualModel(model, units, nil); qa.OK || !strings.Contains(strings.Join(qa.Errors, " "), `names field "nepostoji"`) {
		t.Fatalf("a lifecycle naming a missing field must be a derivation error: %+v", qa)
	}
}

// Two states held by a yes/no flag stay a boolean column instead of becoming a
// string enum of "true" and "false".
func TestLifecycleOnBooleanFlagKeepsBooleanColumn(t *testing.T) {
	units := acceptedUnits(t)
	description := testDescription()
	description.Things[1].Properties = append(description.Things[1].Properties, DescriptionProperty{
		Name: "placen", ValueType: "boolean", Shape: "single", Presence: "required", Origin: "entered",
		AllowedValues: []string{"false", "true"}, Evidence: DescriptionEvidence{Segments: []string{"SU-007"}, Mode: "direct"},
	})
	description.Things[1].States = []string{"false", "true"}
	description.Things[1].Transitions = []DescriptionTransition{{From: "false", To: "true", Trigger: "уплата", Evidence: DescriptionEvidence{Segments: []string{"SU-007"}, Mode: "direct"}}}
	model := ConceptualDescriptionToModel(description, units)
	patch, _, err := MapConceptualToLogical(model, LogicalMappingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range patch.Operations {
		if op.StateMachine != nil && op.StateMachine.Owner == "ENT-ZAHTEV" {
			t.Fatalf("a boolean flag became a state machine: %+v", op.StateMachine)
		}
		if op.Entity == nil || op.Entity.ID != "ENT-ZAHTEV" {
			continue
		}
		for _, attribute := range op.Entity.Attributes {
			if attribute.ID == "placen" && (attribute.Type != "boolean" || len(attribute.EnumValues) != 0 || len(attribute.Notes) == 0) {
				t.Fatalf("boolean lifecycle field was retyped or lost its transitions: %+v", attribute)
			}
		}
	}
}

// Description references use the description's own grammar; rules, queries
// and other elements carry a prefix, so a rule may share its ID with a thing.
func TestDescriptionReferencesCoverEveryCitableElement(t *testing.T) {
	description := testDescription()
	description.Rules = append(description.Rules, DescriptionRule{ID: "igra", Kind: "quantity", Statement: "Правило са ID-јем ствари.", AppliesTo: []string{"igra"}})
	refs := descriptionReferences(description)
	for ref, root := range map[string]string{
		"korisnik":                thingRoot("korisnik"),
		"korisnik.email":          propertyRoot("korisnik", "email"),
		"zahtev.korisnik":         linkRoot("zahtev", "korisnik"),
		"zahtev.stanje":           lifecycleRoot("zahtev"),
		"rule:igra":               "rule:igra",
		"igra":                    thingRoot("igra"),
		"actor:korisnik":          "actor:korisnik",
		"excluded:SU-010":         "excluded:SU-010",
		"rule:jedna_igra":         "rule:jedna_igra",
		"korisnik.aktivan":        propertyRoot("korisnik", "aktivan"),
		"igra.korisnik":           linkRoot("igra", "korisnik"),
		"korisnik.ime":            propertyRoot("korisnik", "ime"),
		"zahtev.vreme_podnosenja": propertyRoot("zahtev", "vreme_podnosenja"),
	} {
		if !containsString(refs[ref], root) {
			t.Errorf("reference %q does not authorize %q: %v", ref, root, refs[ref])
		}
	}
	if _, ok := refs["korisnik.stanje"]; ok {
		t.Error("a thing without states has no lifecycle reference")
	}
}

// A finding about one link of a thing lets the correction change that link,
// not the thing's properties.
func TestCorrectionScopedToLinkRejectsPropertyChange(t *testing.T) {
	units := acceptedUnits(t)
	current := testDescription()
	finding := AdversarialFinding{
		ID: "AR-LINK", Severity: "error", Category: "cardinality", SourceUnitIDs: []string{"SU-009"}, DescriptionRefs: []string{"igra.korisnik"},
		SourceQuote: "игра", Claim: "Wrong count.", Expected: "Each user plays many games.", Actual: "Other count.", SuggestedCorrection: "Fix the count.",
	}
	run := func(mutate func(*ConceptualDescription)) error {
		corrected := cloneDescription(t, current)
		corrected.Things[2].Links[0].PerOther = "1..N"
		mutate(&corrected)
		payload := patchPayload(t, current, corrected)
		_, _, err := RunConceptualCorrection(context.Background(), &reviewScriptClient{responses: []json.RawMessage{payload}}, ConceptualCorrectionOptions{
			OutDir: t.TempDir(), SourceUnits: units, Current: current, Findings: []AdversarialFinding{finding},
			Decisions: []ConceptualCorrectionDecision{{FindingID: finding.ID, Decision: "apply", Note: "Fix the count of this link only."}},
		})
		return err
	}
	if err := run(func(*ConceptualDescription) {}); err != nil {
		t.Fatalf("the cited link alone could not be corrected: %v", err)
	}
	if err := run(func(value *ConceptualDescription) { value.Things[2].Properties[0].Presence = "optional" }); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("a property changed under a link-scoped finding: %v", err)
	}
}

// patchPayload is what a correcting model returns to turn current into
// corrected: every element that differs or is new, whole, and the removed ones.
func patchPayload(t *testing.T, current, corrected ConceptualDescription) json.RawMessage {
	t.Helper()
	patch := DescriptionPatch{Remove: []string{}}
	var removed []string
	patch.Upsert.Actors, removed = diffByID(current.Actors, corrected.Actors, func(v DescriptionActor) string { return v.ID }, "actor:")
	patch.Remove = append(patch.Remove, removed...)
	patch.Upsert.Things, removed = diffByID(current.Things, corrected.Things, func(v DescriptionThing) string { return v.ID }, "")
	patch.Remove = append(patch.Remove, removed...)
	patch.Upsert.Rules, removed = diffByID(current.Rules, corrected.Rules, func(v DescriptionRule) string { return v.ID }, "rule:")
	patch.Remove = append(patch.Remove, removed...)
	patch.Upsert.Queries, removed = diffByID(current.Queries, corrected.Queries, func(v DescriptionQuery) string { return v.ID }, "query:")
	patch.Remove = append(patch.Remove, removed...)
	patch.Upsert.Imports, removed = diffByID(current.Imports, corrected.Imports, func(v DescriptionImport) string { return v.ID }, "import:")
	patch.Remove = append(patch.Remove, removed...)
	patch.Upsert.Excluded, removed = diffByID(current.Excluded, corrected.Excluded, func(v DescriptionExcluded) string { return v.Segment }, "excluded:")
	patch.Remove = append(patch.Remove, removed...)
	patch.Upsert.OpenQuestions, removed = diffByID(current.OpenQuestions, corrected.OpenQuestions, func(v DescriptionQuestion) string { return v.ID }, "question:")
	patch.Remove = append(patch.Remove, removed...)
	payload, err := json.Marshal(patch)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func diffByID[T any](before, after []T, id func(T) string, prefix string) ([]T, []string) {
	old := map[string]T{}
	for _, item := range before {
		old[id(item)] = item
	}
	changed, kept := []T{}, map[string]bool{}
	for _, item := range after {
		kept[id(item)] = true
		if previous, ok := old[id(item)]; !ok || !reflect.DeepEqual(previous, item) {
			changed = append(changed, item)
		}
	}
	removed := []string{}
	for _, item := range before {
		if !kept[id(item)] {
			removed = append(removed, prefix+id(item))
		}
	}
	return changed, removed
}

// A patch replaces elements in place, appends new ones and removes the named
// ones; everything it does not name is left exactly as it was.
func TestApplyDescriptionPatch(t *testing.T) {
	current := testDescription()
	rule := current.Rules[1]
	rule.Statement = "Е-пошта је јединствена у систему."
	patch := DescriptionPatch{
		Upsert: DescriptionPatchUpsert{
			Rules:   []DescriptionRule{rule},
			Queries: []DescriptionQuery{{ID: "nov_upit", Description: "Нов упит.", Needs: []string{"igra"}}},
		},
		Remove: []string{"rule:jedna_igra", "excluded:SU-010"},
	}
	merged, errs := applyDescriptionPatch(current, patch)
	if len(errs) != 0 {
		t.Fatalf("valid patch rejected: %v", errs)
	}
	if len(merged.Rules) != 1 || merged.Rules[0].Statement != rule.Statement || len(merged.Queries) != len(current.Queries)+1 || len(merged.Excluded) != 0 {
		t.Fatalf("patch not applied: rules=%+v queries=%d excluded=%+v", merged.Rules, len(merged.Queries), merged.Excluded)
	}
	if !reflect.DeepEqual(merged.Things, current.Things) || !reflect.DeepEqual(merged.Actors, current.Actors) || current.Rules[1].Statement == rule.Statement {
		t.Fatal("the patch changed elements it did not name, or the current description")
	}
	_, errs = applyDescriptionPatch(current, DescriptionPatch{
		Upsert: DescriptionPatchUpsert{Rules: []DescriptionRule{rule, rule}},
		Remove: []string{"rule:jedinstven_email", "question:nepostoji"},
	})
	joined := strings.Join(errs, " | ")
	for _, want := range []string{`names "rule:jedinstven_email" twice`, `both replaces and removes "rule:jedinstven_email"`, `removes "question:nepostoji"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("patch errors %q do not mention %q", joined, want)
		}
	}
}

// The correction asks for a patch, not for the whole description again.
func TestConceptualCorrectionRequestsPatch(t *testing.T) {
	units := acceptedUnits(t)
	current := testDescription()
	corrected := cloneDescription(t, current)
	corrected.Things[0].Properties[2].Presence = "required"
	finding := AdversarialFinding{
		ID: "AR-EMAIL", Severity: "warning", Category: "contradiction", SourceUnitIDs: []string{"SU-003"}, DescriptionRefs: []string{"korisnik.email"},
		SourceQuote: "нови корисник унесе", Claim: "The email is required.", Expected: "Required email.", Actual: "Optional email.", SuggestedCorrection: "Make it required.",
	}
	client := &reviewScriptClient{responses: []json.RawMessage{patchPayload(t, current, corrected)}}
	result, qa, err := RunConceptualCorrection(context.Background(), client, ConceptualCorrectionOptions{
		OutDir: t.TempDir(), SourceUnits: units, Current: current, Findings: []AdversarialFinding{finding},
		Decisions: []ConceptualCorrectionDecision{{FindingID: finding.ID, Decision: "apply", Note: "Make the email required only."}},
	})
	if err != nil || !qa.OK || result.Things[0].Properties[2].Presence != "required" || len(result.Rules) != len(current.Rules) {
		t.Fatalf("patched correction failed: qa=%+v err=%v", qa, err)
	}
	req := client.requests[0]
	properties := req.Schema["properties"].(map[string]any)
	if _, ok := properties["upsert"]; !ok || req.SchemaName != "DBDSLConceptualDescriptionPatch" || !strings.Contains(req.Instructions, "Vrati samo izmene, ne ceo opis") {
		t.Fatalf("correction does not request a patch: %s %v", req.SchemaName, properties)
	}
}

// The pipeline writes Serbian in ASCII Latin ("ošišana latinica") whatever
// script the task uses; only the critic's quote keeps the task's own text.
func TestSerbianIsWrittenInASCIILatin(t *testing.T) {
	units := acceptedUnits(t)
	description := testDescription()
	description.Things[1].Properties[0].Meaning = "Време подношења; čuva se đačko ime"
	if qa := ValidateConceptualDescription(&description, units); !qa.OK {
		t.Fatalf("description rejected: %v", qa.Errors)
	}
	raw, _ := json.Marshal(description)
	for _, r := range string(raw) {
		if r > 127 && r != '–' && r != '„' && r != '“' {
			t.Fatalf("non-ASCII letter %q left in the description: %s", r, raw)
		}
	}
	if description.Things[1].Properties[0].Meaning != "Vreme podnosenja; cuva se djacko ime" {
		t.Fatalf("unexpected transliteration: %q", description.Things[1].Properties[0].Meaning)
	}
	proposal := AdversarialReviewProposal{Summary: "Један налаз.", Findings: []AdversarialFinding{{
		Claim: "Недостаје", Expected: "Треба", Actual: "Нема", SuggestedCorrection: "Додати", SourceQuote: "нови корисник унесе",
	}}}
	writeFindingsASCII(&proposal)
	finding := proposal.Findings[0]
	if proposal.Summary != "Jedan nalaz." || finding.Claim != "Nedostaje" || finding.SuggestedCorrection != "Dodati" || finding.SourceQuote != "нови корисник унесе" {
		t.Fatalf("findings are not ASCII Serbian, or the quote changed: %+v", proposal)
	}
}
