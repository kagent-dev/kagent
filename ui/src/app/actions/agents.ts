"use server";

import {
  Agent,
  AgentResponse,
  BaseResponse,
} from "@/types";
import { revalidatePath } from "next/cache";
import { fetchApi, createErrorResponse } from "./utils";
import type {
  AgentFormData,
  AgentWorkloadFormData,
} from "@/lib/agentFormDomain";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { buildAgentHarnessCRDraft } from "@/lib/agentHarnessForm";
import {
  agentFormDataToAgent,
  agentFormDataToSandboxAgent,
} from "@/lib/agentFormDomain";

function isAgentWorkloadFormData(
  agentConfig: AgentFormData,
): agentConfig is AgentWorkloadFormData {
  return agentConfig.type !== "AgentHarness";
}

function revalidateAgentListAndChat(namespace: string | undefined, name: string): void {
  const agentRef = k8sRefUtils.toRef(namespace || "", name);
  revalidatePath("/agents");
  revalidatePath(`/agents/${agentRef}/chat`);
  revalidatePath(`/sandbox-agents/${agentRef}/chat`);
}

/** Mutates `agentConfig` — strips namespace/name ref to name only for API payloads. */
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
  agentConfig: AgentWorkloadFormData,
  update: boolean,
): Promise<BaseResponse<Agent>> {
  const sandboxPayload = agentFormDataToSandboxAgent(agentConfig);
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
  agentConfig: AgentWorkloadFormData,
  update: boolean,
): Promise<BaseResponse<Agent>> {
  const agentPayload = agentFormDataToAgent(agentConfig);
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
 * Resolves an agent's kind and GETs it. A caller-supplied kind (derived from
 * the chat route prefix, see chatPathKind) wins; otherwise the kind is looked
 * up from the merged list, which resolves a namespace/name shared across kinds
 * to the Agent.
 */
export async function getAgentWithResolvedKind(
  agentName: string,
  namespace: string,
  kubernetesKind?: string
): Promise<BaseResponse<AgentResponse>> {
  if (kubernetesKind) {
    return getAgent(agentName, namespace, kubernetesKind);
  }
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
    if (!isAgentWorkloadFormData(agentConfig)) {
      if (update) {
        throw new Error("Updating an AgentHarness from this form is not supported.");
      }
      return await createAgentHarnessFromForm(agentConfig);
    }

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
