import type { LucideIcon } from "lucide-react";
import { coreNavItems } from "@/components/Structure/navItems";
import type { NavItem } from "@/components/Structure/navItems";

/**
 * The navigation entries this application ships, by key.
 *
 * Derived from the entries themselves, so a key cannot be overridden that does
 * not exist, and adding a page makes it overridable without any change here.
 */
export type CoreNavKey = (typeof coreNavItems)[number]["key"];

/**
 * A change an extension makes to one of the application's own nav entries.
 *
 * Contributing an entry covers "this product has a page kagent does not". This
 * covers the other half: a product whose navigation names, orders or groups the
 * *same* pages differently — or which replaces one of them with its own, and so
 * needs the original out of the way.
 *
 * Every field is optional and absent means "leave it as the application has it",
 * so an override states only the difference.
 */
export interface ExtensionNavOverride {
  /**
   * Removes the entry from navigation. The route still resolves, so a page is
   * never made unreachable by a typed URL — only unlisted.
   */
  hidden?: boolean;
  /** Renames it, for a product whose vocabulary differs. */
  label?: string;
  /**
   * Sends it somewhere else. For when a product supplies its own version of a
   * destination and wants the familiar entry to lead there.
   */
  path?: string;
  /**
   * Adds path prefixes under which the entry is highlighted, for a page a product
   * adds that belongs to it. Appended to the entry's own, never replacing them, so a
   * product cannot unclaim a page the application files under the entry.
   */
  activePaths?: readonly string[];
  icon?: LucideIcon;
  order?: number;
}

export type ExtensionNavOverrides = Partial<Record<CoreNavKey, ExtensionNavOverride>>;

/**
 * Several extensions' overrides flattened into one table.
 *
 * Merged two levels deep, unlike every other singular capability, because an
 * override is itself a table of independent choices: one extension hiding an entry
 * and another renaming it should produce a hidden, renamed entry rather than
 * whichever spoke last. Within a single field the ordinary rule still applies — the
 * later extension's `label` wins.
 */
export function mergeExtensionNavOverrides(
  parts: readonly (ExtensionNavOverrides | undefined)[],
): ExtensionNavOverrides {
  const merged: Record<string, ExtensionNavOverride> = {};

  for (const part of parts) {
    if (!part) continue;
    for (const [key, override] of Object.entries(part)) {
      if (!override) continue;
      const previous = merged[key];
      merged[key] = { ...previous, ...definedFields(override) };
      // A list of claims, not one choice, so a later extension adds to it rather than
      // replacing it — otherwise installing a second product unhighlights the first's page.
      if (previous?.activePaths && override.activePaths) {
        merged[key].activePaths = [...previous.activePaths, ...override.activePaths];
      }
    }
  }

  return merged as ExtensionNavOverrides;
}

/** Absent means "no opinion", so an unset field must not blank an earlier one. */
function definedFields(override: ExtensionNavOverride): ExtensionNavOverride {
  return Object.fromEntries(
    Object.entries(override).filter(([, value]) => value !== undefined),
  );
}

/**
 * The application's navigation with an extension's overrides applied.
 *
 * Hidden entries are dropped and the rest keep their relative order unless an
 * override changes it, so a sidebar can render the result directly.
 */
export function applyNavOverrides(
  items: readonly NavItem[],
  overrides: ExtensionNavOverrides | undefined,
): NavItem[] {
  if (!overrides) return [...items];

  return items
    .filter((item) => !overrides[item.key as CoreNavKey]?.hidden)
    .map((item) => {
      const override = overrides[item.key as CoreNavKey];
      if (!override) return item;
      return {
        ...item,
        label: override.label ?? item.label,
        path: override.path ?? item.path,
        activePaths: [...(item.activePaths ?? []), ...(override.activePaths ?? [])],
        icon: override.icon ?? item.icon,
        order: override.order ?? item.order,
      };
    })
    .sort((a, b) => a.order - b.order);
}
