import { describe, expect, it } from "vitest";
import { branchNodeIDs, edgeLabelVisible, expansionMode, expansionPathKeys, labelsVisibleByDefault, matchesAssetFilter } from "./GraphCanvas";
import type { GraphEdgeModel, GraphNodeModel } from "../graph/model";

describe("GraphCanvas density defaults", () => {
  it("shows labels for small graphs and hides them for dense graphs", () => {
    expect(labelsVisibleByDefault(8)).toBe(true);
    expect(labelsVisibleByDefault(74)).toBe(false);
  });

  it("keeps direct seed edges labeled in dense graphs", () => {
    expect(edgeLabelVisible(false, false, true)).toBe(true);
    expect(edgeLabelVisible(false, false, false)).toBe(false);
    expect(edgeLabelVisible(false, true, false)).toBe(true);
  });
});

describe("GraphCanvas branch expansion", () => {
  const node = (id: string, hop: number): GraphNodeModel => ({ id, hop, chain: "ethereum", address: id, terminal: false, seed: hop === 0, risk: "normal", hotWallet: false, labelTypes: [] });
  const edge = (source: string, target: string): GraphEdgeModel => ({ id: `${source}-${target}`, source, target, chain: "ethereum", asset: "ETH", assetSymbol: "ETH", sourceType: "aggregate", kind: "transfer", count: 1, totalAmount: "1" });
  const nodes = [node("seed", 0), node("middle", 1), node("right", 2), node("left", -1)];
  const edges = [edge("seed", "middle"), edge("middle", "right"), edge("left", "middle")];

  it("returns nodes extending on each side independently", () => {
    expect(branchNodeIDs("middle", "right", nodes, edges)).toEqual(["right"]);
    expect(branchNodeIDs("middle", "left", nodes, edges)).toEqual(["seed", "left"]);
  });

  it("returns no expansion target for the rightmost node", () => {
    expect(branchNodeIDs("right", "right", nodes, edges)).toEqual([]);
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
    expect(expansionPathKeys("right", "right", nodes, edges)).toEqual(["right:right", "middle:right", "seed:right"]);
    expect(expansionPathKeys("left", "left", nodes, [...edges, edge("left", "seed")])).toEqual(["left:left", "seed:left"]);
  });
});

describe("GraphCanvas asset filters", () => {
  const edge = (assetType: string, asset: string, assetSymbol: string, kind = "transfer"): GraphEdgeModel => ({ id: "1", source: "a", target: "b", chain: "ethereum", assetType, asset, assetSymbol, sourceType: "aggregate", kind, count: 1, totalAmount: "1", decimals: 18 });

  it("separates ETH, USDT and other ERC-20 edges", () => {
    expect(matchesAssetFilter(edge("native", "ETH", "ETH"), "ETH")).toBe(true);
    expect(matchesAssetFilter(edge("erc20", "0xdac", "USDT"), "USDT")).toBe(true);
    expect(matchesAssetFilter(edge("erc20", "0xabc", "USDC"), "erc20")).toBe(true);
    expect(matchesAssetFilter(edge("erc20", "0xdac", "USDT"), "erc20")).toBe(false);
  });

  it("hides bridge edges outside the all-assets view", () => {
    expect(matchesAssetFilter(edge("native", "ETH", "ETH", "bridge"), "ETH")).toBe(false);
    expect(matchesAssetFilter(edge("native", "ETH", "ETH", "bridge"), "all")).toBe(true);
  });

  it("matches either asset leg of a bidirectional swap", () => {
    const swap = { ...edge("erc20", "0xabc", "USDC", "swap"), bidirectional: true, swapLegs: [
      { source: "a", target: "b", assetType: "native", asset: "ETH", assetSymbol: "ETH", totalAmount: "1", decimals: 18, kind: "transfer" },
      { source: "b", target: "a", assetType: "erc20", asset: "0xdac", assetSymbol: "USDT", totalAmount: "2", decimals: 6, kind: "swap" },
    ] } satisfies GraphEdgeModel;
    expect(matchesAssetFilter(swap, "ETH")).toBe(true);
    expect(matchesAssetFilter(swap, "USDT")).toBe(true);
  });
});
