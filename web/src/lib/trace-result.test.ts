import { describe, expect, it } from "vitest";
import type { TraceResult } from "../api/types";
import { isRenderableTraceResult } from "./trace-result";

function result(ruleVersion: string): TraceResult {
  return {
    nodes: [],
    edges: [],
    dataThroughBlock: 0,
    dataStatus: "complete",
    risk: { score: 0, level: "no_conclusion", ruleVersion: "risk-v1" },
    ruleVersion,
  };
}

describe("trace result compatibility", () => {
  it("renders a structurally compatible result after a rule upgrade", () => {
    expect(isRenderableTraceResult(result("trace-v999"))).toBe(true);
  });

  it("rejects results without graph arrays", () => {
    expect(
      isRenderableTraceResult({
        ...result("trace-v6"),
        nodes: undefined,
      }),
    ).toBe(false);
    expect(
      isRenderableTraceResult({ ...result("trace-v6"), edges: "bad" }),
    ).toBe(false);
  });
});
