import { createBrowserRouter, Navigate } from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import { AppLayout } from "@/components/Structure/AppLayout";
import { coreNavItems } from "@/components/Structure/navItems";
import { paths } from "./routes";
import type { VendorExtensionConfig } from "@/vendorExtensions";
import { applyNavOverrides } from "@/vendorExtensions";
import { DashboardPage } from "@/pages/DashboardPage";
import { AgentsLandingPage } from "@/pages/agents/AgentsLandingPage";
import { HarnessNewPage } from "@/pages/agents/HarnessNewPage";
import { AgentNewChatPage } from "@/pages/AgentNewChatPage";
import { UnmappedConversationsPage } from "@/pages/UnmappedConversationsPage";
import { AgentPage } from "@/pages/AgentPage";
import { AgentDetailsPage } from "@/pages/AgentDetailsPage";
import { AgentChatPage } from "@/pages/AgentChatPage";

import { AgentTemplateNewPage } from "@/pages/AgentTemplateNewPage";
import { AgentTemplateDetailsPage } from "@/pages/AgentTemplateDetailsPage";
import { ModelsPage } from "@/pages/ModelsPage";
import { ModelNewPage } from "@/pages/ModelNewPage";
import { ModelEditPage } from "@/pages/ModelEditPage";
import { McpServersPage } from "@/pages/McpServersPage";
import { McpServerNewPage } from "@/pages/McpServerNewPage";
import { PromptsPage } from "@/pages/PromptsPage";
import { PromptNewPage } from "@/pages/PromptNewPage";
import { PromptDetailPage } from "@/pages/PromptDetailPage";
import { SubstratePage } from "@/pages/SubstratePage";
import { AppDetailPage } from "@/pages/AppDetailPage";
import { SharedSessionPage } from "@/pages/SharedSessionPage";
import { SharedAgentPage } from "@/pages/SharedAgentPage";
import { LoginPage } from "@/pages/LoginPage";
import { NotFoundPage } from "@/pages/NotFoundPage";

/**
 * Routes inside the app shell, each under a stable key.
 *
 * The keys exist so an extension can say which route it is replacing. Naming
 * the route rather than matching on its path means a replacement keeps working
 * when a path changes, and — more importantly — that a *collision* is still an
 * error: only a contribution that declares what it replaces is allowed to take
 * a path this application already claims.
 */
const coreLayoutRoutes: (RouteObject & { key: string })[] = [
  { key: "dashboard", path: paths.dashboard, element: <DashboardPage /> },
  { key: "agents", path: paths.agents, element: <AgentsLandingPage /> },
  /* One agent, listing its conversations. Four segments, so it cannot collide
     with the two conversation routes below. */
  // Before the two-segment agent routes: `unmapped` is a literal where they expect a
  // namespace, and the router takes the first match.
  { key: "agentsUnmapped", path: paths.agentsUnmapped, element: <UnmappedConversationsPage /> },
  { key: "agent", path: paths.agent, element: <AgentPage /> },
  { key: "agentNewChat", path: paths.agentNewChat, element: <AgentNewChatPage /> },
  /* After the static `/agents/...` segments, which must win over this pattern. */
  { key: "agentDetail", path: paths.agentDetail, element: <AgentDetailsPage /> },
  { key: "agentChat", path: paths.agentChat, element: <AgentChatPage /> },
  { key: "harnessNew", path: paths.harnessNew, element: <HarnessNewPage /> },
  { key: "agentTemplates", path: paths.agentTemplates, element: <Navigate to={`${paths.agents}?tab=templates`} replace /> },
  {
    key: "agentTemplateNew",
    path: paths.agentTemplateNew,
    element: <AgentTemplateNewPage />,
  },
  /* After `agentTemplateNew`, whose single static segment must win over this. */
  {
    key: "agentTemplateDetail",
    path: paths.agentTemplateDetail,
    element: <AgentTemplateDetailsPage />,
  },
  { key: "models", path: paths.models, element: <ModelsPage /> },
  { key: "modelNew", path: paths.modelNew, element: <ModelNewPage /> },
  { key: "modelEdit", path: paths.modelEdit, element: <ModelEditPage /> },
  { key: "mcpServers", path: paths.mcpServers, element: <McpServersPage /> },
  { key: "mcpServerNew", path: paths.mcpServerNew, element: <McpServerNewPage /> },
  { key: "prompts", path: paths.prompts, element: <PromptsPage /> },
  { key: "promptNew", path: paths.promptNew, element: <PromptNewPage /> },
  { key: "promptDetail", path: paths.promptDetail, element: <PromptDetailPage /> },
  { key: "substrate", path: paths.substrate, element: <SubstratePage /> },
  { key: "appDetail", path: paths.appDetail, element: <AppDetailPage /> },
  { key: "sharedSession", path: paths.sharedSession, element: <SharedSessionPage /> },
  { key: "sharedAgent", path: paths.sharedAgent, element: <SharedAgentPage /> },
];

/** Every path the app claims, so a colliding vendor route is rejected. */
export const reservedRoutePaths: readonly string[] = [
  paths.login,
  ...coreLayoutRoutes.map((route) => route.path).filter((path) => path !== undefined),
];

/** The routes an extension may declare a replacement for. */
export const coreRouteKeys: readonly string[] = coreLayoutRoutes.map((r) => r.key);

/**
 * Builds the router with the vendor's pages merged in.
 *
 * Vendor routes are inserted ahead of the catch-all so `*` keeps meaning "not
 * found", and default to rendering inside the shell — a vendor page is a page
 * of this app, not a separate site. `standalone` opts out for full-screen
 * flows.
 */
export function createAppRouter(config: VendorExtensionConfig) {
  const vendorRoutes = config.routes ?? [];

  // A contribution that declares `replaces` takes the named route's place, so
  // the original is dropped rather than both matching the same path.
  const replaced = new Set(
    vendorRoutes.map((route) => route.replaces).filter((key) => key !== undefined),
  );
  // Whatever the extension said about one of these routes (`routeHandles`, keyed
  // by route key) rides along on its `handle`, which is where `useMatches` hands
  // it back. Passed through unread: the application does not define the shape, so
  // it has nothing to say about it, and a route nobody described carries nothing.
  const remainingCoreRoutes = coreLayoutRoutes
    .filter((route) => !replaced.has(route.key))
    .map((route) => {
      const handle = config.routeHandles?.[route.key];
      return handle ? { ...route, handle } : route;
    });

  // A replacement owns the chrome AND the frame the pages render into, so it
  // stands in for `AppLayout` here rather than nesting inside it. It is handed
  // the navigation so it renders this application's pages rather than a copy.
  const Layout = config.shell?.Layout;
  const shell = Layout ? (
    <Layout
      coreNavItems={applyNavOverrides(coreNavItems, config.navOverrides)}
      vendorNavItems={config.navItems ?? []}
    />
  ) : (
    <AppLayout />
  );

  return createBrowserRouter([
    { path: paths.login, element: <LoginPage /> },
    ...vendorRoutes
      .filter((route) => route.standalone)
      .map(({ path, element }) => ({ path, element })),
    {
      element: shell,
      children: [
        ...remainingCoreRoutes,
        ...vendorRoutes
          .filter((route) => !route.standalone)
          .map(({ path, element, handle }) => ({
            path,
            element,
            ...(handle ? { handle } : {}),
          })),
        { path: "*", element: <NotFoundPage /> },
      ],
    },
  ]);
}
