import type { ConversionEvidence, NodeRef, TraceResult } from "../api/types";
import { displayDecimals } from "../lib/format";

export const ZERO_ADDRESS = "0x0000000000000000000000000000000000000000";

export interface GraphNodeModel {
  id: string;
  chain: string;
  address: string;
  hop: number;
  terminal: boolean;
  seed: boolean;
  risk: "high" | "suspected" | "normal";
  hotWallet: boolean;
  labelTypes: string[];
  inferenceConfidence?: number;
  addressType?: "unknown" | "eoa" | "contract";
  protocol?: string;
  roles?: string[];
  stopReason?: string;
  category?:
    | "seed"
    | "high_frequency"
    | "contract"
    | "hot_wallet"
    | "terminal"
    | "address";
}

export interface GraphEdgeModel {
  id: string;
  source: string;
  target: string;
  chain: string;
  sourceChain?: string;
  targetChain?: string;
  assetType?: string;
  asset: string;
  assetSymbol: string;
  sourceType: string;
  kind: string;
  count: number;
  totalAmount: string;
  decimals?: number;
  txHash?: string;
  txHashes?: string[];
  sourceTxHash?: string;
  sourceAmount?: string;
  sourceAsset?: string;
  bidirectional?: boolean;
  swapLegs?: GraphEdgeLeg[];
  firstBlock?: number;
  firstTime?: string;
  latestBlock?: number;
  latestTime?: string;
  flow?: "inbound" | "outbound" | "return";
  conversionStatus?: "complete" | "partial";
  conversionScanned?: number;
  conversionEvidence?: ConversionEvidence[];
  protocol?: string;
  protocolAction?: string;
  protocolMemo?: string;
}

export interface GraphEdgeLeg {
  source: string;
  target: string;
  assetType?: string;
  asset: string;
  assetSymbol: string;
  totalAmount: string;
  decimals?: number;
  kind: string;
}

export interface GraphModel {
  nodes: GraphNodeModel[];
  edges: GraphEdgeModel[];
}

const nodeID = (chain: string, address: string) =>
  `${chain}:${address.toLowerCase()}`;

function signedHops(result: TraceResult, seed: NodeRef): Map<string, number> {
  const seedID = nodeID(seed.chain, seed.address);
  const hops = new Map<string, number>([[seedID, 0]]);
  const queue = [...hops.keys()];
  while (queue.length > 0) {
    const current = queue.shift()!;
    const currentHop = hops.get(current)!;
    for (const edge of result.edges) {
      const sourceChain = edge.sourceChain ?? edge.chain;
      const targetChain = edge.targetChain ?? edge.chain;
      const source = nodeID(sourceChain, edge.from);
      const target = nodeID(targetChain, edge.to);
      let next: string | undefined;
      let hop = currentHop;
      if (target === current && currentHop <= 0) {
        next = source;
        hop = currentHop - 1;
      } else if (source === current && currentHop >= 0) {
        next = target;
        hop = currentHop + 1;
      }
      if (next && !hops.has(next)) {
        hops.set(next, hop);
        queue.push(next);
      }
    }
  }
  return hops;
}

export function buildGraphModel(
  result: TraceResult,
  seed: NodeRef,
): GraphModel {
  const hops = signedHops(result, seed);
  const labelMap = new Map<
    string,
    Array<{ type: string; confidence: number; riskLevel?: string }>
  >();
  for (const label of result.labels ?? []) {
    const values = labelMap.get(label.address.toLowerCase()) ?? [];
    labelMap.set(label.address.toLowerCase(), [
      ...values,
      {
        type: label.type,
        confidence: label.confidence,
        riskLevel: label.riskLevel,
      },
    ]);
  }
  const nodes = result.nodes.map((node): GraphNodeModel => {
    const isSeedChain = node.chain === seed.chain;
    const labels = isSeedChain
      ? (labelMap.get(node.address.toLowerCase()) ?? [])
      : [];
    return {
      id: nodeID(node.chain, node.address),
      chain: node.chain,
      address: node.address.toLowerCase(),
      hop: hops.get(nodeID(node.chain, node.address)) ?? node.depth,
      terminal: node.terminal,
      seed:
        nodeID(node.chain, node.address) === nodeID(seed.chain, seed.address),
      risk: labels.some(
        (label) =>
          label.riskLevel === "high" ||
          label.type === "known_illicit" ||
          label.type === "sanctioned",
      )
        ? "high"
        : "normal",
      hotWallet: labels.some((label) => label.type === "suspected_hot_wallet"),
      labelTypes: labels.map((label) => label.type),
      addressType: node.addressType,
      protocol: node.protocol,
      roles: node.roles ?? [],
      stopReason: node.stopReason,
      category:
        node.stopReason === "high_frequency"
          ? "high_frequency"
          : node.addressType === "contract"
            ? "contract"
            : labels.some((label) => label.type === "suspected_hot_wallet")
              ? "hot_wallet"
              : node.terminal
                ? "terminal"
                : nodeID(node.chain, node.address) ===
                    nodeID(seed.chain, seed.address)
                  ? "seed"
                  : "address",
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
  result.edges.forEach((edge) => {
    const key = [
      edge.sourceChain ?? edge.chain,
      edge.targetChain ?? edge.chain,
      edge.from.toLowerCase(),
      edge.to.toLowerCase(),
      edge.asset.toLowerCase(),
      edge.kind,
      edge.protocol ?? "",
      edge.protocolAction ?? "",
      protocolRouteKey(edge.protocolMemo),
      edge.path.join(">"),
    ].join("|");
    const sourceChain = edge.sourceChain ?? edge.chain;
    const targetChain = edge.targetChain ?? edge.chain;
    const source = nodeID(sourceChain, edge.from);
    const target = nodeID(targetChain, edge.to);
    const existing = grouped.get(key);
    if (existing) {
      const left = BigInt(existing.totalAmount || "0");
      const right = BigInt(edge.totalAmount || "0");
      existing.totalAmount = (left + right).toString();
      existing.count += edge.transferCount;
      existing.txHashes = [...(existing.txHashes ?? []), ...(edge.txHash ? [edge.txHash] : [])];
      existing.firstBlock = Math.min(existing.firstBlock ?? edge.firstBlock ?? 0, edge.firstBlock ?? existing.firstBlock ?? 0);
      existing.latestBlock = Math.max(existing.latestBlock ?? edge.latestBlock ?? 0, edge.latestBlock ?? existing.latestBlock ?? 0);
      existing.latestTime = edge.latestTime ?? existing.latestTime;
      return;
    }
    grouped.set(key, {
      id: key,
      source,
      target,
      chain: edge.chain,
      sourceChain,
      targetChain,
      assetType: edge.assetType,
      asset: edge.asset,
      assetSymbol: edge.symbol || edge.asset,
      sourceType: "aggregate",
      kind: edge.kind,
      count: edge.transferCount,
      totalAmount: edge.totalAmount,
      txHash: edge.txHash,
      txHashes: edge.txHash ? [edge.txHash] : [],
      sourceTxHash: edge.sourceTxHash,
      sourceAmount: edge.sourceAmount,
      sourceAsset: edge.sourceAsset,
      flow: flow(source, target),
      decimals: displayDecimals(
        edge.assetType,
        edge.asset,
        edge.decimals,
        edge.tokenMetadataComplete,
      ),
      firstBlock: edge.firstBlock,
      firstTime: edge.firstTime,
      latestBlock: edge.latestBlock,
      latestTime: edge.latestTime,
      conversionStatus: edge.conversionStatus,
      conversionScanned: edge.conversionScanned,
      conversionEvidence: edge.conversionEvidence,
      protocol: edge.protocol,
      protocolAction: edge.protocolAction,
      protocolMemo: edge.protocolMemo,
    });
  });
  return {
    nodes: applyTHORChainVaultRoles(nodes, [...grouped.values()]),
    edges: combineSwapEdges([...grouped.values()]),
  };
}

function protocolRouteKey(memo?: string): string {
  if (!memo) return "";
  const parts = memo.split(":");
  return parts.length >= 3
    ? parts.slice(0, 3).map((part) => part.toLowerCase()).join(":")
    : memo.toLowerCase();
}

function combineSwapEdges(edges: GraphEdgeModel[]): GraphEdgeModel[] {
  const consumed = new Set<string>();
  const result: GraphEdgeModel[] = [];
  for (const edge of edges) {
    if (consumed.has(edge.id)) continue;
    const verifiedSwap =
      edge.kind === "swap" &&
      edge.txHash &&
      edge.conversionEvidence?.some(
        (item) =>
          item.status === "complete" &&
          item.txHash.toLowerCase() === edge.txHash?.toLowerCase(),
      );
    const inputs = verifiedSwap
      ? edges.filter(
          (candidate) =>
            candidate.id !== edge.id &&
            !consumed.has(candidate.id) &&
            candidate.kind !== "swap" &&
            candidate.chain === edge.chain &&
            candidate.txHash?.toLowerCase() === edge.txHash?.toLowerCase() &&
            candidate.target === edge.source,
        )
      : [];
    if (inputs.length !== 1) {
      result.push(edge);
      continue;
    }
    const input = inputs[0];
    consumed.add(edge.id);
    consumed.add(input.id);
    const existingInput = result.findIndex(
      (candidate) => candidate.id === input.id,
    );
    if (existingInput >= 0) result.splice(existingInput, 1);
    const endpoints = [input.source, input.target].sort().join("|");
    result.push({
      ...edge,
      id: `swap:${edge.chain}:${edge.txHash}:${endpoints}`,
      source: input.source,
      target: input.target,
      flow: input.flow,
      count: 1,
      bidirectional: true,
      swapLegs: [toSwapLeg(input), toSwapLeg(edge)],
    });
  }
  return result;
}

function toSwapLeg(edge: GraphEdgeModel): GraphEdgeLeg {
  return {
    source: edge.source,
    target: edge.target,
    assetType: edge.assetType,
    asset: edge.asset,
    assetSymbol: edge.assetSymbol,
    totalAmount: edge.totalAmount,
    decimals: edge.decimals,
    kind: edge.kind,
  };
}

function applyTHORChainVaultRoles(
  nodes: GraphNodeModel[],
  edges: GraphEdgeModel[],
): GraphNodeModel[] {
  const vaults = new Set(
    edges
      .filter(
        (edge) =>
          edge.protocol === "thorchain" &&
          edge.protocolAction === "vault_migration",
      )
      .map((edge) => edge.target),
  );
  return nodes.map((node) =>
    vaults.has(node.id)
      ? {
          ...node,
          protocol: "thorchain",
          roles: [...new Set([...(node.roles ?? []), "thorchain_vault"])],
        }
      : node,
  );
}
