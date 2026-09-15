package generate

import (
	"bytes"
	"fmt"
	"strings"

	"dbdsl/internal/dsl"
)

type traceRow struct {
	Kind       string
	Element    string
	Expression string
	Fragments  []string
	Reviews    []string
	Support    string
	Confidence string
}

func TraceFile(path string) (string, error) {
	doc, err := dsl.LoadDocument(path)
	if err != nil {
		return "", err
	}
	if doc.DSL.Version == "0.5" {
		bundle, err := dsl.LoadV05Bundle(path)
		if err != nil {
			return "", err
		}
		return TraceReportV05(bundle), nil
	}

	doc, source, sourcePath, err := dsl.LoadBundle(path)
	if err != nil {
		return "", err
	}
	return TraceReport(doc, source, sourcePath), nil
}

func TraceReport(doc *dsl.Document, source *dsl.ReviewedSource, sourcePath string) string {
	fragmentByID := map[string]dsl.SourceFragment{}
	for _, fragment := range source.Fragments {
		fragmentByID[fragment.ID] = fragment
	}
	reviewByID := map[string]dsl.ReviewItem{}
	for _, item := range source.ReviewItems {
		reviewByID[item.ID] = item
	}

	var out bytes.Buffer
	fmt.Fprintf(&out, "# Traceability Report: %s\n\n", doc.Model.Name)
	fmt.Fprintln(&out, "## Summary")
	writeKeyValueTable(&out, [][2]string{
		{"Model ID", doc.Model.ID},
		{"Model status", doc.Model.Status},
		{"Domain slice", doc.Model.DomainSlice},
		{"DSL", doc.DSL.Name + " " + doc.DSL.Version},
		{"Reviewed source file", sourcePath},
		{"Reviewed source state", doc.Source.ReviewState},
		{"Entities", fmt.Sprintf("%d", len(doc.Entities))},
		{"Relationships", fmt.Sprintf("%d", len(doc.Relationships))},
		{"Constraints", fmt.Sprintf("%d", len(doc.Constraints))},
		{"Import specs", fmt.Sprintf("%d", len(doc.ImportSpecs))},
	})
	fmt.Fprintln(&out)
	if doc.Model.Description != "" {
		fmt.Fprintf(&out, "%s\n\n", doc.Model.Description)
	}

	fmt.Fprintln(&out, "## Review State")
	writeKeyValueTable(&out, [][2]string{
		{"Status", source.ReviewState.Status},
		{"All required reviews resolved", boolPtrText(source.ReviewState.AllRequiredReviewsResolved)},
		{"Unresolved review flags", intPtrText(source.ReviewState.UnresolvedRequiresReviewFlags)},
	})
	fmt.Fprintln(&out)

	if source.Scope.Description != "" || len(source.Scope.Include) > 0 || len(source.Scope.Defer) > 0 {
		fmt.Fprintln(&out, "## Scope")
		if source.Scope.Description != "" {
			fmt.Fprintf(&out, "%s\n\n", source.Scope.Description)
		}
		writeList(&out, "Included in PoC", source.Scope.Include)
		writeList(&out, "Deferred from PoC", source.Scope.Defer)
	}

	fmt.Fprintln(&out, "## Accepted Review Decisions")
	writeReviewDecisionTable(&out, doc.Source.AcceptedReviewDecisions, reviewByID)

	fmt.Fprintln(&out, "## Model Trace Matrix")
	writeTraceTable(&out, buildTraceRows(doc))

	fmt.Fprintln(&out, "## Fragment Appendix")
	writeFragmentTable(&out, source.Fragments)

	fmt.Fprintln(&out, "## Review Decision Appendix")
	writeReviewItemTable(&out, source.ReviewItems)

	deferred := deferredFragments(source.Fragments)
	if len(deferred) > 0 {
		fmt.Fprintln(&out, "## Deferred Source Fragments")
		writeFragmentTable(&out, deferred)
	}

	_ = fragmentByID
	return strings.TrimRight(out.String(), "\n") + "\n"
}

func buildTraceRows(doc *dsl.Document) []traceRow {
	var rows []traceRow

	for _, entity := range doc.Entities {
		rows = append(rows, traceRow{
			Kind:       "entity",
			Element:    entity.ID,
			Expression: fmt.Sprintf("%s table=%s kind=%s", entity.Label, entity.TableName, entity.Kind),
			Fragments:  entity.Evidence.Fragments,
			Reviews:    entity.Evidence.ReviewDecisions,
			Support:    entity.Evidence.SupportLevel,
			Confidence: entity.Evidence.Confidence,
		})
		for _, attribute := range entity.Attributes {
			rows = append(rows, traceRow{
				Kind:       "attribute",
				Element:    entity.ID + "." + attribute.ID,
				Expression: attributeExpression(attribute),
				Fragments:  attribute.Evidence.Fragments,
				Reviews:    attribute.Evidence.ReviewDecisions,
				Support:    attribute.Evidence.SupportLevel,
				Confidence: attribute.Evidence.Confidence,
			})
		}
	}

	for _, relationship := range doc.Relationships {
		rows = append(rows, traceRow{
			Kind:       "relationship",
			Element:    relationship.ID,
			Expression: relationshipExpression(relationship),
			Fragments:  relationship.Evidence.Fragments,
			Reviews:    relationship.Evidence.ReviewDecisions,
			Support:    relationship.Evidence.SupportLevel,
			Confidence: relationship.Evidence.Confidence,
		})
	}

	for _, constraint := range doc.Constraints {
		rows = append(rows, traceRow{
			Kind:       "constraint",
			Element:    constraint.ID,
			Expression: constraintExpression(constraint),
			Fragments:  constraint.Evidence.Fragments,
			Reviews:    constraint.Evidence.ReviewDecisions,
			Support:    constraint.Evidence.SupportLevel,
			Confidence: constraint.Evidence.Confidence,
		})
	}

	for _, importSpec := range doc.ImportSpecs {
		rows = append(rows, traceRow{
			Kind:       "import_spec",
			Element:    importSpec.ID,
			Expression: fmt.Sprintf("%s root=%s mappings=%d", importSpec.Format, importSpec.Root, len(importSpec.Mappings)),
			Fragments:  importSpec.Evidence.Fragments,
			Reviews:    importSpec.Evidence.ReviewDecisions,
			Support:    importSpec.Evidence.SupportLevel,
			Confidence: importSpec.Evidence.Confidence,
		})
	}

	for _, machine := range doc.StateMachines {
		rows = append(rows, traceRow{
			Kind:       "state_machine",
			Element:    machine.ID,
			Expression: fmt.Sprintf("%s.%s states=%d transitions=%d", machine.Owner, machine.Field, len(machine.States), len(machine.Transitions)),
			Fragments:  machine.Evidence.Fragments,
			Reviews:    machine.Evidence.ReviewDecisions,
			Support:    machine.Evidence.SupportLevel,
			Confidence: machine.Evidence.Confidence,
		})
	}

	for _, view := range doc.DerivedViews {
		rows = append(rows, traceRow{
			Kind:       "derived_view",
			Element:    view.ID,
			Expression: fmt.Sprintf("%s kind=%s persistence=%s sources=%s", view.Label, view.Kind, view.Persistence, strings.Join(view.Sources, ", ")),
			Fragments:  view.Evidence.Fragments,
			Reviews:    view.Evidence.ReviewDecisions,
			Support:    view.Evidence.SupportLevel,
			Confidence: view.Evidence.Confidence,
		})
	}

	for _, spec := range doc.FileSpecs {
		rows = append(rows, traceRow{
			Kind:       "file_spec",
			Element:    spec.ID,
			Expression: fmt.Sprintf("%s.%s storage=%s", spec.Owner, spec.Field, spec.Storage),
			Fragments:  spec.Evidence.Fragments,
			Reviews:    spec.Evidence.ReviewDecisions,
			Support:    spec.Evidence.SupportLevel,
			Confidence: spec.Evidence.Confidence,
		})
	}

	return rows
}

func writeTraceTable(out *bytes.Buffer, rows []traceRow) {
	fmt.Fprintln(out, "| Kind | Element | Expression | Fragments | Review decisions | Support | Confidence |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- | --- |")
	for _, row := range rows {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | %s |\n",
			mdCell(row.Kind),
			mdCell(row.Element),
			mdCell(row.Expression),
			mdCell(strings.Join(row.Fragments, ", ")),
			mdCell(strings.Join(row.Reviews, ", ")),
			mdCell(row.Support),
			mdCell(row.Confidence),
		)
	}
	fmt.Fprintln(out)
}

func writeFragmentTable(out *bytes.Buffer, fragments []dsl.SourceFragment) {
	fmt.Fprintln(out, "| Fragment | Decision | Eligibility | Types | Source | Support | Confidence | Description | Normalized text | Reviews |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, fragment := range fragments {
		source := fragment.Source.ID
		if fragment.Source.Locator != "" {
			source += " " + fragment.Source.Locator
		}
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			mdCell(fragment.ID),
			mdCell(fragment.Decision),
			mdCell(fragment.Phase2Eligibility),
			mdCell(strings.Join(fragment.Types, ", ")),
			mdCell(source),
			mdCell(fragment.Evidence.SupportLevel),
			mdCell(fragment.Evidence.Confidence),
			mdCell(fragment.Description),
			mdCell(fragment.Text.Normalized),
			mdCell(strings.Join(fragment.ReviewRefs, ", ")),
		)
	}
	fmt.Fprintln(out)
}

func writeReviewDecisionTable(out *bytes.Buffer, decisions []dsl.AcceptedReviewDecision, reviewByID map[string]dsl.ReviewItem) {
	fmt.Fprintln(out, "| Review | Selected option | Decision status | Question | Resolution note |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- |")
	for _, decision := range decisions {
		item := reviewByID[decision.ReviewID]
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s |\n",
			mdCell(decision.ReviewID),
			mdCell(decision.SelectedOption),
			mdCell(item.Decision.Status),
			mdCell(item.Question),
			mdCell(item.Decision.ResolutionNote),
		)
	}
	fmt.Fprintln(out)
}

func writeReviewItemTable(out *bytes.Buffer, items []dsl.ReviewItem) {
	fmt.Fprintln(out, "| Review | Status | Selected option | Affected fragments | Depends on | Rationale |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- |")
	for _, item := range items {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s |\n",
			mdCell(item.ID),
			mdCell(item.Decision.Status),
			mdCell(item.Decision.SelectedOption),
			mdCell(strings.Join(item.AffectedFragments, ", ")),
			mdCell(strings.Join(item.DependsOn, ", ")),
			mdCell(item.Decision.Rationale),
		)
	}
	fmt.Fprintln(out)
}

func writeKeyValueTable(out *bytes.Buffer, rows [][2]string) {
	fmt.Fprintln(out, "| Field | Value |")
	fmt.Fprintln(out, "| --- | --- |")
	for _, row := range rows {
		fmt.Fprintf(out, "| %s | %s |\n", mdCell(row[0]), mdCell(row[1]))
	}
}

func writeList(out *bytes.Buffer, title string, values []string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(out, "### %s\n", title)
	for _, value := range values {
		fmt.Fprintf(out, "- %s\n", value)
	}
	fmt.Fprintln(out)
}

func deferredFragments(fragments []dsl.SourceFragment) []dsl.SourceFragment {
	var result []dsl.SourceFragment
	for _, fragment := range fragments {
		if fragment.Decision != "include" || fragment.Phase2Eligibility != "eligible" {
			result = append(result, fragment)
		}
	}
	return result
}

func attributeExpression(attribute dsl.Attribute) string {
	parts := []string{attribute.Type}
	if attribute.Required != nil && *attribute.Required {
		parts = append(parts, "required")
	} else if attribute.Required != nil {
		parts = append(parts, "optional")
	}
	if attribute.SourceField != "" {
		parts = append(parts, "source_field="+attribute.SourceField)
	}
	if len(attribute.EnumValues) > 0 {
		parts = append(parts, "values="+strings.Join(attribute.EnumValues, "/"))
	}
	if attribute.Type == "decimal" {
		precision := 10
		scale := 2
		if attribute.Precision != nil {
			precision = *attribute.Precision
		}
		if attribute.Scale != nil {
			scale = *attribute.Scale
		}
		parts = append(parts, fmt.Sprintf("precision=%d scale=%d", precision, scale))
	}
	return strings.Join(parts, " ")
}

func relationshipExpression(relationship dsl.Relationship) string {
	expression := fmt.Sprintf("%s %s %s", relationship.From, relationship.Cardinality, relationship.To)
	if relationship.Through != "" {
		expression += " through " + relationship.Through
	}
	if relationship.Required != nil && *relationship.Required {
		expression += " required"
	} else if relationship.Required != nil {
		expression += " optional"
	}
	return expression
}

func constraintExpression(constraint dsl.Constraint) string {
	switch constraint.Type {
	case "required":
		return fmt.Sprintf("%s.%s required", constraint.Owner, constraint.Field)
	case "unique":
		return fmt.Sprintf("%s unique(%s)", constraint.Owner, strings.Join(constraintFields(constraint), ", "))
	case "min_inclusive":
		return fmt.Sprintf("%s.%s >= %v", constraint.Owner, constraint.Field, constraintBoundValue(constraint))
	case "min_exclusive":
		return fmt.Sprintf("%s.%s > %v", constraint.Owner, constraint.Field, constraintBoundValue(constraint))
	default:
		return constraint.Description
	}
}

func boolPtrText(value *bool) string {
	if value == nil {
		return ""
	}
	if *value {
		return "true"
	}
	return "false"
}

func intPtrText(value *int) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func mdCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}
