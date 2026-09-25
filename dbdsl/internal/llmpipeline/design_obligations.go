package llmpipeline

import (
	"fmt"
	"sort"
	"strings"
)

var obligationKinds = map[string]bool{
	"identity": true, "key": true, "attribute": true, "relationship": true,
	"cardinality": true, "ownership": true, "lifecycle": true,
	"event_history": true, "generated_snapshot": true, "derived_view": true,
	"invariant": true, "security": true, "completeness": true, "transient": true,
}

// DeriveDesignObligations provides a deterministic v0.7 bridge over the
// existing RA contract. It deliberately separates where behavior executes
// from what data must exist to verify that behavior.
func DeriveDesignObligations(atoms []RequirementAtomProposal) (DesignObligationsFile, DesignObligationQA) {
	atoms = NormalizeRequirementReviewSemantics(atoms)
	items := make([]DesignObligation, 0, len(atoms))
	for _, atom := range atoms {
		for _, classified := range classifyObligations(atom) {
			status := "accepted"
			if classified.persistence == "not_required" {
				status = "not_required"
			}
			items = append(items, DesignObligation{
				ID: fmt.Sprintf("DO-%04d", len(items)+1), Statement: atom.Statement, Kind: classified.kind,
				Persistence: classified.persistence, SourceUnits: append([]string(nil), atom.SourceUnits...),
				RequirementAtoms: []string{atom.ID}, VerificationTarget: classified.target, Risk: classified.risk,
				RequiresReview: classified.persistence == "unresolved" || (classified.persistence != "not_required" && atom.RequiresReview),
				Status:         status, Rationale: obligationRationale(classified.kind, classified.persistence),
			})
		}
	}
	file := DesignObligationsFile{
		Document:          map[string]any{"pipeline_version": "0.7.3", "policy_version": "design_obligations/v0.7.2", "derivation_strategy": "deterministic_from_requirement_atoms"},
		DesignObligations: items,
	}
	return file, ValidateDesignObligations(file, atoms)
}

type obligationClassification struct{ kind, persistence, risk, target string }

func classifyObligations(atom RequirementAtomProposal) []obligationClassification {
	kind, persistence, risk, target := classifyObligation(atom)
	out := []obligationClassification{{kind, persistence, risk, target}}
	if persistence == "not_required" || persistence == "unresolved" {
		return out
	}
	// A typed atom already states what it is; keyword-derived extra obligations
	// (e.g. "must" in "the application must allow") would only add false invariants.
	if _, typed := typedAtomObligation(atom); typed {
		return out
	}
	text := strings.ToLower(atom.Statement + " " + atom.AtomType + " " + atom.ModelingRelevance)
	seen := map[string]bool{kind: true}
	add := func(candidate obligationClassification) {
		if !seen[candidate.kind] {
			seen[candidate.kind] = true
			out = append(out, candidate)
		}
	}
	if containsAny(text, "score", "point", "attempt", "answer", "move", "history", "rezultat", "poen", "bod", "pokušaj", "odgovor", "potez", "istor") {
		add(obligationClassification{"event_history", "required", "high", "Retain enough typed facts to reconstruct and verify the result."})
	}
	if containsAny(text, "accept", "reject", "status", "start", "complete", "future", "prihvat", "odbij", "započ", "počin", "zavr", "buduć") {
		add(obligationClassification{"lifecycle", "required", "high", "Represent the relevant state or transition and its temporal boundary."})
	}
	if containsAny(text, "exactly", "at most", "must", "only", "tačno", "najviše", "mora", "samo") {
		add(obligationClassification{"invariant", "required", "high", "Represent the rule and identify its enforcement strategy."})
	}
	return out
}

func ValidateDesignObligations(file DesignObligationsFile, atoms []RequirementAtomProposal) DesignObligationQA {
	qa := DesignObligationQA{RequirementAtoms: len(atoms), Obligations: len(file.DesignObligations), Errors: []string{}, Warnings: []string{}, NeedsAttention: []string{}}
	atomIDs := map[string]bool{}
	for _, atom := range atoms {
		atomIDs[atom.ID] = true
	}
	covered := map[string]bool{}
	seen := map[string]bool{}
	for _, item := range file.DesignObligations {
		if item.ID == "" || seen[item.ID] {
			qa.Errors = append(qa.Errors, "empty or duplicate design obligation id: "+item.ID)
		}
		seen[item.ID] = true
		if !obligationKinds[item.Kind] {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has unknown kind %q", item.ID, item.Kind))
		}
		if item.Persistence != "required" && item.Persistence != "derived" && item.Persistence != "not_required" && item.Persistence != "unresolved" {
			qa.Errors = append(qa.Errors, fmt.Sprintf("%s has unknown persistence %q", item.ID, item.Persistence))
		}
		for _, atomID := range item.RequirementAtoms {
			if !atomIDs[atomID] {
				qa.Errors = append(qa.Errors, fmt.Sprintf("%s references unknown atom %s", item.ID, atomID))
			} else {
				covered[atomID] = true
			}
		}
		if item.RequiresReview {
			qa.NeedsAttention = append(qa.NeedsAttention, item.ID)
		}
	}
	for _, atom := range atoms {
		if !covered[atom.ID] {
			qa.UncoveredAtomIDs = append(qa.UncoveredAtomIDs, atom.ID)
		}
	}
	sort.Strings(qa.UncoveredAtomIDs)
	if len(qa.UncoveredAtomIDs) > 0 {
		qa.Errors = append(qa.Errors, "one or more requirement atoms have no design obligation")
	}
	qa.CoveredAtoms = len(covered)
	qa.OK = len(qa.Errors) == 0
	return qa
}

// RequirementAtomTypes is the closed atom_type vocabulary the extraction stage
// must use; typed atoms map to obligations without keyword guessing.
var RequirementAtomTypes = []string{"entity_identity", "persistent_data", "data_attribute", "relationship", "cardinality_constraint", "ownership_rule", "uniqueness_constraint", "validation_rule", "business_rule", "state_transition", "lifecycle_event", "history_requirement", "generated_value", "derived_value", "report_query", "authorization_rule", "actor_role", "operation", "file_import", "notification", "ui_behavior", "navigation", "technology_constraint", "example", "other"}

// typedObligations maps an atom_type to its design obligation. Types absent
// from the map (operation-free legacy values, "other") fall back to keywords.
var typedObligations = map[string]obligationClassification{
	"entity_identity":        {"identity", "required", "high", "Provide a stable identity for the concept."},
	"persistent_data":        {"attribute", "required", "medium", "Map the requirement to a typed model element or justify exclusion."},
	"data_attribute":         {"attribute", "required", "medium", "Map the stated value to a typed attribute."},
	"actor_role":             {"attribute", "required", "medium", "Represent the actor or role that the system must distinguish."},
	"operation":              {"attribute", "required", "medium", "Represent the persistent data that the operation creates or changes."},
	"file_import":            {"attribute", "required", "medium", "Represent the imported data as typed model elements."},
	"relationship":           {"relationship", "required", "high", "Represent the association between the concepts."},
	"cardinality_constraint": {"cardinality", "required", "high", "Represent the stated multiplicity on a relationship or constraint."},
	"ownership_rule":         {"ownership", "required", "high", "Represent which concept owns or contains the other."},
	"uniqueness_constraint":  {"key", "required", "high", "Provide an enforceable key or uniqueness rule."},
	"validation_rule":        {"invariant", "required", "medium", "Represent the rule and identify its enforcement strategy."},
	"business_rule":          {"invariant", "required", "medium", "Represent the rule and identify its enforcement strategy."},
	"state_transition":       {"lifecycle", "required", "high", "Represent the relevant state or transition and its temporal boundary."},
	"lifecycle_event":        {"lifecycle", "required", "high", "Represent the relevant state or transition and its temporal boundary."},
	"history_requirement":    {"event_history", "required", "high", "Retain enough typed facts to reconstruct the history."},
	"generated_value":        {"generated_snapshot", "required", "high", "Persist the generated value so the result is reproducible."},
	"derived_value":          {"derived_view", "derived", "medium", "Identify the persistent facts from which the value is derived."},
	"report_query":           {"derived_view", "derived", "medium", "Identify the persistent facts from which the report is derived."},
	"authorization_rule":     {"security", "required", "high", "Represent or explicitly assign enforcement of the security rule."},
	"notification":           {"transient", "not_required", "low", "Confirm that no persistent state is needed."},
	"ui_behavior":            {"transient", "not_required", "low", "Confirm that no persistent state is needed."},
	"navigation":             {"transient", "not_required", "low", "Confirm that no persistent state is needed."},
	"technology_constraint":  {"transient", "not_required", "low", "Technology choices have no durable-data consequence."},
	"example":                {"transient", "not_required", "low", "Illustrative examples are not normative data."},
}

func typedAtomObligation(atom RequirementAtomProposal) (obligationClassification, bool) {
	if atom.ModelingOutcome == "requires_app_logic" || atom.ModelingOutcome == "external_system" ||
		atom.ModelingRelevance == "application_logic" || atom.ModelingRelevance == "external" {
		return obligationClassification{}, false
	}
	classified, ok := typedObligations[atom.AtomType]
	return classified, ok
}

func classifyObligation(atom RequirementAtomProposal) (kind, persistence, risk, target string) {
	text := strings.ToLower(atom.Statement + " " + atom.AtomType + " " + atom.ModelingRelevance)
	persistence, risk = "required", "medium"
	switch atom.PersistenceEffect {
	case "not_required":
		return "transient", "not_required", "low", "Confirm that no persistent state is needed."
	case "derived_basis":
		return "derived_view", "derived", "medium", "Identify the persistent facts from which the value is derived."
	case "audit_history":
		return "event_history", "required", "high", "Retain enough typed facts to reconstruct and verify the event."
	case "external":
		return "relationship", "unresolved", "high", "Decide whether an external identity or snapshot is retained."
	case "unclear":
		return "attribute", "unresolved", "high", "Resolve the durable-data consequence before modeling."
	}
	if atom.AtomType == "example" {
		switch atom.ExampleRole {
		case "schema_shape":
			return "attribute", "required", "high", "Represent the normative structure demonstrated by the example."
		case "seed_data":
			return "completeness", "required", "medium", "Retain the target concept and verify its required initial data."
		case "constraint_boundary":
			return "invariant", "required", "high", "Represent the normative boundary demonstrated by the example."
		case "illustrative_instance":
			return "transient", "not_required", "low", "Illustrative instances do not define persistent model structure."
		}
	}
	if atom.ModelingOutcome == "intentionally_not_in_db" || atom.ModelingOutcome == "unsupported" || atom.ExampleRole == "illustrative_instance" {
		return "transient", "not_required", "low", "Confirm that no persistent state is needed."
	}
	if atom.SupportLevel == "example_based" && atom.ExampleRole != "schema_shape" && atom.ExampleRole != "seed_data" && atom.ExampleRole != "constraint_boundary" {
		return "transient", "not_required", "low", "The legacy example-based atom is treated as illustrative unless explicitly typed otherwise."
	}
	if atom.ModelingOutcome == "deferred" && len(atom.ReviewDecisions) == 0 {
		return "attribute", "unresolved", "high", "Resolve the deferred modeling outcome before conceptual generation."
	}
	if atom.ModelingRelevance == "non_model" || atom.ModelingRelevance == "ui_only" {
		return "transient", "not_required", "low", "Confirm that no persistent state is needed."
	}
	if typed, ok := typedAtomObligation(atom); ok {
		return typed.kind, typed.persistence, typed.risk, typed.target
	}
	switch {
	case containsAny(text, "random", "generate", "combination", "nasumi", "generi", "kombinacij"):
		return "generated_snapshot", "required", "high", "Persist the generated value that determines a played result."
	case containsAny(text, "leaderboard", "ranking", "report", "display", "rang", "izveštaj", "izvještaj", "prikaz"):
		return "derived_view", "derived", "medium", "Identify the persistent facts from which the requested view is derived."
	case containsAny(text, "password", "authoriz", "cannot play", "lozink", "autoriz", "ne može"):
		return "security", "required", "high", "Represent or explicitly assign enforcement of the security rule."
	case containsAny(text, "unique", " key", "jedinstv", "ključ"):
		return "key", "required", "high", "Provide an enforceable key or uniqueness rule."
	case containsAny(text, "score", "point", "attempt", "answer", "move", "history", "rezultat", "poen", "bod", "pokušaj", "odgovor", "potez", "istor"):
		return "event_history", "required", "high", "Retain enough typed facts to reconstruct and verify the result."
	case containsAny(text, "accept", "reject", "status", "start", "complete", "future", "prihvat", "odbij", "započ", "počin", "zavr", "buduć"):
		return "lifecycle", "required", "high", "Represent the relevant state or transition and its temporal boundary."
	case containsAny(text, "exactly", "at most", "must", "only", "tačno", "najviše", "mora", "samo"):
		return "invariant", "required", "high", "Represent the rule and identify its enforcement strategy."
	case atom.ModelingOutcome == "external_system" || atom.ModelingRelevance == "external":
		if containsAny(text, "store", "save", "persist", "snapshot", "identifier", "sačuv", "čuv", "identifik") {
			return "relationship", "required", "high", "Represent the retained external identity or snapshot."
		}
		return "transient", "not_required", "low", "External behavior has no explicit persistent representation."
	case atom.ModelingOutcome == "requires_app_logic" || atom.ModelingRelevance == "application_logic":
		if containsAny(text, "store", "save", "persist", "history", "audit", "password", "username", "status", "sačuv", "čuv", "istor", "lozink", "korisnič", "stanje") {
			return "invariant", "required", "medium", "Identify the persistent basis and application/database enforcement boundary."
		}
		return "transient", "not_required", "low", "Application behavior has no explicit durable-data consequence."
	default:
		return "attribute", persistence, risk, "Map the requirement to a typed model element or justify exclusion."
	}
}

// ReclassifyDesignObligations applies the current policy without changing the
// identity or order of existing obligations. It is used by pre-conceptual
// compatibility migration so failed historical jobs remain reproducible.
func ReclassifyDesignObligations(file DesignObligationsFile, atoms []RequirementAtomProposal) (DesignObligationsFile, DesignObligationQA) {
	atoms = NormalizeRequirementReviewSemantics(atoms)
	copyFile := DesignObligationsFile{Document: map[string]any{}, DesignObligations: append([]DesignObligation(nil), file.DesignObligations...)}
	for key, value := range file.Document {
		copyFile.Document[key] = value
	}
	file = copyFile
	byID := map[string]RequirementAtomProposal{}
	for _, atom := range atoms {
		byID[atom.ID] = atom
	}
	for i := range file.DesignObligations {
		item := &file.DesignObligations[i]
		if len(item.RequirementAtoms) == 0 {
			continue
		}
		atom, ok := byID[item.RequirementAtoms[0]]
		if !ok {
			continue
		}
		classifications := classifyObligations(atom)
		selected := classifications[0]
		for _, classified := range classifications {
			if classified.kind == item.Kind {
				selected = classified
				break
			}
		}
		item.Persistence = selected.persistence
		item.Risk = selected.risk
		item.VerificationTarget = selected.target
		item.RequiresReview = selected.persistence == "unresolved" || (selected.persistence != "not_required" && atom.RequiresReview && len(atom.ReviewDecisions) == 0)
		item.Status = "accepted"
		if selected.persistence == "not_required" {
			item.Status = "not_required"
		}
		item.Rationale = obligationRationale(item.Kind, selected.persistence)
	}
	if file.Document == nil {
		file.Document = map[string]any{}
	}
	file.Document["pipeline_version"] = "0.7.3"
	file.Document["policy_version"] = "design_obligations/v0.7.2"
	file.Document["derivation_strategy"] = "compatibility_reclassification_preserving_ids"
	return file, ValidateDesignObligations(file, atoms)
}

func containsAny(text string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(text, term) {
			return true
		}
	}
	return false
}

func obligationRationale(kind, persistence string) string {
	if persistence == "unresolved" {
		return "The durable-data consequence must be resolved before model generation."
	}
	if persistence == "not_required" {
		return "The atom is explicitly classified outside persistent schema scope."
	}
	if persistence == "derived" {
		return "The requested value is derived, but its persistent source facts must be identified."
	}
	return "The atom has a database-design consequence even when part of its execution remains in application logic."
}
