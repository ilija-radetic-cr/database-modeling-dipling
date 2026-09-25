package llmpipeline

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"dbdsl/internal/dsl"
)

// DescriptionTransformVersion identifies the deterministic rules that turn a
// conceptual description into the conceptual model read by the logical mapper.
const DescriptionTransformVersion = "conceptual_description_to_model_v2"

// SegmentAtomID is the evidence atom that stands for one source unit in the
// DB-DSL bundle. The segment-based flow has no separate requirement stage, so
// every cited unit is its own requirement.
func SegmentAtomID(unitID string) string {
	return "RA-" + strings.TrimPrefix(unitID, "SU-")
}

type descriptionTransformer struct {
	description ConceptualDescription
	units       map[string]dsl.SourceUnit
	entityOf    map[string]string              // thing ID -> entity concept ID
	attributes  map[string]map[string][]string // entity ID -> property key -> attribute IDs
	thingByID   map[string]DescriptionThing
	fallback    map[string]DescriptionEvidence // thing ID -> evidence to use when an element cites none
	model       ConceptualModelProposal
	usedIDs     map[string]bool
}

// ConceptualDescriptionToModel applies the formal transformations the
// description leaves to the backend:
//   - every thing becomes an entity (classifications become lookups);
//   - single and copied properties become attributes, composite properties one
//     attribute per part, and multiple properties a child entity with a 1:N link;
//   - derived properties and queries become derived concepts instead of columns;
//   - links become relationships whose cardinality follows from per_this/per_other;
//   - states become a lifecycle with a status column;
//   - rules become constraint concepts on the elements they apply to.
func ConceptualDescriptionToModel(description ConceptualDescription, units []dsl.SourceUnit) ConceptualModelProposal {
	t := &descriptionTransformer{
		description: description, units: map[string]dsl.SourceUnit{}, entityOf: map[string]string{},
		attributes: map[string]map[string][]string{}, thingByID: map[string]DescriptionThing{},
		fallback: map[string]DescriptionEvidence{}, usedIDs: map[string]bool{},
		model: ConceptualModelProposal{
			EntityConcepts: []ConceptualEntityProposal{}, Relationships: []ConceptualRelationshipProposal{},
			ConstraintConcepts: []ConceptualConstraintProposal{}, LifecycleConcepts: []PlanElementProposal{},
			DerivedConcepts: []PlanElementProposal{}, FileConcepts: []PlanElementProposal{}, ImportConcepts: []PlanElementProposal{},
			UnresolvedReviewIDs: []string{}, Warnings: []string{},
			ConfidenceSummary: map[string]string{"strategy": DescriptionTransformVersion},
		},
	}
	for _, unit := range units {
		t.units[unit.ID] = unit
	}
	for _, thing := range description.Things {
		t.thingByID[thing.ID] = thing
		t.fallback[thing.ID] = thingEvidence(thing)
	}
	for _, thing := range description.Things {
		if len(t.fallback[thing.ID].Segments) == 0 {
			t.warn("thing %s cites no segments and is not modeled", thing.ID)
			continue
		}
		t.entityOf[thing.ID] = t.uniqueID("ENT-" + upperID(nonEmpty(thing.ID, thing.Name)))
	}
	for _, thing := range description.Things {
		if t.entityOf[thing.ID] != "" {
			t.mapThing(thing)
		}
	}
	for _, thing := range description.Things {
		if t.entityOf[thing.ID] != "" {
			t.mapLinks(thing)
			t.mapLifecycle(thing)
		}
	}
	t.mapIdentities()
	t.mapRules()
	t.mapQueries()
	t.mapImports()
	return t.model
}

func (t *descriptionTransformer) warn(format string, args ...any) {
	t.model.Warnings = append(t.model.Warnings, fmt.Sprintf(format, args...))
}

func (t *descriptionTransformer) uniqueID(base string) string {
	id := base
	for n := 2; t.usedIDs[id]; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	t.usedIDs[id] = true
	return id
}

// thingEvidence falls back to the union of what the thing's parts cite, so a
// thing described only through its properties still has evidence.
func thingEvidence(thing DescriptionThing) DescriptionEvidence {
	if len(thing.Evidence.Segments) > 0 {
		return thing.Evidence
	}
	out := DescriptionEvidence{Segments: []string{}, Mode: "direct"}
	for _, property := range thing.Properties {
		for _, id := range property.Evidence.Segments {
			out.Segments = appendUnique(out.Segments, id)
		}
	}
	for _, link := range thing.Links {
		for _, id := range link.Evidence.Segments {
			out.Segments = appendUnique(out.Segments, id)
		}
	}
	return out
}

func (t *descriptionTransformer) evidence(primary DescriptionEvidence, fallbacks ...DescriptionEvidence) EvidenceProposal {
	chosen := primary
	if len(chosen.Segments) == 0 {
		for _, candidate := range fallbacks {
			if len(candidate.Segments) > 0 {
				chosen = candidate
				break
			}
		}
	}
	out := EvidenceProposal{SourceUnits: []string{}, RequirementAtoms: []string{}, ReviewDecisions: []string{}, Notes: []string{}, SupportLevel: "explicit", Confidence: "high"}
	if chosen.Mode == "implied" {
		out.SupportLevel, out.Confidence = "inferred", "medium"
	}
	for _, id := range chosen.Segments {
		if _, ok := t.units[id]; ok {
			out.SourceUnits = appendUnique(out.SourceUnits, id)
			out.RequirementAtoms = appendUnique(out.RequirementAtoms, SegmentAtomID(id))
		}
	}
	return out
}

func (t *descriptionTransformer) mapThing(thing DescriptionThing) {
	entityID := t.entityOf[thing.ID]
	kind := "regular"
	if thing.Kind == "classification" {
		kind = "lookup"
	}
	entity := ConceptualEntityProposal{
		ID: entityID, Label: shortLabel(thing.Name, thing.ID), Description: descriptionText(thing.Description, thing.Name),
		Kind: kind, Attributes: []ConceptualAttributeProposal{}, Evidence: t.evidence(thing.Evidence, t.fallback[thing.ID]),
	}
	columns := map[string]bool{}
	for _, property := range thing.Properties {
		if strings.TrimSpace(property.Name) == "" {
			continue
		}
		key := snakeIdentifier(property.Name)
		evidence := t.evidence(property.Evidence, thing.Evidence, t.fallback[thing.ID])
		switch {
		case property.Origin == "derived":
			t.addDerivedProperty(thing, entityID, property, evidence)
		case property.Shape == "multiple":
			t.addMultipleProperty(thing, entityID, property, evidence)
		case property.Shape == "composite" && len(property.Parts) > 0:
			for _, part := range property.Parts {
				name := snakeIdentifier(property.Name + " " + part)
				attribute := t.attribute(entityID, name, part, property, evidence, columns)
				entity.Attributes = append(entity.Attributes, attribute)
				t.rememberAttribute(entityID, key, attribute.ID)
			}
		default:
			attribute := t.attribute(entityID, key, property.Name, property, evidence, columns)
			entity.Attributes = append(entity.Attributes, attribute)
			t.rememberAttribute(entityID, key, attribute.ID)
		}
	}
	t.model.EntityConcepts = append(t.model.EntityConcepts, entity)
}

func (t *descriptionTransformer) attribute(entityID, name, label string, property DescriptionProperty, evidence EvidenceProposal, columns map[string]bool) ConceptualAttributeProposal {
	name = nonEmpty(name, "vrednost")
	base := name
	for n := 2; columns[name]; n++ {
		name = fmt.Sprintf("%s_%d", base, n)
	}
	columns[name] = true
	attribute := ConceptualAttributeProposal{
		ID: t.uniqueID("ATTR-" + strings.TrimPrefix(entityID, "ENT-") + "-" + upperID(name)), Label: shortLabel(label, name),
		Description: propertyDescription(property), Name: name,
		Required: property.Presence == "required" && property.Variant == "", Evidence: evidence,
	}
	attribute.ValueType = descriptionValueType(property.ValueType)
	values := trimmedUniqueStrings(property.AllowedValues)
	if property.ValueType == "boolean" || ((property.ValueType == "" || property.ValueType == "text") && isYesNo(values)) {
		// A yes/no choice is a flag, not a two-value enum.
		attribute.ValueType, values = "boolean", nil
	}
	if len(values) > 0 && property.Shape != "composite" {
		if expression, ok := numericValuesCheck(name, attribute.ValueType, values); ok {
			// Numbers from a closed set keep their type; the set becomes a check.
			t.model.ConstraintConcepts = append(t.model.ConstraintConcepts, ConceptualConstraintProposal{
				ID: t.uniqueID("CON-" + strings.TrimPrefix(attribute.ID, "ATTR-") + "-VALUES"), Label: shortLabel("Dozvoljene vrednosti "+label, "Dozvoljene vrednosti"),
				Description: fmt.Sprintf("%s: %s.", label, strings.Join(values, ", ")), Kind: "check",
				Targets: []string{attribute.ID}, Expression: expression, Evidence: evidence,
			})
		} else {
			attribute.ValueType, attribute.EnumValues = "string", values
		}
	}
	return attribute
}

// numericValuesCheck turns the allowed values of a numeric attribute into a
// check expression: a range for consecutive integers, a list otherwise.
func numericValuesCheck(column, valueType string, values []string) (string, bool) {
	switch valueType {
	case "integer", "decimal", "money":
	default:
		return "", false
	}
	numbers := make([]float64, 0, len(values))
	for _, value := range values {
		number, err := strconv.ParseFloat(strings.ReplaceAll(value, ",", "."), 64)
		if err != nil {
			return "", false
		}
		numbers = append(numbers, number)
	}
	sort.Float64s(numbers)
	literals := make([]string, 0, len(numbers))
	consecutive := valueType == "integer"
	for i, number := range numbers {
		literals = append(literals, strconv.FormatFloat(number, 'f', -1, 64))
		if number != float64(int64(number)) || (i > 0 && number != numbers[i-1]+1) {
			consecutive = false
		}
	}
	if consecutive && len(numbers) > 2 {
		return fmt.Sprintf("%s BETWEEN %s AND %s", column, literals[0], literals[len(literals)-1]), true
	}
	return fmt.Sprintf("%s IN (%s)", column, strings.Join(literals, ", ")), true
}

func (t *descriptionTransformer) rememberAttribute(entityID, key, attributeID string) {
	if t.attributes[entityID] == nil {
		t.attributes[entityID] = map[string][]string{}
	}
	t.attributes[entityID][key] = append(t.attributes[entityID][key], attributeID)
}

// addMultipleProperty turns a repeating value into its own entity (first
// normal form): one row per value, owned by the thing through a 1:N link.
func (t *descriptionTransformer) addMultipleProperty(thing DescriptionThing, ownerID string, property DescriptionProperty, evidence EvidenceProposal) {
	childID := t.uniqueID(ownerID + "-" + upperID(property.Name))
	child := ConceptualEntityProposal{
		ID: childID, Label: shortLabel(property.Name, property.Name), Description: propertyDescription(property),
		Kind: "regular", Attributes: []ConceptualAttributeProposal{}, Evidence: evidence,
	}
	columns := map[string]bool{}
	parts := property.Parts
	if len(parts) == 0 {
		parts = []string{property.Name}
	}
	single := property
	single.Shape, single.Presence = "single", "required"
	for _, part := range parts {
		child.Attributes = append(child.Attributes, t.attribute(childID, snakeIdentifier(part), part, single, evidence, columns))
	}
	t.model.EntityConcepts = append(t.model.EntityConcepts, child)
	required := true
	t.model.Relationships = append(t.model.Relationships, ConceptualRelationshipProposal{
		ID: t.uniqueID("REL-" + strings.TrimPrefix(childID, "ENT-")), Label: shortLabel(property.Name, property.Name),
		Description: fmt.Sprintf("%s: više vrednosti za jedan zapis %s.", property.Name, thing.Name),
		From:        ownerID, To: childID, Cardinality: "one_to_many", Required: &required, Evidence: evidence,
	})
}

func (t *descriptionTransformer) addDerivedProperty(thing DescriptionThing, entityID string, property DescriptionProperty, evidence EvidenceProposal) {
	metric := property.Name
	if strings.TrimSpace(property.Source) != "" {
		metric += ": " + strings.TrimSpace(property.Source)
	}
	t.model.DerivedConcepts = append(t.model.DerivedConcepts, PlanElementProposal{
		ID: t.uniqueID("DER-" + strings.TrimPrefix(entityID, "ENT-") + "-" + upperID(property.Name)), Label: shortLabel(property.Name, property.Name),
		Description: propertyDescription(property), Kind: "derived", Sources: []string{entityID}, Metrics: []string{metric},
		SourceUnits: evidence.SourceUnits, RequirementAtoms: evidence.RequirementAtoms,
	})
}

// mapLinks derives cardinality from both counts. A link that repeats one
// already declared from the other side is skipped so the pair gets one
// foreign key, not two.
func (t *descriptionTransformer) mapLinks(thing DescriptionThing) {
	fromID := t.entityOf[thing.ID]
	for _, link := range thing.Links {
		toID := t.entityOf[link.To]
		if toID == "" {
			continue
		}
		if t.hasInverseRelationship(fromID, toID) {
			continue
		}
		thisMany, otherMany := countIsMany(link.PerThis, false), countIsMany(link.PerOther, true)
		cardinality := "many_to_one"
		switch {
		case thisMany && otherMany:
			cardinality = "many_to_many"
		case thisMany:
			cardinality = "one_to_many"
		case !otherMany:
			cardinality = "one_to_one"
		}
		required := countIsRequired(link.PerThis)
		target := t.thingByID[link.To]
		t.model.Relationships = append(t.model.Relationships, ConceptualRelationshipProposal{
			ID:    t.uniqueID("REL-" + strings.TrimPrefix(fromID, "ENT-") + "-" + strings.TrimPrefix(toID, "ENT-")),
			Label: shortLabel(link.Meaning, thing.Name+" "+target.Name), Description: descriptionText(link.Meaning, thing.Name+" – "+target.Name),
			From: fromID, To: toID, Cardinality: cardinality, Required: &required,
			Evidence: t.evidence(link.Evidence, thing.Evidence, t.fallback[thing.ID]),
		})
	}
}

func (t *descriptionTransformer) hasInverseRelationship(fromID, toID string) bool {
	if fromID == toID {
		return false
	}
	for _, rel := range t.model.Relationships {
		if rel.From == toID && rel.To == fromID {
			return true
		}
	}
	return false
}

func (t *descriptionTransformer) mapLifecycle(thing DescriptionThing) {
	states := trimmedUniqueStrings(thing.States)
	if len(states) == 0 {
		return
	}
	outgoing := map[string]bool{}
	transitions := []ConceptualTransition{}
	evidence := t.evidence(thing.Evidence, t.fallback[thing.ID])
	for _, transition := range thing.Transitions {
		from, to := strings.TrimSpace(transition.From), strings.TrimSpace(transition.To)
		if !containsString(states, from) || !containsString(states, to) {
			continue
		}
		outgoing[from] = true
		transitions = append(transitions, ConceptualTransition{From: from, To: to})
		for _, id := range t.evidence(transition.Evidence).SourceUnits {
			evidence.SourceUnits = appendUnique(evidence.SourceUnits, id)
			evidence.RequirementAtoms = appendUnique(evidence.RequirementAtoms, SegmentAtomID(id))
		}
	}
	terminal := []string{}
	if len(transitions) > 0 {
		for _, state := range states {
			if !outgoing[state] {
				terminal = append(terminal, state)
			}
		}
	}
	entityID := t.entityOf[thing.ID]
	t.model.LifecycleConcepts = append(t.model.LifecycleConcepts, PlanElementProposal{
		ID: t.uniqueID("LC-" + strings.TrimPrefix(entityID, "ENT-")), Label: shortLabel("Stanje "+thing.Name, "Stanje"),
		Description: "Stanja: " + strings.Join(states, ", ") + ".", Kind: "lifecycle", Owner: entityID, Field: "status",
		States: states, Initial: states[0], Terminal: terminal, Transitions: transitions,
		SourceUnits: evidence.SourceUnits, RequirementAtoms: evidence.RequirementAtoms,
	})
}

var ruleConstraintKind = map[string]string{
	"access": "security", "visibility": "security", "time": "temporal", "quantity": "check",
}

// mapIdentities turns identified_by into a unique key: the listed properties,
// plus the foreign key of a listed owner when the identity holds only within
// it ("šifra" within "preduzece").
func (t *descriptionTransformer) mapIdentities() {
	for _, thing := range t.description.Things {
		entityID := t.entityOf[thing.ID]
		if entityID == "" || len(thing.IdentifiedBy) == 0 {
			continue
		}
		targets, ok := t.uniqueKey(thing.ID, thing.IdentifiedBy)
		if !ok {
			t.warn("identity of %s (%s) does not name its properties or owner and is not enforced as a key", thing.ID, strings.Join(thing.IdentifiedBy, "; "))
			continue
		}
		t.addUniqueness("CON-ID-"+strings.TrimPrefix(entityID, "ENT-"), "Identitet "+thing.Name, "Jedinstveno određuje: "+strings.Join(thing.IdentifiedBy, ", ")+".", targets, t.evidence(thing.Evidence, t.fallback[thing.ID]))
	}
}

// uniqueKey resolves references on one thing into attribute and relationship
// IDs. It fails when any reference is unknown, because a partial key would be
// a different (usually stricter) rule than the one the text states.
func (t *descriptionTransformer) uniqueKey(thingID string, refs []string) ([]string, bool) {
	entityID := t.entityOf[thingID]
	actorThing := map[string]string{}
	for _, actor := range t.description.Actors {
		if actor.RepresentedBy != "" {
			actorThing[actor.ID] = actor.RepresentedBy
		}
	}
	targets := []string{}
	for _, raw := range refs {
		ref := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), thingID+"."))
		if ids := t.attributes[entityID][snakeIdentifier(ref)]; len(ids) > 0 {
			for _, id := range ids {
				targets = appendUnique(targets, id)
			}
			continue
		}
		owner := ref
		if mapped, ok := actorThing[owner]; ok && t.entityOf[owner] == "" {
			owner = mapped
		}
		relationship := t.foreignKeyTo(entityID, t.entityOf[owner])
		if relationship == "" {
			return nil, false
		}
		targets = appendUnique(targets, relationship)
	}
	return targets, len(targets) > 0
}

// foreignKeyTo returns the relationship whose foreign key lives on entityID
// and points at targetID.
func (t *descriptionTransformer) foreignKeyTo(entityID, targetID string) string {
	if entityID == "" || targetID == "" {
		return ""
	}
	for _, rel := range t.model.Relationships {
		if (rel.From != entityID || rel.To != targetID) && (rel.From != targetID || rel.To != entityID) {
			continue
		}
		for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: rel.From, To: rel.To, Cardinality: rel.Cardinality}) {
			if fk.OwnerEntityID == entityID {
				return rel.ID
			}
		}
	}
	return ""
}

func (t *descriptionTransformer) addUniqueness(id, label, description string, targets []string, evidence EvidenceProposal) {
	key := strings.Join(sortedCopy(targets), ",")
	for _, existing := range t.model.ConstraintConcepts {
		if existing.Kind == "uniqueness" && strings.Join(sortedCopy(existing.Targets), ",") == key {
			return
		}
	}
	if len(evidence.SourceUnits) == 0 {
		return
	}
	t.model.ConstraintConcepts = append(t.model.ConstraintConcepts, ConceptualConstraintProposal{
		ID: t.uniqueID(id), Label: shortLabel(label, "Jedinstvenost"), Description: descriptionText(description, label),
		Kind: "uniqueness", Targets: targets, Evidence: evidence,
	})
}

func sortedCopy(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func (t *descriptionTransformer) mapRules() {
	for _, rule := range t.description.Rules {
		if rule.Kind == "uniqueness" {
			if owner := ruleOwner(rule.AppliesTo); owner != "" {
				if targets, ok := t.uniqueKey(owner, rule.AppliesTo); ok {
					t.addUniqueness("CON-"+upperID(nonEmpty(rule.ID, "uniqueness")), rule.Statement, rule.Statement, targets, t.evidence(rule.Evidence))
					continue
				}
			}
			t.warn("uniqueness rule %s could not be resolved to a key and is kept as an application rule", nonEmpty(rule.ID, rule.Statement))
		}
		targets := t.resolveTargets(rule.AppliesTo)
		evidence := t.evidence(rule.Evidence)
		if len(targets) == 0 || len(evidence.SourceUnits) == 0 {
			t.warn("rule %s has no resolvable target or evidence and is kept only in the description", nonEmpty(rule.ID, rule.Statement))
			continue
		}
		kind := ruleConstraintKind[rule.Kind]
		if kind == "" {
			kind = "application_enforced"
		}
		t.model.ConstraintConcepts = append(t.model.ConstraintConcepts, ConceptualConstraintProposal{
			ID: t.uniqueID("CON-" + upperID(nonEmpty(rule.ID, rule.Kind))), Label: shortLabel(rule.Statement, "Pravilo"),
			Description: descriptionText(rule.Statement, rule.Kind), Kind: kind, Targets: targets, Evidence: evidence,
		})
	}
}

// resolveTargets maps "thing" and "thing.property" references onto entity and
// attribute concept IDs; actors resolve through the thing that represents them.
// resolveRef splits "thing" or "thing.property" and returns the entity of the
// thing (an actor resolves to the thing that represents it) and the property.
func (t *descriptionTransformer) resolveRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	thingID, property := ref, ""
	if dot := strings.Index(ref, "."); dot > 0 {
		thingID, property = ref[:dot], ref[dot+1:]
	}
	if t.entityOf[thingID] == "" {
		for _, actor := range t.description.Actors {
			if actor.ID == thingID && actor.RepresentedBy != "" {
				thingID = actor.RepresentedBy
			}
		}
	}
	return t.entityOf[thingID], property
}

// resolveTargets resolves references to attributes where the property is an
// attribute, and to entities otherwise.
func (t *descriptionTransformer) resolveTargets(refs []string) []string {
	targets := []string{}
	for _, ref := range refs {
		entityID, property := t.resolveRef(ref)
		if entityID == "" {
			continue
		}
		if property != "" {
			if ids := t.attributes[entityID][snakeIdentifier(property)]; len(ids) > 0 {
				for _, id := range ids {
					targets = appendUnique(targets, id)
				}
				continue
			}
		}
		targets = appendUnique(targets, entityID)
	}
	return targets
}

// resolveEntities resolves references to the entities they read from.
func (t *descriptionTransformer) resolveEntities(refs []string) []string {
	entities := []string{}
	for _, ref := range refs {
		if entityID, _ := t.resolveRef(ref); entityID != "" {
			entities = appendUnique(entities, entityID)
		}
	}
	return entities
}

func (t *descriptionTransformer) mapQueries() {
	for _, query := range t.description.Queries {
		entities := t.resolveEntities(query.Needs)
		evidence := t.evidence(query.Evidence)
		if len(entities) == 0 || len(evidence.SourceUnits) == 0 {
			continue
		}
		t.model.DerivedConcepts = append(t.model.DerivedConcepts, PlanElementProposal{
			ID: t.uniqueID("DER-" + upperID(nonEmpty(query.ID, "query"))), Label: shortLabel(query.Description, "Pregled"),
			Description: descriptionText(query.Description, "Pregled"), Kind: "derived", Sources: entities, Metrics: []string{query.Description},
			SourceUnits: evidence.SourceUnits, RequirementAtoms: evidence.RequirementAtoms,
		})
	}
}

func (t *descriptionTransformer) mapImports() {
	for _, item := range t.description.Imports {
		evidence := t.evidence(item.Evidence)
		if len(evidence.SourceUnits) == 0 {
			continue
		}
		t.model.ImportConcepts = append(t.model.ImportConcepts, PlanElementProposal{
			ID: t.uniqueID("IMP-" + upperID(nonEmpty(item.ID, "import"))), Label: shortLabel(item.Description, "Uvoz"),
			Description: descriptionText(item.Description, "Uvoz podataka"), Kind: "import",
			SourceUnits: evidence.SourceUnits, RequirementAtoms: evidence.RequirementAtoms,
		})
	}
}

var (
	countNumber    = regexp.MustCompile(`\d+`)
	countManyToken = regexp.MustCompile(`(^|[^\p{L}])(n|m|\*)([^\p{L}]|$)`)
)

// countIsMany reads a count such as "1", "0..1", "0..N", "1..N" or "najviše 3".
// An empty count uses the given default.
func countIsMany(count string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(count))
	if value == "" {
		return fallback
	}
	if countManyToken.MatchString(value) {
		return true
	}
	words := strings.NewReplacer("najviše", "", "највише", "", "najvise", "").Replace(value)
	for _, word := range []string{"više", "vise", "више", "many", "mnogo", "много", "proizvolj", "произвољ"} {
		if strings.Contains(words, word) {
			return true
		}
	}
	for _, number := range countNumber.FindAllString(value, -1) {
		if n, err := strconv.Atoi(number); err == nil && n > 1 {
			return true
		}
	}
	return false
}

// countIsRequired is true when the lower bound of a count is at least one.
func countIsRequired(count string) bool {
	value := strings.TrimSpace(count)
	if value == "" || strings.HasPrefix(value, "0") {
		return false
	}
	lower := strings.ToLower(value)
	return !strings.Contains(lower, "najviše") && !strings.Contains(lower, "највише") && !strings.Contains(lower, "at most") && !strings.Contains(lower, "opcion")
}

// ruleOwner is the thing a rule's first "thing.property" reference belongs to.
func ruleOwner(refs []string) string {
	for _, ref := range refs {
		if dot := strings.Index(ref, "."); dot > 0 {
			return strings.TrimSpace(ref[:dot])
		}
	}
	return ""
}

// descriptionValueType maps a description value kind onto a conceptual value
// type; an empty result leaves the type to name-based inference in the mapper.
func descriptionValueType(kind string) string {
	switch kind {
	case "text":
		return "string"
	case "long_text":
		return "text"
	case "file":
		return "file_path"
	case "integer", "decimal", "money", "boolean", "date", "time", "datetime", "email", "phone", "url":
		return kind
	}
	return ""
}

var yesNoWords = map[string]bool{"da": true, "ne": true, "да": true, "не": true, "yes": true, "no": true, "true": true, "false": true}

func isYesNo(values []string) bool {
	if len(values) != 2 {
		return false
	}
	for _, value := range values {
		if !yesNoWords[strings.ToLower(strings.TrimSpace(value))] {
			return false
		}
	}
	return true
}

func upperID(value string) string {
	id := strings.ToUpper(strings.ReplaceAll(snakeIdentifier(value), "_", "-"))
	if id == "" {
		return "X"
	}
	return id
}

// shortLabel keeps a label to a short business name: the conceptual validator
// accepts at most six words and no question or exclamation marks.
func shortLabel(value, fallback string) string {
	value = strings.NewReplacer("?", "", "!", "").Replace(strings.TrimSpace(value))
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	words := strings.Fields(value)
	if len(words) > 6 {
		words = words[:6]
	}
	if len(words) == 0 {
		return "Element"
	}
	return strings.TrimRight(strings.Join(words, " "), ",;:.")
}

func descriptionText(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = strings.TrimSpace(fallback)
	}
	if value == "" {
		return "Opis nije naveden."
	}
	return value
}

func propertyDescription(property DescriptionProperty) string {
	text := descriptionText(property.Meaning, property.Name)
	notes := []string{}
	if property.Origin == "copied" && property.Source != "" {
		notes = append(notes, "vrednost se pamti u trenutku događaja, preuzeta iz "+property.Source)
	}
	if property.Origin == "generated" {
		notes = append(notes, "vrednost dodeljuje sistem")
	}
	if property.Presence == "conditional" && property.Condition != "" {
		notes = append(notes, "postoji kada: "+property.Condition)
	}
	if property.Variant != "" {
		notes = append(notes, "samo za varijantu: "+property.Variant)
	}
	if len(notes) > 0 {
		text += " (" + strings.Join(notes, "; ") + ")"
	}
	return text
}

// SegmentEvidenceArtifacts builds the requirement, functional and CRUD files the
// DB-DSL v0.5 bundle format still requires. In the segment-based flow they are
// not analysis results: one evidence atom per source unit, one functional area
// for the whole system, the actors of the description, and no operations.
func SegmentEvidenceArtifacts(units []dsl.SourceUnit, description ConceptualDescription, model ConceptualModelProposal) (RequirementAtomExtractionProposal, FunctionalAnalysisProposal, CRUDMappingProposal) {
	cited := map[string]bool{}
	mark := func(atoms []string) {
		for _, id := range atoms {
			cited[id] = true
		}
	}
	for _, entity := range model.EntityConcepts {
		mark(entity.Evidence.RequirementAtoms)
		for _, attribute := range entity.Attributes {
			mark(attribute.Evidence.RequirementAtoms)
		}
	}
	for _, rel := range model.Relationships {
		mark(rel.Evidence.RequirementAtoms)
	}
	for _, constraint := range model.ConstraintConcepts {
		mark(constraint.Evidence.RequirementAtoms)
	}
	for _, group := range [][]PlanElementProposal{model.LifecycleConcepts, model.DerivedConcepts, model.FileConcepts, model.ImportConcepts} {
		for _, item := range group {
			mark(item.RequirementAtoms)
		}
	}
	atoms := RequirementAtomExtractionProposal{RequirementAtoms: []RequirementAtomProposal{}, Warnings: []string{}, ConfidenceSummary: map[string]string{"strategy": "segment_evidence_v1"}}
	atomIDs, atomUnits := []string{}, []string{}
	for _, unit := range units {
		if !conceptualInputUnit(unit) || unit.Kind == "heading" {
			continue
		}
		id := SegmentAtomID(unit.ID)
		atom := RequirementAtomProposal{
			ID: id, Statement: unitText(unit), AtomType: "source_segment", SourceUnits: []string{unit.ID},
			SupportLevel: "explicit", Confidence: "high", FunctionalArea: "FA-001", FunctionalPattern: "domain_modeling",
			ReviewClass: ReviewClassNone, ReviewTopic: "none", ExampleRole: "none", Warnings: []string{}, ReviewDecisions: []string{},
			ModelingRelevance: "non_model", ModelingOutcome: "intentionally_not_in_db", PersistenceEffect: "not_required",
		}
		if cited[id] {
			atom.ModelingRelevance, atom.ModelingOutcome, atom.PersistenceEffect = "direct_db", "represented", "required"
		}
		atoms.RequirementAtoms = append(atoms.RequirementAtoms, atom)
		atomIDs = append(atomIDs, id)
		atomUnits = append(atomUnits, unit.ID)
	}
	actors := []ActorProposal{}
	actorIDs := []string{}
	seen := map[string]bool{}
	for _, actor := range description.Actors {
		id := snakeIdentifier(nonEmpty(actor.ID, actor.Name))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		actors = append(actors, ActorProposal{ID: id, Label: nonEmpty(strings.TrimSpace(actor.Name), id), Description: descriptionText(actor.Description, actor.Name), Kind: "human"})
		actorIDs = append(actorIDs, id)
	}
	if len(actors) == 0 {
		actors = append(actors, ActorProposal{ID: "sistem", Label: "Sistem", Description: "Opisani sistem.", Kind: "system"})
		actorIDs = append(actorIDs, "sistem")
	}
	sort.Strings(actorIDs)
	functional := FunctionalAnalysisProposal{
		FunctionalAreas: []FunctionalAreaProposal{{ID: "FA-001", Label: "Sistem", Purpose: "Ceo opisani sistem; oblasti se u toku zasnovanom na segmentima ne izdvajaju.", MainActors: actorIDs, Atoms: atomIDs, ModelingFocus: []string{}}},
		Actors:          actors, Warnings: []string{}, ConfidenceSummary: map[string]string{"strategy": "segment_evidence_v1"},
	}
	// One placeholder operation keeps the CRUD matrix well-formed; operations
	// are not analysed in this flow.
	crud := CRUDMappingProposal{Operations: []CRUDOperationProposal{{
		ID: "OP-001", Label: "Rad sa podacima sistema", ActorID: actorIDs[0], FunctionalAreaID: "FA-001",
		Creates: []string{}, Reads: []string{}, Updates: []string{}, Deletes: []string{}, PersistentData: []string{},
		Outcome: "Operacije se u toku zasnovanom na segmentima ne izdvajaju.", RequirementAtoms: atomIDs, SourceUnits: atomUnits, Warnings: []string{},
	}}, Warnings: []string{}, ConfidenceSummary: map[string]string{"strategy": "segment_evidence_v1"}}
	return atoms, functional, crud
}

// ReconcileSegmentAtomOutcomes aligns evidence atoms with the final logical
// model: an atom is represented exactly when some element of the model cites
// it. The mapper may fold or drop conceptual elements, so the outcome decided
// before mapping is not reliable.
func ReconcileSegmentAtomOutcomes(artifacts *LogicalArtifacts) {
	for i := range artifacts.RequirementAtoms.RequirementAtoms {
		atom := &artifacts.RequirementAtoms.RequirementAtoms[i]
		if countImpacts(atom.ModelImpacts) > 0 {
			atom.ModelingOutcome.Status = "represented"
			atom.ModelingRelevance = "direct_db"
			continue
		}
		atom.ModelingOutcome.Status = "intentionally_not_in_db"
		atom.ModelingRelevance = "non_model"
	}
}

func countImpacts(impacts dsl.RequirementModelImpacts) int {
	return len(impacts.Entities) + len(impacts.Attributes) + len(impacts.Relationships) + len(impacts.Constraints) +
		len(impacts.ImportSpecs) + len(impacts.StateMachines) + len(impacts.DerivedViews) + len(impacts.FileSpecs)
}
