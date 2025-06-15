/**
 * Creates a Kubernetes reference string in the format "namespace/name"
 * @param namespace The namespace of the resource
 * @param name The name of the resource
 * @returns A string in the format "namespace/name" or just "name" if namespace is empty
 */
export function toRef(namespace: string, name: string): string {
  if (!namespace) {
    return name;
  }
  return `${namespace}/${name}`;
} 