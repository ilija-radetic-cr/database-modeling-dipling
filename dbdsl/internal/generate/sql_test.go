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
