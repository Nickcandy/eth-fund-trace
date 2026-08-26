import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { GraphNodeModel } from "../graph/model";
import { DetailsPanel } from "./DetailsPanel";

afterEach(cleanup);

describe("DetailsPanel labels", () => {
  it("creates a label for the selected node from the labels tab", async () => {
    const user = userEvent.setup();
    const onLabel = vi.fn().mockResolvedValue(undefined);
    const node: GraphNodeModel = {
      id: "ethereum:0x0000000000000000000000000000000000000002",
      chain: "ethereum",
      address: "0x0000000000000000000000000000000000000002",
      hop: 1,
      terminal: false,
      seed: false,
      risk: "normal",
      hotWallet: false,
      labelTypes: [],
    };

    render(<DetailsPanel node={node} labels={[]} onLabel={onLabel} onClose={() => undefined} onFocus={() => undefined} />);
    await user.click(screen.getByRole("tab", { name: "标签" }));
    await user.type(screen.getByPlaceholderText("标签类型，如 exchange"), "hacker");
    await user.selectOptions(screen.getByLabelText("风险等级"), "high");
    await user.type(screen.getByPlaceholderText("证据，每行一条"), "case-1");
    await user.click(screen.getByRole("button", { name: "保存标签" }));

    await waitFor(() => expect(onLabel).toHaveBeenCalledWith({
      chain: "ethereum",
      address: node.address,
      type: "hacker",
      source: "manual",
      riskLevel: "high",
      confidence: 1,
      note: "",
      evidence: ["case-1"],
    }));
    expect(screen.getByText("标签已保存，重新追踪后传播生效")).toBeVisible();
  });

  it("shows automatic target assessment without a manual start action", async () => {
    const node: GraphNodeModel = { id: "ethereum:0x1", chain: "ethereum", address: "0x0000000000000000000000000000000000000001", hop: 0, terminal: false, seed: true, risk: "normal", hotWallet: false, labelTypes: ["hacker"] };
    render(<DetailsPanel node={node} labels={[{ chain: "ethereum", address: node.address, type: "hacker", source: "manual", riskLevel: "high", confidence: 1 }]} onLabel={vi.fn()} onClose={() => undefined} onFocus={() => undefined} />);
    expect(screen.queryByRole("button", { name: "启动传播" })).toBeNull();
    expect(screen.getByText("等待 Trace 完成后自动评估")).toBeVisible();
  });
});
