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
});
