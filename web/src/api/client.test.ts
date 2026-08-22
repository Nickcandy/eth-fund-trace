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
});
