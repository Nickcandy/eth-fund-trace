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
      {
        chain: "ethereum",
        from: upstream,
        to: seed,
        assetType: "native",
        asset: "ETH",
        totalAmount: "100",
        transferCount: 1,
        kind: "transfer",
        depth: 1,
        path: [seed, upstream],
      },
      {
        chain: "ethereum",
        from: seed,
        to: downstream,
        assetType: "erc20",
        asset: "0x0000000000000000000000000000000000000010",
        symbol: "USDC",
        decimals: 6,
        tokenMetadataComplete: true,
        totalAmount: "500",
        transferCount: 2,
        kind: "transfer",
        depth: 1,
        path: [seed, downstream],
      },
    ],
    paths: [],
    dataThroughBlock: 3,
    dataThroughBlocks: { ethereum: 3 },
    dataStatus: "synced",
    labels: [],
    risk: {
      score: 0,
      level: "no_conclusion",
      evidence: [],
      ruleVersion: "risk-v1",
    },
    ruleVersion: "trace-v1",
  };
}

describe("buildGraphModel", () => {
  it("places upstream and downstream on opposite signed hops", () => {
    const model = buildGraphModel(result(), {
      chain: "ethereum",
      address: seed,
    });
    expect(model.nodes.find((node) => node.address === upstream)?.hop).toBe(-1);
    expect(model.nodes.find((node) => node.address === downstream)?.hop).toBe(
      1,
    );
  });

  it("uses the backend aggregate amount and transfer count", () => {
    const model = buildGraphModel(result(), {
      chain: "ethereum",
      address: seed,
    });
    const edge = model.edges.find(
      (candidate) => candidate.assetSymbol === "USDC",
    );
    expect(edge).toMatchObject({ count: 2, totalAmount: "500" });
  });

  it("uses token precision from the aggregate edge", () => {
    const value = result();
    const model = buildGraphModel(value, { chain: "ethereum", address: seed });
    const edge = model.edges.find(
      (candidate) => candidate.asset === value.edges[1].asset,
    );

    expect(edge).toMatchObject({ decimals: 6, assetSymbol: "USDC" });
  });

  it("preserves protocol roles and conversion evidence", () => {
    const value = result();
    value.nodes[1] = {
      ...value.nodes[1],
      addressType: "contract",
      protocol: "kyberswap",
      roles: ["kyberswap_executor"],
    };
    value.edges[0] = {
      ...value.edges[0],
      kind: "swap",
      conversionEvidence: [
        {
          txHash: "0xswap",
          protocol: "kyberswap",
          version: "rfq",
          status: "complete",
          liquidityProvider: downstream,
          tokenIn: "USDT",
          amountIn: "1000000",
          tokenOut: "ETH",
          amountOut: "1",
          evidence: ["internal ETH calls"],
        },
      ],
    };

    const model = buildGraphModel(value, { chain: "ethereum", address: seed });
    expect(model.nodes.find((node) => node.address === upstream)).toMatchObject(
      {
        addressType: "contract",
        protocol: "kyberswap",
        roles: ["kyberswap_executor"],
      },
    );
    expect(model.edges[0].conversionEvidence?.[0]).toMatchObject({
      txHash: "0xswap",
      liquidityProvider: downstream,
    });
  });

  it("combines a verified reverse swap pair into one bidirectional display edge", () => {
    const value = result();
    value.edges = [
      {
        chain: "ethereum",
        txHash: "0xswap",
        from: seed,
        to: downstream,
        assetType: "native",
        asset: "ETH",
        totalAmount: "100",
        transferCount: 1,
        kind: "transfer",
        depth: 1,
        path: [seed, downstream],
      },
      {
        chain: "ethereum",
        txHash: "0xswap",
        from: downstream,
        to: seed,
        assetType: "erc20",
        asset: "0x0000000000000000000000000000000000000010",
        symbol: "USDC",
        decimals: 6,
        tokenMetadataComplete: true,
        totalAmount: "250",
        transferCount: 1,
        kind: "swap",
        depth: 2,
        path: [seed, downstream, seed],
        conversionEvidence: [
          {
            txHash: "0xswap",
            protocol: "uniswap",
            version: "v3",
            status: "complete",
            tokenIn: "ETH",
            amountIn: "100",
            tokenOut: "USDC",
            amountOut: "250",
            evidence: ["verified logs"],
          },
        ],
      },
    ];

    const model = buildGraphModel(value, { chain: "ethereum", address: seed });
    expect(model.edges).toHaveLength(1);
    expect(model.edges[0]).toMatchObject({ bidirectional: true, kind: "swap" });
    expect(model.edges[0].swapLegs).toHaveLength(2);
  });

  it("combines a verified contract swap with a distinct recipient into one bidirectional display edge", () => {
    const value = result();
    const contract = upstream;
    value.edges = [
      {
        chain: "ethereum",
        txHash: "0xswap",
        from: seed,
        to: contract,
        assetType: "erc20",
        asset: "0x0000000000000000000000000000000000000011",
        symbol: "USDT",
        decimals: 6,
        tokenMetadataComplete: true,
        totalAmount: "100",
        transferCount: 1,
        kind: "transfer",
        depth: 1,
        path: [seed, contract],
      },
      {
        chain: "ethereum",
        txHash: "0xswap",
        from: contract,
        to: downstream,
        assetType: "native",
        asset: "ETH",
        totalAmount: "2",
        transferCount: 1,
        kind: "swap",
        depth: 2,
        path: [seed, contract, downstream],
        conversionEvidence: [
          {
            txHash: "0xswap",
            protocol: "kyberswap",
            version: "rfq",
            status: "complete",
            initiator: seed,
            recipient: downstream,
            tokenIn: "USDT",
            amountIn: "100",
            tokenOut: "ETH",
            amountOut: "2",
            evidence: ["verified receipt"],
          },
        ],
      },
    ];

    const model = buildGraphModel(value, { chain: "ethereum", address: seed });
    expect(model.edges).toHaveLength(1);
    expect(model.edges[0]).toMatchObject({
      source: `ethereum:${seed}`,
      target: `ethereum:${contract}`,
      bidirectional: true,
      kind: "swap",
      count: 1,
    });
    expect(model.edges[0].swapLegs).toEqual([
      expect.objectContaining({
        source: `ethereum:${seed}`,
        target: `ethereum:${contract}`,
        assetSymbol: "USDT",
      }),
      expect.objectContaining({
        source: `ethereum:${contract}`,
        target: `ethereum:${downstream}`,
        assetSymbol: "ETH",
      }),
    ]);
  });

  it("does not combine swap legs with different transaction hashes", () => {
    const value = result();
    value.edges[0] = {
      ...value.edges[0],
      txHash: "0xin",
      from: seed,
      to: downstream,
    };
    value.edges[1] = {
      ...value.edges[1],
      txHash: "0xout",
      from: downstream,
      to: seed,
      kind: "swap",
      conversionEvidence: [
        {
          txHash: "0xout",
          protocol: "uniswap",
          version: "v3",
          status: "complete",
          evidence: [],
        },
      ],
    };
    expect(
      buildGraphModel(value, { chain: "ethereum", address: seed }).edges,
    ).toHaveLength(2);
  });

  it("restores the THORChain vault role from a migration edge", () => {
    const value = result();
    value.edges[1] = {
      ...value.edges[1],
      kind: "thorchain_vault_migration",
      protocol: "thorchain",
      protocolAction: "vault_migration",
      protocolMemo: "MIGRATE:42",
    };
    expect(
      buildGraphModel(value, { chain: "ethereum", address: seed }).nodes.find(
        (node) => node.address === downstream,
      ),
    ).toMatchObject({ protocol: "thorchain", roles: ["thorchain_vault"] });
  });

  it("classifies transfer direction relative to the query center", () => {
    const model = buildGraphModel(result(), {
      chain: "ethereum",
      address: seed,
    });
    expect(
      model.edges.find((edge) => edge.source.endsWith(upstream))?.flow,
    ).toBe("inbound");
    expect(
      model.edges.find((edge) => edge.target.endsWith(downstream))?.flow,
    ).toBe("outbound");
  });
});
