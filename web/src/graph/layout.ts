import ELK from "elkjs/lib/elk.bundled.js";
import type { GraphModel } from "./model";

const elk = new ELK();
export const NODE_WIDTH = 214;
export const NODE_HEIGHT = 88;

export interface PositionedNode { id: string; x: number; y: number }

export function buildELKInput(model: GraphModel, side: "upstream" | "downstream") {
  const eligible = model.nodes.filter((node) => side === "upstream" ? node.hop <= 0 : node.hop >= 0);
  const ids = new Set(eligible.map((node) => node.id));
  return {
    id: side,
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "RIGHT",
      "elk.spacing.nodeNode": "58",
      "elk.layered.spacing.nodeNodeBetweenLayers": "110",
      "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
    },
    children: eligible.map((node) => ({ id: node.id, width: NODE_WIDTH, height: NODE_HEIGHT })),
    edges: model.edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target)).map((edge) => ({
      id: edge.id, sources: side === "upstream" ? [edge.target] : [edge.source], targets: side === "upstream" ? [edge.source] : [edge.target],
    })),
  };
}

export async function layoutGraph(model: GraphModel): Promise<Map<string, PositionedNode>> {
  const [upstream, downstream] = await Promise.all([
    elk.layout(buildELKInput(model, "upstream")), elk.layout(buildELKInput(model, "downstream")),
  ]);
  const result = new Map<string, PositionedNode>();
  const seed = model.nodes.find((node) => node.seed);
  if (!seed) return result;
  const upSeed = upstream.children?.find((node) => node.id === seed.id);
  const downSeed = downstream.children?.find((node) => node.id === seed.id);
  const upBaseX = upSeed?.x ?? 0; const upBaseY = upSeed?.y ?? 0;
  const downBaseX = downSeed?.x ?? 0; const downBaseY = downSeed?.y ?? 0;
  for (const child of upstream.children ?? []) {
    result.set(child.id, { id: child.id, x: -((child.x ?? 0) - upBaseX), y: (child.y ?? 0) - upBaseY });
  }
  for (const child of downstream.children ?? []) {
    result.set(child.id, { id: child.id, x: (child.x ?? 0) - downBaseX, y: (child.y ?? 0) - downBaseY });
  }
  // Place Base below the full Ethereum extent instead of using a fixed offset.
  const ethereumBottom = Math.max(0, ...model.nodes.filter((node) => node.chain === "ethereum").map((node) => result.get(node.id)?.y ?? 0));
  const baseTop = Math.min(0, ...model.nodes.filter((node) => node.chain === "base").map((node) => result.get(node.id)?.y ?? 0));
  const baseOffset = ethereumBottom - baseTop + NODE_HEIGHT + 120;
  for (const node of model.nodes) {
    const position = result.get(node.id);
    if (position && node.chain === "base") position.y += baseOffset;
  }
  return result;
}
