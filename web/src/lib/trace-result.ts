import type { TraceResult } from "../api/types";

export function isRenderableTraceResult(value: unknown): value is TraceResult {
  if (!value || typeof value !== "object") return false;
  const result = value as Partial<TraceResult>;
  return (
    Array.isArray(result.nodes) &&
    Array.isArray(result.edges) &&
    typeof result.ruleVersion === "string" &&
    typeof result.dataStatus === "string" &&
    typeof result.dataThroughBlock === "number"
  );
}
