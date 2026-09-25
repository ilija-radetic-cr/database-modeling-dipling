package llmpipeline

import (
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

// LogicalMappingRuleVersion identifies the deterministic conceptual-to-DB-DSL
// rule set. It is recorded in the mapping report so a logical model can be
// traced to the exact rules that produced it.
const LogicalMappingRuleVersion = "conceptual_to_dbdsl_v2"

// LogicalMappingReport is the audit trail of one deterministic projection:
// which rules fired, which values were inferred instead of declared, and which
// conceptual elements could not be represented in DDL.
type LogicalMappingReport struct {
	Strategy      string   `json:"strategy"`
	RuleVersion   string   `json:"rule_version"`
	Entities      int      `json:"entities"`
	Relationships int      `json:"relationships"`
	Constraints   int      `json:"constraints"`
	StateMachines int      `json:"state_machines"`
	DerivedViews  int      `json:"derived_views"`
	FileSpecs     int      `json:"file_specs"`
	Inferred      []string `json:"inferred"`
	Decisions     []string `json:"decisions"`
	Warnings      []string `json:"warnings"`
}

type mappedAttribute struct {
	proposal  AttributeProposal
	conceptID string
}

type mappedEntity struct {
	concept    ConceptualEntityProposal
	proposal   EntityProposal
	attributes []*mappedAttribute
	byName     map[string]*mappedAttribute
}

// LogicalMappingOptions carries project context the rules depend on.
type LogicalMappingOptions struct {
	// Language of the conceptual labels; English labels get plural table names,
	// Serbian labels keep the transliterated noun as written.
	Language string
}

type logicalMapper struct {
	options        LogicalMappingOptions
	model          ConceptualModelProposal
	atomSources    map[string]map[string]bool
	entities       []*mappedEntity
	entityByID     map[string]*mappedEntity
	attributeByID  map[string]*mappedAttribute
	attributeOwner map[string]string
	fileConcepts   map[string]PlanElementProposal
	relationships  []*RelationshipProposal
	constraints    []ConstraintProposal
	stateMachines  []StateMachineProposal
	derivedViews   []DerivedViewProposal
	fileSpecs      []FileSpecProposal
	report         LogicalMappingReport
	errors         []string
}

// MapConceptualToLogical projects an accepted conceptual model into DB-DSL
// patch operations without an LLM. Every relational decision (column names,
// types, foreign-key placement, nullability, link tables) follows fixed rules;
// values the conceptual model does not declare are inferred and reported.
func MapConceptualToLogical(model ConceptualModelProposal, atoms []RequirementAtomProposal, options LogicalMappingOptions) (PatchProposal, LogicalMappingReport, error) {
	m := &logicalMapper{
		options: options, model: model, atomSources: map[string]map[string]bool{}, entityByID: map[string]*mappedEntity{},
		attributeByID: map[string]*mappedAttribute{}, attributeOwner: map[string]string{}, fileConcepts: map[string]PlanElementProposal{},
		report: LogicalMappingReport{Strategy: "deterministic", RuleVersion: LogicalMappingRuleVersion, Inferred: []string{}, Decisions: []string{}, Warnings: []string{}},
	}
	for _, atom := range atoms {
		sources := map[string]bool{}
		for _, id := range atom.SourceUnits {
			sources[id] = true
		}
		m.atomSources[atom.ID] = sources
	}
	for _, concept := range model.FileConcepts {
		m.fileConcepts[concept.ID] = concept
	}
	m.mapEntities()
	m.mapRelationships()
	m.dropAttributesShadowingForeignKeys()
	m.mapConstraints()
	m.mapLifecycles()
	m.mapDerivedViews()
	for _, concept := range model.ImportConcepts {
		m.warn("import concept %s is not projected: import mappings are defined outside the relational schema", concept.ID)
	}
	m.preserveConceptualCoverage()
	if len(m.errors) > 0 {
		return PatchProposal{}, m.report, fmt.Errorf("conceptual model cannot be mapped deterministically: %s", strings.Join(m.errors, "; "))
	}
	return m.patch(), m.report, nil
}

func (m *logicalMapper) english() bool {
	return m.options.Language == "" || m.options.Language == LanguageEnglish
}

func (m *logicalMapper) tableName(label string) string {
	if m.english() {
		return pluralize(snakeIdentifier(label))
	}
	return snakeIdentifier(label)
}

func (m *logicalMapper) linkTableName(from, to string) string {
	if m.english() {
		return snakeIdentifier(singular(from) + "_" + to)
	}
	return snakeIdentifier(from + "_" + to)
}

func (m *logicalMapper) warn(format string, args ...any) {
	m.report.Warnings = append(m.report.Warnings, fmt.Sprintf(format, args...))
}

func (m *logicalMapper) decide(format string, args ...any) {
	m.report.Decisions = append(m.report.Decisions, fmt.Sprintf(format, args...))
}

// evidence keeps only citations the DB-DSL validator accepts: known atoms, and
// source units that belong to at least one cited atom. Empty evidence falls back
// to the owning element so every projected element stays traceable.
func (m *logicalMapper) evidence(proposal EvidenceProposal, fallback EvidenceProposal) EvidenceProposal {
	atoms := []string{}
	for _, id := range proposal.RequirementAtoms {
		if _, ok := m.atomSources[id]; ok {
			atoms = appendUnique(atoms, id)
		}
	}
	if len(atoms) == 0 && len(fallback.RequirementAtoms) > 0 {
		return m.evidence(fallback, EvidenceProposal{})
	}
	allowed := map[string]bool{}
	for _, id := range atoms {
		for source := range m.atomSources[id] {
			allowed[source] = true
		}
	}
	sources := []string{}
	for _, id := range proposal.SourceUnits {
		if allowed[id] {
			sources = appendUnique(sources, id)
		}
	}
	if len(sources) == 0 {
		for id := range allowed {
			sources = append(sources, id)
		}
		sort.Strings(sources)
	}
	out := proposal
	out.RequirementAtoms, out.SourceUnits = atoms, sources
	out.ReviewDecisions = append([]string{}, proposal.ReviewDecisions...)
	out.Notes = append([]string{}, proposal.Notes...)
	out.SupportLevel = nonEmpty(proposal.SupportLevel, "inferred")
	out.Confidence = nonEmpty(proposal.Confidence, "medium")
	return out
}

func mergeEvidenceInto(target *EvidenceProposal, extra EvidenceProposal) {
	for _, id := range extra.RequirementAtoms {
		target.RequirementAtoms = appendUnique(target.RequirementAtoms, id)
	}
	for _, id := range extra.SourceUnits {
		target.SourceUnits = appendUnique(target.SourceUnits, id)
	}
	for _, id := range extra.ReviewDecisions {
		target.ReviewDecisions = appendUnique(target.ReviewDecisions, id)
	}
}

func (m *logicalMapper) mapEntities() {
	tables := map[string]bool{}
	for _, concept := range m.model.EntityConcepts {
		kind := concept.Kind
		switch kind {
		case "regular", "lookup", "association":
		case "weak":
			kind = "regular"
			m.decide("%s: weak entity mapped to a regular table; its identifying relationship supplies the owner key", concept.ID)
		default:
			kind = "regular"
		}
		table := uniqueName(m.tableName(nonEmpty(concept.Label, strings.TrimPrefix(concept.ID, "ENT-"))), tables)
		entity := &mappedEntity{concept: concept, byName: map[string]*mappedAttribute{}, proposal: EntityProposal{
			ID: concept.ID, Label: concept.Label, Description: concept.Description, TableName: table, Kind: kind,
			Evidence: m.evidence(concept.Evidence, EvidenceProposal{}),
		}}
		surrogate := surrogateNames(concept.ID)
		for _, attribute := range concept.Attributes {
			name := attribute.Name
			if !lowerSnakeIdentifierPattern.MatchString(name) {
				name = fallbackColumnName(concept.ID, attribute)
				m.report.Inferred = append(m.report.Inferred, fmt.Sprintf("%s.%s: column name %q derived from the label or ID", concept.ID, attribute.ID, name))
			}
			if surrogate[name] {
				m.decide("%s.%s: surrogate identifier %q omitted; the table receives a generated primary key", concept.ID, attribute.ID, name)
				mergeEvidenceInto(&entity.proposal.Evidence, m.evidence(attribute.Evidence, EvidenceProposal{}))
				continue
			}
			if existing := entity.byName[name]; existing != nil {
				mergeEvidenceInto(&existing.proposal.Evidence, m.evidence(attribute.Evidence, entity.proposal.Evidence))
				m.decide("%s.%s: merged into column %q declared by another attribute", concept.ID, attribute.ID, name)
				m.attributeByID[attribute.ID] = existing
				m.attributeOwner[attribute.ID] = concept.ID
				continue
			}
			valueType := attribute.ValueType
			if !containsString(ConceptualValueTypes, valueType) {
				valueType = inferValueType(name, attribute.Label+" "+attribute.Description)
				m.report.Inferred = append(m.report.Inferred, fmt.Sprintf("%s.%s: type %s inferred from the name", concept.ID, name, valueType))
			}
			enumValues := append([]string(nil), attribute.EnumValues...)
			if len(enumValues) > 0 && valueType != "string" {
				m.warn("%s.%s: enum values dropped because type %s is not string", concept.ID, name, valueType)
				enumValues = nil
			}
			mapped := &mappedAttribute{conceptID: attribute.ID, proposal: AttributeProposal{
				ID: name, Label: attribute.Label, Description: attribute.Description, Type: valueType, Required: attribute.Required,
				SourceField: name, EnumValues: enumValues, Notes: []string{}, Evidence: m.evidence(attribute.Evidence, entity.proposal.Evidence),
			}}
			entity.attributes = append(entity.attributes, mapped)
			entity.byName[name] = mapped
			m.attributeByID[attribute.ID] = mapped
			m.attributeOwner[attribute.ID] = concept.ID
			if attribute.Unique {
				m.constraints = append(m.constraints, ConstraintProposal{
					ID:   fmt.Sprintf("CON-UQ-%s-%s", strings.TrimPrefix(concept.ID, "ENT-"), strings.ToUpper(strings.ReplaceAll(name, "_", "-"))),
					Type: "unique", Owner: concept.ID, Field: name, Description: attribute.Label + " is unique.", Evidence: mapped.proposal.Evidence,
				})
			}
		}
		m.entities = append(m.entities, entity)
		m.entityByID[concept.ID] = entity
	}
}

func (m *logicalMapper) mapRelationships() {
	type fkKey struct{ owner, field string }
	producers := map[fkKey]*RelationshipProposal{}
	for _, rel := range m.model.Relationships {
		if file, ok := m.fileConcepts[rel.To]; ok && m.entityByID[rel.From] != nil {
			m.mapFileReference(rel, rel.From, file)
			continue
		}
		if file, ok := m.fileConcepts[rel.From]; ok && m.entityByID[rel.To] != nil {
			m.mapFileReference(rel, rel.To, file)
			continue
		}
		from, to := m.entityByID[rel.From], m.entityByID[rel.To]
		if from == nil || to == nil {
			m.errors = append(m.errors, fmt.Sprintf("relationship %s references an unknown entity", rel.ID))
			continue
		}
		cardinality := rel.Cardinality
		if cardinality != "one_to_one" && cardinality != "one_to_many" && cardinality != "many_to_one" && cardinality != "many_to_many" {
			m.errors = append(m.errors, fmt.Sprintf("relationship %s has unresolved cardinality %q; decide it in the conceptual model", rel.ID, cardinality))
			continue
		}
		referenced := to
		if cardinality == "one_to_many" {
			referenced = from
		}
		if cardinality != "many_to_many" && referenced.proposal.Kind == "association" {
			referenced.proposal.Kind = "regular"
			m.decide("%s: association entity %s is referenced by %s, so it becomes a regular table with its own key", referenced.proposal.ID, referenced.proposal.ID, rel.ID)
		}
		required := true
		if rel.Required != nil {
			required = *rel.Required
		} else if cardinality != "many_to_many" {
			m.report.Inferred = append(m.report.Inferred, fmt.Sprintf("%s: reference assumed mandatory because the conceptual model does not declare optionality", rel.ID))
		}
		proposal := &RelationshipProposal{
			ID: rel.ID, Label: rel.Label, Description: rel.Description, From: rel.From, To: rel.To, Cardinality: cardinality,
			Required: required, FKRequired: required, OnDelete: "restrict", Notes: []string{},
			Evidence: m.evidence(rel.Evidence, from.proposal.Evidence),
		}
		if !required {
			proposal.OnDelete = "set_null"
		}
		if cardinality == "many_to_many" {
			if rel.From == rel.To {
				m.errors = append(m.errors, fmt.Sprintf("relationship %s links %s to itself many-to-many; model it as an association entity with two roles", rel.ID, rel.From))
				continue
			}
			proposal.Through = m.linkEntity(rel, from, to, proposal.Evidence)
			proposal.Required, proposal.FKRequired, proposal.OnDelete = true, true, "cascade"
		}
		duplicate := false
		for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: proposal.From, To: proposal.To, Cardinality: proposal.Cardinality, Through: proposal.Through}) {
			key := fkKey{fk.OwnerEntityID, fk.Field}
			if existing := producers[key]; existing != nil {
				mergeEvidenceInto(&existing.Evidence, proposal.Evidence)
				existing.Notes = append(existing.Notes, fmt.Sprintf("Also represents %s (%s).", rel.ID, rel.Label))
				m.decide("%s merged into %s: both would create foreign key %s.%s and DB-DSL has no role-named foreign keys", rel.ID, existing.ID, fk.OwnerEntityID, fk.Field)
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: proposal.From, To: proposal.To, Cardinality: proposal.Cardinality, Through: proposal.Through}) {
			producers[fkKey{fk.OwnerEntityID, fk.Field}] = proposal
		}
		m.relationships = append(m.relationships, proposal)
	}
}

// linkEntity creates the association table a plain many-to-many relationship
// needs; the generator derives its two foreign keys from the relationship.
func (m *logicalMapper) linkEntity(rel ConceptualRelationshipProposal, from, to *mappedEntity, evidence EvidenceProposal) string {
	id := fmt.Sprintf("ENT-%s-%s", strings.TrimPrefix(rel.From, "ENT-"), strings.TrimPrefix(rel.To, "ENT-"))
	if existing := m.entityByID[id]; existing != nil {
		return id
	}
	tables := map[string]bool{}
	for _, entity := range m.entities {
		tables[entity.proposal.TableName] = true
	}
	link := &mappedEntity{byName: map[string]*mappedAttribute{}, proposal: EntityProposal{
		ID: id, Label: from.proposal.Label + " " + to.proposal.Label + " link", TableName: uniqueName(m.linkTableName(from.proposal.TableName, to.proposal.TableName), tables),
		Description: "Association table for " + nonEmpty(rel.Label, rel.ID) + ".", Kind: "association", Evidence: evidence,
	}}
	m.entities = append(m.entities, link)
	m.entityByID[id] = link
	m.decide("%s: many-to-many relationship mapped through association table %s", rel.ID, link.proposal.TableName)
	return id
}

func (m *logicalMapper) mapFileReference(rel ConceptualRelationshipProposal, ownerID string, file PlanElementProposal) {
	owner := m.entityByID[ownerID]
	name := uniqueName(snakeIdentifier(nonEmpty(file.Label, strings.TrimPrefix(file.ID, "FILE-")))+"_path", attributeNames(owner))
	required := rel.Required != nil && *rel.Required
	evidence := m.evidence(rel.Evidence, owner.proposal.Evidence)
	attribute := &mappedAttribute{conceptID: file.ID, proposal: AttributeProposal{
		ID: name, Label: nonEmpty(file.Label, name), Description: nonEmpty(file.Description, "Stored file reference."), Type: "file_path",
		Required: required, SourceField: name, Notes: []string{}, Evidence: evidence,
	}}
	owner.attributes = append(owner.attributes, attribute)
	owner.byName[name] = attribute
	m.fileSpecs = append(m.fileSpecs, FileSpecProposal{ID: "FS-" + strings.TrimPrefix(file.ID, "FILE-"), Owner: ownerID, Field: name, Storage: "path", Notes: []string{}, Evidence: evidence})
	m.decide("%s: file concept %s mapped to file_path column %s.%s with a file spec", rel.ID, file.ID, ownerID, name)
}

// dropAttributesShadowingForeignKeys removes conceptual attributes that would
// duplicate a generated foreign-key column; their evidence moves to the
// relationship so the requirement stays realized.
func (m *logicalMapper) dropAttributesShadowingForeignKeys() {
	for _, rel := range m.relationships {
		for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: rel.From, To: rel.To, Cardinality: rel.Cardinality, Through: rel.Through}) {
			owner := m.entityByID[fk.OwnerEntityID]
			if owner == nil || owner.byName[fk.Field] == nil {
				continue
			}
			shadow := owner.byName[fk.Field]
			mergeEvidenceInto(&rel.Evidence, shadow.proposal.Evidence)
			delete(owner.byName, fk.Field)
			kept := owner.attributes[:0]
			for _, attribute := range owner.attributes {
				if attribute != shadow {
					kept = append(kept, attribute)
				}
			}
			owner.attributes = kept
			m.decide("%s.%s: attribute replaced by the foreign key generated for %s", fk.OwnerEntityID, fk.Field, rel.ID)
		}
	}
}

func (m *logicalMapper) mapConstraints() {
	for _, concept := range m.model.ConstraintConcepts {
		owner, fields := "", []string{}
		for _, target := range concept.Targets {
			if attribute, ok := m.attributeByID[target]; ok {
				if owner == "" {
					owner = m.attributeOwner[target]
				}
				if m.attributeOwner[target] == owner && m.entityByID[owner].byName[attribute.proposal.ID] == attribute {
					fields = appendUnique(fields, attribute.proposal.ID)
				}
			} else if m.entityByID[target] != nil && owner == "" {
				owner = target
			}
		}
		for _, target := range concept.Targets {
			for _, rel := range m.relationships {
				if rel.ID != target || rel.Cardinality == "many_to_many" {
					continue
				}
				for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: rel.From, To: rel.To, Cardinality: rel.Cardinality, Through: rel.Through}) {
					// A key made only of links belongs to the table that holds
					// their foreign keys.
					if owner == "" {
						owner = fk.OwnerEntityID
					}
					if fk.OwnerEntityID == owner {
						fields = appendUnique(fields, rel.ID)
					}
				}
			}
		}
		entity := m.entityByID[owner]
		evidence := EvidenceProposal{}
		if entity != nil {
			evidence = m.evidence(concept.Evidence, entity.proposal.Evidence)
		}
		switch {
		case concept.Kind == "uniqueness" && entity != nil && len(fields) > 0:
			constraint := ConstraintProposal{ID: concept.ID, Type: "unique", Owner: owner, Description: nonEmpty(concept.Description, concept.Label), Evidence: evidence}
			if len(fields) == 1 {
				constraint.Field = fields[0]
			} else {
				constraint.Fields = fields
			}
			m.constraints = append(m.constraints, constraint)
		case concept.Kind == "check" && entity != nil && strings.TrimSpace(concept.Expression) != "":
			m.constraints = append(m.constraints, ConstraintProposal{ID: concept.ID, Type: "check", Owner: owner, Fields: fields,
				Expression: strings.TrimSpace(concept.Expression), Description: nonEmpty(concept.Description, concept.Label), Evidence: evidence})
		default:
			m.recordApplicationRule(concept, owner, fields)
		}
	}
}

// recordApplicationRule keeps a rule that plain DDL cannot enforce visible on
// the columns (or table) it governs, instead of emitting an invalid constraint.
func (m *logicalMapper) recordApplicationRule(concept ConceptualConstraintProposal, owner string, fields []string) {
	note := fmt.Sprintf("Application-enforced rule %s: %s", concept.ID, nonEmpty(concept.Description, concept.Label))
	for _, target := range concept.Targets {
		for _, rel := range m.relationships {
			if rel.ID == target {
				mergeEvidenceInto(&rel.Evidence, m.evidence(concept.Evidence, rel.Evidence))
				rel.Notes = append(rel.Notes, note)
				m.decide("%s: %s rule recorded on relationship %s", concept.ID, concept.Kind, rel.ID)
				return
			}
		}
	}
	entity := m.entityByID[owner]
	if entity == nil {
		m.warn("%s (%s) has no resolvable target and is not represented in the logical model", concept.ID, concept.Kind)
		return
	}
	attached := false
	for _, field := range fields {
		if attribute := entity.byName[field]; attribute != nil {
			mergeEvidenceInto(&attribute.proposal.Evidence, m.evidence(concept.Evidence, attribute.proposal.Evidence))
			attribute.proposal.Notes = append(attribute.proposal.Notes, note)
			attached = true
		}
	}
	if !attached {
		mergeEvidenceInto(&entity.proposal.Evidence, m.evidence(concept.Evidence, entity.proposal.Evidence))
	}
	m.decide("%s: %s rule recorded as application-enforced on %s", concept.ID, concept.Kind, owner)
}

func (m *logicalMapper) mapLifecycles() {
	for _, lifecycle := range m.model.LifecycleConcepts {
		evidence := m.evidence(EvidenceProposal{SourceUnits: lifecycle.SourceUnits, RequirementAtoms: lifecycle.RequirementAtoms, SupportLevel: "explicit", Confidence: "high"}, EvidenceProposal{})
		owner := m.entityByID[lifecycle.Owner]
		field := lifecycle.Field
		if owner == nil {
			owner, field = m.findStatusAttribute(lifecycle)
		}
		if owner == nil {
			m.warn("lifecycle %s has no owner entity and is not projected", lifecycle.ID)
			continue
		}
		if !lowerSnakeIdentifierPattern.MatchString(field) {
			field = nonEmpty(snakeIdentifier(field), "status")
		}
		states := trimmedUniqueStrings(lifecycle.States)
		attribute := owner.byName[field]
		if len(states) == 0 && attribute != nil {
			states = trimmedUniqueStrings(attribute.proposal.EnumValues)
		}
		if len(states) == 0 {
			if attribute != nil {
				mergeEvidenceInto(&attribute.proposal.Evidence, evidence)
			}
			m.warn("lifecycle %s declares no states; only its evidence is attached to %s.%s", lifecycle.ID, owner.proposal.ID, field)
			continue
		}
		if attribute == nil {
			attribute = &mappedAttribute{conceptID: lifecycle.ID, proposal: AttributeProposal{ID: field, Label: "Status", Description: nonEmpty(lifecycle.Description, "Lifecycle state."),
				SourceField: field, Required: true, Notes: []string{}, Evidence: evidence}}
			owner.attributes = append(owner.attributes, attribute)
			owner.byName[field] = attribute
			m.decide("%s: status column %s.%s added for the lifecycle", lifecycle.ID, owner.proposal.ID, field)
		}
		attribute.proposal.Type = "string"
		attribute.proposal.EnumValues = trimmedUniqueStrings(append(append([]string{}, attribute.proposal.EnumValues...), states...))
		known := map[string]bool{}
		for _, state := range attribute.proposal.EnumValues {
			known[state] = true
		}
		initial := lifecycle.Initial
		if !known[initial] {
			initial = states[0]
		}
		terminal := []string{}
		for _, state := range lifecycle.Terminal {
			if known[state] {
				terminal = appendUnique(terminal, state)
			}
		}
		transitions := []dsl.StateTransition{}
		for _, transition := range lifecycle.Transitions {
			if known[transition.From] && known[transition.To] {
				transitions = append(transitions, dsl.StateTransition{From: transition.From, To: transition.To})
			}
		}
		m.stateMachines = append(m.stateMachines, StateMachineProposal{ID: lifecycle.ID, Owner: owner.proposal.ID, Field: field, States: attribute.proposal.EnumValues,
			Initial: initial, Terminal: terminal, Transitions: transitions, Notes: []string{}, Evidence: evidence})
	}
}

func (m *logicalMapper) findStatusAttribute(lifecycle PlanElementProposal) (*mappedEntity, string) {
	atoms := map[string]bool{}
	for _, id := range lifecycle.RequirementAtoms {
		atoms[id] = true
	}
	for _, entity := range m.entities {
		for _, attribute := range entity.attributes {
			if !strings.Contains(attribute.proposal.ID, "status") && !strings.Contains(attribute.proposal.ID, "state") {
				continue
			}
			for _, id := range attribute.proposal.Evidence.RequirementAtoms {
				if atoms[id] {
					m.report.Inferred = append(m.report.Inferred, fmt.Sprintf("%s: owner %s.%s inferred from shared evidence", lifecycle.ID, entity.proposal.ID, attribute.proposal.ID))
					return entity, attribute.proposal.ID
				}
			}
		}
	}
	return nil, ""
}

func (m *logicalMapper) mapDerivedViews() {
	for _, derived := range m.model.DerivedConcepts {
		sources := []string{}
		for _, id := range derived.Sources {
			if m.entityByID[id] != nil {
				sources = appendUnique(sources, id)
			}
		}
		if len(sources) == 0 {
			atoms := map[string]bool{}
			for _, id := range derived.RequirementAtoms {
				atoms[id] = true
			}
			for _, entity := range m.entities {
				for _, id := range entity.proposal.Evidence.RequirementAtoms {
					if atoms[id] {
						sources = appendUnique(sources, entity.proposal.ID)
					}
				}
			}
			if len(sources) == 0 {
				if entity := m.closestEntity(derived.RequirementAtoms, derived.SourceUnits, derived.Label); entity != nil {
					sources = []string{entity.proposal.ID}
				}
			}
			if len(sources) > 0 {
				m.report.Inferred = append(m.report.Inferred, fmt.Sprintf("%s: sources %s inferred from shared evidence", derived.ID, strings.Join(sources, ", ")))
			}
		}
		if len(sources) == 0 {
			m.warn("derived concept %s has no source entity and is not projected", derived.ID)
			continue
		}
		fallback := m.entityByID[sources[0]].proposal.Evidence
		m.derivedViews = append(m.derivedViews, DerivedViewProposal{ID: derived.ID, Label: derived.Label, Description: derived.Description,
			Kind: derivedViewKind(derived), Sources: sources, Persistence: "virtual", Metrics: append([]string{}, derived.Metrics...), Filters: []string{}, Notes: []string{},
			Evidence: m.evidence(EvidenceProposal{SourceUnits: derived.SourceUnits, RequirementAtoms: derived.RequirementAtoms, SupportLevel: "explicit", Confidence: "high"}, fallback)})
	}
}

// closestEntity picks the entity that shares the most source units with the
// given evidence, breaking ties by label words. It is only used to keep
// unprojectable conceptual elements traceable.
func (m *logicalMapper) closestEntity(atoms, sourceUnits []string, label string) *mappedEntity {
	sources := map[string]bool{}
	for _, id := range sourceUnits {
		sources[id] = true
	}
	for _, id := range atoms {
		for source := range m.atomSources[id] {
			sources[source] = true
		}
	}
	words := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(label)) {
		if len(word) > 3 {
			words[strings.TrimSuffix(word, "s")] = true
		}
	}
	var best *mappedEntity
	bestScore := 0
	for _, entity := range m.entities {
		score := 0
		for _, id := range entity.proposal.Evidence.SourceUnits {
			if sources[id] {
				score += 2
			}
		}
		for _, word := range strings.Fields(strings.ToLower(entity.proposal.Label)) {
			if words[strings.TrimSuffix(word, "s")] {
				score += 3
			}
		}
		if score > bestScore {
			best, bestScore = entity, score
		}
	}
	return best
}

// preserveConceptualCoverage guarantees that every requirement atom cited by
// the accepted conceptual model is still cited by some projected element; an
// atom whose conceptual element had no DDL form is attached to the closest table.
func (m *logicalMapper) preserveConceptualCoverage() {
	cited := map[string]bool{}
	mark := func(evidence EvidenceProposal) {
		for _, id := range evidence.RequirementAtoms {
			cited[id] = true
		}
	}
	for _, entity := range m.entities {
		mark(entity.proposal.Evidence)
		for _, attribute := range entity.attributes {
			mark(attribute.proposal.Evidence)
		}
	}
	for _, rel := range m.relationships {
		mark(rel.Evidence)
	}
	for _, item := range m.constraints {
		mark(item.Evidence)
	}
	for _, item := range m.stateMachines {
		mark(item.Evidence)
	}
	for _, item := range m.derivedViews {
		mark(item.Evidence)
	}
	for _, item := range m.fileSpecs {
		mark(item.Evidence)
	}
	type orphan struct {
		id, label string
		atoms     []string
		sources   []string
	}
	orphans := []orphan{}
	for _, item := range m.model.ConstraintConcepts {
		orphans = append(orphans, orphan{item.ID, item.Label, item.Evidence.RequirementAtoms, item.Evidence.SourceUnits})
	}
	for _, group := range [][]PlanElementProposal{m.model.LifecycleConcepts, m.model.DerivedConcepts, m.model.FileConcepts, m.model.ImportConcepts} {
		for _, item := range group {
			orphans = append(orphans, orphan{item.ID, item.Label, item.RequirementAtoms, item.SourceUnits})
		}
	}
	for _, item := range orphans {
		missing := []string{}
		for _, id := range item.atoms {
			if _, known := m.atomSources[id]; known && !cited[id] {
				missing = appendUnique(missing, id)
			}
		}
		if len(missing) == 0 {
			continue
		}
		entity := m.closestEntity(missing, item.sources, item.label)
		if entity == nil {
			m.warn("%s: atoms %s have no table to attach to", item.id, strings.Join(missing, ", "))
			continue
		}
		mergeEvidenceInto(&entity.proposal.Evidence, m.evidence(EvidenceProposal{RequirementAtoms: missing, SourceUnits: item.sources}, EvidenceProposal{}))
		for _, id := range missing {
			cited[id] = true
		}
		m.decide("%s: evidence for %s attached to table %s because the element has no relational form", item.id, strings.Join(missing, ", "), entity.proposal.ID)
	}
}

func fallbackColumnName(entityID string, attribute ConceptualAttributeProposal) string {
	if label := strings.TrimSpace(attribute.Label); label != "" && len(strings.Fields(label)) <= 4 && !strings.ContainsAny(label, "?!") {
		return snakeIdentifier(label)
	}
	name := snakeIdentifier(strings.TrimPrefix(strings.TrimPrefix(attribute.ID, "ATTR-"), "ATT-"))
	base := strings.TrimSuffix(dsl.GeneratedForeignKeyName(entityID), "_id")
	for _, prefix := range []string{base + "_", strings.SplitN(base, "_", 2)[0] + "_"} {
		if trimmed := strings.TrimPrefix(name, prefix); trimmed != name && trimmed != "" {
			return trimmed
		}
	}
	return name
}

func (m *logicalMapper) patch() PatchProposal {
	patch := PatchProposal{Operations: []PatchOperation{}, Warnings: append([]string{}, m.report.Warnings...), UnresolvedQuestions: []string{},
		ConfidenceSummary: map[string]string{"strategy": "deterministic_mapping", "rule_version": LogicalMappingRuleVersion}}
	for _, entity := range m.entities {
		proposal := entity.proposal
		proposal.Attributes = make([]AttributeProposal, 0, len(entity.attributes))
		for _, attribute := range entity.attributes {
			proposal.Attributes = append(proposal.Attributes, attribute.proposal)
		}
		patch.Operations = append(patch.Operations, PatchOperation{Operation: "add_entity", Entity: &proposal})
	}
	for _, rel := range m.relationships {
		patch.Operations = append(patch.Operations, PatchOperation{Operation: "add_relationship", Relationship: rel})
	}
	for i := range m.constraints {
		patch.Operations = append(patch.Operations, PatchOperation{Operation: "add_constraint", Constraint: &m.constraints[i]})
	}
	for i := range m.stateMachines {
		patch.Operations = append(patch.Operations, PatchOperation{Operation: "add_state_machine", StateMachine: &m.stateMachines[i]})
	}
	for i := range m.derivedViews {
		patch.Operations = append(patch.Operations, PatchOperation{Operation: "add_derived_view", DerivedView: &m.derivedViews[i]})
	}
	for i := range m.fileSpecs {
		patch.Operations = append(patch.Operations, PatchOperation{Operation: "add_file_spec", FileSpec: &m.fileSpecs[i]})
	}
	m.report.Entities, m.report.Relationships, m.report.Constraints = len(m.entities), len(m.relationships), len(m.constraints)
	m.report.StateMachines, m.report.DerivedViews, m.report.FileSpecs = len(m.stateMachines), len(m.derivedViews), len(m.fileSpecs)
	return patch
}

// inferValueType is the fallback for conceptual models that predate declared
// value types. It reads English column names, which the conceptual stage uses.
func inferValueType(name, text string) string {
	words := " " + strings.ReplaceAll(name, "_", " ") + " "
	has := func(tokens ...string) bool {
		for _, token := range tokens {
			if strings.Contains(words, " "+token+" ") || strings.HasPrefix(strings.TrimSpace(words), token+" ") {
				return true
			}
		}
		return false
	}
	switch {
	// English and transliterated Serbian column vocabulary.
	case has("email", "mail", "imejl"):
		return "email"
	case has("phone", "telephone", "mobile", "telefon", "mobilni"):
		return "phone"
	case has("url", "link", "website", "sajt"):
		return "url"
	case has("file", "image", "photo", "picture", "attachment", "path", "fajl", "slika", "fotografija", "prilog", "putanja"):
		return "file_path"
	case strings.HasPrefix(name, "is_") || strings.HasPrefix(name, "has_") || strings.HasPrefix(name, "da_li_") ||
		has("available", "active", "enabled", "flag", "accepted", "approved", "deleted", "blocked", "dostupan", "dostupno", "aktivan", "odobren", "obrisan", "blokiran"):
		return "boolean"
	case strings.HasSuffix(name, "_at") || has("timestamp", "datetime", "trenutak"):
		return "datetime"
	case has("date", "birthday", "day", "datum", "rodjenja", "dan"):
		return "date"
	case has("time", "vreme", "sat", "polazak", "dolazak"):
		return "time"
	case has("price", "cost", "amount", "fee", "total", "salary", "balance", "payment", "adjustment", "cena", "iznos", "ukupno", "ukupna", "plata", "uplata", "naknada", "doplata"):
		return "money"
	case has("percent", "percentage", "rate", "ratio", "discount", "procenat", "popust", "stopa"):
		return "decimal"
	case has("count", "quantity", "qty", "year", "age", "capacity", "seats", "points", "score", "rating", "duration", "minutes", "hours", "days",
		"kolicina", "godina", "starost", "kapacitet", "mesta", "poeni", "bodovi", "ocena", "trajanje", "minuta", "iskustvo"):
		return "integer"
	case has("description", "comment", "note", "notes", "text", "content", "body", "message", "details", "bio", "opis", "komentar", "napomena", "tekst", "sadrzaj", "poruka"):
		return "text"
	}
	return "string"
}

func derivedViewKind(derived PlanElementProposal) string {
	text := strings.ToLower(latinTransliteration.Replace(derived.Label + " " + derived.Description + " " + strings.Join(derived.Metrics, " ")))
	for _, token := range []string{"total", "sum", "count", "average", "statistic", "number of", "ukupn", "zbir", "prosek", "prosecn", "statistik", "broj "} {
		if strings.Contains(text, token) {
			return "aggregate"
		}
	}
	if strings.Contains(text, "report") || strings.Contains(text, "izvestaj") {
		return "report"
	}
	return "projection"
}

func surrogateNames(entityID string) map[string]bool {
	base := strings.TrimSuffix(dsl.GeneratedForeignKeyName(entityID), "_id")
	return map[string]bool{"id": true, "identifier": true, base + "_id": true, base + "_identifier": true}
}

var latinTransliteration = strings.NewReplacer(
	"č", "c", "ć", "c", "š", "s", "ž", "z", "đ", "dj", "Č", "C", "Ć", "C", "Š", "S", "Ž", "Z", "Đ", "Dj",
	"а", "a", "б", "b", "в", "v", "г", "g", "д", "d", "ђ", "dj", "е", "e", "ж", "z", "з", "z", "и", "i", "ј", "j",
	"к", "k", "л", "l", "љ", "lj", "м", "m", "н", "n", "њ", "nj", "о", "o", "п", "p", "р", "r", "с", "s", "т", "t",
	"ћ", "c", "у", "u", "ф", "f", "х", "h", "ц", "c", "ч", "c", "џ", "dz", "ш", "s",
	"А", "A", "Б", "B", "В", "V", "Г", "G", "Д", "D", "Ђ", "Dj", "Е", "E", "Ж", "Z", "З", "Z", "И", "I", "Ј", "J",
	"К", "K", "Л", "L", "Љ", "Lj", "М", "M", "Н", "N", "Њ", "Nj", "О", "O", "П", "P", "Р", "R", "С", "S", "Т", "T",
	"Ћ", "C", "У", "U", "Ф", "F", "Х", "H", "Ц", "C", "Ч", "C", "Џ", "Dz", "Ш", "S",
)

// snakeIdentifier converts a label into a lower snake_case identifier,
// transliterating Serbian Latin letters and dropping other non-ASCII runes.
func snakeIdentifier(value string) string {
	value = latinTransliteration.Replace(strings.TrimSpace(value))
	var out strings.Builder
	lastUnderscore := true
	for i, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z':
			if i > 0 && !lastUnderscore && isLowerASCII(value[i-1]) {
				out.WriteByte('_')
			}
			out.WriteRune(r + 'a' - 'A')
			lastUnderscore = false
		default:
			if !lastUnderscore {
				out.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	result := strings.Trim(out.String(), "_")
	if result != "" && result[0] >= '0' && result[0] <= '9' {
		result = "n_" + result
	}
	return result
}

func isLowerASCII(b byte) bool { return b >= 'a' && b <= 'z' }

func pluralize(name string) string {
	switch {
	case name == "":
		return name
	case strings.HasSuffix(name, "s"), strings.HasSuffix(name, "x"), strings.HasSuffix(name, "ch"), strings.HasSuffix(name, "sh"):
		if strings.HasSuffix(name, "ss") || !strings.HasSuffix(name, "s") {
			return name + "es"
		}
		return name
	case strings.HasSuffix(name, "y") && len(name) > 1 && !strings.ContainsRune("aeiou", rune(name[len(name)-2])):
		return name[:len(name)-1] + "ies"
	}
	return name + "s"
}

func singular(name string) string {
	switch {
	case strings.HasSuffix(name, "ies"):
		return name[:len(name)-3] + "y"
	case strings.HasSuffix(name, "ses"), strings.HasSuffix(name, "xes"):
		return name[:len(name)-2]
	case strings.HasSuffix(name, "s"):
		return name[:len(name)-1]
	}
	return name
}

func uniqueName(name string, used map[string]bool) string {
	if name == "" {
		name = "item"
	}
	candidate := name
	for i := 2; used[candidate]; i++ {
		candidate = fmt.Sprintf("%s_%d", name, i)
	}
	used[candidate] = true
	return candidate
}

func attributeNames(entity *mappedEntity) map[string]bool {
	out := map[string]bool{}
	for name := range entity.byName {
		out[name] = true
	}
	return out
}

func trimmedUniqueStrings(values []string) []string {
	out := []string{}
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = appendUnique(out, strings.TrimSpace(value))
		}
	}
	return out
}
