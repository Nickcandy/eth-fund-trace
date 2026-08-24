import { describe, expect, it } from "vitest";
import { describeTraceJob, mergeSyncJobs } from "./job";

describe("describeTraceJob", () => {
  it("maps waiting sync progress to a user-visible state", () => {
    expect(describeTraceJob({ status: "waiting_sync", currentDepth: 0, visitedNodes: 1, edgeCount: 0 })).toEqual({ tone: "pending", label: "等待数据同步", detail: "已发现 1 个节点" });
  });

  it("keeps partial results distinct from success", () => {
    expect(describeTraceJob({ status: "partial", currentDepth: 3, visitedNodes: 12, edgeCount: 20 }).tone).toBe("warning");
  });
});

describe("mergeSyncJobs", () => {
  it("keeps a recovered address job and de-duplicates a linked copy", () => {
    const linked = { jobId: "same", status: "running" } as import("./types").SyncJob;
    const recovered = { ...linked, progress: { pagesFetched: 12 } } as import("./types").SyncJob;
    expect(mergeSyncJobs([linked], recovered)).toEqual([recovered]);
    expect(mergeSyncJobs([], recovered)).toEqual([recovered]);
  });

  it("updates a recovered job without changing its position", () => {
    const first = { jobId: "first", status: "running" } as import("./types").SyncJob;
    const second = { jobId: "second", status: "queued" } as import("./types").SyncJob;
    const updated = { ...first, status: "succeeded" } as import("./types").SyncJob;
    expect(mergeSyncJobs([first, second], updated).map((job) => job.jobId)).toEqual(["first", "second"]);
  });
});
