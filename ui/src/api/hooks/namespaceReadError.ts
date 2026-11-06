import { isPermissionDenied } from "../ApiError";

/** Preserve permission denial only when every failed read was denied. */
export function namespaceReadError(results: PromiseRejectedResult[]): Error {
  if (results.every(({ reason }) => isPermissionDenied(reason))) return results[0].reason;
  return new Error(results.map(({ reason }) => reason instanceof Error ? reason.message : String(reason)).join("; "));
}
