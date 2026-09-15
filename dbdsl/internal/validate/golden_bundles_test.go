package validate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"dbdsl/internal/lint"
	"gopkg.in/yaml.v3"
)

type bundleAuditManifest struct {
	Version int `yaml:"version"`
	Bundles []struct {
		Path                    string `yaml:"path"`
		Role                    string `yaml:"role"`
		Origin                  string `yaml:"origin"`
		ExpectedValidation      string `yaml:"expected_validation"`
		ExpectedLintErrors      int    `yaml:"expected_lint_errors"`
		ExpectedLintWarningsMax int    `yaml:"expected_lint_warnings_max"`
	} `yaml:"bundles"`
}

func TestDeclaredBundleFixturesMatchValidationExpectations(t *testing.T) {
	_, currentFile, _, _ := runtime.Caller(0)
	workspaceRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	manifestPath := filepath.Join(workspaceRoot, "fixtures", "bundles.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read bundle manifest: %v", err)
	}
	var manifest bundleAuditManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse bundle manifest: %v", err)
	}
	if manifest.Version != 1 || len(manifest.Bundles) == 0 {
		t.Fatalf("invalid or empty bundle manifest: %+v", manifest)
	}
	goldenCount := 0
	for _, fixture := range manifest.Bundles {
		fixture := fixture
		t.Run(fixture.Role+"/"+filepath.Base(filepath.Dir(fixture.Path)), func(t *testing.T) {
			modelPath := filepath.Join(workspaceRoot, filepath.FromSlash(fixture.Path))
			if _, err := os.Stat(modelPath); err != nil {
				t.Fatalf("declared fixture does not exist: %v", err)
			}
			validation := ValidateFile(modelPath)
			switch fixture.ExpectedValidation {
			case "pass":
				if !validation.OK() {
					t.Fatalf("expected validation pass: %v", validation.Errors)
				}
			case "fail":
				if validation.OK() {
					t.Fatalf("expected negative fixture to fail validation")
				}
			default:
				t.Fatalf("unknown expected_validation %q", fixture.ExpectedValidation)
			}
			if fixture.Role == "golden" {
				goldenCount++
				result := lint.LintFile(modelPath)
				if got := result.Count(lint.SeverityError); got != fixture.ExpectedLintErrors {
					t.Fatalf("lint errors: got %d want %d", got, fixture.ExpectedLintErrors)
				}
				if got := result.Count(lint.SeverityWarning); got > fixture.ExpectedLintWarningsMax {
					t.Fatalf("lint warnings: got %d max %d", got, fixture.ExpectedLintWarningsMax)
				}
			}
		})
	}
	if goldenCount == 0 {
		t.Fatal("bundle manifest has no golden fixture")
	}
}
