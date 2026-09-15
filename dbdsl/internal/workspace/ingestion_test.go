package workspace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/llm"
	"dbdsl/internal/scaffold"
)

func TestAddPastedTextResourceWritesFilesAndManifest(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	resource, revision, err := store.AddPastedTextResource(project.ID, "Task text", "System stores products in a catalog.")
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

	resource, _, err := store.AddUploadedResource(project.ID, "Rules", "rules.md", "", bytes.NewBufferString("# Rules\nProducts have prices.\n"))
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

func TestProcessSourcesWritesCombinedDocumentWithLineage(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	resource, revision, err := store.AddPastedTextResource(project.ID, "Task text", "System stores products in a catalog.")
	if err != nil {
		t.Fatalf("add pasted text: %v", err)
	}
	mock := llm.NewDefaultMockClient()
	mock.Structured["combined_document"] = json.RawMessage(fmt.Sprintf(`{
	  "sentences": [
	    {
	      "id": "OD-S-001",
	      "text": "System stores products in a catalog.",
	      "derived_from": [
	        {"resource_id": %q, "line_start": 1, "line_end": 1, "exact_text": "System stores products in a catalog."}
	      ],
	      "transformation": "copied",
	      "confidence": "high",
	      "warnings": []
	    }
	  ],
	  "warnings": [],
	  "confidence_summary": {"overall": "test"}
	}`, resource.ID))

	revision, updated, err := store.ProcessSources(context.Background(), mock, project.ID, ProcessSourcesOptions{
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
	if document.Summary.SentenceCount != 1 || !strings.Contains(document.Markdown, "[OD-S-0001]") {
		t.Fatalf("unexpected combined document: %+v", document.Summary)
	}
	if document.Lineage.Sentences[0].DerivedFrom[0].ResourceID != resource.ID {
		t.Fatalf("unexpected lineage: %+v", document.Lineage.Sentences[0].DerivedFrom)
	}
	fidelity, err := store.SourceFidelity(project.ID)
	if err != nil || !fidelity.OK || fidelity.NormativeCoverage != 1 {
		t.Fatalf("expected complete deterministic source fidelity: report=%+v err=%v", fidelity, err)
	}
}

func TestGenerateSourceUnitsWritesRevisionedArtifacts(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, revision, err := store.AddPastedTextResource(project.ID, "Task", "Products have names.")
	if err != nil {
		t.Fatalf("add resource: %v", err)
	}
	revision, _, err = store.ProcessSources(context.Background(), llm.NewDefaultMockClient(), project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("process sources: %v", err)
	}
	revision, updated, err := store.GenerateSourceUnits(context.Background(), llm.NewDefaultMockClient(), project.ID, GenerateSourceUnitsOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("generate source units: %v", err)
	}
	if !testContains(updated, "source_units") {
		t.Fatalf("expected source-unit update, got %v", updated)
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
		t.Fatalf("source units are not revisioned: %s", state.SourceUnitsPath)
	}
	units, err := store.SourceUnits(project.ID)
	if err != nil || len(units) != 1 || len(units[0].OriginSpans) != 1 {
		t.Fatalf("unexpected API source units: units=%+v err=%v", units, err)
	}
}

func TestGenerateSourceUnitsFallsBackWithoutLLM(t *testing.T) {
	store := newIngestionTestStore(t)
	project, _ := store.CreateProject("Products", "", "en", "catalog")
	_, revision, _ := store.AddPastedTextResource(project.ID, "Task", "Products have names.")
	revision, _, err := store.ProcessSources(context.Background(), llm.NewDefaultMockClient(), project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("process sources: %v", err)
	}
	if _, _, err := store.GenerateSourceUnits(context.Background(), nil, project.ID, GenerateSourceUnitsOptions{BaseRevision: revision}); err != nil {
		t.Fatalf("fallback source units: %v", err)
	}
	artifacts, _ := store.SourceUnitArtifacts(project.ID)
	if artifacts.QA.DerivationStrategy != "deterministic_fallback" {
		t.Fatalf("expected fallback strategy, got %s", artifacts.QA.DerivationStrategy)
	}
}

func TestDeleteProjectRemovesWorkspaceAndPersistsDeletion(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Disposable", "", "en", "test")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, _, err := store.AddPastedTextResource(project.ID, "Task", "Product has a name."); err != nil {
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

func TestDeleteCanonicalProjectPersistsDeletion(t *testing.T) {
	store := newIngestionTestStore(t)
	if err := store.DeleteProject(CanonicalProjectID); err != nil {
		t.Fatalf("delete canonical project: %v", err)
	}

	reloaded, err := NewStore(store.root)
	if err != nil {
		t.Fatalf("reload store: %v", err)
	}
	if _, ok := reloaded.Project(CanonicalProjectID); ok {
		t.Fatalf("deleted canonical project returned after reload")
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
