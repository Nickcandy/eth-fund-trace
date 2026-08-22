import type { Chain, Direction, TraceQuery } from "../api/types";

const ADDRESS = /^0x[0-9a-fA-F]{40}$/;

export function validateTraceQuery(query: TraceQuery): string | undefined {
  if (!ADDRESS.test(query.address)) return "请输入 0x 开头的 40 位十六进制地址";
  if (!(["ethereum", "base"] as string[]).includes(query.chain)) return "不支持的网络";
  if (!(["in", "out", "both"] as string[]).includes(query.direction)) return "不支持的追踪方向";
  if (!Number.isInteger(query.depth) || query.depth < 1 || query.depth > 5) return "深度必须在 1 到 5 之间";
  if (!Number.isInteger(query.topN) || query.topN < 1 || query.topN > 20) return "Top-N 必须在 1 到 20 之间";
  if (!query.asset.trim()) return "请选择资产";
  if (!["all", "ETH", "erc20"].includes(query.asset) && !ADDRESS.test(query.asset)) return "指定 Token 必须是有效合约地址";
}

export function readTraceQuery(search: string): TraceQuery {
  const params = new URLSearchParams(search);
  const chain = params.get("chain") === "base" ? "base" : "ethereum";
  const directionValue = params.get("direction");
  const direction: Direction = directionValue === "in" || directionValue === "out" ? directionValue : "both";
  const depth = Number(params.get("depth") || 3);
  const topN = Number(params.get("topN") || 10);
  return {
    chain: chain as Chain,
    address: params.get("address") ?? "",
    direction,
    depth: Number.isInteger(depth) && depth >= 1 && depth <= 5 ? depth : 3,
    topN: Number.isInteger(topN) && topN >= 1 && topN <= 20 ? topN : 10,
    asset: params.get("asset") || "all",
  };
}

export function writeTraceQuery(query: TraceQuery): string {
  const params = new URLSearchParams();
  Object.entries(query).forEach(([key, value]) => params.set(key, String(value)));
  return `?${params.toString()}`;
}
