package dsl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadV06BundleLoadsCompanionFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "source_units.yaml", "source_units:\n  - id: SU-001\n    text:\n      normalized: Test source.\n")
	writeTestFile(t, dir, "review_decisions.yaml", "review_decisions:\n  - id: RD-001\n")
	modelPath := writeTestFile(t, dir, "db_model.dsl.yaml", `
dsl:
  name: DB-DSL
  version: "0.6"
model:
  id: test
source:
  source_units_file: source_units.yaml
  review_decisions_file: review_decisions.yaml
`)

	bundle, err := LoadV06Bundle(modelPath)
	if err != nil {
		t.Fatalf("LoadV06Bundle failed: %v", err)
	}
	if got := len(bundle.SourceUnits.SourceUnits); got != 1 {
		t.Fatalf("expected 1 source unit, got %d", got)
	}
	if got := len(bundle.ReviewDecisions.ReviewDecisions); got != 1 {
		t.Fatalf("expected 1 review decision, got %d", got)
	}
}

func TestLoadV06BundleRequiresSourceUnitsFile(t *testing.T) {
	dir := t.TempDir()
	modelPath := writeTestFile(t, dir, "db_model.dsl.yaml", `
dsl:
  name: DB-DSL
  version: "0.6"
model:
  id: test
source:
  review_decisions_file: review_decisions.yaml
`)

	if _, err := LoadV06Bundle(modelPath); err == nil {
		t.Fatalf("expected missing source_units_file error")
	}
}

func TestLoadV06BundleRejectsOtherVersions(t *testing.T) {
	dir := t.TempDir()
	modelPath := writeTestFile(t, dir, "db_model.dsl.yaml", "dsl:\n  name: DB-DSL\n  version: \"0.5\"\nmodel:\n  id: test\n")
	if _, err := LoadV06Bundle(modelPath); err == nil {
		t.Fatalf("expected version error for a v0.5 document")
	}
}

func writeTestFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}
