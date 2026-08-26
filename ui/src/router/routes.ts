/**
 * Every route path in one place, so links are never hand-written strings and a
 * rename is a single edit.
 */
export const paths = {
  dashboard: "/",
  login: "/login",

  /*
   * Agents, which are `(AgentTemplate, Harness)` pairs.
   *
   * There is no separate "agent instances" page: an `AgentInstance` is one
   * *conversation* with an agent, so instances are listed inside an agent rather
   * than beside them. See `api/domain/agentPairs` for why the pair is the agent.
   *
   * There is no `agentEdit` either. A pair has no spec of its own — what the agent
   * *is* lives on its `AgentTemplate` and how it *runs* on its `Harness` — so
   * changing an agent means changing one of those, which is what the template link
   * on the agent's page is for.
   */
  agents: "/agents",
  /*
   * One agent, listing its conversations.
   *
   * Four segments with a static `on` in the middle, which is both what it reads as
   * — "k8s-agent-7f3a91c on k8s-agent", the same wording as the templates page's
   * "Runs on" column — and what keeps it out of `agentChat`'s way. Three dynamic
   * segments would have collided with `/agents/:namespace/:id/chat`, where only the
   * literal `chat` distinguishes them, and a harness that happened to be called
   * `chat` would then have had no page at all.
   */
  agent: "/agents/:namespace/:agentTemplate/on/:harness",
  /*
   * A conversation with this agent that does not exist yet.
   *
   * Addressed by the *agent* rather than by an instance, because there is no instance
   * — that is the whole point. Clicking "New chat" used to call
   * `CreateAgentInstance` and navigate to the result, so every visit that changed its
   * mind left a permanent empty conversation behind. The live cluster carries nine of
   * them, all unnamed, all with no messages, and each holding a prepared revision that
   * is not collected when the last instance referencing it goes.
   *
   * So the instance is created by the first message instead, and this route is where a
   * reader waits in the meantime. It redirects to `agentChat` as soon as the create
   * returns.
   */
  agentNewChat: "/agents/:namespace/:agentTemplate/on/:harness/new",
  /*
   * Conversations that belong to no agent.
   *
   * A single flat address rather than one beneath an agent, because there is no agent
   * to put it under — that is the whole condition. Two segments, so it cannot collide
   * with `/agents/:namespace/:id`: `unmapped` is a literal and an instance id is a
   * UUID, and the router is given this one first.
   */
  agentsUnmapped: "/agents/unmapped",
  /*
   * One conversation's record.
   *
   * Two segments, so it cannot be confused with `/agents/new` — which is one, and
   * which the router must still be given first for the reader who types it.
   *
   * Addressed directly under `/agents` rather than beneath its agent, and kept that
   * way deliberately: an instance is addressed as `(namespace, id)` on every RPC,
   * every share link issued so far points here, and nesting it would have made the
   * pair part of an address the API does not need. The page links *up* to its agent
   * instead.
   */
  agentDetail: "/agents/:namespace/:id",
  /*
   * The conversation with one agent.
   *
   * No session segment, because there is nothing to put in one: the instance *is*
   * the conversation. The A2A gateway files every task under the instance as its
   * `contextId`, so a second conversation with the same template and harness is a
   * second instance rather than a second session under this one.
   */
  agentChat: "/agents/:namespace/:id/chat",

  /*
   * Agent templates: the behaviour half an agent is cut from.
   *
   * Below the agents in the navigation because that is the order a reader meets
   * them — you come looking for an agent, and a template is what one is made of.
   */
  agentTemplates: "/agent-templates",
  agentTemplateNew: "/agent-templates/new",
  harnessNew: "/harnesses/new",
  /*
   * One template, read first.
   *
   * After `new`, whose single static segment must win over this pattern.
   *
   * This address used to be the edit form, so clicking a row to *look* at a
   * template dropped the reader into a page of inputs with Save waiting. It is a
   * details page now, with editing as a mode of it — there is no separate edit
   * address, because "am I reading or writing this" is a state of the page rather
   * than a place to be.
   */
  agentTemplateDetail: "/agent-templates/:namespace/:name",

  models: "/models",
  modelNew: "/models/new",
  modelEdit: "/models/:namespace/:name/edit",

  mcpServers: "/mcp",
  mcpServerNew: "/mcp/new",

  prompts: "/prompts",
  promptNew: "/prompts/new",
  promptDetail: "/prompts/:namespace/:name",

  substrate: "/substrate",

  appDetail: "/apps/:appName",

  /*
   * A conversation opened through a share link.
   *
   * The token is in the path because that is what makes the link a link — it is the
   * whole credential, and one a reader forwards by copying the address bar. Which
   * conversation it is for is in the path too: the token identifies the share to the
   * backend, but the app has to know what to ask for before it can spend it.
   */
  sharedSession: "/shared/:sessionId/:token",

  /*
   * A conversation opened through a share link.
   *
   * Addressed by the instance, because the instance *is* the conversation. The
   * token is in the path for the reason the session one is: it is the whole
   * credential, and one a reader forwards by copying the address bar.
   *
   * `sharedSession` above is kept for the links issued before this existed. It reads
   * a session's stored turns; this one reads the instance's through the A2A gateway.
   */
  sharedAgent: "/shared/agent/:namespace/:id/:token",
} as const;

/**
 * Where the templates list actually is.
 *
 * `paths.agentTemplates` is a redirect, kept so an address somebody already has still
 * resolves. Navigating through it from inside the app costs a render on a URL that is
 * replaced immediately — and anything watching the address, a test most of all, can
 * lose the race against that replacement and wait for a URL that no longer exists.
 * This is the address the reader ends up at, so it is the one to navigate to.
 */
export const agentTemplatesTab = `${paths.agents}?tab=templates`;

export function buildPath(
  path: string,
  params: Record<string, string | undefined>,
): string {
  return path.replace(/:([A-Za-z0-9_]+)/g, (_match, key: string) => {
    const value = params[key];
    if (value === undefined) {
      throw new Error(`buildPath: missing value for ":${key}" in "${path}"`);
    }
    return encodeURIComponent(value);
  });
}
