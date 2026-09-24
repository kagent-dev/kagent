import type { NavItem } from "@/components/Structure/navItems";
import type { ExtensionNavItemContribution } from "./types";

/**
 * A run of the sidebar. Consecutive core items are grouped so they can share a
 * single antd `Menu`, with contributed items rendered between the groups — that is
 * what lets a contribution land at its `order` position rather than being
 * appended to the end.
 */
export type SidebarSection =
  | { kind: "core"; key: string; items: NavItem[] }
  | { kind: "extension"; key: string; item: ExtensionNavItemContribution };

/**
 * Core and contributed nav entries merged into ordered, renderable runs.
 *
 * `extensionItems` is every installed extension's entries already flattened and
 * sorted, so this needs to know nothing about how many extensions there are: two
 * contributions at 250 and 260 interleave with the core items exactly as one
 * extension's two would.
 */
export function buildSidebarSections(
  coreItems: readonly NavItem[],
  extensionItems: readonly ExtensionNavItemContribution[],
): SidebarSection[] {
  const merged = [
    ...coreItems.map((item) => ({ order: item.order, core: item })),
    ...extensionItems.map((item) => ({ order: item.order, extension: item })),
  ].sort((a, b) => a.order - b.order);

  const sections: SidebarSection[] = [];

  for (const entry of merged) {
    if ("extension" in entry) {
      sections.push({
        kind: "extension",
        key: `extension-${entry.extension.key}`,
        item: entry.extension,
      });
      continue;
    }

    const last = sections.at(-1);
    if (last?.kind === "core") {
      last.items.push(entry.core);
    } else {
      sections.push({
        kind: "core",
        key: `core-${entry.core.key}`,
        items: [entry.core],
      });
    }
  }

  return sections;
}

/**
 * Whether a nav path is the one the current location is under. Longest-prefix
 * matching lives in the sidebar; this is the per-item predicate it uses, shared
 * so extension items highlight on exactly the same rule as core ones.
 */
export function isNavPathActive(path: string, pathname: string): boolean {
  return path === "/" ? pathname === "/" : pathname.startsWith(path);
}
