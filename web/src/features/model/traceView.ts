import type { ModelEdge, ModelNode, SourceUnit } from "@/shared/api/types";

export function filterTraceSourceUnits(units: SourceUnit[], query: string, keepIds: ReadonlySet<string> = new Set()) {
  const needle = query.trim().toLocaleLowerCase();
  if (!needle) return units;
  return units.filter((unit) =>
    keepIds.has(unit.id)
    || unit.id.toLocaleLowerCase().includes(needle)
    || unit.normalized_text.toLocaleLowerCase().includes(needle)
    || (unit.exact_text ?? "").toLocaleLowerCase().includes(needle),
  );
}

export function modelElementLabel(nodes: ModelNode[], edges: ModelEdge[], elementId: string) {
  const node = nodes.find((item) => item.id === elementId);
  if (node) return node.label;
  for (const item of nodes) {
    const field = item.fields?.find((candidate) => candidate.element_id === elementId);
    if (field) return `${item.label}.${field.label || field.id}`;
  }
  return edges.find((item) => (item.element_id ?? item.id) === elementId)?.label ?? elementId;
}
