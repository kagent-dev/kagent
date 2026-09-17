import type { CoreRailKey, RailItem } from "@/components/agent/railItems";
import type { ExtensionNavOverride } from "./navOverrides";

/**
 * Changes to the agent rail's own entries, keyed by the entry they apply to.
 *
 * The same `ExtensionNavOverride` the application sidebar takes, because the choices
 * are the same ones: hide it, rename it, send it somewhere else, change its icon,
 * move it. Its `path` field lands on the rail's `to`, which is the same idea under
 * the name the rail uses for it.
 */
export type ExtensionAgentRailOverrides = Partial<
  Record<CoreRailKey, ExtensionNavOverride>
>;

/**
 * Several extensions' rail overrides flattened into one table.
 *
 * Merged two levels deep, like the sidebar's, because an override is itself a table
 * of independent choices: one extension hiding an entry and another renaming it
 * should produce a hidden, renamed entry rather than whichever spoke last. Within a
 * single field the ordinary rule still applies and the later extension wins.
 */
export function mergeExtensionAgentRailOverrides(
  parts: readonly (ExtensionAgentRailOverrides | undefined)[],
): ExtensionAgentRailOverrides {
  const merged: Record<string, ExtensionNavOverride> = {};

  for (const part of parts) {
    if (!part) continue;
    for (const [key, override] of Object.entries(part)) {
      if (!override) continue;
      merged[key] = { ...merged[key], ...definedFields(override) };
    }
  }

  return merged as ExtensionAgentRailOverrides;
}

/** Absent means "no opinion", so an unset field must not blank an earlier one. */
function definedFields(override: ExtensionNavOverride): ExtensionNavOverride {
  return Object.fromEntries(
    Object.entries(override).filter(([, value]) => value !== undefined),
  );
}

/**
 * The rail's entries with an extension's overrides applied.
 *
 * Hidden entries are dropped and the rest keep their relative order unless an
 * override changes it, so the rail can render the result directly.
 */
export function applyAgentRailOverrides(
  items: readonly RailItem[],
  overrides: ExtensionAgentRailOverrides | undefined,
): RailItem[] {
  if (!overrides) return [...items];

  return items
    .filter((item) => !overrides[item.key as CoreRailKey]?.hidden)
    .map((item) => {
      const override = overrides[item.key as CoreRailKey];
      if (!override) return item;
      return {
        ...item,
        label: override.label ?? item.label,
        to: override.path ?? item.to,
        icon: override.icon ?? item.icon,
        order: override.order ?? item.order,
      };
    })
    .sort((left, right) => left.order - right.order);
}

/** Whether an override has taken one of the application's entries off the rail. */
export function isRailEntryHidden(
  key: CoreRailKey,
  overrides: ExtensionAgentRailOverrides | undefined,
): boolean {
  return overrides?.[key]?.hidden === true;
}
