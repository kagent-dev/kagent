import { Button, Input, Space, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Plus, Trash2 } from "lucide-react";

import { type FragmentRow, newFragmentRow } from "./fragmentRows";

const { Text } = Typography;

interface FragmentEditorProps {
  rows: FragmentRow[];
  onChange: (rows: FragmentRow[]) => void;
  /** Library name, so each row can show the include tag it will produce. */
  library?: string;
  disabled?: boolean;
}


/** Repeatable key/content rows, the shape a prompt library is authored in. */
export function FragmentEditor({
  rows,
  onChange,
  library,
  disabled,
}: FragmentEditorProps) {
  const theme = useTheme();

  const update = (id: string, patch: Partial<FragmentRow>) =>
    onChange(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));

  const remove = (id: string) => {
    const remaining = rows.filter((row) => row.id !== id);
    // The editor is never empty: removing the last row leaves a blank one, so
    // there is always somewhere to type without hunting for "Add fragment".
    onChange(remaining.length > 0 ? remaining : [newFragmentRow()]);
  };

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
      {rows.map((row, index) => (
        <div
          key={row.id}
          data-testid="fragment-row"
          css={{
            border: `1px solid ${theme.color.border}`,
            borderRadius: theme.radius.md,
            padding: theme.space(3),
            background: theme.color.bgElevated,
          }}
        >
          <div
            css={{
              display: "flex",
              gap: theme.space(2),
              alignItems: "center",
              marginBottom: theme.space(2),
            }}
          >
            <Input
              data-testid="fragment-key"
              aria-label={`Fragment key ${index + 1}`}
              placeholder="Key, e.g. safety-rules"
              value={row.key}
              disabled={disabled}
              onChange={(event) => update(row.id, { key: event.target.value })}
              css={{ fontFamily: theme.font.mono }}
            />
            <Button
              data-testid="fragment-remove"
              aria-label={`Remove fragment ${index + 1}`}
              danger
              icon={<Trash2 size={14} />}
              disabled={disabled || (rows.length === 1 && !row.key && !row.value)}
              onClick={() => remove(row.id)}
            />
          </div>

          {/* The include tag is what this row is for, so it's shown as soon as
              there is enough to build one rather than only after saving. */}
          {library && row.key.trim() ? (
            <Text
              data-testid="fragment-include-preview"
              css={{
                display: "block",
                marginBottom: theme.space(2),
                fontFamily: theme.font.mono,
                color: theme.color.textMuted,
              }}
            >
              {`{{include "${library}/${row.key.trim()}"}}`}
            </Text>
          ) : null}

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
        </div>
      ))}

      <Button
        data-testid="fragment-add"
        icon={<Plus size={14} />}
        disabled={disabled}
        onClick={() => onChange([...rows, newFragmentRow()])}
      >
        Add fragment
      </Button>
    </Space>
  );
}


