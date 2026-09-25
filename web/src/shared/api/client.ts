import type { LogicalMappingReport,
  ActorSummary,
  ApiError,
  ArtifactHealth,
  BundleCandidate,
	CombinedDocument,
	ConceptualModel,
	ConceptualDescription,
  CrudOperation,
  FunctionalArea,
  InputResource,
	Job,
  JobEvent,
  JobRef,
  LlmStatus,
	LLMRunSummary,
	LLMOptimizationReport,
  ModelElementDetails,
  ModelGraph,
  MutationResult,
  Page,
  ProjectResponse,
	ProjectStageName,
  ProjectSummary,
  QualityReport,
  RequirementAtom,
  ReviewCandidate,
  ReviewDecision,
	SemanticVerificationReport,
  SourceManifest,
	SourceUnit,
	SourceUnitQA,
	SourceUnitReviewResult,
	SourceFidelityReport,
	SourceSegmentationProposal,
  StructuredExample,
  TraceIndex,
} from "./types";
import { mockApi } from "@/mocks/mockApi";

const apiMode = import.meta.env.VITE_API_MODE ?? "real";
const baseURL = import.meta.env.VITE_API_BASE_URL ?? "/api/v1";

export class HttpError extends Error {
  code: string;
  details?: unknown;
  status: number;

  constructor(status: number, body: ApiError) {
    super(body.error.message);
    this.status = status;
    this.code = body.error.code;
    this.details = body.error.details;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  if (apiMode === "mock") {
    return mockApi.request<T>(path, init);
  }
  const headers = new Headers(init.headers);
  if (!(init.body instanceof FormData)) {
    headers.set("Content-Type", "application/json");
    headers.set("Accept", "application/json");
  }
  const response = await fetch(`${baseURL}${path}`, { ...init, headers });
  if (!response.ok) {
    const body = (await response.json()) as ApiError;
    throw new HttpError(response.status, body);
  }
  return (await response.json()) as T;
}

export const api = {
  llmStatus: () => request<LlmStatus>("/llm/status"),
  listBundles: () => request<{ items: BundleCandidate[] }>("/bundles"),
  scaffoldBundleFromTask: (body: { name?: string; content: string }) =>
    request<{ bundle: BundleCandidate }>("/bundles/scaffold-from-task", { method: "POST", body: JSON.stringify(body) }),
  llmPlanBundleFromTask: (body: {
    name?: string;
    content: string;
    model?: string;
    reasoning_effort?: string;
    max_output_tokens?: number;
    max_repair_attempts?: number;
    mock?: boolean;
  }) =>
    request<{ bundle: BundleCandidate }>("/bundles/llm-plan-from-task", { method: "POST", body: JSON.stringify(body) }),
  importBundle: (path: string) =>
    request<{ project: ProjectSummary }>("/projects/import-bundle", { method: "POST", body: JSON.stringify({ path }) }),

  listProjects: (status = "active", search = "") =>
    request<Page<ProjectSummary>>(`/projects?status=${encodeURIComponent(status)}&search=${encodeURIComponent(search)}`),
  createProject: (body: { name: string; description?: string; language?: string; domain?: string }) =>
    request<{ project: ProjectSummary }>("/projects", { method: "POST", body: JSON.stringify(body) }),
  getProject: (projectId: string) => request<ProjectResponse>(`/projects/${projectId}`),
  updateProject: (projectId: string, body: { base_revision: number; name?: string; description?: string }) =>
    request<MutationResult>(`/projects/${projectId}`, { method: "PATCH", body: JSON.stringify(body) }),
  deleteProject: (projectId: string) =>
    request<{ deleted_project_id: string; message: string }>(`/projects/${projectId}`, { method: "DELETE" }),
  completeProject: (projectId: string, baseRevision: number) =>
    request<{ project: ProjectSummary }>(`/projects/${projectId}/complete`, {
      method: "POST",
      body: JSON.stringify({ base_revision: baseRevision }),
    }),
  reopenProject: (projectId: string, note: string) =>
    request<{ project: ProjectSummary; source_snapshot_id: string }>(`/projects/${projectId}/reopen`, {
      method: "POST",
      body: JSON.stringify({ mode: "new_revision", note }),
    }),

  listResources: (projectId: string) => request<{ items: InputResource[] }>(`/projects/${projectId}/resources`),
  resource: (projectId: string, resourceId: string) =>
    request<{ resource: InputResource }>(`/projects/${projectId}/resources/${encodeURIComponent(resourceId)}`),
  resourceText: (projectId: string, resourceId: string) =>
    request<{ resource: InputResource; text: string }>(`/projects/${projectId}/resources/${encodeURIComponent(resourceId)}/text`),
  addPastedText: (projectId: string, body: { title: string; content: string }) =>
    request<{ resource: InputResource; project_revision: number }>(`/projects/${projectId}/resources`, {
      method: "POST",
      body: JSON.stringify({ kind: "pasted_text", ...body }),
    }),
  uploadFile: (projectId: string, formData: FormData) =>
    request<{ resource: InputResource; project_revision: number }>(`/projects/${projectId}/resources/upload`, {
      method: "POST",
      body: formData,
    }),
  deleteResource: (projectId: string, resourceId: string) =>
    request<MutationResult>(`/projects/${projectId}/resources/${encodeURIComponent(resourceId)}`, { method: "DELETE" }),
  sourceManifest: (projectId: string) => request<{ manifest: SourceManifest }>(`/projects/${projectId}/source-manifest`),
  combinedDocument: (projectId: string) => request<{ combined_document: CombinedDocument }>(`/projects/${projectId}/combined-document`),
	sourceSegmentation: (projectId: string) =>
		request<{ proposal: SourceSegmentationProposal }>(`/projects/${projectId}/source-segmentation`),
  sourceFidelity: (projectId: string) => request<{ source_fidelity: SourceFidelityReport }>(`/projects/${projectId}/source-fidelity`),
  processSources: (
    projectId: string,
    baseRevision: number,
		options: { model?: string; reasoning_effort?: string; max_output_tokens?: number; mock?: boolean } = {},
  ) =>
    request<JobRef>(`/projects/${projectId}/process-sources`, {
      method: "POST",
			body: JSON.stringify({ base_revision: baseRevision, ...options }),
    }),
	runStage: (projectId: string, stage: ProjectStageName, baseRevision: number, mock = false) =>
		request<JobRef>(`/projects/${projectId}/stages/${stage}/run`, {
			method: "POST",
			body: JSON.stringify({ base_revision: baseRevision, mock }),
		}),
	stageStatus: (projectId: string) =>
		request<{ artifact_health: ArtifactHealth; next_stage: ProjectStageName | "source_review" | "review_decisions" | "conceptual_review" | "model_review" | "completed" }>(`/projects/${projectId}/stages`),

  analysisSummary: (projectId: string) =>
    request<{ project_revision: number; summary: Record<string, Record<string, number> | boolean> }>(
      `/projects/${projectId}/analysis/summary`,
    ),
  sourceUnits: (projectId: string, filter = "all", search = "") =>
    request<Page<SourceUnit> & { project_revision: number }>(
      `/projects/${projectId}/source-units?filter=${encodeURIComponent(filter)}&search=${encodeURIComponent(search)}`,
    ),
	sourceUnitQA: (projectId: string) => request<{ qa: SourceUnitQA }>(`/projects/${projectId}/source-units/qa`),
	sourceUnit: (projectId: string, sourceUnitId: string) =>
    request<{ source_unit: SourceUnit; original_excerpt: { text: string } }>(
      `/projects/${projectId}/source-units/${sourceUnitId}`,
    ),
	reviewSourceUnit: (
		projectId: string,
		sourceUnitId: string,
		body: { base_revision: number; decision: "accept" | "revise" | "exclude"; note?: string },
	) =>
		request<SourceUnitReviewResult>(`/projects/${projectId}/source-units/${encodeURIComponent(sourceUnitId)}/review`, {
			method: "POST",
			body: JSON.stringify({ ...body, reviewed_by: "web_user" }),
		}),
  examples: (projectId: string) => request<{ project_revision: number; items: StructuredExample[] }>(`/projects/${projectId}/examples`),
  requirements: (projectId: string, filter = "all", search = "") =>
    request<Page<RequirementAtom> & { project_revision: number; coverage: Record<string, number> }>(
      `/projects/${projectId}/requirements?filter=${encodeURIComponent(filter)}&search=${encodeURIComponent(search)}`,
    ),
  designObligations: (projectId: string) => request<{ design_obligations: Array<{ id: string; statement: string; kind: string; persistence: string; risk: string; status: string }>; qa: { ok: boolean; errors: string[]; warnings: string[] } }>(`/projects/${projectId}/design-obligations`),
  functionalAreas: (projectId: string) =>
    request<{ project_revision: number; items: FunctionalArea[] }>(`/projects/${projectId}/functional-areas`),
  actors: (projectId: string) => request<{ project_revision: number; items: ActorSummary[] }>(`/projects/${projectId}/actors`),
  crudOperations: (projectId: string) =>
    request<{ project_revision: number; items: CrudOperation[] }>(`/projects/${projectId}/crud-operations`),

  reviewCandidates: (projectId: string) =>
    request<Page<ReviewCandidate> & { project_revision: number }>(`/projects/${projectId}/review-candidates`),
	answerReview: (projectId: string, reviewId: string, baseRevision: number, selectedOption: string, mock = false) =>
    request<JobRef & { project_revision: number }>(`/projects/${projectId}/review-candidates/${reviewId}/answer`, {
      method: "POST",
		body: JSON.stringify({ base_revision: baseRevision, selected_option: selectedOption, mock }),
    }),
	answerReviewBatch: (
		projectId: string,
		baseRevision: number,
		selections: Array<{ candidate_id: string; selected_option_id: string }>,
		activeReviewMs: number,
	) =>
		request<JobRef>(`/projects/${projectId}/review-decisions/batch`, {
			method: "POST",
			body: JSON.stringify({ base_revision: baseRevision, selections, reviewed_by: "web_user", active_review_ms: activeReviewMs }),
		}),
  applyRecommended: (projectId: string, baseRevision: number) =>
    request<JobRef & { project_revision: number }>(`/projects/${projectId}/review-candidates/apply-recommended`, {
      method: "POST",
      body: JSON.stringify({ base_revision: baseRevision }),
    }),
  reviewDecisions: (projectId: string) =>
    request<{ project_revision: number; items: ReviewDecision[] }>(`/projects/${projectId}/review-decisions`),

  modelReadiness: (projectId: string) =>
    request<{ project_revision: number; readiness: { can_generate_model: boolean; open_review_questions: number; blocking_reasons: string[] } }>(
      `/projects/${projectId}/model-generation/readiness`,
    ),
  generateModel: (projectId: string, baseRevision: number) =>
    request<JobRef>(`/projects/${projectId}/generate-model`, {
      method: "POST",
      body: JSON.stringify({ base_revision: baseRevision }),
    }),
	conceptualModel: (projectId: string) =>
		request<{ conceptual_model: ConceptualModel; accepted: boolean; diff: Record<string, unknown>; qa: { ok: boolean; errors: string[]; warnings: string[]; coverage: Record<string, number> }; description?: ConceptualDescription | null }>(`/projects/${projectId}/conceptual-model`),
	acceptConceptualModel: (projectId: string, baseRevision: number) =>
		request<{ project_revision: number; message: string }>(`/projects/${projectId}/conceptual-model/accept`, {
			method: "POST", body: JSON.stringify({ base_revision: baseRevision }),
		}),
	logicalMappingReport: (projectId: string) =>
		request<{ logical_mapping_report: LogicalMappingReport }>(`/projects/${projectId}/logical-mapping-report`),
	semanticVerification: (projectId: string) => request<{ semantic_verification: SemanticVerificationReport }>(`/projects/${projectId}/semantic-verification`),
	createSemanticRepairCandidates: (projectId: string, baseRevision: number) =>
		request<{ project_revision: number; review_candidate_ids: string[]; message: string }>(`/projects/${projectId}/semantic-verification/repair-candidates`, {
			method: "POST", body: JSON.stringify({ base_revision: baseRevision }),
		}),
	acceptModel: (projectId: string, baseRevision: number) =>
		request<{ project_revision: number; message: string }>(`/projects/${projectId}/model-acceptance`, {
			method: "POST", body: JSON.stringify({ base_revision: baseRevision }),
		}),
	requestModelCorrection: (projectId: string, body: { base_revision: number; element_id: string; correction_type: string; note: string }) =>
		request<{ project_revision: number; review_candidate_id: string; message: string }>(`/projects/${projectId}/model-corrections`, {
			method: "POST", body: JSON.stringify(body),
		}),
  modelGraph: (projectId: string) =>
    request<{ project_revision: number; model_graph: ModelGraph }>(`/projects/${projectId}/model-graph`),
  traceIndex: (projectId: string) =>
    request<{ project_revision: number; trace_index: TraceIndex }>(`/projects/${projectId}/trace-index`),
  modelElement: (projectId: string, elementId: string) =>
    request<{ model_element: ModelElementDetails }>(`/projects/${projectId}/model-elements/${encodeURIComponent(elementId)}`),
  quality: (projectId: string) => request<{ project_revision: number; quality: QualityReport }>(`/projects/${projectId}/quality`),
  runQuality: (projectId: string) => request<JobRef>(`/projects/${projectId}/quality/run`, { method: "POST", body: "{}" }),
  acceptQuality: (projectId: string, issueId: string) =>
    request<MutationResult>(`/projects/${projectId}/quality/issues/${issueId}/accept`, { method: "POST", body: "{}" }),

  dbml: (projectId: string) => request<{ project_revision: number; dbml: string }>(`/projects/${projectId}/dbml`),
  regenerateDbml: (projectId: string) => request<JobRef>(`/projects/${projectId}/dbml/regenerate`, { method: "POST", body: "{}" }),
	jobs: (projectId: string) => request<{ items: Job[] }>(`/projects/${projectId}/jobs`),
	job: (projectId: string, jobId: string) => request<{ job: Job }>(`/projects/${projectId}/jobs/${jobId}`),
	llmRuns: (projectId: string) => request<{ items: LLMRunSummary[] }>(`/projects/${projectId}/llm-runs`),
	optimizationReport: (projectId: string) => request<{ report: LLMOptimizationReport }>(`/projects/${projectId}/optimization-report`),
	retryJob: (projectId: string, jobId: string, baseRevision: number) =>
		request<JobRef>(`/projects/${projectId}/jobs/${jobId}/retry`, { method: "POST", body: JSON.stringify({ base_revision: baseRevision }) }),
  postgresql: (projectId: string) => request<{ dialect: string; sql: string }>(`/projects/${projectId}/sql`),
  exportURL: (projectId: string, kind: "dbml" | "sql" | "report" | "bundle") => `${baseURL}/projects/${projectId}/exports/${kind}`,
};

export function subscribeToJob(
  projectId: string,
  jobId: string,
  onEvent: (event: JobEvent) => void,
	onDone?: (event?: JobEvent) => void,
) {
  if (apiMode === "mock") {
    return mockApi.subscribeToJob(projectId, jobId, onEvent, onDone);
  }
  const source = new EventSource(`${baseURL}/projects/${projectId}/jobs/${jobId}/events`);
  const handler = (message: MessageEvent) => {
    const event = JSON.parse(message.data) as JobEvent;
    onEvent(event);
		if (["completed", "failed", "cancelled", "superseded", "interrupted"].includes(event.status)) {
      source.close();
		onDone?.(event);
    }
  };
	for (const status of ["queued", "running", "retrying", "waiting_for_review", "completed", "failed", "cancelled", "superseded", "interrupted"]) {
		source.addEventListener(`job.${status}`, handler);
	}
  source.onerror = () => {
    source.close();
  };
  return () => source.close();
}

export function projectDefaultPath(project: ProjectSummary) {
	return project.lifecycle_status === "completed"
		? `/projects/${project.id}/completed`
		: `/projects/${project.id}/analysis/overview`;
}
