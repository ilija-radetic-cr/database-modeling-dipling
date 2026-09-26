package llmpipeline

import (
	"strings"
	"testing"
)

func mapperEvidence(unit string) EvidenceProposal {
	return EvidenceProposal{SourceUnits: []string{"SU-1", unit}, SupportLevel: "explicit", Confidence: "high"}
}

func mapperFixture() ConceptualModelProposal {
	required := true
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
			{ID: "REL-PAYS", Label: "pays for", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one", Required: &required, Evidence: mapperEvidence("RA-5")},
			{ID: "REL-CONTAINS", Label: "contains", From: "ENT-ORDER", To: "ENT-PIZZA", Cardinality: "many_to_many", Evidence: mapperEvidence("RA-6")},
		},
		ConstraintConcepts: []ConceptualConstraintProposal{
			{ID: "CON-TOTAL", Label: "Positive total", Kind: "check", Targets: []string{"ATTR-ORDER-TOTAL"}, Expression: "total_price >= 0", Evidence: mapperEvidence("RA-2")},
			{ID: "CON-ACCESS", Label: "Owner access", Kind: "security", Targets: []string{"REL-PLACES"}, Evidence: mapperEvidence("RA-7")},
			{ID: "CON-MANAGE", Label: "Manage products", Description: "Only an administrator may manage products.", Kind: "security", Targets: []string{"ENT-ORDER"}, Evidence: mapperEvidence("RA-8")},
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
	if c := constraints["CON-MANAGE"]; c == nil || c.Type != "check" || c.Owner != "model" || !strings.Contains(c.Expression, "Application-enforced rule CON-MANAGE") || !containsString(c.Evidence.SourceUnits, "RA-8") {
		t.Errorf("an entity-targeted application rule must remain as a documented model-level rule: %+v", c)
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

func TestMapConceptualToLogicalRecordsApplicationRuleOnAllTargets(t *testing.T) {
	model := mapperFixture()
	model.ConstraintConcepts = append(model.ConstraintConcepts,
		ConceptualConstraintProposal{
			ID: "CON-MULTI-OWNER", Label: "Cross-record quantity", Description: "Quantities across records must remain consistent.",
			Kind: "application_enforced", Targets: []string{"ATTR-ORDER-TOTAL", "ATTR-PIZZA-NAME"}, Evidence: mapperEvidence("RA-10"),
		},
		ConceptualConstraintProposal{
			ID: "CON-REL-AND-ATTRIBUTE", Label: "Relationship and value rule", Description: "The relationship and recorded value are checked together.",
			Kind: "application_enforced", Targets: []string{"REL-PLACES", "ATTR-ORDER-TOTAL"}, Evidence: mapperEvidence("RA-11"),
		},
		ConceptualConstraintProposal{
			ID: "CON-TWO-ENTITIES", Label: "Entity-level rule", Description: "The rule governs both records.",
			Kind: "application_enforced", Targets: []string{"ENT-ORDER", "ENT-PIZZA"}, Evidence: mapperEvidence("RA-12"),
		},
	)
	patch, _, err := MapConceptualToLogical(model, LogicalMappingOptions{})
	if err != nil {
		t.Fatal(err)
	}
	entities := map[string]*EntityProposal{}
	relationships := map[string]*RelationshipProposal{}
	constraints := map[string][]*ConstraintProposal{}
	for _, op := range patch.Operations {
		switch op.Operation {
		case "add_entity":
			entities[op.Entity.ID] = op.Entity
		case "add_relationship":
			relationships[op.Relationship.ID] = op.Relationship
		case "add_constraint":
			constraints[op.Constraint.ID] = append(constraints[op.Constraint.ID], op.Constraint)
		}
	}
	attribute := func(entityID, field string) *AttributeProposal {
		for i := range entities[entityID].Attributes {
			if entities[entityID].Attributes[i].ID == field {
				return &entities[entityID].Attributes[i]
			}
		}
		return nil
	}
	hasNote := func(notes []string, id string) bool {
		for _, note := range notes {
			if strings.Contains(note, id) {
				return true
			}
		}
		return false
	}
	total, pizzaName := attribute("ENT-ORDER", "total_price"), attribute("ENT-PIZZA", "name")
	if total == nil || pizzaName == nil || !hasNote(total.Notes, "CON-MULTI-OWNER") || !hasNote(pizzaName.Notes, "CON-MULTI-OWNER") {
		t.Fatalf("multi-owner rule was not attached to every governed attribute: total=%+v pizza=%+v", total, pizzaName)
	}
	if !containsString(total.Evidence.SourceUnits, "RA-10") || !containsString(pizzaName.Evidence.SourceUnits, "RA-10") {
		t.Fatalf("multi-owner rule evidence was lost: total=%+v pizza=%+v", total.Evidence, pizzaName.Evidence)
	}
	if rel := relationships["REL-PLACES"]; rel == nil || !hasNote(rel.Notes, "CON-REL-AND-ATTRIBUTE") || !containsString(rel.Evidence.SourceUnits, "RA-11") {
		t.Fatalf("relationship target did not retain application rule: %+v", rel)
	}
	if !hasNote(total.Notes, "CON-REL-AND-ATTRIBUTE") || !containsString(total.Evidence.SourceUnits, "RA-11") {
		t.Fatalf("attribute target beside relationship did not retain application rule: %+v", total)
	}
	if len(constraints["CON-MULTI-OWNER"]) != 0 || len(constraints["CON-REL-AND-ATTRIBUTE"]) != 0 {
		t.Fatalf("attribute/relationship application rules became invented SQL checks: %+v", constraints)
	}
	if got := constraints["CON-TWO-ENTITIES"]; len(got) != 1 || got[0].Owner != "model" || !strings.Contains(got[0].Expression, "ENT-ORDER, ENT-PIZZA") {
		t.Fatalf("multi-entity fallback must be one documented model constraint: %+v", got)
	}
}

func TestMapConceptualToLogicalRejectsUnresolvedCardinality(t *testing.T) {
	model := mapperFixture()
	model.Relationships[0].Cardinality = "unknown"
	if _, _, err := MapConceptualToLogical(model, LogicalMappingOptions{}); err == nil || !strings.Contains(err.Error(), "unresolved cardinality") {
		t.Fatalf("unknown cardinality must stop the mapping instead of being defaulted: %v", err)
	}
}

func TestMapConceptualToLogicalPreservesDistinctRolesOnSeparatePhysicalFKs(t *testing.T) {
	required := true
	model := ConceptualModelProposal{
		EntityConcepts: []ConceptualEntityProposal{
			{ID: "ENT-ORDER", Label: "Order", Description: "An order.", Kind: "regular"},
			{ID: "ENT-CUSTOMER", Label: "Customer", Description: "A customer.", Kind: "regular"},
		},
		Relationships: []ConceptualRelationshipProposal{
			{ID: "REL-BILLING", Label: "billing customer", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one", Required: &required},
			{ID: "REL-SHIPPING", Label: "shipping customer", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one", Required: &required},
		},
	}
	optional := false
	model.Relationships[1].Required = &optional
	for pass := 0; pass < 2; pass++ {
		patch, _, err := MapConceptualToLogical(model, LogicalMappingOptions{})
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]string{}
		for _, op := range patch.Operations {
			if op.Relationship == nil {
				continue
			}
			r := op.Relationship
			seen[r.ID] = r.ForeignKey
			if r.ID == "REL-BILLING" && (!r.FKRequired || r.OnDelete != "restrict") {
				t.Fatalf("lost required role: %+v", r)
			}
			if r.ID == "REL-SHIPPING" && (r.FKRequired || r.OnDelete != "set_null") {
				t.Fatalf("lost optional role: %+v", r)
			}
		}
		if seen["REL-BILLING"] != "rel_billing_id" || seen["REL-SHIPPING"] != "rel_shipping_id" {
			t.Fatalf("role columns lost: %v", seen)
		}
		model.Relationships[0], model.Relationships[1] = model.Relationships[1], model.Relationships[0]
	}
}

func TestMapConceptualToLogicalKeepsInverseRelationshipDedupe(t *testing.T) {
	required := true
	model := ConceptualModelProposal{
		EntityConcepts: []ConceptualEntityProposal{
			{ID: "ENT-CUSTOMER", Label: "Customer", Description: "A customer.", Kind: "regular"},
			{ID: "ENT-ORDER", Label: "Order", Description: "An order.", Kind: "regular"},
		},
		Relationships: []ConceptualRelationshipProposal{
			{ID: "REL-PLACES", Label: "places", From: "ENT-CUSTOMER", To: "ENT-ORDER", Cardinality: "one_to_many", Required: &required},
			{ID: "REL-BELONGS", Label: "belongs to", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one", Required: &required},
		},
	}
	patch, report, err := MapConceptualToLogical(model, LogicalMappingOptions{})
	if err != nil {
		t.Fatalf("inverse relationship pair should dedupe: %v", err)
	}
	count := 0
	for _, operation := range patch.Operations {
		if operation.Operation == "add_relationship" {
			count++
		}
	}
	if count != 1 || !strings.Contains(strings.Join(report.Decisions, " "), "inverse relationship") {
		t.Fatalf("inverse relationship pair was not recorded as one logical link: count=%d report=%+v", count, report)
	}
}

func TestMapConceptualToLogicalRejectsInverseRelationshipsWithConflictingOptionality(t *testing.T) {
	required, optional := true, false
	model := ConceptualModelProposal{
		EntityConcepts: []ConceptualEntityProposal{
			{ID: "ENT-CUSTOMER", Label: "Customer", Description: "A customer.", Kind: "regular"},
			{ID: "ENT-ORDER", Label: "Order", Description: "An order.", Kind: "regular"},
		},
		Relationships: []ConceptualRelationshipProposal{
			{ID: "REL-PLACES", Label: "places", From: "ENT-CUSTOMER", To: "ENT-ORDER", Cardinality: "one_to_many", Required: &required},
			{ID: "REL-BELONGS", Label: "belongs to", From: "ENT-ORDER", To: "ENT-CUSTOMER", Cardinality: "many_to_one", Required: &optional},
		},
	}
	_, _, err := MapConceptualToLogical(model, LogicalMappingOptions{})
	if err == nil || !strings.Contains(err.Error(), "disagree on physical semantics") || !strings.Contains(err.Error(), "required true/false") {
		t.Fatalf("inverse relationships with conflicting nullability must stop mapping: %v", err)
	}
}

func TestMapConceptualToLogicalRejectsReversedManyToManyBeforeCreatingSecondLinkTable(t *testing.T) {
	model := ConceptualModelProposal{
		EntityConcepts: []ConceptualEntityProposal{
			{ID: "ENT-STUDENT", Label: "Student", Description: "A student.", Kind: "regular"},
			{ID: "ENT-COURSE", Label: "Course", Description: "A course.", Kind: "regular"},
		},
		Relationships: []ConceptualRelationshipProposal{
			{ID: "REL-ENROLLS", Label: "enrolls in", From: "ENT-STUDENT", To: "ENT-COURSE", Cardinality: "many_to_many"},
			{ID: "REL-HAS-STUDENTS", Label: "has students", From: "ENT-COURSE", To: "ENT-STUDENT", Cardinality: "many_to_many"},
		},
	}
	_, _, err := MapConceptualToLogical(model, LogicalMappingOptions{})
	if err == nil || !strings.Contains(err.Error(), "same many-to-many link in reverse directions") || !strings.Contains(err.Error(), "model explicit association roles") {
		t.Fatalf("reversed many-to-many relationships must fail with an actionable error: %v", err)
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
