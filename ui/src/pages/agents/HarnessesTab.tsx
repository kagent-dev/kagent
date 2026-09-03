import { useMemo, useState } from "react";
import { RefreshButton } from "@/components/table/RefreshButton";
import { Alert, Skeleton, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { useTheme } from "@emotion/react";
import { useHarnessesAcrossNamespaces, useNamespaces, type Harness } from "@/api";
import { admitsLabels, harnessSelector } from "@/api/domain/harnesses";
import { apiClient, useAgentTemplatesAcrossNamespaces } from "@/api";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import { matchesQuery } from "@/components/table/listTable";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";

/** The filters this tab offers, which `useListView` keeps in the URL. */
const FILTER_IDS = ["ns"] as const;

/**
 * What deleting a harness costs, counted rather than described in general terms.
 *
 * Every template admitted only by this harness stops being admitted by anything, and
 * every agent built on it stops existing — an agent is the pair, so removing one half
 * removes the agent. Saying how many makes that concrete instead of leaving the reader
 * to work out whether it applies to them.
 */
function describeLoss(admitted: number): string {
  if (admitted === 0) {
    return "No agent template is admitted by this harness, so no agent depends on it.";
  }
  return `${admitted} agent ${admitted === 1 ? "template is" : "templates are"} admitted by this harness. Every agent built on it stops existing — an agent is a template paired with a harness, so removing the harness removes the agent. The templates themselves are untouched.`;
}

const { Text } = Typography;

/**
 * The harnesses on the cluster.
 *
 * This tab was read-only, on a note in the codebase saying `HarnessService` was
 * read-only in this build. That was wrong: the service implements create, update and
 * delete and always did — what was read-only was this application, which only ever
 * called `list`. Harnesses are usually installed with the chart, but nothing stops one
 * being made here, and "you cannot" was a claim about the wrong thing.
 *
 * What a reader comes here for is the runtime half of an agent: which runtimes exist,
 * whether they are ready, and — the part that decides whether a template ever becomes
 * an agent — which labels each admits templates on.
 */
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
            // Searchable by what it admits, because that is how somebody looks for the
            // harness that will run a template they are holding.
            ...Object.entries(harnessSelector(row)).map(([key, value]) => `${key}=${value}`),
          ]),
        ),
    [rows, selectedNamespaces, view.query],
  );

  /*
   * How many templates each harness admits, for the delete confirmation.
   *
   * Read here rather than counted from the harness, because admission is a property of
   * the *template's* labels: the harness carries the selector, and only the templates
   * know whether they match it.
   */
  const templates = useAgentTemplatesAcrossNamespaces(
    namespaces.data?.map((row) => row.name),
  );
  const admitted = (harness: Harness) =>
    (templates.data?.templates ?? []).filter((template) =>
      admitsLabels(harness, template.resource.metadata.labels ?? {}),
    ).length;

  const [deleting, setDeleting] = useState<string>();
  const [failure, setFailure] = useState<string>();

  async function remove(row: Harness) {
    setDeleting(row.ref);
    setFailure(undefined);
    try {
      await apiClient.agentBuildingBlocks.removeHarness(row.namespace, row.name);
      await harnesses.refresh();
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
      title: "Admits templates labelled",
      key: "selector",
      render: (_: unknown, row: Harness) => {
        const pairs = Object.entries(harnessSelector(row));
        return pairs.length > 0 ? (
          <Space size={4} wrap data-testid="harness-selector">
            {pairs.map(([key, value]) => (
              <Tag key={key} css={{ fontFamily: theme.font.mono, fontSize: 11 }}>
                {key}={value}
              </Tag>
            ))}
          </Space>
        ) : (
          /* A harness with no selector admits nothing at all — the CRD says so. Worth
             stating, because a template will simply never become an agent under it and
             nothing else on the page would explain why. */
          <Text css={{ color: theme.color.textMuted, fontSize: 12 }}>
            No selector, so it admits no templates
          </Text>
        );
      },
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
        /*
         * Deleting a harness is not deleting a runtime nobody is using.
         *
         * Every template admitted only by this one stops being admitted by anything,
         * so every agent built on it stops existing — which is why the confirmation
         * counts them rather than asking a generic question.
         */
        <DeleteResourceButton
          kind="harness"
          name={row.name}
          disabled={!row.canDelete || deleting === row.ref}
          onDelete={() => remove(row)}
          onDeleted={() => undefined}
          description={describeLoss(admitted(row))}
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
          placeholder: "Search names, runtimes, images and admission labels",
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


      {harnesses.error ? (
        <Alert
          type="error"
          showIcon
          data-testid="harnesses-error"
          title="Could not load harnesses"
          description={harnesses.error.message}
        />
      ) : null}

      {harnesses.isLoading ? (
        <Skeleton active paragraph={{ rows: 4 }} data-testid="harnesses-loading" />
      ) : (
        <Table<Harness>
          data-testid="harnesses-table"
          rowKey={(row) => row.ref}
          columns={columns}
          dataSource={harnesses.error ? [] : filtered}
          pagination={false}
          size="small"
        />
      )}
    </Space>
  );
}
