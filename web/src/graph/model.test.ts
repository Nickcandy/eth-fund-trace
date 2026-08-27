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
      { chain: "ethereum", from: upstream, to: seed, assetType: "native", asset: "ETH", totalAmount: "100", transferCount: 1, kind: "transfer", depth: 1, path: [seed, upstream] },
      { chain: "ethereum", from: seed, to: downstream, assetType: "erc20", asset: "0x0000000000000000000000000000000000000010", symbol: "USDC", decimals: 6, tokenMetadataComplete: true, totalAmount: "500", transferCount: 2, kind: "transfer", depth: 1, path: [seed, downstream] },
    ],
    bridgeEdges: [], crossChainPaths: [], paths: [], dataThroughBlock: 3, dataThroughBlocks: { ethereum: 3 }, dataStatus: "synced",
	labels: [], risk: { score: 0, level: "no_conclusion", evidence: [], inferredLabels: [], ruleVersion: "risk-v1", propagationVersion: "propagation-v1" }, ruleVersion: "trace-v1",
  };
}

describe("buildGraphModel", () => {
  it("places upstream and downstream on opposite signed hops", () => {
    const model = buildGraphModel(result(), { chain: "ethereum", address: seed });
    expect(model.nodes.find((node) => node.address === upstream)?.hop).toBe(-1);
    expect(model.nodes.find((node) => node.address === downstream)?.hop).toBe(1);
  });

  it("uses the backend aggregate amount and transfer count", () => {
    const model = buildGraphModel(result(), { chain: "ethereum", address: seed });
    const edge = model.edges.find((candidate) => candidate.assetSymbol === "USDC");
    expect(edge).toMatchObject({ count: 2, totalAmount: "500" });
  });

  it("uses token precision from the aggregate edge", () => {
    const value = result();
    const model = buildGraphModel(value, { chain: "ethereum", address: seed });
    const edge = model.edges.find((candidate) => candidate.asset === value.edges[1].asset);

    expect(edge).toMatchObject({ decimals: 6, assetSymbol: "USDC" });
  });

  it("preserves protocol roles and conversion evidence", () => {
	const value = result();
	value.nodes[1] = { ...value.nodes[1], addressType: "contract", protocol: "kyberswap", roles: ["kyberswap_executor"] };
	value.edges[0] = { ...value.edges[0], kind: "swap", conversionEvidence: [{ txHash: "0xswap", protocol: "kyberswap", version: "rfq", status: "complete", liquidityProvider: downstream, tokenIn: "USDT", amountIn: "1000000", tokenOut: "ETH", amountOut: "1", evidence: ["internal ETH calls"] }] };

	const model = buildGraphModel(value, { chain: "ethereum", address: seed });
	expect(model.nodes.find((node) => node.address === upstream)).toMatchObject({ addressType: "contract", protocol: "kyberswap", roles: ["kyberswap_executor"] });
	expect(model.edges[0].conversionEvidence?.[0]).toMatchObject({ txHash: "0xswap", liquidityProvider: downstream });
  });

  it("keeps the same address on another chain distinct from the seed", () => {
    const value = result();
    value.nodes.push({ chain: "base", address: seed, depth: 2, terminal: false });
    value.bridgeEdges = [{
      depth: 2,
      path: [{ chain: "ethereum", address: downstream }, { chain: "base", address: seed }],
      link: { sourceChain: "ethereum", sourceTxHash: "0x1", sourceLogIndex: 1, sourceAddress: downstream, targetChain: "base", targetTxHash: "0x2", targetLogIndex: 1, targetAddress: seed, bridgeAddress: downstream, sourceAsset: "ETH", sourceAmount: "1", targetAsset: "ETH", targetAmount: "1", evidence: ["case"] },
    }];
    const model = buildGraphModel(value, { chain: "ethereum", address: seed });
    expect(model.nodes.filter((node) => node.seed).map((node) => node.id)).toEqual([`ethereum:${seed}`]);
    expect(model.nodes.find((node) => node.id === `base:${seed}`)?.hop).toBe(2);
    expect(model.nodes.find((node) => node.id === `base:${seed}`)?.risk).toBe("normal");
  });

  it("treats null associations from historical propagation results as empty", () => {
    expect(() => buildGraphModel(result(), { chain: "ethereum", address: seed }, null as never)).not.toThrow();
  });

  it("classifies transfer direction relative to the query center", () => {
    const model = buildGraphModel(result(), { chain: "ethereum", address: seed });
    expect(model.edges.find((edge) => edge.source.endsWith(upstream))?.flow).toBe("inbound");
    expect(model.edges.find((edge) => edge.target.endsWith(downstream))?.flow).toBe("outbound");
  });
});
