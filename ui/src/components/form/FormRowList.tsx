import { Children, createContext, useContext, useEffect, useRef, type ReactNode } from "react";
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
  /** More add controls, such as a picker, shown beside the add button. */
  actions?: ReactNode;
  /** Focuses the first control of an added row, and the add button after a remove. */
  manageFocus?: boolean;
  "data-testid"?: string;
}

/** Tells the list a row's remove button was used, so focus can move to "Add". */
const RowRemovedContext = createContext<(() => void) | undefined>(undefined);

const FOCUSABLE = "input:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex='-1'])";

/** Repeated rows as a divided list: hairlines between rows, add button below. */
export function FormRowList({
  children,
  addLabel = "Add",
  onAdd,
  addDisabled,
  empty,
  actions,
  manageFocus,
  "data-testid": testId,
}: FormRowListProps) {
  const theme = useTheme();
  const count = Children.toArray(children).length;
  const isEmpty = count === 0;
  const list = useRef<HTMLUListElement>(null);
  const addButton = useRef<HTMLButtonElement>(null);
  const pending = useRef<"row" | "add" | undefined>(undefined);

  useEffect(() => {
    const target = pending.current;
    pending.current = undefined;
    if (target === "add") addButton.current?.focus();
    if (target === "row") list.current?.lastElementChild?.querySelector<HTMLElement>(FOCUSABLE)?.focus();
  }, [count]);

  const add = () => {
    if (manageFocus) pending.current = "row";
    onAdd?.();
  };
  const rowRemoved = manageFocus ? () => { pending.current = "add"; } : undefined;

  return (
    <div data-testid={testId}>
      {isEmpty ? (
        empty ? <div css={{ color: theme.color.textMuted, padding: `${theme.space(2)} 0` }}>{empty}</div> : null
      ) : (
        <ul
          ref={list}
          css={{
            margin: 0,
            padding: 0,
            listStyle: "none",
            borderTop: `1px solid ${theme.color.borderStrong}`,
          }}
        >
          <RowRemovedContext.Provider value={rowRemoved}>{children}</RowRemovedContext.Provider>
        </ul>
      )}
      {onAdd || actions ? (
        <div css={{ display: "flex", flexWrap: "wrap", gap: theme.space(2), marginTop: theme.space(2) }}>
          {actions}
          {onAdd ? (
            <Button
              ref={addButton}
              icon={<Plus size={14} aria-hidden />}
              onClick={add}
              disabled={addDisabled}
              data-testid={testId ? `${testId}-add` : undefined}
            >
              {addLabel}
            </Button>
          ) : null}
        </div>
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
  const rowRemoved = useContext(RowRemovedContext);
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
          onClick={() => {
            rowRemoved?.();
            onRemove();
          }}
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
