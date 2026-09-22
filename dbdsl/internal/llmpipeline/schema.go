package llmpipeline

import "sort"

func sourceSegmentationSchema() map[string]any {
	return object(map[string]any{
		"classifications": array(object(map[string]any{
			"candidate_id":         str(),
			"role":                 enum("sentence", "heading", "list_item", "footnote", "structured_example", "external_reference", "layout_noise", "other_structural"),
			"boundary":             enum("start", "continue", "resume"),
			"join_to_candidate_id": str(),
			"confidence":           enum("high", "medium", "low"),
			"requires_review":      boolSchema(),
			"warnings":             array(str()),
		})),
		"warnings":           array(str()),
		"confidence_summary": confidenceSummarySchema(),
	})
}

func sourceUnitClassificationSchema() map[string]any {
	return object(map[string]any{
		"classifications": array(object(map[string]any{
			"od_sentence_id": str(),
			"kind":           enum("requirement_sentence", "business_rule", "actor", "operation", "data_example", "structured_example", "ui_requirement", "report_requirement", "file_requirement", "heading", "noise"),
			"section":        str(), "relevance": enum("model_relevant", "model_supporting", "non_model", "example"),
			"tags": array(str()), "confidence": enum("high", "medium", "low"),
			"requires_review": boolSchema(), "warnings": array(str()),
		})),
		"warnings": array(str()), "confidence_summary": confidenceSummarySchema(),
	})
}

func requirementAtomExtractionSchema() map[string]any {
	return object(map[string]any{
		"requirement_atoms": array(object(map[string]any{
			"id":                 str(),
			"statement":          str(),
			"subject":            str(),
			"predicate":          str(),
			"object":             str(),
			"quantifier":         str(),
			"condition":          str(),
			"temporal_semantics": str(),
			"ownership":          str(),
			"atom_type":          str(),
			"modeling_relevance": enum("direct_db", "model_supporting", "non_model", "ui_only", "application_logic", "external"),
			"source_units":       array(str()),
			"functional_area":    str(),
			"functional_pattern": str(),
			"support_level":      enum("explicit", "example_based", "inferred", "assumption"),
			"confidence":         enum("high", "medium", "low"),
			"requires_review":    boolSchema(),
			"modeling_outcome":   enum("represented", "intentionally_not_in_db", "requires_app_logic", "external_system", "unsupported", "deferred"),
			"persistence_effect": enum("required", "derived_basis", "audit_history", "not_required", "external", "unclear"),
			"example_role":       enum("none", "schema_shape", "seed_data", "constraint_boundary", "illustrative_instance"),
			"warnings":           array(str()),
		})),
		"warnings":           array(str()),
		"confidence_summary": confidenceSummarySchema(),
	})
}

func functionalAnalysisSchema() map[string]any {
	return object(map[string]any{
		"functional_areas": array(object(map[string]any{
			"id":             str(),
			"label":          str(),
			"purpose":        str(),
			"main_actors":    array(str()),
			"atoms":          array(str()),
			"modeling_focus": array(str()),
			"confidence":     enum("high", "medium", "low"),
			"warnings":       array(str()),
		})),
		"actors": array(object(map[string]any{
			"id":          str(),
			"label":       str(),
			"description": str(),
			"kind":        enum("human", "system", "organization", "external"),
		})),
		"warnings":           array(str()),
		"confidence_summary": confidenceSummarySchema(),
	})
}

func crudMappingSchema() map[string]any {
	return object(map[string]any{
		"operations": array(object(map[string]any{
			"id":                 str(),
			"label":              str(),
			"actor_id":           str(),
			"functional_area_id": str(),
			"creates":            array(str()),
			"reads":              array(str()),
			"updates":            array(str()),
			"deletes":            array(str()),
			"persistent_data":    array(str()),
			"outcome":            str(),
			"requirement_atoms":  array(str()),
			"source_units":       array(str()),
			"requires_review":    boolSchema(),
			"warnings":           array(str()),
		})),
		"warnings":           array(str()),
		"confidence_summary": confidenceSummarySchema(),
	})
}

func projectReviewSchema() map[string]any {
	return object(map[string]any{
		"review_candidates":  array(projectReviewCandidateSchema()),
		"warnings":           array(str()),
		"confidence_summary": confidenceSummarySchema(),
	})
}

func projectReviewCandidateSchema() map[string]any {
	return object(map[string]any{
		"id":                        str(),
		"decision_key":              str(),
		"question":                  str(),
		"description":               str(),
		"category":                  str(),
		"phase":                     str(),
		"severity":                  enum("low", "medium", "high", "critical"),
		"blocking":                  boolSchema(),
		"affected_source_units":     array(str()),
		"affected_atoms":            array(str()),
		"affected_functional_areas": array(str()),
		"affected_operations":       array(str()),
		"affected_model_candidates": array(str()),
		"depends_on":                array(str()),
		"may_affect":                array(str()),
		"created_by_decision":       str(),
		"options": array(object(map[string]any{
			"id":                      str(),
			"label":                   str(),
			"rationale":               str(),
			"effect_summary":          str(),
			"benefits":                array(str()),
			"risks":                   array(str()),
			"affected_artifact_kinds": array(str()),
			"recommended":             boolSchema(),
			"effects": object(map[string]any{
				"modeling_outcome":   enum("no_change", "represented", "intentionally_not_in_db", "requires_app_logic", "external_system", "unsupported", "deferred"),
				"persistence_effect": enum("no_change", "required", "derived_basis", "audit_history", "not_required", "external", "unclear"),
				"support_level":      enum("no_change", "explicit", "example_based", "inferred", "assumption"),
				"requires_followup":  boolSchema(),
				"atom_updates": array(object(map[string]any{
					"atom_id":            str(),
					"modeling_outcome":   enum("no_change", "represented", "intentionally_not_in_db", "requires_app_logic", "external_system", "unsupported", "deferred"),
					"persistence_effect": enum("no_change", "required", "derived_basis", "audit_history", "not_required", "external", "unclear"),
					"support_level":      enum("no_change", "explicit", "example_based", "inferred", "assumption"),
					"confidence":         enum("no_change", "high", "medium", "low"),
				})),
				"impact_dimensions":      array(enum("identity", "key", "cardinality", "ownership", "lifecycle", "history", "persistence", "security", "enforceability", "derived_data", "naming", "documentation")),
				"followup_candidate_ids": array(str()),
			}),
		})),
		"recommended_option_id":     str(),
		"recommendation_confidence": enum("high", "medium", "low"),
		"warnings":                  array(str()),
	})
}

func reviewResolutionPatchSchema() map[string]any {
	return object(map[string]any{
		"operations": array(object(map[string]any{
			"operation": enum("link_review_decision", "update_modeling_outcome", "update_persistence_effect", "update_support_level", "update_confidence", "mark_deferred"),
			"target_id": str(),
			"field":     str(),
			"value":     str(),
		})),
		"affected_artifacts":      array(str()),
		"explanation":             str(),
		"new_review_candidates":   array(projectReviewCandidateSchema()),
		"requires_human_review":   boolSchema(),
		"validation_expectations": array(str()),
		"warnings":                array(str()),
	})
}

func conceptualModelSchema() map[string]any {
	conceptEvidence := evidenceSchema()
	planElements := array(object(map[string]any{
		"id": str(), "label": str(), "description": str(), "table_name": str(), "kind": str(),
		"source_units": array(str()), "requirement_atoms": array(str()),
	}))
	return object(map[string]any{
		"entity_concepts": array(object(map[string]any{
			"id": str(), "label": str(), "description": str(), "kind": enum("regular", "lookup", "association", "weak"),
			"attributes": array(object(map[string]any{
				"id": str(), "label": str(), "description": str(), "required": boolSchema(), "evidence": conceptEvidence,
			})),
			"evidence": conceptEvidence,
		})),
		"relationships": array(object(map[string]any{
			"id": str(), "label": str(), "description": str(), "from": str(), "to": str(),
			"cardinality": enum("one_to_one", "one_to_many", "many_to_one", "many_to_many", "unknown"), "evidence": conceptEvidence,
		})),
		"constraint_concepts": array(object(map[string]any{
			"id": str(), "label": str(), "description": str(),
			"kind":    enum("uniqueness", "check", "cardinality", "ownership", "security", "temporal", "cross_row", "application_enforced"),
			"targets": array(str()), "evidence": conceptEvidence,
		})),
		"lifecycle_concepts": planElements, "derived_concepts": planElements, "file_concepts": planElements, "import_concepts": planElements,
		"unresolved_review_ids": array(str()), "warnings": array(str()), "confidence_summary": confidenceSummarySchema(),
	})
}

func requirementExtractionSchema() map[string]any {
	return object(map[string]any{
		"requirement_atoms": array(object(map[string]any{
			"id":                 str(),
			"statement":          str(),
			"atom_type":          str(),
			"modeling_relevance": str(),
			"source_units":       array(str()),
			"functional_area":    str(),
			"functional_pattern": str(),
			"support_level":      enum("explicit", "example_based", "inferred", "assumption"),
			"confidence":         enum("high", "medium", "low"),
			"requires_review":    boolSchema(),
			"modeling_outcome":   enum("represented", "intentionally_not_in_db", "requires_app_logic", "external_system", "unsupported", "deferred"),
		})),
		"functional_areas": array(object(map[string]any{
			"id":             str(),
			"label":          str(),
			"purpose":        str(),
			"main_actors":    array(str()),
			"atoms":          array(str()),
			"modeling_focus": array(str()),
		})),
		"actors": array(object(map[string]any{
			"id":          str(),
			"label":       str(),
			"description": str(),
		})),
		"operations": array(object(map[string]any{
			"id":                 str(),
			"label":              str(),
			"functional_area":    str(),
			"functional_pattern": str(),
			"actor":              str(),
			"source_atoms":       array(str()),
			"source_units":       array(str()),
			"description":        str(),
		})),
		"review_candidates":  reviewCandidatesSchema(),
		"warnings":           array(str()),
		"confidence_summary": confidenceSummarySchema(),
	})
}

func modelPlanSchema() map[string]any {
	planElements := array(object(map[string]any{
		"id":                str(),
		"label":             str(),
		"description":       str(),
		"table_name":        str(),
		"kind":              str(),
		"source_units":      array(str()),
		"requirement_atoms": array(str()),
	}))
	return object(map[string]any{
		"candidate_entities":       planElements,
		"candidate_relationships":  planElements,
		"candidate_constraints":    planElements,
		"candidate_state_machines": planElements,
		"candidate_derived_views":  planElements,
		"candidate_file_specs":     planElements,
		"review_candidates":        reviewCandidatesSchema(),
		"warnings":                 array(str()),
		"unresolved_questions":     array(str()),
		"confidence_summary":       confidenceSummarySchema(),
	})
}

func patchSchema() map[string]any {
	return object(map[string]any{
		"operations":           array(patchOperationSchema()),
		"warnings":             array(str()),
		"unresolved_questions": array(str()),
		"confidence_summary":   confidenceSummarySchema(),
	})
}

func repairSchema() map[string]any {
	return object(map[string]any{
		"patch_operations":      array(patchOperationSchema()),
		"requires_human_review": boolSchema(),
		"explanation":           str(),
		"warnings":              array(str()),
	})
}

func patchOperationSchema() map[string]any {
	return object(map[string]any{
		"operation":     enum("add_entity", "add_relationship", "add_constraint", "add_state_machine", "add_derived_view", "add_file_spec", "add_import_spec"),
		"entity":        nullable(entitySchema()),
		"relationship":  nullable(relationshipSchema()),
		"constraint":    nullable(constraintSchema()),
		"state_machine": nullable(stateMachineSchema()),
		"derived_view":  nullable(derivedViewSchema()),
		"file_spec":     nullable(fileSpecSchema()),
		"import_spec":   nullable(importSpecSchema()),
	})
}

func entitySchema() map[string]any {
	return object(map[string]any{
		"id":          str(),
		"label":       str(),
		"description": str(),
		"table_name":  str(),
		"kind":        enum("regular", "lookup", "association"),
		"evidence":    evidenceSchema(),
		"attributes":  array(attributeSchema()),
	})
}

func attributeSchema() map[string]any {
	return object(map[string]any{
		"id":           str(),
		"label":        str(),
		"description":  str(),
		"type":         enum("id", "string", "text", "integer", "decimal", "boolean", "date", "time", "datetime", "uuid", "email", "phone", "url", "file_path", "money"),
		"required":     boolSchema(),
		"precision":    nullable(map[string]any{"type": "integer"}),
		"scale":        nullable(map[string]any{"type": "integer"}),
		"default":      nullable(map[string]any{"type": "string"}),
		"source_field": str(),
		"enum_values":  array(str()),
		"notes":        array(str()),
		"evidence":     evidenceSchema(),
	})
}

func relationshipSchema() map[string]any {
	return object(map[string]any{
		"id":          str(),
		"label":       str(),
		"description": str(),
		"from":        str(),
		"to":          str(),
		"cardinality": enum("one_to_one", "one_to_many", "many_to_one", "many_to_many"),
		"required":    boolSchema(),
		"fk_required": boolSchema(),
		"on_delete":   enum("", "restrict", "cascade", "set_null"),
		"identifying": boolSchema(),
		"through":     str(),
		"notes":       array(str()),
		"evidence":    evidenceSchema(),
	})
}

func constraintSchema() map[string]any {
	return object(map[string]any{
		"id":          str(),
		"type":        enum("required", "unique", "min_inclusive", "min_exclusive", "max_inclusive", "max_exclusive", "length", "regex", "check", "conditional_required"),
		"owner":       str(),
		"field":       str(),
		"fields":      array(str()),
		"value":       nullable(map[string]any{"type": "string"}),
		"min":         nullable(map[string]any{"type": "string"}),
		"max":         nullable(map[string]any{"type": "string"}),
		"pattern":     str(),
		"expression":  str(),
		"description": str(),
		"evidence":    evidenceSchema(),
	})
}

func stateMachineSchema() map[string]any {
	return object(map[string]any{
		"id":       str(),
		"owner":    str(),
		"field":    str(),
		"states":   array(str()),
		"initial":  str(),
		"terminal": array(str()),
		"transitions": array(object(map[string]any{
			"from": str(),
			"to":   str(),
		})),
		"notes":    array(str()),
		"evidence": evidenceSchema(),
	})
}

func derivedViewSchema() map[string]any {
	return object(map[string]any{
		"id":          str(),
		"label":       str(),
		"description": str(),
		"kind":        enum("projection", "aggregate", "report"),
		"sources":     array(str()),
		"persistence": enum("virtual", "materialized_candidate"),
		"metrics":     array(str()),
		"filters":     array(str()),
		"notes":       array(str()),
		"evidence":    evidenceSchema(),
	})
}

func fileSpecSchema() map[string]any {
	return object(map[string]any{
		"id":                 str(),
		"owner":              str(),
		"field":              str(),
		"allowed_extensions": array(str()),
		"max_size_mb":        nullable(map[string]any{"type": "integer"}),
		"mime_types":         array(str()),
		"storage":            enum("path", "url", "external_reference"),
		"notes":              array(str()),
		"evidence":           evidenceSchema(),
	})
}

func importSpecSchema() map[string]any {
	return object(map[string]any{
		"id":          str(),
		"label":       str(),
		"description": str(),
		"format":      enum("json", "csv", "sql_seed", "manual"),
		"source": object(map[string]any{
			"fragment":     str(),
			"source_id":    str(),
			"file":         str(),
			"source_units": array(str()),
		}),
		"root": str(),
		"mappings": array(object(map[string]any{
			"source_path": str(),
			"target":      str(),
			"notes":       array(str()),
		})),
		"evidence": evidenceSchema(),
	})
}

func evidenceSchema() map[string]any {
	return object(map[string]any{
		"source_units":      array(str()),
		"requirement_atoms": array(str()),
		"review_decisions":  array(str()),
		"support_level":     enum("explicit", "example_based", "inferred", "assumption"),
		"confidence":        enum("high", "medium", "low"),
		"notes":             array(str()),
	})
}

func reviewCandidatesSchema() map[string]any {
	return array(object(map[string]any{
		"id":                    str(),
		"question":              str(),
		"affected_atoms":        array(str()),
		"recommended_option_id": str(),
		"rationale":             str(),
	}))
}

func object(properties map[string]any) map[string]any {
	required := make([]string, 0, len(properties))
	for key := range properties {
		required = append(required, key)
	}
	sort.Strings(required)
	return map[string]any{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func array(items any) map[string]any {
	return map[string]any{"type": "array", "items": items}
}

func str() map[string]any {
	return map[string]any{"type": "string"}
}

func boolSchema() map[string]any {
	return map[string]any{"type": "boolean"}
}

func intSchema() map[string]any {
	return map[string]any{"type": "integer"}
}

func enum(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}

func nullable(schema map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range schema {
		out[key] = value
	}
	if typ, ok := out["type"].(string); ok {
		out["type"] = []string{typ, "null"}
		return out
	}
	return map[string]any{"anyOf": []any{schema, map[string]any{"type": "null"}}}
}

func confidenceSummarySchema() map[string]any {
	return object(map[string]any{
		"overall": str(),
		"risks":   str(),
	})
}
