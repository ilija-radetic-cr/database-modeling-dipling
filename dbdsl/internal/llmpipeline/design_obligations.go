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
	items := make([]DesignObligation, 0, len(atoms))
	for _, atom := range atoms {
		for _, classified := range classifyObligations(atom) {
			items = append(items, DesignObligation{
				ID: fmt.Sprintf("DO-%04d", len(items)+1), Statement: atom.Statement, Kind: classified.kind,
				Persistence: classified.persistence, SourceUnits: append([]string(nil), atom.SourceUnits...),
				RequirementAtoms: []string{atom.ID}, VerificationTarget: classified.target, Risk: classified.risk,
				RequiresReview: atom.RequiresReview || classified.persistence == "unresolved",
				Status:         "accepted", Rationale: obligationRationale(classified.kind, classified.persistence),
			})
		}
	}
	file := DesignObligationsFile{
		Document:          map[string]any{"pipeline_version": "0.7", "derivation_strategy": "deterministic_from_requirement_atoms"},
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

func classifyObligation(atom RequirementAtomProposal) (kind, persistence, risk, target string) {
	text := strings.ToLower(atom.Statement + " " + atom.AtomType + " " + atom.ModelingRelevance)
	persistence, risk = "required", "medium"
	switch {
	case atom.ModelingRelevance == "non_model" || atom.ModelingRelevance == "ui_only":
		return "transient", "not_required", "low", "Confirm that no persistent state is needed."
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
	case atom.ModelingRelevance == "application_logic":
		return "invariant", "required", "medium", "Identify the persistent basis and application/database enforcement boundary."
	case atom.ModelingRelevance == "external":
		return "relationship", "unresolved", "high", "Decide what external identity or snapshot is retained."
	default:
		return "attribute", persistence, risk, "Map the requirement to a typed model element or justify exclusion."
	}
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
	if persistence == "not_required" {
		return "The atom is explicitly classified outside persistent schema scope."
	}
	if persistence == "derived" {
		return "The requested value is derived, but its persistent source facts must be identified."
	}
	return "The atom has a database-design consequence even when part of its execution remains in application logic."
}
