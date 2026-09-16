/**
 * Helpers for the live suite.
 *
 * Only what is genuinely live-specific. Anything that works on either backend lives in
 * `helpers/app.ts` instead, so a spec in `shared/` can reach it — `loadApp`,
 * `expectNoLoadFailure`, `rowNamed` and `dataRows` all moved there, and this file had
 * its own slightly different copies of the last two for no reason anybody could name.
 */

/**
 * The pages the live sweep visits, which is not every route the app has.
 *
 * Its own table rather than `routes` from `helpers/app.ts`: that one is every address a
 * spec might drive, including forms and detail pages that need an id. This is the set of
 * *landing pages* worth loading against a cluster, and `pages.spec.ts` iterates it.
 *
 * Mirrors `src/router/routes.ts` rather than importing it, like the mock suite's table:
 * a spec that reads the app's own constant follows a rename silently. The copy can rot
 * instead — this one carried `/agents/new` for a form that had been deleted, and
 * `agentDetail` (`/agents/:id`) swallowed the address so it was not even a 404.
 */
export const liveRoutes = {
  dashboard: "/",
  agents: "/agents",
  /* A tab of the agents page. `/agent-templates` still redirects here, but a spec
     should go where the reader goes. */
  agentTemplates: "/agents?tab=templates",
  agentTemplateNew: "/agent-templates/new",
  models: "/models",
  mcpServers: "/mcp",
  prompts: "/prompts",
  schedules: "/schedules",
  substrate: "/substrate",
} as const;
