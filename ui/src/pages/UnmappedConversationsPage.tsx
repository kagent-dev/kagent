import { useMemo } from "react";
import { Alert, Button, Space, Table, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Link } from "react-router-dom";
import { useTheme } from "@emotion/react";
import { PageFrame } from "@/components/Structure/PageFrame";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { StateTag } from "@/components/agent-instances/InstanceTags";
import {
  conversationTitle,
  relativeAge,
} from "@/components/agent-instances/instanceLabels";
import { agentUrl } from "@/components/agent/agentUrl";
import { paths } from "@/router/routes";
import {
  apiClient,
  useAgentInstances,
  useAgentsAcrossNamespaces,
  useNamespaces,
  type AgentInstance,
} from "@/api";

const { Text } = Typography;


export function UnmappedConversationsPage() {
  const theme = useTheme();
  const namespaces = useNamespaces();

  const namespaceNames = useMemo(
    () => (namespaces.data ?? []).map((entry) => entry.name),
    [namespaces.data],
  );

  const agents = useAgentsAcrossNamespaces(namespaceNames);
  const conversations = useAgentInstances(true);

  /*
   * Computed here rather than handed over, so the page is a real address.
   *
   * A reader who bookmarks this or reloads it gets the same answer as one who arrived
   * from the list, and the two cannot disagree about which conversations are orphaned.
   */
  const orphans = useMemo(() => {
    if (!agents.data || !conversations.data) return [];
    const known = new Set(agents.data.agents.map((agent) => agent.ref));
    const unreadable = new Set(agents.data.refused.map((entry) => entry.namespace));
    return (conversations.data ?? []).filter((instance) => {
      if (unreadable.has(instance.agent?.split("/")[0] ?? "")) return false;
      return !instance.agent || !known.has(instance.agent);
    });
  }, [agents.data, conversations.data]);

  const loadFailure = namespaces.error ?? agents.error ?? conversations.error;

  /*
   * Every one of them, in parallel, because they are independent.
   *
   * No creator filter here, unlike the agent page: a delete of somebody else's is
   * refused by the controller rather than silently succeeding, and a refusal fails the
   * whole action and re-reads the list — so what could not be deleted stays visible
   * rather than appearing to have gone.
   */
  async function removeAll(): Promise<void> {
    await Promise.all(
      orphans.map((row) => apiClient.agentInstances.remove(row.id)),
    );
  }

  const columns: ColumnsType<AgentInstance> = [
    {
      title: "Conversation",
      key: "name",
      render: (_, row) => (
        <Link
          to={agentUrl.chat({ id: row.id })}
          data-testid={`unmapped-link-${row.id}`}
        >
          {conversationTitle(row)}
        </Link>
      ),
    },
    {
      // Keep the deleted Agent identity visible so conversations remain identifiable.
      title: "Was built from",
      key: "agent",
      render: (_, row) => (
        <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
          {row.agent ?? "not reported"}
        </Text>
      ),
    },
    { title: "State", key: "state", render: (_, row) => <StateTag state={row.state} /> },
    {
      title: "Last active",
      key: "updated",
      render: (_, row) => (
        <Text css={{ color: theme.color.textMuted }}>
          {row.updatedAt ? relativeAge(row.updatedAt) : "not reported"}
        </Text>
      ),
    },
    {
      title: "",
      key: "actions",
      width: 60,
      render: (_, row) => (
        <DeleteResourceButton
          kind="conversation"
          name={conversationTitle(row)}
          onDelete={() => apiClient.agentInstances.remove(row.id)}
          onDeleted={conversations.refresh}
          description="This conversation cannot be reached from any agent, and deleting it releases the worker it holds."
        />
      ),
    },
  ];

  return (
    <PageFrame
      title="Unmapped conversations"
      description="Conversations whose Agent definition no longer exists. They retain their prepared revisions."
      actions={
        <Space size={8}>
          {/*
            The whole point of this page, offered once rather than row by row.

            These conversations are reachable from nowhere else and each holds a worker
            nothing will reclaim, so clearing them is the ordinary thing to do here —
            and doing it one row at a time for twenty of them is a chore that leaves
            most of the workers held.
          */}
          {orphans.length > 0 ? (
            <DeleteResourceButton
              kind="conversations"
              name={`all ${orphans.length} unmapped`}
              label="Delete all"
              outlined
              onDelete={removeAll}
              onDeleted={conversations.refresh}
              description={
                <span
                  css={{ display: "inline-block", maxWidth: 340 }}
                  data-testid="unmapped-delete-all-consequence"
                >
                  This will delete all conversations that do not have an agent Agent definition — {orphans.length} in all. They cannot be
                  recovered, and the workers they hold are released.
                </span>
              }
            />
          ) : null}
          <Link to={paths.agents}>
            <Button data-testid="unmapped-back">All agents</Button>
          </Link>
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {loadFailure ? (
          <Alert
            type="error"
            showIcon
            data-testid="unmapped-error"
            title="Could not work out which conversations are unmapped"
            // Both reads are needed to answer the question at all: without the
            // Agents there are no identities to compare against, so every conversation
            // would look orphaned.
            description={loadFailure.message}
          />
        ) : null}

        <Alert
          type="info"
          showIcon
          data-testid="unmapped-explainer"
          title="These are not broken, and deleting them is the point"
          description="An instance runs from the revision it was built against, which the collector keeps for it — so it outlives the template it names. Each one holds a worker that nothing will reclaim, and deleting it gives that worker back."
        />

        <Table<AgentInstance>
          data-testid="unmapped-table"
          rowKey={(row) => row.id}
          columns={columns}
          dataSource={loadFailure ? [] : orphans}
          loading={agents.isLoading || conversations.isLoading}
          pagination={false}
          locale={{
            emptyText: loadFailure
              ? " "
              : "Every conversation belongs to an agent. Nothing to show here.",
          }}
        />
      </Space>
    </PageFrame>
  );
}
