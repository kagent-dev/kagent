import type { AgentsContextType } from "@/components/AgentsProvider";
import type { Agent, AgentResponse, ModelConfig, PromptTemplateSummary, ToolServerResponse } from "@/types";

/** Storybook-only agents context; inner provider overrides the preview mock. */
export function createStoryAgentsContext(overrides: Partial<AgentsContextType>): AgentsContextType {
  const base: AgentsContextType = {
    agents: [],
    models: [],
    loading: false,
    error: "",
    tools: [],
    refreshAgents: async () => {},
    refreshModels: async () => {},
    refreshTools: async () => {},
    createNewAgent: async () => ({ message: "ok", data: {} as Agent }),
    updateAgent: async () => ({ message: "ok", data: {} as Agent }),
    getAgent: async () => null,
    validateAgentData: () => ({}),
  };
  return { ...base, ...overrides };
}

export const storyAgentResponses: AgentResponse[] = [
  {
    id: 1,
    agent: {
      metadata: { name: "support-bot", namespace: "kagent" },
      spec: {
        type: "Declarative",
        description: "Answers support questions using cluster context.",
      },
    },
    model: "gpt-4o",
    modelProvider: "openai",
    modelConfigRef: "kagent/default-openai",
    tools: [
      {
        type: "McpServer",
        mcpServer: { name: "cluster-tools", namespace: "kagent", toolNames: ["kubectl_get"] },
      },
    ],
    deploymentReady: true,
    accepted: true,
  },
  {
    id: 2,
    agent: {
      metadata: { name: "long-running-analyzer", namespace: "team-a" },
      spec: {
        type: "Declarative",
        description: "Batch analysis and summarization.",
      },
    },
    model: "claude-3-5-sonnet",
    modelProvider: "anthropic",
    modelConfigRef: "team-a/anthropic",
    tools: [],
    deploymentReady: false,
    accepted: true,
  },
];

export const storyModelConfigs: ModelConfig[] = [
  {
    ref: "default/openai-prod",
    spec: {
      model: "gpt-4o",
      provider: "OpenAI",
      apiKeySecret: "openai-api-secret",
      openAI: { temperature: "0.2", maxTokens: 4096 },
    },
  },
  {
    ref: "kagent/ollama-local",
    spec: {
      model: "llama3.2",
      provider: "Ollama",
      ollama: { host: "http://ollama:11434" },
    },
  },
];

export const storyMcpServers: ToolServerResponse[] = [
  {
    ref: "kagent/filesystem-mcp",
    groupKind: "MCPServer.kagent.dev/v1alpha1",
    discoveredTools: [
      { name: "read_file", description: "Read contents from a path" },
      { name: "write_file", description: "Write contents to a path" },
    ],
  },
  {
    ref: "default/kubernetes-tools",
    groupKind: "RemoteMCPServer.kagent.dev/v1alpha1",
    discoveredTools: [{ name: "kubectl", description: "Run kubectl commands" }],
  },
];

export const storyPromptLibraries: PromptTemplateSummary[] = [
  { namespace: "kagent", name: "shared-lib", keyCount: 4, keys: ["system", "tools", "memory", "closing"] },
  { namespace: "kagent", name: "team-a", keyCount: 2, keys: ["intro", "outro"] },
];
