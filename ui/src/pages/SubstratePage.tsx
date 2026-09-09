import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import {
  Alert,
  Button,
  Card,
  Input,
  InputNumber,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import type { ColumnsType } from "antd/es/table";
import { useTheme, type CSSObject, type Theme } from "@emotion/react";
import { useThemeMode } from "@/theme/themeMode";
import { Radio, Search } from "lucide-react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { StatTile } from "@/components/dashboard/StatTile";
import { RefreshButton } from "@/components/table/RefreshButton";
import { PageControls, usePageStack } from "@/components/table/PageControls";
import {
  useNamespaces,
  useSubstrateActors,
  useSubstrateSummary,
  useSubstrateWorkers,
  type SubstrateActorEntry,
  type SubstrateActorTemplateEntry,
  type SubstrateStatusCount,
  type SubstrateWorkerEntry,
  type SubstrateWorkerPoolEntry,
} from "@/api";

const { Text } = Typography;

/**
 * How tall the actor and worker tables get before they scroll internally.
 *
 * These two lists are the only ones here whose length is set by the cluster rather
 * than by configuration, and ate-api will hand over as many as exist: a real cluster
 * answered with 34,356 actors, which unbounded came to a 1.4-million-pixel page that
 * took seconds to become interactive and could not even be screenshotted. Bounding
 * the body and letting antd window the rows keeps the page a fixed size whatever the
 * backend reports.
 */
const GROWING_TABLE_HEIGHT = 420;

/**
 * The interval polling starts at, in seconds.
 *
 * Half a second is quick enough to watch an actor move between workers, which is what
 * this control is for. It is also the floor below, so the default is the fastest this
 * page will ask — a reader who turns polling on wants to see the cluster move.
 */
const DEFAULT_POLL_SECONDS = 0.5;

/**
 * The fastest this page will ask, in seconds.
 *
 * Below this the reader is not watching a cluster, they are load-testing one. The two
 * list reads are pages now and are cheap, but the summary is not: ate-api reports no
 * totals, so the controller walks every one of its pages to count, and on a cluster
 * holding 410,110 actors that walk is seconds. Enforced on the field rather than only
 * in the timer, so the number on screen is the number being used.
 */
const MIN_POLL_SECONDS = 0.5;

/**
 * What a poll interval means, given whatever is in the field.
 *
 * `null` is an empty or unparseable field — antd hands back `null` for "." and for a
 * cleared input — and zero is a deliberate stop. Both mean the same thing here: the
 * toggle can stay on without a timer running behind it, so a reader who wants to pause
 * without losing their place has a way to.
 *
 * Anything faster than the floor is read as the floor rather than refused, so a
 * half-typed "0.1" polls at 0.5 instead of hammering the controller for the moment
 * before the field is corrected.
 */
function pollIntervalMs(seconds: number | null): number | undefined {
  if (seconds === null || !Number.isFinite(seconds) || seconds <= 0) return undefined;
  return Math.max(seconds, MIN_POLL_SECONDS) * 1000;
}

/** Where the chosen scope lives, so a link carries what the reader is looking at. */
const NAMESPACE_PARAM = "namespace";

/**
 * The scope that means "everything the controller watches".
 *
 * `GetSubstrateStatusRequest.namespace` is empty for it — `substrateNamespaces("")` in
 * the controller expands that to its observed namespaces — so the absence of the URL
 * param and the absence of the field are the same fact, and neither needs a sentinel.
 */
const ALL_NAMESPACES = "";

/**
 * A wire enum as a word: `ACTOR_STATE_CRASHED` reads as `Crashed`.
 *
 * The controller names the states it knows, but falls back to the protobuf constant for
 * any it does not, so an unmapped state reaches this page as a wire symbol. Proto names
 * every value after its own enum, and that prefix only repeats the column header, so it
 * goes rather than being spelled out as `Actor state crashed`.
 *
 * Anything not shaped like a constant is returned untouched: a status the controller has
 * already written for a reader must not be rewritten by a guess about its casing.
 */
function humanizeEnum(label: string): string {
  const value = label.trim();
  if (!/^[A-Z][A-Z0-9]*(_[A-Z0-9]+)+$/.test(value)) return value;
  const words = value.replace(/^[A-Z0-9]+_STATE_/, "").toLowerCase().replace(/_/g, " ");
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * What a status or phase is telling you, as five readings rather than a dozen strings.
 *
 * The substrate's vocabulary is not a closed enum on the wire: `phase` and `status` are
 * plain strings that ate-api and the ActorTemplate controller each fill in their own way,
 * so this classifies rather than switches. Anything unrecognised falls through to
 * `neutral` and is shown as it arrived — inventing a colour for a word this page has
 * never seen would be a claim about health nobody made.
 */
type StatusTone = "healthy" | "danger" | "warning" | "progress" | "idle" | "neutral";

function statusTone(label: string): StatusTone {
  const value = humanizeEnum(label).trim().toLowerCase();
  if (value === "ready" || value === "running") return "healthy";
  // A crashed or failed actor is not a caution, it is the thing that went wrong.
  if (value === "failed" || value === "crashed") return "danger";
  // Deletion is in flight like the transitions below and is checked before them, because
  // it is the one that does not come back: an actor that reads the same shade as one
  // taking a snapshot is an actor nobody looks at twice.
  if (value.includes("delet")) return "warning";
  // `idle` among them because that is the word the workers table already uses for a pod
  // holding no actor, and a parked worker and a parked actor are the same news.
  if (
    value === "suspended" ||
    value === "paused" ||
    value === "idle" ||
    value === "unknown" ||
    value === ""
  ) {
    return "idle";
  }
  // Shapes rather than words, because these arrive spelled several ways: `Resuming`,
  // `Suspending`, `WaitingForWorker`, `GoldenSnapshotPending`. All of them mean the same
  // thing to a reader — something is under way and the next read will say otherwise.
  if (value.endsWith("ing") || value.includes("wait") || value.includes("golden")) {
    return "progress";
  }
  return "neutral";
}

/**
 * Each tone's three colours, the theme's own rather than antd's presets.
 *
 * antd derives a tag's three from one foreground token on the assumption of a light
 * page. `primary` is not among them in any tone — it is a fill chosen to carry light
 * text, and as ink on this page it measures about 2.2:1.
 *
 * `color` is the saturated one and the only one that carries meaning on its own: the
 * fills are near-identical tints, about ΔE 3 apart, so a stripe painted with them
 * would read as one stripe. That is what the bar below fills with, and it is why the
 * bar and the chips read the same status the same way.
 */
function statusPalette(theme: Theme): Record<StatusTone, CSSObject> {
  return {
    healthy: {
      background: theme.color.successBg,
      borderColor: theme.color.successBorder,
      color: theme.color.successText,
    },
    danger: {
      background: theme.color.dangerBg,
      borderColor: theme.color.dangerBorder,
      color: theme.color.dangerText,
    },
    warning: {
      background: theme.color.warningBg,
      borderColor: theme.color.warningBorder,
      color: theme.color.warningText,
    },
    progress: {
      background: theme.color.infoBg,
      borderColor: theme.color.infoBorder,
      color: theme.color.infoText,
    },
    idle: {
      background: theme.color.bgElevated,
      // `borderStrong` and not `border`: the hairline token is the app's dividers, and at
      // 1.4:1 it is a decorative edge rather than a boundary. This one measures 3.5:1.
      borderColor: theme.color.borderStrong,
      color: theme.color.textMuted,
    },
    neutral: {
      background: theme.color.bgElevated,
      borderColor: theme.color.borderStrong,
      color: theme.color.text,
    },
  };
}

/** A status, coloured by what it means. */
function StatusChip({ label }: { label: string }) {
  const theme = useTheme();
  const tone = statusTone(label);
  const text = humanizeEnum(label);
  const pill = statusPalette(theme)[tone];

  return (
    <Tag
      css={{
        ...pill,
        /*
         * The substrate's vocabulary is open-ended: `phase` and `status` are plain
         * strings, and a value this build has never seen is shown as it arrived. Some
         * of them are long — `WaitingForWorker` at one line overflowed its column and
         * printed itself across the next one. So the tag wraps inside the width it is
         * given rather than spilling out of it.
         */
        whiteSpace: "normal",
        maxWidth: "100%",
        wordBreak: "break-word",
      }}
      data-tone={tone}
    >
      {text === "" ? "not reported" : text}
    </Tag>
  );
}

/**
 * A count at a glance: 999 stays 999, 1,100 becomes `1.1k`.
 *
 * The legend and the bar are read sideways, and a cluster answered with 410,110 actors —
 * a row of exact figures there is a row nobody reads. The exact numbers stay where they
 * are acted on: the tiles, the section counts and the table.
 *
 * `K` lowercased because that is the convention for thousands; `M` and above are left as
 * `Intl` writes them, where uppercase is the convention instead.
 */
const compactNumber = new Intl.NumberFormat(undefined, {
  notation: "compact",
  maximumFractionDigits: 1,
});
const atAGlance = (count: number) => compactNumber.format(count).replace("K", "k");

/**
 * Every actor state a controller can report, so the legend is the vocabulary rather than
 * today's sample: a reader learns that `Crashed` is a thing that happens by seeing it at
 * zero, not by waiting for one.
 *
 * Mirrors `ActorStatusLabel` in `go/core/internal/substrate/list.go`, which names the
 * `ate.dev` `ActorState` enum. Drift is not a failure here: this decides only what is
 * listed at zero, and any state the controller reports that is missing from it is added
 * to the legend from the data — so a new one appears the first time it happens.
 */
const ACTOR_STATES = [
  "Crashed",
  "Deleting",
  "Pausing",
  "Resuming",
  "Running",
  "Snapshotting",
  "Suspending",
  "Paused",
  "Suspended",
  "Unknown",
];

/**
 * How many actors the bar will draw one segment each for.
 *
 * A segment per actor is what makes the bar countable — eight ticks with two green is
 * read, not estimated. It stops being countable long before it stops being drawable, and
 * a cluster answered with 410,110 actors, so past this the bar falls back to one
 * proportional band per status. The number is where counting gives out, not where the
 * browser does.
 */
const ACTORS_DRAWN_INDIVIDUALLY = 80;

/**
 * The whole actor inventory as one bar, coloured by what each actor is doing.
 *
 * Two running of ten with the rest suspended is two green segments and eight grey. The
 * tile above says how many are running; only this says what the other eight are doing,
 * and with the table paged it is the one place the whole distribution appears at all — a
 * reader on page one of 410,110 actors has otherwise no way to learn that most of them
 * have crashed.
 *
 * The fills are the pills' own text colours, so an actor is the same colour here as in
 * the table. Not the pills' fills: those are near-identical tints about ΔE 3 apart, and a
 * bar painted with them would read as one long smudge.
 */
function StatusBar({
  counts,
  title,
  caption,
  emptyText,
  testId,
  vocabulary,
  noun,
  unread,
}: {
  counts: SubstrateStatusCount[];
  /** Every status worth listing at zero. Anything counted but missing is added to it. */
  vocabulary: string[];
  /** What is being counted, for the places with room to say it: `Actors`, `Workers`. */
  noun: string;
  /** True when the read failed, so nothing here is a count of anything. */
  unread?: boolean;
  /**
   * The bar's accessible name, announced with its breakdown. Not drawn: the legend
   * beneath already names every colour on it, and a heading over a card that is already
   * called "Actors" would only say it twice.
   */
  title: string;
  /** What this bar is counting, when it is not simply the whole scope. */
  caption?: string;
  emptyText: string;
  testId: string;
}) {
  const theme = useTheme();
  const { mode } = useThemeMode();
  const dark = mode === "dark";
  const palette = statusPalette(theme);
  // Short in the legend, where the swatch and the column already say what is counted.
  const read = (entry: SubstrateStatusCount) =>
    `${entry.status || "not reported"}: ${atAGlance(entry.count)}`;
  // Long wherever the reading stands on its own — `Suspended Actors: 6` rather than a
  // number under a status a tooltip has floated away from.
  const readFull = (entry: SubstrateStatusCount) =>
    `${entry.status || "Not reported"} ${noun}: ${atAGlance(entry.count)}`;

  /*
   * Counted by the word rather than by the wire value.
   *
   * A controller that has learned a state sends `Crashed` and one that has not sends
   * `ACTOR_STATE_CRASHED`; both read as `Crashed`, and keyed by the raw string they came
   * out as two entries — the legend listed `Crashed` twice, once at zero.
   */
  const merged = new Map<string, number>();
  for (const entry of counts) {
    const key = humanizeEnum(entry.status);
    merged.set(key, (merged.get(key) ?? 0) + entry.count);
  }

  /*
   * Grouped by status and ordered by the word, with everything parked pushed to the end.
   *
   * Idle is where a bar's dead weight belongs: a cluster that is mostly suspended reads as
   * a short band of activity against a long grey tail, rather than having the interesting
   * part cut in half by it. Sorted here rather than trusted from the server, because a bar
   * whose segments reorder between polls is a bar nobody can point at.
   */
  const order = (entries: SubstrateStatusCount[]) =>
    [...entries].sort((a, b) => {
      const parked = (entry: SubstrateStatusCount) => (statusTone(entry.status) === "idle" ? 1 : 0);
      return parked(a) - parked(b) || a.status.localeCompare(b.status);
    });
  const entries = [...merged].map(([status, count]) => ({ status, count }));
  const present = order(entries.filter((entry) => entry.count > 0));
  const total = present.reduce((sum, entry) => sum + entry.count, 0);
  const perActor = total > 0 && total <= ACTORS_DRAWN_INDIVIDUALLY;
  const summary = [caption, present.map(readFull).join(", ")].filter(Boolean).join(". ");

  // The one place a tone becomes two colours, so a legend key and the segment it explains
  // are the same colour by construction rather than by two expressions agreeing.
  const paint = (tone: StatusTone): CSSObject => ({
    /*
     * The pill's own three colours, not a mix of one of them with the page.
     *
     * Mixing toward the page is what turned these grey: every tone converges on the
     * background as the fill weakens, so at a subtle strength they all read as the same
     * washed-out slab. Taking the pill's fill and the pill's own border instead makes a
     * segment the same colour as the chip in the row below by construction, rather than
     * by two sets of numbers agreeing — and both are lighter than the mix was.
     */
    background: `color-mix(in srgb, ${palette[tone].color} var(--seg-fill), ${palette[tone].background})`,
    border: `1px solid ${palette[tone].borderColor}`,
  });

  const segment = (tone: StatusTone, key: string, grow: number, first: boolean, last: boolean) => (
    <div
      key={key}
      data-tone={tone}
      css={{
        /* The pill's own colour, as a wash behind its own outline. Both are mixed toward
           the page rather than used at full strength — which dims them on a dark page and
           lightens them on a light one, from one expression. At full strength eight of
           these is a row of paint chips.
           The strengths come from the track's own custom properties, so hovering the bar
           deepens every segment at once without any of them having to know the tone. */
        ...paint(tone),
        flexGrow: grow,
        flexBasis: 0,
        /* One crashed actor in 410,110 is 0.0002% of the width: without a floor it is not
           a pixel, let alone something to point at — and it is the most important thing
           on the bar. */
        minWidth: 6,
        height: 18,
        // Only the two ends are rounded, so the row reads as one bar rather than as a
        // line of separate lozenges.
        borderRadius: `${first ? 4 : 0}px ${last ? 4 : 0}px ${last ? 4 : 0}px ${first ? 4 : 0}px`,
        boxSizing: "border-box",
        transition: "background 120ms, border-color 120ms",
      }}
    />
  );

  const track = (
    <div
      data-testid={testId}
      /* The tooltip needs a pointer, which a screen reader has not got and a keyboard
         cannot produce. So the same summary is the bar's own name — colour and hover are
         never the only things carrying it. */
      role="img"
      aria-label={total === 0 ? emptyText : `${title}. ${summary}`}
      css={{
        display: "flex",
        gap: 3,
        minHeight: 18,
        /* Hover only: pointing at the bar reveals the breakdown, but nothing happens on
           press, and an active state would promise that it does.
           Deepening the mix rather than brightening it: `brightness` on a fill that is
           mostly page colour washes it out to the page instead of strengthening it, which
           on a light theme reads as the segments going transparent. */
        ":hover": { "--seg-fill": dark ? "30%" : "22%" },
      }}
    >
      {present
        .flatMap((entry) => {
          const tone = statusTone(entry.status);
          return perActor
            ? Array.from({ length: entry.count }, (_, i) => ({ tone, key: `${entry.status}-${i}`, grow: 1 }))
            : [{ tone, key: entry.status, grow: entry.count }];
        })
        .map((part, index, all) =>
          segment(part.tone, part.key, part.grow, index === 0, index === all.length - 1),
        )}
    </div>
  );

  /*
   * The legend, in the bar's own order and colours.
   *
   * The bar says the proportions and the legend says the numbers; between them a reader
   * gets both without hovering anything, which is what a tooltip alone cannot give
   * someone reading a screenshot or printing the page.
   */
  const keys = order(
    [...new Set([...vocabulary.map(humanizeEnum), ...merged.keys()])].map((status) => ({
      status,
      count: merged.get(status) ?? 0,
    })),
  );

  const legend = (
    <div
      data-testid={`${testId}-legend`}
      css={{ display: "flex", flexWrap: "wrap", gap: "2px 4px", marginTop: 8 }}
    >
      {keys.map((entry) => (
        <span
          key={entry.status}
          css={{
            display: "inline-flex",
            alignItems: "center",
            gap: 6,
            fontSize: 12,
            /* The padding and the radius are the same whether or not anything holds this
               status, and only the fill changes: a highlight that added weight or space
               would move every key beside it each time a count crossed zero, on a page
               that polls. */
            padding: "2px 8px",
            borderRadius: 6,
            // A key is something to read past, not text to drag through: selecting it while
            // sweeping the pointer along the row is never what anyone meant.
            userSelect: "none",
            background: entry.count === 0 ? "transparent" : theme.color.bgElevated,
          }}
        >
          {/* A status nothing is in is still worth listing, and still worth being the
              quietest thing here — but the fading is mostly the swatch's job. The text at
              the swatch's own opacity measured 3.79:1 on a light page, under AA; at 0.95
              it is 4.64:1 there and 7.20:1 on a dark one, and still visibly the quieter. */}
          <span
            aria-hidden
            css={{
              ...paint(statusTone(entry.status)),
              width: 10,
              height: 10,
              borderRadius: 3,
              opacity: entry.count === 0 ? 0.45 : 1,
            }}
          />
          <Text
            css={{
              color: entry.count === 0 ? theme.color.textMuted : theme.color.text,
              fontSize: 12,
              opacity: entry.count === 0 ? 0.95 : 1,
            }}
          >
            {read(entry)}
          </Text>
        </span>
      ))}
    </div>
  );

  return (
    /* The empty row keeps its height, with the reason beneath it. A bar that vanished when
       a search stopped matching would move the table under a reader at the moment they
       were reading why. */
    <div
      css={{
        marginBottom: 6,
        /* Declared here rather than on the bar, because the legend keys are painted from
           the same expressions and are the bar's siblings: on the track they resolved to
           nothing outside it, and every key came out invisible.

           At rest this is the pill's fill exactly; hovering pulls it toward the pill's own
           saturated colour, further on a dark page where the same step shows less. */
        "--seg-fill": "0%",
      }}
    >
      {total === 0 ? (
        <>
          {track}
          {/* Silent when the read failed: the banner above already says so, and "no actors
              in this scope" under a broken backend reports a healthy empty cluster. The
              legend stays either way — it is ten keys and two rows tall, and dropping it
              as the last actor drains moves the table under whoever is reading it. */}
          {unread ? null : (
            <Text
              data-testid={`${testId}-empty`}
              css={{ color: theme.color.textMuted, fontSize: 12, display: "block", marginTop: 8 }}
            >
              {emptyText}
            </Text>
          )}
          {legend}
        </>
      ) : (
        <Tooltip
          title={
            <>
              {caption ? <div css={{ opacity: 0.75 }}>{caption}</div> : null}
              {present.map((entry) => (
                <div key={entry.status}>{readFull(entry)}</div>
              ))}
            </>
          }
        >
          {/* The bar and its legend under one tooltip: they are the same reading, and a
              breakdown reachable from the chart but not from the key that explains it is
              a breakdown half the pointers on the page will miss. */}
          <div>
            {track}
            {legend}
          </div>
        </Tooltip>
      )}
    </div>
  );
}

/**
 * A section's name and how many rows are under it, which is worth knowing before
 * reading them.
 *
 * Both numbers whenever the rows on screen are not the whole answer — because a
 * search has narrowed them, or because they are one page of many. A bare count is
 * how a reader concludes their cluster has three actors when it is running four
 * hundred thousand, and with the lists paged that is now the *default* case rather
 * than an edge one.
 *
 * The total is the server's, never `rows.length`. That is the whole reason the
 * summary RPC exists: a page cannot count what it did not fetch.
 */
function SectionTitle({
  title,
  count,
  total,
}: {
  title: string;
  /** How many rows are on screen. */
  count: number;
  /** How many there are in total, counted server-side. */
  total?: number;
}) {
  const theme = useTheme();
  const narrowed = total !== undefined && total !== count;
  return (
    <Space size={8}>
      <span>{title}</span>
      <Text css={{ color: theme.color.textMuted, fontWeight: 400 }}>
        {narrowed ? `${count} of ${total.toLocaleString()}` : count}
      </Text>
    </Space>
  );
}

/**
 * A paged section's heading, which has two counts to tell apart.
 *
 * Unsearched, the honest sentence is "100 of 4,312": this page's rows, against the
 * total the summary counted server-side. Searched, it is "3 of 100 on this page" —
 * because the search reached one page, and rendering "3 of 4,312" would say it had
 * been run against the cluster. That second sentence is the one that matters: a
 * reader who searches for an actor sitting on page nine is told there are no matches
 * here, not that there are none.
 *
 * With no total at all — the summary failed while the page read succeeded, which is
 * why they are separate reads — the count keeps "on this page". A bare "100" is the
 * one thing this component exists to prevent: it is indistinguishable from a total,
 * and it would be claiming a cluster of 410,110 actors is running a hundred.
 */
function PagedSectionTitle({
  title,
  shown,
  onPage,
  total,
  searching,
}: {
  title: string;
  /** Rows after the search box. */
  shown: number;
  /** Rows the page arrived with. */
  onPage: number;
  /** Rows in scope across every page, counted server-side. */
  total?: number;
  searching: boolean;
}) {
  const theme = useTheme();
  const count = searching
    ? `${shown} of ${onPage} on this page`
    : total === undefined
      ? `${onPage} on this page`
      : total === onPage
        ? String(onPage)
        : `${onPage} of ${total.toLocaleString()}`;

  return (
    <Space size={8}>
      <span>{title}</span>
      <Text css={{ color: theme.color.textMuted, fontWeight: 400 }}>{count}</Text>
    </Space>
  );
}

/**
 * Narrows one section's rows by what the reader typed.
 *
 * Per section rather than one box for the page, because these four lists answer four
 * different questions: narrowing the actors to one template should not also empty the
 * table that says what that template is.
 *
 * Matching is a substring of everything the row shows, case-insensitively. A row's own
 * text is built by the caller so the search covers what is on screen — including the
 * parts a column composes, like a pod and its IP — rather than a field list that drifts
 * from the columns beside it.
 */
function filterRows<T>(
  rows: readonly T[],
  query: string,
  text: (row: T) => string,
): readonly T[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) => text(row).toLowerCase().includes(needle));
}

/**
 * A column comparator over whatever string the column shows.
 *
 * `localeCompare` rather than `<`, so a list of names sorts the way the reader reads
 * them. Every column gets one and every one carries a `multiple`, which is what makes
 * the tables multi-sortable: antd sorts by each active column in `multiple` order, so
 * shift-clicking Status then Template groups by status and orders within each group.
 */
function byText<T>(of: (row: T) => string) {
  return (a: T, b: T) => of(a).localeCompare(of(b));
}

/** The same, for a column showing a number, which must not sort as one. */
function byNumber<T>(of: (row: T) => number) {
  return (a: T, b: T) => of(a) - of(b);
}

/**
 * A paged table's rows: what arrived, and what is left of it after the search box.
 *
 * Both, because the heading needs to tell them apart — "3 of 100 on this page" is a
 * different claim from "100 of 4,312", and only one of them is true at a time.
 *
 * One hook for both tables rather than four memos, so the two cannot drift into
 * filtering or ordering by different rules. Memoised because this page can be polling:
 * filtering and sorting in the render body would run on every tick whether or not
 * anything changed.
 */
function usePagedRows<Row>(
  // Only the failure is read from the resource; the rows are passed separately
  // because which field holds them differs between the two.
  read: { error?: unknown },
  rows: readonly Row[] | undefined,
  query: string,
  text: (row: Row) => string,
  key: (row: Row) => string,
): { page: readonly Row[]; shown: Row[] } {
  // A failed read shows no rows: its banner says why, and leaving the previous page
  // underneath it would date the table without dating the message above it.
  const page = useMemo(
    () => (read.error ? [] : (rows ?? [])),
    [read.error, rows],
  );
  const shown = useMemo(
    () => orderedBy(filterRows(page, query, text), key),
    // `text` and `key` are declared inline by the caller, so they are new on every
    // render and deliberately not dependencies: what decides these rows is the page
    // and the term.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [page, query],
  );
  return { page, shown };
}

/**
 * The order a paged table's rows are in before a reader clicks anything.
 *
 * ate-api returns actors and workers in whatever order it holds them, which is not an
 * order: the same rows come back arranged differently from one read to the next, so a
 * page that polls has rows moving under the pointer while they are being read. antd
 * sorts by an active column and otherwise leaves `dataSource` alone, so this is what
 * `dataSource` has to arrive as.
 *
 * A copy, because `sort` is in place and the array belongs to the SWR cache.
 */
function orderedBy<T>(rows: readonly T[], key: (row: T) => string): T[] {
  return [...rows].sort((left, right) => key(left).localeCompare(key(right)));
}

/**
 * A section's search box.
 *
 * In the card's own corner rather than above the page: it belongs to the table it
 * filters, and a reader who has typed into it can see which list went quiet.
 */
function SectionSearch({
  label,
  testId,
  value,
  onChange,
}: {
  label: string;
  testId: string;
  value: string;
  onChange: (value: string) => void;
}) {
  const theme = useTheme();
  return (
    // The id is on a wrapper this app owns rather than on the control: antd spreads
    // unknown props onto its inner input, so an id there is an assertion about their
    // markup. The same reason the scope Select and the polling interval are wrapped.
    <div data-testid={testId}>
      <Input
        allowClear
        size="small"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        onClear={() => onChange("")}
        aria-label={label}
        placeholder="Search"
        prefix={<Search size={13} color={theme.color.textMuted} aria-hidden />}
        css={{ width: 200 }}
      />
    </div>
  );
}

/**
 * The Agent Substrate's own inventory.
 *
 * Four sections, in the order a reader needs them: the worker pools sandboxes run on and
 * the templates actors are cut from, both read from Kubernetes; then the actors placed
 * right now and the pods they are placed on, both read from ate-api. The split matters
 * more than it looks — the Kubernetes halves are complete whenever the request succeeded,
 * while the ate-api halves can be absent (`enabled: false`, no endpoint configured) or
 * partial (`ateApiError` on an otherwise successful response). Each of those is said in
 * the place it applies rather than as one banner over everything.
 */

/**
 * How many rows a paged section asks for.
 *
 * The controller's maximum, because these tables are virtualised and bounded in
 * height: a bigger page costs nothing to render and means fewer round trips for a
 * reader scrolling through actors. It is also how much the sort and the search below
 * cover, which is the other reason to ask for as many as allowed. Anything above 100
 * is refused outright rather than clamped.
 */
const PAGE_SIZE = 100;

/**
 * What the sort and the search on a paged table actually reach, said beside it.
 *
 * The claim this replaced was "Sorted across the whole inventory", which was true of
 * the read it stood over: that read fetched every actor and ordered all of them before
 * the browser sliced out a page. It could not survive a large cluster — one response
 * of 410,110 actors is roughly 43MB against gRPC's 16MB ceiling — so the read is a
 * page now, and the sentence has to be.
 *
 * What replaced it is narrower and true: the columns sort the hundred rows in front of
 * the reader, and the search box narrows the same hundred. ate-api offers paging and
 * nothing else — no order, no filter — so ordering the cluster would mean reading the
 * cluster, which is the thing that could not be done. Saying so is what keeps a reader
 * from concluding, from an empty search, that their cluster has no such actor.
 *
 * The age is here for a related reason: a page that showed a stale answer while
 * claiming to poll would be the polling bug this codebase has already shipped once.
 */
function PageScopeNote({
  computedAt,
  testId,
}: {
  computedAt?: string;
  testId: string;
}) {
  const theme = useTheme();
  const age = useDataAge(computedAt);

  /*
   * Rendered whether or not there is an age, unlike the age-only note this replaced.
   * What the sort and the search reach is true of the page regardless of when it was
   * read, and it is the sentence keeping a reader from taking "no matches" on one page
   * for "no such actor" in the cluster. The age is what it can go without.
   */
  return (
    <Text
      data-testid={testId}
      css={{ color: theme.color.textMuted, fontSize: 12 }}
    >
      Sorting and search apply to this page only
      {age ? ` · ${age}` : ""}
    </Text>
  );
}

function useDataAge(computedAt: string | undefined): string {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 500);
    return () => window.clearInterval(timer);
  }, []);

  if (!computedAt) return "";
  const at = new Date(computedAt).getTime();
  if (Number.isNaN(at)) return "";
  const seconds = Math.max(0, (now - at) / 1000);
  if (seconds < 1) return "read just now";
  return `read ${seconds.toFixed(1)}s ago`;
}

/**
 * An ate-api failure that reached one page and not the whole read.
 *
 * `ListSubstrateActors` answers with this string rather than failing, for the same
 * reason the summary does: the call succeeded, and only the runtime half of it is
 * missing. Without a sentence here that page is a short or empty table beside a tile
 * reporting four hundred thousand actors, which reads as a bug in the tile.
 *
 * The rows below it may be there or may not. A page is filled from several ate-api
 * pages when a namespace narrows it, so a failure part-way keeps what it had already
 * collected — which is why this says the read did not finish rather than that it
 * failed outright.
 */
function PageWarning({ message, testId }: { message: string; testId: string }) {
  return (
    <Alert
      type="warning"
      showIcon
      title="ate-api could not finish reading this page"
      description={message}
      data-testid={testId}
    />
  );
}


/**
 * The Agent Substrate's own inventory.
 *
 * Four sections, in the order a reader needs them: the worker pools sandboxes run on
 * and the templates actors are cut from, both read from Kubernetes; then the actors
 * placed right now and the pods they are placed on, both read from ate-api. The split
 * matters more than it looks — the Kubernetes halves are complete whenever the request
 * succeeded, while the ate-api halves can be absent (`enabled: false`, no endpoint
 * configured) or partial (`ateApiError` on an otherwise successful response). Each of
 * those is said in the place it applies rather than as one banner over everything.
 *
 * ## Three reads, not one
 *
 * This page used to make a single call for the whole inventory, and it stopped
 * working: `GetSubstrateStatus` answers with every actor and every worker in one
 * message, and on a cluster reporting 410,110 actors that is a message gRPC refuses
 * to send — *"trying to send message larger than max"*. The page reported it honestly
 * as a failed read, which was right, and the read could not succeed.
 *
 * So it reads three things:
 *
 * - **the summary** (`GetSubstrateSummary`), for the tiles and for the two lists that
 *   are inherently small — worker pools and actor templates ride inline;
 * - **a page of actors** (`ListSubstrateActors`) and **a page of workers**
 *   (`ListSubstrateWorkers`), each passing a token through to ate-api's own paging.
 *
 * The split was cosmetic until the API caught up: all three used to call
 * `GetSubstrateStatus`, so a page of a hundred rows still cost the whole inventory and
 * still failed at the same size. Three reads that page are what makes it real.
 *
 * ## Where the counts come from, and why it matters
 *
 * Every total on this page is the summary's, counted server-side. None of them is
 * `rows.length`. With the lists paged, counting what arrived and calling it a total
 * would report a hundred actors for a cluster running four hundred thousand — the
 * exact failure the "3 of 4" rendering already existed to prevent, made far more
 * likely by paging.
 *
 * ## What the sorts and the searches reach
 *
 * The worker pool and actor template tables arrive whole, so sorting or searching
 * them covers all of them.
 *
 * The actor and worker tables do not, and their sort and search cover one page. That
 * is not a shortcut: ate-api's `ListActors` takes a page size and a token and offers
 * no order and no filter, so ordering or narrowing the cluster would mean reading the
 * cluster first — the read that could not succeed. Both tables say so beneath
 * themselves, both headings distinguish "3 of 100 on this page" from "100 of 4,312",
 * and an empty search says which pages it looked at. A reader who is told "no matches"
 * without being told what was searched will conclude the actor does not exist.
 */
export function SubstratePage() {
  const theme = useTheme();
  const [searchParams, setSearchParams] = useSearchParams();

  /**
   * The scope, from the URL rather than from state.
   *
   * So that a link to what someone is looking at is a link to what they are looking
   * at, and — the reason it is not remembered in storage — so one address is never
   * two different pages.
   */
  const namespace = searchParams.get(NAMESPACE_PARAM) ?? ALL_NAMESPACES;
  const scope = namespace === ALL_NAMESPACES ? undefined : namespace;

  const namespaces = useNamespaces();
  const summary = useSubstrateSummary(scope);

  /*
   * One search per section, and all four of them are applied here.
   *
   * What each one reaches is not the same, which is why the two paged tables say so
   * beside them. Worker pools and actor templates arrive whole in the summary, so a
   * search over them is a search over all of them. Actors and workers arrive one page
   * at a time — ate-api has no filter to push a search into, and pushing one into the
   * controller would mean reading every actor to apply it — so those two searches
   * narrow the page, and nothing here pretends otherwise.
   */
  const [poolQuery, setPoolQuery] = useState("");
  const [templateQuery, setTemplateQuery] = useState("");
  const [actorQuery, setActorQuery] = useState("");
  const [workerQuery, setWorkerQuery] = useState("");

  /*
   * Only the scope resets the page stacks.
   *
   * The sort and the search used to be in these keys, because both were part of the
   * read: each header click re-fetched the whole inventory to reorder rows the browser
   * was already holding. Neither travels now, so neither invalidates a token — a
   * reorder is a re-render.
   */
  const actorPage = usePageStack(namespace);
  const workerPage = usePageStack(namespace);

  const actors = useSubstrateActors({
    namespace: scope,
    limit: PAGE_SIZE,
    pageToken: actorPage.current,
  });
  const workers = useSubstrateWorkers({
    namespace: scope,
    limit: PAGE_SIZE,
    pageToken: workerPage.current,
  });

  /*
   * Off by default, and deliberately not remembered.
   *
   * Twice a second is a rate to watch something at, not a rate to leave a page at: a
   * remembered setting would have a tab left open in the background — or reopened
   * tomorrow — asking the controller for the inventory 7,000 times an hour for nobody.
   * Switching it on is cheap, so it is asked for each time it is wanted.
   */
  const [isPolling, setPolling] = useState(false);
  const [pollSeconds, setPollSeconds] = useState<number | null>(DEFAULT_POLL_SECONDS);
  const [behindAt, setBehindAt] = useState<number>();
  const pollMs = pollIntervalMs(pollSeconds);
  const isTicking = isPolling && pollMs !== undefined;
  const isBehind = isTicking && behindAt === pollMs;

  const isRefreshing =
    !isTicking &&
    (summary.isValidating ||
      actors.isValidating ||
      workers.isValidating ||
      namespaces.isValidating);

  /** What Refresh re-reads: the whole page, the list of namespaces included. */
  async function refreshAll(): Promise<void> {
    await Promise.all([
      summary.refresh(),
      actors.refresh(),
      workers.refresh(),
      namespaces.refresh(),
    ]);
  }

  /*
   * The timer lives here rather than in the data hooks, and re-reads the inventory
   * only.
   *
   * Driven from the page because `refresh` fetches directly, where the caching
   * layer's polling goes through revalidation — and revalidation is deduplicated by a
   * window that outlasts the interval, so asking it for twice a second produced a
   * read every two and a half. The page reported it was polling and it was not, which
   * is worse than not offering it.
   *
   * The namespace list is left alone: it is the page's scope control, not its data,
   * and it does not change twice a second.
   */
  /*
   * Not memoised, deliberately: the ref below is reassigned on every render, so this
   * is rebuilt each time either way — and a `useCallback` over three hook objects
   * would either capture a stale one or list dependencies that change every render,
   * which is the memoisation doing nothing while claiming to.
   */
  /*
   * All three together, including the expensive one.
   *
   * The summary is documented as the read to poll least often, and this ticks it at
   * the reader's chosen interval alongside the two cheap ones. That is deliberate: the
   * tiles and the rows are one picture, and totals that held still while the table
   * beneath them moved would be two moments shown as one. `isTickInFlight` drops a
   * tick that lands while the last is still running, so on a cluster where the walk
   * takes seconds the whole page settles to the summary's cadence rather than queueing
   * — which is the honest cost of keeping them in step, and the reason the floor on
   * the interval exists.
   */
  const refreshInventory = async () => {
    await Promise.all([summary.refresh(), actors.refresh(), workers.refresh()]);
  };

  const refreshRef = useRef(refreshInventory);
  // Assigned in an effect rather than during render: a ref written while rendering is
  // a value React is entitled to discard, and the lint rule that says so is right.
  useEffect(() => {
    refreshRef.current = refreshInventory;
  });
  const isTickInFlight = useRef(false);

  useEffect(() => {
    if (!isTicking || pollMs === undefined) return;

    const timer = window.setInterval(() => {
      // A tick that lands while the last one is still running is dropped rather than
      // stacked: against a backend slower than the interval, queueing would turn a
      // live view into a growing backlog of requests nobody is waiting for.
      if (isTickInFlight.current) {
        setBehindAt(pollMs);
        return;
      }
      isTickInFlight.current = true;
      void refreshRef
        .current()
        .catch(() => {
          // A failed read is already on screen as an error beside the data it belongs
          // to; there is nothing for the timer to add, and it must keep going either
          // way.
        })
        .finally(() => {
          isTickInFlight.current = false;
        });
    }, pollMs);

    return () => window.clearInterval(timer);
  }, [isTicking, pollMs]);

  // A failure has its own banner; leaving the rows and the counts out keeps the rest
  // of the page from also claiming the cluster is running nothing.
  const inventory = summary.error ? undefined : summary.data;
  const unread = summary.error ? "Could not be read" : undefined;

  /*
   * The two inline lists, filtered here because they arrive here whole.
   *
   * Memoised because this page can be polling: filtering inside the render would run
   * on every tick whether or not anything changed.
   */
  const pools = useMemo(
    () =>
      filterRows(inventory?.workerPools ?? [], poolQuery, (pool) =>
        [pool.namespace, pool.name, String(pool.replicas), pool.ateomImage].join(" "),
      ),
    [inventory?.workerPools, poolQuery],
  );

  const templates = useMemo(
    () =>
      filterRows(inventory?.actorTemplates ?? [], templateQuery, (template) =>
        [
          template.namespace,
          template.name,
          template.goldenActorId,
          template.phase,
          template.sandboxClass,
          template.workerSelector,
          template.harnessName,
        ]
          .filter(Boolean)
          .join(" "),
      ),
    [inventory?.actorTemplates, templateQuery],
  );

  /*
   * The page of actors, and what is left of it after the search box.
   *
   * Both are kept, because the heading needs to say which is which: "3 of 100 on this
   * page" is a different claim from "100 of 4,312", and only one of them is true at a
   * time. Memoised because this page can be polling — filtering in the render body
   * would run on every tick whether or not anything changed.
   */
  const { page: actorPageRows, shown: actorRows } = usePagedRows(
    actors,
    actors.data?.actors,
    actorQuery,
    (actor) =>
      [
        actor.actorId,
        actor.status,
        actor.actorTemplateNamespace,
        actor.actorTemplateName,
        actor.ateomPodNamespace,
        actor.ateomPodName,
        actor.ateomPodIp,
      ]
        .filter(Boolean)
        .join(" "),
    // Status, then id. Two keys because the second breaks ties in the first: with
    // status alone, two Running actors could swap places between polls.
    (actor) => `${actor.status}\u0000${actor.actorId}`,
  );

  const { page: workerPageRows, shown: workerRows } = usePagedRows(
    workers,
    workers.data?.workers,
    workerQuery,
    (worker) =>
      [worker.workerNamespace, worker.workerPool, worker.workerPod, worker.ip]
        .filter(Boolean)
        .join(" "),
    (worker) => `${worker.workerPool}\u0000${worker.workerNamespace}/${worker.workerPod}`,
  );

  /*
   * What the bar above the actor table counts.
   *
   * Unfiltered it is the summary's own counts, which is the only honest source of a whole
   * cluster: the table holds one page, and a page counted and drawn as the cluster would
   * report eight actors for a deployment running 410,110.
   *
   * A search has no server-side breakdown, so the matches are counted here from the rows
   * that came back — and those are also a page. `actorBarCaption` is what stops the bar
   * claiming the rest: it says how many of the matches are actually in it.
   */
  const actorBar = useMemo(() => {
    const query = actorQuery.trim();
    if (!query) {
      return {
        counts: inventory?.actorStatusCounts ?? [],
        caption: undefined as string | undefined,
      };
    }
    const byStatus = new Map<string, number>();
    for (const actor of actorRows) {
      byStatus.set(actor.status, (byStatus.get(actor.status) ?? 0) + 1);
    }
    return {
      counts: [...byStatus].map(([status, count]) => ({ status, count })),
      /*
       * "on this page", where this used to say how many of the matches were shown.
       *
       * It could say that while the search was the server's and `totalSize` came back
       * with the matches across every page. ate-api has no filter to push a search into,
       * so there is no such number any more: what is counted here is what is in front of
       * the reader, and the caption has to be the one that cannot be read as the cluster.
       */
      caption: `Matching “${query}” on this page: ${atAGlance(actorRows.length)} of ${atAGlance(actorPageRows.length)}`,
    };
  }, [actorQuery, actorRows, actorPageRows.length, inventory?.actorStatusCounts]);
  /*
   * The tiles, from the summary's own counts.
   *
   * Not derived from the rows on screen, and that is the point of the summary
   * existing: the rows are one page, and a page counted as a total is how a cluster
   * running 410,110 actors gets reported as running 100.
   */
  const readyTemplates = useMemo(() => {
    let ready = 0;
    for (const template of inventory?.actorTemplates ?? []) {
      if (template.phase?.toLowerCase() === "ready") ready += 1;
    }
    return ready;
  }, [inventory?.actorTemplates]);

  const mono = useMemo(
    () => ({ fontFamily: theme.font.mono, fontSize: 12 }),
    [theme.font.mono],
  );
  const muted = useMemo(() => ({ color: theme.color.textMuted }), [theme.color.textMuted]);

  /** `namespace/name`, with the namespace quieter than the name it qualifies. */
  const qualified = useCallback(
    (ns: string | undefined, name: string) => (
      <span css={mono}>
        {ns ? <span css={muted}>{ns}/</span> : null}
        {name}
      </span>
    ),
    [mono, muted],
  );

  /*
   * Every column of the two inline tables sorts, and every sorter carries a
   * `multiple` — antd applies each active sorter in that order, so shift-clicking two
   * headers sorts by both. The numbers are a fixed priority rather than click order,
   * so they are chosen to put the column worth *grouping* by first.
   *
   * A comparator here rather than a read, because these two lists arrive whole: the
   * summary carries every pool and every template, so sorting them in the browser
   * sorts all of them. The paged tables below wear the same header and reach only
   * their own page — see `PageScopeNote`.
   */
  const workerPoolColumns: ColumnsType<SubstrateWorkerPoolEntry> = useMemo(
    () => [
      {
        title: "Pool",
        key: "pool",
        sorter: { compare: byText((pool) => `${pool.namespace}/${pool.name}`), multiple: 3 },
        render: (_, pool) => qualified(pool.namespace, pool.name),
      },
      {
        title: "Replicas",
        key: "replicas",
        width: 110,
        // Numerically: as text, 10 replicas sort before 9.
        sorter: { compare: byNumber((pool) => pool.replicas), multiple: 2 },
        render: (_, pool) => pool.replicas,
      },
      {
        title: "Ateom image",
        key: "ateomImage",
        sorter: { compare: byText((pool) => pool.ateomImage), multiple: 1 },
        // The image tag is what an operator checks against a release, so it is not
        // truncated.
        render: (_, pool) => (
          <Text css={{ ...mono, ...muted, wordBreak: "break-all" }}>{pool.ateomImage}</Text>
        ),
      },
    ],
    [mono, muted, qualified],
  );

  const actorTemplateColumns: ColumnsType<SubstrateActorTemplateEntry> = useMemo(
    () => [
      {
        title: "Template",
        key: "template",
        sorter: { compare: byText((t) => `${t.namespace}/${t.name}`), multiple: 5 },
        render: (_, template) => (
          <div>
            {qualified(template.namespace, template.name)}
            {/* The golden actor is the snapshot every new actor of this template is
                cut from, so it is the one identifier worth carrying beside the name. */}
            {template.goldenActorId ? (
              <Text css={{ ...mono, ...muted, display: "block" }}>
                golden: {template.goldenActorId}
              </Text>
            ) : null}
          </div>
        ),
      },
      {
        title: "Phase",
        key: "phase",
        width: 130,
        sorter: { compare: byText((t) => t.phase ?? ""), multiple: 4 },
        render: (_, template) => <StatusChip label={template.phase ?? ""} />,
      },
      {
        title: "Sandbox class",
        key: "sandboxClass",
        width: 140,
        sorter: { compare: byText((t) => t.sandboxClass ?? ""), multiple: 3 },
        render: (_, template) => template.sandboxClass ?? "—",
      },
      {
        title: "Worker selector",
        key: "workerSelector",
        sorter: { compare: byText((t) => t.workerSelector ?? ""), multiple: 2 },
        render: (_, template) =>
          template.workerSelector ? (
            <Text css={{ ...mono, ...muted }}>{template.workerSelector}</Text>
          ) : (
            "—"
          ),
      },
      {
        // Text and not a link: the agents list has no namespace filter to send a
        // reader to, so a link here would land them on an unfiltered page and imply
        // otherwise.
        title: "Harness",
        key: "harness",
        sorter: { compare: byText((t) => t.harnessName ?? ""), multiple: 1 },
        render: (_, template) => template.harnessName ?? "—",
      },
    ],
    [mono, muted, qualified],
  );

  /*
   * The paged tables sort the same way the two inline ones do, and reach less by it.
   *
   * A comparator rather than `sorter: true`. The `true` form gives a column antd's
   * header while leaving the table nothing to reorder, which was right while a header
   * click was a new read: the read ordered every actor in the cluster and handed back
   * a slice of the ordering. That read cannot survive a large cluster, so a comparator
   * over the rows in hand is what is left — and what the note beneath the table says.
   *
   * `multiple` for the same reason as the inline tables, though not by the mechanism
   * "multi-sort" suggests: antd reads no modifier key. `triggerSorter` appends to the
   * active sorters whenever the clicked column and the current head both carry a
   * number, so *any* second header click adds to the sort rather than replacing it,
   * and the rows stay grouped by the higher number — sorters run in descending
   * `multiple`. Status leads on the actors and pool on the workers, because those are
   * the columns worth grouping by.
   *
   * The cost is that there is no single click that sorts by one of the other columns
   * alone; the leading column has to be cycled off first. All four tables on this page
   * behave that way, which is the only reason it is left as it is.
   */
  const actorColumns: ColumnsType<SubstrateActorEntry> = useMemo(
    () => [
      {
        title: "Actor",
        key: "actorId",
        sorter: { compare: byText((actor) => actor.actorId), multiple: 1 },
        width: 300,
        render: (_, actor) => <span css={mono}>{actor.actorId}</span>,
      },
      {
        title: "Status",
        key: "status",
        sorter: { compare: byText((actor) => actor.status), multiple: 4 },
        // Wide enough for the longest status seen on a real cluster
        // (`ACTOR_STATE_CRASHED`) without wrapping it to three lines.
        width: 130,
        render: (_, actor) => <StatusChip label={actor.status} />,
      },
      {
        title: "Template",
        key: "template",
        sorter: {
          compare: byText(
            (actor) =>
              `${actor.actorTemplateNamespace ?? ""}/${actor.actorTemplateName ?? ""}`,
          ),
          multiple: 3,
        },
        width: 240,
        render: (_, actor) =>
          actor.actorTemplateName
            ? qualified(actor.actorTemplateNamespace, actor.actorTemplateName)
            : "—",
      },
      {
        title: "Worker pod",
        key: "workerPod",
        sorter: {
          compare: byText(
            (actor) => `${actor.ateomPodNamespace ?? ""}/${actor.ateomPodName ?? ""}`,
          ),
          multiple: 2,
        },
        width: 260,
        render: (_, actor) =>
          actor.ateomPodName ? (
            /* One line, always. A pod name and an IP together outrun the column, and
               wrapping them made the row two lines tall — which moves every row under it,
               on a page that polls. It runs into the slack on its right instead. */
            <Text css={{ ...mono, ...muted, whiteSpace: "nowrap" }}>
              {actor.ateomPodNamespace ?? ""}/{actor.ateomPodName}
              {actor.ateomPodIp ? ` · ${actor.ateomPodIp}` : ""}
            </Text>
          ) : (
            "—"
          ),
      },
    ],
    [mono, muted, qualified],
  );

  /*
   * The same, for the workers.
   *
   * There is no Actor column, and that is not an omission. ate-api's `Worker` carries
   * capacity and allocation and no actor reference: the binding lives on the *actor*,
   * so the only way to fill that column is to read every actor in the cluster and join
   * — the read this page stopped doing. The column stood here reading "idle" for every
   * worker on every real cluster, and looked populated only against a fixture that had
   * invented the field. How much of the fleet is busy is on a tile instead, where the
   * summary counts it once.
   */
  const workerColumns: ColumnsType<SubstrateWorkerEntry> = useMemo(
    () => [
      {
        title: "Pod",
        key: "pod",
        sorter: {
          compare: byText((worker) => `${worker.workerNamespace}/${worker.workerPod}`),
          multiple: 2,
        },
        width: 420,
        render: (_, worker) => qualified(worker.workerNamespace, worker.workerPod),
      },
      {
        title: "Pool",
        key: "pool",
        sorter: { compare: byText((worker) => worker.workerPool), multiple: 3 },
        width: 260,
        render: (_, worker) => worker.workerPool,
      },
      {
        title: "IP",
        key: "ip",
        sorter: { compare: byText((worker) => worker.ip ?? ""), multiple: 1 },
        width: 200,
        render: (_, worker) =>
          worker.ip ? (
            <Text css={{ ...mono, ...muted }}>{worker.ip}</Text>
          ) : (
            <Text css={muted}>—</Text>
          ),
      },
    ],
    [mono, muted, qualified],
  );

  /*
   * Whether ate-api is configured, from whichever read answered.
   *
   * Not the summary alone: it is the expensive read of the three — a walk of every
   * ate-api page — so it is the one most likely to fail, and `inventory` is undefined
   * whenever it does. Deciding from it alone told a reader whose summary timed out that
   * their controller had no ate-api endpoint, which is a different problem with a
   * different fix. The two page reads carry the same flag and are cheap.
   */
  const ateApiEnabled =
    actors.data?.enabled ?? workers.data?.enabled ?? inventory?.enabled ?? false;

  return (
    <PageFrame
      title="Substrate"
      description="Worker pools and actor templates from Kubernetes, plus live actors and worker assignments from ate-api."
      actions={
        <Space size={8}>
          <Tooltip
            title={
              isTicking
                ? `Re-reading the inventory every ${pollSeconds}s, so an actor moving between workers is visible as it happens.`
                : "Re-read the inventory on a timer, so an actor moving between workers is visible as it happens."
            }
          >
            <Button
              type={isTicking ? "primary" : "default"}
              aria-pressed={isPolling}
              onClick={() => setPolling((on) => !on)}
              data-testid="substrate-poll-toggle"
              icon={<Radio size={14} aria-hidden />}
            >
              Request polling: {isPolling ? "enabled" : "disabled"}
            </Button>
          </Tooltip>

          {/* Only while polling is on: an interval with nothing to drive is a control
              that reads as switched on when nothing is happening. */}
          {isPolling ? (
            <Tooltip
              title={`How often to re-read, in seconds. ${MIN_POLL_SECONDS}s is as fast as this page will ask; 0 stops without switching polling off.`}
            >
              {/* The test id is on a wrapper rather than on the control, the same as
                  the scope Select below: antd renders its own tree underneath and
                  spreads unknown props onto the inner input, so an id put here would
                  be asserting on their markup rather than ours. */}
              <div data-testid="substrate-poll-interval">
                <InputNumber
                  aria-label="Polling interval in seconds"
                  value={pollSeconds}
                  onChange={setPollSeconds}
                  /* The floor lands on blur rather than on every keystroke: applied as
                     typed, "0.1" would jump to "0.5" between the "." and the "1" and
                     fight the person entering it. */
                  onBlur={() =>
                    setPollSeconds((seconds) =>
                      seconds !== null && seconds > 0 && seconds < MIN_POLL_SECONDS
                        ? MIN_POLL_SECONDS
                        : seconds,
                    )
                  }
                  min={0}
                  /* No `step`: antd derives a display precision from it, which
                     rendered the default as "1.0". */
                  precision={undefined}
                  css={{ width: 132 }}
                  /* Singular for exactly one, because "1 seconds" beside a number the
                     reader chose looks like the page is not reading its own value.
                     `suffix` rather than `addonAfter`: antd 6 deprecates the latter. */
                  suffix={pollSeconds === 1 ? "second" : "seconds"}
                  status={isPolling && !isTicking ? "warning" : undefined}
                />
              </div>
            </Tooltip>
          ) : null}

          {isTicking && isBehind ? (
            <Tooltip title="The reads are taking longer than the interval, so some ticks are skipped rather than queued. A narrower scope, or a longer interval, gets an honest rate.">
              <Text
                data-testid="substrate-poll-behind"
                css={{ color: theme.color.warning, fontSize: 12 }}
              >
                reads slower than this rate
              </Text>
            </Tooltip>
          ) : null}

          <RefreshButton onRefresh={refreshAll} what="Substrate" loading={isRefreshing} />
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        <Space size={8}>
          <Text css={muted}>Scope</Text>
          {/* The test id is on a wrapper rather than on the Select, because antd
              renders its own tree underneath and a prop that survives today is not
              something to assert on. The wrapper is this app's own markup. */}
          <div data-testid="substrate-namespace">
            <Select
              css={{ minWidth: 240 }}
              value={namespace}
              loading={namespaces.isLoading}
              onChange={(value: string) => {
                const next = new URLSearchParams(searchParams);
                if (value === ALL_NAMESPACES) next.delete(NAMESPACE_PARAM);
                else next.set(NAMESPACE_PARAM, value);
                // Replaced rather than pushed: changing scope is refining one
                // question, and a Back button that walks every refinement is one
                // nobody can use to leave the page.
                setSearchParams(next, { replace: true });
              }}
              options={[
                // First, and the default, because the substrate is a cluster-wide
                // thing and an operator arriving here wants to know what is running at
                // all.
                { value: ALL_NAMESPACES, label: "All watched namespaces" },
                ...(namespaces.data ?? []).map((entry) => ({
                  value: entry.name,
                  // A namespace that is going away can still hold actors, so it is
                  // offered — with its condition said out loud rather than left for
                  // the reader to wonder about when the tables come back empty.
                  label:
                    entry.status === "Active"
                      ? entry.name
                      : `${entry.name} (${entry.status.toLowerCase()})`,
                })),
              ]}
            />
          </div>
        </Space>

        {namespaces.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load the list of namespaces"
            description={`${namespaces.error.message} You can still name a namespace in this page's address.`}
            data-testid="substrate-namespaces-error"
            action={
              <Button size="small" onClick={() => void namespaces.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {summary.error ? (
          <Alert
            type="error"
            showIcon
            title="Substrate inventory could not be read"
            description={summary.error.message}
            data-testid="substrate-inventory-error"
            action={
              <Button size="small" onClick={() => void summary.refresh()}>
                Try again
              </Button>
            }
          />
        ) : inventory?.ateApiError ? (
          /* A warning beside the data rather than an error instead of it. The read
             succeeded and the Kubernetes-derived halves are complete; only the runtime
             ones may be short. Flattening this into the error above would tell an
             operator their substrate was broken when part of it answered fine. */
          <Alert
            type="warning"
            showIcon
            title="Runtime actor state is incomplete"
            description={`Worker pools come from Kubernetes and are complete. Everything else on this page — the actor templates as well as the actors and workers below — comes from ate-api, which answered with an error, so those may be short: ${inventory.ateApiError}`}
            data-testid="substrate-partial"
          />
        ) : null}

        <div
          css={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))",
            gap: theme.space(4),
          }}
        >
          <StatTile
            label="Worker pools"
            testId="substrate-stat-pools"
            value={inventory?.workerPools.length}
            isLoading={summary.isLoading}
            hint={unread}
          />
          {/* Both halves of each ratio, because the useful question is never the count
              on its own: three templates is good news or bad depending on how many of
              them came up. */}
          <StatTile
            label="Templates ready"
            testId="substrate-stat-templates"
            value={
              inventory
                ? `${readyTemplates}/${inventory.actorTemplates.length}`
                : undefined
            }
            isLoading={summary.isLoading}
            hint={unread}
          />
          <StatTile
            label="Actors running"
            testId="substrate-stat-actors"
            value={
              inventory
                ? `${inventory.runningActorCount.toLocaleString()}/${inventory.actorCount.toLocaleString()}`
                : undefined
            }
            isLoading={summary.isLoading}
            hint={unread}
          />
          <StatTile
            label="Workers busy"
            testId="substrate-stat-workers"
            value={
              inventory
                ? `${inventory.busyWorkerCount.toLocaleString()}/${inventory.workerCount.toLocaleString()}`
                : undefined
            }
            isLoading={summary.isLoading}
            hint={unread}
          />
          <StatTile
            label="Scope"
            testId="substrate-stat-scope"
            // Not read from the response — it is what this page asked for, which is
            // known even when the read failed, and is the thing that explains an empty
            // table.
            value={namespace === ALL_NAMESPACES ? "all" : namespace}
          />
        </div>

        <Card
          title={
            <SectionTitle
              title="Worker pools"
              count={pools.length}
              total={inventory?.workerPools.length ?? 0}
            />
          }
          extra={
            <Space size={8}>
              <SectionSearch
                label="Search worker pools"
                testId="substrate-pools-search"
                value={poolQuery}
                onChange={setPoolQuery}
              />
            </Space>
          }
          data-testid="substrate-pools-card"
        >
          <Table<SubstrateWorkerPoolEntry>
            data-testid="substrate-pools-table"
            rowKey={(pool) => `${pool.namespace}/${pool.name}`}
            columns={workerPoolColumns}
            dataSource={pools}
            loading={summary.isLoading}
            pagination={false}
            size="small"
            locale={{
              emptyText: poolQuery.trim()
                ? "No worker pools match your search."
                : "No worker pools in this scope. Create one in the cluster, or install one with the Helm chart.",
            }}
          />
        </Card>

        <Card
          title={
            <SectionTitle
              title="Actor templates"
              count={templates.length}
              total={inventory?.actorTemplates.length ?? 0}
            />
          }
          extra={
            <Space size={8}>
              <SectionSearch
                label="Search actor templates"
                testId="substrate-templates-search"
                value={templateQuery}
                onChange={setTemplateQuery}
              />
            </Space>
          }
          data-testid="substrate-templates-card"
        >
          <Table<SubstrateActorTemplateEntry>
            data-testid="substrate-templates-table"
            rowKey={(template) => `${template.namespace}/${template.name}`}
            columns={actorTemplateColumns}
            dataSource={templates}
            loading={summary.isLoading}
            pagination={false}
            size="small"
            locale={{
              emptyText: templateQuery.trim()
                ? "No actor templates match your search."
                : "No actor templates yet. One appears when you create a harness and an agent template.",
            }}
          />
        </Card>

        <Card
          title={
            <PagedSectionTitle
              title="Actors"
              shown={actorRows.length}
              onPage={actorPageRows.length}
              total={inventory?.actorCount}
              searching={Boolean(actorQuery.trim())}
            />
          }
          extra={
            <Space size={8}>
              <SectionSearch
                label="Search actors"
                testId="substrate-actors-search"
                value={actorQuery}
                onChange={setActorQuery}
              />
            </Space>
          }
          data-testid="substrate-actors-card"
        >
          {actors.error ? (
            <Alert
              type="error"
              showIcon
              title="Actors could not be read"
              description={actors.error.message}
              data-testid="substrate-actors-error"
              action={
                <Button size="small" onClick={() => void actors.refresh()}>
                  Try again
                </Button>
              }
            />
          ) : actors.data?.ateApiError ? (
            <PageWarning
              message={actors.data.ateApiError}
              testId="substrate-actors-partial"
            />
          ) : null}

          <StatusBar
            testId="substrate-actor-status-counts"
            title="Actor status"
            vocabulary={ACTOR_STATES}
            noun="Actors"
            unread={Boolean(summary.error || actors.error)}
            counts={actorBar.counts}
            caption={actorBar.caption}
            emptyText={
              actorQuery.trim()
                ? "No actors on this page match your search."
                : ateApiEnabled
                  ? "No actors in this scope."
                  : "ate-api is not configured, so there are no actors to show."
            }
          />

          <Table<SubstrateActorEntry>
            data-testid="substrate-actors-table"
            rowKey={(actor) => actor.actorId}
            columns={actorColumns}
            dataSource={actorRows}
            loading={actors.isLoading}
            /* antd's own pager is off because the pages come from the server by token,
               not by number — `PageControls` below turns them. */
            pagination={false}
            virtual
            /* The sum of the column widths, so the table asks for exactly what it uses:
               a wider `x` reserves space no column wants and scrolls the card for it. */
            scroll={{ y: GROWING_TABLE_HEIGHT, x: 930 }}
            size="small"
            /* Three different sentences, because they are three different facts and
               only one is something to act on: a controller with no ate-api endpoint
               is a deployment choice, a configured one reporting nothing means the
               actors really are not there, and a search that matched nothing is the
               reader's own doing. */
            locale={{
              emptyText: actors.error
                ? " "
                : actorQuery.trim()
                  ? "No actors on this page match your search. Other pages are not searched."
                  : actors.data?.nextPageToken
                    ? "No actors in this scope on this page. There are more pages — use Next to keep looking."
                    : ateApiEnabled
                      ? "ate-api reported no actors in this scope."
                    : "ate-api is not configured on this controller. Set substrate-ate-api-endpoint to see live actors.",
            }}
          />

          {/* One row, the way antd lays out a paged table: what the page is on the
              left, the controls to turn it on the right. `PageControls` carries its
              own top margin for the stacked layout it was written for, which here
              would drop it below the sentence it sits beside — so the row owns the
              spacing and the control's own is cleared. */}
          <div
            css={{
              alignItems: "center",
              display: "flex",
              gap: theme.space(4),
              justifyContent: "space-between",
              marginTop: theme.space(3),
              "& > [data-testid$='-pages']": { marginTop: 0 },
            }}
          >
            <PageScopeNote
              testId="substrate-actors-order"
              computedAt={actors.data?.computedAt}
            />

            <PageControls
              testId="substrate-actors-pages"
              page={actorPage}
              hasNext={Boolean(actors.data?.nextPageToken)}
              onNext={() => actorPage.next(actors.data?.nextPageToken ?? "")}
              onBack={actorPage.back}
              isLoading={actors.isLoading}
            />
          </div>
        </Card>

        <Card
          title={
            <PagedSectionTitle
              title="Workers"
              shown={workerRows.length}
              onPage={workerPageRows.length}
              total={inventory?.workerCount}
              searching={Boolean(workerQuery.trim())}
            />
          }
          extra={
            <Space size={8}>
              <SectionSearch
                label="Search workers"
                testId="substrate-workers-search"
                value={workerQuery}
                onChange={setWorkerQuery}
              />
            </Space>
          }
          data-testid="substrate-workers-card"
        >
          {workers.error ? (
            <Alert
              type="error"
              showIcon
              title="Workers could not be read"
              description={workers.error.message}
              data-testid="substrate-workers-error"
              action={
                <Button size="small" onClick={() => void workers.refresh()}>
                  Try again
                </Button>
              }
            />
          ) : workers.data?.ateApiError ? (
            <PageWarning
              message={workers.data.ateApiError}
              testId="substrate-workers-partial"
            />
          ) : null}

<Table<SubstrateWorkerEntry>
            data-testid="substrate-workers-table"
            rowKey={(worker) =>
              `${worker.workerNamespace}/${worker.workerPool}/${worker.workerPod}`
            }
            columns={workerColumns}
            dataSource={workerRows}
            loading={workers.isLoading}
            pagination={false}
            virtual
            scroll={{ y: GROWING_TABLE_HEIGHT, x: 880 }}
            size="small"
            locale={{
              emptyText: workers.error
                ? " "
                : workerQuery.trim()
                  ? "No workers on this page match your search. Other pages are not searched."
                  : workers.data?.ateApiError
                    ? "This page could not be read from ate-api, so there may be workers it did not reach."
                    : workers.data?.nextPageToken
                      ? "No workers in this scope on this page. There are more pages — use Next to keep looking."
                      : ateApiEnabled
                        ? "ate-api reported no worker assignments."
                        : "Worker assignments come from ate-api, which is not configured on this controller.",
            }}
          />

          {/* One row, the way antd lays out a paged table: what the page is on the
              left, the controls to turn it on the right. `PageControls` carries its
              own top margin for the stacked layout it was written for, which here
              would drop it below the sentence it sits beside — so the row owns the
              spacing and the control's own is cleared. */}
          <div
            css={{
              alignItems: "center",
              display: "flex",
              gap: theme.space(4),
              justifyContent: "space-between",
              marginTop: theme.space(3),
              "& > [data-testid$='-pages']": { marginTop: 0 },
            }}
          >
            <PageScopeNote
              testId="substrate-workers-order"
              computedAt={workers.data?.computedAt}
            />

            <PageControls
              testId="substrate-workers-pages"
              page={workerPage}
              hasNext={Boolean(workers.data?.nextPageToken)}
              onNext={() => workerPage.next(workers.data?.nextPageToken ?? "")}
              onBack={workerPage.back}
              isLoading={workers.isLoading}
            />
          </div>
        </Card>
      </Space>
    </PageFrame>
  );
}
