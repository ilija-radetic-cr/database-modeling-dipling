import type {
  AdversarialReviewActor,
  AdversarialReviewAuditEvent,
  AdversarialReviewDecision,
	AdversarialReviewFinding,
	AdversarialReviewFindingProvenance,
  AdversarialReviewRecord,
  AdversarialReviewResponse,
  ArtifactHealth,
  BundleCandidate,
  CombinedDocument,
  ConceptualDescription,
  ConceptualModel,
  InputResource,
  Job,
  JobEvent,
  LLMOptimizationReport,
  LogicalMappingReport,
  ModelElementDetails,
  ModelGraph,
  ProjectNextStage,
  ProjectSummary,
  SourceManifest,
	SourceSegmentationProposal,
  SourceUnit,
  SourceUnitQA,
} from "@/shared/api/types";

// The fixture is a project in the segment-based flow that has reached the
// conceptual review gate: sources are segmented, every unit is reviewed and a
// conceptual model is proposed but not yet accepted.
const projectId = "project_mock";
const pipelineVersion = "0.8.0";
const now = () => new Date().toISOString();

const resources: InputResource[] = [
  {
    id: "R-001",
    kind: "uploaded_file",
    file_type: "markdown",
    title: "Printing House task text",
    file_name: "TASK_FULL.md",
    size_bytes: 12000,
    content_path: "mock/printing_house_v06/TASK_FULL.md",
    extracted_text_path: "mock/printing_house_v06/TASK_FULL.md",
    content_hash: "sha256:mock",
    extracted_text_hash: "sha256:mock",
    language: "sr-Cyrl",
    authority: "normative",
    line_count: 3,
    warnings: [],
    extraction_status: "ready",
    extraction_confidence: "high",
    created_at: now(),
    updated_at: now(),
  },
];

const segments: SourceSegmentationProposal["segments"] = [
  { id: "SU-001", type: "heading", text: "Sistem za naručivanje štampe" },
  { id: "SU-002", type: "sentence", text: "Postoje tri vrste korisnika: klijenti, štampari i administrator web sistema." },
  { id: "SU-003", type: "sentence", text: "Klijent kreira narudžbinu koja sadrži jedan ili više proizvoda." },
];

const sourceManifest: SourceManifest = {
  document: {
    id: `${projectId}_source_manifest`,
    project_id: projectId,
    project_name: "Printing House Full",
	pipeline_version: pipelineVersion,
    workspace_path: `.dbdsl_workbench/projects/${projectId}`,
    generated_at: now(),
  },
  resources,
  summary: {
    total: 1,
    ready: 1,
    needs_attention: 0,
    failed: 0,
    line_count: 3,
  },
};

const combinedDocument: CombinedDocument = {
  markdown: `# Combined Document\n\n${segments.map((segment) => `[${segment.id}] ${segment.text}`).join("\n")}\n`,
  lineage: {
    document: {
      id: `${projectId}_combined_document_lineage`,
      project_id: projectId,
      project_name: "Printing House Full",
      source_manifest_file: "source_manifest.yaml",
		pipeline_version: pipelineVersion,
      created_at: now(),
    },
    sentences: segments.map((segment, index) => ({
      id: segment.id,
      kind: segment.type === "heading" ? "structural" : "sentence",
      role: segment.type,
      text: segment.text,
      derived_from: [{ resource_id: "R-001", source_segment_id: segment.id, line_start: index + 1, line_end: index + 1, exact_text: segment.text }],
      transformation: "copied",
      confidence: "high",
      warnings: [],
    })),
    warnings: [],
    confidence_summary: { strategy: "segments_v1" },
  },
  summary: {
    status: "ready",
	unit_count: segments.length,
    sentence_count: segments.filter((segment) => segment.type === "sentence").length,
	structural_unit_count: segments.filter((segment) => segment.type === "heading").length,
    resource_count: 1,
    warning_count: 0,
	segmentation_strategy: "llm_complete_resource",
	llm_assisted: true,
	fallback_used: false,
	needs_attention_count: 0,
	layout_segment_count: 0,
  },
};

const sourceSegmentation: { proposal: SourceSegmentationProposal } = { proposal: { segments } };

// Segment-based units carry the segment text verbatim; the backend records
// that no normalization was applied.
const sourceUnits: SourceUnit[] = segments.map((segment, index) => ({
  id: segment.id,
  kind: segment.type,
  section: segment.type === "heading" ? "document_title" : "requirements",
  normalized_text: segment.text,
  normalization: { version: "none", strategy: "none", exact_hash: "", normalized_hash: "", changed: false, operations: [] },
  exact_text: segment.text,
  relevance: segment.type === "heading" ? "non_model" : "",
  confidence: "high",
  review_status: "reviewed",
  origin_spans: [{ resource_id: "R-001", label: `TASK_FULL.md · line ${index + 1}`, line_start: index + 1, line_end: index + 1 }],
  segment_ids: [segment.id],
  warnings: [],
}));

const sourceUnitQA: SourceUnitQA = {
  ok: true,
  derivation_strategy: "segments_v1",
  segments_total: segments.length,
  segments_referenced: segments.length,
  unreferenced_segments: [],
  needs_attention: [],
  origin_chains: Object.fromEntries(segments.map((segment) => [segment.id, ["R-001"]])),
  errors: [],
  warnings: [],
  review_decisions: [],
};

const conceptualModel: ConceptualModel = {
  entity_concepts: [
    {
      id: "ENT-user", label: "User", description: "A person who uses the system: client, printer or administrator.", kind: "regular",
      attributes: [
        { id: "ATTR-user-username", label: "username", description: "Unique login name.", name: "username", value_type: "text", unique: true, required: true, evidence: { source_units: ["SU-002"], review_decisions: [] } },
        { id: "ATTR-user-role", label: "role", description: "Client, printer or administrator.", name: "role", value_type: "enum", enum_values: ["client", "printer", "administrator"], required: true, evidence: { source_units: ["SU-002"], review_decisions: [] } },
      ],
      evidence: { source_units: ["SU-002"], review_decisions: [] },
    },
    {
      id: "ENT-order", label: "Order", description: "An order placed by a client.", kind: "regular",
      attributes: [
        { id: "ATTR-order-created-at", label: "created_at", description: "When the order was placed.", name: "created_at", value_type: "datetime", required: true, evidence: { source_units: ["SU-003"], review_decisions: [] } },
      ],
      evidence: { source_units: ["SU-003"], review_decisions: [] },
    },
  ],
  relationships: [
    { id: "REL-order-user", label: "placed by", description: "Each order belongs to one client.", from: "ENT-order", to: "ENT-user", cardinality: "many_to_one", required: true, evidence: { source_units: ["SU-003"], review_decisions: [] } },
  ],
  lifecycle_concepts: [],
  derived_concepts: [],
  file_concepts: [],
  import_concepts: [],
  index_concepts: [],
  unresolved_review_ids: [],
  warnings: [],
  confidence_summary: { overall: "high" },
};

const conceptualDescription: ConceptualDescription = {
  actors: [
    { id: "ACT-client", name: "Client", description: "Orders printed products.", represented_by: "ENT-user", differs_by: "role", evidence: { segments: ["SU-002"], mode: "direct" } },
  ],
  things: [
    {
      id: "ENT-user", name: "User", kind: "regular", description: "A person who uses the system.", identified_by: ["username"],
      properties: [{ name: "username", meaning: "Unique login name.", value_type: "text", shape: "single", presence: "required", origin: "user", source: "SU-002", evidence: { segments: ["SU-002"], mode: "direct" } }],
      links: [], states: [], evidence: { segments: ["SU-002"], mode: "direct" },
    },
    {
      id: "ENT-order", name: "Order", kind: "regular", description: "An order placed by a client.", identified_by: ["id"],
      properties: [], links: [{ to: "ENT-user", meaning: "placed by", per_this: "one", per_other: "many", evidence: { segments: ["SU-003"], mode: "direct" } }],
      states: [], evidence: { segments: ["SU-003"], mode: "direct" },
    },
  ],
  rules: [
    { id: "RULE-username-unique", kind: "uniqueness", statement: "Usernames are unique.", applies_to: ["ENT-user"], evidence: { segments: ["SU-002"], mode: "implied" } },
  ],
  queries: [],
  imports: [],
  boundaries: [],
  excluded: [{ segment: "SU-001", reason: "document title" }],
  open_questions: [],
};

const modelGraph: ModelGraph = {
  nodes: [
    {
      id: "table:user", kind: "table", label: "User", table_name: "users", description: "Application user.",
      fields: [
        { id: "id", element_id: "field:user.id", label: "id", type: "bigint", required: true, evidence: { source_units: ["SU-002"], review_decisions: [] } },
        { id: "username", element_id: "field:user.username", label: "username", type: "varchar(255)", required: true, evidence: { source_units: ["SU-002"], review_decisions: [] } },
        { id: "role", element_id: "field:user.role", label: "role", type: "varchar(64)", required: true, evidence: { source_units: ["SU-002"], review_decisions: [] } },
      ],
      evidence: { source_units: ["SU-002"], review_decisions: [] },
    },
    {
      id: "table:order", kind: "table", label: "Order", table_name: "orders", description: "Client order.",
      fields: [
        { id: "id", element_id: "field:order.id", label: "id", type: "bigint", required: true, evidence: { source_units: ["SU-003"], review_decisions: [] } },
        { id: "client_id", element_id: "field:order.client_id", label: "client_id", type: "bigint", required: true, evidence: { source_units: ["SU-003"], review_decisions: [] } },
      ],
      evidence: { source_units: ["SU-003"], review_decisions: [] },
    },
  ],
  edges: [
    { id: "rel:order-client", kind: "relationship", label: "placed by", from: "table:order", to: "table:user", cardinality: "many_to_one", required: true, evidence: { source_units: ["SU-003"], review_decisions: [] } },
  ],
};

const traceIndex = {
  source_to_elements: {
    "SU-002": ["table:user", "field:user.id", "field:user.username", "field:user.role"],
    "SU-003": ["table:order", "field:order.id", "field:order.client_id", "rel:order-client"],
  },
  element_to_sources: {
    "table:user": ["SU-002"], "field:user.id": ["SU-002"], "field:user.username": ["SU-002"], "field:user.role": ["SU-002"],
    "table:order": ["SU-003"], "field:order.id": ["SU-003"], "field:order.client_id": ["SU-003"], "rel:order-client": ["SU-003"],
  },
  review_to_elements: {},
};

const logicalMappingReport: LogicalMappingReport = {
  strategy: "deterministic",
  rule_version: "conceptual_to_v06/v1",
  entities: 2,
  relationships: 1,
  constraints: 1,
  state_machines: 0,
  derived_views: 0,
  decisions: ["Mapped User and Order concepts to regular tables.", "Mapped placed by to orders.client_id."],
  inferred: [],
  warnings: [],
};

function modelElementDetails(elementId: string): ModelElementDetails {
  for (const node of modelGraph.nodes) {
    if (node.id === elementId) {
      return { id: node.id, kind: node.kind, label: node.label, description: node.description, evidence: node.evidence, related_elements: node.fields?.map((field) => field.element_id) ?? [] };
    }
    const field = node.fields?.find((item) => item.element_id === elementId);
    if (field) {
      return { id: field.element_id, kind: "field", label: field.label, description: field.description, evidence: field.evidence, related_elements: [node.id] };
    }
  }
  const edge = modelGraph.edges.find((item) => item.id === elementId);
  if (edge) return { id: edge.id, kind: edge.kind, label: edge.label, evidence: edge.evidence, related_elements: [edge.from, edge.to] };
  return { id: elementId, kind: "element", label: elementId, evidence: { source_units: [], review_decisions: [] }, related_elements: [] };
}

let projectDeleted = false;
let conceptualAccepted = false;
let modelGenerated = false;
let finalModelAccepted = false;
let outputsReady = false;
let projectCompleted = false;
let revision = 4;
let jobSequence = 0;
const mockJobs = new Map<string, Job>();
let reviewStatus: AdversarialReviewResponse["status"] = "not_run";
let mockReview: AdversarialReviewRecord | null = null;
let reviewDecisions: AdversarialReviewDecision[] = [];
let reviewAudit: AdversarialReviewAuditEvent[] = [];
let reviewFindingProvenance: AdversarialReviewFindingProvenance[] = [];
let reviewSequence = 0;

function resetReviewState() {
	reviewStatus = "not_run";
	mockReview = null;
	reviewDecisions = [];
	reviewAudit = [];
	reviewFindingProvenance = [];
	reviewSequence = 0;
}

function currentReviewDecisions() {
	const latest = new Map<string, AdversarialReviewDecision>();
	if (!mockReview) return latest;
	for (const decision of reviewDecisions) {
		if (decision.review_id === mockReview.review_id) latest.set(decision.finding_id, decision);
	}
	return latest;
}

function adversarialReviewResponse(): AdversarialReviewResponse {
	const latest = currentReviewDecisions();
	const canAccept = reviewStatus === "current" && !!mockReview && mockReview.findings.every((finding) => {
		const decision = latest.get(finding.id);
		return !!decision && decision.decision !== "request_correction";
	});
	return {
		project_revision: revision,
		status: reviewStatus,
		review: mockReview,
		decisions: reviewDecisions,
		audit: reviewAudit,
		finding_provenance: reviewFindingProvenance,
		can_accept: canAccept,
	};
}

function appendAudit(event: Omit<AdversarialReviewAuditEvent, "event_id" | "created_at" | "project_revision">) {
	reviewAudit.push({ ...event, event_id: `audit_mock_${reviewAudit.length + 1}`, created_at: now(), project_revision: revision });
}

function generateMockReview() {
	const reviewId = `review_mock_${++reviewSequence}`;
	mockReview = {
		review_id: reviewId,
		scope: "conceptual",
		candidate_revision: revision,
		candidate_hash: `sha256:mock-candidate-r${revision}`,
		source_hash: "sha256:mock-source-task",
		summary: "The proposal captures users and orders, but it collapses three source roles into one modeled actor.",
		findings: [{
			id: "ARF-001",
			severity: "warning",
			category: "coverage",
			source_unit_ids: ["SU-002"],
			description_refs: ["actor:client", "user"],
			source_quote: "Postoje tri vrste korisnika: klijenti, štampari i administrator web sistema.",
			claim: "The proposal represents every user role named by the task.",
			expected: "Client, printer, and web administrator roles remain distinguishable.",
			actual: "Only the Client actor is described; printer and administrator responsibilities are absent.",
			suggested_correction: "Represent all three roles or explicitly justify their exclusion from the data model.",
		}],
	};
	reviewStatus = "current";
	reviewFindingProvenance = mockReview.findings.map((finding) => ({
		finding_id: finding.id,
		review_id: reviewId,
		origin: "llm",
		actor: "adversarial_reviewer",
		created_at: now(),
		project_revision: revision,
	}));
	appendAudit({
		review_id: reviewId,
		action: "review_completed",
		actor: "adversarial_reviewer",
		candidate_hash: mockReview.candidate_hash,
		source_hash: mockReview.source_hash,
		reason: "Conceptual proposal challenged against cited source units.",
		result: "1 finding",
	});
}

export function resetMockState() {
  projectDeleted = false;
  conceptualAccepted = false;
  modelGenerated = false;
  finalModelAccepted = false;
  outputsReady = false;
  projectCompleted = false;
  revision = 4;
  jobSequence = 0;
  mockJobs.clear();
	resetReviewState();
}

function health(): ArtifactHealth {
  return {
    source_manifest_status: "ready",
    combined_document_status: "ready",
	source_segmentation_status: "ready",
	source_units_status: "ready",
		conceptual_model_status: conceptualAccepted ? "ready" : "proposed",
	    model_status: modelGenerated ? "ready" : "not_generated",
	    dbml_status: outputsReady ? "ready" : "not_generated",
	    can_continue_to_dbml: modelGenerated,
	    can_complete_project: modelGenerated && finalModelAccepted && outputsReady,
		can_project_logical_model: conceptualAccepted,
		can_generate_outputs: modelGenerated && finalModelAccepted,
		final_model_accepted: finalModelAccepted,
	  };
}

function nextStage(): ProjectNextStage {
	  if (!conceptualAccepted) return "conceptual_review";
	  if (!modelGenerated) return "logical_model";
	  if (!finalModelAccepted) return "model_review";
	  if (!outputsReady) return "generate_outputs";
	  return "completed";
}

function project(): ProjectSummary {
  return {
    id: projectId,
    name: "Printing House Full",
    description: "PIA task specification, segment-based evidence bundle.",
    language: "sr-Cyrl",
    domain: "information_system",
	    lifecycle_status: projectCompleted ? "completed" : outputsReady ? "ready_for_dbml" : modelGenerated ? "model_generated" : conceptualAccepted ? "ready_for_model_generation" : "conceptual_review",
    current_revision: revision,
    counts: {
      resources: resources.length,
      combined_sentences: combinedDocument.summary.sentence_count,
      source_units: sourceUnits.length,
    },
    quality: {
      validation_errors: 0,
      lint_warnings: 0,
	      traceability_status: outputsReady ? "complete" : "not_generated",
	      dbml_status: outputsReady ? "ready" : "not_generated",
	    },
	    last_activity: projectCompleted ? "Project completed." : outputsReady ? "Final outputs generated." : modelGenerated ? "Logical model generated." : conceptualAccepted ? "Conceptual model accepted." : "Conceptual model proposed.",
	    created_at: now(),
	    updated_at: now(),
	    llm_execution_profile: {
	      provider: "mock", model: "mock-model", reasoning_effort: "low", max_output_tokens: 16000,
	      max_repair_attempts: 2, max_parallelism: 1, prompt_version: "mock", policy_version: "segment_description/v0.8",
	    },
	  };
}

const optimizationReport: LLMOptimizationReport = {
  version: 1, project_id: projectId, policy_version: "segment_description/v0.8", budget_policy: "adaptive_v1",
  context_policy: "minimal_context_v1", call_gate_policy: "stage_gate_v1", risk_policy: "review_risk_value_v1",
  totals: { runs: 2, provider_calls: 2, cache_hits: 0, retries: 0, input_tokens: 4200, output_tokens: 1800, total_tokens: 6000, wasted_tokens: 0, unknown_usage_attempts: 0, context_bytes: 9000, full_context_bytes: 9000, context_bytes_saved: 0, context_reduction_ratio: 0 },
  by_stage: {}, calls_by_reason: { source_segmentation: 1, conceptual_model: 1 },
};

const bundles: BundleCandidate[] = [
  {
    id: "fixtures__golden__bus_v06",
    name: "Bus Transport DB-DSL v0.6",
    description: "Golden DB-DSL v0.6 bundle for the transport demo.",
    model_path: "fixtures/golden/bus_v06/db_model.dsl.yaml",
    bundle_path: "fixtures/golden/bus_v06",
    dsl_version: "0.6",
    pipeline_version: "0.6",
    source_units: 194,
    requirements: 36,
    functional_areas: 9,
    operations: 26,
    entities: 23,
    relationships: 36,
    validation_errors: 0,
    lint_warnings: 0,
    dbml_status: "ready",
  },
];

function mockJob(stage: string): Job {
	  const job: Job = {
	    id: `job_mock_${++jobSequence}`,
    project_id: projectId,
    type: stage,
    stage,
    status: "queued",
    events_url: "/mock",
    attempt: 1,
    progress: 0,
    message: "Job queued.",
    input_revision: revision,
    created_at: now(),
    updated_at: now(),
	  };
	  mockJobs.set(job.id, job);
	  return job;
}

function completeMockStage(stage: string) {
  switch (stage) {
    case "process_sources":
    case "conceptual_model":
      conceptualAccepted = false;
      modelGenerated = false;
      finalModelAccepted = false;
      outputsReady = false;
      projectCompleted = false;
	  if (mockReview) reviewStatus = "stale";
      break;
	case "adversarial_review":
	  generateMockReview();
	  break;
	case "conceptual_correction": {
	  conceptualAccepted = false;
	  modelGenerated = false;
	  finalModelAccepted = false;
	  outputsReady = false;
	  projectCompleted = false;
	  if (mockReview) {
		const beforeHash = mockReview.candidate_hash;
		reviewStatus = "stale";
		appendAudit({
			review_id: mockReview.review_id,
			action: "correction_proposed",
			actor: "adversarial_reviewer",
			candidate_hash: beforeHash,
			source_hash: mockReview.source_hash,
			before_candidate_hash: beforeHash,
			after_candidate_hash: `sha256:mock-candidate-r${revision + 1}`,
			before_model_summary: "One modeled actor represents the client role.",
			after_model_summary: "Client, printer, and web administrator roles are represented explicitly.",
			result: "new_proposal",
		});
	  }
	  break;
	}
    case "logical_model":
      modelGenerated = true;
      break;
    case "generate_outputs":
      outputsReady = true;
      break;
  }
  revision += 1;
}

const projectPrefix = `/projects/${projectId}`;

export const mockApi = {
  async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const method = init.method ?? "GET";
    if (path === "/llm/status") {
      return {
        available: true,
        default_model: "mock-model",
        mock_available: true,
        provider: "mock",
		pipeline_version: pipelineVersion,
      } as T;
    }
	    if (path === "/bundles") {
	      return { items: bundles } as T;
	    }
	    if (path === "/projects" && method === "POST") {
	      projectDeleted = false;
	      conceptualAccepted = false;
	      modelGenerated = false;
	      finalModelAccepted = false;
	      outputsReady = false;
	      projectCompleted = false;
	      resetReviewState();
	      revision += 1;
	      return { project: project() } as T;
	    }
    if (path === "/bundles/scaffold-from-task" && method === "POST") {
      return {
        bundle: {
          id: "poc__generated__mock__v0_6",
          name: "Mock Scaffold",
          description: "Importable mock scaffold generated from task text.",
          model_path: "poc/generated/mock/v0.6/db_model.dsl.yaml",
          bundle_path: "poc/generated/mock/v0.6",
          dsl_version: "0.6",
          pipeline_version: "0.6",
          source_units: 4,
          requirements: 0,
          functional_areas: 0,
          operations: 0,
          entities: 2,
          relationships: 1,
          validation_errors: 0,
          lint_warnings: 0,
          dbml_status: "ready",
        },
      } as T;
    }
	    if (path === "/projects/import-bundle" && method === "POST") {
	      conceptualAccepted = true;
	      modelGenerated = true;
	      finalModelAccepted = false;
	      outputsReady = true;
	      projectCompleted = false;
	      resetReviewState();
	      return {
	        project: {
	          ...project(),
          counts: { ...project().counts, entities: 23, relationships: 36 },
          quality: { ...project().quality, traceability_status: "complete", dbml_status: "ready" },
          last_activity: "Mock v0.6 bundle imported.",
        },
      } as T;
    }
    if (path.startsWith("/projects?")) {
      return { items: projectDeleted ? [] : [project()], page: { limit: 50, next_cursor: null } } as T;
    }
    if (path === projectPrefix && method === "DELETE") {
      projectDeleted = true;
      return { deleted_project_id: projectId, message: "Project deleted." } as T;
    }
    if (path === projectPrefix && !projectDeleted) {
      return { project: project(), artifact_health: health() } as T;
    }
		if (path === `${projectPrefix}/stages`) {
			return { artifact_health: health(), next_stage: nextStage() } as T;
		}
		if (/\/stages\/[^/]+\/run$/.test(path) && method === "POST") {
			const stage = path.split("/").at(-2) ?? "mock";
			return { job: mockJob(stage) } as T;
		}
		if (path === `${projectPrefix}/process-sources` && method === "POST") {
			return { job: mockJob("process_sources") } as T;
		}
			if (path === `${projectPrefix}/jobs`) return { items: [...mockJobs.values()].reverse() } as T;
			if (/\/projects\/[^/]+\/jobs\/job_mock_[0-9]+$/.test(path)) {
				const id = path.split("/").at(-1) ?? "";
				const job = mockJobs.get(id);
				if (!job) throw new Error(`Mock job not found: ${id}`);
				return { job } as T;
		}
		if (path === `${projectPrefix}/llm-runs`) return { items: [] } as T;
		if (path === `${projectPrefix}/optimization-report`) return { report: optimizationReport } as T;
		if (path === `${projectPrefix}/adversarial-review` && method === "GET") {
			return adversarialReviewResponse() as T;
		}
		if (path === `${projectPrefix}/adversarial-review/decisions` && method === "POST") {
			if (!mockReview || reviewStatus !== "current") throw new Error("Only findings from the current review can be decided.");
			const body = JSON.parse(String(init.body)) as {
				base_revision: number;
				actor: AdversarialReviewActor;
				decisions: Array<{ finding_id: string; decision: AdversarialReviewDecision["decision"]; note: string }>;
			};
			if (body.decisions.some((decision) => !decision.note.trim())) throw new Error("Every decision requires a note.");
			revision += 1;
			for (const item of body.decisions) {
				const decision: AdversarialReviewDecision = {
					review_id: mockReview.review_id,
					finding_id: item.finding_id,
					decision: item.decision,
					note: item.note,
					actor: body.actor,
					decided_at: now(),
					project_revision: revision,
				};
				reviewDecisions.push(decision);
				appendAudit({
					review_id: mockReview.review_id,
					finding_id: item.finding_id,
					action: "finding_decided",
					actor: body.actor,
					note: item.note,
					candidate_hash: mockReview.candidate_hash,
					source_hash: mockReview.source_hash,
					reason: item.note,
					result: item.decision,
				});
			}
			return adversarialReviewResponse() as T;
		}
		if (path === `${projectPrefix}/adversarial-review/findings` && method === "POST") {
			if (!mockReview || reviewStatus !== "current") throw new Error("Operator findings require a current adversarial review.");
			const body = JSON.parse(String(init.body)) as {
				base_revision: number;
				actor: AdversarialReviewActor;
				note: string;
				finding: Omit<AdversarialReviewFinding, "id">;
			};
			if (!body.note.trim()) throw new Error("An operator finding requires an audit note.");
			const currentReview = mockReview;
			const finding: AdversarialReviewFinding = {
				...body.finding,
				id: `OF-${String(reviewFindingProvenance.filter((item) => item.origin === "operator").length + 1).padStart(3, "0")}`,
			};
			revision += 1;
			currentReview.findings.push(finding);
			reviewFindingProvenance.push({
				finding_id: finding.id,
				review_id: currentReview.review_id,
				origin: "operator",
				actor: body.actor,
				note: body.note,
				created_at: now(),
				project_revision: revision,
			});
			appendAudit({
				review_id: currentReview.review_id,
				finding_id: finding.id,
				action: "operator_finding_added",
				actor: body.actor,
				note: body.note,
				candidate_hash: currentReview.candidate_hash,
				source_hash: currentReview.source_hash,
				reason: body.note,
				result: "finding_added",
			});
			return adversarialReviewResponse() as T;
		}
		if (path === `${projectPrefix}/adversarial-review/correct` && method === "POST") {
			if (!mockReview || reviewStatus !== "current" || ![...currentReviewDecisions().values()].some((decision) => decision.decision === "request_correction")) {
				throw new Error("Record a correction request on a current finding first.");
			}
			const body = JSON.parse(String(init.body)) as { actor: AdversarialReviewActor; note: string };
			if (!body.note.trim()) throw new Error("A correction instruction is required.");
			revision += 1;
			appendAudit({
				review_id: mockReview.review_id,
				action: "correction_requested",
				actor: body.actor,
				note: body.note,
				candidate_hash: mockReview.candidate_hash,
				source_hash: mockReview.source_hash,
				before_candidate_hash: mockReview.candidate_hash,
				reason: body.note,
				result: "job_started",
			});
			return { project_revision: revision, job: mockJob("conceptual_correction") } as T;
		}
		if (path === `${projectPrefix}/conceptual-model/accept` && method === "POST") {
			const state = adversarialReviewResponse();
			if (!state.can_accept || !mockReview) throw new Error("Current adversarial review decisions are required before acceptance.");
			const body = JSON.parse(String(init.body)) as { actor: AdversarialReviewActor; note: string };
			if (!body.note.trim()) throw new Error("An acceptance reason is required.");
			conceptualAccepted = true;
			revision += 1;
			appendAudit({
				review_id: mockReview.review_id,
				action: "conceptual_accepted",
				actor: body.actor,
				note: body.note,
				candidate_hash: mockReview.candidate_hash,
				source_hash: mockReview.source_hash,
				before_candidate_hash: mockReview.candidate_hash,
				after_candidate_hash: mockReview.candidate_hash,
				reason: body.note,
				result: "accepted",
			});
			return { project_revision: revision, message: "Conceptual model accepted." } as T;
		}
		if (path === `${projectPrefix}/conceptual-model`) {
			return {
				conceptual_model: conceptualModel,
				accepted: conceptualAccepted,
				diff: {},
				qa: { ok: true, errors: [], warnings: [], coverage: { description_segments: 2, description_covered_segments: 2, description_uncovered_segments: 0 } },
				description: conceptualDescription,
			} as T;
		}
		if (path === `${projectPrefix}/model-acceptance` && method === "POST") {
			finalModelAccepted = true;
			revision += 1;
			return { project_revision: revision, message: "Final model accepted." } as T;
		}
		if (path === `${projectPrefix}/complete` && method === "POST") {
			projectCompleted = true;
			revision += 1;
			return { project: project() } as T;
		}
		if (path === `${projectPrefix}/reopen` && method === "POST") {
			projectCompleted = false;
			finalModelAccepted = false;
			outputsReady = false;
			revision += 1;
			return { project: project(), source_snapshot_id: `snapshot_${projectId}` } as T;
		}
		if (path === `${projectPrefix}/source-units/qa`) {
			return { qa: sourceUnitQA } as T;
		}
		if (/\/projects\/[^/]+\/source-units\/[^/]+\/review$/.test(path) && method === "POST") {
			return { project_revision: revision + 1, source_unit: { ...sourceUnits[1], review_status: "reviewed" }, remaining_needs_attention: 0 } as T;
		}
    if (path.includes("/source-units")) {
      return { project_revision: revision, items: sourceUnits, page: { limit: 50, next_cursor: null } } as T;
    }
	    if (path === `${projectPrefix}/resources` && method === "POST") {
	      revision += 1;
	      return { resource: resources[0], project_revision: revision } as T;
	    }
	    if (path === `${projectPrefix}/resources/upload` && method === "POST") {
	      revision += 1;
	      return { resource: resources[0], project_revision: revision } as T;
	    }
	    if (path === `${projectPrefix}/resources`) {
	      return { items: resources } as T;
	    }
    if (path === `${projectPrefix}/resources/R-001/text`) {
      return { resource: resources[0], text: `${segments.map((segment) => segment.text).join("\n")}\n` } as T;
    }
    if (path === `${projectPrefix}/source-manifest`) {
      return { manifest: sourceManifest } as T;
    }
    if (path === `${projectPrefix}/combined-document`) {
      return { combined_document: combinedDocument } as T;
    }
	if (path === `${projectPrefix}/source-segmentation`) {
		return sourceSegmentation as T;
	}
	    if (path === `${projectPrefix}/model-graph`) {
	      if (!modelGenerated) throw new Error("Model is not generated in the mock fixture.");
	      return { project_revision: revision, model_graph: modelGraph } as T;
	    }
	    if (path === `${projectPrefix}/trace-index`) {
	      if (!modelGenerated) throw new Error("Model is not generated in the mock fixture.");
	      return { project_revision: revision, trace_index: traceIndex } as T;
	    }
	    if (path.startsWith(`${projectPrefix}/model-elements/`)) {
	      if (!modelGenerated) throw new Error("Model is not generated in the mock fixture.");
	      return { model_element: modelElementDetails(decodeURIComponent(path.split("/").at(-1) ?? "")) } as T;
	    }
	    if (path === `${projectPrefix}/logical-mapping-report`) {
	      if (!modelGenerated) throw new Error("Model is not generated in the mock fixture.");
	      return { logical_mapping_report: logicalMappingReport } as T;
	    }
    if (path.includes("/quality")) {
      return {
        project_revision: revision,
        quality: {
          summary: {
            validation_errors: 0,
            lint_warnings: 0,
            lint_info: 0,
            blocking_issues: 0,
	            traceability_status: outputsReady ? "complete" : "not_generated",
	            dbml_status: outputsReady ? "ready" : "not_generated",
          },
          issues: [],
        },
      } as T;
    }
	    if (path === `${projectPrefix}/dbml/regenerate` && method === "POST") {
	      return { job: mockJob("generate_outputs") } as T;
	    }
	    if (path.includes("/dbml")) {
      return { project_revision: revision, dbml: "Project printing_house_full {}" } as T;
    }
    if (path.includes("/sql")) {
      return { dialect: "postgresql", sql: "-- mock schema\n" } as T;
    }
    if (method === "POST") {
      return { job: mockJob("mock") } as T;
    }
	    throw new Error(`Mock endpoint not implemented: ${path}`);
	  },

	  exportURL(kind: "dbml" | "sql" | "report" | "bundle") {
	    const content = kind === "dbml"
	      ? "Project printing_house_v06 {\n  database_type: 'PostgreSQL'\n}\n"
	      : kind === "sql"
	        ? "CREATE TABLE users (id bigint PRIMARY KEY, username varchar(255) NOT NULL);\n"
	        : kind === "report"
	          ? "# Mock traceability report\n\nSU-002 -> User\nSU-003 -> Order\n"
	          : JSON.stringify({ fixture: "mock", dsl_version: "0.6", files: ["final.dbml", "schema.postgresql.sql", "traceability_report.md"] }, null, 2);
	    const mime = kind === "report" ? "text/markdown" : kind === "bundle" ? "application/json" : "text/plain";
	    return `data:${mime};charset=utf-8,${encodeURIComponent(content)}`;
	  },

	  subscribeToJob(
    _projectId: string,
	    mockJobId: string,
    onEvent: (event: JobEvent) => void,
		onDone?: (event?: JobEvent) => void,
  ) {
	    const active = mockJobs.get(mockJobId);
	    const stage = active?.stage ?? active?.type ?? "mock";
	    const timers = [25, 250, 520].map((delay, index) =>
	      globalThis.setTimeout(() => {
	      const completed = index === 2;
	      if (completed) completeMockStage(stage);
			const event: JobEvent = {
	          job_id: mockJobId,
	          type: stage,
	          status: completed ? "completed" : "running",
	          message: completed ? "Job completed." : "Running mock job.",
	          progress: completed ? 100 : 30 + index * 25,
	          project_revision: completed ? revision : undefined,
	          created_at: now(),
			};
			const existing = mockJobs.get(mockJobId);
			if (existing) mockJobs.set(mockJobId, {
			  ...existing,
			  status: event.status,
			  progress: event.progress,
			  message: event.message,
			  output_revision: event.project_revision,
			  updated_at: event.created_at,
			  completed_at: completed ? event.created_at : undefined,
			});
			onEvent(event);
			if (completed) onDone?.(event);
      }, delay),
    );
	    return () => timers.forEach((timer) => globalThis.clearTimeout(timer));
	  },
};
