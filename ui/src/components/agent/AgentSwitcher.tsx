import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Input, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { Search } from "lucide-react";
import {
  useNamespaces,
  useAgentTemplatesAcrossNamespaces,
  agentPairsFrom,
  type AgentPair,
} from "@/api";
import { agentNewChatUrl } from "./agentUrl";
import { rowStyles, searchInputStyles } from "./controlStyles";

const { Text } = Typography;

/**
 * Change which agent the rail is scoped to, without leaving the rail.
 *
 * Rendered *in flow* under the identity card rather than as an overlay, so opening
 * it pushes the rest of the rail down. That is deliberate: what a reader checks when
 * deciding whether they are on the right agent is the conversations listed below, and
 * a dropdown floating over them hides the evidence.
 *
 * ## Where it sends you
 *
 * To the chosen agent's chat, not to whatever page you were on. A form or a
 * particular conversation belongs to the agent it was opened from — carrying that
 * path across would ask the new agent for a conversation that is not theirs.
 *
 * ## Why it reads the list itself
 *
 * The rail is handed the instances of one namespace, and switching agent means
 * leaving that namespace behind. This is the only part that needs every agent in the
 * cluster, so it is the part that asks — and it asks only while it is open, because
 * it is only rendered then.
 *
 * Reading across namespaces is a request per namespace (`AgentInstanceService` has
 * no cross-namespace read), which is another reason this is not loaded until asked
 * for.
 */
export function AgentSwitcher({
  current,
  onPicked,
}: {
  /** The agent the rail is scoped to: a template and the harness that runs it. */
  current: { namespace: string; agentTemplate?: string; harness?: string };
  onPicked: () => void;
}) {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();
  const namespaceNames = useMemo(
    () => (namespaces.data ?? []).map((entry) => entry.name),
    [namespaces.data],
  );
  /*
   * Agents, not conversations.
   *
   * This listed `AgentInstance`s — so switching "agent" moved between *conversations*,
   * and one agent with nine of them filled the switcher nine times over with rows a
   * reader could not tell apart. An agent is a `(template, harness)` pair, which is
   * what the agents page lists, and what somebody opening a switcher labelled "agent"
   * is looking for.
   *
   * Free to read: the pairs come from each template's `status.harnesses`, which is the
   * same read the agents page makes.
   */
  const templates = useAgentTemplatesAcrossNamespaces(namespaceNames);
  const agents = useMemo(
    () => agentPairsFrom(templates.data?.templates ?? []),
    [templates.data],
  );
  const [query, setQuery] = useState("");

  /**
   * Brings the agent you are on into view when the list opens.
   *
   * The list is in load order and the current agent can be anywhere in it — at the
   * foot of a scroll box, in a list of forty. Opening a switcher that does not show
   * where you are makes the reader scroll to find out, which is the question they
   * opened it to answer.
   *
   * No state is set here, so there is no render to cascade: the effect only moves a
   * scrollbar.
   */
  const currentRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    currentRef.current?.scrollIntoView({ block: "nearest" });
  }, [agents]);

  /*
   * Matched on what the reader can see: the template that names the agent, and the
   * id that distinguishes this conversation. Searching a UUID nobody can read would
   * be a search box that only works for text nobody has.
   */
  const matches = useMemo(() => {
    /*
     * Everything except the agent you are already on.
     *
     * This is a *switcher*: every row is somewhere to go, and the one row that goes
     * nowhere is the agent whose page is already open. Listing it made the reader read
     * past their own agent to find another, and offered a click that did nothing —
     * which is worse than absent, because it looks like a destination.
     *
     * The card that opens this menu names the current agent directly above it, so
     * nothing is lost by leaving it out.
     */
    /*
     * Matched on what is known, not on all three parts regardless.
     *
     * `harness` reaches this from the open conversation's record, so it is undefined
     * until that record loads — and on the surfaces that have no conversation at all,
     * the agent's own page and the new-conversation page, it never arrives. Requiring
     * it to match meant nothing matched, and the agent the reader was already on
     * appeared in its own switcher: not always, which is what made it look
     * intermittent, but exactly whenever the record had not arrived yet.
     *
     * A template on two harnesses is two agents, so dropping both when the harness is
     * unknown does hide one more row than strictly necessary. That is the better error:
     * the row it hides is a near-duplicate of the one the reader is on, where the row
     * it used to show was the one place that goes nowhere.
     */
    const others = agents.filter(
      (row) =>
        !(
          row.namespace === current.namespace &&
          row.agentTemplate === current.agentTemplate &&
          (current.harness === undefined || row.harness === current.harness)
        ),
    );

    const needle = query.trim().toLowerCase();
    if (!needle) return others;

    return others.filter((row) =>
      `${row.namespace}/${row.agentTemplate}/${row.harness}`.toLowerCase().includes(needle),
    );
  }, [agents, query, current.namespace, current.agentTemplate, current.harness]);

  function pick(row: AgentPair) {
    onPicked();
    // To the call to action for that agent — a conversation that does not exist yet.
    // Picking an agent is the start of talking to it, and nothing is created until a
    // message is sent.
    const href = agentNewChatUrl(row);
    if (href) navigate(href);
  }

  return (
    <div
      data-testid="agent-switcher"
      css={{
        display: "grid",
        gap: theme.space(2),
        padding: theme.space(2),
        borderRadius: theme.radius.md,
        border: `1px solid ${theme.color.border}`,
        background: theme.color.bgElevated,
      }}
    >
      <Input
        size="small"
        allowClear
        value={query}
        onChange={(event) => setQuery(event.target.value)}
        prefix={<Search size={13} color={theme.color.textMuted} />}
        placeholder="Find an agent"
        data-testid="agent-switcher-filter"
        css={searchInputStyles(theme)}
      />

      {templates.error ? (
        <Text data-testid="agent-switcher-error" css={{ fontSize: 12, color: theme.color.danger }}>
          Could not list agents. {templates.error.message}
        </Text>
      ) : null}

      <div
        css={{
          display: "grid",
          /*
           * `min-content` rows, and content packed to the top.
           *
           * A grid distributes spare height across its rows, so with two agents in a
           * box that had a minimum height the two options stretched to fill it — the
           * name and its namespace drifted a finger apart and the list read as
           * half-loaded. The height belongs to the container; the rows should be as
           * tall as their content and no taller.
           */
          gridAutoRows: "min-content",
          alignContent: "start",
          gap: theme.space(1),
          /*
           * Tall enough to be a list, short enough not to be a wall.
           *
           * 260px showed about five agents, so a dozen was mostly scrollbar. Bounded by
           * the viewport rather than a fixed number so it cannot grow past the rail it
           * opens inside. No minimum: a cluster with two agents should look like a
           * cluster with two agents, not like a panel that failed to load.
           */
          maxHeight: "min(60vh, 520px)",
          overflowY: "auto",
          // Room for the scrollbar so the last row is not pinned against it.
          paddingInlineEnd: theme.space(1),
        }}
      >
        {matches.map((row) => {
          const namespace = row.namespace;
          // The agent this rail is scoped to, which is the pair — not the conversation
          // that happens to be open within it.
          const isCurrent =
            namespace === current.namespace &&
            row.agentTemplate === current.agentTemplate &&
            row.harness === current.harness;

          return (
            <button
              key={row.id}
              ref={isCurrent ? currentRef : undefined}
              type="button"
              onClick={() => pick(row)}
              aria-current={isCurrent}
              data-testid={`agent-switcher-option-${row.agentTemplate}-${row.harness}`}
              css={{
                /*
                 * The same row idiom as the rail's conversation list, which sits
                 * directly below this menu when it opens — and the same shape as the
                 * card that opened it, which is directly above.
                 *
                 * `rowStyles` carries the tints and press states so all three highlight
                 * identically; the layout here is local because an agent is three facts
                 * — its initials, its name, and where it runs.
                 */
                ...rowStyles(theme, isCurrent),
                display: "grid",
                gridTemplateColumns: "auto minmax(0, 1fr)",
                gridAutoRows: "min-content",
                alignItems: "center",
                columnGap: theme.space(2),
                rowGap: 0,
                textAlign: "left",
                width: "100%",
                cursor: "pointer",
              }}
            >
              {/* The same initials badge the trigger wears, so the row a reader picks
                  and the card it becomes are recognisably the same thing. */}
              <span
                aria-hidden
                css={{
                  gridRow: "1 / span 2",
                  display: "grid",
                  placeItems: "center",
                  width: 28,
                  height: 28,
                  borderRadius: theme.radius.sm,
                  background: isCurrent
                    ? `${theme.color.primary}33`
                    : `${theme.color.text}0f`,
                  color: isCurrent ? theme.color.primaryText : theme.color.textMuted,
                  fontSize: 11,
                  fontWeight: 600,
                  letterSpacing: 0.3,
                }}
              >
                {row.agentTemplate.slice(0, 2).toUpperCase()}
              </span>

              <Text
                ellipsis
                css={{
                  fontSize: 13,
                  lineHeight: 1.35,
                  color: isCurrent ? theme.color.primaryText : theme.color.text,
                }}
              >
                {row.agentTemplate}
              </Text>
              <Text
                ellipsis
                css={{ fontSize: 11, lineHeight: 1.35, color: theme.color.textMuted }}
              >
                on {row.harness} · {namespace}
              </Text>
            </button>
          );
        })}

        {/*
          Rows of the same size as the real ones while they are on their way.
          
          The list rendered nothing at all until the agents arrived, so the panel opened
          at the height of its search field and then jumped to the height of a list —
          under a pointer that was already moving toward where the first row was about
          to be. Standing in for three rows is enough to hold the shape: the panel
          scrolls beyond that anyway, so being wrong about the count costs nothing.
        */}
        {templates.isLoading
          ? [0, 1, 2].map((row) => (
              <div
                key={row}
                data-testid="agent-switcher-loading"
                aria-hidden
                css={{
                  height: 46,
                  borderRadius: theme.radius.sm,
                  background: theme.color.bgElevated,
                  opacity: 0.6,
                }}
              />
            ))
          : null}

        {!templates.isLoading && matches.length === 0 ? (
          <Text
            data-testid="agent-switcher-empty"
            css={{ fontSize: 12, color: theme.color.textMuted, padding: theme.space(2) }}
          >
            {query.trim()
              ? "No other agent matches that."
              : "No other agents on this cluster."}
          </Text>
        ) : null}
      </div>
    </div>
  );
}
