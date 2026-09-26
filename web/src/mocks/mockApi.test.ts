import { beforeEach, describe, expect, it } from "vitest";
import type { AdversarialReviewResponse, Job, JobEvent, ProjectResponse } from "@/shared/api/types";
import { mockApi, resetMockState } from "./mockApi";

const projectId = "project_mock";

function finish(job: Job) {
  return new Promise<JobEvent | undefined>((resolve) => {
    mockApi.subscribeToJob(projectId, job.id, () => undefined, resolve);
  });
}

describe("mock API review-to-completion flow", () => {
  beforeEach(resetMockState);

  it("advances real UI gates instead of reporting a completed job against stale health", async () => {
    const reviewJob = await mockApi.request<{ job: Job }>(`/projects/${projectId}/stages/adversarial_review/run`, { method: "POST" });
    await finish(reviewJob.job);
    const review = await mockApi.request<AdversarialReviewResponse>(`/projects/${projectId}/adversarial-review`);
    expect(review.status).toBe("current");
    expect(review.can_accept).toBe(false);
    await expect(mockApi.request(`/projects/${projectId}/conceptual-model/accept`, {
      method: "POST",
      body: JSON.stringify({ base_revision: review.project_revision, actor: "test_operator", note: "Premature acceptance." }),
    })).rejects.toThrow("Current adversarial review decisions are required");
    await mockApi.request(`/projects/${projectId}/adversarial-review/decisions`, {
      method: "POST",
      body: JSON.stringify({
        base_revision: review.project_revision,
        actor: "test_operator",
        decisions: [{ finding_id: "ARF-001", decision: "waive", note: "Verified in the mock task." }],
      }),
    });
    await mockApi.request(`/projects/${projectId}/conceptual-model/accept`, {
      method: "POST",
      body: JSON.stringify({ base_revision: review.project_revision + 1, actor: "test_operator", note: "Reviewed for the mock flow." }),
    });
    const logical = await mockApi.request<{ job: Job }>(`/projects/${projectId}/stages/logical_model/run`, { method: "POST" });
    await finish(logical.job);
    expect((await mockApi.request<{ next_stage: string }>(`/projects/${projectId}/stages`)).next_stage).toBe("model_review");

    await mockApi.request(`/projects/${projectId}/model-acceptance`, { method: "POST" });
    const outputs = await mockApi.request<{ job: Job }>(`/projects/${projectId}/stages/generate_outputs/run`, { method: "POST" });
    await finish(outputs.job);
    expect((await mockApi.request<ProjectResponse>(`/projects/${projectId}`)).artifact_health.can_complete_project).toBe(true);

    await mockApi.request(`/projects/${projectId}/complete`, { method: "POST" });
    expect((await mockApi.request<ProjectResponse>(`/projects/${projectId}`)).project.lifecycle_status).toBe("completed");
  });

  it("keeps a correction proposal unaccepted and makes the previous review stale", async () => {
    const reviewJob = await mockApi.request<{ job: Job }>(`/projects/${projectId}/stages/adversarial_review/run`, { method: "POST" });
    await finish(reviewJob.job);
    const reviewed = await mockApi.request<AdversarialReviewResponse>(`/projects/${projectId}/adversarial-review`);
    await mockApi.request(`/projects/${projectId}/adversarial-review/decisions`, {
      method: "POST",
      body: JSON.stringify({
        base_revision: reviewed.project_revision,
        actor: "human",
        decisions: [{ finding_id: "ARF-001", decision: "request_correction", note: "Represent all three source roles." }],
      }),
    });
    const correction = await mockApi.request<{ job: Job }>(`/projects/${projectId}/adversarial-review/correct`, {
      method: "POST",
      body: JSON.stringify({ base_revision: reviewed.project_revision + 1, actor: "human", note: "Add the printer and administrator roles." }),
    });
    await finish(correction.job);

    const after = await mockApi.request<AdversarialReviewResponse>(`/projects/${projectId}/adversarial-review`);
    const conceptual = await mockApi.request<{ accepted: boolean }>(`/projects/${projectId}/conceptual-model`);
    expect(after.status).toBe("stale");
    expect(after.can_accept).toBe(false);
    expect(conceptual.accepted).toBe(false);
    expect(after.audit.map((event) => event.action)).toContain("correction_proposed");
  });

  it("adds an operator finding to the current review with explicit provenance and audit", async () => {
    const reviewJob = await mockApi.request<{ job: Job }>(`/projects/${projectId}/stages/adversarial_review/run`, { method: "POST" });
    await finish(reviewJob.job);
    const reviewed = await mockApi.request<AdversarialReviewResponse>(`/projects/${projectId}/adversarial-review`);

    const after = await mockApi.request<AdversarialReviewResponse>(`/projects/${projectId}/adversarial-review/findings`, {
      method: "POST",
      body: JSON.stringify({
        base_revision: reviewed.project_revision,
        actor: "test_operator",
        note: "TEST-OPERATOR: verified manually against the exact source.",
        finding: {
          severity: "error",
          category: "missing",
          source_unit_ids: ["SU-002"],
          description_refs: ["user"],
          source_quote: "Postoje tri vrste korisnika: klijenti, štampari i administrator web sistema.",
          claim: "A source role is missing.",
          expected: "Every named role remains distinguishable.",
          actual: "The model omits a role.",
          suggested_correction: "Add the missing role without changing unrelated concepts.",
        },
      }),
    });

    const added = after.review?.findings.find((finding) => finding.id === "OF-001");
    expect(added?.claim).toBe("A source role is missing.");
    expect(after.finding_provenance.find((item) => item.finding_id === "OF-001")).toMatchObject({
      origin: "operator",
      actor: "test_operator",
    });
    expect(after.audit.at(-1)?.action).toBe("operator_finding_added");
    expect(after.can_accept).toBe(false);
  });
});
