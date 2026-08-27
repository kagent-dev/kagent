import type { NavItem } from "@/components/Structure/navItems";
import type { VendorNavItemContribution } from "./types";

/**
 * A run of the sidebar. Consecutive core items are grouped so they can share a
 * single antd `Menu`, with vendor items rendered between the groups — that is
 * what lets a contribution land at its `order` position rather than being
 * appended to the end.
 */
export type SidebarSection =
  | { kind: "core"; key: string; items: NavItem[] }
  | { kind: "vendor"; key: string; item: VendorNavItemContribution };

/** Core and vendor nav entries merged into ordered, renderable runs. */
export function buildSidebarSections(
  coreItems: readonly NavItem[],
  vendorItems: readonly VendorNavItemContribution[],
): SidebarSection[] {
  const merged = [
    ...coreItems.map((item) => ({ order: item.order, core: item })),
    ...vendorItems.map((item) => ({ order: item.order, vendor: item })),
  ].sort((a, b) => a.order - b.order);

  const sections: SidebarSection[] = [];

  for (const entry of merged) {
    if ("vendor" in entry) {
      sections.push({
        kind: "vendor",
        key: `vendor-${entry.vendor.key}`,
        item: entry.vendor,
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
 * so vendor items highlight on exactly the same rule as core ones.
 */
export function isNavPathActive(path: string, pathname: string): boolean {
  return path === "/" ? pathname === "/" : pathname.startsWith(path);
}
