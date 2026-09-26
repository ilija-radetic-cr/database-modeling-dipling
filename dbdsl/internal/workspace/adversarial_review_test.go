package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"

	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

type countingReviewClient struct {
	delegate llm.Client
	mu       sync.Mutex
	requests []llm.Request
}

func (c *countingReviewClient) GenerateStructured(ctx context.Context, req llm.Request) (llm.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	return c.delegate.GenerateStructured(ctx, req)
}

func (c *countingReviewClient) GenerateText(ctx context.Context, req llm.TextRequest) (llm.TextResponse, error) {
	return c.delegate.GenerateText(ctx, req)
}

func (c *countingReviewClient) structuredRequests() []llm.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]llm.Request(nil), c.requests...)
}

func TestAdversarialReviewIsRevisionBoundCachedAndAudited(t *testing.T) {
	store, projectID, revision, units := conceptualCandidateForReview(t)
	client := &countingReviewClient{delegate: llm.NewDefaultMockClient()}
	if _, _, err := store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Scope: "logical", Model: "mock-model"}); err == nil || !strings.Contains(err.Error(), "scope must be conceptual") {
		t.Fatalf("logical review scope should be rejected in the conceptual-only release: %v", err)
	}

	revision, _, err := store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("generate review: %v", err)
	}
	project, _ := store.Project(projectID)
	historyPath := project.AdversarialReviewPath
	before, err := os.ReadFile(store.absoluteWorkspacePath(historyPath))
	if err != nil {
		t.Fatalf("read persisted review: %v", err)
	}

	cachedRevision, updated, err := store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil || cachedRevision != revision || len(updated) != 0 {
		t.Fatalf("unchanged review rerun mutated state: revision=%d updated=%v err=%v", cachedRevision, updated, err)
	}
	if calls := len(client.structuredRequests()); calls != 1 {
		t.Fatalf("unchanged review rerun made %d provider calls, want 1 total", calls)
	}
	after, _ := os.ReadFile(store.absoluteWorkspacePath(historyPath))
	if !bytes.Equal(before, after) {
		t.Fatal("unchanged review rerun rewrote persisted audit history")
	}

	if _, err := store.AcceptConceptualModel(projectID, AcceptConceptualModelOptions{BaseRevision: revision, Actor: "test_operator"}); err == nil {
		t.Fatal("acceptance without an attributed note succeeded")
	}
	revision, err = store.AcceptConceptualModel(projectID, AcceptConceptualModelOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "Synthetic acceptance exercised by the test operator.",
	})
	if err != nil {
		t.Fatalf("accept reviewed candidate: %v", err)
	}
	view, err := store.AdversarialReview(projectID)
	if err != nil || view.Status != "current" || !view.CanAccept {
		t.Fatalf("unexpected accepted review view: %+v err=%v", view, err)
	}
	last := view.Audit[len(view.Audit)-1]
	if last.Action != "conceptual_model_accepted" || last.Actor != "test_operator" || last.ProjectRevision != revision {
		t.Fatalf("acceptance audit lost attribution or revision: %+v", last)
	}
	project, _ = store.Project(projectID)
	history, err := store.loadAdversarialReviewHistory(project)
	if err != nil || len(history.Snapshots) != 1 || history.Snapshots[0].CandidateHash != view.Review.CandidateHash {
		t.Fatalf("exact reviewed candidate snapshot was not preserved: %+v err=%v", history.Snapshots, err)
	}
	acceptedPath := project.ConceptualModelAcceptedPath
	if _, err := store.RecordAdversarialOperatorFinding(projectID, RecordAdversarialOperatorFindingOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "Must not reopen an accepted candidate.",
		Finding: llmpipeline.AdversarialFinding{
			Severity: "warning", Category: "missing", SourceUnitIDs: []string{units[0].ID}, DescriptionRefs: descriptionRefs(t, store, projectID),
			SourceQuote: units[0].Text.Exact, Claim: "Late finding.", Expected: "Stay accepted.", Actual: "Already accepted.", SuggestedCorrection: "Do not append.",
		},
	}); err == nil || !strings.Contains(err.Error(), "after conceptual model acceptance") {
		t.Fatalf("operator finding after acceptance returned %v", err)
	}
	project, _ = store.Project(projectID)
	if project.CurrentRevision != revision || project.ConceptualModelAcceptedPath != acceptedPath {
		t.Fatalf("rejected post-acceptance finding mutated accepted state: %+v", project)
	}
}

func TestAdversarialReviewReuseIsBoundToReviewerPolicyVersion(t *testing.T) {
	store, projectID, revision, units := conceptualCandidateForReview(t)
	client := &countingReviewClient{delegate: llm.NewDefaultMockClient()}
	firstRevision, _, err := store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("first review: %v", err)
	}
	firstView, err := store.AdversarialReview(projectID)
	if err != nil || firstView.Review == nil || firstView.Review.ReviewerPolicyVersion == "" {
		t.Fatalf("first review omitted its reviewer policy version: %+v err=%v", firstView, err)
	}
	firstView, err = store.RecordAdversarialOperatorFinding(projectID, RecordAdversarialOperatorFindingOptions{
		BaseRevision: firstRevision, Actor: "test_operator", Note: "Preserve this unresolved finding across policy review.",
		Finding: llmpipeline.AdversarialFinding{
			Severity: "error", Category: "missing", SourceUnitIDs: []string{units[0].ID}, DescriptionRefs: []string{},
			SourceQuote: units[0].Text.Exact, Claim: "A required identity rule is absent.", Expected: "Represent the identity rule.",
			Actual: "The candidate omits it.", SuggestedCorrection: "Add the grounded identity rule.",
		},
	})
	if err != nil {
		t.Fatalf("add unresolved operator finding: %v", err)
	}
	firstRevision = firstView.ProjectRevision
	operatorReviewID := firstView.Review.ReviewID

	store.mu.Lock()
	project := store.projects[projectID]
	profile := *project.LLMExecutionProfile
	profile.PromptVersion += "+reviewer_policy_test_bump"
	project.LLMExecutionProfile = &profile
	if err := store.saveLocked(); err != nil {
		store.mu.Unlock()
		t.Fatalf("persist test policy bump: %v", err)
	}
	store.mu.Unlock()
	staleView, err := store.AdversarialReview(projectID)
	if err != nil || staleView.Status != "stale" || staleView.CanAccept {
		t.Fatalf("old reviewer policy remained acceptable after policy bump: %+v err=%v", staleView, err)
	}

	secondRevision, _, err := store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: firstRevision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("review after policy bump: %v", err)
	}
	if secondRevision != firstRevision+1 || len(client.structuredRequests()) != 2 {
		t.Fatalf("policy bump reused old review: revisions=%d->%d calls=%d", firstRevision, secondRevision, len(client.structuredRequests()))
	}
	secondView, err := store.AdversarialReview(projectID)
	if err != nil || secondView.Status != "current" || secondView.Review == nil || len(secondView.Review.Findings) != 1 || secondView.Review.Findings[0].ID != "OF-001" || secondView.CanAccept {
		t.Fatalf("policy re-review lost unresolved operator finding or opened the gate: %+v err=%v", secondView, err)
	}
	if secondView.Review.ReviewID == operatorReviewID || len(secondView.FindingProvenance) != 1 || secondView.FindingProvenance[0].ReviewID != operatorReviewID {
		t.Fatalf("carried finding lost its immutable origin review: review=%s provenance=%+v", secondView.Review.ReviewID, secondView.FindingProvenance)
	}
	if _, err := store.AcceptConceptualModel(projectID, AcceptConceptualModelOptions{BaseRevision: secondRevision, Actor: "test_operator", Note: "Must remain blocked."}); err == nil {
		t.Fatal("policy re-review accepted a candidate with an unresolved operator finding")
	}
	requests := client.structuredRequests()
	if !strings.Contains(requests[1].Input, "OF-001") || !strings.Contains(requests[1].Input, "A required identity rule is absent") {
		t.Fatalf("policy re-review omitted the preserved operator finding context: %s", requests[1].Input)
	}
	projectState, _ := store.Project(projectID)
	history, err := store.loadAdversarialReviewHistory(projectState)
	if err != nil || len(history.Reviews) != 2 || len(history.OperatorFindings) != 1 || history.Reviews[0].ReviewerPolicyVersion == history.Reviews[1].ReviewerPolicyVersion {
		t.Fatalf("review policy history was not append-only and version-bound: %+v err=%v", history.Reviews, err)
	}
	secondView, err = store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: secondRevision, Actor: "test_operator", Decisions: []AdversarialReviewDecisionInput{{
			FindingID: "OF-001", Decision: "dismiss", Note: "Explicitly disposed after the policy re-review.",
		}},
	})
	if err != nil || !secondView.CanAccept || len(secondView.Decisions) != 1 || secondView.Decisions[0].ReviewID != operatorReviewID {
		t.Fatalf("effective operator disposition did not retain its immutable review binding: %+v err=%v", secondView, err)
	}
}

func TestOperatorFindingIsGroundedAppendOnlyAndCorrectionEligible(t *testing.T) {
	store, projectID, revision, units := conceptualCandidateForReview(t)
	client := llm.NewDefaultMockClient()
	var err error
	revision, _, err = store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("generate empty review: %v", err)
	}
	refs := descriptionRefs(t, store, projectID)
	if len(refs) == 0 {
		t.Fatal("conceptual fixture has no description references")
	}
	valid := llmpipeline.AdversarialFinding{
		Severity: "error", Category: "missing", SourceUnitIDs: []string{units[0].ID}, DescriptionRefs: []string{refs[0]},
		SourceQuote: units[0].Text.Exact, Claim: "The explicit identity rule is absent.",
		Expected: "Preserve the cited identity rule.", Actual: "The candidate omits the identity rule.",
		SuggestedCorrection: "Add the source-grounded identity constraint.",
	}
	invalid := []RecordAdversarialOperatorFindingOptions{
		{BaseRevision: revision - 1, Actor: "test_operator", Note: "Stale finding.", Finding: valid},
		{BaseRevision: revision, Actor: "test_operator", Note: "Unknown source.", Finding: func() llmpipeline.AdversarialFinding {
			item := valid
			item.SourceUnitIDs = []string{"SU-unknown"}
			return item
		}()},
		{BaseRevision: revision, Actor: "test_operator", Note: "Unsupported quote.", Finding: func() llmpipeline.AdversarialFinding {
			item := valid
			item.SourceQuote = "not an exact source substring"
			return item
		}()},
		{BaseRevision: revision, Actor: "test_operator", Note: "Unknown description reference.", Finding: func() llmpipeline.AdversarialFinding {
			item := valid
			item.DescriptionRefs = []string{"ENT-unknown"}
			return item
		}()},
	}
	for _, input := range invalid {
		if _, recordErr := store.RecordAdversarialOperatorFinding(projectID, input); recordErr == nil {
			t.Fatalf("ungrounded or stale operator finding succeeded: %+v", input)
		}
	}
	project, _ := store.Project(projectID)
	if project.CurrentRevision != revision {
		t.Fatalf("invalid operator finding mutated revision to %d", project.CurrentRevision)
	}

	view, err := store.RecordAdversarialOperatorFinding(projectID, RecordAdversarialOperatorFindingOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "The test operator found an omitted identity requirement.", Finding: valid,
	})
	if err != nil {
		t.Fatalf("record grounded operator finding: %v", err)
	}
	revision = view.ProjectRevision
	if view.Review == nil || len(view.Review.Findings) != 1 || view.Review.Findings[0].ID != "OF-001" || view.CanAccept {
		t.Fatalf("combined review did not expose the blocking operator finding: %+v", view)
	}
	if len(view.FindingProvenance) != 1 || view.FindingProvenance[0].Origin != "operator" || view.FindingProvenance[0].Actor != "test_operator" {
		t.Fatalf("operator finding provenance was not explicit: %+v", view.FindingProvenance)
	}
	project, _ = store.Project(projectID)
	history, err := store.loadAdversarialReviewHistory(project)
	if err != nil {
		t.Fatalf("reload operator finding history: %v", err)
	}
	if len(history.Reviews) != 1 || len(history.Reviews[0].Findings) != 0 || len(history.OperatorFindings) != 1 || history.OperatorFindings[0].Finding.ID != "OF-001" {
		t.Fatalf("operator finding rewrote the original LLM review: %+v", history)
	}
	if last := history.Audit[len(history.Audit)-1]; last.Action != "operator_finding_added" || last.Actor != "test_operator" || last.FindingID != "OF-001" {
		t.Fatalf("operator finding audit is incomplete: %+v", last)
	}

	duplicate := valid
	duplicate.ID = "OF-001"
	if _, err := store.RecordAdversarialOperatorFinding(projectID, RecordAdversarialOperatorFindingOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "Duplicate ID.", Finding: duplicate,
	}); err == nil {
		t.Fatal("duplicate operator finding ID succeeded")
	}

	view, err = store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: revision, Actor: "human", Decisions: []AdversarialReviewDecisionInput{{
			FindingID: "OF-001", Decision: "request_correction", Note: "Initial attribution was mistaken.",
		}},
	})
	if err != nil {
		t.Fatalf("record initial correction decision: %v", err)
	}
	view, err = store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: view.ProjectRevision, Actor: "test_operator", Decisions: []AdversarialReviewDecisionInput{{
			FindingID: "OF-001", Decision: "request_correction", Note: "Corrected append-only test-operator attribution.",
		}},
	})
	if err != nil {
		t.Fatalf("append corrected request_correction attribution: %v", err)
	}
	if len(view.Decisions) != 2 || view.Decisions[0].Actor != "human" || view.Decisions[1].Actor != "test_operator" || view.CanAccept {
		t.Fatalf("corrected attribution did not preserve history and blocking disposition: %+v", view.Decisions)
	}
	if err := store.ValidateAdversarialCorrectionRequest(projectID, AdversarialCorrectionRequestOptions{
		BaseRevision: view.ProjectRevision, Actor: "test_operator", Note: "Apply the source-grounded operator finding.",
	}); err != nil {
		t.Fatalf("operator finding was not eligible for bounded correction: %v", err)
	}
	if _, err := store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: view.ProjectRevision, Actor: "test_operator", Decisions: []AdversarialReviewDecisionInput{{
			FindingID: "OF-001", Decision: "waive", Note: "Do not bypass an outstanding correction.",
		}},
	}); err == nil {
		t.Fatal("outstanding correction was bypassed by a later waiver")
	}
}

func TestAdversarialReviewDecisionsCorrectionAndReReview(t *testing.T) {
	store, projectID, revision, units := conceptualCandidateForReview(t)
	mock := llm.NewDefaultMockClient()
	mock.Structured[llmpipeline.AdversarialReviewStage] = reviewPayload(t, units[0].ID, units[0].Text.Exact, descriptionRefs(t, store, projectID))
	client := &countingReviewClient{delegate: mock}

	revision, _, err := store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("generate review with finding: %v", err)
	}
	project, _ := store.Project(projectID)
	beforePath := project.AdversarialReviewPath
	before, _ := os.ReadFile(store.absoluteWorkspacePath(beforePath))
	invalid := []RecordAdversarialReviewDecisionsOptions{
		{BaseRevision: revision, Actor: "test_operator", Decisions: []AdversarialReviewDecisionInput{{FindingID: "AR-001", Decision: "dismiss"}}},
		{BaseRevision: revision, Actor: "test_operator", Decisions: []AdversarialReviewDecisionInput{{FindingID: "missing", Decision: "dismiss", Note: "Not applicable."}}},
		{BaseRevision: revision, Actor: "automation", Decisions: []AdversarialReviewDecisionInput{{FindingID: "AR-001", Decision: "dismiss", Note: "Not applicable."}}},
	}
	for _, input := range invalid {
		if _, err := store.RecordAdversarialReviewDecisions(projectID, input); err == nil {
			t.Fatalf("invalid decision succeeded: %+v", input)
		}
	}
	unchanged, _ := store.Project(projectID)
	unchangedBytes, _ := os.ReadFile(store.absoluteWorkspacePath(beforePath))
	if unchanged.CurrentRevision != revision || !bytes.Equal(before, unchangedBytes) {
		t.Fatal("invalid decision mutated the revision or persisted history")
	}

	view, err := store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: revision, Actor: "test_operator",
		Decisions: []AdversarialReviewDecisionInput{{FindingID: "AR-001", Decision: "request_correction", Note: "Treat the date as an explicit unresolved detail."}},
	})
	if err != nil {
		t.Fatalf("record correction decision: %v", err)
	}
	revision = view.ProjectRevision
	if view.CanAccept {
		t.Fatal("request_correction decision allowed acceptance")
	}
	if _, err := store.AcceptConceptualModel(projectID, AcceptConceptualModelOptions{BaseRevision: revision, Actor: "test_operator", Note: "Should remain blocked."}); err == nil {
		t.Fatal("candidate awaiting correction was accepted")
	}
	if _, err := store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
		BaseRevision: revision - 1, Actor: "test_operator",
		Decisions: []AdversarialReviewDecisionInput{{FindingID: "AR-001", Decision: "waive", Note: "Stale mutation."}},
	}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale decision returned %v, want revision conflict", err)
	}

	revision, err = store.RecordAdversarialCorrectionRequest(projectID, AdversarialCorrectionRequestOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "Generate one bounded correction proposal.",
	})
	if err != nil {
		t.Fatalf("record correction request: %v", err)
	}
	revision, _, err = store.ApplyAdversarialCorrection(context.Background(), mock, projectID, AdversarialCorrectionRunOptions{
		BaseRevision: revision, Actor: "test_operator", Note: "Generate one bounded correction proposal.", Model: "mock-model",
	})
	if err != nil {
		t.Fatalf("apply correction: %v", err)
	}
	view, err = store.AdversarialReview(projectID)
	if err != nil || view.Status != "stale" || view.CanAccept {
		t.Fatalf("correction did not invalidate the prior review: %+v err=%v", view, err)
	}
	lastAudit := view.Audit[len(view.Audit)-1]
	if lastAudit.Action != "correction_proposed" || len(lastAudit.Changes) == 0 || !strings.Contains(lastAudit.Result, lastAudit.Changes[0]) {
		t.Fatalf("correction audit omitted deterministic change details: %+v", lastAudit)
	}
	project, _ = store.Project(projectID)
	if project.ConceptualModelAcceptedPath != "" {
		t.Fatal("correction proposal silently accepted the conceptual model")
	}
	if _, err := store.AcceptConceptualModel(projectID, AcceptConceptualModelOptions{BaseRevision: revision, Actor: "test_operator", Note: "Fresh review still required."}); err == nil {
		t.Fatal("corrected proposal was accepted without a fresh review")
	}

	revision, _, err = store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("re-review corrected proposal: %v", err)
	}
	requests := client.structuredRequests()
	if len(requests) != 2 || !strings.Contains(requests[1].Input, `"actor":"test_operator"`) || !strings.Contains(requests[1].Input, "Treat the date as an explicit unresolved detail") {
		t.Fatalf("re-review omitted explicit prior operator context: calls=%d input=%s", len(requests), requests[len(requests)-1].Input)
	}
	view, _ = store.AdversarialReview(projectID)
	if view.Status != "current" || view.Review == nil || view.Review.CandidateRevision != revision-1 {
		t.Fatalf("fresh review is not bound to the correction revision: %+v", view)
	}
}

func TestAdversarialOperatorContextIsSourceScopedAndDispositionAware(t *testing.T) {
	history := AdversarialReviewHistory{
		Reviews: []AdversarialReviewRecord{
			{ReviewID: "review-old", CandidateHash: "candidate-old", SourceHash: "source-old"},
			{ReviewID: "review-current", CandidateHash: "candidate-current", SourceHash: "source-current"},
		},
		Audit: []AdversarialReviewAuditEvent{
			{EventID: "audit-old", ReviewID: "review-old", FindingID: "AR-old", Action: "finding_decided", Actor: "test_operator", Note: "Old-source ambiguity.", Result: "dismiss"},
			{EventID: "audit-current", ReviewID: "review-current", FindingID: "AR-current", Action: "finding_decided", Actor: "test_operator", Note: "Current-source clarification.", Result: "waive"},
		},
	}
	context := adversarialOperatorContext(history, "source-current", "candidate-current")
	if len(context) != 1 || context[0].FindingID != "AR-current" || context[0].Action != "finding_decided:waive" || !strings.Contains(context[0].ID, "candidate-current:source-current") {
		t.Fatalf("operator context was not source-scoped and disposition-aware: %+v", context)
	}
}

func TestConcurrentAdversarialDecisionsPersistOnlyWinner(t *testing.T) {
	store, projectID, revision, units := conceptualCandidateForReview(t)
	mock := llm.NewDefaultMockClient()
	mock.Structured[llmpipeline.AdversarialReviewStage] = reviewPayload(t, units[0].ID, units[0].Text.Exact, descriptionRefs(t, store, projectID))
	var err error
	revision, _, err = store.GenerateAdversarialReview(context.Background(), mock, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		decision string
		err      error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for _, decision := range []string{"dismiss", "waive"} {
		decision := decision
		go func() {
			<-start
			_, recordErr := store.RecordAdversarialReviewDecisions(projectID, RecordAdversarialReviewDecisionsOptions{
				BaseRevision: revision, Actor: "test_operator",
				Decisions: []AdversarialReviewDecisionInput{{FindingID: "AR-001", Decision: decision, Note: "Concurrent " + decision + " decision."}},
			})
			results <- result{decision: decision, err: recordErr}
		}()
	}
	close(start)
	first, second := <-results, <-results
	successes := 0
	winner := ""
	for _, got := range []result{first, second} {
		if got.err == nil {
			successes++
			winner = got.decision
		} else if !errors.Is(got.err, ErrRevisionConflict) {
			t.Fatalf("concurrent loser returned %v", got.err)
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent decisions had %d winners: %+v %+v", successes, first, second)
	}
	project, _ := store.Project(projectID)
	var persisted AdversarialReviewHistory
	if err := readJSON(store.absoluteWorkspacePath(project.AdversarialReviewPath), &persisted); err != nil {
		t.Fatalf("read persisted winning history: %v", err)
	}
	if len(persisted.Decisions) != 1 || persisted.Decisions[0].Decision != winner || persisted.Decisions[0].Actor != "test_operator" {
		t.Fatalf("losing write contaminated persisted history: winner=%s history=%+v", winner, persisted.Decisions)
	}
}

func TestMalformedAdversarialReviewDoesNotMutateProject(t *testing.T) {
	store, projectID, revision, units := conceptualCandidateForReview(t)
	mock := llm.NewDefaultMockClient()
	payload := llmpipeline.AdversarialReviewProposal{
		Summary: "Malformed source reference.",
		Findings: []llmpipeline.AdversarialFinding{{
			ID: "AR-BAD", Severity: "error", Category: "missing", SourceUnitIDs: []string{"SU-unknown"},
			SourceQuote: units[0].Text.Exact, Claim: "Claim", Expected: "Expected", Actual: "Actual", SuggestedCorrection: "Correction",
		}},
	}
	raw, _ := json.Marshal(payload)
	mock.Structured[llmpipeline.AdversarialReviewStage] = raw
	client := &countingReviewClient{delegate: mock}
	if _, _, err := store.GenerateAdversarialReview(context.Background(), client, projectID, AdversarialReviewRunOptions{BaseRevision: revision, Model: "mock-model"}); err == nil {
		t.Fatal("malformed provider review succeeded")
	}
	project, _ := store.Project(projectID)
	if project.CurrentRevision != revision || project.AdversarialReviewPath != "" {
		t.Fatalf("malformed review mutated project: %+v", project)
	}
	if calls := len(client.structuredRequests()); calls != 1 {
		t.Fatalf("malformed review retried provider %d times", calls)
	}
}

func conceptualCandidateForReview(t *testing.T) (*Store, string, int, []llmpipelineSourceUnitAlias) {
	t.Helper()
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Review", "", "en", "review")
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := store.AddPastedTextResource(project.ID, 0, "Task", "A product has a name. Its retirement date may be recorded.")
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err = store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatal(err)
	}
	revision, _, err = store.GenerateConceptualModel(context.Background(), llm.NewDefaultMockClient(), project.ID, ModelStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatal(err)
	}
	artifacts, err := store.SourceUnitArtifacts(project.ID)
	if err != nil || len(artifacts.Accepted.SourceUnits) == 0 {
		t.Fatalf("read source units: %v", err)
	}
	units := make([]llmpipelineSourceUnitAlias, len(artifacts.Accepted.SourceUnits))
	for i, unit := range artifacts.Accepted.SourceUnits {
		units[i] = llmpipelineSourceUnitAlias{ID: unit.ID, Text: struct{ Exact string }{Exact: unit.Text.Exact}}
	}
	return store, project.ID, revision, units
}

// This narrow alias keeps test helpers independent of the source-unit package
// while exposing only the evidence fields used to build a valid mock finding.
type llmpipelineSourceUnitAlias struct {
	ID   string
	Text struct{ Exact string }
}

func reviewPayload(t *testing.T, sourceID, quote string, refs []string) json.RawMessage {
	t.Helper()
	proposal := llmpipeline.AdversarialReviewProposal{
		Summary: "One source-grounded issue requires an explicit operator decision.",
		Findings: []llmpipeline.AdversarialFinding{{
			ID: "AR-001", Severity: "warning", Category: "missing", SourceUnitIDs: []string{sourceID}, DescriptionRefs: refs, SourceQuote: quote,
			Claim: "The retirement date is not represented.", Expected: "Represent or explicitly defer the stated date.",
			Actual: "No candidate concept records it.", SuggestedCorrection: "Record the detail as an explicit open question.",
		}},
	}
	raw, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// descriptionRefs lists description elements a finding may cite.
func descriptionRefs(t *testing.T, store *Store, projectID string) []string {
	t.Helper()
	artifacts, err := store.ConceptualModel(projectID)
	if err != nil || artifacts.Description == nil {
		t.Fatalf("read conceptual description: %v", err)
	}
	refs := []string{}
	for _, item := range artifacts.Description.Things {
		refs = append(refs, item.ID)
	}
	for _, item := range artifacts.Description.Actors {
		refs = append(refs, "actor:"+item.ID)
	}
	for _, item := range artifacts.Description.Rules {
		refs = append(refs, "rule:"+item.ID)
	}
	return refs
}
