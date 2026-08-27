import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { PropagationJob, PropagationResult } from "../api/types";
import type { GraphEdgeModel, GraphNodeModel } from "../graph/model";
import { DetailsPanel } from "./DetailsPanel";

afterEach(cleanup);

describe("DetailsPanel labels", () => {
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

    render(<DetailsPanel node={node} labels={[]} onLabel={onLabel} onClose={() => undefined} onFocus={() => undefined} />);
    await user.click(screen.getByRole("tab", { name: "标签" }));
    await user.type(screen.getByPlaceholderText("标签类型，如 exchange"), "hacker");
    await user.selectOptions(screen.getByLabelText("风险等级"), "high");
    await user.type(screen.getByPlaceholderText("证据，每行一条"), "case-1");
    await user.click(screen.getByRole("button", { name: "保存标签" }));

    await waitFor(() => expect(onLabel).toHaveBeenCalledWith({
      chain: "ethereum",
      address: node.address,
      type: "hacker",
      source: "manual",
      riskLevel: "high",
      confidence: 1,
      note: "",
      evidence: ["case-1"],
    }));
    expect(screen.getByText("标签已保存，风险重新评估已提交")).toBeVisible();
  });

  it("shows automatic target assessment without a manual start action", async () => {
    const node: GraphNodeModel = { id: "ethereum:0x1", chain: "ethereum", address: "0x0000000000000000000000000000000000000001", hop: 0, terminal: false, seed: true, risk: "normal", hotWallet: false, labelTypes: ["hacker"] };
    render(<DetailsPanel node={node} labels={[{ chain: "ethereum", address: node.address, type: "hacker", source: "manual", riskLevel: "high", confidence: 1 }]} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
    expect(screen.queryByRole("button", { name: "启动传播" })).toBeNull();
    expect(screen.getByText("等待 Trace 完成后自动评估")).toBeVisible();
  });

  it("distinguishes an unevaluated node from a zero score", () => {
    const node: GraphNodeModel = { id: "ethereum:0x2", chain: "ethereum", address: "0x0000000000000000000000000000000000000002", hop: 2, terminal: false, seed: false, risk: "normal", hotWallet: false, labelTypes: [] };
    render(<DetailsPanel node={node} labels={[]} propagationJob={propagationJob(propagationResult())} propagationResult={propagationResult()} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
    expect(screen.getByText("未纳入本次评估")).toBeVisible();
    expect(screen.getByText("--")).toBeVisible();
    expect(screen.queryByText("暂无关联结论")).toBeNull();
  });

  it("shows missing propagation data as unknown instead of zero", () => {
    const node: GraphNodeModel = { id: "ethereum:0x2", chain: "ethereum", address: "0x0000000000000000000000000000000000000002", hop: 2, terminal: false, seed: false, risk: "normal", hotWallet: false, labelTypes: [] };
    const result = propagationResult();
    result.missingAddresses = [`ethereum:${node.address}`];
    render(<DetailsPanel node={node} labels={[]} propagationJob={propagationJob(result)} propagationResult={result} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
    expect(screen.getByText("数据不足")).toBeVisible();
    expect(screen.getByText("--")).toBeVisible();
  });

  it("shows the selected transfer direction and endpoints", () => {
    const edge: GraphEdgeModel = {
      id: "edge-1", source: "ethereum:0x0000000000000000000000000000000000000002", target: "ethereum:0x0000000000000000000000000000000000000001",
      chain: "ethereum", asset: "ETH", assetSymbol: "ETH", sourceType: "aggregate", kind: "transfer", count: 2, totalAmount: "10", flow: "inbound", firstBlock: 100, firstTime: "2025-05-12T14:18:23Z", latestBlock: 123, latestTime: "2025-06-02T14:37:35Z",
    };
    render(<DetailsPanel edge={edge} labels={[]} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
    expect(screen.getByText("资金流入查询中心")).toBeVisible();
    expect(screen.getByText(/0x000000000000/)).toBeVisible();
    expect(screen.getByText(/区块范围/)).toBeVisible();
    expect(screen.getByText(/时间范围/)).toBeVisible();
    expect(screen.getByText(/不代表该金额来自查询中心/)).toBeVisible();
  });

  it("shows contract roles and Kyber swap evidence", () => {
	const node: GraphNodeModel = { id: "ethereum:0x1", chain: "ethereum", address: "0x0000000000000000000000000000000000000001", hop: 1, terminal: false, seed: false, risk: "normal", hotWallet: false, labelTypes: [], addressType: "contract", protocol: "kyberswap", roles: ["kyberswap_executor"] };
	const edge: GraphEdgeModel = {
	  id: "swap", source: "ethereum:0x1", target: "ethereum:0x2", chain: "ethereum", asset: "ETH", assetSymbol: "ETH", sourceType: "aggregate", kind: "swap", count: 1, totalAmount: "274823886000000000000", decimals: 18,
	  conversionEvidence: [{ txHash: "0xswap", protocol: "kyberswap", version: "rfq", status: "complete", liquidityProvider: "0x67336cec42645f55059eff241cb02ea5cc52ff86", tokenIn: "USDT", amountIn: "1000000000000", tokenOut: "ETH", amountOut: "274823886000000000000", evidence: ["internal ETH calls"] }],
	};
	const { rerender } = render(<DetailsPanel node={node} labels={[]} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
	expect(screen.getByText("KyberSwap Executor")).toBeVisible();
	rerender(<DetailsPanel edge={edge} labels={[]} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
	expect(screen.getByText(/kyberswap.*rfq/i)).toBeVisible();
	expect(screen.getByText("0xswap")).toBeVisible();
	expect(screen.getByText(/0x67336cec/)).toBeVisible();
	expect(screen.getByText("internal ETH calls")).toBeVisible();
  });
});

function propagationResult(): PropagationResult {
  return {
    status: "partial", score: 0, level: "no_evidence", directRisk: { present: false, score: 0, labels: [] }, nodes: [], associations: [], coverage: [], missingAddresses: [], candidateCoverage: 1,
    ruleVersion: "risk-association-v2", propagationVersion: "propagation-v4", dataThroughBlock: 100, visitedNodes: 1, edgeCount: 0, truncated: false,
  };
}

function propagationJob(result: PropagationResult): PropagationJob {
  return {
    id: "job-1", chain: "ethereum", targetAddress: "0x0000000000000000000000000000000000000001", asset: "ETH", direction: "both", status: "partial",
    maxHops: 3, maxNodes: 100, maxEdges: 100, maxAssetChannels: 100, perNodeCandidateCap: 10, maxPathsPerTarget: 3, currentHop: 3, visitedNodes: result.visitedNodes, edgeCount: result.edgeCount,
    dataThroughBlock: result.dataThroughBlock, ruleVersion: result.ruleVersion, propagationVersion: result.propagationVersion, truncated: false, retryCount: 0, createdAt: "2026-08-26T00:00:00Z", result, retryable: false,
  };
}
