"use client";

import * as React from "react";
import { Input } from "@/components/ui/input";
import { FieldHint, FieldLabel, FieldRoot } from "./form-primitives";

export function ServiceAccountNameField({
  value,
  onChange,
  onBlur,
  error,
  disabled,
  inputId: inputIdProp,
}: {
  value: string;
  onChange: (value: string) => void;
  onBlur: () => void;
  error?: string;
  disabled: boolean;
  /** Defaults to a stable `agent-field-service-account` when omitted. */
  inputId?: string;
}) {
  const inputId = inputIdProp ?? "agent-field-service-account";
  return (
    <FieldRoot>
      <FieldLabel htmlFor={inputId}>Service account (optional)</FieldLabel>
      <FieldHint>Existing Kubernetes ServiceAccount for the agent pod. If empty, the default or controller-created SA is used.</FieldHint>
      <Input
        id={inputId}
        name="serviceAccountName"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onBlur={onBlur}
        className={error ? "border-destructive" : ""}
        placeholder="e.g. my-workload-identity-sa"
        autoComplete="off"
        spellCheck={false}
        translate="no"
        disabled={disabled}
        aria-invalid={!!error}
        aria-describedby={error ? `${inputId}-err` : undefined}
      />
      {error ? (
        <p id={`${inputId}-err`} className="text-sm text-destructive mt-1" role="alert">
          {error}
        </p>
      ) : null}
    </FieldRoot>
  );
}
