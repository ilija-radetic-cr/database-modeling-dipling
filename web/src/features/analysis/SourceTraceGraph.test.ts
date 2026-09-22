import { describe, expect, it } from "vitest";
import type { CombinedDocumentSentence, SourceUnit } from "@/shared/api/types";
import { buildSourceTrace } from "./SourceTraceGraph";

describe("source trace graph", () => {
  it("maps backend OD references to SourceUnits without deriving text in the UI", () => {
    const trace = buildSourceTrace([sentence("OD-S-0001", "Proizvod   mora imati naziv .")], [unit("SU-001", ["OD-S-0001"])]);

    expect(trace.nodes.map((node) => node.id)).toEqual(["od:OD-S-0001", "su:SU-001"]);
    expect(trace.nodes[0].text).toBe("Proizvod   mora imati naziv .");
    expect(trace.nodes[1].text).toBe("Proizvod mora imati naziv.");
    expect(trace.edges).toEqual([{ id: "edge:OD-S-0001:SU-001", source: "od:OD-S-0001", target: "su:SU-001" }]);
  });

  it("supports multiple OD references and marks missing lineage", () => {
    const trace = buildSourceTrace([sentence("OD-S-0001", "Prva rečenica.")], [unit("SU-001", ["OD-S-0001", "OD-S-9999"])]);

    expect(trace.edges).toHaveLength(2);
    expect(trace.nodes.find((node) => node.id === "od:OD-S-9999")).toMatchObject({ missing: true });
  });
});

function sentence(id: string, text: string): CombinedDocumentSentence {
  return { id, kind: "sentence", text, derived_from: [], transformation: "copied", confidence: "high", warnings: [] };
}

function unit(id: string, odSentenceIDs: string[]): SourceUnit {
  return {
    id,
    kind: "requirement_sentence",
    normalized_text: "Proizvod mora imati naziv.",
    normalization: {
      version: "source_text_normalizer_v1",
      strategy: "backend_deterministic",
      exact_hash: "sha256:exact",
      normalized_hash: "sha256:normalized",
      changed: true,
      operations: [],
    },
    exact_text: "Proizvod   mora imati naziv .",
    relevance: "model_relevant",
    confidence: "high",
    review_status: "reviewed",
    origin_spans: [],
    linked_examples: [],
    linked_requirements: [],
    open_review_candidates: [],
    od_sentence_ids: odSentenceIDs,
  };
}
