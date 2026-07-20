// Typed, spec-side mock builders. Use these in specs to construct payloads for
// assertions or (from Stage 1) to POST scenarios to the stub's /__mock/scenario
// endpoint. The stub backend's runtime happy-path lives in server.mjs; keep the
// shapes here in sync with it.
//
// Typed against the app's own types (via the @/ alias, see playwright/tsconfig.json)
// so drift from the real API surface fails at compile time.

import type {
  AgentResponse,
  ModelConfig,
  ProviderModelsResponse,
  RemoteMCPServerResponse,
  ToolsResponse,
} from "@/types";

export type Namespace = { name: string; status: string };
export type ToolServerListEntry = RemoteMCPServerResponse;

export function mockAgentResponse(
  overrides: Partial<AgentResponse> = {},
): AgentResponse {
  return {
    id: "1",
    agent: {
      metadata: { name: "e2e-agent", namespace: "default" },
      spec: { type: "Declarative", description: "Seeded E2E agent" },
    },
    model: "gpt-4o",
    modelProvider: "OpenAI",
    modelConfigRef: "default/default-model-config",
    tools: [],
    deploymentReady: true,
    accepted: true,
    ...overrides,
  };
}

export function mockModelsResponse(): ProviderModelsResponse {
  return { openai: [{ name: "gpt-4o", function_calling: true }] };
}

export function mockModelConfig(
  overrides: Partial<ModelConfig> = {},
): ModelConfig {
  return {
    ref: "default/default-model-config",
    spec: { model: "gpt-4o", provider: "OpenAI" },
    ...overrides,
  };
}

export function mockNamespace(overrides: Partial<Namespace> = {}): Namespace {
  return { name: "default", status: "Active", ...overrides };
}

export function mockToolServer(
  overrides: Partial<ToolServerListEntry> = {},
): ToolServerListEntry {
  return {
    ref: "default/e2e-tool-server",
    groupKind: "RemoteMCPServer.kagent.dev",
    discoveredTools: [],
    ...overrides,
  };
}

export function mockTool(
  overrides: Partial<ToolsResponse> = {},
): ToolsResponse {
  return {
    id: "e2e-tool",
    server_name: "e2e-tool-server",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    deleted_at: "",
    description: "Seeded E2E tool",
    group_kind: "RemoteMCPServer.kagent.dev",
    ...overrides,
  };
}
