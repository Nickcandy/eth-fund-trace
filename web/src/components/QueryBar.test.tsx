import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { QueryBar } from "./QueryBar";

const query = { chain: "ethereum" as const, address: "", direction: "both" as const, depth: 3, topN: 10, asset: "all" };

describe("QueryBar", () => {
  it("submits the current analysis command", async () => {
    const submit = vi.fn();
    render(<QueryBar value={query} onChange={() => undefined} onSubmit={submit} busy={false} />);
    await userEvent.click(screen.getByRole("button", { name: "开始分析" }));
    expect(submit).toHaveBeenCalledOnce();
  });

  it("reveals a contract field for a specific token", async () => {
    const change = vi.fn();
    render(<QueryBar value={{ ...query, asset: "" }} onChange={change} onSubmit={() => undefined} busy={false} />);
    expect(screen.getByLabelText("Token 合约")).toBeVisible();
  });
});
