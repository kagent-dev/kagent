import { useEffect, useId, useRef, type ReactNode } from "react";
import { Button, Input } from "antd";
import { useTheme } from "@emotion/react";
import { Plus, Trash2 } from "lucide-react";
import { FieldLabel } from "./FormField";
import type { FieldValidation } from "./useSubmitValidation";

interface KeyValueListProps {
  rows: readonly { key: string; value: string }[];
  /** A stable React key per row; the index where rows have no id. */
  rowKey?: (index: number) => string | number;
  keyTitle: string;
  valueTitle: string;
  /** Marks the columns required and optional; leave off to show plain titles. */
  keyRequired?: boolean;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  mono?: boolean;
  onChange: (index: number, patch: { key?: string; value?: string }) => void;
  onAdd: () => void;
  onRemove: (index: number) => void;
  addLabel: string;
  removeLabel: (index: number) => string;
  /** The key cell's message and touch callback, from `useSubmitValidation`. */
  keyField?: (index: number) => FieldValidation;
  "data-testid"?: string;
}

/** Unnumbered key and value pairs as a compact two-column list, with a delete button per row. */
export function KeyValueList({
  rows,
  rowKey = (index) => index,
  keyTitle,
  valueTitle,
  keyRequired,
  keyPlaceholder,
  valuePlaceholder,
  mono,
  onChange,
  onAdd,
  onRemove,
  addLabel,
  removeLabel,
  keyField,
  "data-testid": testId,
}: KeyValueListProps) {
  const theme = useTheme();
  const ids = useId();
  const list = useRef<HTMLDivElement>(null);
  const addButton = useRef<HTMLButtonElement>(null);
  const pending = useRef<"added" | "removed" | undefined>(undefined);

  useEffect(() => {
    const target = pending.current;
    pending.current = undefined;
    if (target === "added") [...(list.current?.querySelectorAll<HTMLElement>("[data-kv-row] input") ?? [])].at(-2)?.focus();
    else if (target === "removed") addButton.current?.focus();
  }, [rows.length]);

  const title = (text: string, required: boolean | undefined, id: string): ReactNode => (
    <div id={id} css={{ fontSize: 13, fontWeight: 500, color: theme.color.textMuted }}>
      {required === undefined ? text : <FieldLabel required={required}>{text}</FieldLabel>}
    </div>
  );
  const control = { width: "100%", ...(mono ? { fontFamily: theme.font.mono } : {}) };

  return (
    <div data-testid={testId}>
      {rows.length > 0 ? (
        <div
          ref={list}
          css={{
            display: "grid",
            gridTemplateColumns: "minmax(0, 1fr) minmax(0, 1fr) auto",
            columnGap: theme.space(2),
            rowGap: theme.space(2),
            alignItems: "start",
            padding: theme.space(3),
            border: `1px solid ${theme.color.border}`,
            borderRadius: theme.radius.md,
            background: theme.color.bgElevated,
          }}
        >
          {title(keyTitle, keyRequired, `${ids}-key`)}
          {title(valueTitle, keyRequired === undefined ? undefined : false, `${ids}-value`)}
          <span />
          {rows.map((row, index) => {
            const field = keyField?.(index);
            const errorId = `${ids}-${index}-error`;
            return (
              <div key={rowKey(index)} data-kv-row data-testid={testId ? `${testId}-row` : undefined} css={{ display: "contents" }}>
                <div onBlur={field?.onTouch}>
                  <Input
                    data-testid={testId ? `${testId}-key` : undefined}
                    aria-label={`${keyTitle} ${index + 1}`}
                    aria-invalid={field?.error ? true : undefined}
                    aria-describedby={field?.error ? errorId : undefined}
                    status={field?.error ? "error" : undefined}
                    placeholder={keyPlaceholder}
                    value={row.key}
                    onChange={(event) => onChange(index, { key: event.target.value })}
                    css={control}
                  />
                  {field?.error ? (
                    <div id={errorId} css={{ marginTop: theme.space(1), color: theme.color.dangerText, fontSize: 13 }}>
                      {field.error}
                    </div>
                  ) : null}
                </div>
                <Input
                  data-testid={testId ? `${testId}-value` : undefined}
                  aria-label={`${valueTitle} ${index + 1}`}
                  placeholder={valuePlaceholder}
                  value={row.value}
                  onChange={(event) => onChange(index, { value: event.target.value })}
                  css={control}
                />
                <Button
                  type="text"
                  danger
                  aria-label={removeLabel(index)}
                  icon={<Trash2 size={14} aria-hidden />}
                  onClick={() => {
                    pending.current = "removed";
                    onRemove(index);
                  }}
                  css={{
                    "&&:not(:disabled):hover": { background: theme.color.dangerBg },
                    "&&:not(:disabled):active": { background: theme.color.dangerBg, borderColor: theme.color.dangerBorder },
                  }}
                />
              </div>
            );
          })}
        </div>
      ) : null}
      <Button
        ref={addButton}
        icon={<Plus size={14} aria-hidden />}
        onClick={() => {
          pending.current = "added";
          onAdd();
        }}
        data-testid={testId ? `${testId}-add` : undefined}
        css={{ marginTop: rows.length > 0 ? theme.space(2) : 0 }}
      >
        {addLabel}
      </Button>
    </div>
  );
}
