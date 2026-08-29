import { describe, expect, it } from "vitest";
import {
  displayDecimals,
  formatAssetAmount,
  formatChainAmount,
} from "./format";

describe("formatChainAmount", () => {
  it("formats a large integer as a compact human-readable amount", () => {
    expect(formatChainAmount("123456789012345678901234", 18)).toBe(
      "123,456.789",
    );
  });

  it("compacts the raw integer when token precision is unknown", () => {
    expect(formatChainAmount("12345678901234567890")).toBe("1.2345e+19");
  });

  it("keeps human-readable precision without rendering long fractional tails", () => {
    expect(formatChainAmount("5847159150689873", 18)).toBe("0.0058");
    expect(formatChainAmount("1404823016834864512415", 18)).toBe("1,404.823");
    expect(formatChainAmount("1404823016834864512415", 18, 2)).toBe("1,404.82");
  });

  it("places the asset unit after the compact amount", () => {
    expect(formatAssetAmount("1234567890000000000", 18, "ETH", 2)).toBe(
      "1.23 ETH",
    );
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
