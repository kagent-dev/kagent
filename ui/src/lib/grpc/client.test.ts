import { Code, ConnectError } from "@connectrpc/connect";
import { fromJson } from "@bufbuild/protobuf";
import { timestampFromDate } from "@bufbuild/protobuf/wkt";
import { Role, TaskState } from "@a2a-js/sdk";
import { TaskSchema } from "@/generated/a2a_pb";
import { extractMessagesFromTasks } from "@/lib/messageHandlers";

import {
  DEFAULT_GRPC_TIMEOUT_MS,
  AgentGrpcGateway,
  FeedbackGrpcGateway,
  GrpcRequestError,
  ModelGrpcGateway,
  MemoryGrpcGateway,
  PromptTemplateGrpcGateway,
  SessionGrpcGateway,
  SystemGrpcGateway,
  ToolGrpcGateway,
  callGetVersion,
  getGrpcTarget,
  mapGrpcError,
  type AgentClient,
  type FeedbackClient,
  type ModelClient,
  type MemoryClient,
  type PromptTemplateClient,
  type SessionClient,
  type SystemClient,
  type TaskClient,
  type ToolClient,
} from "./client";

jest.mock("server-only", () => ({}), { virtual: true });

describe("gRPC client", () => {
  it("calls the generated client with metadata and the default deadline", async () => {
    const getVersion = jest.fn().mockResolvedValue({
      kagentVersion: "v1.2.3",
      gitCommit: "abc123",
      buildDate: "2026-07-28",
      $typeName: "kagent.api.v1alpha1.GetVersionResponse",
    });

    const result = await callGetVersion(
      { getVersion } as Pick<SystemClient, "getVersion">,
      {
        authorization: "Bearer token",
        "x-share-token": "share-token",
      },
    );

    expect(result).toEqual({
      kagentVersion: "v1.2.3",
      gitCommit: "abc123",
      buildDate: "2026-07-28",
    });
    expect(getVersion).toHaveBeenCalledTimes(1);
    const [request, options] = getVersion.mock.calls[0];
    expect(request).toEqual({});
    expect(options.timeoutMs).toBe(DEFAULT_GRPC_TIMEOUT_MS);
    expect(options.headers).toBeInstanceOf(Headers);
    expect(options.headers.get("authorization")).toBe("Bearer token");
    expect(options.headers.get("x-share-token")).toBe("share-token");
  });

  it.each([
    [Code.InvalidArgument, 400],
    [Code.Unauthenticated, 401],
    [Code.PermissionDenied, 403],
    [Code.NotFound, 404],
    [Code.AlreadyExists, 409],
    [Code.ResourceExhausted, 429],
    [Code.DeadlineExceeded, 504],
    [Code.Unavailable, 503],
    [Code.Internal, 500],
  ])("maps gRPC code %s to compatibility status %s", (code, expectedStatus) => {
    const mapped = mapGrpcError(new ConnectError("backend message", code));

    expect(mapped).toBeInstanceOf(GrpcRequestError);
    expect(mapped.code).toBe(code);
    expect(mapped.status).toBe(expectedStatus);
  });

  it("uses stable messages for deadlines and unavailable backends", () => {
    expect(mapGrpcError(new ConnectError("transport detail", Code.DeadlineExceeded)).message)
      .toBe("Request timed out - server took too long to respond.");
    expect(mapGrpcError(new ConnectError("transport detail", Code.Unavailable)).message)
      .toBe("Network error - Could not reach backend server.");
  });

  it("normalizes the configured target and falls back for local development", () => {
    expect(getGrpcTarget({ BACKEND_GRPC_URL: " http://controller:8084/// " }))
      .toBe("http://controller:8084");
    expect(getGrpcTarget({})).toBe("http://127.0.0.1:8084");
  });

  it("rejects unsupported target schemes", () => {
    expect(() => getGrpcTarget({ BACKEND_GRPC_URL: "dns:///controller:8084" }))
      .toThrow("BACKEND_GRPC_URL must use http:// or https://");
  });

  it("maps SystemService identity, namespaces, and substrate inventory to plain DTOs", async () => {
    const getCurrentUser = jest.fn().mockResolvedValue({
      claims: { sub: "user-1", groups: ["admins"] },
    });
    const listNamespaces = jest.fn().mockResolvedValue({
      namespaces: [{ name: "alpha", status: "Active" }],
    });
    const getSubstrateStatus = jest.fn().mockResolvedValue({
      enabled: true,
      ateApiError: "ate unavailable",
      workerPools: [{ namespace: "alpha", name: "pool", replicas: 2, ateomImage: "ateom:test" }],
      actorTemplates: [{
        namespace: "alpha",
        name: "template",
        phase: "Ready",
        goldenActorId: "",
        goldenSnapshot: "",
        sandboxClass: "gvisor",
        workerSelector: "",
        harnessName: "harness",
        managedByKagent: true,
      }],
      actors: [{
        actorId: "actor-1",
        atespace: "",
        status: "Running",
        actorTemplateNamespace: "alpha",
        actorTemplateName: "template",
        ateomPodNamespace: "",
        ateomPodName: "",
        ateomPodIp: "10.0.0.1",
        latestSnapshot: "",
        workerPoolName: "pool",
        inProgressSnapshot: "",
        version: BigInt(3),
      }],
      workers: [{
        workerNamespace: "alpha",
        workerPool: "pool",
        workerPod: "worker-0",
        actorNamespace: "",
        actorTemplate: "template",
        actorId: "actor-1",
        ip: "",
        version: BigInt(0),
      }],
    });
    const gateway = new SystemGrpcGateway({
      getCurrentUser,
      listNamespaces,
      getSubstrateStatus,
    } as unknown as SystemClient, { authorization: "Bearer token" }, 1_234);

    await expect(gateway.getCurrentUserClaims()).resolves.toEqual({
      sub: "user-1",
      groups: ["admins"],
    });
    await expect(gateway.listNamespaces()).resolves.toEqual([{ name: "alpha", status: "Active" }]);
    await expect(gateway.getSubstrateStatus("alpha")).resolves.toEqual({
      enabled: true,
      ateApiError: "ate unavailable",
      workerPools: [{ namespace: "alpha", name: "pool", replicas: 2, ateomImage: "ateom:test" }],
      actorTemplates: [{
        namespace: "alpha",
        name: "template",
        phase: "Ready",
        sandboxClass: "gvisor",
        harnessName: "harness",
        managedByKagent: true,
      }],
      actors: [{
        actorId: "actor-1",
        status: "Running",
        actorTemplateNamespace: "alpha",
        actorTemplateName: "template",
        ateomPodIp: "10.0.0.1",
        workerPoolName: "pool",
        version: 3,
      }],
      workers: [{
        workerNamespace: "alpha",
        workerPool: "pool",
        workerPod: "worker-0",
        actorTemplate: "template",
        actorId: "actor-1",
      }],
    });
    expect(getSubstrateStatus.mock.calls[0][0]).toEqual({ namespace: "alpha" });
    for (const operation of [getCurrentUser, listNamespaces, getSubstrateStatus]) {
      const options = operation.mock.calls[0][1];
      expect(options.timeoutMs).toBe(1_234);
      expect(options.headers.get("authorization")).toBe("Bearer token");
    }
  });

  it("submits FeedbackService mutations with bigint IDs and compatibility metadata", async () => {
    const createFeedback = jest.fn().mockResolvedValue({});
    const gateway = new FeedbackGrpcGateway(
      { createFeedback } as unknown as FeedbackClient,
      { authorization: "Bearer token" },
      1_234,
    );

    await gateway.submitFeedback({
      messageId: 42,
      isPositive: false,
      feedbackText: "incorrect answer",
      issueType: "factual" as never,
    });

    const [request, options] = createFeedback.mock.calls[0];
    expect(request).toEqual({
      messageId: BigInt(42),
      isPositive: false,
      feedbackText: "incorrect answer",
      issueType: "factual",
    });
    expect(options.timeoutMs).toBe(1_234);
    expect(options.headers.get("authorization")).toBe("Bearer token");
    await expect(gateway.submitFeedback({
      messageId: Number.MAX_SAFE_INTEGER + 1,
      isPositive: true,
      feedbackText: "helpful",
    })).rejects.toThrow("safe integer");
  });

  it("converts ModelConfig resources to plain DTOs with metadata and a deadline", async () => {
    const listModelConfigs = jest.fn().mockResolvedValue({
      modelConfigs: [{
        ref: { namespace: "team-a", name: "main" },
        resource: {
          apiVersion: "kagent.dev/v1alpha3",
          kind: "ModelConfig",
          value: {
            metadata: { name: "main", namespace: "team-a" },
            spec: { model: "gpt-4.1", provider: "OpenAI", openAI: { temperature: "0.2" } },
          },
        },
      }],
    });
    const gateway = new ModelGrpcGateway(
      { listModelConfigs } as unknown as ModelClient,
      { authorization: "Bearer token" },
      1_234,
    );

    await expect(gateway.listModelConfigs()).resolves.toEqual([{
      ref: "team-a/main",
      spec: { model: "gpt-4.1", provider: "OpenAI", openAI: { temperature: "0.2" } },
    }]);
    const [request, options] = listModelConfigs.mock.calls[0];
    expect(request).toEqual({});
    expect(options.timeoutMs).toBe(1_234);
    expect(options.headers.get("authorization")).toBe("Bearer token");
  });

  it("constructs create and update requests without turning null API keys into values", async () => {
    const modelConfigResponse = {
      modelConfig: {
        ref: { namespace: "default", name: "main" },
        resource: {
          apiVersion: "kagent.dev/v1alpha2",
          kind: "ModelConfig",
          value: { spec: { model: "gpt-4.1", provider: "OpenAI" } },
        },
      },
    };
    const createModelConfig = jest.fn().mockResolvedValue(modelConfigResponse);
    const updateModelConfig = jest.fn().mockResolvedValue(modelConfigResponse);
    const gateway = new ModelGrpcGateway(
      { createModelConfig, updateModelConfig } as unknown as ModelClient,
      {},
    );

    await gateway.createModelConfig({
      ref: "default/main",
      apiKey: "secret",
      spec: { model: "gpt-4.1", provider: "OpenAI" },
      secrets: [{ name: "ca", key: "ca.crt", value: "CERT" }],
    });
    expect(createModelConfig.mock.calls[0][0]).toEqual({
      ref: { namespace: "default", name: "main" },
      resource: {
        apiVersion: "kagent.dev/v1alpha2",
        kind: "ModelConfig",
        value: { spec: { model: "gpt-4.1", provider: "OpenAI" } },
      },
      apiKey: "secret",
      secrets: [{ name: "ca", key: "ca.crt", value: "CERT" }],
    });

    await gateway.updateModelConfig("default", "main", {
      apiKey: null,
      spec: { model: "gpt-4.1", provider: "OpenAI" },
    });
    expect(updateModelConfig.mock.calls[0][0]).toEqual({
      ref: { namespace: "default", name: "main" },
      resource: {
        apiVersion: "kagent.dev/v1alpha2",
        kind: "ModelConfig",
        value: { spec: { model: "gpt-4.1", provider: "OpenAI" } },
      },
      secrets: [],
    });
  });

  it("maps generated discovery responses to the existing UI DTO shapes", async () => {
    const listSupportedModelProviders = jest.fn().mockResolvedValue({
      providers: [{ name: "OpenAI", type: "OpenAI", requiredParams: [], optionalParams: ["baseUrl"] }],
    });
    const listConfiguredProviders = jest.fn().mockResolvedValue({
      providers: [{ name: "corp", type: "OpenAI", endpoint: "https://models.example.com" }],
    });
    const listProviderModels = jest.fn().mockResolvedValue({ provider: "corp", models: ["model-a"] });
    const listSupportedModels = jest.fn().mockResolvedValue({
      providers: [{
        provider: "OpenAI",
        models: [{ name: "gpt-4.1", functionCalling: true }],
      }],
    });
    const gateway = new ModelGrpcGateway({
      listSupportedModelProviders,
      listConfiguredProviders,
      listProviderModels,
      listSupportedModels,
    } as unknown as ModelClient, {});

    await expect(gateway.listSupportedModelProviders()).resolves.toEqual([{
      name: "OpenAI",
      type: "OpenAI",
      requiredParams: [],
      optionalParams: ["baseUrl"],
    }]);
    await expect(gateway.listConfiguredProviders()).resolves.toEqual([{
      name: "corp",
      type: "OpenAI",
      endpoint: "https://models.example.com",
    }]);
    await expect(gateway.listProviderModels("corp", true)).resolves.toEqual({
      provider: "corp",
      models: ["model-a"],
    });
    expect(listProviderModels.mock.calls[0][0]).toEqual({ providerName: "corp", refresh: true });
    await expect(gateway.listSupportedModels()).resolves.toEqual({
      OpenAI: [{ name: "gpt-4.1", function_calling: true }],
    });
  });

  it("maps ModelService failures through the shared compatibility error", async () => {
    const listModelConfigs = jest.fn().mockRejectedValue(new ConnectError("missing", Code.NotFound));
    const gateway = new ModelGrpcGateway({ listModelConfigs } as unknown as ModelClient, {});

    await expect(gateway.listModelConfigs()).rejects.toMatchObject({
      name: "GrpcRequestError",
      code: Code.NotFound,
      status: 404,
      message: "missing",
    });
  });

  it("maps all PromptTemplateService RPCs to prompt DTOs with metadata and deadlines", async () => {
    const listPromptTemplates = jest.fn().mockResolvedValue({
      promptTemplates: [{
        ref: { namespace: "team", name: "library" },
        keyCount: 2,
        keys: ["intro", "rules"],
      }],
    });
    const getPromptTemplate = jest.fn().mockResolvedValue({
      promptTemplate: {
        ref: { namespace: "team", name: "library" },
        data: { intro: "hello" },
      },
    });
    const createPromptTemplate = jest.fn().mockImplementation(async (request) => ({
      promptTemplate: { ref: request.ref, data: request.data },
    }));
    const updatePromptTemplate = jest.fn().mockImplementation(async (request) => ({
      promptTemplate: { ref: request.ref, data: request.data },
    }));
    const deletePromptTemplate = jest.fn().mockResolvedValue({});
    const gateway = new PromptTemplateGrpcGateway({
      listPromptTemplates,
      getPromptTemplate,
      createPromptTemplate,
      updatePromptTemplate,
      deletePromptTemplate,
    } as unknown as PromptTemplateClient, { authorization: "Bearer token" }, 1_234);

    await expect(gateway.listPromptTemplates("team")).resolves.toEqual([{
      namespace: "team",
      name: "library",
      keyCount: 2,
      keys: ["intro", "rules"],
    }]);
    expect(listPromptTemplates.mock.calls[0][0]).toEqual({ namespace: "team" });

    await expect(gateway.getPromptTemplate("team", "library")).resolves.toEqual({
      namespace: "team",
      name: "library",
      data: { intro: "hello" },
    });
    expect(getPromptTemplate.mock.calls[0][0]).toEqual({
      ref: { namespace: "team", name: "library" },
    });

    await expect(gateway.createPromptTemplate("team", "created", { intro: "hello" }))
      .resolves.toEqual({ namespace: "team", name: "created", data: { intro: "hello" } });
    expect(createPromptTemplate.mock.calls[0][0]).toEqual({
      ref: { namespace: "team", name: "created" },
      data: { intro: "hello" },
    });

    await expect(gateway.updatePromptTemplate("team", "created", { rules: "updated" }))
      .resolves.toEqual({ namespace: "team", name: "created", data: { rules: "updated" } });
    expect(updatePromptTemplate.mock.calls[0][0]).toEqual({
      ref: { namespace: "team", name: "created" },
      data: { rules: "updated" },
    });

    await gateway.deletePromptTemplate("team", "created");
    expect(deletePromptTemplate.mock.calls[0][0]).toEqual({
      ref: { namespace: "team", name: "created" },
    });

    for (const operation of [
      listPromptTemplates,
      getPromptTemplate,
      createPromptTemplate,
      updatePromptTemplate,
      deletePromptTemplate,
    ]) {
      const options = operation.mock.calls[0][1];
      expect(options.timeoutMs).toBe(1_234);
      expect(options.headers.get("authorization")).toBe("Bearer token");
    }
  });

  it("rejects incomplete PromptTemplateService responses", async () => {
    const listPromptTemplates = jest.fn().mockResolvedValue({
      promptTemplates: [{ keyCount: 1, keys: ["intro"] }],
    });
    const getPromptTemplate = jest.fn().mockResolvedValue({});
    const gateway = new PromptTemplateGrpcGateway({
      listPromptTemplates,
      getPromptTemplate,
    } as unknown as PromptTemplateClient, {});

    await expect(gateway.listPromptTemplates("team"))
      .rejects.toThrow("complete summary reference");
    await expect(gateway.getPromptTemplate("team", "missing"))
      .rejects.toThrow("did not include a PromptTemplate");
  });

  it("maps merged AgentService rows to the existing UI DTOs", async () => {
    const listAgents = jest.fn().mockResolvedValue({
      agents: [{
        ref: { namespace: "default", name: "sandbox" },
        kind: 2,
        resource: {
          apiVersion: "kagent.dev/v1alpha2",
          kind: "SandboxAgent",
          value: {
            metadata: { name: "sandbox", namespace: "default" },
            spec: { type: "Declarative", description: "Sandbox" },
          },
        },
        id: "default__NS__sandbox",
        modelProvider: "OpenAI",
        model: "gpt-4.1",
        modelConfigRef: { namespace: "default", name: "model" },
        tools: [{ kind: "Tool", value: { type: "McpServer", mcpServer: { name: "tools" } } }],
        ready: true,
        accepted: true,
        memoryRefs: [],
      }, {
        ref: { namespace: "default", name: "harness" },
        kind: 3,
        resource: {
          apiVersion: "kagent.dev/v1alpha3",
          kind: "AgentHarness",
          value: {
            metadata: { name: "harness", namespace: "default" },
            spec: { backend: "openclaw", description: " Harness " },
          },
        },
        id: "default__NS__harness",
        modelProvider: "",
        model: "",
        tools: [],
        ready: false,
        accepted: true,
        memoryRefs: [],
        agentHarness: {
          backend: "openclaw",
          actorId: "actor-1",
          backendRefId: "actor-1",
          endpoint: "http://actor",
          acpPath: "/api/agentharnesses/default/harness/acp",
        },
      }],
    });
    const gateway = new AgentGrpcGateway(
      { listAgents } as unknown as AgentClient,
      { authorization: "Bearer token" },
      1_234,
    );

    const agents = await gateway.listAgents("default");

    expect(agents).toHaveLength(2);
    expect(agents[0]).toMatchObject({
      id: "default__NS__sandbox",
      agent: { kind: "SandboxAgent", metadata: { namespace: "default", name: "sandbox" } },
      modelConfigRef: "default/model",
      ready: true,
    });
    expect(agents[0].tools).toEqual([{ type: "McpServer", mcpServer: { name: "tools" } }]);
    expect(agents[1]).toMatchObject({
      agent: { kind: "AgentHarness", spec: { description: "Harness" } },
      substrateAgentHarness: {
        backend: "openclaw",
        actorId: "actor-1",
        acpPath: "/api/agentharnesses/default/harness/acp",
      },
    });
    const [request, options] = listAgents.mock.calls[0];
    expect(request).toEqual({ namespace: "default" });
    expect(options.timeoutMs).toBe(1_234);
    expect(options.headers.get("authorization")).toBe("Bearer token");
  });

  it("builds AgentService mutation payloads and maps actor lifecycle states", async () => {
    const agentMessage = {
      ref: { namespace: "default", name: "assistant" },
      kind: 2,
      resource: {
        apiVersion: "kagent.dev/v1alpha3",
        kind: "SandboxAgent",
        value: {
          metadata: { name: "assistant", namespace: "default" },
          spec: { type: "BYO", description: "Assistant", byo: { deployment: { image: "test" } } },
        },
      },
      id: "default__NS__assistant",
      modelProvider: "",
      model: "",
      tools: [],
      ready: false,
      accepted: false,
      memoryRefs: [],
    };
    const createSandboxAgent = jest.fn().mockResolvedValue({ agent: agentMessage });
    const ensureAgentHarnessSessionActor = jest.fn().mockResolvedValue({
      actor: {
        ref: { namespace: "default", name: "harness" },
        sessionId: "session-1",
        actorId: "actor-1",
        state: 1,
      },
    });
    const gateway = new AgentGrpcGateway({
      createSandboxAgent,
      ensureAgentHarnessSessionActor,
    } as unknown as AgentClient, {});

    await gateway.createSandboxAgent(agentMessage.resource.value as never);
    expect(createSandboxAgent.mock.calls[0][0]).toEqual({
      ref: { namespace: "default", name: "assistant" },
      resource: {
        apiVersion: "kagent.dev/v1alpha3",
        kind: "SandboxAgent",
        value: agentMessage.resource.value,
      },
    });
    await expect(gateway.ensureAgentHarnessSessionActor("default", "harness", "session-1"))
      .resolves.toEqual({
        namespace: "default",
        name: "harness",
        sessionId: "session-1",
        actorId: "actor-1",
        state: "running",
      });
  });

  it("maps ToolService discovery and ToolServer CRUD through generated messages", async () => {
    const listTools = jest.fn().mockResolvedValue({
      tools: [{
        resource: {
          apiVersion: "kagent.api/v1alpha1",
          kind: "Tool",
          value: {
            id: "move_task",
            server_name: "default/board",
            group_kind: "RemoteMCPServer.kagent.dev",
            description: "Move a task",
            created_at: "2026-07-28T00:00:00Z",
            updated_at: "2026-07-28T00:00:00Z",
            deleted_at: null,
          },
        },
      }],
    });
    const listToolServers = jest.fn().mockResolvedValue({
      toolServers: [{
        ref: "default/board",
        groupKind: "RemoteMCPServer.kagent.dev",
        discoveredTools: [{ name: "move_task", description: "Move a task" }],
      }],
    });
    const listToolServerTypes = jest.fn().mockResolvedValue({
      types: ["RemoteMCPServer", "MCPServer"],
    });
    const createToolServer = jest.fn().mockImplementation(async (request) => ({
      resource: request.resource,
    }));
    const deleteToolServer = jest.fn().mockResolvedValue({});
    const gateway = new ToolGrpcGateway({
      listTools,
      listToolServers,
      listToolServerTypes,
      createToolServer,
      deleteToolServer,
    } as unknown as ToolClient, { authorization: "Bearer token" }, 1_234);

    await expect(gateway.listTools()).resolves.toEqual([{
      id: "move_task",
      server_name: "default/board",
      group_kind: "RemoteMCPServer.kagent.dev",
      description: "Move a task",
      created_at: "2026-07-28T00:00:00Z",
      updated_at: "2026-07-28T00:00:00Z",
      deleted_at: null,
    }]);
    await expect(gateway.listToolServers()).resolves.toEqual([{
      ref: "default/board",
      groupKind: "RemoteMCPServer.kagent.dev",
      discoveredTools: [{ name: "move_task", description: "Move a task" }],
    }]);
    await expect(gateway.listToolServerTypes()).resolves.toEqual(["RemoteMCPServer", "MCPServer"]);

    const remote = {
      metadata: { namespace: "default", name: "board" },
      spec: {
        description: "Board tools",
        protocol: "STREAMABLE_HTTP" as const,
        url: "https://board.example/mcp",
        headersFrom: [],
      },
    };
    await expect(gateway.createToolServer({
      type: "RemoteMCPServer",
      remoteMCPServer: remote,
      secrets: [{ name: "board-token", key: "token", value: "secret" }],
    })).resolves.toEqual(remote);
    expect(createToolServer.mock.calls[0][0]).toEqual({
      type: "RemoteMCPServer",
      ref: { namespace: "default", name: "board" },
      resource: {
        apiVersion: "kagent.dev/v1alpha2",
        kind: "RemoteMCPServer",
        value: remote,
      },
      secrets: [{ name: "board-token", key: "token", value: "secret" }],
    });

    const managed = {
      metadata: { namespace: "default", name: "managed" },
      spec: {
        deployment: { image: "example/mcp:latest", port: 3000 },
        transportType: "stdio" as const,
        stdioTransport: {},
      },
    };
    await expect(gateway.createToolServer({
      type: "MCPServer",
      mcpServer: managed,
    })).resolves.toEqual(managed);
    expect(createToolServer.mock.calls[1][0]).toEqual({
      type: "MCPServer",
      ref: { namespace: "default", name: "managed" },
      resource: {
        apiVersion: "kagent.dev/v1alpha1",
        kind: "MCPServer",
        value: managed,
      },
      secrets: [],
    });

    await gateway.deleteToolServer("default", "board");
    expect(deleteToolServer.mock.calls[0][0]).toEqual({
      ref: { namespace: "default", name: "board" },
    });
    for (const operation of [listTools, listToolServers, listToolServerTypes, createToolServer, deleteToolServer]) {
      const options = operation.mock.calls[0][1];
      expect(options.timeoutMs).toBe(1_234);
      expect(options.headers.get("authorization")).toBe("Bearer token");
    }
  });

  it("maps the MCP Apps facade without changing MCP JSON payloads", async () => {
    const listMCPAppTools = jest.fn().mockResolvedValue({
      tools: [{
        name: "move_task",
        description: "Move a task",
        inputSchema: {
          apiVersion: "mcp.kagent.dev/v1alpha1",
          kind: "MCPInputSchema",
          value: { type: "object", properties: { id: { type: "string" } } },
        },
        uiResourceUri: "ui://board",
        meta: {
          apiVersion: "mcp.kagent.dev/v1alpha1",
          kind: "MCPMetadata",
          value: { ui: { resourceUri: "ui://board" } },
        },
      }, {
        name: "refresh",
        description: "",
        uiResourceUri: "",
      }],
    });
    const callMCPAppTool = jest.fn().mockResolvedValue({
      result: {
        apiVersion: "mcp.kagent.dev/v1alpha1",
        kind: "MCPCallToolResult",
        value: { content: [{ type: "text", text: "moved" }], isError: false },
      },
    });
    const readMCPAppResource = jest.fn().mockResolvedValue({
      result: {
        apiVersion: "mcp.kagent.dev/v1alpha1",
        kind: "MCPReadResourceResult",
        value: { contents: [{ uri: "ui://board", mimeType: "text/html", text: "<main>Board</main>" }] },
      },
    });
    const gateway = new ToolGrpcGateway({
      listMCPAppTools,
      callMCPAppTool,
      readMCPAppResource,
    } as unknown as ToolClient, {}, 2_345);

    await expect(gateway.listMcpAppTools("default", "board", "RemoteMCPServer.kagent.dev"))
      .resolves.toEqual([{
        name: "move_task",
        description: "Move a task",
        inputSchema: { type: "object", properties: { id: { type: "string" } } },
        uiResourceUri: "ui://board",
        _meta: { ui: { resourceUri: "ui://board" } },
      }, {
        name: "refresh",
      }]);
    expect(listMCPAppTools.mock.calls[0][0]).toEqual({
      server: {
        ref: { namespace: "default", name: "board" },
        groupKind: "RemoteMCPServer.kagent.dev",
      },
    });

    await expect(gateway.callMcpAppTool(
      "default",
      "board",
      "move_task",
      { id: "task-1" },
      "RemoteMCPServer.kagent.dev",
    )).resolves.toEqual({
      content: [{ type: "text", text: "moved" }],
      isError: false,
    });
    expect(callMCPAppTool.mock.calls[0][0]).toEqual({
      server: {
        ref: { namespace: "default", name: "board" },
        groupKind: "RemoteMCPServer.kagent.dev",
      },
      toolName: "move_task",
      arguments: {
        apiVersion: "mcp.kagent.dev/v1alpha1",
        kind: "MCPArguments",
        value: { id: "task-1" },
      },
    });

    await expect(gateway.readMcpAppResource(
      "default",
      "board",
      "ui://board",
      "RemoteMCPServer.kagent.dev",
    )).resolves.toEqual({
      contents: [{ uri: "ui://board", mimeType: "text/html", text: "<main>Board</main>" }],
    });
    expect(readMCPAppResource.mock.calls[0][0]).toEqual({
      server: {
        ref: { namespace: "default", name: "board" },
        groupKind: "RemoteMCPServer.kagent.dev",
      },
      uri: "ui://board",
    });
  });

  it("maps Session, Task, and Share generated messages to compatibility DTOs", async () => {
    const createdAt = timestampFromDate(new Date("2026-08-04T09:00:00.000Z"));
    const updatedAt = timestampFromDate(new Date("2026-08-04T09:05:00.000Z"));
    const session = {
      id: "session-1",
      name: "Chat",
      userId: "user-1",
      agentId: "default__NS__agent",
      createdAt,
      updatedAt,
      shareToken: "share-token",
      shareReadOnly: true,
    };
    const getSession = jest.fn().mockResolvedValue({
      session,
      events: [{
        id: "event-1",
        sessionId: "session-1",
        userId: "user-1",
        createdAt,
        updatedAt,
        data: "{\"kind\":\"message\"}",
      }],
      readOnly: true,
    });
    const listTasks = jest.fn().mockResolvedValue({
      tasks: [fromJson(TaskSchema, {
          id: "task-1",
          contextId: "session-1",
          status: { state: "TASK_STATE_WORKING" },
      })],
    });
    const createSessionShare = jest.fn().mockResolvedValue({
      share: {
        id: BigInt(7),
        token: "new-token",
        sessionId: "session-1",
        userId: "user-1",
        readOnly: true,
        createdAt,
      },
    });
    const sessionClient = {
      getSession,
      createSessionShare,
    } as unknown as SessionClient;
    const gateway = new SessionGrpcGateway(
      sessionClient,
      { listTasks } as unknown as TaskClient,
      { authorization: "Bearer token" },
      1_234,
    );

    await expect(gateway.getSessionWithEvents("session-1", "share-token")).resolves.toEqual({
      session: {
        id: "session-1",
        name: "Chat",
        agent_id: "default__NS__agent",
        user_id: "user-1",
        created_at: "2026-08-04T09:00:00.000Z",
        updated_at: "2026-08-04T09:05:00.000Z",
        deleted_at: "",
        share_token: "share-token",
        share_read_only: true,
      },
      events: [{
        id: "event-1",
        session_id: "session-1",
        user_id: "user-1",
        created_at: "2026-08-04T09:00:00.000Z",
        updated_at: "2026-08-04T09:05:00.000Z",
        data: "{\"kind\":\"message\"}",
      }],
      read_only: true,
    });
    await expect(gateway.listTasks("session-1", "share-token")).resolves.toMatchObject([{
      id: "task-1",
      contextId: "session-1",
      status: { state: TaskState.TASK_STATE_WORKING },
    }]);
    await expect(gateway.createSessionShare("session-1")).resolves.toEqual({
      token: "new-token",
      session_id: "session-1",
      read_only: true,
      created_at: "2026-08-04T09:00:00.000Z",
    });

    for (const operation of [getSession, listTasks]) {
      const options = operation.mock.calls[0][1];
      expect(options.timeoutMs).toBe(1_234);
      expect(options.headers.get("authorization")).toBe("Bearer token");
      expect(options.headers.get("x-share-token")).toBe("share-token");
    }
  });

  it("normalizes canonical Go A2A tasks before rebuilding session history", async () => {
    const listTasks = jest.fn().mockResolvedValue({
      tasks: [fromJson(TaskSchema, {
          id: "task-1",
          contextId: "session-1",
          status: { state: "TASK_STATE_COMPLETED" },
          history: [{
            contextId: "session-1",
            messageId: "user-message",
            parts: [{ text: "hello" }],
            role: "ROLE_USER",
          }],
          artifacts: [{
            artifactId: "agent-answer",
            parts: [{ text: "Hello!" }],
          }],
      })],
    });
    const gateway = new SessionGrpcGateway(
      {} as SessionClient,
      { listTasks } as unknown as TaskClient,
      {},
    );

    const tasks = await gateway.listTasks("session-1");

    expect(tasks).toEqual([expect.objectContaining({
      status: expect.objectContaining({ state: TaskState.TASK_STATE_COMPLETED }),
    })]);
    expect(extractMessagesFromTasks(tasks)).toMatchObject([
      {
        messageId: "user-message",
        role: Role.ROLE_USER,
        parts: [{ content: { $case: "text", value: "hello" } }],
      },
      {
        role: Role.ROLE_AGENT,
        parts: [{ content: { $case: "text", value: "Hello!" } }],
      },
    ]);
  });

  it("normalizes nested canonical Go A2A task content", async () => {
    const listTasks = jest.fn().mockResolvedValue({
      tasks: [fromJson(TaskSchema, {
          id: "task-1",
          contextId: "session-1",
          metadata: { source: "task" },
          status: {
            state: "TASK_STATE_INPUT_REQUIRED",
            message: {
              messageId: "status-message",
              role: "ROLE_AGENT",
              parts: [{ data: { pending: true }, metadata: { source: "status" } }],
            },
          },
          history: [{
            messageId: "rich-message",
            role: "ROLE_USER",
            referenceTaskIds: ["task-0"],
            parts: [
              { text: "hello", metadata: { source: "text" } },
              { data: { answer: 42 }, metadata: { source: "data" } },
              {
                url: "https://example.com/doc.md",
                filename: "doc.md",
                mediaType: "text/markdown",
                metadata: { source: "url" },
              },
              {
                raw: "UkFXX0JZVEVT",
                filename: "blob.bin",
                mediaType: "application/octet-stream",
                metadata: { source: "raw" },
              },
            ],
          }],
          artifacts: [{
            artifactId: "artifact-1",
            name: "result",
            parts: [{ text: "artifact text" }],
          }],
      })],
    });
    const gateway = new SessionGrpcGateway(
      {} as SessionClient,
      { listTasks } as unknown as TaskClient,
      {},
    );

    await expect(gateway.listTasks("session-1")).resolves.toMatchObject([{
      id: "task-1",
      contextId: "session-1",
      metadata: { source: "task" },
      status: {
        state: TaskState.TASK_STATE_INPUT_REQUIRED,
        message: {
          messageId: "status-message",
          role: Role.ROLE_AGENT,
          parts: [{
            content: { $case: "data", value: { pending: true } },
            metadata: { source: "status" },
          }],
        },
      },
      history: [{
        messageId: "rich-message",
        role: Role.ROLE_USER,
        referenceTaskIds: ["task-0"],
        parts: [
          { content: { $case: "text", value: "hello" }, metadata: { source: "text" } },
          { content: { $case: "data", value: { answer: 42 } }, metadata: { source: "data" } },
          {
            content: { $case: "url", value: "https://example.com/doc.md" },
            filename: "doc.md",
            mediaType: "text/markdown",
            metadata: { source: "url" },
          },
          {
            content: { $case: "raw", value: Buffer.from("RAW_BYTES") },
            filename: "blob.bin",
            mediaType: "application/octet-stream",
            metadata: { source: "raw" },
          },
        ],
      }],
      artifacts: [{
        artifactId: "artifact-1",
        name: "result",
        parts: [{ content: { $case: "text", value: "artifact text" } }],
      }],
    }]);
  });

  it("maps Memory summaries and rejects unsafe access counts", async () => {
    const list = jest.fn().mockResolvedValue({
      memories: [{
        id: "memory-1",
        content: "Remember this",
        accessCount: BigInt(3),
        createdAt: timestampFromDate(new Date("2026-08-04T09:00:00.000Z")),
        expiresAt: timestampFromDate(new Date("2026-08-19T09:00:00.000Z")),
      }],
    });
    const deleteMemory = jest.fn().mockResolvedValue({ status: "deleted" });
    const gateway = new MemoryGrpcGateway(
      { list, delete: deleteMemory } as unknown as MemoryClient,
      { authorization: "Bearer token" },
      2_345,
    );

    await expect(gateway.listAgentMemories("default__NS__agent", "user-1")).resolves.toEqual([{
      id: "memory-1",
      content: "Remember this",
      access_count: 3,
      created_at: "2026-08-04T09:00:00.000Z",
      expires_at: "2026-08-19T09:00:00.000Z",
    }]);
    await gateway.clearAgentMemory("default__NS__agent", "user-1");
    expect(deleteMemory.mock.calls[0][0]).toEqual({
      agentName: "default__NS__agent",
      userId: "user-1",
    });

    list.mockResolvedValueOnce({
      memories: [{
        id: "memory-2",
        content: "Too popular",
        accessCount: BigInt(Number.MAX_SAFE_INTEGER) + BigInt(1),
      }],
    });
    await expect(gateway.listAgentMemories("agent", "user-1")).rejects.toThrow("safe integer range");
  });
});
