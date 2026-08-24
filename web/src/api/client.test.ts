import { afterEach, describe, expect, it, vi } from "vitest";
import { api, setBearerToken } from "./client";

describe("API authentication", () => {
  afterEach(() => { setBearerToken(""); vi.unstubAllGlobals(); });

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
    await api.latestTraceJob({ chain: "ethereum", address: "0x0000000000000000000000000000000000000001", direction: "both", depth: 3, topN: 10, asset: "all" });
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/trace-jobs/latest?chain=ethereum&address=0x0000000000000000000000000000000000000001&direction=both&depth=3&topN=10&asset=all");
  });

  it("recovers an active sync through the idempotent enqueue endpoint", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ jobId: "job-1" }), { status: 202 }));
    vi.stubGlobal("fetch", fetchMock);
    await api.createSync("ethereum", "0x0000000000000000000000000000000000000001");
    expect(fetchMock.mock.calls[0][0]).toBe("/api/v1/sync");
    expect(JSON.parse(fetchMock.mock.calls[0][1].body)).toEqual({ chain: "ethereum", address: "0x0000000000000000000000000000000000000001", neighborLimit: 0 });
  });
});
