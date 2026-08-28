import type { ComponentType } from "react";
import type { NavItem } from "@/components/Structure/navItems";
import type { ExtensionNavItemContribution } from "./types";

/** What a replacement sidebar is given. */
export interface ExtensionSidebarProps {
  /**
   * The application's own navigation, so a replacement can still offer it
   * rather than hard-coding a copy that drifts when a page is added.
   */
  coreNavItems: readonly NavItem[];
  /** Everything the installed extensions contributed through `navItems`, in order. */
  extensionNavItems: readonly ExtensionNavItemContribution[];
}

/**
 * Whole regions of the shell an extension may replace.
 *
 * Contributing a nav item is enough when a product wants its pages listed
 * alongside the application's. It is not enough when the product's navigation is
 * a different shape — grouped sections, a collapse control, a logo, a footer —
 * because those are properties of the sidebar itself and no amount of items adds
 * them.
 *
 * A replacement owns the region completely, including rendering the application's
 * own navigation. That is the same bargain the rest of the framework strikes: the
 * extension supplies the whole renderer, and in exchange it is not limited to
 * what a configuration language happened to anticipate.
 */
/**
 * What a replacement layout is given: the same navigation a replacement sidebar
 * gets, since it is responsible for rendering that navigation itself.
 */
export type ExtensionLayoutProps = ExtensionSidebarProps;

export interface ExtensionShell {
  Sidebar?: ComponentType<ExtensionSidebarProps>;
  Header?: ComponentType;
  /**
   * Replaces the whole shell: chrome, navigation and the frame the routed pages
   * render into. The replacement is responsible for rendering React Router's
   * `<Outlet />` — without it no page appears at all.
   *
   * Needed when the shell's *arrangement* differs, not just its regions. A
   * product whose logo lives in the sidebar and which has no top bar cannot be
   * expressed by swapping this application's sidebar, because the header would
   * still be above it. Replacing regions piecemeal can only ever produce a
   * variation on this application's arrangement; this produces a different one.
   *
   * Takes precedence over `Sidebar` and `Header`, which it subsumes.
   */
  Layout?: ComponentType<ExtensionLayoutProps>;
}
