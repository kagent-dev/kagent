import { useState } from "react";

/** The name a copy of `name` starts with, kept within a Kubernetes name's 63 characters. */
export function copyName(name: string) {
  // A cut can end on `.` or `-`, which DNS-1123 forbids before the suffix's own `-`.
  return `${name.slice(0, 63 - "-copy".length).replace(/[^a-z0-9]+$/, "")}-copy`;
}

/**
 * Whether the form for `key` was first shown without its copy source. A form reads its
 * initial values once, so a source that arrives afterwards is never applied.
 */
export function useMountedBlank(key: string | undefined, shown: boolean, loaded: boolean) {
  const [mounted, setMounted] = useState<{ key: string; blank: boolean }>();
  // No key is a plain new form: it has no copy source to have missed.
  if (key && shown && mounted?.key !== key) setMounted({ key, blank: !loaded });
  return key ? (mounted?.blank ?? false) : false;
}

/** The "new" form's address for a copy of `source`; read back with `useSearchParams().get("from")`. */
export function copyPath(newPath: string, source: { namespace: string; name: string }) {
  return `${newPath}?from=${encodeURIComponent(`${source.namespace}/${source.name}`)}`;
}

/**
 * The resource a "new" form should copy, from its `?from=` parameter.
 *
 * Anything but `namespace/name` is no source at all, so a hand-edited address opens a
 * blank form rather than waiting on a resource that cannot exist.
 */
export function copySource(params: URLSearchParams): { namespace: string; name: string } | undefined {
  const [namespace, name, ...rest] = (params.get("from") ?? "").split("/");
  return namespace && name && rest.length === 0 ? { namespace, name } : undefined;
}
