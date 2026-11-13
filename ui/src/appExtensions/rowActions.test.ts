import { afterEach, describe, expect, it, vi } from "vitest";
import { isRowActionVisible, type ExtensionRowAction } from "./rowActions";

const row = { kind: "agent", namespace: "ns", name: "web" };
const action = (
  visible?: ExtensionRowAction["visible"],
  overrides: Partial<ExtensionRowAction> = {},
): ExtensionRowAction => ({
  id: "a", menuId: "app_agents_agentsList_rowMenu", label: "A", onClick: () => {}, visible, ...overrides,
} as ExtensionRowAction);
const boom = () => {
  throw new Error("boom");
};

afterEach(() => vi.restoreAllMocks());

describe("isRowActionVisible", () => {
  it("shows an action with no visible callback", () => {
    expect(isRowActionVisible(action(), row)).toBe(true);
  });

  it("passes the row to the callback and uses its answer", () => {
    const visible = vi.fn(() => false);
    expect(isRowActionVisible(action(visible), row)).toBe(false);
    expect(visible).toHaveBeenCalledWith(row);
  });

  it("leaves out an action whose callback throws, without throwing", () => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    expect(isRowActionVisible(action(boom, { id: "throws" }), row)).toBe(false);
  });

  it("reports a crashing callback once, however often it is rendered", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const crashing = action(boom, { id: "report-once" });
    isRowActionVisible(crashing, row);
    isRowActionVisible(crashing, row);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("reports a different failure from the same action", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    let n = 0;
    const flaky = action(() => {
      throw new Error(`boom ${n++}`);
    }, { id: "flaky" });
    isRowActionVisible(flaky, row);
    isRowActionVisible(flaky, row);
    expect(spy).toHaveBeenCalledTimes(2);
  });

  it("reports the same id separately in different menus", () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    isRowActionVisible(action(boom, { id: "shared" }), row);
    isRowActionVisible(action(boom, { id: "shared", menuId: "app_agents_harnessesList_rowMenu" }), row);
    expect(spy).toHaveBeenCalledTimes(2);
  });
});
