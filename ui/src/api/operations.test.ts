/**
 * Every operation, exercised against the real gRPC services running in-process.
 *
 * This replaces the two suites that checked REST paths against the fixture
 * backend. They existed because a client method whose endpoint had no handler
 * answered 404 and nothing noticed until someone wired it up; the same risk is
 * here, in a different shape — a method that names the wrong RPC, or reads a field
 * the message does not carry, fails only when a page asks for it.
 *
 * `createRouterTransport` serves the generated service descriptors in-process, so
 * a handler below *is* the controller as far as the client can tell: the message
 * it receives is the one the client encoded, and the message it returns is decoded
 * against the same descriptor. Getting the RPC or a field name wrong fails here.
 *
 * It goes in through `setApiTransport`, which replaces the *inner* transport only —
 * so the app's own interceptors still run above it, and the header-attaching and
 * transform wiring is exercised rather than bypassed. That is the same path the
 * fixtures take, which is the point: a substituted transport that skipped the chain
 * would let mock mode and live mode drift apart silently.
 *
 * What is asserted, deliberately, is *which operation ran, against what identity,
 * and what came back* — not the bytes. A test that pinned the wire format would
 * fail every time buf regenerated and prove nothing about the app.
 */

import { afterEach, describe, expect, it } from "vitest";
import { Code, ConnectError, createRouterTransport } from "@connectrpc/connect";
import type { ConnectRouter } from "@connectrpc/connect";
import { AgentKind, AgentService } from "@/generated/kagent/api/v1alpha1/agents_pb";
import { ModelService } from "@/generated/kagent/api/v1alpha1/models_pb";
import { ToolService } from "@/generated/kagent/api/v1alpha1/tools_pb";
import { PromptTemplateService } from "@/generated/kagent/api/v1alpha1/prompts_pb";
import { SystemService } from "@/generated/kagent/api/v1alpha1/system_pb";
import {
  AgentInstanceOperation as PbAgentInstanceOperation,
  AgentInstanceService,
  AgentInstanceState as PbAgentInstanceState,
} from "@/generated/kagent/api/v1alpha1/agent_instances_pb";
import { ApiError, isNotFound } from "./ApiError";
import { apiClient } from "./client";
import { clearApiExtensions } from "./extensionPoints";
import { registerAuthTokenSource, setApiTransport } from "./transport";
import { registerApiTransform } from "./extensionPoints";

/** Installs in-process services as the transport the client calls through. */
function serve(routes: (router: ConnectRouter) => void): void {
  setApiTransport(createRouterTransport(routes));
}

afterEach(() => {
  setApiTransport(undefined);
  clearApiExtensions();
});

/** A `SandboxAgent` custom resource, as the controller marshals one. */
function sandboxAgentResource(name: string, namespace = "kagent") {
  return {
    apiVersion: "kagent.dev/v1alpha3",
    kind: "SandboxAgent",
    metadata: { name, namespace, creationTimestamp: "2026-01-01T00:00:00Z" },
    spec: { description: `the ${name} agent` },
  };
}

function agentMessage(name: string, namespace = "kagent", kind = AgentKind.SANDBOX_AGENT) {
  return {
    ref: { namespace, name },
    kind,
    resource: {
      apiVersion: "kagent.dev/v1alpha3",
      kind: kind === AgentKind.AGENT_HARNESS ? "AgentHarness" : "SandboxAgent",
      value: sandboxAgentResource(name, namespace),
    },
    id: `id-${name}`,
    model: "gpt-4.1",
    modelProvider: "OpenAI",
    modelConfigRef: { namespace: "kagent", name: "default" },
    tools: [],
    ready: true,
    accepted: true,
  };
}

describe("agents.list", () => {
  it("reads the custom resource out of the StructuredObject envelope", async () => {
    serve(({ service }) => {
      service(AgentService, { listAgents: () => ({ agents: [agentMessage("k8s")] }) });
    });

    const [row] = await apiClient.agents.list();

    expect(row.agent.metadata.name).toBe("k8s");
    expect(row.agent.spec.description).toBe("the k8s agent");
    expect(row.modelConfigRef).toBe("kagent/default");
    expect(row.agentKind).toBe("SandboxAgent");
    // Absent repeated fields, normalised at the boundary so no render site needs
    // its own `?? []`.
    expect(row.tools).toEqual([]);
    expect(row.memoryRefs).toEqual([]);
  });

  it("orders rows by namespace then name, descending", async () => {
    serve(({ service }) => {
      service(AgentService, {
        listAgents: () => ({
          agents: [
            agentMessage("alpha", "one"),
            agentMessage("beta", "two"),
            agentMessage("gamma", "one"),
          ],
        }),
      });
    });

    expect(
      (await apiClient.agents.list()).map(
        (row) => `${row.agent.metadata.namespace}/${row.agent.metadata.name}`,
      ),
    ).toEqual(["two/beta", "one/gamma", "one/alpha"]);
  });

  it("reports an empty cluster as empty rather than as a failure", async () => {
    serve(({ service }) => {
      service(AgentService, { listAgents: () => ({ agents: [] }) });
    });
    await expect(apiClient.agents.list()).resolves.toEqual([]);
  });

  it("fails loudly when the controller does", async () => {
    serve(({ service }) => {
      service(AgentService, {
        listAgents: () => {
          throw new ConnectError("informer cache is cold", Code.Internal);
        },
      });
    });

    const error = await apiClient.agents.list().catch((caught: unknown) => caught);
    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).status).toBe(500);
    expect((error as ApiError).code).toBe("Internal");
    expect((error as ApiError).url).toBe("AgentService/ListAgents");
  });
});

describe("agents.get", () => {
  it("asks for the sandbox agent by ref", async () => {
    const asked: string[] = [];
    serve(({ service }) => {
      service(AgentService, {
        getSandboxAgent: (request) => {
          asked.push(`${request.ref?.namespace}/${request.ref?.name}`);
          return { agent: agentMessage("k8s") };
        },
      });
    });

    await expect(apiClient.agents.get("kagent", "k8s")).resolves.toMatchObject({
      agentKind: "SandboxAgent",
    });
    expect(asked).toEqual(["kagent/k8s"]);
  });

  /**
   * The one place the id-to-RPC map is not a map. A detail page holds a namespace
   * and a name and cannot know which of the two resources it is looking at, so the
   * sandbox agent is tried and the harness is tried after a genuine 404.
   */
  it("falls back to the harness when there is no sandbox agent by that name", async () => {
    const asked: string[] = [];
    serve(({ service }) => {
      service(AgentService, {
        getSandboxAgent: () => {
          asked.push("sandbox");
          throw new ConnectError("no such agent", Code.NotFound);
        },
        getAgentHarness: () => {
          asked.push("harness");
          return { agent: agentMessage("claude", "kagent", AgentKind.AGENT_HARNESS) };
        },
      });
    });

    await expect(apiClient.agents.get("kagent", "claude")).resolves.toMatchObject({
      agentKind: "AgentHarness",
    });
    expect(asked).toEqual(["sandbox", "harness"]);
  });

  // Retrying on anything but a 404 would report "no such agent" for a permission
  // problem, which sends the reader looking for the wrong thing.
  it("does not retry a failure that is not a 404", async () => {
    const asked: string[] = [];
    serve(({ service }) => {
      service(AgentService, {
        getSandboxAgent: () => {
          asked.push("sandbox");
          throw new ConnectError("not allowed", Code.PermissionDenied);
        },
        getAgentHarness: () => {
          asked.push("harness");
          return { agent: agentMessage("claude") };
        },
      });
    });

    const error = await apiClient.agents
      .get("kagent", "claude")
      .catch((caught: unknown) => caught);
    expect((error as ApiError).status).toBe(403);
    expect(asked).toEqual(["sandbox"]);
  });

  it("goes straight to the right RPC when the caller knows the kind", async () => {
    const asked: string[] = [];
    serve(({ service }) => {
      service(AgentService, {
        getSandboxAgent: () => {
          asked.push("sandbox");
          throw new ConnectError("should not be asked", Code.Internal);
        },
        getAgentHarness: () => {
          asked.push("harness");
          return { agent: agentMessage("claude", "kagent", AgentKind.AGENT_HARNESS) };
        },
      });
    });

    await apiClient.agents.get("kagent", "claude", { kind: "AgentHarness" });
    expect(asked).toEqual(["harness"]);
  });

  it("reports a missing agent as a 404 the caller can recognise", async () => {
    serve(({ service }) => {
      service(AgentService, {
        getSandboxAgent: () => {
          throw new ConnectError("gone", Code.NotFound);
        },
        getAgentHarness: () => {
          throw new ConnectError("gone", Code.NotFound);
        },
      });
    });

    const error = await apiClient.agents
      .get("kagent", "ghost")
      .catch((caught: unknown) => caught);
    expect(isNotFound(error)).toBe(true);
  });
});

describe("agents.create and agents.update", () => {
  const draft = {
    apiVersion: "kagent.dev/v1alpha3",
    kind: "SandboxAgent",
    metadata: { name: "written-agent", namespace: "kagent" },
    spec: { type: "Declarative" as const, description: "created by a test" },
  };

  it("sends the resource and the ref, and answers with the created resource", async () => {
    let received: unknown;
    serve(({ service }) => {
      service(AgentService, {
        createSandboxAgent: (request) => {
          received = { ref: request.ref, resource: request.resource };
          return { agent: agentMessage("written-agent") };
        },
      });
    });

    const created = await apiClient.agents.create(draft);

    expect(received).toMatchObject({
      ref: { namespace: "kagent", name: "written-agent" },
      // The kind on the envelope is checked by the handler, not ignored:
      // `structuredobject.ToGo` rejects a mismatch outright.
      resource: { kind: "SandboxAgent", value: { spec: { description: "created by a test" } } },
    });
    expect(created.metadata.name).toBe("written-agent");
  });

  it("creates a harness through the harness RPC when the draft says so", async () => {
    const asked: string[] = [];
    serve(({ service }) => {
      service(AgentService, {
        createSandboxAgent: () => {
          asked.push("sandbox");
          return { agent: agentMessage("x") };
        },
        createAgentHarness: () => {
          asked.push("harness");
          return { agent: agentMessage("x", "kagent", AgentKind.AGENT_HARNESS) };
        },
      });
    });

    await apiClient.agents.create({ ...draft, kind: "AgentHarness" });
    expect(asked).toEqual(["harness"]);
  });

  it("replaces a sandbox agent", async () => {
    serve(({ service }) => {
      service(AgentService, {
        updateSandboxAgent: (request) => ({
          agent: {
            ...agentMessage("written-agent"),
            resource: { ...request.resource! },
          },
        }),
      });
    });

    const updated = await apiClient.agents.update({
      ...draft,
      spec: { ...draft.spec, description: "revised" },
    });
    expect(updated.spec.description).toBe("revised");
  });

  /**
   * `agents.proto` has no `UpdateAgentHarness`. Refusing here rather than sending
   * something is the difference between "this cannot be edited" and an
   * unimplemented-method error about a call the reader never asked for.
   */
  it("refuses to update a harness, without calling anything", async () => {
    let called = false;
    serve(({ service }) => {
      service(AgentService, {
        updateSandboxAgent: () => {
          called = true;
          return { agent: agentMessage("x") };
        },
      });
    });

    const error = await apiClient.agents
      .update({ ...draft, kind: "AgentHarness" })
      .catch((caught: unknown) => caught);

    expect(error).toBeInstanceOf(ApiError);
    expect((error as ApiError).message).toMatch(/cannot be edited/i);
    expect(called).toBe(false);
  });

  it("deletes through the RPC for the kind it was given", async () => {
    const asked: string[] = [];
    serve(({ service }) => {
      service(AgentService, {
        deleteSandboxAgent: () => {
          asked.push("sandbox");
          return {};
        },
        deleteAgentHarness: () => {
          asked.push("harness");
          return {};
        },
      });
    });

    await apiClient.agents.remove("kagent", "a");
    await apiClient.agents.remove("kagent", "b", "AgentHarness");
    expect(asked).toEqual(["sandbox", "harness"]);
  });
});

describe("model configurations", () => {
  const modelConfigMessage = (name: string, model: string) => ({
    ref: { namespace: "kagent", name },
    resource: {
      apiVersion: "kagent.dev/v1alpha3",
      kind: "ModelConfig",
      value: {
        apiVersion: "kagent.dev/v1alpha3",
        kind: "ModelConfig",
        metadata: { name, namespace: "kagent" },
        spec: { model, provider: "OpenAI" },
      },
    },
  });

  it("reads the spec out of the resource and the ref from beside it", async () => {
    serve(({ service }) => {
      service(ModelService, {
        listModelConfigs: () => ({
          modelConfigs: [modelConfigMessage("default", "gpt-4.1")],
        }),
      });
    });

    expect(await apiClient.models.list()).toEqual([
      { ref: "kagent/default", spec: { model: "gpt-4.1", provider: "OpenAI" } },
    ]);
  });

  it("sends a whole ModelConfig resource on create, with the ref beside it", async () => {
    let received: unknown;
    serve(({ service }) => {
      service(ModelService, {
        createModelConfig: (request) => {
          received = request;
          return { modelConfig: modelConfigMessage("new", "gpt-4.1") };
        },
      });
    });

    await apiClient.models.create({
      ref: "kagent/new",
      apiKey: "sk-test",
      spec: { model: "gpt-4.1", provider: "OpenAI" },
    });

    expect(received).toMatchObject({
      ref: { namespace: "kagent", name: "new" },
      apiKey: "sk-test",
      resource: {
        kind: "ModelConfig",
        value: { metadata: { name: "new", namespace: "kagent" } },
      },
    });
  });

  /**
   * `api_key` is `optional string` on the update request, so omitted and empty are
   * different requests: omitted leaves the stored key alone, empty clears it. A
   * form that did not touch the key must not clear it.
   */
  it("omits the api key on update when the caller did not supply one", async () => {
    const seen: Array<string | undefined> = [];
    serve(({ service }) => {
      service(ModelService, {
        updateModelConfig: (request) => {
          seen.push(request.apiKey);
          return { modelConfig: modelConfigMessage("default", "gpt-4.1") };
        },
      });
    });

    const spec = { model: "gpt-4.1", provider: "OpenAI" };
    await apiClient.models.update("kagent", "default", { ref: "kagent/default", spec });
    await apiClient.models.update("kagent", "default", {
      ref: "kagent/default",
      spec,
      apiKey: "",
    });

    expect(seen).toEqual([undefined, ""]);
  });

  /**
   * One list on screen, two RPCs behind it. A picker offering only the stock
   * providers would hide whatever the operator added; one offering only the
   * configured providers would be empty on a fresh install.
   */
  it("merges the stock providers with the configured ones", async () => {
    serve(({ service }) => {
      service(ModelService, {
        listSupportedModelProviders: () => ({
          providers: [
            {
              name: "OpenAI",
              type: "openai",
              requiredParams: ["apiKey"],
              optionalParams: ["baseUrl"],
            },
          ],
        }),
        listConfiguredProviders: () => ({
          providers: [
            { name: "in-house", type: "openai", endpoint: "https://llm.internal" },
          ],
        }),
      });
    });

    expect(await apiClient.models.providers()).toEqual([
      {
        name: "OpenAI",
        type: "openai",
        requiredParams: ["apiKey"],
        optionalParams: ["baseUrl"],
        source: "stock",
      },
      {
        name: "in-house",
        type: "openai",
        requiredParams: [],
        optionalParams: [],
        source: "configured",
        endpoint: "https://llm.internal",
      },
    ]);
  });

  it("groups the model catalogue by provider, keeping the names the pickers read", async () => {
    serve(({ service }) => {
      service(ModelService, {
        listSupportedModels: () => ({
          providers: [
            {
              provider: "OpenAI",
              models: [
                { name: "gpt-4.1", functionCalling: true },
                { name: "o4-mini", functionCalling: false },
              ],
            },
          ],
        }),
      });
    });

    expect(await apiClient.models.providerModels()).toEqual({
      OpenAI: [
        { name: "gpt-4.1", function_calling: true },
        { name: "o4-mini", function_calling: false },
      ],
    });
  });
});

describe("tool servers and tools", () => {
  it("lists tool servers with the tools the controller discovered", async () => {
    serve(({ service }) => {
      service(ToolService, {
        listToolServers: () => ({
          toolServers: [
            {
              ref: "kagent/kagent-tool-server",
              groupKind: "RemoteMCPServer.kagent.dev",
              discoveredTools: [{ name: "k8s_get_resources", description: "read" }],
            },
          ],
        }),
      });
    });

    expect(await apiClient.mcpServers.list()).toEqual([
      {
        ref: "kagent/kagent-tool-server",
        groupKind: "RemoteMCPServer.kagent.dev",
        discoveredTools: [{ name: "k8s_get_resources", description: "read" }],
      },
    ]);
  });

  it("sends the server as a resource whose kind is the server type", async () => {
    let received: unknown;
    serve(({ service }) => {
      service(ToolService, {
        createToolServer: (request) => {
          received = request;
          return {
            resource: {
              apiVersion: "kagent.dev/v1alpha3",
              kind: "RemoteMCPServer",
              value: { metadata: { name: "extra", namespace: "kagent" } },
            },
          };
        },
      });
    });

    const created = await apiClient.mcpServers.create({
      type: "RemoteMCPServer",
      remoteMCPServer: {
        metadata: { name: "extra", namespace: "kagent" },
        spec: {
          description: "an extra server",
          protocol: "STREAMABLE_HTTP",
          url: "https://tools.internal",
          headersFrom: [],
        },
      },
    });

    expect(received).toMatchObject({
      type: "RemoteMCPServer",
      ref: { namespace: "kagent", name: "extra" },
      resource: { kind: "RemoteMCPServer" },
    });
    expect(created.ref).toBe("kagent/extra");
    expect(created.groupKind).toBe("RemoteMCPServer.kagent.dev");
    // Empty because it is: the controller has not handshaken with the server yet,
    // and inventing tools here would put unconfirmed ones on screen.
    expect(created.discoveredTools).toEqual([]);
  });

  /**
   * The tool rows are `database.Tool` marshalled to JSON and carried through the
   * envelope untouched — which is why their field names are snake-cased, and why
   * renaming them here would rename them away from what arrives.
   */
  it("reads a flat tool row straight out of the envelope", async () => {
    serve(({ service }) => {
      service(ToolService, {
        listTools: () => ({
          tools: [
            {
              resource: {
                apiVersion: "kagent.api/v1alpha1",
                kind: "Tool",
                value: {
                  id: "k8s_get_resources",
                  server_name: "kagent-tool-server",
                  group_kind: "RemoteMCPServer.kagent.dev",
                  created_at: "2026-01-01T00:00:00Z",
                  updated_at: "2026-01-01T00:00:00Z",
                  description: "read resources",
                },
              },
            },
          ],
        }),
      });
    });

    const [tool] = await apiClient.mcpServers.tools();
    expect(tool.id).toBe("k8s_get_resources");
    expect(tool.server_name).toBe("kagent-tool-server");
    expect(tool.deleted_at).toBeUndefined();
  });
});

describe("prompt libraries", () => {
  it("splits the ref into the namespace and name the pages read", async () => {
    serve(({ service }) => {
      service(PromptTemplateService, {
        listPromptTemplates: (request) => ({
          promptTemplates: [
            {
              ref: { namespace: request.namespace || "kagent", name: "house-style" },
              keyCount: 2,
              keys: ["tone", "format"],
            },
          ],
        }),
      });
    });

    expect(await apiClient.prompts.list("kagent")).toEqual([
      { namespace: "kagent", name: "house-style", keyCount: 2, keys: ["tone", "format"] },
    ]);
  });

  it("round-trips the fragments on an update", async () => {
    serve(({ service }) => {
      service(PromptTemplateService, {
        updatePromptTemplate: (request) => ({
          promptTemplate: { ref: request.ref, data: request.data },
        }),
      });
    });

    expect(
      await apiClient.prompts.update("kagent", "house-style", {
        data: { tone: "plain" },
      }),
    ).toEqual({ namespace: "kagent", name: "house-style", data: { tone: "plain" } });
  });
});

describe("the cluster", () => {
  it("lists namespaces with their phase", async () => {
    serve(({ service }) => {
      service(SystemService, {
        listNamespaces: () => ({
          namespaces: [
            { name: "kagent", status: "Active" },
            { name: "retired-team", status: "Terminating" },
          ],
        }),
      });
    });

    const namespaces = await apiClient.namespaces.list();
    // Phase matters to a picker: a Terminating namespace is not a create target.
    expect(namespaces.find((row) => row.name === "retired-team")?.status).toBe(
      "Terminating",
    );
  });

  it("returns the substrate inventory, and a partial-data warning as a warning", async () => {
    serve(({ service }) => {
      service(SystemService, {
        getSubstrateStatus: () => ({
          enabled: true,
          ateApiError: "ate-api list calls failed",
          workerPools: [
            { namespace: "kagent", name: "pool", replicas: 2, ateomImage: "ateom:1" },
          ],
          actorTemplates: [
            { namespace: "kagent", name: "tpl", managedByKagent: true, phase: "Ready" },
          ],
          actors: [{ actorId: "a1", atespace: "kagent", status: "Running", version: 3n }],
          workers: [],
        }),
      });
    });

    const status = await apiClient.substrate.status();
    expect(status.enabled).toBe(true);
    // The request succeeded; the runtime halves may be incomplete. That is a
    // message to put beside the data, not an error to throw.
    expect(status.ateApiError).toMatch(/ate-api/);
    expect(status.actors[0].atespace).toBe("kagent");
    expect(status.actors[0].version).toBe(3);
    expect(status.actorTemplates[0].managedByKagent).toBe(true);
  });

  // Proto3 cannot tell an unset string from an empty one, and an empty warning
  // renders as a warning with no text in it.
  it("reads an empty warning as no warning", async () => {
    serve(({ service }) => {
      service(SystemService, {
        getSubstrateStatus: () => ({ enabled: false, ateApiError: "" }),
      });
    });
    expect((await apiClient.substrate.status()).ateApiError).toBeUndefined();
  });

  it("passes the namespace filter through", async () => {
    const asked: string[] = [];
    serve(({ service }) => {
      service(SystemService, {
        getSubstrateStatus: (request) => {
          asked.push(request.namespace);
          return { enabled: true };
        },
      });
    });

    await apiClient.substrate.status("kagent");
    await apiClient.substrate.status();
    expect(asked).toEqual(["kagent", ""]);
  });
});

/**
 * The seam a deployment reshapes calls through, exercised end to end rather than
 * by calling the appliers directly: a transform that is registered but never
 * reached is the failure worth catching, and it is invisible to a unit test of the
 * registry.
 */
describe("transforms reaching the wire", () => {
  /**
   * The interceptors live above the transport rather than inside it precisely so a
   * substituted one cannot skip them. This is the test that says so: the transport
   * here is a router, not the network, and the bearer token still arrives.
   */
  it("attaches the bearer token through a substituted transport", async () => {
    const seen: Array<string | null> = [];
    serve(({ service }) => {
      service(SystemService, {
        listNamespaces: (_request, context) => {
          seen.push(context.requestHeader.get("Authorization"));
          return { namespaces: [] };
        },
      });
    });

    await apiClient.namespaces.list();
    const undo = registerAuthTokenSource(() => "tok-bearer");
    await apiClient.namespaces.list();
    undo();
    await apiClient.namespaces.list();

    // Nothing before the source is registered, and nothing after it is removed —
    // an empty `Authorization` header is refused outright by some proxies, so the
    // absence matters as much as the presence.
    expect(seen).toEqual([null, "Bearer tok-bearer", null]);
  });

  it("lets a transform target one operation and leave the others alone", async () => {
    const seen: Record<string, string | null> = {};
    serve(({ service }) => {
      service(SystemService, {
        listNamespaces: (_request, context) => {
          seen.namespaces = context.requestHeader.get("X-Tenant");
          return { namespaces: [] };
        },
      });
      service(AgentService, {
        listAgents: (_request, context) => {
          seen.agents = context.requestHeader.get("X-Tenant");
          return { agents: [] };
        },
      });
    });

    registerApiTransform({
      name: "tenant",
      request: (context) =>
        context.endpoint === "agents.list"
          ? { ...context, headers: { ...context.headers, "X-Tenant": "eu-1" } }
          : context,
    });

    await apiClient.agents.list();
    await apiClient.namespaces.list();
    expect(seen).toEqual({ agents: "eu-1", namespaces: null });
  });
});

/*
 * Agent instances differ from everything above in one structural way: an instance
 * is a row in the controller's own database rather than a custom resource, so
 * nothing arrives inside a `StructuredObject` and there is no envelope to unwrap.
 * What there *is* instead — two enums, a paged list, and a namespace that is part
 * of the address rather than a filter — is what these cover.
 */
describe("agent instances", () => {
  const INSTANCE_ID = "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44";

  function instanceMessage(overrides: Record<string, unknown> = {}) {
    return {
      id: INSTANCE_ID,
      namespace: "kagent",
      creator: "alice@example.com",
      harness: { namespace: "kagent", name: "k8s-agent" },
      agentTemplate: { namespace: "kagent", name: "k8s-agent-7f3a91c" },
      preparedRevision: "rev-7f3a91c",
      a2aAuthority: "k8s-agent.kagent.svc:8080",
      state: PbAgentInstanceState.READY,
      operation: PbAgentInstanceOperation.UNSPECIFIED,
      createdAt: { seconds: 1767225600n, nanos: 0 },
      updatedAt: { seconds: 1767225600n, nanos: 0 },
      labels: { team: "platform" },
      ...overrides,
    };
  }

  it("reads the two enums as words, and the refs as namespace/name", async () => {
    serve(({ service }) => {
      service(AgentInstanceService, {
        listAgentInstances: () => ({
          agentInstances: [
            instanceMessage(),
            instanceMessage({
              id: "b28e4f13-5c66-4d90-8f2b-77a1e9c34d05",
              state: PbAgentInstanceState.SUSPENDED,
              operation: PbAgentInstanceOperation.RESUME,
            }),
          ],
          page: {},
        }),
      });
    });

    const rows = await apiClient.agentInstances.list("kagent");
    // Sorted by namespace then id descending, like every other list here — so the
    // `b28e…` row comes first. Asserted by looking each one up rather than by index,
    // because the order is not what this test is about.
    const ready = rows.find((row) => row.id === INSTANCE_ID);
    const suspended = rows.find((row) => row.id.startsWith("b28e"));

    expect(ready?.state).toBe("ready");
    expect(ready?.operation).toBe("unspecified");
    expect(ready?.harness).toBe("kagent/k8s-agent");
    expect(ready?.agentTemplate).toBe("kagent/k8s-agent-7f3a91c");
    expect(ready?.createdAt).toBe("2026-01-01T00:00:00.000Z");
    expect(ready?.labels).toEqual({ team: "platform" });

    expect(suspended?.state).toBe("suspended");
    expect(suspended?.operation).toBe("resume");
  });

  /*
   * The failure this exists for: a controller newer than this build sends an enum
   * member the generated code has never heard of. The lookup yields `undefined`,
   * and an undefined state renders as an empty cell — a row that looks like the UI
   * failed to read it rather than one whose state this build cannot name.
   */
  it("names an enum value it does not recognise rather than dropping it", async () => {
    serve(({ service }) => {
      service(AgentInstanceService, {
        listAgentInstances: () => ({
          // 99 is not a member of either enum. Cast because the generated type
          // describes the proto this build compiled against, which is exactly the
          // assumption under test.
          agentInstances: [
            instanceMessage({ state: 99, operation: 98 } as Record<string, unknown>),
          ],
          page: {},
        }),
      });
    });

    const [row] = await apiClient.agentInstances.list("kagent");
    expect(row.state).toBe("unknown");
    expect(row.operation).toBe("unknown");
  });

  /*
   * Proto3 cannot tell an unset string from an empty one, so every optional field
   * arrives as `""`. Left alone, an instance still being created would render an
   * empty A2A authority as though it had one — a blank where "not reported" belongs.
   */
  it("reports an unset field as absent rather than as an empty string", async () => {
    serve(({ service }) => {
      service(AgentInstanceService, {
        listAgentInstances: () => ({
          agentInstances: [
            instanceMessage({
              harness: undefined,
              agentTemplate: undefined,
              preparedRevision: "",
              a2aAuthority: "",
              createdAt: undefined,
              state: PbAgentInstanceState.CREATING,
              operation: PbAgentInstanceOperation.CREATE,
            }),
          ],
          page: {},
        }),
      });
    });

    const [row] = await apiClient.agentInstances.list("kagent");
    expect(row.harness).toBeUndefined();
    expect(row.agentTemplate).toBeUndefined();
    expect(row.preparedRevision).toBeUndefined();
    expect(row.a2aAuthority).toBeUndefined();
    expect(row.createdAt).toBe("");
    expect(row.failure).toBeUndefined();
  });

  it("keeps a failure the controller sent with nothing in it", async () => {
    serve(({ service }) => {
      service(AgentInstanceService, {
        listAgentInstances: () => ({
          agentInstances: [
            instanceMessage({
              state: PbAgentInstanceState.FAILED,
              // Present, and empty. The message being there is the only signal that
              // something went wrong, so the presence must survive the conversion
              // even when neither half has any text in it.
              failure: { reason: "", message: "" },
            }),
          ],
          page: {},
        }),
      });
    });

    const [row] = await apiClient.agentInstances.list("kagent");
    expect(row.failure).toEqual({ reason: undefined, message: undefined });
  });

  /*
   * The list is paged and the page a reader sees must not be the first one only.
   * The controller answers at most 100 rows and hands back a token; a client that
   * ignored it would show a hundred instances out of three hundred and say nothing
   * about the rest.
   */
  it("follows the page token until the controller stops handing one back", async () => {
    const tokensSeen: string[] = [];
    serve(({ service }) => {
      service(AgentInstanceService, {
        listAgentInstances: (request) => {
          tokensSeen.push(request.page?.pageToken ?? "");
          if (!request.page?.pageToken) {
            return {
              agentInstances: [instanceMessage()],
              page: { nextPageToken: "page-2" },
            };
          }
          return {
            agentInstances: [
              instanceMessage({ id: "b28e4f13-5c66-4d90-8f2b-77a1e9c34d05" }),
            ],
            page: {},
          };
        },
      });
    });

    const rows = await apiClient.agentInstances.list("kagent");
    expect(rows).toHaveLength(2);
    expect(tokensSeen).toEqual(["", "page-2"]);
  });

  /*
   * A server that keeps handing back the token it was given would otherwise re-read
   * the same page until the page cap, taking fifty round trips to produce fifty
   * copies of the same rows. Caught where the reason is still legible.
   */
  it("refuses a page token that does not advance", async () => {
    serve(({ service }) => {
      service(AgentInstanceService, {
        listAgentInstances: () => ({
          agentInstances: [instanceMessage()],
          page: { nextPageToken: "stuck" },
        }),
      });
    });

    await expect(apiClient.agentInstances.list("kagent")).rejects.toThrow(
      /repeated the same page/,
    );
  });

  it("asks for other people's instances only when told to", async () => {
    const asked: boolean[] = [];
    serve(({ service }) => {
      service(AgentInstanceService, {
        listAgentInstances: (request) => {
          asked.push(request.allCreators);
          return { agentInstances: [], page: {} };
        },
      });
    });

    await apiClient.agentInstances.list("kagent");
    await apiClient.agentInstances.list("kagent", { allCreators: true });
    expect(asked).toEqual([false, true]);
  });

  it("addresses one instance by namespace and id, and reports a missing one as a 404", async () => {
    const asked: { namespace: string; id: string }[] = [];
    serve(({ service }) => {
      service(AgentInstanceService, {
        getAgentInstance: (request) => {
          asked.push({ namespace: request.namespace, id: request.agentInstanceId });
          if (request.agentInstanceId !== INSTANCE_ID) {
            throw new ConnectError("no such instance", Code.NotFound);
          }
          return { agentInstance: instanceMessage() };
        },
      });
    });

    const instance = await apiClient.agentInstances.get("kagent", INSTANCE_ID);
    expect(instance.id).toBe(INSTANCE_ID);
    expect(asked[0]).toEqual({ namespace: "kagent", id: INSTANCE_ID });

    const missing = await apiClient.agentInstances
      .get("kagent", "b28e4f13-5c66-4d90-8f2b-77a1e9c34d05")
      .catch((error: unknown) => error);
    expect(isNotFound(missing)).toBe(true);
  });

  /*
   * Suspend and resume complete synchronously on the controller, so the record they
   * answer with is the finished state. Returning it rather than discarding it is
   * what lets a caller update what is on screen without a second read.
   */
  it("suspends and resumes through their own RPCs, answering with the new state", async () => {
    const called: string[] = [];
    serve(({ service }) => {
      service(AgentInstanceService, {
        suspendAgentInstance: (request) => {
          called.push(`suspend ${request.namespace}/${request.agentInstanceId}`);
          return {
            agentInstance: instanceMessage({ state: PbAgentInstanceState.SUSPENDED }),
          };
        },
        resumeAgentInstance: (request) => {
          called.push(`resume ${request.namespace}/${request.agentInstanceId}`);
          return { agentInstance: instanceMessage({ state: PbAgentInstanceState.READY }) };
        },
      });
    });

    const suspended = await apiClient.agentInstances.suspend("kagent", INSTANCE_ID);
    expect(suspended.state).toBe("suspended");

    const resumed = await apiClient.agentInstances.resume("kagent", INSTANCE_ID);
    expect(resumed.state).toBe("ready");

    expect(called).toEqual([
      `suspend kagent/${INSTANCE_ID}`,
      `resume kagent/${INSTANCE_ID}`,
    ]);
  });

  /*
   * The controller refuses a lifecycle operation from the wrong state with
   * `Aborted` — "conflicting lifecycle operation". It must reach the caller as
   * itself: a page that reported it as a generic failure would tell a reader to
   * retry the one thing that cannot work.
   */
  it("passes a refused lifecycle operation through as the error it was", async () => {
    serve(({ service }) => {
      service(AgentInstanceService, {
        suspendAgentInstance: () => {
          throw new ConnectError(
            "AgentInstance has a conflicting lifecycle operation",
            Code.Aborted,
          );
        },
      });
    });

    const failure = await apiClient.agentInstances
      .suspend("kagent", INSTANCE_ID)
      .catch((error: unknown) => error);

    expect(failure).toBeInstanceOf(ApiError);
    expect((failure as ApiError).code).toBe("Aborted");
    expect((failure as ApiError).message).toMatch(/conflicting lifecycle operation/);
    expect((failure as ApiError).url).toBe(
      "AgentInstanceService/SuspendAgentInstance",
    );
  });
});
