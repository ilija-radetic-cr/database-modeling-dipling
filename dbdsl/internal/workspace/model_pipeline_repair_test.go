package workspace

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"dbdsl/internal/llmpipeline"
)

func TestFailedLogicalRepairContextLoadsUncommittedCandidate(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Repair candidate", "", "en", "repair")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	state, ok := store.Project(project.ID)
	if !ok {
		t.Fatal("created project state is missing")
	}

	candidateDir := store.absoluteWorkspacePath(store.projectRevisionRel(project.ID, state.CurrentRevision+1))
	proposal := llmpipeline.PatchProposal{
		Operations: []llmpipeline.PatchOperation{{Operation: "add_entity"}},
	}
	report := map[string]any{
		"ok":     false,
		"errors": []string{"import target is invalid", "file field is unknown"},
	}
	writeRepairFixture(t, filepath.Join(candidateDir, "dbdsl_patch.proposed.json"), proposal)
	writeRepairFixture(t, filepath.Join(candidateDir, "validation_report.json"), report)

	loaded, validationErrors := store.failedLogicalRepairContext(state)
	if loaded == nil || len(loaded.Operations) != 1 || loaded.Operations[0].Operation != "add_entity" {
		t.Fatalf("failed candidate was not loaded: %+v", loaded)
	}
	if len(validationErrors) != 2 || validationErrors[0] != "import target is invalid" {
		t.Fatalf("validation errors were not loaded: %v", validationErrors)
	}

	validationErrors[0] = "mutated by caller"
	var persisted struct {
		Errors []string `json:"errors"`
	}
	if err := readJSON(filepath.Join(candidateDir, "validation_report.json"), &persisted); err != nil {
		t.Fatalf("read persisted validation report: %v", err)
	}
	if persisted.Errors[0] != "import target is invalid" {
		t.Fatalf("repair context leaked a mutable persisted error slice: %v", persisted.Errors)
	}
}

func TestFailedLogicalRepairContextIgnoresSuccessfulReport(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("No repair", "", "en", "no-repair")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	state, ok := store.Project(project.ID)
	if !ok {
		t.Fatal("created project state is missing")
	}

	candidateDir := store.absoluteWorkspacePath(store.projectRevisionRel(project.ID, state.CurrentRevision+1))
	writeRepairFixture(t, filepath.Join(candidateDir, "dbdsl_patch.proposed.json"), llmpipeline.PatchProposal{
		Operations: []llmpipeline.PatchOperation{{Operation: "add_entity"}},
	})
	writeRepairFixture(t, filepath.Join(candidateDir, "validation_report.json"), map[string]any{
		"ok":     true,
		"errors": []string{},
	})

	proposal, validationErrors := store.failedLogicalRepairContext(state)
	if proposal != nil || validationErrors != nil {
		t.Fatalf("successful candidate unexpectedly enabled repair: proposal=%+v errors=%v", proposal, validationErrors)
	}
}

func writeRepairFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal repair fixture: %v", err)
	}
	if err := writeAtomic(path, data); err != nil {
		t.Fatalf("write repair fixture: %v", err)
	}
}
