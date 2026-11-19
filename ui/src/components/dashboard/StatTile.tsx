import type { ReactNode } from "react";
import { Card, Typography } from "antd";
import { useTheme } from "@emotion/react";

const { Text } = Typography;

interface StatTileProps {
  label: string;
  /**
   * The headline value, when it is known. `undefined` means it could not be
   * read — which is not the same as zero, and must not render as one.
   *
   * A string for the tiles whose answer is not a count: a ratio that shows both
   * halves (`2/3` ready), or a one-word state (`connected`). Those are still one
   * headline value, and a second component for them would be two tiles that had
   * to be kept looking alike by hand.
   */
  value: number | string | undefined;
  /** Shown under the value: a breakdown, a qualifier, or why there is no value. */
  hint?: ReactNode;
  /** Dims the value while the first load is still in flight. */
  isLoading?: boolean;
  testId?: string;
}

/**
 * One headline count.
 *
 * A single current value is a stat tile rather than a one-bar chart — the number
 * is the point, and a chart around it adds axes and gridlines that carry nothing.
 *
 * `value === undefined` renders an em dash instead of `0`. A failed request and
 * an empty cluster produce the same absence of rows, and a dashboard that prints
 * "0 agents" when it simply could not ask is asserting something it does not
 * know. That distinction is the reason `value` is nullable at all.
 */
export function StatTile({
  label,
  value,
  hint,
  isLoading = false,
  testId,
}: StatTileProps) {
  const theme = useTheme();
  const known = value !== undefined;

  return (
    <Card size="small" data-testid={testId} css={{ height: "100%" }}>
      <Text css={{ color: theme.color.textMuted }}>{label}</Text>
      <div
        data-testid={testId ? `${testId}-value` : undefined}
        css={{
          // Proportional figures on purpose: tabular digits give every glyph the
          // width of a zero, which reads loose at this size. Tabular belongs in
          // columns that have to line up, not here.
          fontSize: 30,
          fontWeight: 600,
          lineHeight: 1.2,
          // For the tiles whose value is a word rather than a number: a namespace
          // name long enough to overflow its tile must wrap inside it, not push the
          // grid sideways.
          overflowWrap: "anywhere",
          marginTop: theme.space(1),
          color: known ? theme.color.text : theme.color.textMuted,
          opacity: isLoading ? 0.4 : 1,
          transition: "opacity 120ms ease-out",
        }}
      >
        {known ? value : "—"}
      </div>
      {hint ? (
        <Text css={{ display: "block", color: theme.color.textMuted, fontSize: 12 }}>
          {hint}
        </Text>
      ) : null}
    </Card>
  );
}
