/**
 * What the mock backend has been *told*, as opposed to what it was seeded with.
 *
 * The fixtures in `fixtures.ts` are constants; this is the layer that makes the
 * backend behave like one. A create that reports success while the list it
 * belongs to stays empty is its own kind of lie, and it is the one a form is most
 * likely to ship with — so writes are recorded here and the reads in
 * `transport.ts` fold them in.
 *
 * Everything is in memory, so it starts empty for every browser context: one
 * spec's creates cannot leak into the next one's list.
 */

import type {
  Agent,
  AgentCreateRequest,
  AgentKindName,
  AgentResponse,
} from "@/api/domain/agents";
import type { ModelConfig, ModelConfigSpec } from "@/api/domain/models";
import type { ToolServerResponse } from "@/api/domain/mcpServers";
import type {
  PromptTemplateDetail,
  PromptTemplateSummary,
} from "@/api/domain/prompts";
import type {
  AgentInstance,
  AgentInstanceShare,
  AgentInstanceSharePermission,
} from "@/api/domain/agentInstances";
import type { Harness } from "@/api/domain/harnesses";
import type { AgentTemplate } from "@/api/domain/agentTemplates";
import { admitsLabels } from "@/api/domain/harnesses";
import type { Session, SessionShare } from "@/api/domain/sessions";
import {
  mockAgentInstances,
  mockAgentTemplates,
  mockHarnesses,
  mockAgents,
  mockMcpServers,
  mockModels,
  mockPromptDetails,
  mockPrompts,
  mockSessions,
} from "./fixtures";

/** What has been written during this browsing session. */
const created = {
  agents: [] as AgentResponse[],
  models: [] as ModelConfig[],
  mcpServers: [] as ToolServerResponse[],
  prompts: [] as PromptTemplateDetail[],
  sessions: [] as Session[],
  agentInstances: [] as AgentInstance[],
  agentTemplates: [] as AgentTemplate[],
  harnesses: [] as Harness[],
};

/** Refs deleted during this browsing session, so a delete is visible too. */
const deleted = new Set<string>();

const isLive = (ref: string) => !deleted.has(ref);

/** Records a delete. The resource stops appearing in the list it belonged to. */
export function markDeleted(ref: string): void {
  deleted.add(ref);
}

/** Keeps the last entry for each ref — later writes shadow earlier fixtures. */
function dedupeByRef<T>(rows: readonly T[], refOf: (row: T) => string): T[] {
  const byRef = new Map<string, T>();
  for (const row of rows) byRef.set(refOf(row), row);
  return [...byRef.values()];
}

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

export const agentRef = (row: AgentResponse) =>
  `${row.agent.metadata.namespace ?? ""}/${row.agent.metadata.name}`;

/**
 * Every agent, deduped, so an edit to a *fixture* agent shadows it rather than
 * sitting beside it.
 *
 * Without the dedupe the list showed the same agent twice after a save and a read
 * answered with the original, because `find` returns the first match and the
 * fixtures come first — so the edit form reopened showing the values the user had
 * just replaced, and the feature looked broken when only the fixture was.
 */
export function allAgents(): AgentResponse[] {
  return dedupeByRef([...mockAgents, ...created.agents], agentRef).filter((row) =>
    isLive(agentRef(row)),
  );
}

/**
 * Records a create or an edit and answers with the row a list would now show.
 *
 * An edit to a seeded agent is recorded as an addition rather than by mutating the
 * fixture, which is what lets the list show the new values while the fixtures stay
 * constants.
 */
export function saveAgent(row: AgentResponse): AgentResponse {
  const ref = agentRef(row);
  const at = created.agents.findIndex((existing) => agentRef(existing) === ref);
  if (at === -1) created.agents.push(row);
  else created.agents[at] = row;
  // A resource written again after being deleted exists again, which is what the
  // cluster would say too.
  deleted.delete(ref);
  return row;
}

/**
 * The row the controller would have reported for a freshly written agent.
 *
 * `model` and `modelProvider` are resolved from the referenced ModelConfig here
 * for the same reason the controller resolves them: they are not in the resource,
 * and a new row that left them blank would read differently from every other row
 * in the same list.
 */
export function buildAgentResponse(
  draft: AgentCreateRequest,
  kind: AgentKindName,
): AgentResponse {
  const now = new Date().toISOString();

  const agent: Agent = {
    apiVersion: draft.apiVersion ?? "kagent.dev/v1alpha3",
    kind: draft.kind ?? kind,
    metadata: {
      ...draft.metadata,
      creationTimestamp: now,
      resourceVersion: `${30_000 + created.agents.length}`,
    },
    spec: draft.spec,
    status: {
      observedGeneration: 1,
      conditions: [
        {
          type: "Ready",
          status: "True",
          reason: "DeploymentReady",
          lastTransitionTime: now,
        },
      ],
    },
  };

  return {
    id: `created-${created.agents.length + 1}`,
    agent,
    ...resolveModel(draft.spec.declarative?.modelConfig, draft.metadata.namespace),
    modelConfigRef: draft.spec.declarative?.modelConfig ?? "",
    tools: draft.spec.declarative?.tools ?? [],
    memoryRefs: [],
    deploymentReady: true,
    accepted: true,
    agentKind: kind,
  };
}

/**
 * The model behind a `modelConfig` reference.
 *
 * The CRD stores it bare — "must be in the same namespace as the Agent" — while
 * the fixtures' own refs are namespaced, so the agent's namespace is what joins
 * the two. A ref that is already namespaced is taken as it stands.
 */
function resolveModel(
  ref: string | undefined,
  namespace: string | undefined,
): { model: string; modelProvider: string } {
  if (!ref) return { model: "", modelProvider: "" };

  const qualified = ref.includes("/") ? ref : `${namespace ?? ""}/${ref}`;
  const config = allModels().find((candidate) => candidate.ref === qualified);
  return {
    model: config?.spec.model ?? "",
    modelProvider: config?.spec.provider ?? "",
  };
}

// ---------------------------------------------------------------------------
// Model configurations
// ---------------------------------------------------------------------------

export function allModels(): ModelConfig[] {
  return dedupeByRef([...mockModels, ...created.models], (model) => model.ref).filter(
    (row) => isLive(row.ref),
  );
}

export function saveModel(ref: string, spec: ModelConfigSpec): ModelConfig {
  // The API key is write-only: accepted, never echoed back — which is why it is
  // not a parameter here.
  const model: ModelConfig = { ref, spec };
  const at = created.models.findIndex((existing) => existing.ref === ref);
  if (at === -1) created.models.push(model);
  else created.models[at] = model;
  deleted.delete(ref);
  return model;
}

// ---------------------------------------------------------------------------
// Tool servers
// ---------------------------------------------------------------------------

export function allToolServers(): ToolServerResponse[] {
  return dedupeByRef(
    [...mockMcpServers, ...created.mcpServers],
    (server) => server.ref,
  ).filter((row) => isLive(row.ref));
}

/**
 * Records a created tool server and answers with the row a list would show.
 *
 * Takes the type and the resource's metadata rather than the whole create request,
 * because that is all the RPC carries that identifies the server — and the two
 * server kinds keep their metadata in the same place.
 */
export function saveToolServer(
  type: string,
  metadata: { name?: string; namespace?: string } | undefined,
): ToolServerResponse {
  const server: ToolServerResponse = {
    ref: `${metadata?.namespace ?? "kagent"}/${metadata?.name ?? "unnamed"}`,
    groupKind: `${type}.kagent.dev`,
    // Empty until the controller has handshaken with the server, which is the
    // honest state immediately after a create. Claiming otherwise would put tools
    // on screen that nothing has confirmed exist.
    discoveredTools: [],
  };

  const at = created.mcpServers.findIndex((existing) => existing.ref === server.ref);
  if (at === -1) created.mcpServers.push(server);
  else created.mcpServers[at] = server;
  deleted.delete(server.ref);
  return server;
}

// ---------------------------------------------------------------------------
// Prompt libraries
// ---------------------------------------------------------------------------

export const promptRef = (row: { namespace: string; name: string }) =>
  `${row.namespace}/${row.name}`;

/** Every library, with a written copy shadowing the seeded one it edited. */
export function allPromptDetails(): PromptTemplateDetail[] {
  return dedupeByRef(
    [...Object.values(mockPromptDetails), ...created.prompts],
    promptRef,
  ).filter((row) => isLive(promptRef(row)));
}

export function allPromptSummaries(): PromptTemplateSummary[] {
  return dedupeByRef(
    [...mockPrompts, ...created.prompts.map(promptSummary)],
    promptRef,
  ).filter((row) => isLive(promptRef(row)));
}

const promptSummary = (detail: PromptTemplateDetail): PromptTemplateSummary => ({
  namespace: detail.namespace,
  name: detail.name,
  keyCount: Object.keys(detail.data).length,
  keys: Object.keys(detail.data),
});

export function savePrompt(detail: PromptTemplateDetail): PromptTemplateDetail {
  const ref = promptRef(detail);
  const at = created.prompts.findIndex((existing) => promptRef(existing) === ref);
  if (at === -1) created.prompts.push(detail);
  else created.prompts[at] = detail;
  deleted.delete(ref);
  return detail;
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

export function allSessions(): Session[] {
  return [...mockSessions, ...created.sessions].filter((row) => isLive(row.id));
}

export function saveSession(request: {
  id?: string;
  agentRef?: string;
  name?: string;
}): Session {
  const now = new Date().toISOString();
  const session: Session = {
    id: request.id || `session-${created.sessions.length + 1}-mock`,
    name: request.name ?? "New conversation",
    // The `namespace__NS__name` form the chat client splits on, from the
    // `namespace/name` ref a create sends.
    agent_id: (request.agentRef || "kagent/k8s-agent").replace("/", "__NS__"),
    user_id: "admin@kagent.dev",
    created_at: now,
    updated_at: now,
    deleted_at: "",
  };
  created.sessions.push(session);
  return session;
}

// ---------------------------------------------------------------------------
// Share links
// ---------------------------------------------------------------------------

/**
 * Share links created in this tab.
 *
 * In `sessionStorage` rather than a module variable, which is what the other
 * writes here use. A share link is spent by *opening* it, and opening it is a full
 * page load — so a module variable meant the token stopped existing at the moment
 * it was used, and the one flow this fixture exists to support was the one flow it
 * could not serve. Still per-tab and still gone when the tab closes, so nothing
 * leaks between runs.
 */
const SHARES_KEY = "kagent.mock.shares";

/**
 * A share that was issued before this tab opened.
 *
 * Seeded because nothing in the UI mints one any more: chat is addressed by
 * `AgentInstance`, and an instance share would hand out a token no read path
 * validates (the interceptor resolves `X-Share-Token` through
 * `GetSessionShareByToken` and produces a share context naming a *session*). The
 * links already issued still have to open, so this is one of them — the fixture
 * equivalent of a link somebody was sent last week.
 */
export const SEEDED_SHARE: SessionShare = {
  id: 1,
  token: "mock-share-token-1",
  session_id: "session-8f31",
  user_id: "alice",
  read_only: true,
  created_at: "2026-08-01T09:00:00Z",
};

export function readShares(): SessionShare[] {
  try {
    const stored = window.sessionStorage.getItem(SHARES_KEY);
    // Absent, not empty: a tab that has revoked the seeded share stores `[]`, and
    // re-seeding it there would make a revoke impossible to observe.
    if (stored === null) return [SEEDED_SHARE];
    return JSON.parse(stored) as SessionShare[];
  } catch {
    return [];
  }
}

function writeShares(rows: SessionShare[]): void {
  try {
    window.sessionStorage.setItem(SHARES_KEY, JSON.stringify(rows));
  } catch {
    // Storage can be refused. The list is then empty for this load, which is the
    // same answer as a tab that has created nothing.
  }
}

export function createShare(sessionId: string, readOnly: boolean): SessionShare {
  const existing = readShares();
  const share: SessionShare = {
    id: existing.length + 1,
    // Long enough to look like the controller's hex, and obviously fake.
    token: `mock-share-token-${existing.length + 1}`,
    session_id: sessionId,
    user_id: "alice",
    read_only: readOnly,
    created_at: new Date().toISOString(),
  };
  writeShares([...existing, share]);
  return share;
}

export function deleteShare(token: string): void {
  writeShares(readShares().filter((share) => share.token !== token));
}

// ---------------------------------------------------------------------------
// Agent templates
// ---------------------------------------------------------------------------

export const agentTemplateRef = (row: AgentTemplate) => `${row.namespace}/${row.name}`;

/**
 * Every agent template, with anything written since the page loaded folded in.
 *
 * Deduped the way agents are, so an edit to a *fixture* template shadows it rather
 * than sitting beside it — without that, a saved template appears twice and a read
 * answers with the original, so the form reopens showing the values just replaced.
 */
/** How a harness is addressed. */
export const harnessRef = (row: Harness) => `${row.namespace}/${row.name}`;

/**
 * Every harness, with anything created or deleted folded in.
 *
 * Deduped and filtered the same way templates are: a written harness is recorded as a
 * new entry rather than by mutating the fixture, so a created one appears once and a
 * deleted one stops appearing, while `fixtures.ts` stays a file of constants.
 */
export function allHarnesses(): Harness[] {
  return dedupeByRef([...mockHarnesses, ...created.harnesses], harnessRef).filter((row) =>
    isLive(harnessRef(row)),
  );
}

/** Records a written harness, so the next list read answers with it. */
export function saveHarness(row: Harness): Harness {
  const at = created.harnesses.findIndex((existing) => harnessRef(existing) === harnessRef(row));
  if (at === -1) created.harnesses.push(row);
  else created.harnesses[at] = row;
  return row;
}

export function allAgentTemplates(): AgentTemplate[] {
  return dedupeByRef(
    [...mockAgentTemplates, ...created.agentTemplates],
    agentTemplateRef,
  ).filter((row) => isLive(agentTemplateRef(row)));
}

/**
 * Records a written template, recomputing which harnesses admit it.
 *
 * Admission is derived rather than stored, because on a cluster it *is* derived:
 * the controller matches each Harness's label selector against the template's
 * labels and writes the result into status. A fixture that let a caller assert
 * `admittingHarnesses` directly would happily accept a template whose labels admit
 * nothing while reporting that a harness would run it — which is the exact
 * confusion this form exists to prevent.
 */
export function saveAgentTemplate(row: AgentTemplate): AgentTemplate {
  const labels = row.resource.metadata.labels ?? {};
  const admitting = mockHarnesses
    // Admission never crosses a namespace: a Harness selects templates beside it,
    // so a fixture that matched on labels alone would admit a template the
    // controller never would — and the agents page reads admission to decide what
    // exists at all.
    .filter((harness) => harness.namespace === row.namespace)
    // A harness with no selector admits none — `admitsLabels` is the one place
    // that rule lives, shared with the form's preview so the two cannot disagree.
    .filter((harness) => admitsLabels(harness, labels))
    .map((harness) => harness.name);

  const stored: AgentTemplate = {
    ...row,
    admittingHarnesses: admitting.map((harness) => harness),
    resource: {
      ...row.resource,
      /*
       * The status the controller would write, written here too.
       *
       * `admittingHarnesses` is *derived from* this on a cluster — the service reads
       * `status.harnesses[].harness` — so a fixture that filled one and not the
       * other would let the two disagree, and the agents page reads the status half
       * because it carries the revision as well as the name. Whichever half a test
       * looked at would then be the half that was right.
       *
       * A ready harness gets a successful revision; one the controller has not
       * observed gets only a desired one, which is the "preparing" state a
       * freshly-labelled template really passes through.
       */
      status: {
        ...row.resource.status,
        harnesses: admitting.map((harnessName) => {
          const harness = mockHarnesses.find(
            (candidate) =>
              candidate.namespace === row.namespace && candidate.name === harnessName,
          );
          const revision = `rev-${row.name}-${harnessName}`;
          return harness?.ready
            ? {
                harness: harnessName,
                desiredRevision: revision,
                latestSuccessfulRevision: revision,
              }
            : {
                harness: harnessName,
                desiredRevision: revision,
                conditions: [
                  {
                    type: "Ready",
                    status: "False",
                    reason: "HarnessNotReady",
                    message: `The ${harnessName} harness has not reported ready, so no revision has been built yet.`,
                  },
                ],
              };
        }),
      },
    },
  };
  const ref = agentTemplateRef(stored);
  const at = created.agentTemplates.findIndex(
    (existing) => agentTemplateRef(existing) === ref,
  );
  if (at === -1) created.agentTemplates.push(stored);
  else created.agentTemplates[at] = stored;
  return stored;
}

// ---------------------------------------------------------------------------
// Agent instance shares
// ---------------------------------------------------------------------------

/**
 * Where share links live between page loads.
 *
 * `sessionStorage` for the reason the session shares use it: a share is created on
 * one page and *spent* on another, which is a full page load — a module variable
 * would mean the token stopped existing at the moment it was used, and the one flow
 * this exists to support would be the one flow it could not serve.
 */
const INSTANCE_SHARES_KEY = "kagent.mock.instanceShares";

/**
 * A share issued before this tab opened.
 *
 * Seeded so a link can be *spent* without first being created: opening one is a
 * full page load, and the flow this exists to support is the one where somebody was
 * sent a link last week. Its token is `SEEDED_INSTANCE_SHARE_TOKEN`.
 */
export const SEEDED_INSTANCE_SHARE: AgentInstanceShare = {
  id: "mock-instance-share-seed",
  namespace: "kagent",
  agentInstanceId: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
  permission: "readOnly",
  createdAt: "2026-08-01T09:00:00Z",
};

export const SEEDED_INSTANCE_SHARE_TOKEN = "mock-instance-token-seed";

export function readInstanceShares(): AgentInstanceShare[] {
  try {
    const stored = window.sessionStorage.getItem(INSTANCE_SHARES_KEY);
    // Absent, not empty: a tab that has revoked the seeded share stores `[]`, and
    // re-seeding it there would make a revoke impossible to observe.
    if (stored === null) return [SEEDED_INSTANCE_SHARE];
    return JSON.parse(stored) as AgentInstanceShare[];
  } catch {
    return [];
  }
}

function writeInstanceShares(rows: AgentInstanceShare[]): void {
  try {
    window.sessionStorage.setItem(INSTANCE_SHARES_KEY, JSON.stringify(rows));
  } catch {
    // Storage can be refused; the list is then empty for this load, which is the
    // same answer as a tab that has created nothing.
  }
}

/** The token a share was issued with, so the fixture can honour the link. */
const instanceShareTokens = new Map<string, string>();
const TOKENS_KEY = "kagent.mock.instanceShareTokens";

function readTokens(): Record<string, string> {
  const seeded = { [SEEDED_INSTANCE_SHARE_TOKEN]: SEEDED_INSTANCE_SHARE.id };
  try {
    return {
      ...seeded,
      ...(JSON.parse(window.sessionStorage.getItem(TOKENS_KEY) ?? "{}") as Record<
        string,
        string
      >),
    };
  } catch {
    return seeded;
  }
}

export function createInstanceShare(
  namespace: string,
  agentInstanceId: string,
  permission: AgentInstanceSharePermission,
): { share: AgentInstanceShare; token: string } {
  const existing = readInstanceShares();
  const share: AgentInstanceShare = {
    id: `mock-share-${existing.length + 1}`,
    namespace,
    agentInstanceId,
    permission,
    createdAt: new Date().toISOString(),
  };
  // Obviously fake, and long enough to look like the controller's own.
  const token = `mock-instance-token-${existing.length + 1}`;
  writeInstanceShares([...existing, share]);
  const tokens = { ...readTokens(), [token]: share.id };
  instanceShareTokens.set(token, share.id);
  try {
    window.sessionStorage.setItem(TOKENS_KEY, JSON.stringify(tokens));
  } catch {
    // See writeInstanceShares.
  }
  return { share, token };
}

/** The share a token names, or `undefined` — which is a refusal, not an empty read. */
export function instanceShareForToken(token: string): AgentInstanceShare | undefined {
  const id = readTokens()[token] ?? instanceShareTokens.get(token);
  if (!id) return undefined;
  return readInstanceShares().find((share) => share.id === id);
}

export function revokeInstanceShare(shareId: string): boolean {
  const existing = readInstanceShares();
  const remaining = existing.filter((share) => share.id !== shareId);
  if (remaining.length === existing.length) return false;
  writeInstanceShares(remaining);
  return true;
}

// ---------------------------------------------------------------------------
// Agent instances
// ---------------------------------------------------------------------------

/**
 * How an instance is addressed, and it is not a resource ref.
 *
 * `namespace/id` because that is the pair `AgentInstanceService` takes on every
 * call — an instance has no name, and the id is a UUID scoped to its namespace.
 */
export const agentInstanceRef = (row: AgentInstance) => `${row.namespace}/${row.id}`;

/**
 * Every instance, with anything suspend or resume has done to it folded in.
 *
 * Deduped the same way agents are, and for the same reason: a lifecycle operation
 * is recorded as a new entry rather than by mutating the fixture, so the list shows
 * the new state while `fixtures.ts` stays a file of constants. Without the dedupe a
 * suspended instance would appear twice — once running and once suspended — which
 * is the most confusing possible answer to "did that work?".
 */
export function allAgentInstances(): AgentInstance[] {
  return dedupeByRef(
    [...mockAgentInstances, ...created.agentInstances],
    agentInstanceRef,
  ).filter((row) => isLive(agentInstanceRef(row)));
}

/** Records what a lifecycle operation left behind, and answers with it. */
export function saveAgentInstance(row: AgentInstance): AgentInstance {
  const ref = agentInstanceRef(row);
  const at = created.agentInstances.findIndex(
    (existing) => agentInstanceRef(existing) === ref,
  );
  if (at === -1) created.agentInstances.push(row);
  else created.agentInstances[at] = row;
  return row;
}
