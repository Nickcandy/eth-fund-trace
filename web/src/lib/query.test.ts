import { describe, expect, it } from "vitest";
import { readTraceJobID, readTraceQuery, validateTraceQuery, writeTraceQuery } from "./query";

const valid = { chain: "ethereum" as const, address: "0x0000000000000000000000000000000000000001", direction: "both" as const, depth: 3, asset: "ETH" };

describe("trace query", () => {
  it("rejects invalid addresses and non-ETH roots", () => {
    expect(validateTraceQuery({ ...valid, address: "bad" })).toContain("40 位");
    expect(validateTraceQuery({ ...valid, asset: "USDC" })).toContain("ETH");
  });

  it("round-trips shareable analysis parameters", () => {
    expect(readTraceQuery(writeTraceQuery(valid))).toEqual(valid);
  });

  it("round-trips the active trace job for refresh recovery", () => {
    const search = writeTraceQuery(valid, "6a8a8c307fcbef52929d0d09");
    expect(readTraceQuery(search)).toEqual(valid);
    expect(readTraceJobID(search)).toBe("6a8a8c307fcbef52929d0d09");
  });
});
