package evaluation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareIdenticalGoldenBundle(t *testing.T) {
	model := filepath.Join("..", "..", "..", "poc", "printing_house_full", "v0.5_granularity_sentance", "db_model.dsl.yaml")
	report, err := Compare(model, model)
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
