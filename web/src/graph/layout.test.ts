import { describe, expect, it } from "vitest";
import type { GraphModel } from "./model";
import { layoutGraph, NODE_HEIGHT } from "./layout";

describe("layoutGraph", () => {
  it("keeps semantic hops in fixed columns without overlapping nodes", async () => {
    const model: GraphModel = {
      nodes: [
        { id: "ethereum:seed", chain: "ethereum", address: "seed", hop: 0, terminal: false, seed: true, risk: "normal", hotWallet: false, labelTypes: [] },
        { id: "ethereum:up-1", chain: "ethereum", address: "up-1", hop: -1, terminal: false, seed: false, risk: "normal", hotWallet: false, labelTypes: [] },
        { id: "ethereum:up-2", chain: "ethereum", address: "up-2", hop: -2, terminal: false, seed: false, risk: "normal", hotWallet: false, labelTypes: [] },
        { id: "ethereum:up-peer", chain: "ethereum", address: "up-peer", hop: -1, terminal: false, seed: false, risk: "normal", hotWallet: false, labelTypes: [] },
        { id: "ethereum:down-1", chain: "ethereum", address: "down-1", hop: 1, terminal: false, seed: false, risk: "normal", hotWallet: false, labelTypes: [] },
      ],
      edges: [
        { id: "a", source: "ethereum:up-2", target: "ethereum:up-1", chain: "ethereum", asset: "ETH", assetSymbol: "ETH", sourceType: "aggregate", kind: "transfer", count: 1, totalAmount: "1" },
        { id: "b", source: "ethereum:up-peer", target: "ethereum:seed", chain: "ethereum", asset: "ETH", assetSymbol: "ETH", sourceType: "aggregate", kind: "transfer", count: 1, totalAmount: "1" },
        { id: "c", source: "ethereum:seed", target: "ethereum:down-1", chain: "ethereum", asset: "ETH", assetSymbol: "ETH", sourceType: "aggregate", kind: "transfer", count: 1, totalAmount: "1" },
      ],
    };

    const positions = await layoutGraph(model);
    expect(positions.get("ethereum:up-2")?.x).toBeLessThan(positions.get("ethereum:up-1")?.x ?? 0);
    expect(positions.get("ethereum:up-1")?.x).toBeLessThan(positions.get("ethereum:seed")?.x ?? 0);
    expect(positions.get("ethereum:down-1")?.x).toBeGreaterThan(positions.get("ethereum:seed")?.x ?? 0);
    expect(positions.get("ethereum:up-peer")?.x).toBe(positions.get("ethereum:up-1")?.x);
    expect(Math.abs((positions.get("ethereum:up-peer")?.y ?? 0) - (positions.get("ethereum:up-1")?.y ?? 0))).toBeGreaterThanOrEqual(NODE_HEIGHT);
  });
});
