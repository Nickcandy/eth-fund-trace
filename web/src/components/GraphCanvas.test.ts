import { describe, expect, it } from "vitest";
import {
  branchNodeIDs,
  edgeLabelVisible,
  expansionMode,
  expansionPathKeys,
  matchesAssetFilter,
  revealedNodeIDs,
  thorchainEdgeLabel,
} from "./GraphCanvas";
import type { GraphEdgeModel, GraphNodeModel } from "../graph/model";

describe("GraphCanvas density defaults", () => {
  it("keeps direct seed edges labeled in dense graphs", () => {
    expect(edgeLabelVisible(false, false, true)).toBe(true);
    expect(edgeLabelVisible(false, false, false)).toBe(false);
    expect(edgeLabelVisible(false, true, false)).toBe(true);
  });
});

describe("GraphCanvas branch expansion", () => {
  const node = (id: string, hop: number): GraphNodeModel => ({
    id,
    hop,
    chain: "ethereum",
    address: id,
    terminal: false,
    seed: hop === 0,
    risk: "normal",
    hotWallet: false,
    labelTypes: [],
  });
  const edge = (source: string, target: string): GraphEdgeModel => ({
    id: `${source}-${target}`,
    source,
    target,
    chain: "ethereum",
    asset: "ETH",
    assetSymbol: "ETH",
    sourceType: "aggregate",
    kind: "transfer",
    count: 1,
    totalAmount: "1",
  });
  const nodes = [
    node("seed", 0),
    node("middle", 1),
    node("right", 2),
    node("left", -1),
  ];
  const edges = [
    edge("seed", "middle"),
    edge("middle", "right"),
    edge("left", "middle"),
  ];

  it("returns nodes extending on each side independently", () => {
    expect(branchNodeIDs("middle", "right", nodes, edges)).toEqual(["right"]);
    expect(branchNodeIDs("middle", "left", nodes, edges)).toEqual([
      "seed",
      "left",
    ]);
  });

  it("returns no expansion target for the rightmost node", () => {
    expect(branchNodeIDs("right", "right", nodes, edges)).toEqual([]);
  });

  it("keeps a directed downstream reconnection visible when its target has a shallower hop", () => {
    const reconnected = node("terminal", 3);
    const deep = node("deep", 4);
    expect(
      branchNodeIDs(
        deep.id,
        "right",
        [...nodes, deep, reconnected],
        [edge(deep.id, reconnected.id)],
      ),
    ).toEqual([reconnected.id]);
  });

  it("offers continued tracing for an ordinary rightmost node", () => {
    expect(expansionMode(nodes[2], [])).toBe("trace");
  });

  it("does not offer continued tracing for a terminal node", () => {
    expect(expansionMode({ ...nodes[2], terminal: true }, [])).toBe("none");
  });

  it("does not offer a continuation without an entering edge for the seed", () => {
    expect(expansionMode(nodes[0], [])).toBe("none");
  });

  it("restores every expansion on the path to a continued node", () => {
    expect(expansionPathKeys("right", "right", nodes, edges)).toEqual([
      "right:right",
      "middle:right",
      "seed:right",
    ]);
    expect(
      expansionPathKeys("left", "left", nodes, [
        ...edges,
        edge("left", "seed"),
      ]),
    ).toEqual(["left:left", "seed:left"]);
  });

  it("reveals manually expanded nodes beyond the original five-hop viewport", () => {
    const deepNodes = Array.from({ length: 7 }, (_, hop) =>
      node(`node-${hop}`, hop),
    );
    const deepEdges = deepNodes
      .slice(1)
      .map((current, index) => edge(deepNodes[index].id, current.id));
    const expanded = new Set(
      deepNodes.slice(0, -1).map((current) => `${current.id}:right`),
    );

    expect(revealedNodeIDs(deepNodes, deepEdges, expanded)).toContain("node-6");
  });
});

describe("GraphCanvas asset filters", () => {
  const edge = (
    assetType: string,
    asset: string,
    assetSymbol: string,
    kind = "transfer",
  ): GraphEdgeModel => ({
    id: "1",
    source: "a",
    target: "b",
    chain: "ethereum",
    assetType,
    asset,
    assetSymbol,
    sourceType: "aggregate",
    kind,
    count: 1,
    totalAmount: "1",
    decimals: 18,
  });

  it("separates ETH, USDT and other ERC-20 edges", () => {
    expect(matchesAssetFilter(edge("native", "ETH", "ETH"), "ETH")).toBe(true);
    expect(matchesAssetFilter(edge("erc20", "0xdac", "USDT"), "USDT")).toBe(
      true,
    );
    expect(matchesAssetFilter(edge("erc20", "0xabc", "USDC"), "erc20")).toBe(
      true,
    );
    expect(matchesAssetFilter(edge("erc20", "0xdac", "USDT"), "erc20")).toBe(
      false,
    );
  });

  it("matches either asset leg of a bidirectional swap", () => {
    const swap = {
      ...edge("erc20", "0xabc", "USDC", "swap"),
      bidirectional: true,
      swapLegs: [
        {
          source: "a",
          target: "b",
          assetType: "native",
          asset: "ETH",
          assetSymbol: "ETH",
          totalAmount: "1",
          decimals: 18,
          kind: "transfer",
        },
        {
          source: "b",
          target: "a",
          assetType: "erc20",
          asset: "0xdac",
          assetSymbol: "USDT",
          totalAmount: "2",
          decimals: 6,
          kind: "swap",
        },
      ],
    } satisfies GraphEdgeModel;
    expect(matchesAssetFilter(swap, "ETH")).toBe(true);
    expect(matchesAssetFilter(swap, "USDT")).toBe(true);
  });
});

describe("GraphCanvas THORChain semantics", () => {
  it("labels every supported THORChain protocol action", () => {
    expect(thorchainEdgeLabel("router_inbound")).toBe("进入 THORChain Router");
    expect(thorchainEdgeLabel("vault_migration")).toBe("THORChain Vault 迁移");
    expect(thorchainEdgeLabel("protocol_outbound")).toBe("THORChain 协议出站");
    expect(thorchainEdgeLabel("cross_chain_swap")).toBe("THORChain 跨链兑换");
    expect(thorchainEdgeLabel("refund")).toBe("THORChain 退款");
  });
});
