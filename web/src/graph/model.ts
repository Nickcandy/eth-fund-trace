import type { BridgeEdge, NodeRef, RiskAssociation, TraceResult } from "../api/types";
import { displayDecimals } from "../lib/format";

export const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";

export interface GraphNodeModel {
  id: string; chain: string; address: string; hop: number; terminal: boolean; seed: boolean;
  risk: "high" | "suspected" | "normal"; hotWallet: boolean; labelTypes: string[]; inferenceConfidence?: number;
}

export interface GraphEdgeModel {
  id: string; source: string; target: string; chain: string; asset: string; assetSymbol: string;
  sourceType: string; kind: string; count: number; totalAmount: string; decimals?: number;
  conversionStatus?: "complete" | "partial"; conversionScanned?: number; bridge?: BridgeEdge;
}

export interface GraphModel { nodes: GraphNodeModel[]; edges: GraphEdgeModel[] }

const nodeID = (chain: string, address: string) => `${chain}:${address.toLowerCase()}`;

function signedHops(result: TraceResult, seed: NodeRef): Map<string, number> {
  const seedID = nodeID(seed.chain, seed.address);
  const hops = new Map<string, number>([[seedID, 0]]);
  const queue = [...hops.keys()];
  while (queue.length > 0) {
    const current = queue.shift()!;
    const [chain, address] = current.split(":");
    const currentHop = hops.get(current)!;
    for (const edge of result.edges) {
      if (edge.chain !== chain) continue;
      let next: string | undefined;
      let hop = currentHop;
      if (edge.to.toLowerCase() === address && currentHop <= 0) {
        next = nodeID(chain, edge.from); hop = currentHop - 1;
      } else if (edge.from.toLowerCase() === address && currentHop >= 0) {
        next = nodeID(chain, edge.to); hop = currentHop + 1;
      }
      if (next && !hops.has(next)) { hops.set(next, hop); queue.push(next); }
    }
    for (const edge of result.bridgeEdges ?? []) {
      const link = edge.link;
      const source = nodeID(link.sourceChain, link.sourceAddress);
      const target = nodeID(link.targetChain, link.targetAddress);
      if (current === source && currentHop >= 0 && !hops.has(target)) { hops.set(target, currentHop + 1); queue.push(target); }
      if (current === target && currentHop <= 0 && !hops.has(source)) { hops.set(source, currentHop - 1); queue.push(source); }
    }
  }
  return hops;
}

export function buildGraphModel(result: TraceResult, seed: NodeRef, associations: RiskAssociation[] = [], riskSource?: NodeRef): GraphModel {
  const hops = signedHops(result, seed);
  const labelMap = new Map<string, Array<{ type: string; confidence: number }>>();
  for (const label of result.labels ?? []) {
    const values = labelMap.get(label.address.toLowerCase()) ?? [];
    labelMap.set(label.address.toLowerCase(), [...values, { type: label.type, confidence: label.confidence }]);
  }
  const nodes = result.nodes.map((node): GraphNodeModel => {
    const isSeedChain = node.chain === seed.chain;
    const labels = isSeedChain ? labelMap.get(node.address.toLowerCase()) ?? [] : [];
    const association = associations.find((item) => item.targetChain === node.chain && item.targetAddress.toLowerCase() === node.address.toLowerCase());
    const isRiskSource = riskSource && nodeID(node.chain, node.address) === nodeID(riskSource.chain, riskSource.address);
    return {
      id: nodeID(node.chain, node.address), chain: node.chain, address: node.address.toLowerCase(),
      hop: hops.get(nodeID(node.chain, node.address)) ?? node.depth,
      terminal: node.terminal, seed: nodeID(node.chain, node.address) === nodeID(seed.chain, seed.address),
      risk: isRiskSource || association?.level === "strong" ? "high" : association ? "suspected" : "normal",
      hotWallet: labels.some((label) => label.type === "suspected_hot_wallet"), labelTypes: labels.map((label) => label.type),
      inferenceConfidence: association?.confidence,
    };
  });
  const grouped = new Map<string, GraphEdgeModel>();
  result.edges.forEach((edge, index) => {
    const key = [edge.chain, edge.from.toLowerCase(), edge.to.toLowerCase(), edge.asset.toLowerCase(), edge.kind, index].join("|");
    grouped.set(key, {
      id: key, source: nodeID(edge.chain, edge.from), target: nodeID(edge.chain, edge.to), chain: edge.chain,
      asset: edge.asset, assetSymbol: edge.symbol || edge.asset, sourceType: "aggregate", kind: edge.kind,
      count: edge.transferCount, totalAmount: edge.totalAmount,
      decimals: displayDecimals(edge.assetType, edge.asset, edge.decimals, edge.tokenMetadataComplete),
      conversionStatus: edge.conversionStatus, conversionScanned: edge.conversionScanned,
    });
  });
  for (const bridge of result.bridgeEdges ?? []) {
    const link = bridge.link;
    const key = `bridge:${link.sourceChain}:${link.sourceTxHash}:${link.sourceLogIndex}:${link.targetChain}:${link.targetTxHash}:${link.targetLogIndex}`;
    grouped.set(key, {
      id: key, source: nodeID(link.sourceChain, link.sourceAddress), target: nodeID(link.targetChain, link.targetAddress), chain: `${link.sourceChain}->${link.targetChain}`,
      asset: link.sourceAsset, assetSymbol: link.sourceAsset, sourceType: "bridge", kind: "bridge", count: 1,
      totalAmount: link.sourceAmount, decimals: displayDecimals(undefined, link.sourceAsset, undefined, undefined), bridge,
    });
  }
  return { nodes, edges: [...grouped.values()] };
}
