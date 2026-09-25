package dsl

import (
	"strings"
	"testing"
)

func TestResolveConstraintReferenceSupportsRelationshipOrientations(t *testing.T) {
	doc := &Document{
		Entities: []Entity{
			{ID: "ENT-ORDER"}, {ID: "ENT-CUSTOMER"}, {ID: "ENT-LINE"},
			{ID: "ENT-TAG"}, {ID: "ENT-ORDER-TAG", Kind: "association"},
		},
		Relationships: []Relationship{
			{ID: "REL-ORDER-CUSTOMER", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one"},
			{ID: "REL-CUSTOMER-PRIMARY-ORDER", From: "ENT-CUSTOMER", To: "ENT-ORDER", Cardinality: "one_to_one"},
			{ID: "REL-ORDER-LINES", From: "ENT-ORDER", To: "ENT-LINE", Cardinality: "one_to_many"},
			{ID: "REL-ORDER-TAGS", From: "ENT-ORDER", To: "ENT-TAG", Cardinality: "many_to_many", Through: "ENT-ORDER-TAG"},
		},
	}

	cases := []struct {
		owner, reference string
		fields           []string
	}{
		{"ENT-ORDER", "REL-ORDER-CUSTOMER", []string{"customer_id"}},
		{"ENT-CUSTOMER", "REL-CUSTOMER-PRIMARY-ORDER", []string{"order_id"}},
		{"ENT-LINE", "REL-ORDER-LINES", []string{"order_id"}},
		{"ENT-ORDER-TAG", "REL-ORDER-TAGS", []string{"order_id", "tag_id"}},
		{"ENT-ORDER", "customer_id", []string{"customer_id"}},
	}
	for _, tc := range cases {
		resolved, err := ResolveConstraintReference(doc, tc.owner, tc.reference)
		if err != nil {
			t.Fatalf("resolve %s on %s: %v", tc.reference, tc.owner, err)
		}
		if strings.Join(resolved.PhysicalFields, ",") != strings.Join(tc.fields, ",") {
			t.Fatalf("resolve %s on %s = %v, want %v", tc.reference, tc.owner, resolved.PhysicalFields, tc.fields)
		}
	}
}

func TestResolveConstraintReferenceRejectsWrongOwnerAndPhysicalCollision(t *testing.T) {
	doc := &Document{
		Entities: []Entity{
			{ID: "ENT-ORDER", Attributes: []Attribute{{ID: "customer_id"}}},
			{ID: "ENT-CUSTOMER"},
		},
		Relationships: []Relationship{{ID: "REL-ORDER-CUSTOMER", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one"}},
	}
	if _, err := ResolveConstraintReference(doc, "ENT-CUSTOMER", "REL-ORDER-CUSTOMER"); err == nil || !strings.Contains(err.Error(), "does not materialize") {
		t.Fatalf("wrong relationship owner was not rejected: %v", err)
	}
	if _, err := ResolveConstraintReference(doc, "ENT-ORDER", "REL-ORDER-CUSTOMER"); err == nil || !strings.Contains(err.Error(), "colliding physical field") {
		t.Fatalf("attribute/FK collision was not rejected: %v", err)
	}
}
