package llmpipeline

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"dbdsl/internal/dsl"
)

// LogicalMappingRuleVersion identifies the deterministic conceptual-to-DB-DSL
// rule set. It is recorded in the mapping report so a logical model can be
// traced to the exact rules that produced it.
const LogicalMappingRuleVersion = "conceptual_to_dbdsl_v8_roles_keys_and_normalization"

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
	Indexes       int      `json:"indexes"`
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
	indexes        []IndexProposal
	report         LogicalMappingReport
	errors         []string
}

// MapConceptualToLogical projects an accepted conceptual model into DB-DSL
// patch operations without an LLM. Every relational decision (column names,
// types, foreign-key placement, nullability, link tables) follows fixed rules;
// values the conceptual model does not declare are inferred and reported.
func MapConceptualToLogical(model ConceptualModelProposal, options LogicalMappingOptions) (PatchProposal, LogicalMappingReport, error) {
	m := &logicalMapper{
		options: options, model: model, entityByID: map[string]*mappedEntity{},
		attributeByID: map[string]*mappedAttribute{}, attributeOwner: map[string]string{}, fileConcepts: map[string]PlanElementProposal{},
		report: LogicalMappingReport{Strategy: "deterministic", RuleVersion: LogicalMappingRuleVersion, Inferred: []string{}, Decisions: []string{}, Warnings: []string{}},
	}
	for _, concept := range model.FileConcepts {
		m.fileConcepts[concept.ID] = concept
	}
	m.mapEntities()
	m.mapRelationships()
	m.dropAttributesShadowingForeignKeys()
	m.dropDerivableForeignKeys()
	m.mapConstraints()
	m.mapLifecycles()
	m.mapDerivedViews()
	m.mapIndexes()
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

// evidence keeps the cited source units (deduplicated) and falls back to the
// owning element so every projected element stays traceable.
func (m *logicalMapper) evidence(proposal EvidenceProposal, fallback EvidenceProposal) EvidenceProposal {
	sources := []string{}
	for _, id := range proposal.SourceUnits {
		sources = appendUnique(sources, id)
	}
	if len(sources) == 0 && len(fallback.SourceUnits) > 0 {
		return m.evidence(fallback, EvidenceProposal{})
	}
	out := proposal
	out.SourceUnits = sources
	out.ReviewDecisions = append([]string{}, proposal.ReviewDecisions...)
	out.Notes = append([]string{}, proposal.Notes...)
	out.SupportLevel = nonEmpty(proposal.SupportLevel, "inferred")
	out.Confidence = nonEmpty(proposal.Confidence, "medium")
	return out
}

func mergeEvidenceInto(target *EvidenceProposal, extra EvidenceProposal) {
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
		if cardinality != "many_to_many" && m.hasParallelRole(rel) {
			proposal.ForeignKey = roleForeignKeyName(rel.ID)
			m.decide("%s: distinct relationship role uses explicit foreign key %s", rel.ID, proposal.ForeignKey)
		}
		if !required {
			proposal.OnDelete = "set_null"
		}
		if cardinality == "many_to_many" {
			if rel.From == rel.To {
				m.errors = append(m.errors, fmt.Sprintf("relationship %s links %s to itself many-to-many; model it as an association entity with two roles", rel.ID, rel.From))
				continue
			}
			if inverse := reversedManyToMany(m.relationships, proposal); inverse != nil {
				m.errors = append(m.errors, fmt.Sprintf(
					"relationships %s and %s declare the same many-to-many link in reverse directions; keep one direction or model explicit association roles",
					inverse.ID, rel.ID,
				))
				continue
			}
			proposal.Through = m.linkEntity(rel, from, to, proposal.Evidence)
			proposal.Required, proposal.FKRequired, proposal.OnDelete = true, true, "cascade"
		}
		duplicate := false
		for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: proposal.From, To: proposal.To, Cardinality: proposal.Cardinality, Through: proposal.Through, ForeignKey: proposal.ForeignKey}) {
			key := fkKey{fk.OwnerEntityID, fk.Field}
			if existing := producers[key]; existing != nil {
				if inverseRelationship(existing, proposal) {
					if sameRelationshipSemantics(existing, proposal) {
						mergeEvidenceInto(&existing.Evidence, proposal.Evidence)
						existing.Notes = append(existing.Notes, fmt.Sprintf("Also represents inverse %s (%s).", rel.ID, rel.Label))
						m.decide("%s merged into inverse relationship %s on foreign key %s.%s", rel.ID, existing.ID, fk.OwnerEntityID, fk.Field)
					} else {
						m.errors = append(m.errors, fmt.Sprintf(
							"inverse relationships %s and %s disagree on physical semantics for foreign key %s.%s (required %t/%t, fk_required %t/%t, on_delete %s/%s)",
							existing.ID, rel.ID, fk.OwnerEntityID, fk.Field, existing.Required, proposal.Required,
							existing.FKRequired, proposal.FKRequired, existing.OnDelete, proposal.OnDelete,
						))
					}
				} else {
					m.errors = append(m.errors, fmt.Sprintf(
						"relationships %s (%s) and %s (%s) require distinct roles but both map to foreign key %s.%s; resolve the roles before logical mapping",
						existing.ID, existing.Label, rel.ID, rel.Label, fk.OwnerEntityID, fk.Field,
					))
				}
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: proposal.From, To: proposal.To, Cardinality: proposal.Cardinality, Through: proposal.Through, ForeignKey: proposal.ForeignKey}) {
			producers[fkKey{fk.OwnerEntityID, fk.Field}] = proposal
		}
		m.relationships = append(m.relationships, proposal)
	}
}

// Parallel roles use relationship IDs, not guessed language semantics. This is
// stable under reordering and preserves labels/evidence on each relationship.
func (m *logicalMapper) hasParallelRole(rel ConceptualRelationshipProposal) bool {
	for _, other := range m.model.Relationships {
		if other.ID != rel.ID && other.From == rel.From && other.To == rel.To && other.Cardinality != "many_to_many" {
			// Reverse declarations cannot identify which parallel role they invert.
			for _, inverse := range m.model.Relationships {
				if rel.From != rel.To && inverse.From == rel.To && inverse.To == rel.From {
					m.errors = append(m.errors, fmt.Sprintf("relationship %s has ambiguous inverse %s across parallel roles; keep one declaration per explicit role", rel.ID, inverse.ID))
				}
			}
			return true
		}
	}
	return false
}

func roleForeignKeyName(id string) string {
	name := dsl.GeneratedForeignKeyName(id)
	if len(name) > 63 {
		digest := sha256.Sum256([]byte(id))
		name = name[:50] + fmt.Sprintf("_%x_id", digest[:4])
	}
	return name
}

func reversedManyToMany(existing []*RelationshipProposal, candidate *RelationshipProposal) *RelationshipProposal {
	for _, relationship := range existing {
		if relationship.Cardinality == "many_to_many" && relationship.From == candidate.To && relationship.To == candidate.From {
			return relationship
		}
	}
	return nil
}

func sameRelationshipSemantics(existing, candidate *RelationshipProposal) bool {
	return existing.Required == candidate.Required &&
		existing.FKRequired == candidate.FKRequired &&
		existing.OnDelete == candidate.OnDelete
}

func inverseRelationship(existing, candidate *RelationshipProposal) bool {
	if existing == nil || candidate == nil || existing.From != candidate.To || existing.To != candidate.From {
		return false
	}
	switch existing.Cardinality {
	case "one_to_many":
		return candidate.Cardinality == "many_to_one"
	case "many_to_one":
		return candidate.Cardinality == "one_to_many"
	case "one_to_one":
		return candidate.Cardinality == "one_to_one"
	default:
		return false
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
		for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: rel.From, To: rel.To, Cardinality: rel.Cardinality, Through: rel.Through, ForeignKey: rel.ForeignKey}) {
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
		resolved := map[string]bool{}
		for _, target := range concept.Targets {
			if attribute, ok := m.attributeByID[target]; ok {
				if owner == "" {
					owner = m.attributeOwner[target]
				}
				if m.attributeOwner[target] == owner && m.entityByID[owner].byName[attribute.proposal.ID] == attribute {
					fields = appendUnique(fields, attribute.proposal.ID)
					resolved[target] = true
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
				for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: rel.From, To: rel.To, Cardinality: rel.Cardinality, Through: rel.Through, ForeignKey: rel.ForeignKey}) {
					// A key made only of links belongs to the table that holds
					// their foreign keys.
					if owner == "" {
						owner = fk.OwnerEntityID
					}
					if fk.OwnerEntityID == owner {
						fields = appendUnique(fields, rel.ID)
						resolved[target] = true
					}
				}
			}
		}
		entity := m.entityByID[owner]
		evidence := EvidenceProposal{}
		if entity != nil {
			evidence = m.evidence(concept.Evidence, entity.proposal.Evidence)
		}
		if concept.Kind == "uniqueness" && len(resolved) < len(concept.Targets) {
			// A key that lost a part is a different, stricter rule than the one the
			// model states; it is kept visible as an application rule instead.
			m.warn("%s: %d of %d key parts have no column in the logical model, so the key is kept as an application rule", concept.ID, len(concept.Targets)-len(resolved), len(concept.Targets))
			m.recordApplicationRule(concept)
			continue
		}
		switch {
		case concept.Kind == "uniqueness" && entity != nil && len(fields) > 0:
			constraint := ConstraintProposal{ID: concept.ID, Type: "unique", Owner: owner, Comparison: concept.Comparison, Description: nonEmpty(concept.Description, concept.Label), Evidence: evidence}
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
			m.recordApplicationRule(concept)
		}
	}
}

// recordApplicationRule keeps a rule that plain DDL cannot enforce visible on
// every mapped element it governs, instead of emitting an invalid constraint.
// Entity targets share one model-level documentation constraint per conceptual
// rule so multi-entity rules do not create duplicate check IDs.
func (m *logicalMapper) recordApplicationRule(concept ConceptualConstraintProposal) {
	note := fmt.Sprintf("Application-enforced rule %s: %s", concept.ID, nonEmpty(concept.Description, concept.Label))
	recorded, entityTargets, unresolved := []string{}, []string{}, []string{}
	for _, target := range concept.Targets {
		if attribute := m.attributeByID[target]; attribute != nil {
			owner := m.entityByID[m.attributeOwner[target]]
			if owner != nil && owner.byName[attribute.proposal.ID] == attribute {
				mergeEvidenceInto(&attribute.proposal.Evidence, m.evidence(concept.Evidence, attribute.proposal.Evidence))
				attribute.proposal.Notes = appendUnique(attribute.proposal.Notes, note)
				recorded = appendUnique(recorded, target)
				continue
			}
			// The target column may have been replaced by a relationship FK.
			// Keep the rule visible at model level for its owning entity.
			if owner != nil {
				mergeEvidenceInto(&owner.proposal.Evidence, m.evidence(concept.Evidence, owner.proposal.Evidence))
				entityTargets = appendUnique(entityTargets, owner.proposal.ID)
				recorded = appendUnique(recorded, target)
				continue
			}
		}
		if entity := m.entityByID[target]; entity != nil {
			mergeEvidenceInto(&entity.proposal.Evidence, m.evidence(concept.Evidence, entity.proposal.Evidence))
			entityTargets = appendUnique(entityTargets, target)
			recorded = appendUnique(recorded, target)
			continue
		}
		matchedRelationship := false
		for _, rel := range m.relationships {
			if rel.ID == target {
				mergeEvidenceInto(&rel.Evidence, m.evidence(concept.Evidence, rel.Evidence))
				rel.Notes = appendUnique(rel.Notes, note)
				recorded = appendUnique(recorded, target)
				matchedRelationship = true
			}
		}
		if matchedRelationship {
			continue
		}
		unresolved = appendUnique(unresolved, target)
	}
	if len(entityTargets) > 0 {
		if m.hasConstraintID(concept.ID) {
			m.warn("%s model-level application rule was not duplicated because its constraint ID already exists", concept.ID)
		} else {
			fallback := EvidenceProposal{}
			if entity := m.entityByID[entityTargets[0]]; entity != nil {
				fallback = entity.proposal.Evidence
			}
			m.constraints = append(m.constraints, ConstraintProposal{
				ID: concept.ID, Type: "check", Owner: "model",
				Expression:  fmt.Sprintf("%s (applies to %s).", note, strings.Join(entityTargets, ", ")),
				Description: nonEmpty(concept.Description, concept.Label),
				Evidence:    m.evidence(concept.Evidence, fallback),
			})
		}
	}
	if len(unresolved) > 0 {
		m.warn("%s (%s) has unresolved targets: %s", concept.ID, concept.Kind, strings.Join(unresolved, ", "))
	}
	if len(recorded) == 0 {
		m.warn("%s (%s) has no resolvable target and is not represented in the logical model", concept.ID, concept.Kind)
		return
	}
	m.decide("%s: %s rule recorded as application-enforced on %s", concept.ID, concept.Kind, strings.Join(recorded, ", "))
}

func (m *logicalMapper) hasConstraintID(id string) bool {
	for _, constraint := range m.constraints {
		if constraint.ID == id {
			return true
		}
	}
	return false
}

// mapIndexes projects the access paths of the conceptual model. A column that
// already leads a unique key is indexed by that key and gets no second index.
func (m *logicalMapper) mapIndexes() {
	leading := map[string]bool{}
	for _, constraint := range m.constraints {
		if constraint.Type != "unique" {
			continue
		}
		if first := nonEmpty(constraint.Field, firstString(constraint.Fields)); first != "" {
			leading[constraint.Owner+"."+first] = true
		}
	}
	for _, concept := range m.model.IndexConcepts {
		entity := m.entityByID[concept.Owner]
		if entity == nil {
			m.warn("index %s has no mapped owner entity and is not projected", concept.ID)
			continue
		}
		fields := []string{}
		for _, target := range concept.Targets {
			if attribute, ok := m.attributeByID[target]; ok && m.attributeOwner[target] == concept.Owner && entity.byName[attribute.proposal.ID] == attribute {
				fields = appendUnique(fields, attribute.proposal.ID)
			}
		}
		if len(fields) == 0 {
			m.warn("index %s has no mapped column and is not projected", concept.ID)
			continue
		}
		if attribute := entity.byName[fields[0]]; attribute != nil && !indexable(attribute.proposal) {
			m.decide("%s: %s.%s is not indexed: an index does not help searching long text or a column with few distinct values", concept.ID, entity.proposal.TableName, fields[0])
			continue
		}
		if leading[concept.Owner+"."+fields[0]] {
			m.decide("%s: %s.%s is already indexed by a unique key", concept.ID, entity.proposal.TableName, fields[0])
			continue
		}
		leading[concept.Owner+"."+fields[0]] = true
		m.indexes = append(m.indexes, IndexProposal{ID: concept.ID, Owner: concept.Owner, Fields: fields,
			Description: nonEmpty(concept.Description, concept.Label), Evidence: m.evidence(concept.Evidence, entity.proposal.Evidence)})
	}
}

// indexable reports whether a plain B-tree index on the column helps a search:
// long text is searched by content, not by prefix, and a flag or a closed set
// of values has too few distinct values to narrow a search.
func indexable(attribute AttributeProposal) bool {
	return attribute.Type != "text" && attribute.Type != "boolean" && len(attribute.EnumValues) == 0
}

// dropDerivableForeignKeys removes a foreign key that the owner already reaches
// through a mandatory chain of classifications (a place that belongs to a
// municipality that belongs to a city). A classification hierarchy is a fixed
// fact, so the direct key only repeats it and could contradict it. A path
// through an ordinary record is not such a fact: the city of an owner is not
// the city of what the owner offers, so that path never makes a key redundant.
// Keys that a constraint names, and role-named keys, are kept.
func (m *logicalMapper) dropDerivableForeignKeys() {
	named := map[string]bool{}
	for _, concept := range m.model.ConstraintConcepts {
		for _, target := range concept.Targets {
			named[target] = true
		}
	}
	type edge struct {
		rel       *RelationshipProposal
		from, to  string
		mandatory bool
	}
	edges := func(skip *RelationshipProposal) []edge {
		out := []edge{}
		for _, rel := range m.relationships {
			if rel == skip || rel.Cardinality == "many_to_many" {
				continue
			}
			for _, fk := range dsl.RelationshipForeignKeys(dsl.Relationship{From: rel.From, To: rel.To, Cardinality: rel.Cardinality, ForeignKey: rel.ForeignKey}) {
				target := rel.To
				if fk.OwnerEntityID == rel.To {
					target = rel.From
				}
				if target != fk.OwnerEntityID {
					out = append(out, edge{rel: rel, from: fk.OwnerEntityID, to: target, mandatory: rel.FKRequired})
				}
			}
		}
		return out
	}
	reaches := func(from, to string, graph []edge) (string, bool) {
		previous := map[string]string{from: ""}
		queue := []string{from}
		for len(queue) > 0 {
			current := queue[0]
			queue = queue[1:]
			for _, e := range graph {
				if e.from != current || !e.mandatory {
					continue
				}
				if e.to != to && m.entityByID[e.to].proposal.Kind != "lookup" {
					continue
				}
				if _, seen := previous[e.to]; seen {
					continue
				}
				previous[e.to] = current
				if e.to == to {
					path := []string{to}
					for step := current; step != ""; step = previous[step] {
						path = append([]string{step}, path...)
					}
					return strings.Join(path, " → "), true
				}
				queue = append(queue, e.to)
			}
		}
		return "", false
	}
	for changed := true; changed; {
		changed = false
		for _, direct := range edges(nil) {
			if named[direct.rel.ID] || direct.rel.ForeignKey != "" {
				continue
			}
			path, ok := reaches(direct.from, direct.to, edges(direct.rel))
			if !ok {
				continue
			}
			owner := m.entityByID[direct.from]
			mergeEvidenceInto(&owner.proposal.Evidence, direct.rel.Evidence)
			kept := m.relationships[:0]
			for _, rel := range m.relationships {
				if rel != direct.rel {
					kept = append(kept, rel)
				}
			}
			m.relationships = kept
			m.warn("%s removed: %s already reaches %s through a mandatory chain of classifications (%s), so a second foreign key would repeat that fact", direct.rel.ID, direct.from, direct.to, path)
			changed = true
			break
		}
	}
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (m *logicalMapper) mapLifecycles() {
	for _, lifecycle := range m.model.LifecycleConcepts {
		evidence := m.evidence(EvidenceProposal{SourceUnits: lifecycle.SourceUnits, SupportLevel: "explicit", Confidence: "high"}, EvidenceProposal{})
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
		if attribute != nil && attribute.proposal.Type == "boolean" {
			// Two states held by a yes/no flag stay a flag: turning the column into
			// a string enum of "true" and "false" would change its type. The
			// transitions stay on the column as notes.
			for _, transition := range lifecycle.Transitions {
				if note := conceptualTransitionNote(transition); note != "" {
					attribute.proposal.Notes = appendUnique(attribute.proposal.Notes, note)
				}
			}
			mergeEvidenceInto(&attribute.proposal.Evidence, evidence)
			m.decide("%s: lifecycle kept as the boolean %s.%s; its transitions are enforced by the application", lifecycle.ID, owner.proposal.ID, field)
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
		notes := []string{}
		for _, transition := range lifecycle.Transitions {
			if known[transition.From] && known[transition.To] {
				transitions = append(transitions, dsl.StateTransition{From: transition.From, To: transition.To})
				if note := conceptualTransitionNote(transition); note != "" {
					notes = appendUnique(notes, note)
				}
			}
		}
		m.stateMachines = append(m.stateMachines, StateMachineProposal{ID: lifecycle.ID, Owner: owner.proposal.ID, Field: field, States: attribute.proposal.EnumValues,
			Initial: initial, Terminal: terminal, Transitions: transitions, Notes: notes, Evidence: evidence})
	}
}

func conceptualTransitionNote(transition ConceptualTransition) string {
	trigger, actor, effects := strings.TrimSpace(transition.Trigger), strings.TrimSpace(transition.By), strings.TrimSpace(transition.Effects)
	if trigger == "" && actor == "" && effects == "" {
		return ""
	}
	parts := []string{fmt.Sprintf("Transition %q -> %q", transition.From, transition.To)}
	if trigger != "" {
		parts = append(parts, "trigger/guard: "+trigger)
	}
	if actor != "" {
		parts = append(parts, "actor: "+actor)
	}
	if effects != "" {
		parts = append(parts, "effects: "+effects)
	}
	return strings.Join(parts, "; ") + "."
}

func (m *logicalMapper) findStatusAttribute(lifecycle PlanElementProposal) (*mappedEntity, string) {
	sources := map[string]bool{}
	for _, id := range lifecycle.SourceUnits {
		sources[id] = true
	}
	for _, entity := range m.entities {
		for _, attribute := range entity.attributes {
			if !strings.Contains(attribute.proposal.ID, "status") && !strings.Contains(attribute.proposal.ID, "state") {
				continue
			}
			for _, id := range attribute.proposal.Evidence.SourceUnits {
				if sources[id] {
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
			cited := map[string]bool{}
			for _, id := range derived.SourceUnits {
				cited[id] = true
			}
			for _, entity := range m.entities {
				for _, id := range entity.proposal.Evidence.SourceUnits {
					if cited[id] {
						sources = appendUnique(sources, entity.proposal.ID)
					}
				}
			}
			if len(sources) == 0 {
				if entity := m.closestEntity(derived.SourceUnits, derived.Label); entity != nil {
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
			Evidence: m.evidence(EvidenceProposal{SourceUnits: derived.SourceUnits, SupportLevel: "explicit", Confidence: "high"}, fallback)})
	}
}

// closestEntity picks the entity that shares the most source units with the
// given evidence, breaking ties by label words. It is only used to keep
// unprojectable conceptual elements traceable.
func (m *logicalMapper) closestEntity(sourceUnits []string, label string) *mappedEntity {
	sources := map[string]bool{}
	for _, id := range sourceUnits {
		sources[id] = true
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

// preserveConceptualCoverage guarantees that every source unit cited by the
// accepted conceptual model is still cited by some projected element; a unit
// whose conceptual element had no DDL form is attached to the closest table.
func (m *logicalMapper) preserveConceptualCoverage() {
	cited := map[string]bool{}
	mark := func(evidence EvidenceProposal) {
		for _, id := range evidence.SourceUnits {
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
	for _, item := range m.indexes {
		mark(item.Evidence)
	}
	type orphan struct {
		id, label string
		sources   []string
	}
	orphans := []orphan{}
	for _, item := range m.model.ConstraintConcepts {
		orphans = append(orphans, orphan{item.ID, item.Label, item.Evidence.SourceUnits})
	}
	for _, group := range [][]PlanElementProposal{m.model.LifecycleConcepts, m.model.DerivedConcepts, m.model.FileConcepts, m.model.ImportConcepts} {
		for _, item := range group {
			orphans = append(orphans, orphan{item.ID, item.Label, item.SourceUnits})
		}
	}
	for _, item := range orphans {
		missing := []string{}
		for _, id := range item.sources {
			if !cited[id] {
				missing = appendUnique(missing, id)
			}
		}
		if len(missing) == 0 {
			continue
		}
		entity := m.closestEntity(missing, item.label)
		if entity == nil {
			m.warn("%s: source units %s have no table to attach to", item.id, strings.Join(missing, ", "))
			continue
		}
		mergeEvidenceInto(&entity.proposal.Evidence, m.evidence(EvidenceProposal{SourceUnits: missing}, EvidenceProposal{}))
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
	for i := range m.indexes {
		patch.Operations = append(patch.Operations, PatchOperation{Operation: "add_index", Index: &m.indexes[i]})
	}
	m.report.Entities, m.report.Relationships, m.report.Constraints = len(m.entities), len(m.relationships), len(m.constraints)
	m.report.StateMachines, m.report.DerivedViews, m.report.FileSpecs = len(m.stateMachines), len(m.derivedViews), len(m.fileSpecs)
	m.report.Indexes = len(m.indexes)
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
	text := strings.ToLower(asciiSerbian(derived.Label + " " + derived.Description + " " + strings.Join(derived.Metrics, " ")))
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

// snakeIdentifier converts a label into a lower snake_case identifier,
// writing Serbian in ASCII letters and dropping other non-ASCII runes.
func snakeIdentifier(value string) string {
	value = asciiSerbian(strings.TrimSpace(value))
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
