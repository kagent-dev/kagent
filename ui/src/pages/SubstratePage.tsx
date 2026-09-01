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
import { useTheme } from "@emotion/react";
import { Radio, Search } from "lucide-react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { StatTile } from "@/components/dashboard/StatTile";
import { RefreshButton } from "@/components/table/RefreshButton";
import {
  useNamespaces,
  useSubstrateActors,
  useSubstrateSummary,
  useSubstrateWorkers,
  type SubstrateActorEntry,
  type SubstrateActorSortField,
  type SubstrateSortOrder,
  type SubstrateWorkerSortField,
  type SubstrateActorTemplateEntry,
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
 * A second is quick enough to watch an actor move between workers and slow enough to
 * leave running while reading, which is what this control is for.
 */
const DEFAULT_POLL_SECONDS = 1;

/**
 * The fastest this page will ask, in seconds.
 *
 * Below this the reader is not watching a cluster, they are load-testing one: the
 * inventory is a single unpaginated read of every actor, and on the cluster that
 * answered with 34,356 of them one read is not free. Enforced on the field rather than
 * only in the timer, so the number on screen is the number being used.
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
 * What a status or phase is telling you, as four readings rather than a dozen strings.
 *
 * The substrate's vocabulary is not a closed enum on the wire: `phase` and `status` are
 * plain strings that ate-api and the ActorTemplate controller each fill in their own way,
 * so this classifies rather than switches. Anything unrecognised falls through to
 * `neutral` and is shown as it arrived — inventing a colour for a word this page has
 * never seen would be a claim about health nobody made.
 */
type StatusTone = "healthy" | "warning" | "progress" | "idle" | "neutral";

function statusTone(label: string): StatusTone {
  const value = label.trim().toLowerCase();
  if (value === "ready" || value === "running") return "healthy";
  if (value === "failed" || value === "suspending") return "warning";
  if (value === "suspended" || value === "unknown" || value === "") return "idle";
  // Substrings, because these arrive spelled several ways: `Resuming`, `WaitingForWorker`,
  // `GoldenSnapshotPending`. All of them mean the same thing to a reader — something is
  // under way and the next read will say something different.
  if (value.includes("resume") || value.includes("wait") || value.includes("golden")) {
    return "progress";
  }
  return "neutral";
}

/**
 * A status, coloured by what it means.
 *
 * The triples are the theme's own rather than antd's presets, for the reason the palette
 * states: antd derives a tag's three colours from one foreground token on the assumption
 * of a light page. `primary` is not among them in any tone — it is a fill chosen to carry
 * light text, and as ink on this page it measures about 2.2:1.
 */
function StatusChip({ label }: { label: string }) {
  const theme = useTheme();
  const tone = statusTone(label);

  const pill = {
    healthy: {
      background: theme.color.successBg,
      borderColor: theme.color.successBorder,
      color: theme.color.successText,
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
      borderColor: theme.color.border,
      color: theme.color.textMuted,
    },
    neutral: {
      background: theme.color.bgElevated,
      borderColor: theme.color.borderStrong,
      color: theme.color.text,
    },
  }[tone];

  return (
    <Tag
      css={{
        ...pill,
        /*
         * The substrate's vocabulary is open-ended: `phase` and `status` are plain
         * strings, and a value this build has never seen is shown as it arrived. Some
         * of them are long — a cluster answered with `ACTOR_STATE_CRASHED`, which at
         * one line overflowed its column and printed itself across the next one. So
         * the tag wraps inside the width it is given rather than spilling out of it.
         */
        whiteSpace: "normal",
        maxWidth: "100%",
        wordBreak: "break-word",
      }}
      data-tone={tone}
    >
      {label.trim() === "" ? "not reported" : label}
    </Tag>
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

/** Where a section's rows come from, said once beside the section rather than per row. */
function SectionHint({ children }: { children: string }) {
  const theme = useTheme();
  return (
    <Text css={{ color: theme.color.textMuted, fontSize: 12, fontWeight: 400 }}>
      {children}
    </Text>
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
 * reader scrolling through actors. Anything above 100 is refused outright rather
 * than clamped.
 */
const PAGE_SIZE = 100;

/**
 * How long to wait after a keystroke before asking the server.
 *
 * The filters are server-side now — which is the point, since filtering a fetched
 * page searches only what was fetched — so every keystroke would otherwise be a
 * request against a cluster with hundreds of thousands of actors. Long enough to
 * coalesce typing, short enough not to feel like lag.
 */
const FILTER_DEBOUNCE_MS = 300;

/** A value that follows its input, but only once it has stopped changing. */
function useDebounced<T>(value: T, delayMs: number): T {
  const [settled, setSettled] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setSettled(value), delayMs);
    return () => window.clearTimeout(timer);
  }, [value, delayMs]);
  return settled;
}

/**
 * A paged section's position, as a stack of the tokens that got us here.
 *
 * A stack rather than a page number, because the API pages by token: the only way
 * back to the previous page is the token that produced it. Reset whenever the
 * question changes — a new filter or a new scope makes every token meaningless,
 * and reusing one would ask for "the page after a row that is no longer in the
 * result".
 */
function usePageStack(resetKey: string) {
  const [state, setState] = useState<{ key: string; tokens: string[] }>({
    key: resetKey,
    tokens: [],
  });

  /*
   * The reset is derived, not performed.
   *
   * Clearing the stack from an effect would be a `setState` inside one — a cascading
   * render, and the rule that forbids it is right — and it would also render one
   * frame of the *old* page against the new question before correcting itself.
   * Reading the key alongside the tokens means a changed question is already on the
   * first page in the render that discovers it.
   */
  const tokens = state.key === resetKey ? state.tokens : [];

  return {
    /** The token for the page being shown. Empty is the first page. */
    current: tokens.length > 0 ? tokens[tokens.length - 1] : "",
    pageNumber: tokens.length + 1,
    canGoBack: tokens.length > 0,
    next: (token: string) =>
      setState({ key: resetKey, tokens: [...tokens, token] }),
    back: () => setState({ key: resetKey, tokens: tokens.slice(0, -1) }),
  };
}

/**
 * A column header that asks the *server* to sort.
 *
 * Not antd's own `sorter`, deliberately. That reorders the rows the table was
 * given, which for one page out of hundreds of thousands looks like sorting and is
 * not — the first row of the sorted cluster is almost certainly not on this page.
 * So the header sends the column and the direction, and the rows come back ordered.
 *
 * Clicking cycles ascending → descending → back to the default order, so a reader
 * can undo a sort without knowing which column was the default one.
 */
function SortableHeader<Field extends string>({
  title,
  field,
  sort,
  onSort,
}: {
  title: string;
  field: Field;
  sort: { field: Field | "default"; order: SubstrateSortOrder };
  onSort: (field: Field | "default", order: SubstrateSortOrder) => void;
}) {
  const theme = useTheme();
  const isActive = sort.field === field;
  const arrow = !isActive ? "" : sort.order === "asc" ? " ↑" : " ↓";

  return (
    <button
      type="button"
      data-testid={`substrate-sort-${field}`}
      data-active={isActive}
      data-order={isActive ? sort.order : undefined}
      onClick={() => {
        if (!isActive) onSort(field, "asc");
        else if (sort.order === "asc") onSort(field, "desc");
        else onSort("default", "asc");
      }}
      css={{
        all: "unset",
        cursor: "pointer",
        fontWeight: 600,
        color: isActive ? theme.color.primaryText : "inherit",
        "&:hover": { color: theme.color.primaryText },
      }}
    >
      {title}
      {arrow}
    </button>
  );
}

/**
 * What the server actually did, said beside the table.
 *
 * The order applied comes back on the response rather than being assumed from the
 * control, so a request the server did not honour reads as what it did rather than
 * as what was asked for. The age is here for the same reason: these reads are
 * memoised for a fraction of a second, and a page that showed cached numbers while
 * claiming to poll would be the polling bug this codebase has already shipped once.
 */
function ServerOrder({
  field,
  order,
  computedAt,
  labels,
  testId,
}: {
  field: string;
  order: SubstrateSortOrder;
  computedAt?: string;
  labels: Record<string, string>;
  testId: string;
}) {
  const theme = useTheme();
  const age = useDataAge(computedAt);

  return (
    <Text
      data-testid={testId}
      css={{ color: theme.color.textMuted, fontSize: 12 }}
    >
      Sorted by the server: {labels[field] ?? field}
      {order === "desc" ? ", descending" : ", ascending"}
      {age ? ` · ${age}` : ""}
    </Text>
  );
}

/**
 * How old an answer is, in words, ticking as it ages.
 *
 * A clock of its own, because nothing else re-renders while the page sits idle: a
 * stale figure with no ticking age beside it is indistinguishable from a live one.
 */
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

/** Previous and Next for one paged section, with the page number between them. */
function PageControls({
  testId,
  page,
  hasNext,
  onNext,
  onBack,
  isLoading,
}: {
  testId: string;
  page: { pageNumber: number; canGoBack: boolean };
  hasNext: boolean;
  onNext: () => void;
  onBack: () => void;
  isLoading: boolean;
}) {
  const theme = useTheme();

  // Nothing to page through: one page and no way off it. Hidden rather than shown
  // disabled, because two dead buttons under a five-row table read as a broken
  // control rather than as a complete list.
  if (!hasNext && !page.canGoBack) return null;

  return (
    <Space size={8} css={{ marginTop: theme.space(3) }} data-testid={testId}>
      <Button
        size="small"
        disabled={!page.canGoBack || isLoading}
        onClick={onBack}
        data-testid={`${testId}-prev`}
      >
        Previous
      </Button>
      <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
        Page {page.pageNumber}
      </Text>
      <Button
        size="small"
        disabled={!hasNext || isLoading}
        onClick={onNext}
        data-testid={`${testId}-next`}
      >
        Next
      </Button>
    </Space>
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
 * So it now reads three things:
 *
 * - **the summary**, for the tiles and for the two lists that are inherently small
 *   (worker pools and actor templates ride inline);
 * - **a page of actors** and **a page of workers**, each `PageRequest`/`PageResponse`
 *   as `ListAgentInstances` does it.
 *
 * ## Where the counts come from, and why it matters
 *
 * Every total on this page is the summary's, computed server-side. None of them is
 * `rows.length`. With the lists paged, counting what arrived and calling it a total
 * would report twenty actors for a cluster running four hundred thousand — the exact
 * failure the "3 of 4" rendering already existed to prevent, made far more likely by
 * paging.
 *
 * ## Why the searches are server-side
 *
 * Because a filter applied to a page searches only that page. A reader searching for
 * an actor on page nine would be told there are no matches, which is worse than no
 * search at all. The term is sent and the server narrows the whole set — the counts
 * beside each heading are then the filtered totals, which is what makes "3 of 4,312"
 * true.
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
   * One search per section, and only two of them are sent to the server.
   *
   * The actors and the workers are paged, so their filters have to be — see the
   * page's own note. Worker pools and actor templates arrive whole in the summary,
   * so filtering those here is not a half-truth: the whole list is present to
   * filter.
   */
  const [poolQuery, setPoolQuery] = useState("");
  const [templateQuery, setTemplateQuery] = useState("");
  const [actorQuery, setActorQuery] = useState("");
  const [workerQuery, setWorkerQuery] = useState("");

  const actorFilter = useDebounced(actorQuery.trim(), FILTER_DEBOUNCE_MS);
  const workerFilter = useDebounced(workerQuery.trim(), FILTER_DEBOUNCE_MS);

  /*
   * The order each paged table is read in, sent to the server rather than applied
   * here — see `SortableHeader` for why a local sort would be a lie at this size.
   */
  const [actorSort, setActorSort] = useState<{
    field: SubstrateActorSortField;
    order: SubstrateSortOrder;
  }>({ field: "default", order: "asc" });
  const [workerSort, setWorkerSort] = useState<{
    field: SubstrateWorkerSortField;
    order: SubstrateSortOrder;
  }>({ field: "default", order: "asc" });

  // A new order is a new result, so the page stack resets with it — a token from
  // the previous order names a row's position in an ordering that no longer holds.
  const actorPage = usePageStack(
    `${namespace}|${actorFilter}|${actorSort.field}|${actorSort.order}`,
  );
  const workerPage = usePageStack(
    `${namespace}|${workerFilter}|${workerSort.field}|${workerSort.order}`,
  );

  const actors = useSubstrateActors({
    namespace: scope,
    filter: actorFilter,
    limit: PAGE_SIZE,
    pageToken: actorPage.current,
    sortField: actorSort.field,
    sortOrder: actorSort.order,
  });
  const workers = useSubstrateWorkers({
    namespace: scope,
    filter: workerFilter,
    limit: PAGE_SIZE,
    pageToken: workerPage.current,
    sortField: workerSort.field,
    sortOrder: workerSort.order,
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
          template.phase,
          template.sandboxClass,
          template.workerSelector,
        ]
          .filter(Boolean)
          .join(" "),
      ),
    [inventory?.actorTemplates, templateQuery],
  );

  const actorRows = actors.error ? [] : (actors.data?.actors ?? []);
  const workerRows = workers.error ? [] : (workers.data?.workers ?? []);

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
   * The actor and worker tables have no sorters at all any more, and that is the
   * honest consequence of paging: a client-side sorter reorders the page it was
   * given, which looks like sorting and is not — the first row of the sorted cluster
   * is almost certainly not on this page. The server's order is stated instead.
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
          qualified(template.namespace, template.name)
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

  /*
   * Every column asks the server to sort, and none of them sorts locally.
   *
   * antd's own `sorter` is deliberately absent: it reorders the rows the table was
   * handed, and one page out of 410,110 reordered is not the cluster sorted.
   */
  const actorColumns: ColumnsType<SubstrateActorEntry> = useMemo(
    () => [
      {
        title: (
          <SortableHeader
            title="Actor"
            field="actorId"
            sort={actorSort}
            onSort={(field, order) => setActorSort({ field, order })}
          />
        ),
        key: "actorId",
        width: 320,
        render: (_, actor) => <span css={mono}>{actor.actorId}</span>,
      },
      {
        title: (
          <SortableHeader
            title="Status"
            field="status"
            sort={actorSort}
            onSort={(field, order) => setActorSort({ field, order })}
          />
        ),
        key: "status",
        // Wide enough for the longest status seen on a real cluster
        // (`ACTOR_STATE_CRASHED`) without wrapping it to three lines.
        width: 190,
        render: (_, actor) => <StatusChip label={actor.status} />,
      },
      {
        title: (
          <SortableHeader
            title="Template"
            field="template"
            sort={actorSort}
            onSort={(field, order) => setActorSort({ field, order })}
          />
        ),
        key: "template",
        width: 260,
        render: (_, actor) =>
          actor.actorTemplateName
            ? qualified(actor.actorTemplateNamespace, actor.actorTemplateName)
            : "—",
      },
      {
        title: (
          <SortableHeader
            title="Worker pod"
            field="workerPod"
            sort={actorSort}
            onSort={(field, order) => setActorSort({ field, order })}
          />
        ),
        key: "pod",
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
    [actorSort, mono, muted, qualified],
  );

  const workerColumns: ColumnsType<SubstrateWorkerEntry> = useMemo(
    () => [
      {
        title: (
          <SortableHeader
            title="Pod"
            field="pod"
            sort={workerSort}
            onSort={(field, order) => setWorkerSort({ field, order })}
          />
        ),
        key: "pod",
        width: 360,
        render: (_, worker) => qualified(worker.workerNamespace, worker.workerPod),
      },
      {
        title: (
          <SortableHeader
            title="Pool"
            field="pool"
            sort={workerSort}
            onSort={(field, order) => setWorkerSort({ field, order })}
          />
        ),
        key: "pool",
        width: 220,
        render: (_, worker) => worker.workerPool,
      },
      {
        title: (
          <SortableHeader
            title="Actor"
            field="actor"
            sort={workerSort}
            onSort={(field, order) => setWorkerSort({ field, order })}
          />
        ),
        key: "actor",
        width: 360,
        // "idle" rather than a dash: a worker with no actor on it is available, which
        // is a state worth reading, where a dash says only that a cell is empty.
        render: (_, worker) =>
          worker.actorId ? (
            <span css={mono}>{worker.actorId}</span>
          ) : (
            <Text css={muted}>idle</Text>
          ),
      },
    ],
    [mono, muted, qualified, workerSort],
  );

  const ateApiEnabled = inventory?.enabled ?? false;

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
            description={`Worker pools and actor templates come from Kubernetes and are complete. The actors and workers below come from ate-api, which answered with an error: ${inventory.ateApiError}`}
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
                ? `${inventory.busyWorkerCount}/${inventory.workerCount}`
                : undefined
            }
            isLoading={summary.isLoading}
            hint={unread}
          />
          <StatTile
            label="ate-api"
            testId="substrate-stat-ateapi"
            value={inventory ? (ateApiEnabled ? "connected" : "off") : undefined}
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

        {/* Every actor status, not only the running tally.
            Knowing that 1 of 410,110 actors is running says nothing about the other
            410,109 — and on this cluster what it does not say is that most of them
            have crashed. The summary counts them all, so the page can. */}
        {inventory && inventory.actorStatusCounts.length > 1 ? (
          <Space size={6} wrap data-testid="substrate-actor-status-counts">
            <Text css={{ ...muted, fontSize: 12 }}>Actors by status</Text>
            {inventory.actorStatusCounts.map((entry) => (
              <Tag key={entry.status} css={mono}>
                {entry.status || "not reported"}: {entry.count.toLocaleString()}
              </Tag>
            ))}
          </Space>
        ) : null}

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
              <SectionHint>Kubernetes WorkerPool resources</SectionHint>
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
              <SectionHint>Golden snapshots and harness-owned templates</SectionHint>
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
            <SectionTitle
              title="Actors"
              count={actorRows.length}
              total={actors.data?.totalSize}
            />
          }
          extra={
            <Space size={8}>
              <SectionHint>Live state from ate-api, one page at a time</SectionHint>
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
          ) : null}

          <Table<SubstrateActorEntry>
            data-testid="substrate-actors-table"
            rowKey={(actor) => actor.actorId}
            columns={actorColumns}
            dataSource={actorRows}
            loading={actors.isLoading}
            pagination={false}
            virtual
            scroll={{ y: GROWING_TABLE_HEIGHT, x: 1040 }}
            size="small"
            /* Three different sentences, because they are three different facts and
               only one is something to act on: a controller with no ate-api endpoint
               is a deployment choice, a configured one reporting nothing means the
               actors really are not there, and a search that matched nothing is the
               reader's own doing. */
            locale={{
              emptyText: actors.error
                ? " "
                : actorFilter
                  ? "No actors match your search, anywhere in this scope."
                  : ateApiEnabled
                    ? "ate-api reported no actors in this scope."
                    : "ate-api is not configured on this controller. Set substrate-ate-api-endpoint to see live actors.",
            }}
          />

          <ServerOrder
            testId="substrate-actors-order"
            field={actors.data?.appliedSortField ?? "default"}
            order={actors.data?.appliedSortOrder ?? "asc"}
            computedAt={actors.data?.computedAt}
            labels={{
              default: "status, then actor",
              status: "status",
              actorId: "actor",
              template: "template",
              workerPod: "worker pod",
            }}
          />

          <PageControls
            testId="substrate-actors-pages"
            page={actorPage}
            hasNext={Boolean(actors.data?.nextPageToken)}
            onNext={() => actorPage.next(actors.data?.nextPageToken ?? "")}
            onBack={actorPage.back}
            isLoading={actors.isLoading}
          />
        </Card>

        <Card
          title={
            <SectionTitle
              title="Workers"
              count={workerRows.length}
              total={workers.data?.totalSize}
            />
          }
          extra={
            <Space size={8}>
              <SectionHint>ateom pod assignments</SectionHint>
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
            scroll={{ y: GROWING_TABLE_HEIGHT, x: 940 }}
            size="small"
            locale={{
              emptyText: workers.error
                ? " "
                : workerFilter
                  ? "No workers match your search, anywhere in this scope."
                  : ateApiEnabled
                    ? "ate-api reported no worker assignments."
                    : "Worker assignments come from ate-api, which is not configured on this controller.",
            }}
          />

          <ServerOrder
            testId="substrate-workers-order"
            field={workers.data?.appliedSortField ?? "default"}
            order={workers.data?.appliedSortOrder ?? "asc"}
            computedAt={workers.data?.computedAt}
            labels={{
              default: "pool, then pod",
              pool: "pool",
              pod: "pod",
              actor: "actor",
            }}
          />

          <PageControls
            testId="substrate-workers-pages"
            page={workerPage}
            hasNext={Boolean(workers.data?.nextPageToken)}
            onNext={() => workerPage.next(workers.data?.nextPageToken ?? "")}
            onBack={workerPage.back}
            isLoading={workers.isLoading}
          />
        </Card>
      </Space>
    </PageFrame>
  );
}
