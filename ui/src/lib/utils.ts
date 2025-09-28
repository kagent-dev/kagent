import { clsx, type ClassValue } from "clsx";
import { twMerge } from "tailwind-merge";
import { Message as A2AMessage, Task as A2ATask, TaskStatusUpdateEvent as A2ATaskStatusUpdateEvent, TaskArtifactUpdateEvent as A2ATaskArtifactUpdateEvent } from "@a2a-js/sdk";

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}

export function getBackendUrl() {
  // The NEXT_PUBLIC_BACKEND_URL is set in the Helm chart to the Kubernetes service name
  if (process.env.NEXT_PUBLIC_BACKEND_URL) {
    return process.env.NEXT_PUBLIC_BACKEND_URL;
  }

  if (process.env.NODE_ENV === "production") {
    // This is more of a fallback; the NEXT_PUBLIC_BACKEND_URL should be set in the Helm chart
    return "http://kagent.kagent.svc.cluster.local/api";
  }

  // Fallback for local development
  return "http://localhost:8083/api";
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
  // Overall length check (max 253)
  if (name.length > 253) {
    return false;
  }
  // Must not start or end with '.' or '-'
  if (name.startsWith('.') || name.endsWith('.') || name.startsWith('-') || name.endsWith('-')) {
      return false;
  }
  // Check for invalid characters (only allows a-z, 0-9, -, .)
  if (!/^[a-z0-9.-]+$/.test(name)) {
      return false;
  }
  // Split into labels and check each label
  const labels = name.split('.');
  const singleLabelPattern = /^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/; // Original pattern for a single label

  for (const label of labels) {
    // Label length check (1-63)
    if (label.length === 0 || label.length > 63) {
      return false;
    }
    // Label format check (must match single label pattern)
     if (!singleLabelPattern.test(label)) {
         return false;
     }
  }
  return true; // Passed all checks
};

/**
 * Creates a valid RFC 1123 subdomain name from string parts.
 * Sanitizes each part, joins with hyphens, and cleans up the result.
 * Returns an empty string if no valid parts can be generated.
 * Note: This aims for label compliance (max 63 chars, stricter format).
 */
export const createRFC1123ValidName = (parts: string[]): string => {
  const sanitizePart = (str: string): string => {
    return str
      .toLowerCase()                // Ensure lowercase
      .replace(/[^a-z0-9-]+/g, '-') // Replace invalid chars with hyphen
      .replace(/-{2,}/g, '-')       // Collapse multiple hyphens
      .replace(/^-+|-+$/g, '');     // Remove leading/trailing hyphens from the part itself
  };

  const sanitizedParts = parts
    .map(sanitizePart)
    .filter(part => part.length > 0); // Remove empty parts after sanitization

  if (sanitizedParts.length === 0) {
    return ""; // No valid parts to join
  }

  let combined = sanitizedParts.join('-');
  // Final cleanup: remove leading/trailing hyphens from the combined string
  combined = combined.replace(/^-+|-+$/g, '');

  // Optional: Truncate if exceeding a typical label length limit (e.g., 63 chars)
  if (combined.length > 63) {
      combined = combined.substring(0, 63);
       // Re-trim hyphens potentially created by truncation
      combined = combined.replace(/-+$/g, '');
  }


  // Final validation check - though sanitization should make it valid
  if (!isResourceNameValid(combined)) {
      console.warn(`Generated name '${combined}' is still not RFC 1123 valid after sanitization.`);
      // Returning potentially invalid name, caller should ideally re-validate
      // Or return "" to indicate failure? Returning the attempt might be more informative.
      return combined;
  }

  return combined;
};

export const messageUtils = {
  isA2AMessage(content: unknown): content is A2AMessage {
    return typeof content === "object" && content !== null && "kind" in content && content.kind === "message";
  },

  isA2ATask(content: unknown): content is A2ATask {
    return typeof content === "object" && content !== null && "kind" in content && content.kind === "task";
  },

  isA2ATaskStatusUpdate(content: unknown): content is A2ATaskStatusUpdateEvent {
    return typeof content === "object" && content !== null && "kind" in content && content.kind === "status-update";
  },

  isA2ATaskArtifactUpdate(content: unknown): content is A2ATaskArtifactUpdateEvent {
    return typeof content === "object" && content !== null && "kind" in content && content.kind === "artifact-update";
  },
};

const NAMESPACE_SEPARATOR = "__NS__";
export function convertToUserFriendlyName(name: string): string {
  if (!name) return "Unknown Source";
  name = name.replace(NAMESPACE_SEPARATOR, "/");
  return name.replace(/_/g, "-");
}

export function isAgentToolName(name: string | undefined): boolean {
  return typeof name === "string" && name.includes(NAMESPACE_SEPARATOR);
}