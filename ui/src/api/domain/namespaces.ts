/**
 * Namespaces the controller is watching.
 *
 * Mirrors `httpapi.NamespaceResponse`. When the controller has no watch filter
 * configured it lists every namespace in the cluster; when it does, it lists only
 * those — so this is "namespaces kagent can place things in", not "namespaces
 * that exist".
 */
export interface NamespaceResponse {
  name: string;
  /** Kubernetes namespace phase, e.g. `Active` or `Terminating`. */
  status: string;
}
