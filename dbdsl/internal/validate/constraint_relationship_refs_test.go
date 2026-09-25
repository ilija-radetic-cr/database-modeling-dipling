package validate

import (
	"strings"
	"testing"

	"dbdsl/internal/dsl"
)

func TestV05ConstraintAcceptsOwnerLocalRelationshipReferences(t *testing.T) {
	doc := relationshipConstraintDocument("unique")
	v := constraintReferenceValidator(doc)
	v.validateConstraints()
	if len(v.errors) != 0 {
		t.Fatalf("relationship references in composite unique were rejected: %s", strings.Join(v.errors, "\n"))
	}
}

func TestV05RequiredConstraintRejectsRelationshipReference(t *testing.T) {
	doc := relationshipConstraintDocument("required")
	doc.Constraints[0].Field = "REL-INVENTORY-ITEM"
	doc.Constraints[0].Fields = nil
	v := constraintReferenceValidator(doc)
	v.validateConstraints()
	if !containsValidationError(v.errors, "redundantly declares relationship requiredness") {
		t.Fatalf("redundant relationship required constraint was not rejected: %v", v.errors)
	}
}

func relationshipConstraintDocument(constraintType string) *dsl.Document {
	return &dsl.Document{
		Entities: []dsl.Entity{
			{ID: "ENT-ITEM-INVENTORY"}, {ID: "ENT-ITEM"}, {ID: "ENT-WAREHOUSE-FACILITY"},
		},
		Relationships: []dsl.Relationship{
			{ID: "REL-INVENTORY-ITEM", From: "ENT-ITEM-INVENTORY", To: "ENT-ITEM", Cardinality: "many_to_one"},
			{ID: "REL-INVENTORY-LOCATION", From: "ENT-ITEM-INVENTORY", To: "ENT-WAREHOUSE-FACILITY", Cardinality: "many_to_one"},
		},
		Constraints: []dsl.Constraint{{
			ID: "CON-INVENTORY-ITEM-LOCATION-UNIQUE", Type: constraintType, Owner: "ENT-ITEM-INVENTORY",
			Fields: []string{"REL-INVENTORY-ITEM", "REL-INVENTORY-LOCATION"}, Description: "Inventory identity.",
		}},
	}
}

func constraintReferenceValidator(doc *dsl.Document) *validatorV06 {
	entities := map[string]dsl.Entity{}
	attributes := map[string]map[string]bool{}
	for _, entity := range doc.Entities {
		entities[entity.ID] = entity
		attributes[entity.ID] = map[string]bool{}
	}
	return &validatorV06{doc: doc, entityByID: entities, attributeIDs: attributes, generatedFKs: map[string]map[string]bool{}}
}

func containsValidationError(errors []string, part string) bool {
	for _, err := range errors {
		if strings.Contains(err, part) {
			return true
		}
	}
	return false
}

func TestV05IndexRejectsUnknownFieldAndOwner(t *testing.T) {
	doc := relationshipConstraintDocument("unique")
	doc.Indexes = []dsl.Index{
		{ID: "IDX-INVENTORY-ITEM", Owner: "ENT-ITEM-INVENTORY", Fields: []string{"REL-INVENTORY-ITEM"}, Description: "By item."},
		{ID: "IDX-MISSING-FIELD", Owner: "ENT-ITEM-INVENTORY", Fields: []string{"missing"}, Description: "Broken."},
		{ID: "IDX-MISSING-OWNER", Owner: "ENT-NONE", Fields: []string{"x"}, Description: "Broken."},
	}
	v := constraintReferenceValidator(doc)
	v.validateIndexes()
	if len(v.errors) != 2 || !containsValidationError(v.errors, "IDX-MISSING-FIELD") || !containsValidationError(v.errors, "unknown owner entity ENT-NONE") {
		t.Fatalf("unexpected index validation: %v", v.errors)
	}
}
