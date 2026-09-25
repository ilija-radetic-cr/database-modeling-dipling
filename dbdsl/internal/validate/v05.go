package validate

import (
	"fmt"
	"regexp"
	"strings"

	"dbdsl/internal/dsl"
)

var v05AttributeIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validateV05(bundle *dsl.V05Bundle) Result {
	validator := validatorV05{
		bundle:             bundle,
		doc:                bundle.Document,
		sourceUnitByID:     map[string]dsl.SourceUnit{},
		atomByID:           map[string]dsl.RequirementAtom{},
		reviewByID:         map[string]dsl.V05ReviewDecision{},
		functionalAreaByID: map[string]dsl.FunctionalArea{},
		actorByID:          map[string]dsl.CRUDActor{},
		operationByID:      map[string]dsl.CRUDOperation{},
		entityByID:         map[string]dsl.Entity{},
		attributeIDs:       map[string]map[string]bool{},
		generatedFKs:       map[string]map[string]bool{},
		elementByKind:      map[string]map[string]bool{},
		modelRefsByAtom:    map[string][]string{},
	}
	validator.run()
	return Result{Errors: validator.errors}
}

// ValidateV05Bundle validates an in-memory logical candidate before it is
// written to a project revision.
func ValidateV05Bundle(bundle *dsl.V05Bundle) Result {
	if bundle == nil || bundle.Document == nil || bundle.SourceUnits == nil || bundle.RequirementAtoms == nil || bundle.FunctionalDecomposition == nil || bundle.CRUDMatrix == nil || bundle.ReviewDecisions == nil {
		return Result{Errors: []string{"in-memory DB-DSL v0.5 bundle is incomplete"}}
	}
	// TopLevelKeys is populated by the YAML loader. In-memory candidates already
	// have every typed section, so synthesize the same parser metadata without
	// mutating the caller's document.
	document := *bundle.Document
	document.TopLevelKeys = map[string]bool{}
	for _, key := range []string{"dsl", "model", "source", "entities", "relationships", "constraints", "import_specs", "state_machines", "derived_views", "file_specs"} {
		document.TopLevelKeys[key] = true
	}
	candidate := *bundle
	candidate.Document = &document
	return validateV05(&candidate)
}

type validatorV05 struct {
	bundle *dsl.V05Bundle
	doc    *dsl.Document
	errors []string

	sourceUnitByID     map[string]dsl.SourceUnit
	atomByID           map[string]dsl.RequirementAtom
	reviewByID         map[string]dsl.V05ReviewDecision
	functionalAreaByID map[string]dsl.FunctionalArea
	actorByID          map[string]dsl.CRUDActor
	operationByID      map[string]dsl.CRUDOperation
	entityByID         map[string]dsl.Entity
	attributeIDs       map[string]map[string]bool
	generatedFKs       map[string]map[string]bool
	elementByKind      map[string]map[string]bool
	modelRefsByAtom    map[string][]string
}

func (v *validatorV05) run() {
	v.buildBundleIndexes()
	v.validateTopLevel()
	v.buildModelIndexes()
	v.validateEntities()
	v.validateRelationships()
	v.buildGeneratedFKs()
	v.validateConstraints()
	v.validateImportSpecs()
	v.validateStateMachines()
	v.validateDerivedViews()
	v.validateFileSpecs()
	v.validateEvidence()
	v.validateRequirementAtoms()
	v.validateFunctionalDecomposition()
	v.validateCRUDMatrix()
	v.validateReferenceableTargets()
}

func (v *validatorV05) buildBundleIndexes() {
	for _, unit := range v.bundle.SourceUnits.SourceUnits {
		if unit.ID == "" {
			v.add("source_units contains source unit with empty id")
			continue
		}
		if _, exists := v.sourceUnitByID[unit.ID]; exists {
			v.add("duplicate source unit id %s", unit.ID)
			continue
		}
		v.sourceUnitByID[unit.ID] = unit
	}

	for _, atom := range v.bundle.RequirementAtoms.RequirementAtoms {
		if atom.ID == "" {
			v.add("requirement_atoms contains atom with empty id")
			continue
		}
		if _, exists := v.atomByID[atom.ID]; exists {
			v.add("duplicate requirement atom id %s", atom.ID)
			continue
		}
		v.atomByID[atom.ID] = atom
	}

	for _, area := range v.bundle.FunctionalDecomposition.FunctionalAreas {
		if area.ID == "" {
			v.add("functional_decomposition contains functional area with empty id")
			continue
		}
		if _, exists := v.functionalAreaByID[area.ID]; exists {
			v.add("duplicate functional area id %s", area.ID)
			continue
		}
		v.functionalAreaByID[area.ID] = area
	}

	for _, actor := range v.bundle.CRUDMatrix.Actors {
		if actor.ID == "" {
			v.add("crud_matrix contains actor with empty id")
			continue
		}
		if _, exists := v.actorByID[actor.ID]; exists {
			v.add("duplicate CRUD actor id %s", actor.ID)
			continue
		}
		v.actorByID[actor.ID] = actor
	}

	for _, operation := range v.bundle.CRUDMatrix.Operations {
		if operation.ID == "" {
			v.add("crud_matrix contains operation with empty id")
			continue
		}
		if _, exists := v.operationByID[operation.ID]; exists {
			v.add("duplicate CRUD operation id %s", operation.ID)
			continue
		}
		v.operationByID[operation.ID] = operation
	}

	for _, review := range v.bundle.ReviewDecisions.ReviewDecisions {
		if review.ID == "" {
			v.add("review_decisions contains decision with empty id")
			continue
		}
		if _, exists := v.reviewByID[review.ID]; exists {
			v.add("duplicate review decision id %s", review.ID)
			continue
		}
		v.reviewByID[review.ID] = review
	}
}

func (v *validatorV05) validateTopLevel() {
	for _, key := range []string{"dsl", "model", "source", "entities", "relationships", "constraints", "import_specs", "state_machines", "derived_views", "file_specs"} {
		if !v.doc.TopLevelKeys[key] {
			v.add("missing top-level section %s", key)
		}
	}
	if v.doc.DSL.Name != "DB-DSL" {
		v.add("dsl.name must be DB-DSL")
	}
	if v.doc.DSL.Version != "0.5" {
		v.add("dsl.version must be 0.5 for v0.5 validator")
	}
	if v.doc.Model.ID == "" {
		v.add("model.id is required")
	}
	if v.doc.Model.Name == "" {
		v.add("model.name is required")
	}
	if v.doc.Model.Status == "" {
		v.add("model.status is required")
	}
	if v.doc.Model.Description == "" {
		v.add("model.description is required")
	}

	requiredSources := map[string]string{
		"source.source_units_file":             v.doc.Source.SourceUnitsFile,
		"source.requirement_atoms_file":        v.doc.Source.RequirementAtomsFile,
		"source.functional_decomposition_file": v.doc.Source.FunctionalDecompositionFile,
		"source.crud_matrix_file":              v.doc.Source.CRUDMatrixFile,
		"source.review_decisions_file":         v.doc.Source.ReviewDecisionsFile,
		"source.review_state":                  v.doc.Source.ReviewState,
	}
	for field, value := range requiredSources {
		if value == "" {
			v.add("%s is required", field)
		}
	}
}

func (v *validatorV05) buildModelIndexes() {
	v.elementByKind = map[string]map[string]bool{
		"entities":       {},
		"relationships":  {},
		"constraints":    {},
		"import_specs":   {},
		"state_machines": {},
		"derived_views":  {},
		"file_specs":     {},
	}

	for _, entity := range v.doc.Entities {
		if entity.ID == "" {
			v.add("entity id is required")
			continue
		}
		if _, exists := v.entityByID[entity.ID]; exists {
			v.add("duplicate entity id %s", entity.ID)
			continue
		}
		v.entityByID[entity.ID] = entity
		v.elementByKind["entities"][entity.ID] = true

		v.attributeIDs[entity.ID] = map[string]bool{}
		seenAttributes := map[string]bool{}
		for _, attribute := range entity.Attributes {
			if attribute.ID == "" {
				v.add("attribute id is required on entity %s", entity.ID)
				continue
			}
			if seenAttributes[attribute.ID] {
				v.add("duplicate attribute id %s.%s", entity.ID, attribute.ID)
				continue
			}
			seenAttributes[attribute.ID] = true
			v.attributeIDs[entity.ID][attribute.ID] = true
		}
	}

	v.indexIDs("relationship", "relationships", relationshipIDs(v.doc.Relationships))
	v.indexIDs("constraint", "constraints", constraintIDs(v.doc.Constraints))
	v.indexIDs("import spec", "import_specs", importSpecIDs(v.doc.ImportSpecs))
	v.indexIDs("state machine", "state_machines", stateMachineIDs(v.doc.StateMachines))
	v.indexIDs("derived view", "derived_views", derivedViewIDs(v.doc.DerivedViews))
	v.indexIDs("file spec", "file_specs", fileSpecIDs(v.doc.FileSpecs))
}

func (v *validatorV05) indexIDs(label, kind string, ids []string) {
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			v.add("%s id is required", label)
			continue
		}
		if seen[id] {
			v.add("duplicate %s id %s", label, id)
			continue
		}
		seen[id] = true
		v.elementByKind[kind][id] = true
	}
}

func (v *validatorV05) validateEntities() {
	validKinds := set("regular", "lookup", "association")
	validTypes := set("id", "string", "text", "integer", "decimal", "boolean", "date", "time", "datetime", "uuid", "email", "phone", "url", "file_path", "money")
	for _, entity := range v.doc.Entities {
		prefix := fmt.Sprintf("entity %s", displayID(entity.ID))
		v.requireString(prefix, "label", entity.Label)
		v.requireString(prefix, "description", entity.Description)
		v.requireString(prefix, "table_name", entity.TableName)
		v.requireString(prefix, "kind", entity.Kind)
		if entity.Kind != "" && !validKinds[entity.Kind] {
			v.add("%s has invalid kind %s", prefix, entity.Kind)
		}

		for _, attribute := range entity.Attributes {
			attrPrefix := fmt.Sprintf("attribute %s.%s", displayID(entity.ID), displayID(attribute.ID))
			if attribute.ID != "" && !v05AttributeIDPattern.MatchString(attribute.ID) {
				v.add("%s.id must be a lower snake_case column name", attrPrefix)
			}
			if (entity.Kind == "regular" || entity.Kind == "lookup") && strings.EqualFold(attribute.ID, "id") {
				v.add("%s uses reserved generated primary key id; omit this attribute because DB-DSL v0.5 generates it for %s entities", attrPrefix, entity.Kind)
			}
			v.requireString(attrPrefix, "label", attribute.Label)
			v.requireString(attrPrefix, "description", attribute.Description)
			v.requireString(attrPrefix, "type", attribute.Type)
			if attribute.Type != "" && !validTypes[attribute.Type] {
				v.add("%s has invalid type %s", attrPrefix, attribute.Type)
			}
			if attribute.Required == nil {
				v.add("%s.required is required", attrPrefix)
			}
			if !decimalLikeType(attribute.Type) && (attribute.Precision != nil || attribute.Scale != nil) {
				v.add("%s precision/scale may only be used with decimal or money type", attrPrefix)
			}
			if attribute.Type != "string" && len(attribute.EnumValues) > 0 {
				v.add("%s enum_values may only be used with string type", attrPrefix)
			}
		}
	}
}

func (v *validatorV05) validateRelationships() {
	validCardinalities := set("one_to_one", "one_to_many", "many_to_one", "many_to_many")
	validOnDelete := set("", "restrict", "cascade", "set_null")
	for _, relationship := range v.doc.Relationships {
		prefix := fmt.Sprintf("relationship %s", displayID(relationship.ID))
		v.requireString(prefix, "label", relationship.Label)
		v.requireString(prefix, "description", relationship.Description)
		v.requireString(prefix, "from", relationship.From)
		v.requireString(prefix, "to", relationship.To)
		v.requireString(prefix, "cardinality", relationship.Cardinality)
		if relationship.Required == nil {
			v.add("%s.required is required", prefix)
		}
		if relationship.Cardinality != "" && !validCardinalities[relationship.Cardinality] {
			v.add("%s has invalid cardinality %s", prefix, relationship.Cardinality)
		}
		if relationship.OnDelete != "" && !validOnDelete[relationship.OnDelete] {
			v.add("%s has invalid on_delete %s", prefix, relationship.OnDelete)
		}
		if relationship.OnDelete == "set_null" && effectiveFKRequired(relationship) {
			v.add("%s cannot use on_delete set_null with fk_required=true", prefix)
		}
		if relationship.From != "" && !v.hasEntity(relationship.From) {
			v.add("%s references unknown from entity %s", prefix, relationship.From)
		}
		if relationship.To != "" && !v.hasEntity(relationship.To) {
			v.add("%s references unknown to entity %s", prefix, relationship.To)
		}
		if relationship.Cardinality == "many_to_many" {
			if relationship.Through == "" {
				v.add("%s with many_to_many cardinality requires through", prefix)
				continue
			}
			through, ok := v.entityByID[relationship.Through]
			if !ok {
				v.add("%s references unknown through entity %s", prefix, relationship.Through)
				continue
			}
			if through.Kind != "association" {
				v.add("%s through entity %s must be kind: association", prefix, relationship.Through)
			}
		} else if relationship.Through != "" {
			v.add("%s has through but cardinality is not many_to_many", prefix)
		}
	}
}

func (v *validatorV05) buildGeneratedFKs() {
	for entityID := range v.entityByID {
		v.generatedFKs[entityID] = map[string]bool{}
	}
	for _, relationship := range v.doc.Relationships {
		for _, fk := range dsl.RelationshipForeignKeys(relationship) {
			if !v.hasEntity(fk.OwnerEntityID) {
				continue
			}
			if _, err := dsl.ResolveConstraintReference(v.doc, fk.OwnerEntityID, fk.Field); err != nil {
				v.add("relationship %s has invalid generated foreign key: %v", relationship.ID, err)
				continue
			}
			v.generatedFKs[fk.OwnerEntityID][fk.Field] = true
		}
	}
}

func (v *validatorV05) validateConstraints() {
	validTypes := set("required", "unique", "min_inclusive", "min_exclusive", "max_inclusive", "max_exclusive", "length", "regex", "check", "conditional_required")
	for _, constraint := range v.doc.Constraints {
		prefix := fmt.Sprintf("constraint %s", displayID(constraint.ID))
		v.requireString(prefix, "type", constraint.Type)
		v.requireString(prefix, "owner", constraint.Owner)
		v.requireString(prefix, "description", constraint.Description)
		if constraint.Type != "" && !validTypes[constraint.Type] {
			v.add("%s has invalid type %s", prefix, constraint.Type)
		}
		if constraint.Owner == "model" {
			if constraint.Type != "check" {
				v.add("%s model-level constraints must use type check", prefix)
			}
			if constraint.Expression == "" {
				v.add("%s requires expression", prefix)
			}
			continue
		}
		if constraint.Owner != "" && !v.hasEntity(constraint.Owner) {
			v.add("%s references unknown owner entity %s", prefix, constraint.Owner)
			continue
		}

		switch constraint.Type {
		case "required":
			if constraint.Field == "" {
				v.add("%s requires field", prefix)
			} else {
				resolved := v.validateSingleConstraintField(prefix, constraint.Owner, constraint.Field)
				if resolved.Kind == "relationship" || resolved.Kind == "generated_fk" {
					v.add("%s redundantly declares relationship requiredness; use relationship.required or relationship.fk_required", prefix)
				}
				if attribute := v.attribute(constraint.Owner, constraint.Field); attribute != nil && attribute.Required != nil && !*attribute.Required {
					v.add("%s requires %s.%s but attribute.required is false", prefix, constraint.Owner, constraint.Field)
				}
			}
		case "unique":
			fields := append([]string(nil), constraint.Fields...)
			if constraint.Field != "" {
				fields = append(fields, constraint.Field)
			}
			if len(fields) == 0 {
				v.add("%s requires field or non-empty fields", prefix)
			}
			for _, field := range fields {
				v.validateConstraintField(prefix, constraint.Owner, field)
			}
		case "min_inclusive", "min_exclusive", "max_inclusive", "max_exclusive":
			if constraint.Field == "" {
				v.add("%s requires field", prefix)
			} else {
				v.validateSingleConstraintField(prefix, constraint.Owner, constraint.Field)
			}
			if constraint.Value == nil && constraint.Min == nil && constraint.Max == nil {
				v.add("%s requires value, min, or max", prefix)
			}
		case "length":
			if constraint.Field == "" {
				v.add("%s requires field", prefix)
			} else {
				v.validateSingleConstraintField(prefix, constraint.Owner, constraint.Field)
			}
			if constraint.Value == nil && constraint.Min == nil && constraint.Max == nil {
				v.add("%s requires value, min, or max", prefix)
			}
		case "regex":
			if constraint.Field == "" {
				v.add("%s requires field", prefix)
			} else {
				v.validateSingleConstraintField(prefix, constraint.Owner, constraint.Field)
			}
			if constraint.Pattern == "" {
				v.add("%s requires pattern", prefix)
			} else if _, err := regexp.Compile(constraint.Pattern); err != nil {
				v.add("%s has invalid regex pattern: %v", prefix, err)
			}
		case "check":
			if constraint.Expression == "" {
				v.add("%s requires expression", prefix)
			}
		case "conditional_required":
			v.validateConditionalRequired(prefix, constraint)
		}
	}
}

func (v *validatorV05) validateConditionalRequired(prefix string, constraint dsl.Constraint) {
	if constraint.Condition == nil {
		v.add("%s requires condition", prefix)
	} else {
		validOperators := set("equals", "not_equals", "in", "not_in", "is_null", "is_not_null")
		if constraint.Condition.Field == "" {
			v.add("%s condition.field is required", prefix)
		} else {
			v.validateSingleConstraintField(prefix, constraint.Owner, constraint.Condition.Field)
		}
		if constraint.Condition.Operator == "" {
			v.add("%s condition.operator is required", prefix)
		} else if !validOperators[constraint.Condition.Operator] {
			v.add("%s condition has invalid operator %s", prefix, constraint.Condition.Operator)
		}
	}
	if len(constraint.Requires) == 0 {
		v.add("%s requires non-empty requires", prefix)
	}
	validRequirementKinds := set("field", "related_entity")
	for i, requirement := range constraint.Requires {
		reqPrefix := fmt.Sprintf("%s requires[%d]", prefix, i)
		if requirement.Kind == "" {
			v.add("%s.kind is required", reqPrefix)
			continue
		}
		if !validRequirementKinds[requirement.Kind] {
			v.add("%s has invalid kind %s", reqPrefix, requirement.Kind)
			continue
		}
		switch requirement.Kind {
		case "field":
			if requirement.Field == "" {
				v.add("%s.field is required", reqPrefix)
			} else {
				v.validateSingleConstraintField(reqPrefix, constraint.Owner, requirement.Field)
			}
		case "related_entity":
			if requirement.Entity == "" {
				v.add("%s.entity is required", reqPrefix)
			} else if !v.hasEntity(requirement.Entity) {
				v.add("%s references unknown entity %s", reqPrefix, requirement.Entity)
			}
		}
	}
}

func (v *validatorV05) validateImportSpecs() {
	validFormats := set("json", "csv", "sql_seed", "manual")
	for _, importSpec := range v.doc.ImportSpecs {
		prefix := fmt.Sprintf("import spec %s", displayID(importSpec.ID))
		v.requireString(prefix, "label", importSpec.Label)
		v.requireString(prefix, "description", importSpec.Description)
		v.requireString(prefix, "format", importSpec.Format)
		if importSpec.Format != "" && !validFormats[importSpec.Format] {
			v.add("%s has invalid format %s", prefix, importSpec.Format)
		}
		v.requireString(prefix, "source.source_id", importSpec.Source.SourceID)
		if importSpec.Source.Fragment == "" && len(importSpec.Source.SourceUnits) == 0 {
			v.add("%s source requires fragment or source_units", prefix)
		}
		for _, sourceUnit := range importSpec.Source.SourceUnits {
			v.requireSourceUnit(prefix+" source", sourceUnit)
		}
		v.requireString(prefix, "root", importSpec.Root)
		if len(importSpec.Mappings) == 0 {
			v.add("%s requires at least one mapping", prefix)
		}
		for i, mapping := range importSpec.Mappings {
			mappingPrefix := fmt.Sprintf("%s mapping[%d]", prefix, i)
			v.requireString(mappingPrefix, "source_path", mapping.SourcePath)
			v.requireString(mappingPrefix, "target", mapping.Target)
			if mapping.Target != "" {
				v.validateImportTarget(mappingPrefix, mapping.Target)
			}
		}
	}
}

func (v *validatorV05) validateStateMachines() {
	for _, machine := range v.doc.StateMachines {
		prefix := fmt.Sprintf("state machine %s", displayID(machine.ID))
		v.requireString(prefix, "owner", machine.Owner)
		v.requireString(prefix, "field", machine.Field)
		v.requireString(prefix, "initial", machine.Initial)
		if len(machine.States) == 0 {
			v.add("%s.states must not be empty", prefix)
		}
		if machine.Owner != "" && !v.hasEntity(machine.Owner) {
			v.add("%s references unknown owner entity %s", prefix, machine.Owner)
			continue
		}
		var attribute *dsl.Attribute
		if machine.Owner != "" && machine.Field != "" {
			attribute = v.attribute(machine.Owner, machine.Field)
			if attribute == nil {
				v.add("%s references unknown field %s.%s", prefix, machine.Owner, machine.Field)
			}
		}
		stateSet := set(machine.States...)
		if machine.Initial != "" && !stateSet[machine.Initial] {
			v.add("%s initial state %s is not in states", prefix, machine.Initial)
		}
		for _, terminal := range machine.Terminal {
			if !stateSet[terminal] {
				v.add("%s terminal state %s is not in states", prefix, terminal)
			}
		}
		for i, transition := range machine.Transitions {
			transitionPrefix := fmt.Sprintf("%s transition[%d]", prefix, i)
			if transition.From == "" {
				v.add("%s.from is required", transitionPrefix)
			} else if !stateSet[transition.From] {
				v.add("%s.from state %s is not in states", transitionPrefix, transition.From)
			}
			if transition.To == "" {
				v.add("%s.to is required", transitionPrefix)
			} else if !stateSet[transition.To] {
				v.add("%s.to state %s is not in states", transitionPrefix, transition.To)
			}
		}
		if attribute != nil && len(attribute.EnumValues) > 0 {
			enumSet := set(attribute.EnumValues...)
			for _, state := range machine.States {
				if !enumSet[state] {
					v.add("%s state %s is not in %s.%s enum_values", prefix, state, machine.Owner, machine.Field)
				}
			}
		}
	}
}

func (v *validatorV05) validateDerivedViews() {
	validKinds := set("projection", "aggregate", "report")
	validPersistence := set("virtual", "materialized_candidate")
	for _, view := range v.doc.DerivedViews {
		prefix := fmt.Sprintf("derived view %s", displayID(view.ID))
		v.requireString(prefix, "label", view.Label)
		v.requireString(prefix, "description", view.Description)
		v.requireString(prefix, "kind", view.Kind)
		v.requireString(prefix, "persistence", view.Persistence)
		if view.Kind != "" && !validKinds[view.Kind] {
			v.add("%s has invalid kind %s", prefix, view.Kind)
		}
		if view.Persistence != "" && !validPersistence[view.Persistence] {
			v.add("%s has invalid persistence %s", prefix, view.Persistence)
		}
		if len(view.Sources) == 0 {
			v.add("%s.sources must not be empty", prefix)
		}
		for _, source := range view.Sources {
			if !v.hasEntity(source) {
				v.add("%s references unknown source entity %s", prefix, source)
			}
		}
	}
}

func (v *validatorV05) validateFileSpecs() {
	validStorage := set("path", "url", "external_reference")
	for _, spec := range v.doc.FileSpecs {
		prefix := fmt.Sprintf("file spec %s", displayID(spec.ID))
		v.requireString(prefix, "owner", spec.Owner)
		v.requireString(prefix, "field", spec.Field)
		v.requireString(prefix, "storage", spec.Storage)
		if spec.Owner != "" && !v.hasEntity(spec.Owner) {
			v.add("%s references unknown owner entity %s", prefix, spec.Owner)
			continue
		}
		if spec.Owner != "" && spec.Field != "" {
			if attribute := v.attribute(spec.Owner, spec.Field); attribute == nil {
				v.add("%s references unknown field %s.%s", prefix, spec.Owner, spec.Field)
			}
		}
		if spec.Storage != "" && !validStorage[spec.Storage] {
			v.add("%s has invalid storage %s", prefix, spec.Storage)
		}
		if spec.AllowedExtensions != nil && len(spec.AllowedExtensions) == 0 {
			v.add("%s.allowed_extensions must not be empty when present", prefix)
		}
		if spec.MaxSizeMB != nil && *spec.MaxSizeMB <= 0 {
			v.add("%s.max_size_mb must be positive", prefix)
		}
	}
}

func (v *validatorV05) validateEvidence() {
	for _, entity := range v.doc.Entities {
		v.validateEvidenceObject(fmt.Sprintf("entity %s", displayID(entity.ID)), entity.Evidence)
		for _, attribute := range entity.Attributes {
			v.validateEvidenceObject(fmt.Sprintf("attribute %s.%s", displayID(entity.ID), displayID(attribute.ID)), attribute.Evidence)
		}
	}
	for _, relationship := range v.doc.Relationships {
		v.validateEvidenceObject(fmt.Sprintf("relationship %s", displayID(relationship.ID)), relationship.Evidence)
	}
	for _, constraint := range v.doc.Constraints {
		v.validateEvidenceObject(fmt.Sprintf("constraint %s", displayID(constraint.ID)), constraint.Evidence)
	}
	for _, importSpec := range v.doc.ImportSpecs {
		v.validateEvidenceObject(fmt.Sprintf("import spec %s", displayID(importSpec.ID)), importSpec.Evidence)
	}
	for _, machine := range v.doc.StateMachines {
		v.validateEvidenceObject(fmt.Sprintf("state machine %s", displayID(machine.ID)), machine.Evidence)
	}
	for _, view := range v.doc.DerivedViews {
		v.validateEvidenceObject(fmt.Sprintf("derived view %s", displayID(view.ID)), view.Evidence)
	}
	for _, spec := range v.doc.FileSpecs {
		v.validateEvidenceObject(fmt.Sprintf("file spec %s", displayID(spec.ID)), spec.Evidence)
	}
}

func (v *validatorV05) validateEvidenceObject(prefix string, evidence dsl.Evidence) {
	validSupportLevels := set("explicit", "example_based", "inferred", "assumption")
	validConfidences := set("high", "medium", "low")
	if len(evidence.SourceUnits) == 0 {
		v.add("%s evidence.source_units must not be empty", prefix)
	}
	if len(evidence.RequirementAtoms) == 0 {
		v.add("%s evidence.requirement_atoms must not be empty", prefix)
	}
	for _, sourceUnit := range evidence.SourceUnits {
		v.requireSourceUnit(prefix+" evidence", sourceUnit)
	}
	for _, atomID := range evidence.RequirementAtoms {
		_, ok := v.atomByID[atomID]
		if !ok {
			v.add("%s evidence references unknown requirement atom %s", prefix, atomID)
			continue
		}
		v.modelRefsByAtom[atomID] = append(v.modelRefsByAtom[atomID], prefix)
	}
	if len(evidence.SourceUnits) > 0 && len(evidence.RequirementAtoms) > 0 && !allSourceUnitsSupportedByAtoms(evidence.SourceUnits, evidence.RequirementAtoms, v.atomByID) {
		v.add("%s evidence source_units are not supported by requirement_atoms", prefix)
	}
	for _, reviewID := range evidence.ReviewDecisions {
		if _, ok := v.reviewByID[reviewID]; !ok {
			v.add("%s evidence references unknown review decision %s", prefix, reviewID)
		}
	}
	if evidence.SupportLevel == "" {
		v.add("%s evidence.support_level is required", prefix)
	} else if !validSupportLevels[evidence.SupportLevel] {
		v.add("%s evidence has invalid support_level %s", prefix, evidence.SupportLevel)
	}
	if evidence.Confidence == "" {
		v.add("%s evidence.confidence is required", prefix)
	} else if !validConfidences[evidence.Confidence] {
		v.add("%s evidence has invalid confidence %s", prefix, evidence.Confidence)
	}
	if evidence.SupportLevel == "assumption" && len(evidence.ReviewDecisions) == 0 {
		v.add("%s evidence with support_level assumption requires review_decisions", prefix)
	}
}

func (v *validatorV05) validateRequirementAtoms() {
	validSupportLevels := set("explicit", "example_based", "inferred", "assumption")
	validConfidences := set("high", "medium", "low")
	for _, atom := range v.bundle.RequirementAtoms.RequirementAtoms {
		prefix := fmt.Sprintf("requirement atom %s", displayID(atom.ID))
		v.requireString(prefix, "statement", atom.Statement)
		v.requireString(prefix, "atom_type", atom.AtomType)
		v.requireString(prefix, "modeling_relevance", atom.ModelingRelevance)
		v.requireString(prefix, "functional_area", atom.FunctionalArea)
		v.requireString(prefix, "functional_pattern", atom.FunctionalPattern)
		v.requireString(prefix, "support_level", atom.SupportLevel)
		v.requireString(prefix, "confidence", atom.Confidence)
		if atom.SupportLevel != "" && !validSupportLevels[atom.SupportLevel] {
			v.add("%s has invalid support_level %s", prefix, atom.SupportLevel)
		}
		if atom.Confidence != "" && !validConfidences[atom.Confidence] {
			v.add("%s has invalid confidence %s", prefix, atom.Confidence)
		}
		if len(atom.SourceUnits) == 0 {
			v.add("%s source_units must not be empty", prefix)
		}
		for _, sourceUnit := range atom.SourceUnits {
			v.requireSourceUnit(prefix, sourceUnit)
		}
		for _, reviewID := range atom.ReviewDecisions {
			if _, ok := v.reviewByID[reviewID]; !ok {
				v.add("%s references unknown review decision %s", prefix, reviewID)
			}
		}
		if atom.RequiresReview && len(atom.ReviewDecisions) == 0 {
			v.add("%s requires_review=true but has no review_decisions", prefix)
		}
		if atom.FunctionalArea != "" {
			if _, ok := v.functionalAreaByID[atom.FunctionalArea]; !ok {
				v.add("%s references unknown functional area %s", prefix, atom.FunctionalArea)
			}
		}
		if atom.ModelingOutcome.Status == "represented" {
			if countModelImpacts(atom.ModelImpacts) == 0 {
				v.add("%s is represented but has no model_impacts", prefix)
			}
			if len(v.modelRefsByAtom[atom.ID]) == 0 {
				v.add("%s is represented but no model element evidence references it", prefix)
			}
			v.validateModelImpacts(prefix, atom.ModelImpacts)
		}
	}

	for _, review := range v.bundle.ReviewDecisions.ReviewDecisions {
		prefix := fmt.Sprintf("review decision %s", displayID(review.ID))
		v.requireString(prefix, "question", review.Question)
		status, _ := review.Decision["status"].(string)
		selectedOption, _ := review.Decision["selected_option"].(string)
		if status == "" {
			v.add("%s decision.status is required", prefix)
		}
		if status == "pending" {
			v.add("%s is pending", prefix)
		}
		if selectedOption == "" {
			v.add("%s decision.selected_option is required", prefix)
		}
		for _, atomID := range review.AffectedAtoms {
			if _, ok := v.atomByID[atomID]; !ok {
				v.add("%s references unknown affected atom %s", prefix, atomID)
			}
		}
	}
}

func (v *validatorV05) validateModelImpacts(prefix string, impacts dsl.RequirementModelImpacts) {
	for _, id := range impacts.Entities {
		if !v.elementByKind["entities"][id] {
			v.add("%s model_impacts.entities references unknown entity %s", prefix, id)
		}
	}
	for _, ref := range impacts.Attributes {
		if !v.hasAttributeRef(ref) {
			v.add("%s model_impacts.attributes references unknown attribute %s", prefix, ref)
		}
	}
	for _, id := range impacts.Relationships {
		if !v.elementByKind["relationships"][id] {
			v.add("%s model_impacts.relationships references unknown relationship %s", prefix, id)
		}
	}
	for _, id := range impacts.Constraints {
		if !v.elementByKind["constraints"][id] {
			v.add("%s model_impacts.constraints references unknown constraint %s", prefix, id)
		}
	}
	for _, id := range impacts.ImportSpecs {
		if !v.elementByKind["import_specs"][id] {
			v.add("%s model_impacts.import_specs references unknown import spec %s", prefix, id)
		}
	}
	for _, id := range impacts.StateMachines {
		if !v.elementByKind["state_machines"][id] {
			v.add("%s model_impacts.state_machines references unknown state machine %s", prefix, id)
		}
	}
	for _, id := range impacts.DerivedViews {
		if !v.elementByKind["derived_views"][id] {
			v.add("%s model_impacts.derived_views references unknown derived view %s", prefix, id)
		}
	}
	for _, id := range impacts.FileSpecs {
		if !v.elementByKind["file_specs"][id] {
			v.add("%s model_impacts.file_specs references unknown file spec %s", prefix, id)
		}
	}
}

func (v *validatorV05) validateFunctionalDecomposition() {
	for _, area := range v.bundle.FunctionalDecomposition.FunctionalAreas {
		prefix := fmt.Sprintf("functional area %s", displayID(area.ID))
		v.requireString(prefix, "label", area.Label)
		v.requireString(prefix, "purpose", area.Purpose)
		for _, atomID := range area.Atoms {
			if _, ok := v.atomByID[atomID]; !ok {
				v.add("%s references unknown atom %s", prefix, atomID)
			}
		}
		for _, actorID := range area.MainActors {
			if _, ok := v.actorByID[actorID]; !ok {
				v.add("%s references unknown actor %s", prefix, actorID)
			}
		}
	}
}

func (v *validatorV05) validateCRUDMatrix() {
	if len(v.bundle.CRUDMatrix.Actors) == 0 {
		v.add("crud_matrix.actors must not be empty")
	}
	for _, actor := range v.bundle.CRUDMatrix.Actors {
		prefix := fmt.Sprintf("CRUD actor %s", displayID(actor.ID))
		v.requireString(prefix, "label", actor.Label)
		v.requireString(prefix, "description", actor.Description)
	}
	for _, operation := range v.bundle.CRUDMatrix.Operations {
		prefix := fmt.Sprintf("CRUD operation %s", displayID(operation.ID))
		v.requireString(prefix, "label", operation.Label)
		v.requireString(prefix, "functional_area", operation.FunctionalArea)
		v.requireString(prefix, "functional_pattern", operation.FunctionalPattern)
		v.requireString(prefix, "actor", operation.Actor)
		v.requireString(prefix, "description", operation.Description)
		if operation.Actor != "" {
			if _, ok := v.actorByID[operation.Actor]; !ok {
				v.add("%s references unknown actor %s", prefix, operation.Actor)
			}
		}
		if operation.FunctionalArea != "" {
			if _, ok := v.functionalAreaByID[operation.FunctionalArea]; !ok {
				v.add("%s references unknown functional area %s", prefix, operation.FunctionalArea)
			}
		}
		if len(operation.SourceAtoms) == 0 {
			v.add("%s source_atoms must not be empty", prefix)
		}
		for _, atomID := range operation.SourceAtoms {
			if _, ok := v.atomByID[atomID]; !ok {
				v.add("%s references unknown source atom %s", prefix, atomID)
			}
		}
		if len(operation.SourceUnits) == 0 {
			v.add("%s source_units must not be empty", prefix)
		}
		for _, sourceUnit := range operation.SourceUnits {
			v.requireSourceUnit(prefix, sourceUnit)
		}
		if len(operation.SourceAtoms) > 0 && !allSourceUnitsSupportedByAtoms(operation.SourceUnits, operation.SourceAtoms, v.atomByID) {
			v.add("%s source_units are not supported by source_atoms", prefix)
		}
	}

	seenRows := map[string]bool{}
	for _, row := range v.bundle.CRUDMatrix.Matrix {
		prefix := fmt.Sprintf("CRUD row %s", displayID(row.Entity))
		if row.Entity == "" {
			v.add("CRUD row entity is required")
			continue
		}
		if seenRows[row.Entity] {
			v.add("duplicate CRUD row for entity %s", row.Entity)
			continue
		}
		seenRows[row.Entity] = true
		entity, ok := v.entityByID[row.Entity]
		if !ok {
			v.add("%s references unknown entity", prefix)
		} else if row.Table != "" && row.Table != entity.TableName {
			v.add("%s table %s does not match entity table_name %s", prefix, row.Table, entity.TableName)
		}
		for operationID, actions := range row.Operations {
			if _, ok := v.operationByID[operationID]; !ok {
				v.add("%s references unknown operation %s", prefix, operationID)
			}
			for _, action := range actions {
				if !set("C", "R", "U", "D")[action] {
					v.add("%s operation %s has invalid CRUD action %s", prefix, operationID, action)
				}
			}
		}
	}
	for _, entity := range v.doc.Entities {
		if !seenRows[entity.ID] {
			v.add("entity %s has no CRUD matrix row", entity.ID)
		}
	}
}

func (v *validatorV05) validateReferenceableTargets() {
	for _, relationship := range v.doc.Relationships {
		switch relationship.Cardinality {
		case "many_to_one", "one_to_one":
			if target, ok := v.entityByID[relationship.To]; ok && target.Kind == "association" {
				v.add("relationship %s targets association entity %s without scalar id; model it as regular or avoid scalar FK", relationship.ID, relationship.To)
			}
		case "one_to_many":
			if target, ok := v.entityByID[relationship.From]; ok && target.Kind == "association" {
				v.add("relationship %s targets association entity %s without scalar id; model it as regular or avoid scalar FK", relationship.ID, relationship.From)
			}
		case "many_to_many":
			if target, ok := v.entityByID[relationship.From]; ok && target.Kind == "association" {
				v.add("relationship %s uses association entity %s as many-to-many endpoint without scalar id", relationship.ID, relationship.From)
			}
			if target, ok := v.entityByID[relationship.To]; ok && target.Kind == "association" {
				v.add("relationship %s uses association entity %s as many-to-many endpoint without scalar id", relationship.ID, relationship.To)
			}
		}
	}
}

func (v *validatorV05) validateConstraintField(prefix, owner, field string) dsl.ConstraintReference {
	if field == "" || owner == "" || owner == "model" {
		return dsl.ConstraintReference{}
	}
	resolved, err := dsl.ResolveConstraintReference(v.doc, owner, field)
	if err != nil {
		v.add("%s references invalid field %s.%s: %v", prefix, owner, field, err)
		return dsl.ConstraintReference{}
	}
	return resolved
}

func (v *validatorV05) validateSingleConstraintField(prefix, owner, field string) dsl.ConstraintReference {
	resolved := v.validateConstraintField(prefix, owner, field)
	if len(resolved.PhysicalFields) > 1 {
		v.add("%s reference %s expands to multiple physical fields and is only valid in a fields list", prefix, field)
	}
	return resolved
}

func (v *validatorV05) validateImportTarget(prefix, target string) {
	entityID, attributeID, ok := splitAttributeReference(target)
	if !ok {
		v.add("%s target must be Entity.attribute, got %s", prefix, target)
		return
	}
	if !v.hasEntity(entityID) {
		v.add("%s target references unknown entity %s", prefix, entityID)
		return
	}
	if !v.attributeIDs[entityID][attributeID] {
		v.add("%s target references unknown attribute %s", prefix, target)
	}
}

func (v *validatorV05) hasAttributeRef(ref string) bool {
	entityID, attributeID, ok := splitAttributeReference(ref)
	if !ok {
		return false
	}
	return v.attributeIDs[entityID][attributeID]
}

func (v *validatorV05) requireSourceUnit(prefix, sourceUnit string) {
	if sourceUnit == "" {
		v.add("%s references empty source unit", prefix)
		return
	}
	if _, ok := v.sourceUnitByID[sourceUnit]; !ok {
		v.add("%s references unknown source unit %s", prefix, sourceUnit)
	}
}

func allSourceUnitsSupportedByAtoms(sourceUnits, atomIDs []string, atoms map[string]dsl.RequirementAtom) bool {
	allowed := map[string]bool{}
	for _, atomID := range atomIDs {
		for _, sourceUnit := range atoms[atomID].SourceUnits {
			allowed[sourceUnit] = true
		}
	}
	for _, sourceUnit := range sourceUnits {
		if !allowed[sourceUnit] {
			return false
		}
	}
	return true
}

func (v *validatorV05) hasEntity(id string) bool {
	_, ok := v.entityByID[id]
	return ok
}

func (v *validatorV05) attribute(entityID, attributeID string) *dsl.Attribute {
	entity, ok := v.entityByID[entityID]
	if !ok {
		return nil
	}
	for i := range entity.Attributes {
		if entity.Attributes[i].ID == attributeID {
			return &entity.Attributes[i]
		}
	}
	return nil
}

func (v *validatorV05) requireString(prefix, field, value string) {
	if value == "" {
		v.add("%s.%s is required", prefix, field)
	}
}

func (v *validatorV05) add(format string, args ...any) {
	v.errors = append(v.errors, fmt.Sprintf(format, args...))
}

func countModelImpacts(impacts dsl.RequirementModelImpacts) int {
	return len(impacts.Entities) +
		len(impacts.Attributes) +
		len(impacts.Relationships) +
		len(impacts.Constraints) +
		len(impacts.ImportSpecs) +
		len(impacts.StateMachines) +
		len(impacts.DerivedViews) +
		len(impacts.FileSpecs)
}
