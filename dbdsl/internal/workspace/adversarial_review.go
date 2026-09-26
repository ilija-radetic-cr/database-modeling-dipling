package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"dbdsl/internal/dsl"
	"dbdsl/internal/jobs"
	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

const adversarialReviewHistoryVersion = 1

type AdversarialReviewRecord struct {
	ReviewID              string                           `json:"review_id"`
	Scope                 string                           `json:"scope"`
	ReviewerPolicyVersion string                           `json:"reviewer_policy_version,omitempty"`
	CandidateRevision     int                              `json:"candidate_revision"`
	CandidateHash         string                           `json:"candidate_hash"`
	SourceHash            string                           `json:"source_hash"`
	Summary               string                           `json:"summary"`
	Findings              []llmpipeline.AdversarialFinding `json:"findings"`
	CreatedAt             string                           `json:"created_at"`
}

type AdversarialReviewDecision struct {
	ReviewID        string `json:"review_id"`
	FindingID       string `json:"finding_id"`
	Decision        string `json:"decision"`
	Note            string `json:"note"`
	Actor           string `json:"actor"`
	DecidedAt       string `json:"decided_at"`
	ProjectRevision int    `json:"project_revision"`
}

// AdversarialOperatorFinding preserves an explicitly authored finding without
// rewriting the immutable LLM review that it supplements.
type AdversarialOperatorFinding struct {
	ReviewID        string                         `json:"review_id"`
	CandidateHash   string                         `json:"candidate_hash"`
	SourceHash      string                         `json:"source_hash"`
	Finding         llmpipeline.AdversarialFinding `json:"finding"`
	Actor           string                         `json:"actor"`
	Note            string                         `json:"note"`
	CreatedAt       string                         `json:"created_at"`
	ProjectRevision int                            `json:"project_revision"`
}

type AdversarialReviewAuditEvent struct {
	EventID             string   `json:"event_id"`
	ReviewID            string   `json:"review_id"`
	FindingID           string   `json:"finding_id,omitempty"`
	Action              string   `json:"action"`
	Actor               string   `json:"actor"`
	Note                string   `json:"note,omitempty"`
	CandidateHash       string   `json:"candidate_hash,omitempty"`
	SourceHash          string   `json:"source_hash,omitempty"`
	BeforeCandidateHash string   `json:"before_candidate_hash,omitempty"`
	AfterCandidateHash  string   `json:"after_candidate_hash,omitempty"`
	BeforeModelSummary  string   `json:"before_model_summary,omitempty"`
	AfterModelSummary   string   `json:"after_model_summary,omitempty"`
	Reason              string   `json:"reason,omitempty"`
	Result              string   `json:"result,omitempty"`
	Changes             []string `json:"changes,omitempty"`
	CreatedAt           string   `json:"created_at"`
	ProjectRevision     int      `json:"project_revision"`
}

// AdversarialCandidateSnapshot keeps the exact description a review, decision
// or correction applied to. Histories written before the review moved to the
// description also kept the derived model, which was part of their hash.
type AdversarialCandidateSnapshot struct {
	CandidateRevision int                                  `json:"candidate_revision"`
	CandidateHash     string                               `json:"candidate_hash"`
	SourceHash        string                               `json:"source_hash"`
	Reason            string                               `json:"reason"`
	Description       llmpipeline.ConceptualDescription    `json:"conceptual_description"`
	LegacyModel       *llmpipeline.ConceptualModelProposal `json:"conceptual_model,omitempty"`
	CreatedAt         string                               `json:"created_at"`
}

type AdversarialReviewHistory struct {
	Version          int                            `json:"version"`
	Reviews          []AdversarialReviewRecord      `json:"reviews"`
	OperatorFindings []AdversarialOperatorFinding   `json:"operator_findings"`
	Decisions        []AdversarialReviewDecision    `json:"decisions"`
	Audit            []AdversarialReviewAuditEvent  `json:"audit"`
	Snapshots        []AdversarialCandidateSnapshot `json:"snapshots,omitempty"`
}

type AdversarialFindingProvenance struct {
	FindingID       string `json:"finding_id"`
	ReviewID        string `json:"review_id"`
	Origin          string `json:"origin"`
	Actor           string `json:"actor"`
	Note            string `json:"note,omitempty"`
	CreatedAt       string `json:"created_at"`
	ProjectRevision int    `json:"project_revision,omitempty"`
}

type AdversarialReviewView struct {
	ProjectRevision   int                            `json:"project_revision"`
	Status            string                         `json:"status"`
	Review            *AdversarialReviewRecord       `json:"review"`
	FindingProvenance []AdversarialFindingProvenance `json:"finding_provenance"`
	Decisions         []AdversarialReviewDecision    `json:"decisions"`
	Audit             []AdversarialReviewAuditEvent  `json:"audit"`
	CanAccept         bool                           `json:"can_accept"`
	Accepted          bool                           `json:"accepted"`
}

type AdversarialReviewRunOptions struct {
	BaseRevision    int
	Scope           string
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	OnProgress      jobs.StepEmitter
}

type AdversarialReviewDecisionInput struct {
	FindingID string `json:"finding_id"`
	Decision  string `json:"decision"`
	Note      string `json:"note"`
}

type RecordAdversarialReviewDecisionsOptions struct {
	BaseRevision int
	Actor        string
	Decisions    []AdversarialReviewDecisionInput
}

type RecordAdversarialOperatorFindingOptions struct {
	BaseRevision int
	Actor        string
	Note         string
	Finding      llmpipeline.AdversarialFinding
}

type AdversarialCorrectionRequestOptions struct {
	BaseRevision int
	Actor        string
	Note         string
}

type AdversarialCorrectionRunOptions struct {
	BaseRevision    int
	Actor           string
	Note            string
	Model           string
	ReasoningEffort string
	MaxOutputTokens int
	OnProgress      jobs.StepEmitter
}

// adversarialCandidate is what the critic reviews: the conceptual description
// and the source units it cites. The conceptual model is derived from the
// description only when it is accepted.
type adversarialCandidate struct {
	SourceHash    string
	CandidateHash string
	Summary       string
	SourceUnits   []dsl.SourceUnit
	Description   llmpipeline.ConceptualDescription
}

func (s *Store) AdversarialReview(projectID string) (AdversarialReviewView, error) {
	project, ok := s.Project(projectID)
	if !ok {
		return AdversarialReviewView{}, ErrNotFound
	}
	history, err := s.loadAdversarialReviewHistory(project)
	if err != nil {
		return AdversarialReviewView{}, err
	}
	return s.adversarialReviewView(project, history), nil
}

func (s *Store) GenerateAdversarialReview(ctx context.Context, client llm.Client, projectID string, opts AdversarialReviewRunOptions) (int, []string, error) {
	if opts.BaseRevision <= 0 {
		return 0, nil, errors.New("base_revision is required for adversarial review")
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for adversarial review")
	}
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if project.CurrentRevision != opts.BaseRevision {
		return 0, nil, ErrRevisionConflict
	}
	if err := projectMutable(project); err != nil {
		return 0, nil, err
	}
	scope := strings.ToLower(strings.TrimSpace(opts.Scope))
	if scope == "" {
		scope = "conceptual"
	}
	if scope != "conceptual" {
		return 0, nil, errors.New("adversarial review scope must be conceptual")
	}
	snapshot, err := s.adversarialCandidate(project)
	if err != nil {
		return 0, nil, err
	}
	history, err := s.loadAdversarialReviewHistory(project)
	if err != nil {
		return 0, nil, err
	}
	controls := s.resolveLLMExecutionControls(projectID)
	reviewerPolicyVersion := adversarialReviewerPolicyVersion(controls.PromptVersion)
	if latest := latestAdversarialReview(history); latest != nil && latest.Scope == scope && latest.ReviewerPolicyVersion == reviewerPolicyVersion && latest.CandidateHash == snapshot.CandidateHash && latest.SourceHash == snapshot.SourceHash {
		return project.CurrentRevision, []string{}, nil
	}

	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "adversarial_review", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "adversarial_review", opts.MaxOutputTokens, stageBudgetInput{Units: len(snapshot.SourceUnits)})
	emit := stageEmitter(opts.OnProgress)
	emit("compare_candidate_to_sources", "Reviewing the conceptual candidate against its exact source units.", 20, map[string]any{"scope": scope, "candidate_hash": snapshot.CandidateHash})
	proposal, err := llmpipeline.RunAdversarialReview(ctx, client, llmpipeline.AdversarialReviewOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: snapshot.SourceUnits, Description: snapshot.Description,
		OperatorContext: adversarialOperatorContext(history, snapshot.SourceHash, snapshot.CandidateHash),
		Model:           opts.Model, ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		PromptVersion: reviewerPolicyVersion, RunKey: shortHash(snapshot.CandidateHash),
	})
	if err != nil {
		return 0, nil, err
	}
	emit("validate_adversarial_review", "Validating cited source units and review findings.", 72, map[string]any{"finding_count": len(proposal.Findings)})
	if err := validateAdversarialFindings(proposal.Findings, snapshot.SourceUnits); err != nil {
		return 0, nil, err
	}
	if err := validateEffectiveFindingIDs(history, snapshot, proposal.Findings); err != nil {
		return 0, nil, err
	}
	nextRevision := project.CurrentRevision + 1
	review := AdversarialReviewRecord{
		ReviewID: fmt.Sprintf("review_%s_%06d", shortHash(snapshot.CandidateHash), nextRevision), Scope: scope, ReviewerPolicyVersion: reviewerPolicyVersion,
		CandidateRevision: artifactRevision(project.ConceptualDescriptionPath, project.CurrentRevision), CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		Summary: strings.TrimSpace(proposal.Summary), Findings: append([]llmpipeline.AdversarialFinding{}, proposal.Findings...),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	history.Reviews = append(history.Reviews, review)
	appendAdversarialSnapshot(&history, AdversarialCandidateSnapshot{
		CandidateRevision: review.CandidateRevision, CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		Reason: "adversarial_review", Description: snapshot.Description,
	})
	appendAdversarialAudit(&history, AdversarialReviewAuditEvent{
		ReviewID: review.ReviewID, Action: "review_completed", Actor: "llm", CandidateHash: snapshot.CandidateHash,
		SourceHash: snapshot.SourceHash, BeforeCandidateHash: snapshot.CandidateHash, AfterCandidateHash: snapshot.CandidateHash,
		BeforeModelSummary: snapshot.Summary, AfterModelSummary: snapshot.Summary,
		Result: fmt.Sprintf("%d_findings", len(review.Findings)), ProjectRevision: nextRevision,
	})
	if err := validateAdversarialReviewHistory(history); err != nil {
		return 0, nil, err
	}
	revision, err := s.commitAnalysisRevision(projectID, opts.BaseRevision, map[string]artifactValue{
		"adversarial_review_history.json": {Value: history, JSON: true},
	}, func(current *ProjectState, paths map[string]string) error {
		current.AdversarialReviewPath = paths["adversarial_review_history.json"]
		current.LifecycleStatus = "conceptual_review"
		current.LastActivity = "Adversarial source review completed; every finding requires an explicit disposition."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	emit("write_adversarial_review", "Writing the revision-bound review and visible audit history.", 88, nil)
	return revision, []string{"adversarial_review"}, nil
}

func (s *Store) RecordAdversarialReviewDecisions(projectID string, opts RecordAdversarialReviewDecisionsOptions) (AdversarialReviewView, error) {
	if opts.BaseRevision <= 0 {
		return AdversarialReviewView{}, errors.New("base_revision is required for adversarial review decisions")
	}
	if err := validateReviewActor(opts.Actor); err != nil {
		return AdversarialReviewView{}, err
	}
	if len(opts.Decisions) == 0 {
		return AdversarialReviewView{}, errors.New("at least one adversarial review decision is required")
	}
	project, ok := s.Project(projectID)
	if !ok {
		return AdversarialReviewView{}, ErrNotFound
	}
	if project.CurrentRevision != opts.BaseRevision {
		return AdversarialReviewView{}, ErrRevisionConflict
	}
	if err := projectMutable(project); err != nil {
		return AdversarialReviewView{}, err
	}
	if project.ConceptualModelAcceptedPath != "" {
		// Later gates were passed on the dispositions that existed at acceptance;
		// changing one now would leave them standing on a decision that no longer
		// holds. A new proposal (regenerate or reopen) starts a new cycle.
		return AdversarialReviewView{}, errors.New("review decisions cannot change after conceptual model acceptance; regenerate the conceptual model or reopen the project to start a new review cycle")
	}
	history, err := s.loadAdversarialReviewHistory(project)
	if err != nil {
		return AdversarialReviewView{}, err
	}
	view := s.adversarialReviewView(project, history)
	if view.Status != "current" || view.Review == nil || view.Review.Scope != "conceptual" {
		return AdversarialReviewView{}, errors.New("the current conceptual proposal has no current adversarial review")
	}
	findingIDs := map[string]bool{}
	for _, finding := range view.Review.Findings {
		findingIDs[finding.ID] = true
	}
	decisionReviewIDs := effectiveFindingReviewIDs(history, *view.Review)
	latest := effectiveAdversarialDecisions(history, *view.Review)
	seen := map[string]bool{}
	for _, input := range opts.Decisions {
		input.FindingID = strings.TrimSpace(input.FindingID)
		input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
		input.Note = strings.TrimSpace(input.Note)
		if !findingIDs[input.FindingID] {
			return AdversarialReviewView{}, fmt.Errorf("unknown adversarial review finding %q", input.FindingID)
		}
		if seen[input.FindingID] {
			return AdversarialReviewView{}, fmt.Errorf("duplicate adversarial review finding %q", input.FindingID)
		}
		seen[input.FindingID] = true
		if input.Decision != "dismiss" && input.Decision != "waive" && input.Decision != "request_correction" {
			return AdversarialReviewView{}, errors.New("decision must be dismiss, waive, or request_correction")
		}
		if input.Note == "" {
			return AdversarialReviewView{}, fmt.Errorf("a note is required for finding %s", input.FindingID)
		}
		if previous, exists := latest[input.FindingID]; exists && previous.Decision == "request_correction" && input.Decision != "request_correction" {
			return AdversarialReviewView{}, fmt.Errorf("finding %s already awaits correction and re-review", input.FindingID)
		}
	}
	snapshot, err := s.adversarialCandidate(project)
	if err != nil {
		return AdversarialReviewView{}, err
	}
	appendAdversarialSnapshot(&history, AdversarialCandidateSnapshot{
		CandidateRevision: view.Review.CandidateRevision, CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		Reason: "operator_decision", Description: snapshot.Description,
	})

	nextRevision := project.CurrentRevision + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, input := range opts.Decisions {
		decision := AdversarialReviewDecision{
			ReviewID: decisionReviewIDs[strings.TrimSpace(input.FindingID)], FindingID: strings.TrimSpace(input.FindingID), Decision: strings.ToLower(strings.TrimSpace(input.Decision)),
			Note: strings.TrimSpace(input.Note), Actor: opts.Actor, DecidedAt: now, ProjectRevision: nextRevision,
		}
		history.Decisions = append(history.Decisions, decision)
		appendAdversarialAudit(&history, AdversarialReviewAuditEvent{
			ReviewID: decision.ReviewID, FindingID: decision.FindingID, Action: "finding_decided", Actor: decision.Actor,
			Note: decision.Note, Reason: decision.Note, Result: decision.Decision, CandidateHash: view.Review.CandidateHash,
			SourceHash: view.Review.SourceHash, BeforeCandidateHash: view.Review.CandidateHash, AfterCandidateHash: view.Review.CandidateHash,
			ProjectRevision: nextRevision,
		})
	}
	if err := validateAdversarialReviewHistory(history); err != nil {
		return AdversarialReviewView{}, err
	}
	_, err = s.commitAnalysisRevision(projectID, opts.BaseRevision, map[string]artifactValue{
		"adversarial_review_history.json": {Value: history, JSON: true},
	}, func(current *ProjectState, paths map[string]string) error {
		current.AdversarialReviewPath = paths["adversarial_review_history.json"]
		current.LastActivity = "Adversarial review decisions recorded with attributed audit history."
		return nil
	})
	if err != nil {
		return AdversarialReviewView{}, err
	}
	return s.AdversarialReview(projectID)
}

func (s *Store) RecordAdversarialOperatorFinding(projectID string, opts RecordAdversarialOperatorFindingOptions) (AdversarialReviewView, error) {
	if opts.BaseRevision <= 0 {
		return AdversarialReviewView{}, errors.New("base_revision is required for an operator-authored finding")
	}
	if err := validateReviewActor(opts.Actor); err != nil {
		return AdversarialReviewView{}, err
	}
	opts.Note = strings.TrimSpace(opts.Note)
	if opts.Note == "" {
		return AdversarialReviewView{}, errors.New("a note is required for an operator-authored finding")
	}
	project, ok := s.Project(projectID)
	if !ok {
		return AdversarialReviewView{}, ErrNotFound
	}
	if project.CurrentRevision != opts.BaseRevision {
		return AdversarialReviewView{}, ErrRevisionConflict
	}
	if err := projectMutable(project); err != nil {
		return AdversarialReviewView{}, err
	}
	if project.ConceptualModelAcceptedPath != "" {
		return AdversarialReviewView{}, errors.New("operator findings cannot be added after conceptual model acceptance")
	}
	history, err := s.loadAdversarialReviewHistory(project)
	if err != nil {
		return AdversarialReviewView{}, err
	}
	view := s.adversarialReviewView(project, history)
	if view.Status != "current" || view.Review == nil || view.Review.Scope != "conceptual" {
		return AdversarialReviewView{}, errors.New("the current conceptual proposal has no current adversarial review")
	}
	snapshot, err := s.adversarialCandidate(project)
	if err != nil {
		return AdversarialReviewView{}, err
	}
	if snapshot.CandidateHash != view.Review.CandidateHash || snapshot.SourceHash != view.Review.SourceHash {
		return AdversarialReviewView{}, errors.New("the adversarial review no longer matches the current candidate and sources")
	}

	finding := normalizeAdversarialFinding(opts.Finding)
	// IDs are unique for a candidate and its sources across the whole history,
	// including reviews that a later policy change superseded.
	existingIDs := map[string]bool{}
	for _, existing := range view.Review.Findings {
		existingIDs[existing.ID] = true
	}
	for _, review := range history.Reviews {
		if review.CandidateHash == snapshot.CandidateHash && review.SourceHash == snapshot.SourceHash {
			for _, existing := range review.Findings {
				existingIDs[existing.ID] = true
			}
		}
	}
	for _, existing := range history.OperatorFindings {
		if existing.CandidateHash == snapshot.CandidateHash && existing.SourceHash == snapshot.SourceHash {
			existingIDs[existing.Finding.ID] = true
		}
	}
	if finding.ID == "" {
		finding.ID = nextOperatorFindingID(existingIDs)
	}
	if existingIDs[finding.ID] {
		return AdversarialReviewView{}, fmt.Errorf("duplicate adversarial review finding id %q", finding.ID)
	}
	proposal := llmpipeline.AdversarialReviewProposal{Summary: "Operator-authored source-grounded finding.", Findings: []llmpipeline.AdversarialFinding{finding}}
	if validationErrors := llmpipeline.ValidateAdversarialReviewProposal(proposal, snapshot.SourceUnits, snapshot.Description); len(validationErrors) > 0 {
		return AdversarialReviewView{}, errors.New(strings.Join(validationErrors, "; "))
	}

	nextRevision := project.CurrentRevision + 1
	now := time.Now().UTC().Format(time.RFC3339Nano)
	record := AdversarialOperatorFinding{
		ReviewID: view.Review.ReviewID, CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		Finding: finding, Actor: opts.Actor, Note: opts.Note, CreatedAt: now, ProjectRevision: nextRevision,
	}
	history.OperatorFindings = append(history.OperatorFindings, record)
	appendAdversarialSnapshot(&history, AdversarialCandidateSnapshot{
		CandidateRevision: view.Review.CandidateRevision, CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		Reason: "operator_finding_added", Description: snapshot.Description,
	})
	appendAdversarialAudit(&history, AdversarialReviewAuditEvent{
		ReviewID: view.Review.ReviewID, FindingID: finding.ID, Action: "operator_finding_added", Actor: opts.Actor,
		Note: opts.Note, Reason: opts.Note, Result: finding.Severity + ":" + finding.Category,
		CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		BeforeCandidateHash: snapshot.CandidateHash, AfterCandidateHash: snapshot.CandidateHash,
		BeforeModelSummary: snapshot.Summary, AfterModelSummary: snapshot.Summary, ProjectRevision: nextRevision,
	})
	// The history is checked as a whole before anything is written, so a
	// rejected finding never leaves an unreadable history behind.
	if err := validateAdversarialReviewHistory(history); err != nil {
		return AdversarialReviewView{}, err
	}
	_, err = s.commitAnalysisRevision(projectID, opts.BaseRevision, map[string]artifactValue{
		"adversarial_review_history.json": {Value: history, JSON: true},
	}, func(current *ProjectState, paths map[string]string) error {
		current.AdversarialReviewPath = paths["adversarial_review_history.json"]
		current.LifecycleStatus = "conceptual_review"
		current.LastActivity = "An explicit source-grounded finding was added to the adversarial review."
		return nil
	})
	if err != nil {
		return AdversarialReviewView{}, err
	}
	return s.AdversarialReview(projectID)
}

func (s *Store) RecordAdversarialCorrectionRequest(projectID string, opts AdversarialCorrectionRequestOptions) (int, error) {
	project, history, view, pending, err := s.adversarialCorrectionRequestState(projectID, opts)
	if err != nil {
		return 0, err
	}
	snapshot, err := s.adversarialCandidate(project)
	if err != nil {
		return 0, err
	}
	appendAdversarialSnapshot(&history, AdversarialCandidateSnapshot{
		CandidateRevision: view.Review.CandidateRevision, CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		Reason: "correction_requested", Description: snapshot.Description,
	})
	nextRevision := project.CurrentRevision + 1
	for _, findingID := range pending {
		appendAdversarialAudit(&history, AdversarialReviewAuditEvent{
			ReviewID: view.Review.ReviewID, FindingID: findingID, Action: "correction_requested", Actor: opts.Actor,
			Note: strings.TrimSpace(opts.Note), Reason: strings.TrimSpace(opts.Note), Result: "queued", CandidateHash: view.Review.CandidateHash, SourceHash: view.Review.SourceHash,
			BeforeCandidateHash: view.Review.CandidateHash, ProjectRevision: nextRevision,
		})
	}
	if err := validateAdversarialReviewHistory(history); err != nil {
		return 0, err
	}
	revision, err := s.commitAnalysisRevision(projectID, opts.BaseRevision, map[string]artifactValue{
		"adversarial_review_history.json": {Value: history, JSON: true},
	}, func(current *ProjectState, paths map[string]string) error {
		current.AdversarialReviewPath = paths["adversarial_review_history.json"]
		current.LastActivity = "An explicit conceptual correction was requested."
		return nil
	})
	if err != nil {
		return 0, err
	}
	return revision, nil
}

func (s *Store) ValidateAdversarialCorrectionRequest(projectID string, opts AdversarialCorrectionRequestOptions) error {
	_, _, _, _, err := s.adversarialCorrectionRequestState(projectID, opts)
	return err
}

func (s *Store) adversarialCorrectionRequestState(projectID string, opts AdversarialCorrectionRequestOptions) (*ProjectState, AdversarialReviewHistory, AdversarialReviewView, []string, error) {
	if opts.BaseRevision <= 0 {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, errors.New("base_revision is required for conceptual correction")
	}
	if err := validateReviewActor(opts.Actor); err != nil {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, err
	}
	opts.Note = strings.TrimSpace(opts.Note)
	if opts.Note == "" {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, errors.New("a correction note is required")
	}
	project, ok := s.Project(projectID)
	if !ok {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, ErrNotFound
	}
	if project.CurrentRevision != opts.BaseRevision {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, ErrRevisionConflict
	}
	if err := projectMutable(project); err != nil {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, err
	}
	history, err := s.loadAdversarialReviewHistory(project)
	if err != nil {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, err
	}
	view := s.adversarialReviewView(project, history)
	if view.Status != "current" || view.Review == nil || view.Review.Scope != "conceptual" {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, errors.New("the current conceptual proposal has no current adversarial review")
	}
	pending := correctionFindingIDs(history, *view.Review)
	if len(pending) == 0 {
		return nil, AdversarialReviewHistory{}, AdversarialReviewView{}, nil, errors.New("no current finding has a request_correction decision")
	}
	return project, history, view, pending, nil
}

func (s *Store) ApplyAdversarialCorrection(ctx context.Context, client llm.Client, projectID string, opts AdversarialCorrectionRunOptions) (int, []string, error) {
	if opts.BaseRevision <= 0 {
		return 0, nil, errors.New("base_revision is required for conceptual correction")
	}
	if err := validateReviewActor(opts.Actor); err != nil {
		return 0, nil, err
	}
	if strings.TrimSpace(opts.Note) == "" {
		return 0, nil, errors.New("a correction note is required")
	}
	if client == nil {
		return 0, nil, errors.New("LLM client is required for conceptual correction")
	}
	project, ok := s.Project(projectID)
	if !ok {
		return 0, nil, ErrNotFound
	}
	if project.CurrentRevision != opts.BaseRevision {
		return 0, nil, ErrRevisionConflict
	}
	if err := projectMutable(project); err != nil {
		return 0, nil, err
	}
	history, err := s.loadAdversarialReviewHistory(project)
	if err != nil {
		return 0, nil, err
	}
	view := s.adversarialReviewView(project, history)
	if view.Status != "current" || view.Review == nil || view.Review.Scope != "conceptual" {
		return 0, nil, errors.New("the current conceptual proposal has no current adversarial review")
	}
	snapshot, err := s.adversarialCandidate(project)
	if err != nil {
		return 0, nil, err
	}
	appendAdversarialSnapshot(&history, AdversarialCandidateSnapshot{
		CandidateRevision: view.Review.CandidateRevision, CandidateHash: snapshot.CandidateHash, SourceHash: snapshot.SourceHash,
		Reason: "before_conceptual_correction", Description: snapshot.Description,
	})
	pendingIDs := correctionFindingIDs(history, *view.Review)
	if len(pendingIDs) == 0 {
		return 0, nil, errors.New("no current finding has a request_correction decision")
	}
	findingByID := map[string]llmpipeline.AdversarialFinding{}
	for _, finding := range view.Review.Findings {
		findingByID[finding.ID] = finding
	}
	decisionByID := effectiveAdversarialDecisions(history, *view.Review)
	findings := make([]llmpipeline.AdversarialFinding, 0, len(pendingIDs))
	correctionDecisions := make([]llmpipeline.ConceptualCorrectionDecision, 0, len(pendingIDs))
	for _, id := range pendingIDs {
		findings = append(findings, findingByID[id])
		correctionDecisions = append(correctionDecisions, llmpipeline.ConceptualCorrectionDecision{FindingID: id, Decision: "apply", Note: decisionByID[id].Note})
	}
	opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens = s.resolveLLMOptions(projectID, "conceptual_correction", opts.Model, opts.ReasoningEffort, opts.MaxOutputTokens)
	opts.MaxOutputTokens = s.resolveStageBudget(projectID, "conceptual_correction", opts.MaxOutputTokens, stageBudgetInput{Units: len(snapshot.SourceUnits)})
	controls := s.resolveLLMExecutionControls(projectID)
	emit := stageEmitter(opts.OnProgress)
	emit("propose_conceptual_correction", "Applying only the explicitly requested, source-grounded corrections.", 18, map[string]any{"finding_count": len(findings)})
	description, descriptionQA, err := llmpipeline.RunConceptualCorrection(ctx, client, llmpipeline.ConceptualCorrectionOptions{
		OutDir: s.projectWorkspaceDir(projectID), SourceUnits: snapshot.SourceUnits, Current: snapshot.Description,
		Findings: findings, Decisions: correctionDecisions, Feedback: strings.TrimSpace(opts.Note), Model: opts.Model,
		ReasoningEffort: opts.ReasoningEffort, MaxOutputTokens: opts.MaxOutputTokens,
		PromptVersion: controls.PromptVersion, RunKey: shortHash(snapshot.CandidateHash),
	})
	if err != nil {
		return 0, nil, err
	}
	qa := conceptualDescriptionQA(description, descriptionQA, snapshot.SourceUnits)
	if !qa.OK {
		return 0, nil, fmt.Errorf("corrected conceptual description failed validation: %s", strings.Join(qa.Errors, "; "))
	}
	afterHash, err := conceptualCandidateHash(description, nil)
	if err != nil {
		return 0, nil, err
	}
	nextRevision := project.CurrentRevision + 1
	changes := conceptualChangedPaths(snapshot.Description, description)
	appendAdversarialSnapshot(&history, AdversarialCandidateSnapshot{
		CandidateRevision: nextRevision, CandidateHash: afterHash, SourceHash: snapshot.SourceHash,
		Reason: "conceptual_correction", Description: description,
	})
	appendAdversarialAudit(&history, AdversarialReviewAuditEvent{
		ReviewID: view.Review.ReviewID, Action: "correction_proposed", Actor: "llm", Note: strings.TrimSpace(opts.Note),
		Reason: strings.TrimSpace(opts.Note), Result: "new_proposal_requires_review: " + strings.Join(changes, ", "), Changes: changes,
		CandidateHash: afterHash, SourceHash: snapshot.SourceHash,
		BeforeCandidateHash: snapshot.CandidateHash, AfterCandidateHash: afterHash,
		BeforeModelSummary: snapshot.Summary, AfterModelSummary: descriptionSummary(description), ProjectRevision: nextRevision,
	})
	emit("validate_conceptual_correction", "Validating the corrected description.", 70, coverageMetadata(qa.Coverage))
	if err := validateAdversarialReviewHistory(history); err != nil {
		return 0, nil, err
	}
	revision, err := s.commitAnalysisRevision(projectID, opts.BaseRevision, map[string]artifactValue{
		"conceptual_description.json":     {Value: description, JSON: true},
		"conceptual_model_qa.json":        {Value: qa, JSON: true},
		"adversarial_review_history.json": {Value: history, JSON: true},
	}, func(current *ProjectState, paths map[string]string) error {
		invalidateLogicalArtifacts(current)
		current.ConceptualDescriptionPath = paths["conceptual_description.json"]
		current.ConceptualModelAcceptedPath = ""
		current.ConceptualModelQAPath = paths["conceptual_model_qa.json"]
		current.AdversarialReviewPath = paths["adversarial_review_history.json"]
		current.LifecycleStatus = "conceptual_review"
		current.LastActivity = "A corrected conceptual description was generated; a fresh adversarial review and explicit acceptance are required."
		return nil
	})
	if err != nil {
		return 0, nil, err
	}
	emit("write_conceptual_correction", "Writing the corrected description without accepting it.", 88, nil)
	return revision, []string{"conceptual_model", "adversarial_review"}, nil
}

func (s *Store) adversarialCandidate(project *ProjectState) (adversarialCandidate, error) {
	var out adversarialCandidate
	if project.SourceUnitsPath == "" || project.ConceptualDescriptionPath == "" {
		return out, errors.New("source units and a conceptual description are required for adversarial review")
	}
	artifacts, err := s.SourceUnitArtifacts(project.ID)
	if err != nil {
		return out, err
	}
	out.SourceUnits = artifacts.Accepted.SourceUnits
	sourceBytes, err := os.ReadFile(s.absoluteWorkspacePath(project.SourceUnitsPath))
	if err != nil {
		return out, err
	}
	descriptionBytes, err := os.ReadFile(s.absoluteWorkspacePath(project.ConceptualDescriptionPath))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(descriptionBytes, &out.Description); err != nil {
		return out, err
	}
	out.SourceHash = sha256Hash(sourceBytes)
	out.CandidateHash, err = combinedCandidateHash(sha256Hash(descriptionBytes), "")
	out.Summary = descriptionSummary(out.Description)
	return out, err
}

func (s *Store) loadAdversarialReviewHistory(project *ProjectState) (AdversarialReviewHistory, error) {
	history := AdversarialReviewHistory{Version: adversarialReviewHistoryVersion, Reviews: []AdversarialReviewRecord{}, OperatorFindings: []AdversarialOperatorFinding{}, Decisions: []AdversarialReviewDecision{}, Audit: []AdversarialReviewAuditEvent{}, Snapshots: []AdversarialCandidateSnapshot{}}
	if project == nil || strings.TrimSpace(project.AdversarialReviewPath) == "" {
		return history, nil
	}
	if err := readJSON(s.absoluteWorkspacePath(project.AdversarialReviewPath), &history); err != nil {
		return history, err
	}
	if history.Version == 0 {
		history.Version = adversarialReviewHistoryVersion
	}
	if history.Reviews == nil {
		history.Reviews = []AdversarialReviewRecord{}
	}
	if history.OperatorFindings == nil {
		history.OperatorFindings = []AdversarialOperatorFinding{}
	}
	if history.Decisions == nil {
		history.Decisions = []AdversarialReviewDecision{}
	}
	if history.Audit == nil {
		history.Audit = []AdversarialReviewAuditEvent{}
	}
	if history.Snapshots == nil {
		history.Snapshots = []AdversarialCandidateSnapshot{}
	}
	if err := validateAdversarialReviewHistory(history); err != nil {
		return history, err
	}
	return history, nil
}

func (s *Store) adversarialReviewView(project *ProjectState, history AdversarialReviewHistory) AdversarialReviewView {
	view := AdversarialReviewView{
		ProjectRevision: project.CurrentRevision, Status: "not_run", Review: nil,
		FindingProvenance: []AdversarialFindingProvenance{},
		Decisions:         append([]AdversarialReviewDecision{}, history.Decisions...),
		Audit:             append([]AdversarialReviewAuditEvent{}, history.Audit...),
		// CanAccept describes whether acceptance is possible now, which needs a
		// current, resolved review; an earlier acceptance is shown separately.
		CanAccept: false,
		Accepted:  project.ConceptualModelAcceptedPath != "",
	}
	latest := latestAdversarialReview(history)
	if latest == nil {
		return view
	}
	review := *latest
	review.Findings = append([]llmpipeline.AdversarialFinding{}, latest.Findings...)
	reviewRevision := 0
	for _, event := range history.Audit {
		if event.ReviewID == review.ReviewID && event.Action == "review_completed" {
			reviewRevision = event.ProjectRevision
			break
		}
	}
	for _, finding := range latest.Findings {
		view.FindingProvenance = append(view.FindingProvenance, AdversarialFindingProvenance{
			FindingID: finding.ID, ReviewID: latest.ReviewID, Origin: "llm", Actor: "llm", CreatedAt: latest.CreatedAt, ProjectRevision: reviewRevision,
		})
	}
	for _, operatorFinding := range effectiveOperatorFindings(history, review) {
		review.Findings = append(review.Findings, operatorFinding.Finding)
		view.FindingProvenance = append(view.FindingProvenance, AdversarialFindingProvenance{
			FindingID: operatorFinding.Finding.ID, ReviewID: operatorFinding.ReviewID, Origin: "operator", Actor: operatorFinding.Actor, Note: operatorFinding.Note,
			CreatedAt: operatorFinding.CreatedAt, ProjectRevision: operatorFinding.ProjectRevision,
		})
	}
	view.Review = &review
	view.Status = "stale"
	currentPolicyVersion := adversarialReviewerPolicyVersion(s.resolveLLMExecutionControls(project.ID).PromptVersion)
	if review.ReviewerPolicyVersion != currentPolicyVersion {
		return view
	}
	snapshot, err := s.adversarialCandidate(project)
	if err != nil || snapshot.CandidateHash != review.CandidateHash || snapshot.SourceHash != review.SourceHash {
		return view
	}
	view.Status = "current"
	view.CanAccept = review.Scope == "conceptual" && adversarialReviewResolved(history, review)
	return view
}

func latestAdversarialReview(history AdversarialReviewHistory) *AdversarialReviewRecord {
	if len(history.Reviews) == 0 {
		return nil
	}
	return &history.Reviews[len(history.Reviews)-1]
}

func latestAdversarialDecisions(history AdversarialReviewHistory, reviewID string) map[string]AdversarialReviewDecision {
	out := map[string]AdversarialReviewDecision{}
	for _, decision := range history.Decisions {
		if decision.ReviewID == reviewID {
			out[decision.FindingID] = decision
		}
	}
	return out
}

func effectiveOperatorFindings(history AdversarialReviewHistory, review AdversarialReviewRecord) []AdversarialOperatorFinding {
	out := []AdversarialOperatorFinding{}
	for _, finding := range history.OperatorFindings {
		if finding.CandidateHash == review.CandidateHash && finding.SourceHash == review.SourceHash {
			out = append(out, finding)
		}
	}
	return out
}

// effectiveFindingReviewIDs preserves the immutable review binding of an
// operator finding when a policy-only re-review produces a newer LLM record
// for the exact same candidate and sources.
func effectiveFindingReviewIDs(history AdversarialReviewHistory, review AdversarialReviewRecord) map[string]string {
	out := map[string]string{}
	for _, storedReview := range history.Reviews {
		if storedReview.ReviewID != review.ReviewID {
			continue
		}
		for _, finding := range storedReview.Findings {
			out[finding.ID] = storedReview.ReviewID
		}
		break
	}
	for _, finding := range effectiveOperatorFindings(history, review) {
		out[finding.Finding.ID] = finding.ReviewID
	}
	return out
}

func effectiveAdversarialDecisions(history AdversarialReviewHistory, review AdversarialReviewRecord) map[string]AdversarialReviewDecision {
	bindings := effectiveFindingReviewIDs(history, review)
	out := map[string]AdversarialReviewDecision{}
	for _, decision := range history.Decisions {
		if bindings[decision.FindingID] == decision.ReviewID {
			out[decision.FindingID] = decision
		}
	}
	return out
}

func adversarialReviewResolved(history AdversarialReviewHistory, review AdversarialReviewRecord) bool {
	latest := effectiveAdversarialDecisions(history, review)
	for _, finding := range review.Findings {
		decision, ok := latest[finding.ID]
		if !ok || (decision.Decision != "dismiss" && decision.Decision != "waive") {
			return false
		}
	}
	return true
}

func correctionFindingIDs(history AdversarialReviewHistory, review AdversarialReviewRecord) []string {
	latest := effectiveAdversarialDecisions(history, review)
	out := []string{}
	for _, finding := range review.Findings {
		if latest[finding.ID].Decision == "request_correction" {
			out = append(out, finding.ID)
		}
	}
	return out
}

func appendAdversarialAudit(history *AdversarialReviewHistory, event AdversarialReviewAuditEvent) {
	event.EventID = fmt.Sprintf("audit_%06d", len(history.Audit)+1)
	if event.CreatedAt == "" {
		event.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	history.Audit = append(history.Audit, event)
}

func appendAdversarialSnapshot(history *AdversarialReviewHistory, snapshot AdversarialCandidateSnapshot) {
	for _, existing := range history.Snapshots {
		if existing.CandidateHash == snapshot.CandidateHash && existing.SourceHash == snapshot.SourceHash {
			return
		}
	}
	if snapshot.CreatedAt == "" {
		snapshot.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	history.Snapshots = append(history.Snapshots, snapshot)
}

func conceptualChangedPaths(beforeDescription, afterDescription llmpipeline.ConceptualDescription) []string {
	changes := []string{}
	appendJSONChangedPaths(&changes, "conceptual_description", jsonValue(beforeDescription), jsonValue(afterDescription))
	changes = uniqueSorted(changes)
	if len(changes) > 100 {
		remainder := len(changes) - 100
		changes = append(changes[:100], fmt.Sprintf("... %d additional changed paths", remainder))
	}
	return changes
}

func jsonValue(value any) any {
	data, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

func appendJSONChangedPaths(out *[]string, path string, before, after any) {
	if reflect.DeepEqual(before, after) {
		return
	}
	beforeMap, beforeIsMap := before.(map[string]any)
	afterMap, afterIsMap := after.(map[string]any)
	if beforeIsMap && afterIsMap {
		keys := map[string]bool{}
		for key := range beforeMap {
			keys[key] = true
		}
		for key := range afterMap {
			keys[key] = true
		}
		sorted := make([]string, 0, len(keys))
		for key := range keys {
			sorted = append(sorted, key)
		}
		sort.Strings(sorted)
		for _, key := range sorted {
			appendJSONChangedPaths(out, path+"."+key, beforeMap[key], afterMap[key])
		}
		return
	}
	beforeSlice, beforeIsSlice := before.([]any)
	afterSlice, afterIsSlice := after.([]any)
	if beforeIsSlice && afterIsSlice {
		beforeByID, beforeHasIDs := jsonSliceByID(beforeSlice)
		afterByID, afterHasIDs := jsonSliceByID(afterSlice)
		if beforeHasIDs && afterHasIDs {
			keys := map[string]bool{}
			for key := range beforeByID {
				keys[key] = true
			}
			for key := range afterByID {
				keys[key] = true
			}
			sorted := make([]string, 0, len(keys))
			for key := range keys {
				sorted = append(sorted, key)
			}
			sort.Strings(sorted)
			for _, key := range sorted {
				appendJSONChangedPaths(out, path+"[id="+key+"]", beforeByID[key], afterByID[key])
			}
			return
		}
		maxLen := len(beforeSlice)
		if len(afterSlice) > maxLen {
			maxLen = len(afterSlice)
		}
		for i := 0; i < maxLen; i++ {
			var beforeItem, afterItem any
			if i < len(beforeSlice) {
				beforeItem = beforeSlice[i]
			}
			if i < len(afterSlice) {
				afterItem = afterSlice[i]
			}
			appendJSONChangedPaths(out, fmt.Sprintf("%s[%d]", path, i), beforeItem, afterItem)
		}
		return
	}
	*out = append(*out, path)
}

func jsonSliceByID(items []any) (map[string]any, bool) {
	out := map[string]any{}
	if len(items) == 0 {
		return out, false
	}
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, false
		}
		id, ok := object["id"].(string)
		if !ok || id == "" || out[id] != nil {
			return nil, false
		}
		out[id] = item
	}
	return out, true
}

func adversarialOperatorContext(history AdversarialReviewHistory, sourceHash, candidateHash string) []llmpipeline.AdversarialOperatorContext {
	out := []llmpipeline.AdversarialOperatorContext{}
	reviews := map[string]AdversarialReviewRecord{}
	for _, review := range history.Reviews {
		reviews[review.ReviewID] = review
	}
	for _, finding := range history.OperatorFindings {
		if finding.SourceHash != sourceHash || finding.CandidateHash != candidateHash {
			continue
		}
		note := strings.Join([]string{
			finding.Note,
			"Claim: " + finding.Finding.Claim,
			"Expected: " + finding.Finding.Expected,
			"Actual: " + finding.Finding.Actual,
			"Suggested correction: " + finding.Finding.SuggestedCorrection,
		}, " ")
		out = append(out, llmpipeline.AdversarialOperatorContext{
			ID:    finding.ReviewID + ":operator_finding:" + finding.Finding.ID + ":" + finding.CandidateHash + ":" + finding.SourceHash,
			Actor: finding.Actor, Action: "operator_finding_added:" + finding.Finding.Severity + ":" + finding.Finding.Category,
			FindingID: finding.Finding.ID, DescriptionRefs: finding.Finding.DescriptionRefs, Note: note,
		})
	}
	// A decision carries the claim and references of the finding it decided, so
	// a re-review can tell which of its own findings were already settled.
	decided := map[string]llmpipeline.AdversarialFinding{}
	for _, review := range history.Reviews {
		for _, finding := range review.Findings {
			decided[review.ReviewID+":"+finding.ID] = finding
		}
	}
	for _, finding := range history.OperatorFindings {
		decided[finding.ReviewID+":"+finding.Finding.ID] = finding.Finding
	}
	for _, event := range history.Audit {
		if event.Actor != "human" && event.Actor != "test_operator" {
			continue
		}
		if event.Action != "finding_decided" && event.Action != "correction_requested" {
			continue
		}
		review, ok := reviews[event.ReviewID]
		if !ok || review.SourceHash != sourceHash {
			continue
		}
		finding := decided[event.ReviewID+":"+event.FindingID]
		out = append(out, llmpipeline.AdversarialOperatorContext{
			ID:    review.ReviewID + ":" + event.EventID + ":" + review.CandidateHash + ":" + review.SourceHash,
			Actor: event.Actor, Action: event.Action + ":" + event.Result,
			FindingID: event.FindingID, Claim: finding.Claim, DescriptionRefs: finding.DescriptionRefs, Note: event.Note,
		})
	}
	return out
}

func artifactRevision(artifactPath string, fallback int) int {
	for _, part := range strings.Split(filepath.ToSlash(artifactPath), "/") {
		if !strings.HasPrefix(part, "rev_") {
			continue
		}
		value, err := strconv.Atoi(strings.TrimPrefix(part, "rev_"))
		if err == nil && value > 0 {
			return value
		}
	}
	return fallback
}

func validateReviewActor(actor string) error {
	if actor != "human" && actor != "test_operator" {
		return errors.New("actor must be human or test_operator")
	}
	return nil
}

func normalizeAdversarialFinding(finding llmpipeline.AdversarialFinding) llmpipeline.AdversarialFinding {
	finding.ID = strings.TrimSpace(finding.ID)
	finding.Severity = strings.ToLower(strings.TrimSpace(finding.Severity))
	finding.Category = strings.ToLower(strings.TrimSpace(finding.Category))
	for i := range finding.SourceUnitIDs {
		finding.SourceUnitIDs[i] = strings.TrimSpace(finding.SourceUnitIDs[i])
	}
	for i := range finding.DescriptionRefs {
		finding.DescriptionRefs[i] = strings.TrimSpace(finding.DescriptionRefs[i])
	}
	finding.Claim = strings.TrimSpace(finding.Claim)
	finding.Expected = strings.TrimSpace(finding.Expected)
	finding.Actual = strings.TrimSpace(finding.Actual)
	finding.SuggestedCorrection = strings.TrimSpace(finding.SuggestedCorrection)
	return finding
}

func nextOperatorFindingID(existing map[string]bool) string {
	for number := 1; ; number++ {
		candidate := fmt.Sprintf("OF-%03d", number)
		if !existing[candidate] {
			return candidate
		}
	}
}

func validateAdversarialReviewHistory(history AdversarialReviewHistory) error {
	if history.Version != adversarialReviewHistoryVersion {
		return fmt.Errorf("unsupported adversarial review history version %d", history.Version)
	}
	reviews := map[string]map[string]bool{}
	reviewRecords := map[string]AdversarialReviewRecord{}
	reviewFindingsByBinding := map[string]map[string]bool{}
	for _, review := range history.Reviews {
		if strings.TrimSpace(review.ReviewID) == "" || reviews[review.ReviewID] != nil {
			return fmt.Errorf("invalid or duplicate adversarial review id %q", review.ReviewID)
		}
		if review.Scope != "conceptual" || review.CandidateRevision <= 0 || review.CandidateHash == "" || review.SourceHash == "" || review.CreatedAt == "" {
			return fmt.Errorf("adversarial review %s has invalid revision binding", review.ReviewID)
		}
		findingIDs := map[string]bool{}
		for _, finding := range review.Findings {
			if strings.TrimSpace(finding.ID) == "" || findingIDs[finding.ID] || len(finding.SourceUnitIDs) == 0 || strings.TrimSpace(finding.SourceQuote) == "" {
				return fmt.Errorf("adversarial review %s has an invalid finding", review.ReviewID)
			}
			findingIDs[finding.ID] = true
		}
		reviews[review.ReviewID] = findingIDs
		reviewRecords[review.ReviewID] = review
		binding := review.CandidateHash + ":" + review.SourceHash
		if reviewFindingsByBinding[binding] == nil {
			reviewFindingsByBinding[binding] = map[string]bool{}
		}
		for findingID := range findingIDs {
			reviewFindingsByBinding[binding][findingID] = true
		}
	}
	operatorRecords := map[string]AdversarialOperatorFinding{}
	operatorBindings := map[string]bool{}
	for _, operatorFinding := range history.OperatorFindings {
		review, ok := reviewRecords[operatorFinding.ReviewID]
		if !ok || operatorFinding.CandidateHash != review.CandidateHash || operatorFinding.SourceHash != review.SourceHash {
			return errors.New("operator finding references an unknown or mismatched adversarial review")
		}
		if err := validateReviewActor(operatorFinding.Actor); err != nil {
			return err
		}
		finding := operatorFinding.Finding
		if strings.TrimSpace(finding.ID) == "" || reviews[operatorFinding.ReviewID][finding.ID] {
			return fmt.Errorf("operator finding has an empty or duplicate id %q", finding.ID)
		}
		binding := operatorFinding.CandidateHash + ":" + operatorFinding.SourceHash + ":" + finding.ID
		if reviewFindingsByBinding[operatorFinding.CandidateHash+":"+operatorFinding.SourceHash][finding.ID] || operatorBindings[binding] {
			return fmt.Errorf("operator finding id %q is not unique for its candidate and sources", finding.ID)
		}
		if len(finding.SourceUnitIDs) == 0 || strings.TrimSpace(finding.SourceQuote) == "" || strings.TrimSpace(finding.Claim) == "" || strings.TrimSpace(finding.Expected) == "" || strings.TrimSpace(finding.Actual) == "" || strings.TrimSpace(finding.SuggestedCorrection) == "" {
			return fmt.Errorf("operator finding %s is missing required grounded content", finding.ID)
		}
		if strings.TrimSpace(operatorFinding.Note) == "" || operatorFinding.CreatedAt == "" || operatorFinding.ProjectRevision <= 0 {
			return fmt.Errorf("operator finding %s is missing immutable provenance", finding.ID)
		}
		reviews[operatorFinding.ReviewID][finding.ID] = true
		key := operatorFinding.ReviewID + ":" + finding.ID
		if _, exists := operatorRecords[key]; exists {
			return fmt.Errorf("duplicate operator finding history record %s", key)
		}
		operatorRecords[key] = operatorFinding
		operatorBindings[binding] = true
	}
	for _, decision := range history.Decisions {
		findings, ok := reviews[decision.ReviewID]
		if !ok || !findings[decision.FindingID] {
			return fmt.Errorf("adversarial decision references an unknown review or finding")
		}
		if err := validateReviewActor(decision.Actor); err != nil {
			return err
		}
		if decision.Decision != "dismiss" && decision.Decision != "waive" && decision.Decision != "request_correction" {
			return fmt.Errorf("adversarial decision has invalid disposition %q", decision.Decision)
		}
		if strings.TrimSpace(decision.Note) == "" || decision.DecidedAt == "" || decision.ProjectRevision <= 0 {
			return errors.New("adversarial decision is missing its immutable audit fields")
		}
	}
	events := map[string]bool{}
	operatorAudit := map[string]bool{}
	for _, event := range history.Audit {
		if event.EventID == "" || events[event.EventID] || reviews[event.ReviewID] == nil || event.Action == "" || event.CreatedAt == "" || event.ProjectRevision <= 0 {
			return errors.New("adversarial audit contains an invalid event")
		}
		if event.Actor != "llm" {
			if err := validateReviewActor(event.Actor); err != nil {
				return err
			}
		}
		if event.Action == "operator_finding_added" {
			key := event.ReviewID + ":" + event.FindingID
			record, ok := operatorRecords[key]
			if !ok || event.Actor != record.Actor || event.Note != record.Note || event.ProjectRevision != record.ProjectRevision || event.CandidateHash != record.CandidateHash || event.SourceHash != record.SourceHash {
				return errors.New("operator finding audit does not match its immutable history record")
			}
			operatorAudit[key] = true
		}
		events[event.EventID] = true
	}
	for key := range operatorRecords {
		if !operatorAudit[key] {
			return fmt.Errorf("operator finding %s is missing its audit event", key)
		}
	}
	snapshots := map[string]bool{}
	for _, snapshot := range history.Snapshots {
		key := snapshot.CandidateHash + ":" + snapshot.SourceHash
		if snapshot.CandidateRevision <= 0 || snapshot.CandidateHash == "" || snapshot.SourceHash == "" || snapshot.Reason == "" || snapshot.CreatedAt == "" || snapshots[key] {
			return errors.New("adversarial audit contains an invalid candidate snapshot")
		}
		hash, err := conceptualCandidateHash(snapshot.Description, snapshot.LegacyModel)
		if err != nil || hash != snapshot.CandidateHash {
			return errors.New("adversarial candidate snapshot hash does not match its exact contents")
		}
		snapshots[key] = true
	}
	return nil
}

func validateAdversarialFindings(findings []llmpipeline.AdversarialFinding, units []dsl.SourceUnit) error {
	knownSources := map[string]bool{}
	for _, unit := range units {
		knownSources[unit.ID] = true
	}
	seen := map[string]bool{}
	for _, finding := range findings {
		if strings.TrimSpace(finding.ID) == "" || seen[finding.ID] {
			return fmt.Errorf("adversarial review returned an empty or duplicate finding id %q", finding.ID)
		}
		seen[finding.ID] = true
		for _, id := range finding.SourceUnitIDs {
			if !knownSources[id] {
				return fmt.Errorf("adversarial review finding %s references unknown source unit %s", finding.ID, id)
			}
		}
	}
	return nil
}

func validateEffectiveFindingIDs(history AdversarialReviewHistory, snapshot adversarialCandidate, findings []llmpipeline.AdversarialFinding) error {
	seen := map[string]bool{}
	for _, finding := range findings {
		seen[finding.ID] = true
	}
	for _, operatorFinding := range history.OperatorFindings {
		if operatorFinding.CandidateHash != snapshot.CandidateHash || operatorFinding.SourceHash != snapshot.SourceHash {
			continue
		}
		if seen[operatorFinding.Finding.ID] {
			return fmt.Errorf("adversarial review finding id %q collides with a preserved operator finding", operatorFinding.Finding.ID)
		}
		seen[operatorFinding.Finding.ID] = true
	}
	return nil
}

// combinedCandidateHash identifies a reviewed candidate: the description, and
// in histories written before the review moved to the description, also the
// model derived from it. The key set is unchanged, so those hashes still verify.
func combinedCandidateHash(descriptionHash, legacyModelHash string) (string, error) {
	data, err := json.Marshal(map[string]string{"scope": "conceptual", "description": descriptionHash, "conceptual_model": legacyModelHash, "logical_model": ""})
	if err != nil {
		return "", err
	}
	return sha256Hash(data), nil
}

// conceptualCandidateHash hashes the description as it is written to disk.
func conceptualCandidateHash(description llmpipeline.ConceptualDescription, legacyModel *llmpipeline.ConceptualModelProposal) (string, error) {
	descriptionBytes, err := json.MarshalIndent(description, "", "  ")
	if err != nil {
		return "", err
	}
	legacyModelHash := ""
	if legacyModel != nil {
		modelBytes, err := json.MarshalIndent(legacyModel, "", "  ")
		if err != nil {
			return "", err
		}
		legacyModelHash = sha256Hash(modelBytes)
	}
	return combinedCandidateHash(sha256Hash(descriptionBytes), legacyModelHash)
}

func descriptionSummary(description llmpipeline.ConceptualDescription) string {
	return fmt.Sprintf("%d things, %d rules, %d queries, %d open questions",
		len(description.Things), len(description.Rules), len(description.Queries), len(description.OpenQuestions))
}

func shortHash(value string) string {
	value = strings.TrimPrefix(value, "sha256:")
	if len(value) > 12 {
		return value[:12]
	}
	return value
}

func adversarialReviewerPolicyVersion(projectPromptVersion string) string {
	base := strings.TrimSpace(projectPromptVersion)
	if base == "" {
		base = llmpipeline.PromptTemplateVersion
	}
	if marker := strings.Index(base, "+adversarial_review_"); marker >= 0 {
		base = base[:marker]
	}
	return base + "+" + llmpipeline.AdversarialReviewPolicyVersion
}
