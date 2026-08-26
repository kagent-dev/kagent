import { css } from "@emotion/react";
import type { VendorExtensionConfig } from "@/vendorExtensions";
import { ExampleInsightsPage } from "./ExampleInsightsPage";
import { ExampleNavItem } from "./ExampleNavItem";
import { ExampleTenantProvider } from "./ExampleTenantProvider";
import { exampleComplianceTierField } from "./exampleFormFields";
import { exampleAgentRegionColumn } from "./exampleTableColumns";
import { EXAMPLE_INSIGHTS_PATH } from "./paths";
import {
  ExampleAgentBadge,
  ExampleAgentsHeaderAction,
  ExampleDashboardCard,
  ExampleMessageAction,
  ExampleOverlayWidget,
  ExamplePolicyBanner,
  ExampleSidebarFooter,
} from "./ExampleSlots";

/**
 * A worked example of the extension contract, exercising every capability the
 * framework offers. "Example" is a fictional vendor — this ships as documentation
 * you can run, not as a feature of the application.
 *
 * Nothing here is special-cased by the framework: a real extension is a config
 * object of exactly this shape, and installing it is one edit in
 * `src/vendorExtensions/activeConfig.ts`.
 */
export const exampleVendorExtension: VendorExtensionConfig = {
  id: "example",
  name: "Example Agent Platform",

  // Site-wide: a nav entry positioned between Agents (200) and Models (300).
  navItems: [
    {
      key: "exampleInsights",
      order: 250,
      path: EXAMPLE_INSIGHTS_PATH,
      Component: ExampleNavItem,
    },
  ],

  // Site-wide: a whole page merged into the router.
  routes: [{ path: EXAMPLE_INSIGHTS_PATH, element: <ExampleInsightsPage /> }],

  // Per-point components. Keys are checked against the extension point union,
  // and each component against that point's context contract.
  slots: {
    app_shell_appLayout_contentArea_leadingBanner: ExamplePolicyBanner,
    app_shell_appLayout_contentArea_globalOverlay: ExampleOverlayWidget,
    app_shell_appLayout_appSidebar_footer: ExampleSidebarFooter,
    app_agents_agentsList_pageHeader_actions: ExampleAgentsHeaderAction,
    app_agents_agentsList_agentListItem_badge: ExampleAgentBadge,
    app_agents_agentChat_agentChatMessage_additionalActionsButton: ExampleMessageAction,
    app_dashboard_dashboardOverview_summaryGrid_leadingCard: ExampleDashboardCard,
  },

  // A field added to a core form, mapped into the vendor's own CRD shape.
  formFields: [exampleComplianceTierField],

  // A column on a core table. The application has no concept of the dimension
  // this adds, which is the case a column contribution exists for.
  tableColumns: [exampleAgentRegionColumn],

  // Restyling the host, not just the contributions. The application's own pages
  // pick these up because every one of its components reads its colours and
  // radii from the tokens — nothing here touches a core component.
  theme: {
    tokens: {
      color: { primary: "#0084c0", primaryHover: "#006ba6" },
      radius: { sm: 2, md: 4, lg: 6 },
    },
    globalStyles: css`
      /* What tokens cannot express: a gradient rule under the header, to show
         that an extension reaches styling the application has no token for. */
      [data-testid="app-header"] {
        border-bottom: 1px solid transparent;
        border-image: linear-gradient(90deg, #0084c0, #79d4f8) 1;
      }
    `,
  },

  // Endpoint overrides and payload reshaping, folded into the data layer's
  // registry by `installVendorApiExtension`.
  //
  // Only a transform here, deliberately. `baseUrl` and `endpoints` are part of
  // the contract — a vendor pointing `agents.list` at `/managed-agents` on
  // their own host is exactly the case it exists for — but setting either here
  // would send every call somewhere the mock backend does not answer, so the
  // example would break the app whenever it is switched on. A header is real,
  // observable in the network panel, and harmless.
  api: {
    transforms: {
      "agents.list": {
        request: (context) => ({
          ...context,
          headers: { ...context.headers, "x-example-tenant": "example-eu-1" },
        }),
      },
    },
  },

  // App-level context providers, outermost first.
  providers: [ExampleTenantProvider],
};
