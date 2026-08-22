import { describe, expect, it } from "vitest";
import { formatChainAmount } from "./format";

describe("formatChainAmount", () => {
  it("formats an integer string without losing precision", () => {
    expect(formatChainAmount("123456789012345678901234", 18)).toBe("123456.789012345678901234");
  });

  it("keeps the raw integer when token precision is unknown", () => {
    expect(formatChainAmount("12345678901234567890")).toBe("12345678901234567890");
  });
});
