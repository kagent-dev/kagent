import { ChatStatus } from "@/components/chat/ChatInterface";
import { TaskState } from "@a2a-js/sdk";

export interface StatusInfo {
  text: string;
  placeholder: string;
}

// Map A2A TaskState to our ChatStatus for UI purposes
export const mapA2AStateToStatus = (state: TaskState): ChatStatus => {
  switch (state) {
    case "submitted":
      return "submitted";
    case "working":
      return "working";
    case "input-required":
      return "input_required";
    case "completed":
      return "ready";
    case "canceled":
    case "failed": 
    case "rejected":
      return "error";
    case "auth-required":
      return "auth_required";
    case "unknown":
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
        text: "Input required...",
        placeholder: "Input required..."
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