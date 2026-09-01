/**
 * Shapes shared by every kagent resource.
 *
 * Every Kubernetes-backed resource carries the standard object metadata, and the
 * controller now hands those resources over inside a `StructuredObject` — the
 * object as JSON, unchanged — so these types describe both what arrives and what
 * a write sends.
 */

/**
 * The envelope the REST API used to wrap every payload in.
 *
 * Nothing on the live path produces one any more: the application API is gRPC and
 * a response is a proto message. It survives for the in-browser fixture backend,
 * which still speaks the older shape, and it should go when that does.
 */
export interface BaseResponse<T> {
  message: string;
  data?: T;
  error?: string;
}

/** The subset of Kubernetes `ObjectMeta` the UI reads and writes. */
export interface ResourceMetadata {
  name: string;
  namespace?: string;
  /** RFC3339, from `metadata.creationTimestamp`. */
  creationTimestamp?: string;
  resourceVersion?: string;
  labels?: Record<string, string>;
  /**
   * Where an extension form field folds its value on the way to the controller.
   *
   * Spelled out rather than left to a catch-all index signature: these are the
   * only extra keys `ObjectMeta` accepts, so an open record would type a typo as
   * valid and misrepresent what the API takes.
   */
  annotations?: Record<string, string>;
}

/** A reference to another Kubernetes object, optionally cross-kind. */
export interface TypedLocalReference {
  kind?: string;
  apiGroup?: string;
  name: string;
  namespace?: string;
}

export interface SecretKeySelector {
  name: string;
  key: string;
  optional?: boolean;
}

/** A value supplied inline or pulled from a Secret/ConfigMap key. */
export interface ValueRef {
  name: string;
  value?: string;
  valueFrom?: {
    type: string;
    name: string;
    key: string;
  };
}

export interface TLSConfig {
  disableVerify?: boolean;
  caCertSecretRef?: string;
  caCertSecretKey?: string;
  disableSystemCAs?: boolean;
}

/** `namespace/name`, the ref format the API uses for path segments. */
export type ResourceRef = string;

/** Splits a `namespace/name` ref; a bare name yields an empty namespace. */
export function parseRef(ref: ResourceRef): { namespace: string; name: string } {
  const slash = ref.indexOf("/");
  if (slash === -1) return { namespace: "", name: ref };
  return { namespace: ref.slice(0, slash), name: ref.slice(slash + 1) };
}

/** Joins a namespace and name into the `namespace/name` ref format. */
export function toRef(namespace: string | undefined, name: string): ResourceRef {
  return namespace ? `${namespace}/${name}` : name;
}
