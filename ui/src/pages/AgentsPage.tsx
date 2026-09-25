import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Alert, Button, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { RefreshButton } from "@/components/table/RefreshButton";
import { useTheme } from "@emotion/react";
import { paths } from "@/router/routes";
import {
  agentSummariesFrom,
  useAgentTemplatesAcrossNamespaces,
  unmappedAgent,
  UNMAPPED_AGENT_NAME,
  agentRefOfInstance,
  useAgentInstances,
  useAgentsAcrossNamespaces,
  useNamespaces,
  type AgentSummary,
} from "@/api";
import { agentNewChatUrl } from "@/components/agent/agentUrl";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import { clickableRow } from "@/components/table/rowClick";
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

  const definitions = useAgentsAcrossNamespaces(readNamespaces);
  const templateNamespaces = useMemo(
    () => [...new Set((definitions.data?.agents ?? [])
      .filter((agent) => agent.resource.spec.templateRef)
      .map((agent) => agent.namespace))],
    [definitions.data],
  );
  const templates = useAgentTemplatesAcrossNamespaces(templateNamespaces);

  // The namespace read precedes the Agent read.
  const loadFailure = namespaces.error ?? definitions.error;

  /*
   * Every conversation, so each agent can carry a count of its own.
   *
   * Always `all_creators`: a count of "conversations with this agent" that silently
   * meant "conversations *you* have had with this agent" would be a number nobody
   * could interpret, and the switch that used to ask about it is gone — the list
   * should show everyone's work, which was the decision.
   */
  const conversations = useAgentInstances(true);

  const conversationCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const instance of conversations.data ?? []) {
      const agentRef = agentRefOfInstance(instance);
      if (!agentRef) continue;
      counts.set(agentRef, (counts.get(agentRef) ?? 0) + 1);
    }
    return counts;
  }, [conversations.data]);

  const agents = useMemo(
    () => agentSummariesFrom(definitions.data?.agents ?? [], templates.data?.templates),
    [definitions.data, templates.data],
  );


  const refreshThisTab = async () => {
    await Promise.all([definitions.refresh(), templates.refresh(), conversations.refresh()]);
  };

  const orphanedConversations = useMemo(() => {
    if (!definitions.data || !conversations.data) return 0;
    const known = new Set(agents.map((agent) => agent.id));
    // A refused Agent read cannot establish whether its conversations are orphaned.
    const unreadable = new Set(
      (definitions.data.refused ?? []).map((entry) => entry.namespace),
    );
    return (conversations.data ?? []).filter((instance) => {
      const namespace = instance.agent?.split("/")[0];
      if (namespace && (!readNamespaces.includes(namespace) || unreadable.has(namespace))) return false;
      const agentRef = agentRefOfInstance(instance);
      return agentRef === undefined || !known.has(agentRef);
    }).length;
  }, [agents, conversations.data, definitions.data, readNamespaces]);

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
        // Both configuration choices and the template's description, because all three
        // are on the row: somebody hunting an agent may remember what it does rather
        // than what it is called.
        return matchesQuery(view.query, [
          agent.name,
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
   * not an agent: it is a stand-in for conversations whose Agent no longer exists, and
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
      real.sort((left, right) => left.name.localeCompare(right.name));
    }
    return [...real, ...stranded];
  }, [matching, view]);

  const columns = useMemo<ColumnsType<AgentSummary>>(
    () => [
      {
        title: "Agent",
        key: "name",
        sorter: byText<AgentSummary>((row) => row.name),
        sortOrder: sortOrderFor(view, "name"),
        render: (_, row) => (
          <Space orientation="vertical" size={0}>

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
              data-testid={`agent-link-${row.namespace}-${row.name}-${row.harness}`}
              css={{ fontFamily: theme.font.mono, color: theme.color.primaryText }}
            >
              {row.name}
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
        sorter: byText<AgentSummary>((row) => row.namespace),
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
        sorter: byText<AgentSummary>((row) => row.harness),
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
        sorter: byText<AgentSummary>((row) => row.revisionState),
        sortOrder: sortOrderFor(view, "revisionState"),
        render: (_, row) => <RevisionTag agent={row} />,
      },
      {
        title: "Conversations",
        key: "conversations",
        width: 150,
        sorter: byNumber<AgentSummary>((row) => conversationCounts.get(row.id) ?? 0),
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
          const count = row.isUnmapped ? orphanedConversations : conversationCounts.get(row.id) ?? 0;
          return (
            <Text data-testid={`agent-conversations-${row.id}`}>
              {count} {count === 1 ? "conversation" : "conversations"}
            </Text>
          );
        },
      },
    ],
    [conversationCounts, conversations.data, conversations.error, orphanedConversations, theme, view],
  );

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>

      <Text data-testid="agents-derived-note" css={{ color: theme.color.textMuted }}>
        An Agent combines template and Harness
        configurations.
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
                  void definitions.refresh();
                }}
              >
                Try again
              </Button>
            }
          />
        ) : null}

        {templates.error || templates.data?.refused.length ? (
          <Alert type="warning" showIcon title="Some agent descriptions could not be loaded"
            description={templates.error?.message ?? templates.data?.refused.map((entry) => `${entry.namespace}: ${entry.reason}`).join("; ")}
            data-testid="agent-descriptions-error" />
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

        {orphanedConversations > 0 ? (
          <Alert
            type="info"
            showIcon
            data-testid="agents-orphaned-conversations"
            title={`${orphanedConversations} ${orphanedConversations === 1 ? "conversation is" : "conversations are"} not listed under any agent here`}
            description={`Their Agent definition no longer exists. They still run from the revision they were built against. They are gathered under ${UNMAPPED_AGENT_NAME} in the list below, where they can be opened and deleted.`}
          />
        ) : null}

        <FilterBar
          testId="agents-filters"
          view={view}
          search={{
            label: "Search agents by name, template or harness",
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
              {!loadFailure && !definitions.isLoading ? (
                <Text data-testid="agents-summary" css={{ color: theme.color.textMuted }}>
                  {filtered.filter((agent) => !agent.isUnmapped).length} of {agents.length}{" "}
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

        <Table<AgentSummary>
          data-testid="agents-table"
          rowKey={(row) => row.id}
          columns={columns}
          // A failure has its own banner above; leaving the rows out keeps the table
          // from also claiming the cluster is running nothing.
          dataSource={loadFailure ? [] : filtered}
          loading={definitions.isLoading}
          onChange={listTableChange<AgentSummary>(view)}
          pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
          locale={{
            emptyText: loadFailure
              ? " "
              : view.isNarrowed
                ? "No agents match those filters."
                : definitions.data && agents.length === 0
                  ? // Two different facts, and the second is the one worth acting
                    definitions.data.agents.length > 0
                    ? "No Agents yet. Create an Agent with a template and a Harness."
                    : "No agents yet."
                  : " ",
          }}
          onRow={(row) =>
            clickableRow(() => {
              const destination = row.isUnmapped
                ? paths.agentsUnmapped
                : agentNewChatUrl(row);
              if (destination) void navigate(destination);
            })
          }
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
 * `domain/agentSummaries` is where the distinction is drawn.
 */
function RevisionTag({ agent }: { agent: AgentSummary }) {
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
          : "The controller has not reported a revision for this Agent."));

  return explanation ? (
    <Tooltip title={explanation}>
      <span css={{ color: theme.color.text }}>{tag}</span>
    </Tooltip>
  ) : (
    tag
  );
}
