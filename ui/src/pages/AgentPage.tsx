import { useMemo, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { Alert, Button, Card, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Pencil } from "lucide-react";
import { useTheme } from "@emotion/react";
import { AgentRail } from "@/components/agent/AgentRail";
import { AgentContextPanel } from "@/components/chat/AgentContextPanel";
import { PageFrame } from "@/components/Structure/PageFrame";
import { buildPath, paths } from "@/router/routes";
import {
  VendorSlot,
  useVendorExtensionConfig,
  useVendorTableColumns,
  withVendorColumns,
} from "@/vendorExtensions";
import {
  agentPairsOf,
  apiClient,
  isNotFound,
  newConversationBlockedReason,
  useAgentConversations,
  useAgentTemplate,
  type AgentInstance,
  type AgentPair,
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

/**
 * One agent, and the conversations people have had with it.
 *
 * An agent is a `(AgentTemplate, Harness)` pair and an `AgentInstance` is one
 * conversation with it — so this is the page between the two: the agents list leads
 * here, and each row here leads to a chat. See `api/domain/agentPairs`.
 *
 * ## The narrowing here really is server-side
 *
 * `ListAgentInstances` gained `agent_template` and `harness`, which the controller
 * resolves through the instance's `prepared_revision` to the pair it was built from.
 * That matters twice over: the list is paged, so filtering it in the browser would
 * search one page and report "no conversations" about a row further down; and
 * resolving through the revision rather than through labels selects conversations
 * stored before the fields existed, with no migration and no backfill.
 *
 * Search and sort are still the browser's, over whatever pages have been read, and
 * the note under the table says so rather than implying otherwise.
 *
 * ## Somebody else's conversation is listed and cannot be opened
 *
 * This is the part worth reading before changing anything here. The list is asked
 * for with `all_creators` always — the old "include agents created by others" switch
 * is gone, because a list that hides most of a shared cluster by default is a list
 * that misleads. But an instance is scoped to its creator on *read*:
 * `GetAgentInstance` resolves through `WHERE namespace = $1 AND id = $2 AND user_id
 * = $3`, and the A2A gateway reads the instance through that same call. So a
 * conversation somebody else started is listable and genuinely not openable — its
 * record page and its chat both answer `NotFound`, and a share link is the only way
 * in.
 *
 * Listing such a row with a link into a chat that will fail would be worse than not
 * listing it at all, so those rows carry no link and say whose they are. The page
 * says it once, above the table, so the absence of a link reads as a rule rather
 * than as a bug.
 */
export function AgentPage() {
  const theme = useTheme();
  const navigate = useNavigate();
  const { namespace, agentTemplate, harness } = useParams<{
    namespace: string;
    agentTemplate: string;
    harness: string;
  }>();
  const view = useListView(FILTER_IDS);

  const template = useAgentTemplate(namespace, agentTemplate);
  const conversations = useAgentConversations(namespace, agentTemplate, harness);

  /*
   * The agent itself: this template's pair with *this* harness.
   *
   * Found among the pairs the template reports rather than assumed to exist. A
   * template admitted by two harnesses is two agents, and an address naming a
   * harness that no longer admits it is an address for an agent that has been
   * retired — which is a different answer from "the template is missing" and is
   * worth saying separately.
   */
  const agent: AgentPair | undefined = useMemo(() => {
    if (!template.data || !harness) return undefined;
    return agentPairsOf(template.data).find((pair) => pair.harness === harness);
  }, [template.data, harness]);

  const templateMissing = template.error !== undefined && isNotFound(template.error);
  const notAdmitted = template.data !== undefined && agent === undefined;

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
   * `agentLinks.fromAgentsList` — which is where instance rows are listed now — and
   * a contribution that throws or answers with nothing falls back to this
   * application's own route rather than to a dead link.
   */
  const fromAgentsList = useVendorExtensionConfig().agentLinks?.fromAgentsList;
  const chatPath = (row: AgentInstance) => {
    const own = agentUrl.chat({ namespace: row.namespace, id: row.id });
    if (!fromAgentsList) return own;
    try {
      const destination = fromAgentsList(row);
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
    if (!namespace || !agentTemplate || !harness) return;
    navigate(
      buildPath(paths.agentNewChat, { namespace, agentTemplate, harness }),
    );
  }

  /*
   * Delete every conversation, then the template.
   *
   * In that order and not the other: deleting the template retires the pair, and a
   * conversation whose pair is retired still runs — it holds a prepared revision the
   * collector keeps for it — so it would be left behind with nothing describing it.
   *
   * The conversations go in parallel because they are independent, and the template only
   * after all of them have: a partial delete that removed the template first would leave
   * a state no page can explain.
   */
  async function removeAgent(): Promise<void> {
    /*
     * Only the conversations this reader can actually delete.
     *
     * An instance is scoped to its creator on every write as well as every read, so
     * deleting somebody else's is refused — and a delete that tried them all failed
     * partway, leaving the agent half-removed with no way to finish. `openableIds` is
     * the controller's own answer about which are this reader's, which is the same
     * answer the rows use to decide whether to offer a delete at all.
     *
     * The ones left behind keep running, exactly as they would if the template were
     * deleted on its own — see the prompt, which says so rather than implying the agent
     * is gone entirely.
     */
    const mine = rows.filter((row) => openableIds?.has(row.id) ?? true);
    await Promise.all(
      mine.map((row) => apiClient.agentInstances.remove(row.namespace, row.id)),
    );

    /*
     * The template goes only when nothing is left behind.
     *
     * Deleting it retires the pair, and a conversation whose pair is retired keeps
     * running with nothing describing it — reachable only from the unmapped list. Doing
     * that to somebody else's conversation, to tidy up an agent they did not ask to
     * delete, is not ours to do.
     *
     * So when every conversation was this reader's, the mapping goes with them and the
     * agent is gone. When any belonged to someone else, the conversations this reader
     * owned are deleted and the agent stays — which the prompt says before they commit.
     */
    const strandsOthers = mine.length < rows.length;
    if (!strandsOthers && agentTemplate && namespace) {
      await apiClient.agentBuildingBlocks.removeAgentTemplate(namespace, agentTemplate);
    }
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
        targets.map((row) => apiClient.agentInstances.remove(row.namespace, row.id)),
      );
      await conversations.refresh();
      setSelectedIds([]);
    } finally {
      setBulkDeleting(false);
    }
  }

  const vendorColumns = useVendorTableColumns<AgentInstance>(
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
              <VendorSlot
                id="app_agents_agentsList_agentListItem_badge"
                context={{ agentName: row.id, namespace: row.namespace }}
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
            <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
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
      ...withVendorColumns([], vendorColumns),
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
              <RenameConversationButton
                instance={row}
                disabled={!mine}
                onRenamed={conversations.refresh}
              />
              <DeleteResourceButton
                kind="conversation"
                name={conversationTitle(row)}
                disabled={!mine}
                onDelete={() => apiClient.agentInstances.remove(row.namespace, row.id)}
                onDeleted={conversations.refresh}
              />
            </Space>
          );
        },
      },
    ],
    // `chatPath` is rebuilt every render and closes only over the vendor link, which
    // is stable for the life of the app, so it is deliberately not a dependency:
    // antd rebuilds a table's internal column state whenever this array changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [conversations.refresh, openableIds, theme, vendorColumns, view],
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
            agentRef={{ namespace }}
            agentTitle={{
              primary: agentTemplate ?? namespace,
              secondary: harness ? `on ${harness}` : namespace,
            }}
            agentHref={agentPageUrl({ namespace, agentTemplate, harness })}
            // Known from the URL here, so the switcher can leave this agent out of its
            // own list without waiting for a conversation that does not exist.
            agentPair={{ namespace: namespace ?? "", agentTemplate, harness }}
            instances={{ ...conversations, data: rows }}
            onNewChat={startConversation}
          />
        ) : null}

      <Space
        orientation="vertical"
        size="middle"
        css={{ display: "flex", flex: 1, minWidth: 0 }}
      >
        {templateMissing ? (
          <Alert
            type="warning"
            showIcon
            title="This agent's template does not exist"
            description={`No agent template ${agentTemplate ?? ""} was found in ${namespace ?? "that namespace"}. Conversations cut from it keep running — an instance runs from the revision it was built against — but no new ones can be started and this page has nothing to describe.`}
            data-testid="agent-template-missing"
            action={
              <Link to={paths.agents}>
                <Button size="small">All agents</Button>
              </Link>
            }
          />
        ) : template.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this agent's template"
            description={template.error.message}
            data-testid="agent-template-error"
            action={
              <Button size="small" onClick={() => void template.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {notAdmitted ? (
          <Alert
            type="warning"
            showIcon
            title={`The ${harness} harness no longer admits this template`}
            description="A harness admits templates by label selector, and the controller retires the pair when the labels stop matching. The conversations below still exist, and no new one can be started until the labels agree again."
            data-testid="agent-not-admitted"
          />
        ) : null}


        {blockedReason ? (
          <Alert
            type="warning"
            showIcon
            data-testid="agent-cannot-start"
            data-blocked-reason={blockedReason}
            title="No new conversation can be started with this agent"
            // The controller's own words: a pair with no successful revision answers
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
          onRow={(row) => ({
            className:
              openableIds === undefined || openableIds.has(row.id)
                ? "clickable-table-row"
                : undefined,
            onClick: (event) => {
              if (openableIds !== undefined && !openableIds.has(row.id)) return;
              if (
                (event.target as HTMLElement).closest(
                  "a, button, input, [role='button'], .ant-popover, .ant-dropdown",
                )
              ) {
                return;
              }
              void navigate(chatPath(row));
            },
          })}
        />
      </Space>

      {/*
        What this agent is, beside the conversations it has had.
        
        The same panel the chat carries, given the pair rather than a conversation: the
        model, the instructions and the tools all live on the template, so this page can
        show them without an instance to read them through. Only the prepared revision
        needs a conversation, and it is left out here rather than guessed at.
      */}
      <div
        css={{
          flexShrink: 0,
          position: "sticky",
          top: theme.layout.headerHeight + 24,
          alignSelf: "start",
          width: 248,
        }}
        data-testid="agent-context-aside"
      >
        <AgentContextPanel pair={{ namespace: namespace ?? "", agentTemplate, harness }} />

        {/*
          Deleting the agent, which is more than one object.
          
          An agent is a (template, harness) pair, and the pair is *derived* — the
          controller materialises it from admission and retires it when the labels stop
          matching. So there is nothing to delete called "the agent": deleting the
          template retires the pair, which is what stops new conversations, and the
          conversations already open are separate rows that outlive it.
          
          Both halves happen here, conversations first. Deleting the template alone
          would leave every conversation running against a retired pair with no way back
          to the thing that describes them.
        */}
        {agent ? (
          <div css={{ marginTop: theme.space(5) }}>
            <DeleteResourceButton
              kind="agent"
              name={`${agentTemplate} on ${harness}`}
              label="Delete agent"
              outlined
              onDelete={removeAgent}
              onDeleted={() => navigate(paths.agents)}
              description={
                <span css={{ display: "inline-block", maxWidth: 320 }} data-testid="agent-delete-consequence">
                  {(() => {
                    const mine = rows.filter((row) => openableIds?.has(row.id) ?? true).length;
                    const others = rows.length - mine;
                    const yours =
                      mine === 0
                        ? "You have no conversations with this agent. "
                        : `${mine} of your ${mine === 1 ? "conversation" : "conversations"} will be deleted, and cannot be recovered. `;
                    // Said plainly rather than left to be discovered: an instance is
                    // scoped to its creator on write as well as read, so somebody
                    // else's cannot be deleted from here and will keep running.
                    // Two different outcomes, and which one applies is decided by
                    // whether anything would be left stranded.
                    const theirs =
                      others > 0
                        ? `${others} started by other people cannot be deleted from here, so they keep running and the agent stays — it is only gone once nothing is left under it. `
                        : "The agent template is deleted too, which is what stops this agent existing. ";
                    return `${yours}${theirs}`;
                  })()}
                  Deleting the conversations releases the workers they hold.
                </span>
              }
            />
          </div>
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

/**
 * What this agent is made of, and where to change it.
 *
 * Item 3's link, and the whole of it: from an agent to its template, because a
 * template is a real object a reader may want to edit. There is no filter in the
 * other direction any more — "the agents using this template" was circular once an
 * agent lists its own conversations, since that was only ever a way of saying "the
 * other conversations with this same agent".
 */
function AgentIdentityCard({ agent }: { agent: AgentPair }) {
  const theme = useTheme();

  return (
    <Card data-testid="agent-identity" size="small">
      {/*
        Three blocks that reflow, not three columns of a table.
        
        This was an antd `Descriptions`, which lays its items out as a table — so the
        three sections could not wrap independently and, at a narrow window, three
        monospace values were squeezed into thirds of the width until they overran.
        Making the item content break `anywhere` stopped the overrun and replaced it
        with a worse problem: names broken mid-word, a few letters per line.
        
        As an auto-fitting grid each field is its own section with a floor on how narrow
        it may get, so they drop to two and then to one as the window narrows rather
        than being compressed past readability. The values then need no character-level
        breaking at all — see `IdentityField`.
      */}
      <div
        css={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))",
          gap: theme.space(4),
        }}
      >
        <IdentityField label="Agent template">
          <Link
            to={buildPath(paths.agentTemplateDetail, {
              namespace: agent.namespace,
              name: agent.agentTemplate,
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
              ellipsis={{ tooltip: agent.agentTemplate }}
              css={{ color: "inherit", fontFamily: "inherit", fontSize: 12 }}
            >
              {agent.agentTemplate}
            </Text>
            <Pencil size={12} aria-hidden color={theme.color.textMuted} />
          </Link>
        </IdentityField>

        <IdentityField label="Runs on">
          <Text
            ellipsis={{ tooltip: agent.harness }}
            css={{ fontFamily: theme.font.mono, fontSize: 12 }}
          >
            {agent.harness}
          </Text>
        </IdentityField>

        <IdentityField label="Revision">
          {agent.latestSuccessfulRevision ? (
            /* Truncated with the whole value a click away, rather than wrapped.
               A revision is a 64-character hash with nowhere to break: wrapping it
               costs four lines to show something nobody reads in full, and nobody
               reads it by eye anyway — they copy it. */
            <Text
              ellipsis={{ tooltip: agent.latestSuccessfulRevision }}
              copyable={{ text: agent.latestSuccessfulRevision }}
              css={{ fontFamily: theme.font.mono, fontSize: 12 }}
              data-testid="agent-revision"
            >
              {agent.latestSuccessfulRevision}
            </Text>
          ) : (
            <Tag data-testid="agent-revision-state" color="default">
              {agent.revisionState === "preparing" ? "Preparing" : "Not reported"}
            </Tag>
          )}
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
        Editing the template changes every agent cut from it, not only this one — a
        template admitted by two harnesses is two agents sharing one configuration.
      </Paragraph>
    </Card>
  );
}
