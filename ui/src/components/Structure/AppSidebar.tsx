import { useState } from "react";
import { Layout, Menu } from "antd";
import { useTheme, type Theme } from "@emotion/react";
import { Link, useLocation, useNavigate } from "react-router-dom";
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


/** Where every rail icon's left edge sits, from the row's inset: the same expanded and collapsed. */
export const NAV_INSET = 18;

function navEntry(item: NavItem) {
  const Icon = item.icon;
  return {
    key: item.key,
    icon: <Icon size={16} />,
    // A real link so the entry can be opened in a new tab; it inherits the row's colour.
    label: (
      <Link to={item.path} css={{ color: "inherit" }}>
        {item.label}
      </Link>
    ),
    "data-testid": `nav-${item.key}`,
  };
}

/** Type roles and states for the rail. Colours come from the theme's nav tokens. */
function navRailStyles(theme: Theme) {
  return {
    "& .ant-menu-inline, & .ant-menu-vertical": { background: "transparent", borderInlineEnd: "none" },
    "& .ant-menu-item": {
      fontWeight: 400,
      lineHeight: "20px",
      userSelect: "none" as const,
    },
    "& .ant-menu-item-selected": { fontWeight: 500 },
    "& .ant-menu-item-selected .ant-menu-item-icon": { color: theme.color.navActiveIcon },
    "& .ant-menu-item-selected:active": { background: theme.color.navActivePressBg },
    "& .ant-menu-item:has(a:focus-visible)": {
      outline: `2px solid ${theme.color.navFocus}`,
      outlineOffset: 0,
    },
    "& .ant-menu-item a:focus-visible": { outline: "none" },
    "& .ant-menu:focus-visible": { outline: `2px solid ${theme.color.navFocus}`, outlineOffset: -2 },
    "& .ant-menu-item::after": { display: "none" },
    "& .ant-menu-inline-collapsed .ant-menu-item": {
      paddingInline: 0,
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
    },
  };
}

export function AppSidebar() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const extensionNavItems = useExtensionNavItems();
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
        background: theme.color.surfaceNav,
        // Viewport height and sticky, so the footer stays reachable on long pages.
        position: "sticky",
        top: theme.layout.headerHeight,
        height: `calc(100vh - ${theme.layout.headerHeight}px)`,
        alignSelf: "flex-start",
        display: "flex",
        flexDirection: "column",
        minHeight: 0,
        "& .ant-layout-sider-children": {
          display: "flex",
          flexDirection: "column",
          minHeight: 0,
          background: "transparent",
        },
        ...navRailStyles(theme),
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
            // Colours come from the Menu tokens in both modes, so the light menu is used for both.
            mode="inline"
            inlineIndent={NAV_INSET}
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
            items={section.items.map(navEntry)}
          />
          ) : (
            <section.item.Component
              key={section.key}
              isActive={
                section.item.path !== undefined &&
                isNavPathActive(section.item.path, pathname)
              }
              collapsed={collapsed}
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
