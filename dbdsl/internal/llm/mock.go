package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
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

func dynamicSourceSegmentationPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		Candidates []struct {
			ID            string `json:"id"`
			ResourceID    string `json:"resource_id"`
			Text          string `json:"text"`
			SuggestedRole string `json:"suggested_role"`
			Scope         string `json:"scope"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, fmt.Errorf("parse mock source-segmentation input: %w", err)
	}
	classifications := []map[string]any{}
	previousByResource := map[string]string{}
	previousTextByResource := map[string]string{}
	for _, candidate := range parsed.Candidates {
		if candidate.Scope != "core" {
			previousByResource[candidate.ResourceID] = candidate.ID
			previousTextByResource[candidate.ResourceID] = candidate.Text
			continue
		}
		role := "sentence"
		lower := strings.ToLower(candidate.Text)
		if strings.Contains(lower, "универзитет у београду") || strings.Contains(lower, "univerzitet u beogradu") {
			role = "layout_noise"
		} else if strings.HasPrefix(strings.TrimSpace(candidate.Text), "{") || strings.HasPrefix(strings.TrimSpace(candidate.Text), "[") {
			role = "structured_example"
		}
		boundary := "start"
		previous := previousTextByResource[candidate.ResourceID]
		trimmed := strings.TrimSpace(candidate.Text)
		if role != "layout_noise" && previousByResource[candidate.ResourceID] != "" &&
			(!strings.HasSuffix(strings.TrimSpace(previous), ".") || startsWithLower(trimmed)) {
			boundary = "continue"
		}
		classifications = append(classifications, map[string]any{
			"candidate_id": candidate.ID, "role": role, "boundary": boundary, "join_to_candidate_id": "",
			"confidence": "high", "requires_review": false, "warnings": []string{},
		})
		previousByResource[candidate.ResourceID] = candidate.ID
		previousTextByResource[candidate.ResourceID] = candidate.Text
	}
	data, err := json.Marshal(map[string]any{
		"classifications": classifications, "warnings": []string{},
		"confidence_summary": map[string]string{"overall": "mock source segmentation"},
	})
	return json.RawMessage(data), err
}

func startsWithLower(value string) bool {
	for _, r := range value {
		return unicode.IsLower(r)
	}
	return false
}

func dynamicSourceUnitPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		Sentences []struct {
			ID   string `json:"id"`
			Kind string `json:"kind"`
		} `json:"sentences"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, fmt.Errorf("parse mock source-unit input: %w", err)
	}
	if len(parsed.Sentences) == 0 {
		return nil, fmt.Errorf("mock source-unit extraction requires combined-document sentences")
	}
	units := make([]map[string]any, 0, len(parsed.Sentences))
	for _, sentence := range parsed.Sentences {
		kind := "requirement_sentence"
		relevance := "model_relevant"
		if sentence.Kind == "structural" {
			kind = "heading"
			relevance = "model_supporting"
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
		statement := source.Normalized
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
			"confidence": "high", "requires_review": false, "modeling_outcome": "represented",
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
			ID string `json:"id"`
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
	sources, atoms, areas, operations := []string{}, []string{}, []string{}, []string{}
	for _, item := range parsed.SourceUnits {
		sources = append(sources, item.ID)
	}
	for _, item := range parsed.RequirementAtoms {
		atoms = append(atoms, item.ID)
	}
	for _, item := range parsed.FunctionalAreas {
		areas = append(areas, item.ID)
	}
	for _, item := range parsed.CRUDOperations {
		operations = append(operations, item.ID)
	}
	atomUpdates := make([]map[string]any, 0, len(atoms))
	for _, atomID := range atoms {
		atomUpdates = append(atomUpdates, map[string]any{"atom_id": atomID, "modeling_outcome": "represented", "persistence_effect": "required", "support_level": "no_change", "confidence": "no_change"})
	}
	generatedEffects := map[string]any{"modeling_outcome": "represented", "persistence_effect": "required", "support_level": "no_change", "requires_followup": false, "atom_updates": atomUpdates, "impact_dimensions": []string{"identity", "key"}, "followup_candidate_ids": []string{}}
	data, err := json.Marshal(map[string]any{
		"review_candidates": []map[string]any{{
			"id": "RC-001", "decision_key": "domain_record_identity", "question": "Should domain records use generated internal identity?",
			"description": "The source describes persistent data but does not define a technical primary key.",
			"category":    "identity", "phase": "pre_conceptual", "severity": "medium", "blocking": true,
			"affected_source_units": sources, "affected_atoms": atoms, "affected_functional_areas": areas,
			"affected_operations": operations, "affected_model_candidates": []string{"DomainRecord"},
			"depends_on": []string{}, "may_affect": []string{"conceptual_model", "logical_model"}, "created_by_decision": "",
			"options": []map[string]any{
				{"id": "generated_identity", "label": "Generated internal identity", "rationale": "Keeps technical identity separate from mutable business fields.", "effect_summary": "Adds a generated logical identity during DB-DSL projection.", "benefits": []string{"Stable references"}, "risks": []string{"Adds an inferred technical key"}, "affected_artifact_kinds": []string{"conceptual_model", "logical_model"}, "recommended": true, "effects": generatedEffects},
				{"id": "natural_identity", "label": "Natural business identity", "rationale": "Uses an explicit business field when one is identified.", "effect_summary": "Requires a source-supported unique business field.", "benefits": []string{"Business-visible key"}, "risks": []string{"May be mutable or absent"}, "affected_artifact_kinds": []string{"conceptual_model", "logical_model"}, "recommended": false, "effects": generatedEffects},
			},
			"recommended_option_id": "generated_identity", "recommendation_confidence": "medium", "warnings": []string{},
		}},
		"warnings": []string{}, "confidence_summary": map[string]string{"overall": "mock review proposal"},
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
