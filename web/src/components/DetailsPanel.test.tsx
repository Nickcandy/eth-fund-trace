import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { GraphEdgeModel, GraphNodeModel } from "../graph/model";
import { DetailsPanel } from "./DetailsPanel";

afterEach(cleanup);

describe("DetailsPanel labels", () => {
	 it("shows contract identity for contract nodes", () => {
		const node: GraphNodeModel = {
			id: "ethereum:0x1",
			chain: "ethereum",
			address: "0x0000000000000000000000000000000000000001",
			hop: 1,
			terminal: true,
			seed: false,
			risk: "normal",
			hotWallet: false,
			labelTypes: [],
			addressType: "contract",
		};
		render(<DetailsPanel node={node} labels={[]} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
		expect(screen.getByText("合约")).toBeVisible();
	});

  it("shows the selected address native balance at an explicit block", () => {
    const node: GraphNodeModel = {
      id: "ethereum:0x1",
      chain: "ethereum",
      address: "0x0000000000000000000000000000000000000001",
      hop: 0,
      terminal: false,
      seed: true,
      risk: "normal",
      hotWallet: false,
      labelTypes: [],
    };
    render(
      <DetailsPanel
        node={node}
        balance={{
          chain: "ethereum",
          chainId: 1,
          address: node.address,
          asset: "ETH",
          amount: "1234567890000000000",
          decimals: 18,
          blockNumber: 123,
          fetchedAt: "2026-08-29T00:00:00Z",
        }}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("1.2346 ETH")).toBeVisible();
    expect(screen.getByText("区块 123")).toBeVisible();
  });

  it("shows the Relay Solver identity", () => {
    const node: GraphNodeModel = {
      id: "ethereum:relay",
      chain: "ethereum",
      address: "0xf70da97812cb96acdf810712aa562db8dfa3dbef",
      hop: 0,
      terminal: false,
      seed: true,
      risk: "normal",
      hotWallet: false,
      labelTypes: [],
      addressType: "eoa",
      protocol: "relay",
      roles: ["solver"],
    };
    render(
      <DetailsPanel
        node={node}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("Relay Solver")).toBeVisible();
    expect(screen.getByText("relay · eoa")).toBeVisible();
  });
  it("creates a label for the selected node from the labels tab", async () => {
    const user = userEvent.setup();
    const onLabel = vi.fn().mockResolvedValue(undefined);
    const node: GraphNodeModel = {
      id: "ethereum:0x0000000000000000000000000000000000000002",
      chain: "ethereum",
      address: "0x0000000000000000000000000000000000000002",
      hop: 1,
      terminal: false,
      seed: false,
      risk: "normal",
      hotWallet: false,
      labelTypes: [],
    };

    render(
      <DetailsPanel
        node={node}
        labels={[]}
        onLabel={onLabel}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    await user.click(screen.getByRole("tab", { name: "标签" }));
    await user.type(
      screen.getByPlaceholderText("标签类型，如 exchange"),
      "hacker",
    );
    await user.selectOptions(screen.getByLabelText("风险等级"), "high");
    await user.type(screen.getByPlaceholderText("证据，每行一条"), "case-1");
    await user.click(screen.getByRole("button", { name: "保存标签" }));

    await waitFor(() =>
      expect(onLabel).toHaveBeenCalledWith({
        chain: "ethereum",
        address: node.address,
        type: "hacker",
        source: "manual",
        riskLevel: "high",
        confidence: 1,
        note: "",
        evidence: ["case-1"],
      }),
    );
    expect(screen.getByText("标签已保存，风险重新评估已提交")).toBeVisible();
  });

  it("uses only direct labels for the risk summary", async () => {
    const node: GraphNodeModel = {
      id: "ethereum:0x1",
      chain: "ethereum",
      address: "0x0000000000000000000000000000000000000001",
      hop: 0,
      terminal: false,
      seed: true,
      risk: "normal",
      hotWallet: false,
      labelTypes: ["hacker"],
    };
    render(
      <DetailsPanel
        node={node}
        labels={[
          {
            chain: "ethereum",
            address: node.address,
            type: "hacker",
            source: "manual",
            riskLevel: "high",
            confidence: 1,
          },
        ]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("确定性风险标签")).toBeVisible();
    expect(screen.queryByText(/传播/)).not.toBeInTheDocument();
  });

  it("shows the selected transfer direction and endpoints", () => {
    const edge: GraphEdgeModel = {
      id: "edge-1",
      source: "ethereum:0x0000000000000000000000000000000000000002",
      target: "ethereum:0x0000000000000000000000000000000000000001",
      chain: "ethereum",
      asset: "ETH",
      assetSymbol: "ETH",
      sourceType: "aggregate",
      kind: "transfer",
      count: 2,
      totalAmount: "10",
      flow: "inbound",
      firstBlock: 100,
      firstTime: "2025-05-12T14:18:23Z",
      latestBlock: 123,
      latestTime: "2025-06-02T14:37:35Z",
    };
    render(
      <DetailsPanel
        edge={edge}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("资金流入查询中心")).toBeVisible();
    expect(screen.getByText(/0x000000000000/)).toBeVisible();
    expect(screen.getByText(/区块范围/)).toBeVisible();
    expect(screen.getByText(/时间范围/)).toBeVisible();
    expect(screen.getByText(/不代表该金额来自查询中心/)).toBeVisible();
  });

  it("does not present an unavailable THORChain verification as confirmed", () => {
    const edge: GraphEdgeModel = {
      id: "thorchain-pending",
      source: "ethereum:0x1",
      target: "ethereum:0x2",
      chain: "ethereum",
      asset: "ETH",
      assetSymbol: "ETH",
      sourceType: "aggregate",
      kind: "transfer",
      count: 1,
      totalAmount: "100",
      protocol: "thorchain",
      protocolAction: "router_inbound",
      protocolMemo: "=:b:bc1ptest",
      conversionStatus: "partial",
    };
    render(
      <DetailsPanel
        edge={edge}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("验证暂不可用")).toBeVisible();
    expect(screen.queryByText("已确认")).not.toBeInTheDocument();
  });

  it("shows contract roles and Kyber swap evidence", () => {
    const node: GraphNodeModel = {
      id: "ethereum:0x1",
      chain: "ethereum",
      address: "0x0000000000000000000000000000000000000001",
      hop: 1,
      terminal: false,
      seed: false,
      risk: "normal",
      hotWallet: false,
      labelTypes: [],
      addressType: "contract",
      protocol: "kyberswap",
      roles: ["kyberswap_executor"],
    };
    const edge: GraphEdgeModel = {
      id: "swap",
      source: "ethereum:0x1",
      target: "ethereum:0x2",
      chain: "ethereum",
      asset: "ETH",
      assetSymbol: "ETH",
      sourceType: "aggregate",
      kind: "swap",
      count: 1,
      totalAmount: "274823886000000000000",
      decimals: 18,
      conversionEvidence: [
        {
          txHash: "0xswap",
          protocol: "kyberswap",
          version: "rfq",
          status: "complete",
          liquidityProvider: "0x67336cec42645f55059eff241cb02ea5cc52ff86",
          tokenIn: "USDT",
          amountIn: "1000000000000",
          tokenOut: "ETH",
          amountOut: "274823886000000000000",
          evidence: ["internal ETH calls"],
        },
      ],
    };
    const { rerender } = render(
      <DetailsPanel
        node={node}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("KyberSwap Executor")).toBeVisible();
    rerender(
      <DetailsPanel
        edge={edge}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText(/kyberswap.*rfq/i)).toBeVisible();
    expect(screen.getByText("0xswap")).toBeVisible();
    expect(screen.getByText(/0x67336cec/)).toBeVisible();
    expect(screen.getByText("internal ETH calls")).toBeVisible();
  });

  it("shows both legs of a combined swap and THORChain vault roles", () => {
    const node: GraphNodeModel = {
      id: "ethereum:0x1",
      chain: "ethereum",
      address: "0x0000000000000000000000000000000000000001",
      hop: 1,
      terminal: false,
      seed: false,
      risk: "normal",
      hotWallet: false,
      labelTypes: [],
      addressType: "eoa",
      protocol: "thorchain",
      roles: ["thorchain_vault"],
    };
    const edge: GraphEdgeModel = {
      id: "swap",
      source: "ethereum:0x1",
      target: "ethereum:0x2",
      chain: "ethereum",
      asset: "ETH",
      assetSymbol: "ETH",
      sourceType: "aggregate",
      kind: "swap",
      count: 1,
      totalAmount: "1000000000000000000",
      bidirectional: true,
      swapLegs: [
        {
          source: "ethereum:0x1",
          target: "ethereum:0x2",
          assetType: "eth",
          asset: "ETH",
          assetSymbol: "ETH",
          totalAmount: "1000000000000000000",
          decimals: 18,
          kind: "transfer",
        },
        {
          source: "ethereum:0x2",
          target: "ethereum:0x1",
          assetType: "erc20",
          asset: "0xdac",
          assetSymbol: "USDT",
          totalAmount: "2500000",
          decimals: 6,
          kind: "swap",
        },
      ],
    };
    const { rerender } = render(
      <DetailsPanel
        node={node}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("THORChain Vault")).toBeVisible();
    rerender(
      <DetailsPanel
        edge={edge}
        labels={[]}
        onLabel={vi.fn()}
        onClose={() => undefined}
        onFocus={() => undefined}
      />,
    );
    expect(screen.getByText("1 笔已确认 Swap")).toBeVisible();
    expect(screen.getByText(/1 ETH/)).toBeVisible();
    expect(screen.getByText(/2.5 USDT/)).toBeVisible();
    expect(screen.getByText("资产双向交换")).toBeVisible();
  });
});
