import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "./client";

describe("source-unit review client", () => {
  afterEach(() => vi.unstubAllGlobals());

  it("never sends client-authored normalized text", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({
      project_revision: 3,
      source_unit: {},
      remaining_needs_attention: 0,
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    await api.reviewSourceUnit("project-1", "SU-001", {
      base_revision: 2,
      decision: "accept",
      note: "Classification confirmed.",
    });

    const [, init] = fetchMock.mock.calls[0];
    const body = JSON.parse(String(init?.body));
    expect(body).toEqual({
      base_revision: 2,
      decision: "accept",
      note: "Classification confirmed.",
      reviewed_by: "web_user",
    });
    expect(body).not.toHaveProperty("normalized_text");
  });

  it("starts source processing with optional LLM segmentation controls", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({ job: {} }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await api.processSources("project-1", 7, { model: "mock-model", mock: true });

    const [, init] = fetchMock.mock.calls[0];
	expect(JSON.parse(String(init?.body))).toEqual({ base_revision: 7, model: "mock-model", mock: true });
  });

  it("records a named adversarial finding decision with the required note", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({
      project_revision: 9,
      status: "current",
      review: null,
      decisions: [],
      audit: [],
      can_accept: false,
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    await api.decideAdversarialReview("project-1", {
      base_revision: 8,
      actor: "test_operator",
      decisions: [{ finding_id: "ARF-001", decision: "waive", note: "Risk accepted for this bounded test." }],
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/projects/project-1/adversarial-review/decisions");
    expect(JSON.parse(String(init?.body))).toEqual({
      base_revision: 8,
      actor: "test_operator",
      decisions: [{ finding_id: "ARF-001", decision: "waive", note: "Risk accepted for this bounded test." }],
    });
  });

  it("appends an operator finding without a client-authored ID or provider request", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({
      project_revision: 10,
      status: "current",
      review: null,
      decisions: [],
      audit: [],
      finding_provenance: [],
      can_accept: false,
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    await api.appendAdversarialFinding("project-1", {
      base_revision: 9,
      actor: "test_operator",
      note: "TEST-OPERATOR: verified against SU-014 and the conceptual constraint.",
      finding: {
        severity: "error",
        category: "missing",
        source_unit_ids: ["SU-014"],
        description_refs: ["rule:username_unique"],
        source_quote: "Korisničko ime mora biti jedinstveno bez obzira na velika i mala slova.",
        claim: "Username uniqueness lost its comparison semantics.",
        expected: "Case-insensitive uniqueness.",
        actual: "Plain uniqueness.",
        suggested_correction: "Set the comparison mode to case_insensitive.",
      },
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(String(url)).toContain("/projects/project-1/adversarial-review/findings");
    const body = JSON.parse(String(init?.body));
    expect(body.actor).toBe("test_operator");
    expect(body.finding).not.toHaveProperty("id");
    expect(body.finding.description_refs).toEqual(["rule:username_unique"]);
  });

  it("normalizes null adversarial review collections from the Go API", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({
      project_revision: 9,
      status: "current",
      review: {
        review_id: "review-1",
        scope: "conceptual",
        candidate_revision: 8,
        candidate_hash: "candidate-hash",
        source_hash: "source-hash",
        summary: "No findings.",
        findings: null,
      },
      decisions: null,
      audit: null,
      finding_provenance: null,
      can_accept: true,
    }), { status: 200, headers: { "Content-Type": "application/json" } }));
    vi.stubGlobal("fetch", fetchMock);

    const response = await api.adversarialReview("project-1");

    expect(response.review?.findings).toEqual([]);
    expect(response.decisions).toEqual([]);
    expect(response.audit).toEqual([]);
    expect(response.finding_provenance).toEqual([]);
  });

  it("sends the human actor and reason when accepting the reviewed conceptual model", async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init?: RequestInit) => new Response(JSON.stringify({ project_revision: 10, message: "accepted" }), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }));
    vi.stubGlobal("fetch", fetchMock);

    await api.acceptConceptualModel("project-1", 9, "human", "All current findings were reviewed and disposed.");

    const [, init] = fetchMock.mock.calls[0];
    expect(JSON.parse(String(init?.body))).toEqual({
      base_revision: 9,
      actor: "human",
      note: "All current findings were reviewed and disposed.",
    });
  });
});
