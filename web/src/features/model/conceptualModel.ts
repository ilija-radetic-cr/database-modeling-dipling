import type { ConceptualModel } from "@/shared/api/types";

type ConceptualModelWire = {
  [Key in keyof ConceptualModel]: ConceptualModel[Key] | null | undefined;
};

export function normalizeConceptualModelForView(model: ConceptualModel): ConceptualModel {
  const wire = model as ConceptualModelWire;
  const entityConcepts = (wire.entity_concepts ?? []).map((entity) => ({
    ...entity,
    attributes: entity.attributes ?? [],
    evidence: {
      ...entity.evidence,
      source_units: entity.evidence?.source_units ?? [],
      requirement_atoms: entity.evidence?.requirement_atoms ?? [],
      review_decisions: entity.evidence?.review_decisions ?? [],
    },
  }));
  const relationships = (wire.relationships ?? []).map((relationship) => ({
    ...relationship,
    evidence: {
      ...relationship.evidence,
      source_units: relationship.evidence?.source_units ?? [],
      requirement_atoms: relationship.evidence?.requirement_atoms ?? [],
      review_decisions: relationship.evidence?.review_decisions ?? [],
    },
  }));

  return {
    ...model,
    entity_concepts: entityConcepts,
    relationships,
    lifecycle_concepts: wire.lifecycle_concepts ?? [],
    derived_concepts: wire.derived_concepts ?? [],
    file_concepts: wire.file_concepts ?? [],
    import_concepts: wire.import_concepts ?? [],
    unresolved_review_ids: wire.unresolved_review_ids ?? [],
    warnings: wire.warnings ?? [],
    confidence_summary: wire.confidence_summary ?? {},
  };
}
