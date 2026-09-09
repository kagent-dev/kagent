import { Bot, SquarePen } from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ExtensionAgentRailItemContribution } from "@/appExtensions";

/**
 * One entry in the agent rail's navigation.
 *
 * The shape of `NavItem` in the application sidebar, with the rail's own two
 * additions: `to` rather than `path`, because the rail renders the link itself, and
 * a named `testId`, because these entries were reachable by name in the browser
 * suite before they were data and renaming them would have bought nothing.
 */
export interface RailItem {
  /** Stable identifier. Overrides address entries by this. */
  key: string;
  label: string;
  to: string;
  icon: LucideIcon;
  /** Lower sorts first. Core items use multiples of 100 to leave gaps. */
  order: number;
  testId: string;
  /** Other paths this entry stands for — the edit view belongs to Agent Details. */
  alsoActiveOn?: string[];
}

export const CORE_RAIL_KEYS = ["agentDetails", "newChat"] as const;

/** The rail entries the application ships, addressable by an extension override. */
export type CoreRailKey = (typeof CORE_RAIL_KEYS)[number];

/**
 * The rail's own entries, for the agent in front of the reader.
 *
 * A function rather than a constant, unlike `coreNavItems`: both destinations are
 * addresses of *this* agent, so there is no list to declare until one is known. An
 * entry whose address cannot be derived is left out rather than rendered dead — an
 * instance with no prepared revision belongs to no pair, and there would be nothing
 * at the other end of it.
 */
export function coreRailItems(targets: {
  agentHref?: string;
  newChatHref?: string;
}): RailItem[] {
  const items: RailItem[] = [];

  if (targets.agentHref) {
    items.push({
      key: "agentDetails",
      // "Agent Details" rather than "Agent": beside "New chat" and a list of chats,
      // a bare noun reads as a heading for the section rather than a place to go.
      label: "Agent Details",
      to: targets.agentHref,
      icon: Bot,
      order: 100,
      testId: "agent-nav-agent-conversations",
    });
  }

  if (targets.newChatHref) {
    items.push({
      key: "newChat",
      label: "New chat",
      to: targets.newChatHref,
      icon: SquarePen,
      order: 200,
      testId: "chat-new-session",
    });
  }

  return items;
}

/** A place in the rendered rail: one of the application's entries, or a contribution. */
export type RailEntrySlot =
  | { kind: "core"; order: number; item: RailItem }
  | { kind: "extension"; order: number; contribution: ExtensionAgentRailItemContribution };

/**
 * The application's entries and every contribution, in one order.
 *
 * Interleaved rather than appended, which is the whole point of the `order` field: a
 * product can put its entry between Agent Details and New chat instead of below both.
 * Ties keep the application's entry first, so adopting a core entry's order cannot
 * silently displace it.
 */
export function mergeRailEntries(
  items: readonly RailItem[],
  contributions: readonly ExtensionAgentRailItemContribution[],
): RailEntrySlot[] {
  const slots: RailEntrySlot[] = [
    ...items.map((item) => ({ kind: "core" as const, order: item.order, item })),
    ...contributions.map((contribution) => ({
      kind: "extension" as const,
      order: contribution.order,
      contribution,
    })),
  ];

  return slots.sort((left, right) => {
    if (left.order !== right.order) return left.order - right.order;
    if (left.kind === right.kind) return 0;
    return left.kind === "core" ? -1 : 1;
  });
}
