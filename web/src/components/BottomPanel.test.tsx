import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import type { SyncJob, TraceResult } from "../api/types";
import { BottomPanel } from "./BottomPanel";

afterEach(cleanup);

describe("BottomPanel", () => {
  it("keeps only trace-v1 operational tabs", () => {
    render(<BottomPanel facts={[]} onMore={() => undefined} syncJobs={[]} />);
    expect(screen.getByRole("button", { name: "事实边0" })).toBeVisible();
    expect(screen.getByRole("button", { name: "资金对账" })).toBeVisible();
    expect(screen.getByRole("button", { name: "任务进度" })).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /桥接|写入证据/ }),
    ).not.toBeInTheDocument();
  });

  it("shows money transfers, FIFO states, and reconciliation", async () => {
    const user = userEvent.setup();
    const result = {
      nodes: [],
      edges: [],
      dataThroughBlock: 2,
      dataStatus: "partial",
      ruleVersion: "trace-v1",
      risk: {
        score: 0,
        level: "no_conclusion",
        ruleVersion: "risk-v1",
      },
      reconciliation: "partial",
      ledgers: [
        {
          address: "0x0000000000000000000000000000000000000001",
          asset: "ETH",
          openingAmount: "5",
          incomingAmount: "10",
          outgoingAmount: "15",
          explainedAmount: "10",
          unexplainedAmount: "5",
          status: "partial",
        },
      ],
      moneyTransfers: [
        {
          chain: "ethereum",
          from: "0x0000000000000000000000000000000000000001",
          to: "0x0000000000000000000000000000000000000002",
          asset: "ETH",
          amount: "15",
          txHash: "0xabc",
          kind: "transfer",
          blockNumber: 2,
          inferred: true,
        },
      ],
      moneyStates: [
        {
          chain: "ethereum",
          address: "0x0000000000000000000000000000000000000001",
          direction: "out",
          assetType: "eth",
          asset: "ETH",
          amount: "15",
          remainingAmount: "5",
          entryTxHash: "0xabc",
          entryBlock: 2,
          path: [],
          inferred: true,
        },
      ],
    } satisfies TraceResult;
    render(
      <BottomPanel
        facts={[]}
        traceResult={result}
        onMore={() => undefined}
        syncJobs={[]}
      />,
    );
    await user.click(screen.getByRole("button", { name: "资金对账" }));
    expect(screen.getByText("对账状态：部分")).toBeVisible();
    expect(screen.getByText("未解释 5")).toBeVisible();
    expect(screen.getAllByText("FIFO 推断")).toHaveLength(2);
  });

  it("shows the configured record limit for a partial sync", async () => {
    const user = userEvent.setup();
    const job = {
      jobId: "limited",
      chain: "ethereum",
      address: "0x0000000000000000000000000000000000000001",
      status: "partial",
      createdAt: "2026-08-27T00:00:00Z",
      safeHead: 20,
      totalAddresses: 1,
      completedAddresses: 1,
      processedAddresses: 1,
      cachedAddresses: 0,
      fetched: 150000,
      retryable: false,
      maxRecordsPerAction: 50000,
      truncatedActions: ["tokentx"],
    } as SyncJob;
    render(
      <BottomPanel facts={[]} onMore={() => undefined} syncJobs={[job]} />,
    );
    await user.click(screen.getByRole("button", { name: "任务进度" }));
    expect(screen.getByText(/每类最多 50,000 条/)).toBeVisible();
  });
});
