import type { ReactNode } from "react";
import { useTheme } from "@emotion/react";
import { X } from "lucide-react";
import { dataPillButtonStates, dataPillLook } from "./dataPillLook";

interface DataPillProps {
  children: ReactNode;
  /** Makes the whole pill a button that calls this, with an x to show it removes. */
  onRemove?: () => void;
  /** Accessible name for the pill's button. Defaults to "Remove <children>". */
  removeLabel?: string;
  disabled?: boolean;
  /** Wraps long text onto more lines instead of cutting it off. */
  wrap?: boolean;
  "data-testid"?: string;
}

/** A value the user entered or picked: a key, a host, an audience. */
export function DataPill({ children, onRemove, removeLabel, disabled, wrap, "data-testid": testId }: DataPillProps) {
  const theme = useTheme();
  const look = {
    ...dataPillLook(theme),
    display: "inline-flex",
    alignItems: "center",
    gap: theme.space(1),
    maxWidth: "100%",
    padding: `1px ${theme.space(2)}`,
    lineHeight: "20px",
  } as const;
  const text = (
    <span
      css={
        wrap
          ? { overflowWrap: "anywhere", textAlign: "start" }
          : { overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }
      }
    >
      {children}
    </span>
  );

  if (!onRemove) {
    return (
      <span data-testid={testId} css={look}>
        {text}
      </span>
    );
  }
  return (
    <button
      type="button"
      data-testid={testId}
      aria-label={removeLabel ?? `Remove ${typeof children === "string" ? children : "item"}`}
      disabled={disabled}
      onClick={onRemove}
      css={{ ...look, paddingRight: theme.space(1), ...dataPillButtonStates(theme) }}
    >
      {text}
      <span
        data-pill-x
        aria-hidden
        css={{
          display: "inline-flex",
          alignItems: "center",
          justifyContent: "center",
          flexShrink: 0,
          width: 16,
          height: 16,
          borderRadius: 3,
          color: theme.color.textMuted,
        }}
      >
        <X size={12} />
      </span>
    </button>
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
