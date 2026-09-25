package workspace

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/scaffold"
)

func TestAddPastedTextResourceWritesFilesAndManifest(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	resource, revision, err := store.AddPastedTextResource(project.ID, 0, "Task text", "System stores products in a catalog.")
	if err != nil {
		t.Fatalf("add pasted text: %v", err)
	}
	if revision <= project.CurrentRevision {
		t.Fatalf("expected revision increment, got %d after %d", revision, project.CurrentRevision)
	}
	if resource.ContentPath == "" || resource.ExtractedTextPath == "" {
		t.Fatalf("expected stored paths, got %+v", resource)
	}
	if !strings.HasPrefix(resource.ContentHash, "sha256:") || !strings.HasPrefix(resource.ExtractedTextHash, "sha256:") {
		t.Fatalf("expected sha256 hashes, got %+v", resource)
	}
	text, err := os.ReadFile(store.absoluteWorkspacePath(resource.ExtractedTextPath))
	if err != nil {
		t.Fatalf("read extracted text: %v", err)
	}
	if string(text) != "System stores products in a catalog.\n" {
		t.Fatalf("unexpected extracted text %q", string(text))
	}
	manifest, err := store.SourceManifestYAML(project.ID)
	if err != nil {
		t.Fatalf("source manifest: %v", err)
	}
	if !strings.Contains(manifest, "source_manifest") || !strings.Contains(manifest, resource.ID) {
		t.Fatalf("manifest missing resource: %s", manifest)
	}
}

func TestAddUploadedTextResourceExtractsText(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Uploads", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	resource, _, err := store.AddUploadedResource(project.ID, 0, "Rules", "rules.md", "", bytes.NewBufferString("# Rules\nProducts have prices.\n"))
	if err != nil {
		t.Fatalf("add uploaded text: %v", err)
	}
	if resource.FileType != "markdown" || resource.ExtractionStatus != "ready" {
		t.Fatalf("unexpected resource extraction: %+v", resource)
	}
	text, _, err := store.ResourceText(project.ID, resource.ID)
	if err != nil {
		t.Fatalf("resource text: %v", err)
	}
	if !strings.Contains(text, "Products have prices.") {
		t.Fatalf("unexpected resource text: %q", text)
	}
}

func TestProcessSourcesWritesCombinedDocumentWithoutLineage(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, revision, err := store.AddPastedTextResource(project.ID, 0, "Task text", "System stores products in a catalog.")
	if err != nil {
		t.Fatalf("add pasted text: %v", err)
	}
	revision, updated, err := store.ProcessSources(project.ID, ProcessSourcesOptions{
		BaseRevision: revision,
		Model:        "mock-model",
	})
	if err != nil {
		t.Fatalf("process sources: %v", err)
	}
	if revision == 0 || !testContains(updated, "combined_document") {
		t.Fatalf("expected combined document update, revision=%d updated=%v", revision, updated)
	}
	document, err := store.CombinedDocument(project.ID)
	if err != nil {
		t.Fatalf("combined document: %v", err)
	}
	if document.Summary.SentenceCount != 1 || !strings.Contains(document.Markdown, "[SU-001]") {
		t.Fatalf("unexpected combined document: %+v", document.Summary)
	}
	if len(document.Lineage.Sentences[0].DerivedFrom) != 0 {
		t.Fatalf("segmentation unexpectedly reconstructed lineage: %+v", document.Lineage.Sentences[0].DerivedFrom)
	}
	segmentation, err := store.SourceSegmentation(project.ID)
	if err != nil || len(segmentation.Segments) != 1 || segmentation.Segments[0].ID != "SU-001" {
		t.Fatalf("unexpected enriched segmentation: proposal=%+v err=%v", segmentation, err)
	}
}

func TestProcessSourcesCountsSentencesAndStructuralUnitsSeparately(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Rules", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, revision, err := store.AddUploadedResource(project.ID, project.CurrentRevision, "Rules", "rules.md", "", bytes.NewBufferString("# Rules\nProducts have names. Prices are required.\n- Short label\n"))
	if err != nil {
		t.Fatalf("add markdown resource: %v", err)
	}
	if _, _, err := store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"}); err != nil {
		t.Fatalf("process sources: %v", err)
	}
	document, err := store.CombinedDocument(project.ID)
	if err != nil {
		t.Fatalf("combined document: %v", err)
	}
	if document.Summary.UnitCount != 4 || document.Summary.SentenceCount != 2 || document.Summary.StructuralUnitCount != 2 {
		t.Fatalf("unexpected combined-document counts: %+v", document.Summary)
	}
	if document.Lineage.Sentences[0].Kind != "structural" || document.Lineage.Sentences[1].Kind != "sentence" {
		t.Fatalf("unexpected unit kinds: %+v", document.Lineage.Sentences)
	}
}

func TestIntakeMutationsEnforceOptionalBaseRevision(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Revisions", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	resource, revision, err := store.AddPastedTextResource(project.ID, project.CurrentRevision, "Task", "Product has a name.")
	if err != nil {
		t.Fatalf("add resource: %v", err)
	}
	if _, _, err := store.AddPastedTextResource(project.ID, project.CurrentRevision, "Stale", "Stale write."); err != ErrRevisionConflict {
		t.Fatalf("stale pasted-text write error = %v, want %v", err, ErrRevisionConflict)
	}
	if _, _, err := store.AddUploadedResource(project.ID, project.CurrentRevision, "Stale", "stale.txt", "", bytes.NewBufferString("Stale upload.")); err != ErrRevisionConflict {
		t.Fatalf("stale upload error = %v, want %v", err, ErrRevisionConflict)
	}
	if _, err := store.DeleteResource(project.ID, project.CurrentRevision, resource.ID); err != ErrRevisionConflict {
		t.Fatalf("stale delete error = %v, want %v", err, ErrRevisionConflict)
	}
	if _, found, err := store.Resource(project.ID, resource.ID); err != nil || !found {
		t.Fatalf("stale delete removed resource: found=%v err=%v", found, err)
	}
	if _, err := store.DeleteResource(project.ID, revision, resource.ID); err != nil {
		t.Fatalf("delete with current revision: %v", err)
	}
}

func TestProcessSourcesWritesSourceUnitsInSameRevision(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, revision, err := store.AddPastedTextResource(project.ID, 0, "Task", "Products have names.")
	if err != nil {
		t.Fatalf("add resource: %v", err)
	}
	revision, updated, err := store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("process sources: %v", err)
	}
	if !testContains(updated, "source_units") {
		t.Fatalf("source processing did not report source-unit artifacts: %v", updated)
	}
	artifacts, err := store.SourceUnitArtifacts(project.ID)
	if err != nil {
		t.Fatalf("read source-unit artifacts: %v", err)
	}
	if !artifacts.QA.OK || len(artifacts.Accepted.SourceUnits) != 1 {
		t.Fatalf("unexpected artifacts: %+v", artifacts)
	}
	state, _ := store.Project(project.ID)
	if !strings.Contains(state.SourceUnitsPath, fmt.Sprintf("rev_%06d", revision)) {
		t.Fatalf("source units do not belong to the source-processing revision: %s", state.SourceUnitsPath)
	}
	units, err := store.SourceUnits(project.ID)
	if err != nil || len(units) != 1 || len(units[0].OriginSpans) != 0 {
		t.Fatalf("unexpected API source units: units=%+v err=%v", units, err)
	}
}

// Source units are derived deterministically during source processing.
func TestProcessSourcesDerivesSourceUnitsDeterministically(t *testing.T) {
	store := newIngestionTestStore(t)
	project, _ := store.CreateProject("Products", "", "en", "catalog")
	_, revision, _ := store.AddPastedTextResource(project.ID, 0, "Task", "Products have names.")
	_, _, err := store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("process sources: %v", err)
	}
	artifacts, err := store.SourceUnitArtifacts(project.ID)
	if err != nil || artifacts.QA.DerivationStrategy != "segments_v1" || len(artifacts.Accepted.SourceUnits) == 0 {
		t.Fatalf("unexpected source units: %+v err=%v", artifacts.QA, err)
	}
}

func TestDeleteProjectRemovesWorkspaceAndPersistsDeletion(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Disposable", "", "en", "test")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, _, err := store.AddPastedTextResource(project.ID, 0, "Task", "Product has a name."); err != nil {
		t.Fatalf("add resource: %v", err)
	}
	workspaceDir := store.projectWorkspaceDir(project.ID)
	if _, err := os.Stat(workspaceDir); err != nil {
		t.Fatalf("expected project workspace: %v", err)
	}

	if err := store.DeleteProject(project.ID); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	if _, ok := store.Project(project.ID); ok {
		t.Fatalf("deleted project is still available")
	}
	if _, err := os.Stat(workspaceDir); !os.IsNotExist(err) {
		t.Fatalf("expected project workspace to be removed, got %v", err)
	}

	reloaded, err := NewStore(store.root)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if _, ok := reloaded.Project(project.ID); ok {
		t.Fatalf("deleted project returned after reload")
	}
}

func newIngestionTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	fixtureDir := filepath.Join(root, "poc", "printing_house_full", "v0.5_granularity_sentance")
	if _, err := scaffold.BundleFromText("Product has a name.", fixtureDir, scaffold.Options{ModelID: "fixture", Name: "Fixture"}); err != nil {
		t.Fatalf("create fixture bundle: %v", err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return store
}

func testContains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
