package llmpipeline

import (
	"fmt"
	"strings"

	"dbdsl/internal/dsl"
)

const (
	defaultConceptualMaxOutputTokens = 24000
	defaultLogicalMaxOutputTokens    = 32000
)

type LogicalArtifacts struct {
	ReviewDecisions dsl.ReviewDecisionsFile
	Model           dsl.Document
}

// validateConceptLabel rejects labels that carry model commentary instead of a
// short business name; such labels leak into every downstream view and export.
func validateConceptLabel(qa *StageQA, owner, label string) {
	trimmed := strings.TrimSpace(label)
	if trimmed == "" {
		qa.Errors = append(qa.Errors, owner+" has an empty label")
		return
	}
	if strings.ContainsAny(trimmed, "?!") || len(strings.Fields(trimmed)) > 6 {
		qa.Errors = append(qa.Errors, fmt.Sprintf("%s label %q must be a short business name (at most 6 words, no commentary)", owner, trimmed))
	}
}

func ValidateConceptualModel(proposal ConceptualModelProposal, units []dsl.SourceUnit, reviewDecisions []string) StageQA {
	qa := newStageQA(proposal.Warnings)
	sourceIDs, decisionIDs := map[string]bool{}, map[string]bool{}
	for _, item := range units {
		sourceIDs[item.ID] = true
	}
	for _, id := range reviewDecisions {
		decisionIDs[id] = true
	}
	concepts := map[string]bool{}
	fileConcepts := map[string]bool{}
	if len(proposal.EntityConcepts) == 0 {
		qa.Errors = append(qa.Errors, "conceptual model produced no entity concepts")
	}
	validateEvidence := func(owner string, evidence EvidenceProposal) {
		if len(evidence.SourceUnits) == 0 {
			qa.Errors = append(qa.Errors, owner+" has incomplete evidence")
		}
		for _, id := range evidence.SourceUnits {
			if !sourceIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown source unit %s", owner, id))
			}
		}
		for _, id := range evidence.ReviewDecisions {
			if !decisionIDs[id] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown decision %s", owner, id))
			}
		}
		if evidence.SupportLevel == "assumption" && len(evidence.ReviewDecisions) == 0 {
			qa.Errors = append(qa.Errors, owner+" assumption has no review decision")
		}
	}
	for _, concept := range proposal.EntityConcepts {
		if concept.ID == "" || concepts[concept.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate concept id %q", concept.ID))
		}
		concepts[concept.ID] = true
		validateEvidence(concept.ID, concept.Evidence)
		validateConceptLabel(&qa, concept.ID, concept.Label)
		attributeIDs := map[string]bool{}
		columnNames := map[string]string{}
		for _, attribute := range concept.Attributes {
			if attribute.ValueType != "" && !containsString(ConceptualValueTypes, attribute.ValueType) {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s.%s has unsupported value_type %q", concept.ID, attribute.ID, attribute.ValueType))
			}
			if name := attribute.Name; name != "" {
				if !lowerSnakeIdentifierPattern.MatchString(name) {
					qa.Errors = append(qa.Errors, fmt.Sprintf("%s.%s name %q must be a lower snake_case column name", concept.ID, attribute.ID, name))
				} else if other := columnNames[name]; other != "" {
					// The deterministic mapper merges same-named columns; flag it without forcing an LLM repair.
					qa.Warnings = append(qa.Warnings, fmt.Sprintf("%s attributes %s and %s both use column name %q and will be merged", concept.ID, other, attribute.ID, name))
				}
				columnNames[name] = attribute.ID
			}
			if attribute.ID == "" || attributeIDs[attribute.ID] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s has empty or duplicate attribute %q", concept.ID, attribute.ID))
			}
			attributeIDs[attribute.ID] = true
			validateEvidence(concept.ID+"."+attribute.ID, attribute.Evidence)
			validateConceptLabel(&qa, concept.ID+"."+attribute.ID, attribute.Label)
		}
	}
	for _, concept := range proposal.FileConcepts {
		if concept.ID != "" {
			fileConcepts[concept.ID] = true
		}
	}
	for _, relationship := range proposal.Relationships {
		entityToEntity := concepts[relationship.From] && concepts[relationship.To]
		entityToFile := (concepts[relationship.From] && fileConcepts[relationship.To]) ||
			(fileConcepts[relationship.From] && concepts[relationship.To])
		if !entityToEntity && !entityToFile {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown relationship endpoint", relationship.ID))
		}
		if relationship.Cardinality == "unknown" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has unresolved cardinality", relationship.ID))
		}
		validateEvidence(relationship.ID, relationship.Evidence)
	}
	for _, lifecycle := range proposal.LifecycleConcepts {
		if lifecycle.Owner != "" && !concepts[lifecycle.Owner] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("lifecycle %s owner %s is not an entity concept", lifecycle.ID, lifecycle.Owner))
		}
		if len(lifecycle.States) > 0 && lifecycle.Initial != "" && !containsString(lifecycle.States, lifecycle.Initial) {
			qa.Errors = append(qa.Errors, fmt.Sprintf("lifecycle %s initial state %q is not one of its states", lifecycle.ID, lifecycle.Initial))
		}
	}
	constraintIDs := map[string]bool{}
	for _, constraint := range proposal.ConstraintConcepts {
		if constraint.ID == "" || constraintIDs[constraint.ID] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("empty or duplicate constraint concept id %q", constraint.ID))
		}
		constraintIDs[constraint.ID] = true
		if len(constraint.Targets) == 0 {
			qa.Errors = append(qa.Errors, constraint.ID+" has no target")
		}
		validateEvidence(constraint.ID, constraint.Evidence)
	}
	if len(proposal.UnresolvedReviewIDs) > 0 {
		qa.Errors = append(qa.Errors, "conceptual model retains unresolved review IDs: "+strings.Join(proposal.UnresolvedReviewIDs, ", "))
	}
	qa.Coverage["entity_concepts"] = len(proposal.EntityConcepts)
	qa.Coverage["relationships"] = len(proposal.Relationships)
	qa.Coverage["constraint_concepts"] = len(proposal.ConstraintConcepts)
	qa.OK = len(qa.Errors) == 0
	return qa
}

func BuildLogicalArtifacts(name string, sourceUnits []dsl.SourceUnit, patch PatchProposal, decisions []dsl.ReviewDecision) (LogicalArtifacts, error) {
	built, err := buildArtifacts(name, sourceUnits, patch, &conversionDiagnostics{})
	if err != nil {
		return LogicalArtifacts{}, err
	}
	built.Model.Model.Status = "logical_draft_requires_final_review"
	built.Model.Source.ReviewState = "resolved"
	built.Model.Source.AcceptedReviewDecisions = []dsl.AcceptedReviewDecision{}
	for _, decision := range decisions {
		selected, _ := decision.Decision["selected_option"].(string)
		built.Model.Source.AcceptedReviewDecisions = append(built.Model.Source.AcceptedReviewDecisions, dsl.AcceptedReviewDecision{ReviewID: decision.ID, SelectedOption: selected})
	}
	built.ReviewDecisions.ReviewDecisions = decisions
	built.ReviewDecisions.ReviewState = map[string]any{"status": "resolved", "all_required_reviews_resolved": true, "unresolved_requires_review_flags": 0}
	return LogicalArtifacts{ReviewDecisions: built.ReviewDecisions, Model: built.Model}, nil
}
