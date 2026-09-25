package evaluation

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"dbdsl/internal/dsl"
	"dbdsl/internal/generate"
	"dbdsl/internal/lint"
	"dbdsl/internal/validate"
)

type Metric struct {
	Reference int     `json:"reference"`
	Candidate int     `json:"candidate"`
	Matched   int     `json:"matched"`
	Precision float64 `json:"precision"`
	Recall    float64 `json:"recall"`
	F1        float64 `json:"f1"`
}

type Report struct {
	Version               int                 `json:"version"`
	GeneratedAt           time.Time           `json:"generated_at"`
	ReferenceModel        string              `json:"reference_model"`
	CandidateModel        string              `json:"candidate_model"`
	Entity                Metric              `json:"entity"`
	Relationship          Metric              `json:"relationship"`
	Attribute             Metric              `json:"attribute"`
	Constraint            Metric              `json:"constraint"`
	SourceTraceCoverage   float64             `json:"source_trace_coverage"`
	UnsupportedElements   int                 `json:"unsupported_elements"`
	ValidationSuccess     bool                `json:"validation_success"`
	LintErrors            int                 `json:"lint_errors"`
	LintWarnings          int                 `json:"lint_warnings"`
	DBMLGenerationSuccess bool                `json:"dbml_generation_success"`
	Missed                map[string][]string `json:"missed"`
	Unsupported           map[string][]string `json:"unsupported"`
}

func Compare(referencePath, candidatePath string) (Report, error) {
	reference, err := dsl.LoadV06Bundle(referencePath)
	if err != nil {
		return Report{}, fmt.Errorf("load reference bundle: %w", err)
	}
	candidate, err := dsl.LoadV06Bundle(candidatePath)
	if err != nil {
		return Report{}, fmt.Errorf("load candidate bundle: %w", err)
	}
	refSets := bundleSets(reference)
	candidateSets := bundleSets(candidate)
	report := Report{
		Version: 1, GeneratedAt: time.Now(), ReferenceModel: referencePath, CandidateModel: candidatePath,
		Entity: compareSets(refSets.entities, candidateSets.entities), Relationship: compareSets(refSets.relationships, candidateSets.relationships),
		Attribute: compareSets(refSets.attributes, candidateSets.attributes), Constraint: compareSets(refSets.constraints, candidateSets.constraints),
		Missed: map[string][]string{}, Unsupported: map[string][]string{},
	}
	report.Missed["entities"], report.Unsupported["entities"] = difference(refSets.entities, candidateSets.entities), difference(candidateSets.entities, refSets.entities)
	report.Missed["relationships"], report.Unsupported["relationships"] = difference(refSets.relationships, candidateSets.relationships), difference(candidateSets.relationships, refSets.relationships)
	report.Missed["attributes"], report.Unsupported["attributes"] = difference(refSets.attributes, candidateSets.attributes), difference(candidateSets.attributes, refSets.attributes)
	report.Missed["constraints"], report.Unsupported["constraints"] = difference(refSets.constraints, candidateSets.constraints), difference(candidateSets.constraints, refSets.constraints)
	report.UnsupportedElements = len(report.Unsupported["entities"]) + len(report.Unsupported["relationships"]) + len(report.Unsupported["attributes"]) + len(report.Unsupported["constraints"])
	report.SourceTraceCoverage = traceCoverage(candidate.Document)
	validation := validate.ValidateFile(candidatePath)
	report.ValidationSuccess = validation.OK()
	lintResult := lint.LintFile(candidatePath)
	for _, issue := range lintResult.Issues {
		switch issue.Severity {
		case "error":
			report.LintErrors++
		case "warning":
			report.LintWarnings++
		}
	}
	_, dbmlErr := generate.DBMLFile(candidatePath)
	report.DBMLGenerationSuccess = dbmlErr == nil
	return report, nil
}

func Write(report Report, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(outDir, "evaluation_report.json"), data); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(outDir, "evaluation_report.md"), []byte(markdown(report)))
}

type sets struct{ entities, relationships, attributes, constraints map[string]bool }

func bundleSets(bundle *dsl.Bundle) sets {
	out := sets{map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}}
	entityNames := map[string]string{}
	for _, entity := range bundle.Document.Entities {
		name := normalize(nonEmpty(entity.TableName, entity.ID))
		entityNames[entity.ID] = name
		out.entities[name] = true
		for _, attribute := range entity.Attributes {
			out.attributes[name+"."+normalize(attribute.ID)] = true
		}
	}
	for _, relationship := range bundle.Document.Relationships {
		from, to := entityNames[relationship.From], entityNames[relationship.To]
		if from > to {
			from, to = to, from
		}
		out.relationships[from+"|"+to+"|"+normalize(relationship.Cardinality)] = true
	}
	for _, constraint := range bundle.Document.Constraints {
		owner := nonEmpty(entityNames[constraint.Owner], normalize(constraint.Owner))
		fields := append([]string(nil), constraint.Fields...)
		if constraint.Field != "" {
			fields = append(fields, constraint.Field)
		}
		sort.Strings(fields)
		out.constraints[owner+"|"+normalize(constraint.Type)+"|"+normalize(strings.Join(fields, ","))] = true
	}
	return out
}

func compareSets(reference, candidate map[string]bool) Metric {
	matched := 0
	for key := range candidate {
		if reference[key] {
			matched++
		}
	}
	metric := Metric{Reference: len(reference), Candidate: len(candidate), Matched: matched}
	metric.Precision = ratio(matched, len(candidate))
	metric.Recall = ratio(matched, len(reference))
	if metric.Precision+metric.Recall > 0 {
		metric.F1 = round(2 * metric.Precision * metric.Recall / (metric.Precision + metric.Recall))
	}
	return metric
}

func traceCoverage(document *dsl.Document) float64 {
	total, sourceBacked := 0, 0
	record := func(evidence dsl.Evidence) {
		total++
		if len(evidence.SourceUnits) > 0 {
			sourceBacked++
		}
	}
	for _, entity := range document.Entities {
		record(entity.Evidence)
		for _, attribute := range entity.Attributes {
			record(attribute.Evidence)
		}
	}
	for _, item := range document.Relationships {
		record(item.Evidence)
	}
	for _, item := range document.Constraints {
		record(item.Evidence)
	}
	for _, item := range document.StateMachines {
		record(item.Evidence)
	}
	for _, item := range document.DerivedViews {
		record(item.Evidence)
	}
	for _, item := range document.ImportSpecs {
		record(item.Evidence)
	}
	for _, item := range document.FileSpecs {
		record(item.Evidence)
	}
	return ratio(sourceBacked, total)
}

func difference(left, right map[string]bool) []string {
	var out []string
	for key := range left {
		if !right[key] {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func ratio(numerator, denominator int) float64 {
	if denominator == 0 {
		if numerator == 0 {
			return 1
		}
		return 0
	}
	return round(float64(numerator) / float64(denominator))
}
func round(value float64) float64 { return math.Round(value*10000) / 10000 }
func normalize(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(value))), " ")
}
func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func markdown(report Report) string {
	return fmt.Sprintf("# Evaluation report\n\nGenerated: %s\n\n| Metric | Precision | Recall | F1 |\n| --- | ---: | ---: | ---: |\n| Entities | %.4f | %.4f | %.4f |\n| Relationships | %.4f | %.4f | %.4f |\n| Attributes | %.4f | %.4f | %.4f |\n| Constraints | %.4f | %.4f | %.4f |\n\n- Source trace coverage: %.4f\n- Unsupported elements: %d\n- Validation success: %t\n- Lint errors: %d\n- Lint warnings: %d\n- DBML generation success: %t\n", report.GeneratedAt.Format(time.RFC3339), report.Entity.Precision, report.Entity.Recall, report.Entity.F1, report.Relationship.Precision, report.Relationship.Recall, report.Relationship.F1, report.Attribute.Precision, report.Attribute.Recall, report.Attribute.F1, report.Constraint.Precision, report.Constraint.Recall, report.Constraint.F1, report.SourceTraceCoverage, report.UnsupportedElements, report.ValidationSuccess, report.LintErrors, report.LintWarnings, report.DBMLGenerationSuccess)
}
