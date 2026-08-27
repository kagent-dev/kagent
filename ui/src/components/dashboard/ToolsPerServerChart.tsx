import { useMemo } from "react";
import { Bar } from "react-chartjs-2";
import { Empty, Typography } from "antd";
import { useTheme } from "@emotion/react";
import type { ToolServerResponse } from "@/api";
import {
  SERIES_COLOR,
  barValueLabels,
  horizontalBarOptions,
} from "./chartTheme";

const { Text } = Typography;

interface ToolsPerServerChartProps {
  servers: ToolServerResponse[];
  /** True when the servers could not be read at all. */
  hasError: boolean;
  isLoading: boolean;
}

/**
 * How many tools each MCP server has reported.
 *
 * Comparing magnitude across a few named things, so: bars, one hue, and no
 * legend — there is a single series and the heading already names it. Horizontal
 * because the categories are `namespace/name` refs that will not fit under a
 * vertical column.
 *
 * A server reporting nothing is drawn as the zero it is, not dropped. A server
 * with no tools is the interesting row on this chart — it usually means the
 * controller has not completed a handshake — and hiding it would make the
 * cluster look healthier than it is.
 */
export function ToolsPerServerChart({
  servers,
  hasError,
  isLoading,
}: ToolsPerServerChartProps) {
  const theme = useTheme();

  const chart = useMemo(() => {
    const rows = [...servers].sort(
      (a, b) => b.discoveredTools.length - a.discoveredTools.length,
    );

    return {
      labels: rows.map((server) => server.ref),
      datasets: [
        {
          label: "Tools",
          data: rows.map((server) => server.discoveredTools.length),
          backgroundColor: SERIES_COLOR,
          // Thin marks: the bar never fills its band, so the leftover is air.
          maxBarThickness: 24,
          // Rounded at the value end, square where it meets the baseline.
          borderRadius: 4,
          borderSkipped: "start" as const,
          // No ring. A 2px border in the surface colour was meant to separate
          // neighbouring bars, but `maxBarThickness` already leaves air between them,
          // so all it did was eat 2px off every bar — visible as a seam on the short
          // ones and a mismatch between a bar's length and its value.
        },
      ],
    };
  }, [servers]);

  // A failed read must not be drawn as a chart of zeroes: an empty plot with
  // real axes looks like a measurement, and this is the absence of one.
  if (hasError) {
    return (
      <Text data-testid="tools-chart-unavailable" css={{ color: theme.color.textMuted }}>
        Tool counts are unavailable while the server list cannot be read.
      </Text>
    );
  }

  if (!isLoading && servers.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        data-testid="tools-chart-empty"
        description="No MCP servers are registered yet."
      />
    );
  }

  return (
    <div
      data-testid="tools-per-server-chart"
      // Height is fixed and the aspect ratio is off, so the plot keeps its
      // proportions instead of growing a bar's thickness with the viewport.
      css={{ height: 30 * Math.max(servers.length, 1) + 72, minHeight: 140 }}
    >
      <Bar
        data={chart}
        options={horizontalBarOptions({
          one: "tool reported",
          many: "tools reported",
        })}
        plugins={[barValueLabels]}
        aria-label="Discovered tools per MCP server"
      />
    </div>
  );
}
