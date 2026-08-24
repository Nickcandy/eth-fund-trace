import type { BridgeEdge, NodeRef, TraceResult, Transfer } from "../api/types";
import { displayDecimals } from "../lib/format";

export const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";

export interface GraphNodeModel {
  id: string; chain: string; address: string; hop: number; terminal: boolean; seed: boolean;
  risk: "high" | "suspected" | "normal"; hotWallet: boolean; labelTypes: string[]; inferenceConfidence?: number;
}

export interface GraphEdgeModel {
  id: string; source: string; target: string; chain: string; asset: string; assetSymbol: string;
  sourceType: string; kind: string; count: number; totalAmount: string; decimals?: number;
  facts: Transfer[]; bridge?: BridgeEdge;
}

export interface GraphModel { nodes: GraphNodeModel[]; edges: GraphEdgeModel[] }

const nodeID = (chain: string, address: string) => `${chain}:${address.toLowerCase()}`;

function addIntegerStrings(left: string, right: string): string {
  try { return (BigInt(left || "0") + BigInt(right || "0")).toString(); }
  catch { return left || right || "0"; }
}

function signedHops(result: TraceResult, seed: NodeRef): Map<string, number> {
  const seedID = nodeID(seed.chain, seed.address);
  const hops = new Map<string, number>([[seedID, 0]]);
  const queue = [...hops.keys()];
  while (queue.length > 0) {
    const current = queue.shift()!;
    const [chain, address] = current.split(":");
    const currentHop = hops.get(current)!;
    for (const edge of result.edges) {
      const transfer = edge.transfer;
      if (transfer.chain !== chain) continue;
      let next: string | undefined;
      let hop = currentHop;
      if (transfer.to.toLowerCase() === address && currentHop <= 0) {
        next = nodeID(chain, transfer.from); hop = currentHop - 1;
      } else if (transfer.from.toLowerCase() === address && currentHop >= 0) {
        next = nodeID(chain, transfer.to); hop = currentHop + 1;
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

export function buildGraphModel(result: TraceResult, seed: NodeRef, aggregate: boolean): GraphModel {
  const hops = signedHops(result, seed);
  const labelMap = new Map<string, Array<{ type: string; confidence: number }>>();
  for (const label of result.labels ?? []) {
    const values = labelMap.get(label.address.toLowerCase()) ?? [];
    labelMap.set(label.address.toLowerCase(), [...values, { type: label.type, confidence: label.confidence }]);
  }
  const nodes = result.nodes.map((node): GraphNodeModel => {
    const isSeedChain = node.chain === seed.chain;
    const labels = isSeedChain ? labelMap.get(node.address.toLowerCase()) ?? [] : [];
    const riskEvidence = isSeedChain && result.risk.evidence?.some((e) => e.address.toLowerCase() === node.address.toLowerCase() && e.score >= 70);
    return {
      id: nodeID(node.chain, node.address), chain: node.chain, address: node.address.toLowerCase(),
      hop: hops.get(nodeID(node.chain, node.address)) ?? node.depth,
      terminal: node.terminal, seed: nodeID(node.chain, node.address) === nodeID(seed.chain, seed.address),
      risk: riskEvidence ? "high" : labels.length ? "suspected" : "normal",
      hotWallet: labels.some((label) => label.type === "suspected_hot_wallet"), labelTypes: labels.map((label) => label.type),
      inferenceConfidence: labels.length ? Math.max(...labels.map((label) => label.confidence)) : undefined,
    };
  });
  const grouped = new Map<string, GraphEdgeModel>();
  result.edges.forEach((edge, index) => {
    const fact = edge.transfer;
    const key = aggregate
      ? [fact.chain, fact.from.toLowerCase(), fact.to.toLowerCase(), fact.asset.toLowerCase(), fact.source].join("|")
      : `${fact.txHash}|${fact.source}|${fact.traceId}|${fact.logIndex}|${index}`;
    const existing = grouped.get(key);
    if (existing) {
      existing.count += 1;
      existing.totalAmount = addIntegerStrings(existing.totalAmount, fact.amount ?? fact.tokenValue ?? "0");
      existing.facts.push(fact);
      const decimals = displayDecimals(fact.assetType, fact.asset, fact.decimals, fact.tokenMetadataComplete);
      if (existing.decimals === undefined && decimals !== undefined) existing.decimals = decimals;
      if (existing.assetSymbol === existing.asset && fact.symbol) existing.assetSymbol = fact.symbol;
    } else {
      grouped.set(key, {
        id: key, source: nodeID(fact.chain, fact.from), target: nodeID(fact.chain, fact.to), chain: fact.chain,
        asset: fact.asset, assetSymbol: fact.symbol || fact.asset, sourceType: fact.source, kind: fact.transferKind ?? "transfer",
        count: 1, totalAmount: fact.amount ?? fact.tokenValue ?? "0", decimals: displayDecimals(fact.assetType, fact.asset, fact.decimals, fact.tokenMetadataComplete),
        facts: [fact],
      });
    }
  });
  for (const bridge of result.bridgeEdges ?? []) {
    const link = bridge.link;
    const key = `bridge:${link.sourceChain}:${link.sourceTxHash}:${link.sourceLogIndex}:${link.targetChain}:${link.targetTxHash}:${link.targetLogIndex}`;
    grouped.set(key, {
      id: key, source: nodeID(link.sourceChain, link.sourceAddress), target: nodeID(link.targetChain, link.targetAddress), chain: `${link.sourceChain}->${link.targetChain}`,
      asset: link.sourceAsset, assetSymbol: link.sourceAsset, sourceType: "bridge", kind: "bridge", count: 1,
      totalAmount: link.sourceAmount, decimals: displayDecimals(undefined, link.sourceAsset, undefined, undefined), facts: [], bridge,
    });
  }
  return { nodes, edges: [...grouped.values()] };
}
