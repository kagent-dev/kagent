"use server";

import {
  Agent,
  AgentResponse,
  AgentSpec,
  BaseResponse,
  DeclarativeAgentSpec,
  DeclarativeRuntime,
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
import { buildAgentHarnessCRDraft } from "@/lib/agentHarnessForm";
import { buildSandboxPlatformFromForm, buildSandboxSubstrateFromForm } from "@/lib/sandboxAgentForm";

function declarativeRuntimeFromForm(agentFormData: AgentFormData): DeclarativeRuntime {
  if (agentFormData.sandboxPlatform === "substrate") {
    return "go";
  }
  return agentFormData.declarativeRuntime === "go" ? "go" : "python";
}

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
        const agent = tool.agent;
        if (!agent) {
          throw new Error("Agent not found");
        }

        let name = agent.name;
        let namespace: string | undefined = agent.namespace;

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
      runtime: declarativeRuntimeFromForm(agentFormData),
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
      base.spec!.declarative!.memory = {
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
  const substrate = buildSandboxSubstrateFromForm(agentFormData);
  const platform = buildSandboxPlatformFromForm(agentFormData);
  const kind = agentFormData.type || "Declarative";

  if (kind === "BYO") {
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
        // BYO agents are not supported on Agent Substrate.
        platform: undefined,
        substrate: undefined,
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
    runtime: declarativeRuntimeFromForm(agentFormData),
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

  if (platform) {
    spec.platform = platform;
  }
  if (substrate) {
    spec.substrate = substrate;
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

function revalidateAgentListAndChat(namespace: string | undefined, name: string): void {
  const agentRef = k8sRefUtils.toRef(namespace || "", name);
  revalidatePath("/agents");
  revalidatePath(`/agents/${agentRef}/chat`);
}

/** Mutates `agentConfig` — strips namespace/name ref to name only for API payloads. */
function normalizeFormModelNameRef(agentConfig: AgentFormData): void {
  if (agentConfig.modelName && k8sRefUtils.isValidRef(agentConfig.modelName)) {
    agentConfig.modelName = k8sRefUtils.fromRef(agentConfig.modelName).name;
  }
}

async function createAgentHarnessFromForm(agentConfig: AgentFormData): Promise<BaseResponse<Agent>> {
  if (!agentConfig.agentHarness) {
    throw new Error("AgentHarness configuration is missing.");
  }
  const draft = buildAgentHarnessCRDraft({
    name: agentConfig.name,
    namespace: agentConfig.namespace || "",
    description: agentConfig.description || "",
    modelRef: agentConfig.modelName || "",
    harness: agentConfig.agentHarness,
  });
  if ("error" in draft) {
    throw new Error(draft.error);
  }

  const response = await fetchApi<BaseResponse<AgentResponse>>(`/agentharnesses`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(draft),
  });

  const agent = response.data?.agent;
  if (!agent) {
    throw new Error("Failed to create AgentHarness");
  }

  revalidateAgentListAndChat(agent.metadata.namespace, agent.metadata.name);
  return { message: response.message || "Successfully created AgentHarness", data: agent };
}

async function createOrUpdateSandboxAgentFromForm(
  agentConfig: AgentFormData,
  update: boolean,
): Promise<BaseResponse<Agent>> {
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

  revalidateAgentListAndChat(agent.metadata.namespace, agent.metadata.name);
  return { message: response.message || "Successfully created agent", data: agent };
}

async function createOrUpdateStandardAgentFromForm(
  agentConfig: AgentFormData,
  update: boolean,
): Promise<BaseResponse<Agent>> {
  const agentPayload = fromAgentFormDataToAgent(agentConfig);
  const response = await fetchApi<BaseResponse<Agent>>(`/agents`, {
    method: update ? "PUT" : "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(agentPayload),
  });

  if (!response?.data) {
    throw new Error("Failed to create agent");
  }

  revalidateAgentListAndChat(response.data.metadata.namespace, response.data.metadata.name);
  return { message: "Successfully created agent", data: response.data };
}

/**
 * Fetches one workload by Kubernetes kind so namespace/name is unambiguous across Agent / SandboxAgent / AgentHarness.
 */
export async function getAgent(
  agentName: string,
  namespace: string,
  kubernetesKind?: string
): Promise<BaseResponse<AgentResponse>> {
  try {
    let path = `/agents/${namespace}/${agentName}`;
    if (kubernetesKind === "SandboxAgent") {
      path = `/sandboxagents/${namespace}/${agentName}`;
    } else if (kubernetesKind === "AgentHarness") {
      path = `/agentharnesses/${namespace}/${agentName}`;
    }
    const agentData = await fetchApi<BaseResponse<AgentResponse>>(path);
    return { message: "Successfully fetched agent", data: agentData.data };
  } catch (error) {
    return createErrorResponse<AgentResponse>(error, "Error getting agent");
  }
}

/**
 * Lists agents then GETs using the row's `kind` (for chat links and edit when kind is not in the URL).
 */
export async function getAgentWithResolvedKind(
  agentName: string,
  namespace: string
): Promise<BaseResponse<AgentResponse>> {
  const list = await getAgents();
  if (list.error || !list.data) {
    return createErrorResponse<AgentResponse>(
      new Error(list.message || list.error || "Failed to fetch agents"),
      list.message || list.error || "Failed to fetch agents"
    );
  }
  const row = list.data.find(
    (a) =>
      a.agent.metadata?.name === agentName &&
      (a.agent.metadata?.namespace || "") === namespace
  );
  return getAgent(agentName, namespace, row?.agent.kind);
}

/**
 * Polls GET /api/sandboxagents/{namespace}/{name} until deploymentReady is true (Sandbox workload ready).
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
    const res = await getAgent(agentName, namespace, "SandboxAgent");
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
 * Deletes an agent workload. Uses kind-specific DELETE URLs when `kubernetesKind` is SandboxAgent or AgentHarness
 * so the same namespace/name cannot remove the wrong CR.
 */
export async function deleteAgent(
  agentName: string,
  namespace: string,
  kubernetesKind?: string
): Promise<BaseResponse<void>> {
  try {
    let path = `/agents/${namespace}/${agentName}`;
    if (kubernetesKind === "SandboxAgent") {
      path = `/sandboxagents/${namespace}/${agentName}`;
    } else if (kubernetesKind === "AgentHarness") {
      path = `/agentharnesses/${namespace}/${agentName}`;
    }
    await fetchApi(path, {
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
    if (agentConfig.type === "AgentHarness") {
      if (update) {
        throw new Error("Updating an AgentHarness from this form is not supported.");
      }
      return await createAgentHarnessFromForm(agentConfig);
    }

    normalizeFormModelNameRef(agentConfig);

    if (agentConfig.runInSandbox) {
      return await createOrUpdateSandboxAgentFromForm(agentConfig, update);
    }

    return await createOrUpdateStandardAgentFromForm(agentConfig, update);
  } catch (error) {
    return createErrorResponse<Agent>(error, "Error creating agent");
  }
}

/**
 * Gets all agents, optionally filtered by namespace.
 * @param opts.namespace When set, calls `/agents?namespace=<ns>`; otherwise calls `/agents`.
 * @returns A promise with the matching agents
 */
export async function getAgents(opts: { namespace?: string } = {}): Promise<BaseResponse<AgentResponse[]>> {
  try {
    const path = opts.namespace ? `/agents?namespace=${encodeURIComponent(opts.namespace)}` : `/agents`;
    const { data } = await fetchApi<BaseResponse<AgentResponse[]>>(path);

    const sortedData = (data ?? []).sort((a, b) => {
      const aRef = k8sRefUtils.toRef(a.agent.metadata.namespace || "", a.agent.metadata.name);
      const bRef = k8sRefUtils.toRef(b.agent.metadata.namespace || "", b.agent.metadata.name);
      return aRef.localeCompare(bRef);
    });

    return { message: "Successfully fetched agents", data: sortedData };
  } catch (error) {
    return createErrorResponse<AgentResponse[]>(error, "Error getting agents");
  }
}
