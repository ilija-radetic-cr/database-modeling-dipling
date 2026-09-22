package api

import (
	"bytes"
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

func TestLLMPlanFromTaskWithMockReturnsValidBundle(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)
	payload := []byte(`{
	  "name": "Products",
	  "content": "System stores products in a catalog. Each product has a name.",
	  "mock": true,
	  "model": "mock-model"
	}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bundles/llm-plan-from-task", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Bundle struct {
			Name             string `json:"name"`
			ModelPath        string `json:"model_path"`
			BundlePath       string `json:"bundle_path"`
			SourceUnits      int    `json:"source_units"`
			Requirements     int    `json:"requirements"`
			Entities         int    `json:"entities"`
			ValidationErrors int    `json:"validation_errors"`
			DBMLStatus       string `json:"dbml_status"`
		} `json:"bundle"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Bundle.Name == "" || body.Bundle.ModelPath == "" || body.Bundle.BundlePath == "" {
		t.Fatalf("expected bundle paths and name, got %+v", body.Bundle)
	}
	if body.Bundle.Name != "Products LLM Draft" {
		t.Fatalf("expected bundle name from request, got %q", body.Bundle.Name)
	}
	if !strings.Contains(body.Bundle.BundlePath, "poc/generated/products_llm/v0.5") {
		t.Fatalf("expected stable LLM bundle path, got %q", body.Bundle.BundlePath)
	}
	if body.Bundle.SourceUnits == 0 || body.Bundle.Requirements == 0 || body.Bundle.Entities == 0 {
		t.Fatalf("expected populated bundle counts, got %+v", body.Bundle)
	}
	if body.Bundle.ValidationErrors != 0 || body.Bundle.DBMLStatus != "ready" {
		t.Fatalf("expected valid ready bundle, got %+v", body.Bundle)
	}
}

func TestLLMPlanFromTaskWithoutKeyRequiresMock(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)
	payload := []byte(`{"name":"Products","content":"Each product has a name.","mock":false}`)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bundles/llm-plan-from-task", bytes.NewReader(payload))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected status 412, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "llm_unavailable") {
		t.Fatalf("expected llm_unavailable error, got %s", rec.Body.String())
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

	deadline := time.Now().Add(2 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		job, ok := server.jobs.Get(started.Job.ID)
		if !ok {
			t.Fatalf("job not found")
		}
		if job.Status == "completed" {
			completed = true
			break
		}
		if job.Status == "failed" {
			t.Fatalf("job failed: %+v", job)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !completed {
		t.Fatalf("process-sources job did not complete")
	}

	docReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/combined-document", nil)
	docRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(docRec, docReq)
	if docRec.Code != http.StatusOK || !strings.Contains(docRec.Body.String(), "OD-S-0001") {
		t.Fatalf("expected combined document, got %d: %s", docRec.Code, docRec.Body.String())
	}
	if !strings.Contains(docRec.Body.String(), `"llm_assisted":true`) || strings.Contains(docRec.Body.String(), `"fallback_used":true`) {
		t.Fatalf("mock processing did not expose LLM-assisted segmentation: %s", docRec.Body.String())
	}
	segmentationReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/source-segmentation", nil)
	segmentationRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(segmentationRec, segmentationReq)
	if segmentationRec.Code != http.StatusOK || !strings.Contains(segmentationRec.Body.String(), `"candidates_total":1`) {
		t.Fatalf("expected segmentation audit artifact, got %d: %s", segmentationRec.Code, segmentationRec.Body.String())
	}
}

func TestCanonicalProcessSourcesStageRunsWithoutLLM(t *testing.T) {
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
	if runRec.Code != http.StatusAccepted {
		t.Fatalf("canonical process_sources stage returned %d: %s", runRec.Code, runRec.Body.String())
	}
	var started struct {
		Job struct {
			ID    string `json:"id"`
			Stage string `json:"stage"`
		} `json:"job"`
	}
	if err := json.Unmarshal(runRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode process job: %v", err)
	}
	if started.Job.Stage != "process_sources" {
		t.Fatalf("unexpected stage name: %+v", started.Job)
	}
	waitForTestJob(t, server, started.Job.ID)

	docReq := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+projectID+"/combined-document", nil)
	docRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(docRec, docReq)
	if docRec.Code != http.StatusOK || !strings.Contains(docRec.Body.String(), `"sentence_count":2`) {
		t.Fatalf("canonical stage did not sentence-segment the document: %d %s", docRec.Code, docRec.Body.String())
	}
	if !strings.Contains(docRec.Body.String(), `"fallback_used":true`) || !strings.Contains(docRec.Body.String(), `"segmentation_strategy":"deterministic_fallback"`) {
		t.Fatalf("LLM-free fallback is not visible in the combined-document summary: %s", docRec.Body.String())
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

func TestSourceUnitEndpointRunsRealRunnerAndReturnsQA(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server := newTestServer(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "Products have names.")
	revision = processSourcesWithMock(t, server, projectID, revision)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/source-units/generate", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d,"mock":true}`, revision))))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var started struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode job: %v", err)
	}
	waitForTestJob(t, server, started.Job.ID)

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
}

func TestSourceUnitReviewEndpointResolvesAttentionGate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	server, testRoot := newTestServerWithRoot(t)
	projectID, revision := createProjectAndAddPastedText(t, server, "Products have names.")
	revision = processSourcesWithMock(t, server, projectID, revision)

	generateReq := httptest.NewRequest(http.MethodPost, "/api/v1/projects/"+projectID+"/source-units/generate", bytes.NewReader([]byte(fmt.Sprintf(`{"base_revision":%d,"mock":true}`, revision))))
	generateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(generateRec, generateReq)
	if generateRec.Code != http.StatusAccepted {
		t.Fatalf("generate source units returned %d: %s", generateRec.Code, generateRec.Body.String())
	}
	var started struct {
		Job struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(generateRec.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode source-unit job: %v", err)
	}
	waitForTestJob(t, server, started.Job.ID)
	state, _ := server.store.Project(projectID)
	revision = state.CurrentRevision
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
	if stageRec.Code != http.StatusOK || !strings.Contains(stageRec.Body.String(), `"next_stage":"requirement_atoms"`) {
		t.Fatalf("requirement stage was not unlocked: %d %s", stageRec.Code, stageRec.Body.String())
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
	server.jobs.StartWithRunner(projectID, "test", nil, func(_ string, _ string, _ jobs.StepEmitter) (int, []string, error) {
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
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := server.jobs.Get(jobID)
		if !ok {
			t.Fatalf("job %s not found", jobID)
		}
		switch job.Status {
		case jobs.StatusCompleted:
			return
		case jobs.StatusFailed:
			t.Fatalf("job %s failed", jobID)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s did not finish", jobID)
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
