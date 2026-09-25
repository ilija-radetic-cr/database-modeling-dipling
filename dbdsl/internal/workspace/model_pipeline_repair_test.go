package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/llmpipeline"
	"dbdsl/internal/scaffold"
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

func TestNormalizeConceptualModelCollectionsRepairsLegacyNulls(t *testing.T) {
	model := llmpipeline.ConceptualModelProposal{}
	normalizeConceptualModelCollections(&model)
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("marshal normalized conceptual model: %v", err)
	}
	for _, forbidden := range []string{
		`"entity_concepts":null`, `"relationships":null`, `"lifecycle_concepts":null`,
		`"derived_concepts":null`, `"file_concepts":null`, `"import_concepts":null`,
		`"unresolved_review_ids":null`, `"warnings":null`, `"confidence_summary":null`,
	} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("legacy null collection was not normalized: %s", encoded)
		}
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

func TestPromoteStagedLogicalDraftRevalidatesBeforeCommit(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Recover logical draft", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	conceptualRel := filepath.ToSlash(filepath.Join(store.projectWorkspaceRel(project.ID), "conceptual_model.accepted.json"))
	if err := writeAtomic(store.absoluteWorkspacePath(conceptualRel), []byte(`{"entity_concepts":[{"id":"entity.product"}]}`)); err != nil {
		t.Fatalf("write conceptual marker: %v", err)
	}
	if err := store.withProject(project.ID, project.CurrentRevision, func(current *ProjectState) error {
		current.AnalysisReady = true
		current.ConceptualModelAcceptedPath = conceptualRel
		current.LifecycleStatus = "ready_for_model_generation"
		return nil
	}); err != nil {
		t.Fatalf("prepare project state: %v", err)
	}
	state, _ := store.Project(project.ID)
	candidateDir := store.absoluteWorkspacePath(store.projectRevisionRel(project.ID, state.CurrentRevision+1))
	if _, err := scaffold.BundleFromText("Products have names.", candidateDir, scaffold.Options{ModelID: "recovery", Name: "Recovery"}); err != nil {
		t.Fatalf("scaffold staged bundle: %v", err)
	}
	writeRepairFixture(t, filepath.Join(candidateDir, "dbdsl_patch.proposed.json"), llmpipeline.PatchProposal{
		Operations: []llmpipeline.PatchOperation{{Operation: "add_entity"}},
	})
	writeRepairFixture(t, filepath.Join(candidateDir, "validation_report.json"), map[string]any{
		"ok": false, "errors": []string{"validator bug fixed after draft generation"},
	})

	revision, updated, err := store.PromoteStagedLogicalDraft(project.ID, state.CurrentRevision, nil)
	if err != nil {
		t.Fatalf("promote staged logical draft: %v", err)
	}
	if revision != state.CurrentRevision+1 || !testContains(updated, "logical_model") {
		t.Fatalf("unexpected promotion result: revision=%d updated=%v", revision, updated)
	}
	current, _ := store.Project(project.ID)
	if !current.ModelGenerated || current.LifecycleStatus != "model_generated" || current.ModelPath == "" {
		t.Fatalf("logical draft was not committed: %+v", current)
	}
	var report struct {
		OK bool `json:"ok"`
	}
	if err := readJSON(store.absoluteWorkspacePath(current.ValidationReportPath), &report); err != nil || !report.OK {
		t.Fatalf("fresh validation report is not successful: report=%+v err=%v", report, err)
	}
	if _, err := os.Stat(store.absoluteWorkspacePath(current.QualityReportPath)); err != nil {
		t.Fatalf("quality report missing after promotion: %v", err)
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
