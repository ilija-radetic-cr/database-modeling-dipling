package generate

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"dbdsl/internal/dsl"
)

type traceRowV05 struct {
	Kind        string
	Element     string
	Expression  string
	Atoms       []string
	SourceUnits []string
	Reviews     []string
	Support     string
	Confidence  string
}

func TraceReportV05(bundle *dsl.V05Bundle) string {
	doc := bundle.Document
	rows := buildTraceRowsV05(doc)
	modelSourceUnits, modelAtoms := v05TraceCoverage(rows)
	atomSourceUnits := sourceUnitsReferencedByAtoms(bundle.RequirementAtoms.RequirementAtoms)

	var out bytes.Buffer
	fmt.Fprintf(&out, "# Traceability Report v0.5: %s\n\n", doc.Model.Name)
	fmt.Fprintln(&out, "## Summary")
	writeKeyValueTable(&out, [][2]string{
		{"Model ID", doc.Model.ID},
		{"Model status", doc.Model.Status},
		{"Domain slice", doc.Model.DomainSlice},
		{"DSL", doc.DSL.Name + " " + doc.DSL.Version},
		{"Source units file", doc.Source.SourceUnitsFile},
		{"Requirement atoms file", doc.Source.RequirementAtomsFile},
		{"CRUD matrix file", doc.Source.CRUDMatrixFile},
		{"Review decisions file", doc.Source.ReviewDecisionsFile},
		{"Entities", fmt.Sprintf("%d", len(doc.Entities))},
		{"Relationships", fmt.Sprintf("%d", len(doc.Relationships))},
		{"Constraints", fmt.Sprintf("%d", len(doc.Constraints))},
		{"Requirement atoms", fmt.Sprintf("%d", len(bundle.RequirementAtoms.RequirementAtoms))},
		{"Source units", fmt.Sprintf("%d", len(bundle.SourceUnits.SourceUnits))},
	})
	fmt.Fprintln(&out)
	if doc.Model.Description != "" {
		fmt.Fprintf(&out, "%s\n\n", doc.Model.Description)
	}

	fmt.Fprintln(&out, "## Coverage")
	writeKeyValueTable(&out, [][2]string{
		{"Source units referenced by atoms", fmt.Sprintf("%d", len(atomSourceUnits))},
		{"Source units referenced by model", fmt.Sprintf("%d", len(modelSourceUnits))},
		{"Requirement atoms referenced by model", fmt.Sprintf("%d", len(modelAtoms))},
		{"CRUD actors", fmt.Sprintf("%d", len(bundle.CRUDMatrix.Actors))},
		{"CRUD operations", fmt.Sprintf("%d", len(bundle.CRUDMatrix.Operations))},
		{"CRUD rows", fmt.Sprintf("%d", len(bundle.CRUDMatrix.Matrix))},
	})
	fmt.Fprintln(&out)

	fmt.Fprintln(&out, "## Review Decisions")
	writeReviewDecisionTableV05(&out, bundle.ReviewDecisions.ReviewDecisions)

	fmt.Fprintln(&out, "## Requirement Atom Coverage")
	writeRequirementAtomTableV05(&out, bundle.RequirementAtoms.RequirementAtoms, modelAtoms)

	fmt.Fprintln(&out, "## Model Trace Matrix")
	writeTraceTableV05(&out, rows)

	fmt.Fprintln(&out, "## CRUD Operations")
	writeCRUDOperationTableV05(&out, bundle.CRUDMatrix.Operations)

	fmt.Fprintln(&out, "## Source Unit Appendix")
	writeSourceUnitTableV05(&out, bundle.SourceUnits.SourceUnits, modelSourceUnits)

	return strings.TrimRight(out.String(), "\n") + "\n"
}

func buildTraceRowsV05(doc *dsl.Document) []traceRowV05 {
	var rows []traceRowV05
	for _, entity := range doc.Entities {
		rows = append(rows, traceRowV05{
			Kind:        "entity",
			Element:     entity.ID,
			Expression:  fmt.Sprintf("%s table=%s kind=%s", entity.Label, entity.TableName, entity.Kind),
			Atoms:       entity.Evidence.RequirementAtoms,
			SourceUnits: entity.Evidence.SourceUnits,
			Reviews:     entity.Evidence.ReviewDecisions,
			Support:     entity.Evidence.SupportLevel,
			Confidence:  entity.Evidence.Confidence,
		})
		for _, attribute := range entity.Attributes {
			rows = append(rows, traceRowV05{
				Kind:        "attribute",
				Element:     entity.ID + "." + attribute.ID,
				Expression:  attributeExpression(attribute),
				Atoms:       attribute.Evidence.RequirementAtoms,
				SourceUnits: attribute.Evidence.SourceUnits,
				Reviews:     attribute.Evidence.ReviewDecisions,
				Support:     attribute.Evidence.SupportLevel,
				Confidence:  attribute.Evidence.Confidence,
			})
		}
	}
	for _, relationship := range doc.Relationships {
		rows = append(rows, traceRowV05{
			Kind:        "relationship",
			Element:     relationship.ID,
			Expression:  relationshipExpression(relationship),
			Atoms:       relationship.Evidence.RequirementAtoms,
			SourceUnits: relationship.Evidence.SourceUnits,
			Reviews:     relationship.Evidence.ReviewDecisions,
			Support:     relationship.Evidence.SupportLevel,
			Confidence:  relationship.Evidence.Confidence,
		})
	}
	for _, constraint := range doc.Constraints {
		rows = append(rows, traceRowV05{
			Kind:        "constraint",
			Element:     constraint.ID,
			Expression:  constraintExpression(constraint),
			Atoms:       constraint.Evidence.RequirementAtoms,
			SourceUnits: constraint.Evidence.SourceUnits,
			Reviews:     constraint.Evidence.ReviewDecisions,
			Support:     constraint.Evidence.SupportLevel,
			Confidence:  constraint.Evidence.Confidence,
		})
	}
	for _, importSpec := range doc.ImportSpecs {
		rows = append(rows, traceRowV05{
			Kind:        "import_spec",
			Element:     importSpec.ID,
			Expression:  fmt.Sprintf("%s root=%s mappings=%d", importSpec.Format, importSpec.Root, len(importSpec.Mappings)),
			Atoms:       importSpec.Evidence.RequirementAtoms,
			SourceUnits: importSpec.Evidence.SourceUnits,
			Reviews:     importSpec.Evidence.ReviewDecisions,
			Support:     importSpec.Evidence.SupportLevel,
			Confidence:  importSpec.Evidence.Confidence,
		})
	}
	for _, machine := range doc.StateMachines {
		rows = append(rows, traceRowV05{
			Kind:        "state_machine",
			Element:     machine.ID,
			Expression:  fmt.Sprintf("%s.%s states=%d transitions=%d", machine.Owner, machine.Field, len(machine.States), len(machine.Transitions)),
			Atoms:       machine.Evidence.RequirementAtoms,
			SourceUnits: machine.Evidence.SourceUnits,
			Reviews:     machine.Evidence.ReviewDecisions,
			Support:     machine.Evidence.SupportLevel,
			Confidence:  machine.Evidence.Confidence,
		})
	}
	for _, view := range doc.DerivedViews {
		rows = append(rows, traceRowV05{
			Kind:        "derived_view",
			Element:     view.ID,
			Expression:  fmt.Sprintf("%s kind=%s persistence=%s sources=%s", view.Label, view.Kind, view.Persistence, strings.Join(view.Sources, ", ")),
			Atoms:       view.Evidence.RequirementAtoms,
			SourceUnits: view.Evidence.SourceUnits,
			Reviews:     view.Evidence.ReviewDecisions,
			Support:     view.Evidence.SupportLevel,
			Confidence:  view.Evidence.Confidence,
		})
	}
	for _, spec := range doc.FileSpecs {
		rows = append(rows, traceRowV05{
			Kind:        "file_spec",
			Element:     spec.ID,
			Expression:  fmt.Sprintf("%s.%s storage=%s", spec.Owner, spec.Field, spec.Storage),
			Atoms:       spec.Evidence.RequirementAtoms,
			SourceUnits: spec.Evidence.SourceUnits,
			Reviews:     spec.Evidence.ReviewDecisions,
			Support:     spec.Evidence.SupportLevel,
			Confidence:  spec.Evidence.Confidence,
		})
	}
	return rows
}

func v05TraceCoverage(rows []traceRowV05) (map[string]bool, map[string]bool) {
	sourceUnits := map[string]bool{}
	atoms := map[string]bool{}
	for _, row := range rows {
		for _, sourceUnit := range row.SourceUnits {
			sourceUnits[sourceUnit] = true
		}
		for _, atom := range row.Atoms {
			atoms[atom] = true
		}
	}
	return sourceUnits, atoms
}

func sourceUnitsReferencedByAtoms(atoms []dsl.RequirementAtom) map[string]bool {
	out := map[string]bool{}
	for _, atom := range atoms {
		for _, sourceUnit := range atom.SourceUnits {
			out[sourceUnit] = true
		}
	}
	return out
}

func writeTraceTableV05(out *bytes.Buffer, rows []traceRowV05) {
	fmt.Fprintln(out, "| Kind | Element | Expression | Requirement atoms | Source units | Review decisions | Support | Confidence |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, row := range rows {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			mdCell(row.Kind),
			mdCell(row.Element),
			mdCell(row.Expression),
			mdCell(strings.Join(row.Atoms, ", ")),
			mdCell(strings.Join(row.SourceUnits, ", ")),
			mdCell(strings.Join(row.Reviews, ", ")),
			mdCell(row.Support),
			mdCell(row.Confidence),
		)
	}
	fmt.Fprintln(out)
}

func writeRequirementAtomTableV05(out *bytes.Buffer, atoms []dsl.RequirementAtom, modelAtoms map[string]bool) {
	fmt.Fprintln(out, "| Atom | Outcome | Area | Relevance | Source units | Model impact count | Referenced by model | Statement |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- | --- | --- |")
	for _, atom := range atoms {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %d | %s | %s |\n",
			mdCell(atom.ID),
			mdCell(atom.ModelingOutcome.Status),
			mdCell(atom.FunctionalArea),
			mdCell(atom.ModelingRelevance),
			mdCell(strings.Join(atom.SourceUnits, ", ")),
			modelImpactCountV05(atom.ModelImpacts),
			mdCell(boolText(modelAtoms[atom.ID])),
			mdCell(atom.Statement),
		)
	}
	fmt.Fprintln(out)
}

func writeReviewDecisionTableV05(out *bytes.Buffer, decisions []dsl.V05ReviewDecision) {
	fmt.Fprintln(out, "| Review | Status | Selected option | Affected atoms | Question | Rationale |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- |")
	for _, decision := range decisions {
		status, _ := decision.Decision["status"].(string)
		selectedOption, _ := decision.Decision["selected_option"].(string)
		rationale, _ := decision.Decision["rationale"].(string)
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s |\n",
			mdCell(decision.ID),
			mdCell(status),
			mdCell(selectedOption),
			mdCell(strings.Join(decision.AffectedAtoms, ", ")),
			mdCell(decision.Question),
			mdCell(rationale),
		)
	}
	fmt.Fprintln(out)
}

func writeCRUDOperationTableV05(out *bytes.Buffer, operations []dsl.CRUDOperation) {
	fmt.Fprintln(out, "| Operation | Actor | Area | Source atoms | Source units | Description |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- | --- |")
	for _, operation := range operations {
		fmt.Fprintf(out, "| %s | %s | %s | %s | %s | %s |\n",
			mdCell(operation.ID),
			mdCell(operation.Actor),
			mdCell(operation.FunctionalArea),
			mdCell(strings.Join(operation.SourceAtoms, ", ")),
			mdCell(strings.Join(operation.SourceUnits, ", ")),
			mdCell(operation.Description),
		)
	}
	fmt.Fprintln(out)
}

func writeSourceUnitTableV05(out *bytes.Buffer, units []dsl.SourceUnit, modelSourceUnits map[string]bool) {
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

func modelImpactCountV05(impacts dsl.RequirementModelImpacts) int {
	return len(impacts.Entities) +
		len(impacts.Attributes) +
		len(impacts.Relationships) +
		len(impacts.Constraints) +
		len(impacts.ImportSpecs) +
		len(impacts.StateMachines) +
		len(impacts.DerivedViews) +
		len(impacts.FileSpecs)
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
