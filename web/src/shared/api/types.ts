export type ProjectLifecycleStatus =
  | "intake"
  | "sources_processed"
	| "source_review"
	| "conceptual_review"
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

// ArtifactHealth mirrors the backend's view of the segment-based pipeline:
// segmentation, source units, the conceptual model, the deterministic logical
// model and the DBML output, plus the gates a person has passed.
export interface ArtifactHealth {
  source_manifest_status: "not_generated" | "ready";
  combined_document_status: "not_generated" | "ready";
	source_segmentation_status: "not_generated" | "ready";
	source_units_status: "not_generated" | "ready" | "needs_attention";
	conceptual_model_status: "not_generated" | "proposed" | "ready";
  model_status: "not_generated" | "failed" | "ready";
  dbml_status: "not_generated" | "ready" | "outdated" | "blocked";
  can_continue_to_dbml: boolean;
  can_complete_project: boolean;
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
	section?: string;
	relevance?: string;
	tags?: string[];
  text: string;
	normalized_text?: string;
	derived_from?: CombinedDocumentOrigin[];
	transformation?: "copied" | "cleaned" | "merged" | "summarized" | "llm_grouped";
	segmentation_strategy?: string;
  confidence?: "high" | "medium" | "low";
	requires_review?: boolean;
  warnings?: string[];
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

export interface SourceSegmentationSegment {
	id: string;
	type: "heading" | "sentence" | "list" | "example" | "footnote" | "page_header" | "page_footer" | "page_number" | "other";
	text: string;
}

export interface SourceSegmentationProposal {
	segments: SourceSegmentationSegment[];
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

export interface SourceUnit {
  id: string;
  kind: string;
  section?: string;
  normalized_text: string;
  normalization: SourceTextNormalization;
  exact_text?: string;
  relevance: string;
  confidence: "high" | "medium" | "low";
  review_status: "reviewed" | "needs_attention";
  origin_spans: OriginSpan[];
	segment_ids?: string[];
	od_sentence_ids?: string[];
	warnings?: string[];
	requirement_notes?: string[];
}

export interface SourceUnitQA {
	ok: boolean;
	derivation_strategy: "segments_v1" | "llm_classification_backend_normalization" | "llm" | "llm_chunked" | "deterministic_fallback";
	segments_total?: number;
	segments_referenced?: number;
	unreferenced_segments?: string[];
	od_sentences_total?: number;
	od_sentences_referenced?: number;
	unreferenced_od_sentences?: string[];
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

// ProjectStageName lists the stages `POST /stages/{stage}/run` accepts.
export type ProjectStageName =
	| "process_sources"
	| "conceptual_model"
	| "logical_model"
	| "generate_outputs"
	| "validation_lint";

// ProjectNextStage is what `GET /stages` recommends next: a runnable stage, a
// human gate (source, conceptual or final-model review) or completion.
export type ProjectNextStage =
	| "process_sources"
	| "source_review"
	| "conceptual_model"
	| "conceptual_review"
	| "logical_model"
	| "model_review"
	| "generate_outputs"
	| "completed";

// Evidence points at source units; review_decisions stays in the wire format
// but is always empty in the segment-based flow.
export interface EvidenceRef {
  source_units: string[];
  review_decisions: string[];
  support_level?: string;
  confidence?: string;
}

export interface ConceptualAttribute {
	id: string;
	label: string;
	description: string;
	name?: string;
	value_type?: string;
	unique?: boolean;
	enum_values?: string[];
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
	required?: boolean;
	evidence: EvidenceRef;
}

export interface LogicalMappingReport {
	strategy: string;
	rule_version?: string;
	entities?: number;
	relationships?: number;
	constraints?: number;
	state_machines?: number;
	derived_views?: number;
	file_specs?: number;
	inferred?: string[];
	decisions?: string[];
	warnings?: string[];
}

export interface ConceptualModel {
	entity_concepts: ConceptualEntity[];
	relationships: ConceptualRelationship[];
	lifecycle_concepts: Array<{ id: string; label: string; description: string }>;
	derived_concepts: Array<{ id: string; label: string; description: string }>;
	file_concepts: Array<{ id: string; label: string; description: string }>;
	import_concepts: Array<{ id: string; label: string; description: string }>;
	index_concepts?: Array<{ id: string; label: string; description: string; owner: string; targets: string[] }>;
	unresolved_review_ids: string[];
	warnings: string[];
	confidence_summary: Record<string, string>;
}

// ConceptualDescription is the rich LLM description of what the system must
// remember; the conceptual model is derived from it deterministically.
export interface DescriptionEvidence {
	segments: string[];
	mode: "direct" | "implied";
}

export interface ConceptualDescription {
	actors: Array<{ id: string; name: string; description: string; represented_by: string; differs_by: string; evidence: DescriptionEvidence }>;
	things: Array<{
		id: string;
		name: string;
		kind: string;
		description: string;
		// A list since prompt v0.10.1; older descriptions hold free text.
		identified_by: string[] | string;
		properties: Array<{ name: string; meaning: string; value_type?: string; shape: string; presence: string; origin: string; source: string; evidence: DescriptionEvidence }>;
		links: Array<{ to: string; meaning: string; per_this: string; per_other: string; evidence: DescriptionEvidence }>;
		states: string[];
		evidence: DescriptionEvidence;
	}>;
	rules: Array<{ id: string; kind: string; statement: string; applies_to: string[]; evidence: DescriptionEvidence }>;
	queries: Array<{ id: string; description: string; needs: string[]; criteria?: string[]; evidence: DescriptionEvidence }>;
	imports: Array<{ id: string; description: string; fills: string[]; evidence: DescriptionEvidence }>;
	boundaries: Array<{ kind: string; description: string; kept_outcome: string; evidence: DescriptionEvidence }>;
	excluded: Array<{ segment: string; reason: string }>;
	open_questions: Array<{ id: string; question: string; readings: string[]; affects: string[]; evidence: DescriptionEvidence }>;
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
	metadata?: {
		repair_round?: number;
		max_repair_attempts?: number;
		validation_errors?: number;
		stalled?: boolean;
		[key: string]: unknown;
	};
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
	validation_scope?: "structured_schema" | "patch_preflight" | "full_dbdsl_v05" | string;
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
