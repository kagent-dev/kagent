"use server";

import {
  Agent,
  AgentResponse,
  BaseResponse,
} from "@/types";
import { revalidatePath } from "next/cache";
import { createErrorResponse } from "./utils";
import {
  getAgentGrpcGateway,
  type AgentKubernetesKind,
} from "@/lib/grpc/client";
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
}

/** Builds an AgentHarness payload with a name-only model reference for the controller API. */
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

  const gateway = await getAgentGrpcGateway();
  const response = await gateway.createAgentHarness(draft);
  const agent = response.agent;
  if (!agent) {
    throw new Error("Failed to create AgentHarness");
  }

  revalidateAgentListAndChat(agent.metadata.namespace, agent.metadata.name);
  return { message: "Successfully created AgentHarness", data: agent };
}

async function createOrUpdateSandboxAgentFromForm(
  agentConfig: AgentWorkloadFormData,
  update: boolean,
): Promise<BaseResponse<Agent>> {
  const sandboxPayload = agentFormDataToSandboxAgent(agentConfig);
  const gateway = await getAgentGrpcGateway();
  const response = update
    ? await gateway.updateSandboxAgent(sandboxPayload)
    : await gateway.createSandboxAgent(sandboxPayload);
  const agent = response.agent;
  if (!agent) {
    throw new Error("Failed to create sandbox agent");
  }

  revalidateAgentListAndChat(agent.metadata.namespace, agent.metadata.name);
  return { message: update ? "Successfully updated agent" : "Successfully created agent", data: agent };
}

async function createOrUpdateStandardAgentFromForm(
  agentConfig: AgentWorkloadFormData,
  update: boolean,
): Promise<BaseResponse<Agent>> {
  const agentPayload = agentFormDataToAgent(agentConfig);
  const gateway = await getAgentGrpcGateway();
  const response = update
    ? await gateway.updateAgent(agentPayload)
    : await gateway.createAgent(agentPayload);
  if (!response.agent) {
    throw new Error("Failed to create agent");
  }

  revalidateAgentListAndChat(response.agent.metadata.namespace, response.agent.metadata.name);
  return {
    message: update ? "Successfully updated agent" : "Successfully created agent",
    data: response.agent,
  };
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
    const gateway = await getAgentGrpcGateway();
    const agent = await gateway.getAgent(namespace, agentName, agentKind(kubernetesKind));
    return { message: "Successfully fetched agent", data: agent };
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
 * Polls the SandboxAgent RPC until deploymentReady is true (Sandbox workload ready).
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
    const gateway = await getAgentGrpcGateway();
    await gateway.deleteAgent(namespace, agentName, agentKind(kubernetesKind));

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
    const gateway = await getAgentGrpcGateway();
    const data = await gateway.listAgents(opts.namespace);

    const sortedData = data.sort((a, b) => {
      const aRef = k8sRefUtils.toRef(a.agent.metadata.namespace || "", a.agent.metadata.name);
      const bRef = k8sRefUtils.toRef(b.agent.metadata.namespace || "", b.agent.metadata.name);
      return aRef.localeCompare(bRef);
    });

    return { message: "Successfully fetched agents", data: sortedData };
  } catch (error) {
    return createErrorResponse<AgentResponse[]>(error, "Error getting agents");
  }
}

function agentKind(kind: string | undefined): AgentKubernetesKind {
  if (kind === "SandboxAgent" || kind === "AgentHarness") {
    return kind;
  }
  return "Agent";
}
