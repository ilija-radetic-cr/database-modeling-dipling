package workspace

import (
	"context"
	"strings"
	"testing"

	"dbdsl/internal/llm"
)

// TestSegmentFlowRunsWithoutRequirementStages covers the segment-based flow:
// segmentation with deterministic source-unit IDs, the conceptual description, the
// deterministic logical model and final outputs, with no requirement,
// functional, CRUD or review stage.
func TestSegmentFlowRunsWithoutRequirementStages(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Library", "", "sr", "library")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, revision, err := store.AddPastedTextResource(project.ID, 0, "Task", "Član biblioteke pozajmljuje knjige. Svaka pozajmica ima datum vraćanja.")
	if err != nil {
		t.Fatalf("add resource: %v", err)
	}
	mock := llm.NewDefaultMockClient()
	revision, _, err = store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("process sources: %v", err)
	}
	revision, _, err = store.GenerateConceptualModel(context.Background(), mock, project.ID, ModelStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("conceptual model: %v", err)
	}
	conceptual, err := store.ConceptualModel(project.ID)
	if err != nil || !conceptual.QA.OK || conceptual.Description == nil || len(conceptual.Proposed.EntityConcepts) != 2 {
		t.Fatalf("unexpected conceptual model: %+v err=%v", conceptual, err)
	}
	if revision, err = store.AcceptConceptualModel(project.ID, revision); err != nil {
		t.Fatalf("accept conceptual model: %v", err)
	}
	if revision, _, err = store.GenerateLogicalModel(context.Background(), nil, project.ID, ModelStageOptions{BaseRevision: revision}); err != nil {
		t.Fatalf("logical model: %v", err)
	}
	health, err := store.ArtifactHealth(project.ID)
	if err != nil || health.ModelStatus != "ready" {
		t.Fatalf("unexpected health after logical model: %+v err=%v", health, err)
	}
	if revision, err = store.AcceptFinalModel(project.ID, revision); err != nil {
		t.Fatalf("accept final model: %v", err)
	}
	if _, _, err = store.GenerateFinalOutputs(project.ID, revision, nil); err != nil {
		t.Fatalf("final outputs: %v", err)
	}
	if dbml, err := store.DBML(project.ID); err != nil || !strings.Contains(dbml, "Table") {
		t.Fatalf("DBML missing: %q %v", dbml, err)
	}
	if health, _ := store.ArtifactHealth(project.ID); !health.CanCompleteProject {
		t.Fatalf("project should be completable: %+v", health)
	}
}
