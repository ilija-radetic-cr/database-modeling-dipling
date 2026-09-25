import { describe, expect, it } from "vitest";
import type { CombinedDocumentSentence, SourceUnit } from "@/shared/api/types";
import { buildSourceTrace } from "./SourceTraceGraph";

describe("source trace graph", () => {
  it("maps LLM evidence segments to persisted SourceUnits without deriving text in the UI", () => {
	const trace = buildSourceTrace([sentence("SU-001", "Proizvod   mora imati naziv .")], [unit("SU-001", ["SU-001"])]);

	expect(trace.nodes.map((node) => node.id)).toEqual(["segment:SU-001", "su:SU-001"]);
    expect(trace.nodes[0].text).toBe("Proizvod   mora imati naziv .");
    expect(trace.nodes[1].text).toBe("Proizvod mora imati naziv.");
	expect(trace.edges).toEqual([{ id: "edge:SU-001:SU-001", source: "segment:SU-001", target: "su:SU-001" }]);
  });

  it("supports multiple segment references and marks missing lineage", () => {
	const trace = buildSourceTrace([sentence("SU-001", "Prva rečenica.")], [unit("SU-001", ["SU-001", "SU-9999"])]);

    expect(trace.edges).toHaveLength(2);
	expect(trace.nodes.find((node) => node.id === "segment:SU-9999")).toMatchObject({ missing: true });
  });
});

function sentence(id: string, text: string): CombinedDocumentSentence {
  return { id, kind: "sentence", text, derived_from: [], transformation: "copied", confidence: "high", warnings: [] };
}

function unit(id: string, segmentIDs: string[]): SourceUnit {
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
	segment_ids: segmentIDs,
  };
}
