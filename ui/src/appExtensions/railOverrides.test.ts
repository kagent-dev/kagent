import { describe, expect, it } from "vitest";
import { Bot, Puzzle } from "lucide-react";
import {
  applyAgentRailOverrides,
  isRailEntryHidden,
  mergeExtensionAgentRailOverrides,
} from "./railOverrides";
import { coreRailItems } from "@/components/agent/railItems";

const items = coreRailItems({ agentHref: "/a", newChatHref: "/a/new" });

describe("merging rail overrides from several extensions", () => {
  it("keeps independent choices from different extensions", () => {
    // The whole reason this merges two levels deep: one product hiding an entry and
    // another renaming it should produce a hidden, renamed entry.
    const merged = mergeExtensionAgentRailOverrides([
      { newChat: { hidden: true } },
      { newChat: { label: "Start something" } },
    ]);
    expect(merged.newChat).toEqual({ hidden: true, label: "Start something" });
  });

  it("lets the later extension win one field", () => {
    const merged = mergeExtensionAgentRailOverrides([
      { agentDetails: { label: "First" } },
      { agentDetails: { label: "Second" } },
    ]);
    expect(merged.agentDetails?.label).toBe("Second");
  });

  it("does not let an unset field blank an earlier one", () => {
    const merged = mergeExtensionAgentRailOverrides([
      { agentDetails: { label: "Kept" } },
      { agentDetails: { order: 400 } },
    ]);
    expect(merged.agentDetails).toEqual({ label: "Kept", order: 400 });
  });

  it("ignores extensions with no opinion", () => {
    expect(mergeExtensionAgentRailOverrides([undefined, {}])).toEqual({});
  });
});

describe("applying rail overrides", () => {
  it("changes nothing when there are none", () => {
    expect(applyAgentRailOverrides(items, undefined)).toEqual(items);
  });

  it("drops a hidden entry", () => {
    const applied = applyAgentRailOverrides(items, { newChat: { hidden: true } });
    expect(applied.map((item) => item.key)).toEqual(["agentDetails"]);
  });

  it("renames, retargets and re-icons one entry", () => {
    // `path` on the override lands on the rail's `to`, which is the same idea under
    // the name the rail uses for it.
    const [entry] = applyAgentRailOverrides(items, {
      agentDetails: { label: "About this agent", path: "/mine", icon: Puzzle },
    });
    expect(entry.label).toBe("About this agent");
    expect(entry.to).toBe("/mine");
    expect(entry.icon).toBe(Puzzle);
    expect(entry.testId).toBe("agent-nav-agent-conversations");
  });

  it("reorders the application's own entries", () => {
    const applied = applyAgentRailOverrides(items, { newChat: { order: 50 } });
    expect(applied.map((item) => item.key)).toEqual(["newChat", "agentDetails"]);
  });

  it("leaves untouched entries as they were", () => {
    const [details] = applyAgentRailOverrides(items, { newChat: { label: "x" } });
    expect(details.icon).toBe(Bot);
    expect(details.label).toBe("Agent Details");
  });
});

describe("isRailEntryHidden", () => {
  it("answers for the fallback that is not a rail item", () => {
    // "New chat" becomes a button where the agent has no address, and a `hidden`
    // override has to reach that form too or hiding it would only half work.
    expect(isRailEntryHidden("newChat", { newChat: { hidden: true } })).toBe(true);
    expect(isRailEntryHidden("newChat", { newChat: { label: "x" } })).toBe(false);
    expect(isRailEntryHidden("newChat", undefined)).toBe(false);
  });
});
