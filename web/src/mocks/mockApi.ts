import type {
  ArtifactHealth,
  BundleCandidate,
  CombinedDocument,
  ConceptualDescription,
  ConceptualModel,
  InputResource,
  Job,
  JobEvent,
  LLMOptimizationReport,
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
    content_path: "poc/printing_house_full/v0.5_granularity_sentance/TASK_FULL.md",
    extracted_text_path: "poc/printing_house_full/v0.5_granularity_sentance/TASK_FULL.md",
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

let projectDeleted = false;
let conceptualAccepted = false;
let revision = 4;

function health(): ArtifactHealth {
  return {
    source_manifest_status: "ready",
    combined_document_status: "ready",
	source_segmentation_status: "ready",
	source_units_status: "ready",
	conceptual_model_status: conceptualAccepted ? "ready" : "proposed",
    model_status: "not_generated",
    dbml_status: "not_generated",
    can_continue_to_dbml: false,
    can_complete_project: false,
	can_project_logical_model: conceptualAccepted,
	can_generate_outputs: false,
	final_model_accepted: false,
  };
}

function nextStage(): ProjectNextStage {
  return conceptualAccepted ? "logical_model" : "conceptual_review";
}

function project(): ProjectSummary {
  return {
    id: projectId,
    name: "Printing House Full",
    description: "PIA task specification, segment-based evidence bundle.",
    language: "sr-Cyrl",
    domain: "information_system",
    lifecycle_status: conceptualAccepted ? "ready_for_model_generation" : "conceptual_review",
    current_revision: revision,
    counts: {
      resources: resources.length,
      combined_sentences: combinedDocument.summary.sentence_count,
      source_units: sourceUnits.length,
    },
    quality: {
      validation_errors: 0,
      lint_warnings: 0,
      traceability_status: "not_generated",
      dbml_status: "not_generated",
    },
    last_activity: conceptualAccepted ? "Conceptual model accepted." : "Conceptual model proposed.",
    created_at: now(),
    updated_at: now(),
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
    id: "poc__printing_house_full__v0_5_granularity_sentance",
    name: "Printing House Full v0.5 Granularity Sentence Fresh",
    description: "Fresh v0.5 full-system logical relational model generated from task text.",
    model_path: "poc/printing_house_full/v0.5_granularity_sentance/db_model.dsl.yaml",
    bundle_path: "poc/printing_house_full/v0.5_granularity_sentance",
    dsl_version: "0.5",
    pipeline_version: "0.5",
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
  return {
    id: "job_mock",
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
    if (path === "/bundles/scaffold-from-task" && method === "POST") {
      return {
        bundle: {
          id: "poc__generated__mock__v0_5",
          name: "Mock Scaffold",
          description: "Importable mock scaffold generated from task text.",
          model_path: "poc/generated/mock/v0.5/db_model.dsl.yaml",
          bundle_path: "poc/generated/mock/v0.5",
          dsl_version: "0.5",
          pipeline_version: "0.5",
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
      return {
        project: {
          ...project(),
          lifecycle_status: "ready_for_dbml",
          counts: { ...project().counts, entities: 23, relationships: 36 },
          quality: { ...project().quality, traceability_status: "complete", dbml_status: "ready" },
          last_activity: "Mock v0.5 bundle imported.",
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
		if (path === `${projectPrefix}/jobs`) return { items: [] } as T;
		if (/\/projects\/[^/]+\/jobs\/job_mock$/.test(path)) {
			return { job: mockJob("mock") } as T;
		}
		if (path === `${projectPrefix}/llm-runs`) return { items: [] } as T;
		if (path === `${projectPrefix}/optimization-report`) return { report: optimizationReport } as T;
		if (path === `${projectPrefix}/conceptual-model/accept` && method === "POST") {
			conceptualAccepted = true;
			revision += 1;
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
		if (path === `${projectPrefix}/source-units/qa`) {
			return { qa: sourceUnitQA } as T;
		}
		if (/\/projects\/[^/]+\/source-units\/[^/]+\/review$/.test(path) && method === "POST") {
			return { project_revision: revision + 1, source_unit: { ...sourceUnits[1], review_status: "reviewed" }, remaining_needs_attention: 0 } as T;
		}
    if (path.includes("/source-units")) {
      return { project_revision: revision, items: sourceUnits, page: { limit: 50, next_cursor: null } } as T;
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
    // The fixture stops before the logical model, so model artifacts are absent.
    if (path.includes("/model-graph") || path.includes("/trace-index") || path.includes("/model-elements/") || path.includes("/logical-mapping-report")) {
      throw new Error("Model is not generated in the mock fixture.");
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
            traceability_status: "not_generated",
            dbml_status: "not_generated",
          },
          issues: [],
        },
      } as T;
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

  subscribeToJob(
    _projectId: string,
    _jobId: string,
    onEvent: (event: JobEvent) => void,
		onDone?: (event?: JobEvent) => void,
  ) {
    const timers = [25, 250, 520].map((delay, index) =>
      window.setTimeout(() => {
		const event: JobEvent = {
          job_id: "job_mock",
          type: "mock",
          status: index === 2 ? "completed" : "running",
          message: index === 2 ? "Job completed." : "Running mock job.",
          progress: index === 2 ? 100 : 30 + index * 25,
          created_at: now(),
		};
		onEvent(event);
		if (index === 2) onDone?.(event);
      }, delay),
    );
    return () => timers.forEach((timer) => window.clearTimeout(timer));
  },
};
