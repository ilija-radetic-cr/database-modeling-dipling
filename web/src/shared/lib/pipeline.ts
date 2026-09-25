import type { Job, JobStatus, ProjectStageName, ReviewCandidate } from "@/shared/api/types";

export function isRunnableStage(stage: string | undefined): stage is ProjectStageName {
  return !!stage && [
    "process_sources", "combined_document", "requirement_atoms", "functional_analysis", "crud_mapping",
    "review_candidates", "conceptual_model", "logical_model", "semantic_verification", "generate_outputs",
  ].includes(stage);
}

export function nextStageLabel(stage: string | undefined, openReviews: number) {
  if (openReviews > 0 || stage === "review_decisions") return `Resolve ${openReviews} decision${openReviews === 1 ? "" : "s"}`;
  const labels: Record<string, string> = {
	process_sources: "Segment Sources", combined_document: "Segment Sources", source_review: "Review Source Units",
    requirement_atoms: "Extract Requirements", functional_analysis: "Build Functional Analysis", crud_mapping: "Map CRUD Operations",
    review_candidates: "Propose Review Decisions", conceptual_model: "Generate Conceptual Model", logical_model: "Project Logical Model",
	conceptual_review: "Review Conceptual Model", semantic_verification: "Verify Semantic Obligations",
    model_review: "Review Final Model", generate_outputs: "Generate DBML & Trace", completed: "Open Finalize",
  };
  return stage ? labels[stage] ?? "Continue" : "Loading next action";
}

export function unresolvedReviewDependencies(candidate: ReviewCandidate, all: ReviewCandidate[]) {
  const resolved = new Set(all.filter((item) => item.status !== "open").map((item) => item.id));
  return candidate.depends_on.filter((id) => !resolved.has(id));
}

export function canRetryJob(status: JobStatus) {
  return ["failed", "interrupted", "superseded", "cancelled"].includes(status);
}

export function requiresLogicalValidationRepair(job: Pick<Job, "stage" | "error" | "message">) {
  const failure = job.error ?? job.message ?? "";
  return job.stage === "logical_model" && failure.includes("logical DB-DSL validation failed");
}

export function isTerminalJobStatus(status: JobStatus) {
  return ["completed", "failed", "cancelled", "superseded", "interrupted"].includes(status);
}

export function shouldRecoverLatestJob(
  job: Pick<Job, "status" | "input_revision">,
  currentRevision: number,
) {
  if (job.status === "completed") return false;
  if (!isTerminalJobStatus(job.status)) return true;

  // A failed/interrupted job remains useful for retry only while it still
  // targets the current project revision. Once another safe operation has
  // advanced the project, the terminal job is history and must not block the
  // next pipeline action.
  return !job.input_revision || job.input_revision >= currentRevision;
}

export function newProjectActionState(input: { created: boolean; name: string; sourceText: string; readyResources: number }) {
  return {
    canCreate: !input.created && input.name.trim() !== "",
    canAddText: input.created && input.sourceText.trim() !== "",
    canBuildCombinedDocument: input.created && input.readyResources > 0,
  };
}

export function finalModelAction(input: { modelReady: boolean; accepted: boolean; dbmlReady: boolean; semanticStatus?: string }) {
  if (!input.modelReady) return "return_to_pipeline";
	if (input.semanticStatus === "blocked") return "resolve_semantic";
	if (input.semanticStatus !== "passed" && input.semanticStatus !== "legacy_not_applicable" && input.semanticStatus !== "not_applicable") return "verify_semantic";
  if (!input.accepted) return "accept_model";
  if (!input.dbmlReady) return "generate_outputs";
  return "finalize";
}
