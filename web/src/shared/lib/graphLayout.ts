import ELK from "elkjs/lib/elk.bundled.js";

export interface LayoutNode {
	id: string;
	width: number;
	height: number;
}

export interface LayoutEdge {
	id: string;
	from: string;
	to: string;
}

const elk = new ELK();

// Layered left-to-right layout so relationships read in one direction and
// crossings are minimized; unknown edge endpoints are ignored instead of failing.
export async function layoutGraph(nodes: LayoutNode[], edges: LayoutEdge[]): Promise<Record<string, { x: number; y: number }>> {
	const ids = new Set(nodes.map((node) => node.id));
	const graph = await elk.layout({
		id: "root",
		layoutOptions: {
			"elk.algorithm": "layered",
			"elk.direction": "RIGHT",
			"elk.spacing.nodeNode": "40",
			"elk.layered.spacing.nodeNodeBetweenLayers": "90",
			"elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
			"elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
			"elk.separateConnectedComponents": "true",
			"elk.spacing.componentComponent": "60",
		},
		children: nodes.map((node) => ({ id: node.id, width: node.width, height: node.height })),
		edges: edges
			.filter((edge) => ids.has(edge.from) && ids.has(edge.to) && edge.from !== edge.to)
			.map((edge) => ({ id: edge.id, sources: [edge.from], targets: [edge.to] })),
	});
	const positions: Record<string, { x: number; y: number }> = {};
	for (const child of graph.children ?? []) positions[child.id] = { x: child.x ?? 0, y: child.y ?? 0 };
	return positions;
}
