import { A2AService, TaskState, type TaskPushNotificationConfig } from "@/generated/a2a_pb";
import { fromConnectError } from "../ApiError";
import { agentInstanceShareToken } from "../shareToken";
import { serviceClient } from "../transport";
import type { ChatConversationRef } from "./types";

export interface TaskPushConfig {
  id: string;
  url: string;
  token?: string;
  bearerCredential?: string;
}

function callOptions(conversation: ChatConversationRef, signal?: AbortSignal) {
  const headers: Record<string, string> = {};
  const share = agentInstanceShareToken(conversation.id);
  if (share) headers["X-Share-Token"] = share;
  return { headers, signal };
}

function fromWire(config: TaskPushNotificationConfig): TaskPushConfig {
  return { id: config.id, url: config.url };
}

export async function taskAcceptsPushConfigs(conversation: ChatConversationRef, taskId: string): Promise<boolean> {
  try {
    const task = await serviceClient(A2AService).getTask(
      { tenant: conversation.agent, id: taskId, historyLength: 0 },
      callOptions(conversation),
    );
    return ![
      TaskState.COMPLETED, TaskState.FAILED, TaskState.CANCELED, TaskState.REJECTED,
    ].includes(task.status?.state ?? TaskState.UNSPECIFIED);
  } catch (error) {
    throw fromConnectError(error, "A2AService/GetTask");
  }
}

export async function listTaskPushConfigs(
  conversation: ChatConversationRef,
  taskId: string,
  signal?: AbortSignal,
): Promise<TaskPushConfig[]> {
  const configs: TaskPushConfig[] = [];
  let pageToken = "";
  const seenTokens = new Set<string>();
  try {
    do {
      const page = await serviceClient(A2AService).listTaskPushNotificationConfigs(
        { tenant: conversation.agent, taskId, pageSize: 100, pageToken },
        callOptions(conversation, signal),
      );
      configs.push(...page.configs.map(fromWire));
      pageToken = page.nextPageToken;
      if (pageToken && seenTokens.has(pageToken)) {
        throw new Error("The server repeated a push configuration page token.");
      }
      if (pageToken) seenTokens.add(pageToken);
    } while (pageToken);
    return configs;
  } catch (error) {
    throw fromConnectError(error, "A2AService/ListTaskPushNotificationConfigs");
  }
}

export async function saveTaskPushConfig(
  conversation: ChatConversationRef,
  taskId: string,
  config: TaskPushConfig,
): Promise<TaskPushConfig> {
  try {
    const saved = await serviceClient(A2AService).createTaskPushNotificationConfig(
      { tenant: conversation.agent, taskId, id: config.id, url: config.url, token: config.token ?? "", authentication: { scheme: "Bearer", credentials: config.bearerCredential ?? "" } },
      callOptions(conversation),
    );
    return fromWire(saved);
  } catch (error) {
    throw fromConnectError(error, "A2AService/CreateTaskPushNotificationConfig");
  }
}

export async function deleteTaskPushConfig(
  conversation: ChatConversationRef,
  taskId: string,
  id: string,
): Promise<void> {
  try {
    await serviceClient(A2AService).deleteTaskPushNotificationConfig(
      { tenant: conversation.agent, taskId, id },
      callOptions(conversation),
    );
  } catch (error) {
    throw fromConnectError(error, "A2AService/DeleteTaskPushNotificationConfig");
  }
}
