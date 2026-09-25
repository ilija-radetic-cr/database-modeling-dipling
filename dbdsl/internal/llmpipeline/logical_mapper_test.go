package llmpipeline

import (
	"strings"
	"testing"
)

func mapperEvidence(unit string) EvidenceProposal {
	return EvidenceProposal{SourceUnits: []string{"SU-1", unit}, SupportLevel: "explicit", Confidence: "high"}
}

func mapperFixture() ConceptualModelProposal {
	required, optional := true, false
	model := ConceptualModelProposal{
		EntityConcepts: []ConceptualEntityProposal{
			{ID: "ENT-CUSTOMER", Label: "Customer", Kind: "regular", Evidence: mapperEvidence("RA-1"), Attributes: []ConceptualAttributeProposal{
				{ID: "ATTR-CUSTOMER-ID", Label: "Customer identifier", Name: "customer_id", ValueType: "string", Evidence: mapperEvidence("RA-1")},
				{ID: "ATTR-CUSTOMER-EMAIL", Label: "Email", Name: "email", ValueType: "email", Required: true, Unique: true, Evidence: mapperEvidence("RA-1")},
			}},
			{ID: "ENT-ORDER", Label: "Order", Kind: "regular", Evidence: mapperEvidence("RA-2"), Attributes: []ConceptualAttributeProposal{
				{ID: "ATTR-ORDER-CUSTOMER", Label: "Customer", Name: "customer_id", ValueType: "string", Evidence: mapperEvidence("RA-2")},
				{ID: "ATTR-ORDER-TOTAL", Label: "Total price", Name: "total_price", ValueType: "money", Required: true, Evidence: mapperEvidence("RA-2")},
				{ID: "ATTR-ORDER-STATUS", Label: "Status", Name: "status", ValueType: "string", Required: true, Evidence: mapperEvidence("RA-3")},
			}},
			{ID: "ENT-PIZZA", Label: "Pizza", Kind: "regular", Evidence: mapperEvidence("RA-4"), Attributes: []ConceptualAttributeProposal{
				{ID: "ATTR-PIZZA-NAME", Label: "Name", Evidence: mapperEvidence("RA-4")},
			}},
		},
		Relationships: []ConceptualRelationshipProposal{
			{ID: "REL-PLACES", Label: "places", From: "ENT-CUSTOMER", To: "ENT-ORDER", Cardinality: "one_to_many", Required: &required, Evidence: mapperEvidence("RA-2")},
			{ID: "REL-PAYS", Label: "pays for", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one", Required: &optional, Evidence: mapperEvidence("RA-5")},
			{ID: "REL-CONTAINS", Label: "contains", From: "ENT-ORDER", To: "ENT-PIZZA", Cardinality: "many_to_many", Evidence: mapperEvidence("RA-6")},
		},
		ConstraintConcepts: []ConceptualConstraintProposal{
			{ID: "CON-TOTAL", Label: "Positive total", Kind: "check", Targets: []string{"ATTR-ORDER-TOTAL"}, Expression: "total_price >= 0", Evidence: mapperEvidence("RA-2")},
			{ID: "CON-ACCESS", Label: "Owner access", Kind: "security", Targets: []string{"REL-PLACES"}, Evidence: mapperEvidence("RA-7")},
		},
		LifecycleConcepts: []PlanElementProposal{{ID: "LFC-ORDER", Label: "Order lifecycle", Owner: "ENT-ORDER", Field: "status",
			States: []string{"new", "accepted", "rejected"}, Initial: "new", Terminal: []string{"accepted", "rejected"},
			Transitions: []ConceptualTransition{{From: "new", To: "accepted"}, {From: "new", To: "rejected"}, {From: "gone", To: "new"}},
			SourceUnits: []string{"SU-1"}}},
	}
	return model
}

func TestMapConceptualToLogicalAppliesRelationalRules(t *testing.T) {
	model := mapperFixture()
	patch, report, err := MapConceptualToLogical(model, LogicalMappingOptions{})
	if err != nil {
		t.Fatalf("map: %v", err)
	}
	entities, relationships, constraints, machines := map[string]*EntityProposal{}, map[string]*RelationshipProposal{}, map[string]*ConstraintProposal{}, []*StateMachineProposal{}
	for _, op := range patch.Operations {
		switch op.Operation {
		case "add_entity":
			entities[op.Entity.ID] = op.Entity
		case "add_relationship":
			relationships[op.Relationship.ID] = op.Relationship
		case "add_constraint":
			constraints[op.Constraint.ID] = op.Constraint
		case "add_state_machine":
			machines = append(machines, op.StateMachine)
		}
	}
	attr := func(entity, name string) *AttributeProposal {
		for i := range entities[entity].Attributes {
			if entities[entity].Attributes[i].ID == name {
				return &entities[entity].Attributes[i]
			}
		}
		return nil
	}
	if attr("ENT-CUSTOMER", "customer_id") != nil {
		t.Error("surrogate identifier must be dropped; the generator supplies the primary key")
	}
	if attr("ENT-ORDER", "customer_id") != nil {
		t.Error("attribute shadowing the generated foreign key must be dropped")
	}
	if got := attr("ENT-ORDER", "total_price"); got == nil || got.Type != "money" {
		t.Errorf("declared value type must be kept: %+v", got)
	}
	if got := attr("ENT-PIZZA", "name"); got == nil || got.Type != "string" {
		t.Errorf("missing name/type must be inferred: %+v", got)
	}
	if entities["ENT-ORDER"].TableName != "orders" || entities["ENT-CUSTOMER"].TableName != "customers" {
		t.Errorf("tables must be plural snake_case: %s %s", entities["ENT-ORDER"].TableName, entities["ENT-CUSTOMER"].TableName)
	}
	if rel := relationships["REL-PLACES"]; rel == nil || !rel.FKRequired || rel.OnDelete != "restrict" {
		t.Errorf("required relationship must produce a NOT NULL restrict FK: %+v", rel)
	}
	if relationships["REL-PAYS"] != nil {
		t.Error("REL-PAYS creates the same orders.customer_id foreign key and must be merged into REL-PLACES")
	}
	if !containsString(relationships["REL-PLACES"].Evidence.SourceUnits, "RA-5") {
		t.Error("merged relationship must keep the evidence of the relationship it absorbed")
	}
	link := entities["ENT-ORDER-PIZZA"]
	if link == nil || link.Kind != "association" || relationships["REL-CONTAINS"].Through != "ENT-ORDER-PIZZA" {
		t.Fatalf("many-to-many must go through an association table: %+v %+v", link, relationships["REL-CONTAINS"])
	}
	if c := constraints["CON-UQ-CUSTOMER-EMAIL"]; c == nil || c.Type != "unique" || c.Field != "email" {
		t.Errorf("unique attribute must produce a unique constraint: %+v", c)
	}
	if c := constraints["CON-TOTAL"]; c == nil || c.Type != "check" || c.Expression != "total_price >= 0" {
		t.Errorf("check with expression must be emitted: %+v", c)
	}
	if constraints["CON-ACCESS"] != nil || !containsString(relationships["REL-PLACES"].Evidence.SourceUnits, "RA-7") {
		t.Error("an application-enforced rule on a relationship must be recorded on that relationship, not as DDL")
	}
	if len(machines) != 1 || machines[0].Initial != "new" || len(machines[0].Transitions) != 2 {
		t.Fatalf("lifecycle must become a state machine with only valid transitions: %+v", machines)
	}
	if status := attr("ENT-ORDER", "status"); status == nil || strings.Join(status.EnumValues, ",") != "new,accepted,rejected" {
		t.Errorf("status column must enumerate the lifecycle states: %+v", status)
	}
	if len(report.Decisions) == 0 || report.RuleVersion != LogicalMappingRuleVersion {
		t.Errorf("mapping report must record applied rules: %+v", report)
	}
}

func TestMapConceptualToLogicalRejectsUnresolvedCardinality(t *testing.T) {
	model := mapperFixture()
	model.Relationships[0].Cardinality = "unknown"
	if _, _, err := MapConceptualToLogical(model, LogicalMappingOptions{}); err == nil || !strings.Contains(err.Error(), "unresolved cardinality") {
		t.Fatalf("unknown cardinality must stop the mapping instead of being defaulted: %v", err)
	}
}

func TestInferValueType(t *testing.T) {
	cases := map[string]string{"email": "email", "birth_date": "date", "created_at": "datetime", "unit_price": "money",
		"is_available": "boolean", "seat_count": "integer", "description": "text", "line_number": "string", "photo_path": "file_path"}
	for name, want := range cases {
		if got := inferValueType(name, ""); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
}
