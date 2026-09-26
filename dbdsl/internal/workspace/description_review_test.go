package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dbdsl/internal/llm"
	"dbdsl/internal/llmpipeline"
)

// The critic reviews the description; the conceptual model is not an artifact
// until acceptance, when it is derived from exactly the reviewed description.
func TestConceptualModelIsDerivedFromReviewedDescriptionAtAcceptance(t *testing.T) {
	store, projectID, revision, _ := reviewedWithOperatorFinding(t)
	project, _ := store.Project(projectID)
	if project.ConceptualModelProposalPath != "" {
		t.Fatalf("a proposed conceptual model was written before review: %s", project.ConceptualModelProposalPath)
	}
	artifacts, err := store.ConceptualModel(projectID)
	if err != nil || artifacts.Description == nil || len(artifacts.Proposed.EntityConcepts) == 0 || artifacts.IsAccepted {
		t.Fatalf("the model is not derived for display: %+v err=%v", artifacts, err)
	}

	requests, err := filepath.Glob(filepath.Join(store.projectWorkspaceDir(projectID), "llm_runs", "*adversarial_review*", "request.json"))
	if err != nil || len(requests) == 0 {
		t.Fatalf("review request not recorded: %v", err)
	}
	var request llm.Request
	data, _ := os.ReadFile(requests[0])
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(request.Input, `"conceptual_model"`) || !strings.Contains(request.Input, `"conceptual_description"`) || !strings.Contains(request.Input, `"valid_description_refs"`) {
		t.Fatalf("the critic did not get the description alone: %s", request.Input)
	}

	acceptConceptual(t, store, projectID, revision)
	project, _ = store.Project(projectID)
	var accepted llmpipeline.ConceptualModelProposal
	if err := readJSON(store.absoluteWorkspacePath(project.ConceptualModelAcceptedPath), &accepted); err != nil {
		t.Fatal(err)
	}
	artifacts, _ = store.ConceptualModel(projectID)
	sources, _ := store.SourceUnitArtifacts(projectID)
	if !reflect.DeepEqual(accepted, llmpipeline.ConceptualDescriptionToModel(*artifacts.Description, sources.Accepted.SourceUnits)) {
		t.Fatal("the accepted model is not the derivation of the reviewed description")
	}
}

// A re-review learns what was decided: each decision carries the claim and
// references of the finding it settled.
func TestOperatorContextCarriesDecidedFindingContent(t *testing.T) {
	store, projectID, _, _ := reviewedWithOperatorFinding(t)
	project, _ := store.Project(projectID)
	history, err := store.loadAdversarialReviewHistory(project)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.adversarialCandidate(project)
	if err != nil {
		t.Fatal(err)
	}
	var decision *llmpipeline.AdversarialOperatorContext
	for _, item := range adversarialOperatorContext(history, candidate.SourceHash, candidate.CandidateHash) {
		if strings.HasPrefix(item.Action, "finding_decided") && item.FindingID == "OF-001" {
			decision = &item
		}
	}
	if decision == nil || decision.Claim != "A cited detail is absent." || decision.Note == "" {
		t.Fatalf("decision context lacks the decided finding: %+v", decision)
	}
}

// Histories written while the review also covered the derived model still
// verify: their snapshots keep the model that was part of their hash.
func TestLegacySnapshotWithModelStillVerifies(t *testing.T) {
	description := llmpipeline.ConceptualDescription{Things: []llmpipeline.DescriptionThing{{ID: "zapis", Name: "Zapis"}}}
	model := llmpipeline.ConceptualModelProposal{EntityConcepts: []llmpipeline.ConceptualEntityProposal{{ID: "ENT-ZAPIS"}}}
	legacyHash, err := conceptualCandidateHash(description, &model)
	if err != nil {
		t.Fatal(err)
	}
	currentHash, _ := conceptualCandidateHash(description, nil)
	if legacyHash == currentHash {
		t.Fatal("a legacy candidate must not match the description-only candidate")
	}
	history := AdversarialReviewHistory{Version: adversarialReviewHistoryVersion, Snapshots: []AdversarialCandidateSnapshot{
		{CandidateRevision: 3, CandidateHash: legacyHash, SourceHash: "sha256:source", Reason: "adversarial_review", Description: description, LegacyModel: &model, CreatedAt: "2026-09-25T00:00:00Z"},
		{CandidateRevision: 5, CandidateHash: currentHash, SourceHash: "sha256:source", Reason: "adversarial_review", Description: description, CreatedAt: "2026-09-26T00:00:00Z"},
	}}
	if err := validateAdversarialReviewHistory(history); err != nil {
		t.Fatalf("legacy and current snapshots should both verify: %v", err)
	}
}
