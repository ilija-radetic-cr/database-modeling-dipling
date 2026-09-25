import { describe, expect, it } from "vitest";
import type { ConceptualModel } from "@/shared/api/types";
import { normalizeConceptualModelForView } from "./conceptualModel";

describe("normalizeConceptualModelForView", () => {
  it("turns legacy null collections into render-safe arrays", () => {
    const legacy = {
      entity_concepts: [{
        id: "ENT-pizza",
        label: "Pizza",
        description: "A pizza in the catalog.",
        kind: "regular",
        attributes: null,
        evidence: {
          source_units: null,
          requirement_atoms: ["RA-0009"],
          review_decisions: null,
        },
      }],
      relationships: null,
      lifecycle_concepts: null,
      derived_concepts: null,
      file_concepts: null,
      import_concepts: null,
      unresolved_review_ids: null,
      warnings: null,
      confidence_summary: null,
    } as unknown as ConceptualModel;

    const normalized = normalizeConceptualModelForView(legacy);

    expect(normalized.relationships).toEqual([]);
    expect(normalized.entity_concepts[0].attributes).toEqual([]);
    expect(normalized.entity_concepts[0].evidence.source_units).toEqual([]);
    expect(normalized.unresolved_review_ids).toEqual([]);
    expect(normalized.confidence_summary).toEqual({});
  });
});
