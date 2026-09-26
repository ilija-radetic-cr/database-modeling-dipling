package llmpipeline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A finding that cites one property authorizes that property only; the other
// properties of the same thing stay protected even when they cite the same
// source unit.
func TestConceptualCorrectionScopedToAttributeRejectsSiblingAttributeChange(t *testing.T) {
	units := acceptedUnits(t)
	current := testDescription()

	finding := AdversarialFinding{
		ID: "AR-EMAIL", Severity: "warning", Category: "contradiction", SourceUnitIDs: []string{"SU-003"}, DescriptionRefs: []string{"korisnik.email"},
		SourceQuote: "нови корисник унесе", Claim: "The email is required.", Expected: "Required email.", Actual: "Optional email.",
		SuggestedCorrection: "Make the email required.",
	}
	run := func(mutate func(*ConceptualDescription)) error {
		corrected := cloneDescription(t, current)
		corrected.Things[0].Properties[2].Presence = "required"
		mutate(&corrected)
		payload := patchPayload(t, current, corrected)
		_, _, err := RunConceptualCorrection(context.Background(), &reviewScriptClient{responses: []json.RawMessage{payload}}, ConceptualCorrectionOptions{
			OutDir: t.TempDir(), SourceUnits: units, Current: current, Findings: []AdversarialFinding{finding},
			Decisions: []ConceptualCorrectionDecision{{FindingID: finding.ID, Decision: "apply", Note: "Make the email required only."}},
		})
		return err
	}
	if err := run(func(*ConceptualDescription) {}); err != nil {
		t.Fatalf("the cited attribute alone could not be corrected: %v", err)
	}
	err := run(func(value *ConceptualDescription) { value.Things[0].Properties[0].Presence = "optional" })
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("a sibling attribute changed under an attribute-scoped finding: %v", err)
	}
}

// Two opposite links that each refer to exactly one record are two facts (a
// request is filed by a user; the user points to its latest request), so both
// foreign keys stay.
func TestOppositeManyToOneLinksAreBothKept(t *testing.T) {
	units := acceptedUnits(t)
	description := testDescription()
	description.Things[0].Links = append(description.Things[0].Links, DescriptionLink{
		To: "zahtev", Meaning: "последњи захтев", PerThis: "0..1", PerOther: "0..N",
		Evidence: DescriptionEvidence{Segments: []string{"SU-007"}, Mode: "direct"},
	})
	if issues := ConceptualDescriptionIssues(description, units); strings.Contains(strings.Join(issues, " "), "both sides") {
		t.Fatalf("two distinct many-to-one links were reported as one link stated twice: %v", issues)
	}
	model := ConceptualDescriptionToModel(description, units)
	var manyToOne []ConceptualRelationshipProposal
	for _, relationship := range model.Relationships {
		if relationship.Cardinality == "many_to_one" && (relationship.From == model.EntityConcepts[0].ID || relationship.To == model.EntityConcepts[0].ID) &&
			(relationship.From == model.EntityConcepts[1].ID || relationship.To == model.EntityConcepts[1].ID) {
			manyToOne = append(manyToOne, relationship)
		}
	}
	if len(manyToOne) != 2 || manyToOne[0].From == manyToOne[1].From {
		t.Fatalf("expected one many-to-one link in each direction, got %+v (all: %+v)", manyToOne, model.Relationships)
	}
}
