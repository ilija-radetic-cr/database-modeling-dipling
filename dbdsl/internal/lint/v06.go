package lint

import (
	"fmt"

	"dbdsl/internal/dsl"
)

const VersionV06 = "v0.6"

func LintV06(bundle *dsl.Bundle) Result {
	l := v06Linter{
		bundle:     bundle,
		doc:        bundle.Document,
		reviewByID: map[string]dsl.ReviewDecision{},
		reviewRefs: map[string]int{},
	}
	l.run()
	sortIssues(l.issues)
	return Result{Version: VersionV06, Issues: l.issues}
}

type v06Linter struct {
	bundle *dsl.Bundle
	doc    *dsl.Document
	issues []Issue

	reviewByID map[string]dsl.ReviewDecision
	reviewRefs map[string]int
}

func (l *v06Linter) run() {
	l.buildIndexes()
	l.lintEvidenceBreadth()
	l.lintReviewDecisionUsage()
	l.lintDerivedViews()
	l.lintFileSpecs()
	l.lintCheckConstraints()
}

func (l *v06Linter) buildIndexes() {
	for _, review := range l.bundle.ReviewDecisions.ReviewDecisions {
		l.reviewByID[review.ID] = review
	}
	l.collectEvidence()
}

func (l *v06Linter) collectEvidence() {
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

func (l *v06Linter) recordEvidence(element string, evidence dsl.Evidence) {
	for _, reviewID := range evidence.ReviewDecisions {
		l.reviewRefs[reviewID]++
	}
}

func (l *v06Linter) lintEvidenceBreadth() {
	const broadEvidenceThreshold = 20
	checkEvidenceBreadth := func(element string, evidence dsl.Evidence) {
		if len(evidence.SourceUnits) > broadEvidenceThreshold {
			l.add(SeverityWarning, "DBDSL_V06_EVID001", element,
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

func (l *v06Linter) lintReviewDecisionUsage() {
	for _, review := range l.bundle.ReviewDecisions.ReviewDecisions {
		if l.reviewRefs[review.ID] == 0 {
			l.add(SeverityInfo, "DBDSL_V06_REVIEW001", "review_decision "+review.ID,
				"review decision is resolved but not referenced by any model element evidence")
		}
	}
}

func (l *v06Linter) lintDerivedViews() {
	for _, view := range l.doc.DerivedViews {
		if view.Kind != "report" && len(view.Metrics) == 0 && len(view.Filters) == 0 {
			l.add(SeverityWarning, "DBDSL_V06_DV002", "derived_view "+view.ID,
				"derived view has neither metrics nor filters; ensure it is a data projection and not only a UI screen")
		}
	}
}

func (l *v06Linter) lintFileSpecs() {
	for _, spec := range l.doc.FileSpecs {
		element := "file_spec " + spec.ID
		if spec.Storage == "path" && spec.MaxSizeMB == nil {
			l.add(SeverityWarning, "DBDSL_V06_FS004", element,
				"path-backed file spec has no max_size_mb; keep it unresolved only if the source text gives no size policy")
		}
		if spec.Storage == "path" && len(spec.AllowedExtensions) == 0 {
			l.add(SeverityWarning, "DBDSL_V06_FS005", element,
				"path-backed file spec has no allowed_extensions")
		}
	}
}

func (l *v06Linter) lintCheckConstraints() {
	for _, constraint := range l.doc.Constraints {
		if constraint.Type != "check" {
			continue
		}
		element := "constraint " + constraint.ID
		if looksCrossRowOrCrossTable(constraint.Expression) {
			l.add(SeverityInfo, "DBDSL_V06_CK002", element,
				"check expression appears to reference other rows or related entities; SQL DDL generation may require application logic, trigger, or assertion")
		}
	}
}

func (l *v06Linter) add(severity, code, element, message string) {
	l.issues = append(l.issues, Issue{
		Severity: severity,
		Code:     code,
		Element:  element,
		Message:  message,
	})
}
