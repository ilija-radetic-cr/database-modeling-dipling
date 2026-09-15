package lint

import (
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

const Version = "0.2"

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

type Issue struct {
	Severity string
	Code     string
	Element  string
	Message  string
}

type Result struct {
	Version string
	Issues  []Issue
}

func (r Result) HasErrors() bool {
	return r.Count(SeverityError) > 0
}

func (r Result) HasWarnings() bool {
	return r.Count(SeverityWarning) > 0
}

func (r Result) Count(severity string) int {
	count := 0
	for _, issue := range r.Issues {
		if issue.Severity == severity {
			count++
		}
	}
	return count
}

func LintFile(path string) Result {
	doc, err := dsl.LoadDocument(path)
	if err != nil {
		return Result{Issues: []Issue{{
			Severity: SeverityError,
			Code:     "DBDSL_LOAD",
			Element:  path,
			Message:  err.Error(),
		}}}
	}
	if doc.DSL.Version == "0.5" {
		bundle, err := dsl.LoadV05Bundle(path)
		if err != nil {
			return Result{Version: "v0.5", Issues: []Issue{{
				Severity: SeverityError,
				Code:     "DBDSL_V05_LOAD",
				Element:  path,
				Message:  err.Error(),
			}}}
		}
		return LintV05(bundle)
	}

	doc, source, _, err := dsl.LoadBundle(path)
	if err != nil {
		return Result{Version: "v0.2", Issues: []Issue{{
			Severity: SeverityError,
			Code:     "DBDSL_V02_LOAD",
			Element:  path,
			Message:  err.Error(),
		}}}
	}
	return Lint(doc, source)
}

func Lint(doc *dsl.Document, source *dsl.ReviewedSource) Result {
	l := linter{
		doc:               doc,
		source:            source,
		fragmentByID:      map[string]dsl.SourceFragment{},
		entityByID:        map[string]dsl.Entity{},
		attributeByEntity: map[string]map[string]dsl.Attribute{},
	}
	l.run()
	sortIssues(l.issues)
	return Result{Version: "v0.2", Issues: l.issues}
}

type linter struct {
	doc    *dsl.Document
	source *dsl.ReviewedSource
	issues []Issue

	fragmentByID      map[string]dsl.SourceFragment
	entityByID        map[string]dsl.Entity
	attributeByEntity map[string]map[string]dsl.Attribute
}

func (l *linter) run() {
	l.buildIndexes()
	l.lintVersion()
	if l.doc.DSL.Version != Version {
		return
	}
	l.lintReviewedFragments()
	l.lintStateMachines()
	l.lintDerivedViews()
	l.lintFileSpecs()
	l.lintCheckConstraints()
	l.lintRelationshipDefaults()
}

func (l *linter) buildIndexes() {
	for _, fragment := range l.source.Fragments {
		if fragment.ID != "" {
			l.fragmentByID[fragment.ID] = fragment
		}
	}
	for _, entity := range l.doc.Entities {
		if entity.ID == "" {
			continue
		}
		l.entityByID[entity.ID] = entity
		l.attributeByEntity[entity.ID] = map[string]dsl.Attribute{}
		for _, attribute := range entity.Attributes {
			if attribute.ID != "" {
				l.attributeByEntity[entity.ID][attribute.ID] = attribute
			}
		}
	}
}

func (l *linter) lintVersion() {
	if l.doc.DSL.Version == Version {
		return
	}
	l.add(SeverityError, "DBDSL_V02_VERSION", "dsl.version",
		"lint v0.2 supports only DB-DSL v0.2 models; run validate/generate for older versions")
}

func (l *linter) lintReviewedFragments() {
	for _, fragment := range l.source.Fragments {
		if fragment.Decision != "include" || fragment.Phase2Eligibility != "eligible" {
			continue
		}
		if len(fragment.DerivedCandidates) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_SRC001", fragment.ID,
				"eligible included fragment has no derived_candidates; v0.2 mapping should leave an explicit candidate or exclusion rationale")
		}
	}
}

func (l *linter) lintStateMachines() {
	for _, machine := range l.doc.StateMachines {
		element := "state_machine " + machine.ID
		if !l.hasEvidenceFragmentType(machine.Evidence, "state_machine_candidate") &&
			!l.hasEvidenceCandidate(machine.Evidence, "state_machines") &&
			len(machine.Evidence.ReviewDecisions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_SM001", element,
				"state machine has no state_machine_candidate fragment and no review decision; do not create state machines from enum values alone")
		}
		if len(machine.Transitions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_SM002", element,
				"state machine has no transitions; use enum_values only unless a transition flow is known")
		}
	}
}

func (l *linter) lintDerivedViews() {
	for _, view := range l.doc.DerivedViews {
		element := "derived_view " + view.ID
		if !l.hasEvidenceFragmentType(view.Evidence, "derived_view_candidate") &&
			!l.hasEvidenceCandidate(view.Evidence, "derived_views") &&
			len(view.Evidence.ReviewDecisions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_DV001", element,
				"derived view has no derived_view_candidate fragment and no review decision; avoid turning ordinary UI screens into model elements")
		}
		if view.Kind != "report" && len(view.Metrics) == 0 && len(view.Filters) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_DV002", element,
				"derived view has neither metrics nor filters; ensure it is a projection/aggregate over data, not only a UI screen")
		}
		if view.Persistence == "materialized_candidate" && len(view.Evidence.ReviewDecisions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_DV003", element,
				"materialized_candidate should be backed by a review decision or explicit performance/storage rationale")
		}
	}
}

func (l *linter) lintFileSpecs() {
	for _, spec := range l.doc.FileSpecs {
		element := "file_spec " + spec.ID
		if !l.hasEvidenceFragmentType(spec.Evidence, "file_spec_candidate") &&
			!l.hasEvidenceCandidate(spec.Evidence, "file_specs") &&
			len(spec.Evidence.ReviewDecisions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_FS001", element,
				"file spec has no file_spec_candidate fragment and no review decision; avoid adding file specs for ordinary text or URL fields")
		}

		attribute, ok := l.attribute(spec.Owner, spec.Field)
		if ok && attribute.Type != "file_path" && attribute.Type != "url" {
			l.add(SeverityWarning, "DBDSL_V02_FS002", element,
				fmt.Sprintf("file spec targets %s.%s with type %s; expected file_path or url for v0.2 file semantics", spec.Owner, spec.Field, attribute.Type))
		}
		if ok && attribute.Type == "url" && spec.Storage == "url" &&
			!l.hasEvidenceFragmentType(spec.Evidence, "file_spec_candidate") &&
			!l.hasEvidenceCandidate(spec.Evidence, "file_specs") &&
			len(spec.Evidence.ReviewDecisions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_FS003", element,
				"url-backed file specs need explicit file evidence or review; otherwise type: url is usually enough")
		}
		if spec.Storage == "path" && spec.MaxSizeMB == nil {
			l.add(SeverityWarning, "DBDSL_V02_FS004", element,
				"path-backed file spec has no max_size_mb; upload/storage requirements should normally define a size policy")
		}
		if spec.Storage == "path" && len(spec.AllowedExtensions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_FS005", element,
				"path-backed file spec has no allowed_extensions; upload/storage requirements should normally define accepted file types")
		}
	}
}

func (l *linter) lintCheckConstraints() {
	for _, constraint := range l.doc.Constraints {
		if constraint.Type != "check" {
			continue
		}
		element := "constraint " + constraint.ID
		if !l.hasEvidenceFragmentType(constraint.Evidence, "constraint_candidate") &&
			!l.hasEvidenceCandidate(constraint.Evidence, "constraints") &&
			len(constraint.Evidence.ReviewDecisions) == 0 {
			l.add(SeverityWarning, "DBDSL_V02_CK001", element,
				"check constraint has no constraint_candidate fragment and no review decision; prefer required/unique/min/max/length/regex/conditional_required before check")
		}
		if looksCrossRowOrCrossTable(constraint.Expression) {
			l.add(SeverityInfo, "DBDSL_V02_CK002", element,
				"check expression appears to reference other rows or related entities; SQL DDL generation may require a trigger, assertion, or application-level enforcement")
		}
	}
}

func (l *linter) lintRelationshipDefaults() {
	redundant := 0
	for _, relationship := range l.doc.Relationships {
		if relationship.Cardinality == "many_to_many" {
			continue
		}
		if relationship.FKRequired == nil || relationship.OnDelete == "" || relationship.Identifying == nil || relationship.Required == nil {
			continue
		}
		if *relationship.FKRequired == *relationship.Required &&
			relationship.OnDelete == "restrict" &&
			!*relationship.Identifying {
			redundant++
		}
	}
	if redundant > 0 {
		l.add(SeverityInfo, "DBDSL_V02_REL001", "relationships",
			fmt.Sprintf("%d relationships explicitly repeat v0.2 defaults for fk_required/on_delete/identifying; a canonicalizer may omit them unless they are important evidence", redundant))
	}
}

func (l *linter) attribute(entityID, attributeID string) (dsl.Attribute, bool) {
	attrs, ok := l.attributeByEntity[entityID]
	if !ok {
		return dsl.Attribute{}, false
	}
	attribute, ok := attrs[attributeID]
	return attribute, ok
}

func (l *linter) hasEvidenceFragmentType(evidence dsl.Evidence, fragmentType string) bool {
	for _, fragmentID := range evidence.Fragments {
		fragment, ok := l.fragmentByID[fragmentID]
		if !ok {
			continue
		}
		for _, typ := range fragment.Types {
			if typ == fragmentType {
				return true
			}
		}
	}
	return false
}

func (l *linter) hasEvidenceCandidate(evidence dsl.Evidence, candidateKey string) bool {
	for _, fragmentID := range evidence.Fragments {
		fragment, ok := l.fragmentByID[fragmentID]
		if !ok {
			continue
		}
		if candidatePresent(fragment.DerivedCandidates[candidateKey]) {
			return true
		}
	}
	return false
}

func candidatePresent(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case []any:
		return len(typed) > 0
	case []map[string]any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func looksCrossRowOrCrossTable(expression string) bool {
	lower := strings.ToLower(expression)
	return strings.Contains(lower, "count(") ||
		strings.Contains(lower, " related ") ||
		strings.Contains(lower, " where ") ||
		strings.Contains(lower, "select ")
}

func (l *linter) add(severity, code, element, message string) {
	l.issues = append(l.issues, Issue{
		Severity: severity,
		Code:     code,
		Element:  element,
		Message:  message,
	})
}

func sortIssues(issues []Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		if severityRank(issues[i].Severity) != severityRank(issues[j].Severity) {
			return severityRank(issues[i].Severity) < severityRank(issues[j].Severity)
		}
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].Element < issues[j].Element
	})
}

func severityRank(severity string) int {
	switch severity {
	case SeverityError:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}
