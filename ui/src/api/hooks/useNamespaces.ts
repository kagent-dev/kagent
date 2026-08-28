import { apiClient } from "../client";
import type { NamespaceResponse } from "../domain/namespaces";
import { type ApiResource, useApiResource } from "./useApiResource";

/**
 * Namespaces kagent can place resources in.
 *
 * The controller returns everything in the cluster when it has no watch filter,
 * and only the watched set when it has — so this is the list a namespace picker
 * should offer, not a list of every namespace that exists.
 */
export function useNamespaces(): ApiResource<NamespaceResponse[]> {
  return useApiResource(["namespaces.list"], () => apiClient.namespaces.list());
}
