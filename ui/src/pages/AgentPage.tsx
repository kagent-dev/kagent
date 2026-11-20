import { useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Skeleton, Space, Table, Tabs, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { MessageSquarePlus, Pencil } from "lucide-react";
import { useTheme } from "@emotion/react";
import { AgentRail } from "@/components/agent/AgentRail";
import { AgentStatusTag } from "@/components/agent/AgentStatusTag";
import { AgentSchedules } from "@/components/agent/AgentSchedules";
import { useAgentSchedules } from "@/components/agent/useAgentSchedules";
import { AgentStructure, AgentStructureSkeleton } from "@/components/agent/AgentStructure";
import { PageFrame } from "@/components/Structure/PageFrame";
import { buildPath, paths } from "@/router/routes";
import { useUrlState } from "@/router/useUrlState";
import {
  ExtensionSlot,
  useExtensionAgentLinks,
  useExtensionTableColumns,
  withExtensionColumns,
} from "@/appExtensions";
import {
  apiClient,
  agentUpdateFailedReason,
  isNotFound,
  newConversationBlockedReason,
  useAgentConversations,
  useAgent,
  useInvalidateAgents,
  type AgentInstance,
} from "@/api";
import { agentPageUrl, agentUrl } from "@/components/agent/agentUrl";
import { useConversationTitles } from "@/api/hooks/useConversationTitles";
import { byNewestFirst } from "@/components/agent-instances/conversationOrder";
import { StateTag, ValueOrNotReported } from "@/components/agent-instances/InstanceTags";
import {
  conversationTitle,
  relativeAge,
  shortInstanceId,
} from "@/components/agent-instances/instanceLabels";
import { RenameConversationButton } from "@/components/agent-instances/RenameConversationButton";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import { clickableRow } from "@/components/table/rowClick";
import {
  byText,
  listTableChange,
  matchesQuery,
  paginationFor,
  sortOrderFor,
} from "@/components/table/listTable";

const { Text, Title } = Typography;

/** A muted field name with its value below it, for the row under the page title. */
function MetaField({ label, children }: { label: string; children: ReactNode }) {
  const theme = useTheme();
  return (
    <span css={{ display: "inline-flex", flexDirection: "column", alignItems: "flex-start", gap: theme.space(1), minWidth: 0 }}>
      <span>{label}</span>
      {/* One height and font size for every value, so a tag and plain text share a centre line. */}
      <span css={{ color: theme.color.text, display: "inline-flex", alignItems: "center", height: 24, fontSize: 12, lineHeight: 1 }}>{children}</span>
    </span>
  );
}

const TAB_KEYS = ["overview", "conversations", "schedules"] as const;
type TabKey = (typeof TAB_KEYS)[number];
const FILTER_IDS: readonly string[] = ["state"];
const PAGE_SIZE = 25;

/** An Agent definition and the conversations pinned to its revisions. */
export function AgentPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const view = useListView(FILTER_IDS);
  const definition = useAgent(namespace, name);
  const invalidateAgents = useInvalidateAgents();
  const conversations = useAgentConversations(namespace, name);
  const agent = definition.data;
  const [tab, setTab] = useUrlState("tab", "overview");
  // An unknown `?tab=` shows the default tab.
  const activeTab: TabKey = TAB_KEYS.find((key) => key === tab) ?? "overview";
  const schedules = useAgentSchedules({ namespace: namespace ?? "", name: name ?? "" });
  const agentMissing = definition.error !== undefined && isNotFound(definition.error);

  /*
   * Memoised rather than defaulted inline: `?? []` builds a new array on every
   * render while the read is in flight, and everything below depends on it — so the
   * filter, the state options and the columns would all rebuild continuously, and
   * antd rebuilds a table's internal column state whenever its columns change.
   */
  // Newest first, the order the rail lists the same conversations in.
  const rows = useMemo(
    () => [...(conversations.data?.all ?? [])].sort(byNewestFirst),
    [conversations.data],
  );
  // The rail's title for each unnamed conversation, so the two lists name them alike.
  const derivedTitles = useConversationTitles(rows);
  const openableIds = useMemo(
    () => conversations.data && new Set(conversations.data.own.map((row) => row.id)),
    [conversations.data],
  );

  const stateOptions = useMemo(() => {
    // Built from the rows rather than from the enum: offering all eight states on a
    // list of three conversations gives a reader six choices that produce nothing.
    const seen = [...new Set(rows.map((row) => row.state))].sort();
    return seen.map((state) => ({ value: state }));
  }, [rows]);

  const selectedStates = view.selected("state");

  const filtered = useMemo(
    () =>
      rows.filter((row) => {
        if (selectedStates.length > 0 && !selectedStates.includes(row.state)) {
          return false;
        }
        // The id as well as the title: an id is what a bug report or a log line
        // carries, and it is the only handle an untitled conversation has.
        return matchesQuery(view.query, [
          conversationTitle(row, derivedTitles[row.id]),
          row.id,
          row.creator,
        ]);
      }),
    [rows, derivedTitles, selectedStates, view.query],
  );

  /**
   * Where a conversation row leads.
   *
   * A distribution serving its own chat redirects it through
   * `agentLinks.fromInstancesList` — which is where instance rows are listed now — and
   * a contribution that throws or answers with nothing falls back to this
   * application's own route rather than to a dead link.
   */
  const { fromInstancesList } = useExtensionAgentLinks();
  const chatPath = (row: AgentInstance) => {
    const own = agentUrl.chat({ id: row.id });
    if (!fromInstancesList) return own;
    try {
      const destination = fromInstancesList(row);
      return destination.trim() === "" ? own : destination;
    } catch {
      return own;
    }
  };

  const blockedReason = agent ? newConversationBlockedReason(agent) : undefined;
  const updateFailedReason = agent ? agentUpdateFailedReason(agent) : undefined;

  /*
   * Goes to the new-conversation page rather than creating one here.
   *
   * Creating on the click is what left nine empty conversations on the live cluster —
   * every visit that changed its mind kept its instance, and an instance holds a
   * prepared revision that is not collected when the last one referencing it goes. The
   * create now belongs to the first message; see `AgentNewChatPage`.
   */
  function startConversation(): void {
    if (namespace && name) navigate(buildPath(paths.agentNewChat, { namespace, name }));
  }

  async function removeAgent(): Promise<void> {
    if (namespace && name) await apiClient.agentBuildingBlocks.removeAgent(namespace, name);
  }

  /*
   * The same picking behaviour the rail has, on the table.
   *
   * A reader clearing out an agent will do it from whichever surface they are on, and
   * one that offers it while the other does not is a difference they have to learn.
   * Only their own conversations can be ticked: an instance is scoped to its creator on
   * write, so offering a checkbox beside somebody else's would be offering a delete
   * that is refused.
   */
  const [selectedIds, setSelectedIds] = useState<readonly string[]>([]);
  const [isBulkDeleting, setBulkDeleting] = useState(false);

  async function deleteSelected(force = false): Promise<void> {
    setBulkDeleting(true);
    try {
      const targets = rows.filter((row) => selectedIds.includes(row.id));
      await Promise.all(
        targets.map((row) => apiClient.agentInstances.remove(row.id, { force })),
      );
      await conversations.refresh();
      setSelectedIds([]);
    } finally {
      setBulkDeleting(false);
    }
  }

  const extensionColumns = useExtensionTableColumns<AgentInstance>(
    "app_agents_agentsList_table",
  );

  const columns = useMemo<ColumnsType<AgentInstance>>(
    () => [
      {
        title: "Conversation",
        key: "name",
        sorter: byText<AgentInstance>((row) => conversationTitle(row, derivedTitles[row.id])),
        sortOrder: sortOrderFor(view, "name"),
        render: (_, row) => {
          const title = conversationTitle(row, derivedTitles[row.id]);
          // Until the read lands nothing is known about who may open what, so
          // nothing is claimed: treating an unread answer as "not yours" would
          // strip the links off a list that is about to be perfectly openable.
          const openable = openableIds === undefined || openableIds.has(row.id);

          return (
            <Space size={8}>
              {openable ? (
                <Link
                  to={chatPath(row)}
                  aria-label={`Open conversation ${title}`}
                  data-testid={`conversation-link-${row.id}`}
                  css={{ color: theme.color.primaryText }}
                >
                  {title}
                </Link>
              ) : (
                /* No link, deliberately. `GetAgentInstance` is scoped to its
                   creator and the A2A gateway reads through the same call, so this
                   conversation answers NotFound to everyone but the person who
                   started it. A link here would be an invitation to a 404. */
                <Tooltip
                  title="Only the conversation’s creator can open it. Other callers need a share link."
                >
                  <Text
                    data-testid={`conversation-unopenable-${row.id}`}
                    css={{ color: theme.color.textMuted }}
                  >
                    {title}
                  </Text>
                </Tooltip>
              )}
              <ExtensionSlot
                id="app_agents_agentsList_agentListItem_badge"
                context={{ agentName: row.id, namespace: row.agent?.split("/")[0] ?? "" }}
              />
            </Space>
          );
        },
      },
      {
        title: "ID",
        key: "id",
        width: 110,
        render: (_, row) => (
          <Tooltip title={row.id}>
            <Text css={{ fontFamily: theme.font.mono, fontSize: 12, whiteSpace: "nowrap" }}>
              {shortInstanceId(row.id)}
            </Text>
          </Tooltip>
        ),
      },
      {
        title: "State",
        key: "state",
        width: 130,
        render: (_, row) => <StateTag state={row.state} testId={`state-${row.id}`} />,
      },
      {
        title: "Creator ID",
        key: "creator",
        width: 180,
        sorter: byText<AgentInstance>((row) => row.creator),
        sortOrder: sortOrderFor(view, "creator"),
        render: (_, row) => <ValueOrNotReported value={row.creator} />,
      },
      {
        // `updatedAt` rather than `createdAt`: the question a list of conversations
        // answers is "which one was I last in", and the controller stamps this on
        // every transition.
        title: "Last active",
        key: "updatedAt",
        width: 140,
        sorter: byText<AgentInstance>((row) => row.updatedAt),
        sortOrder: sortOrderFor(view, "updatedAt"),
        render: (_, row) =>
          row.updatedAt ? (
            <Tooltip title={row.updatedAt}>
              <Text css={{ fontSize: 12 }}>{relativeAge(row.updatedAt)}</Text>
            </Tooltip>
          ) : (
            <ValueOrNotReported value={undefined} />
          ),
      },
      ...withExtensionColumns([], extensionColumns),
      {
        title: "",
        key: "actions",
        width: 96,
        render: (_, row) => {
          // Both writes are scoped to the creator exactly as the read is, so a
          // conversation somebody else started can be neither retitled nor deleted.
          // Offered and refused would be worse than plainly unavailable.
          const mine = openableIds === undefined || openableIds.has(row.id);
          return (
            <Space size={0}>
              <RenameConversationButton instance={row} disabled={!mine} />
              <DeleteResourceButton
                kind="conversation"
                name={conversationTitle(row, derivedTitles[row.id])}
                disabled={!mine}
                forceLabel="Stop active turns and delete"
                onDelete={(force) => apiClient.agentInstances.remove(row.id, { force })}
                onDeleted={conversations.refresh}
              />
            </Space>
          );
        },
      },
    ],
    // `chatPath` is rebuilt every render and closes only over the extension link, which
    // is stable for the life of the app, so it is deliberately not a dependency:
    // antd rebuilds a table's internal column state whenever this array changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [conversations.refresh, openableIds, derivedTitles, theme, extensionColumns, view],
  );

  const othersCount = rows.filter(
    (row) => openableIds !== undefined && !openableIds.has(row.id),
  ).length;

  return (
    <PageFrame>
      {/* The rail is the navigation within an agent, so this page keeps it like every other
          agent surface. Nothing is current here, so it highlights nothing. */}
      <div
        data-testid="agent-surface"
        css={{
          display: "flex",
          gap: theme.space(6),
          // `flex-start` so the sticky rail keeps its own height and stays put on scroll.
          alignItems: "flex-start",
        }}
      >
        {namespace ? (
          <AgentRail
            instanceRef={{}}
            agentTitle={{
              primary: name ?? namespace,
              secondary: namespace,
            }}
            agentHref={agentPageUrl({ namespace, name })}
            // Known from the URL here, so the switcher can leave this agent out of its
            // own list without waiting for a conversation that does not exist.
            agentRef={{ namespace: namespace ?? "", name }}
            instances={{ ...conversations, data: rows }}
            onNewChat={startConversation}
          />
        ) : null}

      <Space
        orientation="vertical"
        size="large"
        css={{ display: "flex", flex: 1, minWidth: 0 }}
      >
        <header
          data-testid="agent-header"
          css={{
            display: "grid",
            gap: theme.space(3),
            padding: `${theme.space(4)} ${theme.space(5)}`,
            background: theme.color.surfaceCard,
            border: `1px solid ${theme.color.border}`,
            borderRadius: theme.radius.lg,
          }}
        >
          <div
            css={{
              display: "flex",
              flexWrap: "wrap",
              alignItems: "center",
              justifyContent: "space-between",
              columnGap: theme.space(4),
              rowGap: theme.space(3),
            }}
          >
            <Title level={4} css={{ margin: 0, minWidth: 0, wordBreak: "break-word" }} data-testid="agent-name">
              {name}
            </Title>
            <div
              data-testid="agent-actions"
              css={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: theme.space(2) }}
            >
              {agent ? (
                <>
                  <DeleteResourceButton kind="agent" name={agent.name} label="Delete agent" outlined
                    onDelete={removeAgent}
                    // Navigate first: re-reading here would flash "does not exist" for the deleted agent.
                    onDeleted={() => {
                      navigate(paths.agents);
                      void invalidateAgents().catch(() => {});
                    }}
                    description="Stops new conversations. Existing conversations keep their prepared revisions. Shared templates and Harnesses are preserved."
                  />
                  {/* A wide gap and a rule keep the destructive action away from the other two. */}
                  <span
                    aria-hidden
                    css={{
                      width: 1,
                      height: 24,
                      background: theme.color.borderStrong,
                      marginInline: theme.space(3),
                      // When the buttons wrap, a lone rule reads as a stray mark; the gap stays.
                      "@media (max-width: 900px)": { background: "transparent" },
                    }}
                  />
                  <Link to={buildPath(paths.agentEdit, { namespace: agent.namespace, name: agent.name })}>
                    <Button icon={<Pencil size={14} />} data-testid="agent-edit">Edit agent</Button>
                  </Link>
                </>
              ) : null}
              <Button
                type="primary"
                icon={<MessageSquarePlus size={14} />}
                onClick={startConversation}
                data-testid="agent-new-chat"
              >
                New chat
              </Button>
            </div>
          </div>
            <div
              css={{
                display: "flex",
                flexWrap: "wrap",
                alignItems: "center",
                columnGap: theme.space(3),
                rowGap: theme.space(1),
                color: theme.color.textMuted,
                fontSize: 12,
              }}
            >
              <MetaField label="Status">
                {agent ? (
                  <AgentStatusTag agent={agent} testId="agent-revision-state" />
                ) : (
                  <Skeleton.Button active size="small" css={{ width: 56, height: 22 }} />
                )}
              </MetaField>
              <MetaField label="Namespace">{namespace}</MetaField>
              {agent ? (
                <MetaField label="Revision">
                  {agent.resource.status?.latestSuccessfulRevision ? (
                    // Shortened, with the full value in a tooltip on the text only and one copy away.
                    <Text
                      copyable={{ text: agent.resource.status.latestSuccessfulRevision }}
                      css={{ fontFamily: theme.font.mono, fontSize: 12 }}
                      data-testid="agent-revision"
                    >
                      <Tooltip title={agent.resource.status.latestSuccessfulRevision}>
                        <span>
                          {agent.resource.status.latestSuccessfulRevision.length > 12
                            ? `${agent.resource.status.latestSuccessfulRevision.slice(0, 12)}…`
                            : agent.resource.status.latestSuccessfulRevision}
                        </span>
                      </Tooltip>
                    </Text>
                  ) : (
                    <span>None yet</span>
                  )}
                </MetaField>
              ) : null}
            </div>
        </header>

        {agentMissing ? (
          <Alert
            type="warning"
            showIcon
            title="This Agent does not exist"
            description={`No Agent ${name ?? ""} was found in ${namespace ?? "that namespace"}. Conversations cut from it keep running — an instance runs from the revision it was built against — but no new ones can be started and this page has nothing to describe.`}
            data-testid="agent-template-missing"
            action={
              <Link to={paths.agents}>
                <Button size="small">All agents</Button>
              </Link>
            }
          />
        ) : definition.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this Agent"
            description={definition.error.message}
            data-testid="agent-template-error"
            action={
              <Button size="small" onClick={() => void definition.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {blockedReason ? (
          <Alert
            type="warning"
            showIcon
            data-testid="agent-cannot-start"
            data-blocked-reason={blockedReason}
            title="No new conversation can be started with this agent"
            // The controller's own words: an Agent with no successful revision answers
            // FailedPrecondition, and naming the reason is what tells a reader whether
            // to wait or to go and look at the template.
            description={blockedReason}
          />
        ) : null}

        {updateFailedReason ? (
          <Alert
            type="warning"
            showIcon
            data-testid="agent-update-failed"
            data-update-failed-reason={updateFailedReason}
            title="The latest change to this agent failed. Conversations still use the last good revision."
            // Nothing is blocked here, because conversations keep the last good revision, so
            // the status tag's tooltip was the only place this showed up. The message is
            // the controller's Ready condition, which is already redacted for status.
            description={updateFailedReason}
          />
        ) : null}

        {conversations.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this agent's conversations"
            description={conversations.error.message}
            data-testid="conversations-error"
            action={
              <Button size="small" onClick={() => void conversations.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {/* The wide read is authorised separately from the list and the controller
            refuses the request outright when it is not allowed, rather than quietly
            narrowing it. Answering with the reader's own conversations and saying so
            is better than answering with nothing — but it must be said, or a partial
            list reads as the whole one. */}
        {conversations.data?.widerReadRefused ? (
          <Alert
            type="info"
            showIcon
            title="Showing only the conversations you started"
            description={`Reading everyone's conversations is authorised separately from the list, and that request was refused: ${conversations.data.widerReadRefused}`}
            data-testid="conversations-own-only"
          />
        ) : null}


        <Tabs
          data-testid="agent-activity-tabs"
          activeKey={activeTab}
          onChange={setTab}
          items={[
            {
              key: "overview",
              label: "Overview",
              children: agent ? (
                <AgentStructure agent={agent} />
              ) : definition.isLoading ? (
                <AgentStructureSkeleton />
              ) : null,
            },
            {
              key: "conversations",
              label: conversations.data ? `Conversations (${rows.length})` : "Conversations",
              children: (
                <Space orientation="vertical" size="middle" css={{ display: "flex" }}>

                <FilterBar
                  testId="conversations-filters"
                  view={view}
                  search={{
                    label: "Search conversations by name, id or who started them",
                    placeholder: "Search conversations",
                  }}
                  filters={[
                    {
                      id: "state",
                      label: "State",
                      allLabel: "Any state",
                      options: stateOptions,
                      minWidth: 180,
                    },
                  ]}
                  trailing={
                    !conversations.error && !conversations.isLoading ? (
                      <Text
                        data-testid="conversations-summary"
                        css={{ color: theme.color.textMuted }}
                      >
                        {filtered.length} of {rows.length}{" "}
                        {rows.length === 1 ? "conversation" : "conversations"}
                      </Text>
                    ) : null
                  }
                />

                {othersCount > 0 ? (
                  <Alert
                    type="info"
                    showIcon
                    data-testid="conversations-others-note"
                    title={`${othersCount} of these ${othersCount === 1 ? "conversation was" : "conversations were"} started by somebody else`}
                    description="They are listed because this is a shared agent and hiding them would understate what it is doing. They cannot be opened, renamed or deleted from here: the controller reads an instance as its creator, so it answers “not found” to anybody else. A share link from the person who started one is the only other way in."
                  />
                ) : null}

                {/* Offered above the table only when something is ticked: a bar that is always
                    there costs a row of the page for an action most visits never take. */}
                {selectedIds.length > 0 ? (
                  <div
                    data-testid="conversations-bulk-bar"
                    css={{
                      display: "flex",
                      alignItems: "center",
                      gap: theme.space(3),
                      // The count and the button drop onto separate lines rather than the
                      // button being pushed off the edge of a narrow page.
                      flexWrap: "wrap",
                    }}
                  >
                    <Text css={{ color: theme.color.textMuted }}>
                      {selectedIds.length}{" "}
                      {selectedIds.length === 1 ? "conversation" : "conversations"} selected
                    </Text>
                    <DeleteResourceButton
                      kind="conversations"
                      name={`${selectedIds.length} selected`}
                      label="Delete selected"
                      outlined
                      disabled={isBulkDeleting}
                      forceLabel="Stop active turns and delete"
                      onDelete={deleteSelected}
                      onDeleted={() => undefined}
                      description="Everything said in them goes too, and none of it can be recovered. The workers they hold are released."
                    />
                  </div>
                ) : null}

                <Table<AgentInstance>
                  data-testid="conversations-table"
                  scroll={{ x: "max-content" }}
                  /* A bigger target than antd's default 16px box.

                     The row is selected by hitting a square barely larger than the tick drawn
                     inside it, which is a miss more often than it should be — and the cell
                     around it is already the width of a column, so the space costs nothing.
                     The padding is on the wrapper rather than the box, so the clickable area
                     grows without the tick itself changing size. */
                  css={{
                    "& .ant-table-selection-column .ant-checkbox-wrapper": {
                      padding: theme.space(2),
                      margin: `-${theme.space(2)}`,
                    },
                    "& .ant-table-selection-column .ant-checkbox .ant-checkbox-inner": {
                      width: 18,
                      height: 18,
                    },
                  }}
                  rowSelection={{
                    selectedRowKeys: selectedIds as string[],
                    onChange: (keys) => setSelectedIds(keys.map(String)),
                    // Somebody else's conversation cannot be deleted from here, so it cannot be
                    // ticked either — a checkbox that leads to a refusal is worse than none.
                    getCheckboxProps: (row) => ({
                      disabled: !(openableIds?.has(row.id) ?? true),
                    }),
                  }}
                  rowKey={(row) => row.id}
                  columns={columns}
                  dataSource={conversations.error ? [] : filtered}
                  loading={conversations.isLoading}
                  onChange={listTableChange<AgentInstance>(view)}
                  pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
                  locale={{
                    emptyText: conversations.error
                      ? " "
                      : view.isNarrowed
                        ? "No conversations match those filters."
                        : conversations.data
                          ? "No conversations with this agent yet. Start one with “New chat”."
                          : " ",
                  }}
                  onRow={(row) =>
                    clickableRow(() => void navigate(chatPath(row)), {
                      enabled: openableIds === undefined || openableIds.has(row.id),
                    })
                  }
                />

                </Space>
              ),
            },
            {
              key: "schedules",
              label: schedules.rows ? `Schedules (${schedules.rows.length})` : "Schedules",
              children:
                namespace && name ? <AgentSchedules agent={{ namespace, name }} /> : null,
            },
          ]}
        />
      </Space>
      </div>
    </PageFrame>
  );
}
