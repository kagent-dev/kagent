import type { BaseResponse } from "@/types";

const TRUTHY_VALUES = new Set(["1", "on", "true", "yes"]);

export function isReadOnlyModeEnabled(): boolean {
  const value = process.env.NEXT_PUBLIC_READONLY_MODE?.trim().toLowerCase();
  return value !== undefined && TRUTHY_VALUES.has(value);
}

export function getReadOnlyModeMessage(scope: string): string {
  return `${scope} are disabled because this UI is running in read-only mode.`;
}

export function createReadOnlyModeResponse<T>(scope: string): BaseResponse<T> {
  const error = getReadOnlyModeMessage(scope);
  return {
    message: error,
    error,
  };
}
