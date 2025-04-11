import { LLMCall } from "@/components/chat/LLMCallModal";
import { FunctionCall, FunctionExecutionResult, ImageContent, TeamResult } from "@/types/datamodel";
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import type { TextMessageConfig } from "@/types/datamodel";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function getWsUrl() {
  if (process.env.NEXT_PUBLIC_WS_URL) {
    return process.env.NEXT_PUBLIC_WS_URL;
  }

  let url = "";
  if (process.env.NODE_ENV === "production") {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    url = `${protocol}//${window.location.host}/api/ws`;
  } else {
    url = "ws://localhost:8081/api/ws";
  }
  return url;
}

export function getBackendUrl() {
  let url = "";

  if (process.env.NODE_ENV === "production") {
    url = process.env.NEXT_PUBLIC_BACKEND_URL || "http://0.0.0.0/api";
  } else {
    url = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8083/api";
  }
  return url;
}

export function getWebSocketUrl() {
  const backendUrl = getBackendUrl();
  const wsProtocol = backendUrl.startsWith("https") ? "wss" : "ws";

  return backendUrl.replace(/^https?/, wsProtocol);
}

export function getRelativeTimeString(date: string | number | Date): string {
  const now = new Date();
  const past = new Date(date);
  const diffInMs = now.getTime() - past.getTime();

  const diffInSeconds = Math.floor(diffInMs / 1000);
  const diffInMinutes = Math.floor(diffInSeconds / 60);
  const diffInHours = Math.floor(diffInMinutes / 60);
  const diffInDays = Math.floor(diffInHours / 24);
  const diffInMonths = Math.floor(diffInDays / 30);
  const diffInYears = Math.floor(diffInDays / 365);

  if (diffInSeconds < 60) {
    return "just now";
  } else if (diffInMinutes < 60) {
    return `${diffInMinutes} ${diffInMinutes === 1 ? "minute" : "minutes"} ago`;
  } else if (diffInHours < 24) {
    return `${diffInHours} ${diffInHours === 1 ? "hour" : "hours"} ago`;
  } else if (diffInDays < 30) {
    return `${diffInDays} ${diffInDays === 1 ? "day" : "days"} ago`;
  } else if (diffInMonths < 12) {
    return `${diffInMonths} ${diffInMonths === 1 ? "month" : "months"} ago`;
  } else {
    return `${diffInYears} ${diffInYears === 1 ? "year" : "years"} ago`;
  }
}

// All resource names must be valid RFC 1123 subdomains
export const isResourceNameValid = (name: string): boolean => {
  // RFC 1123 subdomain regex pattern
  const rfc1123Pattern = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$/;
  return rfc1123Pattern.test(name);
};

export const messageUtils = {
  isToolCallContent(content: unknown): content is FunctionCall[] {
    if (!Array.isArray(content)) return false;
    return content.every((item) => typeof item === "object" && item !== null && "id" in item && "arguments" in item && "name" in item);
  },

  isMultiModalContent(content: unknown): content is (string | ImageContent)[] {
    if (!Array.isArray(content)) return false;
    return content.every((item) => typeof item === "string" || (typeof item === "object" && item !== null && ("url" in item || "data" in item)));
  },

  isFunctionExecutionResult(content: unknown): content is FunctionExecutionResult[] {
    if (!Array.isArray(content)) return false;
    return content.every((item) => typeof item === "object" && item !== null && "call_id" in item && "content" in item);
  },

  isTextMessageContent(content: unknown): content is TextMessageConfig {
    return typeof content === "object" && content !== null && "content" in content && "type" in content && content.type === "TextMessage";
  },

  isUserTextMessageContent(content: unknown): content is TextMessageConfig {
    return messageUtils.isTextMessageContent(content) && content.source === "user";
  },

  isLlmCallEvent(content: unknown): content is LLMCall {
    try {
      const parsed = JSON.parse(String(content));
      return typeof parsed === "object" && parsed !== null && "type" in parsed && parsed.type === "LLMCall";
    } catch {
      return false;
    }
  },

  isTeamResult(content: unknown): content is TeamResult {
    return typeof content === "object" && content !== null && "task_result" in content && "duration" in content && "usage" in content;
  },

  isUser(source: string): boolean {
    return source === "user";
  },
};
