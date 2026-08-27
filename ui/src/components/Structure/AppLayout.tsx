import { Layout } from "antd";
import { useTheme } from "@emotion/react";
import { Outlet } from "react-router-dom";
import { AppHeader } from "./AppHeader";
import { AppSidebar } from "./AppSidebar";
import { coreNavItems } from "./navItems";
import {
  VendorSlot,
  applyNavOverrides,
  useVendorExtensionConfig,
  useVendorNavItems,
} from "@/vendorExtensions";

const { Content } = Layout;

export function AppLayout() {
  const theme = useTheme();
  const { shell, navOverrides } = useVendorExtensionConfig();
  const vendorNavItems = useVendorNavItems();

  // A replacement owns the region outright, including rendering the app's own
  // navigation — which is why it is handed `coreNavItems` rather than having to
  // keep a copy that drifts as pages are added.
  const Sidebar = shell?.Sidebar;
  const Header = shell?.Header ?? AppHeader;

  return (
    /*
     * A wash of the brand colour from the top right, and nothing more than a wash.
     *
     * Fixed rather than scrolling with the page, so it reads as light falling on the
     * app rather than as an image the content moves over — a gradient that scrolls
     * away gives the second screenful of a long page a different background from the
     * first. `background-attachment` would do it on a normal element but not reliably
     * on the scrolling root, so it is painted here and pinned.
     *
     * Kept very weak on purpose: this sits under every page in the product, so
     * anything strong enough to notice on the dashboard is too strong behind a table
     * of a hundred rows. The stop is placed so the colour is gone well before the
     * lower left, where the densest reading usually is.
     */
    <Layout
      css={{
        minHeight: "100vh",
        backgroundAttachment: "fixed",
        backgroundImage: `radial-gradient(120% 90% at 100% 0%, ${theme.color.primary}22 0%, ${theme.color.primary}0D 35%, transparent 70%)`,
      }}
    >
      <Header />
      <Layout>
        {Sidebar ? (
          <Sidebar
            coreNavItems={applyNavOverrides(coreNavItems, navOverrides)}
            vendorNavItems={vendorNavItems}
          />
        ) : (
          <AppSidebar />
        )}
        {/* No `overflow: auto` here. It made the content area its own scroll box, so
            a long page scrolled *inside* the layout and left the sidebar pinned —
            two scrollbars, and a sidebar whose own footer could never be reached on
            a short viewport. The document scrolls instead, and the sidebar goes with
            it, which is what every page on the web does. */}
        <Content data-testid="app-content" css={{ padding: theme.space(6) }}>
          <VendorSlot id="app_shell_appLayout_contentArea_leadingBanner" />
          <Outlet />
        </Content>
      </Layout>
      {/* Portalled to the document root, so an overlay is never clipped by, or
          stacked beneath, anything in the layout it was opened from. */}
      <VendorSlot id="app_shell_appLayout_contentArea_globalOverlay" />
    </Layout>
  );
}
