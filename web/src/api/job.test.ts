import { describe, expect, it } from "vitest";
import { describeTraceJob } from "./job";

describe("describeTraceJob", () => {
  it("maps waiting sync progress to a user-visible state", () => {
    expect(describeTraceJob({ status: "waiting_sync", currentDepth: 0, visitedNodes: 1, edgeCount: 0 })).toEqual({ tone: "pending", label: "等待数据同步", detail: "已发现 1 个节点" });
  });

  it("keeps partial results distinct from success", () => {
    expect(describeTraceJob({ status: "partial", currentDepth: 3, visitedNodes: 12, edgeCount: 20 }).tone).toBe("warning");
  });
});
