package scaffold

import (
	"path/filepath"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/lint"
	"dbdsl/internal/validate"
)

func TestBundleFromTextProducesValidV06Bundle(t *testing.T) {
	dir := t.TempDir()
	result, err := BundleFromText("# Library\n\nA member borrows books. Every loan has a due date.", dir, Options{Name: "Library"})
	if err != nil {
		t.Fatalf("scaffold: %v", err)
	}
	if len(result.Files) != 4 {
		t.Fatalf("expected 4 bundle files, got %v", result.Files)
	}
	bundle, err := dsl.LoadV06Bundle(result.ModelPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if bundle.Document.DSL.Version != "0.6" || len(bundle.SourceUnits.SourceUnits) == 0 {
		t.Fatalf("unexpected bundle: version=%s units=%d", bundle.Document.DSL.Version, len(bundle.SourceUnits.SourceUnits))
	}
	if validation := validate.ValidateFile(result.ModelPath); !validation.OK() {
		t.Fatalf("scaffold bundle is invalid: %v", validation.Errors)
	}
	if issues := lint.LintFile(filepath.Join(dir, "db_model.dsl.yaml")); issues.HasErrors() {
		t.Fatalf("scaffold bundle has lint errors: %+v", issues.Issues)
	}
}
