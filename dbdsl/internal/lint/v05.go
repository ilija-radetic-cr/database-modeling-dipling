package lint

import (
	"fmt"
	"strings"

	"dbdsl/internal/dsl"
)

const VersionV05 = "v0.5"

func LintV05(bundle *dsl.V05Bundle) Result {
	l := v05Linter{
		bundle:          bundle,
		doc:             bundle.Document,
		atomByID:        map[string]dsl.RequirementAtom{},
		reviewByID:      map[string]dsl.V05ReviewDecision{},
		modelRefsByAtom: map[string][]string{},
		reviewRefs:      map[string]int{},
		actionsByOp:     map[string]map[string]bool{},
	}
	l.run()
	sortIssues(l.issues)
	return Result{Version: VersionV05, Issues: l.issues}
}

type v05Linter struct {
	bundle *dsl.V05Bundle
	doc    *dsl.Document
	issues []Issue

	atomByID        map[string]dsl.RequirementAtom
	reviewByID      map[string]dsl.V05ReviewDecision
	modelRefsByAtom map[string][]string
	reviewRefs      map[string]int
	actionsByOp     map[string]map[string]bool
}

func (l *v05Linter) run() {
	l.buildIndexes()
	l.lintEvidenceBreadth()
	l.lintRequirementAtomCoverage()
	l.lintReviewDecisionUsage()
	l.lintDerivedViews()
	l.lintFileSpecs()
	l.lintCheckConstraints()
	l.lintCRUDOperations()
}

func (l *v05Linter) buildIndexes() {
	for _, atom := range l.bundle.RequirementAtoms.RequirementAtoms {
		l.atomByID[atom.ID] = atom
	}
	for _, review := range l.bundle.ReviewDecisions.ReviewDecisions {
		l.reviewByID[review.ID] = review
	}
	for _, row := range l.bundle.CRUDMatrix.Matrix {
		for operationID, actions := range row.Operations {
			if l.actionsByOp[operationID] == nil {
				l.actionsByOp[operationID] = map[string]bool{}
			}
			for _, action := range actions {
				l.actionsByOp[operationID][action] = true
			}
		}
	}

	l.collectEvidence()
}

func (l *v05Linter) collectEvidence() {
	for _, entity := range l.doc.Entities {
		l.recordEvidence("entity "+entity.ID, entity.Evidence)
		for _, attribute := range entity.Attributes {
			l.recordEvidence("attribute "+entity.ID+"."+attribute.ID, attribute.Evidence)
		}
	}
	for _, relationship := range l.doc.Relationships {
		l.recordEvidence("relationship "+relationship.ID, relationship.Evidence)
	}
	for _, constraint := range l.doc.Constraints {
		l.recordEvidence("constraint "+constraint.ID, constraint.Evidence)
	}
	for _, importSpec := range l.doc.ImportSpecs {
		l.recordEvidence("import_spec "+importSpec.ID, importSpec.Evidence)
	}
	for _, machine := range l.doc.StateMachines {
		l.recordEvidence("state_machine "+machine.ID, machine.Evidence)
	}
	for _, view := range l.doc.DerivedViews {
		l.recordEvidence("derived_view "+view.ID, view.Evidence)
	}
	for _, spec := range l.doc.FileSpecs {
		l.recordEvidence("file_spec "+spec.ID, spec.Evidence)
	}
}

func (l *v05Linter) recordEvidence(element string, evidence dsl.Evidence) {
	for _, atomID := range evidence.RequirementAtoms {
		l.modelRefsByAtom[atomID] = append(l.modelRefsByAtom[atomID], element)
	}
	for _, reviewID := range evidence.ReviewDecisions {
		l.reviewRefs[reviewID]++
	}
}

func (l *v05Linter) lintEvidenceBreadth() {
	const broadEvidenceThreshold = 20
	checkEvidenceBreadth := func(element string, evidence dsl.Evidence) {
		if len(evidence.SourceUnits) > broadEvidenceThreshold {
			l.add(SeverityWarning, "DBDSL_V05_EVID001", element,
				fmt.Sprintf("evidence references %d source_units; consider narrowing traceability for review/UI precision", len(evidence.SourceUnits)))
		}
	}
	for _, entity := range l.doc.Entities {
		checkEvidenceBreadth("entity "+entity.ID, entity.Evidence)
		for _, attribute := range entity.Attributes {
			checkEvidenceBreadth("attribute "+entity.ID+"."+attribute.ID, attribute.Evidence)
		}
	}
	for _, relationship := range l.doc.Relationships {
		checkEvidenceBreadth("relationship "+relationship.ID, relationship.Evidence)
	}
	for _, constraint := range l.doc.Constraints {
		checkEvidenceBreadth("constraint "+constraint.ID, constraint.Evidence)
	}
	for _, importSpec := range l.doc.ImportSpecs {
		checkEvidenceBreadth("import_spec "+importSpec.ID, importSpec.Evidence)
	}
	for _, machine := range l.doc.StateMachines {
		checkEvidenceBreadth("state_machine "+machine.ID, machine.Evidence)
	}
	for _, view := range l.doc.DerivedViews {
		checkEvidenceBreadth("derived_view "+view.ID, view.Evidence)
	}
	for _, spec := range l.doc.FileSpecs {
		checkEvidenceBreadth("file_spec "+spec.ID, spec.Evidence)
	}
}

func (l *v05Linter) lintRequirementAtomCoverage() {
	for _, atom := range l.bundle.RequirementAtoms.RequirementAtoms {
		if atom.ModelingOutcome.Status != "represented" {
			continue
		}
		if len(l.modelRefsByAtom[atom.ID]) == 0 {
			l.add(SeverityWarning, "DBDSL_V05_ATOM001", "requirement_atom "+atom.ID,
				"represented atom is not referenced by any model element evidence")
		}
	}
}

func (l *v05Linter) lintReviewDecisionUsage() {
	for _, review := range l.bundle.ReviewDecisions.ReviewDecisions {
		if l.reviewRefs[review.ID] == 0 {
			l.add(SeverityInfo, "DBDSL_V05_REVIEW001", "review_decision "+review.ID,
				"review decision is resolved but not referenced by any model element evidence")
		}
	}
}

func (l *v05Linter) lintDerivedViews() {
	for _, view := range l.doc.DerivedViews {
		if view.Kind != "report" && len(view.Metrics) == 0 && len(view.Filters) == 0 {
			l.add(SeverityWarning, "DBDSL_V05_DV002", "derived_view "+view.ID,
				"derived view has neither metrics nor filters; ensure it is a data projection and not only a UI screen")
		}
	}
}

func (l *v05Linter) lintFileSpecs() {
	for _, spec := range l.doc.FileSpecs {
		element := "file_spec " + spec.ID
		if spec.Storage == "path" && spec.MaxSizeMB == nil {
			l.add(SeverityWarning, "DBDSL_V05_FS004", element,
				"path-backed file spec has no max_size_mb; keep it unresolved only if the source text gives no size policy")
		}
		if spec.Storage == "path" && len(spec.AllowedExtensions) == 0 {
			l.add(SeverityWarning, "DBDSL_V05_FS005", element,
				"path-backed file spec has no allowed_extensions")
		}
	}
}

func (l *v05Linter) lintCheckConstraints() {
	for _, constraint := range l.doc.Constraints {
		if constraint.Type != "check" {
			continue
		}
		element := "constraint " + constraint.ID
		if looksCrossRowOrCrossTable(constraint.Expression) {
			l.add(SeverityInfo, "DBDSL_V05_CK002", element,
				"check expression appears to reference other rows or related entities; SQL DDL generation may require application logic, trigger, or assertion")
		}
	}
}

func (l *v05Linter) lintCRUDOperations() {
	for _, operation := range l.bundle.CRUDMatrix.Operations {
		if !operationNameSuggestsMutation(operation.ID, operation.Label) {
			continue
		}
		actions := l.actionsByOp[operation.ID]
		if !actions["C"] && !actions["U"] && !actions["D"] {
			l.add(SeverityWarning, "DBDSL_V05_CRUD001", "CRUD operation "+operation.ID,
				"operation name suggests mutation but CRUD matrix has no C/U/D action")
		}
	}
}

func operationNameSuggestsMutation(id, label string) bool {
	text := strings.ToLower(id + " " + label)
	for _, needle := range []string{"create", "update", "confirm", "pay", "submit", "close", "process", "manage", "import", "register", "reset", "approve", "delete", "cancel"} {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func (l *v05Linter) add(severity, code, element, message string) {
	l.issues = append(l.issues, Issue{
		Severity: severity,
		Code:     code,
		Element:  element,
		Message:  message,
	})
}
