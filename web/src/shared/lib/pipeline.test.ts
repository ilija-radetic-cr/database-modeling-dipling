import { describe, expect, it } from "vitest";
import type { ReviewCandidate } from "@/shared/api/types";
import {
  canRetryJob,
  finalModelAction,
  isTerminalJobStatus,
  newProjectActionState,
  nextStageLabel,
  requiresLogicalValidationRepair,
  shouldRecoverLatestJob,
  unresolvedReviewDependencies,
} from "./pipeline";

describe("pipeline action state", () => {
  it("exposes only valid new-project actions", () => {
    expect(newProjectActionState({ created: false, name: "  ", sourceText: "task", readyResources: 0 }).canCreate).toBe(false);
    expect(newProjectActionState({ created: true, name: "Demo", sourceText: "task", readyResources: 2 })).toEqual({
      canCreate: false, canAddText: true, canBuildCombinedDocument: true,
    });
	expect(newProjectActionState({ created: true, name: "Demo", sourceText: "", readyResources: 1 }).canBuildCombinedDocument).toBe(true);
  });

  it("keeps review questions locked until dependencies resolve", () => {
    const root = candidate("RC-1", "open", []);
    const child = candidate("RC-2", "open", ["RC-1"]);
    expect(unresolvedReviewDependencies(child, [root, child])).toEqual(["RC-1"]);
    expect(unresolvedReviewDependencies(child, [{ ...root, status: "resolved" }, child])).toEqual([]);
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
		expect(shouldRecoverLatestJob({ status: "queued", input_revision: 15 }, 16)).toBe(true);
		expect(shouldRecoverLatestJob({ status: "failed", input_revision: 16 }, 16)).toBe(true);
		expect(shouldRecoverLatestJob({ status: "failed", input_revision: 15 }, 16)).toBe(false);
		expect(shouldRecoverLatestJob({ status: "completed", input_revision: 16 }, 16)).toBe(false);
	});

  it("enforces final model acceptance before output generation", () => {
		expect(finalModelAction({ modelReady: true, accepted: false, dbmlReady: false, semanticStatus: "not_generated" })).toBe("verify_semantic");
		expect(finalModelAction({ modelReady: true, accepted: false, dbmlReady: false, semanticStatus: "blocked" })).toBe("resolve_semantic");
		expect(finalModelAction({ modelReady: true, accepted: false, dbmlReady: false, semanticStatus: "passed" })).toBe("accept_model");
		expect(finalModelAction({ modelReady: true, accepted: true, dbmlReady: false, semanticStatus: "passed" })).toBe("generate_outputs");
		expect(finalModelAction({ modelReady: true, accepted: true, dbmlReady: true, semanticStatus: "passed" })).toBe("finalize");
		// The segment-based flow has no design obligations to verify.
		expect(finalModelAction({ modelReady: true, accepted: false, dbmlReady: false, semanticStatus: "not_applicable" })).toBe("accept_model");
  });

	it("renders the project next-stage action explicitly", () => {
		expect(nextStageLabel("combined_document", 0)).toBe("Segment Sources");
    expect(nextStageLabel("conceptual_model", 0)).toBe("Generate Conceptual Model");
    expect(nextStageLabel("review_decisions", 2)).toBe("Resolve 2 decisions");
  });
});

function candidate(id: string, status: ReviewCandidate["status"], dependsOn: string[]): ReviewCandidate {
  return { id, question: id, description: "", status, affected_atoms: [], depends_on: dependsOn, may_affect: [], options: [], blocking: true };
}
