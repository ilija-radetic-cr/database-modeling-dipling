package generate

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

type traceRowV06 struct {
	Kind        string
	Element     string
	Expression  string
	SourceUnits []string
	Reviews     []string
	Support     string
	Confidence  string
}

func TraceReportV06(bundle *dsl.Bundle) string {
	doc := bundle.Document
	rows := buildTraceRowsV06(doc)
	modelSourceUnits := traceCoverageV06(rows)

	var out bytes.Buffer
	fmt.Fprintf(&out, "# Traceability Report v0.6: %s\n\n", doc.Model.Name)
	fmt.Fprintln(&out, "## Summary")
	writeKeyValueTable(&out, [][2]string{
		{"Model ID", doc.Model.ID},
		{"Model status", doc.Model.Status},
		{"Domain slice", doc.Model.DomainSlice},
		{"DSL", doc.DSL.Name + " " + doc.DSL.Version},
		{"Source units file", doc.Source.SourceUnitsFile},
		{"Review decisions file", doc.Source.ReviewDecisionsFile},
		{"Entities", fmt.Sprintf("%d", len(doc.Entities))},
		{"Relationships", fmt.Sprintf("%d", len(doc.Relationships))},
		{"Constraints", fmt.Sprintf("%d", len(doc.Constraints))},
		{"Source units", fmt.Sprintf("%d", len(bundle.SourceUnits.SourceUnits))},
	})
	fmt.Fprintln(&out)
	if doc.Model.Description != "" {
		fmt.Fprintf(&out, "%s\n\n", doc.Model.Description)
	}

	fmt.Fprintln(&out, "## Coverage")
	writeKeyValueTable(&out, [][2]string{
		{"Source units", fmt.Sprintf("%d", len(bundle.SourceUnits.SourceUnits))},
		{"Source units referenced by model", fmt.Sprintf("%d", len(modelSourceUnits))},
		{"Model elements", fmt.Sprintf("%d", len(rows))},
	})
	fmt.Fprintln(&out)

	fmt.Fprintln(&out, "## Review Decisions")
	writeReviewDecisionTableV06(&out, bundle.ReviewDecisions.ReviewDecisions)

	fmt.Fprintln(&out, "## Model Trace Matrix")
	writeTraceTableV06(&out, rows)

	fmt.Fprintln(&out, "## Source Unit Appendix")
	writeSourceUnitTableV06(&out, bundle.SourceUnits.SourceUnits, modelSourceUnits)

	return strings.TrimRight(out.String(), "\n") + "\n"
}

func buildTraceRowsV06(doc *dsl.Document) []traceRowV06 {
	var rows []traceRowV06
	for _, entity := range doc.Entities {
		rows = append(rows, traceRowV06{
			Kind:        "entity",
			Element:     entity.ID,
			Expression:  fmt.Sprintf("%s table=%s kind=%s", entity.Label, entity.TableName, entity.Kind),
			SourceUnits: entity.Evidence.SourceUnits,
			Reviews:     entity.Evidence.ReviewDecisions,
			Support:     entity.Evidence.SupportLevel,
			Confidence:  entity.Evidence.Confidence,
		})
		for _, attribute := range entity.Attributes {
			rows = append(rows, traceRowV06{
				Kind:        "attribute",
				Element:     entity.ID + "." + attribute.ID,
				Expression:  attributeExpression(attribute),
				SourceUnits: attribute.Evidence.SourceUnits,
				Reviews:     attribute.Evidence.ReviewDecisions,
				Support:     attribute.Evidence.SupportLevel,
				Confidence:  attribute.Evidence.Confidence,
			})
		}
	}
	for _, relationship := range doc.Relationships {
		rows = append(rows, traceRowV06{
			Kind:        "relationship",
			Element:     relationship.ID,
			Expression:  relationshipExpression(relationship),
			SourceUnits: relationship.Evidence.SourceUnits,
			Reviews:     relationship.Evidence.ReviewDecisions,
			Support:     relationship.Evidence.SupportLevel,
			Confidence:  relationship.Evidence.Confidence,
		})
	}
	for _, constraint := range doc.Constraints {
		rows = append(rows, traceRowV06{
			Kind:        "constraint",
			Element:     constraint.ID,
			Expression:  constraintExpression(constraint),
			SourceUnits: constraint.Evidence.SourceUnits,
			Reviews:     constraint.Evidence.ReviewDecisions,
			Support:     constraint.Evidence.SupportLevel,
			Confidence:  constraint.Evidence.Confidence,
		})
	}
	for _, importSpec := range doc.ImportSpecs {
		rows = append(rows, traceRowV06{
			Kind:        "import_spec",
			Element:     importSpec.ID,
			Expression:  fmt.Sprintf("%s root=%s mappings=%d", importSpec.Format, importSpec.Root, len(importSpec.Mappings)),
			SourceUnits: importSpec.Evidence.SourceUnits,
			Reviews:     importSpec.Evidence.ReviewDecisions,
			Support:     importSpec.Evidence.SupportLevel,
			Confidence:  importSpec.Evidence.Confidence,
		})
	}
	for _, machine := range doc.StateMachines {
		rows = append(rows, traceRowV06{
			Kind:        "state_machine",
			Element:     machine.ID,
			Expression:  fmt.Sprintf("%s.%s states=%d transitions=%d", machine.Owner, machine.Field, len(machine.States), len(machine.Transitions)),
			SourceUnits: machine.Evidence.SourceUnits,
			Reviews:     machine.Evidence.ReviewDecisions,
			Support:     machine.Evidence.SupportLevel,
			Confidence:  machine.Evidence.Confidence,
		})
	}
	for _, view := range doc.DerivedViews {
		rows = append(rows, traceRowV06{
			Kind:        "derived_view",
			Element:     view.ID,
			Expression:  fmt.Sprintf("%s kind=%s persistence=%s sources=%s", view.Label, view.Kind, view.Persistence, strings.Join(view.Sources, ", ")),
			SourceUnits: view.Evidence.SourceUnits,
			Reviews:     view.Evidence.ReviewDecisions,
			Support:     view.Evidence.SupportLevel,
			Confidence:  view.Evidence.Confidence,
		})
	}
	for _, spec := range doc.FileSpecs {
		rows = append(rows, traceRowV06{
			Kind:        "file_spec",
			Element:     spec.ID,
			Expression:  fmt.Sprintf("%s.%s storage=%s", spec.Owner, spec.Field, spec.Storage),
			SourceUnits: spec.Evidence.SourceUnits,
			Reviews:     spec.Evidence.ReviewDecisions,
			Support:     spec.Evidence.SupportLevel,
			Confidence:  spec.Evidence.Confidence,
		})
	}
	return rows
}

func traceCoverageV06(rows []traceRowV06) map[string]bool {
	sourceUnits := map[string]bool{}
	for _, row := range rows {
		for _, sourceUnit := range row.SourceUnits {
			sourceUnits[sourceUnit] = true
		}
	}
	return sourceUnits
}

func writeTraceTableV06(out *bytes.Buffer, rows []traceRowV06) {
	fmt.Fprintln(out, "| Kind | Element | Expression | Source units | Review decisions | Support | Confidence |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- | --- |")
	for _, row := range rows {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | %s |\n",
			mdCell(row.Kind),
			mdCell(row.Element),
			mdCell(row.Expression),
			mdCell(strings.Join(row.SourceUnits, ", ")),
			mdCell(strings.Join(row.Reviews, ", ")),
			mdCell(row.Support),
			mdCell(row.Confidence),
		)
	}
	fmt.Fprintln(out)
}

func writeReviewDecisionTableV06(out *bytes.Buffer, decisions []dsl.ReviewDecision) {
	fmt.Fprintln(out, "| Review | Status | Selected option | Question | Rationale |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- |")
	for _, decision := range decisions {
		status, _ := decision.Decision["status"].(string)
		selectedOption, _ := decision.Decision["selected_option"].(string)
		rationale, _ := decision.Decision["rationale"].(string)
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s |\n",
			mdCell(decision.ID),
			mdCell(status),
			mdCell(selectedOption),
			mdCell(decision.Question),
			mdCell(rationale),
		)
	}
	fmt.Fprintln(out)
}

func writeSourceUnitTableV06(out *bytes.Buffer, source []dsl.SourceUnit, modelSourceUnits map[string]bool) {
	units := append([]dsl.SourceUnit(nil), source...)
	sort.SliceStable(units, func(i, j int) bool { return units[i].ID < units[j].ID })
	fmt.Fprintln(out, "| Source unit | Section | Kind | Relevance | Referenced by model | Text |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- |")
	for _, unit := range units {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s |\n",
			mdCell(unit.ID),
			mdCell(unit.Section),
			mdCell(unit.Kind),
			mdCell(unit.Relevance),
			mdCell(boolText(modelSourceUnits[unit.ID])),
			mdCell(unit.Text.Normalized),
		)
	}
	fmt.Fprintln(out)
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
