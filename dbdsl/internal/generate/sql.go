package generate

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

// PostgreSQLFile renders PostgreSQL DDL for a DB-DSL document or bundle path.
func PostgreSQLFile(path string) (string, error) {
	doc, err := dsl.LoadDocument(path)
	if err != nil {
		return "", err
	}
	if doc.DSL.Version != "0.6" {
		if doc, _, _, err = dsl.LoadBundle(path); err != nil {
			return "", err
		}
	}
	return PostgreSQL(doc), nil
}

// PostgreSQL renders DDL with the same relational rules as the DBML generator:
// surrogate bigint keys, generated foreign-key columns, composite keys for
// association tables and enum types for closed value sets. Foreign keys are
// added after all tables so relationship cycles never block creation. Rules
// plain DDL cannot enforce (state transitions, derived views, conditional
// requirements) are emitted as comments so nothing from the model is dropped.
func PostgreSQL(doc *dsl.Document) string {
	entityByID := map[string]dsl.Entity{}
	for _, entity := range doc.Entities {
		entityByID[entity.ID] = entity
	}
	fks, _ := buildFKColumns(doc, entityByID)
	enumTypes := buildEnumTypes(doc)
	uniques := buildUniqueIndexes(doc)
	addOneToOneIndexes(doc, uniques)
	checks, notes := buildSQLChecks(doc)

	var out bytes.Buffer
	fmt.Fprintf(&out, "-- %s\n", nonEmptyString(doc.Model.Name, doc.Model.ID))
	fmt.Fprintf(&out, "-- PostgreSQL DDL generated deterministically from DB-DSL v%s (model %s).\n", doc.DSL.Version, doc.Model.ID)
	fmt.Fprintln(&out, "-- Every table and column comment carries the DB-DSL element ID for traceability.")
	fmt.Fprintln(&out)
	fmt.Fprintln(&out, "BEGIN;")
	fmt.Fprintln(&out)

	enumKeys := make([]string, 0, len(enumTypes))
	for key := range enumTypes {
		enumKeys = append(enumKeys, key)
	}
	sort.Strings(enumKeys)
	for _, key := range enumKeys {
		enum := enumTypes[key]
		values := make([]string, 0, len(enum.Values))
		for _, value := range enum.Values {
			values = append(values, sqlString(value))
		}
		fmt.Fprintf(&out, "CREATE TYPE %s AS ENUM (%s);\n", sqlIdentifier(enum.Name), strings.Join(values, ", "))
	}
	if len(enumKeys) > 0 {
		fmt.Fprintln(&out)
	}

	for _, entity := range doc.Entities {
		writeSQLTable(&out, entity, fks[entity.ID], uniques[entity.ID], checks[entity.ID], enumTypes)
	}

	wroteFK := false
	indexed := map[string]bool{}
	for _, entity := range doc.Entities {
		for _, index := range uniques[entity.ID] {
			if len(index.Fields) > 0 {
				indexed[entity.ID+"."+index.Fields[0]] = true
			}
		}
		if entity.Kind == "association" && len(fks[entity.ID]) >= 2 {
			indexed[entity.ID+"."+fks[entity.ID][0].Name] = true
		}
	}
	for _, relationship := range doc.Relationships {
		for _, fk := range dsl.RelationshipForeignKeys(relationship) {
			owner, ok := entityByID[fk.OwnerEntityID]
			refID := relationship.To
			if fk.Field == dsl.GeneratedForeignKeyName(relationship.From) && fk.OwnerEntityID != relationship.From {
				refID = relationship.From
			}
			if relationship.Cardinality == "one_to_many" {
				refID = relationship.From
			}
			ref, refOK := entityByID[refID]
			if !ok || !refOK {
				continue
			}
			if !wroteFK {
				fmt.Fprintln(&out, "-- Foreign keys")
				wroteFK = true
			}
			name := sqlConstraintName("fk", owner.TableName, fk.Field)
			fmt.Fprintf(&out, "ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (id) ON DELETE %s; -- %s\n",
				sqlIdentifier(owner.TableName), sqlIdentifier(name), sqlIdentifier(fk.Field), sqlIdentifier(ref.TableName),
				sqlOnDelete(relationship, fk.OwnerEntityID == relationship.Through), relationship.ID)
		}
	}
	if wroteFK {
		fmt.Fprintln(&out)
		fmt.Fprintln(&out, "-- PostgreSQL does not index foreign keys automatically.")
		for _, entity := range doc.Entities {
			for _, fk := range fks[entity.ID] {
				if indexed[entity.ID+"."+fk.Name] {
					continue
				}
				fmt.Fprintf(&out, "CREATE INDEX %s ON %s (%s);\n", sqlIdentifier(sqlConstraintName("idx", entity.TableName, fk.Name)), sqlIdentifier(entity.TableName), sqlIdentifier(fk.Name))
			}
		}
		fmt.Fprintln(&out)
	}
	if len(doc.Indexes) > 0 {
		fmt.Fprintln(&out, "-- Search indexes")
		for _, index := range doc.Indexes {
			owner, ok := entityByID[index.Owner]
			if !ok {
				continue
			}
			columns := make([]string, 0, len(index.Fields))
			for _, field := range indexPhysicalFields(doc, index) {
				columns = append(columns, sqlIdentifier(field))
			}
			fmt.Fprintf(&out, "CREATE INDEX %s ON %s (%s); -- %s\n", sqlIdentifier(sqlConstraintName("idx", owner.TableName, strings.Join(indexPhysicalFields(doc, index), "_"))),
				sqlIdentifier(owner.TableName), strings.Join(columns, ", "), index.ID)
		}
		fmt.Fprintln(&out)
	}

	writeSQLComments(&out, doc)
	writeSQLNotes(&out, doc, notes)
	fmt.Fprintln(&out, "COMMIT;")
	return out.String()
}

func writeSQLTable(out *bytes.Buffer, entity dsl.Entity, fks []fkColumn, uniques []dbmlIndex, checks []string, enumTypes map[string]enumType) {
	lines := []string{}
	composite := entity.Kind == "association" && len(fks) >= 2
	if !composite {
		lines = append(lines, "id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY")
	}
	for _, fk := range fks {
		line := sqlIdentifier(fk.Name) + " bigint"
		if fk.Required || composite {
			line += " NOT NULL"
		}
		lines = append(lines, line)
	}
	for _, attribute := range entity.Attributes {
		line := sqlIdentifier(attribute.ID) + " " + sqlType(entity.ID, attribute, enumTypes)
		if attribute.Required != nil && *attribute.Required {
			line += " NOT NULL"
		}
		if attribute.Default != nil {
			line += " DEFAULT " + sqlDefault(attribute.Default)
		}
		lines = append(lines, line)
	}
	if composite {
		fields := make([]string, 0, len(fks))
		for _, fk := range fks {
			fields = append(fields, sqlIdentifier(fk.Name))
		}
		lines = append(lines, "PRIMARY KEY ("+strings.Join(fields, ", ")+")")
	}
	for _, index := range uniques {
		name := sqlConstraintName("uq", entity.TableName, strings.Join(index.Fields, "_"))
		if label := uniqueIndexName(index.Settings); label != "" {
			name = sqlConstraintName("uq", label, "")
		}
		fields := make([]string, 0, len(index.Fields))
		for _, field := range index.Fields {
			fields = append(fields, sqlIdentifier(field))
		}
		lines = append(lines, fmt.Sprintf("CONSTRAINT %s UNIQUE (%s)", sqlIdentifier(name), strings.Join(fields, ", ")))
	}
	lines = append(lines, checks...)
	fmt.Fprintf(out, "CREATE TABLE %s (\n  %s\n);\n\n", sqlIdentifier(entity.TableName), strings.Join(lines, ",\n  "))
}

// buildSQLChecks turns enforceable DB-DSL constraints into table CHECKs and
// collects the rest as notes for the trailing comment block.
func buildSQLChecks(doc *dsl.Document) (map[string][]string, []string) {
	checks := map[string][]string{}
	notes := []string{}
	for _, constraint := range doc.Constraints {
		field := sqlIdentifier(physicalConstraintField(doc, constraint.Owner, constraint.Field))
		name := sqlIdentifier(sqlConstraintName("ck", constraint.ID, ""))
		expression := ""
		switch constraint.Type {
		case "min_inclusive":
			expression = fmt.Sprintf("%s >= %s", field, sqlLiteral(constraintBoundValue(constraint)))
		case "min_exclusive":
			expression = fmt.Sprintf("%s > %s", field, sqlLiteral(constraintBoundValue(constraint)))
		case "max_inclusive":
			expression = fmt.Sprintf("%s <= %s", field, sqlLiteral(constraintBoundValue(constraint)))
		case "max_exclusive":
			expression = fmt.Sprintf("%s < %s", field, sqlLiteral(constraintBoundValue(constraint)))
		case "length":
			switch {
			case constraint.Value != nil:
				expression = fmt.Sprintf("char_length(%s) = %s", field, sqlLiteral(constraint.Value))
			case constraint.Min != nil && constraint.Max != nil:
				expression = fmt.Sprintf("char_length(%s) BETWEEN %s AND %s", field, sqlLiteral(constraint.Min), sqlLiteral(constraint.Max))
			case constraint.Min != nil:
				expression = fmt.Sprintf("char_length(%s) >= %s", field, sqlLiteral(constraint.Min))
			case constraint.Max != nil:
				expression = fmt.Sprintf("char_length(%s) <= %s", field, sqlLiteral(constraint.Max))
			}
		case "regex":
			expression = fmt.Sprintf("%s ~ %s", field, sqlString(constraint.Pattern))
		case "check":
			if constraint.Owner == "" {
				notes = append(notes, fmt.Sprintf("%s (model-level): %s", constraint.ID, constraint.Expression))
				continue
			}
			expression = strings.TrimSpace(constraint.Expression)
		case "conditional_required":
			notes = append(notes, fmt.Sprintf("%s: conditional requirement on %s is enforced by the application", constraint.ID, constraint.Owner))
			continue
		default:
			continue
		}
		if expression == "" || constraint.Owner == "" {
			continue
		}
		checks[constraint.Owner] = append(checks[constraint.Owner], fmt.Sprintf("CONSTRAINT %s CHECK (%s)", name, expression))
	}
	return checks, notes
}

func writeSQLComments(out *bytes.Buffer, doc *dsl.Document) {
	fmt.Fprintln(out, "-- Traceability comments")
	for _, entity := range doc.Entities {
		fmt.Fprintf(out, "COMMENT ON TABLE %s IS %s;\n", sqlIdentifier(entity.TableName), sqlString(elementComment(entity.ID, entity.Label, entity.Description)))
		for _, attribute := range entity.Attributes {
			fmt.Fprintf(out, "COMMENT ON COLUMN %s.%s IS %s;\n", sqlIdentifier(entity.TableName), sqlIdentifier(attribute.ID),
				sqlString(elementComment(entity.ID+"."+attribute.ID, attribute.Label, attribute.Description)))
		}
	}
	fmt.Fprintln(out)
}

func writeSQLNotes(out *bytes.Buffer, doc *dsl.Document, notes []string) {
	entityTable := map[string]string{}
	for _, entity := range doc.Entities {
		entityTable[entity.ID] = entity.TableName
	}
	for _, machine := range doc.StateMachines {
		transitions := make([]string, 0, len(machine.Transitions))
		for _, transition := range machine.Transitions {
			transitions = append(transitions, transition.From+" -> "+transition.To)
		}
		notes = append(notes, fmt.Sprintf("%s: %s.%s starts in %s; allowed transitions %s (enforced by the application)",
			machine.ID, entityTable[machine.Owner], machine.Field, machine.Initial, nonEmptyString(strings.Join(transitions, ", "), "none declared")))
	}
	for _, view := range doc.DerivedViews {
		sources := make([]string, 0, len(view.Sources))
		for _, source := range view.Sources {
			sources = append(sources, nonEmptyString(entityTable[source], source))
		}
		notes = append(notes, fmt.Sprintf("%s: %s %s over %s (query specification, not materialized)", view.ID, view.Kind, nonEmptyString(view.Label, view.ID), strings.Join(sources, ", ")))
	}
	if len(notes) == 0 {
		return
	}
	fmt.Fprintln(out, "-- Rules outside plain DDL")
	for _, note := range notes {
		fmt.Fprintf(out, "--   %s\n", strings.ReplaceAll(note, "\n", " "))
	}
	fmt.Fprintln(out)
}

func sqlType(entityID string, attribute dsl.Attribute, enumTypes map[string]enumType) string {
	if len(attribute.EnumValues) > 0 {
		return sqlIdentifier(enumTypes[enumKey(entityID, attribute.ID)].Name)
	}
	precision := func(fallbackPrecision, fallbackScale int) string {
		p, s := fallbackPrecision, fallbackScale
		if attribute.Precision != nil {
			p = *attribute.Precision
		}
		if attribute.Scale != nil {
			s = *attribute.Scale
		}
		return fmt.Sprintf("numeric(%d,%d)", p, s)
	}
	switch attribute.Type {
	case "id", "integer":
		if attribute.Type == "id" {
			return "bigint"
		}
		return "integer"
	case "text":
		return "text"
	case "decimal":
		return precision(10, 2)
	case "money":
		return precision(12, 2)
	case "boolean":
		return "boolean"
	case "date":
		return "date"
	case "time":
		return "time"
	case "datetime":
		return "timestamp"
	case "uuid":
		return "uuid"
	case "email":
		return "varchar(320)"
	case "url", "file_path":
		return "varchar(2048)"
	case "phone":
		return "varchar(32)"
	default:
		return "varchar(255)"
	}
}

func sqlOnDelete(relationship dsl.Relationship, throughTable bool) string {
	switch relationship.OnDelete {
	case "cascade":
		return "CASCADE"
	case "set_null":
		if effectiveFKRequired(relationship) && !throughTable {
			return "RESTRICT"
		}
		return "SET NULL"
	case "restrict":
		return "RESTRICT"
	}
	if throughTable {
		return "CASCADE"
	}
	return "RESTRICT"
}

// PostgreSQL identifiers are limited to 63 bytes; longer generated names are
// shortened with a stable suffix so they stay unique.
func sqlConstraintName(prefix, table, column string) string {
	parts := []string{prefix, toSnake(table)}
	if column != "" {
		parts = append(parts, toSnake(column))
	}
	name := strings.Trim(strings.Join(parts, "_"), "_")
	if len(name) <= 63 {
		return name
	}
	sum := 0
	for _, r := range name {
		sum = (sum*31 + int(r)) % 1000000
	}
	return fmt.Sprintf("%s_%06d", name[:55], sum)
}

func uniqueIndexName(settings string) string {
	start := strings.Index(settings, "name: '")
	if start < 0 {
		return ""
	}
	rest := settings[start+len("name: '"):]
	end := strings.Index(rest, "'")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

var sqlReservedWords = map[string]bool{
	"all": true, "and": true, "any": true, "as": true, "asc": true, "authorization": true, "between": true, "both": true, "case": true,
	"check": true, "collate": true, "column": true, "constraint": true, "create": true, "current_date": true, "current_time": true,
	"current_user": true, "default": true, "desc": true, "distinct": true, "do": true, "else": true, "end": true, "except": true,
	"false": true, "for": true, "foreign": true, "from": true, "grant": true, "group": true, "having": true, "in": true,
	"initially": true, "intersect": true, "into": true, "is": true, "join": true, "leading": true, "left": true, "like": true,
	"limit": true, "not": true, "null": true, "offset": true, "on": true, "only": true, "or": true, "order": true, "primary": true,
	"references": true, "right": true, "select": true, "session_user": true, "some": true, "table": true, "then": true, "to": true,
	"trailing": true, "true": true, "union": true, "unique": true, "user": true, "using": true, "when": true, "where": true, "with": true,
}

func sqlIdentifier(value string) string {
	if plainIdentifier.MatchString(value) && strings.ToLower(value) == value && !sqlReservedWords[value] {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func sqlString(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func sqlLiteral(value any) string {
	switch typed := value.(type) {
	case nil:
		return "NULL"
	case string:
		return sqlString(typed)
	case bool:
		if typed {
			return "TRUE"
		}
		return "FALSE"
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func sqlDefault(value any) string {
	return sqlLiteral(value)
}

func elementComment(id, label, description string) string {
	text := strings.TrimSpace(label)
	if description = strings.TrimSpace(description); description != "" && description != text {
		if text != "" {
			text += " - "
		}
		text += description
	}
	if text == "" {
		return "[" + id + "]"
	}
	return text + " [" + id + "]"
}

func nonEmptyString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
