import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Alert, Button, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { RefreshButton } from "@/components/table/RefreshButton";
import { useTheme } from "@emotion/react";
import { paths } from "@/router/routes";
import {
  agentPairsFrom,
  unmappedAgent,
  UNMAPPED_AGENT_NAME,
  pairIdOfInstance,
  useAgentInstancesAcrossNamespaces,
  useAgentTemplatesAcrossNamespaces,
  useNamespaces,
  type AgentPair,
} from "@/api";
import { agentNewChatUrl } from "@/components/agent/agentUrl";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import {
  byNumber,
  byText,
  listTableChange,
  matchesQuery,
  paginationFor,
  sortOrderFor,
} from "@/components/table/listTable";

const { Text } = Typography;

const FILTER_IDS: readonly string[] = ["ns"];
const PAGE_SIZE = 25;

/**
 * The agents in the cluster: one row per `(AgentTemplate, Harness)` pair.
 *
 * ## Why this page lists pairs and not `AgentInstance`s
 *
 * Because an instance is a *conversation*, not an agent. The A2A gateway files every
 * task under its instance as the task's `contextId`, so an instance is one thread of
 * turns — and this page, listing them under a heading of "Agents", was listing
 * conversations. Every symptom followed from that: nothing to search on, two rows
 * from one template that could not be told apart, and a "create agent" form that was
 * two dropdowns because it was not creating an agent.
 *
 * The durable, runnable thing is the pair. `agent_template_harness_pair` is a real
 * table with a revision lifecycle of its own, and it is already on the wire as
 * `AgentTemplate.status.harnesses[]` — one entry per pair — which is what the
 * templates page's "Runs on" column reads. So listing agents costs no new RPC and no
 * new service: it is the template read, regrouped. See `api/domain/agentPairs`.
 *
 * ## Why one template can be two rows
 *
 * A harness admits templates by label selector, and two harnesses can select the
 * same one. That is genuinely two agents — the same behaviour on two runtimes, each
 * with its own prepared revision and its own conversations — so it is two rows,
 * distinguished by the harness column. Collapsing them would hide the one thing that
 * differs.
 *
 * ## Why the search and sort are in the browser, and why that is honest here
 *
 * `ListAgentTemplates` takes exactly one field: a namespace. No page, no filter, no
 * sort. So the whole list arrives in one read and narrowing it in the browser covers
 * every row there is — which the note under the table says, naming the RPC, because
 * a search box that only searched the page in front of you looks identical to one
 * that searched everything.
 *
 * ## Why the conversation counts can be partial, and say so
 *
 * They come from `ListAgentInstances`, which has no cross-namespace read — so a wide
 * count is a request per namespace, and each namespace is authorised on its own. A
 * refused one is named rather than quietly making a column read lower than the truth.
 */
export function AgentsTab() {
  const theme = useTheme();
  const navigate = useNavigate();
  const view = useListView(FILTER_IDS);
  const selectedNamespaces = view.selected("ns");
  const namespaces = useNamespaces();

  const namespaceNames = useMemo(
    () => (namespaces.data ?? []).map((entry) => entry.name),
    [namespaces.data],
  );

  /*
   * Every template, read one namespace at a time.
   *
   * This was a single unscoped call, on the reasoning that `ListAgentTemplates`
   * returns everything anyway so a request per namespace would be more round trips
   * for a smaller answer. **The reasoning was sound and the premise was false.** The
   * service validates its namespace first and answers `InvalidArgument: namespace is
   * required` for an empty one — it is not a wildcard. Against a real controller this
   * page failed to load entirely, while the fixture backend served the unscoped read
   * happily, so nothing in the suite objected.
   *
   * A request per namespace is therefore what "all namespaces" costs here, exactly as
   * it does for conversations. Refusals are named rather than silently shortening the
   * list.
   */
  /*
   * The namespaces actually read: the reader's choice when they have made one, and
   * every namespace when they have not.
   *
   * Scoping the *read* rather than reading everything and narrowing afterwards. The
   * filter is a multi-select over namespaces, and each namespace costs a round trip
   * here, so honouring the choice is cheaper as well as more honest — a page that
   * reads twelve namespaces to show one is doing eleven reads it was told not to.
   */
  const readNamespaces = useMemo(
    () => (selectedNamespaces.length > 0 ? selectedNamespaces : namespaceNames),
    [selectedNamespaces, namespaceNames],
  );

  const templates = useAgentTemplatesAcrossNamespaces(readNamespaces);

  /*
   * Whichever read stopped this page from having anything to show.
   *
   * The namespace list first, because the template read depends on it: reporting
   * "no templates" when the namespaces could not be listed names the wrong call.
   */
  const loadFailure = namespaces.error ?? templates.error;


  /*
   * Every conversation, so each agent can carry a count of its own.
   *
   * Always `all_creators`: a count of "conversations with this agent" that silently
   * meant "conversations *you* have had with this agent" would be a number nobody
   * could interpret, and the switch that used to ask about it is gone — the list
   * should show everyone's work, which was the decision.
   */
  const conversations = useAgentInstancesAcrossNamespaces(namespaceNames, true);

  const conversationCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const instance of conversations.data?.instances ?? []) {
      const pairId = pairIdOfInstance(instance);
      if (!pairId) continue;
      counts.set(pairId, (counts.get(pairId) ?? 0) + 1);
    }
    return counts;
  }, [conversations.data]);

  const agents = useMemo(
    () => agentPairsFrom(templates.data?.templates ?? []),
    [templates.data],
  );

  /**
   * Conversations whose agent is not on this page.
   *
   * A real state, not a bug: deleting a template does not stop the conversations cut
   * from it — an instance runs from the prepared revision it was built against, and
   * that revision is a separate row the schema protects. So a conversation can
   * outlive the template it names, and a pair is also retired when a harness stops
   * admitting the template. Either way the conversation is still there and is no
   * longer reachable from this list, which is worth one sentence rather than being
   * left as a silent shortfall between two numbers.
   */
  const refreshThisTab = async () => {
    await Promise.all([templates.refresh(), conversations.refresh()]);
  };

  const orphanedConversations = useMemo(() => {
    if (!templates.data || !conversations.data) return 0;
    const known = new Set(agents.map((agent) => agent.id));
    /*
     * A namespace whose templates could not be read tells us nothing about its
     * conversations.
     *
     * Templates and conversations are read one namespace at a time, and either can be
     * refused on its own. When the template read for a namespace fails, none of its
     * pairs are in `known` — so every conversation in it looks stranded, and the page
     * says so beside a separate notice explaining that the namespace could not be read
     * at all. Two notices about one failure, one of them an accusation about
     * conversations that are almost certainly fine.
     *
     * "Not known to be orphaned" is the honest reading of a namespace we could not
     * look in, so those are left out of the count entirely.
     */
    const unreadable = new Set(
      (templates.data.refused ?? []).map((entry) => entry.namespace),
    );
    return (conversations.data.instances ?? []).filter((instance) => {
      if (unreadable.has(instance.namespace)) return false;
      const pairId = pairIdOfInstance(instance);
      return pairId === undefined || !known.has(pairId);
    }).length;
  }, [agents, conversations.data, templates.data]);

  /*
   * The agents, plus a stand-in for the conversations that belong to none of them.
   *
   * Only when there are some: a permanent row for an empty case is a row every reader
   * has to learn to ignore. It goes first because it is the exception — a reader
   * scanning for their agent should not have to notice it, and a reader who came
   * because of the notice above should not have to hunt for it.
   */
  const rowsWithUnmapped = useMemo(
    () =>
      orphanedConversations > 0
        ? [unmappedAgent(selectedNamespaces[0] ?? namespaceNames[0] ?? ""), ...agents]
        : agents,
    [agents, orphanedConversations, selectedNamespaces, namespaceNames],
  );

  const namespaceOptions = useMemo(
    () => (namespaces.data ?? []).map((entry) => ({ value: entry.name })),
    [namespaces.data],
  );


  const matching = useMemo(
    () =>
      rowsWithUnmapped.filter((agent) => {
        /*
         * The stand-in row ignores the namespace filter but not the search.
         *
         * It belongs to no namespace in the sense the filter means — its conversations
         * are gathered from all of them — so hiding it because a namespace was chosen
         * would take away the only way to reach them. But a reader typing a name is
         * hunting a particular agent, and a row that always matched would be one they
         * had to look past every time.
         */
        if (
          !agent.isUnmapped &&
          selectedNamespaces.length > 0 &&
          !selectedNamespaces.includes(agent.namespace)
        ) {
          return false;
        }
        // Both halves of the pair and the template's description, because all three
        // are on the row: somebody hunting an agent may remember what it does rather
        // than what it is called.
        return matchesQuery(view.query, [
          agent.agentTemplate,
          agent.harness,
          agent.namespace,
          agent.description,
        ]);
      }),
    [rowsWithUnmapped, selectedNamespaces, view.query],
  );

  /**
   * Ordered by name, with the stand-in row last.
   *
   * A default order at all, because the list arrived in whatever order the namespaces
   * were read in — stable within a read and meaningless to a reader, so an agent moved
   * when an unrelated namespace answered more slowly.
   *
   * `Unmapped conversations` is pinned to the bottom whichever way the sort runs. It is
   * not an agent: it is a stand-in for conversations whose pair no longer exists, and
   * sorting it among real agents by its name would put it in the middle of the list on
   * a `U`. A reader looking for their agents should not have to look past it.
   *
   * Only when nothing else is chosen — a reader who has clicked a column heading has
   * asked for something, and antd applies it to what this hands over.
   */
  const filtered = useMemo(() => {
    const stranded = matching.filter((agent) => agent.isUnmapped);
    const real = matching.filter((agent) => !agent.isUnmapped);
    if (!view.sort) {
      real.sort((left, right) => left.agentTemplate.localeCompare(right.agentTemplate));
    }
    return [...real, ...stranded];
  }, [matching, view]);

  const refused = conversations.data?.refused ?? [];

  const columns = useMemo<ColumnsType<AgentPair>>(
    () => [
      {
        title: "Agent",
        key: "agentTemplate",
        sorter: byText<AgentPair>((row) => row.agentTemplate),
        sortOrder: sortOrderFor(view, "agentTemplate"),
        render: (_, row) => (
          <Space orientation="vertical" size={0}>
            {/* An agent is named by its template — there is nothing else to name it
                by. A pair is derived rather than authored, so no RPC could store a
                name for one even if there were somewhere to type it. */}
            <Link
              // Straight into a conversation with it, which is what a reader clicking
              // an agent's name is after. Nothing is created until they send something,
              // and the rail on that page lists the conversations they already have.
              // The stand-in row has no agent to start a conversation with — that is
              // the condition it describes — so it goes to the list of the
              // conversations it stands for instead.
              to={
                row.isUnmapped
                  ? paths.agentsUnmapped
                  : (agentNewChatUrl(row) ?? paths.agents)
              }
              data-testid={`agent-link-${row.namespace}-${row.agentTemplate}-${row.harness}`}
              css={{ fontFamily: theme.font.mono, color: theme.color.primaryText }}
            >
              {row.agentTemplate}
            </Link>
            {row.description ? (
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                {row.description}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        title: "Namespace",
        key: "namespace",
        width: 150,
        sorter: byText<AgentPair>((row) => row.namespace),
        sortOrder: sortOrderFor(view, "namespace"),
        render: (_, row) => (
          <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
            {row.namespace}
          </Text>
        ),
      },
      {
        // The column that tells two agents cut from one template apart, which is
        // why it is beside the name rather than at the end of the row.
        title: "Runs on",
        key: "harness",
        width: 180,
        sorter: byText<AgentPair>((row) => row.harness),
        sortOrder: sortOrderFor(view, "harness"),
        render: (_, row) => (
          <Text
            css={{ fontFamily: theme.font.mono, fontSize: 12 }}
            data-testid={`agent-harness-${row.id}`}
          >
            {row.harness}
          </Text>
        ),
      },
      {
        title: "Revision",
        key: "revisionState",
        width: 150,
        sorter: byText<AgentPair>((row) => row.revisionState),
        sortOrder: sortOrderFor(view, "revisionState"),
        render: (_, row) => <RevisionTag agent={row} />,
      },
      {
        title: "Conversations",
        key: "conversations",
        width: 150,
        sorter: byNumber<AgentPair>((row) => conversationCounts.get(row.id) ?? 0),
        sortOrder: sortOrderFor(view, "conversations"),
        render: (_, row) => {
          // Until the read lands there is no count, and a confident `0` would be a
          // claim this page has not earned — indistinguishable on screen from an
          // agent nobody has ever talked to.
          if (conversations.error || !conversations.data) {
            return (
              <Text
                css={{ color: theme.color.textMuted, fontSize: 12 }}
                data-not-reported="true"
              >
                Not counted
              </Text>
            );
          }
          const count = conversationCounts.get(row.id) ?? 0;
          return (
            <Text data-testid={`agent-conversations-${row.id}`}>
              {count} {count === 1 ? "conversation" : "conversations"}
            </Text>
          );
        },
      },
    ],
    [conversationCounts, conversations.data, conversations.error, theme, view],
  );

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
      {/* Said on the list it is about, rather than in the overview above: this is the
          answer to "why is there no New agent button", and it is wanted by somebody
          looking at the list rather than by somebody reading the model. */}
      <Text data-testid="agents-derived-note" css={{ color: theme.color.textMuted }}>
        The agents list is populated automatically from the template and harness
        configurations available.
      </Text>

        {/*
         * The namespace read counts as a failure of this page, not as a detail.
         *
         * Templates are read one namespace at a time, so the namespace list is an
         * input to the read rather than a nicety beside it. When it fails there are
         * no namespaces to iterate, the template read never runs, and without this
         * the page would sit at "no agents" — an empty state describing a backend
         * that was never asked. That is the failure this whole spec exists to catch,
         * and introducing the per-namespace read is what re-opened it.
         */}
        {loadFailure ? (
          <Alert
            type="error"
            showIcon
            title="Could not load agents"
            description={loadFailure.message}
            data-testid="agents-error"
            action={
              <Button
                size="small"
                onClick={() => {
                  void namespaces.refresh();
                  void templates.refresh();
                }}
              >
                Try again
              </Button>
            }
          />
        ) : null}

        {/* The counts are the only thing this failure costs, so it is said as that
            rather than as a failure of the page: the agents themselves came from a
            different read and are on screen behind it. */}
        {conversations.error && !loadFailure ? (
          <Alert
            type="warning"
            showIcon
            title="Could not count conversations"
            description={`${conversations.error.message} The agents below are from a separate read and are complete.`}
            data-testid="agents-counts-error"
            action={
              <Button size="small" onClick={() => void conversations.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {/* Partial by design: conversations are read one namespace at a time and each
            is authorised on its own, so a reader can be allowed six and refused the
            seventh. Naming them is the difference between a lower count and a lower
            count you know about. */}
        {refused.length > 0 ? (
          <Alert
            type="warning"
            showIcon
            data-testid="agents-counts-refused"
            title={
              refused.length === 1
                ? `Conversations in one namespace could not be counted: ${refused[0].namespace}`
                : `Conversations in ${refused.length} namespaces could not be counted`
            }
            description={refused
              .map((entry) => `${entry.namespace}: ${entry.reason}`)
              .join(" · ")}
          />
        ) : null}

        {orphanedConversations > 0 ? (
          <Alert
            type="info"
            showIcon
            data-testid="agents-orphaned-conversations"
            title={`${orphanedConversations} ${orphanedConversations === 1 ? "conversation is" : "conversations are"} not listed under any agent here`}
            description={`Each was cut from a template and harness that no longer pair — the template was deleted, or the harness stopped admitting it. They still run from the revision they were built against. They are gathered under ${UNMAPPED_AGENT_NAME} in the list below, where they can be opened and deleted.`}
          />
        ) : null}

        <FilterBar
          testId="agents-filters"
          view={view}
          search={{
            label: "Search agents by template, harness or description",
            placeholder: "Search agents",
          }}
          filters={[
            {
              id: "ns",
              label: "Namespace",
              allLabel: "All namespaces",
              options: namespaceOptions,
            },
          ]}
          trailing={
            <Space size={8}>
              {!loadFailure && !templates.isLoading ? (
                <Text data-testid="agents-summary" css={{ color: theme.color.textMuted }}>
                  {filtered.length} of {agents.length}{" "}
                  {agents.length === 1 ? "agent" : "agents"}
                </Text>
              ) : null}
              {/* Beside the controls that narrow this list, and it refreshes this list:
                  a control in a table's own filter row that quietly re-read two other
                  tabs would be doing more than it appears to. */}
              <RefreshButton onRefresh={refreshThisTab} what="Agents" />
            </Space>
          }
        />

        <Table<AgentPair>
          data-testid="agents-table"
          rowKey={(row) => row.id}
          columns={columns}
          // A failure has its own banner above; leaving the rows out keeps the table
          // from also claiming the cluster is running nothing.
          dataSource={loadFailure ? [] : filtered}
          loading={templates.isLoading}
          onChange={listTableChange<AgentPair>(view)}
          pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
          locale={{
            emptyText: loadFailure
              ? " "
              : view.isNarrowed
                ? "No agents match those filters."
                : templates.data && agents.length === 0
                  ? // Two different facts, and the second is the one worth acting
                    // on: a template no harness admits reaches no prepared revision
                    // and can never become an agent, so a cluster full of templates
                    // and empty of agents is a cluster with an admission problem.
                    templates.data.templates.length > 0
                    ? "No agents yet. There are agent templates, but no harness admits any of them — a template a harness does not admit never reaches a revision, so it cannot be run."
                    : "No agents yet."
                  : " ",
          }}
          onRow={(row) => ({
            className: "clickable-table-row",
            onClick: (event) => {
              // Anything itself interactive handles its own click. One rule rather
              // than a `stopPropagation` per control, so a control added later
              // cannot silently inherit the row's navigation.
              if (
                (event.target as HTMLElement).closest(
                  "a, button, input, [role='button'], .ant-popover, .ant-dropdown",
                )
              ) {
                return;
              }
              const destination = row.isUnmapped
                ? paths.agentsUnmapped
                : agentNewChatUrl(row);
              if (destination) void navigate(destination);
            },
          })}
        />


    </Space>
  );
}

/**
 * Whether the controller has built something this agent can run.
 *
 * Three states rather than a tick or a cross, because "not ready" covers two very
 * different things: a revision still being prepared, which is ordinary and
 * self-correcting, and a controller that has said nothing at all, which is not a
 * failure either and must not be reported as one. `AgentRevisionState` in
 * `domain/agentPairs` is where the distinction is drawn.
 */
function RevisionTag({ agent }: { agent: AgentPair }) {
  const theme = useTheme();

  const appearance = {
    ready: { label: "Ready", color: "success" as const },
    preparing: { label: "Preparing", color: "processing" as const },
    notReported: { label: "Not reported", color: "default" as const },
  }[agent.revisionState];

  const tag = (
    <Tag
      color={appearance.color}
      data-testid={`agent-revision-${agent.id}`}
      data-revision-state={agent.revisionState}
      css={{ marginInlineEnd: 0 }}
    >
      {appearance.label}
    </Tag>
  );

  const explanation =
    agent.revisionState === "ready"
      ? agent.latestSuccessfulRevision
      : (agent.notReadyReason ??
        (agent.revisionState === "preparing"
          ? "A revision is desired and none has succeeded yet."
          : "The controller has not reported a revision for this pair."));

  return explanation ? (
    <Tooltip title={explanation}>
      <span css={{ color: theme.color.text }}>{tag}</span>
    </Tooltip>
  ) : (
    tag
  );
}
