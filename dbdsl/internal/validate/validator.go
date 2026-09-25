package validate

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

type Result struct {
	Errors []string
}

func (r Result) OK() bool {
	return len(r.Errors) == 0
}

func ValidateFile(path string) Result {
	var errors []string

	doc, err := dsl.LoadDocument(path)
	if err != nil {
		return Result{Errors: []string{err.Error()}}
	}
	if doc.DSL.Version == "0.5" {
		bundle, err := dsl.LoadV05Bundle(path)
		if err != nil {
			return Result{Errors: []string{err.Error()}}
		}
		return validateV05(bundle)
	}

	sourcePath := dsl.ResolveReviewedSourcePath(path, doc.Source.ReviewedFragmentsFile)
	source, err := dsl.LoadReviewedSource(sourcePath)
	if err != nil {
		errors = append(errors, err.Error())
		source = &dsl.ReviewedSource{}
	}

	validator := validator{
		doc:    doc,
		source: source,
		errors: errors,
	}
	validator.run()

	return Result{Errors: validator.errors}
}

type validator struct {
	doc    *dsl.Document
	source *dsl.ReviewedSource
	errors []string

	entityByID       map[string]dsl.Entity
	attributeIDs     map[string]map[string]bool
	generatedFKs     map[string]map[string]bool
	sourceFragments  map[string]dsl.SourceFragment
	sourceReviewByID map[string]dsl.ReviewItem
}

func (v *validator) run() {
	v.buildSourceIndexes()
	v.validateReviewedSource()
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
}

func (v *validator) buildSourceIndexes() {
	v.sourceFragments = map[string]dsl.SourceFragment{}
	for _, fragment := range v.source.Fragments {
		if fragment.ID == "" {
			continue
		}
		v.sourceFragments[fragment.ID] = fragment
	}

	v.sourceReviewByID = map[string]dsl.ReviewItem{}
	for _, item := range v.source.ReviewItems {
		if item.ID == "" {
			continue
		}
		v.sourceReviewByID[item.ID] = item
	}
}

func (v *validator) validateReviewedSource() {
	if v.doc.Source.ReviewedFragmentsFile == "" {
		return
	}
	if v.source.ReviewState.AllRequiredReviewsResolved != nil && !*v.source.ReviewState.AllRequiredReviewsResolved {
		v.add("reviewed source has all_required_reviews_resolved=false")
	}
	if v.source.ReviewState.UnresolvedRequiresReviewFlags != nil && *v.source.ReviewState.UnresolvedRequiresReviewFlags != 0 {
		v.add("reviewed source has unresolved_requires_review_flags=%d", *v.source.ReviewState.UnresolvedRequiresReviewFlags)
	}
	for _, item := range v.source.ReviewItems {
		if item.ID == "" {
			v.add("reviewed source contains review item with empty id")
			continue
		}
		if item.Decision.Status == "pending" {
			v.add("review item %s is pending", item.ID)
		}
	}
}

func (v *validator) validateTopLevel() {
	required := []string{"dsl", "model", "source", "entities", "relationships", "constraints", "import_specs"}
	switch v.doc.DSL.Version {
	case "0.2":
		required = append(required, "state_machines", "derived_views", "file_specs")
	case "0.1", "":
	default:
		v.add("unsupported dsl.version %s", v.doc.DSL.Version)
	}
	for _, key := range required {
		if !v.doc.TopLevelKeys[key] {
			v.add("missing top-level section %s", key)
		}
	}

	if v.doc.DSL.Name != "DB-DSL" {
		v.add("dsl.name must be DB-DSL")
	}
	if v.doc.DSL.Version != "0.1" && v.doc.DSL.Version != "0.2" {
		v.add("dsl.version must be 0.1 or 0.2")
	}

	requiredModelFields := map[string]string{
		"id":          v.doc.Model.ID,
		"name":        v.doc.Model.Name,
		"status":      v.doc.Model.Status,
		"description": v.doc.Model.Description,
	}
	for field, value := range requiredModelFields {
		if value == "" {
			v.add("model.%s is required", field)
		}
	}

	if v.doc.Source.ReviewedFragmentsFile == "" {
		v.add("source.reviewed_fragments_file is required")
	}
	if v.doc.Source.ReviewState == "" {
		v.add("source.review_state is required")
	}
	if v.doc.Source.AcceptedReviewDecisions == nil {
		v.add("source.accepted_review_decisions is required")
	}
	for i, decision := range v.doc.Source.AcceptedReviewDecisions {
		if decision.ReviewID == "" {
			v.add("source.accepted_review_decisions[%d].review_id is required", i)
			continue
		}
		if _, ok := v.sourceReviewByID[decision.ReviewID]; !ok {
			v.add("source.accepted_review_decisions[%d] references unknown review %s", i, decision.ReviewID)
		}
		if decision.SelectedOption == "" {
			v.add("source.accepted_review_decisions[%d].selected_option is required", i)
		}
	}
}

func (v *validator) buildModelIndexes() {
	v.entityByID = map[string]dsl.Entity{}
	v.attributeIDs = map[string]map[string]bool{}

	seenEntities := map[string]bool{}
	for _, entity := range v.doc.Entities {
		if entity.ID == "" {
			v.add("entity id is required")
			continue
		}
		if seenEntities[entity.ID] {
			v.add("duplicate entity id %s", entity.ID)
			continue
		}
		seenEntities[entity.ID] = true
		v.entityByID[entity.ID] = entity

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

	checkUniqueIDs(v, "relationship", relationshipIDs(v.doc.Relationships))
	checkUniqueIDs(v, "constraint", constraintIDs(v.doc.Constraints))
	checkUniqueIDs(v, "import spec", importSpecIDs(v.doc.ImportSpecs))
	checkUniqueIDs(v, "state machine", stateMachineIDs(v.doc.StateMachines))
	checkUniqueIDs(v, "derived view", derivedViewIDs(v.doc.DerivedViews))
	checkUniqueIDs(v, "file spec", fileSpecIDs(v.doc.FileSpecs))
}

func (v *validator) validateEntities() {
	validKinds := set("regular", "lookup", "association")
	validTypes := set("id", "string", "text", "integer", "decimal", "boolean")
	if v.isV02() {
		validTypes = set("id", "string", "text", "integer", "decimal", "boolean", "date", "time", "datetime", "uuid", "email", "phone", "url", "file_path", "money")
	}

	for _, entity := range v.doc.Entities {
		prefix := fmt.Sprintf("entity %s", displayID(entity.ID))
		requireString(v, prefix, "label", entity.Label)
		requireString(v, prefix, "description", entity.Description)
		requireString(v, prefix, "table_name", entity.TableName)
		requireString(v, prefix, "kind", entity.Kind)
		if entity.Kind != "" && !validKinds[entity.Kind] {
			v.add("%s has invalid kind %s", prefix, entity.Kind)
		}

		for _, attribute := range entity.Attributes {
			attrPrefix := fmt.Sprintf("attribute %s.%s", displayID(entity.ID), displayID(attribute.ID))
			requireString(v, attrPrefix, "label", attribute.Label)
			requireString(v, attrPrefix, "description", attribute.Description)
			requireString(v, attrPrefix, "type", attribute.Type)
			if attribute.Type != "" && !validTypes[attribute.Type] {
				v.add("%s has invalid type %s", attrPrefix, attribute.Type)
			}
			if attribute.Required == nil {
				v.add("%s.required is required", attrPrefix)
			}
			if !decimalLikeType(attribute.Type) && (attribute.Precision != nil || attribute.Scale != nil) {
				if v.isV02() {
					v.add("%s precision/scale may only be used with decimal or money type", attrPrefix)
				} else {
					v.add("%s precision/scale may only be used with decimal type", attrPrefix)
				}
			}
			if attribute.Type != "string" && len(attribute.EnumValues) > 0 {
				v.add("%s enum_values may only be used with string type", attrPrefix)
			}
		}
	}
}

func (v *validator) validateRelationships() {
	validCardinalities := set("one_to_one", "one_to_many", "many_to_one", "many_to_many")
	validOnDelete := set("", "restrict", "cascade", "set_null")

	for _, relationship := range v.doc.Relationships {
		prefix := fmt.Sprintf("relationship %s", displayID(relationship.ID))
		requireString(v, prefix, "label", relationship.Label)
		requireString(v, prefix, "description", relationship.Description)
		requireString(v, prefix, "from", relationship.From)
		requireString(v, prefix, "to", relationship.To)
		requireString(v, prefix, "cardinality", relationship.Cardinality)
		if relationship.Required == nil {
			v.add("%s.required is required", prefix)
		}
		if relationship.Cardinality != "" && !validCardinalities[relationship.Cardinality] {
			v.add("%s has invalid cardinality %s", prefix, relationship.Cardinality)
		}
		if relationship.OnDelete != "" && !v.isV02() {
			v.add("%s.on_delete is only supported in DB-DSL v0.2", prefix)
		}
		if relationship.FKRequired != nil && !v.isV02() {
			v.add("%s.fk_required is only supported in DB-DSL v0.2", prefix)
		}
		if relationship.Identifying != nil && !v.isV02() {
			v.add("%s.identifying is only supported in DB-DSL v0.2", prefix)
		}
		if v.isV02() {
			if !validOnDelete[relationship.OnDelete] {
				v.add("%s has invalid on_delete %s", prefix, relationship.OnDelete)
			}
			if relationship.OnDelete == "set_null" && effectiveFKRequired(relationship) {
				v.add("%s cannot use on_delete set_null with fk_required=true", prefix)
			}
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

func (v *validator) buildGeneratedFKs() {
	v.generatedFKs = map[string]map[string]bool{}
	for entityID := range v.entityByID {
		v.generatedFKs[entityID] = map[string]bool{}
	}

	for _, relationship := range v.doc.Relationships {
		switch relationship.Cardinality {
		case "many_to_one", "one_to_one":
			if v.hasEntity(relationship.From) && v.hasEntity(relationship.To) {
				v.generatedFKs[relationship.From][fkName(relationship.To)] = true
			}
		case "one_to_many":
			if v.hasEntity(relationship.From) && v.hasEntity(relationship.To) {
				v.generatedFKs[relationship.To][fkName(relationship.From)] = true
			}
		case "many_to_many":
			if relationship.Through != "" && v.hasEntity(relationship.Through) {
				if v.hasEntity(relationship.From) {
					v.generatedFKs[relationship.Through][fkName(relationship.From)] = true
				}
				if v.hasEntity(relationship.To) {
					v.generatedFKs[relationship.Through][fkName(relationship.To)] = true
				}
			}
		}
	}
}

func (v *validator) validateConstraints() {
	validTypes := set("required", "unique", "min_inclusive", "min_exclusive")
	if v.isV02() {
		validTypes = set("required", "unique", "min_inclusive", "min_exclusive", "max_inclusive", "max_exclusive", "length", "regex", "check", "conditional_required")
	}

	for _, constraint := range v.doc.Constraints {
		prefix := fmt.Sprintf("constraint %s", displayID(constraint.ID))
		requireString(v, prefix, "type", constraint.Type)
		requireString(v, prefix, "owner", constraint.Owner)
		requireString(v, prefix, "description", constraint.Description)
		if constraint.Type != "" && !validTypes[constraint.Type] {
			v.add("%s has invalid type %s", prefix, constraint.Type)
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
				v.validateConstraintField(prefix, constraint.Owner, constraint.Field)
			}
		case "unique":
			if len(constraint.Fields) == 0 {
				v.add("%s requires non-empty fields", prefix)
			}
			for _, field := range constraint.Fields {
				v.validateConstraintField(prefix, constraint.Owner, field)
			}
		case "min_inclusive", "min_exclusive", "max_inclusive", "max_exclusive":
			if constraint.Field == "" {
				v.add("%s requires field", prefix)
			} else {
				v.validateConstraintField(prefix, constraint.Owner, constraint.Field)
			}
			if constraint.Value == nil {
				v.add("%s requires value", prefix)
			}
		case "length":
			if constraint.Field == "" {
				v.add("%s requires field", prefix)
			} else {
				v.validateConstraintField(prefix, constraint.Owner, constraint.Field)
			}
			if constraint.Value == nil && constraint.Min == nil && constraint.Max == nil {
				v.add("%s requires value, min, or max", prefix)
			}
		case "regex":
			if constraint.Field == "" {
				v.add("%s requires field", prefix)
			} else {
				v.validateConstraintField(prefix, constraint.Owner, constraint.Field)
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

func (v *validator) validateConditionalRequired(prefix string, constraint dsl.Constraint) {
	if constraint.Condition == nil {
		v.add("%s requires condition", prefix)
	} else {
		validOperators := set("equals", "not_equals", "in", "not_in", "is_null", "is_not_null")
		if constraint.Condition.Field == "" {
			v.add("%s condition.field is required", prefix)
		} else {
			v.validateConstraintField(prefix, constraint.Owner, constraint.Condition.Field)
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
				v.validateConstraintField(reqPrefix, constraint.Owner, requirement.Field)
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

func (v *validator) validateConstraintField(prefix, owner, field string) {
	if field == "" || owner == "" {
		return
	}
	if v.attributeIDs[owner][field] {
		return
	}
	if v.generatedFKs[owner][field] {
		return
	}
	v.add("%s references unknown field %s.%s", prefix, owner, field)
}

func (v *validator) validateImportSpecs() {
	validFormats := set("json")
	if v.isV02() {
		validFormats = set("json", "csv", "sql_seed", "manual")
	}
	for _, importSpec := range v.doc.ImportSpecs {
		prefix := fmt.Sprintf("import spec %s", displayID(importSpec.ID))
		requireString(v, prefix, "label", importSpec.Label)
		requireString(v, prefix, "description", importSpec.Description)
		requireString(v, prefix, "format", importSpec.Format)
		if importSpec.Format != "" && !validFormats[importSpec.Format] {
			v.add("%s has invalid format %s", prefix, importSpec.Format)
		}
		requireString(v, prefix, "source.fragment", importSpec.Source.Fragment)
		requireString(v, prefix, "source.source_id", importSpec.Source.SourceID)
		requireString(v, prefix, "root", importSpec.Root)
		if len(importSpec.Mappings) == 0 {
			v.add("%s requires at least one mapping", prefix)
		}
		for i, mapping := range importSpec.Mappings {
			mappingPrefix := fmt.Sprintf("%s mapping[%d]", prefix, i)
			requireString(v, mappingPrefix, "source_path", mapping.SourcePath)
			requireString(v, mappingPrefix, "target", mapping.Target)
			if mapping.Target != "" {
				v.validateImportTarget(mappingPrefix, mapping.Target)
			}
		}
	}
}

func (v *validator) validateStateMachines() {
	for _, machine := range v.doc.StateMachines {
		prefix := fmt.Sprintf("state machine %s", displayID(machine.ID))
		if !v.isV02() {
			v.add("%s is only supported in DB-DSL v0.2", prefix)
		}
		requireString(v, prefix, "owner", machine.Owner)
		requireString(v, prefix, "field", machine.Field)
		requireString(v, prefix, "initial", machine.Initial)
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

func (v *validator) validateDerivedViews() {
	validKinds := set("projection", "aggregate", "report")
	validPersistence := set("virtual", "materialized_candidate")
	for _, view := range v.doc.DerivedViews {
		prefix := fmt.Sprintf("derived view %s", displayID(view.ID))
		if !v.isV02() {
			v.add("%s is only supported in DB-DSL v0.2", prefix)
		}
		requireString(v, prefix, "label", view.Label)
		requireString(v, prefix, "description", view.Description)
		requireString(v, prefix, "kind", view.Kind)
		requireString(v, prefix, "persistence", view.Persistence)
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

func (v *validator) validateFileSpecs() {
	validStorage := set("path", "url", "external_reference")
	for _, spec := range v.doc.FileSpecs {
		prefix := fmt.Sprintf("file spec %s", displayID(spec.ID))
		if !v.isV02() {
			v.add("%s is only supported in DB-DSL v0.2", prefix)
		}
		requireString(v, prefix, "owner", spec.Owner)
		requireString(v, prefix, "field", spec.Field)
		requireString(v, prefix, "storage", spec.Storage)
		if spec.Owner != "" && !v.hasEntity(spec.Owner) {
			v.add("%s references unknown owner entity %s", prefix, spec.Owner)
			continue
		}
		if spec.Owner != "" && spec.Field != "" {
			attribute := v.attribute(spec.Owner, spec.Field)
			if attribute == nil {
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

func (v *validator) validateImportTarget(prefix, target string) {
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

// splitAttributeReference separates a qualified attribute reference at its
// final dot. Entity IDs may themselves be namespaced (for example,
// entity.user-account), while attribute IDs are scalar lower-snake-case names
// and therefore cannot contain dots.
func splitAttributeReference(ref string) (entityID, attributeID string, ok bool) {
	separator := strings.LastIndex(ref, ".")
	if separator <= 0 || separator == len(ref)-1 {
		return "", "", false
	}
	return ref[:separator], ref[separator+1:], true
}

func (v *validator) validateEvidence() {
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

func (v *validator) validateEvidenceObject(prefix string, evidence dsl.Evidence) {
	validSupportLevels := set("explicit", "example_based", "inferred", "assumption")
	validConfidences := set("high", "medium", "low")

	if len(evidence.Fragments) == 0 {
		v.add("%s evidence.fragments must not be empty", prefix)
	}
	for _, fragmentID := range evidence.Fragments {
		if _, ok := v.sourceFragments[fragmentID]; !ok {
			v.add("%s evidence references unknown fragment %s", prefix, fragmentID)
		}
	}
	for _, reviewID := range evidence.ReviewDecisions {
		if _, ok := v.sourceReviewByID[reviewID]; !ok {
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

func (v *validator) hasEntity(id string) bool {
	_, ok := v.entityByID[id]
	return ok
}

func (v *validator) isV02() bool {
	return v.doc.DSL.Version == "0.2"
}

func (v *validator) attribute(entityID, attributeID string) *dsl.Attribute {
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

func (v *validator) add(format string, args ...any) {
	v.errors = append(v.errors, fmt.Sprintf(format, args...))
}

func requireString(v *validator, prefix, field, value string) {
	if value == "" {
		v.add("%s.%s is required", prefix, field)
	}
}

func checkUniqueIDs(v *validator, label string, ids []string) {
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" {
			v.add("%s id is required", label)
			continue
		}
		if seen[id] {
			v.add("duplicate %s id %s", label, id)
		}
		seen[id] = true
	}
}

func relationshipIDs(items []dsl.Relationship) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func constraintIDs(items []dsl.Constraint) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func importSpecIDs(items []dsl.ImportSpec) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func stateMachineIDs(items []dsl.StateMachine) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func derivedViewIDs(items []dsl.DerivedView) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func fileSpecIDs(items []dsl.FileSpec) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	return ids
}

func set(values ...string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, value := range values {
		result[value] = true
	}
	return result
}

func decimalLikeType(attributeType string) bool {
	return attributeType == "decimal" || attributeType == "money"
}

func effectiveFKRequired(relationship dsl.Relationship) bool {
	if relationship.FKRequired != nil {
		return *relationship.FKRequired
	}
	return relationship.Required != nil && *relationship.Required
}

func fkName(entityID string) string {
	return dsl.GeneratedForeignKeyName(entityID)
}

var snakeBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)

func toSnake(value string) string {
	return strings.ToLower(snakeBoundary.ReplaceAllString(value, `${1}_${2}`))
}

func displayID(id string) string {
	if id == "" {
		return "<empty>"
	}
	return id
}

func SortedErrors(errors []string) []string {
	result := append([]string(nil), errors...)
	sort.Strings(result)
	return result
}
