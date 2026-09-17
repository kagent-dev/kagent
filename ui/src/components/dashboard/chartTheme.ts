/**
 * Chart.js registration and the shared look every chart on this page wears.
 *
 * Chart.js 4 is tree-shaken: nothing renders unless the pieces it needs are
 * registered first, and registration is global, so it happens once here rather
 * than in each chart component.
 *
 * The palette is not a taste call. `dataviz`'s validator was run against the
 * surface these charts actually sit on — the elevated card, not the page plane —
 * and a single-hue series is what passed: adding a second hue alongside this
 * violet failed both the colour-vision separation check (violet against the
 * reference blue measures ΔE 1.9 under protanopia) and the normal-vision floor.
 * A second series therefore needs re-validating, not just picking.
 */

import {
  BarElement,
  CategoryScale,
  Chart,
  LinearScale,
  Tooltip,
  type ChartOptions,
  type Plugin,
} from "chart.js";
import { tokens } from "@/theme/theme";

Chart.register(CategoryScale, LinearScale, BarElement, Tooltip);

/** The one series hue, validated against the card surface in dark mode. */
export const SERIES_COLOR = "#9085e9";

/** The surface charts are drawn on — the card, which labels are measured against. */
export const CHART_SURFACE = tokens.color.bgElevated;

const AXIS_TEXT = tokens.color.textMuted;
const GRID_LINE = tokens.color.border;

/**
 * Horizontal bars for comparing magnitude across a handful of long-named things.
 *
 * Horizontal because the category labels are `namespace/name` refs, which do not
 * fit under a vertical column without being turned on their side.
 *
 * `valueLabel` is both forms of the unit rather than one string, because the tooltip
 * reads out a count and a single string made it say "1 tools reported". Given
 * explicitly instead of suffixed with an "s": the caller knows its own noun, and a
 * helper that guessed would be wrong the first time something irregular is counted.
 */
export function horizontalBarOptions(valueLabel: {
  one: string;
  many: string;
}): ChartOptions<"bar"> {
  return {
    indexAxis: "y",
    responsive: true,
    maintainAspectRatio: false,
    // Drawn at its final value, not grown into it. The library animates bars from zero
    // on every render, and a dashboard re-renders whenever any of its reads settle — so
    // a figure already on screen would replay its own animation because a different card
    // finished loading. It also means a screenshot or a recording catches the chart
    // mid-grow and shows numbers that were never the measurement.
    animation: false,
    // Room at the right for the value sitting past the end of the longest bar.
    layout: { padding: { right: 28 } },
    plugins: {
      // One series, so there is nothing for a legend to disambiguate — the
      // card's own heading already says what is plotted.
      legend: { display: false },
      tooltip: {
        backgroundColor: tokens.color.bg,
        borderColor: GRID_LINE,
        borderWidth: 1,
        titleColor: tokens.color.text,
        bodyColor: tokens.color.textMuted,
        padding: 10,
        displayColors: false,
        callbacks: {
          // Zero takes the plural, which is what English does: "0 tools reported".
          label: (item) =>
            `${item.parsed.x} ${
              item.parsed.x === 1 ? valueLabel.one : valueLabel.many
            }`,
        },
      },
    },
    scales: {
      x: {
        beginAtZero: true,
        border: { color: GRID_LINE },
        // Whole things are being counted, so a "2.5" tick would be a lie.
        ticks: { color: AXIS_TEXT, precision: 0, font: { size: 12 } },
        grid: { color: GRID_LINE, lineWidth: 1, tickLength: 0 },
      },
      y: {
        border: { color: GRID_LINE },
        ticks: { color: AXIS_TEXT, font: { size: 12 } },
        // A gridline per category would draw a line through every bar.
        grid: { display: false },
      },
    },
  };
}

/**
 * Writes each bar's value just past its end.
 *
 * `dataviz` wants the value on the mark rather than left to the axis, and with
 * three bars there is no risk of the flood that makes direct labels unreadable.
 * Drawn here instead of with a plugin dependency: it is a dozen lines of canvas
 * and the alternative is another package in a shared tree.
 */
export const barValueLabels: Plugin<"bar"> = {
  id: "barValueLabels",
  afterDatasetsDraw(chart) {
    const { ctx } = chart;
    const meta = chart.getDatasetMeta(0);
    if (!meta) return;

    ctx.save();
    ctx.fillStyle = AXIS_TEXT;
    ctx.font = `12px ${tokens.font.body}`;
    ctx.textAlign = "left";
    ctx.textBaseline = "middle";

    meta.data.forEach((element, index) => {
      const value = chart.data.datasets[0]?.data[index];
      if (typeof value !== "number") return;
      // 6px clear of the rounded end, so the glyph never touches the mark.
      ctx.fillText(String(value), element.x + 6, element.y);
    });

    ctx.restore();
  },
};
