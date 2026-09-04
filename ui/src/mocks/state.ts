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
import {
  mockAgentInstances,
  mockAgentTemplates,
  mockHarnesses,
  mockMcpServers,
  mockModels,
  mockPromptDetails,
  mockPrompts,
} from "./fixtures";

/** What has been written during this browsing session. */
const created = {
  models: [] as ModelConfig[],
  mcpServers: [] as ToolServerResponse[],
  prompts: [] as PromptTemplateDetail[],
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
