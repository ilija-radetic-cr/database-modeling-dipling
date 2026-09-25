import type { Job, JobEvent, JobStatus, ProjectStageName } from "@/shared/api/types";

const runnableStages: readonly ProjectStageName[] = [
  "process_sources", "conceptual_model", "logical_model", "generate_outputs", "validation_lint",
];

export function isRunnableStage(stage: string | undefined): stage is ProjectStageName {
  return !!stage && (runnableStages as readonly string[]).includes(stage);
}

// nextStageLabel names the recommended next action: an automatic stage the
// button starts, or a human gate it navigates to.
export function nextStageLabel(stage: string | undefined) {
  const labels: Record<string, string> = {
    process_sources: "Segment Sources", source_review: "Review Source Units",
    conceptual_model: "Generate Conceptual Model", conceptual_review: "Review Conceptual Model",
    logical_model: "Project Logical Model", model_review: "Review Final Model",
    generate_outputs: "Generate DBML & Trace", completed: "Open Finalize",
  };
  return stage ? labels[stage] ?? "Continue" : "Loading next action";
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

// SSE and polling are intentionally redundant. Reconcile them monotonically so
// a delayed `running` event can never overwrite a terminal polled job, while a
// terminal SSE event can finish a job before the next poll arrives.
export function reconcileJobEvent(job: Job, event: JobEvent | null): Job {
  if (!event || event.job_id !== job.id) return job;
  if (isTerminalJobStatus(job.status) && !isTerminalJobStatus(event.status)) return job;

  const jobTime = Date.parse(job.updated_at);
  const eventTime = Date.parse(event.created_at);
  const eventIsNewer = Number.isNaN(jobTime) || Number.isNaN(eventTime) || eventTime >= jobTime;
  if (!isTerminalJobStatus(event.status) && !eventIsNewer) return job;

  return {
    ...job,
    status: event.status,
    progress: event.progress,
    message: event.message ?? job.message,
    error: event.status === "failed" ? event.message ?? job.error : job.error,
    output_revision: event.project_revision ?? job.output_revision,
    updated_at: event.created_at,
    completed_at: isTerminalJobStatus(event.status) ? event.created_at : job.completed_at,
  };
}

export function shouldRecoverLatestJob(
  job: Pick<Job, "status" | "input_revision">,
  currentRevision: number,
) {
  if (job.status === "completed") return false;
  // A successful runner commits its project revision immediately before the
  // job summary becomes terminal. During that short window a stale jobs-list
  // response may still say `running`; never remount it against the newer state.
  if (job.input_revision && job.input_revision < currentRevision) return false;
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

// finalModelAction is the end of the pipeline: accept the logical model, then
// generate the deterministic outputs, then finalize.
export function finalModelAction(input: { modelReady: boolean; accepted: boolean; dbmlReady: boolean }) {
  if (!input.modelReady) return "return_to_pipeline";
  if (!input.accepted) return "accept_model";
  if (!input.dbmlReady) return "generate_outputs";
  return "finalize";
}
