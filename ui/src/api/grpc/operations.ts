/**
 * What each operation id actually calls.
 *
 * One entry per id in `../operations.ts`, each one an RPC on one of the
 * controller's gRPC services plus the conversion between its proto messages and
 * this app's domain types. Nothing above this file names a service or a method.
 *
 * ## The map from ids to RPCs, and the four places it is not one-to-one
 *
 * Most ids are a single RPC. These are not, and each is a decision rather than a
 * detail:
 *
 * - **`agents.get`** has two RPCs behind it, `GetSandboxAgent` and
 *   `GetAgentHarness`, because a `SandboxAgent` and an `AgentHarness` are
 *   different resources. A caller holding only a namespace and a name — a detail
 *   page reading a URL — cannot know which, so this tries the sandbox first and
 *   falls back to the harness on a `NotFound`. Any other failure is reported as
 *   itself rather than retried, so a permission error does not come back as
 *   "no such agent".
 * - **`agents.update`** is `UpdateSandboxAgent` and nothing else. `AgentHarness`
 *   has **no Update RPC** in `agents.proto`. Asking to update one fails here,
 *   loudly, rather than at the server: the alternative is a request the server
 *   cannot possibly satisfy and an error message about the wrong thing.
 * - **`models.providers`** merges `ListSupportedModelProviders` (the providers the
 *   controller ships with) and `ListConfiguredProviders` (the ones an operator
 *   added). The old REST endpoint returned one list and the UI's provider picker
 *   still wants one; keeping them apart here would mean every caller merging them
 *   itself.
 * - **`mcpServers.list`** is also how a single tool server is read, because
 *   `ToolService` has **no GetToolServer RPC**. Nothing in the app reads one today
 *   — which is why there is no `mcpServers.get` id — and anything that starts to
 *   has to filter the list.
 *
 * ## What is not reachable from here
 *
 * `ListProviderModels` (refresh one provider's catalogue),
 * `ListSupportedMemoryProviders`, `ListToolServerTypes`, the MCP-app RPCs
 * (`ListMCPAppTools`, `CallMCPAppTool`, `ReadMCPAppResource`), the
 * `AgentHarness` session-actor RPCs and `SystemService`'s `GetVersion` and
 * `GetCurrentUser` all exist on the controller and have no operation id, because
 * nothing in the app calls them yet. Adding one is a new id here, not a new path
 * anywhere else.
 *
 * Four of `AgentInstanceService`'s RPCs are absent for a stronger reason than "not
 * yet". `CreateAgentInstance` needs a harness *and* an AgentTemplate chosen, and
 * `AgentTemplate` has no service to choose one from — it is a shipped CRD that
 * appears in no proto and on no route. `DeleteAgentInstance` is destructive and
 * irreversible. `CheckpointService` — including `ForkAgentInstance` — and the three
 * share RPCs are whole features rather than a control on a list. Each is a product
 * decision, and until one is taken the honest surface is the one that reads and
 * the two lifecycle operations that undo each other.
 */

import { AgentKind, AgentService } from "@/generated/kagent/api/v1alpha1/agents_pb";
import { ModelService } from "@/generated/kagent/api/v1alpha1/models_pb";
import { ToolService } from "@/generated/kagent/api/v1alpha1/tools_pb";
import { PromptTemplateService } from "@/generated/kagent/api/v1alpha1/prompts_pb";
import { SystemService } from "@/generated/kagent/api/v1alpha1/system_pb";
import { HarnessService } from "@/generated/kagent/api/v1alpha1/harnesses_pb";
import type { Harness as PbHarness } from "@/generated/kagent/api/v1alpha1/harnesses_pb";
import { AgentTemplateService } from "@/generated/kagent/api/v1alpha1/agent_templates_pb";
import type { AgentTemplate as PbAgentTemplate } from "@/generated/kagent/api/v1alpha1/agent_templates_pb";
import {
  AgentInstanceOperation as PbAgentInstanceOperation,
  AgentInstanceService,
  AgentInstanceSharePermission as PbSharePermission,
  AgentInstanceState as PbAgentInstanceState,
} from "@/generated/kagent/api/v1alpha1/agent_instances_pb";
import type { AgentInstanceShare as PbAgentInstanceShare } from "@/generated/kagent/api/v1alpha1/agent_instances_pb";
import type { AgentInstance as PbAgentInstance } from "@/generated/kagent/api/v1alpha1/agent_instances_pb";
import type { Agent as PbAgent } from "@/generated/kagent/api/v1alpha1/agents_pb";
import type { ToolServer as PbToolServer } from "@/generated/kagent/api/v1alpha1/tools_pb";
import type {
  GetSubstrateStatusResponse,
  SubstrateActor as PbSubstrateActor,
  SubstrateActorTemplate as PbSubstrateActorTemplate,
  SubstrateWorker as PbSubstrateWorker,
  SubstrateWorkerPool as PbSubstrateWorkerPool,
} from "@/generated/kagent/api/v1alpha1/system_pb";
import type { StructuredObject } from "@/generated/kagent/api/v1alpha1/common_pb";
import { ApiError, fromConnectError, isNotFound, rethrowIfAborted } from "../ApiError";
import { operationContext, serviceClient } from "../transport";
import {
  KAGENT_API_VERSION,
  isoFrom,
  list,
  orUndefined,
  refToString,
  toNumber,
  unwrap,
  wrap,
} from "./wire";
import type {
  Agent,
  AgentCreateRequest,
  AgentKindName,
  AgentResponse,
  Tool,
} from "../domain/agents";
import { normaliseAgentResponse } from "../domain/agents";
import type { ModelConfig, ModelConfigSpec, Provider } from "../domain/models";
import type {
  ToolServerResponse,
  ToolsResponse,
} from "../domain/mcpServers";
import type { PromptTemplateDetail, PromptTemplateSummary } from "../domain/prompts";
import type {
  SubstrateActorEntry,
  SubstrateActorTemplateEntry,
  SubstrateStatusResponse,
  SubstrateWorkerEntry,
  SubstrateWorkerPoolEntry,
} from "../domain/substrate";
import type { Harness } from "../domain/harnesses";
import type {
  AgentTemplate,
  AgentTemplateResource,
} from "../domain/agentTemplates";
import type {
  AgentInstance,
  AgentInstanceOperation,
  AgentInstanceShare,
  AgentInstanceSharePermission,
  AgentInstanceState,
} from "../domain/agentInstances";
import type {
  ApiOperations,
  AgentRef,
  OperationCallOptions,
  SubstratePageInput,
} from "../operations";
import { createContextValues } from "@connectrpc/connect";

/**
 * The call options every RPC is given.
 *
 * `contextValues` is what carries the operation id into the transport, where the
 * interceptor that runs registered transforms can read it — a service and a
 * method are not enough to identify an operation (see `agents.get`).
 */
function call(operation: keyof ApiOperations, options: OperationCallOptions) {
  return {
    signal: options.signal,
    contextValues: createContextValues().set(operationContext, operation),
  };
}

/** Turns whatever a call rejected with into an `ApiError` naming the RPC. */
async function rpc<T>(
  name: string,
  signal: AbortSignal | undefined,
  run: () => Promise<T>,
): Promise<T> {
  try {
    return await run();
  } catch (error) {
    rethrowIfAborted(error, signal);
    throw fromConnectError(error, name);
  }
}

// region Agents

const KIND_BY_ENUM: Record<AgentKind, AgentKindName | "unknown"> = {
  [AgentKind.UNSPECIFIED]: "unknown",
  [AgentKind.SANDBOX_AGENT]: "SandboxAgent",
  [AgentKind.AGENT_HARNESS]: "AgentHarness",
};

/**
 * One `Agent` message as the row every agent screen reads.
 *
 * The custom resource comes out of `resource.value` unchanged — it is the object
 * as JSON, which is the same thing the REST API used to return — while the
 * resolved fields beside it (`model`, `ready`, the tool list) are the
 * controller's own denormalisation and have no equivalent inside the resource.
 */
function toAgentRow(agent: PbAgent, rpcName: string): AgentResponse {
  const resource = unwrap<Agent>(agent.resource, rpcName, "agent resource");

  return normaliseAgentResponse({
    id: agent.id,
    agent: resource,
    model: agent.model,
    modelProvider: agent.modelProvider,
    modelConfigRef: refToString(agent.modelConfigRef),
    tools: list(agent.tools).map((tool) => (tool.value ?? {}) as unknown as Tool),
    memoryRefs: list(agent.memoryRefs),
    // Two separate truths the controller reports separately: `accepted` is
    // "the spec was valid", `ready` is "it is running". A screen showing only one
    // of them tells half the story, so both are carried.
    deploymentReady: agent.ready,
    accepted: agent.accepted,
    agentKind: KIND_BY_ENUM[agent.kind] ?? "unknown",
    agentHarness: agent.agentHarness
      ? {
          backend: agent.agentHarness.backend,
          actorId: agent.agentHarness.actorId,
          backendRefId: agent.agentHarness.backendRefId,
          endpoint: agent.agentHarness.endpoint,
          acpPath: agent.agentHarness.acpPath,
        }
      : undefined,
  });
}

/**
 * Which resource a draft is for.
 *
 * Read from the draft's own `kind`, because that is the only place the answer
 * exists: the create RPCs are per-kind and a form that does not say which kind it
 * built cannot be guessed at. Defaults to `SandboxAgent`, the kind every existing
 * form in this app produces.
 */
function draftKind(draft: AgentCreateRequest): AgentKindName {
  return draft.kind === "AgentHarness" ? "AgentHarness" : "SandboxAgent";
}

const agents: Pick<
  ApiOperations,
  "agents.list" | "agents.get" | "agents.create" | "agents.update" | "agents.delete"
> = {
  "agents.list": async (input, options) => {
    const name = "AgentService/ListAgents";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentService).listAgents(
        { namespace: input.namespace ?? "" },
        call("agents.list", options),
      ),
    );
    return list(response.agents).map((agent) => toAgentRow(agent, name));
  },

  "agents.get": async (input, options) => {
    if (input.kind === "AgentHarness") return getHarness(input, options);
    if (input.kind === "SandboxAgent") return getSandbox(input, options);

    // No kind given: the caller holds a URL and nothing else. Sandbox first
    // because it is the common case, and only a genuine 404 justifies asking
    // again — anything else is this agent's real answer.
    try {
      return await getSandbox(input, options);
    } catch (error) {
      if (!isNotFound(error)) throw error;
      return getHarness(input, options);
    }
  },

  "agents.create": async (input, options) => {
    const kind = draftKind(input.resource);
    const ref = {
      namespace: input.resource.metadata.namespace ?? "",
      name: input.resource.metadata.name,
    };
    const resource = wrap(kind, input.resource, input.resource.apiVersion);

    if (kind === "AgentHarness") {
      const name = "AgentService/CreateAgentHarness";
      const response = await rpc(name, options.signal, () =>
        serviceClient(AgentService).createAgentHarness(
          { ref, resource },
          call("agents.create", options),
        ),
      );
      return unwrap<Agent>(response.agent?.resource, name, "created agent");
    }

    const name = "AgentService/CreateSandboxAgent";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentService).createSandboxAgent(
        { ref, resource },
        call("agents.create", options),
      ),
    );
    return unwrap<Agent>(response.agent?.resource, name, "created agent");
  },

  "agents.update": async (input, options) => {
    const name = "AgentService/UpdateSandboxAgent";
    const kind = draftKind(input.resource);

    // Stated here rather than sent and refused, because `agents.proto` has no
    // UpdateAgentHarness at all — there is no request that could succeed.
    if (kind === "AgentHarness") {
      throw new ApiError(
        "An AgentHarness cannot be edited: the controller offers no update operation for one. " +
          "Delete it and create it again.",
        { kind: "http", status: 501, code: "Unimplemented", url: name },
      );
    }

    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentService).updateSandboxAgent(
        {
          ref: {
            namespace: input.resource.metadata.namespace ?? "",
            name: input.resource.metadata.name,
          },
          resource: wrap(kind, input.resource, input.resource.apiVersion),
        },
        call("agents.update", options),
      ),
    );
    return unwrap<Agent>(response.agent?.resource, name, "updated agent");
  },

  "agents.delete": async (input, options) => {
    const ref = { namespace: input.namespace, name: input.name };
    const client = serviceClient(AgentService);

    if (input.kind === "AgentHarness") {
      await rpc("AgentService/DeleteAgentHarness", options.signal, () =>
        client.deleteAgentHarness({ ref }, call("agents.delete", options)),
      );
      return;
    }
    await rpc("AgentService/DeleteSandboxAgent", options.signal, () =>
      client.deleteSandboxAgent({ ref }, call("agents.delete", options)),
    );
  },
};

async function getSandbox(
  input: AgentRef,
  options: OperationCallOptions,
): Promise<AgentResponse> {
  const name = "AgentService/GetSandboxAgent";
  const response = await rpc(name, options.signal, () =>
    serviceClient(AgentService).getSandboxAgent(
      { ref: { namespace: input.namespace, name: input.name } },
      call("agents.get", options),
    ),
  );
  return toAgentRow(required(response.agent, name, `agent ${input.namespace}/${input.name}`), name);
}

async function getHarness(
  input: AgentRef,
  options: OperationCallOptions,
): Promise<AgentResponse> {
  const name = "AgentService/GetAgentHarness";
  const response = await rpc(name, options.signal, () =>
    serviceClient(AgentService).getAgentHarness(
      { ref: { namespace: input.namespace, name: input.name } },
      call("agents.get", options),
    ),
  );
  return toAgentRow(required(response.agent, name, `agent ${input.namespace}/${input.name}`), name);
}

// endregion

// region Models

/** `spec` lives inside the resource; the ref is carried beside it. */
function toModelConfig(
  ref: string,
  resource: StructuredObject | undefined,
  rpcName: string,
): ModelConfig {
  const object = unwrap<{ spec?: ModelConfigSpec }>(
    resource,
    rpcName,
    "model configuration",
  );
  // A ModelConfig with no spec is not something to render as a blank form: the
  // resource exists and the one field the whole screen is about is missing.
  if (!object.spec) {
    throw new ApiError(`The model configuration ${ref} carries no spec.`, {
      kind: "parse",
      url: rpcName,
    });
  }
  return { ref, spec: object.spec };
}

/**
 * The ModelConfig custom resource a write sends.
 *
 * The controller decodes the envelope into a whole `ModelConfig` and keeps only
 * its `spec` (`modelServer.modelConfigSpec`), but it decodes the whole object —
 * so the kind has to be right (`structuredobject.ToGo` rejects a mismatch) and
 * the metadata has to agree with the ref.
 */
function modelConfigResource(ref: string, spec: ModelConfigSpec) {
  const slash = ref.indexOf("/");
  const namespace = slash === -1 ? "" : ref.slice(0, slash);
  const name = slash === -1 ? ref : ref.slice(slash + 1);
  return {
    ref: { namespace, name },
    resource: wrap("ModelConfig", {
      apiVersion: KAGENT_API_VERSION,
      kind: "ModelConfig",
      metadata: { name, namespace },
      spec,
    }),
  };
}

const models: Pick<
  ApiOperations,
  | "models.list"
  | "models.get"
  | "models.create"
  | "models.update"
  | "models.delete"
  | "models.providers"
  | "models.providerModels"
> = {
  "models.list": async (_input, options) => {
    const name = "ModelService/ListModelConfigs";
    const response = await rpc(name, options.signal, () =>
      serviceClient(ModelService).listModelConfigs({}, call("models.list", options)),
    );
    return list(response.modelConfigs).map((entry) =>
      toModelConfig(refToString(entry.ref), entry.resource, name),
    );
  },

  "models.get": async (input, options) => {
    const name = "ModelService/GetModelConfig";
    const response = await rpc(name, options.signal, () =>
      serviceClient(ModelService).getModelConfig(
        { ref: { namespace: input.namespace, name: input.name } },
        call("models.get", options),
      ),
    );
    const entry = required(
      response.modelConfig,
      name,
      `model ${input.namespace}/${input.name}`,
    );
    return toModelConfig(refToString(entry.ref), entry.resource, name);
  },

  "models.create": async (input, options) => {
    const name = "ModelService/CreateModelConfig";
    const { payload } = input;
    const response = await rpc(name, options.signal, () =>
      serviceClient(ModelService).createModelConfig(
        {
          ...modelConfigResource(payload.ref, payload.spec),
          apiKey: payload.apiKey ?? "",
          secrets: payload.secrets ?? [],
        },
        call("models.create", options),
      ),
    );
    const entry = required(response.modelConfig, name, "created model");
    return toModelConfig(refToString(entry.ref), entry.resource, name);
  },

  "models.update": async (input, options) => {
    const name = "ModelService/UpdateModelConfig";
    const ref = `${input.namespace}/${input.name}`;
    const response = await rpc(name, options.signal, () =>
      serviceClient(ModelService).updateModelConfig(
        {
          ...modelConfigResource(ref, input.payload.spec),
          // `optional string` in the proto, so omitting it and sending `""` are
          // different requests: omitted leaves the stored key alone, empty clears
          // it. A form that did not touch the key must not clear it.
          apiKey: input.payload.apiKey === undefined ? undefined : input.payload.apiKey,
          secrets: input.payload.secrets ?? [],
        },
        call("models.update", options),
      ),
    );
    const entry = required(response.modelConfig, name, `updated model ${ref}`);
    return toModelConfig(refToString(entry.ref), entry.resource, name);
  },

  "models.delete": async (input, options) => {
    await rpc("ModelService/DeleteModelConfig", options.signal, () =>
      serviceClient(ModelService).deleteModelConfig(
        { ref: { namespace: input.namespace, name: input.name } },
        call("models.delete", options),
      ),
    );
  },

  "models.providers": async (_input, options) => {
    const client = serviceClient(ModelService);

    // Both lists, in parallel: a picker that offered only the stock providers
    // would silently hide whatever the operator added, and one that offered only
    // the configured ones would be empty on a fresh install.
    const [supported, configured] = await Promise.all([
      rpc("ModelService/ListSupportedModelProviders", options.signal, () =>
        client.listSupportedModelProviders({}, call("models.providers", options)),
      ),
      rpc("ModelService/ListConfiguredProviders", options.signal, () =>
        client.listConfiguredProviders({}, call("models.providers", options)),
      ),
    ]);

    const stock: Provider[] = list(supported.providers).map((provider) => ({
      name: provider.name,
      type: provider.type,
      requiredParams: list(provider.requiredParams),
      optionalParams: list(provider.optionalParams),
      source: "stock",
    }));

    // A configured provider carries no parameter lists — the controller does not
    // report any — so the fields are empty rather than absent, because every
    // caller iterates them.
    const added: Provider[] = list(configured.providers).map((provider) => ({
      name: provider.name,
      type: provider.type,
      requiredParams: [],
      optionalParams: [],
      source: "configured",
      endpoint: orUndefined(provider.endpoint),
    }));

    return [...stock, ...added];
  },

  "models.providerModels": async (_input, options) => {
    const name = "ModelService/ListSupportedModels";
    const response = await rpc(name, options.signal, () =>
      serviceClient(ModelService).listSupportedModels(
        {},
        call("models.providerModels", options),
      ),
    );

    const grouped: Record<string, Array<{ name: string; function_calling: boolean }>> = {};
    for (const provider of list(response.providers)) {
      grouped[provider.provider] = list(provider.models).map((model) => ({
        name: model.name,
        // Snake-cased on purpose: this is the name every picker in the app already
        // reads, and renaming it here would rename it in eight components for no
        // gain.
        function_calling: model.functionCalling,
      }));
    }
    return grouped;
  },
};

// endregion

// region Tool servers

function toToolServerRow(server: PbToolServer): ToolServerResponse {
  return {
    ref: server.ref,
    groupKind: server.groupKind,
    discoveredTools: list(server.discoveredTools).map((tool) => ({
      name: tool.name,
      description: tool.description,
    })),
  };
}

const toolServers: Pick<
  ApiOperations,
  "mcpServers.list" | "mcpServers.create" | "mcpServers.delete" | "tools.list"
> = {
  "mcpServers.list": async (_input, options) => {
    const response = await rpc("ToolService/ListToolServers", options.signal, () =>
      serviceClient(ToolService).listToolServers({}, call("mcpServers.list", options)),
    );
    return list(response.toolServers).map(toToolServerRow);
  },

  "mcpServers.create": async (input, options) => {
    const name = "ToolService/CreateToolServer";
    const { payload } = input;
    const server =
      payload.type === "RemoteMCPServer" ? payload.remoteMCPServer : payload.mcpServer;

    if (!server) {
      throw new ApiError(
        `A ${payload.type} was asked for, but no ${payload.type} was supplied.`,
        { kind: "parse", url: name },
      );
    }

    const response = await rpc(name, options.signal, () =>
      serviceClient(ToolService).createToolServer(
        {
          type: payload.type,
          ref: {
            namespace: server.metadata.namespace ?? "",
            name: server.metadata.name,
          },
          // The envelope's kind is the server type, which is what the handler
          // checks it against (`decodeCreateToolServerResource`).
          resource: wrap(payload.type, server),
          secrets: payload.secrets ?? [],
        },
        call("mcpServers.create", options),
      ),
    );

    // The RPC answers with the created resource rather than with a list row, so
    // the row is assembled here. `discoveredTools` is empty because it is: the
    // controller has not handshaken with the server yet, and claiming otherwise
    // would put tools on screen that nothing has confirmed exist.
    const created = response.resource;
    const metadata = (created?.value as { metadata?: { name?: string; namespace?: string } })
      ?.metadata;
    const group = (created?.apiVersion ?? "").split("/")[0];

    return {
      ref: `${metadata?.namespace ?? server.metadata.namespace ?? ""}/${
        metadata?.name ?? server.metadata.name
      }`,
      groupKind: group ? `${created?.kind ?? payload.type}.${group}` : payload.type,
      discoveredTools: [],
    };
  },

  "mcpServers.delete": async (input, options) => {
    await rpc("ToolService/DeleteToolServer", options.signal, () =>
      serviceClient(ToolService).deleteToolServer(
        { ref: { namespace: input.namespace, name: input.name } },
        call("mcpServers.delete", options),
      ),
    );
  },

  "tools.list": async (_input, options) => {
    const name = "ToolService/ListTools";
    const response = await rpc(name, options.signal, () =>
      serviceClient(ToolService).listTools({}, call("tools.list", options)),
    );
    // Each entry wraps a `database.Tool` marshalled to JSON, so the row inside is
    // already exactly `ToolsResponse` — snake-cased names included.
    return list(response.tools).map((tool) =>
      unwrap<ToolsResponse>(tool.resource, name, "tool"),
    );
  },
};

// endregion

// region Prompt libraries

const prompts: Pick<
  ApiOperations,
  "prompts.list" | "prompts.get" | "prompts.create" | "prompts.update" | "prompts.delete"
> = {
  "prompts.list": async (input, options) => {
    const response = await rpc(
      "PromptTemplateService/ListPromptTemplates",
      options.signal,
      () =>
        serviceClient(PromptTemplateService).listPromptTemplates(
          { namespace: input.namespace ?? "" },
          call("prompts.list", options),
        ),
    );
    return list(response.promptTemplates).map((entry): PromptTemplateSummary => ({
      namespace: entry.ref?.namespace ?? "",
      name: entry.ref?.name ?? "",
      keyCount: entry.keyCount,
      keys: list(entry.keys),
    }));
  },

  "prompts.get": async (input, options) => {
    const name = "PromptTemplateService/GetPromptTemplate";
    const response = await rpc(name, options.signal, () =>
      serviceClient(PromptTemplateService).getPromptTemplate(
        { ref: { namespace: input.namespace, name: input.name } },
        call("prompts.get", options),
      ),
    );
    return toPromptDetail(
      required(
        response.promptTemplate,
        name,
        `prompt library ${input.namespace}/${input.name}`,
      ),
    );
  },

  "prompts.create": async (input, options) => {
    const name = "PromptTemplateService/CreatePromptTemplate";
    const { payload } = input;
    const response = await rpc(name, options.signal, () =>
      serviceClient(PromptTemplateService).createPromptTemplate(
        {
          ref: { namespace: payload.namespace, name: payload.name },
          data: payload.data,
        },
        call("prompts.create", options),
      ),
    );
    return toPromptDetail(
      required(response.promptTemplate, name, "created prompt library"),
    );
  },

  "prompts.update": async (input, options) => {
    const name = "PromptTemplateService/UpdatePromptTemplate";
    const response = await rpc(name, options.signal, () =>
      serviceClient(PromptTemplateService).updatePromptTemplate(
        {
          ref: { namespace: input.namespace, name: input.name },
          data: input.payload.data,
        },
        call("prompts.update", options),
      ),
    );
    return toPromptDetail(
      required(
        response.promptTemplate,
        name,
        `prompt library ${input.namespace}/${input.name}`,
      ),
    );
  },

  "prompts.delete": async (input, options) => {
    await rpc("PromptTemplateService/DeletePromptTemplate", options.signal, () =>
      serviceClient(PromptTemplateService).deletePromptTemplate(
        { ref: { namespace: input.namespace, name: input.name } },
        call("prompts.delete", options),
      ),
    );
  },
};

function toPromptDetail(template: {
  ref?: { namespace: string; name: string };
  data: Record<string, string>;
}): PromptTemplateDetail {
  return {
    namespace: template.ref?.namespace ?? "",
    name: template.ref?.name ?? "",
    data: template.data ?? {},
  };
}

// endregion

// region Agent instances

/**
 * The two enums, as the words the domain type carries.
 *
 * Written as complete records keyed by the generated enum, so adding a member to
 * the proto and regenerating breaks `yarn typecheck` here — which is the only
 * place that would otherwise keep quiet about it and render the new state as a
 * blank cell.
 */
const INSTANCE_STATE_BY_ENUM: Record<PbAgentInstanceState, AgentInstanceState> = {
  [PbAgentInstanceState.UNSPECIFIED]: "unspecified",
  [PbAgentInstanceState.CREATING]: "creating",
  [PbAgentInstanceState.READY]: "ready",
  [PbAgentInstanceState.SUSPENDED]: "suspended",
  [PbAgentInstanceState.FAILED]: "failed",
  [PbAgentInstanceState.DELETING]: "deleting",
  [PbAgentInstanceState.DELETED]: "deleted",
};

const INSTANCE_OPERATION_BY_ENUM: Record<
  PbAgentInstanceOperation,
  AgentInstanceOperation
> = {
  [PbAgentInstanceOperation.UNSPECIFIED]: "unspecified",
  [PbAgentInstanceOperation.CREATE]: "create",
  [PbAgentInstanceOperation.SUSPEND]: "suspend",
  [PbAgentInstanceOperation.RESUME]: "resume",
  [PbAgentInstanceOperation.DELETE]: "delete",
};

/**
 * One `AgentInstance` message as the record every instance screen reads.
 *
 * Nothing is unwrapped here: an instance is a row in the controller's database
 * rather than a custom resource, so there is no `StructuredObject` in the way —
 * this is a rename and a pair of enum conversions, and that is the whole job.
 *
 * The `?? "unknown"` on each enum is not dead code, however much the types make it
 * look so. The generated enum describes the proto this build was compiled against;
 * a newer controller can send a number outside it, and the lookup then yields
 * `undefined`. Falling through to `"unknown"` puts that on screen as an unknown
 * state instead of an empty cell.
 */
function toAgentInstance(instance: PbAgentInstance): AgentInstance {
  return {
    id: instance.id,
    namespace: instance.namespace,
    // Carried through as it arrives, empty included: empty means unnamed, which is
    // a state the controller writes deliberately and every row predating the column
    // is in. Turning it into `undefined` here would make every caller handle two
    // spellings of the same fact.
    name: instance.name,
    creator: instance.creator,
    harness: orUndefined(refToString(instance.harness)),
    agentTemplate: orUndefined(refToString(instance.agentTemplate)),
    preparedRevision: orUndefined(instance.preparedRevision),
    a2aAuthority: orUndefined(instance.a2aAuthority),
    state: INSTANCE_STATE_BY_ENUM[instance.state] ?? "unknown",
    operation: INSTANCE_OPERATION_BY_ENUM[instance.operation] ?? "unknown",
    // Present but empty is a real answer — the controller recorded a failure and
    // said nothing about it — so the message's presence decides this, not its
    // contents. `orUndefined` then keeps the two halves separately absent.
    failure: instance.failure
      ? {
          reason: orUndefined(instance.failure.reason),
          message: orUndefined(instance.failure.message),
        }
      : undefined,
    createdAt: isoFrom(instance.createdAt),
    updatedAt: isoFrom(instance.updatedAt),
    // `map<string, string>` is never absent in the generated type, but a
    // hand-written fake can still omit it.
    labels: instance.labels ?? {},
  };
}

/**
 * How many pages `agentInstances.list` will follow before giving up.
 *
 * The list is paged — the controller answers at most 100 rows and hands back a
 * token — and a page that showed only the first hundred while saying nothing would
 * be exactly the quiet half-truth this codebase keeps having to undo. So every page
 * is followed.
 *
 * The cap exists because "follow the token until it is empty" trusts the server to
 * eventually clear it, and a server that does not would spin here forever with the
 * page stuck on a spinner. At 100 rows a page this is 5,000 instances, far past any
 * real namespace; reaching it means the token is not advancing, which is a fault
 * worth reporting rather than a list worth truncating.
 */
const INSTANCE_PAGE_LIMIT = 50;

/** Which permission the wire spells, both ways. */
const SHARE_PERMISSION_TO_PB = {
  readOnly: PbSharePermission.READ_ONLY,
  readWrite: PbSharePermission.READ_WRITE,
} as const;

const SHARE_PERMISSION_FROM_PB: Partial<
  Record<PbSharePermission, AgentInstanceSharePermission>
> = {
  [PbSharePermission.READ_ONLY]: "readOnly",
  [PbSharePermission.READ_WRITE]: "readWrite",
};

/**
 * One share record.
 *
 * An unrecognised permission reads as `readOnly` rather than as the permissive
 * one — which is also what the controller does, treating anything that is not
 * `READ_WRITE` as read-only. A build newer than this one adding a permission must
 * not have it silently widen access here.
 */
function toAgentInstanceShare(share: PbAgentInstanceShare): AgentInstanceShare {
  return {
    id: share.id,
    namespace: share.namespace,
    agentInstanceId: share.agentInstanceId,
    permission: SHARE_PERMISSION_FROM_PB[share.permission] ?? "readOnly",
    createdAt: isoFrom(share.createdAt),
  };
}

const agentInstances: Pick<
  ApiOperations,
  | "agentInstances.shares.list"
  | "agentInstances.shares.create"
  | "agentInstances.shares.revoke"
  | "agentInstances.list"
  | "agentInstances.get"
  | "agentInstances.create"
  | "agentInstances.rename"
  | "agentInstances.delete"
  | "agentInstances.suspend"
  | "agentInstances.resume"
> = {
  "agentInstances.list": async (input, options) => {
    const name = "AgentInstanceService/ListAgentInstances";
    const rows: AgentInstance[] = [];
    let pageToken = "";

    for (let page = 0; page < INSTANCE_PAGE_LIMIT; page += 1) {
      const response = await rpc(name, options.signal, () =>
        serviceClient(AgentInstanceService).listAgentInstances(
          {
            namespace: input.namespace,
            allCreators: input.allCreators ?? false,
            /*
             * One agent's conversations, narrowed by the server.
             *
             * Both fields are optional and either may be given alone. The controller
             * resolves them through `prepared_revision` to the pair the instance was
             * built from, so they also select instances stored before the fields
             * existed — and, more importantly, so the narrowing happens before the
             * page is cut. Filtering a page after fetching it is the defect the
             * substrate tables were fixed for: a match on page nine reads as "no
             * conversations".
             *
             * Empty strings rather than absent, because proto3 has no absent string
             * and the controller reads an empty one as "do not filter".
             */
            agentTemplate: input.agentTemplate ?? "",
            harness: input.harness ?? "",
            // No `limit`: the controller's own default (50) is a better answer than
            // a number invented here, and it rejects anything over 100 outright.
            page: { pageToken },
          },
          call("agentInstances.list", options),
        ),
      );

      rows.push(...list(response.agentInstances).map(toAgentInstance));

      const next = response.page?.nextPageToken ?? "";
      if (!next) return rows;
      // A token that has not changed is a server that is not advancing. Left
      // alone it would re-read the same page until the cap, so it is caught here
      // where the reason is still obvious.
      if (next === pageToken) {
        throw new ApiError(
          "The API repeated the same page of agent instances instead of advancing.",
          { kind: "parse", url: name },
        );
      }
      pageToken = next;
    }

    throw new ApiError(
      `The API offered more than ${INSTANCE_PAGE_LIMIT} pages of agent instances; the list was not read to the end.`,
      { kind: "parse", url: name },
    );
  },

  /*
   * Creating an instance is choosing a pair, not filling in a spec.
   *
   * `CreateAgentInstanceRequest` is a namespace and two names, because what the
   * agent *is* lives on the AgentTemplate and how it *runs* lives on the Harness.
   * There is nothing else for a form here to collect.
   *
   * `request_id` is the controller's idempotency key and is required: `validateCreate`
   * refuses an empty one, or one with surrounding whitespace, or one over 128
   * characters. It is passed in rather than invented here, because the point of the
   * key is that a *retry* reuses it — a value generated per call would make every
   * retry a new instance, which is the opposite of what it is for.
   */
  "agentInstances.create": async (input, options) => {
    const name = "AgentInstanceService/CreateAgentInstance";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentInstanceService).createAgentInstance(
        {
          namespace: input.namespace,
          harness: input.harness,
          agentTemplate: input.agentTemplate,
          requestId: input.requestId,
          // Optional, and empty means unnamed rather than untitled-by-mistake: a
          // conversation started from a "New chat" button has nothing to be called
          // yet, and inventing a name for it would be putting words in the reader's
          // mouth that they would then have to clear.
          name: input.name ?? "",
        },
        call("agentInstances.create", options),
      ),
    );
    return toAgentInstance(required(response.agentInstance, name, "created agent instance"));
  },

  /*
   * Renaming a conversation, which is the one write on this service that is not a
   * lifecycle operation.
   *
   * It authorises as a write — `AccessUpdate` in the controller's policy — so a
   * read-only share link cannot retitle a conversation for everybody holding the
   * link. And it is scoped to the creator, like every other read of an instance, so
   * a conversation somebody else started cannot be renamed even by a reader who can
   * see it in a list.
   *
   * An empty name is valid and clears the title, returning the conversation to being
   * identified by its id.
   */
  "agentInstances.rename": async (input, options) => {
    const name = "AgentInstanceService/UpdateAgentInstanceName";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentInstanceService).updateAgentInstanceName(
        { namespace: input.namespace, agentInstanceId: input.id, name: input.name },
        call("agentInstances.rename", options),
      ),
    );
    return toAgentInstance(required(response.agentInstance, name, "renamed agent instance"));
  },

  /*
   * Deleting takes the conversation with it — the instance is the conversation —
   * so the response's final record is discarded rather than returned: there is
   * nothing left to show, and handing back a row invites rendering one.
   */
  "agentInstances.delete": async (input, options) => {
    await rpc("AgentInstanceService/DeleteAgentInstance", options.signal, () =>
      serviceClient(AgentInstanceService).deleteAgentInstance(
        { namespace: input.namespace, agentInstanceId: input.id },
        call("agentInstances.delete", options),
      ),
    );
  },

  "agentInstances.shares.list": async (input, options) => {
    const name = "AgentInstanceService/ListAgentInstanceShares";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentInstanceService).listAgentInstanceShares(
        { namespace: input.namespace, agentInstanceId: input.id },
        call("agentInstances.shares.list", options),
      ),
    );
    return list(response.shares).map(toAgentInstanceShare);
  },

  "agentInstances.shares.create": async (input, options) => {
    const name = "AgentInstanceService/CreateAgentInstanceShare";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentInstanceService).createAgentInstanceShare(
        {
          namespace: input.namespace,
          agentInstanceId: input.id,
          permission: SHARE_PERMISSION_TO_PB[input.permission],
        },
        call("agentInstances.shares.create", options),
      ),
    );
    return {
      share: toAgentInstanceShare(required(response.share, name, "created share")),
      // Returned only here: the controller stores the digest, so this is the one
      // moment the token exists to be shown.
      token: response.token,
    };
  },

  "agentInstances.shares.revoke": async (input, options) => {
    await rpc("AgentInstanceService/RevokeAgentInstanceShare", options.signal, () =>
      serviceClient(AgentInstanceService).revokeAgentInstanceShare(
        { namespace: input.namespace, shareId: input.shareId },
        call("agentInstances.shares.revoke", options),
      ),
    );
  },

  "agentInstances.get": async (input, options) => {
    const name = "AgentInstanceService/GetAgentInstance";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentInstanceService).getAgentInstance(
        { namespace: input.namespace, agentInstanceId: input.id },
        call("agentInstances.get", options),
      ),
    );
    return toAgentInstance(required(response.agentInstance, name, "agent instance"));
  },

  /*
   * Suspend and resume answer with the instance as it now stands, and that answer
   * is returned rather than discarded: both complete synchronously on the
   * controller (see `ActorWorkflow.Suspend`), so the record handed back is the
   * finished state and a caller that re-read the list instead would be asking for
   * something it already has.
   */
  "agentInstances.suspend": async (input, options) => {
    const name = "AgentInstanceService/SuspendAgentInstance";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentInstanceService).suspendAgentInstance(
        { namespace: input.namespace, agentInstanceId: input.id },
        call("agentInstances.suspend", options),
      ),
    );
    return toAgentInstance(required(response.agentInstance, name, "agent instance"));
  },

  "agentInstances.resume": async (input, options) => {
    const name = "AgentInstanceService/ResumeAgentInstance";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentInstanceService).resumeAgentInstance(
        { namespace: input.namespace, agentInstanceId: input.id },
        call("agentInstances.resume", options),
      ),
    );
    return toAgentInstance(required(response.agentInstance, name, "agent instance"));
  },
};

// endregion

// region Harnesses and agent templates
//
// The two halves an AgentInstance is created from. Both were kubectl-only until
// these services existed, which is why no picker or form could offer them.

/** One `Harness` message as the record every harness picker reads. */
function toHarness(harness: PbHarness): Harness {
  return {
    ref: refToString(harness.ref),
    namespace: harness.ref?.namespace ?? "",
    name: harness.ref?.name ?? "",
    runtime: harness.runtime,
    workloadImage: harness.workloadImage,
    ready: harness.ready,
    resource: unwrap(
      harness.resource,
      "HarnessService/ListHarnesses",
      `harness ${harness.ref?.name ?? ""}`,
    ),
  };
}

/** One `AgentTemplate` message as the record every template picker reads. */
function toAgentTemplate(template: PbAgentTemplate): AgentTemplate {
  return {
    ref: refToString(template.ref),
    namespace: template.ref?.namespace ?? "",
    name: template.ref?.name ?? "",
    modelConfigRef: refToString(template.modelConfigRef),
    description: template.description,
    // Reported in status and derivable only from the harness side — a harness
    // admits templates through a label selector, so nothing on a template says
    // which ones match it.
    admittingHarnesses: list(template.admittingHarnesses),
    resource: templateResource(template, "AgentTemplateService/ListAgentTemplates"),
  };
}

/** The whole custom resource an AgentTemplate form reads and writes. */
function templateResource(template: PbAgentTemplate, rpc: string): AgentTemplateResource {
  return unwrap<AgentTemplateResource>(
    template.resource,
    rpc,
    `agent template ${template.ref?.name ?? ""}`,
  );
}

const agentBuildingBlocks: Pick<
  ApiOperations,
  | "harnesses.list"
  | "agentTemplates.list"
  | "agentTemplates.get"
  | "harnesses.create"
  | "harnesses.delete"
  | "agentTemplates.create"
  | "agentTemplates.update"
  | "agentTemplates.delete"
> = {
  "harnesses.list": async (input, options) => {
    const response = await rpc("HarnessService/ListHarnesses", options.signal, () =>
      serviceClient(HarnessService).listHarnesses(
        { namespace: input.namespace ?? "" },
        call("harnesses.list", options),
      ),
    );
    return list(response.harnesses).map(toHarness);
  },

  "harnesses.create": async (input, options) => {
    const name = "HarnessService/CreateHarness";
    const response = await rpc(name, options.signal, () =>
      serviceClient(HarnessService).createHarness(
        {
          ref: { namespace: input.namespace, name: input.name },
          resource: wrap("Harness", input.resource),
        },
        call("harnesses.create", options),
      ),
    );
    return toHarness(required(response.harness, name, "created harness"));
  },

  "harnesses.delete": async (input, options) => {
    await rpc("HarnessService/DeleteHarness", options.signal, () =>
      serviceClient(HarnessService).deleteHarness(
        { ref: { namespace: input.namespace, name: input.name } },
        call("harnesses.delete", options),
      ),
    );
  },

  "agentTemplates.list": async (input, options) => {
    const response = await rpc(
      "AgentTemplateService/ListAgentTemplates",
      options.signal,
      () =>
        serviceClient(AgentTemplateService).listAgentTemplates(
          { namespace: input.namespace ?? "" },
          call("agentTemplates.list", options),
        ),
    );
    return list(response.agentTemplates).map(toAgentTemplate);
  },

  "agentTemplates.get": async (input, options) => {
    const name = "AgentTemplateService/GetAgentTemplate";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentTemplateService).getAgentTemplate(
        { ref: { namespace: input.namespace, name: input.name } },
        call("agentTemplates.get", options),
      ),
    );
    return toAgentTemplate(
      required(response.agentTemplate, name, `agent template ${input.name}`),
    );
  },

  /*
   * Create and update both send the whole custom resource.
   *
   * `metadata.labels` ride with it, and they decide whether the template can be
   * used at all: a Harness admits templates through a label selector, and the CRD
   * says one with no selector admits none.
   *
   * The controller forces `metadata.name` and `metadata.namespace` to agree with
   * the ref — `decodeResource` rejects a payload naming a different object — so the
   * ref is the address and the resource is the content.
   */
  "agentTemplates.create": async (input, options) => {
    const name = "AgentTemplateService/CreateAgentTemplate";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentTemplateService).createAgentTemplate(
        {
          ref: { namespace: input.namespace, name: input.name },
          resource: wrap("AgentTemplate", input.resource),
        },
        call("agentTemplates.create", options),
      ),
    );
    return toAgentTemplate(required(response.agentTemplate, name, "created agent template"));
  },

  "agentTemplates.update": async (input, options) => {
    const name = "AgentTemplateService/UpdateAgentTemplate";
    const response = await rpc(name, options.signal, () =>
      serviceClient(AgentTemplateService).updateAgentTemplate(
        {
          ref: { namespace: input.namespace, name: input.name },
          resource: wrap("AgentTemplate", input.resource),
        },
        call("agentTemplates.update", options),
      ),
    );
    return toAgentTemplate(
      required(response.agentTemplate, name, `agent template ${input.name}`),
    );
  },

  "agentTemplates.delete": async (input, options) => {
    await rpc("AgentTemplateService/DeleteAgentTemplate", options.signal, () =>
      serviceClient(AgentTemplateService).deleteAgentTemplate(
        { ref: { namespace: input.namespace, name: input.name } },
        call("agentTemplates.delete", options),
      ),
    );
  },
};

// endregion

// region Cluster

function toSubstrateStatus(
  response: GetSubstrateStatusResponse,
): SubstrateStatusResponse {
  return {
    enabled: response.enabled,
    // Empty means nothing went wrong. Left as `undefined` so a page can test the
    // field rather than testing it for emptiness.
    ateApiError: orUndefined(response.ateApiError),
    workerPools: list(response.workerPools).map(toWorkerPoolEntry),
    actorTemplates: list(response.actorTemplates).map(toActorTemplateEntry),
    actors: list(response.actors).map(toActorEntry),
    workers: list(response.workers).map(toWorkerEntry),
  };
}

/** The four substrate row conversions, shared by the unpaged read and the paged ones. */
function toWorkerPoolEntry(pool: PbSubstrateWorkerPool): SubstrateWorkerPoolEntry {
  return {
    namespace: pool.namespace,
    name: pool.name,
    replicas: pool.replicas,
    ateomImage: pool.ateomImage,
  };
}

function toActorTemplateEntry(
  template: PbSubstrateActorTemplate,
): SubstrateActorTemplateEntry {
  return {
    namespace: template.namespace,
    name: template.name,
    phase: orUndefined(template.phase),
    goldenActorId: orUndefined(template.goldenActorId),
    goldenSnapshot: orUndefined(template.goldenSnapshot),
    sandboxClass: orUndefined(template.sandboxClass),
    workerSelector: orUndefined(template.workerSelector),
    harnessName: orUndefined(template.harnessName),
    managedByKagent: template.managedByKagent,
  };
}

function toActorEntry(actor: PbSubstrateActor): SubstrateActorEntry {
  return {
    actorId: actor.actorId,
    atespace: orUndefined(actor.atespace),
    status: actor.status,
    actorTemplateNamespace: orUndefined(actor.actorTemplateNamespace),
    actorTemplateName: orUndefined(actor.actorTemplateName),
    ateomPodNamespace: orUndefined(actor.ateomPodNamespace),
    ateomPodName: orUndefined(actor.ateomPodName),
    ateomPodIp: orUndefined(actor.ateomPodIp),
    latestSnapshot: orUndefined(actor.latestSnapshot),
    workerPoolName: orUndefined(actor.workerPoolName),
    inProgressSnapshot: orUndefined(actor.inProgressSnapshot),
    version: toNumber(actor.version),
  };
}

function toWorkerEntry(worker: PbSubstrateWorker): SubstrateWorkerEntry {
  return {
    workerNamespace: worker.workerNamespace,
    workerPool: worker.workerPool,
    workerPod: worker.workerPod,
    actorNamespace: orUndefined(worker.actorNamespace),
    actorTemplate: orUndefined(worker.actorTemplate),
    actorId: orUndefined(worker.actorId),
    ip: orUndefined(worker.ip),
    version: toNumber(worker.version),
  };
}

async function substrateStatus(
  namespace: string | undefined,
  operation:
    | "substrate.status"
    | "substrate.summary"
    | "substrate.actors"
    | "substrate.workers",
  options: OperationCallOptions,
): Promise<SubstrateStatusResponse> {
  const response = await rpc("SystemService/GetSubstrateStatus", options.signal, () =>
    serviceClient(SystemService).getSubstrateStatus(
      { namespace: namespace ?? "" },
      call(operation, options),
    ),
  );
  return toSubstrateStatus(response);
}

function localPage<T>(
  rows: T[],
  input: SubstratePageInput<string>,
  key: (row: T) => string,
  text: (row: T) => string,
) {
  const needle = input.filter?.trim().toLowerCase();
  const matching = needle
    ? rows.filter((row) => text(row).toLowerCase().includes(needle))
    : rows;
  matching.sort((left, right) => {
    const compared = key(left).localeCompare(key(right));
    return input.sortOrder === "desc" ? -compared : compared;
  });
  const start = Number.parseInt(input.pageToken ?? "0", 10) || 0;
  const limit = input.limit || 50;
  const end = Math.min(start + limit, matching.length);
  return {
    rows: matching.slice(start, end),
    nextPageToken: end < matching.length ? String(end) : undefined,
    totalSize: matching.length,
  };
}

const cluster: Pick<
  ApiOperations,
  | "namespaces.list"
  | "substrate.status"
  | "substrate.summary"
  | "substrate.actors"
  | "substrate.workers"
> = {
  "namespaces.list": async (_input, options) => {
    const response = await rpc("SystemService/ListNamespaces", options.signal, () =>
      serviceClient(SystemService).listNamespaces({}, call("namespaces.list", options)),
    );
    return list(response.namespaces).map((namespace) => ({
      name: namespace.name,
      status: namespace.status,
    }));
  },

  "substrate.status": async (input, options) => {
    return substrateStatus(input.namespace, "substrate.status", options);
  },

  "substrate.summary": async (input, options) => {
    const response = await substrateStatus(input.namespace, "substrate.summary", options);
    const actorStatusCounts = new Map<string, number>();
    for (const actor of response.actors) {
      actorStatusCounts.set(actor.status, (actorStatusCounts.get(actor.status) ?? 0) + 1);
    }
    return {
      enabled: response.enabled,
      ateApiError: response.ateApiError,
      workerPools: response.workerPools,
      actorTemplates: response.actorTemplates,
      actorCount: response.actors.length,
      workerCount: response.workers.length,
      runningActorCount: response.actors.filter(
        (actor) => actor.status.toLowerCase() === "running",
      ).length,
      busyWorkerCount: response.workers.filter((worker) => Boolean(worker.actorId)).length,
      actorStatusCounts: [...actorStatusCounts].map(([status, count]) => ({ status, count })),
    };
  },

  "substrate.actors": async (input, options) => {
    const response = await substrateStatus(input.namespace, "substrate.actors", options);
    const sortField = input.sortField ?? "default";
    const page = localPage(
      response.actors,
      input,
      (actor) => {
        if (sortField === "actorId") return actor.actorId;
        if (sortField === "template") {
          return `${actor.actorTemplateNamespace ?? ""}/${actor.actorTemplateName ?? ""}\0${actor.actorId}`;
        }
        if (sortField === "workerPod") {
          return `${actor.ateomPodNamespace ?? ""}/${actor.ateomPodName ?? ""}\0${actor.actorId}`;
        }
        /*
         * `status` and `default` are one branch because they are one ordering: the
         * default *is* status then id, as the field's own type says. So the Status
         * header changes nothing ascending and reverses the grouping descending, which
         * is correct and not obvious — named here so that a change to the default order
         * has to decide what Status means rather than quietly turning it into a no-op.
         */
        return `${actor.status}\0${actor.actorId}`;
      },
      (actor) =>
        [
          actor.actorId,
          actor.status,
          actor.actorTemplateNamespace,
          actor.actorTemplateName,
          actor.ateomPodNamespace,
          actor.ateomPodName,
          actor.ateomPodIp,
        ].join(" "),
    );
    return {
      actors: page.rows,
      nextPageToken: page.nextPageToken,
      totalSize: page.totalSize,
      appliedSortField: sortField,
      appliedSortOrder: input.sortOrder ?? "asc",
    };
  },

  "substrate.workers": async (input, options) => {
    const response = await substrateStatus(input.namespace, "substrate.workers", options);
    const sortField = input.sortField ?? "default";
    const page = localPage(
      response.workers,
      input,
      (worker) => {
        const pod = `${worker.workerNamespace}/${worker.workerPod}`;
        if (sortField === "pod") return pod;
        if (sortField === "actor") return `${worker.actorId || "\uffff"}\0${pod}`;
        // `pool` and `default` are one ordering for the reason the actors' `status` is:
        // the default is pool then pod.
        return `${worker.workerPool}\0${pod}`;
      },
      (worker) =>
        [
          worker.workerNamespace,
          worker.workerPool,
          worker.workerPod,
          worker.actorNamespace,
          worker.actorTemplate,
          worker.actorId,
          worker.ip,
        ].join(" "),
    );
    return {
      workers: page.rows,
      nextPageToken: page.nextPageToken,
      totalSize: page.totalSize,
      appliedSortField: sortField,
      appliedSortOrder: input.sortOrder ?? "asc",
    };
  },
};

// endregion

/** A missing single resource is an error: the caller asked for a specific thing. */
function required<T>(value: T | undefined, rpcName: string, what: string): T {
  if (value === undefined) {
    throw new ApiError(`The API returned no ${what}.`, { kind: "parse", url: rpcName });
  }
  return value;
}

/**
 * The default implementation of every operation.
 *
 * `operationIds` is derived from this object's keys, so an operation cannot be
 * declared in `OperationMap` without appearing here — the compiler insists.
 */
export const defaultOperations: ApiOperations = {
  ...agents,
  ...agentBuildingBlocks,
  ...models,
  ...toolServers,
  ...prompts,
  ...agentInstances,
  ...cluster,
};
