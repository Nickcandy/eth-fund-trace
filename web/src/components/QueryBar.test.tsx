import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { QueryBar } from "./QueryBar";

const query = { chain: "ethereum" as const, address: "", direction: "both" as const, depth: 3, topN: 10, asset: "ETH" };
afterEach(cleanup);

describe("QueryBar", () => {
  it("submits the current analysis command", async () => {
    const submit = vi.fn();
    render(<QueryBar value={query} onChange={() => undefined} onSubmit={submit} busy={false} />);
    await userEvent.click(screen.getByRole("button", { name: "开始分析" }));
    expect(submit).toHaveBeenCalledOnce();
  });

  it("keeps the trace root asset fixed to ETH", () => {
    render(<QueryBar value={query} onChange={() => undefined} onSubmit={() => undefined} busy={false} />);
    expect(screen.queryByLabelText("资产")).not.toBeInTheDocument();
  });

  it("switches to transaction hash mode", async () => {
    const changeMode = vi.fn();
    const view = render(<QueryBar value={query} onChange={() => undefined} onSubmit={() => undefined} busy={false} onModeChange={changeMode} />);
    await userEvent.click(view.getByRole("button", { name: "交易哈希" }));
    expect(changeMode).toHaveBeenCalledWith("transaction");
    view.rerender(<QueryBar value={query} onChange={() => undefined} onSubmit={() => undefined} busy={false} mode="transaction" txHash="0xabc" onTxHashChange={() => undefined} />);
    expect(view.getByLabelText("交易哈希")).toHaveValue("0xabc");
    expect(view.queryByLabelText("方向")).not.toBeInTheDocument();
  });
});
