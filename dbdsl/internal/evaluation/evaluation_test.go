package evaluation

import (
	"os"
	"path/filepath"
	"testing"

	"dbdsl/internal/scaffold"
)

func TestCompareIdenticalScaffoldBundle(t *testing.T) {
	dir := t.TempDir()
	result, err := scaffold.BundleFromText("Print shop stores products. Every product has a unique code.", dir, scaffold.Options{Name: "Print shop"})
	if err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	report, err := Compare(result.ModelPath, result.ModelPath)
	if err != nil {
		t.Fatalf("compare: %v", err)
	}
	if report.Entity.F1 != 1 || report.Relationship.F1 != 1 || report.Attribute.F1 != 1 || !report.ValidationSuccess || !report.DBMLGenerationSuccess {
		t.Fatalf("unexpected identical-model metrics: %+v", report)
	}
	outDir := t.TempDir()
	if err := Write(report, outDir); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, name := range []string{"evaluation_report.json", "evaluation_report.md"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}
