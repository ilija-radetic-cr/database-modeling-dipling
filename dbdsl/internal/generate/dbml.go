package generate

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

type fkColumn struct {
	OwnerEntityID string
	Name          string
	RefEntityID   string
	RefTable      string
	Required      bool
}

type dbmlRef struct {
	FromTable string
	FromField string
	ToTable   string
	ToField   string
	OnDelete  string
}

func DBMLFile(path string) (string, error) {
	doc, err := dsl.LoadDocument(path)
	if err != nil {
		return "", err
	}
	if doc.DSL.Version != "0.6" {
		if doc, _, _, err = dsl.LoadBundle(path); err != nil {
			return "", err
		}
	}
	if err := dbmlRepresentable(doc); err != nil {
		return "", err
	}
	return DBML(doc), nil
}

// dbmlRepresentable rejects enum values that DBML cannot carry unchanged. The
// DBML parser keeps an escape such as \u0022 as literal text, so a value with
// a quote, a backslash or a control character would come back as a different
// value; the export fails with a clear reason instead of changing it silently.
// SQL keeps such values exactly and is not affected.
func dbmlRepresentable(doc *dsl.Document) error {
	check := func(owner, field, value string) error {
		for _, r := range value {
			if r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
				return fmt.Errorf("enum value %q of %s.%s cannot be written in DBML without changing it; rename the value or use the SQL export", value, owner, field)
			}
		}
		return nil
	}
	for _, entity := range doc.Entities {
		for _, attribute := range entity.Attributes {
			for _, value := range attribute.EnumValues {
				if err := check(entity.ID, attribute.ID, value); err != nil {
					return err
				}
			}
		}
	}
	for _, machine := range doc.StateMachines {
		for _, state := range machine.States {
			if err := check(machine.Owner, machine.Field, state); err != nil {
				return err
			}
		}
	}
	return nil
}

func DBML(doc *dsl.Document) string {
	entityByID := map[string]dsl.Entity{}
	for _, entity := range doc.Entities {
		entityByID[entity.ID] = entity
	}

	fks, refs := buildFKColumns(doc, entityByID)
	uniqueIndexes := buildUniqueIndexes(doc)
	addOneToOneIndexes(doc, uniqueIndexes)
	addSearchIndexes(doc, uniqueIndexes)
	tableComments := buildTableComments(doc)
	enumTypes := buildEnumTypes(doc)

	var out bytes.Buffer
	fmt.Fprintf(&out, "Project %s {\n", dbmlIdentifier(doc.Model.ID))
	fmt.Fprintf(&out, "  Note: '%s'\n", dbmlQuote("Generated from DB-DSL v"+doc.DSL.Version))
	fmt.Fprintln(&out, "}")
	fmt.Fprintln(&out)

	writeEnums(&out, enumTypes)

	for _, entity := range doc.Entities {
		writeTable(&out, entity, fks[entity.ID], uniqueIndexes[entity.ID], tableComments[entity.ID], enumTypes)
		fmt.Fprintln(&out)
	}

	for _, ref := range refs {
		fmt.Fprintf(&out, "Ref: %s.%s > %s.%s", ref.FromTable, ref.FromField, ref.ToTable, ref.ToField)
		if ref.OnDelete != "" {
			fmt.Fprintf(&out, " [delete: %s]", ref.OnDelete)
		}
		fmt.Fprintln(&out)
	}

	// Keep the same application-rule trace as SQL; these comments never claim
	// that a diagram or database enforces the described business behavior.
	var rules bytes.Buffer
	_, notes := buildSQLChecks(doc)
	writeSQLNotes(&rules, doc, notes)
	for _, line := range strings.Split(strings.TrimSpace(rules.String()), "\n") {
		if strings.HasPrefix(line, "--") {
			fmt.Fprintln(&out, "//"+strings.TrimPrefix(line, "--"))
		}
	}
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func writeTable(out *bytes.Buffer, entity dsl.Entity, fks []fkColumn, uniqueIndexes []dbmlIndex, comments []string, enumTypes map[string]enumType) {
	fmt.Fprintf(out, "Table %s {\n", dbmlIdentifier(entity.TableName))

	if entity.Kind != "association" {
		fmt.Fprintln(out, "  id bigint [pk, increment]")
	}

	for _, fk := range fks {
		if fk.Required {
			fmt.Fprintf(out, "  %s bigint [not null]\n", dbmlIdentifier(fk.Name))
		} else {
			fmt.Fprintf(out, "  %s bigint\n", dbmlIdentifier(fk.Name))
		}
	}

	for _, attribute := range entity.Attributes {
		settings := columnSettings(attribute)
		columnType := dbmlType(entity.ID, attribute, enumTypes)
		if settings == "" {
			fmt.Fprintf(out, "  %s %s\n", dbmlIdentifier(attribute.ID), columnType)
		} else {
			fmt.Fprintf(out, "  %s %s %s\n", dbmlIdentifier(attribute.ID), columnType, settings)
		}
	}

	indexes := append([]dbmlIndex(nil), uniqueIndexes...)
	if entity.Kind == "association" && len(fks) >= 2 {
		fields := make([]string, 0, len(fks))
		for _, fk := range fks {
			fields = append(fields, fk.Name)
		}
		indexes = append([]dbmlIndex{{Fields: fields, Settings: "[pk]"}}, indexes...)
	}

	if len(indexes) > 0 {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "  indexes {")
		for _, index := range indexes {
			fmt.Fprintf(out, "    %s %s\n", dbmlUniqueIndexFields(index), index.Settings)
		}
		fmt.Fprintln(out, "  }")
	}

	for _, comment := range comments {
		fmt.Fprintf(out, "  // %s\n", comment)
	}

	fmt.Fprintln(out, "}")
}

type enumType struct {
	Name   string
	Values []string
}

func buildEnumTypes(doc *dsl.Document) map[string]enumType {
	enums := map[string]enumType{}
	for _, entity := range doc.Entities {
		for _, attribute := range entity.Attributes {
			if len(attribute.EnumValues) == 0 {
				continue
			}
			key := enumKey(entity.ID, attribute.ID)
			enums[key] = enumType{
				Name:   fmt.Sprintf("%s_%s_enum", toSnake(entity.ID), attribute.ID),
				Values: append([]string(nil), attribute.EnumValues...),
			}
		}
	}
	return enums
}

func writeEnums(out *bytes.Buffer, enumTypes map[string]enumType) {
	if len(enumTypes) == 0 {
		return
	}

	keys := make([]string, 0, len(enumTypes))
	for key := range enumTypes {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		enumType := enumTypes[key]
		fmt.Fprintf(out, "Enum %s {\n", dbmlIdentifier(enumType.Name))
		for _, value := range enumType.Values {
			fmt.Fprintf(out, "  %s\n", dbmlEnumValue(value))
		}
		fmt.Fprintln(out, "}")
		fmt.Fprintln(out)
	}
}

func buildFKColumns(doc *dsl.Document, entityByID map[string]dsl.Entity) (map[string][]fkColumn, []dbmlRef) {
	fks := map[string][]fkColumn{}
	var refs []dbmlRef

	for _, relationship := range doc.Relationships {
		from, hasFrom := entityByID[relationship.From]
		to, hasTo := entityByID[relationship.To]
		if !hasFrom || !hasTo {
			continue
		}

		switch relationship.Cardinality {
		case "many_to_one", "one_to_one":
			fk := fkColumn{
				OwnerEntityID: relationship.From,
				Name:          dsl.RelationshipForeignKeyName(relationship, relationship.To),
				RefEntityID:   relationship.To,
				RefTable:      to.TableName,
				Required:      effectiveFKRequired(relationship),
			}
			fks[relationship.From] = appendUniqueFK(fks[relationship.From], fk)
			refs = append(refs, dbmlRef{FromTable: from.TableName, FromField: fk.Name, ToTable: to.TableName, ToField: "id", OnDelete: dbmlOnDelete(relationship.OnDelete)})
		case "one_to_many":
			fk := fkColumn{
				OwnerEntityID: relationship.To,
				Name:          dsl.RelationshipForeignKeyName(relationship, relationship.From),
				RefEntityID:   relationship.From,
				RefTable:      from.TableName,
				Required:      effectiveFKRequired(relationship),
			}
			fks[relationship.To] = appendUniqueFK(fks[relationship.To], fk)
			refs = append(refs, dbmlRef{FromTable: to.TableName, FromField: fk.Name, ToTable: from.TableName, ToField: "id", OnDelete: dbmlOnDelete(relationship.OnDelete)})
		case "many_to_many":
			through, hasThrough := entityByID[relationship.Through]
			if !hasThrough {
				continue
			}
			fromFK := fkColumn{
				OwnerEntityID: relationship.Through,
				Name:          dsl.RelationshipForeignKeyName(relationship, relationship.From),
				RefEntityID:   relationship.From,
				RefTable:      from.TableName,
				Required:      effectiveFKRequired(relationship),
			}
			toFK := fkColumn{
				OwnerEntityID: relationship.Through,
				Name:          dsl.RelationshipForeignKeyName(relationship, relationship.To),
				RefEntityID:   relationship.To,
				RefTable:      to.TableName,
				Required:      effectiveFKRequired(relationship),
			}
			fks[relationship.Through] = appendUniqueFK(fks[relationship.Through], fromFK)
			fks[relationship.Through] = appendUniqueFK(fks[relationship.Through], toFK)
			refs = append(refs,
				dbmlRef{FromTable: through.TableName, FromField: fromFK.Name, ToTable: from.TableName, ToField: "id", OnDelete: dbmlOnDelete(relationship.OnDelete)},
				dbmlRef{FromTable: through.TableName, FromField: toFK.Name, ToTable: to.TableName, ToField: "id", OnDelete: dbmlOnDelete(relationship.OnDelete)},
			)
		}
	}

	return fks, refs
}

func dbmlOnDelete(value string) string {
	switch value {
	case "cascade", "restrict":
		return value
	case "set_null":
		return "set null"
	default:
		return ""
	}
}

func appendUniqueFK(items []fkColumn, next fkColumn) []fkColumn {
	for _, item := range items {
		if item.Name == next.Name {
			return items
		}
	}
	return append(items, next)
}

type dbmlIndex struct {
	ID         string
	Comparison string
	Fields     []string
	Settings   string
}

func buildUniqueIndexes(doc *dsl.Document) map[string][]dbmlIndex {
	indexes := map[string][]dbmlIndex{}
	for _, constraint := range doc.Constraints {
		fields := constraintFields(constraint)
		if constraint.Type != "unique" || len(fields) == 0 {
			continue
		}
		physicalFields := make([]string, 0, len(fields))
		for _, field := range fields {
			resolved, err := dsl.ResolveConstraintReference(doc, constraint.Owner, field)
			if err != nil {
				physicalFields = append(physicalFields, field)
				continue
			}
			physicalFields = append(physicalFields, resolved.PhysicalFields...)
		}
		indexes[constraint.Owner] = append(indexes[constraint.Owner], dbmlIndex{
			ID:         constraint.ID,
			Fields:     physicalFields,
			Comparison: constraint.Comparison,
			Settings:   fmt.Sprintf("[unique, name: '%s']", dbmlQuote(constraint.ID)),
		})
	}
	return indexes
}

// addSearchIndexes adds the non-unique indexes of the model after its keys.
func addSearchIndexes(doc *dsl.Document, indexes map[string][]dbmlIndex) {
	for _, index := range doc.Indexes {
		indexes[index.Owner] = append(indexes[index.Owner], dbmlIndex{
			Fields:   indexPhysicalFields(doc, index),
			Settings: fmt.Sprintf("[name: '%s']", dbmlQuote(index.ID)),
		})
	}
}

func indexPhysicalFields(doc *dsl.Document, index dsl.Index) []string {
	fields := make([]string, 0, len(index.Fields))
	for _, field := range index.Fields {
		resolved, err := dsl.ResolveConstraintReference(doc, index.Owner, field)
		if err != nil {
			fields = append(fields, field)
			continue
		}
		fields = append(fields, resolved.PhysicalFields...)
	}
	return fields
}

// hasIndexOn reports whether one of the unique indexes already covers exactly
// the given columns, in any order, with the same comparison.
func hasIndexOn(indexes []dbmlIndex, fields []string, comparison string) bool {
	want := append([]string(nil), fields...)
	sort.Strings(want)
	for _, index := range indexes {
		have := append([]string(nil), index.Fields...)
		sort.Strings(have)
		if strings.Join(have, ",") == strings.Join(want, ",") && index.Comparison == comparison {
			return true
		}
	}
	return false
}

func addOneToOneIndexes(doc *dsl.Document, indexes map[string][]dbmlIndex) {
	for _, relationship := range doc.Relationships {
		if relationship.Cardinality != "one_to_one" {
			continue
		}
		fields := []string{dsl.RelationshipForeignKeyName(relationship, relationship.To)}
		if hasIndexOn(indexes[relationship.From], fields, "") {
			// A unique key on the same column already makes the link one-to-one.
			continue
		}
		indexes[relationship.From] = append(indexes[relationship.From], dbmlIndex{
			Fields:   fields,
			Settings: fmt.Sprintf("[unique, name: '%s_one_to_one']", dbmlQuote(relationship.ID)),
		})
	}
}

func buildTableComments(doc *dsl.Document) map[string][]string {
	comments := map[string][]string{}
	for _, relationship := range doc.Relationships {
		for _, fk := range dsl.RelationshipForeignKeys(relationship) {
			comments[fk.OwnerEntityID] = append(comments[fk.OwnerEntityID],
				fmt.Sprintf("%s: %s", fk.Field, strings.ReplaceAll(elementComment(relationship.ID, relationship.Label, relationship.Description), "\n", " ")))
		}
	}
	for _, constraint := range doc.Constraints {
		field := physicalConstraintField(doc, constraint.Owner, constraint.Field)
		switch constraint.Type {
		case "min_inclusive":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: %s >= %v", constraint.ID, field, constraintBoundValue(constraint)))
		case "min_exclusive":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: %s > %v", constraint.ID, field, constraintBoundValue(constraint)))
		case "max_inclusive":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: %s <= %v", constraint.ID, field, constraintBoundValue(constraint)))
		case "max_exclusive":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: %s < %v", constraint.ID, field, constraintBoundValue(constraint)))
		case "length":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: length(%s) value=%v min=%v max=%v", constraint.ID, field, constraint.Value, constraint.Min, constraint.Max))
		case "regex":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: %s matches %s", constraint.ID, field, constraint.Pattern))
		case "check":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: %s", constraint.ID, constraint.Expression))
		case "conditional_required":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: conditional required", constraint.ID))
		case "required":
			comments[constraint.Owner] = append(comments[constraint.Owner],
				fmt.Sprintf("Constraint %s: %s is required", constraint.ID, field))
		}
	}
	return comments
}

func physicalConstraintField(doc *dsl.Document, owner, field string) string {
	resolved, err := dsl.ResolveConstraintReference(doc, owner, field)
	if err == nil && len(resolved.PhysicalFields) == 1 {
		return resolved.PhysicalFields[0]
	}
	return field
}

func columnSettings(attribute dsl.Attribute) string {
	var settings []string
	if attribute.Required != nil && *attribute.Required {
		settings = append(settings, "not null")
	}
	if attribute.Default != nil {
		settings = append(settings, "default: "+formatDefault(attribute.Default))
	}
	if len(settings) == 0 {
		return ""
	}
	return "[" + strings.Join(settings, ", ") + "]"
}

func dbmlType(entityID string, attribute dsl.Attribute, enumTypes map[string]enumType) string {
	if len(attribute.EnumValues) > 0 {
		return dbmlIdentifier(enumTypes[enumKey(entityID, attribute.ID)].Name)
	}

	switch attribute.Type {
	case "id":
		return "int"
	case "string":
		return "varchar(255)"
	case "text":
		return "text"
	case "integer":
		return "int"
	case "decimal":
		precision := 10
		scale := 2
		if attribute.Precision != nil {
			precision = *attribute.Precision
		}
		if attribute.Scale != nil {
			scale = *attribute.Scale
		}
		return fmt.Sprintf("decimal(%d,%d)", precision, scale)
	case "boolean":
		return "boolean"
	case "date":
		return "date"
	case "time":
		return "time"
	case "datetime":
		return "datetime"
	case "money":
		precision := 12
		scale := 2
		if attribute.Precision != nil {
			precision = *attribute.Precision
		}
		if attribute.Scale != nil {
			scale = *attribute.Scale
		}
		return fmt.Sprintf("decimal(%d,%d)", precision, scale)
	case "uuid", "email", "phone", "url", "file_path":
		return "varchar(255)"
	default:
		return "varchar(255)"
	}
}

func effectiveFKRequired(relationship dsl.Relationship) bool {
	if relationship.FKRequired != nil {
		return *relationship.FKRequired
	}
	return relationship.Required != nil && *relationship.Required
}

func enumKey(entityID, attributeID string) string {
	return entityID + "." + attributeID
}

// Expression indexes preserve explicit case-insensitive uniqueness in DBML.
func dbmlUniqueIndexFields(index dbmlIndex) string {
	if index.Comparison != "case_insensitive" {
		return dbmlIndexFields(index.Fields)
	}
	expressions := make([]string, 0, len(index.Fields))
	for _, field := range index.Fields {
		expressions = append(expressions, "`lower("+sqlIdentifier(field)+")`")
	}
	if len(expressions) == 1 {
		return expressions[0]
	}
	return "(" + strings.Join(expressions, ", ") + ")"
}

func dbmlIndexFields(fields []string) string {
	if len(fields) == 1 {
		return dbmlIdentifier(fields[0])
	}
	quoted := make([]string, 0, len(fields))
	for _, field := range fields {
		quoted = append(quoted, dbmlIdentifier(field))
	}
	return "(" + strings.Join(quoted, ", ") + ")"
}

func formatDefault(value any) string {
	switch v := value.(type) {
	case string:
		return "'" + dbmlQuote(v) + "'"
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func fkName(entityID string) string {
	return dsl.GeneratedForeignKeyName(entityID)
}

var snakeBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var nonSnakeIdentifier = regexp.MustCompile(`[^A-Za-z0-9]+`)
var repeatedUnderscore = regexp.MustCompile(`_+`)

func toSnake(value string) string {
	// Logical-projection IDs commonly use an ENT- namespace prefix. It is useful
	// in the DSL and trace, but it is not part of the generated FK column name.
	upper := strings.ToUpper(value)
	if strings.HasPrefix(upper, "ENT-") || strings.HasPrefix(upper, "ENT_") {
		value = value[4:]
	}
	value = snakeBoundary.ReplaceAllString(value, `${1}_${2}`)
	value = nonSnakeIdentifier.ReplaceAllString(value, "_")
	value = repeatedUnderscore.ReplaceAllString(value, "_")
	return strings.ToLower(strings.Trim(value, "_"))
}

var plainIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func dbmlIdentifier(value string) string {
	if plainIdentifier.MatchString(value) {
		return value
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

func dbmlQuote(value string) string {
	return strings.ReplaceAll(value, "'", "\\'")
}

// Enum fields use quoted-identifier syntax. Backtick identifiers are accepted
// for table and column names by dbdiagram, but @dbml/core rejects them in enum
// declarations. The legacy parser also terminates on an escaped double quote,
// so encode that character as a Unicode escape to keep the DBML parseable.
func dbmlEnumValue(value string) string {
	escaped := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\u0022",
		"\n", "\\n",
		"\r", "\\r",
		"\t", "\\t",
		"\b", "\\b",
		"\f", "\\f",
		"\v", "\\v",
		"\x00", "\\u0000",
	).Replace(value)
	return `"` + escaped + `"`
}
