import { describe, expect, it } from "vitest";
import { edgeLabelVisible, labelsVisibleByDefault, matchesAssetFilter } from "./GraphCanvas";
import type { GraphEdgeModel } from "../graph/model";

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
});
