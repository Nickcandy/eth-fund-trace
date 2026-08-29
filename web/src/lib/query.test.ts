import { describe, expect, it } from "vitest";
import {
  readTraceJobID,
  readTraceQuery,
  validateTraceQuery,
  writeTraceQuery,
} from "./query";

const valid = {
  chain: "ethereum" as const,
  address: "0x0000000000000000000000000000000000000001",
  direction: "both" as const,
  depth: 0,
  asset: "ETH",
};

describe("trace query", () => {
  it("rejects invalid addresses and non-ETH roots", () => {
    expect(validateTraceQuery({ ...valid, address: "bad" })).toContain("40 位");
    expect(validateTraceQuery({ ...valid, asset: "USDC" })).toContain("ETH");
  });

  it("round-trips shareable analysis parameters", () => {
    expect(readTraceQuery(writeTraceQuery(valid))).toEqual(valid);
  });

  it("uses automatic terminal tracing regardless of legacy depth parameters", () => {
    expect(readTraceQuery("?depth=5").depth).toBe(0);
  });

  it("round-trips the active trace job for refresh recovery", () => {
    const search = writeTraceQuery(valid, "6a8a8c307fcbef52929d0d09");
    expect(readTraceQuery(search)).toEqual(valid);
    expect(readTraceJobID(search)).toBe("6a8a8c307fcbef52929d0d09");
  });
});
