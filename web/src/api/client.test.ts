import { afterEach, describe, expect, it, vi } from "vitest";
import { api, setBearerToken } from "./client";

describe("API authentication", () => {
  afterEach(() => { setBearerToken(""); vi.useRealTimers(); vi.unstubAllGlobals(); });

  it("sends a session-only Bearer token when configured", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify([]), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    setBearerToken("secret");
    await api.labels("ethereum", "0x0000000000000000000000000000000000000001");
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe("Bearer secret");
  });

  it("encodes the transaction hash route", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({}), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await api.transaction("ethereum", "0xabc");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/transactions/0xabc?chain=ethereum");
  });

  it("looks up the latest sync job by chain and address", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ jobId: "job-1" }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await api.latestSyncJob("ethereum", "0x0000000000000000000000000000000000000001");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/sync-jobs/latest?chain=ethereum&address=0x0000000000000000000000000000000000000001");
  });

  it("looks up the latest trace job by the complete trace query", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ id: "trace-1" }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
	await api.latestTraceJob({ chain: "ethereum", address: "0x0000000000000000000000000000000000000001", direction: "both", depth: 3, asset: "all" });
	expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/trace-jobs/latest?chain=ethereum&address=0x0000000000000000000000000000000000000001&direction=both&depth=3&asset=all");
  });

  it("recovers an active sync through the idempotent enqueue endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ jobId: "job-1" }), { status: 202 }));
    vi.stubGlobal("fetch", fetchMock);
    await api.createSync("ethereum", "0x0000000000000000000000000000000000000001");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/sync");
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ chain: "ethereum", address: "0x0000000000000000000000000000000000000001", neighborLimit: 0 });
  });

  it("creates and stops a propagation job through resource routes", async () => {
    const fetchMock = vi.fn().mockImplementation(() => Promise.resolve(new Response(JSON.stringify({ id: "propagation-1" }), { status: 202 })));
    vi.stubGlobal("fetch", fetchMock);
    const input = { chain: "ethereum" as const, targetAddress: "0x0000000000000000000000000000000000000001", direction: "both" as const, asset: "ETH" };
    await api.createPropagation(input);
    await api.stopPropagationJob("propagation-1");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/propagation-jobs");
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual(input);
    expect(fetchMock.mock.calls[1][0]).toBe("/api/v1/propagation-jobs/propagation-1/stop");
  });

  it("loads sync job progress in bounded batches and keeps successful rows", async () => {
    vi.useFakeTimers();
    const fetchMock = vi.fn((path: string) => Promise.resolve(new Response(
      path.endsWith("job-3") ? JSON.stringify({ error: { code: "rate_limited", message: "slow down", retryable: true } }) : JSON.stringify({ jobId: path.split("/").pop() }),
      { status: path.endsWith("job-3") ? 429 : 200 },
    )));
    vi.stubGlobal("fetch", fetchMock);
    const promise = api.syncJobs(Array.from({ length: 7 }, (_, index) => `job-${index + 1}`));
    await vi.advanceTimersByTimeAsync(1_200);
    const jobs = await promise;
    expect(jobs.map((job) => job.jobId)).toEqual(["job-1", "job-2", "job-4", "job-5", "job-6", "job-7"]);
    expect(fetchMock).toHaveBeenCalledTimes(7);
  });
});
