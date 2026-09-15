package workspace

import (
	"path/filepath"
	"testing"

	"dbdsl/internal/llmpipeline"
)

func TestNormalizeNewReviewCandidateIDsResolvesExistingCollision(t *testing.T) {
	existing := []llmpipeline.ProjectReviewCandidateProposal{{ID: "RC-001"}, {ID: "RC-004"}, {ID: "RC-011"}}
	proposed := []llmpipeline.ProjectReviewCandidateProposal{{
		ID: "RC-004", RecommendedOptionID: "RC-004-O1",
		Options: []llmpipeline.ReviewOptionProposal{
			{ID: "RC-004-O1", Recommended: true},
			{ID: "RC-004-O2"},
		},
	}}

	renamed := normalizeNewReviewCandidateIDs(existing, proposed)
	if len(renamed) != 1 || renamed[0] != "RC-004 -> RC-012" {
		t.Fatalf("unexpected renaming audit: %+v", renamed)
	}
	if proposed[0].ID != "RC-012" || proposed[0].RecommendedOptionID != "RC-012-O1" {
		t.Fatalf("candidate IDs were not normalized: %+v", proposed[0])
	}
	if proposed[0].Options[0].ID != "RC-012-O1" || proposed[0].Options[1].ID != "RC-012-O2" {
		t.Fatalf("option IDs were not normalized: %+v", proposed[0].Options)
	}
}

func TestLoadReviewAnalysisContextRecoversClearedPointers(t *testing.T) {
	store := newIngestionTestStore(t)
	project := &ProjectState{ID: "project_review_recovery", CurrentRevision: 8}
	functional := llmpipeline.FunctionalAnalysisProposal{
		FunctionalAreas: []llmpipeline.FunctionalAreaProposal{{ID: "FA-004"}},
		Actors:          []llmpipeline.ActorProposal{{ID: "ACT-SYSTEM"}},
	}
	crud := llmpipeline.CRUDMappingProposal{
		Operations: []llmpipeline.CRUDOperationProposal{{ID: "OP-010", FunctionalAreaID: "FA-004", ActorID: "ACT-SYSTEM"}},
	}
	functionalPath := filepath.ToSlash(filepath.Join(store.projectRevisionRel(project.ID, 5), "functional_analysis.proposed.json"))
	crudPath := filepath.ToSlash(filepath.Join(store.projectRevisionRel(project.ID, 6), "crud_mapping.proposed.json"))
	writeRepairFixture(t, store.absoluteWorkspacePath(functionalPath), functional)
	writeRepairFixture(t, store.absoluteWorkspacePath(crudPath), crud)

	loadedFunctional, loadedCRUD, recoveredFunctionalPath, recoveredCRUDPath, err := store.loadReviewAnalysisContext(project)
	if err != nil {
		t.Fatalf("recover review analysis context: %v", err)
	}
	if recoveredFunctionalPath != functionalPath || recoveredCRUDPath != crudPath {
		t.Fatalf("unexpected recovered paths: functional=%q CRUD=%q", recoveredFunctionalPath, recoveredCRUDPath)
	}
	if len(loadedFunctional.FunctionalAreas) != 1 || loadedFunctional.FunctionalAreas[0].ID != "FA-004" {
		t.Fatalf("functional analysis snapshot was not recovered: %+v", loadedFunctional)
	}
	if len(loadedCRUD.Operations) != 1 || loadedCRUD.Operations[0].ID != "OP-010" {
		t.Fatalf("CRUD snapshot was not recovered: %+v", loadedCRUD)
	}

	restoreReviewAnalysisPaths(project, recoveredFunctionalPath, recoveredCRUDPath)
	if project.FunctionalAnalysisProposalPath != functionalPath || project.CRUDMappingProposalPath != crudPath {
		t.Fatalf("recovered pointers were not restored: %+v", project)
	}
	if filepath.Base(project.FunctionalDecompositionPath) != "functional_decomposition.yaml" || filepath.Base(project.CRUDMatrixPath) != "crud_matrix.yaml" {
		t.Fatalf("accepted artifact siblings were not restored: %+v", project)
	}
}

func TestNormalizeNewReviewCandidateIDsPreservesUnusedID(t *testing.T) {
	existing := []llmpipeline.ProjectReviewCandidateProposal{{ID: "RC-001"}}
	proposed := []llmpipeline.ProjectReviewCandidateProposal{{ID: "RC-005"}}
	if renamed := normalizeNewReviewCandidateIDs(existing, proposed); len(renamed) != 0 || proposed[0].ID != "RC-005" {
		t.Fatalf("unused candidate ID should be preserved: renamed=%+v candidate=%+v", renamed, proposed[0])
	}
}

func TestNormalizeNewReviewCandidateReferencesRepairsUniqueTypo(t *testing.T) {
	proposed := []llmpipeline.ProjectReviewCandidateProposal{{
		ID: "RC-018", AffectedFunctionalAreas: []string{"FA-CART-CHECKOUT", "FA-ORDER-FULFMENT", "FA-PROCUREMENT"},
	}}
	repairs := normalizeNewReviewCandidateReferences(proposed, reviewReferenceCatalog{
		FunctionalAreas: []string{"FA-CART-CHECKOUT", "FA-ORDER-FULFILLMENT", "FA-PROCUREMENT"},
	})
	if len(repairs) != 1 || proposed[0].AffectedFunctionalAreas[1] != "FA-ORDER-FULFILLMENT" {
		t.Fatalf("unique area typo was not normalized: repairs=%+v candidate=%+v", repairs, proposed[0])
	}
}

func TestUniqueNearReferenceRejectsAmbiguousOrDistantMatch(t *testing.T) {
	if replacement, ok := uniqueNearReference("FA-CORE-X", []string{"FA-CORE-A", "FA-CORE-B"}); ok || replacement != "" {
		t.Fatalf("ambiguous reference must not be normalized: %q", replacement)
	}
	if replacement, ok := uniqueNearReference("FA-COMPLETELY-DIFFERENT", []string{"FA-CATALOG"}); ok || replacement != "" {
		t.Fatalf("distant reference must not be normalized: %q", replacement)
	}
}
