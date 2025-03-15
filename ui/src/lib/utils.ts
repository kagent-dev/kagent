import { LLMCall } from "@/components/chat/LLMCallModal";
import { FunctionCall, FunctionExecutionResult, ImageContent, TeamResult } from "@/types/datamodel";
import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import type { TextMessageConfig } from "@/types/datamodel";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function getWsUrl() {
  let url = "";
  if (process.env.NODE_ENV === "production") {
    url = process.env.NEXT_PUBLIC_BACKEND_URL || `ws://${window.location.host}/api/ws`;
  } else {
    url = process.env.NEXT_PUBLIC_BACKEND_URL || "ws://localhost:8081/api/ws";
  }
  return url;

}
export function getBackendUrl() {
  let url = "";

  if (process.env.NODE_ENV === "production") {
    url = process.env.NEXT_PUBLIC_BACKEND_URL || "http://0.0.0.0/api";
  } else {
    url = process.env.NEXT_PUBLIC_BACKEND_URL || "http://localhost:8081/api";
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

// Python keywords (as of Python 3.12)
const PYTHON_KEYWORDS = new Set([
  "False",
  "None",
  "True",
  "and",
  "as",
  "assert",
  "async",
  "await",
  "break",
  "class",
  "continue",
  "def",
  "del",
  "elif",
  "else",
  "except",
  "finally",
  "for",
  "from",
  "global",
  "if",
  "import",
  "in",
  "is",
  "lambda",
  "nonlocal",
  "not",
  "or",
  "pass",
  "raise",
  "return",
  "try",
  "while",
  "with",
  "yield",
  "case",
  "match",
  "type",
]);

/**
 * Checks whether a string is a valid Python identifier.
 * This is a TypeScript implementation of Python's str.isidentifier()
 *
 * @param str - The string to check
 * @returns True if the string is a valid Python identifier, false otherwise
 */
export function isIdentifier(str: string): boolean {
  // Handle empty string
  if (!str) {
    return false;
  }

  // Use the XID_Start and XID_Continue Unicode properties to match Python's behavior
  // Note: In Python, the first character must be in XID_Start and the rest in XID_Continue

  // Check first character (must be XID_Start)
  const firstChar = str[0];
  if (!/^\p{XID_Start}$/u.test(firstChar)) {
    return false;
  }

  // Check remaining characters (must be XID_Continue)
  const restOfString = str.slice(1);
  if (restOfString.length > 0 && !/^[\p{XID_Continue}]*$/u.test(restOfString)) {
    return false;
  }

  // Check if it's not a Python keyword
  if (PYTHON_KEYWORDS.has(str)) {
    return false;
  }
  return true;
}

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
