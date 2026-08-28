/**
 * The data the mock backend serves.
 *
 * Shaped like a small but plausible cluster — several namespaces, a declarative
 * agent and a BYO one, a model that is not ready — so that layouts, truncation
 * and status rendering are exercised rather than flattered.
 */

import type { AgentInstance } from "@/api/domain/agentInstances";
import type { AgentResponse } from "@/api/domain/agents";
import type { ToolServerResponse, ToolsResponse } from "@/api/domain/mcpServers";
import type {
  ModelConfig,
  Provider,
  ProviderModelsResponse,
} from "@/api/domain/models";
import type { PromptTemplateDetail, PromptTemplateSummary } from "@/api/domain/prompts";
import type { Session } from "@/api/domain/sessions";
import type { NamespaceResponse } from "@/api/domain/namespaces";
import type { SubstrateStatusResponse } from "@/api/domain/substrate";
import type { Harness } from "@/api/domain/harnesses";
import type { AgentTemplate } from "@/api/domain/agentTemplates";

export const mockAgents: AgentResponse[] = [
  {
    id: 1,
    model: "gpt-4.1",
    modelProvider: "OpenAI",
    modelConfigRef: "kagent/default-model-config",
    deploymentReady: true,
    accepted: true,
    agentKind: "SandboxAgent",
    memoryRefs: [],
    tools: [
      {
        type: "McpServer",
        mcpServer: {
          kind: "MCPServer",
          apiGroup: "kagent.dev",
          name: "kagent-tool-server",
          namespace: "kagent",
          toolNames: ["k8s_get_pods", "k8s_describe_resource", "k8s_get_events"],
          // Mirrors the spec below, because the response carries its own copy of the
          // bindings and that copy is what the UI reads. Setting it on only one of the two
          // left the approval marker rendered by nothing and asserted by nothing.
          requireApproval: ["k8s_describe_resource"],
        },
      },
    ],
    agent: {
      apiVersion: "kagent.dev/v1alpha2",
      kind: "Agent",
      metadata: {
        name: "k8s-agent",
        namespace: "kagent",
        creationTimestamp: "2026-06-02T09:14:00Z",
        resourceVersion: "10241",
      },
      spec: {
        type: "Declarative",
        description: "Investigates cluster state and explains what it finds.",
        declarative: {
          // Bare, as the CRD requires and as every agent on a real cluster
          // stores it: "The name of the model config to use… Must be in the same
          // namespace as the Agent." The response's own `modelConfigRef` above is
          // the namespaced form — two different fields, and the fixture used to
          // carry the namespaced value in both. That made the edit form show
          // "kagent/kagent/default-model-config", because it rebuilds the select's
          // `namespace/name` ref from the agent's namespace and this name.
          modelConfig: "default-model-config",
          stream: true,
          systemMessage:
            "You are a Kubernetes expert. Diagnose problems from real cluster state and never guess.",
          tools: [
            {
              type: "McpServer",
              mcpServer: {
                name: "kagent-tool-server",
                namespace: "kagent",
                toolNames: ["k8s_get_pods", "k8s_describe_resource", "k8s_get_events"],
                requireApproval: ["k8s_describe_resource"],
              },
            },
          ],
          // Both meanings of "skills", because they are different things and the panel
          // that shows them has to keep them apart: capabilities this agent advertises,
          // and packages fetched into its filesystem.
          a2aConfig: {
            skills: [
              {
                id: "cluster-triage",
                name: "Cluster triage",
                description: "Finds what is broken and explains why.",
                tags: ["kubernetes", "diagnostics"],
                examples: [
                  "Which pods are crash-looping?",
                  "Why is the checkout deployment not ready?",
                ],
                inputModes: ["text"],
                outputModes: ["text"],
              },
            ],
          },
          skills: {
            refs: ["ghcr.io/kagent-dev/skills/kubernetes:0.4.1"],
            gitRefs: [
              {
                url: "https://github.com/kagent-dev/runbooks",
                ref: "main",
                path: "sre",
                name: "runbooks",
              },
            ],
          },
        },
      },
      status: {
        observedGeneration: 3,
        conditions: [
          {
            type: "Ready",
            status: "True",
            reason: "DeploymentReady",
            lastTransitionTime: "2026-06-02T09:15:11Z",
          },
        ],
      },
    },
  },
  {
    id: 2,
    model: "claude-sonnet-4",
    modelProvider: "Anthropic",
    modelConfigRef: "kagent/anthropic-model-config",
    deploymentReady: true,
    accepted: true,
    agentKind: "SandboxAgent",
    memoryRefs: [],
    tools: [
      {
        type: "Agent",
        agent: {
          kind: "Agent",
          apiGroup: "kagent.dev",
          name: "k8s-agent",
          namespace: "kagent",
        },
      },
    ],
    agent: {
      apiVersion: "kagent.dev/v1alpha2",
      kind: "Agent",
      metadata: {
        name: "incident-commander",
        namespace: "platform",
        creationTimestamp: "2026-06-11T16:40:00Z",
        resourceVersion: "18877",
      },
      spec: {
        type: "Declarative",
        description: "Coordinates an incident and delegates to specialist agents.",
        declarative: {
          modelConfig: "anthropic-model-config",
          systemMessage: "You run incident response. Delegate, then summarise.",
          tools: [
            {
              type: "Agent",
              agent: { name: "k8s-agent", namespace: "kagent" },
            },
          ],
        },
      },
      status: {
        observedGeneration: 1,
        conditions: [
          {
            type: "Ready",
            status: "True",
            reason: "DeploymentReady",
            lastTransitionTime: "2026-06-11T16:41:02Z",
          },
        ],
      },
    },
  },
  {
    id: 3,
    model: "",
    modelProvider: "",
    modelConfigRef: "",
    deploymentReady: false,
    accepted: false,
    // Declared by the substrate rather than held as an Agent resource — the cluster has
    // two of these, and they are why `hasAgentResource` exists: the list reports them
    // with a uid and a resourceVersion, `GET /agents/{ns}/{name}` answers 404, and a PUT
    // answers 400. Without one in the fixtures nothing exercises the screens that have
    // to decline to edit them.
    agentKind: "AgentHarness",
    memoryRefs: [],
    tools: [],
    agent: {
      apiVersion: "kagent.dev/v1alpha2",
      kind: "Agent",
      metadata: {
        name: "reporting-agent",
        namespace: "analytics",
        creationTimestamp: "2026-07-21T08:02:00Z",
        resourceVersion: "20913",
      },
      spec: {
        type: "BYO",
        description: "Bring-your-own image that renders weekly reports.",
        byo: {
          deployment: {
            image: "ghcr.io/kagent-dev/reporting-agent:0.4.2",
            replicas: 1,
            imagePullPolicy: "IfNotPresent",
          },
        },
      },
      status: {
        observedGeneration: 1,
        conditions: [
          {
            type: "Ready",
            status: "False",
            reason: "ImagePullBackOff",
            message: "Back-off pulling image ghcr.io/kagent-dev/reporting-agent:0.4.2",
            lastTransitionTime: "2026-07-21T08:05:44Z",
          },
        ],
      },
    },
  },
];

export const mockModels: ModelConfig[] = [
  {
    ref: "kagent/default-model-config",
    spec: {
      model: "gpt-4.1",
      provider: "OpenAI",
      apiKeySecret: "kagent-openai",
      apiKeySecretKey: "OPENAI_API_KEY",
      openAI: { temperature: "0.2", maxTokens: 8192 },
    },
  },
  {
    ref: "kagent/anthropic-model-config",
    spec: {
      model: "claude-sonnet-4",
      provider: "Anthropic",
      apiKeySecret: "kagent-anthropic",
      apiKeySecretKey: "ANTHROPIC_API_KEY",
      anthropic: { maxTokens: 16384, temperature: "0.1" },
    },
  },
  {
    ref: "platform/ollama-local",
    spec: {
      model: "llama3.2",
      provider: "Ollama",
      ollama: { host: "ollama.platform.svc.cluster.local:11434" },
    },
  },
  {
    ref: "analytics/bedrock-haiku",
    spec: {
      model: "anthropic.claude-3-5-haiku",
      provider: "AmazonBedrock",
      bedrock: { region: "us-east-1" },
    },
  },
];

export const mockProviderModels: ProviderModelsResponse = {
  OpenAI: [
    { name: "gpt-4.1", function_calling: true },
    { name: "gpt-4.1-mini", function_calling: true },
    { name: "o4-mini", function_calling: true },
  ],
  Anthropic: [
    { name: "claude-sonnet-4", function_calling: true },
    { name: "claude-haiku-4", function_calling: true },
  ],
  Ollama: [{ name: "llama3.2", function_calling: false }],
};

/**
 * Supported providers with the parameters each one's config block accepts.
 *
 * Mirrors `GET /providers/models`: `name` and `type` are both the provider enum,
 * `requiredParams` are the fields the controller marks required (Azure's
 * endpoint/version, Bedrock's region, SAP's base URL), and `optionalParams` are
 * the remaining JSON keys of that provider's config struct.
 */
export const mockProviders: Provider[] = [
  {
    name: "OpenAI",
    type: "OpenAI",
    requiredParams: [],
    optionalParams: [
      "baseUrl",
      "organization",
      "temperature",
      "maxTokens",
      "topP",
      "frequencyPenalty",
      "presencePenalty",
      "seed",
      "n",
      "timeout",
      "reasoningEffort",
    ],
  },
  {
    name: "Anthropic",
    type: "Anthropic",
    requiredParams: [],
    optionalParams: ["baseUrl", "maxTokens", "temperature", "topP", "topK"],
  },
  {
    name: "AzureOpenAI",
    type: "AzureOpenAI",
    requiredParams: ["azureEndpoint", "apiVersion"],
    optionalParams: ["azureDeployment", "azureAdToken", "temperature", "maxTokens", "topP"],
  },
  {
    name: "Ollama",
    type: "Ollama",
    requiredParams: [],
    optionalParams: ["host", "options"],
  },
  {
    name: "Gemini",
    type: "Gemini",
    requiredParams: [],
    optionalParams: ["baseUrl", "temperature", "maxTokens", "topP", "topK"],
  },
  {
    name: "Bedrock",
    type: "Bedrock",
    requiredParams: ["region"],
    optionalParams: [],
  },
  {
    name: "SAPAICore",
    type: "SAPAICore",
    requiredParams: ["baseUrl"],
    optionalParams: ["resourceGroup", "authUrl"],
  },
  /*
   * One provider an operator added, rather than one the controller ships with.
   *
   * `models.providers` merges two RPCs — `ListSupportedModelProviders` and
   * `ListConfiguredProviders` — and a merge with nothing on one side of it is
   * wired and unexercised, which is the shape of thing that is wrong and green.
   *
   * Checked against the controller rather than against this app's types
   * (`Service.ListConfiguredProviders`, `go/core/internal/service/model/discovery.go`):
   * a configured provider is a `ModelProviderConfig` resource, so its `name` is the
   * *resource's* name and its `type` is the provider enum — the two differ, where
   * for every stock provider above they are the same string. It carries an endpoint
   * and no parameter lists, because the controller reports none for one. Only
   * configs whose Ready condition is true are listed at all, so this fixture stands
   * for a provider that came up rather than one merely created.
   */
  {
    name: "example-openai-proxy",
    type: "OpenAI",
    requiredParams: [],
    optionalParams: [],
    source: "configured",
    endpoint: "https://llm.example.test/v1",
  },
];

export const mockMcpServers: ToolServerResponse[] = [
  {
    ref: "kagent/kagent-tool-server",
    groupKind: "MCPServer.kagent.dev",
    discoveredTools: [
      { name: "k8s_get_pods", description: "List pods in a namespace." },
      { name: "k8s_describe_resource", description: "Describe any cluster resource." },
      { name: "k8s_get_events", description: "Read recent events for a resource." },
    ],
  },
  {
    ref: "platform/grafana-mcp",
    groupKind: "RemoteMCPServer.kagent.dev",
    discoveredTools: [
      { name: "grafana_query", description: "Run a PromQL query." },
      { name: "grafana_list_dashboards", description: "List dashboards." },
    ],
  },
  {
    ref: "analytics/warehouse-mcp",
    groupKind: "RemoteMCPServer.kagent.dev",
    discoveredTools: [],
  },
];

export const mockTools: ToolsResponse[] = mockMcpServers.flatMap((server) =>
  server.discoveredTools.map((tool, index) => ({
    // The bare tool name, as the controller returns it. This fixture used to qualify it
    // with the server ref, so anything matching a tool to its description agreed with the
    // fixture and matched nothing on a cluster. Checked against a live controller:
    // {"id": "helm_uninstall", "server_name": "kagent/kagent-tool-server", ...}.
    id: tool.name,
    server_name: server.ref,
    description: tool.description,
    group_kind: server.groupKind,
    created_at: `2026-06-0${index + 1}T10:00:00Z`,
    updated_at: `2026-06-0${index + 1}T10:00:00Z`,
    deleted_at: "",
  })),
);

export const mockPrompts: PromptTemplateSummary[] = [
  {
    namespace: "kagent",
    name: "shared-fragments",
    keyCount: 3,
    keys: ["tone", "safety", "escalation"],
  },
  {
    namespace: "platform",
    name: "incident-playbooks",
    keyCount: 2,
    keys: ["triage", "postmortem"],
  },
];

export const mockPromptDetails: Record<string, PromptTemplateDetail> = {
  "kagent/shared-fragments": {
    namespace: "kagent",
    name: "shared-fragments",
    data: {
      tone: "Be concise. Prefer evidence from the cluster over speculation.",
      safety: "Never run a destructive command without explicit confirmation.",
      escalation: "If two attempts fail, summarise findings and hand off to a human.",
    },
  },
  "platform/incident-playbooks": {
    namespace: "platform",
    name: "incident-playbooks",
    data: {
      triage: "Establish blast radius, then the most recent change.",
      postmortem: "Timeline, contributing factors, action items with owners.",
    },
  },
};

export const mockSessions: Session[] = [
  {
    id: "session-8f31",
    name: "Why is checkout crashlooping?",
    agent_id: "kagent__NS__k8s-agent",
    user_id: "admin@kagent.dev",
    created_at: "2026-07-28T09:12:00Z",
    updated_at: "2026-07-28T09:31:00Z",
    deleted_at: "",
  },
  {
    id: "session-2b07",
    name: "Node pressure on the analytics pool",
    agent_id: "kagent__NS__k8s-agent",
    user_id: "admin@kagent.dev",
    created_at: "2026-07-29T14:02:00Z",
    updated_at: "2026-07-29T14:20:00Z",
    deleted_at: "",
  },
];

/** The namespaces the fixtures' agents and models actually live in, plus a couple more. */
export const mockNamespaces: NamespaceResponse[] = [
  { name: "kagent", status: "Active" },
  { name: "platform", status: "Active" },
  { name: "analytics", status: "Active" },
  { name: "default", status: "Active" },
  // A terminating namespace: a picker should not offer it as a create target.
  { name: "retired-team", status: "Terminating" },
];

/**
 * Substrate inventory.
 *
 * `ateApiError` is set deliberately: a successful response whose runtime halves
 * are partial is the state most likely to be rendered as though everything were
 * fine, so the fixture makes it the default rather than a special case.
 */
export const mockSubstrateStatus: SubstrateStatusResponse = {
  enabled: true,
  ateApiError: "ate-api list actors timed out after 5s; actors may be incomplete",
  workerPools: [
    { namespace: "kagent", name: "default-pool", replicas: 3, ateomImage: "ghcr.io/ate-dev/ateom:1.4.0" },
    { namespace: "platform", name: "gpu-pool", replicas: 1, ateomImage: "ghcr.io/ate-dev/ateom:1.4.0" },
  ],
  actorTemplates: [
    {
      namespace: "kagent",
      name: "coder-template",
      phase: "Ready",
      goldenActorId: "actor-golden-001",
      goldenSnapshot: "snap-2026-07-28",
      sandboxClass: "standard",
      workerSelector: "pool=default-pool",
      harnessName: "openclaw",
      managedByKagent: true,
    },
    {
      namespace: "platform",
      name: "external-template",
      phase: "Pending",
      managedByKagent: false,
    },
  ],
  actors: [
    {
      actorId: "actor-7f21",
      atespace: "kagent",
      status: "Running",
      actorTemplateNamespace: "kagent",
      actorTemplateName: "coder-template",
      ateomPodNamespace: "kagent",
      ateomPodName: "ateom-default-pool-0",
      ateomPodIp: "10.42.1.19",
      latestSnapshot: "snap-2026-07-29",
      workerPoolName: "default-pool",
      version: 4,
    },
    { actorId: "actor-9c03", status: "Snapshotting", inProgressSnapshot: "snap-2026-07-30", version: 2 },
    // Last in the fixture and first once sorted: ate-api returns actors in no
    // particular order, so a fixture that is already in the right order cannot tell
    // a page that sorts from one that does not.
    { actorId: "actor-0aa1", status: "Failed", version: 1 },
    // Shares "Running" with actor-7f21, which is what makes a two-key sort observable:
    // with every status distinct, sorting by status then by id looks the same as
    // sorting by status alone.
    { actorId: "actor-3b55", status: "Running", version: 1 },
  ],
  workers: [
    {
      workerNamespace: "kagent",
      workerPool: "default-pool",
      workerPod: "ateom-default-pool-0",
      actorNamespace: "kagent",
      actorTemplate: "coder-template",
      actorId: "actor-7f21",
      ip: "10.42.1.19",
      version: 4,
    },
    { workerNamespace: "kagent", workerPool: "default-pool", workerPod: "ateom-default-pool-1" },
  ],
};

/**
 * Who the fixture backend treats every caller as.
 *
 * The controller filters an instance list by the authenticated user unless
 * `all_creators` is asked for, and mock mode has nobody signed in — there is no
 * backend to have signed in to. Rather than pretend the filter does not exist, the
 * fake treats every call as this person, so the toggle is observably a filter and
 * not decoration: asked for the `kagent` namespace it lists four instances without
 * the toggle and seven with it.
 */
export const MOCK_INSTANCE_CREATOR = "alice@example.com";

/**
 * Agent instances, spread across the states the page has to render.
 *
 * Deliberately not a set of healthy rows. Six of the seven `AgentInstanceState`
 * values appear here — every one but `deleted`, which is the state a record leaves
 * the list in — because the states are the entire point of the page and a fixture
 * set of ready instances would prove only that a table renders. In particular:
 *
 * - one `ready` and one `suspended`, so both lifecycle buttons have something to
 *   act on and each is disabled on the other's row;
 * - one mid-operation (`creating` with `create` claimed), where both buttons must
 *   be refused because the controller's `claim` refuses a second operation;
 * - one `failed` carrying a `Failure`, which is the only row that has one;
 * - one whose state the controller never reported, with no harness, no template
 *   and no timestamps — the row that catches a page rendering absent values as
 *   blank cells rather than saying so;
 * - three the caller did not create — two of somebody else's and the barely-written
 *   one, whose creator is nobody at all — so `all_creators` changes the answer from
 *   four rows in `kagent` to seven;
 * - one outside `kagent`, so a page that ignored the namespace it was asked for
 *   would show it in the wrong list.
 *
 * The ids are UUIDs because the controller insists: `validateIdentity` parses one
 * and rejects anything else with `InvalidArgument`, so a friendlier fixture id like
 * "instance-1" would work here and fail against a cluster.
 */
export const mockAgentInstances: AgentInstance[] = [
  {
    id: "6f1c9d20-1b7a-4a1e-9a3f-2c0d8e5b1a44",
    namespace: "kagent",
    // Named by the reader, which is the point of the column: this is the row that
    // proves a list of conversations can read as a list of things somebody chose.
    name: "Tuesday cluster review",
    creator: MOCK_INSTANCE_CREATOR,
    harness: "kagent/k8s-agent",
    agentTemplate: "kagent/k8s-agent-7f3a91c",
    preparedRevision: "rev-7f3a91c",
    a2aAuthority: "k8s-agent-6f1c9d20.kagent.svc.cluster.local:8080",
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-18T09:12:00Z",
    updatedAt: "2026-08-20T14:03:00Z",
    labels: { team: "platform", tier: "interactive" },
  },
  {
    id: "b28e4f13-5c66-4d90-8f2b-77a1e9c34d05",
    namespace: "kagent",
    // Unnamed, and the same agent as the row above — so the two sit side by side
    // and a page that rendered a bare UUID as a name would be obvious.
    name: "",
    creator: MOCK_INSTANCE_CREATOR,
    harness: "kagent/k8s-agent",
    agentTemplate: "kagent/k8s-agent-7f3a91c",
    preparedRevision: "rev-7f3a91c",
    a2aAuthority: "k8s-agent-b28e4f13.kagent.svc.cluster.local:8080",
    state: "suspended",
    operation: "unspecified",
    /*
     * Older than the named sibling above it, which the rail renders *after* this one
     * when nothing sorts them.
     *
     * Deliberate, and load-bearing for `agent rail: the newest conversation is at the
     * top`: unsorted, this row comes first, so a rail that sorts newest-first has to
     * move it and one that does not cannot accidentally pass.
     */
    createdAt: "2026-08-11T16:40:00Z",
    updatedAt: "2026-08-19T08:22:00Z",
    labels: { team: "platform" },
  },
  {
    id: "0a7d6c58-9e21-4b3c-a05d-4e8f1b6d2277",
    namespace: "kagent",
    name: "",
    creator: MOCK_INSTANCE_CREATOR,
    harness: "kagent/support-triage",
    agentTemplate: "kagent/support-triage-2b91d0e",
    preparedRevision: "rev-2b91d0e",
    // Not yet reachable: the controller fills the authority once the actor is
    // running, so an instance still being created has none. A page that printed an
    // empty string here would look like a broken endpoint rather than a pending one.
    a2aAuthority: undefined,
    state: "creating",
    operation: "create",
    createdAt: "2026-08-21T07:55:00Z",
    updatedAt: "2026-08-21T07:55:00Z",
    labels: {},
  },
  {
    id: "d4b02f87-3a55-4c18-9e6b-1f70c9a8e332",
    namespace: "kagent",
    name: "Escalation from the weekend",
    creator: MOCK_INSTANCE_CREATOR,
    harness: "kagent/support-triage",
    agentTemplate: "kagent/support-triage-2b91d0e",
    preparedRevision: "rev-2b91d0e",
    a2aAuthority: undefined,
    state: "failed",
    operation: "unspecified",
    failure: {
      reason: "ActorUnavailable",
      message:
        "actor kagent/agent-d4b02f87 cannot be resumed from status ACTOR_STATE_TERMINATED",
    },
    createdAt: "2026-08-15T11:30:00Z",
    updatedAt: "2026-08-20T22:41:00Z",
    labels: { team: "support" },
  },
  {
    id: "3c9a1e64-8d47-4f22-b71a-05e2d8c96b18",
    namespace: "kagent",
    name: "",
    creator: "bob@example.com",
    harness: "kagent/k8s-agent",
    agentTemplate: "kagent/k8s-agent-7f3a91c",
    preparedRevision: "rev-7f3a91c",
    a2aAuthority: "k8s-agent-3c9a1e64.kagent.svc.cluster.local:8080",
    state: "deleting",
    operation: "delete",
    createdAt: "2026-08-09T13:05:00Z",
    updatedAt: "2026-08-21T06:10:00Z",
    labels: {},
  },
  {
    id: "8e5f2b09-6c14-4a7d-83b0-9d1c7e40f5a6",
    namespace: "kagent",
    // Somebody else's, and named — so a row that cannot be opened still reads as a
    // conversation rather than as a blank.
    name: "Search relevance spike",
    creator: "bob@example.com",
    harness: "kagent/k8s-agent",
    agentTemplate: "kagent/k8s-agent-7f3a91c",
    preparedRevision: "rev-7f3a91c",
    a2aAuthority: "k8s-agent-8e5f2b09.kagent.svc.cluster.local:8080",
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-20T10:00:00Z",
    updatedAt: "2026-08-20T10:00:00Z",
    labels: { team: "search" },
  },
  {
    /*
     * The record the controller has barely written.
     *
     * Every optional field absent and the state left at its proto zero, which is a
     * real thing a database row can be and the one shape a table of confident
     * strings gets wrong. It is here so the "not reported" wording is exercised by
     * a test rather than being prose nobody ever sees.
     */
    id: "f07b3d41-2e58-4c96-a8d3-6b9042e17c5f",
    namespace: "kagent",
    name: "",
    creator: "",
    harness: undefined,
    agentTemplate: undefined,
    preparedRevision: undefined,
    a2aAuthority: undefined,
    state: "unspecified",
    operation: "unspecified",
    createdAt: "",
    updatedAt: "",
    labels: {},
  },
  {
    id: "5a3c8e17-4b92-4d05-9f61-8c2e7a03b4d9",
    namespace: "analytics",
    name: "Weekly numbers",
    creator: MOCK_INSTANCE_CREATOR,
    // The harness in `analytics`, not the one in `kagent`: admission never crosses a
    // namespace, so a conversation whose pair spanned two would be one the
    // controller could not have produced.
    harness: "analytics/reporting",
    agentTemplate: "analytics/reporting-agent-9d4e2f1",
    preparedRevision: "rev-9d4e2f1",
    a2aAuthority: "reporting-agent-5a3c8e17.analytics.svc.cluster.local:8080",
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-17T18:20:00Z",
    updatedAt: "2026-08-21T05:15:00Z",
    labels: { team: "analytics" },
  },
  /*
   * One conversation with each of the two agents `shared-brain` is.
   *
   * Same template, different harness — so the two rows are indistinguishable on
   * everything except the pair, and a page that narrowed on the template alone would
   * show each of them under both agents. That is precisely what
   * `ListAgentInstances`'s two filters exist to prevent, and these are what prove
   * the narrowing uses both.
   */
  {
    id: "1d4f7a92-0c38-4e61-b25a-7f930e6c8b14",
    namespace: "kagent",
    name: "Drafting the runbook",
    creator: MOCK_INSTANCE_CREATOR,
    harness: "kagent/k8s-agent",
    agentTemplate: "kagent/shared-brain",
    preparedRevision: "rev-shared-k8s",
    a2aAuthority: "shared-brain-1d4f7a92.kagent.svc.cluster.local:8080",
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-19T09:00:00Z",
    updatedAt: "2026-08-21T11:12:00Z",
    labels: {},
  },
  {
    /*
     * The one a test may delete.
     *
     * It exists because every other instance here is load-bearing for some
     * assertion, and because deleting is now scoped to the creator exactly as
     * reading is — so a sweep cannot simply pick the least interesting row if that
     * row belongs to nobody. Cut from a real pair rather than left orphaned, so its
     * presence changes a conversation count rather than the "not listed under any
     * agent" note, which is a quieter thing to disturb.
     */
    id: "9c3b7e18-40d6-4a52-8b71-e2f05c96a3d7",
    namespace: "kagent",
    name: "Scratch conversation",
    creator: MOCK_INSTANCE_CREATOR,
    harness: "kagent/support-triage",
    agentTemplate: "kagent/support-triage-2b91d0e",
    preparedRevision: "rev-2b91d0e",
    a2aAuthority: undefined,
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-16T12:00:00Z",
    updatedAt: "2026-08-16T12:00:00Z",
    labels: {},
  },
  {
    id: "2b6e0c45-8a71-4f39-9d02-3c85f1a7e6d0",
    namespace: "kagent",
    name: "",
    creator: MOCK_INSTANCE_CREATOR,
    harness: "kagent/fast-lane",
    agentTemplate: "kagent/shared-brain",
    preparedRevision: "rev-shared-fast",
    a2aAuthority: "shared-brain-2b6e0c45.kagent.svc.cluster.local:8080",
    state: "ready",
    operation: "unspecified",
    createdAt: "2026-08-20T15:30:00Z",
    updatedAt: "2026-08-20T15:44:00Z",
    labels: {},
  },
];

/**
 * The harnesses an agent can be built on.
 *
 * A `Harness` is the runtime half of a pair: which adapter, which worker pool,
 * which digest-pinned image. `k8s-agent` and `support-triage` are the two the
 * instances above are cut from, so the create form and the instance list agree with
 * each other.
 *
 * The image is digest-pinned because the CRD's CEL rejects a tag, and a fixture
 * carrying a tag would pass every mock test and fail against a cluster — which is
 * exactly the class of failure this file's fixtures have caused before.
 */
export const mockHarnesses: Harness[] = [
  {
    ref: "kagent/k8s-agent",
    namespace: "kagent",
    name: "k8s-agent",
    runtime: "kagent",
    workloadImage:
      "ghcr.io/kagent-dev/kagent/golang-adk@sha256:3f1c9d2e5b7a48e0a1c6d9f2b4e8a7c30d5f6e9b2a4c8d1e7f0b3a6c9d2e5f8a",
    ready: true,
    resource: {
      metadata: { name: "k8s-agent", namespace: "kagent" },
      spec: {
        kagent: {},
        substrate: { workerPoolRef: { name: "kagent-default" }, snapshotPolicy: "OnIdle" },
        // The selector is the whole of admission: a harness with none admits no
        // templates at all, which the CRD says explicitly. A fixture without one
        // would make every template unusable and every admission control dead.
        allowedAgentTemplates: {
          selector: { matchLabels: { "kagent.dev/runtime": "k8s-agent" } },
        },
      },
    },
  },
  {
    ref: "kagent/support-triage",
    namespace: "kagent",
    name: "support-triage",
    runtime: "claude",
    workloadImage:
      "ghcr.io/kagent-dev/kagent/claude-adk@sha256:9b2a4c8d1e7f0b3a6c9d2e5f8a3f1c9d2e5b7a48e0a1c6d9f2b4e8a7c30d5f6e",
    // Not ready, and still offered: the controller may simply not have observed it
    // yet, so refusing to let a reader choose it would block a create that would
    // succeed. The picker says so instead.
    ready: false,
    resource: {
      metadata: { name: "support-triage", namespace: "kagent" },
      spec: {
        claude: {},
        substrate: { workerPoolRef: { name: "kagent-default" }, snapshotPolicy: "OnIdle" },
        allowedAgentTemplates: {
          selector: { matchLabels: { "kagent.dev/runtime": "support-triage" } },
        },
      },
    },
  },
  {
    /*
     * The second harness that admits `shared-brain`.
     *
     * It exists so the fixtures carry a template admitted by *two* harnesses, which
     * is the case that makes an agent a pair rather than a template: one
     * configuration, two runtimes, two prepared revisions and two separate sets of
     * conversations. A fixture set where every template had exactly one harness
     * would let the agents page collapse the two and stay green.
     */
    ref: "kagent/fast-lane",
    namespace: "kagent",
    name: "fast-lane",
    runtime: "codex",
    workloadImage:
      "ghcr.io/kagent-dev/kagent/codex-adk@sha256:4e8a7c30d5f6e9b2a4c8d1e7f0b3a6c9d2e5f8a3f1c9d2e5b7a48e0a1c6d9f2b",
    ready: true,
    resource: {
      metadata: { name: "fast-lane", namespace: "kagent" },
      spec: {
        codex: {},
        substrate: { workerPoolRef: { name: "kagent-default" }, snapshotPolicy: "OnIdle" },
        // Selects the *shared* label rather than a runtime name, which is how one
        // template comes to be admitted by two harnesses on a real cluster.
        allowedAgentTemplates: {
          selector: { matchLabels: { "kagent.dev/tier": "shared" } },
        },
      },
    },
  },
  {
    /*
     * A harness outside `kagent`.
     *
     * Admission never crosses a namespace, so this one is what makes the
     * `analytics` template an agent at all — and it is what catches a page or a
     * fixture that matched harnesses to templates on labels alone.
     */
    ref: "analytics/reporting",
    namespace: "analytics",
    name: "reporting",
    runtime: "kagent",
    workloadImage:
      "ghcr.io/kagent-dev/kagent/golang-adk@sha256:6e9b2a4c8d1e7f0b3a6c9d2e5f8a3f1c9d2e5b7a48e0a1c6d9f2b4e8a7c30d5f",
    ready: true,
    resource: {
      metadata: { name: "reporting", namespace: "analytics" },
      spec: {
        kagent: {},
        substrate: { workerPoolRef: { name: "kagent-default" }, snapshotPolicy: "OnIdle" },
        allowedAgentTemplates: {
          selector: { matchLabels: { "kagent.dev/runtime": "reporting" } },
        },
      },
    },
  },
];

/**
 * The templates an agent can be built from.
 *
 * `admittingHarnesses` is what makes the create form's narrowing real: a harness
 * admits templates through a label selector, and only the template's *status* says
 * which ones matched. `note-taker` is admitted by nothing, which is the state the
 * form has to explain rather than hide — a template missing from the picker sends a
 * reader looking for a bug in their template.
 */
export const mockAgentTemplates: AgentTemplate[] = [
  {
    ref: "kagent/k8s-agent-7f3a91c",
    namespace: "kagent",
    name: "k8s-agent-7f3a91c",
    modelConfigRef: "kagent/default-model-config",
    description: "Answers questions about workloads in the cluster.",
    admittingHarnesses: ["k8s-agent"],
    resource: {
      // The label is what a harness admits on, so a fixture without one would be a
      // template no harness could run — which is a real state, and `note-taker`
      // below is the one that covers it.
      metadata: {
        name: "k8s-agent-7f3a91c",
        namespace: "kagent",
        labels: { "kagent.dev/runtime": "k8s-agent" },
      },
      /*
       * One entry per admitting harness — which is one entry per *agent*.
       *
       * This is where the agents list comes from: the controller derives
       * `admittingHarnesses` from exactly this field, and it carries the revision
       * state as well as the name. A fixture that filled `admittingHarnesses` and
       * left the status empty would serve a template that reports two harnesses and
       * produces no agents, and only the page would notice.
       */
      status: {
        observedGeneration: 1,
        harnesses: [
          {
            harness: "k8s-agent",
            desiredRevision: "rev-7f3a91c",
            latestSuccessfulRevision: "rev-7f3a91c",
          },
        ],
      },
      spec: {
        modelConfig: { name: "default-model-config" },
        description: "Answers questions about workloads in the cluster.",
        systemPrompt: "You are a Kubernetes operations assistant.",
        tools: [
          {
            mcp: {
              server: { kind: "RemoteMCPServer", name: "kagent-tool-server" },
              tools: ["k8s_get_pods", "k8s_get_events"],
            },
          },
        ],
        // Not authored by the form, and here on purpose: an edit that dropped it
        // would be invisible on screen, so this is what the round-trip test guards.
        skills: [
          {
            name: "incident-review",
            source: {
              oci: "ghcr.io/kagent-dev/skills@sha256:0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0",
            },
          },
        ],
      },
    },
  },
  {
    ref: "kagent/support-triage-2b91d0e",
    namespace: "kagent",
    name: "support-triage-2b91d0e",
    modelConfigRef: "kagent/default-model-config",
    description: "Triages inbound support conversations.",
    admittingHarnesses: ["support-triage"],
    resource: {
      metadata: {
        name: "support-triage-2b91d0e",
        namespace: "kagent",
        labels: { "kagent.dev/runtime": "support-triage" },
      },
      /*
       * Admitted, and still preparing.
       *
       * A desired revision with none successful yet, because its harness has not
       * reported ready. That is the third state the agents list has to render and
       * the one most easily got wrong: it is not a failure, so a row saying "broken"
       * about it would be inventing a fact — and a create against it really is
       * refused, with `FailedPrecondition`.
       */
      status: {
        observedGeneration: 3,
        harnesses: [
          {
            harness: "support-triage",
            desiredRevision: "rev-2b91d0e",
            conditions: [
              {
                type: "Ready",
                status: "False",
                reason: "ActorTemplateNotReady",
                message: "Waiting for the golden snapshot of the support-triage harness.",
              },
            ],
          },
        ],
      },
      spec: {
        modelConfig: { name: "default-model-config" },
        description: "Triages inbound support conversations.",
        // The other prompt source: read from a ConfigMap rather than inline. The two
        // are mutually exclusive on the CRD, so a form has to know which is in use.
        systemPromptFrom: { name: "support-prompts", key: "triage" },
      },
    },
  },
  {
    ref: "kagent/note-taker",
    namespace: "kagent",
    name: "note-taker",
    modelConfigRef: "kagent/default-model-config",
    description: "Summarises a conversation into notes.",
    // No harness admits it, which the create form names rather than hiding.
    admittingHarnesses: [],
    resource: {
      // No labels at all, which is why nothing admits it. This is the shape a
      // template takes when it is authored without thinking about admission, and
      // the one the form exists to stop a reader creating by accident.
      metadata: { name: "note-taker", namespace: "kagent" },
      // The status a cluster really wrote for an unlabelled template: the generation
      // observed and nothing else. It is *not* an agent, and it is why the agents
      // list can be shorter than the templates list without anything being wrong.
      status: { observedGeneration: 1 },
      spec: {
        modelConfig: { name: "default-model-config" },
        description: "Summarises a conversation into notes.",
        systemPrompt: "You take notes.",
      },
    },
  },
  {
    /*
     * One template, two harnesses — and therefore two agents.
     *
     * The case that decides whether this build models an agent as a pair or as a
     * template. Its label is selected by both `k8s-agent` and `fast-lane`, so the
     * controller materialises two pairs with two revisions, and a reader has two
     * agents with the same name that are told apart only by what runs them. A page
     * that keyed on the template would show one row and quietly merge two agents'
     * conversations.
     */
    ref: "kagent/shared-brain",
    namespace: "kagent",
    name: "shared-brain",
    modelConfigRef: "kagent/default-model-config",
    description: "One configuration, run on two different runtimes.",
    admittingHarnesses: ["fast-lane", "k8s-agent"],
    resource: {
      metadata: {
        name: "shared-brain",
        namespace: "kagent",
        labels: { "kagent.dev/runtime": "k8s-agent", "kagent.dev/tier": "shared" },
      },
      status: {
        observedGeneration: 2,
        harnesses: [
          {
            harness: "fast-lane",
            desiredRevision: "rev-shared-fast",
            latestSuccessfulRevision: "rev-shared-fast",
          },
          {
            harness: "k8s-agent",
            desiredRevision: "rev-shared-k8s",
            latestSuccessfulRevision: "rev-shared-k8s",
          },
        ],
      },
      spec: {
        modelConfig: { name: "default-model-config" },
        description: "One configuration, run on two different runtimes.",
        systemPrompt: "You are a general assistant.",
      },
    },
  },
  {
    /*
     * The template behind the `analytics` conversation.
     *
     * Present so the agents page and the instance fixtures agree about what exists:
     * without it that conversation would be cut from a template nothing lists, which
     * is a real state the page reports separately — and one that should be reached
     * deliberately rather than by a fixture forgetting a row.
     */
    ref: "analytics/reporting-agent-9d4e2f1",
    namespace: "analytics",
    name: "reporting-agent-9d4e2f1",
    modelConfigRef: "analytics/default-model-config",
    description: "Turns weekly numbers into a summary.",
    admittingHarnesses: ["reporting"],
    resource: {
      metadata: {
        name: "reporting-agent-9d4e2f1",
        namespace: "analytics",
        labels: { "kagent.dev/runtime": "reporting" },
      },
      status: {
        observedGeneration: 1,
        harnesses: [
          {
            harness: "reporting",
            desiredRevision: "rev-9d4e2f1",
            latestSuccessfulRevision: "rev-9d4e2f1",
          },
        ],
      },
      spec: {
        modelConfig: { name: "default-model-config" },
        description: "Turns weekly numbers into a summary.",
        systemPrompt: "You summarise reporting data.",
      },
    },
  },
];
