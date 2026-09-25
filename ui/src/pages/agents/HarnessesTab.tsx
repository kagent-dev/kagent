import { useMemo, useState } from "react";
import { RefreshButton } from "@/components/table/RefreshButton";
import { Alert, Skeleton, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useTheme } from "@emotion/react";
import {
  useHarnessesAcrossNamespaces,
  useInvalidateAgentTemplates,
  useNamespaces,
  type Harness,
} from "@/api";
import { apiClient, useAgentsAcrossNamespaces } from "@/api";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import { matchesQuery } from "@/components/table/listTable";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";

/** The filters this tab offers, which `useListView` keeps in the URL. */
const FILTER_IDS = ["ns"] as const;


function describeLoss(count: number): string {
  return `${count} Agent(s) directly reference this Harness. They cannot prepare updated configuration until it is restored or replaced. Their last successful revisions and existing conversations are retained.`;
}

const { Text } = Typography;


export function HarnessesTab() {
  const theme = useTheme();
  /*
   * Read one namespace at a time, like the templates beside them.
   *
   * `ListHarnesses` validates its namespace and refuses an empty one rather than
   * treating it as a wildcard, so asking for "all harnesses" returns nothing at all —
   * and the fixtures answer that happily, which is why this looked fine until a
   * cluster showed an empty table.
   */
  const namespaces = useNamespaces();
  const harnesses = useHarnessesAcrossNamespaces(namespaces.data?.map((row) => row.name));

  // `ListHarnesses` needs a namespace, so a failed namespace read means the harness read
  // is never issued and `harnesses.error` stays empty — an empty table, not a failure.
  const loadFailure = namespaces.error ?? harnesses.error;

  const view = useListView(FILTER_IDS);
  const selectedNamespaces = view.selected("ns");

  const rows = harnesses.data ?? [];
  const filtered = useMemo(
    () =>
      rows
        .filter(
          (row) =>
            selectedNamespaces.length === 0 || selectedNamespaces.includes(row.namespace),
        )
        .filter((row) =>
          matchesQuery(view.query, [
            row.name,
            row.namespace,
            row.runtime,
            row.workloadImage,
          ]),
        ),
    [rows, selectedNamespaces, view.query],
  );


  const agents = useAgentsAcrossNamespaces(namespaces.data?.map(row => row.name));
  const referenced = (harness: Harness) => (agents.data?.agents ?? []).filter(agent =>
    agent.namespace === harness.namespace && agent.resource.spec.harnessRef?.name === harness.name);

  const [deleting, setDeleting] = useState<string>();
  const [failure, setFailure] = useState<string>();
  const invalidateTemplates = useInvalidateAgentTemplates();

  async function remove(row: Harness) {
    setDeleting(row.ref);
    setFailure(undefined);
    try {
      await apiClient.agentBuildingBlocks.removeHarness(row.namespace, row.name);
      // Swallowed for the reason the create pages give: the delete has already
      // succeeded, and `refresh` rethrows into the catch below.
      await harnesses.refresh().catch(() => {});
      // And the templates, because an agent is a template paired with a harness — the
      // confirmation above says every agent built on this one stops existing, and the
      // agents list is derived from the template read rather than from this one.
      await invalidateTemplates().catch(() => {});
    } catch (cause: unknown) {
      setFailure(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setDeleting(undefined);
    }
  }

  const columns: ColumnsType<Harness> = [
    {
      title: "Harness",
      dataIndex: "name",
      key: "name",
      render: (_: unknown, row: Harness) => (
        <Space orientation="vertical" size={0}>
          <Text css={{ fontFamily: theme.font.mono }}>{row.name}</Text>
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>{row.namespace}</Text>
        </Space>
      ),
    },
    {
      title: "Runtime",
      dataIndex: "runtime",
      key: "runtime",
      render: (runtime: string) => <Tag>{runtime || "Not reported"}</Tag>,
    },
    {
      title: "Ready",
      dataIndex: "ready",
      key: "ready",
      render: (ready: boolean) => (
        /* Not "broken" when false. The condition is also false for a harness the
           controller has not observed yet, which is a different thing from one that
           failed — and the wrong word would send somebody debugging a new harness. */
        <Tag color={ready ? "green" : "default"} data-testid="harness-ready">
          {ready ? "Ready" : "Not ready yet"}
        </Tag>
      ),
    },
    {
      title: "Workload image",
      dataIndex: "workloadImage",
      key: "workloadImage",
      render: (image: string) => (
        <Text
          ellipsis={{ tooltip: image }}
          copyable={image ? { text: image } : false}
          css={{ fontFamily: theme.font.mono, fontSize: 11, maxWidth: 260 }}
        >
          {image || "Not reported"}
        </Text>
      ),
    },
    {
      title: "",
      key: "actions",
      width: 56,
      render: (_: unknown, row: Harness) => (

        <DeleteResourceButton
          kind="harness"
          name={row.name}
          disabled={deleting === row.ref}
          onDelete={() => remove(row)}
          onDeleted={() => undefined}
          description={agents.error || !agents.data || agents.data.refused.length ? "Could not determine every Agent using this Harness. Existing revisions are retained; Agents using it cannot prepare updated configuration." : describeLoss(referenced(row).length)}
        />
      ),
    },
  ];

  const refreshThisTab = harnesses.refresh;

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>

      <FilterBar
        testId="harnesses-filters"
        view={view}
        search={{
          label: "Search harnesses",
          placeholder: "Search names, runtimes and images",
        }}
        filters={[
          {
            id: "ns",
            label: "Namespace",
            allLabel: "All namespaces",
            options: (namespaces.data ?? []).map((entry) => ({ value: entry.name })),
          },
        ]}
        trailing={
          <Space size={8}>
            {!harnesses.error && !harnesses.isLoading ? (
              <Text data-testid="harnesses-summary" css={{ color: theme.color.textMuted }}>
                {filtered.length} of {rows.length}{" "}
                {rows.length === 1 ? "harness" : "harnesses"}
              </Text>
            ) : null}
            {/* Beside the controls that narrow this list, and it refreshes this list:
                a control in a table's own filter row that quietly re-read two other
                tabs would be doing more than it appears to. */}
            <RefreshButton onRefresh={refreshThisTab} what="Harnesses" />
          </Space>
        }
      />

      {failure ? (
        <Alert
          type="error"
          showIcon
          data-testid="harnesses-delete-error"
          title="Could not delete that harness"
          description={failure}
        />
      ) : null}


      {loadFailure ? (
        <Alert
          type="error"
          showIcon
          data-testid="harnesses-error"
          title="Could not load harnesses"
          description={loadFailure.message}
        />
      ) : null}

      {harnesses.isLoading ? (
        <Skeleton active paragraph={{ rows: 4 }} data-testid="harnesses-loading" />
      ) : (
        <Table<Harness>
          data-testid="harnesses-table"
          rowKey={(row) => row.ref}
          columns={columns}
          dataSource={loadFailure ? [] : filtered}
          pagination={false}
          size="small"
        />
      )}
    </Space>
  );
}
