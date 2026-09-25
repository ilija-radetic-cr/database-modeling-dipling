package llmpipeline

import (
	"fmt"
	"sort"
)

// RequirementReadinessReport is the deterministic precondition shared by the
// conceptual and logical stages. It contains no model-generated judgement.
type RequirementReadinessReport struct {
	OK                       bool     `json:"ok"`
	BlockingAtomIDs          []string `json:"blocking_atom_ids"`
	UnresolvedAtomIDs        []string `json:"unresolved_atom_ids"`
	Contradictions           []string `json:"contradictions"`
	MissingObligationAtomIDs []string `json:"missing_obligation_atom_ids"`
}

func EvaluateRequirementReadiness(atoms []RequirementAtomProposal, obligations []DesignObligation) RequirementReadinessReport {
	atoms = NormalizeRequirementReviewSemantics(atoms)
	report := RequirementReadinessReport{
		BlockingAtomIDs: []string{}, UnresolvedAtomIDs: []string{}, Contradictions: []string{}, MissingObligationAtomIDs: []string{},
	}
	covered := map[string]bool{}
	activeObligation := map[string]bool{}
	for _, obligation := range obligations {
		for _, atomID := range obligation.RequirementAtoms {
			covered[atomID] = true
			if obligation.Status != "not_required" && obligation.Persistence != "not_required" {
				activeObligation[atomID] = true
			}
		}
	}
	for _, atom := range atoms {
		if atom.RequiresReview {
			report.BlockingAtomIDs = append(report.BlockingAtomIDs, atom.ID)
		}
		if atom.ModelingOutcome == "deferred" || atom.ModelingOutcome == "unsupported" || atom.PersistenceEffect == "unclear" {
			report.UnresolvedAtomIDs = append(report.UnresolvedAtomIDs, atom.ID)
		}
		if atom.ModelingOutcome == "represented" && atom.PersistenceEffect == "not_required" {
			report.Contradictions = append(report.Contradictions, fmt.Sprintf("%s is represented but persistence is not required", atom.ID))
		}
		if atom.ModelingOutcome == "represented" && len(obligations) > 0 && !activeObligation[atom.ID] {
			report.Contradictions = append(report.Contradictions, fmt.Sprintf("%s is represented but has no active design obligation", atom.ID))
		}
		if len(obligations) > 0 && !covered[atom.ID] {
			report.MissingObligationAtomIDs = append(report.MissingObligationAtomIDs, atom.ID)
		}
	}
	sort.Strings(report.BlockingAtomIDs)
	sort.Strings(report.UnresolvedAtomIDs)
	sort.Strings(report.Contradictions)
	sort.Strings(report.MissingObligationAtomIDs)
	report.OK = len(report.BlockingAtomIDs) == 0 && len(report.UnresolvedAtomIDs) == 0 && len(report.Contradictions) == 0 && len(report.MissingObligationAtomIDs) == 0
	return report
}

func (r RequirementReadinessReport) Errors() []string {
	errors := []string{}
	if len(r.BlockingAtomIDs) > 0 {
		errors = append(errors, "blocking requirement reviews remain: "+joinIDs(r.BlockingAtomIDs))
	}
	if len(r.UnresolvedAtomIDs) > 0 {
		errors = append(errors, "unresolved requirements remain: "+joinIDs(r.UnresolvedAtomIDs))
	}
	errors = append(errors, r.Contradictions...)
	if len(r.MissingObligationAtomIDs) > 0 {
		errors = append(errors, "requirements have no design obligation: "+joinIDs(r.MissingObligationAtomIDs))
	}
	return errors
}

func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	out := ids[0]
	for _, id := range ids[1:] {
		out += ", " + id
	}
	return out
}
