import type {
  BundleCandidate,
  CombinedDocument,
  InputResource,
  JobEvent,
  ProjectSummary,
  ReviewCandidate,
  SourceManifest,
	SourceSegmentationProposal,
	SourceSegmentationQA,
  SourceUnit,
} from "@/shared/api/types";

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
    line_count: 194,
    warnings: [],
    extraction_status: "ready",
    extraction_confidence: "high",
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
];

const sourceManifest: SourceManifest = {
  document: {
    id: "project_phf_source_manifest",
    project_id: "project_phf",
    project_name: "Printing House Full",
	pipeline_version: "0.7.4",
    workspace_path: ".dbdsl_workbench/projects/project_phf",
    generated_at: new Date().toISOString(),
  },
  resources,
  summary: {
    total: 1,
    ready: 1,
    needs_attention: 0,
    failed: 0,
    line_count: 194,
  },
};

const combinedDocument: CombinedDocument = {
  markdown: "# Combined Document\n\n[OD-S-001] Postoje  tri vrste korisnika : klijenti, stampari i administrator web sistema.\n",
  lineage: {
    document: {
      id: "project_phf_combined_document_lineage",
      project_id: "project_phf",
      project_name: "Printing House Full",
      source_manifest_file: "source_manifest.yaml",
		pipeline_version: "0.7.4",
      created_at: new Date().toISOString(),
    },
    sentences: [
      {
        id: "OD-S-001",
        kind: "sentence",
        text: "Postoje  tri vrste korisnika : klijenti, stampari i administrator web sistema.",
        derived_from: [
          {
            resource_id: "R-001",
			source_segment_id: "R-001-S-0009",
            line_start: 9,
            line_end: 9,
			start_byte: 0,
			end_byte: 81,
            exact_text: "Postoje  tri vrste korisnika : klijenti, stampari i administrator web sistema.",
          },
        ],
        transformation: "copied",
		role: "semantic",
		segmentation_strategy: "llm_candidate_grouping_v1",
        confidence: "high",
        warnings: [],
      },
    ],
    warnings: [],
    confidence_summary: { overall: "mock combined document" },
  },
  summary: {
    status: "ready",
	unit_count: 1,
    sentence_count: 1,
	structural_unit_count: 0,
    resource_count: 1,
    warning_count: 0,
	segmentation_strategy: "llm_candidate_grouping_v1",
	llm_assisted: true,
	fallback_used: false,
	needs_attention_count: 0,
	layout_segment_count: 0,
  },
};

const sourceSegmentation: { proposal: SourceSegmentationProposal; qa: SourceSegmentationQA } = {
	proposal: {
		candidates: [{
			id: "SC-000001", segment_id: "R-001-S-0009", resource_id: "R-001",
			line_start: 9, line_end: 9, start_byte: 0, end_byte: 81,
			exact_text: "Postoje  tri vrste korisnika : klijenti, stampari i administrator web sistema.", suggested_role: "semantic",
		}],
		groups: [{ id: "SG-000001", role: "semantic", candidate_ids: ["SC-000001"], confidence: "high", requires_review: false, warnings: [], od_sentence_id: "OD-S-001" }],
		strategy: "llm_candidate_grouping_v1", llm_assisted: true, fallback_used: false, warnings: [], confidence_summary: { overall: "high" },
	},
	qa: {
		ok: true, strategy: "llm_candidate_grouping_v1", candidates_total: 1, candidates_assigned: 1,
		semantic_groups: 1, structural_groups: 0, layout_groups: 0, needs_attention: [], errors: [], warnings: [],
		role_counts: { semantic: 1 }, fallback_used: false,
	},
};

const project: ProjectSummary = {
  id: "project_phf",
  name: "Printing House Full",
  description: "PIA task specification, v0.5 sentence-granularity evidence bundle.",
  language: "sr-Cyrl",
  domain: "information_system",
  lifecycle_status: "analysis_review",
  current_revision: 17,
    counts: {
    resources: resources.length,
    combined_sentences: combinedDocument.summary.sentence_count,
    source_units: 194,
    examples: 2,
    requirements: 36,
    functional_areas: 9,
    operations: 26,
    open_review_questions: 2,
    review_decisions: 11,
  },
  quality: {
    validation_errors: 0,
    lint_warnings: 0,
    traceability_status: "complete",
    dbml_status: "not_generated",
  },
  last_activity: "Mock analysis bundle loaded.",
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
};

let projectDeleted = false;

const reviewCandidates: ReviewCandidate[] = [
  {
    id: "PHF-RC-DEMO-001",
    question: "Da li korisnicke uloge modelovati kao jednu tabelu naloga ili kao odvojene tabele po tipu korisnika?",
    description: "Odluka utice na UserAccount, Institution i tok registracije.",
    status: "open",
	blocking: true,
    affected_atoms: ["PHF-RA-001", "PHF-RA-006"],
    depends_on: [],
    may_affect: ["Requirements", "Functional / CRUD", "Review Queue"],
    recommended_option_id: "single_user_account",
    options: [
      {
        id: "single_user_account",
        label: "Jedna tabela naloga sa rolom",
        recommended: true,
        rationale: "Podrzava zajednicku autentifikaciju.",
      },
      {
        id: "separate_role_tables",
        label: "Odvojene tabele po ulozi",
        rationale: "Jasnije razdvaja profile, ali duplira login podatke.",
      },
    ],
  },
];

const sourceUnits: SourceUnit[] = [
  {
    id: "PHF-GSU-005",
    kind: "sentence",
    section: "document_title",
    normalized_text: "Postoje tri vrste korisnika: klijenti, stampari i administrator web sistema.",
    normalization: {
      version: "source_text_normalizer_v1",
      strategy: "backend_deterministic",
      exact_hash: "sha256:mock-exact-source-text",
      normalized_hash: "sha256:mock-normalized-source-text",
      changed: true,
      operations: [
        {
          kind: "compact_whitespace",
          before: "Postoje  tri vrste korisnika : klijenti, stampari i administrator web sistema.",
          after: "Postoje tri vrste korisnika : klijenti, stampari i administrator web sistema.",
        },
        {
          kind: "normalize_punctuation_spacing",
          before: "Postoje tri vrste korisnika : klijenti, stampari i administrator web sistema.",
          after: "Postoje tri vrste korisnika: klijenti, stampari i administrator web sistema.",
        },
      ],
    },
    exact_text: "Postoje  tri vrste korisnika : klijenti, stampari i administrator web sistema.",
    relevance: "model_relevant",
    confidence: "high",
    review_status: "open_review",
    origin_spans: [{ resource_id: "R-001", label: "TASK_FULL.md · line 9", line_start: 9, line_end: 9 }],
    linked_examples: [],
    linked_requirements: ["PHF-RA-001"],
    open_review_candidates: ["PHF-RC-DEMO-001"],
    od_sentence_ids: ["OD-S-001"],
    warnings: [],
  },
];

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

export const mockApi = {
  async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const method = init.method ?? "GET";
    if (path === "/llm/status") {
      return {
        available: true,
        default_model: "mock-model",
        mock_available: true,
        provider: "mock",
		pipeline_version: "0.7.4",
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
          requirements: 4,
          functional_areas: 1,
          operations: 1,
          entities: 2,
          relationships: 1,
          validation_errors: 0,
          lint_warnings: 0,
          dbml_status: "ready",
        },
      } as T;
    }
    if (path === "/bundles/llm-plan-from-task" && method === "POST") {
      return {
        bundle: {
          id: "poc__generated__mock_llm__v0_5",
          name: "Mock LLM Draft",
          description: "Validated mock LLM-assisted v0.5 draft generated from task text.",
          model_path: "poc/generated/mock_llm/v0.5/db_model.dsl.yaml",
          bundle_path: "poc/generated/mock_llm/v0.5",
          dsl_version: "0.5",
          pipeline_version: "0.5",
          source_units: 4,
          requirements: 4,
          functional_areas: 1,
          operations: 1,
          entities: 1,
          relationships: 0,
          validation_errors: 0,
          lint_warnings: 0,
          dbml_status: "ready",
        },
      } as T;
    }
    if (path === "/projects/import-bundle" && method === "POST") {
      return {
        project: {
          ...project,
          lifecycle_status: "ready_for_dbml",
          counts: {
            ...project.counts,
            open_review_questions: 0,
            entities: 23,
            relationships: 36,
          },
          quality: {
            ...project.quality,
            dbml_status: "ready",
          },
          last_activity: "Mock v0.5 bundle imported.",
        },
      } as T;
    }
    if (path.startsWith("/projects?")) {
      return { items: projectDeleted ? [] : [project], page: { limit: 50, next_cursor: null } } as T;
    }
    if (path === "/projects/project_phf" && method === "DELETE") {
      projectDeleted = true;
      return { deleted_project_id: project.id, message: "Project deleted." } as T;
    }
    if (path === "/projects/project_phf" && !projectDeleted) {
      return {
        project,
        artifact_health: {
          analysis_status: "needs_attention",
          source_manifest_status: "ready",
          combined_document_status: "ready",
			source_segmentation_status: "ready",
			source_fidelity_status: "ready",
			source_units_status: "ready",
			requirement_atoms_status: "ready",
			functional_analysis_status: "ready",
			crud_mapping_status: "ready",
			review_candidates_status: "ready",
			conceptual_model_status: "not_generated",
          model_status: "not_generated",
          dbml_status: "not_generated",
          open_review_questions: project.counts.open_review_questions,
          can_generate_model: false,
          can_continue_to_dbml: false,
          can_complete_project: false,
			can_generate_source_units: true,
			can_extract_requirements: true,
			can_build_functional_analysis: true,
			can_build_crud_mapping: true,
			can_propose_review_candidates: true,
			can_project_logical_model: false,
			can_generate_outputs: false,
			final_model_accepted: false,
        },
      } as T;
    }
		if (path === "/projects/project_phf/stages") {
			return {
				artifact_health: {
					analysis_status: "needs_attention", source_manifest_status: "ready", combined_document_status: "ready",
					source_segmentation_status: "ready", source_fidelity_status: "ready",
					source_units_status: "ready", requirement_atoms_status: "ready", functional_analysis_status: "ready", crud_mapping_status: "ready",
					review_candidates_status: "ready", conceptual_model_status: "not_generated", model_status: "not_generated", dbml_status: "not_generated",
					open_review_questions: 1, can_generate_model: false, can_continue_to_dbml: false, can_complete_project: false,
					can_generate_source_units: true, can_extract_requirements: true, can_build_functional_analysis: true, can_build_crud_mapping: true,
					can_propose_review_candidates: true, can_project_logical_model: false, can_generate_outputs: false, final_model_accepted: false,
				},
				next_stage: "review_decisions",
			} as T;
		}
		if (path === "/projects/project_phf/jobs") return { items: [] } as T;
		if (/\/projects\/[^/]+\/jobs\/job_mock$/.test(path)) {
			return {
				job: {
					id: "job_mock",
					project_id: project.id,
					type: "mock",
					stage: "mock",
					status: "queued",
					events_url: "/mock",
					attempt: 1,
					progress: 0,
					message: "Job queued.",
					input_revision: project.current_revision,
					created_at: new Date().toISOString(),
					updated_at: new Date().toISOString(),
				},
			} as T;
		}
		if (path === "/projects/project_phf/llm-runs") return { items: [] } as T;
		if (path === "/projects/project_phf/optimization-report") return { report: {
			version: 1, project_id: project.id, policy_version: "design_obligations/v0.7.1", budget_policy: "adaptive_v1",
			context_policy: "minimal_context_v1", call_gate_policy: "semantic_need_v1", risk_policy: "review_risk_value_v1",
			totals: { runs: 0, provider_calls: 0, cache_hits: 0, retries: 0, input_tokens: 0, output_tokens: 0, total_tokens: 0, wasted_tokens: 0, unknown_usage_attempts: 0, context_bytes: 0, full_context_bytes: 0, context_bytes_saved: 0, context_reduction_ratio: 0 },
			by_stage: {}, calls_by_reason: {}, avoided_review_resolution_calls: 0, avoided_generation_calls: 0, auto_applied_decisions: 0,
			batched_manual_decisions: 0, active_review_ms: 0, unresolved_review_questions: 1,
		} } as T;
		if (path === "/projects/project_phf/conceptual-model") {
			return { conceptual_model: { entity_concepts: [], relationships: [], lifecycle_concepts: [], derived_concepts: [], file_concepts: [], import_concepts: [], unresolved_review_ids: [], warnings: [], confidence_summary: {} }, qa: { ok: true, errors: [], warnings: [], coverage: {} } } as T;
		}
		if (path === "/projects/project_phf/source-units/qa") {
			return { qa: { ok: true, derivation_strategy: "llm_classification_backend_normalization", od_sentences_total: 1, od_sentences_referenced: 1, unreferenced_od_sentences: [], needs_attention: [], origin_chains: { "PHF-GSU-005": ["OD-S-001", "R-001"] }, errors: [], warnings: [], review_decisions: [] } } as T;
		}
		if (/\/projects\/[^/]+\/source-units\/[^/]+\/review$/.test(path) && method === "POST") {
			return { project_revision: project.current_revision + 1, source_unit: { ...sourceUnits[0], review_status: "reviewed" }, remaining_needs_attention: 0 } as T;
		}
    if (path.includes("/source-units")) {
      return { project_revision: project.current_revision, items: sourceUnits, page: { limit: 50, next_cursor: null } } as T;
    }
    if (path === "/projects/project_phf/resources") {
      return { items: resources } as T;
    }
    if (path === "/projects/project_phf/resources/R-001") {
      return { resource: resources[0] } as T;
    }
    if (path === "/projects/project_phf/resources/R-001/text") {
      return { resource: resources[0], text: "Postoje tri vrste korisnika: klijenti, stampari i administrator web sistema.\n" } as T;
    }
    if (path === "/projects/project_phf/source-manifest") {
      return { manifest: sourceManifest } as T;
    }
    if (path === "/projects/project_phf/combined-document") {
      return { combined_document: combinedDocument } as T;
    }
	if (path === "/projects/project_phf/source-segmentation") {
		return sourceSegmentation as T;
	}
    if (path === "/projects/project_phf/source-fidelity") {
      return { source_fidelity: {
        ok: true,
		pipeline_version: "0.7.4",
        segments_total: 194,
        normative_segments: 194,
        normative_covered: 194,
        normative_coverage: 1,
        disposition_counts: { retained: 194 },
        uncovered_segment_ids: [],
        needs_attention: [],
        dispositions: [{ segment_id: "R-001-S-0001", status: "retained", od_sentence_ids: ["OD-S-001"] }],
        errors: [],
        warnings: [],
      } } as T;
    }
    if (path.includes("/examples")) {
      return { project_revision: project.current_revision, items: [] } as T;
    }
    if (path.includes("/requirements")) {
      return {
        project_revision: project.current_revision,
        coverage: { source_units_total: 194, source_units_covered: 1, requirements_needing_review: 1 },
        items: [],
        page: { limit: 50, next_cursor: null },
      } as T;
    }
    if (path.includes("/functional-areas")) {
      return { project_revision: project.current_revision, items: [] } as T;
    }
    if (path.includes("/actors")) {
      return { project_revision: project.current_revision, items: [] } as T;
    }
    if (path.includes("/crud-operations")) {
      return { project_revision: project.current_revision, items: [] } as T;
    }
    if (path.includes("/review-candidates") && method === "GET") {
      return { project_revision: project.current_revision, items: reviewCandidates, page: { limit: 50, next_cursor: null } } as T;
    }
		if (path.endsWith("/review-decisions/batch") && method === "POST") {
			return { job: { id: "job_mock", project_id: project.id, type: "mock", stage: "apply_review_decision_batch", status: "queued", events_url: "/mock", attempt: 1, progress: 0, message: "Job queued.", input_revision: project.current_revision, created_at: new Date().toISOString(), updated_at: new Date().toISOString() } } as T;
		}
    if (path.includes("/review-decisions")) {
      return { project_revision: project.current_revision, items: [] } as T;
    }
    if (path.includes("/model-generation/readiness")) {
      return {
        project_revision: project.current_revision,
        readiness: {
          can_generate_model: false,
          open_review_questions: 1,
          blocking_reasons: ["1 open review questions"],
        },
      } as T;
    }
    if (path.includes("/quality")) {
      return {
        project_revision: project.current_revision,
        quality: {
          summary: {
            validation_errors: 0,
            lint_warnings: 0,
            lint_info: 0,
            blocking_issues: 0,
            traceability_status: "complete",
            dbml_status: "not_generated",
          },
          issues: [],
        },
      } as T;
    }
    if (path.includes("/dbml")) {
      return { project_revision: project.current_revision, dbml: "Project printing_house_full {}" } as T;
    }
    if (method === "POST") {
      return {
        job: {
          id: "job_mock",
			project_id: project.id,
          type: "mock",
			stage: "mock",
          status: "queued",
          events_url: "/mock",
			attempt: 1,
			progress: 0,
			input_revision: project.current_revision,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        },
      } as T;
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
          created_at: new Date().toISOString(),
		};
		onEvent(event);
		if (index === 2) onDone?.(event);
      }, delay),
    );
    return () => timers.forEach((timer) => window.clearTimeout(timer));
  },
};
