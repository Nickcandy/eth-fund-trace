import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import type { SyncJob, TraceJob, TraceResult } from "../api/types";
import { BottomPanel } from "./BottomPanel";

afterEach(cleanup);

describe("BottomPanel sync progress", () => {
	it("shows money transfers, FIFO states, and reconciliation", async () => {
		const user = userEvent.setup();
		const result = {
			nodes: [], edges: [], dataThroughBlock: 2, dataStatus: "partial", ruleVersion: "trace-v1",
			risk: { score: 0, level: "no_conclusion", ruleVersion: "risk-v1", propagationVersion: "propagation-v1" },
			reconciliation: "partial",
			ledgers: [{ address: "0x0000000000000000000000000000000000000001", asset: "ETH", openingAmount: "5", incomingAmount: "10", outgoingAmount: "15", explainedAmount: "10", unexplainedAmount: "5", status: "partial" }],
			moneyTransfers: [{ chain: "ethereum", from: "0x0000000000000000000000000000000000000001", to: "0x0000000000000000000000000000000000000002", asset: "ETH", amount: "15", txHash: "0xabc", kind: "transfer", blockNumber: 2, inferred: true }],
			moneyStates: [{ chain: "ethereum", address: "0x0000000000000000000000000000000000000001", direction: "out", assetType: "eth", asset: "ETH", amount: "15", remainingAmount: "5", entryTxHash: "0xabc", entryBlock: 2, path: [], inferred: true }],
		} satisfies TraceResult;
		render(<BottomPanel facts={[]} traceResult={result} onMore={() => undefined} syncJobs={[]} bridges={[]} chain="ethereum" address={result.ledgers![0].address} onBridge={async()=>undefined}/>);
		await user.click(screen.getByRole("button", { name: "资金对账" }));
		expect(screen.getByText("对账状态：部分")).toBeVisible();
		expect(screen.getByText("未解释 5")).toBeVisible();
		expect(screen.getAllByText("FIFO 推断")).toHaveLength(2);
	});

	 it("keeps manual labels out of the bottom write tab", async () => {
		const user = userEvent.setup();
		render(<BottomPanel facts={[]} onMore={() => undefined} syncJobs={[]} bridges={[]} chain="ethereum" address="0x0000000000000000000000000000000000000001" onBridge={async()=>undefined}/>);
		await user.click(screen.getByRole("button", { name: "写入证据" }));
		expect(screen.queryByText("添加确定性标签")).not.toBeInTheDocument();
		expect(screen.getByText("提交确认式桥接")).toBeVisible();
	 });

  it("opens the jobs tab for a running sync and shows live counters", async () => {
    const job: SyncJob = {
      jobId: "6a8a8c337fcbef52929d0d0b", chain: "ethereum", address: "0xd152f549545093347a162dce210e7293f1452150",
      status: "running", createdAt: "2026-08-23T05:59:15Z", safeHead: 25815903, totalAddresses: 1,
      completedAddresses: 0, processedAddresses: 0, cachedAddresses: 0, fetched: 0, retryable: false,
      progress: { currentAddress: "0xd152f549545093347a162dce210e7293f1452150", currentAction: "txlist", rangeStart: 6562352, rangeEnd: 25815903, currentPage: 42, pagesFetched: 142, recordsRead: 14200, recordsWritten: 9600, splitCount: 1 },
    };
    render(<BottomPanel facts={[]} onMore={() => undefined} syncJobs={[job]} bridges={[]} chain="ethereum" address={job.address} onBridge={async()=>undefined}/>);
    expect(screen.getByText("当前区间第 42 页")).toBeVisible();
    expect(screen.getByText("14,200 条")).toBeVisible();
    expect(screen.getByText("9,600 条")).toBeVisible();
    expect(screen.getByText("区间拆分")).toBeVisible();
  });

  it("shows the configured record limit for a partial sync", async () => {
	const user = userEvent.setup();
	const job = {
		jobId: "limited", chain: "ethereum", address: "0x0000000000000000000000000000000000000001",
		status: "partial", createdAt: "2026-08-27T00:00:00Z", safeHead: 20, totalAddresses: 1,
		completedAddresses: 1, processedAddresses: 1, cachedAddresses: 0, fetched: 300000, retryable: false,
		maxRecordsPerAction: 100000, truncatedActions: ["txlist", "txlistinternal", "tokentx"],
	} as SyncJob;
	render(<BottomPanel facts={[]} onMore={() => undefined} syncJobs={[job]} bridges={[]} chain="ethereum" address={job.address} onBridge={async()=>undefined}/>);
	await user.click(screen.getByRole("button", { name: "任务进度" }));
	expect(screen.getByText(/每类最多 100,000 条/)).toBeVisible();
  });

  it("shows stable task labels, seed context, and only the current sync step", () => {
    const trace = {
      id: "6a8c8c66e845799c12235450", seedAddress: "0x87aab7bac1308faf2a0d59da26b8379e18b26355", chain: "ethereum",
      direction: "both", depth: 3, asset: "all", status: "waiting_sync", createdAt: "2026-08-24T18:24:38Z",
      currentDepth: 0, visitedNodes: 0, edgeCount: 0, dataThroughBlock: 0, ruleVersion: "trace-v1", retryable: false,
    } as TraceJob;
    const done = { jobId: "done", address: trace.seedAddress, status: "succeeded", completedAddresses: 1, processedAddresses: 1 } as SyncJob;
    const active = { jobId: "active", address: "0xd2674da94285660c9b2353131bef2d8211369a4b", status: "running", completedAddresses: 0, processedAddresses: 0, progress: { pagesFetched: 4, recordsRead: 3000, recordsWritten: 2000, splitCount: 0 } } as SyncJob;
    render(<BottomPanel facts={[]} onMore={() => undefined} traceJob={trace} syncJobs={[done, active]} bridges={[]} chain="ethereum" address={trace.seedAddress} onBridge={async()=>undefined}/>);
    expect(screen.getByText("追踪任务 1")).toBeVisible();
    expect(screen.getByText("同步步骤 2")).toBeVisible();
    expect(screen.getByText("1 个地址")).toBeVisible();
    expect(screen.queryByText("6a8c8c66e845799c12235450")).not.toBeInTheDocument();
    expect(screen.queryByText("同步步骤 1")).not.toBeInTheDocument();
  });

  it("shows the live seed and neighbor queue without hiding completed addresses", () => {
    const seed = "0x87aab7bac1308faf2a0d59da26b8379e18b26355";
    const neighbor = "0xd2674da94285660c9b2353131bef2d8211369a4b";
    const trace = { id: "trace", seedAddress: seed, chain: "ethereum", direction: "both", depth: 3, asset: "all", status: "waiting_sync", createdAt: "2026-08-24T18:24:38Z", currentDepth: 0, visitedNodes: 0, edgeCount: 0, dataThroughBlock: 0, ruleVersion: "trace-v1", retryable: false } as TraceJob;
    const done = { jobId: "seed", address: seed, status: "succeeded", completedAddresses: 1, processedAddresses: 1, fetched: 12 } as SyncJob;
    const active = { jobId: "neighbor", address: neighbor, status: "running", completedAddresses: 0, processedAddresses: 0, progress: { currentAddress: neighbor, currentAction: "txlistinternal", currentPage: 7, pagesFetched: 6, recordsRead: 6000, recordsWritten: 4100, splitCount: 1 } } as SyncJob;
    render(<BottomPanel facts={[]} onMore={() => undefined} traceJob={trace} syncJobs={[done, active]} bridges={[]} chain="ethereum" address={seed} onBridge={async()=>undefined}/>);
    expect(screen.getByText("1 个邻居")).toBeVisible();
    expect(screen.getByText("1 / 2 已处理")).toBeVisible();
    expect(screen.getAllByText(seed).length).toBeGreaterThan(0);
    expect(screen.getAllByText(neighbor).length).toBeGreaterThan(0);
    expect(screen.getByText("内部 ETH · 累计 6 页 / 6,000 条")).toBeVisible();
  });
});
