package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dbdsl/internal/jobs"
	"dbdsl/internal/llm"
	"dbdsl/internal/workspace"
)

// Keep the stage deadline above the default OpenAI HTTP timeout so that the
// client can report a precise provider error instead of being preempted by the
// surrounding background job context.
const defaultLLMStageTimeout = 12 * time.Minute

type Server struct {
	store *workspace.Store
	jobs  *jobs.Manager
}

type Config struct {
	Root string
}

func New(config Config) (*Server, error) {
	store, err := workspace.NewStore(config.Root)
	if err != nil {
		return nil, err
	}
	server := &Server{store: store}
	server.jobs = jobs.NewPersistentManager(store.ApplyJobResult, filepath.Join(config.Root, ".dbdsl_workbench", "jobs.json"))
	return server, nil
}

func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serveHTTP)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/api/v1/health" {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "time": time.Now()})
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v1/") {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	segments := splitPath(strings.TrimPrefix(r.URL.Path, "/api/v1/"))
	if len(segments) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"name": "DB Model Workbench API", "version": "v1"})
		return
	}

	if segments[0] == "llm" && len(segments) == 2 && segments[1] == "status" {
		if r.Method != http.MethodGet {
			writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
			return
		}
		s.handleLLMStatus(w, r)
		return
	}

	if segments[0] == "bundles" {
		if len(segments) == 1 {
			if r.Method != http.MethodGet {
				writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
				return
			}
			s.handleListBundles(w, r)
			return
		}
		if len(segments) == 2 && segments[1] == "scaffold-from-task" {
			if r.Method != http.MethodPost {
				writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
				return
			}
			s.handleScaffoldBundleFromTask(w, r)
			return
		}
		if len(segments) == 2 && segments[1] == "llm-plan-from-task" {
			if r.Method != http.MethodPost {
				writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
				return
			}
			s.handleLLMPlanBundleFromTask(w, r)
			return
		}
	}

	if segments[0] == "projects" && len(segments) == 2 && segments[1] == "import-bundle" {
		if r.Method != http.MethodPost {
			writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
			return
		}
		s.handleImportBundle(w, r)
		return
	}

	if segments[0] == "projects" && len(segments) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.handleListProjects(w, r)
		case http.MethodPost:
			s.handleCreateProject(w, r)
		default:
			writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		}
		return
	}

	if segments[0] != "projects" || len(segments) < 2 {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	projectID := segments[1]
	rest := segments[2:]

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			s.handleGetProject(w, r, projectID)
		case http.MethodPatch:
			s.handlePatchProject(w, r, projectID)
		case http.MethodDelete:
			s.handleDeleteProject(w, r, projectID)
		default:
			writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		}
		return
	}

	switch rest[0] {
	case "complete":
		s.handleCompleteProject(w, r, projectID)
	case "reopen":
		s.handleReopenProject(w, r, projectID)
	case "resources":
		s.handleResources(w, r, projectID, rest[1:])
	case "process-sources":
		s.handleProcessSources(w, r, projectID)
	case "source-manifest":
		s.handleSourceManifest(w, r, projectID)
	case "combined-document":
		s.handleCombinedDocument(w, r, projectID)
	case "source-fidelity":
		s.handleSourceFidelity(w, r, projectID)
	case "source-segmentation":
		s.handleSourceSegmentation(w, r, projectID)
	case "stages":
		s.handleStages(w, r, projectID, rest[1:])
	case "analysis":
		s.handleAnalysis(w, r, projectID, rest[1:])
	case "source-units":
		s.handleSourceUnits(w, r, projectID, rest[1:])
	case "examples":
		s.handleExamples(w, r, projectID)
	case "requirements":
		s.handleRequirements(w, r, projectID, rest[1:])
	case "design-obligations":
		s.handleDesignObligations(w, r, projectID)
	case "semantic-verification":
		s.handleSemanticVerification(w, r, projectID)
	case "functional-areas":
		s.handleFunctionalAreas(w, r, projectID)
	case "actors":
		s.handleActors(w, r, projectID)
	case "crud-operations":
		s.handleCrudOperations(w, r, projectID)
	case "review-candidates":
		s.handleReviewCandidates(w, r, projectID, rest[1:])
	case "review-decisions":
		s.handleReviewDecisions(w, r, projectID, rest[1:])
	case "conceptual-model":
		s.handleConceptualModel(w, r, projectID, rest[1:])
	case "model-acceptance":
		s.handleModelAcceptance(w, r, projectID)
	case "model-corrections":
		s.handleModelCorrection(w, r, projectID)
	case "model-generation":
		s.handleModelGeneration(w, r, projectID, rest[1:])
	case "generate-model":
		writeError(w, r, http.StatusConflict, "granular_model_pipeline_required", "Generate the conceptual_model and logical_model stages explicitly.", nil)
	case "model-graph":
		s.handleModelGraph(w, r, projectID)
	case "trace-index":
		s.handleTraceIndex(w, r, projectID)
	case "model-elements":
		s.handleModelElement(w, r, projectID, rest[1:])
	case "source-spans":
		s.handleSourceSpan(w, r, projectID, rest[1:])
	case "quality":
		s.handleQuality(w, r, projectID, rest[1:])
	case "dbml":
		s.handleDBML(w, r, projectID, rest[1:])
	case "exports":
		s.handleExports(w, r, projectID, rest[1:])
	case "jobs":
		s.handleJobs(w, r, projectID, rest[1:])
	case "llm-runs":
		s.handleLLMRuns(w, r, projectID, rest[1:])
	case "optimization-report":
		s.handleOptimizationReport(w, r, projectID)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
	}
}

func (s *Server) handleStages(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		health, err := s.store.ArtifactHealth(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"artifact_health": health, "next_stage": nextProjectStage(health)})
		return
	}
	if len(rest) != 2 || rest[1] != "run" || r.Method != http.MethodPost {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	if s.jobs.HasActiveProject(projectID) {
		writeError(w, r, http.StatusConflict, "project_busy", "Another project job is active.", nil)
		return
	}
	var req struct {
		BaseRevision    int    `json:"base_revision"`
		Model           string `json:"model"`
		ReasoningEffort string `json:"reasoning_effort"`
		MaxOutputTokens int    `json:"max_output_tokens"`
		Mock            bool   `json:"mock"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	project, ok := s.store.Project(projectID)
	if !ok {
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	if req.BaseRevision > 0 && req.BaseRevision != project.CurrentRevision {
		writeMappedError(w, r, workspace.ErrRevisionConflict)
		return
	}
	stage := rest[0]
	client, clientErr := llmClient(req.Mock)
	needsLLM := stage != "process_sources" && stage != "generate_outputs" && stage != "validation_lint" && stage != "semantic_verification"
	if clientErr != nil && stage == "review_candidates" {
		if reviewNeedsLLM, gateErr := s.store.ReviewCandidateGenerationNeedsLLM(projectID); gateErr == nil && !reviewNeedsLLM {
			needsLLM = false
		}
	}
	if clientErr != nil && needsLLM {
		writeError(w, r, http.StatusPreconditionFailed, "llm_unavailable", clientErr.Error(), nil)
		return
	}
	if clientErr != nil {
		client = nil
	}
	options := workspace.AnalysisStageOptions{
		BaseRevision: req.BaseRevision, Model: req.Model, ReasoningEffort: req.ReasoningEffort, MaxOutputTokens: req.MaxOutputTokens,
	}
	var runner jobs.Runner
	var steps []string
	switch stage {
	case "process_sources":
		steps = processSourcesSteps()
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			return s.store.ProcessSourcesWithLLM(ctx, client, projectID, workspace.ProcessSourcesOptions{
				BaseRevision: req.BaseRevision, Model: req.Model, ReasoningEffort: req.ReasoningEffort,
				MaxOutputTokens: req.MaxOutputTokens, OnProgress: emit,
			})
		}
	case "source_units":
		steps = []string{"extract_source_units", "validate_source_units", "write_source_units"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			return s.store.GenerateSourceUnits(ctx, client, projectID, workspace.GenerateSourceUnitsOptions{
				BaseRevision: req.BaseRevision, Model: req.Model, ReasoningEffort: req.ReasoningEffort,
				MaxOutputTokens: req.MaxOutputTokens, OnProgress: emit,
			})
		}
	case "requirement_atoms":
		steps = []string{"extract_requirement_atoms", "derive_design_obligations", "validate_requirement_atoms", "write_requirement_atoms"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			options.OnProgress = emit
			return s.store.GenerateRequirementAtoms(ctx, client, projectID, options)
		}
	case "functional_analysis":
		steps = []string{"build_functional_analysis", "validate_functional_analysis", "write_functional_analysis"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			options.OnProgress = emit
			return s.store.GenerateFunctionalAnalysis(ctx, client, projectID, options)
		}
	case "crud_mapping":
		steps = []string{"build_crud_mapping", "validate_crud_mapping", "write_crud_mapping"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			options.OnProgress = emit
			return s.store.GenerateCRUDMapping(ctx, client, projectID, options)
		}
	case "review_candidates":
		steps = []string{"propose_review_candidates", "validate_review_dag", "write_review_candidates"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			options.OnProgress = emit
			return s.store.GenerateReviewCandidates(ctx, client, projectID, options)
		}
	case "conceptual_model":
		steps = []string{"propose_conceptual_model", "validate_conceptual_model", "write_conceptual_model"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			return s.store.GenerateConceptualModel(ctx, client, projectID, workspace.ModelStageOptions{
				BaseRevision: req.BaseRevision, Model: req.Model, ReasoningEffort: req.ReasoningEffort,
				MaxOutputTokens: req.MaxOutputTokens, OnProgress: emit,
			})
		}
	case "logical_model":
		steps = []string{"project_logical_model", "write_logical_bundle", "validate", "lint"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			return s.store.GenerateLogicalModel(ctx, client, projectID, workspace.ModelStageOptions{
				BaseRevision: req.BaseRevision, Model: req.Model, ReasoningEffort: req.ReasoningEffort,
				MaxOutputTokens: req.MaxOutputTokens, OnProgress: emit,
			})
		}
	case "semantic_verification":
		steps = []string{"map_obligations", "verify_obligations", "write_semantic_report"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			return s.store.RunSemanticVerification(projectID, req.BaseRevision, emit)
		}
	case "generate_outputs":
		steps = []string{"generate_dbml", "generate_trace", "refresh_quality"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			return s.store.GenerateFinalOutputs(projectID, req.BaseRevision, emit)
		}
	case "validation_lint":
		steps = []string{"validate", "lint", "quality"}
		runner = func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			return s.store.RefreshModelQuality(projectID, req.BaseRevision, emit)
		}
	default:
		writeError(w, r, http.StatusBadRequest, "invalid_stage", "Unsupported project stage.", map[string]any{"stage": stage})
		return
	}
	job := s.jobs.StartWithRevision(projectID, stage, req.BaseRevision, steps, runner)
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}

func (s *Server) handleDesignObligations(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	artifacts, err := s.store.DesignObligations(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	preview, previewQA, previewErr := s.store.DesignObligationMigrationPreview(projectID)
	response := map[string]any{"design_obligations": artifacts.Accepted.DesignObligations, "qa": artifacts.QA}
	if previewErr == nil {
		response["migration_preview"] = preview
		response["migration_preview_qa"] = previewQA
	}
	if metrics, metricsErr := s.store.ConceptualOptimizationPreview(projectID); metricsErr == nil {
		response["conceptual_context_preview"] = metrics
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSemanticVerification(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	report, err := s.store.SemanticVerification(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"semantic_verification": report})
}

func nextProjectStage(health workspace.ArtifactHealth) string {
	if health.SemanticVerificationStatus == "legacy_not_applicable" && health.FinalModelAccepted && health.DBMLStatus == "ready" {
		return "completed"
	}
	if health.CombinedDocumentStatus != "ready" {
		return "process_sources"
	}
	if health.SourceFidelityStatus != "ready" {
		return "process_sources"
	}
	if health.SourceUnitsStatus == "not_generated" {
		return "source_units"
	}
	if health.SourceUnitsStatus == "needs_attention" {
		return "source_review"
	}
	if health.RequirementAtomsStatus != "ready" {
		return "requirement_atoms"
	}
	if health.DesignObligationsStatus != "ready" {
		return "requirement_atoms"
	}
	if health.FunctionalAnalysisStatus != "ready" {
		return "functional_analysis"
	}
	if health.CRUDMappingStatus != "ready" {
		return "crud_mapping"
	}
	if health.ReviewCandidatesStatus != "ready" {
		return "review_candidates"
	}
	if health.OpenReviewQuestions > 0 {
		return "review_decisions"
	}
	if health.ConceptualModelStatus != "ready" {
		if health.ConceptualModelStatus == "proposed" {
			return "conceptual_review"
		}
		return "conceptual_model"
	}
	if health.ModelStatus != "ready" {
		return "logical_model"
	}
	if health.SemanticVerificationStatus != "passed" {
		return "semantic_verification"
	}
	if !health.FinalModelAccepted {
		return "model_review"
	}
	if health.DBMLStatus != "ready" {
		return "generate_outputs"
	}
	return "completed"
}

func (s *Server) handleConceptualModel(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 1 && rest[0] == "accept" {
		if r.Method != http.MethodPost {
			writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
			return
		}
		var req struct {
			BaseRevision int `json:"base_revision"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		revision, err := s.store.AcceptConceptualModel(projectID, req.BaseRevision)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"project_revision": revision, "message": "Conceptual model accepted."})
		return
	}
	if len(rest) != 0 || r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	artifacts, err := s.store.ConceptualModel(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"conceptual_model": artifacts.Proposed, "accepted": artifacts.IsAccepted, "diff": artifacts.Diff, "qa": artifacts.QA})
}

func (s *Server) handleModelAcceptance(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	var req struct {
		BaseRevision int `json:"base_revision"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	revision, err := s.store.AcceptFinalModel(projectID, req.BaseRevision)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": revision, "message": "Final model accepted."})
}

func (s *Server) handleModelCorrection(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	var req struct {
		BaseRevision   int    `json:"base_revision"`
		ElementID      string `json:"element_id"`
		CorrectionType string `json:"correction_type"`
		Note           string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	revision, candidateID, err := s.store.CreateModelCorrectionCandidate(projectID, workspace.ModelCorrectionRequest{
		BaseRevision: req.BaseRevision, ElementID: req.ElementID, CorrectionType: req.CorrectionType, Note: req.Note,
	})
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": revision, "review_candidate_id": candidateID, "message": "Model correction was added to the review queue."})
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	status := nonEmpty(r.URL.Query().Get("status"), "active")
	projects, err := s.store.ListProjects(status, r.URL.Query().Get("search"))
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": projects, "page": map[string]any{"limit": 50, "next_cursor": nil}})
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Language    string `json:"language"`
		Domain      string `json:"domain"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	project, err := s.store.CreateProject(req.Name, req.Description, req.Language, req.Domain)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "validation_failed", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project})
}

func (s *Server) handleListBundles(w http.ResponseWriter, r *http.Request) {
	bundles, err := s.store.DiscoverBundles()
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": bundles})
}

func (s *Server) handleLLMStatus(w http.ResponseWriter, r *http.Request) {
	_ = r
	writeJSON(w, http.StatusOK, map[string]any{
		"available":        strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "",
		"default_model":    llm.DefaultModel,
		"mock_available":   true,
		"provider":         "openai",
		"pipeline_version": "0.7.2",
	})
}

func (s *Server) handleScaffoldBundleFromTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	bundle, err := s.store.ScaffoldBundleFromText(req.Name, req.Content)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bundle": bundle})
}

func (s *Server) handleLLMPlanBundleFromTask(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name              string `json:"name"`
		Content           string `json:"content"`
		Model             string `json:"model"`
		ReasoningEffort   string `json:"reasoning_effort"`
		MaxOutputTokens   int    `json:"max_output_tokens"`
		MaxRepairAttempts int    `json:"max_repair_attempts"`
		Mock              bool   `json:"mock"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	client, err := llmClient(req.Mock)
	if err != nil {
		writeError(w, r, http.StatusPreconditionFailed, "llm_unavailable", err.Error(), nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), defaultLLMStageTimeout)
	defer cancel()
	bundle, err := s.store.LLMPlanBundleFromText(ctx, client, workspace.LLMPlanFromTextOptions{
		Name:              req.Name,
		Content:           req.Content,
		Model:             req.Model,
		ReasoningEffort:   req.ReasoningEffort,
		MaxOutputTokens:   req.MaxOutputTokens,
		MaxRepairAttempts: req.MaxRepairAttempts,
	})
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bundle": bundle})
}

func llmClient(mock bool) (llm.Client, error) {
	if mock {
		return llm.NewDefaultMockClient(), nil
	}
	return llm.NewOpenAIClientFromEnv()
}

func (s *Server) handleImportBundle(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	project, err := s.store.ImportBundle(req.Path)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project})
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request, projectID string) {
	project, err := s.store.ProjectSummary(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	health, err := s.store.ArtifactHealth(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project, "artifact_health": health})
}

func (s *Server) handlePatchProject(w http.ResponseWriter, r *http.Request, projectID string) {
	var req struct {
		BaseRevision int    `json:"base_revision"`
		Name         string `json:"name"`
		Description  string `json:"description"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.UpdateProject(projectID, req.BaseRevision, req.Name, req.Description); err != nil {
		writeMappedError(w, r, err)
		return
	}
	project, _ := s.store.ProjectSummary(projectID)
	writeMutationResult(w, project.CurrentRevision, []string{"project"}, nil, "Project updated.")
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request, projectID string) {
	if s.jobs.HasActiveProject(projectID) {
		writeError(w, r, http.StatusConflict, "project_busy", "Project cannot be deleted while a job is active.", nil)
		return
	}
	if err := s.store.DeleteProject(projectID); err != nil {
		writeMappedError(w, r, err)
		return
	}
	s.jobs.ForgetProject(projectID)
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted_project_id": projectID,
		"message":            "Project deleted.",
	})
}

func (s *Server) handleCompleteProject(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	var req struct {
		BaseRevision int `json:"base_revision"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	project, snapshot, err := s.store.CompleteProject(projectID, req.BaseRevision)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project, "completed_snapshot": snapshot})
}

func (s *Server) handleReopenProject(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	var req struct {
		Mode string `json:"mode"`
		Note string `json:"note"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	project, sourceSnapshotID, err := s.store.ReopenProject(projectID, req.Note)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project": project, "source_snapshot_id": sourceSnapshotID})
}

func (s *Server) handleResources(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			resources, err := s.store.Resources(projectID)
			if err != nil {
				writeMappedError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"items": resources})
		case http.MethodPost:
			var req struct {
				Kind         string `json:"kind"`
				Title        string `json:"title"`
				Content      string `json:"content"`
				BaseRevision int    `json:"base_revision"`
			}
			if !decodeJSON(w, r, &req) {
				return
			}
			resource, revision, err := s.store.AddPastedTextResource(projectID, req.BaseRevision, req.Title, req.Content)
			if err != nil {
				writeMappedError(w, r, err)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"resource": resource, "project_revision": revision})
		default:
			writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		}
		return
	}
	if rest[0] == "upload" && r.Method == http.MethodPost {
		s.handleUploadResource(w, r, projectID)
		return
	}
	if len(rest) == 2 && rest[1] == "text" && r.Method == http.MethodGet {
		text, resource, err := s.store.ResourceText(projectID, rest[0])
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resource": resource, "text": text})
		return
	}
	if len(rest) == 1 && r.Method == http.MethodGet {
		resource, found, err := s.store.Resource(projectID, rest[0])
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		if !found {
			writeMappedError(w, r, workspace.ErrNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"resource": resource})
		return
	}
	if len(rest) == 1 && r.Method == http.MethodDelete {
		baseRevision, err := optionalInt(r.URL.Query().Get("base_revision"))
		if err != nil {
			writeError(w, r, http.StatusBadRequest, "bad_request", "base_revision must be an integer.", nil)
			return
		}
		revision, err := s.store.DeleteResource(projectID, baseRevision, rest[0])
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeMutationResult(w, revision, []string{"resources"}, []string{"source_units", "requirements", "functional_crud", "review_candidates", "model", "dbml"}, "Resource removed.")
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
}

func (s *Server) handleUploadResource(w http.ResponseWriter, r *http.Request, projectID string) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, r, http.StatusBadRequest, "bad_request", "Invalid multipart upload.", nil)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "bad_request", "file is required.", nil)
		return
	}
	defer file.Close()
	baseRevision, err := optionalInt(r.FormValue("base_revision"))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "bad_request", "base_revision must be an integer.", nil)
		return
	}
	resource, revision, err := s.store.AddUploadedResource(projectID, baseRevision, r.FormValue("title"), header.Filename, "", file)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"resource": resource, "project_revision": revision})
}

func (s *Server) handleProcessSources(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodPost {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	var req struct {
		BaseRevision    int    `json:"base_revision"`
		Model           string `json:"model"`
		ReasoningEffort string `json:"reasoning_effort"`
		MaxOutputTokens int    `json:"max_output_tokens"`
		Mock            bool   `json:"mock"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	project, ok := s.store.Project(projectID)
	if !ok {
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	if req.BaseRevision > 0 && project.CurrentRevision != req.BaseRevision {
		writeMappedError(w, r, workspace.ErrRevisionConflict)
		return
	}
	if s.jobs.HasActiveProject(projectID) {
		writeError(w, r, http.StatusConflict, "project_busy", "Another project job is active.", nil)
		return
	}
	client, _ := llmClient(req.Mock)
	steps := processSourcesSteps()
	job := s.jobs.StartWithRevision(projectID, "process_sources", req.BaseRevision, steps, func(projectID string, _ string, emit jobs.StepEmitter) (int, []string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
		defer cancel()
		return s.store.ProcessSourcesWithLLM(ctx, client, projectID, workspace.ProcessSourcesOptions{
			BaseRevision:    req.BaseRevision,
			Model:           req.Model,
			ReasoningEffort: req.ReasoningEffort,
			MaxOutputTokens: req.MaxOutputTokens,
			OnProgress:      emit,
		})
	})
	writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
}

func processSourcesSteps() []string {
	return []string{"load_extracted_resources", "write_source_manifest", "build_source_segments", "propose_source_segmentation", "validate_source_segmentation", "validate_source_fidelity", "write_combined_document"}
}

func (s *Server) handleSourceFidelity(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	report, err := s.store.SourceFidelity(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source_fidelity": report})
}

func (s *Server) handleSourceSegmentation(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	proposal, qa, err := s.store.SourceSegmentation(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"proposal": proposal, "qa": qa})
}

func (s *Server) handleSourceManifest(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	if r.URL.Query().Get("format") == "yaml" || strings.Contains(r.Header.Get("Accept"), "yaml") {
		manifest, err := s.store.SourceManifestYAML(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(manifest))
		return
	}
	manifest, err := s.store.SourceManifest(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": manifest})
}

func (s *Server) handleCombinedDocument(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	document, err := s.store.CombinedDocument(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"combined_document": document})
}

func (s *Server) handleAnalysis(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 1 && rest[0] == "summary" && r.Method == http.MethodGet {
		project, err := s.store.ProjectSummary(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		_, coverage, err := s.store.Requirements(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		summary := map[string]any{
			"source_units": map[string]any{
				"total":           project.Counts.SourceUnits,
				"needs_attention": 0,
				"model_relevant":  project.Counts.SourceUnits,
				"non_model":       0,
			},
			"examples": map[string]any{
				"total":          project.Counts.Examples,
				"needs_decision": 0,
				"normative":      1,
				"illustrative":   1,
			},
			"requirements": map[string]any{
				"total":                project.Counts.Requirements,
				"needs_review":         coverage["requirements_needing_review"],
				"direct_db":            coverage["direct_db_requirements"],
				"non_model":            coverage["non_model_requirements"],
				"source_units_covered": coverage["source_units_covered"],
			},
			"functional_crud": map[string]any{
				"functional_areas": project.Counts.FunctionalAreas,
				"operations":       project.Counts.Operations,
				"actors":           8,
			},
			"review": map[string]any{
				"open_questions":     project.Counts.OpenReviewQuestions,
				"answered_questions": project.Counts.ReviewDecisions,
			},
			"can_generate_model": project.Counts.OpenReviewQuestions == 0,
		}
		writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "summary": summary})
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
}

func (s *Server) handleSourceUnits(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 1 && rest[0] == "generate" && r.Method == http.MethodPost {
		var req struct {
			BaseRevision    int    `json:"base_revision"`
			Model           string `json:"model"`
			ReasoningEffort string `json:"reasoning_effort"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			Mock            bool   `json:"mock"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		project, ok := s.store.Project(projectID)
		if !ok {
			writeMappedError(w, r, workspace.ErrNotFound)
			return
		}
		if req.BaseRevision > 0 && project.CurrentRevision != req.BaseRevision {
			writeMappedError(w, r, workspace.ErrRevisionConflict)
			return
		}
		client, clientErr := llmClient(req.Mock)
		if clientErr != nil {
			writeError(w, r, http.StatusPreconditionFailed, "llm_unavailable", clientErr.Error(), nil)
			return
		}
		job := s.jobs.StartWithRevision(projectID, "source_units", req.BaseRevision, []string{"extract_source_units", "validate_source_units", "write_source_units"}, func(projectID string, _ string, emit jobs.StepEmitter) (int, []string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
			defer cancel()
			return s.store.GenerateSourceUnits(ctx, client, projectID, workspace.GenerateSourceUnitsOptions{
				BaseRevision:    req.BaseRevision,
				Model:           req.Model,
				ReasoningEffort: req.ReasoningEffort,
				MaxOutputTokens: req.MaxOutputTokens,
				OnProgress:      emit,
			})
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
		return
	}
	if len(rest) == 1 && (rest[0] == "qa" || rest[0] == "proposed") && r.Method == http.MethodGet {
		artifacts, err := s.store.SourceUnitArtifacts(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		if rest[0] == "qa" {
			writeJSON(w, http.StatusOK, map[string]any{"qa": artifacts.QA})
		} else {
			writeJSON(w, http.StatusOK, map[string]any{"proposal": artifacts.Proposal})
		}
		return
	}
	if len(rest) == 2 && rest[1] == "review" && r.Method == http.MethodPost {
		var req struct {
			BaseRevision   int    `json:"base_revision"`
			Decision       string `json:"decision"`
			NormalizedText string `json:"normalized_text"`
			Note           string `json:"note"`
			ReviewedBy     string `json:"reviewed_by"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		revision, remaining, err := s.store.ReviewSourceUnit(projectID, rest[0], workspace.ReviewSourceUnitOptions{
			BaseRevision: req.BaseRevision, Decision: req.Decision, NormalizedText: req.NormalizedText,
			Note: req.Note, ReviewedBy: req.ReviewedBy,
		})
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		unit, _, err := s.store.SourceUnit(projectID, rest[0])
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"project_revision": revision, "source_unit": unit, "remaining_needs_attention": remaining,
		})
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	if len(rest) == 0 {
		project, err := s.store.ProjectSummary(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		units, err := s.store.SourceUnits(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		units = filterSourceUnits(units, r.URL.Query().Get("filter"), r.URL.Query().Get("search"))
		writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "items": units, "page": map[string]any{"limit": len(units), "next_cursor": nil}})
		return
	}
	unit, found, err := s.store.SourceUnit(projectID, rest[0])
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	if !found {
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source_unit": unit, "original_excerpt": map[string]any{"resource_id": "R-001", "title": "Printing House task", "text": unit.ExactText}})
}

func (s *Server) handleExamples(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	project, err := s.store.ProjectSummary(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "items": s.store.Examples(projectID)})
}

func (s *Server) handleRequirements(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	requirements, coverage, err := s.store.Requirements(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	if len(rest) == 0 {
		project, _ := s.store.ProjectSummary(projectID)
		requirements = filterRequirements(requirements, r.URL.Query().Get("filter"), r.URL.Query().Get("search"))
		writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "coverage": coverage, "items": requirements, "page": map[string]any{"limit": len(requirements), "next_cursor": nil}})
		return
	}
	for _, req := range requirements {
		if req.ID == rest[0] {
			writeJSON(w, http.StatusOK, map[string]any{"requirement": req, "source_evidence": req.SourceUnits})
			return
		}
	}
	writeMappedError(w, r, workspace.ErrNotFound)
}

func (s *Server) handleFunctionalAreas(w http.ResponseWriter, r *http.Request, projectID string) {
	items, err := s.store.FunctionalAreas(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	project, _ := s.store.ProjectSummary(projectID)
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "items": items})
}

func (s *Server) handleActors(w http.ResponseWriter, r *http.Request, projectID string) {
	items, err := s.store.Actors(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	project, _ := s.store.ProjectSummary(projectID)
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "items": items})
}

func (s *Server) handleCrudOperations(w http.ResponseWriter, r *http.Request, projectID string) {
	items, err := s.store.CrudOperations(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	project, _ := s.store.ProjectSummary(projectID)
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "items": items})
}

func (s *Server) handleReviewCandidates(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		items, err := s.store.ReviewCandidates(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		project, _ := s.store.ProjectSummary(projectID)
		writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "items": items, "page": map[string]any{"limit": len(items), "next_cursor": nil}})
		return
	}
	if len(rest) == 1 && rest[0] == "dependency-graph" && r.Method == http.MethodGet {
		items, err := s.store.ReviewCandidates(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": items, "edges": dependencyEdges(items)})
		return
	}
	if len(rest) == 2 && rest[1] == "answer" && r.Method == http.MethodPost {
		var req struct {
			BaseRevision    int    `json:"base_revision"`
			SelectedOption  string `json:"selected_option"`
			ReviewedBy      string `json:"reviewed_by"`
			Model           string `json:"model"`
			ReasoningEffort string `json:"reasoning_effort"`
			MaxOutputTokens int    `json:"max_output_tokens"`
			Mock            bool   `json:"mock"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		project, ok := s.store.Project(projectID)
		if !ok {
			writeMappedError(w, r, workspace.ErrNotFound)
			return
		}
		if project.ReviewCandidatesPath != "" {
			client, _ := llmClient(req.Mock)
			job := s.jobs.StartWithRevision(projectID, "apply_review_decision", req.BaseRevision, []string{"apply_review_decision", "validate_review_patch", "write_review_revision"}, func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
				ctx, cancel := context.WithTimeout(context.Background(), defaultLLMStageTimeout)
				defer cancel()
				return s.store.ApplyProjectReviewDecision(ctx, client, projectID, rest[0], workspace.ApplyReviewDecisionOptions{
					BaseRevision: req.BaseRevision, SelectedOption: req.SelectedOption, ReviewedBy: req.ReviewedBy,
					Model: req.Model, ReasoningEffort: req.ReasoningEffort, MaxOutputTokens: req.MaxOutputTokens, OnProgress: emit,
				})
			})
			writeJSON(w, http.StatusAccepted, map[string]any{"project_revision": project.CurrentRevision, "job": job})
			return
		}
		job := s.jobs.StartWithRevision(projectID, "apply_review_decision", req.BaseRevision, []string{"apply_review_decision"}, func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			emit("apply_review_decision", "Applying the selected legacy fixture decision.", 60, nil)
			revision, err := s.store.AnswerReview(projectID, rest[0], req.SelectedOption, req.BaseRevision)
			return revision, []string{"review_decisions"}, err
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"project_revision": project.CurrentRevision, "job": job})
		return
	}
	if len(rest) == 1 && rest[0] == "apply-recommended" && r.Method == http.MethodPost {
		var req struct {
			BaseRevision int `json:"base_revision"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		if project, ok := s.store.Project(projectID); ok && project.ReviewCandidatesPath != "" {
			writeError(w, r, http.StatusConflict, "blocking_bulk_apply_disabled", "Project-specific blocking decisions must be answered individually.", nil)
			return
		}
		job := s.jobs.StartWithRevision(projectID, "apply_review_decision", req.BaseRevision, []string{"apply_review_decision"}, func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			emit("apply_review_decision", "Applying recommended non-production fixture decisions.", 60, nil)
			revision, err := s.store.ApplyRecommended(projectID, req.BaseRevision)
			return revision, []string{"review_decisions"}, err
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"project_revision": req.BaseRevision, "job": job})
		return
	}
	if len(rest) == 1 && r.Method == http.MethodGet {
		items, err := s.store.ReviewCandidates(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		for _, item := range items {
			if item.ID == rest[0] {
				writeJSON(w, http.StatusOK, map[string]any{"review_candidate": item})
				return
			}
		}
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
}

func (s *Server) handleReviewDecisions(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 1 && rest[0] == "batch" && r.Method == http.MethodPost {
		if s.jobs.HasActiveProject(projectID) {
			writeError(w, r, http.StatusConflict, "project_busy", "Another project job is active.", nil)
			return
		}
		var req struct {
			BaseRevision   int                         `json:"base_revision"`
			Selections     []workspace.ReviewSelection `json:"selections"`
			ReviewedBy     string                      `json:"reviewed_by"`
			ActiveReviewMS int64                       `json:"active_review_ms"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		job := s.jobs.StartWithRevision(projectID, "apply_review_decision_batch", req.BaseRevision, []string{"validate_review_batch", "apply_review_batch", "write_review_revision"}, func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			emit("validate_review_batch", "Validating structured review selections and dependencies.", 25, map[string]any{"selections": len(req.Selections), "llm_call": false})
			revision, updated, err := s.store.ApplyProjectReviewDecisionBatch(projectID, workspace.ApplyReviewBatchOptions{BaseRevision: req.BaseRevision, Selections: req.Selections, ReviewedBy: req.ReviewedBy, DecisionMode: "manual_batch", ActiveReviewMS: req.ActiveReviewMS})
			if err != nil {
				return revision, updated, err
			}
			emit("apply_review_batch", "Applied review decisions deterministically.", 80, map[string]any{"selections": len(req.Selections), "llm_call": false})
			return revision, updated, nil
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
		return
	}
	if len(rest) != 0 || r.Method != http.MethodGet {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	items, err := s.store.ReviewDecisions(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	project, _ := s.store.ProjectSummary(projectID)
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "items": items})
}

func (s *Server) handleModelGeneration(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 1 && rest[0] == "readiness" && r.Method == http.MethodGet {
		project, err := s.store.ProjectSummary(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		health, _ := s.store.ArtifactHealth(projectID)
		writeJSON(w, http.StatusOK, map[string]any{
			"project_revision": project.CurrentRevision,
			"readiness": map[string]any{
				"can_generate_model":    health.CanGenerateModel,
				"open_review_questions": health.OpenReviewQuestions,
				"blocking_reasons":      blockingReasons(health),
			},
		})
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
}

func (s *Server) handleModelGraph(w http.ResponseWriter, r *http.Request, projectID string) {
	graph, err := s.store.ModelGraph(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	project, _ := s.store.ProjectSummary(projectID)
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "model_graph": graph})
}

func (s *Server) handleTraceIndex(w http.ResponseWriter, r *http.Request, projectID string) {
	idx, err := s.store.TraceIndex(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	project, _ := s.store.ProjectSummary(projectID)
	writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "trace_index": idx})
}

func (s *Server) handleModelElement(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) != 1 || r.Method != http.MethodGet {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	details, found, err := s.store.ElementDetails(projectID, rest[0])
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	if !found {
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"model_element": details})
}

func (s *Server) handleSourceSpan(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) != 1 || r.Method != http.MethodGet {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	unit, found, err := s.store.SourceUnit(projectID, rest[0])
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	if !found {
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"source_span": map[string]any{"source_unit": unit, "text": unit.ExactText}})
}

func (s *Server) handleQuality(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		report, err := s.store.Quality(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		project, _ := s.store.ProjectSummary(projectID)
		writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "quality": report})
		return
	}
	if len(rest) == 1 && rest[0] == "run" && r.Method == http.MethodPost {
		project, ok := s.store.Project(projectID)
		if !ok {
			writeMappedError(w, r, workspace.ErrNotFound)
			return
		}
		job := s.jobs.StartWithRevision(projectID, "validation_lint", project.CurrentRevision, []string{"validate", "lint", "quality"}, func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			return s.store.RefreshModelQuality(projectID, project.CurrentRevision, emit)
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
		return
	}
	if len(rest) == 3 && rest[0] == "issues" && rest[2] == "accept" && r.Method == http.MethodPost {
		revision, err := s.store.AcceptQualityIssue(projectID, rest[1])
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeMutationResult(w, revision, []string{"quality"}, nil, "Quality issue accepted.")
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
}

func (s *Server) handleDBML(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		dbml, err := s.store.DBML(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		project, _ := s.store.ProjectSummary(projectID)
		writeJSON(w, http.StatusOK, map[string]any{"project_revision": project.CurrentRevision, "dbml": dbml})
		return
	}
	if len(rest) == 1 && rest[0] == "regenerate" && r.Method == http.MethodPost {
		project, ok := s.store.Project(projectID)
		if !ok {
			writeMappedError(w, r, workspace.ErrNotFound)
			return
		}
		job := s.jobs.StartWithRevision(projectID, "generate_outputs", project.CurrentRevision, []string{"generate_dbml", "generate_trace", "refresh_quality"}, func(projectID, _ string, emit jobs.StepEmitter) (int, []string, error) {
			return s.store.GenerateFinalOutputs(projectID, project.CurrentRevision, emit)
		})
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
}

func (s *Server) handleExports(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if r.Method != http.MethodGet || len(rest) != 1 {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	switch rest[0] {
	case "dbml":
		dbml, err := s.store.DBML(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeDownload(w, "final.dbml", "text/plain; charset=utf-8", []byte(dbml))
	case "report":
		report, err := s.store.TraceReport(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeDownload(w, "traceability_report.md", "text/markdown; charset=utf-8", []byte(report))
	case "bundle":
		bundle, err := s.store.ExportBundle(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeDownload(w, "db_model_workbench_bundle.zip", "application/zip", bundle)
	default:
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
	}
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if len(rest) == 0 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, map[string]any{"items": s.jobs.List(projectID)})
		return
	}
	if len(rest) == 1 && r.Method == http.MethodGet {
		job, ok := s.jobs.Get(rest[0])
		if !ok || job.ProjectID != projectID {
			writeMappedError(w, r, workspace.ErrNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
		return
	}
	if len(rest) == 2 && rest[1] == "retry" && r.Method == http.MethodPost {
		job, ok := s.jobs.Get(rest[0])
		if !ok || job.ProjectID != projectID {
			writeMappedError(w, r, workspace.ErrNotFound)
			return
		}
		var req struct {
			BaseRevision int `json:"base_revision"`
		}
		if !decodeJSON(w, r, &req) {
			return
		}
		retried, err := s.jobs.Retry(rest[0], req.BaseRevision)
		if err != nil {
			writeError(w, r, http.StatusConflict, "job_retry_unavailable", err.Error(), nil)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": retried})
		return
	}
	if len(rest) != 2 || rest[1] != "events" || r.Method != http.MethodGet {
		writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
		return
	}
	jobID := rest[0]
	job, ok := s.jobs.Get(jobID)
	if !ok || job.ProjectID != projectID {
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	events, replay, ok := s.jobs.Subscribe(jobID)
	if !ok {
		writeMappedError(w, r, workspace.ErrNotFound)
		return
	}
	defer s.jobs.Unsubscribe(jobID, events)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, _ := w.(http.Flusher)
	for _, event := range replay {
		writeSSE(w, event)
	}
	if flusher != nil {
		flusher.Flush()
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			writeSSE(w, event)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func (s *Server) handleLLMRuns(w http.ResponseWriter, r *http.Request, projectID string, rest []string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	if len(rest) == 0 {
		items, err := s.store.LLMRuns(projectID)
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
		return
	}
	if len(rest) == 1 {
		details, err := s.store.LLMRun(projectID, rest[0], r.URL.Query().Get("include_raw") == "true")
		if err != nil {
			writeMappedError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"run": details})
		return
	}
	writeError(w, r, http.StatusNotFound, "not_found", "Endpoint not found.", nil)
}

func (s *Server) handleOptimizationReport(w http.ResponseWriter, r *http.Request, projectID string) {
	if r.Method != http.MethodGet {
		writeError(w, r, http.StatusMethodNotAllowed, "bad_request", "Method not allowed.", nil)
		return
	}
	report, err := s.store.LLMOptimizationReport(projectID)
	if err != nil {
		writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": report})
}

func writeSSE(w io.Writer, event jobs.Event) {
	data, _ := json.Marshal(event)
	fmt.Fprintf(w, "event: job.%s\n", event.Status)
	fmt.Fprintf(w, "data: %s\n\n", data)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		writeError(w, r, http.StatusBadRequest, "bad_request", "Invalid JSON body.", nil)
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeMutationResult(w http.ResponseWriter, revision int, updated []string, needsRefresh []string, message string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"project_revision": revision,
		"updated":          updated,
		"needs_refresh":    needsRefresh,
		"message":          message,
	})
}

func writeDownload(w http.ResponseWriter, filename, contentType string, data []byte) {
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeMappedError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "Resource not found.", nil)
	case errors.Is(err, workspace.ErrRevisionConflict):
		writeError(w, r, http.StatusConflict, "revision_conflict", "The project changed since this screen was loaded.", nil)
	case errors.Is(err, workspace.ErrModelNotGenerated), errors.Is(err, workspace.ErrDBMLNotReady):
		writeError(w, r, http.StatusPreconditionFailed, "precondition_failed", err.Error(), nil)
	default:
		writeError(w, r, http.StatusBadRequest, "validation_failed", err.Error(), nil)
	}
}

func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details any) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"code":       code,
			"message":    message,
			"details":    details,
			"request_id": requestID(r),
		},
	})
}

func requestID(r *http.Request) string {
	if id := r.Header.Get("X-Request-ID"); id != "" {
		return id
	}
	return fmt.Sprintf("req_%d", time.Now().UnixNano())
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept, X-Request-ID")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
}

func splitPath(value string) []string {
	clean := strings.Trim(path.Clean("/"+value), "/")
	if clean == "" {
		return nil
	}
	return strings.Split(clean, "/")
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func optionalInt(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, errors.New("value must be a non-negative integer")
	}
	return parsed, nil
}

func uploadedSize(file multipart.File, header *multipart.FileHeader) int64 {
	if header.Size > 0 {
		return header.Size
	}
	current, _ := file.Seek(0, io.SeekCurrent)
	end, err := file.Seek(0, io.SeekEnd)
	if err == nil {
		_, _ = file.Seek(current, io.SeekStart)
		return end
	}
	return 0
}

func filterSourceUnits(items []workspace.SourceUnit, filter, search string) []workspace.SourceUnit {
	if filter == "" {
		filter = "all"
	}
	var out []workspace.SourceUnit
	search = strings.ToLower(search)
	for _, item := range items {
		if search != "" && !strings.Contains(strings.ToLower(item.ID+" "+item.NormalizedText+" "+item.Section), search) {
			continue
		}
		switch filter {
		case "needs_attention":
			if item.ReviewStatus != "needs_attention" && item.ReviewStatus != "open_review" {
				continue
			}
		case "model_relevant":
			if item.Relevance != "model_relevant" && item.Relevance != "model_supporting" {
				continue
			}
		case "examples":
			if len(item.LinkedExamples) == 0 && item.Kind != "example_reference" && item.Kind != "json_field" {
				continue
			}
		case "non_model":
			if item.Relevance != "non_model" {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func filterRequirements(items []workspace.RequirementAtom, filter, search string) []workspace.RequirementAtom {
	if filter == "" {
		filter = "all"
	}
	sourceUnitID := ""
	if filter == "by_source_unit" {
		sourceUnitID = search
		search = ""
	}
	search = strings.ToLower(search)
	var out []workspace.RequirementAtom
	for _, item := range items {
		if search != "" && !strings.Contains(strings.ToLower(item.ID+" "+item.Statement+" "+item.FunctionalArea), search) {
			continue
		}
		if sourceUnitID != "" && !contains(item.SourceUnits, sourceUnitID) {
			continue
		}
		switch filter {
		case "needs_review":
			if item.ReviewStatus != "needs_review" && item.ReviewStatus != "open_review" {
				continue
			}
		case "direct_db":
			if item.ModelingRelevance != "direct_db" {
				continue
			}
		case "non_model":
			if item.ModelingRelevance != "non_model" {
				continue
			}
		case "external":
			if item.ModelingRelevance != "external" {
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func contains(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func dependencyEdges(items []workspace.ReviewCandidate) []map[string]string {
	var out []map[string]string
	for _, item := range items {
		for _, parent := range item.DependsOn {
			out = append(out, map[string]string{"from": parent, "to": item.ID})
		}
	}
	return out
}

func blockingReasons(health workspace.ArtifactHealth) []string {
	var out []string
	if health.OpenReviewQuestions > 0 {
		out = append(out, strconv.Itoa(health.OpenReviewQuestions)+" open review questions")
	}
	if health.AnalysisStatus == "not_started" {
		out = append(out, "analysis not started")
	}
	if out == nil {
		return []string{}
	}
	return out
}
