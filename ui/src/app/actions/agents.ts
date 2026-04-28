"use server";

import {
  Agent,
  AgentResponse,
  AgentSpec,
  BaseResponse,
  DeclarativeAgentSpec,
  PromptSource,
  SandboxAgent,
  SkillForAgent,
  Tool,
} from "@/types";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import { AgentFormData } from "@/components/AgentsProvider";
import { isMcpTool } from "@/lib/toolUtils";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { formRowsToGitRepos, type GitSkillFormRow } from "@/lib/agentSkillsForm";

function attachPromptTemplateToDeclarative(decl: DeclarativeAgentSpec, agentFormData: AgentFormData) {
  if (!agentFormData.promptSources?.some((s) => s.name.trim())) {
    return;
  }
  const dataSources: PromptSource[] = agentFormData.promptSources
    .filter((s) => s.name.trim())
    .map((s) => {
      const src: PromptSource = {
        kind: "ConfigMap",
        name: s.name.trim(),
        apiGroup: "",
      };
      const al = s.alias.trim();
      if (al) {
        src.alias = al;
      }
      return src;
    });
  if (dataSources.length > 0) {
    decl.promptTemplate = { dataSources };
  }
}

function buildSkillsForAgentSpec(agentFormData: AgentFormData): SkillForAgent | undefined {
  const refs = (agentFormData.skillRefs || []).map((r) => r.trim()).filter(Boolean);
  const rows: GitSkillFormRow[] = (agentFormData.skillGitRepos || []).map((g) => ({
    url: g.url ?? "",
    ref: g.ref ?? "",
    path: g.path ?? "",
    name: g.name ?? "",
  }));
  const gitRefs = formRowsToGitRepos(rows);

  if (refs.length === 0 && gitRefs.length === 0) {
    return undefined;
  }

  const skills: SkillForAgent = {};
  if (refs.length > 0) {
    skills.refs = refs;
  }
  if (gitRefs.length > 0) {
    skills.gitRefs = gitRefs;
    const secretName = agentFormData.skillsGitAuthSecretName?.trim();
    if (secretName) {
      skills.gitAuthSecretRef = { name: secretName };
    }
  }
  return skills;
}

/**
 * Converts AgentFormData to Agent format
 * @param agentFormData The form data to convert
 * @returns An Agent object
 */
function fromAgentFormDataToAgent(agentFormData: AgentFormData): Agent {
  const modelConfigName = agentFormData.modelName?.includes("/")
    ? agentFormData.modelName.split("/").pop() || ""
    : agentFormData.modelName;

  const type = agentFormData.type || "Declarative";
  const agentNamespace = agentFormData.namespace || "";

  const convertTools = (tools: Tool[]) =>
    tools.map((tool) => {
      if (isMcpTool(tool)) {
        const mcpServer = tool.mcpServer;
        if (!mcpServer) {
          throw new Error("MCP server not found");
        }
        
        let name = mcpServer.name;
        let namespace: string | undefined = mcpServer.namespace;
        
        if (k8sRefUtils.isValidRef(mcpServer.name)) {
          const parsed = k8sRefUtils.fromRef(mcpServer.name);
          name = parsed.name;
          // Ignore namespace on the name ref if one is set - using namespace/name format is legacy behavior
        }
        
        // If no namespace is set, default to the agent's namespace
        if (!namespace) {
          namespace = agentNamespace;
        }

        const requireApproval =
          mcpServer.requireApproval && mcpServer.requireApproval.length > 0
            ? mcpServer.requireApproval
            : undefined;

        return {
          type: "McpServer",
          mcpServer: {
            name,
            namespace,
            kind: mcpServer.kind,
            apiGroup: mcpServer.apiGroup,
            toolNames: mcpServer.toolNames,
            ...(requireApproval ? { requireApproval } : {}),
          },
        } as Tool;
      }

      if (tool.type === "Agent") {
        const agent = tool.agent;
        if (!agent) {
          throw new Error("Agent not found");
        }

        let name = agent.name;
        let namespace: string | undefined = agent.namespace;
        
        if (k8sRefUtils.isValidRef(name)) {
          const parsed = k8sRefUtils.fromRef(name);
          name = parsed.name;
          // Ignore namespace on the name ref if one is set - using namespace/name format is legacy behavior
        }
        
        // If no namespace is set, default to the agent's namespace
        if (!namespace) {
          namespace = agentNamespace;
        }
        
        return {
          type: "Agent",
          agent: {
            name,
            namespace,
            kind: agent.kind || "Agent",
            apiGroup: agent.apiGroup || "kagent.dev",
          },
        } as Tool;
      }

      console.warn("Unknown tool type:", tool);
      return tool as Tool;
    });

  const base: Partial<Agent> = {
    metadata: {
      name: agentFormData.name,
      namespace: agentFormData.namespace || "",
    },
    spec: {
      type,
      description: agentFormData.description,
    } as AgentSpec,
  };

  if (type === "Declarative") {
    base.spec!.declarative = {
      systemMessage: agentFormData.systemPrompt || "",
      modelConfig: modelConfigName || "",
      stream: agentFormData.stream ?? true,
      tools: convertTools(agentFormData.tools || []),
    };

    const skills = buildSkillsForAgentSpec(agentFormData);
    if (skills) {
      base.spec!.skills = skills;
    }

    if (agentFormData.memory?.modelConfig) {
      const memoryModel = agentFormData.memory.modelConfig;
      const memoryModelName = k8sRefUtils.isValidRef(memoryModel)
        ? k8sRefUtils.fromRef(memoryModel).name
        : memoryModel;
      base.spec!.memory = {
        modelConfig: memoryModelName,
        ttlDays: agentFormData.memory.ttlDays,
      };
    }

    if (agentFormData.context) {
      base.spec!.declarative!.context = agentFormData.context;
    }

    const trimmedSA = agentFormData.serviceAccountName?.trim();
    if (trimmedSA) {
      base.spec!.declarative!.deployment = {
        ...base.spec!.declarative!.deployment,
        serviceAccountName: trimmedSA,
      };
    }

    attachPromptTemplateToDeclarative(base.spec!.declarative!, agentFormData);
  } else if (type === "BYO") {
    base.spec!.byo = {
      deployment: {
        image: agentFormData.byoImage || "",
        cmd: agentFormData.byoCmd,
        args: agentFormData.byoArgs,
        replicas: agentFormData.replicas,
        imagePullSecrets: agentFormData.imagePullSecrets,
        volumes: agentFormData.volumes,
        volumeMounts: agentFormData.volumeMounts,
        labels: agentFormData.labels,
        annotations: agentFormData.annotations,
        env: agentFormData.env,
        imagePullPolicy: agentFormData.imagePullPolicy,
        serviceAccountName: agentFormData.serviceAccountName,
      },
    };
  }

  return base as Agent;
}

function fromAgentFormDataToSandboxAgent(agentFormData: AgentFormData): SandboxAgent {
  if (agentFormData.byoImage?.trim()) {
    return {
      apiVersion: "kagent.dev/v1alpha2",
      kind: "SandboxAgent",
      metadata: {
        name: agentFormData.name,
        namespace: agentFormData.namespace || "",
      },
      spec: {
        type: "BYO",
        description: agentFormData.description,
        byo: {
          deployment: {
            image: agentFormData.byoImage || "",
            cmd: agentFormData.byoCmd,
            args: agentFormData.byoArgs,
            replicas: agentFormData.replicas,
            imagePullSecrets: agentFormData.imagePullSecrets,
            volumes: agentFormData.volumes,
            volumeMounts: agentFormData.volumeMounts,
            labels: agentFormData.labels,
            annotations: agentFormData.annotations,
            env: agentFormData.env,
            imagePullPolicy: agentFormData.imagePullPolicy,
            serviceAccountName: agentFormData.serviceAccountName,
          },
        },
      },
    };
  }

  const modelConfigName = agentFormData.modelName?.includes("/")
    ? agentFormData.modelName.split("/").pop() || ""
    : agentFormData.modelName;

  const agentNamespace = agentFormData.namespace || "";

  const convertTools = (tools: Tool[]) =>
    tools.map((tool) => {
      if (isMcpTool(tool)) {
        const mcpServer = tool.mcpServer;
        if (!mcpServer) {
          throw new Error("MCP server not found");
        }

        let name = mcpServer.name;
        let namespace: string | undefined = mcpServer.namespace;

        if (k8sRefUtils.isValidRef(mcpServer.name)) {
          const parsed = k8sRefUtils.fromRef(mcpServer.name);
          name = parsed.name;
        }

        if (!namespace) {
          namespace = agentNamespace;
        }

        const requireApproval =
          mcpServer.requireApproval && mcpServer.requireApproval.length > 0
            ? mcpServer.requireApproval
            : undefined;

        return {
          type: "McpServer",
          mcpServer: {
            name,
            namespace,
            kind: mcpServer.kind,
            apiGroup: mcpServer.apiGroup,
            toolNames: mcpServer.toolNames,
            ...(requireApproval ? { requireApproval } : {}),
          },
        } as Tool;
      }

      if (tool.type === "Agent") {
        const ag = tool.agent;
        if (!ag) {
          throw new Error("Agent not found");
        }

        let name = ag.name;
        let namespace: string | undefined = ag.namespace;

        if (k8sRefUtils.isValidRef(name)) {
          const parsed = k8sRefUtils.fromRef(name);
          name = parsed.name;
        }

        if (!namespace) {
          namespace = agentNamespace;
        }

        return {
          type: "Agent",
          agent: {
            name,
            namespace,
            kind: ag.kind || "Agent",
            apiGroup: ag.apiGroup || "kagent.dev",
          },
        } as Tool;
      }

      console.warn("Unknown tool type:", tool);
      return tool as Tool;
    });

  const decl: DeclarativeAgentSpec = {
    systemMessage: agentFormData.systemPrompt || "",
    modelConfig: modelConfigName || "",
    stream: agentFormData.stream ?? true,
    tools: convertTools(agentFormData.tools || []),
  };

  if (agentFormData.memory?.modelConfig) {
    const memoryModel = agentFormData.memory.modelConfig;
    const memoryModelName = k8sRefUtils.isValidRef(memoryModel)
      ? k8sRefUtils.fromRef(memoryModel).name
      : memoryModel;
    decl.memory = {
      modelConfig: memoryModelName,
      ttlDays: agentFormData.memory.ttlDays,
    };
  }

  if (agentFormData.context) {
    decl.context = agentFormData.context;
  }

  const trimmedSA = agentFormData.serviceAccountName?.trim();
  if (trimmedSA) {
    decl.deployment = {
      ...decl.deployment,
      serviceAccountName: trimmedSA,
    };
  }

  attachPromptTemplateToDeclarative(decl, agentFormData);

  const spec: AgentSpec = {
    type: "Declarative",
    declarative: decl,
    description: agentFormData.description,
  };

  const skills = buildSkillsForAgentSpec(agentFormData);
  if (skills) {
    spec.skills = skills;
  }

  return {
    apiVersion: "kagent.dev/v1alpha2",
    kind: "SandboxAgent",
    metadata: {
      name: agentFormData.name,
      namespace: agentFormData.namespace || "",
    },
    spec,
  };
}

export async function getAgent(agentName: string, namespace: string): Promise<BaseResponse<AgentResponse>> {
  try {
    const agentData = await fetchApi<BaseResponse<AgentResponse>>(`/agents/${namespace}/${agentName}`);
    return { message: "Successfully fetched agent", data: agentData.data };
  } catch (error) {
    return createErrorResponse<AgentResponse>(error, "Error getting agent");
  }
}

/**
 * Polls GET /api/agents/{namespace}/{name} until deploymentReady is true (Sandbox: workload ready; same Ready condition as reconciler).
 */
export async function waitForSandboxAgentReady(
  agentName: string,
  namespace: string,
  opts?: { timeoutMs?: number; intervalMs?: number }
): Promise<{ ok: boolean; error?: string }> {
  const timeoutMs = opts?.timeoutMs ?? 120_000;
  const intervalMs = opts?.intervalMs ?? 1500;
  const deadline = Date.now() + timeoutMs;

  while (Date.now() < deadline) {
    const res = await getAgent(agentName, namespace);
    if (!res.data) {
      return { ok: false, error: res.message || "Agent not found" };
    }
    if (res.data.deploymentReady === true) {
      return { ok: true };
    }
    await new Promise((r) => setTimeout(r, intervalMs));
  }
  return {
    ok: false,
    error: "Timed out waiting for sandbox agent to become ready",
  };
}

/**
 * Deletes a agent
 * @param agentName The agent name
 * @param namespace The agent namespace
 * @returns A promise with the delete result
 */
export async function deleteAgent(agentName: string, namespace: string): Promise<BaseResponse<void>> {
  try {
    await fetchApi(`/agents/${namespace}/${agentName}`, {
      method: "DELETE",
      headers: {
        "Content-Type": "application/json",
      },
    });

    revalidatePath("/");
    return { message: "Successfully deleted agent" };
  } catch (error) {
    return createErrorResponse<void>(error, "Error deleting agent");
  }
}

/**
 * Creates or updates an agent
 * @param agentConfig The agent configuration
 * @param update Whether to update an existing agent
 * @returns A promise with the created/updated agent
 */
export async function createAgent(agentConfig: AgentFormData, update: boolean = false): Promise<BaseResponse<Agent>> {
  try {
    // Only get the name of the model, not the full ref
    if (agentConfig.modelName) {
      if (k8sRefUtils.isValidRef(agentConfig.modelName)) {
        agentConfig.modelName = k8sRefUtils.fromRef(agentConfig.modelName).name;
      }
    }

    if (agentConfig.type === "Sandbox") {
      const sandboxPayload = fromAgentFormDataToSandboxAgent(agentConfig);
      const ns = sandboxPayload.metadata.namespace || "";
      const name = sandboxPayload.metadata.name;
      const path = update ? `/sandboxagents/${ns}/${name}` : `/sandboxagents`;
      const response = await fetchApi<BaseResponse<AgentResponse>>(path, {
        method: update ? "PUT" : "POST",
        headers: {
          "Content-Type": "application/json",
        },
        body: JSON.stringify(sandboxPayload),
      });

      const agent = response.data?.agent;
      if (!agent) {
        throw new Error("Failed to create sandbox agent");
      }

      const agentRef = k8sRefUtils.toRef(agent.metadata.namespace || "", agent.metadata.name);

      revalidatePath("/agents");
      revalidatePath(`/agents/${agentRef}/chat`);
      return { message: response.message || "Successfully created agent", data: agent };
    }

    const agentPayload = fromAgentFormDataToAgent(agentConfig);
    const response = await fetchApi<BaseResponse<Agent>>(`/agents`, {
      method: update ? "PUT" : "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(agentPayload),
    });

    if (!response) {
      throw new Error("Failed to create agent");
    }

    const agentRef = k8sRefUtils.toRef(
      response.data!.metadata.namespace || "",
      response.data!.metadata.name,
    )

    revalidatePath("/agents");
    revalidatePath(`/agents/${agentRef}/chat`);
    return { message: "Successfully created agent", data: response.data };
  } catch (error) {
    return createErrorResponse<Agent>(error, "Error creating agent");
  }
}

/**
 * Gets all agents
 * @returns A promise with all agents
 */
export async function getAgents(): Promise<BaseResponse<AgentResponse[]>> {
  try {
    const { data } = await fetchApi<BaseResponse<AgentResponse[]>>(`/agents`);

    const sortedData = data?.sort((a, b) => {
      const aRef = k8sRefUtils.toRef(a.agent.metadata.namespace || "", a.agent.metadata.name);
      const bRef = k8sRefUtils.toRef(b.agent.metadata.namespace || "", b.agent.metadata.name);
      return aRef.localeCompare(bRef);
    });

    return { message: "Successfully fetched agents", data: sortedData };
  } catch (error) {
    return createErrorResponse<AgentResponse[]>(error, "Error getting agents");
  }
}
