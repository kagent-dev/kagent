import {
  agentFormDataToAgent,
  agentFormDataToSandboxAgent,
  agentFormStateToData,
  agentResponseToFormState,
  createInitialAgentFormState,
  validateAgentFormData,
  validateAgentFormState,
} from "@/lib/agentFormDomain";
import type { AgentResponse } from "@/types";

function initialState() {
  return createInitialAgentFormState({
    namespace: "default",
    isEditMode: false,
    defaultSystemPrompt: "Default prompt",
  });
}

function response(overrides: Partial<AgentResponse> = {}): AgentResponse {
  return {
    id: "1",
    agent: {
      metadata: { name: "demo", namespace: "team-a" },
      spec: {
        type: "Declarative",
        description: "Demo agent",
        declarative: {
          runtime: "go",
          systemMessage: "Use the tools",
          modelConfig: "main-model",
          stream: true,
          tools: [],
          memory: { modelConfig: "embedding-model", ttlDays: 14 },
          promptTemplate: {
            dataSources: [{ kind: "ConfigMap", name: "runbooks", alias: "ops" }],
          },
          deployment: { serviceAccountName: "agent-sa" },
        },
        skills: {
          refs: ["registry.example.com/skill:v1"],
          gitRefs: [{ url: "https://github.com/example/skills.git", path: "triage" }],
          gitAuthSecretRef: { name: "git-token" },
        },
      },
    },
    model: "gpt-4.1",
    modelProvider: "OpenAI",
    modelConfigRef: "team-a/main-model",
    tools: [{ type: "Agent", agent: { name: "helper", namespace: "team-a" } }],
    deploymentReady: true,
    accepted: true,
    workloadMode: "deployment",
    ...overrides,
  };
}

describe("Agent form state", () => {
  it("initializes create and edit modes through one contract", () => {
    expect(initialState()).toMatchObject({
      namespace: "default",
      systemPrompt: "Default prompt",
      declarativeRuntime: "go",
      isLoading: false,
    });
    expect(
      createInitialAgentFormState({
        namespace: "team-a",
        isEditMode: true,
        defaultSystemPrompt: "Default prompt",
      }),
    ).toMatchObject({ namespace: "team-a", systemPrompt: "", isLoading: true });
  });

  it("loads a declarative Agent response into editable state", () => {
    expect(agentResponseToFormState(response())).toMatchObject({
      name: "demo",
      namespace: "team-a",
      agentType: "Declarative",
      systemPrompt: "Use the tools",
      selectedModel: { ref: "team-a/main-model" },
      selectedMemoryModel: { ref: "team-a/embedding-model" },
      memoryTtlDays: "14",
      selectedTools: [{ type: "Agent" }],
      skillRefs: ["registry.example.com/skill:v1"],
      skillsGitAuthSecretName: "git-token",
      declarativeRuntime: "go",
      serviceAccountName: "agent-sa",
      promptSourceRows: [{ name: "runbooks", alias: "ops" }],
    });
  });

  it("defaults an unqualified memory model to the default namespace", () => {
    const agentResponse = response();
    agentResponse.agent.metadata.namespace = undefined;

    expect(agentResponseToFormState(agentResponse)).toMatchObject({
      selectedMemoryModel: { ref: "default/embedding-model" },
    });
  });

  it("loads BYO deployment and secret environment fields", () => {
    const byo = response({
      agent: {
        metadata: { name: "byo", namespace: "team-a" },
        spec: {
          type: "BYO",
          description: "BYO agent",
          byo: {
            deployment: {
              image: "example/agent:v1",
              cmd: "serve",
              args: ["--port", "8080"],
              replicas: 2,
              imagePullSecrets: [{ name: "registry" }],
              env: [
                { name: "PLAIN", value: "value" },
                {
                  name: "TOKEN",
                  valueFrom: {
                    secretKeyRef: { name: "credentials", key: "token", optional: true },
                  },
                },
              ],
            },
          },
        },
      },
      model: "",
      modelProvider: "",
      modelConfigRef: "",
      tools: [],
    });

    expect(agentResponseToFormState(byo)).toMatchObject({
      agentType: "BYO",
      byoImage: "example/agent:v1",
      byoCmd: "serve",
      byoArgs: "--port 8080",
      replicas: "2",
      imagePullSecrets: ["registry"],
      envPairs: [
        { name: "PLAIN", value: "value", isSecret: false },
        {
          name: "TOKEN",
          isSecret: true,
          secretName: "credentials",
          secretKey: "token",
          optional: true,
        },
      ],
    });
  });
});

describe("Agent form serialization", () => {
  it("coerces editable strings and drops incomplete rows", () => {
    const state = {
      ...initialState(),
      name: "demo",
      description: "Demo agent",
      selectedModel: { ref: "default/main", spec: { model: "gpt", provider: "OpenAI" } },
      selectedMemoryModel: { ref: "default/embed", spec: { model: "embed", provider: "OpenAI" } },
      memoryTtlDays: "30",
      byoArgs: "--port 8080",
      replicas: "3",
      imagePullSecrets: [" registry ", ""],
      envPairs: [
        { name: " PLAIN ", value: "value", isSecret: false },
        { name: "TOKEN", isSecret: true, secretName: "secret", secretKey: "key" },
        { name: "INCOMPLETE", isSecret: true, secretName: "secret", secretKey: "" },
      ],
      serviceAccountName: " agent-sa ",
    };

    expect(agentFormStateToData(state)).toMatchObject({
      modelName: "default/main",
      memory: { modelConfig: "default/embed", ttlDays: 30 },
      byoArgs: ["--port", "8080"],
      replicas: 3,
      imagePullSecrets: [{ name: "registry" }],
      env: [
        { name: "PLAIN", value: "value" },
        { name: "TOKEN", valueFrom: { secretKeyRef: { name: "secret", key: "key" } } },
      ],
      serviceAccountName: "agent-sa",
    });
  });

  it("builds standard and sandbox declarative CRs through the same mapping", () => {
    const data = {
      name: "demo",
      namespace: "team-a",
      description: "Demo agent",
      type: "Declarative" as const,
      runInSandbox: true,
      declarativeRuntime: "python" as const,
      systemPrompt: "Use tools",
      modelName: "team-a/main-model",
      stream: false,
      shareTools: true,
      tools: [
        {
          type: "McpServer" as const,
          mcpServer: {
            name: "tools-ns/tool-server",
            kind: "RemoteMCPServer",
            toolNames: ["lookup"],
            requireApproval: ["lookup"],
          },
        },
        {
          type: "Agent" as const,
          agent: { name: "agents-ns/helper" },
        },
      ],
      memory: { modelConfig: "team-a/embedding", ttlDays: 7 },
      promptSources: [{ name: " runbooks ", alias: " ops " }],
      skillRefs: [" registry.example.com/skill:v1 "],
      serviceAccountName: " agent-sa ",
      substrateWorkerPoolRefName: "pool-a",
      substrateSnapshotsLocation: "gs://snapshots",
    };

    const standard = agentFormDataToAgent(data);
    const sandbox = agentFormDataToSandboxAgent(data);

    expect(standard.spec.declarative).toMatchObject({
      runtime: "python",
      modelConfig: "main-model",
      memory: { modelConfig: "embedding", ttlDays: 7 },
      deployment: { serviceAccountName: "agent-sa" },
      promptTemplate: {
        dataSources: [{ kind: "ConfigMap", name: "runbooks", alias: "ops" }],
      },
      tools: [
        {
          type: "McpServer",
          mcpServer: {
            name: "tool-server",
            namespace: "tools-ns",
            requireApproval: ["lookup"],
          },
        },
        {
          type: "Agent",
          agent: {
            name: "helper",
            namespace: "agents-ns",
            kind: "Agent",
            apiGroup: "kagent.dev",
          },
        },
      ],
    });
    expect(sandbox.spec.declarative).toEqual(standard.spec.declarative);
    expect(standard.spec.skills).toEqual({
      refs: ["registry.example.com/skill:v1"],
    });
    expect(sandbox.spec.skills).toBeUndefined();
    expect(sandbox.spec.substrate).toEqual({
      workerPoolRef: { name: "pool-a" },
      snapshotsConfig: { location: "gs://snapshots" },
    });
  });

  it("adds substrate settings to sandbox BYO workloads", () => {
    const sandbox = agentFormDataToSandboxAgent({
      name: "demo",
      namespace: "team-a",
      description: "BYO sandbox agent",
      type: "BYO",
      byoImage: "example.com/agent:v1",
      byoCmd: "/agent",
      substrateWorkerPoolRefName: "pool-a",
      substrateSnapshotsLocation: "gs://snapshots",
    });

    expect(sandbox.spec.byo?.deployment).toMatchObject({
      image: "example.com/agent:v1",
      cmd: "/agent",
    });
    expect(sandbox.spec.substrate).toEqual({
      workerPoolRef: { name: "pool-a" },
      snapshotsConfig: { location: "gs://snapshots" },
    });
  });
});

describe("Agent form validation", () => {
  it("reports an empty name as required", () => {
    expect(validateAgentFormData({ name: "" })).toMatchObject({
      name: "Agent name is required",
    });
  });

  it("validates field data without a React provider", () => {
    expect(
      validateAgentFormData({
        type: "Declarative",
        name: "Invalid Name",
        namespace: "also invalid",
        description: "",
        systemPrompt: "",
        modelName: "",
        tools: [],
        memory: { modelConfig: "", ttlDays: 0 },
      }),
    ).toMatchObject({
      name: expect.any(String),
      namespace: expect.any(String),
      description: "Description is required",
      systemPrompt: "Agent instructions are required",
      model: "Please select a model",
      memoryModel: "Please select an embedding model",
      memoryTtl: "TTL must be at least 1 day",
    });
  });

  it("adds the sandbox BYO command invariant at whole-form validation", () => {
    const state = {
      ...initialState(),
      agentType: "BYO" as const,
      runInSandbox: true,
      name: "demo",
      description: "Demo",
      byoImage: "example/agent:v1",
      byoCmd: "",
    };

    expect(validateAgentFormState(state)).toMatchObject({
      byoCmd: "Command is required for BYO agents on Agent Substrate",
    });
  });
});
