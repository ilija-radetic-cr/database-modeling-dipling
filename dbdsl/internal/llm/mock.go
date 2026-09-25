package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type MockClient struct {
	Structured map[string]json.RawMessage
	Text       map[string]string
}

func NewDefaultMockClient() *MockClient {
	return &MockClient{
		Structured: map[string]json.RawMessage{
			"source_segmentation":         json.RawMessage(`{}`),
			"source_unit_extraction":      json.RawMessage(defaultSourceUnitExtractionJSON),
			"requirement_atom_extraction": json.RawMessage(defaultRequirementAtomStageJSON),
			"functional_analysis":         json.RawMessage(defaultFunctionalAnalysisJSON),
			"crud_mapping":                json.RawMessage(defaultCRUDMappingJSON),
			"review_candidate_proposal":   json.RawMessage(defaultProjectReviewJSON),
			"review_resolution_patch":     json.RawMessage(defaultReviewResolutionPatchJSON),
			"conceptual_model":            json.RawMessage(defaultConceptualModelJSON),
			"logical_projection":          json.RawMessage(defaultLogicalProjectionJSON),
			"requirement_extraction":      json.RawMessage(defaultRequirementExtractionJSON),
			"model_plan":                  json.RawMessage(defaultModelPlanJSON),
			"dbdsl_patch":                 json.RawMessage(defaultPatchJSON),
			"repair":                      json.RawMessage(defaultRepairJSON),
		},
		Text: map[string]string{
			"baseline_dbml": defaultBaselineDBML,
			"baseline_sql":  defaultBaselineSQL,
		},
	}
}

func (m *MockClient) GenerateStructured(ctx context.Context, req Request) (Response, error) {
	_ = ctx
	if m == nil {
		m = NewDefaultMockClient()
	}
	payload, ok := m.Structured[req.Stage]
	if req.Stage == "source_segmentation" && (!ok || string(payload) == `{}`) {
		var err error
		payload, err = dynamicSourceSegmentationPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "source_unit_extraction" && (!ok || string(payload) == defaultSourceUnitExtractionJSON) {
		var err error
		payload, err = dynamicSourceUnitPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "requirement_atom_extraction" && (!ok || string(payload) == defaultRequirementAtomStageJSON) {
		var err error
		payload, err = dynamicRequirementAtomPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "functional_analysis" && (!ok || string(payload) == defaultFunctionalAnalysisJSON) {
		var err error
		payload, err = dynamicFunctionalAnalysisPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "crud_mapping" && (!ok || string(payload) == defaultCRUDMappingJSON) {
		var err error
		payload, err = dynamicCRUDMappingPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "review_candidate_proposal" && (!ok || string(payload) == defaultProjectReviewJSON) {
		var err error
		payload, err = dynamicProjectReviewPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "review_resolution_patch" && (!ok || string(payload) == defaultReviewResolutionPatchJSON) {
		var err error
		payload, err = dynamicReviewResolutionPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "conceptual_description" && !ok {
		var err error
		payload, err = dynamicConceptualDescriptionPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "conceptual_model" && (!ok || string(payload) == defaultConceptualModelJSON) {
		var err error
		payload, err = dynamicConceptualModelPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if req.Stage == "logical_projection" && (!ok || string(payload) == defaultLogicalProjectionJSON) {
		var err error
		payload, err = dynamicLogicalProjectionPayload(req.Input)
		if err != nil {
			return Response{}, err
		}
		ok = true
	}
	if !ok {
		return Response{}, fmt.Errorf("mock LLM has no structured response for stage %s", req.Stage)
	}
	raw := fmt.Sprintf(`{"id":"mock_%s","status":"completed","output_text":%q}`, req.Stage, string(payload))
	return Response{
		Provider: "mock",
		Model:    nonEmpty(req.Model, "mock"),
		Raw:      raw,
		Text:     string(payload),
		Parsed:   payload,
	}, nil
}

var mockSegmentLine = regexp.MustCompile(`^\s*(SU-[0-9]+(?:\.[0-9]+)?) \[([^\]]+)\]`)

// dynamicConceptualDescriptionPayload builds a small but structurally complete
// description from the numbered segments: two things linked 1:N, a lifecycle,
// a rule, and every remaining segment excluded, so offline runs cover the whole
// segment-based flow.
func dynamicConceptualDescriptionPayload(input string) (json.RawMessage, error) {
	ids := []string{}
	for _, line := range strings.Split(input, "\n") {
		match := mockSegmentLine.FindStringSubmatch(line)
		if match == nil || strings.HasPrefix(match[2], "heading") {
			continue
		}
		ids = append(ids, match[1])
	}
	if len(ids) == 0 {
		return nil, errors.New("mock conceptual description needs at least one non-heading segment")
	}
	evidence := func(id string) map[string]any { return map[string]any{"segments": []string{id}, "mode": "direct"} }
	first, second := ids[0], ids[0]
	if len(ids) > 1 {
		second = ids[1]
	}
	property := func(name, meaning, presence string, allowed []string, id string) map[string]any {
		if allowed == nil {
			allowed = []string{}
		}
		valueType := "text"
		if name == "kolicina" {
			valueType = "integer"
		}
		return map[string]any{"name": name, "meaning": meaning, "value_type": valueType, "shape": "single", "parts": []string{}, "presence": presence, "condition": "", "variant": "",
			"allowed_values": allowed, "origin": "entered", "source": "", "evidence": evidence(id)}
	}
	excluded := []map[string]string{}
	for _, id := range ids[min(2, len(ids)):] {
		excluded = append(excluded, map[string]string{"segment": id, "reason": "other"})
	}
	payload := map[string]any{
		"actors": []map[string]any{{"id": "korisnik", "name": "Korisnik", "description": "Korisnik sistema.", "represented_by": "", "differs_by": "", "evidence": evidence(first)}},
		"things": []map[string]any{
			{"id": "zapis", "name": "Zapis", "kind": "object", "description": "Glavni zapis sistema.", "identified_by": []string{"oznaka"}, "instance_of": "", "variants": []string{}, "created_when": "",
				"properties": []map[string]any{property("oznaka", "Jedinstvena oznaka zapisa.", "required", nil, first), property("naziv", "Naziv zapisa.", "required", nil, first)},
				"links":      []map[string]any{}, "states": []string{"nov", "zavrsen"},
				"transitions": []map[string]any{{"from": "nov", "to": "zavrsen", "trigger": "završetak", "by": "korisnik", "effects": "", "evidence": evidence(first)}},
				"evidence":    evidence(first)},
			{"id": "stavka", "name": "Stavka", "kind": "record", "description": "Stavka zapisa.", "identified_by": []string{}, "instance_of": "", "variants": []string{}, "created_when": "",
				"properties": []map[string]any{property("kolicina", "Količina stavke.", "required", nil, second)},
				"links":      []map[string]any{{"to": "zapis", "meaning": "pripada zapisu", "per_this": "1", "per_other": "0..N", "evidence": evidence(second)}},
				"states":     []string{}, "transitions": []map[string]any{}, "evidence": evidence(second)},
		},
		"rules":   []map[string]any{{"id": "kolicina_pozitivna", "kind": "quantity", "statement": "Količina je pozitivna.", "applies_to": []string{"stavka.kolicina"}, "parameters": []string{}, "evidence": evidence(second)}},
		"queries": []map[string]any{}, "imports": []map[string]any{}, "boundaries": []map[string]any{},
		"excluded": excluded, "open_questions": []map[string]any{},
	}
	data, err := json.Marshal(payload)
	return json.RawMessage(data), err
}

func dynamicSourceSegmentationPayload(input string) (json.RawMessage, error) {
	document := input
	if start := strings.Index(document, "<document>"); start >= 0 {
		document = document[start+len("<document>"):]
	}
	if end := strings.LastIndex(document, "</document>"); end >= 0 {
		document = document[:end]
	}
	segments := []map[string]any{}
	appendSegment := func(typeName, text string) {
		segments = append(segments, map[string]any{"type": typeName, "text": text})
	}
	plainLines := []string{}
	flushPlain := func() {
		if len(plainLines) == 0 {
			return
		}
		for _, sentence := range splitMockSentences(strings.Join(plainLines, " ")) {
			appendSegment("sentence", sentence)
		}
		plainLines = nil
	}
	for _, line := range strings.Split(document, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		typeName := ""
		switch {
		case strings.HasPrefix(trimmed, "#"):
			typeName = "heading"
		case strings.Contains(lower, "универзитет у београду") || strings.Contains(lower, "univerzitet u beogradu"):
			typeName = "page_header"
		case strings.Trim(trimmed, "0123456789") == "":
			typeName = "page_number"
		case strings.HasPrefix(trimmed, "-") || strings.HasPrefix(trimmed, "*"):
			typeName = "list"
		case strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "["):
			typeName = "example"
		}
		if typeName == "" {
			plainLines = append(plainLines, trimmed)
			continue
		}
		flushPlain()
		appendSegment(typeName, trimmed)
	}
	flushPlain()
	data, err := json.Marshal(map[string]any{"segments": segments})
	return json.RawMessage(data), err
}

func splitMockSentences(value string) []string {
	var out []string
	var current strings.Builder
	for _, r := range strings.TrimSpace(value) {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			if sentence := strings.TrimSpace(current.String()); sentence != "" {
				out = append(out, sentence)
			}
			current.Reset()
		}
	}
	if sentence := strings.TrimSpace(current.String()); sentence != "" {
		out = append(out, sentence)
	}
	return out
}

func dynamicSourceUnitPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		Segments []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"segments"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, fmt.Errorf("parse mock source-unit input: %w", err)
	}
	if len(parsed.Segments) == 0 {
		return nil, fmt.Errorf("mock source-unit extraction requires combined-document sentences")
	}
	units := make([]map[string]any, 0, len(parsed.Segments))
	for _, sentence := range parsed.Segments {
		kind := "requirement_sentence"
		relevance := "model_relevant"
		if sentence.Type == "structural" || sentence.Type == "heading" || sentence.Type == "page_header" || sentence.Type == "page_footer" || sentence.Type == "page_number" || sentence.Type == "other" {
			kind = "heading"
			relevance = "model_supporting"
		}
		if sentence.Type == "example" {
			kind = "structured_example"
			relevance = "example"
		}
		units = append(units, map[string]any{
			"od_sentence_id":  sentence.ID,
			"kind":            kind,
			"section":         "combined_document",
			"relevance":       relevance,
			"tags":            []string{"mock"},
			"confidence":      "high",
			"requires_review": false,
			"warnings":        []string{},
		})
	}
	payload, err := json.Marshal(map[string]any{
		"classifications":    units,
		"warnings":           []string{},
		"confidence_summary": map[string]string{"overall": "mock source-unit extraction"},
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

func dynamicRequirementAtomPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		SourceUnits []struct {
			ID         string `json:"id"`
			Text       string `json:"text"`
			Normalized string `json:"normalized"`
			Exact      string `json:"exact"`
		} `json:"source_units"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, err
	}
	if len(parsed.SourceUnits) == 0 {
		return nil, fmt.Errorf("mock requirement extraction requires source units")
	}
	atoms := make([]map[string]any, 0, len(parsed.SourceUnits))
	for i, source := range parsed.SourceUnits {
		statement := source.Text
		if strings.TrimSpace(statement) == "" {
			statement = source.Normalized
		}
		if strings.TrimSpace(statement) == "" {
			statement = source.Exact
		}
		atoms = append(atoms, map[string]any{
			"id": fmt.Sprintf("RA-%03d", i+1), "statement": statement,
			"subject": "system", "predicate": "requires", "object": statement,
			"quantifier": "", "condition": "", "temporal_semantics": "", "ownership": "system",
			"atom_type": "data_requirement", "modeling_relevance": "direct_db",
			"source_units": []string{source.ID}, "functional_area": "core",
			"functional_pattern": "domain_management", "support_level": "explicit",
			"confidence": "high", "review_class": "none", "review_topic": "none", "review_group": "", "modeling_outcome": "represented",
			"persistence_effect": "required", "example_role": "none",
			"warnings": []string{},
		})
	}
	data, err := json.Marshal(map[string]any{"requirement_atoms": atoms, "warnings": []string{}, "confidence_summary": map[string]string{"overall": "mock requirement atoms"}})
	return json.RawMessage(data), err
}

func dynamicFunctionalAnalysisPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		RequirementAtoms []struct {
			ID string `json:"id"`
		} `json:"requirement_atoms"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, err
	}
	if len(parsed.RequirementAtoms) == 0 {
		return nil, fmt.Errorf("mock functional analysis requires atoms")
	}
	atomIDs := make([]string, 0, len(parsed.RequirementAtoms))
	for _, atom := range parsed.RequirementAtoms {
		atomIDs = append(atomIDs, atom.ID)
	}
	data, err := json.Marshal(map[string]any{
		"functional_areas": []map[string]any{{"id": "core", "label": "Core", "purpose": "Manage the domain data described by the source.", "main_actors": []string{"system"}, "atoms": atomIDs, "modeling_focus": []string{"DomainRecord"}, "confidence": "high", "warnings": []string{}}},
		"actors":           []map[string]any{{"id": "system", "label": "System", "description": "Deterministic mock actor.", "kind": "system"}},
		"warnings":         []string{}, "confidence_summary": map[string]string{"overall": "mock functional analysis"},
	})
	return json.RawMessage(data), err
}

func dynamicCRUDMappingPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		SourceUnits []struct {
			ID string `json:"id"`
		} `json:"source_units"`
		RequirementAtoms []struct {
			ID string `json:"id"`
		} `json:"requirement_atoms"`
		FunctionalAreas []struct {
			ID string `json:"id"`
		} `json:"functional_areas"`
		Actors []struct {
			ID string `json:"id"`
		} `json:"actors"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, err
	}
	atomIDs := make([]string, 0, len(parsed.RequirementAtoms))
	for _, item := range parsed.RequirementAtoms {
		atomIDs = append(atomIDs, item.ID)
	}
	sourceIDs := make([]string, 0, len(parsed.SourceUnits))
	for _, item := range parsed.SourceUnits {
		sourceIDs = append(sourceIDs, item.ID)
	}
	actorID, areaID := "system", "core"
	if len(parsed.Actors) > 0 {
		actorID = parsed.Actors[0].ID
	}
	if len(parsed.FunctionalAreas) > 0 {
		areaID = parsed.FunctionalAreas[0].ID
	}
	data, err := json.Marshal(map[string]any{
		"operations": []map[string]any{{
			"id": "OP-001", "label": "Manage domain records", "actor_id": actorID, "functional_area_id": areaID,
			"creates": []string{"DomainRecord"}, "reads": []string{"DomainRecord"}, "updates": []string{"DomainRecord"}, "deletes": []string{},
			"persistent_data": []string{"DomainRecord"}, "outcome": "mutates persistent domain data", "requirement_atoms": atomIDs,
			"source_units": sourceIDs, "requires_review": false, "warnings": []string{},
		}},
		"warnings": []string{}, "confidence_summary": map[string]string{"overall": "mock CRUD mapping"},
	})
	return json.RawMessage(data), err
}

func dynamicProjectReviewPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		SourceUnits []struct {
			ID string `json:"id"`
		} `json:"source_units"`
		RequirementAtoms []struct {
			ID          string `json:"id"`
			ReviewGroup string `json:"review_group"`
		} `json:"requirement_atoms"`
		FunctionalAreas []struct {
			ID string `json:"id"`
		} `json:"functional_areas"`
		CRUDOperations []struct {
			ID string `json:"id"`
		} `json:"crud_operations"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, err
	}
	if len(parsed.RequirementAtoms) == 0 {
		return nil, fmt.Errorf("mock project review requires atoms")
	}
	sources, areas, operations := []string{}, []string{}, []string{}
	for _, item := range parsed.SourceUnits {
		sources = append(sources, item.ID)
	}
	for _, item := range parsed.FunctionalAreas {
		areas = append(areas, item.ID)
	}
	for _, item := range parsed.CRUDOperations {
		operations = append(operations, item.ID)
	}
	groups := map[string][]string{}
	for _, item := range parsed.RequirementAtoms {
		key := item.ReviewGroup
		if key == "" {
			key = item.ID
		}
		groups[key] = append(groups[key], item.ID)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	candidates := make([]map[string]any, 0, len(keys))
	for index, key := range keys {
		atoms := groups[key]
		atomUpdates := make([]map[string]any, 0, len(atoms))
		for _, atomID := range atoms {
			atomUpdates = append(atomUpdates, map[string]any{"atom_id": atomID, "modeling_outcome": "represented", "persistence_effect": "required", "support_level": "no_change", "confidence": "no_change"})
		}
		generatedEffects := map[string]any{"modeling_outcome": "represented", "persistence_effect": "required", "support_level": "no_change", "requires_followup": false, "atom_updates": atomUpdates, "impact_dimensions": []string{"identity", "key"}, "followup_candidate_ids": []string{}}
		candidateID := fmt.Sprintf("RC-%03d", index+1)
		candidates = append(candidates, map[string]any{
			"id": candidateID, "decision_key": key, "question": "Should the affected requirements use generated internal identity?",
			"description": "The source leaves a database-model choice unresolved.",
			"category":    "identity", "phase": "pre_conceptual", "severity": "medium", "blocking": true,
			"affected_source_units": sources, "affected_atoms": atoms, "affected_functional_areas": areas,
			"affected_operations": operations, "affected_model_candidates": []string{"DomainRecord"},
			"depends_on": []string{}, "may_affect": []string{"conceptual_model", "logical_model"}, "created_by_decision": "",
			"options": []map[string]any{
				{"id": candidateID + "-generated", "label": "Generated internal identity", "rationale": "Keeps technical identity separate from mutable business fields.", "effect_summary": "Adds a generated logical identity during DB-DSL projection.", "benefits": []string{"Stable references"}, "risks": []string{"Adds an inferred technical key"}, "affected_artifact_kinds": []string{"conceptual_model", "logical_model"}, "recommended": true, "effects": generatedEffects},
				{"id": candidateID + "-natural", "label": "Natural business identity", "rationale": "Uses an explicit business field when one is identified.", "effect_summary": "Requires a source-supported unique business field.", "benefits": []string{"Business-visible key"}, "risks": []string{"May be mutable or absent"}, "affected_artifact_kinds": []string{"conceptual_model", "logical_model"}, "recommended": false, "effects": generatedEffects},
			},
			"recommended_option_id": candidateID + "-generated", "recommendation_confidence": "medium", "warnings": []string{},
		})
	}
	data, err := json.Marshal(map[string]any{
		"review_candidates": candidates,
		"warnings":          []string{}, "confidence_summary": map[string]string{"overall": "mock review proposal"},
	})
	return json.RawMessage(data), err
}

func dynamicReviewResolutionPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		Candidate struct {
			AffectedAtoms []string `json:"affected_atoms"`
		} `json:"candidate"`
		DecisionID string `json:"decision_id"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, err
	}
	operations := make([]map[string]any, 0, len(parsed.Candidate.AffectedAtoms))
	for _, atom := range parsed.Candidate.AffectedAtoms {
		operations = append(operations, map[string]any{"operation": "link_review_decision", "target_id": atom, "field": "review_decisions", "value": parsed.DecisionID})
	}
	data, err := json.Marshal(map[string]any{
		"operations": operations, "affected_artifacts": []string{"requirement_atoms"},
		"explanation":           "Link the explicit human decision to every affected requirement atom.",
		"new_review_candidates": []any{}, "requires_human_review": false,
		"validation_expectations": []string{"all affected atoms reference the decision"}, "warnings": []string{},
	})
	return json.RawMessage(data), err
}

func dynamicConceptualModelPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		SourceUnits []struct {
			ID string `json:"id"`
		} `json:"source_units"`
		RequirementAtoms []struct {
			ID string `json:"id"`
		} `json:"requirement_atoms"`
		ReviewDecisions []string `json:"review_decisions"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, err
	}
	sources, atoms := []string{}, []string{}
	for _, item := range parsed.SourceUnits {
		sources = append(sources, item.ID)
	}
	for _, item := range parsed.RequirementAtoms {
		atoms = append(atoms, item.ID)
	}
	evidence := map[string]any{"source_units": sources, "requirement_atoms": atoms, "review_decisions": parsed.ReviewDecisions, "support_level": "explicit", "confidence": "high", "notes": []string{"Deterministic mock proposal"}}
	data, err := json.Marshal(map[string]any{
		"entity_concepts": []map[string]any{{
			"id": "DomainRecord", "label": "Domain Record", "description": "Persistent record described by the source.", "kind": "regular",
			"attributes": []map[string]any{{"id": "name", "label": "Name", "description": "Human-readable name.", "required": true, "evidence": evidence}},
			"evidence":   evidence,
		}},
		"relationships": []any{}, "lifecycle_concepts": []any{}, "derived_concepts": []any{}, "file_concepts": []any{}, "import_concepts": []any{},
		"unresolved_review_ids": []string{}, "warnings": []string{}, "confidence_summary": map[string]string{"overall": "mock conceptual model"},
	})
	return json.RawMessage(data), err
}

func dynamicLogicalProjectionPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		SourceUnits []struct {
			ID string `json:"id"`
		} `json:"source_units"`
		RequirementAtoms []struct {
			ID string `json:"id"`
		} `json:"requirement_atoms"`
		ReviewDecisions []string `json:"review_decisions"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, err
	}
	sources, atoms := []string{}, []string{}
	for _, item := range parsed.SourceUnits {
		sources = append(sources, item.ID)
	}
	for _, item := range parsed.RequirementAtoms {
		atoms = append(atoms, item.ID)
	}
	evidence := map[string]any{"source_units": sources, "requirement_atoms": atoms, "review_decisions": parsed.ReviewDecisions, "support_level": "explicit", "confidence": "high", "notes": []string{"Deterministic mock projection"}}
	data, err := json.Marshal(map[string]any{
		"operations": []map[string]any{
			{"operation": "add_entity", "entity": map[string]any{
				"id": "DomainRecord", "label": "Domain Record", "description": "Persistent record described by the source.", "table_name": "domain_records", "kind": "regular", "evidence": evidence,
				"attributes": []map[string]any{{"id": "name", "label": "Name", "description": "Human-readable name.", "type": "string", "required": true, "precision": nil, "scale": nil, "default": nil, "source_field": "name", "enum_values": []string{}, "notes": []string{}, "evidence": evidence}},
			}, "relationship": nil, "constraint": nil, "state_machine": nil, "derived_view": nil, "file_spec": nil, "import_spec": nil},
			{"operation": "add_constraint", "entity": nil, "relationship": nil, "constraint": map[string]any{
				"id": "domain_record_name_required", "type": "required", "owner": "DomainRecord", "field": "name", "fields": []string{}, "value": nil, "min": nil, "max": nil, "pattern": "", "expression": "", "condition": nil, "requires": []any{}, "description": "Domain record name is required.", "evidence": evidence,
			}, "state_machine": nil, "derived_view": nil, "file_spec": nil, "import_spec": nil},
		},
		"warnings": []string{}, "unresolved_questions": []string{}, "confidence_summary": map[string]string{"overall": "mock logical projection"},
	})
	return json.RawMessage(data), err
}

func (m *MockClient) GenerateText(ctx context.Context, req TextRequest) (TextResponse, error) {
	_ = ctx
	if m == nil {
		m = NewDefaultMockClient()
	}
	key := "baseline_" + req.Metadata["target"]
	text, ok := m.Text[key]
	if !ok {
		return TextResponse{}, fmt.Errorf("mock LLM has no text response for %s", key)
	}
	raw := fmt.Sprintf(`{"id":"mock_%s","status":"completed","output_text":%q}`, req.Stage, text)
	return TextResponse{
		Provider: "mock",
		Model:    nonEmpty(req.Model, "mock"),
		Raw:      raw,
		Text:     text,
	}, nil
}

const defaultSourceUnitExtractionJSON = `{
  "classifications": [],
  "warnings": [],
  "confidence_summary": {"overall":"mock source-unit classification"}
}`

const defaultRequirementAtomStageJSON = `{"requirement_atoms":[],"warnings":[],"confidence_summary":{"overall":"mock"}}`
const defaultFunctionalAnalysisJSON = `{"functional_areas":[],"actors":[],"warnings":[],"confidence_summary":{"overall":"mock"}}`
const defaultCRUDMappingJSON = `{"operations":[],"warnings":[],"confidence_summary":{"overall":"mock"}}`
const defaultProjectReviewJSON = `{"review_candidates":[],"warnings":[],"confidence_summary":{"overall":"mock"}}`
const defaultReviewResolutionPatchJSON = `{"operations":[],"affected_artifacts":[],"explanation":"","new_review_candidates":[],"requires_human_review":false,"validation_expectations":[],"warnings":[]}`
const defaultConceptualModelJSON = `{"entity_concepts":[],"relationships":[],"lifecycle_concepts":[],"derived_concepts":[],"file_concepts":[],"import_concepts":[],"unresolved_review_ids":[],"warnings":[],"confidence_summary":{"overall":"mock"}}`
const defaultLogicalProjectionJSON = `{"operations":[],"warnings":[],"unresolved_questions":[],"confidence_summary":{"overall":"mock"}}`

const defaultRequirementExtractionJSON = `{
  "requirement_atoms": [
    {
      "id": "LLM-RA-001",
      "statement": "The system stores catalog products described in the task text.",
      "atom_type": "entity",
      "modeling_relevance": "direct_db",
      "source_units": ["RAW-SU-001"],
      "functional_area": "catalog",
      "functional_pattern": "catalog_management",
      "support_level": "explicit",
      "confidence": "high",
      "requires_review": false,
      "modeling_outcome": "represented"
    }
  ],
  "functional_areas": [
    {
      "id": "catalog",
      "label": "Catalog",
      "purpose": "Manage persistent catalog records.",
      "main_actors": ["system"],
      "atoms": ["LLM-RA-001"],
      "modeling_focus": ["Product"]
    }
  ],
  "actors": [
    {
      "id": "system",
      "label": "System",
      "description": "System actor for deterministic test runs."
    }
  ],
  "operations": [
    {
      "id": "manage_catalog",
      "label": "Manage catalog",
      "functional_area": "catalog",
      "functional_pattern": "catalog_management",
      "actor": "system",
      "source_atoms": ["LLM-RA-001"],
      "source_units": ["RAW-SU-001"],
      "description": "Create, inspect and update catalog records."
    }
  ],
  "review_candidates": [],
  "warnings": [],
  "confidence_summary": {"overall": "mock high-confidence extraction"}
}`

const defaultModelPlanJSON = `{
  "candidate_entities": [
    {
      "id": "Product",
      "label": "Product",
      "description": "Persistent catalog product.",
      "table_name": "products",
      "kind": "regular",
      "source_units": ["RAW-SU-001"],
      "requirement_atoms": ["LLM-RA-001"]
    }
  ],
  "candidate_relationships": [],
  "candidate_constraints": [
    {
      "id": "product_name_required",
      "description": "Product name is required.",
      "source_units": ["RAW-SU-001"],
      "requirement_atoms": ["LLM-RA-001"]
    }
  ],
  "candidate_state_machines": [],
  "candidate_derived_views": [],
  "candidate_file_specs": [],
  "review_candidates": [],
  "warnings": [],
  "unresolved_questions": [],
  "confidence_summary": {"overall": "mock model plan"}
}`

const defaultPatchJSON = `{
  "operations": [
    {
      "operation": "add_entity",
      "entity": {
        "id": "Product",
        "label": "Product",
        "description": "Persistent catalog product.",
        "table_name": "products",
        "kind": "regular",
        "evidence": {
          "source_units": ["RAW-SU-001"],
          "requirement_atoms": ["LLM-RA-001"],
          "support_level": "explicit",
          "confidence": "high"
        },
        "attributes": [
          {
            "id": "name",
            "label": "Name",
            "description": "Product name.",
            "type": "string",
            "required": true,
            "evidence": {
              "source_units": ["RAW-SU-001"],
              "requirement_atoms": ["LLM-RA-001"],
              "support_level": "explicit",
              "confidence": "high"
            }
          }
        ]
      }
    },
    {
      "operation": "add_constraint",
      "constraint": {
        "id": "product_name_required",
        "type": "required",
        "owner": "Product",
        "field": "name",
        "description": "Product name is required.",
        "evidence": {
          "source_units": ["RAW-SU-001"],
          "requirement_atoms": ["LLM-RA-001"],
          "support_level": "explicit",
          "confidence": "high"
        }
      }
    }
  ],
  "warnings": [],
  "unresolved_questions": [],
  "confidence_summary": {"overall": "mock DB-DSL patch"}
}`

const defaultRepairJSON = `{
  "patch_operations": [],
  "requires_human_review": true,
  "explanation": "Mock repair logs the issue but does not apply semantic changes automatically.",
  "warnings": []
}`

const defaultBaselineDBML = `Table products {
  id integer [pk]
  name varchar
}`

const defaultBaselineSQL = `CREATE TABLE products (
  id BIGINT PRIMARY KEY,
  name VARCHAR(255)
);`
