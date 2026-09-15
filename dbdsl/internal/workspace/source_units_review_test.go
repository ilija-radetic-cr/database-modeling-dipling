package workspace

import (
	"context"
	"encoding/json"
	"testing"

	"dbdsl/internal/llm"
)

func TestReviewSourceUnitPersistsRevisionedDecision(t *testing.T) {
	tests := []struct {
		name              string
		decision          string
		normalizedText    string
		expectedText      string
		expectedRelevance string
	}{
		{name: "accept", decision: "accept", expectedText: "Products have names.", expectedRelevance: "model_relevant"},
		{name: "revise", decision: "revise", normalizedText: "Each product has a name.", expectedText: "Each product has a name.", expectedRelevance: "model_relevant"},
		{name: "exclude", decision: "exclude", expectedText: "Products have names.", expectedRelevance: "non_model"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newIngestionTestStore(t)
			project, err := store.CreateProject("Products", "", "en", "catalog")
			if err != nil {
				t.Fatalf("create project: %v", err)
			}
			_, revision, err := store.AddPastedTextResource(project.ID, "Task", "Products have names.")
			if err != nil {
				t.Fatalf("add resource: %v", err)
			}
			mock := llm.NewDefaultMockClient()
			revision, _, err = store.ProcessSources(context.Background(), mock, project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
			if err != nil {
				t.Fatalf("process sources: %v", err)
			}
			revision, _, err = store.GenerateSourceUnits(context.Background(), mock, project.ID, GenerateSourceUnitsOptions{BaseRevision: revision, Model: "mock-model"})
			if err != nil {
				t.Fatalf("generate source units: %v", err)
			}

			state, _ := store.Project(project.ID)
			oldQAPath := state.SourceUnitQAPath
			artifacts, err := store.SourceUnitArtifacts(project.ID)
			if err != nil {
				t.Fatalf("load source-unit artifacts: %v", err)
			}
			artifacts.QA.NeedsAttention = []string{"SU-001"}
			qaBytes, err := json.MarshalIndent(artifacts.QA, "", "  ")
			if err != nil {
				t.Fatalf("marshal QA fixture: %v", err)
			}
			if err := writeAtomic(store.absoluteWorkspacePath(oldQAPath), qaBytes); err != nil {
				t.Fatalf("write QA fixture: %v", err)
			}

			newRevision, remaining, err := store.ReviewSourceUnit(project.ID, "SU-001", ReviewSourceUnitOptions{
				BaseRevision: revision, Decision: test.decision, NormalizedText: test.normalizedText,
				Note: "Reviewed in test", ReviewedBy: "tester",
			})
			if err != nil {
				t.Fatalf("review source unit: %v", err)
			}
			if newRevision != revision+1 || remaining != 0 {
				t.Fatalf("unexpected review result: revision=%d remaining=%d", newRevision, remaining)
			}
			updatedState, _ := store.Project(project.ID)
			if updatedState.SourceUnitQAPath == oldQAPath {
				t.Fatalf("review did not create a revisioned QA artifact")
			}
			updated, err := store.SourceUnitArtifacts(project.ID)
			if err != nil {
				t.Fatalf("reload reviewed artifacts: %v", err)
			}
			if len(updated.QA.NeedsAttention) != 0 || len(updated.QA.ReviewDecisions) != 1 {
				t.Fatalf("review gate or audit record was not updated: %+v", updated.QA)
			}
			decision := updated.QA.ReviewDecisions[0]
			if decision.Decision != test.decision || decision.ReviewedBy != "tester" || decision.ProjectRevision != newRevision {
				t.Fatalf("unexpected audit decision: %+v", decision)
			}
			unit, found, err := store.SourceUnit(project.ID, "SU-001")
			if err != nil || !found {
				t.Fatalf("load reviewed source unit: found=%v err=%v", found, err)
			}
			if unit.ReviewStatus != "reviewed" || unit.NormalizedText != test.expectedText || unit.Relevance != test.expectedRelevance {
				t.Fatalf("unexpected reviewed unit: %+v", unit)
			}
			health, err := store.ArtifactHealth(project.ID)
			if err != nil || health.SourceUnitsStatus != "ready" || !health.CanExtractRequirements {
				t.Fatalf("review did not open the requirement gate: health=%+v err=%v", health, err)
			}
		})
	}
}

func TestReviewSourceUnitValidatesRevisionAndDecision(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, _, err := store.ReviewSourceUnit(project.ID, "SU-001", ReviewSourceUnitOptions{BaseRevision: project.CurrentRevision + 1, Decision: "accept"}); err != ErrRevisionConflict {
		t.Fatalf("expected revision conflict, got %v", err)
	}
}
