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
  /** Focuses the first control of an added row. A remove always moves focus. */
  manageFocus?: boolean;
  "data-testid"?: string;
}

/** Tells the list a row is being removed, passing the row before it, so focus can move. */
const RowRemovedContext = createContext<((previous: Element | null) => void) | undefined>(undefined);

const FOCUSABLE = "input:not([disabled]), textarea:not([disabled]), button:not([disabled]), [tabindex]:not([tabindex='-1'])";

/** A row's first field, skipping the delete button in its header. */
const firstField = (row: Element | null | undefined): HTMLElement | undefined =>
  row ? Array.from(row.querySelectorAll<HTMLElement>(FOCUSABLE)).find((el) => !el.closest("[data-row-header]")) : undefined;

/** Repeated rows, each on its own card with a gap between, and the add button below. */
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
  const container = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const addButton = useRef<HTMLButtonElement>(null);
  const pending = useRef<"row" | { removedAfter: Element | null } | undefined>(undefined);

  useEffect(() => {
    const target = pending.current;
    pending.current = undefined;
    if (target === "row") firstField(list.current?.lastElementChild)?.focus();
    else if (target) {
      // The removed row took focus with it: go to "Add", else a neighbouring row, else the list.
      const row = target.removedAfter?.isConnected ? target.removedAfter : list.current?.firstElementChild;
      const next = addButton.current && !addButton.current.disabled ? addButton.current : firstField(row);
      (next ?? container.current)?.focus();
    }
  }, [count]);

  const add = () => {
    if (manageFocus) pending.current = "row";
    onAdd?.();
  };
  const rowRemoved = (previous: Element | null) => { pending.current = { removedAfter: previous }; };

  return (
    <div ref={container} tabIndex={-1} data-testid={testId}>
      {isEmpty ? (
        empty ? <div css={{ color: theme.color.textMuted, padding: `${theme.space(2)} 0` }}>{empty}</div> : null
      ) : (
        <ul
          ref={list}
          css={{
            margin: 0,
            padding: 0,
            listStyle: "none",
            display: "flex",
            flexDirection: "column",
            gap: theme.space(3),
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
  /** Names the item in the card's header, e.g. "Header 2". */
  title?: ReactNode;
  onRemove?: () => void;
  /** Accessible name for the delete button, e.g. "Remove header X-Team". */
  removeLabel?: string;
  removeDisabled?: boolean;
  "data-testid"?: string;
}

/** One item of a FormRowList: a card with a header (title, then delete button) over its fields. */
export function FormRow({
  children,
  title,
  onRemove,
  removeLabel = "Remove row",
  removeDisabled,
  "data-testid": testId,
}: FormRowProps) {
  const theme = useTheme();
  const rowRemoved = useContext(RowRemovedContext);
  return (
    <li
      data-testid={testId}
      css={{
        padding: theme.space(3),
        border: `1px solid ${theme.color.border}`,
        borderRadius: theme.radius.md,
        background: theme.color.bgElevated,
        boxShadow: "0 1px 3px rgba(0, 0, 0, 0.08)",
        "&:hover": { borderColor: theme.color.borderStrong },
      }}
    >
      {title !== undefined || onRemove ? (
        <div
          data-row-header
          css={{
            display: "flex",
            alignItems: "center",
            gap: theme.space(2),
            minHeight: 24,
            marginBottom: theme.space(2),
          }}
        >
          <div
            css={{
              flex: 1,
              minWidth: 0,
              display: "flex",
              alignItems: "center",
              flexWrap: "wrap",
              gap: theme.space(2),
              fontWeight: 600,
            }}
          >
            {title}
          </div>
          {onRemove ? (
            <Button
              type="text"
              size="small"
              danger
              aria-label={removeLabel}
              icon={<Trash2 size={14} aria-hidden />}
              onClick={(event) => {
                rowRemoved?.(event.currentTarget.closest("li")?.previousElementSibling ?? null);
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
        </div>
      ) : null}
      <div css={{ minWidth: 0 }}>{children}</div>
    </li>
  );
}
