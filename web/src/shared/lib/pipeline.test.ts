import { describe, expect, it } from "vitest";
import {
  canRetryJob,
  finalModelAction,
  isRunnableStage,
  isTerminalJobStatus,
  newProjectActionState,
  nextStageLabel,
  reconcileJobEvent,
  requiresLogicalValidationRepair,
  shouldRecoverLatestJob,
} from "./pipeline";
import type { Job, JobEvent } from "@/shared/api/types";

describe("pipeline action state", () => {
  it("exposes only valid new-project actions", () => {
    expect(newProjectActionState({ created: false, name: "  ", sourceText: "task", readyResources: 0 }).canCreate).toBe(false);
    expect(newProjectActionState({ created: true, name: "Demo", sourceText: "task", readyResources: 2 })).toEqual({
      canCreate: false, canAddText: true, canBuildCombinedDocument: true,
    });
	expect(newProjectActionState({ created: true, name: "Demo", sourceText: "", readyResources: 1 }).canBuildCombinedDocument).toBe(true);
  });

  it("offers retry only for terminal recoverable jobs", () => {
    expect(canRetryJob("failed")).toBe(true);
    expect(canRetryJob("interrupted")).toBe(true);
    expect(canRetryJob("running")).toBe(false);
    expect(canRetryJob("completed")).toBe(false);
  });

  it("routes logical validation failures through a fresh repair stage", () => {
    expect(requiresLogicalValidationRepair({
      stage: "logical_model",
      error: "logical DB-DSL validation failed: import target is invalid",
    })).toBe(true);
    expect(requiresLogicalValidationRepair({
      stage: "logical_model",
      error: "OpenAI request failed: context deadline exceeded",
    })).toBe(false);
    expect(requiresLogicalValidationRepair({
      stage: "conceptual_model",
      message: "logical DB-DSL validation failed",
    })).toBe(false);
  });

	it("recovers active jobs and only current-revision failures", () => {
		expect(isTerminalJobStatus("running")).toBe(false);
		expect(isTerminalJobStatus("failed")).toBe(true);
		expect(shouldRecoverLatestJob({ status: "queued", input_revision: 15 }, 16)).toBe(false);
		expect(shouldRecoverLatestJob({ status: "running", input_revision: 16 }, 16)).toBe(true);
		expect(shouldRecoverLatestJob({ status: "failed", input_revision: 16 }, 16)).toBe(true);
		expect(shouldRecoverLatestJob({ status: "failed", input_revision: 15 }, 16)).toBe(false);
		expect(shouldRecoverLatestJob({ status: "completed", input_revision: 16 }, 16)).toBe(false);
	});

	it("never lets stale SSE running state overwrite terminal polling state", () => {
		const job: Job = {
			id: "job_000373", project_id: "project_057", type: "process_sources", stage: "process_sources",
			status: "completed", events_url: "/events", input_revision: 2, output_revision: 3,
			attempt: 1, progress: 100, message: "Job completed.",
			created_at: "2026-09-25T16:32:18Z", updated_at: "2026-09-25T16:33:26Z", completed_at: "2026-09-25T16:33:26Z",
		};
		const staleRunning: JobEvent = {
			job_id: job.id, type: job.type, status: "running", step: "propose_source_segmentation",
			message: "Segmenting", progress: 55, created_at: "2026-09-25T16:32:19Z",
		};
		expect(reconcileJobEvent(job, staleRunning)).toEqual(job);

		const running = { ...job, status: "running" as const, progress: 55, output_revision: undefined, completed_at: undefined };
		const completed: JobEvent = {
			job_id: job.id, type: job.type, status: "completed", message: "Job completed.", progress: 100,
			project_revision: 3, created_at: "2026-09-25T16:33:26Z",
		};
		expect(reconcileJobEvent(running, completed)).toMatchObject({ status: "completed", progress: 100, output_revision: 3 });
	});

  it("enforces final model acceptance before output generation", () => {
		expect(finalModelAction({ modelReady: false, accepted: false, dbmlReady: false })).toBe("return_to_pipeline");
		expect(finalModelAction({ modelReady: true, accepted: false, dbmlReady: false })).toBe("accept_model");
		expect(finalModelAction({ modelReady: true, accepted: true, dbmlReady: false })).toBe("generate_outputs");
		expect(finalModelAction({ modelReady: true, accepted: true, dbmlReady: true })).toBe("finalize");
  });

	it("runs only the backend's stages and stops at human gates", () => {
		expect(isRunnableStage("process_sources")).toBe(true);
		expect(isRunnableStage("conceptual_model")).toBe(true);
		expect(isRunnableStage("logical_model")).toBe(true);
		expect(isRunnableStage("generate_outputs")).toBe(true);
		expect(isRunnableStage("source_review")).toBe(false);
		expect(isRunnableStage("conceptual_review")).toBe(false);
		expect(isRunnableStage("model_review")).toBe(false);
		expect(isRunnableStage("completed")).toBe(false);
		expect(isRunnableStage(undefined)).toBe(false);
	});

	it("renders the project next-stage action explicitly", () => {
		expect(nextStageLabel("process_sources")).toBe("Segment Sources");
		expect(nextStageLabel("source_review")).toBe("Review Source Units");
    expect(nextStageLabel("conceptual_model")).toBe("Generate Conceptual Model");
		expect(nextStageLabel("model_review")).toBe("Review Final Model");
		expect(nextStageLabel("completed")).toBe("Open Finalize");
		expect(nextStageLabel(undefined)).toBe("Loading next action");
  });
});
