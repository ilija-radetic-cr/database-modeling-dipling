import { describe, expect, it } from "vitest";
import type { ModelEdge, ModelNode, SourceUnit } from "@/shared/api/types";
import { filterTraceSourceUnits, modelElementLabel } from "./traceView";

const units = [
  { id: "SU-001", normalized_text: "Client places an order", exact_text: "Client places an order" },
  { id: "SU-002", normalized_text: "Order has a status", exact_text: "Order has a status" },
] as SourceUnit[];

const nodes = [{
  id: "table:order",
  kind: "table",
  label: "Order",
  fields: [{ id: "status", element_id: "field:order.status", label: "status", type: "text", required: true, evidence: { source_units: ["SU-002"], review_decisions: [] } }],
  evidence: { source_units: ["SU-001"], review_decisions: [] },
}] as ModelNode[];

const edges = [{ id: "rel:placed-by", label: "placed by", from: "table:order", to: "table:user" }] as ModelEdge[];

describe("trace view navigation", () => {
  it("searches source text but keeps evidence linked to the focused model element visible", () => {
    expect(filterTraceSourceUnits(units, "status").map((unit) => unit.id)).toEqual(["SU-002"]);
    expect(filterTraceSourceUnits(units, "no match", new Set(["SU-001"])).map((unit) => unit.id)).toEqual(["SU-001"]);
  });

  it("gives tables, fields and relationships readable focus labels", () => {
    expect(modelElementLabel(nodes, edges, "table:order")).toBe("Order");
    expect(modelElementLabel(nodes, edges, "field:order.status")).toBe("Order.status");
    expect(modelElementLabel(nodes, edges, "rel:placed-by")).toBe("placed by");
  });
});
