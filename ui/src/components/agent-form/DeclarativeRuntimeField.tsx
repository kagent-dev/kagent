"use client";

import type { DeclarativeRuntime } from "@/types";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { FieldHint, FieldLabel, FieldRoot } from "@/components/agent-form/form-primitives";

type DeclarativeRuntimeFieldProps = {
  value: DeclarativeRuntime;
  onChange: (next: DeclarativeRuntime) => void;
  disabled?: boolean;
};

export function DeclarativeRuntimeField({ value, onChange, disabled }: DeclarativeRuntimeFieldProps) {
  return (
    <FieldRoot>
      <FieldLabel htmlFor="agent-declarative-runtime">ADK runtime</FieldLabel>
      <FieldHint>
        Select an ADK runtime to use for this agent.
      </FieldHint>
      <Select value={value} onValueChange={(v) => onChange(v as DeclarativeRuntime)} disabled={disabled}>
        <SelectTrigger id="agent-declarative-runtime" className="w-full max-w-md">
          <SelectValue placeholder="Select runtime…" />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value="python">Python</SelectItem>
          <SelectItem value="go">Go</SelectItem>
        </SelectContent>
      </Select>
    </FieldRoot>
  );
}
