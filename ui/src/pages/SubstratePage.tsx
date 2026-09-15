import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
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
 * The interval polling starts at, in seconds.
 *
 * Half a second is quick enough to watch an actor move between workers, which is what
 * this control is for. It is also the floor below, so the default is the fastest this
 * page will ask — a reader who turns polling on wants to see the cluster move.
 */
const DEFAULT_POLL_SECONDS = 0.5;

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
const ATESPACE_PARAM = "atespace";

/** Empty Kubernetes scope means all watched namespaces. */
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
 * States absent from this list are added to the legend when reported by Substrate.
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
 * A segment's floor, and the space between two of them.
 *
 * Constants rather than literals in the CSS, because the capacity below is arithmetic
 * over exactly these two numbers: a floor changed in one place and not the other gives
 * a bar that computes room it does not have.
 */
const SEGMENT_MIN_WIDTH = 6;
const SEGMENT_GAP = 3;

/**
 * How many segments the bar has room for, side by side, at its current width.
 *
 * The per-actor drawing has a floor per segment and does not wrap, so eighty actors
 * need 717px whatever the window is: at 1024 the sidebar expands and leaves the track
 * 686px, and the bar pushed the whole page into horizontal scroll — which this app
 * forbids, and which was reachable with eighty actors on an ordinary laptop.
 *
 * Measured in a layout effect so the answer is in before the browser paints, rather
 * than after a frame of the overflow this exists to prevent. Zero means not measured —
 * jsdom has no layout and its ResizeObserver is a no-op — and the caller reads that as
 * "no width to cap by" rather than as "no room".
 */
function useSegmentCapacity() {
  const ref = useRef<HTMLDivElement>(null);
  const [capacity, setCapacity] = useState(0);

  useLayoutEffect(() => {
    const element = ref.current;
    if (!element) return;
    const measure = () => {
      const width = element.clientWidth;
      /*
       * A width of zero is the element on its way out — React replacing the node, or
       * the observer's last word as it detaches — not a track with no room in it.
       * Taken at face value it overwrote a good measurement with nothing, and the bar
       * went back to drawing every actor on a track that could not hold them.
       */
      if (width <= 0) return;
      setCapacity(Math.floor((width + SEGMENT_GAP) / (SEGMENT_MIN_WIDTH + SEGMENT_GAP)));
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  return [ref, capacity] as const;
}

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
  const [trackRef, capacity] = useSegmentCapacity();
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
  /*
   * One segment per actor only while there is room for them all.
   *
   * The count is the first limit and the width is the second: below either, the bar
   * falls back to a segment per status sized by its share, which is what it does for a
   * cluster of hundreds of thousands anyway.
   */
  const drawableSegments =
    capacity > 0 ? Math.min(ACTORS_DRAWN_INDIVIDUALLY, capacity) : ACTORS_DRAWN_INDIVIDUALLY;
  const perActor = total > 0 && total <= drawableSegments;
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
        minWidth: SEGMENT_MIN_WIDTH,
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
      ref={trackRef}
      data-testid={testId}
      /* The tooltip needs a pointer, which a screen reader has not got and a keyboard
         cannot produce. So the same summary is the bar's own name — colour and hover are
         never the only things carrying it. */
      role="img"
      aria-label={total === 0 ? emptyText : `${title}. ${summary}`}
      css={{
        display: "flex",
        gap: SEGMENT_GAP,
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

function SectionTitle({
  title,
  count,
  total,
  paged = false,
}: {
  title: string;
  /** How many rows are on screen. */
  count: number;
  /** How many there are in total, counted server-side. */
  total?: number;
  paged?: boolean;
}) {
  const theme = useTheme();
  const narrowed = total !== undefined && total !== count;
  return (
    <Space size={8}>
      <span>{title}</span>
      <Text css={{ color: theme.color.textMuted, fontWeight: 400 }}>
        {paged ? `${count} on this page` : narrowed ? `${count} of ${total.toLocaleString()}` : count}
      </Text>
    </Space>
  );
}

function filterRows<T>(
  rows: readonly T[],
  query: string,
  text: (row: T) => string,
): readonly T[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter((row) => text(row).toLowerCase().includes(needle));
}

function byText<T>(of: (row: T) => string) {
  return (a: T, b: T) => of(a).localeCompare(of(b));
}

/** The same, for a column showing a number, which must not sort as one. */
function byNumber<T>(of: (row: T) => number) {
  return (a: T, b: T) => of(a) - of(b);
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

const PAGE_SIZE = 25;

function PageAge({ computedAt }: { computedAt?: string }) {
  const theme = useTheme();
  const age = useDataAge(computedAt);
  return <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{age}</Text>;
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
  const atespace = searchParams.get(ATESPACE_PARAM) ?? "";

  const namespaces = useNamespaces();
  const summary = useSubstrateSummary({ namespace, atespace });

  const [poolQuery, setPoolQuery] = useState("");
  const [templateQuery, setTemplateQuery] = useState("");
  const actorPage = usePageStack(atespace);
  const workerPage = usePageStack(namespace);

  const actors = useSubstrateActors({
    atespace,
    limit: PAGE_SIZE,
    pageToken: actorPage.current,
  });
  const workers = useSubstrateWorkers({
    namespace,
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
   * The summary is the dearest of the three — it walks ate-api for the actors, the
   * workers and the templates, where a list read walks it once — and this ticks all
   * three at the reader's chosen interval anyway. That is deliberate: the tiles and the
   * rows are one picture, and totals that held still while the table beneath them moved
   * would be two moments shown as one. `isTickInFlight` drops a
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
          template.atespace,
          template.name,
          template.goldenActorId,
          template.phase,
          template.sandboxClass,
          template.workerSelector,
        ]
          .filter(Boolean)
          .join(" "),
      ),
    [inventory?.actorTemplates, templateQuery],
  );

  const actorRows = useMemo(
    () => (actors.error ? [] : (actors.data?.actors ?? [])),
    [actors.data?.actors, actors.error],
  );

  const workerRows = useMemo(
    () => (workers.error ? [] : (workers.data?.workers ?? [])),
    [workers.data?.workers, workers.error],
  );

  /*
   * The tiles, from the summary's own counts.
   *
   * Not derived from the rows on screen, and that is the point of the summary
   * existing: the rows are one page, and a page counted as a total is how a cluster
   * running 410,110 actors gets reported as running 25.
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
        sorter: { compare: byText((t) => `${t.atespace}/${t.name}`), multiple: 5 },
        render: (_, template) => (
          <div>
            {qualified(template.atespace, template.name)}
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
    ],
    [mono, muted, qualified],
  );

  const actorColumns: ColumnsType<SubstrateActorEntry> = useMemo(
    () => [
      {
        title: "Actor",
        key: "actorId",
        width: 300,
        render: (_, actor) => qualified(actor.atespace, actor.actorId),
      },
      {
        title: "Status",
        key: "status",
        width: 190,
        render: (_, actor) => <StatusChip label={actor.status} />,
      },
      {
        title: "Template",
        key: "template",
        width: 260,
        render: (_, actor) =>
          actor.actorTemplateName
            ? qualified(actor.actorTemplateAtespace, actor.actorTemplateName)
            : "—",
      },
      {
        title: "Worker pod",
        key: "workerPod",
        width: 320,
        render: (_, actor) =>
          actor.ateomPodName ? (
            <Text css={{ ...mono, ...muted }}>
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
   * so the only way to fill that column is to read every actor and join. How much of
   * the fleet is busy is on a tile instead, using reported worker allocation.
   */
  const workerColumns: ColumnsType<SubstrateWorkerEntry> = useMemo(
    () => [
      {
        title: "Pod",
        key: "pod",
        width: 420,
        render: (_, worker) => qualified(worker.workerNamespace, worker.workerPod),
      },
      {
        title: "Pool",
        key: "pool",
        width: 260,
        render: (_, worker) => worker.workerPool,
      },
      {
        title: "IP",
        key: "ip",
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

  const ateApiEnabled =
    actors.data?.enabled ?? workers.data?.enabled ?? inventory?.enabled ?? false;

  return (
    <PageFrame
      title="Substrate"
      description="Kubernetes worker pools, plus actor templates, live actors, and worker assignments from Substrate."
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
        <Space size={8} wrap>
          <Text css={muted}>Kubernetes namespace</Text>
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
                  // Terminating namespaces can still contain workers and pools.
                  label:
                    entry.status === "Active"
                      ? entry.name
                      : `${entry.name} (${entry.status.toLowerCase()})`,
                })),
              ]}
            />
          </div>
          <Text css={muted}>ATE atespace</Text>
          <Input.Search
            key={atespace}
            defaultValue={atespace}
            aria-label="ATE atespace"
            placeholder="All atespaces"
            maxLength={63}
            allowClear
            css={{ width: 240 }}
            onSearch={(value) => {
              const next = new URLSearchParams(searchParams);
              if (value) next.set(ATESPACE_PARAM, value);
              else next.delete(ATESPACE_PARAM);
              setSearchParams(next, { replace: true });
            }}
          />
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
            value={!namespace && !atespace ? "all" : `K8s: ${namespace || "all"}; ATE: ${atespace || "all"}`}
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
            rowKey={(template) => `${template.atespace}/${template.name}`}
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
            <SectionTitle
              title="Actors"
              count={actorRows.length}
              paged
            />
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
            unread={Boolean(summary.error)}
            counts={inventory?.actorStatusCounts ?? []}
            emptyText={
              ateApiEnabled
                ? "No actors in this scope."
                : "ate-api is not configured, so there are no actors to show."
            }
          />

          <Table<SubstrateActorEntry>
            data-testid="substrate-actors-table"
            rowKey={(actor) => `${actor.atespace}/${actor.actorId}`}
            columns={actorColumns}
            dataSource={actorRows}
            loading={actors.isLoading}
            /* antd's own pager is off because the pages come from the server by token,
               not by number — `PageControls` below turns them. */
            pagination={false}
            /* Horizontal only. `x` is the sum of the column widths, so the table asks
               for exactly what it uses: a wider one reserves space no column wants and
               scrolls the card for it. There is no `y` because a page of rows is short
               enough to read whole — a body that scrolled would put a second way to
               move through the same list right above the one that turns the pages. */
            scroll={{ x: 1070 }}
            size="small"
            locale={{
              emptyText: actors.error
                ? " "
                : actors.data?.ateApiError
                  ? "This read could not reach ate-api, so there may be actors it did not see."
                  : ateApiEnabled
                    ? "No actors on this page."
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
            <PageAge computedAt={actors.data?.computedAt} />

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
            <SectionTitle
              title="Workers"
              count={workerRows.length}
              paged
            />
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
            scroll={{ x: 880 }}
            size="small"
            locale={{
              emptyText: workers.error
                ? " "
                : workers.data?.ateApiError
                  ? "This read could not reach ate-api, so there may be workers it did not see."
                  : ateApiEnabled
                    ? "No worker assignments in this namespace scope on this page."
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
            <PageAge computedAt={workers.data?.computedAt} />

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
