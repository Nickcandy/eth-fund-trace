import ELK from "elkjs/lib/elk.bundled.js";
import type { GraphModel } from "./model";

const elk = new ELK();
export const NODE_WIDTH = 214;
export const NODE_HEIGHT = 88;
export const COLUMN_GAP = 380;
export const ROW_GAP = 58;

export interface PositionedNode {
  id: string;
  x: number;
  y: number;
}

export function buildELKInput(
  model: GraphModel,
  side: "upstream" | "downstream",
) {
  const eligible = model.nodes.filter((node) =>
    side === "upstream" ? node.hop <= 0 : node.hop >= 0,
  );
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
    children: eligible.map((node) => ({
      id: node.id,
      width: NODE_WIDTH,
      height: NODE_HEIGHT,
    })),
    edges: model.edges
      .filter((edge) => ids.has(edge.source) && ids.has(edge.target))
      .map((edge) => ({
        id: edge.id,
        sources: side === "upstream" ? [edge.target] : [edge.source],
        targets: side === "upstream" ? [edge.source] : [edge.target],
      })),
  };
}

export async function layoutGraph(
  model: GraphModel,
): Promise<Map<string, PositionedNode>> {
  const [upstream, downstream] = await Promise.all([
    elk.layout(buildELKInput(model, "upstream")),
    elk.layout(buildELKInput(model, "downstream")),
  ]);
  const result = new Map<string, PositionedNode>();
  const nodesByID = new Map(model.nodes.map((node) => [node.id, node]));
  const seed = model.nodes.find((node) => node.seed);
  if (!seed) return result;
  const upSeed = upstream.children?.find((node) => node.id === seed.id);
  const downSeed = downstream.children?.find((node) => node.id === seed.id);
  const upBaseY = upSeed?.y ?? 0;
  const downBaseY = downSeed?.y ?? 0;
  for (const child of upstream.children ?? []) {
    const node = nodesByID.get(child.id);
    result.set(child.id, {
      id: child.id,
      x: (node?.hop ?? 0) * COLUMN_GAP,
      y: (child.y ?? 0) - upBaseY,
    });
  }
  for (const child of downstream.children ?? []) {
    const node = nodesByID.get(child.id);
    result.set(child.id, {
      id: child.id,
      x: (node?.hop ?? 0) * COLUMN_GAP,
      y: (child.y ?? 0) - downBaseY,
    });
  }
  const columns = new Map<string, typeof model.nodes>();
  for (const node of model.nodes) {
    const key = `${node.chain}:${node.hop}`;
    columns.set(key, [...(columns.get(key) ?? []), node]);
  }
  for (const column of columns.values()) {
    column.sort((left, right) => {
      const y = (result.get(left.id)?.y ?? 0) - (result.get(right.id)?.y ?? 0);
      return y || left.id.localeCompare(right.id);
    });
    const top = -((column.length - 1) * (NODE_HEIGHT + ROW_GAP)) / 2;
    column.forEach((node, index) => {
      const position = result.get(node.id);
      if (position) position.y = top + index * (NODE_HEIGHT + ROW_GAP);
    });
  }
  return result;
}
