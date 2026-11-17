import { isPermissionDenied } from "@/api/ApiError";
import { ReadError } from "@/components/common/ReadError";
import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Alert, Space, Table, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { RefreshButton } from "@/components/table/RefreshButton";
import { useTheme } from "@emotion/react";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import { listTableChange, matchesQuery, paginationFor } from "@/components/table/listTable";
import { RowActionsMenu } from "@/components/table/RowActionsMenu";
import { copyPath } from "@/components/table/copyFrom";
import { clickableRow } from "@/components/table/rowClick";
import { buildPath, paths } from "@/router/routes";
import {
  apiClient,
  useAgentTemplatesAcrossNamespaces,
  countTemplateTools,
  indexToolServers,
  useInvalidateAgentTemplates,
  useMcpServers,
  useNamespaces,
  type AgentTemplate,
} from "@/api";

const { Text } = Typography;

const FILTER_IDS: readonly string[] = ["ns"];
const PAGE_SIZE = 25;


/**
 * The templates, as a tab of the agents page.
 *
 * It was a page of its own with its own entry in the sidebar, which put the three
 * halves of one idea in three places: a template, the harness that runs it, and the
 * Agent that references them. They are tabs of one surface now, and this is the part of it
 * that lists templates.
 *
 * It keeps its own refresh and its own "new" button rather than handing them to the
 * shell: each tab reads different things and refreshing the one you are not looking at
 * is a request for nothing.
 */
export function AgentTemplatesTab() {
  const theme = useTheme();
  const navigate = useNavigate();
  const namespaces = useNamespaces();
  const namespaceDenied = isPermissionDenied(namespaces.error);

  const view = useListView(FILTER_IDS);
  const selectedNamespaces = view.selected("ns");

  const namespaceNames = useMemo(
    () => (namespaces.data ?? []).map((entry) => entry.name),
    [namespaces.data],
  );

  // The collection RPC can still list visible resources if namespace discovery is denied.
  const readNamespaces = namespaceDenied ? null : selectedNamespaces.length > 0 ? selectedNamespaces : namespaceNames;
  const invalidateTemplates = useInvalidateAgentTemplates();
  const templates = useAgentTemplatesAcrossNamespaces(readNamespaces);

  const mcpServers = useMcpServers();
  const serversByRef = useMemo(
    () => indexToolServers(mcpServers.data ?? []),
    [mcpServers.data],
  );

  const rows = useMemo(() => templates.data?.templates ?? [], [templates.data]);

  /*
   * Narrowed in the browser, because the service narrows nothing.
   *
   * `ListAgentTemplates` takes no page, sort or search parameter — the whole list per
   * namespace is what it returns — so searching here covers every row that was read
   * rather than one page of it. The note under the table says so; a search box that
   * silently covered less than it appeared to is the defect this repo already fixed
   * once on the substrate page.
   */
  const filtered = useMemo(
    () =>
      rows.filter((row) =>
        (!namespaceDenied || selectedNamespaces.length === 0 || selectedNamespaces.includes(row.namespace)) &&
        matchesQuery(view.query, [
          row.name,
          row.namespace,
          row.description ?? "",
          row.modelConfigRef,
        ]),
      ),
    [namespaceDenied, rows, selectedNamespaces, view.query],
  );

  const namespaceFailure = namespaceDenied ? undefined : namespaces.error;
  const loadFailure = namespaceFailure ?? templates.error;

  const columns = useMemo<ColumnsType<AgentTemplate>>(
    () => [
      {
        title: "Template",
        key: "name",
        render: (_, row) => (
          <Link
            to={buildPath(paths.agentTemplateDetail, {
              namespace: row.namespace,
              name: row.name,
            })}
            data-testid={`template-link-${row.name}`}
          >
            <span css={{ fontFamily: theme.font.mono, fontSize: 13 }}>{row.name}</span>
          </Link>
        ),
      },
      {
        title: "Description",
        key: "description",
        render: (_, row) =>
          row.description ? (
            row.description
          ) : (
            <Text css={{ color: theme.color.textMuted }}>—</Text>
          ),
      },
      {
        title: "Model",
        key: "model",
        render: (_, row) => (
          <Text css={{ fontFamily: theme.font.mono, fontSize: 12 }}>
            {row.modelConfigRef || "—"}
          </Text>
        ),
      },
      {
        title: "Tools",
        key: "tools",
        width: 90,
        render: (_, row) => {
          const bindings = row.resource.spec.tools ?? [];
          if (bindings.length === 0) return 0;
          const { total, unknownServers } = countTemplateTools(bindings, serversByRef, row.namespace);
          // Until the servers load, or when one has no discovered tools on record, a total would be a guess.
          if (unknownServers.length > 0 && mcpServers.isLoading) {
            return <Text css={{ color: theme.color.textMuted }}>…</Text>;
          }
          return unknownServers.length === 0 ? (
            total
          ) : (
            <Tooltip title={`Plus all tools of ${unknownServers.join(", ")}, which could not be counted.`}>
              <span>{total}+</span>
            </Tooltip>
          );
        },
      },
      {
        title: "",
        key: "actions",
        width: 56,
        render: (_, row) => (
          <RowActionsMenu
            kind="agent template"
            namespace={row.namespace}
            name={row.name}
            editLabel="Open"
            editTo={buildPath(paths.agentTemplateDetail, { namespace: row.namespace, name: row.name })}
            copyTo={copyPath(paths.agentTemplateNew, row)}
            remove={{
              onDelete: () => apiClient.agentBuildingBlocks.removeAgentTemplate(row.namespace, row.name),
              /* The agents list reads templates under a key of its own, so refreshing
                 this list alone leaves an agent there that nothing runs. */
              onDeleted: invalidateTemplates,
            }}
          />
        ),
      },
    ],
    [invalidateTemplates, mcpServers.isLoading, serversByRef, theme.color.textMuted, theme.font.mono],
  );

  // The tool counts come from the server list, so refreshing the tab reloads it too.
  const refreshThisTab = async () => {
    const results = await Promise.allSettled([templates.refresh(), mcpServers.refresh()]);
    const failed = results.find((result) => result.status === "rejected");
    if (failed) throw failed.reason;
  };

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {loadFailure ? (
          <ReadError
            resource="agent templates"
            error={loadFailure}
            testId="templates-error"
            onRetry={namespaceFailure ? namespaces.refresh : templates.refresh}
          />
        ) : null}

        <FilterBar
          testId="templates-filters"
          view={view}
          search={{
            label: "Search agent templates",
            placeholder: "Search names, descriptions, models and harnesses",
          }}
          filters={[
            {
              id: "ns",
              label: "Namespace",
              allLabel: "All namespaces",
              options: (namespaceDenied ? [...new Set(rows.map((row) => row.namespace))] : namespaceNames).map((value) => ({ value })),
            },
          ]}
          trailing={
            <Space size={8}>
              {/* Only a successful load can be counted: "0 of 0" because a request
                  failed would be a claim the page cannot support. */}
              {!loadFailure && !templates.isLoading ? (
                <Text data-testid="templates-summary" css={{ color: theme.color.textMuted }}>
                  {filtered.length} of {rows.length}{" "}
                  {rows.length === 1 ? "template" : "templates"}
                </Text>
              ) : null}
              {/* Beside the controls that narrow this list, and it refreshes this list:
                  a control in a table's own filter row that quietly re-read two other
                  tabs would be doing more than it appears to. */}
              <RefreshButton onRefresh={refreshThisTab} what="Templates" />
            </Space>
          }
        />

        {/* Partial rather than all-or-nothing: a reader may be allowed six namespaces and
            refused a seventh, and failing the whole list because of one would show them
            nothing. Named rather than quietly shortening the list. */}
        {templates.data?.refused.length ? (
          <Alert
            type="warning"
            showIcon
            data-testid="templates-refused"
            title={`${templates.data.refused.length} namespace${templates.data.refused.length === 1 ? "" : "s"} could not be read`}
            description={templates.data.refused
              .map((entry) => `${entry.namespace}: ${entry.reason}`)
              .join("; ")}
          />
        ) : null}

        <Table<AgentTemplate>
          data-testid="templates-table"
          rowKey={(row) => row.ref}
          columns={columns}
          // A failure has its own banner above; leaving the rows out keeps the table
          // from also claiming the namespace holds nothing.
          dataSource={loadFailure ? [] : filtered}
          loading={templates.isLoading}
          onChange={listTableChange<AgentTemplate>(view)}
          pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
          locale={{
            emptyText: loadFailure
              ? " "
              : rows.length === 0
                ? "No agent templates yet."
                : view.isNarrowed
                  ? "No agent templates match those filters."
                  : " ",
          }}
          onRow={(row) =>
            clickableRow(() =>
              void navigate(
                buildPath(paths.agentTemplateDetail, {
                  namespace: row.namespace,
                  name: row.name,
                }),
              ),
            )
          }
        />
    </Space>
  );
}
