import { describe, expect, it } from "vitest";
import { readTraceQuery, validateTraceQuery, writeTraceQuery } from "./query";

const valid = { chain: "ethereum" as const, address: "0x0000000000000000000000000000000000000001", direction: "both" as const, depth: 3, topN: 10, asset: "all" };

describe("trace query", () => {
  it("rejects invalid address and token contracts", () => {
    expect(validateTraceQuery({ ...valid, address: "bad" })).toContain("40 位");
    expect(validateTraceQuery({ ...valid, asset: "USDC" })).toContain("Token");
  });

  it("round-trips shareable analysis parameters", () => {
    expect(readTraceQuery(writeTraceQuery(valid))).toEqual(valid);
  });
});
