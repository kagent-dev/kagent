import { useEffect, useMemo, useState } from "react";
import { Link, useLocation } from "react-router-dom";
import {
  Alert,
  Button,
  Checkbox,
  Dropdown,
  Input,
  Modal,
  Skeleton,
  Tooltip,
  Typography,
} from "antd";
import { useTheme, type Theme } from "@emotion/react";
import { byNewestFirst } from "@/components/agent-instances/conversationOrder";
import { RenameConversationDialog } from "@/components/agent-instances/RenameConversationDialog";
import { useConversationTitles } from "@/api/hooks/useConversationTitles";
import toast from "react-hot-toast";
import {
  Bot,
  ChevronsUpDown,
  Folder,
  MoreVertical,
  PanelLeftClose,
  PanelLeftOpen,
  Search,
  SquarePen,
  Pencil,
  Trash,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import {
  apiClient,
  bareName,
  type AgentInstance,
  type AgentInstanceOperation,
  type AgentInstanceState,
  type ApiResource,
} from "@/api";
import {
  conversationTitle,
  relativeAge,
  shortInstanceId,
} from "@/components/agent-instances/instanceLabels";
import { useThemeMode } from "@/theme/themeMode";
import { useVendorExtensionConfig } from "@/vendorExtensions/hooks";
import { agentPageUrl, agentUrl, type AgentRef } from "./agentUrl";
import { AgentSwitcher } from "./AgentSwitcher";
import { iconControlStyles, rowStyles, searchInputStyles } from "./controlStyles";

const { Text } = Typography;

/** Where the rail's collapsed state is remembered, per reader. */
const RAIL_COLLAPSED = "kagent.agentRail.collapsed";

/**
 * The navigation for when you are inside one agent.
 *
 * Narrowed to a single agent: which agent you are in, the things you can do to it,
 * and every conversation you have had with it. That is a different shape of
 * navigation from the application's own rail, not a differently styled one, so it is
 * rendered beside the page's content rather than replacing the shell's.
 *
 * ## What "every conversation with it" now means
 *
 * The sibling instances cut from the same pair. An `AgentInstance` *is* one
 * conversation — the A2A gateway files its tasks under the instance as their
 * `contextId`, and there is no session beneath it — so a second conversation with
 * the same agent is a second instance of the same `(Harness, AgentTemplate)`.
 *
 * That makes the list here exactly what it always was on screen (the conversations
 * you have had with this agent) while being addressed the way the API actually
 * works, and it makes "New Chat" a create rather than a navigation.
 */

interface RailLink {
  label: string;
  to: string;
  icon: LucideIcon;
  /**
   * Named rather than derived from the label.
   *
   * "New Chat" answers to `chat-new-session`, which is what it was called before it
   * became a rail entry and what the browser suite still reaches for. A generated id
   * would have renamed it for no reason a reader of the tests could see.
   */
  testId: string;
  /** Other paths this entry stands for — the edit view belongs to Agent Details. */
  alsoActiveOn?: string[];
}

interface AgentRailProps {
  /**
   * Which agent the rail is scoped to. From the URL, so the rail stands up before
   * anything has been read — including when the read fails.
   */
  agentRef: AgentRef | { namespace: string; id?: undefined };
  /**
   * What the identity card names, when no conversation is selected.
   *
   * The agent's own page mounts this rail with no conversation open — nothing is
   * current yet, which is the point of that page. Without this the card would show
   * the blank initials and empty id of a conversation that does not exist.
   */
  agentTitle?: { primary: string; secondary?: string };
  /**
   * Where the agent's own page is, when no conversation is open to derive it from.
   *
   * Normally this comes from the instance — its template and harness are the pair. On
   * the agent's own page and on a conversation that does not exist yet there is no
   * instance, and without this the rail loses its way back to the agent entirely.
   */
  agentHref?: string;
  /**
   * Which agent this rail is scoped to, where the surface already knows.
   *
   * Reconstructed from the open conversation otherwise, which is fine on a chat page
   * and impossible on the agent's own page or a new conversation — neither has a
   * conversation to read a harness from. The switcher needs the whole pair to leave the
   * current agent out of its own list, so a surface that knows it says so rather than
   * having it inferred from a title string.
   */
  agentPair?: { namespace: string; agentTemplate?: string; harness?: string };
  /**
   * Controls the surface wants in the rail's gutter, under the collapse toggle.
   *
   * The gutter is a column this component already owns and keeps sticky, so a page
   * with one more icon control — chat has Share — can put it there rather than
   * spending a whole row of the conversation on it. Stacked under the toggle, so the
   * column stays one icon wide whatever is in it.
   */
  gutterActions?: ReactNode;
  /**
   * The instance itself, once it has loaded.
   *
   * Only the second line of the identity card needs it, and the conversations below
   * are the reason the rail exists — holding all of it back until the instance read
   * succeeded left a failed read with no navigation at all, and no way to see that
   * the conversations had loaded fine.
   */
  instance?: AgentInstance;
  /**
   * Every instance in the namespace, from whichever surface is already reading them.
   *
   * Required rather than read here: the surfaces mounting this rail all list
   * instances anyway, and a second read keyed differently would fetch the same rows
   * twice. The rail narrows them to the siblings of this one.
   */
  instances: ApiResource<AgentInstance[]>;
  /**
   * A title for *this* conversation, derived from what was said in it.
   *
   * Only the surface rendering the transcript can supply one — deriving it costs a
   * read of the conversation's tasks, so the sibling rows below cannot have one and
   * fall back to their id. Ignored entirely once the conversation has a name the
   * reader gave it.
   */
  autoTitle?: string;
  /** Starts another conversation with this agent: a new instance of the same pair. */
  onNewChat?: () => void;
  /**
   * Told after a conversation is deleted, for a surface that must react.
   *
   * The chat page is the one that must: deleting the conversation it is showing leaves
   * it on an address that no longer resolves, so it navigates away. Everything else can
   * ignore it — the list has already been re-read.
   */
  onDeleted?: (instance: AgentInstance) => void;
  /**
   * What the open conversation's state is about to be, from the surface that changed it.
   *
   * The rail keeps its own stand-in states for suspends *it* started, but sending a
   * message and suspending by hand both happen on the chat page — and both change the
   * conversation's state asynchronously, so the row went on showing the old one until
   * something else refreshed the list. This is that page saying what it just asked for.
   */
  pendingState?: AgentInstanceState;
  /** The operation that surface has claimed, drawn before the record shows it. */
  pendingOperation?: AgentInstanceOperation;
}

export function AgentRail({
  agentRef: ref,
  agentTitle,
  agentHref: agentHrefFromCaller,
  agentPair,
  gutterActions,
  instance,
  instances,
  autoTitle,
  onNewChat,
  onDeleted,
  pendingState,
  pendingOperation,
}: AgentRailProps) {
  const theme = useTheme();
  const { mode } = useThemeMode();
  const location = useLocation();
  const [query, setQuery] = useState("");

  /*
   * Where this rail's entries lead.
   *
   * A distribution may serve its own agent surfaces at its own addresses; it can then
   * share this rail rather than keep a copy of it, because the navigation is the same
   * either way and only the destinations differ. Anything it does not redefine falls
   * back to this application's own route.
   */
  const links = useVendorExtensionConfig().agentLinks;
  const url = {
    chat: (ref: AgentRef) => links?.chat?.(ref) ?? agentUrl.chat(ref),
    details: (ref: AgentRef) => links?.details?.(ref) ?? agentUrl.details(ref),
  };

  const conversations = instances;

  /**
   * Which agent the switcher is open *for*, rather than whether it is open.
   *
   * The rail is re-rendered with new props on a switch rather than unmounted, so a
   * plain boolean stayed true over the agent it had just moved to. Holding the
   * identity makes "closed" fall out of the agent changing, with no second render and
   * nothing to keep in step.
   */
  const [switcherFor, setSwitcherFor] = useState<string>();
  const agentKey = `${ref.namespace}/${ref.id ?? ""}`;
  const isSwitcherOpen = switcherFor === agentKey;

  /**
   * Whether the switcher has ever been opened in this rail.
   *
   * Kept because a collapse cannot animate something already unmounted, and never
   * unset: the cost of leaving it mounted is one cached list, and the gain is that
   * reopening animates too. It is not mounted from the start because the switcher
   * reads the agent list, and a rail that fetched forty agents before anybody asked
   * to change agent would be paying for a menu most readers never open.
   */
  const [hasOpenedSwitcher, setHasOpenedSwitcher] = useState(false);

  /**
   * The height the region is animating *towards*, one frame behind the open state.
   *
   * A transition needs a previous value: mounted straight at full height, the first
   * open jumped and only the second one animated. So the region mounts closed and is
   * told to expand on the next frame, which is the frame that has something to
   * interpolate from. Set inside `requestAnimationFrame`, so this is not a render
   * cascade — it is a paint the browser has already committed.
   */
  const [isExpanded, setExpanded] = useState(false);
  useEffect(() => {
    if (!hasOpenedSwitcher) return;
    const frame = requestAnimationFrame(() => setExpanded(isSwitcherOpen));
    return () => cancelAnimationFrame(frame);
  }, [hasOpenedSwitcher, isSwitcherOpen]);

  /*
   * Two entries: what the agent is, and talking to it.
   *
   * Editing is not a third. It is something you do *from* the details page — which
   * shows what the agent is configured with, and offers the pencil that opens those
   * same values — so a separate entry made "look at it" and "change it" read as two
   * places holding the same facts. The details entry stays lit while editing, because
   * that is where the reader came from and where saving returns them.
   */
  const entries: RailLink[] = [
  ];

  /*
   * Up to the agent, when the instance names a pair.
   *
   * The rail already lists the conversations with this agent, so this is not a
   * second way to reach them — it is the way to reach the agent *itself*: what it
   * is made of, its template, and the conversations other people have had with it,
   * which this rail cannot show because it lists only the caller's own.
   *
   * Conditional rather than always present, because an instance with no prepared
   * revision belongs to no pair and there would be nothing at the other end.
   */
  const agentHref =
    agentHrefFromCaller ??
    (instance?.harness && instance.agentTemplate
      ? agentPageUrl({
          namespace: instance.namespace,
          agentTemplate: bareName(instance.agentTemplate),
          harness: bareName(instance.harness),
        })
      : undefined);
  /*
   * Where "New chat" goes.
   *
   * The new-conversation route is the agent's own address with `/new` on the end, so
   * it is derivable wherever the agent is known — including on the pages that have no
   * instance, which is exactly where this button used to be disabled. It was gated on
   * `instance` because it once *created* the conversation and needed a pair to copy;
   * nothing is created now, so all it needs is somewhere to go.
   */
  const newChatHref = agentHref ? `${agentHref}/new` : undefined;

  if (agentHref) {
    entries.unshift({
      // "Agent Details" rather than "Agent": beside "New chat" and a list of chats, a
      // bare noun reads as a heading for the section rather than as a place to go.
      label: "Agent Details",
      to: agentHref,
      icon: Bot,
      testId: "agent-nav-agent-conversations",
    });
  }

  /*
   * The other conversations with this agent: the instances cut from the same pair.
   *
   * Narrowed on the pair rather than showing every instance in the namespace,
   * because "conversations with *this* agent" is what the list means — a different
   * template is a different agent, and listing it here would make the rail a second
   * copy of the agents page.
   *
   * With no instance loaded yet the pair is unknown, so nothing is claimed: an
   * unfiltered list would briefly show every agent in the namespace as though they
   * were all conversations with this one.
   */
  const chats = useMemo(() => {
    /*
     * With a conversation open, the list is narrowed to its siblings here — the
     * surfaces that mount this rail read every instance in the namespace, and only
     * this one knows which pair is current.
     *
     * With none open, the caller has already narrowed it, because the page *is* an
     * agent and could not have read anything else. Returning nothing in that case is
     * what left the rail empty on the agent and new-conversation pages: an agent's own
     * navigation showing none of its conversations.
     */
    const siblings = instance
      ? (conversations.data ?? []).filter(
          (candidate) =>
            candidate.harness === instance.harness &&
            candidate.agentTemplate === instance.agentTemplate,
        )
      : (conversations.data ?? []);
    const needle = query.trim().toLowerCase();
    const found = !needle
      ? siblings
      : // The name as well as the id: a reader who titled a conversation searches for
        // what they called it, and a box that only matched hex would find nothing while
        // the row they wanted was on screen.
        siblings.filter(
          (candidate) =>
            candidate.id.toLowerCase().includes(needle) ||
            candidate.name.toLowerCase().includes(needle),
        );
    return [...found].sort(byNewestFirst);
  }, [conversations.data, instance, query]);

  /*
   * Collapsed, and remembered.
   *
   * The rail is navigation, so it earns its width most of the time — but a reader
   * following a long answer wants the transcript, and 248px of it is a quarter of a
   * laptop screen. The preference is per-reader rather than per-page: collapsing it on
   * a conversation and finding it back on the next one is the behaviour that makes
   * people stop using the control.
   *
   * Absent means expanded, so a reader who has never touched it gets the navigation.
   */
  /*
   * Deleting is the rail's own job now.
   *
   * It used to depend on a caller passing a handler, so the control existed on the chat
   * page and nowhere else — the same row behaved differently depending on which surface
   * had mounted it. The rail lists the conversations, so it deletes them; a surface that
   * cares what happened afterwards says so through `onDeleted`.
   */
  const [deletingId, setDeletingId] = useState<string>();

  /*
   * Which conversations are ticked, and where the last tick was.
   *
   * The anchor is what makes shift-select mean anything: a range needs two ends, and
   * the second one is wherever the reader shift-clicks. Kept as an id rather than an
   * index so a list that re-orders under them — a conversation moving up because it was
   * just used — cannot turn their range into a different one.
   */
  /*
   * A title for every conversation, not just the open one.
   *
   * Every row but the current one used to read `Untitled · 50b46891`, which made the
   * list very nearly unusable: the one row a reader could identify was the one they
   * were already looking at.
   */
  const derivedTitles = useConversationTitles(chats);

  /*
   * Bringing the list into line is the caller's job, not this one's.
   *
   * The row renders from the live read above, so a reader sees a change at once; the
   * list only has to catch up eventually. Re-reading it from here on every transition
   * looked right and broke sending outright: a list refresh re-renders the surface,
   * which re-runs the transcript's history effect, whose cleanup aborts the controller
   * the in-flight send is using. The message went nowhere and nothing said so.
   *
   * So the surface refreshes its own list when its turn is idle — see `AgentChatPage`.
   */

  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());
  const [anchorId, setAnchorId] = useState<string>();
  const [isBulkDeleting, setBulkDeleting] = useState(false);
  const [isConfirmingBulk, setConfirmingBulk] = useState(false);
  /*
   * What a conversation's state is about to be, before the controller says so.
   *
   * Suspending is not synchronous: the call returns once the operation is claimed and
   * the state changes when the work finishes, so the re-read that follows reports the
   * old state and the row's indicator did not move until something else refreshed it
   * — which reads as the click having done nothing.
   *
   * So the row shows the asked-for state at once and the list is re-read until the
   * controller agrees. Cleared either way: on agreement because the real state now says
   * the same thing, and on failure because the row must go back to the truth rather
   * than keep a state that never happened.
   */
  /*
   * A refused delete has to say so.
   *
   * These were `try`/`finally` with no `catch`, so a refusal closed the dialog, cleared
   * the spinner and reported nothing — the rows came back on the next read and the
   * reader was left to notice that what they deleted was still there. An instance is
   * scoped to its creator on write, so a refusal is an ordinary outcome here rather
   * than an exceptional one.
   */
  const [actionError, setActionError] = useState<{ action: string; message: string }>();

  /*
   * Ticking one, or a run of them.
   *
   * Shift extends from the last tick to this one *over the filtered list*, which is
   * what the reader can see — extending over the unfiltered one would silently select
   * conversations that are not on screen, and the count above would then not match the
   * ticks below it.
   */
  function toggleSelected(id: string, withShift: boolean): void {
    setSelected((current) => {
      const next = new Set(current);
      if (withShift && anchorId) {
        const ids = chats.map((chat) => chat.id);
        const from = ids.indexOf(anchorId);
        const to = ids.indexOf(id);
        if (from !== -1 && to !== -1) {
          const [start, end] = from < to ? [from, to] : [to, from];
          for (let i = start; i <= end; i += 1) next.add(ids[i]);
          return next;
        }
      }
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
    setAnchorId(id);
  }

  /*
   * Every conversation the filter is showing, or none of them.
   *
   * Scoped to the filtered list on purpose: a reader who has searched and then selects
   * all means the ones they searched for. Clearing clears everything, including any
   * selection made before the filter narrowed — otherwise ticks would survive out of
   * sight and the next bulk action would take more than the reader could see.
   */
  /*
   * How many of the selected conversations a suspend would actually reach.
   *
   * Derived rather than held: it is a fact about the selection and the list, both of
   * which are already state, and a copy would go stale the moment either changed.
   */

  const visibleIds = chats.map((chat) => chat.id);
  const allVisibleSelected =
    visibleIds.length > 0 && visibleIds.every((id) => selected.has(id));

  function toggleAllVisible(): void {
    setSelected(allVisibleSelected ? new Set() : new Set(visibleIds));
    setAnchorId(undefined);
  }

  async function deleteSelected(): Promise<void> {
    setBulkDeleting(true);
    setActionError(undefined);
    try {
      const targets = chats.filter((chat) => selected.has(chat.id));
      await Promise.all(
        targets.map((target) =>
          apiClient.agentInstances.remove(target.namespace, target.id),
        ),
      );
      await conversations.refresh();
      targets.forEach((target) => onDeleted?.(target));
      setSelected(new Set());
      setAnchorId(undefined);
    } catch (cause: unknown) {
      // The list is re-read either way, so whatever *was* deleted disappears and
      // whatever was refused stays — and this says which happened.
      await conversations.refresh();
      reportActionFailure("delete", cause, setActionError);
    } finally {
      setBulkDeleting(false);
      setConfirmingBulk(false);
    }
  }





  async function deleteConversation(target: AgentInstance): Promise<void> {
    setDeletingId(target.id);
    setActionError(undefined);
    try {
      await apiClient.agentInstances.remove(target.namespace, target.id);
      await conversations.refresh();
      onDeleted?.(target);
    } catch (cause: unknown) {
      reportActionFailure("delete", cause, setActionError);
    } finally {
      setDeletingId(undefined);
    }
  }

  const [isCollapsed, setCollapsed] = useState(
    () => window.localStorage.getItem(RAIL_COLLAPSED) === "true",
  );

  function toggleCollapsed() {
    setCollapsed((collapsed) => {
      window.localStorage.setItem(RAIL_COLLAPSED, String(!collapsed));
      return !collapsed;
    });
  }

  return (
    <>
    {/*
      The rail slides rather than vanishing.
      
      Unmounting it made the transcript jump the full width of the panel in one frame,
      which reads as a layout fault rather than as something closing. Animating `width`
      on a wrapper keeps the rail mounted and lets the page take up the space smoothly;
      `overflow: hidden` is what stops its contents spilling while it is narrow.
      
      Mounted-but-hidden costs nothing here: the conversations it lists are read by the
      page anyway and handed in, so a collapsed rail issues no requests of its own.
    */}
    <div
      css={{
        flexShrink: 0,
        width: isCollapsed ? 0 : 248,
        overflow: "hidden",
        /*
         * Sticky here, on the wrapper, rather than on the rail inside it.
         *
         * A sticky element travels within its *parent's* box, and this wrapper is
         * exactly as tall as the rail — so a sticky rail had nowhere to go and scrolled
         * away with the page, which is the one thing it exists not to do. The wrapper is
         * the element in normal flow, so it is the one that sticks.
         */
        position: "sticky",
        top: theme.layout.headerHeight + 24,
        alignSelf: "start",
        /* Hidden for real once it has finished closing, not merely clipped to zero
           width: a child of a zero-width box still has a bounding box, so assistive
           technology and anything else asking "is this on screen" would be told yes.
           Delayed by the width transition when closing and applied at once when
           opening, so the slide is still visible in both directions. */
        visibility: isCollapsed ? "hidden" : "visible",
        /*
         * The gap this wrapper is still owed, given back when it has no width.
         *
         * The surfaces lay the rail, the gutter and the content out as a flex row with a
         * gap between each. A collapsed rail is zero-wide but still a flex item, so the
         * gap either side of it survives — leaving the gutter floating a gap's width
         * further from the page's own sidebar than anything else on the page, which is
         * the too-wide margin a reader sees. Cancelling it here rather than nudging the
         * gutter keeps the correction where its cause is, and it animates with the width
         * so nothing jumps at the end of the slide.
         */
        marginInlineEnd: isCollapsed ? `-${theme.space(6)}` : 0,
        transition: `width 180ms ease, margin-inline-end 180ms ease, visibility 0s linear ${isCollapsed ? "180ms" : "0s"}`,
      }}
      aria-hidden={isCollapsed}
    >
    <aside
      data-testid="agent-rail"
      css={{
        width: 248,
        flexShrink: 0,
        display: "flex",
        flexDirection: "column",
        gap: theme.space(3),
        /*
         * Sticky, because this is navigation. The page is what scrolls, so a rail in
         * normal flow would be gone by the third exchange of a conversation — and the
         * whole point of narrowing the navigation to one agent is that the things you
         * can do to that agent stay to hand.
         */
        /*
         * Below the header, not under it.
         *
         * The header is sticky at `top: 0` with a z-index above this, so a rail that
         * stuck any higher than the header is tall slid beneath it — and the switcher,
         * which opens from the card at the very top of the rail, came out half-hidden.
         * Measured from the token rather than guessed, so it follows the header.
         *
         * The gap is deliberate rather than the smallest that clears. At 8px the rail
         * came to rest all but touching the header and the two read as one welded
         * block; the space is what makes it legible as a panel that stopped under the
         * header rather than part of it.
         */
        /*
         * The rail is exactly as tall as the space it has, and the conversations are the
         * only part that scrolls.
         *
         * It used to scroll as one box, so a reader with thirty conversations scrolled
         * the agent's name, the switcher and the search field away to reach them — and
         * the search field is the thing you reach for *because* the list is long. Now
         * only the list moves, and everything you would use to narrow it stays put.
         */
        /* The room between the header and the foot of the window, which is the header
           plus this page's own padding above and below — `space(6)` each. It was 40
           rather than 48, so the rail stood 8px taller than the space there is and was
           the tallest thing on the page: a surface whose content fits exactly still
           scrolled, by 8px, because of the column beside it. */
        height: `calc(100vh - ${theme.layout.headerHeight}px - ${theme.space(12)})`,
        overflow: "hidden",
      }}
    >
      {/* Which agent you are in, stated before what you can do to it: a reader
          arriving from a list of forty needs that confirmed before anything else.

          And it switches. The chevron promises a menu, so it opens one — an
          affordance that looked like "change agent" and went to a details page
          instead was the worst of both. */}
      <button
        type="button"
        aria-expanded={isSwitcherOpen}
        onClick={() => {
          setHasOpenedSwitcher(true);
          setSwitcherFor((open) => (open === agentKey ? undefined : agentKey));
        }}
        data-testid="agent-rail-identity"
        css={{
          textAlign: "left",
          cursor: "pointer",
          width: "100%",
          display: "flex",
          alignItems: "center",
          gap: theme.space(3),
          padding: theme.space(3),
          borderRadius: theme.radius.md,
          background: theme.color.bgElevated,
          border: `1px solid ${theme.color.border}`,
          minWidth: 0,
          transition: "background 100ms ease, border-color 100ms ease",
          /*
           * Surface only, and lighter on the light theme.
           *
           * Moving the border as well made the whole card look redrawn on hover,
           * beside rail rows whose borders hold still. And the same percentage does
           * not read the same on both themes: layering near-black over white at 12%
           * is a solid grey step, where near-white over near-black at 12% is barely a
           * lift. So the mix is stated per theme rather than shared and wrong on one.
           */
          "&:hover": {
            background: `color-mix(in srgb, ${theme.color.text} ${
              mode === "light" ? "3.5%" : "6%"
            }, ${theme.color.bgElevated})`,
          },
          "&:active": {
            background: `color-mix(in srgb, ${theme.color.text} ${
              mode === "light" ? "7%" : "12%"
            }, ${theme.color.bgElevated})`,
            transition: "none",
          },
        }}
      >
        <span
          aria-hidden
          css={{
            display: "grid",
            placeItems: "center",
            width: 32,
            height: 32,
            flexShrink: 0,
            borderRadius: theme.radius.sm,
            background: `${theme.color.primary}26`,
            color: theme.color.primaryText,
            fontWeight: 700,
            fontSize: 13,
          }}
        >
          {/* The first two characters of the instance id. An instance has no name,
              and the template's initials would be identical for every conversation
              with the same agent — which is the one thing this badge sits beside a
              list of. */}
          {/* The agent's initials, not the conversation's.
              
              This card is what opens the agent switcher, so it has to name the thing
              being switched. It took them from the instance id, which meant the badge
              changed every time a reader opened a different conversation with the same
              agent — while the menu behind it listed agents that never changed. */}
          {(instance?.agentTemplate ? bareName(instance.agentTemplate) : (agentTitle?.primary ?? ref.id ?? ""))
            .slice(0, 2)
            .toUpperCase()}
        </span>
        {/* A grid, not two block children: antd's `ellipsis` wraps the text in its own
            inline-block span whose class wins on specificity, so a short name and its
            model ran together on one line — visible only when the name was short
            enough not to wrap, which is exactly the kind of bug that ships. */}
        <span css={{ minWidth: 0, flex: 1, display: "grid", gridAutoRows: "min-content" }}>
          {/* The template names what the agent *is*, so it is the line a reader
              recognises the agent by; the id distinguishes this conversation from
              the others with it. Until the instance loads there is only the id. */}
          <Text ellipsis css={{ fontSize: 14, color: theme.color.text }}>
            {instance?.agentTemplate
              ? bareName(instance.agentTemplate)
              : (agentTitle?.primary ?? shortInstanceId(ref.id ?? ""))}
          </Text>
          {/* Which conversation, under which agent. Named the way the reader named
              it, so the card and the row below it agree. */}
          <Text ellipsis css={{ fontSize: 11, color: theme.color.textMuted }}>
            {/* Where it runs, which is the other half of what an agent *is* — a
                template paired with a harness. The conversation is named in the list
                below, where it is one row among its siblings; naming it here made the
                card describe a conversation while the menu it opens describes agents. */}
            {instance?.harness
              ? `on ${bareName(instance.harness)}`
              : (agentTitle?.secondary ?? ref.namespace)}
          </Text>
        </span>
        <ChevronsUpDown size={14} color={theme.color.textMuted} aria-hidden />
      </button>

      {/* Opened *in flow*, so the rail below moves down rather than being covered. An
          overlay would hide the conversations — which is what a reader compares
          against when deciding whether they are on the right agent.

          It grows and shrinks rather than appearing: content that jumps by 300px
          leaves the reader working out what moved. The `0fr`/`1fr` grid row is what
          makes that animatable — a height transition needs a number, and the height
          here depends on how many agents there are. */}
      {hasOpenedSwitcher ? (
        <div
          data-testid="agent-switcher-region"
          aria-hidden={!isSwitcherOpen}
          css={{
            display: "grid",
            gridTemplateRows: isExpanded ? "1fr" : "0fr",
            transition: "grid-template-rows 180ms ease",
            overflow: "hidden",
            /*
             * Keeps its content's height instead of giving it to the rest of the rail.
             *
             * The rail is a fixed-height flex column, so a child that may shrink gets
             * squeezed by whatever is below it — here the conversation list. The menu
             * ended up clipped to 46px around a 155px switcher: the search field showed,
             * the options rendered *outside* the clip, and it looked like a dropdown
             * that had opened empty. The list below is the thing that should give, and
             * it already scrolls.
             */
            flexShrink: 0,
            // Faded on the way out as well, so a collapse interrupted mid-way still
            // reads as one thing leaving rather than a box that is half a box.
            opacity: isExpanded ? 1 : 0,
          }}
        >
          <div css={{ minHeight: 0, overflow: "hidden" }}>
            {/* Scoped to the agent — the pair — rather than to the conversation open
                within it. The switcher lists agents, so "which one am I on" is a
                question about the pair. */}
            <AgentSwitcher
              current={
                agentPair ?? {
                  namespace: ref.namespace,
                  agentTemplate: instance?.agentTemplate
                    ? bareName(instance.agentTemplate)
                    : agentTitle?.primary,
                  harness: instance?.harness ? bareName(instance.harness) : undefined,
                }
              }
              onPicked={() => setSwitcherFor(undefined)}
            />
          </div>
        </div>
      ) : null}

      {/* Agent Details and New chat are the same kind of thing — somewhere to go — so
          they sit together at one gap, and the sections around them at the rail's own.
          New chat used to live with the conversation list, which put a nav entry inside
          a section it did not belong to and left the two gaps visibly different. */}
      <nav css={{ display: "grid", gap: theme.space(3) }}>
        {entries.map((link) => (
          <RailEntry
            key={link.label}
            link={link}
            isActive={
              location.pathname === link.to ||
              (link.alsoActiveOn ?? []).includes(location.pathname)
            }
          />
        ))}
      {/* A button, not a link: another conversation with this agent is another
          instance of the same pair, so this creates rather than navigates. It sits
          where the "New Chat" rail entry used to, because that is where a reader
          reaches for it. */}
      {/* Always a link when the agent is known, so it can be opened in a new tab like
          any other navigation — and so that starting a conversation is one thing
          everywhere rather than a create here and a navigation there. A button only
          where there is no agent to link to and a caller has offered to handle it. */}
      {/*
        Styled as a rail entry, not as a button.
        
        It sits directly under Agent and Conversation and does the same kind of thing
        — it goes somewhere — so a bordered button among them read as a different
        class of control and drew the eye away from the navigation it belongs to.
        Same `rowStyles` as those entries, so all three highlight identically.
      */}
      {newChatHref ? (
        <Link
          to={newChatHref}
          data-testid="chat-new-session"
          // Highlighted while the reader is on it, like every other entry. Without this
          // the new-conversation page was the one surface in the rail that gave no sign
          // of where you were.
          data-active={location.pathname === newChatHref}
          aria-current={location.pathname === newChatHref ? "page" : undefined}
          css={{
            ...rowStyles(theme, location.pathname === newChatHref),
            fontSize: 13,
            fontWeight: location.pathname === newChatHref ? 600 : 400,
          }}
        >
          <SquarePen size={14} aria-hidden />
          New chat
        </Link>
      ) : (
        <button
          type="button"
          disabled={!onNewChat}
          onClick={() => onNewChat?.()}
          data-testid="chat-new-session"
          css={{
            ...rowStyles(theme, false),
            fontSize: 13,
            width: "100%",
            cursor: onNewChat ? "pointer" : "not-allowed",
            opacity: onNewChat ? 1 : 0.5,
          }}
        >
          <SquarePen size={14} aria-hidden />
          New chat
        </button>
      )}
      </nav>

      {/* The one part that gives. `minHeight: 0` because a flex child will not shrink
          below its content without it, which is what would push the list's own scrollbar
          off the bottom of the rail instead of creating one. */}
      <div
        data-testid="chat-sessions"
        css={{
          display: "flex",
          flexDirection: "column",
          // The rail's own gap, so the search and the list sit at the same rhythm as
          // the entries above them rather than closer together.
          gap: theme.space(3),
          flex: "1 1 auto",
          minHeight: 0,
        }}
      >
        {/*
          Styled to sit in the rail rather than on a form.
          
          The default input carries a hard border and the page's own background, which
          in a column of tinted rows read as the one element that had been dropped in
          from a settings page. It takes the same ground and radius the rows use, loses
          the border until it is focused, and states what it searches — "Search" alone,
          under a heading that says CHATS, is a question a reader has to answer by
          trying it.
        */}
        <Input
          data-testid="chat-search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          onClear={() => setQuery("")}
          allowClear
          size="small"
          prefix={
            <Search size={13} color={theme.color.textMuted} css={{ marginInlineEnd: 2 }} />
          }
          placeholder="Search chats"
          css={{
            ...searchInputStyles(theme),
            fontSize: 13,
            "& input": { fontSize: 13 },
            // Clear of New chat above it: the two are different kinds of thing — one
            // goes somewhere, one narrows what is below — and at the entries' own gap
            // they read as a third and fourth entry. Six rather than the scale's eight,
            // which separated them more than the sections are separated from each other.
            marginTop: 6,
          }}
        />

        {/*
          Select-all and the bulk action, under the search because they act on what the
          search left behind.
          
          The row is always here; only the actions button comes and goes. It used to be
          the whole bar, which meant ticking the first conversation inserted a line and
          pushed the entire list down under the reader's pointer — a jump at the exact
          moment they were aiming at something. Keeping the row costs one line and buys
          a list that does not move, and select-all is worth reaching for before a
          selection exists anyway. The button sits at the end of the row behind an auto
          margin, so its arrival moves nothing.
        */}
        {chats.length > 0 ? (
          <div
            css={{
              display: "flex",
              alignItems: "center",
              gap: theme.space(2),
              /*
               * Tall enough for the button before the button is there.
               *
               * A small antd button is a couple of pixels taller than the checkbox
               * beside it, so without this the row grew when the actions appeared and
               * the list still shifted — a smaller jump than the whole bar appearing,
               * but the same jump, at the same moment.
               */
              minHeight: 24,
            }}
            data-testid="chat-bulk-bar"
          >
            <Checkbox
              checked={allVisibleSelected}
              indeterminate={selected.size > 0 && !allVisibleSelected}
              onChange={toggleAllVisible}
              data-testid="chat-select-all"
            >
              <Text
                data-testid="chat-selection-count"
                css={{ fontSize: 12, color: theme.color.textMuted }}
              >
                {/* The count once there is one, and what the box does before that.
                    Never "Select none": that is what the box itself is for, and it
                    already says all-or-some through its checked and indeterminate
                    states — swapping the label hid the number at the moment it
                    mattered most. */}
                {selected.size > 0 ? `${selected.size} selected` : "Select all"}
              </Text>
            </Checkbox>

            {selected.size > 0 ? (
            <Dropdown
              trigger={["click"]}
              menu={{
                items: [
                  {
                    key: "delete",
                    danger: true,
                    icon: <Trash size={13} />,
                    label: `Delete ${selected.size === 1 ? "chat" : "all selected"}`,
                    onClick: () => setConfirmingBulk(true),
                  },
                ],
              }}
            >
              <Button
                type="text"
                size="small"
                loading={isBulkDeleting}
                icon={<MoreVertical size={14} color={theme.color.textMuted} />}
                aria-label="Actions for the selected conversations"
                data-testid="chat-bulk-menu"
                css={{ marginInlineStart: "auto" }}
              />
            </Dropdown>
            ) : null}

            <Modal
              open={isConfirmingBulk}
              onCancel={() => setConfirmingBulk(false)}
              onOk={() => void deleteSelected()}
              okText="Delete"
              okButtonProps={{ danger: true, loading: isBulkDeleting }}
              cancelText="Keep"
              title={`Delete ${selected.size} ${selected.size === 1 ? "conversation" : "conversations"}?`}
              data-testid="chat-bulk-confirm"
            >
              <Text css={{ color: theme.color.textMuted }}>
                Everything said in them goes too, and none of it can be recovered. The
                workers they hold are released.
              </Text>
            </Modal>
          </div>
        ) : null}

        <Text
          css={{
            color: theme.color.textMuted,
            fontSize: 11,
            textTransform: "uppercase",
            letterSpacing: 0.6,
            paddingInline: theme.space(2),
          }}
        >
          Chats
        </Text>

        {actionError ? (
          <Alert
            type="error"
            showIcon
            data-testid="chat-delete-error"
            title={`Could not ${actionError.action}`}
            // The controller's own words. Two are common and neither is a fault: the
            // conversation belongs to somebody else, which is a fact about permission;
            // or a lifecycle operation is already in flight on it, which is a fact about
            // timing and clears on its own. Both read very differently from a failure.
            description={actionError.message}
          />
        ) : null}

        {conversations.isLoading ? (
          /* Sized like the rows it stands in for. A bare skeleton is shorter than the
             list that replaces it, so the first visit to an agent shifted everything
             below it the moment the conversations arrived — and only the first, because
             every visit after that is served from cache with nothing to wait for. */
          <div css={{ minHeight: 132, paddingInline: theme.space(2) }}>
            <Skeleton active paragraph={{ rows: 4 }} title={false} data-testid="chat-sessions-loading" />
          </div>
        ) : conversations.error ? (
          <Alert
            type="error"
            showIcon
            data-testid="chat-sessions-error"
            title="Could not load conversations"
            description={conversations.error.message}
            action={
              <Button size="small" onClick={() => void conversations.refresh()}>
                Try again
              </Button>
            }
          />
        ) : chats.length === 0 ? (
          <Text
            data-testid="chat-sessions-empty"
            css={{
              color: theme.color.textMuted,
              fontSize: 12,
              paddingInline: theme.space(2),
            }}
          >
            {/* Two different facts, and reporting the filtered case as "no
                conversations" would have the reader looking for a bug in the agent
                rather than in what they just typed. */}
            {query.trim() && (conversations.data?.length ?? 0) > 0
              ? "No conversations match your search."
              : "No conversations yet."}
          </Text>
        ) : (
          <ul
            data-testid="chat-sessions-list"
            css={{
              listStyle: "none",
              margin: 0,
              padding: 0,
              display: "grid",
              gap: 2,
              /* Rows sit at the top rather than sharing out the space.
                 A grid's `align-content` defaults to `stretch`, and this list is a flex
                 child that grows — so with three conversations in a tall rail each row
                 stretched to fill it. */
              alignContent: "start",
              flex: "1 1 auto",
              minHeight: 0,
              overflowY: "auto",
            }}
          >
            {chats.map((candidate) => (
              <ChatEntry
                key={candidate.id}
                instance={candidate}
                /* The open conversation's title comes from the transcript already on
                   screen; the rest are read for. Both are derived the same way from the
                   same first message, so a row does not change its name when opened. */
                autoTitle={
                  candidate.id === ref.id ? autoTitle : derivedTitles[candidate.id]
                }
                // The surface's own live read wins for the conversation it is showing:
                // the list is read once and then only on request, while the chat page
                // re-reads the one conversation it renders.
                /*
                 * For the open conversation, the surface's own live read wins.
                 *
                 * These rows come from the *list*, which is read once and then only when
                 * something asks it to be read again — while the chat page re-reads the
                 * one conversation it is showing on a timer. So the row for the
                 * conversation a reader is actually watching had the stalest copy of the
                 * thing they were watching it for, and its indicator did not move until
                 * a list read happened to come along.
                 *
                 * Ahead of both, whatever this page has just asked for, which is newer
                 * than anything either read can know yet.
                 */
                shownState={
                  candidate.id === ref.id
                    ? pendingState ?? instance?.state ?? candidate.state
                    : candidate.state
                }
                shownOperation={
                  (candidate.id === ref.id
                    ? pendingOperation ?? instance?.operation
                    : undefined) ?? candidate.operation
                }
                href={url.chat({ namespace: candidate.namespace, id: candidate.id })}
                isActive={candidate.id === ref.id}
                onDelete={deleteConversation}
                // The list this row came from, re-read so the new name is on screen
                // before the toast says it changed.
                onRenamed={conversations.refresh}
                isDeleting={deletingId === candidate.id}
                isSelected={selected.has(candidate.id)}
                onToggleSelected={toggleSelected}
                isSelecting={selected.size > 0}
              />
            ))}
          </ul>
        )}
      </div>
    </aside>
    </div>

    {/*
      Outside the rail, not inside it.
      
      In the rail it sat above the identity card and pushed everything down, so
      collapsing and expanding moved the switcher under the reader's cursor — the
      control they had just used to open it. Beside the rail it changes nothing within,
      and the rail's contents keep their position whether it is there or not.
      
      Sticky on its own, so it stays reachable through a long conversation rather than
      scrolling away with the top of the page.
    */}
    <div
      css={{
        flexShrink: 0,
        position: "sticky",
        top: theme.layout.headerHeight + 24,
        alignSelf: "start",
        marginInlineStart: -theme.space(3),
        display: "grid",
        gap: theme.space(1),
        justifyItems: "center",
      }}
    >
      {/* One control that stays put and changes its icon, rather than two that swap
          places — a button which moves as you use it is one you have to find again. */}
      <Button
        type="text"
        size="small"
        icon={
          isCollapsed ? (
            <PanelLeftOpen size={16} aria-hidden />
          ) : (
            <PanelLeftClose size={16} aria-hidden />
          )
        }
        onClick={toggleCollapsed}
        aria-label={isCollapsed ? "Show the agent navigation" : "Hide the agent navigation"}
        data-testid={isCollapsed ? "agent-rail-expand" : "agent-rail-collapse"}
        css={iconControlStyles(theme)}
      />
      {gutterActions}
    </div>
    </>
  );
}

/**
 * What to call a conversation in the rail.
 *
 * The reader's own name where there is one, and an honest "untitled" plus enough id
 * to tell it from its neighbours where there is not — `conversationTitle` decides
 * that, and it decides it identically for the rail, the agent's page and the chat
 * header, so the same conversation cannot be called three things.
 *
 * The age is appended here and nowhere else: the rail's rows carry nothing but a
 * label, so how long ago a conversation started is the only other thing that
 * distinguishes two untitled ones. A table has a column for it instead.
 */
function conversationLabel(instance: AgentInstance, autoTitle?: string) {
  const age = instance.createdAt ? ` · ${relativeAge(instance.createdAt)}` : "";
  return `${conversationTitle(instance, autoTitle)}${age}`;
}


/**
 * What state a conversation is in, as one dot at the end of its row.
 *
 * A dot rather than a tag, because the row is a name in a 248px column and a word
 * beside every one of them would leave no room for the name — which is the thing the
 * reader is actually scanning for. The title carries the word for anyone who needs it,
 * and the colour is the same one the state tag uses elsewhere, so the two agree.
 *
 * Ready is drawn like the rest rather than left blank. A missing dot reads as "not
 * loaded yet", not as "nothing to report", and the difference matters most on the row
 * a reader is about to click.
 */
/**
 * What state a conversation is in, as one dot at the end of its row.
 *
 * A dot rather than a tag, because the row is a name in a 248px column and a word
 * beside every one of them would leave no room for the name — which is the thing the
 * reader is actually scanning for. The tooltip carries the words.
 *
 * Three colours for three answers, and the middle one is the reason this reads the
 * operation as well as the state. Suspending is not a state — the record says `ready`
 * with a `suspend` operation claimed on it until the work finishes — so a dot drawn
 * from the state alone showed green right up to the moment it went grey, with nothing
 * in between to say the click had been heard.
 *
 * Ready is drawn like the rest rather than left blank. A missing dot reads as "not
 * loaded yet", not as "nothing to report", and the difference matters most on the row
 * a reader is about to click.
 */
function ConversationStateDot({
  state,
  operation,
}: {
  state?: AgentInstanceState;
  operation?: AgentInstanceOperation;
}) {
  const theme = useTheme();

  const inFlight = operation && operation !== "unspecified" && operation !== "unknown";

  /*
   * Nothing to say about an ordinary conversation, so nothing is drawn.
   *
   * This used to mark every row, `ready` included, on the reasoning that a missing dot
   * reads as "not loaded yet". That was right while `ready` meant something: a
   * conversation held a worker until the page suspended it, so the dot separated the
   * ones that were holding one from the ones that were not.
   *
   * The server quiesces a runtime after every turn now and deliberately leaves the
   * record `ready`, and the manual suspend that was the only other way to change it is
   * gone — so `state` is `ready` for every conversation, permanently. A dot on every
   * row saying the same thing is not a status, it is decoration, and it would be
   * decoration that implies a distinction the API cannot make: whether a runtime is
   * live or quiesced is not on the AgentInstance record at all.
   *
   * What is left is genuinely exceptional and worth spotting in a list — a conversation
   * still being created, one that failed, one being deleted — so the dot now means
   * "look at this one" instead of appearing beside everything.
   */
  if (!inFlight && (state === "ready" || state === undefined)) return null;

  const reading = inFlight
    ? { colour: theme.color.warning, words: `${OPERATION_WORDS[operation]}…` }
    : {
        colour: STATE_COLOUR(theme)[state ?? ""] ?? theme.color.border,
        words: state ? STATE_WORDS[state] ?? state : "in an unknown state",
      };

  return (
    <Tooltip title={`This conversation is ${reading.words}`} placement="left">
      <span
        data-testid={`chat-session-state-${inFlight ? operation : (state ?? "unknown")}`}
        role="status"
        aria-label={`Status: ${reading.words}`}
        css={{
          flexShrink: 0,
          width: 7,
          height: 7,
          borderRadius: "50%",
          background: reading.colour,
        }}
      />
    </Tooltip>
  );
}


const STATE_COLOUR = (theme: Theme): Record<string, string> => ({
  suspended: theme.color.textMuted,
  creating: theme.color.warning,
  failed: theme.color.danger,
  deleting: theme.color.danger,
  deleted: theme.color.danger,
});

/** Said the way a reader would say it, not the way the enum spells it. */
const STATE_WORDS: Record<string, string> = {
  suspended: "suspended",
  creating: "still being created",
  failed: "failed",
  deleting: "being deleted",
  deleted: "deleted",
  unspecified: "in an unreported state",
  unknown: "in an unknown state",
};

const OPERATION_WORDS: Record<string, string> = {
  create: "being created",
  suspend: "suspending",
  resume: "resuming",
  delete: "being deleted",
};

function RailEntry({ link, isActive }: { link: RailLink; isActive: boolean }) {
  const theme = useTheme();
  const { icon: Icon, label, to, testId } = link;

  return (
    <Link
      to={to}
      data-testid={testId}
      data-active={isActive}
      aria-current={isActive ? "page" : undefined}
      css={{ ...rowStyles(theme, isActive), fontSize: 13, fontWeight: isActive ? 600 : 400 }}
    >
      <Icon size={14} aria-hidden />
      {label}
    </Link>
  );
}

function ChatEntry({
  instance,
  autoTitle,
  href,
  isActive,
  onDelete,
  onRenamed,
  shownState,
  shownOperation,
  isDeleting,
  isSelected,
  onToggleSelected,
  isSelecting,
}: {
  instance: AgentInstance;
  autoTitle?: string;
  href: string;
  isActive: boolean;
  onDelete: (instance: AgentInstance) => void;
  /** A rename landed, so whatever read this row came from is now stale. */
  onRenamed: () => void | Promise<void>;
  /**
   * The state to draw, which is not always the state on the record.
   *
   * Suspending is asynchronous, so between the click and the controller agreeing the
   * record still says `ready` — and a row that kept showing it looked like the click
   * had done nothing.
   */
  shownState?: AgentInstanceState;
  /**
   * The lifecycle operation to draw, which may be one not yet on the record.
   *
   * A suspend is claimed and then worked, so the record reads `ready` with a `suspend`
   * operation for a second or two — and until the first re-read it reads `ready` with
   * nothing at all. Both are drawn as suspending.
   */
  shownOperation?: AgentInstanceOperation;
  isDeleting: boolean;
  isSelected: boolean;
  onToggleSelected: (id: string, withShift: boolean) => void;
  /** Whether anything is selected, which is what keeps the boxes on screen. */
  isSelecting: boolean;
}) {
  const theme = useTheme();
  /*
   * Still asked, even from behind a menu.
   *
   * The menu makes deleting deliberate; it does not make it recoverable. A conversation
   * is gone with its whole transcript and there is no undo, so the question stays — and
   * it names the conversation, because in a list of a dozen alike rows the reader's only
   * question is *which one*.
   */
  const [isConfirming, setConfirming] = useState(false);
  const [isRenaming, setRenaming] = useState(false);

  return (
    <li css={{ display: "flex", alignItems: "center", gap: 2, minWidth: 0 }}>
      <Modal
        open={isConfirming}
        onCancel={() => setConfirming(false)}
        onOk={() => {
          setConfirming(false);
          onDelete(instance);
        }}
        okText="Delete"
        okButtonProps={{ danger: true, loading: isDeleting }}
        cancelText="Keep"
        title={`Delete "${conversationLabel(instance, autoTitle)}"?`}
        data-testid={`chat-session-confirm-${instance.id}`}
      >
        <Text css={{ color: theme.color.textMuted }}>
          This conversation and everything said in it cannot be recovered. The worker it
          holds is released.
        </Text>
      </Modal>
      {/*
        One slot, two things: the folder that marks a conversation, and the checkbox
        that picks it.
        
        They share a place rather than sitting side by side, so nothing moves as the
        pointer crosses a row — a list that reflows under the cursor is one where the
        thing you were aiming at has gone. The folder is decoration and the checkbox is
        the control, so the control takes the space when it is reachable: on hover, on
        focus, and for as long as anything is selected.
      */}
      <span
        css={{
          display: "grid",
          placeItems: "center",
          width: 22,
          height: 22,
          flexShrink: 0,
          // Both children occupy the same cell; only one is painted.
          "& > *": { gridArea: "1 / 1" },
        }}
      >
        <Folder
          size={13}
          aria-hidden
          css={{
            color: theme.color.textMuted,
            opacity: isSelecting || isSelected ? 0 : 1,
            transition: "opacity 100ms ease",
            "li:hover &": { opacity: 0 },
          }}
        />
        <Checkbox
          checked={isSelected}
          data-testid={`chat-session-select-${instance.id}`}
          aria-label={`Select ${conversationLabel(instance, autoTitle)}`}
          onClick={(event) => {
            // Read from the event rather than a keydown listener: the shift state that
            // matters is the one at the moment of the click.
            onToggleSelected(instance.id, (event as unknown as MouseEvent).shiftKey);
          }}
          css={{
            opacity: isSelecting || isSelected ? 1 : 0,
            transition: "opacity 100ms ease",
            "li:hover &, &:focus-within": { opacity: 1 },
            /*
             * A target bigger than the tick drawn in it.
             *
             * This box replaces the folder icon in a single grid cell, so it was sized
             * to the icon — about as small as a pointer target gets, and shift-picking
             * a run means hitting several of them in a row. The padding grows the
             * clickable area with negative margin cancelling it, so the cell it shares
             * with the icon does not change size and nothing in the row moves.
             */
            padding: theme.space(2),
            margin: `-${theme.space(2)}`,
            "& .ant-checkbox .ant-checkbox-inner": { width: 18, height: 18 },
          }}
        />
      </span>

      <Link
        to={href}
        data-testid={`chat-session-${instance.id}`}
        data-active={isActive}
        css={{ ...rowStyles(theme, isActive), flex: 1, fontSize: 13, minWidth: 0 }}
      >
        <Text ellipsis css={{ color: "inherit", fontSize: "inherit", flex: 1, minWidth: 0 }}>
          {conversationLabel(instance, autoTitle)}
        </Text>
        <ConversationStateDot
          state={shownState ?? instance.state}
          operation={shownOperation ?? instance.operation}
        />
      </Link>
      {/*
        A menu, revealed on hover, rather than a trash can on every row.
        
        The trash was always visible and sat inches from the conversation being read, in
        a rail where every row looks alike — a slip cost the whole thing with nothing to
        undo it. Behind a menu it takes two deliberate actions, and the row is quieter
        for the reader who is not deleting anything, which is almost always.
        
        It also only existed where a caller passed a handler, so it appeared on the chat
        and nowhere else. The rail owns the delete now, so the row behaves the same on
        every surface that mounts it.
      */}
      <Dropdown
        trigger={["click"]}
        menu={{
          items: [
            {
              key: "rename",
              icon: <Pencil size={13} />,
              label: "Rename chat",
              onClick: () => setRenaming(true),
            },
            {
              key: "delete",
              danger: true,
              icon: <Trash size={13} />,
              label: "Delete chat",
              onClick: () => setConfirming(true),
            },
          ],
        }}
      >
        <Button
          type="text"
          size="small"
          loading={isDeleting}
          data-testid={`chat-session-menu-${instance.id}`}
          aria-label={`Actions for ${conversationLabel(instance, autoTitle)}`}
          icon={<MoreVertical size={14} color={theme.color.textMuted} />}
          css={{
            flexShrink: 0,
            // Hidden until the row is hovered or the button itself has focus, so the
            // list reads as names rather than as a column of controls. Focus matters as
            // much as hover: a keyboard reader has no pointer to reveal it with.
            opacity: 0,
            transition: "opacity 100ms ease",
            "li:hover &, &:focus-visible, &[aria-expanded='true']": { opacity: 1 },
          }}
        />
      </Dropdown>

      {/* Beside the menu that opens it rather than at the rail's root: the row already
          holds the delete confirmation the same way, and one dialog per row costs
          nothing while it is closed. */}
      <RenameConversationDialog
        instance={instance}
        open={isRenaming}
        onClose={() => setRenaming(false)}
        onRenamed={onRenamed}
      />
    </li>
  );
}

/**
 * Says a delete was refused, three ways, because each reaches a different person.
 *
 * The alert stays on screen beside the rows that did not go, which is where the reader
 * looks when they notice something is still there. The toast catches the reader who has
 * already looked away — a bulk delete is the kind of thing you start and stop watching.
 * And the console carries the whole error for whoever is debugging it later, which
 * neither of the other two can without becoming unreadable.
 *
 * It was none of these: the delete paths had `try`/`finally` and no `catch`, so a
 * refusal closed the dialog, cleared the spinner and reported nothing at all. An
 * instance is scoped to its creator on write, so being refused is an ordinary outcome
 * here rather than an exceptional one — which is exactly why it must be said.
 */


function reportActionFailure(
  /** What was attempted, lower case — it is read in the middle of a sentence. */
  action: string,
  cause: unknown,
  setMessage: (message: { action: string; message: string }) => void,
): void {
  const message = cause instanceof Error ? cause.message : String(cause);
  console.error(`Could not ${action} conversation(s):`, cause);
  toast.error(`Could not ${action}: ${message}`);
  setMessage({ action, message });
}
