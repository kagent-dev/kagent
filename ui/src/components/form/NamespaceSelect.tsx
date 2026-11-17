import { useNamespaces } from "@/api";
import { ResourceSelect } from "./ResourceSelect";

interface NamespaceSelectProps {
  value?: string;
  onChange: (namespace: string) => void;
  disabled?: boolean;
  id?: string;
  "aria-invalid"?: boolean;
  "aria-describedby"?: string;
  "data-testid"?: string;
}

/** Picks a namespace kagent can place resources in. */
export function NamespaceSelect({ value, onChange, disabled, id, "data-testid": testId, ...aria }: NamespaceSelectProps) {
  const namespaces = useNamespaces();
  return (
    <ResourceSelect
      kind="namespace"
      names={(namespaces.data ?? []).map((row) => row.name)}
      loading={namespaces.isLoading}
      error={namespaces.error?.message}
      value={value || undefined}
      onChange={onChange}
      disabled={disabled}
      id={id}
      {...aria}
      data-testid={testId}
    />
  );
}
