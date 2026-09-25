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
    expect(normalized.entity_concepts[0].evidence.review_decisions).toEqual([]);
    expect(normalized.unresolved_review_ids).toEqual([]);
    expect(normalized.confidence_summary).toEqual({});
  });

  it("keeps segment evidence on entities and relationships", () => {
    const model = {
      entity_concepts: [{
        id: "ENT-order", label: "Order", description: "", kind: "regular", attributes: [],
        evidence: { source_units: ["SU-004", "SU-005"], review_decisions: [] },
      }],
      relationships: [{
        id: "REL-order-customer", label: "placed by", description: "", from: "ENT-order", to: "ENT-customer", cardinality: "many_to_one",
        evidence: { source_units: ["SU-004"], review_decisions: [] },
      }],
      lifecycle_concepts: [], derived_concepts: [], file_concepts: [], import_concepts: [],
      unresolved_review_ids: [], warnings: [], confidence_summary: {},
    } as ConceptualModel;

    const normalized = normalizeConceptualModelForView(model);

    expect(normalized.entity_concepts[0].evidence.source_units).toEqual(["SU-004", "SU-005"]);
    expect(normalized.relationships[0].evidence.source_units).toEqual(["SU-004"]);
  });
});
