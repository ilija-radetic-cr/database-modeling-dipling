package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

// TestSegmentFlowRunsWithoutRequirementStages covers the segment-based flow:
// segmentation with deterministic source-unit IDs, the conceptual description, the
// deterministic logical model and final outputs, with no requirement,
// functional or CRUD stages. The mandatory adversarial review is included.
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
	revision, _, err = store.GenerateAdversarialReview(context.Background(), mock, project.ID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("adversarial review: %v", err)
	}
	sourceArtifacts, err := store.SourceUnitArtifacts(project.ID)
	if err != nil || len(sourceArtifacts.Accepted.SourceUnits) == 0 {
		t.Fatalf("source evidence for operator finding: %v", err)
	}
	unit := sourceArtifacts.Accepted.SourceUnits[0]
	reviewView, err := store.RecordAdversarialOperatorFinding(project.ID, RecordAdversarialOperatorFindingOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "End-to-end portable operator finding.",
		Finding: llmpipeline.AdversarialFinding{
			Severity: "warning", Category: "missing", SourceUnitIDs: []string{unit.ID}, DescriptionRefs: []string{conceptual.Description.Things[0].ID},
			SourceQuote: unit.Text.Exact, Claim: "A cited detail is absent.", Expected: "Preserve the cited detail.",
			Actual: "The candidate omits the detail.", SuggestedCorrection: "Add the grounded detail.",
		},
	})
	if err != nil {
		t.Fatalf("operator finding: %v", err)
	}
	reviewView, err = store.RecordAdversarialReviewDecisions(project.ID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: reviewView.ProjectRevision, Actor: "test_operator", Decisions: []AdversarialReviewDecisionInput{{
			FindingID: "OF-001", Decision: "waive", Note: "Synthetic portable finding is explicitly waived for this test.",
		}},
	})
	if err != nil {
		t.Fatalf("operator finding disposition: %v", err)
	}
	revision = reviewView.ProjectRevision
	if revision, err = store.AcceptConceptualModel(project.ID, AcceptConceptualModelOptions{BaseRevision: revision, Actor: "test_operator", Note: "Automated end-to-end workspace test."}); err != nil {
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
	if revision, _, err = store.GenerateFinalOutputs(project.ID, revision, nil); err != nil {
		t.Fatalf("final outputs: %v", err)
	}
	if dbml, err := store.DBML(project.ID); err != nil || !strings.Contains(dbml, "Table") {
		t.Fatalf("DBML missing: %q %v", dbml, err)
	}
	if health, _ := store.ArtifactHealth(project.ID); !health.CanCompleteProject {
		t.Fatalf("project should be completable: %+v", health)
	}
	if _, _, err = store.CompleteProject(project.ID, revision); err != nil {
		t.Fatalf("complete project: %v", err)
	}
	originalState, ok := store.Project(project.ID)
	if !ok {
		t.Fatal("completed source project disappeared")
	}
	originalModel, err := os.ReadFile(originalState.ModelPath)
	if err != nil {
		t.Fatalf("read original accepted model: %v", err)
	}
	reopened, snapshotID, err := store.ReopenProject(project.ID, "Continue review")
	if err != nil || snapshotID == "" || reopened.ID == project.ID {
		t.Fatalf("reopen completed project: reopened=%+v snapshot=%q err=%v", reopened, snapshotID, err)
	}
	reopenedState, ok := store.Project(reopened.ID)
	if !ok || reopenedState.LLMExecutionProfile == nil || reopenedState.LLMExecutionProfile.Provider != "mock" || reopenedState.LLMExecutionProfile.Model != "mock-model" {
		t.Fatalf("reopened project did not preserve its execution profile: %+v", reopenedState)
	}
	if reopenedState.ModelPath == originalState.ModelPath || !strings.Contains(reopenedState.ModelPath, reopened.ID) {
		t.Fatalf("reopened project still references the original model: original=%q reopened=%q", originalState.ModelPath, reopenedState.ModelPath)
	}
	reopenedReview, err := store.AdversarialReview(reopened.ID)
	if err != nil || reopenedReview.Status != "current" || len(reopenedReview.Review.Findings) != 1 || len(reopenedReview.FindingProvenance) != 1 || reopenedReview.FindingProvenance[0].Origin != "operator" || len(reopenedReview.Audit) < 4 || reopenedReview.Audit[len(reopenedReview.Audit)-1].Actor != "test_operator" {
		t.Fatalf("reopened project lost review or acceptance audit: view=%+v err=%v", reopenedReview, err)
	}
	reopenedModel, err := os.ReadFile(reopenedState.ModelPath)
	if err != nil {
		t.Fatalf("read reopened model: %v", err)
	}
	if err := os.WriteFile(reopenedState.ModelPath, append(reopenedModel, []byte("\n# reopened snapshot mutation\n")...), 0o644); err != nil {
		t.Fatalf("mutate reopened model snapshot: %v", err)
	}
	reopenedRevision, err := store.AcceptFinalModel(reopened.ID, reopened.CurrentRevision)
	if err != nil {
		t.Fatalf("accept reopened model: %v", err)
	}
	unchangedOriginal, err := os.ReadFile(originalState.ModelPath)
	if err != nil || !bytes.Equal(originalModel, unchangedOriginal) {
		t.Fatalf("mutating reopened project changed the original model: err=%v", err)
	}
	if err := store.DeleteProject(project.ID); err != nil {
		t.Fatalf("delete original completed project: %v", err)
	}
	if _, err := os.Stat(originalState.ModelPath); !os.IsNotExist(err) {
		t.Fatalf("original model still exists after project deletion: %v", err)
	}
	if _, err := store.CombinedDocument(reopened.ID); err != nil {
		t.Fatalf("reopened combined document depends on deleted original: %v", err)
	}
	if _, err := store.ConceptualModel(reopened.ID); err != nil {
		t.Fatalf("reopened conceptual model depends on deleted original: %v", err)
	}
	if _, err := store.SourceUnits(reopened.ID); err != nil {
		t.Fatalf("reopened source units depend on deleted original: %v", err)
	}
	if _, err := store.ModelGraph(reopened.ID); err != nil {
		t.Fatalf("reopened graph depends on deleted original: %v", err)
	}
	if _, err := store.TraceIndex(reopened.ID); err != nil {
		t.Fatalf("reopened trace index depends on deleted original: %v", err)
	}
	if trace, err := store.TraceReport(reopened.ID); err != nil || !strings.Contains(trace, "Traceability") {
		t.Fatalf("reopened trace report unavailable after original deletion: %v", err)
	}
	reopenedRevision, _, err = store.GenerateFinalOutputs(reopened.ID, reopenedRevision, nil)
	if err != nil {
		t.Fatalf("generate reopened outputs after original deletion: %v", err)
	}
	if dbml, err := store.DBML(reopened.ID); err != nil || !strings.Contains(dbml, "Table") {
		t.Fatalf("reopened DBML unavailable: %v", err)
	}
	bundle, err := store.ExportBundle(reopened.ID)
	if err != nil || len(bundle) == 0 {
		t.Fatalf("reopened export unavailable: %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(bundle), int64(len(bundle)))
	if err != nil {
		t.Fatalf("read reopened export: %v", err)
	}
	exported := map[string]bool{}
	var exportedHistory AdversarialReviewHistory
	for _, file := range archive.File {
		exported[file.Name] = true
		if file.Name == "adversarial_review_history.json" {
			reader, openErr := file.Open()
			if openErr != nil {
				t.Fatalf("open exported review history: %v", openErr)
			}
			if decodeErr := json.NewDecoder(reader).Decode(&exportedHistory); decodeErr != nil {
				_ = reader.Close()
				t.Fatalf("decode exported review history: %v", decodeErr)
			}
			_ = reader.Close()
		}
	}
	if !exported["adversarial_review_history.json"] || !exported["export_manifest.json"] {
		t.Fatalf("export omitted adversarial audit history or manifest: %v", exported)
	}
	if len(exportedHistory.OperatorFindings) != 1 || exportedHistory.OperatorFindings[0].Finding.ID != "OF-001" || exportedHistory.OperatorFindings[0].Actor != "test_operator" {
		t.Fatalf("export lost operator finding provenance: %+v", exportedHistory.OperatorFindings)
	}
	historyPath := store.absoluteWorkspacePath(reopenedState.AdversarialReviewPath)
	historyBytes, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatalf("read reopened audit history: %v", err)
	}
	if err := os.WriteFile(historyPath, []byte(`{"version":1,"reviews":`), 0o644); err != nil {
		t.Fatalf("corrupt reopened audit history: %v", err)
	}
	if _, err := store.ExportBundle(reopened.ID); err == nil {
		t.Fatal("export succeeded after required audit history was corrupted")
	}
	if err := os.WriteFile(historyPath, historyBytes, 0o644); err != nil {
		t.Fatalf("restore reopened audit history: %v", err)
	}
	if err := os.Remove(historyPath); err != nil {
		t.Fatalf("remove reopened audit history: %v", err)
	}
	if _, err := store.ExportBundle(reopened.ID); err == nil {
		t.Fatal("export succeeded after required audit history was deleted")
	}
	if err := os.WriteFile(historyPath, historyBytes, 0o644); err != nil {
		t.Fatalf("restore deleted reopened audit history: %v", err)
	}
	if _, _, err := store.CompleteProject(reopened.ID, reopenedRevision); err != nil {
		t.Fatalf("complete reopened project: %v", err)
	}
}
