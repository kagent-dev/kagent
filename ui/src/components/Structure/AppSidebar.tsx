import { useState } from "react";
import { Layout, Menu } from "antd";
import { useTheme } from "@emotion/react";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { useThemeMode } from "@/theme/themeMode";
import { SidebarFooter } from "./SidebarFooter";
import { coreNavItems } from "./navItems";
import type { NavItem } from "./navItems";
import {
  ExtensionSlot,
  applyNavOverrides,
  buildSidebarSections,
  isNavPathActive,
  useExtensionNavItems,
  useExtensionNavOverrides,
} from "@/appExtensions";

const { Sider } = Layout;

/**
 * Picks the nav key whose path is the longest prefix of the current location,
 * so /agents/foo/chat still highlights "Agents" while "/" only matches itself.
 */
function activeKeyFor(pathname: string, items: readonly NavItem[]): string[] {
  const match = items
    .filter((item) => isNavPathActive(item.path, pathname))
    .sort((a, b) => b.path.length - a.path.length)[0];
  return match ? [match.key] : [];
}

export function AppSidebar() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const extensionNavItems = useExtensionNavItems();
  const { mode } = useThemeMode();
  const [collapsed, setCollapsed] = useState(false);
  const navOverrides = useExtensionNavOverrides();

  // Core and extension entries interleave by `order`, so consecutive core items
  // are grouped into one antd Menu and extension components render between the
  // groups. That is what lets a contribution sit at position 250 rather than
  // being appended after everything the app ships with.
  // Overrides applied before grouping, so a hidden or re-ordered entry is
  // grouped as the extension asked rather than as the application declared.
  const sections = buildSidebarSections(
    applyNavOverrides(coreNavItems, navOverrides),
    extensionNavItems,
  );

  return (
    <Sider
      width={theme.layout.sidebarWidth}
      // Still collapses itself on a narrow viewport, and the button below drives
      // the same state — so a reader who collapses it by hand does not have it
      // spring open again on the next resize.
      breakpoint="lg"
      collapsed={collapsed}
      onCollapse={setCollapsed}
      collapsedWidth={theme.layout.sidebarCollapsedWidth}
      data-testid="app-sidebar"
      css={{
        borderRight: `1px solid ${theme.color.border}`,
        paddingTop: theme.space(2),
        // A very shallow gradient, which is what keeps the rail from reading as a
        // flat block beside the page without becoming a decoration of its own.
        background: theme.color.surfaceNav,
        // Exactly the viewport, and held there while the page scrolls past it.
        //
        // The rail is a column with a footer pinned to its bottom, so its height has
        // to be the window's — not the document's. Left to grow with the page, an
        // expanded tool server pushed the footer below the fold and the only way to
        // reach it was to scroll the whole page down. Sticky rather than fixed so it
        // still occupies its column in the layout's flex row and the content beside
        // it keeps its width.
        position: "sticky",
        // Below the header, which stays put, so this is a constant rather than
        // something that has to be measured as the header leaves.
        top: theme.layout.headerHeight,
        height: `calc(100vh - ${theme.layout.headerHeight}px)`,
        alignSelf: "flex-start",
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        // antd puts its own background on the inner wrapper, which would cover the
        // gradient above.
        "& .ant-layout-sider-children": {
          display: "flex",
          flexDirection: "column",
          minHeight: 0,
          background: "transparent",
        },
        // The nav rows: inset from both edges and rounded, so the selected row reads
        // as a pill inside the rail instead of a band spanning it. antd lays these
        // out full-width with a right-hand active marker, which is the look this
        // replaces.
        "& .ant-menu-item": {
          width: "auto",
          marginInline: theme.space(2),
          marginBlock: 2,
          paddingInline: theme.space(3),
          borderRadius: theme.radius.md,
          // Collapsed, the row is a square holding one icon, so the icon is what has to
          // be centred in it. The padding above is measured from the left for a row that
          // also has a label, and it displaced the library's own collapsed centring —
          // every icon sat a few pixels left of the rail's centre line, which is the kind
          // of misalignment that reads as sloppiness without being obvious enough to
          // name. Centred by the box rather than by a padding calculation, so it holds
          // whatever the rail's collapsed width is set to.

          // A nav row is a destination, not prose. Dragging across the rail used to
          // select the labels, which on a double-click left blue highlight sitting
          // over the navigation the click had just performed.
          userSelect: "none",
        },
        // Pressed. antd gives a menu row hover and selected states and nothing in
        // between, so a click on a slow route looked like it had not registered.
        // Deeper than hover, lighter than selected, and instant.
        "& .ant-menu-item:active": {
          background:
            mode === "light"
              ? "rgba(109, 40, 217, 0.14)"
              : "rgba(139, 92, 246, 0.3)",
          transition: "none",
        },
        // Collapsed: the row is a square holding one icon, so the icon is what has to be
        // centred in it. The padding above is measured from the left for a row that also
        // has a label, and it displaced the library's own collapsed centring — every icon
        // sat a few pixels left of the rail's centre line, which reads as sloppiness
        // without being obvious enough to name. Centred by the box rather than by a
        // padding calculation, so it holds at whatever the collapsed width is set to.
        "& .ant-menu-inline-collapsed .ant-menu-item": {
          paddingInline: 0,
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        },
        "& .ant-menu-item::after": { display: "none" },
        "& .ant-menu-inline": { background: "transparent", borderInlineEnd: "none" },
      }}
    >
      {/* The nav list is the part that gives.
          The rail is exactly the window's height, so on a window too short to hold
          every entry something has to yield — and it must not be the footer, which
          carries the theme and collapse controls. Scrolls only when it has to: at any
          ordinary height this box is shorter than its contents' room and no scrollbar
          appears at all. `minHeight: 0` because a flex child will not shrink below its
          content without it, which is what would push the footer out of view again. */}
      <div
        css={{
          flex: "1 1 auto",
          minHeight: 0,
          overflowY: "auto",
          overflowX: "hidden",
        }}
      >
        {sections.map((section) =>
          section.kind === "core" ? (
          <Menu
            key={section.key}
            // Follows the palette. Pinned to "dark", the library paints item
            // text near-white, which on a light rail left every unselected entry
            // invisible — only the selected pill could be read.
            theme={mode === "light" ? "light" : "dark"}
            mode="inline"
            inlineCollapsed={collapsed}
            selectedKeys={activeKeyFor(pathname, section.items)}
            css={{ borderInlineEnd: "none" }}
            /*
             * The whole row stays clickable, but not when the click was already the
             * anchor's.
             *
             * Each label is a real link so the entry can be opened in a new tab — a
             * menu item with no `href` gives a reader nothing to cmd-click, and this
             * is navigation, which is exactly what people expect to be able to do
             * that with. The router handles those clicks itself, so navigating again
             * from here would push a second history entry for one click and break the
             * back button. Clicks that land on the icon or the padding still come
             * through here, which is what keeps the row's hit area whole.
             */
            onClick={({ key, domEvent }) => {
              if ((domEvent.target as HTMLElement).closest("a")) return;
              const target = section.items.find((item) => item.key === key);
              if (target) navigate(target.path);
            }}
            items={section.items.map((item) => {
              const Icon = item.icon;
              return {
                key: item.key,
                icon: <Icon size={16} />,
                label: (
                  // Inherits the menu's own colour rather than the link palette: this
                  // reads as a menu entry, and antd has already decided what that
                  // looks like in each theme and in the selected state.
                  <Link to={item.path} css={{ color: "inherit" }}>
                    {item.label}
                  </Link>
                ),
                "data-testid": `nav-${item.key}`,
              };
            })}
          />
          ) : (
            <section.item.Component
              key={section.key}
              isActive={
                section.item.path !== undefined &&
                isNavPathActive(section.item.path, pathname)
              }
            />
          ),
        )}
      </div>

      <ExtensionSlot id="app_shell_appLayout_appSidebar_footer" />

      {/* After the extension slot, so a distribution's own footer content sits above
          these rather than below the collapse control. */}
      <SidebarFooter
        collapsed={collapsed}
        onToggleCollapsed={() => setCollapsed((open) => !open)}
      />
    </Sider>
  );
}
