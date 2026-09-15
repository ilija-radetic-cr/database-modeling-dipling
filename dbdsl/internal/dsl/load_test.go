package dsl

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadV05BundleLoadsCompanionFiles(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "source_units.yaml", "source_units:\n  - id: SU-001\n    text:\n      normalized: Test source.\n")
	writeTestFile(t, dir, "requirement_atoms.yaml", "requirement_atoms:\n  - id: RA-001\n    statement: Test atom.\n")
	writeTestFile(t, dir, "functional_decomposition.yaml", "functional_areas:\n  - id: area\n")
	writeTestFile(t, dir, "crud_matrix.yaml", "actors:\n  - id: system\noperations: []\nmatrix: []\n")
	writeTestFile(t, dir, "review_decisions.yaml", "review_decisions:\n  - id: RD-001\n")
	modelPath := writeTestFile(t, dir, "db_model.dsl.yaml", `
dsl:
  name: DB-DSL
  version: "0.5"
model:
  id: test
source:
  source_units_file: source_units.yaml
  requirement_atoms_file: requirement_atoms.yaml
  functional_decomposition_file: functional_decomposition.yaml
  crud_matrix_file: crud_matrix.yaml
  review_decisions_file: review_decisions.yaml
`)

	bundle, err := LoadV05Bundle(modelPath)
	if err != nil {
		t.Fatalf("LoadV05Bundle failed: %v", err)
	}
	if got := len(bundle.SourceUnits.SourceUnits); got != 1 {
		t.Fatalf("expected 1 source unit, got %d", got)
	}
	if got := len(bundle.RequirementAtoms.RequirementAtoms); got != 1 {
		t.Fatalf("expected 1 requirement atom, got %d", got)
	}
}

func TestLoadV05BundleRequiresSourceUnitsFile(t *testing.T) {
	dir := t.TempDir()
	modelPath := writeTestFile(t, dir, "db_model.dsl.yaml", `
dsl:
  name: DB-DSL
  version: "0.5"
model:
  id: test
source:
  requirement_atoms_file: requirement_atoms.yaml
  functional_decomposition_file: functional_decomposition.yaml
  crud_matrix_file: crud_matrix.yaml
  review_decisions_file: review_decisions.yaml
`)

	if _, err := LoadV05Bundle(modelPath); err == nil {
		t.Fatalf("expected missing source_units_file error")
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
