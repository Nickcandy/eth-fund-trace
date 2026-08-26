import { describe, expect, it } from "vitest";
import { edgeLabelVisible, labelsVisibleByDefault } from "./GraphCanvas";

describe("GraphCanvas density defaults", () => {
  it("shows labels for small graphs and hides them for dense graphs", () => {
    expect(labelsVisibleByDefault(8)).toBe(true);
    expect(labelsVisibleByDefault(74)).toBe(false);
  });

  it("keeps direct seed edges labeled in dense graphs", () => {
    expect(edgeLabelVisible(false, false, true)).toBe(true);
    expect(edgeLabelVisible(false, false, false)).toBe(false);
    expect(edgeLabelVisible(false, true, false)).toBe(true);
  });
});
