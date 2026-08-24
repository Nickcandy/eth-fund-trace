import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { TransactionAnalysis } from "../api/types";
import { TransactionView } from "./TransactionView";

const address = "0x0000000000000000000000000000000000000001";
afterEach(cleanup);
const analysis: TransactionAnalysis = {
  chain: "ethereum", chainId: 1, txHash: `0x${"a".repeat(64)}`, blockNumber: 1, from: address, to: address,
  value: "0", input: "0x", succeeded: true, transfers: [{ token: address, from: address, to: address, amount: "99", logIndex: 3 }], wraps: [{ type: "deposit", account: address, amount: "42", logIndex: 1, evidence: "receipt" }],
  swaps: [{ pool: address, protocol: "uniswap", version: "v3", verified: true, sender: address, recipient: address, tokenIn: address, tokenOut: address, amountIn: "10", amountOut: "9", fee: 3000, logIndex: 2, outputAddress: address, evidence: ["verified factory"] }],
  finalOutputAddress: address, quality: { status: "complete", ambiguousRoute: false, evidence: ["receipt"] }, analyzedAt: "2026-08-24T00:00:00Z",
};

describe("TransactionView", () => {
  it("shows swap and WETH evidence and continues tracing", async () => {
    const trace = vi.fn();
    render(<TransactionView analysis={analysis} onTrace={trace} />);
    expect(screen.getByText("Uniswap V3")).toBeVisible();
    expect(screen.getByText("包装 ETH")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: /继续追踪/ }));
    expect(trace).toHaveBeenCalledWith(address);
  });

  it("does not invent a protocol for unknown logs", () => {
    const view = render(<TransactionView analysis={{ ...analysis, finalOutputAddress: undefined, swaps: [{ ...analysis.swaps[0], verified: false, protocol: undefined }] }} onTrace={() => undefined} />);
    expect(view.getByText("未验证协议")).toBeVisible();
    expect(view.getByText("99")).toBeVisible();
    expect(view.queryByRole("button", { name: /继续追踪/ })).not.toBeInTheDocument();
  });
});
