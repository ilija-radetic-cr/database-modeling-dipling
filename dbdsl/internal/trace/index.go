package trace

import (
	"sort"

	"dbdsl/internal/dsl"
)

type EvidenceRef struct {
	SourceUnits      []string `json:"source_units"`
	RequirementAtoms []string `json:"requirement_atoms"`
	ReviewDecisions  []string `json:"review_decisions"`
	SupportLevel     string   `json:"support_level,omitempty"`
	Confidence       string   `json:"confidence,omitempty"`
}

type ModelGraph struct {
	Nodes []ModelNode `json:"nodes"`
	Edges []ModelEdge `json:"edges"`
}

type ModelNode struct {
	ID          string       `json:"id"`
	Kind        string       `json:"kind"`
	Label       string       `json:"label"`
	TableName   string       `json:"table_name,omitempty"`
	Description string       `json:"description,omitempty"`
	Fields      []ModelField `json:"fields,omitempty"`
	Evidence    EvidenceRef  `json:"evidence"`
}

type ModelField struct {
	ID          string      `json:"id"`
	ElementID   string      `json:"element_id"`
	Label       string      `json:"label"`
	Type        string      `json:"type"`
	Required    bool        `json:"required"`
	Description string      `json:"description,omitempty"`
	Evidence    EvidenceRef `json:"evidence"`
}

type ModelEdge struct {
	ID          string      `json:"id"`
	Kind        string      `json:"kind"`
	Label       string      `json:"label"`
	From        string      `json:"from"`
	To          string      `json:"to"`
	Cardinality string      `json:"cardinality"`
	Required    bool        `json:"required"`
	Evidence    EvidenceRef `json:"evidence"`
}

type TraceIndex struct {
	SourceToElements      map[string][]string `json:"source_to_elements"`
	ElementToSources      map[string][]string `json:"element_to_sources"`
	RequirementToElements map[string][]string `json:"requirement_to_elements"`
	ReviewToElements      map[string][]string `json:"review_to_elements"`
}

type ElementDetails struct {
	ID                  string      `json:"id"`
	Kind                string      `json:"kind"`
	Label               string      `json:"label"`
	Description         string      `json:"description,omitempty"`
	Expression          string      `json:"expression,omitempty"`
	Evidence            EvidenceRef `json:"evidence"`
	RelatedElements     []string    `json:"related_elements"`
	SourceUnitSummaries []string    `json:"source_unit_summaries,omitempty"`
}

func BuildModelGraph(doc *dsl.Document) ModelGraph {
	graph := ModelGraph{}
	for _, entity := range doc.Entities {
		node := ModelNode{
			ID:          "table:" + entity.ID,
			Kind:        "table",
			Label:       nonEmpty(entity.Label, entity.ID),
			TableName:   entity.TableName,
			Description: entity.Description,
			Evidence:    evidenceRef(entity.Evidence),
		}
		for _, attr := range entity.Attributes {
			required := false
			if attr.Required != nil {
				required = *attr.Required
			}
			node.Fields = append(node.Fields, ModelField{
				ID:          attr.ID,
				ElementID:   "field:" + entity.ID + "." + attr.ID,
				Label:       nonEmpty(attr.Label, attr.ID),
				Type:        attr.Type,
				Required:    required,
				Description: attr.Description,
				Evidence:    evidenceRef(attr.Evidence),
			})
		}
		graph.Nodes = append(graph.Nodes, node)
	}
	for _, view := range doc.DerivedViews {
		graph.Nodes = append(graph.Nodes, ModelNode{
			ID:          "derived_view:" + view.ID,
			Kind:        "derived_view",
			Label:       nonEmpty(view.Label, view.ID),
			Description: view.Description,
			Evidence:    evidenceRef(view.Evidence),
		})
	}
	for _, rel := range doc.Relationships {
		required := false
		if rel.Required != nil {
			required = *rel.Required
		}
		graph.Edges = append(graph.Edges, ModelEdge{
			ID:          "relationship:" + rel.ID,
			Kind:        "relationship",
			Label:       nonEmpty(rel.Label, rel.ID),
			From:        "table:" + rel.From,
			To:          "table:" + rel.To,
			Cardinality: rel.Cardinality,
			Required:    required,
			Evidence:    evidenceRef(rel.Evidence),
		})
	}
	return graph
}

func BuildTraceIndex(doc *dsl.Document) TraceIndex {
	idx := TraceIndex{
		SourceToElements:      map[string][]string{},
		ElementToSources:      map[string][]string{},
		RequirementToElements: map[string][]string{},
		ReviewToElements:      map[string][]string{},
	}
	record := func(elementID string, evidence dsl.Evidence) {
		for _, sourceID := range evidence.SourceUnits {
			addUnique(idx.SourceToElements, sourceID, elementID)
			addUnique(idx.ElementToSources, elementID, sourceID)
		}
		for _, atomID := range evidence.RequirementAtoms {
			addUnique(idx.RequirementToElements, atomID, elementID)
		}
		for _, reviewID := range evidence.ReviewDecisions {
			addUnique(idx.ReviewToElements, reviewID, elementID)
		}
	}
	for _, entity := range doc.Entities {
		record("table:"+entity.ID, entity.Evidence)
		for _, attr := range entity.Attributes {
			record("field:"+entity.ID+"."+attr.ID, attr.Evidence)
		}
	}
	for _, rel := range doc.Relationships {
		record("relationship:"+rel.ID, rel.Evidence)
	}
	for _, constraint := range doc.Constraints {
		record("constraint:"+constraint.ID, constraint.Evidence)
	}
	for _, spec := range doc.ImportSpecs {
		record("import_spec:"+spec.ID, spec.Evidence)
	}
	for _, machine := range doc.StateMachines {
		record("state_machine:"+machine.ID, machine.Evidence)
	}
	for _, view := range doc.DerivedViews {
		record("derived_view:"+view.ID, view.Evidence)
	}
	for _, spec := range doc.FileSpecs {
		record("file_spec:"+spec.ID, spec.Evidence)
	}
	sortIndex(idx.SourceToElements)
	sortIndex(idx.ElementToSources)
	sortIndex(idx.RequirementToElements)
	sortIndex(idx.ReviewToElements)
	return idx
}

func BuildElementDetails(doc *dsl.Document, elementID string) (ElementDetails, bool) {
	for _, entity := range doc.Entities {
		if elementID == "table:"+entity.ID {
			return ElementDetails{
				ID:              elementID,
				Kind:            "table",
				Label:           nonEmpty(entity.Label, entity.ID),
				Description:     entity.Description,
				Expression:      entity.TableName,
				Evidence:        evidenceRef(entity.Evidence),
				RelatedElements: relatedForEntity(doc, entity.ID),
			}, true
		}
		for _, attr := range entity.Attributes {
			if elementID == "field:"+entity.ID+"."+attr.ID {
				return ElementDetails{
					ID:              elementID,
					Kind:            "field",
					Label:           nonEmpty(attr.Label, attr.ID),
					Description:     attr.Description,
					Expression:      entity.ID + "." + attr.ID + " " + attr.Type,
					Evidence:        evidenceRef(attr.Evidence),
					RelatedElements: []string{"table:" + entity.ID},
				}, true
			}
		}
	}
	for _, rel := range doc.Relationships {
		if elementID == "relationship:"+rel.ID {
			return ElementDetails{
				ID:              elementID,
				Kind:            "relationship",
				Label:           nonEmpty(rel.Label, rel.ID),
				Description:     rel.Description,
				Expression:      rel.From + " " + rel.Cardinality + " " + rel.To,
				Evidence:        evidenceRef(rel.Evidence),
				RelatedElements: []string{"table:" + rel.From, "table:" + rel.To},
			}, true
		}
	}
	for _, view := range doc.DerivedViews {
		if elementID == "derived_view:"+view.ID {
			return ElementDetails{
				ID:              elementID,
				Kind:            "derived_view",
				Label:           nonEmpty(view.Label, view.ID),
				Description:     view.Description,
				Expression:      view.Kind,
				Evidence:        evidenceRef(view.Evidence),
				RelatedElements: prefixAll("table:", view.Sources),
			}, true
		}
	}
	return ElementDetails{}, false
}

func evidenceRef(e dsl.Evidence) EvidenceRef {
	return EvidenceRef{
		SourceUnits:      append([]string(nil), e.SourceUnits...),
		RequirementAtoms: append([]string(nil), e.RequirementAtoms...),
		ReviewDecisions:  append([]string(nil), e.ReviewDecisions...),
		SupportLevel:     e.SupportLevel,
		Confidence:       e.Confidence,
	}
}

func relatedForEntity(doc *dsl.Document, entityID string) []string {
	var out []string
	for _, rel := range doc.Relationships {
		if rel.From == entityID {
			out = append(out, "relationship:"+rel.ID, "table:"+rel.To)
		}
		if rel.To == entityID {
			out = append(out, "relationship:"+rel.ID, "table:"+rel.From)
		}
	}
	sort.Strings(out)
	return dedupe(out)
}

func prefixAll(prefix string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, prefix+value)
	}
	return out
}

func addUnique(m map[string][]string, key, value string) {
	for _, existing := range m[key] {
		if existing == value {
			return
		}
	}
	m[key] = append(m[key], value)
}

func sortIndex(m map[string][]string) {
	for key := range m {
		sort.Strings(m[key])
	}
}

func dedupe(values []string) []string {
	out := values[:0]
	var last string
	for i, value := range values {
		if i == 0 || value != last {
			out = append(out, value)
		}
		last = value
	}
	return out
}

func nonEmpty(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
