import { useId } from "react";
import { Typography } from "antd";
import { useNamespaces } from "@/api";
import { NameInput } from "./NameInput";
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

/** Picks a namespace kagent can place resources in, or takes a typed one when the list fails. */
export function NamespaceSelect({ value, onChange, disabled, id, "data-testid": testId, ...aria }: NamespaceSelectProps) {
  const namespaces = useNamespaces();
  const noteId = useId();
  if (namespaces.error) {
    return (
      <>
        <NameInput
          dots={false}
          id={id}
          data-testid={testId}
          value={value ?? ""}
          onChange={onChange}
          disabled={disabled}
          placeholder="Type a namespace"
          aria-invalid={aria["aria-invalid"]}
          aria-describedby={[aria["aria-describedby"], noteId].filter(Boolean).join(" ")}
        />
        <Typography.Text id={noteId} type="secondary" css={{ display: "block", fontSize: 12 }}>
          Could not list namespaces ({namespaces.error.message}). Type the name of one.
        </Typography.Text>
      </>
    );
  }
  return (
    <ResourceSelect
      kind="namespace"
      names={(namespaces.data ?? []).map((row) => row.name)}
      loading={namespaces.isLoading}
      value={value || undefined}
      onChange={onChange}
      disabled={disabled}
      id={id}
      {...aria}
      data-testid={testId}
    />
  );
}
