import { apiClient } from "../client";
import type { EgressPolicy } from "../domain/egressPolicies";
import type { SandboxTemplate } from "../domain/sandboxTemplates";
import { type ApiResource, useApiResource } from "./useApiResource";
import { useInvalidateKeys } from "./useInvalidateKeys";

/** Policies in one namespace, or in every namespace the caller may read. */
export function useEgressPolicies(namespace?: string): ApiResource<EgressPolicy[]> {
  return useApiResource(["egressPolicies.list", namespace ?? ""], () =>
    apiClient.egressPolicies.list(namespace),
  );
}

/** Policies in exactly one namespace; reads nothing until the namespace is known. */
export function useNamespaceEgressPolicies(namespace: string | undefined): ApiResource<EgressPolicy[]> {
  return useApiResource(namespace ? ["egressPolicies.list", namespace] : null, () =>
    apiClient.egressPolicies.list(namespace),
  );
}

export function useEgressPolicy(
  namespace: string | undefined,
  name: string | undefined,
): ApiResource<EgressPolicy> {
  return useApiResource(
    namespace && name ? ["egressPolicies.get", namespace, name] : null,
    () => apiClient.egressPolicies.get(namespace ?? "", name ?? ""),
  );
}

/** Sandbox templates in one namespace, or in every namespace the caller may read. */
export function useSandboxTemplates(namespace?: string): ApiResource<SandboxTemplate[]> {
  return useApiResource(["sandboxTemplates.list", namespace ?? ""], () =>
    apiClient.egressPolicies.sandboxTemplates(namespace),
  );
}

const KEYS = ["egressPolicies."];

/** Re-reads every egress policy list and detail on screen. */
export function useInvalidateEgressPolicies(): () => Promise<void> {
  return useInvalidateKeys(KEYS);
}
