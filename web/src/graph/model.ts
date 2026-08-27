import type { BridgeEdge, ConversionEvidence, NodeRef, RiskAssociation, TraceResult } from "../api/types";
import { displayDecimals } from "../lib/format";

export const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";

export interface GraphNodeModel {
  id: string; chain: string; address: string; hop: number; terminal: boolean; seed: boolean;
  risk: "high" | "suspected" | "normal"; hotWallet: boolean; labelTypes: string[]; inferenceConfidence?: number;
  addressType?: "unknown" | "eoa" | "contract"; protocol?: string; roles?: string[];
  stopReason?: string; category?: "seed" | "high_frequency" | "contract" | "hot_wallet" | "terminal" | "address";
}

export interface GraphEdgeModel {
  id: string; source: string; target: string; chain: string; assetType?: string; asset: string; assetSymbol: string;
  sourceType: string; kind: string; count: number; totalAmount: string; decimals?: number;
  firstBlock?: number; firstTime?: string; latestBlock?: number; latestTime?: string;
  flow?: "inbound" | "outbound" | "return";
  conversionStatus?: "complete" | "partial"; conversionScanned?: number; bridge?: BridgeEdge;
	conversionEvidence?: ConversionEvidence[];
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

export function buildGraphModel(result: TraceResult, seed: NodeRef, associations: RiskAssociation[] | null = [], riskSource?: NodeRef): GraphModel {
  const normalizedAssociations = associations ?? [];
  const hops = signedHops(result, seed);
  const labelMap = new Map<string, Array<{ type: string; confidence: number }>>();
  for (const label of result.labels ?? []) {
    const values = labelMap.get(label.address.toLowerCase()) ?? [];
    labelMap.set(label.address.toLowerCase(), [...values, { type: label.type, confidence: label.confidence }]);
  }
  const nodes = result.nodes.map((node): GraphNodeModel => {
    const isSeedChain = node.chain === seed.chain;
    const labels = isSeedChain ? labelMap.get(node.address.toLowerCase()) ?? [] : [];
    const association = normalizedAssociations.find((item) => item.targetChain === node.chain && item.targetAddress.toLowerCase() === node.address.toLowerCase());
    const isRiskSource = riskSource && nodeID(node.chain, node.address) === nodeID(riskSource.chain, riskSource.address);
    return {
      id: nodeID(node.chain, node.address), chain: node.chain, address: node.address.toLowerCase(),
      hop: hops.get(nodeID(node.chain, node.address)) ?? node.depth,
      terminal: node.terminal, seed: nodeID(node.chain, node.address) === nodeID(seed.chain, seed.address),
      risk: isRiskSource || association?.level === "strong" ? "high" : association ? "suspected" : "normal",
      hotWallet: labels.some((label) => label.type === "suspected_hot_wallet"), labelTypes: labels.map((label) => label.type),
      inferenceConfidence: association?.confidence,
      addressType: node.addressType, protocol: node.protocol, roles: node.roles ?? [],
      stopReason: node.stopReason,
      category: node.stopReason === "high_frequency" ? "high_frequency" : node.addressType === "contract" ? "contract" : labels.some((label) => label.type === "suspected_hot_wallet") ? "hot_wallet" : node.terminal ? "terminal" : nodeID(node.chain, node.address) === nodeID(seed.chain, seed.address) ? "seed" : "address",
    };
  });
  const nodeHops = new Map(nodes.map((node) => [node.id, node.hop]));
  const flow = (source: string, target: string): GraphEdgeModel["flow"] => {
    const sourceHop = nodeHops.get(source) ?? 0;
    const targetHop = nodeHops.get(target) ?? 0;
    if (Math.abs(targetHop) < Math.abs(sourceHop)) return "inbound";
    if (Math.abs(targetHop) > Math.abs(sourceHop)) return "outbound";
    return "return";
  };
  const grouped = new Map<string, GraphEdgeModel>();
  result.edges.forEach((edge, index) => {
    const key = [edge.chain, edge.from.toLowerCase(), edge.to.toLowerCase(), edge.asset.toLowerCase(), edge.kind, index].join("|");
    const source = nodeID(edge.chain, edge.from); const target = nodeID(edge.chain, edge.to);
    grouped.set(key, {
      id: key, source, target, chain: edge.chain,
      assetType: edge.assetType, asset: edge.asset, assetSymbol: edge.symbol || edge.asset, sourceType: "aggregate", kind: edge.kind,
      count: edge.transferCount, totalAmount: edge.totalAmount, flow: flow(source, target),
      decimals: displayDecimals(edge.assetType, edge.asset, edge.decimals, edge.tokenMetadataComplete),
      firstBlock: edge.firstBlock, firstTime: edge.firstTime, latestBlock: edge.latestBlock, latestTime: edge.latestTime,
      conversionStatus: edge.conversionStatus, conversionScanned: edge.conversionScanned,
	  conversionEvidence: edge.conversionEvidence,
    });
  });
  for (const bridge of result.bridgeEdges ?? []) {
    const link = bridge.link;
    const key = `bridge:${link.sourceChain}:${link.sourceTxHash}:${link.sourceLogIndex}:${link.targetChain}:${link.targetTxHash}:${link.targetLogIndex}`;
    const source = nodeID(link.sourceChain, link.sourceAddress); const target = nodeID(link.targetChain, link.targetAddress);
    grouped.set(key, {
      id: key, source, target, chain: `${link.sourceChain}->${link.targetChain}`,
      assetType: "bridge", asset: link.sourceAsset, assetSymbol: link.sourceAsset, sourceType: "bridge", kind: "bridge", count: 1,
      totalAmount: link.sourceAmount, decimals: displayDecimals(undefined, link.sourceAsset, undefined, undefined), flow: flow(source, target), bridge,
    });
  }
  return { nodes, edges: [...grouped.values()] };
}
