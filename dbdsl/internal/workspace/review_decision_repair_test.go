package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepairRestoresWorkbenchReviewDecisionsAfterFinalization(t *testing.T) {
	store := newIngestionTestStore(t)
	project, _ := store.CreateProject("Finalized", "", "en", "test")
	revisions := filepath.Join(store.projectWorkspaceDir(project.ID), "revisions")
	workbench := filepath.Join(revisions, "rev_000005", "review_decisions.yaml")
	export := filepath.Join(revisions, "rev_000009", "review_decisions.yaml")
	for path, content := range map[string]string{
		workbench: "review_decisions:\n  - id: RD-001\n    candidate_id: RC-001\n    selected_option: RC-001-O1\n",
		export:    "review_state:\n  status: resolved\nreview_decisions:\n  - id: RD-001\n    decision:\n      selected_option: RC-001-O1\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store.mu.Lock()
	store.projects[project.ID].ReviewDecisionsPath = store.relativePath(export)
	store.mu.Unlock()

	if err := store.repairReviewDecisionPaths(); err != nil {
		t.Fatalf("repair: %v", err)
	}
	repaired, _ := store.Project(project.ID)
	if store.absoluteWorkspacePath(repaired.ReviewDecisionsPath) != workbench {
		t.Fatalf("decision path = %s, want workbench record %s", repaired.ReviewDecisionsPath, workbench)
	}
	var decisions ReviewDecisionsArtifact
	if err := readYAML(store.absoluteWorkspacePath(repaired.ReviewDecisionsPath), &decisions); err != nil || decisions.ReviewDecisions[0].CandidateID != "RC-001" {
		t.Fatalf("repaired path must expose candidate IDs: %+v %v", decisions, err)
	}
}
