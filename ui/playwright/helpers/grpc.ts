import { createClient } from "@connectrpc/connect";
import { createGrpcTransport } from "@connectrpc/connect-node";
import {
  AgentKind,
  AgentService,
} from "../../src/generated/kagent/api/v1alpha1/agents_pb";
import { ModelService } from "../../src/generated/kagent/api/v1alpha1/models_pb";
import { PromptTemplateService } from "../../src/generated/kagent/api/v1alpha1/prompts_pb";
import { ToolService } from "../../src/generated/kagent/api/v1alpha1/tools_pb";

const DEFAULT_GRPC_URL = "http://127.0.0.1:8084";
const transport = createGrpcTransport({
  baseUrl: process.env.BACKEND_GRPC_URL ?? DEFAULT_GRPC_URL,
  defaultTimeoutMs: 30_000,
});

const agentClient = createClient(AgentService, transport);
const modelClient = createClient(ModelService, transport);
const promptTemplateClient = createClient(PromptTemplateService, transport);
const toolClient = createClient(ToolService, transport);

export interface AgentInfo {
  namespace: string;
  name: string;
  kind: AgentKind;
  ready: boolean;
  accepted: boolean;
}

export interface ModelConfigInfo {
  ref: string;
  model: string;
  namespace: string;
  name: string;
}

function completeRef(ref: { namespace: string; name: string } | undefined) {
  return ref?.namespace && ref.name ? ref : null;
}

function splitRef(ref: string): { namespace: string; name: string } | null {
  const separator = ref.indexOf("/");
  if (separator <= 0 || separator === ref.length - 1) {
    return null;
  }
  return { namespace: ref.slice(0, separator), name: ref.slice(separator + 1) };
}

export async function listAgents(): Promise<AgentInfo[]> {
  const response = await agentClient.listAgents({ namespace: "" });
  return response.agents.flatMap((agent) => {
    const ref = completeRef(agent.ref);
    return ref === null
      ? []
      : [{
          namespace: ref.namespace,
          name: ref.name,
          kind: agent.kind,
          ready: agent.ready,
          accepted: agent.accepted,
        }];
  });
}

export async function listModelConfigs(): Promise<ModelConfigInfo[]> {
  const response = await modelClient.listModelConfigs({});
  return response.modelConfigs.flatMap((config) => {
    const ref = completeRef(config.ref);
    if (ref === null) {
      return [];
    }
    const resource = config.resource?.value;
    const spec = resource && typeof resource.spec === "object" && resource.spec !== null
      ? resource.spec as Record<string, unknown>
      : {};
    return [{
      ref: `${ref.namespace}/${ref.name}`,
      model: typeof spec.model === "string" ? spec.model : "",
      namespace: ref.namespace,
      name: ref.name,
    }];
  });
}

export async function listToolServerRefs(): Promise<string[]> {
  const response = await toolClient.listToolServers({});
  return response.toolServers.map((server) => server.ref).filter(Boolean);
}

export async function listPromptTemplateRefs(namespace: string): Promise<string[]> {
  const response = await promptTemplateClient.listPromptTemplates({ namespace });
  return response.promptTemplates.flatMap((template) => {
    const ref = completeRef(template.ref);
    return ref === null ? [] : [`${ref.namespace}/${ref.name}`];
  });
}

export async function deleteAgent(agent: Pick<AgentInfo, "namespace" | "name" | "kind">): Promise<void> {
  const ref = { namespace: agent.namespace, name: agent.name };
  switch (agent.kind) {
    case AgentKind.SANDBOX_AGENT:
      await agentClient.deleteSandboxAgent({ ref });
      return;
    case AgentKind.AGENT_HARNESS:
      await agentClient.deleteAgentHarness({ ref });
      return;
    default:
      throw new Error(`unsupported agent kind: ${agent.kind}`);
  }
}

export async function deleteModelConfig(ref: string): Promise<void> {
  const parsed = splitRef(ref);
  if (parsed !== null) {
    await modelClient.deleteModelConfig({ ref: parsed });
  }
}

export async function deleteToolServer(ref: string): Promise<void> {
  const parsed = splitRef(ref);
  if (parsed !== null) {
    await toolClient.deleteToolServer({ ref: parsed });
  }
}

export async function deletePromptTemplate(ref: string): Promise<void> {
  const parsed = splitRef(ref);
  if (parsed !== null) {
    await promptTemplateClient.deletePromptTemplate({ ref: parsed });
  }
}
