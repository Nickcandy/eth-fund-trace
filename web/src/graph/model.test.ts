import { describe, expect, it } from "vitest";
import type { TraceResult } from "../api/types";
import { buildGraphModel } from "./model";

const seed = "0x0000000000000000000000000000000000000001";
const upstream = "0x0000000000000000000000000000000000000002";
const downstream = "0x0000000000000000000000000000000000000003";

function result(): TraceResult {
  return {
    nodes: [
      { chain: "ethereum", address: seed, depth: 0, terminal: false },
      { chain: "ethereum", address: upstream, depth: 1, terminal: false },
      { chain: "ethereum", address: downstream, depth: 1, terminal: false },
    ],
    edges: [
      { depth: 1, path: [seed, upstream], transfer: { chain: "ethereum", chainId: 1, txHash: "0xa", blockNumber: 1, from: upstream, to: seed, assetType: "native", asset: "ETH", amount: "100", source: "txlist", traceId: "", logIndex: 0, transferKind: "transfer" } },
      { depth: 1, path: [seed, downstream], transfer: { chain: "ethereum", chainId: 1, txHash: "0xb", blockNumber: 2, from: seed, to: downstream, assetType: "erc20", asset: "0x0000000000000000000000000000000000000010", symbol: "USDC", decimals: 6, amount: "200", source: "tokentx", traceId: "", logIndex: 1, transferKind: "transfer" } },
      { depth: 1, path: [seed, downstream], transfer: { chain: "ethereum", chainId: 1, txHash: "0xc", blockNumber: 3, from: seed, to: downstream, assetType: "erc20", asset: "0x0000000000000000000000000000000000000010", symbol: "USDC", decimals: 6, amount: "300", source: "tokentx", traceId: "", logIndex: 2, transferKind: "transfer" } },
    ],
    bridgeEdges: [], crossChainPaths: [], paths: [], dataThroughBlock: 3, dataThroughBlocks: { ethereum: 3 }, dataStatus: "synced",
    labels: [], risk: { score: 0, level: "no_conclusion", evidence: [], inferredLabels: [], ruleVersion: "risk-v1", propagationVersion: "propagation-v1" }, ruleVersion: "trace-v2",
  };
}

describe("buildGraphModel", () => {
  it("places upstream and downstream on opposite signed hops", () => {
    const model = buildGraphModel(result(), { chain: "ethereum", address: seed }, true);
    expect(model.nodes.find((node) => node.address === upstream)?.hop).toBe(-1);
    expect(model.nodes.find((node) => node.address === downstream)?.hop).toBe(1);
  });

  it("aggregates same-direction facts only within one asset", () => {
    const model = buildGraphModel(result(), { chain: "ethereum", address: seed }, true);
    const edge = model.edges.find((candidate) => candidate.assetSymbol === "USDC");
    expect(edge).toMatchObject({ count: 2, totalAmount: "500" });
    expect(edge?.facts).toHaveLength(2);
  });

  it("enriches an aggregated edge when a later fact has token precision", () => {
    const value = result();
    const first = value.edges[1].transfer;
    delete first.decimals;
    first.tokenMetadataComplete = false;
    first.symbol = undefined;
    value.edges[2].transfer.tokenMetadataComplete = true;

    const model = buildGraphModel(value, { chain: "ethereum", address: seed }, true);
    const edge = model.edges.find((candidate) => candidate.asset === first.asset);

    expect(edge).toMatchObject({ decimals: 6, assetSymbol: "USDC" });
  });

  it("keeps the same address on another chain distinct from the seed", () => {
    const value = result();
    value.nodes.push({ chain: "base", address: seed, depth: 2, terminal: false });
    value.bridgeEdges = [{
      depth: 2,
      path: [{ chain: "ethereum", address: downstream }, { chain: "base", address: seed }],
      link: { sourceChain: "ethereum", sourceTxHash: "0x1", sourceLogIndex: 1, sourceAddress: downstream, targetChain: "base", targetTxHash: "0x2", targetLogIndex: 1, targetAddress: seed, bridgeAddress: downstream, sourceAsset: "ETH", sourceAmount: "1", targetAsset: "ETH", targetAmount: "1", evidence: ["case"] },
    }];
    const model = buildGraphModel(value, { chain: "ethereum", address: seed }, true);
    expect(model.nodes.filter((node) => node.seed).map((node) => node.id)).toEqual([`ethereum:${seed}`]);
    expect(model.nodes.find((node) => node.id === `base:${seed}`)?.hop).toBe(2);
    expect(model.nodes.find((node) => node.id === `base:${seed}`)?.risk).toBe("normal");
  });
});
