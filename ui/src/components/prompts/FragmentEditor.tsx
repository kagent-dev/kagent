import { Input } from "antd";
import { useTheme } from "@emotion/react";
import { DataPill } from "@/components/form/DataPill";
import { FieldLabel } from "@/components/form/FormField";
import { FormRow, FormRowList } from "@/components/form/FormRowList";

import { type FragmentRow, newFragmentRow } from "./fragmentRows";

interface FragmentEditorProps {
  rows: FragmentRow[];
  onChange: (rows: FragmentRow[]) => void;
  /** Library name, so each row can show the include tag it will produce. */
  library?: string;
  disabled?: boolean;
}

/** Repeatable key/content rows, the shape a prompt library is authored in. */
export function FragmentEditor({ rows, onChange, library, disabled }: FragmentEditorProps) {
  const theme = useTheme();

  const update = (id: string, patch: Partial<FragmentRow>) =>
    onChange(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));

  const remove = (id: string) => {
    const remaining = rows.filter((row) => row.id !== id);
    // Removing the last row leaves a blank one, so there is always somewhere to type.
    onChange(remaining.length > 0 ? remaining : [newFragmentRow()]);
  };

  return (
    <FormRowList
      data-testid="fragment-list"
      addLabel="Add fragment"
      onAdd={() => onChange([...rows, newFragmentRow()])}
      addDisabled={disabled}
    >
      {rows.map((row, index) => (
        <FormRow
          key={row.id}
          data-testid="fragment-row"
          title={`Fragment ${index + 1}`}
          onRemove={() => remove(row.id)}
          removeLabel={`Remove fragment ${index + 1}`}
          removeDisabled={disabled || (rows.length === 1 && !row.key && !row.value)}
        >
          <div css={{ display: "flex", flexDirection: "column", gap: theme.space(2) }}>
            <label css={{ display: "flex", flexDirection: "column", gap: theme.space(1) }}>
              <FieldLabel required>Key</FieldLabel>
              <Input
                data-testid="fragment-key"
                aria-label={`Fragment key ${index + 1}`}
                placeholder="safety-rules"
                value={row.key}
                disabled={disabled}
                onChange={(event) => update(row.id, { key: event.target.value })}
                css={{ fontFamily: theme.font.mono }}
              />
            </label>

            {/* The include tag is what the row is for, so it shows as soon as it can be built. */}
            {library && row.key.trim() ? (
              <div>
                <DataPill data-testid="fragment-include-preview">
                  {`{{include "${library}/${row.key.trim()}"}}`}
                </DataPill>
              </div>
            ) : null}

            <label css={{ display: "flex", flexDirection: "column", gap: theme.space(1) }}>
              <FieldLabel required={false}>Text</FieldLabel>
              <Input.TextArea
                data-testid="fragment-value"
                aria-label={`Fragment content ${index + 1}`}
                placeholder="Prompt fragment text…"
                value={row.value}
                disabled={disabled}
                onChange={(event) => update(row.id, { value: event.target.value })}
                autoSize={{ minRows: 3, maxRows: 12 }}
                css={{ fontFamily: theme.font.mono }}
              />
            </label>
          </div>
        </FormRow>
      ))}
    </FormRowList>
  );
}
