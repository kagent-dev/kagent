/**
 * The mock backend, as a gRPC transport.
 *
 * The controller's application API is gRPC now, so the fixtures have to be served
 * over the same thing the app calls: a `Transport`. `setApiTransport` in
 * `@/api/transport` substitutes the one every operation goes through, which means
 * the fake sits exactly where the network used to and *no service worker is in the
 * path at all* for the API.
 *
 * That is a better arrangement than intercepting the request, not merely a
 * cheaper one. A service worker cannot see a request it does not proxy, and
 * answering gRPC-Web on the wire means reimplementing length-prefixed framing and
 * trailers — a lot of machinery whose only product is a byte stream that connect
 * immediately decodes back into the object this file already has. Returning the
 * message is the whole job.
 *
 * ## Why this is hand-written and not `createRouterTransport`
 *
 * Connect ships `createRouterTransport`, which serves real service
 * implementations in-process and would give typed handlers for free. It is the
 * right tool for the unit suite (`src/api/operations.test.ts` uses it) and the
 * wrong one here, for a reason worth writing down before someone converts this
 * file to it: a router transport round-trips every message through protobuf
 * serialisation, and `google.protobuf.Struct` cannot encode `undefined`. Every
 * resource here travels inside a `StructuredObject`, whose `value` is a Struct —
 * and a form draft produces an `undefined` field easily (an optional input nobody
 * filled in). So a create that a real gRPC-Web call accepts would throw inside the
 * fake, and the error would look like a fixture bug while being a serialisation
 * one. Handing the message over by reference sidesteps the whole class.
 *
 * ## What it answers, and what it refuses
 *
 * A call is dispatched on `service.typeName/method.name`, so the table below reads
 * as the controller's own API surface — `kagent.api.v1alpha1.ModelService/ListModelConfigs`
 * and so on. An RPC with no entry answers `Unimplemented` naming itself, and
 * `stream` throws for the same reason: nothing in this app streams over gRPC yet,
 * and a silently empty stream is a fixture that lies.
 *
 * ## The three axes still apply
 *
 * Every call goes through `settle()`, so `?mock=slow` waits, `?mock=error` fails
 * and `?mock=empty` empties — see `scenario.ts`. Failure is a `ConnectError`
 * rather than an invented shape, so the pages see the same `ApiError` they would
 * see from a real failure (`fromConnectError` in `@/api/ApiError`), and a
 * single-resource read under `empty` answers `NotFound` because that — not an
 * empty body — is the state a detail page has to handle.
 *
 * ## The transforms are not this file's business
 *
 * `setApiTransport` substitutes the *inner* transport, and `withApiInterceptors`
 * in `@/api/transport` wraps whichever one is in force — so the bearer token and
 * every registered request transform have already been applied by the time a call
 * arrives here, exactly as they are in production. That matters for one fake in
 * particular: a share link is spent by a transform putting `X-Share-Token` on the
 * call, and `GetAgentInstance` refuses a token it never issued. Applying the transforms
 * again here would be a second implementation of the same thing, and two
 * implementations drift — so the header is simply read from the call.
 *
 * ## Counting what a page asked for
 *
 * Calls are tallied per RPC on `window.__kagentMockCalls`, because a browser test
 * that wants to know whether a page polled has nothing else to look at: under a
 * substituted transport there is no request on the wire to observe. See
 * `publishCallCounts` at the foot of this file.
 */

import { create } from "@bufbuild/protobuf";
import type {
  DescMessage,
  DescMethodUnary,
  JsonObject,
  MessageInitShape,
  MessageShape,
} from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Code, ConnectError } from "@connectrpc/connect";
import type { Transport, UnaryResponse } from "@connectrpc/connect";
import { HarnessService } from "@/generated/kagent/api/v1alpha1/harnesses_pb";
import { AgentTemplateService } from "@/generated/kagent/api/v1alpha1/agent_templates_pb";
import { ModelService } from "@/generated/kagent/api/v1alpha1/models_pb";
import { ToolService } from "@/generated/kagent/api/v1alpha1/tools_pb";
import { PromptTemplateService } from "@/generated/kagent/api/v1alpha1/prompts_pb";
import { SystemService } from "@/generated/kagent/api/v1alpha1/system_pb";
import {
  AgentInstanceOperation as PbAgentInstanceOperation,
  AgentInstanceService,
  AgentInstanceSharePermission as PbSharePermission,
  AgentInstanceState as PbAgentInstanceState,
  type AgentInstanceSchema,
} from "@/generated/kagent/api/v1alpha1/agent_instances_pb";
import type { ResourceReferenceSchema } from "@/generated/kagent/api/v1alpha1/common_pb";
import type {
  AgentInstance,
  AgentInstanceOperation,
  AgentInstanceState,
} from "@/api/domain/agentInstances";
import type { Harness } from "@/api/domain/harnesses";
import type { AgentTemplate } from "@/api/domain/agentTemplates";
import type { AgentInstanceShare } from "@/api/domain/agentInstances";
import type { ModelConfig, ModelConfigSpec } from "@/api/domain/models";
import type { PromptTemplateDetail } from "@/api/domain/prompts";
import {
  SCENARIO_DELAY_MS,
  currentAuthScenario,
  currentScenario,
  type MockScenario,
} from "./scenario";
import {
  MOCK_INSTANCE_CREATOR,
  mockNamespaces,
  mockProviderModels,
  mockProviders,
  mockSubstrateStatus,
  mockTools,
} from "./fixtures";
import {
  agentInstanceRef,
  allAgentInstances,
  allAgentTemplates,
  allModels,
  allPromptDetails,
  allPromptSummaries,
  allToolServers,
  markDeleted,
  allHarnesses,
  saveHarness,
  promptRef,
  saveAgentInstance,
  saveAgentTemplate,
  createInstanceShare,
  readInstanceShares,
  revokeInstanceShare,
  saveModel,
  savePrompt,
  saveToolServer,
} from "./state";

/** What a fake is told about the call it is answering. */
interface MockCall {
  /** The scenario in force. `error` never reaches a fake; `empty` does. */
  readonly scenario: MockScenario;
  /** The call's headers, after any request transform has run. */
  readonly headers: Record<string, string>;
  /** `Service/Method`, for messages that should say what failed. */
  readonly rpc: string;
}

/** A fake, with its input and output types erased so one table can hold them all. */
type Fake = (input: never, call: MockCall) => unknown;

const fakes = new Map<string, Fake>();

/**
 * Registers the fake for one RPC.
 *
 * Bound to the method *descriptor* rather than to a string, which is what makes
 * the compiler check every field of every fixture against the generated messages:
 * a renamed proto field breaks `yarn typecheck` here instead of rendering as a
 * blank column in a browser.
 */
function on<I extends DescMessage, O extends DescMessage>(
  method: DescMethodUnary<I, O>,
  fake: (
    input: MessageShape<I>,
    call: MockCall,
  ) => MessageInitShape<O> | Promise<MessageInitShape<O>>,
): void {
  fakes.set(rpcName(method), fake as Fake);
}

const rpcName = (method: { parent: { typeName: string }; name: string }) =>
  `${method.parent.typeName}/${method.name}`;

// ---------------------------------------------------------------------------
// The transport
// ---------------------------------------------------------------------------

/**
 * The fixtures, as the transport the API layer calls through.
 *
 * Not installed by importing it — `startMockBackend` does that, and only when the
 * app was started in mock mode. Nothing here makes fixtures the default.
 */
export const mockTransport: Transport = {
  async unary<I extends DescMessage, O extends DescMessage>(
    method: DescMethodUnary<I, O>,
    signal: AbortSignal | undefined,
    timeoutMs: number | undefined,
    header: HeadersInit | undefined,
    input: MessageInitShape<I>,
    // `contextValues` is deliberately not taken. The operation id travels in it, and
    // the only thing that read it here was the transform handling that now lives in
    // `withApiInterceptors` — a fake that dispatches on the method needs nothing else.
  ): Promise<UnaryResponse<I, O>> {
    const rpc = rpcName(method);
    const fake = fakes.get(rpc);
    if (!fake) {
      throw new ConnectError(
        `The mock backend has no fake for ${rpc}. Add one in src/mocks/transport.ts ` +
          `rather than letting the call answer with something plausible.`,
        Code.Unimplemented,
      );
    }

    countCall(rpc);

    const message = create(method.input, input);
    const headers = headerRecord(header);

    const scenario = await settle(signal, timeoutMs);
    if (scenario === "error") {
      throw new ConnectError(
        `The mock backend was asked to fail: ${rpc} answered Internal.`,
        Code.Internal,
      );
    }

    const result = await fake(message as never, { scenario, headers, rpc });
    const output = create(method.output, result as MessageInitShape<O>);

    return {
      stream: false,
      service: method.parent,
      method,
      header: new Headers(),
      trailer: new Headers(),
      message: output,
    };
  },

  /**
   * Refused, loudly.
   *
   * No operation in this app streams over gRPC — chat is A2A over HTTP and has its
   * own fake (`@/api/chat/mockChatClient`). Answering with an empty stream would
   * let a future streaming call look like it worked and returned nothing.
   */
  stream(method) {
    return Promise.reject(
      new ConnectError(
        `The mock backend does not serve streams: ${rpcName(method)} was called. ` +
          `Nothing in this app streams over gRPC yet.`,
        Code.Unimplemented,
      ),
    );
  },
};

/**
 * Waits out the scenario's delay and reports which scenario applied.
 *
 * The call's deadline is honoured rather than ignored: a fixture slower than the
 * client's own timeout must time out here too, or `?mock=slow` would be the one
 * scenario that behaves better against the fake than against a cluster.
 */
async function settle(
  signal: AbortSignal | undefined,
  timeoutMs: number | undefined,
): Promise<MockScenario> {
  const scenario = currentScenario();
  const delay = SCENARIO_DELAY_MS[scenario];

  if (timeoutMs !== undefined && delay >= timeoutMs) {
    await sleep(timeoutMs, signal);
    throw new ConnectError(
      `The mock backend took longer than ${timeoutMs}ms to answer.`,
      Code.DeadlineExceeded,
    );
  }

  await sleep(delay, signal);
  return scenario;
}

/** A delay a cancelled call does not outlive. */
function sleep(ms: number, signal: AbortSignal | undefined): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new ConnectError("The call was cancelled.", Code.Canceled));
      return;
    }

    const timer = window.setTimeout(() => {
      signal?.removeEventListener("abort", abort);
      resolve();
    }, ms);

    function abort() {
      window.clearTimeout(timer);
      reject(new ConnectError("The call was cancelled.", Code.Canceled));
    }

    signal?.addEventListener("abort", abort, { once: true });
  });
}

function headerRecord(header: HeadersInit | undefined): Record<string, string> {
  const record: Record<string, string> = {};
  new Headers(header).forEach((value, key) => {
    record[key] = value;
  });
  return record;
}

// ---------------------------------------------------------------------------
// Shared shapes
// ---------------------------------------------------------------------------

/** A resource in the envelope the controller wraps custom resources in. */
function structured(kind: string, value: object, apiVersion = "kagent.dev/v1alpha3") {
  // `google.protobuf.Struct` is a `JsonObject` in the generated types, so a
  // fixture goes in exactly as it is written.
  return { apiVersion, kind, value: value as JsonObject };
}

/** The object inside an envelope a write sent. */
function valueOf<T>(
  resource: { value?: JsonObject } | undefined,
  what: string,
): T {
  if (!resource?.value) {
    throw new ConnectError(`A ${what} was expected in the request.`, Code.InvalidArgument);
  }
  return resource.value as T;
}

/** The inverse of `refString`: a `namespace/name` string as the message's two fields. */
const refPair = (ref: string) => {
  const slash = ref.indexOf("/");
  return slash === -1
    ? { namespace: "", name: ref }
    : { namespace: ref.slice(0, slash), name: ref.slice(slash + 1) };
};

const refString = (ref: { namespace: string; name: string } | undefined) =>
  `${ref?.namespace ?? ""}/${ref?.name ?? ""}`;

/** `namespace/name` split back into a reference message. */
function splitRef(ref: string): MessageInitShape<typeof ResourceReferenceSchema> {
  const slash = ref.indexOf("/");
  if (slash === -1) return { namespace: "", name: ref };
  return { namespace: ref.slice(0, slash), name: ref.slice(slash + 1) };
}

/** A timestamp, or nothing at all when the fixture has no date. */
function stamp(iso: string | undefined) {
  return iso ? timestampFromDate(new Date(iso)) : undefined;
}

const notFound = (what: string) =>
  new ConnectError(`No ${what} exists.`, Code.NotFound);

// ---------------------------------------------------------------------------
// Agents
// ---------------------------------------------------------------------------

function modelMessage(model: ModelConfig) {
  const ref = splitRef(model.ref);
  return {
    ref,
    resource: structured("ModelConfig", {
      apiVersion: "kagent.dev/v1alpha3",
      kind: "ModelConfig",
      metadata: { name: ref.name, namespace: ref.namespace },
      spec: model.spec,
    }),
  };
}

const specOf = (resource: { value?: JsonObject } | undefined) =>
  valueOf<{ spec?: ModelConfigSpec }>(resource, "model configuration").spec ??
  ({} as ModelConfigSpec);

on(ModelService.method.listModelConfigs, (_input, call) => ({
  modelConfigs: call.scenario === "empty" ? [] : allModels().map(modelMessage),
}));

on(ModelService.method.getModelConfig, (input, call) => {
  const wanted = refString(input.ref);
  const found =
    call.scenario === "empty"
      ? undefined
      : allModels().find((model) => model.ref === wanted);
  if (!found) throw notFound(`model configuration ${wanted}`);
  return { modelConfig: modelMessage(found) };
});

on(ModelService.method.createModelConfig, (input) => ({
  // The API key is accepted and never echoed back, which is what write-only means.
  modelConfig: modelMessage(saveModel(refString(input.ref), specOf(input.resource))),
}));

on(ModelService.method.updateModelConfig, (input) => ({
  modelConfig: modelMessage(saveModel(refString(input.ref), specOf(input.resource))),
}));

on(ModelService.method.deleteModelConfig, (input) => {
  markDeleted(refString(input.ref));
  return {};
});

/**
 * The providers the controller ships with.
 *
 * `models.providers` merges these with the configured ones, so the fixtures are
 * split the same way the controller splits them rather than served twice from one
 * list. Every fixture provider is a stock one today; one marked `configured`
 * flows into the other RPC without this file changing.
 */
on(ModelService.method.listSupportedModelProviders, (_input, call) => ({
  providers:
    call.scenario === "empty"
      ? []
      : mockProviders
          .filter((provider) => provider.source !== "configured")
          .map((provider) => ({
            name: provider.name,
            type: provider.type,
            requiredParams: provider.requiredParams,
            optionalParams: provider.optionalParams,
          })),
}));

on(ModelService.method.listConfiguredProviders, (_input, call) => ({
  providers:
    call.scenario === "empty"
      ? []
      : mockProviders
          .filter((provider) => provider.source === "configured")
          .map((provider) => ({
            name: provider.name,
            type: provider.type,
            endpoint: provider.endpoint ?? "",
          })),
}));

on(ModelService.method.listSupportedModels, (_input, call) => ({
  providers:
    call.scenario === "empty"
      ? []
      : Object.entries(mockProviderModels).map(([provider, models]) => ({
          provider,
          models: models.map((model) => ({
            name: model.name,
            functionCalling: model.function_calling,
          })),
        })),
}));

/**
 * One provider's catalogue.
 *
 * No operation id reaches this RPC today; it is served because the controller
 * serves it, so a deployment that overrides an operation to call it finds a fake
 * rather than an `Unimplemented`.
 */
on(ModelService.method.listProviderModels, (input) => ({
  provider: input.providerName,
  models: (mockProviderModels[input.providerName] ?? []).map((model) => model.name),
}));

// ---------------------------------------------------------------------------
// Tool servers
// ---------------------------------------------------------------------------

on(ToolService.method.listToolServers, (_input, call) => ({
  toolServers:
    call.scenario === "empty"
      ? []
      : allToolServers().map((server) => ({
          ref: server.ref,
          groupKind: server.groupKind,
          discoveredTools: server.discoveredTools,
        })),
}));

on(ToolService.method.listTools, (_input, call) => ({
  // Each row is a `database.Tool` marshalled to JSON, so the fixture is already
  // exactly what belongs inside the envelope — snake-cased names included.
  tools: call.scenario === "empty" ? [] : mockTools.map((tool) => ({
    resource: structured("Tool", tool),
  })),
}));

on(ToolService.method.createToolServer, (input) => {
  const server = valueOf<{ metadata?: { name?: string; namespace?: string } }>(
    input.resource,
    "tool server",
  );
  saveToolServer(input.type, server.metadata);
  // The RPC answers with the created resource rather than with a list row, which
  // is what the client assembles the row from.
  return { resource: structured(input.type, server) };
});

on(ToolService.method.deleteToolServer, (input) => {
  markDeleted(refString(input.ref));
  return {};
});

// ---------------------------------------------------------------------------
// Prompt libraries
// ---------------------------------------------------------------------------

const promptMessage = (detail: PromptTemplateDetail) => ({
  ref: { namespace: detail.namespace, name: detail.name },
  data: detail.data,
});

/**
 * Honours the namespace filter, because the controller does.
 *
 * It *requires* one and answers an error without it, and listing across
 * namespaces is a fan-out because the API has no wildcard. A fixture that ignored
 * the filter returned every library for each namespace asked about, so the
 * combined list showed each one repeated once per namespace on the cluster.
 */
on(PromptTemplateService.method.listPromptTemplates, (input, call) => ({
  promptTemplates:
    call.scenario === "empty"
      ? []
      : allPromptSummaries()
          .filter((row) => !input.namespace || row.namespace === input.namespace)
          .map((row) => ({
            ref: { namespace: row.namespace, name: row.name },
            keyCount: row.keyCount,
            keys: row.keys ?? [],
          })),
}));

on(PromptTemplateService.method.getPromptTemplate, (input, call) => {
  const wanted = refString(input.ref);
  const found =
    call.scenario === "empty"
      ? undefined
      : allPromptDetails().find((detail) => promptRef(detail) === wanted);
  if (!found) throw notFound(`prompt library ${wanted}`);
  return { promptTemplate: promptMessage(found) };
});

on(PromptTemplateService.method.createPromptTemplate, (input) => ({
  promptTemplate: promptMessage(
    savePrompt({
      namespace: input.ref?.namespace ?? "",
      name: input.ref?.name ?? "",
      data: input.data,
    }),
  ),
}));

on(PromptTemplateService.method.updatePromptTemplate, (input) => ({
  // An edit to a seeded library is recorded the way a create is, so the next read
  // returns what was saved rather than the fixture.
  promptTemplate: promptMessage(
    savePrompt({
      namespace: input.ref?.namespace ?? "",
      name: input.ref?.name ?? "",
      data: input.data,
    }),
  ),
}));

on(PromptTemplateService.method.deletePromptTemplate, (input) => {
  markDeleted(refString(input.ref));
  return {};
});

// ---------------------------------------------------------------------------
// Agent instances
// ---------------------------------------------------------------------------

/**
 * The two enums, back the way they arrive on the wire.
 *
 * The mirror of the conversion in `@/api/grpc/operations`, and written out in full
 * for the same reason: keyed by the generated enum, so a member added to the proto
 * fails `yarn typecheck` here rather than being served as a zero.
 */
const PB_STATE_BY_NAME: Record<AgentInstanceState, PbAgentInstanceState> = {
  unspecified: PbAgentInstanceState.UNSPECIFIED,
  creating: PbAgentInstanceState.CREATING,
  ready: PbAgentInstanceState.READY,
  suspended: PbAgentInstanceState.SUSPENDED,
  failed: PbAgentInstanceState.FAILED,
  deleting: PbAgentInstanceState.DELETING,
  deleted: PbAgentInstanceState.DELETED,
  // A state this client does not recognise cannot be sent back as anything but
  // the zero value; there is no number to invent. The fixtures never use it.
  unknown: PbAgentInstanceState.UNSPECIFIED,
};

const PB_OPERATION_BY_NAME: Record<AgentInstanceOperation, PbAgentInstanceOperation> = {
  unspecified: PbAgentInstanceOperation.UNSPECIFIED,
  create: PbAgentInstanceOperation.CREATE,
  suspend: PbAgentInstanceOperation.SUSPEND,
  resume: PbAgentInstanceOperation.RESUME,
  delete: PbAgentInstanceOperation.DELETE,
  unknown: PbAgentInstanceOperation.UNSPECIFIED,
};

function agentInstanceMessage(
  row: AgentInstance,
): MessageInitShape<typeof AgentInstanceSchema> {
  return {
    id: row.id,
    namespace: row.namespace,
    // Empty is what an unnamed conversation carries on the wire — proto3 has no
    // absent string — so it goes back empty rather than omitted, and the client
    // turns it into a title rather than treating it as a gap.
    name: row.name,
    creator: row.creator,
    harness: row.harness ? splitRef(row.harness) : undefined,
    agentTemplate: row.agentTemplate ? splitRef(row.agentTemplate) : undefined,
    // Proto3 cannot carry an absent string, so an unset field goes back as the
    // empty one the controller would also send — and `orUndefined` on the client
    // turns it back into "not reported".
    preparedRevision: row.preparedRevision ?? "",
    a2aAuthority: row.a2aAuthority ?? "",
    state: PB_STATE_BY_NAME[row.state],
    operation: PB_OPERATION_BY_NAME[row.operation],
    failure: row.failure
      ? { reason: row.failure.reason ?? "", message: row.failure.message ?? "" }
      : undefined,
    createdAt: stamp(row.createdAt),
    updatedAt: stamp(row.updatedAt),
    labels: row.labels,
  };
}

/**
 * The namespace check the controller performs, performed here too.
 *
 * `validateNamespace` in `go/core/v2/agentinstance/service.go` requires a DNS-1123
 * label, so an empty namespace is an `InvalidArgument` and emphatically *not* "every
 * namespace". A fake that quietly listed everything for an empty namespace would
 * make a page that forgot to pass one look like it worked, right up until it met a
 * cluster.
 */
const DNS_1123_LABEL = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/;

function requireNamespace(namespace: string): string {
  if (namespace.length > 63 || !DNS_1123_LABEL.test(namespace)) {
    throw new ConnectError(
      `namespace is invalid: "${namespace}" is not a DNS-1123 label. ` +
        `AgentInstanceService has no cross-namespace read.`,
      Code.InvalidArgument,
    );
  }
  return namespace;
}

/**
 * The check `validateOptionalName` performs on the two agent filters.
 *
 * A DNS-1123 subdomain when set, and "do not filter" when empty. Copied rather than
 * skipped because the mistake it catches is one this codebase makes easily: an
 * instance reports its pair as `namespace/name`, so passing that straight back as a
 * filter looks right and is an `InvalidArgument` on a cluster.
 */
const DNS_1123_SUBDOMAIN = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$/;

function requireOptionalName(field: string, value: string | undefined): string {
  const given = value ?? "";
  if (given === "") return "";
  if (given.length > 253 || !DNS_1123_SUBDOMAIN.test(given)) {
    throw new ConnectError(
      `${field} is invalid: "${given}" is not a DNS-1123 subdomain. ` +
        `It is a bare name within the request's namespace, not a namespace/name reference.`,
      Code.InvalidArgument,
    );
  }
  return given;
}

/** The name half of a `namespace/name` ref, which is what the filters match on. */
function bareRefName(ref: string | undefined): string | undefined {
  if (!ref) return undefined;
  const slash = ref.lastIndexOf("/");
  return slash === -1 ? ref : ref.slice(slash + 1);
}

/**
 * The name check the controller performs: `validateName`.
 *
 * Empty is valid and means unnamed. Surrounding whitespace is refused rather than
 * trimmed, the bound is counted in runes, and control characters are refused —
 * every rule in the controller's own words, because a fixture more permissive than
 * the backend is a fixture that hides the bug it exists to catch. That is trap 55,
 * which cost this codebase a whole green browser suite over a missing `request_id`.
 */
const MOCK_MAX_NAME_LENGTH = 200;
// eslint-disable-next-line no-control-regex
const MOCK_CONTROL_CHARACTERS = /[\u0000-\u001f\u007f-\u009f]/;

function requireInstanceName(name: string): string {
  if (name === "") return name;
  if (name.trim() !== name) {
    throw new ConnectError(
      "name must not have leading or trailing whitespace",
      Code.InvalidArgument,
    );
  }
  if ([...name].length > MOCK_MAX_NAME_LENGTH) {
    throw new ConnectError(
      `name must be at most ${MOCK_MAX_NAME_LENGTH} characters`,
      Code.InvalidArgument,
    );
  }
  if (MOCK_CONTROL_CHARACTERS.test(name)) {
    throw new ConnectError(
      "name must not contain control characters",
      Code.InvalidArgument,
    );
  }
  return name;
}

/** The id check the controller performs: `validateIdentity` parses a UUID. */
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

function requireInstanceId(id: string): string {
  if (!UUID.test(id)) {
    throw new ConnectError(
      `AgentInstance identifier is invalid: "${id}" is not a UUID.`,
      Code.InvalidArgument,
    );
  }
  return id;
}

/** The controller's own page sizes, so a request it would refuse is refused here. */
const INSTANCE_DEFAULT_PAGE_SIZE = 50;
const INSTANCE_MAX_PAGE_SIZE = 100;

/**
 * One instance, or the `NotFound` the controller answers with.
 *
 * `empty` means the same thing it means everywhere else here: a single-resource
 * read finds nothing, because that — and not an empty body — is the state a detail
 * page has to handle.
 */
function instanceFor(namespace: string, id: string, call: MockCall): AgentInstance {
  const found =
    call.scenario === "empty"
      ? undefined
      : allAgentInstances().find(
          (row) => agentInstanceRef(row) === `${namespace}/${id}`,
        );
  if (!found) throw notFound(`AgentInstance ${namespace}/${id}`);
  /*
   * Somebody else's conversation is not found, not forbidden.
   *
   * The controller resolves every single-instance read through
   * `GetAgentInstanceForUser` — `WHERE namespace = $1 AND id = $2 AND user_id = $3`
   * — so an instance created by another user simply is not there as far as this
   * caller is concerned, and the A2A gateway reads through the same call. Every
   * lifecycle operation, the rename and the delete go through here, so all of them
   * inherit the rule, exactly as they do on a cluster.
   *
   * This is the fixture rule that makes the agent page's "cannot be opened" honest:
   * without it the mock would happily open a conversation the controller refuses,
   * and the page could claim a link works when it does not.
   */
  if (found.creator !== MOCK_INSTANCE_CREATOR) {
    throw notFound(`AgentInstance ${namespace}/${id}`);
  }
  return found;
}

/**
 * A lifecycle operation, refused exactly where the controller refuses it.
 *
 * `ActorWorkflow.Suspend` claims the instance from `READY` and `Resume` from
 * `SUSPENDED`, and the claim itself fails when an operation is already in flight —
 * both come back as `ErrAgentInstanceConflict`, which the service maps to `Aborted`.
 * Reproducing that here is the difference between a disabled button that is right
 * and one that is merely decorative: the page can be tested against a fake that
 * says no for the same reasons a cluster does.
 *
 * Both complete synchronously on success, leaving `operation` cleared — so the
 * record handed back is final, not a promise to look again later.
 */
function lifecycle(
  namespace: string,
  id: string,
  call: MockCall,
  from: AgentInstanceState,
  to: AgentInstanceState,
  operation: AgentInstanceOperation,
): AgentInstance {
  const instance = instanceFor(
    requireNamespace(namespace),
    requireInstanceId(id),
    call,
  );

  if (instance.operation !== "unspecified") {
    throw new ConnectError(
      `AgentInstance has a conflicting lifecycle operation: ${instance.operation} is already in progress.`,
      Code.Aborted,
    );
  }
  if (instance.state !== from) {
    throw new ConnectError(
      `AgentInstance has a conflicting lifecycle operation: only a ${from} instance can be ${operation}ed, and this one is ${instance.state}.`,
      Code.Aborted,
    );
  }

  return saveAgentInstance({
    ...instance,
    state: to,
    operation: "unspecified",
    // A successful operation cannot leave the previous failure standing: the record
    // would then show a healthy instance beside the reason it broke.
    failure: undefined,
    updatedAt: new Date().toISOString(),
  });
}

on(AgentInstanceService.method.listAgentInstances, (input, call) => {
  const namespace = requireNamespace(input.namespace);
  if (call.scenario === "empty") return { agentInstances: [], page: {} };

  const pageSize = input.page?.limit ? input.page.limit : INSTANCE_DEFAULT_PAGE_SIZE;
  if (pageSize < 0 || pageSize > INSTANCE_MAX_PAGE_SIZE) {
    throw new ConnectError(
      `page limit must be between 1 and ${INSTANCE_MAX_PAGE_SIZE}`,
      Code.InvalidArgument,
    );
  }

  /*
   * The template and harness filters, refused where the controller refuses them.
   *
   * `validateOptionalName` requires a DNS-1123 subdomain for each when it is set,
   * and a client that sent a qualified `namespace/name` ref would be rejected —
   * which is easy to do by accident, since that is exactly the shape the instance
   * *reports* its pair in.
   */
  const templateFilter = requireOptionalName("agent_template", input.agentTemplate);
  const harnessFilter = requireOptionalName("harness", input.harness);

  const matching = allAgentInstances().filter((row) => {
    if (row.namespace !== namespace) return false;
    // Somebody else's instances are excluded unless asked for, which is what the
    // controller does with the authenticated user. Mock mode has nobody signed in,
    // so every caller is treated as one fixed person — see `MOCK_INSTANCE_CREATOR`.
    if (!input.allCreators && row.creator !== MOCK_INSTANCE_CREATOR) return false;
    /*
     * Matched on the bare name, because the controller resolves these through the
     * instance's prepared revision rather than against the ref it reports. An
     * instance with no pair matches no filter at all — the controller's own query
     * left-joins the revision, so a NULL never satisfies an equality.
     */
    if (templateFilter && bareRefName(row.agentTemplate) !== templateFilter) {
      return false;
    }
    if (harnessFilter && bareRefName(row.harness) !== harnessFilter) return false;
    return Object.entries(input.matchLabels ?? {}).every(
      ([key, value]) => row.labels[key] === value,
    );
  });

  // The token is the id to resume after — opaque to the client, which only ever
  // hands it back. The controller base64s it; there is nothing to gain by copying
  // the encoding, and a fake that did would still be unable to prove it matched.
  const after = input.page?.pageToken ?? "";
  const start = after ? matching.findIndex((row) => row.id === after) + 1 : 0;
  const page = matching.slice(start, start + pageSize);
  const more = start + pageSize < matching.length;

  return {
    agentInstances: page.map(agentInstanceMessage),
    page: { nextPageToken: more ? (page[page.length - 1]?.id ?? "") : "" },
  };
});

on(AgentInstanceService.method.getAgentInstance, (input, call) => ({
  agentInstance: agentInstanceMessage(
    instanceFor(
      requireNamespace(input.namespace),
      requireInstanceId(input.agentInstanceId),
      call,
    ),
  ),
}));

on(AgentInstanceService.method.suspendAgentInstance, (input, call) => ({
  agentInstance: agentInstanceMessage(
    lifecycle(input.namespace, input.agentInstanceId, call, "ready", "suspended", "suspend"),
  ),
}));

on(AgentInstanceService.method.resumeAgentInstance, (input, call) => ({
  agentInstance: agentInstanceMessage(
    lifecycle(input.namespace, input.agentInstanceId, call, "suspended", "ready", "resume"),
  ),
}));

on(AgentInstanceService.method.createAgentInstance, (input, call) => {
  const namespace = requireNamespace(input.namespace);
  if (!input.harness.trim() || !input.agentTemplate.trim()) {
    throw new ConnectError(
      "a harness and an agent template are both required",
      Code.InvalidArgument,
    );
  }
  /*
   * The controller's own rule, copied rather than approximated.
   *
   * `request_id` is required and this fixture used to accept its absence, so a build
   * that never sent one passed every mock test and failed against a cluster with
   * *"request_id must be 1-128 characters without surrounding whitespace"*. That is
   * exactly the fixture-agrees-with-itself failure this codebase keeps having to
   * undo, so the rule is enforced here in the same words.
   */
  const requestId = input.requestId;
  if (
    requestId === "" ||
    requestId.trim() !== requestId ||
    requestId.length > 128
  ) {
    throw new ConnectError(
      "request_id must be 1-128 characters without surrounding whitespace",
      Code.InvalidArgument,
    );
  }
  // Validated on create as well as on rename, because the controller validates it in
  // both places — a form that could smuggle a name past create and only meet the
  // rule on the second edit would be a form tested against the wrong backend.
  const name = requireInstanceName(input.name ?? "");
  if (call.scenario === "error") {
    // The controller's own refusal for a pair whose prepared revision is not ready,
    // which is the failure a reader is most likely to meet.
    throw new ConnectError(
      `no ready prepared revision for ${input.harness}/${input.agentTemplate}`,
      Code.FailedPrecondition,
    );
  }

  /*
   * A created instance is `READY` immediately here, where the controller takes a few
   * seconds.
   *
   * That is a fixture being useful rather than a fixture lying: the interesting states
   * are already reachable — `mockAgentInstances` carries a `creating` one and a
   * `suspended` one — and making every create sit in `creating` would mean no
   * fixture-backed test could ever reach a conversation.
   */
  const created = {
    // A UUID, because the controller parses one: `validateIdentity` rejects
    // anything else, so a fixture id shaped differently would pass here and fail
    // against a cluster — which is this codebase's most expensive recurring bug.
    id: crypto.randomUUID(),
    namespace,
    name,
    creator: MOCK_INSTANCE_CREATOR,
    harness: `${namespace}/${input.harness}`,
    agentTemplate: `${namespace}/${input.agentTemplate}`,
    preparedRevision: "rev-mock",
    a2aAuthority: `${input.agentTemplate}.${namespace}.svc.cluster.local:8080`,
    state: "ready" as const,
    operation: "unspecified" as const,
    createdAt: new Date().toISOString(),
    updatedAt: new Date().toISOString(),
    labels: {},
  };
  saveAgentInstance(created);
  return { agentInstance: agentInstanceMessage(created) };
});

/*
 * Retitling a conversation.
 *
 * Through `instanceFor`, so it inherits the creator scoping every other read has:
 * a conversation somebody else started cannot be renamed, and it is refused as
 * `NotFound` rather than as a permission error, because that is what a query
 * filtered on `user_id` produces.
 *
 * An empty name is accepted and clears the title. That is not laxity — it is the
 * controller's rule, and it is the only way a name can be taken away.
 */
on(AgentInstanceService.method.updateAgentInstanceName, (input, call) => {
  const instance = instanceFor(
    requireNamespace(input.namespace),
    requireInstanceId(input.agentInstanceId),
    call,
  );
  const name = requireInstanceName(input.name);
  return {
    agentInstance: agentInstanceMessage(
      saveAgentInstance({
        ...instance,
        name,
        // The rename writes the column, and the controller stamps the row.
        updatedAt: new Date().toISOString(),
      }),
    ),
  };
});

on(AgentInstanceService.method.deleteAgentInstance, (input, call) => {
  const instance = instanceFor(
    requireNamespace(input.namespace),
    requireInstanceId(input.agentInstanceId),
    call,
  );
  markDeleted(agentInstanceRef(instance));
  // The record as it stood, which is what the controller answers with: the caller
  // asked for it to go and is told what went.
  return { agentInstance: agentInstanceMessage(instance) };
});

/*
 * Share links over an instance.
 *
 * Tokens are handed out in full here and only their existence is remembered, which
 * is the opposite of the controller — it stores a digest and can never show a token
 * again. That difference is deliberate and bounded: this backend lives in one
 * browser tab, and a fixture that hashed its tokens could not then serve the link
 * the spec had just been handed.
 */
on(AgentInstanceService.method.listAgentInstanceShares, (input, call) => {
  const instance = instanceFor(
    requireNamespace(input.namespace),
    requireInstanceId(input.agentInstanceId),
    call,
  );
  return {
    shares: readInstanceShares()
      .filter((share) => share.agentInstanceId === instance.id)
      .map(instanceShareMessage),
    page: {},
  };
});

on(AgentInstanceService.method.createAgentInstanceShare, (input, call) => {
  const instance = instanceFor(
    requireNamespace(input.namespace),
    requireInstanceId(input.agentInstanceId),
    call,
  );
  const { share, token } = createInstanceShare(
    instance.namespace,
    instance.id,
    input.permission === PbSharePermission.READ_WRITE ? "readWrite" : "readOnly",
  );
  return { share: instanceShareMessage(share), token };
});

on(AgentInstanceService.method.revokeAgentInstanceShare, (input) => {
  requireNamespace(input.namespace);
  if (!revokeInstanceShare(input.shareId)) {
    throw new ConnectError(`share ${input.shareId} not found`, Code.NotFound);
  }
  return {};
});

const instanceShareMessage = (share: AgentInstanceShare) => ({
  id: share.id,
  namespace: share.namespace,
  agentInstanceId: share.agentInstanceId,
  permission:
    share.permission === "readWrite"
      ? PbSharePermission.READ_WRITE
      : PbSharePermission.READ_ONLY,
  createdAt: timestampFromDate(new Date(share.createdAt)),
});

/*
 * `CheckpointService` still has no fake, deliberately: nothing in the app calls it,
 * and an unregistered RPC answers `Unimplemented` naming itself. A fixture for a
 * feature nobody built is one that will be wrong by the time somebody does.
 */

// ---------------------------------------------------------------------------
// Harnesses and agent templates
// ---------------------------------------------------------------------------

on(HarnessService.method.listHarnesses, (input, call) => {
  if (call.scenario === "empty") return { harnesses: [] };
  const scope = input.namespace.trim();
  return {
    harnesses: allHarnesses()
      .filter((harness) => scope === "" || harness.namespace === scope)
      .map((harness) => ({
        ref: { namespace: harness.namespace, name: harness.name },
        resource: structured("Harness", harness.resource as unknown as JsonObject),
        runtime: harness.runtime,
        workloadImage: harness.workloadImage,
        ready: harness.ready,
      })),
  };
});

const agentTemplateMessage = (template: AgentTemplate) => ({
  ref: { namespace: template.namespace, name: template.name },
  resource: structured("AgentTemplate", template.resource as unknown as JsonObject),
  modelConfigRef: refPair(template.modelConfigRef),
  description: template.description,
  admittingHarnesses: template.admittingHarnesses,
});

/** The template at this ref, or the controller's own `NotFound`. */
function templateFor(namespace: string, name: string): AgentTemplate {
  const found = allAgentTemplates().find(
    (template) => template.namespace === namespace && template.name === name,
  );
  if (!found) {
    throw new ConnectError(`AgentTemplate ${namespace}/${name} not found`, Code.NotFound);
  }
  return found;
}

/**
 * Turns a written resource into the record the list serves.
 *
 * The denormalised fields — `modelConfigRef`, `description` — are recomputed from
 * the spec rather than taken from the caller, because on a cluster the controller
 * computes them. A fixture that echoed what it was handed would let the two drift
 * and would agree with a client that had built them wrongly.
 */
/**
 * A written harness, as this backend will answer it back.
 *
 * The denormalised fields — `runtime`, `workloadImage`, `ready` — are what the
 * controller computes from the spec, so they are computed here too rather than taken
 * from the request: a fixture that echoed whatever it was sent would let a client claim
 * a harness was ready by saying so.
 *
 * `ready` is false on a freshly created harness, which is what a cluster reports too:
 * the controller has not observed it yet. That is a state the tab has to render
 * correctly, and it would be missed entirely if new harnesses arrived ready.
 */
function harnessFromResource(namespace: string, name: string, resource: JsonObject): Harness {
  const spec = (resource.spec ?? {}) as {
    kagent?: unknown;
    codex?: unknown;
    claude?: unknown;
    workload?: { image?: string };
  };
  const runtime = ["kagent", "codex", "claude"].find(
    (key) => (spec as Record<string, unknown>)[key] !== undefined,
  );
  return {
    ref: `${namespace}/${name}`,
    namespace,
    name,
    runtime: runtime ?? "",
    workloadImage: spec.workload?.image ?? "",
    ready: false,
    resource: {
      ...(resource as unknown as Harness["resource"]),
      metadata: {
        ...((resource.metadata ?? {}) as unknown as Harness["resource"]["metadata"]),
        name,
        namespace,
      },
    },
  };
}

function templateFromResource(
  namespace: string,
  name: string,
  resource: JsonObject,
): AgentTemplate {
  const spec = (resource.spec ?? {}) as unknown as AgentTemplate["resource"]["spec"];
  return {
    ref: `${namespace}/${name}`,
    namespace,
    name,
    modelConfigRef: `${namespace}/${spec.modelConfig?.name ?? ""}`,
    description: spec.description ?? "",
    // Recomputed by `saveAgentTemplate` from the labels; whatever is passed here is
    // replaced.
    admittingHarnesses: [],
    resource: {
      ...(resource as unknown as AgentTemplate["resource"]),
      metadata: {
        ...((resource.metadata ?? {}) as unknown as AgentTemplate["resource"]["metadata"]),
        name,
        namespace,
      },
    },
  };
}

on(AgentTemplateService.method.listAgentTemplates, (input, call) => {
  if (call.scenario === "empty") return { agentTemplates: [] };
  const scope = input.namespace.trim();
  return {
    agentTemplates: allAgentTemplates()
      .filter((template) => scope === "" || template.namespace === scope)
      .map(agentTemplateMessage),
  };
});

on(AgentTemplateService.method.getAgentTemplate, (input) => ({
  agentTemplate: agentTemplateMessage(
    templateFor(requireNamespace(input.ref?.namespace ?? ""), input.ref?.name ?? ""),
  ),
}));

on(HarnessService.method.createHarness, (input, call) => {
  const namespace = requireNamespace(input.ref?.namespace ?? "");
  const name = input.ref?.name ?? "";
  if (name === "") {
    throw new ConnectError("Harness namespace and name are required", Code.InvalidArgument);
  }
  if (call.scenario === "error") {
    throw new ConnectError("the mock backend was asked to fail", Code.Internal);
  }
  const value = (input.resource?.value ?? {}) as JsonObject;
  const spec = (value.spec ?? {}) as {
    kagent?: unknown;
    codex?: unknown;
    claude?: unknown;
    workload?: { image?: string };
    substrate?: { workerPoolRef?: { name?: string } };
  };

  /*
   * The CRD's own rules, refused here rather than accepted and forgotten.
   *
   * A fixture that took anything would let a form ship that builds a resource a
   * cluster rejects — which is the failure this backend exists to catch, and the one
   * that is invisible until somebody tries it for real.
   */
  const adapters = ["kagent", "codex", "claude"].filter(
    (key) => (spec as Record<string, unknown>)[key] !== undefined,
  );
  if (adapters.length !== 1) {
    throw new ConnectError(
      "exactly one of kagent, codex, or claude must be specified",
      Code.InvalidArgument,
    );
  }
  if (!/^[^\s@]+@sha256:[a-f0-9]{64}$/.test(spec.workload?.image ?? "")) {
    throw new ConnectError(
      "spec.workload.image must be pinned by sha256 digest",
      Code.InvalidArgument,
    );
  }
  if (!spec.substrate?.workerPoolRef?.name) {
    throw new ConnectError("workerPoolRef name must not be empty", Code.InvalidArgument);
  }

  const saved = saveHarness(harnessFromResource(namespace, name, value));
  return {
    harness: {
      ref: { namespace: saved.namespace, name: saved.name },
      resource: structured("Harness", saved.resource as unknown as JsonObject),
      runtime: saved.runtime,
      workloadImage: saved.workloadImage,
      ready: saved.ready,
    },
  };
});

on(HarnessService.method.deleteHarness, (input) => {
  const namespace = requireNamespace(input.ref?.namespace ?? "");
  const name = input.ref?.name ?? "";
  if (!allHarnesses().some((row) => row.namespace === namespace && row.name === name)) {
    throw new ConnectError(`Harness ${namespace}/${name} not found`, Code.NotFound);
  }
  markDeleted(`${namespace}/${name}`);
  return {};
});

on(AgentTemplateService.method.createAgentTemplate, (input, call) => {
  const namespace = requireNamespace(input.ref?.namespace ?? "");
  const name = input.ref?.name ?? "";
  if (name === "") {
    throw new ConnectError("AgentTemplate namespace and name are required", Code.InvalidArgument);
  }
  if (call.scenario === "error") {
    throw new ConnectError("the mock backend was asked to fail", Code.Internal);
  }
  const value = (input.resource?.value ?? {}) as JsonObject;
  // The controller's own rule: the one required spec field.
  const spec = (value.spec ?? {}) as { modelConfig?: { name?: string } };
  if (!spec.modelConfig?.name) {
    throw new ConnectError("spec.modelConfig is required", Code.InvalidArgument);
  }
  return {
    agentTemplate: agentTemplateMessage(
      saveAgentTemplate(templateFromResource(namespace, name, value)),
    ),
  };
});

on(AgentTemplateService.method.updateAgentTemplate, (input, call) => {
  const namespace = requireNamespace(input.ref?.namespace ?? "");
  const name = input.ref?.name ?? "";
  // Reads first, so updating something that is not there fails the way it would on
  // a cluster rather than quietly creating it.
  templateFor(namespace, name);
  if (call.scenario === "error") {
    throw new ConnectError("the mock backend was asked to fail", Code.Internal);
  }
  const value = (input.resource?.value ?? {}) as JsonObject;
  return {
    agentTemplate: agentTemplateMessage(
      saveAgentTemplate(templateFromResource(namespace, name, value)),
    ),
  };
});

on(AgentTemplateService.method.deleteAgentTemplate, (input) => {
  const namespace = requireNamespace(input.ref?.namespace ?? "");
  const name = input.ref?.name ?? "";
  templateFor(namespace, name);
  markDeleted(`${namespace}/${name}`);
  return {};
});

// ---------------------------------------------------------------------------
// Cluster
// ---------------------------------------------------------------------------

on(SystemService.method.listNamespaces, (_input, call) => ({
  namespaces: call.scenario === "empty" ? [] : mockNamespaces,
}));

on(SystemService.method.getSubstrateStatus, (input, call) => {
  // `empty` is a cluster with the substrate switched off rather than a truncated
  // inventory: every list absent and `enabled` false is a state the page renders,
  // where half an inventory is not.
  if (call.scenario === "empty") return { enabled: false };

  const status = mockSubstrateStatus;

  /*
   * The requested scope, narrowed the way the controller narrows it.
   *
   * `system.Service.GetSubstrateStatus` lists the Kubernetes halves per namespace and
   * filters the ate-api halves by the actor's template namespace and the worker's pod
   * namespace — keeping a row whose namespace is blank, because ate-api is not obliged
   * to say. An empty request is every watched namespace, which for a fixture backend is
   * everything it has. Filtering here rather than answering the whole inventory whatever
   * was asked for is the difference between a scope control that is observably a filter
   * and one that is decoration.
   */
  const scope = input.namespace.trim();
  const inScope = (namespace: string | undefined) =>
    scope === "" || !namespace || namespace === scope;

  const workerPools = status.workerPools.filter((pool) => inScope(pool.namespace));
  const actorTemplates = status.actorTemplates.filter((template) =>
    inScope(template.namespace),
  );
  const actors = status.actors.filter((actor) => inScope(actor.actorTemplateNamespace));
  const workers = status.workers.filter((worker) => inScope(worker.workerNamespace));

  return {
    enabled: status.enabled,
    ateApiError: status.ateApiError ?? "",
    workerPools: workerPools.map((pool) => ({
      namespace: pool.namespace,
      name: pool.name,
      replicas: pool.replicas ?? 0,
      ateomImage: pool.ateomImage ?? "",
    })),
    actorTemplates: actorTemplates.map((template) => ({
      namespace: template.namespace,
      name: template.name,
      phase: template.phase ?? "",
      goldenActorId: template.goldenActorId ?? "",
      goldenSnapshot: template.goldenSnapshot ?? "",
      sandboxClass: template.sandboxClass ?? "",
      workerSelector: template.workerSelector ?? "",
      harnessName: template.harnessName ?? "",
    })),
    actors: actors.map((actor) => ({
      actorId: actor.actorId,
      atespace: actor.atespace ?? "",
      status: actor.status ?? "",
      actorTemplateNamespace: actor.actorTemplateNamespace ?? "",
      actorTemplateName: actor.actorTemplateName ?? "",
      ateomPodNamespace: actor.ateomPodNamespace ?? "",
      ateomPodName: actor.ateomPodName ?? "",
      ateomPodIp: actor.ateomPodIp ?? "",
      latestSnapshot: actor.latestSnapshot ?? "",
      workerPoolName: actor.workerPoolName ?? "",
      inProgressSnapshot: actor.inProgressSnapshot ?? "",
      // `int64` on the wire.
      version: BigInt(actor.version ?? 0),
    })),
    workers: workers.map((worker) => ({
      workerNamespace: worker.workerNamespace,
      workerPool: worker.workerPool,
      workerPod: worker.workerPod,
      actorNamespace: worker.actorNamespace ?? "",
      actorTemplate: worker.actorTemplate ?? "",
      actorId: worker.actorId ?? "",
      actorStatus: worker.actorStatus ?? "",
      ip: worker.ip ?? "",
      version: BigInt(worker.version ?? 0),
    })),
  };
});

/**
 * The build, obviously fake.
 *
 * No operation calls it yet. It says "mock" rather than a plausible version number
 * because a version string is exactly the sort of thing that gets pasted into a
 * bug report.
 */

on(SystemService.method.getVersion, () => ({
  kagentVersion: "0.0.0-mock",
  gitCommit: "mockmock",
  buildDate: "1970-01-01T00:00:00Z",
}));

/**
 * Who is signed in, kept consistent with the `?auth=` axis.
 *
 * Nothing calls this RPC yet — the app reads oauth2-proxy's `/oauth2/userinfo`,
 * which `handlers.ts` answers — but the two must not be able to disagree, so both
 * read the same scenario. `unsecured` reports nobody, because in mock mode there
 * is no backend to have signed in to.
 */
on(SystemService.method.getCurrentUser, () => {
  const claims: JsonObject =
    currentAuthScenario() === "authenticated"
      ? {
          email: "alice@example.com",
          preferred_username: "alice",
          groups: ["platform"],
        }
      : {};
  return { claims };
});

// ---------------------------------------------------------------------------
// Counting calls, for the browser suite
// ---------------------------------------------------------------------------

/**
 * How many times each RPC has been called during this page's life.
 *
 * Published on `window` because a browser test asking "did the page poll?" has
 * nothing else to look at: there is no request on the wire under a substituted
 * transport, and `page.on("request")` never fires. Counting operations is also
 * what those tests actually mean — "polling refreshed everything on the page" is
 * a statement about reads, not about HTTP.
 *
 * ## Why every key is seeded
 *
 * Seeded with a zero for every registered fake, and never created on demand. A
 * counter that sprang into existence on first read would answer `0` for a
 * misspelled RPC name, and a test whose subject silently reads zero forever is
 * worse than no test — that exact failure is recorded in the handoff, from the
 * other direction, as an instrument that passed a feature which had stopped
 * working. So an unknown key is absent, and the helper that reads these
 * (`playwright/helpers/mockCalls.ts`) treats absent as an error.
 */
const callCounts: Record<string, number> = {};

/** Where the counts live, for the browser suite to read. */
export const MOCK_CALLS_PROPERTY = "__kagentMockCalls";

function countCall(rpc: string): void {
  // Only counts what was registered. An unregistered RPC has already thrown
  // `Unimplemented` by the time this runs, so there is nothing to record.
  if (rpc in callCounts) callCounts[rpc] += 1;
}

function publishCallCounts(): void {
  for (const rpc of fakes.keys()) callCounts[rpc] = 0;
  if (typeof window === "undefined") return;
  (window as unknown as Record<string, unknown>)[MOCK_CALLS_PROPERTY] = callCounts;
}

// Runs once, after every fake above has been registered — which is why it is the
// last thing in the file.
publishCallCounts();
