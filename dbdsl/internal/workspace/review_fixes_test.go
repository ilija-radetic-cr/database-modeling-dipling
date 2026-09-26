package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

// reviewedWithOperatorFinding runs the review and records one operator finding
// with a waive decision, so the conceptual model can be accepted.
func reviewedWithOperatorFinding(t *testing.T) (*Store, string, int, []llmpipelineSourceUnitAlias) {
	t.Helper()
	store, projectID, revision, units := conceptualCandidateForReview(t)
	revision, _, err := store.GenerateAdversarialReview(context.Background(), llm.NewDefaultMockClient(), projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("review: %v", err)
	}
	view, err := store.RecordAdversarialOperatorFinding(projectID, RecordAdversarialOperatorFindingOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "Operator finding for the test.",
		Finding: operatorFinding(units, "OF-001"),
	})
	if err != nil {
		t.Fatalf("operator finding: %v", err)
	}
	view, err = store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: view.ProjectRevision, Actor: "test_operator",
		Decisions: []AdversarialReviewDecisionInput{{FindingID: "OF-001", Decision: "waive", Note: "Waived for the test."}},
	})
	if err != nil {
		t.Fatalf("waive: %v", err)
	}
	return store, projectID, view.ProjectRevision, units
}

func operatorFinding(units []llmpipelineSourceUnitAlias, id string) llmpipeline.AdversarialFinding {
	return llmpipeline.AdversarialFinding{
		ID: id, Severity: "warning", Category: "missing", SourceUnitIDs: []string{units[0].ID}, DescriptionRefs: []string{},
		SourceQuote: units[0].Text.Exact, Claim: "A cited detail is absent.", Expected: "Preserve the cited detail.",
		Actual: "The candidate omits the detail.", SuggestedCorrection: "Add the grounded detail.",
	}
}

func acceptConceptual(t *testing.T, store *Store, projectID string, revision int) int {
	t.Helper()
	revision, err := store.AcceptConceptualModel(projectID, AcceptConceptualModelOptions{BaseRevision: revision, Actor: "test_operator", Note: "Accepted for the test."})
	if err != nil {
		t.Fatalf("accept conceptual model: %v", err)
	}
	return revision
}

// F01: a writer that loses the race for a revision leaves the winner's files
// untouched and no staging directory behind.
func TestLosingRevisionStageDoesNotTouchWinner(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Race", "", "en", "race")
	if err != nil {
		t.Fatal(err)
	}
	base := project.CurrentRevision
	winner, err := store.newRevisionStage(project.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	loser, err := store.newRevisionStage(project.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	winnerPaths, err := winner.write(map[string]artifactValue{"TASK.md": {Value: "winner", Raw: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loser.write(map[string]artifactValue{"TASK.md": {Value: "loser", Raw: true}, "extra.md": {Value: "loser", Raw: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.absoluteWorkspacePath(winnerPaths["TASK.md"])); !os.IsNotExist(err) {
		t.Fatalf("an unpublished stage is already visible: %v", err)
	}
	revision, err := store.publishRevision(winner, func(*ProjectState) error { return nil })
	if err != nil || revision != base+1 {
		t.Fatalf("publish winner: revision=%d err=%v", revision, err)
	}
	if _, err := store.publishRevision(loser, func(*ProjectState) error { return nil }); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("losing stage published: %v", err)
	}
	final := store.absoluteWorkspacePath(winnerPaths["TASK.md"])
	if data, err := os.ReadFile(final); err != nil || string(data) != "winner" {
		t.Fatalf("winner's file changed: %q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(final), "extra.md")); !os.IsNotExist(err) {
		t.Fatalf("losing stage left a file in the winner's revision: %v", err)
	}
	staging := store.absoluteWorkspacePath(filepath.ToSlash(filepath.Join(store.projectWorkspaceRel(project.ID), "revisions", ".staging")))
	if entries, _ := os.ReadDir(staging); len(entries) != 0 {
		t.Fatalf("stages left behind: %v", entries)
	}
}

// F02: dispositions cannot change once the conceptual model is accepted.
func TestReviewDecisionsCannotChangeAfterConceptualAcceptance(t *testing.T) {
	store, projectID, revision, _ := reviewedWithOperatorFinding(t)
	revision = acceptConceptual(t, store, projectID, revision)
	_, err := store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: revision, Actor: "test_operator",
		Decisions: []AdversarialReviewDecisionInput{{FindingID: "OF-001", Decision: "request_correction", Note: "Changed my mind."}},
	})
	if err == nil || !strings.Contains(err.Error(), "after conceptual model acceptance") {
		t.Fatalf("decision changed after acceptance: %v", err)
	}
	if project, _ := store.Project(projectID); project.CurrentRevision != revision {
		t.Fatalf("rejected decision committed a revision: %d != %d", project.CurrentRevision, revision)
	}
}

// F10: an earlier acceptance does not make a stale review acceptable.
func TestStaleReviewAfterAcceptanceCannotAccept(t *testing.T) {
	store, projectID, revision, _ := reviewedWithOperatorFinding(t)
	acceptConceptual(t, store, projectID, revision)
	store.mu.Lock()
	project := store.projects[projectID]
	profile := *project.LLMExecutionProfile
	profile.PromptVersion += "+reviewer_policy_test_bump"
	project.LLMExecutionProfile = &profile
	store.mu.Unlock()
	view, err := store.AdversarialReview(projectID)
	if err != nil || view.Status != "stale" || view.CanAccept || !view.Accepted {
		t.Fatalf("stale review after acceptance: status=%s can_accept=%v accepted=%v err=%v", view.Status, view.CanAccept, view.Accepted, err)
	}
}

// F05: a finding ID used anywhere in the history of the same candidate and
// sources cannot be reused, and the rejection commits nothing.
func TestOperatorFindingCannotReuseHistoricalID(t *testing.T) {
	store, projectID, revision, units := reviewedWithOperatorFinding(t)
	_, err := store.RecordAdversarialOperatorFinding(projectID, RecordAdversarialOperatorFindingOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "Reuses an existing ID.", Finding: operatorFinding(units, "OF-001"),
	})
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("reused finding ID accepted: %v", err)
	}
	if project, _ := store.Project(projectID); project.CurrentRevision != revision {
		t.Fatalf("rejected finding committed a revision: %d != %d", project.CurrentRevision, revision)
	}
	if _, err := store.AdversarialReview(projectID); err != nil {
		t.Fatalf("history unreadable after rejected finding: %v", err)
	}
}

// F03: a completed project is read-only until it is reopened; its outputs stay
// readable and exportable.
func TestCompletedProjectRejectsMutations(t *testing.T) {
	store, projectID, revision, units := reviewedWithOperatorFinding(t)
	revision = acceptConceptual(t, store, projectID, revision)
	var err error
	if revision, _, err = store.GenerateLogicalModel(context.Background(), nil, projectID, ModelStageOptions{BaseRevision: revision}); err != nil {
		t.Fatalf("logical model: %v", err)
	}
	if revision, err = store.AcceptFinalModel(projectID, revision); err != nil {
		t.Fatalf("accept final model: %v", err)
	}
	if revision, _, err = store.GenerateFinalOutputs(projectID, revision, nil); err != nil {
		t.Fatalf("final outputs: %v", err)
	}
	if _, _, err = store.CompleteProject(projectID, revision); err != nil {
		t.Fatalf("complete: %v", err)
	}
	project, _ := store.Project(projectID)
	revision = project.CurrentRevision

	mutations := map[string]func() error{
		"rename": func() error { return store.UpdateProject(projectID, revision, "Renamed", "") },
		"add resource": func() error {
			_, _, err := store.AddPastedTextResource(projectID, revision, "More", "Another sentence.")
			return err
		},
		"process sources": func() error {
			_, _, err := store.ProcessSourcesWithLLM(context.Background(), llm.NewDefaultMockClient(), projectID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
			return err
		},
		"regenerate conceptual": func() error {
			_, _, err := store.GenerateConceptualModel(context.Background(), llm.NewDefaultMockClient(), projectID, ModelStageOptions{BaseRevision: revision, Model: "mock-model"})
			return err
		},
		"operator finding": func() error {
			_, err := store.RecordAdversarialOperatorFinding(projectID, RecordAdversarialOperatorFindingOptions{
				BaseRevision: revision, Actor: "test_operator", Note: "Late finding.", Finding: operatorFinding(units, ""),
			})
			return err
		},
		"accept final": func() error {
			_, err := store.AcceptFinalModel(projectID, revision)
			return err
		},
		"final outputs": func() error {
			_, _, err := store.GenerateFinalOutputs(projectID, revision, nil)
			return err
		},
	}
	for name, mutate := range mutations {
		if err := mutate(); !errors.Is(err, ErrProjectCompleted) {
			t.Errorf("%s on a completed project: %v, want ErrProjectCompleted", name, err)
		}
	}
	if after, _ := store.Project(projectID); after.CurrentRevision != revision || !after.Completed {
		t.Fatalf("completed project changed: revision %d -> %d completed=%v", revision, after.CurrentRevision, after.Completed)
	}
	if err := store.RequireMutable(projectID); !errors.Is(err, ErrProjectCompleted) {
		t.Fatalf("RequireMutable = %v, want ErrProjectCompleted", err)
	}
	if dbml, err := store.DBML(projectID); err != nil || !strings.Contains(dbml, "Table") {
		t.Fatalf("completed project's DBML unreadable: %v", err)
	}
	if bundle, err := store.ExportBundle(projectID); err != nil || len(bundle) == 0 {
		t.Fatalf("completed project cannot be exported: %v", err)
	}
}
