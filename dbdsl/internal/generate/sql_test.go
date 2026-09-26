package generate

import (
	"strings"
	"testing"

	"dbdsl/internal/dsl"
)

func sqlFixture() *dsl.Document {
	yes, no := true, false
	return &dsl.Document{
		DSL:   dsl.DSLMeta{Name: "DB-DSL", Version: "0.5"},
		Model: dsl.ModelInfo{ID: "shop", Name: "Shop"},
		Entities: []dsl.Entity{
			{ID: "ENT-USER", Label: "User", TableName: "user", Kind: "regular", Attributes: []dsl.Attribute{
				{ID: "email", Label: "Email", Type: "email", Required: &yes},
			}},
			{ID: "ENT-ORDER", Label: "Order", TableName: "order", Kind: "regular", Attributes: []dsl.Attribute{
				{ID: "status", Label: "Status", Type: "string", Required: &yes, EnumValues: []string{"new", "paid", "it's done"}},
				{ID: "total", Label: "Total", Type: "money", Required: &yes},
				{ID: "note", Label: "Note", Type: "text", Required: &no},
			}},
			{ID: "ENT-PRODUCT", Label: "Product", TableName: "product", Kind: "regular", Attributes: []dsl.Attribute{{ID: "name", Type: "string", Required: &yes}}},
			{ID: "ENT-ORDER-PRODUCT", Label: "Order product", TableName: "order_product", Kind: "association"},
		},
		Relationships: []dsl.Relationship{
			{ID: "REL-PLACES", From: "ENT-USER", To: "ENT-ORDER", Cardinality: "one_to_many", Required: &yes, FKRequired: &yes, OnDelete: "restrict"},
			{ID: "REL-CONTAINS", From: "ENT-ORDER", To: "ENT-PRODUCT", Cardinality: "many_to_many", Through: "ENT-ORDER-PRODUCT", OnDelete: "cascade"},
		},
		Constraints: []dsl.Constraint{
			{ID: "CON-EMAIL-UNIQUE", Type: "unique", Owner: "ENT-USER", Field: "email"},
			{ID: "CON-TOTAL", Type: "min_inclusive", Owner: "ENT-ORDER", Field: "total", Value: 0},
			{ID: "CON-NOTE", Type: "check", Owner: "ENT-ORDER", Expression: "note IS NULL OR char_length(note) > 0"},
		},
		StateMachines: []dsl.StateMachine{{ID: "SM-ORDER", Owner: "ENT-ORDER", Field: "status", Initial: "new", Transitions: []dsl.StateTransition{{From: "new", To: "paid"}}}},
	}
}

func TestPostgreSQLRendersRelationalSchema(t *testing.T) {
	sql := PostgreSQL(sqlFixture())
	for _, want := range []string{
		`CREATE TYPE order_status_enum AS ENUM ('new', 'paid', 'it''s done');`,
		`CREATE TABLE "user" (`,
		`CREATE TABLE "order" (`,
		"id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY",
		"user_id bigint NOT NULL",
		"status order_status_enum NOT NULL",
		"total numeric(12,2) NOT NULL",
		"note text,",
		"PRIMARY KEY (order_id, product_id)",
		"CONSTRAINT uq_con_email_unique UNIQUE (email)",
		"CONSTRAINT ck_con_total CHECK (total >= 0)",
		"CONSTRAINT ck_con_note CHECK (note IS NULL OR char_length(note) > 0)",
		`ALTER TABLE "order" ADD CONSTRAINT fk_order_user_id FOREIGN KEY (user_id) REFERENCES "user" (id) ON DELETE RESTRICT; -- REL-PLACES`,
		"FOREIGN KEY (order_id) REFERENCES \"order\" (id) ON DELETE CASCADE",
		"FOREIGN KEY (product_id) REFERENCES product (id) ON DELETE CASCADE",
		`CREATE INDEX idx_order_user_id ON "order" (user_id);`,
		`COMMENT ON TABLE "order" IS 'Order [ENT-ORDER]';`,
		"SM-ORDER: order.status starts in new; allowed transitions new -> paid",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "idx_order_product_order_id") {
		t.Error("the leading column of a composite primary key is already indexed")
	}
	if strings.Count(sql, "(") != strings.Count(sql, ")") || !strings.HasPrefix(strings.TrimSpace(sql[strings.Index(sql, "BEGIN;"):]), "BEGIN;") || !strings.HasSuffix(strings.TrimSpace(sql), "COMMIT;") {
		t.Error("DDL must be one balanced transaction")
	}
}

func TestPostgreSQLConstraintNamesFitIdentifierLimit(t *testing.T) {
	name := sqlConstraintName("fk", strings.Repeat("very_long_table_name_", 5), strings.Repeat("column_", 6))
	if len(name) > 63 || name != sqlConstraintName("fk", strings.Repeat("very_long_table_name_", 5), strings.Repeat("column_", 6)) {
		t.Fatalf("name must be stable and at most 63 bytes: %s (%d)", name, len(name))
	}
}

func TestPostgreSQLPreservesModelLevelCheckAsDocumentation(t *testing.T) {
	doc := &dsl.Document{
		DSL: dsl.DSLMeta{Name: "DB-DSL", Version: "0.6"}, Model: dsl.ModelInfo{ID: "audit", Name: "Audit"},
		Constraints: []dsl.Constraint{{
			ID: "CON-MODEL", Type: "check", Owner: "model", Description: "Cross-table invariant.",
			Expression: "orders.total = payments.total",
		}, {
			ID: "CON-ACCESS", Type: "check", Owner: "model", Description: "Application authorization rule.",
			Expression: "Application-enforced rule CON-ACCESS: only an administrator may manage products (applies to ENT-PRODUCT).",
		}},
	}
	sql := PostgreSQL(doc)
	for _, want := range []string{
		"CON-MODEL (model-level): orders.total = payments.total",
		"CON-ACCESS (model-level): Application-enforced rule CON-ACCESS: only an administrator may manage products (applies to ENT-PRODUCT).",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("validated model-level check %q must remain documented as unenforced:\n%s", want, sql)
		}
	}
}

func TestPostgreSQLPreservesApplicationRuleNotes(t *testing.T) {
	doc := &dsl.Document{
		DSL: dsl.DSLMeta{Name: "DB-DSL", Version: "0.6"}, Model: dsl.ModelInfo{ID: "rules", Name: "Rules"},
		Entities: []dsl.Entity{
			{ID: "ENT-ITEM", TableName: "items", Kind: "regular", Attributes: []dsl.Attribute{{
				ID: "quantity", Type: "integer", Notes: []string{"Application-enforced rule CON-QUANTITY-POSITIVE: quantity must be positive."},
			}}},
			{ID: "ENT-ORDER", TableName: "orders", Kind: "regular"},
		},
		Relationships: []dsl.Relationship{{
			ID: "REL-ORDER-ITEM", From: "ENT-ITEM", To: "ENT-ORDER", Cardinality: "many_to_one",
			Notes: []string{"Application-enforced rule CON-ORDER-ACCESS: only an owner may update the relationship."},
		}},
	}

	sql := PostgreSQL(doc)
	for _, want := range []string{
		"ENT-ITEM.quantity (items.quantity): Application-enforced rule CON-QUANTITY-POSITIVE: quantity must be positive.",
		"REL-ORDER-ITEM: Application-enforced rule CON-ORDER-ACCESS: only an owner may update the relationship.",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("application-enforced model note %q was dropped:\n%s", want, sql)
		}
	}
}

func TestExplicitRelationshipRolesRemainDistinctInSQLAndDBML(t *testing.T) {
	doc := sqlFixture()
	yes, no := true, false
	doc.Relationships = []dsl.Relationship{
		{ID: "REL-REQUESTER", From: "ENT-ORDER", To: "ENT-USER", Cardinality: "many_to_one", ForeignKey: "requester_id", Required: &yes, FKRequired: &yes, OnDelete: "restrict"},
		{ID: "REL-OPERATOR", From: "ENT-ORDER", To: "ENT-USER", Cardinality: "one_to_one", ForeignKey: "operator_id", Required: &no, FKRequired: &no, OnDelete: "set_null"},
	}
	sql := PostgreSQL(doc)
	for _, want := range []string{"requester_id bigint NOT NULL", "operator_id bigint", "FOREIGN KEY (requester_id) REFERENCES \"user\" (id) ON DELETE RESTRICT", "FOREIGN KEY (operator_id) REFERENCES \"user\" (id) ON DELETE SET NULL", "UNIQUE (operator_id)"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("missing %q in SQL:\n%s", want, sql)
		}
	}
	dbml := DBML(doc)
	for _, want := range []string{"requester_id bigint [not null]", "operator_id bigint", "order.requester_id > user.id", "order.operator_id > user.id", "operator_id [unique"} {
		if !strings.Contains(dbml, want) {
			t.Fatalf("missing %q in DBML:\n%s", want, dbml)
		}
	}
}

func TestCaseInsensitiveUniquenessUsesExpressionIndexes(t *testing.T) {
	doc := sqlFixture()
	doc.Constraints[0].Comparison = "case_insensitive"
	sql := PostgreSQL(doc)
	if !strings.Contains(sql, ` ON "user" (lower(email));`) || strings.Contains(sql, "CONSTRAINT uq_con_email_unique UNIQUE") {
		t.Fatalf("case-insensitive identity must use one expression key:\n%s", sql)
	}
	if dbml := DBML(doc); !strings.Contains(dbml, "`lower(email)` [unique, name: 'CON-EMAIL-UNIQUE']") {
		t.Fatalf("DBML loses comparison semantics:\n%s", dbml)
	}
	doc.Constraints[0].Comparison = "case_sensitive"
	if sql := PostgreSQL(doc); !strings.Contains(sql, "CONSTRAINT uq_con_email_unique UNIQUE (email)") || strings.Contains(sql, "lower(email)") {
		t.Fatalf("explicit case-sensitive semantics changed:\n%s", sql)
	}
}

func TestRelationshipRoleDescriptionSurvivesPhysicalExport(t *testing.T) {
	doc := sqlFixture()
	doc.Relationships[0].Label = "Requester"
	doc.Relationships[0].Description = "The member who submitted the request."
	sql := PostgreSQL(doc)
	if !strings.Contains(sql, `COMMENT ON COLUMN "order".user_id IS 'Requester - The member who submitted the request. [REL-PLACES]';`) {
		t.Fatalf("generated FK must retain relationship role meaning:\n%s", sql)
	}
}

func TestTransitionMetadataSurvivesSQLAndDBML(t *testing.T) {
	doc := sqlFixture()
	doc.StateMachines[0].Notes = []string{"new -> paid: actor cashier; trigger before closing; effect record time"}
	for format, out := range map[string]string{"sql": PostgreSQL(doc), "dbml": DBML(doc)} {
		if !strings.Contains(out, "actor cashier; trigger before closing; effect record time (enforced by the application)") {
			t.Fatalf("%s lost lifecycle semantics:\n%s", format, out)
		}
	}
}

func TestExplicitFKIndexDoesNotDuplicateAutomaticIndex(t *testing.T) {
	doc := sqlFixture()
	doc.Indexes = []dsl.Index{{ID: "IDX-ORDER-USER", Owner: "ENT-ORDER", Fields: []string{"user_id"}}}
	sql := PostgreSQL(doc)
	if strings.Count(sql, `CREATE INDEX idx_order_user_id ON "order" (user_id);`) != 1 {
		t.Fatalf("duplicate explicit and auto FK index:\n%s", sql)
	}
}
func TestCaseInsensitiveIndexNamesDoNotAliasNormalizedIDs(t *testing.T) {
	a := dbmlIndex{Settings: "[unique, name: 'A-B']"}
	b := dbmlIndex{Settings: "[unique, name: 'A_B']"}
	if sqlCaseInsensitiveIndexName("User", a) == sqlCaseInsensitiveIndexName("User", b) || sqlCaseInsensitiveIndexName("User", a) == sqlCaseInsensitiveIndexName("Other", a) {
		t.Fatal("distinct index identities alias")
	}
}

func TestCaseInsensitiveIndexIdentityIsNotParsedFromDBMLQuotes(t *testing.T) {
	doc := sqlFixture()
	doc.Constraints = []dsl.Constraint{
		{ID: "A'B", Owner: "ENT-USER", Type: "unique", Field: "email", Comparison: "case_insensitive"},
		{ID: "A'C", Owner: "ENT-USER", Type: "unique", Field: "email", Comparison: "case_insensitive"},
	}
	indexes := buildUniqueIndexes(doc)["ENT-USER"]
	if indexes[0].ID != "A'B" || indexes[1].ID != "A'C" || sqlCaseInsensitiveIndexName("ENT-USER", indexes[0]) == sqlCaseInsensitiveIndexName("ENT-USER", indexes[1]) {
		t.Fatal("raw constraint identity was lost")
	}
}

func TestPostgreSQLGivesDistinctNamesToIDsThatNormalizeAlike(t *testing.T) {
	doc := sqlFixture()
	doc.Constraints = append(doc.Constraints,
		dsl.Constraint{ID: "CON-A-B", Type: "unique", Owner: "ENT-PRODUCT", Field: "name"},
		dsl.Constraint{ID: "CON-A_B", Type: "unique", Owner: "ENT-ORDER", Field: "total"},
		dsl.Constraint{ID: "CON-C-D", Type: "check", Owner: "ENT-ORDER", Expression: "total >= 0"},
		dsl.Constraint{ID: "CON-C_D", Type: "check", Owner: "ENT-ORDER", Expression: "total < 1000000"},
	)
	sql := PostgreSQL(doc)
	names := map[string]int{}
	for _, line := range strings.Split(sql, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if (field == "CONSTRAINT" || field == "INDEX") && i+1 < len(fields) {
				names[fields[i+1]]++
			}
		}
	}
	for name, count := range names {
		if count > 1 && !strings.HasPrefix(name, "fk_") {
			t.Fatalf("physical name %s is used %d times:\n%s", name, count, sql)
		}
	}
	if names["uq_con_a_b"] != 1 || names["ck_con_c_d"] != 1 {
		t.Fatalf("the first owner keeps the plain name, the second gets a suffix: %v", names)
	}
}

func TestDBMLExportRejectsEnumValuesItCannotCarryUnchanged(t *testing.T) {
	for _, value := range []string{`a"b`, `a\b`, "a\nb"} {
		doc := sqlFixture()
		doc.DSL.Version = "0.6"
		doc.Entities[1].Attributes[0].EnumValues = []string{"new", value}
		if err := dbmlRepresentable(doc); err == nil || !strings.Contains(err.Error(), "cannot be written in DBML") {
			t.Fatalf("value %q must be refused, not rewritten: %v", value, err)
		}
		if !strings.Contains(PostgreSQL(doc), sqlString(value)) {
			t.Fatalf("SQL must keep %q exactly", value)
		}
	}
	if err := dbmlRepresentable(sqlFixture()); err != nil {
		t.Fatalf("ordinary values, including an apostrophe, stay allowed: %v", err)
	}
}
