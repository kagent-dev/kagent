import { useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Card, Space, Table, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Pencil } from "lucide-react";
import { useTheme } from "@emotion/react";
import { AgentRail } from "@/components/agent/AgentRail";
import { AgentStatusTag } from "@/components/agent/AgentStatusTag";
import { AgentSchedules } from "@/components/agent/AgentSchedules";
import { AgentContextPanel } from "@/components/chat/AgentContextPanel";
import { PageFrame } from "@/components/Structure/PageFrame";
import { buildPath, paths } from "@/router/routes";
import {
  ExtensionSlot,
  useExtensionAgentLinks,
  useExtensionTableColumns,
  withExtensionColumns,
} from "@/appExtensions";
import {
  apiClient,
  isNotFound,
  newConversationBlockedReason,
  useAgentConversations,
  useAgent,
  useInvalidateAgents,
  type Agent,
  type AgentInstance,
} from "@/api";
import { agentPageUrl, agentUrl } from "@/components/agent/agentUrl";
import { StateTag, ValueOrNotReported } from "@/components/agent-instances/InstanceTags";
import {
  conversationTitle,
  hasConversationName,
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

const { Paragraph, Text } = Typography;

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
  const agentMissing = definition.error !== undefined && isNotFound(definition.error);

  /*
   * Memoised rather than defaulted inline: `?? []` builds a new array on every
   * render while the read is in flight, and everything below depends on it — so the
   * filter, the state options and the columns would all rebuild continuously, and
   * antd rebuilds a table's internal column state whenever its columns change.
   */
  const rows = useMemo(() => conversations.data?.all ?? [], [conversations.data]);
  const openableIds = conversations.data?.openableIds;

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
          conversationTitle(row),
          row.id,
          row.creator,
        ]);
      }),
    [rows, selectedStates, view.query],
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

  async function deleteSelected(): Promise<void> {
    setBulkDeleting(true);
    try {
      const targets = rows.filter((row) => selectedIds.includes(row.id));
      await Promise.all(
        targets.map((row) => apiClient.agentInstances.remove(row.id)),
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
        sorter: byText<AgentInstance>((row) => conversationTitle(row)),
        sortOrder: sortOrderFor(view, "name"),
        render: (_, row) => {
          const title = conversationTitle(row);
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
                  css={{
                    color: theme.color.primaryText,
                    fontStyle: hasConversationName(row) ? undefined : "italic",
                  }}
                >
                  {title}
                </Link>
              ) : (
                /* No link, deliberately. `GetAgentInstance` is scoped to its
                   creator and the A2A gateway reads through the same call, so this
                   conversation answers NotFound to everyone but the person who
                   started it. A link here would be an invitation to a 404. */
                <Tooltip
                  title={`Only ${row.creator || "the person who started it"} can open this conversation. An instance is read as its creator, so it answers "not found" to anyone else — a share link is the only other way in.`}
                >
                  <Text
                    data-testid={`conversation-unopenable-${row.id}`}
                    css={{ color: theme.color.textMuted, fontStyle: "italic" }}
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
        title: "Started by",
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
                name={conversationTitle(row)}
                disabled={!mine}
                onDelete={() => apiClient.agentInstances.remove(row.id)}
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
    [conversations.refresh, openableIds, theme, extensionColumns, view],
  );

  const othersCount = rows.filter(
    (row) => openableIds !== undefined && !openableIds.has(row.id),
  ).length;

  return (
    // No heading and no actions row.
    //
    // The rail beside this page names the agent and carries both the way back to the
    // agents list and New chat, so a title repeating the name, a subtitle repeating the
    // harness, and three buttons duplicating rail entries cost a band across the top and
    // said nothing new.
    //
    // The one thing that row carried alone was *why* a new conversation cannot be
    // started, which was a tooltip on its disabled button. That is an alert on the page
    // now: a reason attached to a control the reader may never hover is a reason they
    // never read.
    <PageFrame>
      {/*
        The same rail as the conversation surfaces.

        This page was the one agent surface without it, so arriving here from a chat
        took the navigation away — and arriving from the agents list gave a reader no
        way onward except back. The rail is the navigation *within* an agent, so a
        surface that drops it is a dead end.

        Mounted with no conversation selected, which is what this page is: nothing is
        current, so the rail highlights nothing, and the two entries that are about one
        conversation — its record, and the switcher — are withheld rather than pointed
        at an id that does not exist.

        Its conversation list and the table below are the same rows twice, and that is
        deliberate rather than overlooked: the rail is chrome that persists across every
        agent surface, and the table is this page's content, carrying state, counts,
        renaming and deletion that the rail does not.
      */}
      <div
        data-testid="agent-surface"
        css={{
          display: "flex",
          gap: theme.space(6),
          /*
           * `flex-start`, not `stretch`.
           *
           * Stretched, each sidebar is as tall as the whole conversation — and a sticky
           * element as tall as its scroll container has nowhere to stick, so on a long
           * chat both rails scrolled away with the page. At `flex-start` they keep
           * their own height and stay put, which is the entire reason they are sticky.
           */
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
        size="middle"
        css={{ display: "flex", flex: 1, minWidth: 0 }}
      >
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

        {agent ? <AgentIdentityCard agent={agent} /> : null}

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

        {/* Below the conversations, because a conversation is what a reader came here
            for and a schedule is how some of them got started. */}
        {namespace && name ? (
          <AgentSchedules agent={{ namespace, name }} />
        ) : null}
      </Space>

      {/*
        What this agent is, beside the conversations it has had.

        The same panel the chat carries, given the Agent reference rather than a conversation: the
        model, the instructions and the tools all live on the template, so this page can
        show them without an instance to read them through. Only the prepared revision
        needs a conversation, and it is left out here rather than guessed at.
      */}
      <div
        css={{
          flexShrink: 0,
          position: "sticky",
          top: `var(--agent-rail-sticky-top, ${theme.layout.headerHeight + 24}px)`,
          alignSelf: "start",
          width: 248,
        }}
        data-testid="agent-context-aside"
      >
        <AgentContextPanel agentRef={{ namespace: namespace ?? "", name }} />

        {agent ? (
          <Space size={8} wrap css={{ marginTop: theme.space(5) }}>
            <Link to={buildPath(paths.agentEdit, { namespace: agent.namespace, name: agent.name })}>
              <Button icon={<Pencil size={14} />} data-testid="agent-edit">Edit agent</Button>
            </Link>
            <DeleteResourceButton kind="agent" name={agent.name} label="Delete agent" outlined
              onDelete={removeAgent}
              // Navigate first: re-reading here would flash "does not exist" for the deleted agent.
              onDeleted={() => {
                navigate(paths.agents);
                void invalidateAgents().catch(() => {});
              }}
              description="Stops new conversations. Existing conversations keep their prepared revisions. Shared templates and Harnesses are preserved."
            />
          </Space>
        ) : null}
      </div>
      </div>
    </PageFrame>
  );
}

/**
 * One labelled value in the identity card.
 *
 * A block rather than a table row, so each can wrap on its own — which is the whole
 * point of the grid above.
 */
function IdentityField({ label, children }: { label: string; children: ReactNode }) {
  const theme = useTheme();

  return (
    // `minWidth: 0` because a grid item will not shrink below its content otherwise,
    // and a long value would then push its column wider than its share instead of
    // truncating inside it.
    <div css={{ minWidth: 0 }}>
      <div
        css={{
          color: theme.color.textMuted,
          fontSize: 12,
          marginBlockEnd: theme.space(1),
        }}
      >
        {label}
      </div>
      {children}
    </div>
  );
}

/** What this agent is made of: each half is a shared resource or inline in the Agent. */
function AgentIdentityCard({ agent }: { agent: Agent }) {
  const theme = useTheme();
  const { spec, status } = agent.resource;
  const mono = { fontFamily: theme.font.mono, fontSize: 12 };

  return (
    <Card data-testid="agent-identity" size="small">
      <div
        css={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))",
          gap: theme.space(4),
        }}
      >
        <IdentityField label="Agent template">
          {spec.templateRef ? (
            <Link
              to={buildPath(paths.agentTemplateDetail, {
                namespace: agent.namespace,
                name: spec.templateRef.name,
              })}
              data-testid="agent-template-link"
              css={{
                fontFamily: theme.font.mono,
                color: theme.color.primaryText,
                display: "inline-flex",
                alignItems: "center",
                gap: theme.space(2),
                minWidth: 0,
              }}
            >
              <Text
                ellipsis={{ tooltip: spec.templateRef.name }}
                css={{ color: "inherit", fontFamily: "inherit", fontSize: 12 }}
              >
                {spec.templateRef.name}
              </Text>
              <Pencil size={12} aria-hidden color={theme.color.textMuted} />
            </Link>
          ) : (
            <Text data-testid="agent-template-inline">Inline</Text>
          )}
        </IdentityField>

        <IdentityField label="Runs on">
          {spec.harnessRef ? (
            <Text ellipsis={{ tooltip: spec.harnessRef.name }} css={mono}>
              {spec.harnessRef.name}
            </Text>
          ) : (
            <Text data-testid="agent-harness-inline">Inline</Text>
          )}
        </IdentityField>

        <IdentityField label="Revision">
          {status?.latestSuccessfulRevision ? (
            // Truncated with a copy button: a revision is a long hash people copy, not read.
            <Text
              ellipsis={{ tooltip: status.latestSuccessfulRevision }}
              copyable={{ text: status.latestSuccessfulRevision }}
              css={mono}
              data-testid="agent-revision"
            >
              {status.latestSuccessfulRevision}
            </Text>
          ) : (
            <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>None yet</Text>
          )}
        </IdentityField>

        <IdentityField label="Status">
          <AgentStatusTag agent={agent} testId="agent-revision-state" />
        </IdentityField>
      </div>
      <Paragraph
        css={{
          margin: `${theme.space(3)} 0 0`,
          color: theme.color.textMuted,
          fontSize: 12,
        }}
        data-testid="agent-identity-note"
      >
        The template says what this agent does and the harness says how it runs.
        Referenced templates and Harnesses can be shared. Inline configuration belongs to this Agent.
      </Paragraph>
    </Card>
  );
}
