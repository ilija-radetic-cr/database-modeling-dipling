package llmpipeline

import (
	"sort"
	"strings"
	"unicode"
)

const (
	ReviewClassNone                 = "none"
	ReviewClassNonBlockingGap       = "non_blocking_gap"
	ReviewClassExternalDependency   = "external_dependency"
	ReviewClassBlockingModelChoice  = "blocking_model_choice"
	ReviewClassBlockingSourceDefect = "blocking_source_defect"
)

var RequirementReviewClasses = []string{
	ReviewClassNone,
	ReviewClassNonBlockingGap,
	ReviewClassExternalDependency,
	ReviewClassBlockingModelChoice,
	ReviewClassBlockingSourceDefect,
}

var RequirementReviewTopics = []string{
	"none", "persistence", "identity", "cardinality", "uniqueness", "lifecycle",
	"enforcement", "example_structure", "external_contract", "other",
}

func IsBlockingReviewClass(value string) bool {
	return value == ReviewClassBlockingModelChoice || value == ReviewClassBlockingSourceDefect
}

// NormalizeRequirementReviewSemantics makes the backend, rather than the model,
// authoritative for review blocking. It also accepts legacy payloads so stored
// historical artifacts and hand-written tests remain readable.
func NormalizeRequirementReviewSemantics(atoms []RequirementAtomProposal) []RequirementAtomProposal {
	out := append([]RequirementAtomProposal(nil), atoms...)
	for i := range out {
		atom := &out[i]
		atom.ReviewClass = strings.TrimSpace(atom.ReviewClass)
		atom.ReviewTopic = strings.TrimSpace(atom.ReviewTopic)
		atom.ReviewGroup = strings.TrimSpace(atom.ReviewGroup)

		if atom.ReviewClass == "" {
			switch {
			case atom.RequiresReview:
				atom.ReviewClass = ReviewClassBlockingModelChoice
			case len(atom.Warnings) > 0:
				atom.ReviewClass = ReviewClassNonBlockingGap
			default:
				atom.ReviewClass = ReviewClassNone
			}
		}
		if atom.ReviewTopic == "" {
			atom.ReviewTopic = inferredReviewTopic(*atom)
		}

		if atom.AtomType == "example" && atom.ExampleRole == "illustrative_instance" {
			atom.ModelingOutcome = "intentionally_not_in_db"
			atom.PersistenceEffect = "not_required"
			atom.ReviewClass = ReviewClassNone
			atom.ReviewTopic = "none"
			atom.ReviewGroup = ""
		}

		if atom.ModelingOutcome == "represented" && atom.PersistenceEffect == "not_required" {
			switch {
			case atom.ModelingRelevance == "application_logic":
				atom.ModelingOutcome = "requires_app_logic"
			case atom.ModelingRelevance == "external":
				atom.ModelingOutcome = "external_system"
			case atom.ModelingRelevance == "ui_only" || atom.ModelingRelevance == "non_model" ||
				atom.AtomType == "ui_behavior" || atom.AtomType == "navigation" || atom.AtomType == "notification" || atom.AtomType == "technology_constraint":
				atom.ModelingOutcome = "intentionally_not_in_db"
			default:
				atom.ReviewClass = ReviewClassBlockingModelChoice
				atom.ReviewTopic = "persistence"
			}
		}
		if (atom.ModelingOutcome == "deferred" || atom.ModelingOutcome == "unsupported" || atom.PersistenceEffect == "unclear") && len(atom.ReviewDecisions) == 0 {
			atom.ReviewClass = ReviewClassBlockingModelChoice
			if atom.ReviewTopic == "none" {
				atom.ReviewTopic = inferredReviewTopic(*atom)
			}
		}

		if (atom.SupportLevel == "assumption" || atom.Confidence == "low") && persistentModelImpact(*atom) && len(atom.ReviewDecisions) == 0 {
			atom.ReviewClass = ReviewClassBlockingModelChoice
			if atom.ReviewTopic == "none" {
				atom.ReviewTopic = inferredReviewTopic(*atom)
			}
		}

		atom.RequiresReview = IsBlockingReviewClass(atom.ReviewClass) && len(atom.ReviewDecisions) == 0
		if atom.RequiresReview && atom.ReviewGroup == "" {
			atom.ReviewGroup = stableReviewGroup(*atom)
		}
		if !atom.RequiresReview && len(atom.ReviewDecisions) > 0 {
			atom.ReviewGroup = ""
		}
	}
	return out
}

func persistentModelImpact(atom RequirementAtomProposal) bool {
	switch atom.PersistenceEffect {
	case "required", "derived_basis", "audit_history", "external", "unclear":
		return true
	}
	return atom.ModelingOutcome == "represented" || atom.ModelingOutcome == "deferred" || atom.ModelingOutcome == "unsupported"
}

func inferredReviewTopic(atom RequirementAtomProposal) string {
	switch atom.AtomType {
	case "entity_identity", "actor_role":
		return "identity"
	case "cardinality_constraint", "relationship":
		return "cardinality"
	case "uniqueness_constraint":
		return "uniqueness"
	case "state_transition", "lifecycle_event", "history_requirement":
		return "lifecycle"
	case "validation_rule", "business_rule", "authorization_rule":
		return "enforcement"
	case "example":
		return "example_structure"
	}
	switch atom.PersistenceEffect {
	case "external":
		return "external_contract"
	case "unclear", "not_required":
		return "persistence"
	}
	if atom.ReviewClass == ReviewClassNone {
		return "none"
	}
	return "other"
}

func stableReviewGroup(atom RequirementAtomProposal) string {
	ids := append([]string(nil), atom.SourceUnits...)
	sort.Strings(ids)
	base := atom.ReviewTopic + "-" + strings.Join(ids, "-")
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(base) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
		} else if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
