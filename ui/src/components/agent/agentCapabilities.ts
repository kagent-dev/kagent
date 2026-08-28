import type { AgentResponse, AgentSkill, GitSkillRepo, ToolsResponse } from "@/api";

/**
 * What an agent can do, read off its spec and the tool catalogue.
 *
 * Kept apart from the component that draws it because the interesting part is the
 * matching, not the markup: a binding names a server and some tool names, and the
 * descriptions live in a flat list from `GET /tools` keyed by `server_name` and `id`.
 *
 * Two things about that list are worth stating, because getting either wrong shows up as
 * "No description available" against every tool rather than as an error:
 *
 * - `server_name` is a `namespace/name` ref, and a binding may omit its namespace — in
 *   which case it means the agent's own. Comparing the bare names instead would make
 *   two servers of the same name in different namespaces indistinguishable.
 * - `id` is the **bare tool name**, not a qualified one. The in-browser fixtures used to
 *   build it as `ref/name`; a test written against those fixtures passes while nothing
 *   matches on a real cluster, which is the failure this repo has been bitten by before.
 *   Checked against a live controller: `{"id": "helm_uninstall", "server_name":
 *   "kagent/kagent-tool-server", ...}`.
 */

/** One tool an agent may call. */
export interface ResolvedTool {
  name: string;
  /** Absent when the catalogue has no row for it — the server may be unreachable. */
  description?: string;
  requiresApproval: boolean;
}

/** One binding on an agent: a tool server it draws tools from, or another agent. */
export interface ToolBinding {
  /** `namespace/name`, always qualified. */
  ref: string;
  kind: "server" | "agent";
  tools: ResolvedTool[];
  /**
   * The binding named no tools, so it takes whatever the server exposes.
   *
   * Worth saying out loud in the UI: the set is not fixed by the agent, and it grows
   * when the server does.
   */
  takesEverything: boolean;
}

function qualify(namespace: string | undefined, name: string, fallback: string) {
  return `${namespace || fallback}/${name}`;
}

/**
 * The tools an agent can call, grouped by where they come from.
 *
 * `catalogue` is `GET /tools`. While it is still loading, the bindings and tool names are
 * returned with no descriptions rather than nothing at all — the names are on the agent
 * itself and there is no reason to make the reader wait for them.
 */
export function toolBindings(
  agent: AgentResponse,
  catalogue: ToolsResponse[] | undefined,
): ToolBinding[] {
  const agentNamespace = agent.agent.metadata.namespace ?? "";
  const rows = catalogue ?? [];

  return agent.tools.map((tool) => {
    if (tool.agent) {
      // Another agent, delegated to as a tool. It exposes no tool names of its own —
      // the agent *is* the capability — so it is listed as itself.
      return {
        ref: qualify(tool.agent.namespace, tool.agent.name, agentNamespace),
        kind: "agent" as const,
        tools: [],
        takesEverything: false,
      };
    }

    const server = tool.mcpServer;
    const ref = qualify(server?.namespace, server?.name ?? "", agentNamespace);
    const fromServer = rows.filter((row) => row.server_name === ref);
    const approval = new Set(server?.requireApproval ?? []);
    const named = server?.toolNames ?? [];
    const takesEverything = named.length === 0;

    // When the binding names tools, those are the agent's reach — even the ones the
    // catalogue has no row for, which is a server that is not answering rather than a
    // tool the agent cannot call. When it names none it takes the server's whole set,
    // and the catalogue is the only record of what that is.
    const names = takesEverything ? fromServer.map((row) => row.id) : named;

    return {
      ref,
      kind: "server" as const,
      takesEverything,
      tools: names.map((name) => ({
        name,
        description: fromServer.find((row) => row.id === name)?.description || undefined,
        requiresApproval: approval.has(name),
      })),
    };
  });
}

/** Every tool across every binding, for a count that means "reach". */
export function toolCount(bindings: ToolBinding[]): number {
  return bindings.reduce(
    (total, binding) => total + (binding.kind === "agent" ? 1 : binding.tools.length),
    0,
  );
}

/**
 * The capabilities an agent advertises to callers — `a2aConfig.skills`.
 *
 * Distinct from the skill *packages* below. These are what the agent says it does, and
 * carry examples, which are the closest thing to a starting point a reader gets.
 */
export function advertisedSkills(agent: AgentResponse): AgentSkill[] {
  return agent.agent.spec.declarative?.a2aConfig?.skills ?? [];
}

/** Skill packages mounted into the agent, by where they are fetched from. */
export interface SkillSources {
  oci: string[];
  git: GitSkillRepo[];
}

export function skillSources(agent: AgentResponse): SkillSources {
  const skills = agent.agent.spec.declarative?.skills;
  return { oci: skills?.refs ?? [], git: skills?.gitRefs ?? [] };
}

/**
 * An OCI reference split for display: `registry/repo:tag` or `…@digest`.
 *
 * The whole reference is often too long for a panel, and the useful half is the
 * repository and version rather than the registry host. Anything that does not parse is
 * returned whole rather than truncated — a reference the reader cannot recognise is worse
 * than a long one.
 */
export function describeOciRef(ref: string): { name: string; version?: string } {
  const match = /^(?:(?<registry>[^/]+\.[^/]+(?::\d+)?|localhost(?::\d+)?)\/)?(?<repo>[^:@]+)(?::(?<tag>[^@]+))?(?:@(?<digest>.+))?$/.exec(
    ref,
  );
  if (!match?.groups?.repo) return { name: ref };

  const { repo, tag, digest } = match.groups;
  const version = tag ?? (digest ? shorten(digest) : "latest");
  return { name: repo, version };
}

function shorten(digest: string) {
  return digest.length > 19 ? `${digest.slice(0, 19)}…` : digest;
}
