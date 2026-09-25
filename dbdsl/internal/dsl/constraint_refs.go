package dsl

import (
	"fmt"
	"regexp"
	"strings"
)

// ConstraintReference describes how a logical constraint reference is
// materialized in the relational model. A relationship reference can expand to
// two physical fields for a many-to-many relationship owned by its through
// entity.
type ConstraintReference struct {
	LogicalReference string
	Kind             string
	RelationshipID   string
	PhysicalFields   []string
}

type RelationshipForeignKey struct {
	OwnerEntityID string
	Field         string
}

// RelationshipForeignKeys is the single source of truth for relationship
// orientation and generated foreign-key names used by validation and DBML
// generation.
func RelationshipForeignKeys(relationship Relationship) []RelationshipForeignKey {
	switch relationship.Cardinality {
	case "many_to_one", "one_to_one":
		return []RelationshipForeignKey{{OwnerEntityID: relationship.From, Field: GeneratedForeignKeyName(relationship.To)}}
	case "one_to_many":
		return []RelationshipForeignKey{{OwnerEntityID: relationship.To, Field: GeneratedForeignKeyName(relationship.From)}}
	case "many_to_many":
		if relationship.Through == "" {
			return nil
		}
		return []RelationshipForeignKey{
			{OwnerEntityID: relationship.Through, Field: GeneratedForeignKeyName(relationship.From)},
			{OwnerEntityID: relationship.Through, Field: GeneratedForeignKeyName(relationship.To)},
		}
	default:
		return nil
	}
}

// ResolveConstraintReference accepts an owner-local scalar attribute, a
// generated FK column name, or a relationship ID. Relationship IDs are logical
// references: they are resolved to the physical FK field(s) before generation.
func ResolveConstraintReference(doc *Document, owner, reference string) (ConstraintReference, error) {
	resolved := ConstraintReference{LogicalReference: reference}
	if doc == nil || owner == "" || reference == "" {
		return resolved, fmt.Errorf("constraint reference owner and field are required")
	}

	attributeExists := false
	for _, entity := range doc.Entities {
		if entity.ID != owner {
			continue
		}
		for _, attribute := range entity.Attributes {
			if attribute.ID == reference {
				attributeExists = true
				break
			}
		}
		break
	}

	type producer struct {
		relationshipID string
		field          string
	}
	physicalProducers := map[string][]producer{}
	var relationship *Relationship
	var relationshipFields []string
	for index := range doc.Relationships {
		rel := &doc.Relationships[index]
		for _, fk := range RelationshipForeignKeys(*rel) {
			if fk.OwnerEntityID != owner {
				continue
			}
			physicalProducers[fk.Field] = append(physicalProducers[fk.Field], producer{relationshipID: rel.ID, field: fk.Field})
			if rel.ID == reference {
				relationship = rel
				relationshipFields = append(relationshipFields, fk.Field)
			}
		}
	}

	if relationship != nil {
		for _, field := range relationshipFields {
			if hasEntityAttribute(doc, owner, field) || len(physicalProducers[field]) > 1 {
				return resolved, fmt.Errorf("relationship %s resolves to colliding physical field %s.%s", relationship.ID, owner, field)
			}
		}
		resolved.Kind = "relationship"
		resolved.RelationshipID = relationship.ID
		resolved.PhysicalFields = append([]string(nil), relationshipFields...)
		return resolved, nil
	}

	for _, rel := range doc.Relationships {
		if rel.ID == reference {
			return resolved, fmt.Errorf("relationship %s does not materialize a foreign key on owner %s", reference, owner)
		}
	}

	if attributeExists {
		if len(physicalProducers[reference]) > 0 {
			return resolved, fmt.Errorf("attribute %s.%s collides with a generated foreign key", owner, reference)
		}
		resolved.Kind = "attribute"
		resolved.PhysicalFields = []string{reference}
		return resolved, nil
	}

	if producers := physicalProducers[reference]; len(producers) > 0 {
		if len(producers) > 1 {
			return resolved, fmt.Errorf("generated foreign key %s.%s is produced by multiple relationships", owner, reference)
		}
		resolved.Kind = "generated_fk"
		resolved.RelationshipID = producers[0].relationshipID
		resolved.PhysicalFields = []string{reference}
		return resolved, nil
	}

	return resolved, fmt.Errorf("unknown field %s.%s", owner, reference)
}

func hasEntityAttribute(doc *Document, owner, field string) bool {
	for _, entity := range doc.Entities {
		if entity.ID != owner {
			continue
		}
		for _, attribute := range entity.Attributes {
			if attribute.ID == field {
				return true
			}
		}
	}
	return false
}

func GeneratedForeignKeyName(entityID string) string {
	return fmt.Sprintf("%s_id", identifierToSnake(entityID))
}

var constraintSnakeBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var constraintNonSnakeIdentifier = regexp.MustCompile(`[^A-Za-z0-9]+`)
var constraintRepeatedUnderscore = regexp.MustCompile(`_+`)

func identifierToSnake(value string) string {
	upper := strings.ToUpper(value)
	if strings.HasPrefix(upper, "ENT-") || strings.HasPrefix(upper, "ENT_") {
		value = value[4:]
	}
	value = constraintSnakeBoundary.ReplaceAllString(value, `${1}_${2}`)
	value = constraintNonSnakeIdentifier.ReplaceAllString(value, "_")
	value = constraintRepeatedUnderscore.ReplaceAllString(value, "_")
	return strings.ToLower(strings.Trim(value, "_"))
}
