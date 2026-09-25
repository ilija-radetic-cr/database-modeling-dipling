package llmpipeline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"

	"dbdsl/internal/dsl"
	"dbdsl/internal/lint"
	"dbdsl/internal/llmpipeline"
	"dbdsl/internal/validate"
)

// TestDeterministicMappingOnRecordedProjects replays accepted conceptual models
// from the local workbench through the deterministic mapper and full DB-DSL v0.5
// validation. It only runs when DBDSL_REAL_PROJECTS points at a projects dir.
func TestDeterministicMappingOnRecordedProjects(t *testing.T) {
	root := os.Getenv("DBDSL_REAL_PROJECTS")
	if root == "" {
		t.Skip("set DBDSL_REAL_PROJECTS to replay recorded projects")
	}
	projects, _ := filepath.Glob(filepath.Join(root, "project_*"))
	for _, project := range projects {
		latest := func(name string) string {
			matches, _ := filepath.Glob(filepath.Join(project, "revisions", "*", name))
			sort.Strings(matches)
			if len(matches) == 0 {
				return ""
			}
			return matches[len(matches)-1]
		}
		conceptualPath := latest("conceptual_model.accepted.json")
		if conceptualPath == "" {
			continue
		}
		t.Run(filepath.Base(project), func(t *testing.T) {
			var conceptual llmpipeline.ConceptualModelProposal
			var atoms llmpipeline.RequirementAtomExtractionProposal
			var functional llmpipeline.FunctionalAnalysisProposal
			var crud llmpipeline.CRUDMappingProposal
			var units dsl.V05SourceUnitsFile
			var decisions dsl.V05ReviewDecisionsFile
			readJSON(t, conceptualPath, &conceptual)
			readJSON(t, latest("requirement_atoms.proposed.json"), &atoms)
			readJSON(t, latest("functional_analysis.proposed.json"), &functional)
			readJSON(t, latest("crud_mapping.proposed.json"), &crud)
			readYAML(t, latest("source_units.yaml"), &units)
			if path := latest("review_decisions.yaml"); path != "" {
				readYAML(t, path, &decisions)
			}
			patch, report, err := llmpipeline.MapConceptualToLogical(conceptual, atoms.RequirementAtoms, llmpipeline.LogicalMappingOptions{})
			if err != nil {
				t.Fatalf("map: %v", err)
			}
			artifacts, err := llmpipeline.BuildLogicalArtifacts("replay", units.SourceUnits, atoms, functional, crud, patch, decisions.ReviewDecisions)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			bundle := &dsl.V05Bundle{Document: &artifacts.Model, SourceUnits: &units, RequirementAtoms: &artifacts.RequirementAtoms,
				FunctionalDecomposition: &artifacts.FunctionalDecomposition, CRUDMatrix: &artifacts.CRUDMatrix, ReviewDecisions: &artifacts.ReviewDecisions}
			result := validate.ValidateV05Bundle(bundle)
			t.Logf("entities=%d relationships=%d constraints=%d state_machines=%d derived=%d inferred=%d decisions=%d warnings=%d",
				report.Entities, report.Relationships, report.Constraints, report.StateMachines, report.DerivedViews, len(report.Inferred), len(report.Decisions), len(report.Warnings))
			for _, line := range append(report.Decisions, report.Warnings...) {
				t.Log("  ", line)
			}
			for _, message := range result.Errors {
				t.Error(message)
			}
			lintResult := lint.LintV05(bundle)
			for _, issue := range lintResult.Issues {
				if issue.Severity == lint.SeverityError {
					t.Errorf("lint error: %+v", issue)
				}
			}
			t.Logf("lint issues=%d (errors=%d)", len(lintResult.Issues), lintResult.Count(lint.SeverityError))
		})
	}
}

func readJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}

func readYAML(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := yaml.Unmarshal(data, target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}
