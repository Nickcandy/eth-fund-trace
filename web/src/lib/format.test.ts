import { describe, expect, it } from "vitest";
import { displayDecimals, formatChainAmount } from "./format";

describe("formatChainAmount", () => {
  it("formats a large integer as a compact human-readable amount", () => {
    expect(formatChainAmount("123456789012345678901234", 18)).toBe("123,456.789012");
  });

  it("compacts the raw integer when token precision is unknown", () => {
    expect(formatChainAmount("12345678901234567890")).toBe("1.23456e+19");
  });

  it("keeps human-readable precision without rendering long fractional tails", () => {
    expect(formatChainAmount("5847159150689873", 18)).toBe("0.00584716");
    expect(formatChainAmount("1404823016834864512415", 18)).toBe("1,404.823017");
  });

  it("infers 18 decimals for native ETH but not for tokens with unknown precision", () => {
    expect(displayDecimals("eth", "ETH", undefined, undefined)).toBe(18);
    expect(displayDecimals("erc20", "0x1", undefined, false)).toBeUndefined();
    expect(displayDecimals("erc20", "0x1", 6, true)).toBe(6);
  });

  it("preserves tiny non-zero values with scientific notation", () => {
    expect(formatChainAmount("1", 18)).toBe("1e-18");
    expect(formatChainAmount("-1", 18)).toBe("-1e-18");
  });

});
