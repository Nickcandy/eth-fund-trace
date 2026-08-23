import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { SyncJob } from "../api/types";
import { BottomPanel } from "./BottomPanel";

describe("BottomPanel sync progress", () => {
  it("shows live API and persistence counters", async () => {
    const job: SyncJob = {
      jobId: "6a8a8c337fcbef52929d0d0b", chain: "ethereum", address: "0xd152f549545093347a162dce210e7293f1452150",
      status: "running", createdAt: "2026-08-23T05:59:15Z", safeHead: 25815903, totalAddresses: 1,
      completedAddresses: 0, processedAddresses: 0, cachedAddresses: 0, fetched: 0, retryable: false,
      progress: { currentAddress: "0xd152f549545093347a162dce210e7293f1452150", currentAction: "txlist", rangeStart: 6562352, rangeEnd: 25815903, currentPage: 42, pagesFetched: 142, recordsRead: 14200, recordsWritten: 9600, splitCount: 1 },
    };
    render(<BottomPanel facts={[]} onMore={() => undefined} syncJobs={[job]} bridges={[]} chain="ethereum" address={job.address} onLabel={async()=>undefined} onBridge={async()=>undefined}/>);
    await userEvent.click(screen.getByRole("button", { name: "任务进度" }));
    expect(screen.getByText("第 42 页")).toBeVisible();
    expect(screen.getByText("14,200 条")).toBeVisible();
    expect(screen.getByText("9,600 条")).toBeVisible();
    expect(screen.getByText("区间拆分")).toBeVisible();
  });
});
