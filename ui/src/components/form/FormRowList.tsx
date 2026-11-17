import { Children, type ReactNode } from "react";
import { Button } from "antd";
import { useTheme } from "@emotion/react";
import { Plus, Trash2 } from "lucide-react";

interface FormRowListProps {
  children: ReactNode;
  /** Label for the add button; no button is shown without `onAdd`. */
  addLabel?: string;
  onAdd?: () => void;
  addDisabled?: boolean;
  /** Shown in place of the list when it has no rows. */
  empty?: ReactNode;
  "data-testid"?: string;
}

/** Repeated rows as a divided list: hairlines between rows, add button below. */
export function FormRowList({
  children,
  addLabel = "Add",
  onAdd,
  addDisabled,
  empty,
  "data-testid": testId,
}: FormRowListProps) {
  const theme = useTheme();
  const isEmpty = Children.toArray(children).length === 0;
  return (
    <div data-testid={testId}>
      {isEmpty ? (
        empty ? <div css={{ color: theme.color.textMuted, padding: `${theme.space(2)} 0` }}>{empty}</div> : null
      ) : (
        <ul
          css={{
            margin: 0,
            padding: 0,
            listStyle: "none",
            borderTop: `1px solid ${theme.color.borderStrong}`,
          }}
        >
          {children}
        </ul>
      )}
      {onAdd ? (
        <Button
          icon={<Plus size={14} aria-hidden />}
          onClick={onAdd}
          disabled={addDisabled}
          data-testid={testId ? `${testId}-add` : undefined}
          css={{ marginTop: theme.space(2) }}
        >
          {addLabel}
        </Button>
      ) : null}
    </div>
  );
}

interface FormRowProps {
  children: ReactNode;
  onRemove?: () => void;
  /** Accessible name for the delete button, e.g. "Remove header X-Team". */
  removeLabel?: string;
  removeDisabled?: boolean;
  "data-testid"?: string;
}

/** One row of a FormRowList: its fields, then a delete button. */
export function FormRow({ children, onRemove, removeLabel = "Remove row", removeDisabled, "data-testid": testId }: FormRowProps) {
  const theme = useTheme();
  return (
    <li
      data-testid={testId}
      css={{
        display: "flex",
        alignItems: "flex-start",
        gap: theme.space(2),
        padding: `${theme.space(2)} ${theme.space(2)}`,
        borderBottom: `1px solid ${theme.color.borderStrong}`,
        "&:hover": { background: `${theme.color.text}0f` },
      }}
    >
      <div css={{ flex: 1, minWidth: 0 }}>{children}</div>
      {onRemove ? (
        <Button
          type="text"
          danger
          aria-label={removeLabel}
          icon={<Trash2 size={14} aria-hidden />}
          onClick={onRemove}
          disabled={removeDisabled}
          css={{
            flexShrink: 0,
            "&&:not(:disabled):hover": { background: theme.color.dangerBg },
            "&&:not(:disabled):active": { background: theme.color.dangerBg, borderColor: theme.color.dangerBorder },
          }}
        />
      ) : null}
    </li>
  );
}
