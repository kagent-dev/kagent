import { Typography } from "antd";
import { useTheme } from "@emotion/react";
import { SERIES_COLOR } from "./chartTheme";

const { Text } = Typography;

interface ReadinessMeterProps {
  ready: number;
  total: number;
}

/**
 * How much of the fleet is ready, as a single ratio against its limit.
 *
 * A meter rather than a two-slice pie: the question is "how far along is this",
 * which a filled track answers at a glance and a pie makes the reader compare
 * two angles for.
 *
 * The fill carries severity — everything ready reads as the accent, a partial
 * fleet as a warning, nothing ready as a failure — and the empty track is the
 * fill's own hue held back, so the state is legible across the whole bar rather
 * than only where the fill stops. The numbers beside it are what actually
 * communicates: the colour is a second channel, never the only one.
 */
export function ReadinessMeter({ ready, total }: ReadinessMeterProps) {
  const theme = useTheme();

  const fraction = total === 0 ? 0 : ready / total;
  const fill =
    total === 0 || ready === total
      ? SERIES_COLOR
      : ready === 0
        ? theme.color.danger
        : theme.color.warning;

  return (
    <div data-testid="agent-readiness">
      <div
        css={{
          display: "flex",
          justifyContent: "space-between",
          gap: theme.space(3),
          marginBottom: theme.space(2),
        }}
      >
        <Text css={{ color: theme.color.textMuted }}>Deployments ready</Text>
        <Text data-testid="agent-readiness-count">
          {ready} of {total}
        </Text>
      </div>
      <div
        role="meter"
        aria-valuenow={ready}
        aria-valuemin={0}
        aria-valuemax={total}
        aria-label="Agent deployments ready"
        css={{
          height: 8,
          borderRadius: 999,
          // Tone-on-tone: the same hue as the fill, held well back, so the
          // unfilled remainder still reads as part of the same measurement.
          background: `color-mix(in srgb, ${fill} 22%, ${theme.color.bg})`,
          overflow: "hidden",
        }}
      >
        <div
          css={{
            width: `${Math.round(fraction * 100)}%`,
            height: "100%",
            borderRadius: 999,
            background: fill,
          }}
        />
      </div>
    </div>
  );
}
