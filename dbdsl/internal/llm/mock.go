package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type MockClient struct {
	Structured map[string]json.RawMessage
	Text       map[string]string
}

func NewDefaultMockClient() *MockClient {
	return &MockClient{
		Structured: map[string]json.RawMessage{
			"combined_document":           json.RawMessage(defaultCombinedDocumentJSON),
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
	if req.Stage == "combined_document" && (!ok || string(payload) == defaultCombinedDocumentJSON) {
		var err error
		payload, err = dynamicCombinedDocumentPayload(req.Input)
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

func dynamicSourceUnitPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		CombinedDocument struct {
			Sentences []struct {
				ID         string   `json:"id"`
				Text       string   `json:"text"`
				Confidence string   `json:"confidence"`
				Warnings   []string `json:"warnings"`
			} `json:"sentences"`
		} `json:"combined_document"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, fmt.Errorf("parse mock source-unit input: %w", err)
	}
	if len(parsed.CombinedDocument.Sentences) == 0 {
		return nil, fmt.Errorf("mock source-unit extraction requires combined-document sentences")
	}
	units := make([]map[string]any, 0, len(parsed.CombinedDocument.Sentences))
	for i, sentence := range parsed.CombinedDocument.Sentences {
		confidence := sentence.Confidence
		if confidence == "" {
			confidence = "high"
		}
		requiresReview := confidence == "low" || len(sentence.Warnings) > 0
		warnings := append([]string(nil), sentence.Warnings...)
		if requiresReview && len(warnings) == 0 {
			warnings = append(warnings, "Low-confidence combined-document sentence.")
		}
		units = append(units, map[string]any{
			"id":              fmt.Sprintf("SU-%03d", i+1),
			"kind":            "requirement_sentence",
			"section":         "combined_document",
			"relevance":       "model_relevant",
			"tags":            []string{"mock"},
			"exact_text":      sentence.Text,
			"normalized_text": sentence.Text,
			"od_sentence_ids": []string{sentence.ID},
			"confidence":      confidence,
			"requires_review": requiresReview,
			"warnings":        warnings,
		})
	}
	payload, err := json.Marshal(map[string]any{
		"source_units":       units,
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
	data, err := json.Marshal(map[string]any{
		"operations": []map[string]any{{
			"id": "OP-001", "label": "Manage domain records", "actor_id": "system", "functional_area_id": "core",
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
	data, err := json.Marshal(map[string]any{
		"review_candidates": []map[string]any{{
			"id": "RC-001", "question": "Should domain records use generated internal identity?",
			"description": "The source describes persistent data but does not define a technical primary key.",
			"category":    "identity", "phase": "pre_conceptual", "severity": "medium", "blocking": true,
			"affected_source_units": sources, "affected_atoms": atoms, "affected_functional_areas": areas,
			"affected_operations": operations, "affected_model_candidates": []string{"DomainRecord"},
			"depends_on": []string{}, "may_affect": []string{"conceptual_model", "logical_model"}, "created_by_decision": "",
			"options": []map[string]any{
				{"id": "generated_identity", "label": "Generated internal identity", "rationale": "Keeps technical identity separate from mutable business fields.", "effect_summary": "Adds a generated logical identity during DB-DSL projection.", "benefits": []string{"Stable references"}, "risks": []string{"Adds an inferred technical key"}, "affected_artifact_kinds": []string{"conceptual_model", "logical_model"}, "recommended": true},
				{"id": "natural_identity", "label": "Natural business identity", "rationale": "Uses an explicit business field when one is identified.", "effect_summary": "Requires a source-supported unique business field.", "benefits": []string{"Business-visible key"}, "risks": []string{"May be mutable or absent"}, "affected_artifact_kinds": []string{"conceptual_model", "logical_model"}, "recommended": false},
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

func dynamicCombinedDocumentPayload(input string) (json.RawMessage, error) {
	var parsed struct {
		Resources []struct {
			ID    string `json:"id"`
			Lines []struct {
				Number int    `json:"number"`
				Text   string `json:"text"`
			} `json:"lines"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		return nil, fmt.Errorf("parse mock combined document input: %w", err)
	}
	if len(parsed.Resources) == 0 {
		return nil, fmt.Errorf("mock combined document requires at least one resource")
	}
	resourceID := parsed.Resources[0].ID
	lineNumber := 1
	text := "No source text."
	for _, line := range parsed.Resources[0].Lines {
		if strings.TrimSpace(line.Text) == "" {
			continue
		}
		lineNumber = line.Number
		text = strings.TrimSpace(line.Text)
		break
	}
	payload, err := json.Marshal(map[string]any{
		"sentences": []map[string]any{{
			"id":   "OD-S-001",
			"text": text,
			"derived_from": []map[string]any{{
				"resource_id": resourceID,
				"line_start":  lineNumber,
				"line_end":    lineNumber,
				"exact_text":  text,
			}},
			"transformation": "copied",
			"confidence":     "high",
			"warnings":       []string{},
		}},
		"warnings":           []string{},
		"confidence_summary": map[string]string{"overall": "mock combined document"},
	})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(payload), nil
}

const defaultCombinedDocumentJSON = `{
  "sentences": [
    {
      "id": "OD-S-001",
      "text": "System stores products in a catalog.",
      "derived_from": [
        {
          "resource_id": "R-001",
          "line_start": 1,
          "line_end": 1,
          "exact_text": "System stores products in a catalog."
        }
      ],
      "transformation": "copied",
      "confidence": "high",
      "warnings": []
    }
  ],
  "warnings": [],
  "confidence_summary": {"overall": "mock combined document"}
}`

const defaultSourceUnitExtractionJSON = `{
  "source_units": [
    {
      "id": "SU-001",
      "kind": "requirement_sentence",
      "section": "combined_document",
      "relevance": "model_relevant",
      "tags": ["mock"],
      "exact_text": "System stores products in a catalog.",
      "normalized_text": "System stores products in a catalog.",
      "od_sentence_ids": ["OD-S-001"],
      "confidence": "high",
      "requires_review": false,
      "warnings": []
    }
  ],
  "warnings": [],
  "confidence_summary": {"overall": "mock source-unit extraction"}
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
