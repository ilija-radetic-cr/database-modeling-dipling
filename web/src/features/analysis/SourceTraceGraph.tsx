import { useMemo, useState } from "react";
import ReactFlow, { Background, Controls, MarkerType, type Edge, type Node } from "reactflow";
import "reactflow/dist/style.css";
import type { CombinedDocumentSentence, SourceUnit } from "@/shared/api/types";
import { Badge } from "@/shared/components/ui";

export interface SourceTraceNode {
  id: string;
  type: "od_sentence" | "source_unit";
  text: string;
  meta: string;
  missing?: boolean;
}

export interface SourceTraceEdge {
  id: string;
  source: string;
  target: string;
}

export function buildSourceTrace(
  sentences: CombinedDocumentSentence[],
  units: SourceUnit[],
): { nodes: SourceTraceNode[]; edges: SourceTraceEdge[] } {
  const sentenceByID = new Map(sentences.map((sentence) => [sentence.id, sentence]));
  const referencedODIDs: string[] = [];
  const seen = new Set<string>();
  for (const unit of units) {
    for (const id of unit.od_sentence_ids ?? []) {
      if (!seen.has(id)) {
        seen.add(id);
        referencedODIDs.push(id);
      }
    }
  }

  const odNodes = referencedODIDs.map((id) => {
    const sentence = sentenceByID.get(id);
    return {
      id: `od:${id}`,
      type: "od_sentence" as const,
      text: sentence?.text ?? "Referenced OD unit is missing from the combined document.",
      meta: `${id} · ${sentence?.kind ?? "sentence"} · ${sentence?.transformation ?? "unknown"}`,
      missing: !sentence,
    };
  });
  const sourceNodes = units.map((unit) => ({
    id: `su:${unit.id}`,
    type: "source_unit" as const,
    text: unit.normalized_text,
    meta: `${unit.id} · ${unit.kind} · ${unit.relevance}`,
  }));
  const edges = units.flatMap((unit) =>
    (unit.od_sentence_ids ?? []).map((odID) => ({
      id: `edge:${odID}:${unit.id}`,
      source: `od:${odID}`,
      target: `su:${unit.id}`,
    })),
  );
  return { nodes: [...odNodes, ...sourceNodes], edges };
}

export function SourceTraceGraph({
  sentences,
  units,
  onSelectUnit,
}: {
  sentences: CombinedDocumentSentence[];
  units: SourceUnit[];
  onSelectUnit: (unit: SourceUnit) => void;
}) {
  const [selectedNode, setSelectedNode] = useState<string | null>(null);
  const trace = useMemo(() => buildSourceTrace(sentences, units), [sentences, units]);
  const unitByNodeID = useMemo(() => new Map(units.map((unit) => [`su:${unit.id}`, unit])), [units]);
  const related = useMemo(() => {
    if (!selectedNode) return new Set<string>();
    const ids = new Set<string>([selectedNode]);
    for (const edge of trace.edges) {
      if (edge.source === selectedNode || edge.target === selectedNode) {
        ids.add(edge.source);
        ids.add(edge.target);
      }
    }
    return ids;
  }, [selectedNode, trace.edges]);

  const flowNodes = useMemo<Node[]>(() => {
    let odIndex = 0;
    let sourceIndex = 0;
    return trace.nodes.map((node) => {
      const index = node.type === "od_sentence" ? odIndex++ : sourceIndex++;
      const selected = selectedNode === node.id;
      const dimmed = selectedNode !== null && !related.has(node.id);
      return {
        id: node.id,
        position: { x: node.type === "od_sentence" ? 30 : 530, y: index * 150 + 30 },
        data: {
          label: (
            <div className="source-trace-node-content">
              <strong>{node.meta}</strong>
              <span>{node.text}</span>
            </div>
          ),
        },
        style: {
          width: 330,
          borderColor: node.missing ? "#dc2626" : selected ? "#0f766e" : node.type === "od_sentence" ? "#64748b" : "#0f766e",
          background: node.type === "od_sentence" ? "#f8fafc" : "#f0fdfa",
          boxShadow: selected ? "0 0 0 3px rgb(15 118 110 / 0.18)" : "0 5px 14px rgb(15 23 42 / 0.08)",
          opacity: dimmed ? 0.28 : 1,
          padding: 0,
        },
      };
    });
  }, [related, selectedNode, trace.nodes]);

  const flowEdges = useMemo<Edge[]>(() => trace.edges.map((edge) => {
    const highlighted = selectedNode === edge.source || selectedNode === edge.target;
    const dimmed = selectedNode !== null && !highlighted;
    return {
      ...edge,
      markerEnd: { type: MarkerType.ArrowClosed },
      animated: highlighted,
      style: { stroke: highlighted ? "#0f766e" : "#94a3b8", strokeWidth: highlighted ? 2.5 : 1.4, opacity: dimmed ? 0.2 : 1 },
    };
  }), [selectedNode, trace.edges]);

  if (!units.length) {
    return <p className="muted">Generate or widen the current SourceUnit filter to populate the trace graph.</p>;
  }
  if (!trace.edges.length) {
    return <p className="muted">The loaded SourceUnits do not contain OD sentence references.</p>;
  }

  return (
    <div className="source-trace">
      <div className="toolbar source-trace-legend">
        <Badge>OD sentence</Badge>
        <span aria-hidden="true">→</span>
        <Badge tone="good">Backend-owned SourceUnit</Badge>
        <span className="muted">Click a node to isolate its lineage.</span>
      </div>
      <div className="source-trace-canvas" aria-label="OD sentence to SourceUnit trace graph">
        <ReactFlow
          nodes={flowNodes}
          edges={flowEdges}
          onNodeClick={(_, node) => {
            setSelectedNode(node.id);
            const unit = unitByNodeID.get(node.id);
            if (unit) onSelectUnit(unit);
          }}
          onPaneClick={() => setSelectedNode(null)}
          nodesDraggable={false}
          fitView
          minZoom={0.25}
          maxZoom={1.5}
        >
          <Background />
          <Controls showInteractive={false} />
        </ReactFlow>
      </div>
    </div>
  );
}
