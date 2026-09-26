package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dbdsl/internal/jobs"
	"dbdsl/internal/scaffold"
)

func TestLLMStatusDoesNotExposeAPIKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-openai-key")
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/llm/status", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "test-openai-key") {
		t.Fatalf("status response exposed API key: %s", rec.Body.String())
	}
	var body struct {
		Available     bool   `json:"available"`
		DefaultModel  string `json:"default_model"`
		MockAvailable bool   `json:"mock_available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Available || body.DefaultModel == "" || !body.MockAvailable {
		t.Fatalf("unexpected status response: %+v", body)
	}
}

func TestListProjectsReturnsEmptyArray(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Items == nil || len(body.Items) != 0 {
		t.Fatalf("expected an empty projects array, got %s", rec.Body.String())
	}
}

func TestProjectJobEntryPointsRejectConcurrentWork(t *testing.T) {
	server := newTestServer(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "Product has a name.")

	failed := server.jobs.StartWithRevision(projectID, "failed_stage", revision, []string{"fail"}, func(_ string, _ string, _ int, _ jobs.StepEmitter) (int, []string, error) {
		return 0, nil, fmt.Errorf("expected test failure")
	})
	waitForFailedTestJob(t, server, failed.ID)

	release := make(chan struct{})
	released := false
	defer func() {
		if !released {
			close(release)
		}
	}()
	active := server.jobs.StartWithRevision(projectID, "blocking_stage", revision, []string{"block"}, func(_ string, _ string, inputRevision int, _ jobs.StepEmitter) (int, []string, error) {
		<-release
		return inputRevision, nil, nil
	})
	waitForRunningTestJob(t, server, active.ID)

	requests := []struct {
		name string
		path string
		body string
	}{
		{name: "quality refresh", path: "/api/v1/projects/" + projectID + "/quality/run", body: `{}`},
		{name: "DBML regeneration", path: "/api/v1/projects/" + projectID + "/dbml/regenerate", body: `{}`},
		{name: "job retry", path: "/api/v1/projects/" + projectID + "/jobs/" + failed.ID + "/retry", body: fmt.Sprintf(`{"base_revision":%d}`, revision)},
		{name: "adversarial review", path: "/api/v1/projects/" + projectID + "/stages/adversarial_review/run", body: fmt.Sprintf(`{"base_revision":%d}`, revision)},
		{name: "operator review finding", path: "/api/v1/projects/" + projectID + "/adversarial-review/findings", body: fmt.Sprintf(`{"base_revision":%d}`, revision)},
		{name: "review decisions", path: "/api/v1/projects/" + projectID + "/adversarial-review/decisions", body: fmt.Sprintf(`{"base_revision":%d}`, revision)},
		{name: "conceptual correction", path: "/api/v1/projects/" + projectID + "/adversarial-review/correct", body: fmt.Sprintf(`{"base_revision":%d}`, revision)},
		{name: "conceptual acceptance", path: "/api/v1/projects/" + projectID + "/conceptual-model/accept", body: fmt.Sprintf(`{"base_revision":%d}`, revision)},
	}
	for _, test := range requests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(test.body))
			rec := httptest.NewRecorder()
			server.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"project_busy"`) {
				t.Fatalf("expected project_busy, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
	close(release)
	released = true
	waitForTestJob(t, server, active.ID)
}

func TestMockExecutionProfileCarriesIntoConceptualStage(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "Product has a name.")

	processReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/process-sources", bytes.NewReader([]byte(fmt.Sprintf(`{
	  "base_revision": %d,
	  "mock": true,
	  "model": "gpt-5.6-sol"
	}`, revision))))
	processRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(processRec, processReq)
	if processRec.Code != http.StatusAccepted {
		t.Fatalf("start mock source processing: %d %s", processRec.Code, processRec.Body.String())
	}
	var processing struct {
		Job jobs.Job `json:"job"`
	}
	if err := json.Unmarshal(processRec.Body.Bytes(), &processing); err != nil {
		t.Fatalf("decode process job: %v", err)
	}
	waitForTestJob(t, server, processing.Job.ID)

	project, ok := server.store.Project(projectID)
	if !ok || project.LLMExecutionProfile == nil {
		t.Fatalf("mock execution profile was not persisted: %+v", project)
	}
	if project.LLMExecutionProfile.Provider != "mock" || project.LLMExecutionProfile.Model != "mock-model" {
		t.Fatalf("mock request froze a paid-provider profile: %+v", project.LLMExecutionProfile)
	}

	conceptualReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/stages/conceptual_model/run", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d}`, project.CurrentRevision))))
	conceptualRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(conceptualRec, conceptualReq)
	if conceptualRec.Code != http.StatusAccepted {
		t.Fatalf("frozen mock profile was not reused: %d %s", conceptualRec.Code, conceptualRec.Body.String())
	}
	var conceptual struct {
		Job jobs.Job `json:"job"`
	}
	if err := json.Unmarshal(conceptualRec.Body.Bytes(), &conceptual); err != nil {
		t.Fatalf("decode conceptual job: %v", err)
	}
	waitForTestJob(t, server, conceptual.Job.ID)
	project, _ = server.store.Project(projectID)
	if project.LLMExecutionProfile.Provider != "mock" || project.LLMExecutionProfile.Model != "mock-model" {
		t.Fatalf("conceptual stage changed the frozen mock profile: %+v", project.LLMExecutionProfile)
	}

	beforeReviewReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/adversarial-review", nil)
	beforeReviewRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(beforeReviewRec, beforeReviewReq)
	if beforeReviewRec.Code != http.StatusOK || !strings.Contains(beforeReviewRec.Body.String(), `"status":"not_run"`) || !strings.Contains(beforeReviewRec.Body.String(), `"decisions":[]`) || !strings.Contains(beforeReviewRec.Body.String(), `"audit":[]`) {
		t.Fatalf("unreviewed wire shape must use empty arrays: %d %s", beforeReviewRec.Code, beforeReviewRec.Body.String())
	}
	logicalReviewReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/stages/adversarial_review/run", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d,"scope":"logical"}`, project.CurrentRevision))))
	logicalReviewRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(logicalReviewRec, logicalReviewReq)
	if logicalReviewRec.Code != http.StatusBadRequest {
		t.Fatalf("unsupported logical review scope was queued: %d %s", logicalReviewRec.Code, logicalReviewRec.Body.String())
	}

	reviewReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/stages/adversarial_review/run", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d}`, project.CurrentRevision))))
	reviewRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(reviewRec, reviewReq)
	if reviewRec.Code != http.StatusAccepted {
		t.Fatalf("start adversarial review: %d %s", reviewRec.Code, reviewRec.Body.String())
	}
	var reviewJob struct {
		Job jobs.Job `json:"job"`
	}
	if err := json.Unmarshal(reviewRec.Body.Bytes(), &reviewJob); err != nil {
		t.Fatalf("decode adversarial review job: %v", err)
	}
	waitForTestJob(t, server, reviewJob.Job.ID)
	project, _ = server.store.Project(projectID)
	afterReviewReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/adversarial-review", nil)
	afterReviewRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(afterReviewRec, afterReviewReq)
	if afterReviewRec.Code != http.StatusOK || !strings.Contains(afterReviewRec.Body.String(), `"status":"current"`) || !strings.Contains(afterReviewRec.Body.String(), `"findings":[]`) || !strings.Contains(afterReviewRec.Body.String(), `"decisions":[]`) {
		t.Fatalf("reviewed wire shape must use empty arrays: %d %s", afterReviewRec.Code, afterReviewRec.Body.String())
	}
	sourceArtifacts, err := server.store.SourceUnitArtifacts(projectID)
	if err != nil || len(sourceArtifacts.Accepted.SourceUnits) == 0 {
		t.Fatalf("read source evidence for operator finding: %v", err)
	}
	conceptualArtifacts, err := server.store.ConceptualModel(projectID)
	if err != nil || len(conceptualArtifacts.Proposed.EntityConcepts) == 0 {
		t.Fatalf("read conceptual IDs for operator finding: %v", err)
	}
	unit := sourceArtifacts.Accepted.SourceUnits[0]
	findingBody, _ := json.Marshal(map[string]any{
		"base_revision": project.CurrentRevision, "actor": "test_operator", "note": "API operator finding.",
		"finding": map[string]any{
			"severity": "warning", "category": "missing", "source_unit_ids": []string{unit.ID},
			"description_refs": []string{conceptualArtifacts.Description.Things[0].ID}, "source_quote": unit.Text.Exact,
			"claim": "A cited identity detail is absent.", "expected": "Preserve the cited identity detail.",
			"actual": "The candidate omits the detail.", "suggested_correction": "Add the grounded detail.",
		},
	})
	findingReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/adversarial-review/findings", bytes.NewReader(findingBody))
	findingRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(findingRec, findingReq)
	if findingRec.Code != http.StatusOK || !strings.Contains(findingRec.Body.String(), `"id":"OF-001"`) || !strings.Contains(findingRec.Body.String(), `"origin":"operator"`) || !strings.Contains(findingRec.Body.String(), `"action":"operator_finding_added"`) {
		t.Fatalf("operator finding endpoint omitted combined finding or provenance: %d %s", findingRec.Code, findingRec.Body.String())
	}
	project, _ = server.store.Project(projectID)
	decisionReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/adversarial-review/decisions", strings.NewReader(fmt.Sprintf(`{"base_revision":%d,"actor":"test_operator","decisions":[{"finding_id":"OF-001","decision":"waive","note":"API test explicitly waives this synthetic finding."}]}`, project.CurrentRevision)))
	decisionRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(decisionRec, decisionReq)
	if decisionRec.Code != http.StatusOK || !strings.Contains(decisionRec.Body.String(), `"can_accept":true`) {
		t.Fatalf("operator finding could not use the existing decision gate: %d %s", decisionRec.Code, decisionRec.Body.String())
	}
	project, _ = server.store.Project(projectID)

	acceptReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/conceptual-model/accept", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d,"actor":"test_operator","note":"API mock acceptance."}`, project.CurrentRevision))))
	acceptRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept adversarially reviewed conceptual model: %d %s", acceptRec.Code, acceptRec.Body.String())
	}
}

func TestBundleDiscoveryIncludesCurrentGoldenFixtures(t *testing.T) {
	server, root := newTestServerWithRoot(t)
	goldenDir := filepath.Join(root, "fixtures", "golden", "demo_v06")
	if _, err := scaffold.BundleFromText("Customer has a name.", goldenDir, scaffold.Options{ModelID: "golden", Name: "Golden fixture"}); err != nil {
		t.Fatalf("create golden fixture: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bundles", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list bundles: %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items []struct {
			BundlePath string `json:"bundle_path"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode bundles: %v", err)
	}
	found := false
	for _, item := range body.Items {
		if item.BundlePath == "fixtures/golden/demo_v06" {
			found = true
		}
	}
	if !found {
		t.Fatalf("current golden fixture directory was not discovered: %s", rec.Body.String())
	}
}

func TestImportedBundleCanBeReviewedExportedCompletedAndReopened(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server, _ := newTestServerWithRoot(t)
	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/import-bundle", bytes.NewReader([]byte(`{"path":"poc/printing_house_full/v0.5_granularity_sentance"}`)))
	importRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(importRec, importReq)
	if importRec.Code != http.StatusOK {
		t.Fatalf("import bundle: %d %s", importRec.Code, importRec.Body.String())
	}
	var imported struct {
		Project struct {
			ID              string `json:"id"`
			CurrentRevision int    `json:"current_revision"`
		} `json:"project"`
	}
	if err := json.Unmarshal(importRec.Body.Bytes(), &imported); err != nil {
		t.Fatalf("decode imported project: %v", err)
	}

	stagesReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+imported.Project.ID+"/stages", nil)
	stagesRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(stagesRec, stagesReq)
	if stagesRec.Code != http.StatusOK || !strings.Contains(stagesRec.Body.String(), `"next_stage":"model_review"`) {
		t.Fatalf("imported valid model was sent backwards in the pipeline: %d %s", stagesRec.Code, stagesRec.Body.String())
	}

	sourceUnitsReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+imported.Project.ID+"/source-units", nil)
	sourceUnitsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(sourceUnitsRec, sourceUnitsReq)
	if sourceUnitsRec.Code != http.StatusOK {
		t.Fatalf("read imported source units: %d %s", sourceUnitsRec.Code, sourceUnitsRec.Body.String())
	}
	var sourceUnits struct {
		Items []struct {
			ID             string `json:"id"`
			NormalizedText string `json:"normalized_text"`
		} `json:"items"`
	}
	if err := json.Unmarshal(sourceUnitsRec.Body.Bytes(), &sourceUnits); err != nil {
		t.Fatalf("decode imported source units: %v", err)
	}
	if len(sourceUnits.Items) == 0 || sourceUnits.Items[0].ID == "" || sourceUnits.Items[0].NormalizedText == "" {
		t.Fatalf("imported bundle lost accepted source evidence: %s", sourceUnitsRec.Body.String())
	}

	traceReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+imported.Project.ID+"/trace-index", nil)
	traceRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(traceRec, traceReq)
	if traceRec.Code != http.StatusOK {
		t.Fatalf("read imported trace index: %d %s", traceRec.Code, traceRec.Body.String())
	}
	var traceBody struct {
		TraceIndex struct {
			SourceToElements map[string][]string `json:"source_to_elements"`
		} `json:"trace_index"`
	}
	if err := json.Unmarshal(traceRec.Body.Bytes(), &traceBody); err != nil {
		t.Fatalf("decode imported trace index: %v", err)
	}
	if len(traceBody.TraceIndex.SourceToElements) == 0 {
		t.Fatalf("imported bundle lost source-to-model trace links: %s", traceRec.Body.String())
	}

	graphReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+imported.Project.ID+"/model-graph", nil)
	graphRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(graphRec, graphReq)
	var graphBody struct {
		ModelGraph struct {
			Nodes []struct {
				Fields []struct {
					ElementID string `json:"element_id"`
					Evidence  struct {
						SourceUnits []string `json:"source_units"`
					} `json:"evidence"`
				} `json:"fields"`
			} `json:"nodes"`
		} `json:"model_graph"`
	}
	if graphRec.Code != http.StatusOK || json.Unmarshal(graphRec.Body.Bytes(), &graphBody) != nil {
		t.Fatalf("read imported model graph: %d %s", graphRec.Code, graphRec.Body.String())
	}
	fieldID := ""
	for _, node := range graphBody.ModelGraph.Nodes {
		for _, field := range node.Fields {
			if len(field.Evidence.SourceUnits) > 0 {
				fieldID = field.ElementID
				break
			}
		}
	}
	if fieldID == "" {
		t.Fatalf("imported model has no field-level evidence: %s", graphRec.Body.String())
	}
	detailsReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+imported.Project.ID+"/model-elements/"+fieldID, nil)
	detailsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(detailsRec, detailsReq)
	if detailsRec.Code != http.StatusOK || !strings.Contains(detailsRec.Body.String(), `"source_unit_summaries":[`) {
		t.Fatalf("imported field evidence is not hydrated with source text: %d %s", detailsRec.Code, detailsRec.Body.String())
	}

	acceptReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+imported.Project.ID+"/model-acceptance", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d}`, imported.Project.CurrentRevision))))
	acceptRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(acceptRec, acceptReq)
	if acceptRec.Code != http.StatusOK {
		t.Fatalf("accept imported model: %d %s", acceptRec.Code, acceptRec.Body.String())
	}
	var accepted struct {
		ProjectRevision int `json:"project_revision"`
	}
	if err := json.Unmarshal(acceptRec.Body.Bytes(), &accepted); err != nil {
		t.Fatalf("decode acceptance: %v", err)
	}

	generateReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+imported.Project.ID+"/stages/generate_outputs/run", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d}`, accepted.ProjectRevision))))
	generateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusAccepted {
		t.Fatalf("start imported outputs: %d %s", generateRec.Code, generateRec.Body.String())
	}
	var generated struct {
		Job jobs.Job `json:"job"`
	}
	if err := json.Unmarshal(generateRec.Body.Bytes(), &generated); err != nil {
		t.Fatalf("decode output job: %v", err)
	}
	waitForTestJob(t, server, generated.Job.ID)
	project, _ := server.store.Project(imported.Project.ID)

	exportSuffixes := map[string]string{
		"dbml": "final.dbml", "sql": "schema.postgresql.sql", "report": "traceability_report.md", "bundle": "db_model_workbench_bundle.zip",
	}
	for _, kind := range []string{"dbml", "sql", "report", "bundle"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+imported.Project.ID+"/exports/"+kind, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.Len() == 0 {
			t.Fatalf("export %s: %d %s", kind, rec.Code, rec.Body.String())
		}
		wantFilename := fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("%s_rev_%06d_%s", imported.Project.ID, project.CurrentRevision, exportSuffixes[kind]))
		if got := rec.Header().Get("Content-Disposition"); got != wantFilename {
			t.Fatalf("export %s has ambiguous filename: got %q want %q", kind, got, wantFilename)
		}
	}

	completeReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+imported.Project.ID+"/complete", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d}`, project.CurrentRevision))))
	completeRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(completeRec, completeReq)
	if completeRec.Code != http.StatusOK || !strings.Contains(completeRec.Body.String(), `"lifecycle_status":"completed"`) {
		t.Fatalf("complete imported project: %d %s", completeRec.Code, completeRec.Body.String())
	}

	reopenReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+imported.Project.ID+"/reopen", bytes.NewReader([]byte(`{"mode":"new_revision","note":"Continue review"}`)))
	reopenRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(reopenRec, reopenReq)
	if reopenRec.Code != http.StatusOK {
		t.Fatalf("reopen completed project: %d %s", reopenRec.Code, reopenRec.Body.String())
	}
	var reopened struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		SourceSnapshotID string `json:"source_snapshot_id"`
	}
	if err := json.Unmarshal(reopenRec.Body.Bytes(), &reopened); err != nil {
		t.Fatalf("decode reopened project: %v", err)
	}
	if reopened.Project.ID == "" || reopened.Project.ID == imported.Project.ID || reopened.SourceSnapshotID == "" {
		t.Fatalf("reopen did not create a revision project: %s", reopenRec.Body.String())
	}
	reopenedStageReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+reopened.Project.ID+"/stages", nil)
	reopenedStageRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(reopenedStageRec, reopenedStageReq)
	if reopenedStageRec.Code != http.StatusOK || !strings.Contains(reopenedStageRec.Body.String(), `"next_stage":"model_review"`) {
		t.Fatalf("reopened project is not ready for explicit model review: %d %s", reopenedStageRec.Code, reopenedStageRec.Body.String())
	}
}

func TestResourceEndpointsPersistPastedTextAndManifest(t *testing.T) {
	server := newTestServer(t)

	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader([]byte(`{"name":"Products","language":"en","domain":"catalog"}`)))
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+created.Project.ID+"/resources", bytes.NewReader([]byte(`{
	  "kind": "pasted_text",
	  "title": "Task text",
	  "content": "System stores products in a catalog."
	}`)))
	addRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("expected add status 200, got %d: %s", addRec.Code, addRec.Body.String())
	}
	var added struct {
		Resource struct {
			ID                string `json:"id"`
			ContentPath       string `json:"content_path"`
			ExtractedTextPath string `json:"extracted_text_path"`
			ContentHash       string `json:"content_hash"`
			ExtractionStatus  string `json:"extraction_status"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(addRec.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	if added.Resource.ContentPath == "" || added.Resource.ExtractedTextPath == "" || !strings.HasPrefix(added.Resource.ContentHash, "sha256:") {
		t.Fatalf("expected stored resource fields, got %+v", added.Resource)
	}
	if added.Resource.ExtractionStatus != "ready" {
		t.Fatalf("expected ready extraction, got %q", added.Resource.ExtractionStatus)
	}

	textReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+created.Project.ID+"/resources/"+added.Resource.ID+"/text", nil)
	textRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(textRec, textReq)
	if textRec.Code != http.StatusOK || !strings.Contains(textRec.Body.String(), "System stores products") {
		t.Fatalf("expected extracted text response, got %d: %s", textRec.Code, textRec.Body.String())
	}

	manifestReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+created.Project.ID+"/source-manifest", nil)
	manifestRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(manifestRec, manifestReq)
	if manifestRec.Code != http.StatusOK || !strings.Contains(manifestRec.Body.String(), added.Resource.ID) {
		t.Fatalf("expected manifest response, got %d: %s", manifestRec.Code, manifestRec.Body.String())
	}
}

func TestProcessSourcesEndpointWritesCombinedDocumentWithMock(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)

	projectID, revision := createProjectAndAddPastedText(t, server, "System stores products in a catalog.")
	processReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/process-sources", bytes.NewReader([]byte(fmt.Sprintf(`{
	  "base_revision": %d,
	  "mock": true,
	  "model": "mock-model"
	}`, revision))))
	processRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(processRec, processReq)
	if processRec.Code != http.StatusAccepted {
		t.Fatalf("expected process status 202, got %d: %s", processRec.Code, processRec.Body.String())
	}
	var started struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(processRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode process response: %v", err)
	}

	waitForTestJob(t, server, started.Job.ID)

	docReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/combined-document", nil)
	docRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(docRec, docReq)
	if docRec.Code != http.StatusOK || !strings.Contains(docRec.Body.String(), "SU-001") {
		t.Fatalf("expected combined document, got %d: %s", docRec.Code, docRec.Body.String())
	}
	if !strings.Contains(docRec.Body.String(), `"llm_assisted":true`) || strings.Contains(docRec.Body.String(), `"fallback_used":true`) {
		t.Fatalf("mock processing did not expose LLM-assisted segmentation: %s", docRec.Body.String())
	}
	segmentationReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/source-segmentation", nil)
	segmentationRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(segmentationRec, segmentationReq)
	if segmentationRec.Code != http.StatusOK || !strings.Contains(segmentationRec.Body.String(), `"segments":[{"id":"SU-001"`) {
		t.Fatalf("expected ID-enriched segmentation output, got %d: %s", segmentationRec.Code, segmentationRec.Body.String())
	}
}

func TestCanonicalProcessSourcesStageRequiresLLM(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "First sentence. Second sentence.")

	stageReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/stages", nil)
	stageRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(stageRec, stageReq)
	if stageRec.Code != http.StatusOK || !strings.Contains(stageRec.Body.String(), `"next_stage":"process_sources"`) {
		t.Fatalf("expected process_sources as next stage, got %d: %s", stageRec.Code, stageRec.Body.String())
	}

	runReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/stages/process_sources/run", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d,"model":"mock-model"}`, revision))))
	runRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(runRec, runReq)
	if runRec.Code != http.StatusPreconditionFailed || !strings.Contains(runRec.Body.String(), `"code":"llm_unavailable"`) {
		t.Fatalf("process_sources must fail before starting without an LLM: %d %s", runRec.Code, runRec.Body.String())
	}
}

func TestValidationLintIsAvailableThroughStageDispatcher(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "Product has a name.")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/stages/validation_lint/run", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d}`, revision))))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("validation_lint stage was not dispatched: %d %s", rec.Code, rec.Body.String())
	}
	var started struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode validation job: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := server.jobs.Get(started.Job.ID)
		if !ok {
			t.Fatalf("validation job disappeared")
		}
		if job.Status == jobs.StatusFailed || job.Status == jobs.StatusCompleted {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("validation_lint job did not reach a terminal state")
}

func TestResourceEndpointsRejectStaleBaseRevision(t *testing.T) {
	server := newTestServer(t)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader([]byte(`{"name":"Revisions","language":"en"}`)))
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)
	var created struct {
		Project struct {
			ID              string `json:"id"`
			CurrentRevision int    `json:"current_revision"`
		} `json:"project"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+created.Project.ID+"/resources", bytes.NewReader([]byte(fmt.Sprintf(`{"title":"Task","content":"Product has a name.","base_revision":%d}`, created.Project.CurrentRevision))))
	addRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("add with current revision returned %d: %s", addRec.Code, addRec.Body.String())
	}
	var added struct {
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
		ProjectRevision int `json:"project_revision"`
	}
	if err := json.Unmarshal(addRec.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode resource: %v", err)
	}

	staleReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+created.Project.ID+"/resources", bytes.NewReader([]byte(fmt.Sprintf(`{"title":"Stale","content":"Stale.","base_revision":%d}`, created.Project.CurrentRevision))))
	staleRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(staleRec, staleReq)
	if staleRec.Code != http.StatusConflict || !strings.Contains(staleRec.Body.String(), "revision_conflict") {
		t.Fatalf("stale resource write returned %d: %s", staleRec.Code, staleRec.Body.String())
	}
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	if err := writer.WriteField("base_revision", fmt.Sprint(created.Project.CurrentRevision)); err != nil {
		t.Fatalf("write upload revision: %v", err)
	}
	part, err := writer.CreateFormFile("file", "stale.txt")
	if err != nil {
		t.Fatalf("create upload part: %v", err)
	}
	if _, err := part.Write([]byte("Stale upload.")); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close upload: %v", err)
	}
	uploadReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+created.Project.ID+"/resources/upload", &upload)
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusConflict {
		t.Fatalf("stale resource upload returned %d: %s", uploadRec.Code, uploadRec.Body.String())
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/projects/%s/resources/%s?base_revision=%d", created.Project.ID, added.Resource.ID, created.Project.CurrentRevision), nil)
	deleteRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusConflict {
		t.Fatalf("stale resource delete returned %d: %s", deleteRec.Code, deleteRec.Body.String())
	}

	deleteReq = httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/v1/projects/%s/resources/%s?base_revision=%d", created.Project.ID, added.Resource.ID, added.ProjectRevision), nil)
	deleteRec = httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("current resource delete returned %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
}

func TestProcessSourcesProducesSourceUnitQAAndUnits(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "Products have names.")
	_ = processSourcesWithMock(t, server, projectID, revision)

	qaReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/source-units/qa", nil)
	qaRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(qaRec, qaReq)
	if qaRec.Code != http.StatusOK || !strings.Contains(qaRec.Body.String(), `"ok":true`) {
		t.Fatalf("unexpected QA response %d: %s", qaRec.Code, qaRec.Body.String())
	}
	unitsReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/source-units", nil)
	unitsRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(unitsRec, unitsReq)
	if unitsRec.Code != http.StatusOK || !strings.Contains(unitsRec.Body.String(), "SU-001") {
		t.Fatalf("unexpected units response %d: %s", unitsRec.Code, unitsRec.Body.String())
	}
	stagesReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/stages", nil)
	stagesRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(stagesRec, stagesReq)
	if stagesRec.Code != http.StatusOK || !strings.Contains(stagesRec.Body.String(), `"next_stage":"conceptual_model"`) {
		t.Fatalf("source processing did not advance directly to conceptual model: %d %s", stagesRec.Code, stagesRec.Body.String())
	}
}

func TestSourceUnitReviewEndpointResolvesAttentionGate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server, testRoot := newTestServerWithRoot(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "Products have names.")
	revision = processSourcesWithMock(t, server, projectID, revision)
	state, _ := server.store.Project(projectID)
	artifacts, err := server.store.SourceUnitArtifacts(projectID)
	if err != nil {
		t.Fatalf("load source-unit artifacts: %v", err)
	}
	artifacts.QA.NeedsAttention = []string{"SU-001"}
	qaBytes, err := json.MarshalIndent(artifacts.QA, "", "  ")
	if err != nil {
		t.Fatalf("marshal QA fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testRoot, state.SourceUnitQAPath), qaBytes, 0o644); err != nil {
		t.Fatalf("write QA fixture: %v", err)
	}

	reviewReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/source-units/SU-001/review", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d,"decision":"revise","normalized_text":"Products have names.","note":"Confirmed deterministic normalization."}`, revision))))
	reviewRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(reviewRec, reviewReq)
	if reviewRec.Code != http.StatusOK || !strings.Contains(reviewRec.Body.String(), `"remaining_needs_attention":0`) || !strings.Contains(reviewRec.Body.String(), "Products have names.") {
		t.Fatalf("unexpected review response %d: %s", reviewRec.Code, reviewRec.Body.String())
	}

	stageReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/stages", nil)
	stageRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(stageRec, stageReq)
	if stageRec.Code != http.StatusOK || !strings.Contains(stageRec.Body.String(), `"next_stage":"conceptual_model"`) {
		t.Fatalf("conceptual stage was not unlocked: %d %s", stageRec.Code, stageRec.Body.String())
	}
}

func TestDeleteProjectEndpoint(t *testing.T) {
	server := newTestServer(t)
	projectID, _ := createProjectAndAddPastedText(t, server, "Product has a name.")

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+projectID, nil)
	deleteRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("expected delete status 200, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
	if !strings.Contains(deleteRec.Body.String(), projectID) {
		t.Fatalf("expected deleted project id in response: %s", deleteRec.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID, nil)
	getRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("expected deleted project to return 404, got %d: %s", getRec.Code, getRec.Body.String())
	}
}

func TestDeleteProjectEndpointRejectsActiveJob(t *testing.T) {
	server := newTestServer(t)
	projectID, _ := createProjectAndAddPastedText(t, server, "Product has a name.")
	started := make(chan struct{})
	release := make(chan struct{})
	server.jobs.StartWithRevision(projectID, "test", 0, nil, func(_ string, _ string, _ int, _ jobs.StepEmitter) (int, []string, error) {
		close(started)
		<-release
		return 0, nil, nil
	})
	<-started

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/projects/"+projectID, nil)
	deleteRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteRec, deleteReq)
	close(release)

	if deleteRec.Code != http.StatusConflict {
		t.Fatalf("expected active job conflict, got %d: %s", deleteRec.Code, deleteRec.Body.String())
	}
	if !strings.Contains(deleteRec.Body.String(), "project_busy") {
		t.Fatalf("expected project_busy error: %s", deleteRec.Body.String())
	}
}

func createProjectAndAddPastedText(t *testing.T, server *Server, text string) (string, int) {
	t.Helper()
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects", bytes.NewReader([]byte(`{"name":"Products","language":"en","domain":"catalog"}`)))
	createRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d: %s", createRec.Code, createRec.Body.String())
	}
	var created struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	addReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+created.Project.ID+"/resources", bytes.NewReader([]byte(fmt.Sprintf(`{
	  "kind": "pasted_text",
	  "title": "Task text",
	  "content": %q
	}`, text))))
	addRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusOK {
		t.Fatalf("expected add status 200, got %d: %s", addRec.Code, addRec.Body.String())
	}
	var added struct {
		ProjectRevision int `json:"project_revision"`
	}
	if err := json.Unmarshal(addRec.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode add response: %v", err)
	}
	return created.Project.ID, added.ProjectRevision
}

func processSourcesWithMock(t *testing.T, server *Server, projectID string, revision int) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/process-sources", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d,"mock":true,"model":"mock-model"}`, revision))))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("process sources returned %d: %s", rec.Code, rec.Body.String())
	}
	var started struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode process job: %v", err)
	}
	waitForTestJob(t, server, started.Job.ID)
	project, ok := server.store.Project(projectID)
	if !ok {
		t.Fatalf("project disappeared after processing")
	}
	return project.CurrentRevision
}

func waitForTestJob(t *testing.T, server *Server, jobID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	job, err := server.jobs.Wait(ctx, jobID)
	if err != nil {
		current, _ := server.jobs.Get(jobID)
		t.Fatalf("job %s did not finish: %v (last state: %+v)", jobID, err, current)
	}
	if job.Status != jobs.StatusCompleted {
		t.Fatalf("job %s reached %s: %s", jobID, job.Status, job.Error)
	}
}

func waitForFailedTestJob(t *testing.T, server *Server, jobID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	job, err := server.jobs.Wait(ctx, jobID)
	if err != nil {
		current, _ := server.jobs.Get(jobID)
		t.Fatalf("job %s did not finish: %v (last state: %+v)", jobID, err, current)
	}
	if job.Status != jobs.StatusFailed {
		t.Fatalf("job %s unexpectedly reached %s", jobID, job.Status)
	}
}

func waitForRunningTestJob(t *testing.T, server *Server, jobID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := server.jobs.Get(jobID)
		if !ok {
			t.Fatalf("job %s not found", jobID)
		}
		if job.Status == jobs.StatusRunning {
			return
		}
		if job.Status == jobs.StatusFailed || job.Status == jobs.StatusCompleted {
			t.Fatalf("job %s reached %s before running assertion", jobID, job.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not start", jobID)
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	server, _ := newTestServerWithRoot(t)
	return server
}

func newTestServerWithRoot(t *testing.T) (*Server, string) {
	t.Helper()
	root := t.TempDir()
	fixtureDir := filepath.Join(root, "poc", "printing_house_full", "v0.5_granularity_sentance")
	if _, err := scaffold.BundleFromText("Product has a name.", fixtureDir, scaffold.Options{ModelID: "fixture", Name: "Fixture"}); err != nil {
		t.Fatalf("create fixture bundle: %v", err)
	}
	server, err := New(Config{Root: root})
	if err != nil {
		t.Fatalf("create API server: %v", err)
	}
	return server, root
}
