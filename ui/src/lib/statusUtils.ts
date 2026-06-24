import type { ChatStatus } from "@/types";
import { TaskState, taskStateFromJSON } from "@a2a-js/sdk";

export interface StatusInfo {
  text: string;
  placeholder: string;
}

// Map A2A TaskState (enum or raw string from API) to our ChatStatus.
export const mapA2AStateToStatus = (state: TaskState | string | undefined): ChatStatus => {
  const normalized = taskStateFromJSON(state);
  if (normalized === TaskState.TASK_STATE_SUBMITTED) {
    return "submitted";
  }
  if (normalized === TaskState.TASK_STATE_WORKING) {
    return "working";
  }
  if (normalized === TaskState.TASK_STATE_INPUT_REQUIRED) {
    return "input_required";
  }
  if (normalized === TaskState.TASK_STATE_COMPLETED) {
    return "ready";
  }
  if (
    normalized === TaskState.TASK_STATE_CANCELED ||
    normalized === TaskState.TASK_STATE_FAILED ||
    normalized === TaskState.TASK_STATE_REJECTED
  ) {
    return "error";
  }
  if (normalized === TaskState.TASK_STATE_AUTH_REQUIRED) {
    return "auth_required";
  }

  switch (normalized) {
    case TaskState.TASK_STATE_UNSPECIFIED:
    default:
      return "thinking";
  }
};

export const getStatusInfo = (status: ChatStatus): StatusInfo => {
  switch (status) {
    case "ready":
      return {
        text: "Ready",
        placeholder: "Send a message..."
      };
    case "thinking":
      return {
        text: "Thinking",
        placeholder: "Thinking..."
      };
    case "submitted":
      return {
        text: "Processing your request...",
        placeholder: "Processing your request..."
      };
    case "working":
      return {
        text: "Agent is thinking...",
        placeholder: "Agent is thinking..."
      };
    case "input_required":
      return {
        text: "Awaiting approval...",
        placeholder: "Awaiting approval..."
      };
    case "auth_required":
      return {
        text: "Authentication required...",
        placeholder: "Authentication required..."
      };
    case "processing_tools":
      return {
        text: "Executing tools...",
        placeholder: "Executing tools..."
      };
    case "generating_response":
      return {
        text: "Generating response...",
        placeholder: "Generating response..."
      };
    case "error":
      return {
        text: "An error occurred",
        placeholder: "An error occurred"
      };
    default:
      return {
        text: "Ready",
        placeholder: "Send a message..."
      };
  }
};

export const getStatusText = (status: ChatStatus): string => {
  return getStatusInfo(status).text;
};

export const getStatusPlaceholder = (status: ChatStatus): string => {
  return getStatusInfo(status).placeholder;
}; 