import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Alert, Button, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Pencil } from "lucide-react";
import { useTheme } from "@emotion/react";
import { RefreshButton } from "@/components/table/RefreshButton";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { buildPath, paths } from "@/router/routes";
import {
  agentDescription,
  agentRevisionState,
  apiClient,
  harnessRefName,
  templateRefName,
  useAgentInstances,
  useAgentTemplatesAcrossNamespaces,
  useAgentsAcrossNamespaces,
  useInvalidateAgents,
  useNamespaces,
  type Agent,
} from "@/api";
import { agentNewChatUrl } from "@/components/agent/agentUrl";
import { AgentStatusTag } from "@/components/agent/AgentStatusTag";
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

  // AgentService lists one namespace per call, so "all namespaces" is a fan-out.
  const readNamespaces = useMemo(
    () => (selectedNamespaces.length > 0 ? selectedNamespaces : namespaceNames),
    [selectedNamespaces, namespaceNames],
  );

  const definitions = useAgentsAcrossNamespaces(readNamespaces);
  const invalidateAgents = useInvalidateAgents();
  // Referenced templates carry the description a shared-template Agent shows.
  const templateNamespaces = useMemo(
    () => [...new Set((definitions.data?.agents ?? [])
      .filter((agent) => agent.resource.spec.templateRef)
      .map((agent) => agent.namespace))],
    [definitions.data],
  );
  const templates = useAgentTemplatesAcrossNamespaces(templateNamespaces);
  const descriptions = useMemo(
    () => new Map((definitions.data?.agents ?? []).map((agent) => [agent.ref, agentDescription(agent, templates.data?.templates)])),
    [definitions.data, templates.data],
  );
  const loadFailure = namespaces.error ?? definitions.error;
  // Everyone's conversations, so a count means what the agent is doing, not what this reader did.
  const conversations = useAgentInstances(true);

  const conversationCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const instance of conversations.data ?? []) {
      if (instance.agent) counts.set(instance.agent, (counts.get(instance.agent) ?? 0) + 1);
    }
    return counts;
  }, [conversations.data]);

  const agents = useMemo(() => definitions.data?.agents ?? [], [definitions.data]);

  const refreshThisTab = async () => {
    await Promise.all([definitions.refresh(), templates.refresh(), conversations.refresh()]);
  };

  // A namespace we could not read says nothing about its conversations, so those are not counted.
  const orphanedConversations = useMemo(() => {
    if (!definitions.data || !conversations.data) return 0;
    const known = new Set(agents.map((agent) => agent.ref));
    const unreadable = new Set(definitions.data.refused.map((entry) => entry.namespace));
    return conversations.data.filter((instance) => {
      const namespace = instance.agent?.split("/")[0];
      if (namespace && (!readNamespaces.includes(namespace) || unreadable.has(namespace))) return false;
      return !instance.agent || !known.has(instance.agent);
    }).length;
  }, [agents, conversations.data, definitions.data, readNamespaces]);

  const namespaceOptions = useMemo(
    () => (namespaces.data ?? []).map((entry) => ({ value: entry.name })),
    [namespaces.data],
  );

  const filtered = useMemo(() => {
    const matching = agents.filter((agent) =>
      matchesQuery(view.query, [
        agent.name,
        agent.namespace,
        templateRefName(agent),
        harnessRefName(agent),
        descriptions.get(agent.ref),
      ]),
    );
    if (!view.sort) matching.sort((left, right) => left.name.localeCompare(right.name));
    return matching;
  }, [agents, descriptions, view]);

  const columns = useMemo<ColumnsType<Agent>>(
    () => [
      {
        title: "Agent",
        key: "name",
        sorter: byText<Agent>((row) => row.name),
        sortOrder: sortOrderFor(view, "name"),
        render: (_, row) => (
          <Space orientation="vertical" size={0}>
            <Link
              to={agentNewChatUrl(row) ?? paths.agents}
              data-testid={`agent-link-${row.namespace}-${row.name}`}
              css={{ fontFamily: theme.font.mono, color: theme.color.primaryText }}
            >
              {row.name}
            </Link>
            {descriptions.get(row.ref) ? (
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
                {descriptions.get(row.ref)}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        title: "Namespace",
        key: "namespace",
        width: 140,
        sorter: byText<Agent>((row) => row.namespace),
        sortOrder: sortOrderFor(view, "namespace"),
        render: (_, row) => (
          <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>{row.namespace}</Text>
        ),
      },
      {
        title: "Template",
        key: "template",
        width: 190,
        sorter: byText<Agent>((row) => templateRefName(row) ?? ""),
        sortOrder: sortOrderFor(view, "template"),
        render: (_, row) => <SourceCell refName={templateRefName(row)} testId={`agent-template-${row.ref}`} />,
      },
      {
        title: "Harness",
        key: "harness",
        width: 170,
        sorter: byText<Agent>((row) => harnessRefName(row) ?? ""),
        sortOrder: sortOrderFor(view, "harness"),
        render: (_, row) => <SourceCell refName={harnessRefName(row)} testId={`agent-harness-${row.ref}`} />,
      },
      {
        title: "Status",
        key: "status",
        width: 130,
        sorter: byText<Agent>((row) => agentRevisionState(row)),
        sortOrder: sortOrderFor(view, "status"),
        render: (_, row) => <AgentStatusTag agent={row} testId={`agent-revision-${row.ref}`} />,
      },
      {
        title: "Conversations",
        key: "conversations",
        width: 140,
        sorter: byNumber<Agent>((row) => conversationCounts.get(row.ref) ?? 0),
        sortOrder: sortOrderFor(view, "conversations"),
        render: (_, row) => {
          // No count until the read lands: a confident 0 would claim nobody talked to it.
          if (conversations.error || !conversations.data) {
            return (
              <Text css={{ color: theme.color.textMuted, fontSize: 12 }} data-not-reported="true">
                Not counted
              </Text>
            );
          }
          const count = conversationCounts.get(row.ref) ?? 0;
          return (
            <Text data-testid={`agent-conversations-${row.ref}`}>
              {count} {count === 1 ? "conversation" : "conversations"}
            </Text>
          );
        },
      },
      {
        title: "",
        key: "actions",
        width: 96,
        render: (_, row) => (
          <Space size={4}>
            <Link to={buildPath(paths.agentEdit, { namespace: row.namespace, name: row.name })}>
              <Button
                type="text"
                icon={<Pencil size={16} />}
                data-testid={`edit-${row.name}`}
                aria-label={`Edit agent ${row.name}`}
              />
            </Link>
            <DeleteResourceButton
              kind="agent"
              name={row.name}
              description="New conversations stop. Existing ones keep running. Shared templates and harnesses stay."
              onDelete={() => apiClient.agentBuildingBlocks.removeAgent(row.namespace, row.name)}
              onDeleted={invalidateAgents}
            />
          </Space>
        ),
      },
    ],
    [conversationCounts, conversations.data, conversations.error, descriptions, invalidateAgents, theme, view],
  );

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
      {/* The namespace list feeds the agent read, so its failure is this page's failure. */}
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
        <Alert
          type="warning"
          showIcon
          title="Some agent descriptions could not be loaded"
          description={templates.error?.message ?? templates.data?.refused.map((entry) => `${entry.namespace}: ${entry.reason}`).join("; ")}
          data-testid="agent-descriptions-error"
        />
      ) : null}

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
          title={`${orphanedConversations} ${orphanedConversations === 1 ? "conversation belongs" : "conversations belong"} to no agent here`}
          description="Their agent was deleted. They still run on the revision they started with."
          action={
            <Link to={paths.agentsUnmapped} data-testid="agents-orphaned-link">
              <Button size="small">View them</Button>
            </Link>
          }
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
                {filtered.length} of {agents.length} {agents.length === 1 ? "agent" : "agents"}
              </Text>
            ) : null}
            <RefreshButton onRefresh={refreshThisTab} what="Agents" />
          </Space>
        }
      />

      <Table<Agent>
        data-testid="agents-table"
        rowKey={(row) => row.ref}
        columns={columns}
        dataSource={loadFailure ? [] : filtered}
        loading={definitions.isLoading}
        onChange={listTableChange<Agent>(view)}
        pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
        scroll={{ x: 960 }}
        locale={{
          emptyText: loadFailure
            ? " "
            : view.isNarrowed
              ? "No agents match those filters."
              : definitions.data
                ? "No agents yet. Create one to pair a template with a harness."
                : " ",
        }}
        onRow={(row) =>
          clickableRow(() => {
            const destination = agentNewChatUrl(row);
            if (destination) void navigate(destination);
          })
        }
      />
    </Space>
  );
}

/** A shared resource's name, or an Inline tag when the Agent carries the spec itself. */
function SourceCell({ refName, testId }: { refName?: string; testId: string }) {
  const theme = useTheme();
  return refName ? (
    <Text
      ellipsis={{ tooltip: refName }}
      data-testid={testId}
      data-source="reference"
      css={{ fontFamily: theme.font.mono, fontSize: 12 }}
    >
      {refName}
    </Text>
  ) : (
    <Tag data-testid={testId} data-source="inline" css={{ marginInlineEnd: 0 }}>
      Inline
    </Tag>
  );
}
