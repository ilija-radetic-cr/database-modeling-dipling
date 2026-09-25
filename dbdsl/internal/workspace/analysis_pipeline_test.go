package workspace

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbdsl/internal/dsl"
	"dbdsl/internal/llm"
	"dbdsl/internal/quality"
	"dbdsl/internal/validate"

	"gopkg.in/yaml.v3"
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
	state, _ := store.Project(project.ID)
	if state.RequirementAtomsPath != "" || state.DesignObligationsPath != "" {
		t.Fatalf("no requirement artifacts may be produced: %+v", state)
	}
	if revision, err = store.AcceptConceptualModel(project.ID, revision); err != nil {
		t.Fatalf("accept conceptual model: %v", err)
	}
	if revision, _, err = store.GenerateLogicalModel(context.Background(), nil, project.ID, ModelStageOptions{BaseRevision: revision}); err != nil {
		t.Fatalf("logical model: %v", err)
	}
	health, err := store.ArtifactHealth(project.ID)
	if err != nil || health.ModelStatus != "ready" || health.SemanticVerificationStatus != "not_applicable" {
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

func TestGranularAnalysisStagesPersistIndependentArtifacts(t *testing.T) {
	store := newIngestionTestStore(t)
	project, err := store.CreateProject("Products", "", "en", "catalog")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	_, revision, err := store.AddPastedTextResource(project.ID, 0, "Task", "Products have names.")
	if err != nil {
		t.Fatalf("add resource: %v", err)
	}
	mock := llm.NewDefaultMockClient()
	mock.Structured["requirement_atom_extraction"] = json.RawMessage(`{
		"requirement_atoms":[{"id":"RA-001","statement":"Products have names.","subject":"product","predicate":"has","object":"name","quantifier":"","condition":"","temporal_semantics":"","ownership":"system","atom_type":"data_requirement","modeling_relevance":"direct_db","source_units":["SU-001"],"functional_area":"core","functional_pattern":"domain_management","support_level":"explicit","confidence":"high","requires_review":true,"modeling_outcome":"represented","persistence_effect":"required","warnings":["identity is unresolved"]}],
		"warnings":[],"confidence_summary":{"overall":"test"}}`)
	revision, _, err = store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("combined document: %v", err)
	}
	revision, _, err = store.GenerateRequirementAtoms(context.Background(), mock, project.ID, AnalysisStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("requirement atoms: %v", err)
	}
	if items, _, err := store.Requirements(project.ID); err != nil || len(items) != 1 {
		t.Fatalf("unexpected requirements: items=%+v err=%v", items, err)
	}
	revision, _, err = store.GenerateFunctionalAnalysis(context.Background(), mock, project.ID, AnalysisStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("functional analysis: %v", err)
	}
	if items, err := store.FunctionalAreas(project.ID); err != nil || len(items) != 1 {
		t.Fatalf("unexpected functional areas: items=%+v err=%v", items, err)
	}
	revision, _, err = store.GenerateCRUDMapping(context.Background(), mock, project.ID, AnalysisStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("CRUD mapping: %v", err)
	}
	if items, err := store.CrudOperations(project.ID); err != nil || len(items) != 1 || len(items[0].Creates) == 0 {
		t.Fatalf("unexpected CRUD operations: items=%+v err=%v", items, err)
	}
	state, _ := store.Project(project.ID)
	if !state.AnalysisReady || state.CRUDMatrixPath == "" || revision != state.CurrentRevision {
		t.Fatalf("analysis state is not ready: %+v", state)
	}
	health, err := store.ArtifactHealth(project.ID)
	if err != nil || health.CRUDMappingStatus != "ready" {
		t.Fatalf("unexpected artifact health: health=%+v err=%v", health, err)
	}
	revision, _, err = store.GenerateReviewCandidates(context.Background(), mock, project.ID, AnalysisStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("review candidates: %v", err)
	}
	candidates, err := store.ReviewCandidates(project.ID)
	if err != nil || len(candidates) != 1 || !candidates[0].Blocking {
		t.Fatalf("unexpected review candidates: items=%+v err=%v", candidates, err)
	}
	revision, _, err = store.ApplyProjectReviewDecisionBatch(project.ID, ApplyReviewBatchOptions{BaseRevision: revision, Selections: []ReviewSelection{{CandidateID: candidates[0].ID, SelectedOptionID: candidates[0].RecommendedID}}, ReviewedBy: "test", DecisionMode: "manual_batch", ActiveReviewMS: 1250})
	if err != nil {
		t.Fatalf("apply review decision batch: %v", err)
	}
	decisions, err := store.ReviewDecisions(project.ID)
	if err != nil || len(decisions) != 1 || decisions[0].ApplyStatus != "applied" {
		t.Fatalf("unexpected review decisions: items=%+v err=%v", decisions, err)
	}
	state, _ = store.Project(project.ID)
	if len(state.OpenReviewIDs) != 0 || state.LifecycleStatus != "ready_for_model_generation" {
		t.Fatalf("review gate did not open: %+v", state)
	}
	optimization, err := store.LLMOptimizationReport(project.ID)
	if err != nil || optimization.AvoidedReviewCalls != 1 || optimization.BatchedManualDecisions != 1 || optimization.ActiveReviewMS != 1250 {
		t.Fatalf("unexpected optimization report after review batch: report=%+v err=%v", optimization, err)
	}
	revision, _, err = store.GenerateConceptualModel(context.Background(), mock, project.ID, ModelStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("conceptual model: %v", err)
	}
	conceptual, err := store.ConceptualModel(project.ID)
	if err != nil || !conceptual.QA.OK || conceptual.IsAccepted {
		t.Fatalf("unexpected conceptual model: artifact=%+v err=%v", conceptual, err)
	}
	revision, err = store.AcceptConceptualModel(project.ID, revision)
	if err != nil {
		t.Fatalf("accept conceptual model: %v", err)
	}
	revision, _, err = store.GenerateLogicalModel(context.Background(), mock, project.ID, ModelStageOptions{BaseRevision: revision, Model: "mock-model"})
	if err != nil {
		t.Fatalf("logical model: %v", err)
	}
	state, _ = store.Project(project.ID)
	if !state.ModelGenerated || state.DBMLReady {
		t.Fatalf("logical model gate state is wrong: %+v", state)
	}
	// The segment-based conceptual stage has no design obligations, so semantic
	// verification does not apply and the final review follows directly.
	if health, err := store.ArtifactHealth(project.ID); err != nil || health.SemanticVerificationStatus != "not_applicable" {
		t.Fatalf("semantic verification should not apply: health=%+v err=%v", health, err)
	}
	revision, err = store.AcceptFinalModel(project.ID, revision)
	if err != nil {
		t.Fatalf("accept final model: %v", err)
	}
	state, _ = store.Project(project.ID)
	acceptedBundle, err := dsl.LoadV05Bundle(state.ModelPath)
	if err != nil || acceptedBundle.Document.Model.Status != "accepted" {
		t.Fatalf("accepted DB-DSL metadata is stale: status=%q err=%v", acceptedBundle.Document.Model.Status, err)
	}
	if filepath.Dir(acceptedBundle.RequirementAtomsPath) != filepath.Dir(state.ModelPath) {
		t.Fatalf("accepted DB-DSL references a different artifact revision: model=%s atoms=%s", state.ModelPath, acceptedBundle.RequirementAtomsPath)
	}
	revision, _, err = store.GenerateFinalOutputs(project.ID, revision, nil)
	if err != nil {
		t.Fatalf("generate final outputs: %v", err)
	}
	state, _ = store.Project(project.ID)
	if !state.DBMLReady || state.DBMLPath == "" || state.TraceReportPath == "" {
		t.Fatalf("final outputs are not ready: %+v", state)
	}
	var persistedQuality quality.Report
	if data, readErr := os.ReadFile(store.absoluteWorkspacePath(state.QualityReportPath)); readErr != nil {
		t.Fatalf("read persisted quality report: %v", readErr)
	} else if decodeErr := json.Unmarshal(data, &persistedQuality); decodeErr != nil || persistedQuality.Summary.DBMLStatus != "ready" {
		t.Fatalf("persisted quality report is stale: report=%+v err=%v", persistedQuality, decodeErr)
	}
	if traceData, readErr := os.ReadFile(store.absoluteWorkspacePath(state.TraceReportPath)); readErr != nil {
		t.Fatalf("read trace report: %v", readErr)
	} else if bytes.Contains(traceData, []byte(store.root)) {
		t.Fatalf("trace report contains an absolute workspace path: %s", traceData)
	}
	summary, err := store.ProjectSummary(project.ID)
	if err != nil || summary.Counts.ReviewDecisions != 1 {
		t.Fatalf("review decisions must not be double counted: summary=%+v err=%v", summary, err)
	}
	if dbml, err := store.DBML(project.ID); err != nil || dbml == "" {
		t.Fatalf("DBML unavailable: %q %v", dbml, err)
	}
	bundle, err := store.ExportBundle(project.ID)
	if err != nil {
		t.Fatalf("export bundle: %v", err)
	}
	assertExportFiles(t, bundle, []string{"TASK.md", "source_manifest.yaml", "source_segmentation.proposed.json", "combined_document.md", "combined_document_lineage.json", "source_units.proposed.json", "source_units.yaml", "source_unit_qa.json", "requirement_atoms.proposed.json", "requirement_atoms.yaml", "functional_analysis.proposed.json", "functional_decomposition.yaml", "crud_mapping.proposed.json", "crud_matrix.yaml", "review_candidates.proposed.json", "review_candidates.yaml", "review_decisions.yaml", "conceptual_model.proposed.json", "conceptual_model.accepted.json", "dbdsl_patch.proposed.json", "db_model.dsl.yaml", "model.dbml", "traceability_report.md", "validation_report.json", "lint_report.json", "quality_report.json", "llm_optimization_report.json", "llm_optimization_report.md", "export_manifest.json"})
	completedSummary, completedSnapshot, err := store.CompleteProject(project.ID, revision)
	if err != nil || completedSummary.LifecycleStatus != "completed" || completedSnapshot.ID == "" {
		t.Fatalf("complete project: summary=%+v snapshot=%+v err=%v", completedSummary, completedSnapshot, err)
	}
	reopenedSummary, sourceSnapshotID, err := store.ReopenProject(project.ID, "Regenerate deterministic outputs")
	if err != nil || sourceSnapshotID != completedSnapshot.ID {
		t.Fatalf("reopen completed project: summary=%+v source=%q err=%v", reopenedSummary, sourceSnapshotID, err)
	}
	reopenedState, _ := store.Project(reopenedSummary.ID)
	if !reopenedState.ModelGenerated || reopenedState.FinalModelAccepted || reopenedState.DBMLReady || reopenedState.ModelPath == "" {
		t.Fatalf("reopened project did not preserve the accepted model boundary: %+v", reopenedState)
	}
	if runs, err := store.LLMRuns(reopenedSummary.ID); err != nil || len(runs) == 0 {
		t.Fatalf("reopened project lost LLM run history: runs=%+v err=%v", runs, err)
	}
	manifestData, err := os.ReadFile(store.absoluteWorkspacePath(reopenedState.SourceManifestPath))
	if err != nil {
		t.Fatalf("read reopened source manifest: %v", err)
	}
	var reopenedManifest SourceManifest
	if err := yaml.Unmarshal(manifestData, &reopenedManifest); err != nil || len(reopenedManifest.Resources) != len(reopenedState.Resources) {
		t.Fatalf("reopened source manifest does not describe copied resources: manifest=%+v err=%v", reopenedManifest, err)
	}
	for _, resource := range reopenedManifest.Resources {
		if !strings.Contains(resource.ContentPath, reopenedSummary.ID) {
			t.Fatalf("reopened resource still points to its source project: %+v", resource)
		}
		if _, err := os.Stat(store.absoluteWorkspacePath(resource.ContentPath)); err != nil {
			t.Fatalf("reopened resource copy is missing: %s: %v", resource.ContentPath, err)
		}
	}
	// The segment-based conceptual stage produced this model, so corrections go
	// through regeneration, not the atom-based review queue.
	if _, _, err := store.CreateModelCorrectionCandidate(reopenedSummary.ID, ModelCorrectionRequest{BaseRevision: reopenedState.CurrentRevision, ElementID: "table:ENT-ZAPIS", CorrectionType: "wrong_entity", Note: "Use the source terminology for this concept."}); err != ErrModelCorrectionUnavailable {
		t.Fatalf("model correction should be unavailable in the segment-based flow, got %v", err)
	}
}

func assertExportFiles(t *testing.T, data []byte, expected []string) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open export zip: %v", err)
	}
	files := map[string][]byte{}
	resourceOriginal, resourceExtracted, resourceMetadata := false, false, false
	for _, file := range reader.File {
		rc, openErr := file.Open()
		if openErr != nil {
			t.Fatal(openErr)
		}
		content, readErr := io.ReadAll(rc)
		_ = rc.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		files[file.Name] = content
		if strings.HasPrefix(file.Name, "resources/") && strings.HasSuffix(file.Name, ".extracted.txt") {
			resourceExtracted = true
		} else if strings.HasPrefix(file.Name, "resources/") && strings.HasSuffix(file.Name, ".metadata.json") {
			resourceMetadata = true
		} else if strings.HasPrefix(file.Name, "resources/") {
			resourceOriginal = true
		}
		if file.Name == "export_manifest.json" {
			if !bytes.Contains(content, []byte("model_sha256")) {
				t.Fatalf("export manifest has no model hash: %s", content)
			}
		}
	}
	for _, name := range expected {
		if _, ok := files[name]; !ok {
			t.Errorf("export is missing %s", name)
		}
	}
	if !resourceOriginal || !resourceExtracted || !resourceMetadata {
		t.Errorf("export does not contain a complete resource triplet: original=%v extracted=%v metadata=%v", resourceOriginal, resourceExtracted, resourceMetadata)
	}
	var manifest struct {
		Artifacts map[string]struct {
			SHA256 string `json:"sha256"`
			Bytes  int    `json:"bytes"`
		} `json:"artifacts"`
		Derivations map[string]struct {
			Source       string `json:"source"`
			SourceSHA256 string `json:"source_sha256"`
		} `json:"derivations"`
	}
	if err := json.Unmarshal(files["export_manifest.json"], &manifest); err != nil {
		t.Fatalf("decode export manifest: %v", err)
	}
	if len(manifest.Artifacts) != len(files)-1 {
		t.Fatalf("export manifest does not cover every payload: manifest=%d payloads=%d", len(manifest.Artifacts), len(files)-1)
	}
	for name, content := range files {
		if name == "export_manifest.json" {
			continue
		}
		record, ok := manifest.Artifacts[name]
		if !ok || record.SHA256 != sha256Hash(content) || record.Bytes != len(content) {
			t.Fatalf("export manifest mismatch for %s: %+v", name, record)
		}
	}
	for _, generated := range []string{"model.dbml", "traceability_report.md"} {
		derivation, ok := manifest.Derivations[generated]
		if !ok || derivation.Source != "db_model.dsl.yaml" || derivation.SourceSHA256 != sha256Hash(files["db_model.dsl.yaml"]) {
			t.Fatalf("missing model derivation for %s: %+v", generated, derivation)
		}
	}
	tempDir := t.TempDir()
	for name, content := range files {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			t.Fatalf("unsafe export path %q", name)
		}
		path := filepath.Join(tempDir, clean)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	validation := validate.ValidateFile(filepath.Join(tempDir, "db_model.dsl.yaml"))
	if !validation.OK() {
		t.Fatalf("exported bundle does not validate after extraction: %v", validation.Errors)
	}
	var sourceManifest SourceManifest
	if err := yaml.Unmarshal(files["source_manifest.yaml"], &sourceManifest); err != nil || len(sourceManifest.Resources) == 0 {
		t.Fatalf("exported source manifest is empty: manifest=%+v err=%v", sourceManifest, err)
	}
	for _, resource := range sourceManifest.Resources {
		if !strings.HasPrefix(resource.ContentPath, "resources/") || !strings.HasPrefix(resource.ExtractedTextPath, "resources/") {
			t.Fatalf("exported resource paths are not portable: %+v", resource)
		}
	}
}

func TestGranularAnalysisRejectsStaleRevision(t *testing.T) {
	store := newIngestionTestStore(t)
	project, _ := store.CreateProject("Products", "", "en", "catalog")
	_, revision, _ := store.AddPastedTextResource(project.ID, 0, "Task", "Products have names.")
	mock := llm.NewDefaultMockClient()
	revision, _, _ = store.ProcessSources(project.ID, ProcessSourcesOptions{BaseRevision: revision, Model: "mock-model"})
	if _, _, err := store.GenerateRequirementAtoms(context.Background(), mock, project.ID, AnalysisStageOptions{BaseRevision: revision - 1}); err != ErrRevisionConflict {
		t.Fatalf("expected revision conflict, got %v", err)
	}
}
