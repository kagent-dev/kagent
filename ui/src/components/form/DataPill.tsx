import type { ReactNode } from "react";
import { useTheme } from "@emotion/react";
import { X } from "lucide-react";
import { dataPillLook, dataPillRemoveStates } from "./dataPillLook";

interface DataPillProps {
  children: ReactNode;
  /** Shows an x button that calls this. */
  onRemove?: () => void;
  /** Accessible name for the x button. Defaults to "Remove <children>". */
  removeLabel?: string;
  disabled?: boolean;
  /** Wraps long text onto more lines instead of cutting it off. */
  wrap?: boolean;
  "data-testid"?: string;
}

/** A value the user entered or picked: a key, a host, an audience. */
export function DataPill({ children, onRemove, removeLabel, disabled, wrap, "data-testid": testId }: DataPillProps) {
  const theme = useTheme();
  const label = removeLabel ?? `Remove ${typeof children === "string" ? children : "item"}`;

  return (
    <span
      data-testid={testId}
      css={{
        ...dataPillLook(theme),
        display: "inline-flex",
        alignItems: "center",
        gap: theme.space(1),
        maxWidth: "100%",
        padding: `1px ${theme.space(2)}`,
        lineHeight: "20px",
      }}
    >
      <span
        css={
          wrap
            ? { overflowWrap: "anywhere" }
            : { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }
        }
      >
        {children}
      </span>
      {onRemove ? (
        <button
          type="button"
          aria-label={label}
          disabled={disabled}
          onClick={onRemove}
          css={{
            display: "inline-flex",
            alignItems: "center",
            justifyContent: "center",
            width: 16,
            height: 16,
            marginRight: -4,
            padding: 0,
            border: 0,
            borderRadius: 2,
            background: "transparent",
            color: theme.color.textMuted,
            cursor: "pointer",
            ...dataPillRemoveStates(theme),
            "&:disabled": { cursor: "not-allowed", background: "transparent", color: theme.color.textMuted },
            "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 1 },
          }}
        >
          <X size={12} aria-hidden />
        </button>
      ) : null}
    </span>
  );
}

interface DataPillListProps {
  items: readonly string[];
  onRemove?: (item: string) => void;
  /** Shown instead of the list when there are no items. */
  empty?: ReactNode;
  "data-testid"?: string;
}

/** A wrapping row of pills, one per item. */
export function DataPillList({ items, onRemove, empty, "data-testid": testId }: DataPillListProps) {
  const theme = useTheme();
  if (items.length === 0) return empty ? <>{empty}</> : null;

  return (
    <ul
      data-testid={testId}
      css={{ display: "flex", flexWrap: "wrap", gap: theme.space(1), margin: 0, padding: 0, listStyle: "none" }}
    >
      {items.map((item) => (
        <li key={item} css={{ minWidth: 0 }}>
          <DataPill onRemove={onRemove ? () => onRemove(item) : undefined}>{item}</DataPill>
        </li>
      ))}
    </ul>
  );
}
