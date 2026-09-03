import { useMemo } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Alert, Button, Space, Table, Tag, Tooltip, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { RefreshButton } from "@/components/table/RefreshButton";
import { useTheme } from "@emotion/react";
import { ChevronRight } from "lucide-react";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import { listTableChange, matchesQuery, paginationFor } from "@/components/table/listTable";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { buildPath, paths } from "@/router/routes";
import {
  apiClient,
  isUsable,
  useAgentTemplatesAcrossNamespaces,
  useNamespaces,
  type AgentTemplate,
} from "@/api";

const { Text } = Typography;

const FILTER_IDS: readonly string[] = ["ns"];
const PAGE_SIZE = 25;

/**
 * The agent templates in the cluster.
 *
 * ## What a template is
 *
 * Half of an agent. A template says what the agent *does* — its model
 * configuration, system prompt and tools — and a `Harness` says how it *runs* —
 * the runtime adapter, the worker pool and the image. An `AgentInstance` is one of
 * each, so creating an agent is choosing a pair rather than filling in a spec.
 *
 * ## The column that matters most
 *
 * "Runs on". A harness admits templates through a **label selector**, and the CRD
 * is explicit that a harness with no selector admits none — so a template no
 * harness matches reaches no prepared revision and **no agent can ever be created
 * from it**. Nothing about such a template looks wrong: it has a model, a prompt,
 * tools, and a row here like any other.
 *
 * That is why the state is a column rather than a detail, and why the empty case
 * says what to do about it instead of showing a dash. It was confirmed on a
 * cluster: an unlabelled template sat at `status: {observedGeneration: 1}` with no
 * harnesses at all until the one label its harness selects on was added.
 */
/**
 * The templates, as a tab of the agents page.
 *
 * It was a page of its own with its own entry in the sidebar, which put the three
 * halves of one idea in three places: a template, the harness that runs it, and the
 * agent that is the pair. They are tabs of one surface now, and this is the part of it
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

  const view = useListView(FILTER_IDS);
  const selectedNamespaces = view.selected("ns");

  const namespaceNames = useMemo(
    () => (namespaces.data ?? []).map((entry) => entry.name),
    [namespaces.data],
  );

  /*
   * Every namespace by default, the reader's choice when they have made one.
   *
   * This page used to pick a single namespace — `kagent` if it existed, otherwise the
   * first — and offer a dropdown to change it. That made it the only landing page a
   * reader could not simply look at: a template in a namespace they had not selected
   * was not "filtered out", it had never been read, and nothing on screen said so.
   *
   * `ListAgentTemplates` takes one namespace and **refuses an empty one** rather than
   * treating it as a wildcard, so "all namespaces" is a request per namespace, merged,
   * with any refusal named rather than silently shortening the list.
   */
  const readNamespaces = useMemo(
    () => (selectedNamespaces.length > 0 ? selectedNamespaces : namespaceNames),
    [selectedNamespaces, namespaceNames],
  );
  const templates = useAgentTemplatesAcrossNamespaces(readNamespaces);

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
        matchesQuery(view.query, [
          row.name,
          row.namespace,
          row.description ?? "",
          row.modelConfigRef,
          ...row.admittingHarnesses,
        ]),
      ),
    [rows, view.query],
  );

  const loadFailure = namespaces.error ?? templates.error;

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
        render: (_, row) => (row.resource.spec.tools ?? []).length,
      },
      {
        /*
         * Whether anything will run it — the question that decides whether the row
         * above is usable at all. A template admitted by nothing is not broken and
         * not incomplete; it simply cannot become an agent, and only this column
         * says so.
         */
        title: "Runs on",
        key: "harnesses",
        render: (_, row) =>
          isUsable(row) ? (
            <Space size={4} wrap>
              {row.admittingHarnesses.map((harness) => (
                <Tag key={harness} color="success">
                  {harness}
                </Tag>
              ))}
            </Space>
          ) : (
            <Tooltip title="A harness admits templates through a label selector, and none of them selects this template's labels. No agent can be created from it until that changes.">
              <Tag color="warning" data-testid={`template-unusable-${row.name}`}>
                No harness
              </Tag>
            </Tooltip>
          ),
      },
      {
        title: "",
        key: "actions",
        width: 96,
        render: (_, row) => (
          <Space size={4}>
            {/*
              Opens the details page, where editing is a mode rather than the
              landing state. Labelled for what it does now: a pencil that lands on
              a read-only page would be promising an input that is not there.
            */}
            <Link
              to={buildPath(paths.agentTemplateDetail, {
                namespace: row.namespace,
                name: row.name,
              })}
            >
              <Button
                type="text"
                icon={<ChevronRight size={16} />}
                data-testid={`open-${row.name}`}
                aria-label={`Open agent template ${row.name}`}
              />
            </Link>
            <DeleteResourceButton
              kind="agent template"
              name={row.name}
              disabled={!row.canDelete}
              onDelete={() =>
                apiClient.agentBuildingBlocks.removeAgentTemplate(
                  row.namespace,
                  row.name,
                )
              }
              onDeleted={templates.refresh}
            />
          </Space>
        ),
      },
    ],
    [templates.refresh, theme.color.textMuted, theme.font.mono],
  );

  const refreshThisTab = templates.refresh;

  return (
    <Space orientation="vertical" size="middle" css={{ display: "flex" }}>

        {/* The namespace read is an input to the template read — templates are fetched
            one namespace at a time — so its failure is a failure of this page rather
            than a detail beside it. Without this the page would sit at "no templates"
            when the namespaces could not be listed at all. */}
        {loadFailure ? (
          <Alert
            type="error"
            showIcon
            title="Could not load agent templates"
            description={loadFailure.message}
            data-testid="templates-error"
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
              options: namespaceNames.map((name) => ({ value: name })),
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
          onRow={(row) => ({
            className: "clickable-table-row",
            onClick: (event) => {
              if (
                (event.target as HTMLElement).closest(
                  "a, button, input, [role='button'], .ant-popover, .ant-dropdown",
                )
              ) {
                return;
              }
              void navigate(
                buildPath(paths.agentTemplateDetail, {
                  namespace: row.namespace,
                  name: row.name,
                }),
              );
            },
          })}
        />
    </Space>
  );
}
