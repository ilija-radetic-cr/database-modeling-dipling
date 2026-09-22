export type ProjectLifecycleStatus =
  | "intake"
  | "sources_processed"
	| "source_review"
  | "analysis_review"
  | "ready_for_model_generation"
  | "model_generated"
  | "ready_for_dbml"
  | "completed";

export interface Page<T> {
  items: T[];
  page?: {
    limit: number;
    next_cursor: string | null;
  };
}

export interface ProjectSummary {
  id: string;
  name: string;
  description?: string;
  language?: string;
  domain?: string;
  lifecycle_status: ProjectLifecycleStatus;
  current_revision: number;
  counts: ProjectCounts;
  quality: ProjectQualitySummary;
  last_activity?: string;
  created_at: string;
  updated_at: string;
  llm_execution_profile?: {
    provider: string;
    model: string;
    reasoning_effort: string;
    max_output_tokens: number;
    max_repair_attempts: number;
    max_parallelism: number;
    prompt_version: string;
    policy_version: string;
		budget_policy?: string;
		context_policy?: string;
		call_gate_policy?: string;
		risk_policy?: string;
		stage_output_limits?: Record<string, number>;
  };
}

export interface ProjectCounts {
  resources: number;
  combined_sentences?: number;
  source_units: number;
  examples: number;
  requirements: number;
  functional_areas: number;
  operations: number;
  open_review_questions: number;
  review_decisions: number;
  entities?: number;
  relationships?: number;
}

export interface ProjectQualitySummary {
  validation_errors: number;
  lint_warnings: number;
  traceability_status: "not_generated" | "complete" | "incomplete";
  dbml_status: "not_generated" | "ready" | "outdated" | "blocked";
}

export interface BundleCandidate {
  id: string;
  name: string;
  description?: string;
  model_path: string;
  bundle_path: string;
  dsl_version: string;
  pipeline_version?: string;
  source_units: number;
  requirements: number;
  functional_areas: number;
  operations: number;
  entities: number;
  relationships: number;
  validation_errors: number;
  lint_warnings: number;
  dbml_status: "not_generated" | "ready" | "outdated" | "blocked";
}

export interface LlmStatus {
  available: boolean;
  default_model: string;
  mock_available: boolean;
  provider: string;
  pipeline_version: string;
}

export interface ArtifactHealth {
  analysis_status: "not_started" | "processing" | "sources_ready" | "ready" | "needs_attention" | "outdated";
  source_manifest_status: "not_generated" | "ready";
  combined_document_status: "not_generated" | "ready";
	source_segmentation_status: "not_generated" | "ready" | "fallback";
	source_fidelity_status: "not_generated" | "ready" | "needs_attention";
	source_units_status: "not_generated" | "ready" | "needs_attention";
	requirement_atoms_status: "not_generated" | "ready";
	design_obligations_status: "not_generated" | "ready";
	functional_analysis_status: "not_generated" | "ready";
	crud_mapping_status: "not_generated" | "ready";
	review_candidates_status: "not_generated" | "ready";
	conceptual_model_status: "not_generated" | "proposed" | "ready";
	semantic_verification_status: "not_generated" | "passed" | "blocked" | "outdated" | "legacy_not_applicable";
	semantic_blocking_issues: number;
  model_status: "not_generated" | "generating" | "ready" | "outdated" | "failed";
  dbml_status: "not_generated" | "ready" | "outdated" | "blocked";
  open_review_questions: number;
  can_generate_model: boolean;
  can_continue_to_dbml: boolean;
  can_complete_project: boolean;
	can_generate_source_units: boolean;
	can_extract_requirements: boolean;
	can_build_functional_analysis: boolean;
	can_build_crud_mapping: boolean;
	can_propose_review_candidates: boolean;
	can_project_logical_model: boolean;
	can_generate_outputs: boolean;
	final_model_accepted: boolean;
}

export interface ProjectResponse {
  project: ProjectSummary;
  artifact_health: ArtifactHealth;
}

export interface InputResource {
  id: string;
  kind: "pasted_text" | "uploaded_file";
  file_type: "pdf" | "markdown" | "text" | "docx" | "json" | "csv" | "xml" | "unknown";
  title: string;
  file_name?: string;
  size_bytes?: number;
  content_path?: string;
  extracted_text_path?: string;
  content_hash?: string;
  extracted_text_hash?: string;
  language?: string;
  authority?: "normative" | "illustrative" | "mixed" | "unknown";
  line_count?: number;
  warnings?: string[];
  extraction_status: "not_started" | "extracting" | "ready" | "needs_attention" | "failed";
  extraction_confidence?: "high" | "medium" | "low";
  created_at: string;
  updated_at: string;
}

export interface SourceManifest {
  document: {
    id: string;
    project_id: string;
    project_name: string;
    pipeline_version: string;
    workspace_path: string;
    generated_at: string;
  };
  resources: InputResource[];
  summary: {
    total: number;
    ready: number;
    needs_attention: number;
    failed: number;
    line_count: number;
  };
}

export interface CombinedDocument {
  markdown: string;
  lineage: {
    document: {
      id: string;
      project_id: string;
      project_name: string;
      source_manifest_file: string;
      pipeline_version: string;
      created_at: string;
    };
    sentences: CombinedDocumentSentence[];
    warnings: string[];
    confidence_summary: Record<string, string>;
  };
  summary: {
    status: string;
	unit_count: number;
    sentence_count: number;
	structural_unit_count: number;
    resource_count: number;
    warning_count: number;
	segmentation_strategy: string;
	llm_assisted: boolean;
	fallback_used: boolean;
	needs_attention_count: number;
	layout_segment_count: number;
  };
}

export interface CombinedDocumentSentence {
  id: string;
  kind?: "sentence" | "structural";
	role?: "semantic" | "structural" | "layout_noise" | "example" | "metadata" | string;
  text: string;
  derived_from: CombinedDocumentOrigin[];
	transformation: "copied" | "cleaned" | "merged" | "summarized" | "llm_grouped";
	segmentation_strategy?: string;
  confidence: "high" | "medium" | "low";
  warnings: string[];
}

export interface CombinedDocumentOrigin {
  resource_id: string;
	source_segment_id?: string;
  line_start: number;
  line_end: number;
	start_byte?: number;
	end_byte?: number;
  exact_text: string;
}

export interface SourceSegmentationCandidate {
	id: string;
	segment_id: string;
	resource_id: string;
	line_start: number;
	line_end: number;
	start_byte: number;
	end_byte: number;
	exact_text: string;
	suggested_role?: string;
}

export interface SourceSegmentationGroup {
	id: string;
	role: "semantic" | "structural" | "layout_noise" | "example" | "metadata" | string;
	candidate_ids: string[];
	confidence: "high" | "medium" | "low";
	requires_review: boolean;
	warnings: string[];
	od_sentence_id?: string;
}

export interface SourceSegmentationProposal {
	candidates: SourceSegmentationCandidate[];
	groups: SourceSegmentationGroup[];
	strategy: string;
	llm_assisted: boolean;
	fallback_used: boolean;
	fallback_reason?: string;
	warnings: string[];
	confidence_summary: Record<string, string>;
}

export interface SourceSegmentationQA {
	ok: boolean;
	strategy: string;
	candidates_total: number;
	candidates_assigned: number;
	semantic_groups: number;
	structural_groups: number;
	layout_groups: number;
	needs_attention: string[];
	errors: string[];
	warnings: string[];
	role_counts: Record<string, number>;
	fallback_used: boolean;
	fallback_reason?: string;
}

export interface OriginSpan {
  resource_id: string;
  label: string;
  page?: number;
  line_start?: number;
  line_end?: number;
  start_offset?: number;
  end_offset?: number;
}

export interface SourceNormalizationOperation {
  kind: "trim_boundary_whitespace" | "compact_whitespace" | "normalize_punctuation_spacing" | string;
  before: string;
  after: string;
}

export interface SourceTextNormalization {
  version: string;
  strategy: "backend_deterministic" | string;
  exact_hash: string;
  normalized_hash: string;
  changed: boolean;
  operations: SourceNormalizationOperation[];
}

export interface SourceSegmentDisposition {
  segment_id: string;
  status: "retained" | "uncovered" | string;
  od_sentence_ids: string[];
  reason?: string;
}

export interface SourceFidelityReport {
  ok: boolean;
  pipeline_version: string;
  segments_total: number;
  normative_segments: number;
  normative_covered: number;
  normative_coverage: number;
  disposition_counts: Record<string, number>;
  uncovered_segment_ids: string[];
  needs_attention: string[];
  dispositions: SourceSegmentDisposition[];
  errors: string[];
  warnings: string[];
}

export interface SourceUnit {
  id: string;
  kind: string;
  section?: string;
  normalized_text: string;
  normalization: SourceTextNormalization;
  exact_text?: string;
  relevance: string;
  confidence: "high" | "medium" | "low";
  review_status: "reviewed" | "needs_attention" | "open_review";
  origin_spans: OriginSpan[];
  linked_examples: string[];
  linked_requirements: string[];
  open_review_candidates: string[];
	od_sentence_ids?: string[];
	warnings?: string[];
}

export interface SourceUnitQA {
	ok: boolean;
	derivation_strategy: "llm_classification_backend_normalization" | "llm" | "llm_chunked" | "deterministic_fallback";
	od_sentences_total: number;
	od_sentences_referenced: number;
	unreferenced_od_sentences: string[];
	needs_attention: string[];
	origin_chains: Record<string, string[]>;
	errors: string[];
	warnings: string[];
	review_decisions?: Array<{
		source_unit_id: string;
		decision: "accept" | "revise" | "exclude";
		previous_normalized_text: string;
		normalized_text: string;
		normalization: SourceTextNormalization;
		previous_relevance: string;
		relevance: string;
		note?: string;
		reviewed_by: string;
		reviewed_at: string;
		project_revision: number;
	}>;
}

export interface SourceUnitReviewResult {
	project_revision: number;
	source_unit: SourceUnit;
	remaining_needs_attention: number;
}

export type ProjectStageName =
	| "combined_document"
	| "source_units"
	| "requirement_atoms"
	| "functional_analysis"
	| "crud_mapping"
	| "review_candidates"
	| "conceptual_model"
	| "logical_model"
	| "semantic_verification"
	| "validation_lint"
	| "generate_outputs"
	| "export_bundle";

export interface StructuredExample {
  id: string;
  type: string;
  title: string;
  origin_spans: OriginSpan[];
  source_units: string[];
  authority: "normative" | "illustrative" | "not_sure";
  mapping_status: "not_mapped" | "partially_mapped" | "mapped";
  raw_content: string;
  parsed_fields: ParsedField[];
  open_review_candidates: string[];
}

export interface ParsedField {
  path: string;
  observed_type: string;
  sample_values: string[];
  mapped_to?: string;
}

export interface RequirementAtom {
  id: string;
  statement: string;
	subject?: string;
	predicate?: string;
	object?: string;
	quantifier?: string;
	condition?: string;
	temporal_semantics?: string;
	ownership?: string;
  atom_type: string;
  modeling_relevance: string;
  source_units: string[];
  functional_area?: string;
  functional_pattern?: string;
  support_level: string;
  confidence: "high" | "medium" | "low";
  review_status: "reviewed" | "needs_review" | "open_review";
  modeling_outcome: string;
  model_impact_preview: string[];
  open_review_candidates: string[];
}

export interface FunctionalArea {
  id: string;
  label: string;
  purpose: string;
  main_actors: string[];
  requirement_atoms: string[];
  modeling_focus: string[];
  open_review_candidates: string[];
}

export interface ActorSummary {
  id: string;
  label: string;
  kind: string;
  operation_count: number;
  functional_areas: string[];
  maps_to_user_role: boolean;
  open_review_candidates: string[];
}

export interface CrudOperation {
  id: string;
  label: string;
  actor_id: string;
  functional_area_id: string;
  creates: string[];
  reads: string[];
  updates: string[];
  deletes: string[];
  persistent_data: string[];
  outcome: string;
  requirement_atoms: string[];
  source_units: string[];
  review_status: "reviewed" | "needs_review" | "open_review";
  open_review_candidates: string[];
}

export interface ReviewCandidate {
  id: string;
	decision_key?: string;
  question: string;
  description: string;
  status: "open" | "answered" | "resolved";
  affected_atoms: string[];
  depends_on: string[];
  may_affect: string[];
  options: ReviewOption[];
  selected_option?: string;
  recommended_option_id?: string;
	category?: string;
	phase?: string;
	severity?: "low" | "medium" | "high" | "critical";
	blocking: boolean;
	affected_source_units?: string[];
	affected_functional_areas?: string[];
	affected_operations?: string[];
	affected_model_candidates?: string[];
	recommendation_confidence?: "high" | "medium" | "low";
	warnings?: string[];
}

export interface ReviewOption {
  id: string;
  label: string;
  recommended?: boolean;
  rationale: string;
	effect_summary?: string;
	benefits?: string[];
	risks?: string[];
	affected_artifact_kinds?: string[];
	effects?: {
		modeling_outcome: string;
		persistence_effect: string;
		support_level: string;
		requires_followup?: boolean;
		atom_updates?: Array<{
			atom_id: string;
			modeling_outcome: string;
			persistence_effect: string;
			support_level: string;
			confidence: string;
		}>;
		impact_dimensions?: string[];
		followup_candidate_ids?: string[];
	};
}

export interface ReviewDecision {
  id: string;
  question: string;
  affected_atoms: string[];
  selected_option: string;
  status: string;
  rationale?: string;
  reviewed_by?: string;
  reviewed_at?: string;
	decision_mode?: string;
	policy_version?: string;
	active_review_ms?: number;
}

export interface EvidenceRef {
  source_units: string[];
  requirement_atoms: string[];
  review_decisions: string[];
  support_level?: string;
  confidence?: string;
}

export interface ConceptualAttribute {
	id: string;
	label: string;
	description: string;
	required: boolean;
	evidence: EvidenceRef;
}

export interface ConceptualEntity {
	id: string;
	label: string;
	description: string;
	kind: string;
	attributes: ConceptualAttribute[];
	evidence: EvidenceRef;
}

export interface ConceptualRelationship {
	id: string;
	label: string;
	description: string;
	from: string;
	to: string;
	cardinality: string;
	evidence: EvidenceRef;
}

export interface ConceptualModel {
	entity_concepts: ConceptualEntity[];
	relationships: ConceptualRelationship[];
	lifecycle_concepts: Array<{ id: string; label: string; description: string }>;
	derived_concepts: Array<{ id: string; label: string; description: string }>;
	file_concepts: Array<{ id: string; label: string; description: string }>;
	import_concepts: Array<{ id: string; label: string; description: string }>;
	unresolved_review_ids: string[];
	warnings: string[];
	confidence_summary: Record<string, string>;
}

export interface ModelGraph {
  nodes: ModelNode[];
  edges: ModelEdge[];
}

export interface ModelNode {
  id: string;
  kind: "table" | "derived_view";
  label: string;
  table_name?: string;
  description?: string;
  fields?: ModelField[];
  evidence: EvidenceRef;
}

export interface ModelField {
  id: string;
  element_id: string;
  label: string;
  type: string;
  required: boolean;
  description?: string;
  evidence: EvidenceRef;
}

export interface ModelEdge {
  id: string;
  kind: string;
  label: string;
  from: string;
  to: string;
  cardinality: string;
  required: boolean;
  evidence: EvidenceRef;
}

export interface TraceIndex {
  source_to_elements: Record<string, string[]>;
  element_to_sources: Record<string, string[]>;
  requirement_to_elements: Record<string, string[]>;
  review_to_elements: Record<string, string[]>;
}

export interface ModelElementDetails {
  id: string;
  kind: string;
  label: string;
  description?: string;
  expression?: string;
  evidence: EvidenceRef;
  related_elements: string[];
  source_unit_summaries?: string[];
}

export interface QualityReport {
  summary: {
    validation_errors: number;
    lint_warnings: number;
    lint_info: number;
    blocking_issues: number;
    traceability_status: string;
    dbml_status: string;
  };
  issues: QualityIssue[];
}

export interface QualityIssue {
  id: string;
  severity: "error" | "warning" | "info";
  code: string;
  element_id?: string;
  message: string;
  blocking: boolean;
  accepted: boolean;
}

export interface JobRef {
  job: Job;
}

export interface Job {
  id: string;
	project_id: string;
  type: string;
	stage: string;
	status: JobStatus;
  events_url: string;
	input_revision?: number;
	output_revision?: number;
	attempt: number;
	progress: number;
	message?: string;
	error?: string;
  created_at: string;
  updated_at: string;
	started_at?: string;
	completed_at?: string;
}

export type JobStatus = "queued" | "running" | "retrying" | "waiting_for_review" | "completed" | "failed" | "cancelled" | "superseded" | "interrupted";

export interface JobEvent {
  job_id: string;
  type: string;
	status: JobStatus;
  step?: string;
  message?: string;
  progress: number;
  project_revision?: number;
  updated?: string[];
  created_at: string;
}

export interface LLMRunSummary {
	id: string;
	version: number;
	stage: string;
	status: string;
	provider: string;
	model: string;
	template_version?: string;
	schema_name?: string;
	input_hash: string;
	reasoning_effort: string;
	max_output_tokens: number;
	usage: { input_tokens?: number; output_tokens?: number; total_tokens?: number };
	started_at: string;
	completed_at?: string;
	duration_ms?: number;
	validation_ok: boolean;
	errors: string[];
	cached?: boolean;
	context_bytes?: number;
	full_context_bytes?: number;
	context_reduction_ratio?: number;
	retry_count?: number;
	call_reason?: string;
	call_gate_policy?: string;
	wasted_tokens?: number;
}

export interface LLMOptimizationStageMetrics {
	runs: number;
	provider_calls: number;
	cache_hits: number;
	retries: number;
	input_tokens: number;
	output_tokens: number;
	total_tokens: number;
	wasted_tokens: number;
	unknown_usage_attempts: number;
	context_bytes: number;
	full_context_bytes: number;
	context_bytes_saved: number;
	context_reduction_ratio: number;
}

export interface LLMOptimizationReport {
	version: number;
	project_id: string;
	policy_version: string;
	budget_policy: string;
	context_policy: string;
	call_gate_policy: string;
	risk_policy: string;
	totals: LLMOptimizationStageMetrics;
	by_stage: Record<string, LLMOptimizationStageMetrics>;
	calls_by_reason: Record<string, number>;
	avoided_review_resolution_calls: number;
	avoided_generation_calls: number;
	auto_applied_decisions: number;
	batched_manual_decisions: number;
	active_review_ms: number;
	unresolved_review_questions: number;
}

export interface MutationResult {
  project_revision: number;
  updated: string[];
  needs_refresh?: string[];
  message?: string;
}

export interface ApiError {
  error: {
    code: string;
    message: string;
    details?: unknown;
    request_id: string;
  };
}
