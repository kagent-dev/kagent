import { describe, expect, it } from "vitest";
import { isRowActionVisible, type ExtensionRowAction } from "./rowActions";

const row = { kind: "agent", namespace: "ns", name: "web" };
const action = (visible?: ExtensionRowAction["visible"]): ExtensionRowAction => ({
  id: "a", menuId: "app_agents_agentsList_rowMenu", label: "A", onClick: () => {}, visible,
});

describe("isRowActionVisible", () => {
  it("shows an action with no visible callback", () => {
    expect(isRowActionVisible(action(), row)).toBe(true);
  });
  it("treats other truthy and falsy results as shown and hidden", () => {
    const loose = (value: unknown) => (() => value) as unknown as ExtensionRowAction["visible"];
    expect(isRowActionVisible(action(loose("x")), row)).toBe(true);
    expect(isRowActionVisible(action(loose(0)), row)).toBe(false);
  });
  it("hides an action whose callback returns a Promise", () => {
    const async = (async () => false) as unknown as ExtensionRowAction["visible"];
    expect(isRowActionVisible(action(async), row)).toBe(false);
  });
});
