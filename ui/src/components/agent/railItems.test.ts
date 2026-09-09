import { describe, expect, it } from "vitest";
import { CORE_RAIL_KEYS, coreRailItems, mergeRailEntries } from "./railItems";
import type { ExtensionAgentRailItemContribution } from "@/appExtensions";

const contribution = (
  key: string,
  order: number,
): ExtensionAgentRailItemContribution => ({
  key,
  order,
  Component: () => null,
});

describe("the agent rail's own entries", () => {
  it("names both entries an override can address", () => {
    // The override table is keyed by these, so a rename here without one there
    // silently stops a product being able to reach the entry.
    expect(CORE_RAIL_KEYS).toEqual(["agentDetails", "newChat"]);
  });

  it("leaves out an entry whose address cannot be derived", () => {
    // An instance with no prepared revision belongs to no pair, so there is nothing
    // at the other end. A dead row is worse than no row.
    expect(coreRailItems({}).map((item) => item.key)).toEqual([]);
    expect(
      coreRailItems({ agentHref: "/agents/kagent/assistant/on/kagent" }).map(
        (item) => item.key,
      ),
    ).toEqual(["agentDetails"]);
  });

  it("orders its own entries with gaps, so a contribution can sit between them", () => {
    const items = coreRailItems({ agentHref: "/a", newChatHref: "/a/new" });
    expect(items.map((item) => [item.key, item.order])).toEqual([
      ["agentDetails", 100],
      ["newChat", 200],
    ]);
  });
});

describe("interleaving contributions", () => {
  const items = coreRailItems({ agentHref: "/a", newChatHref: "/a/new" });

  it("places a contribution by its order, not after everything", () => {
    const merged = mergeRailEntries(items, [contribution("middle", 150)]);
    expect(
      merged.map((slot) =>
        slot.kind === "core" ? slot.item.key : slot.contribution.key,
      ),
    ).toEqual(["agentDetails", "middle", "newChat"]);
  });

  it("keeps the application's entry first on a tie", () => {
    // Adopting a core entry's order is a mistake rather than a bid to replace it,
    // and displacing the application's entry would hide the mistake.
    const merged = mergeRailEntries(items, [contribution("clash", 100)]);
    expect(merged[0].kind).toBe("core");
    expect(merged[1].kind).toBe("extension");
  });

  it("draws contributions in order even with no core entries left", () => {
    const merged = mergeRailEntries([], [contribution("b", 300), contribution("a", 50)]);
    expect(
      merged.map((slot) => (slot.kind === "extension" ? slot.contribution.key : "core")),
    ).toEqual(["a", "b"]);
  });

  it("carries the contribution through untouched", () => {
    const one = { ...contribution("one", 10), path: "/one", label: "One" };
    const [slot] = mergeRailEntries([], [one]);
    // The same object, not a copy: a contribution's component identity is what React
    // reconciles on, so cloning it here would remount the entry on every render.
    expect(slot.kind === "extension" && slot.contribution).toBe(one);
  });
});
