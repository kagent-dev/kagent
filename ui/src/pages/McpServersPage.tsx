import { useMemo } from "react";
import { Alert, Button, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { Plus } from "lucide-react";
import { useTheme } from "@emotion/react";
import { Link } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { DeleteResourceButton } from "@/components/table/DeleteResourceButton";
import { Highlighted, ToolServerTools } from "@/components/mcp/ToolServerTools";
import { paths } from "@/router/routes";
import {
  apiClient,
  parseRef,
  useMcpServers,
  useTools,
  type DiscoveredTool,
  type ToolServerResponse,
} from "@/api";
import { RefreshButton } from "@/components/table/RefreshButton";
import { clickableRow, useExpandedRows } from "@/components/table/rowClick";
import { FilterBar } from "@/components/table/FilterBar";
import { useListView } from "@/components/table/useListView";
import {
  byNumber,
  byText,
  listTableChange,
  paginationFor,
  sortOrderFor,
} from "@/components/table/listTable";

const { Text } = Typography;

const FILTER_IDS: readonly string[] = ["ns", "kind"];
const PAGE_SIZE = 25;

/**
 * The kind, without the API group: `RemoteMCPServer.kagent.dev` → `RemoteMCPServer`.
 *
 * The group is the same for every row, so showing it would cost width and tell
 * the reader nothing.
 */
function shortKind(groupKind: string): string {
  return groupKind.split(".")[0] || groupKind;
}

/** A server the filter matched, plus the subset of its tools that matched. */
interface FilteredServer {
  server: ToolServerResponse;
  tools: DiscoveredTool[];
}

/**
 * Narrows servers by ref, tool name and tool description.
 *
 * A hit on the server's own ref keeps all of its tools — the reader asked for
 * that server, so hiding its tools would be an odd way to answer. A hit only on
 * a tool narrows the expanded panel to the tools that matched.
 */
function filterServers(
  servers: readonly ToolServerResponse[],
  query: string,
): FilteredServer[] {
  const needle = query.trim().toLowerCase();
  if (!needle) {
    return servers.map((server) => ({ server, tools: server.discoveredTools }));
  }

  const matched: FilteredServer[] = [];
  for (const server of servers) {
    if (server.ref.toLowerCase().includes(needle)) {
      matched.push({ server, tools: server.discoveredTools });
      continue;
    }
    const tools = server.discoveredTools.filter(
      (tool) =>
        tool.name.toLowerCase().includes(needle) ||
        tool.description.toLowerCase().includes(needle),
    );
    if (tools.length > 0) matched.push({ server, tools });
  }
  return matched;
}

/**
 * Tool servers.
 *
 * ## Where the narrowing happens
 *
 * `ListToolServers` takes an **empty request** — no page, no filter, no sort — so the
 * whole registry arrives in one message and everything below happens in the browser.
 * That is truthful here for the reason it is truthful on the models page and would
 * not be on the substrate page's paged tables: every row is present, so a search
 * covers every row. The note under the table says so, and `playwright/DEFERRED.md`
 * records the API change the RPC needs.
 */
export function McpServersPage() {
  const theme = useTheme();
  const expanded = useExpandedRows();
  const { data, isLoading, error, isEmpty, refresh } = useMcpServers();
  // `/tools` is the registry agents actually bind against; `discoveredTools` is
  // what each server reported at its last handshake. They normally agree, and
  // repeating the same number twice would be noise — so the registry figure is
  // shown only when the two disagree, which is the case worth noticing. A
  // failure here is deliberately not fatal: the list is still worth reading.
  const tools = useTools();
  const view = useListView(FILTER_IDS);

  const servers = useMemo(() => data ?? [], [data]);

  const namespaceOptions = useMemo(
    () =>
      [...new Set(servers.map((server) => parseRef(server.ref).namespace))]
        .filter(Boolean)
        .sort()
        .map((value) => ({ value })),
    [servers],
  );

  const kindOptions = useMemo(
    () =>
      [...new Set(servers.map((server) => shortKind(server.groupKind)))]
        .filter(Boolean)
        .sort()
        .map((value) => ({ value })),
    [servers],
  );

  const selectedNamespaces = view.selected("ns");
  const selectedKinds = view.selected("kind");

  const filtered = useMemo(() => {
    const inScope = servers.filter((server) => {
      if (
        selectedNamespaces.length > 0 &&
        !selectedNamespaces.includes(parseRef(server.ref).namespace)
      ) {
        return false;
      }
      return (
        selectedKinds.length === 0 ||
        selectedKinds.includes(shortKind(server.groupKind))
      );
    });
    return filterServers(inScope, view.query);
  }, [servers, selectedNamespaces, selectedKinds, view.query]);

  const isFiltering = view.query.trim().length > 0;

  const columns: ColumnsType<FilteredServer> = useMemo(
    () => [
      {
        title: "Name",
        key: "name",
        sorter: byText<FilteredServer>((row) => parseRef(row.server.ref).name),
        sortOrder: sortOrderFor(view, "name"),
        render: (_, row) => (
          <span css={{ fontFamily: theme.font.mono }}>
            <Highlighted text={parseRef(row.server.ref).name} term={view.query} />
          </span>
        ),
      },
      {
        title: "Namespace",
        key: "namespace",
        sorter: byText<FilteredServer>((row) => parseRef(row.server.ref).namespace),
        sortOrder: sortOrderFor(view, "namespace"),
        render: (_, row) => (
          <Highlighted
            text={parseRef(row.server.ref).namespace || "—"}
            term={view.query}
          />
        ),
      },
      {
        title: "Kind",
        key: "kind",
        sorter: byText<FilteredServer>((row) => shortKind(row.server.groupKind)),
        sortOrder: sortOrderFor(view, "kind"),
        render: (_, row) => {
          // Remote servers are dialled out to; managed ones the controller runs
          // itself. Colouring them apart makes the two populations legible at a
          // glance without a legend.
          //
          // The colours are the theme's own rather than antd's presets. Those are derived
          // for a light page: on the dark theme `geekblue` and `purple` measured 4.2:1 and
          // 3.4:1, and small text needs 4.5. These triples are stated per palette for
          // exactly this reason — see the palette's note on status pills.
          const kind = shortKind(row.server.groupKind);
          const pill =
            kind === "RemoteMCPServer"
              ? {
                  background: theme.color.infoBg,
                  borderColor: theme.color.infoBorder,
                  color: theme.color.infoText,
                }
              : {
                  background: theme.color.accentBg,
                  borderColor: theme.color.accentBorder,
                  color: theme.color.accentText,
                };
          return <Tag css={pill}>{kind}</Tag>;
        },
      },
      {
        title: "Tools",
        key: "tools",
        sorter: byNumber<FilteredServer>((row) => row.server.discoveredTools.length),
        sortOrder: sortOrderFor(view, "tools"),
        render: (_, row) =>
          isFiltering && row.tools.length !== row.server.discoveredTools.length
            ? `${row.tools.length} of ${row.server.discoveredTools.length}`
            : row.server.discoveredTools.length,
      },
      {
        // The API has supported removing a tool server all along; nothing here
        // reached it, so a server registered by mistake could only be deleted with
        // kubectl. There is no edit beside it because there is no update endpoint —
        // `mcpServers` has list, get, create and delete and no PUT.
        title: "",
        key: "actions",
        width: 48,
        render: (_, row) => {
          const { namespace, name } = parseRef(row.server.ref);
          return (
            <DeleteResourceButton
              kind="tool server"
              name={name}
              onDelete={() => apiClient.mcpServers.remove(namespace, name)}
              onDeleted={refresh}
            />
          );
        },
      },
    ],
    [theme, view, isFiltering, refresh],
  );

  const toolTotal = servers.reduce(
    (total, server) => total + server.discoveredTools.length,
    0,
  );

  return (
    <PageFrame
      title="MCP servers"
      description="Tool servers agents can call. Expand a server to see the tools it exposes."
      actions={
        <Space size={8}>
          <RefreshButton
            onRefresh={refresh}
            what="Tool servers"
            loading={isLoading}
          />
          <Link to={paths.mcpServerNew}>
            <Button
              type="primary"
              icon={<Plus size={14} />}
              data-testid="mcp-servers-new"
            >
              New Server
            </Button>
          </Link>
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load MCP servers"
            description={error.message}
            data-testid="mcp-servers-error"
            action={
              <Button size="small" onClick={() => void refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        <FilterBar
          testId="mcp-servers-filters"
          view={view}
          search={{
            label: "Search servers and tools",
            placeholder: "Search servers, tools and descriptions",
          }}
          filters={[
            {
              id: "ns",
              label: "Namespace",
              allLabel: "All namespaces",
              options: namespaceOptions,
            },
            {
              id: "kind",
              label: "Kind",
              allLabel: "All kinds",
              options: kindOptions,
              minWidth: 240,
            },
          ]}
          trailing={
            /* Only a successful load can be counted. Saying "0 servers" because a
               request failed would be a claim the page cannot support. */
            !error && !isLoading ? (
              <Text
                data-testid="mcp-servers-summary"
                css={{ color: theme.color.textMuted }}
              >
                {filtered.length} of {servers.length}{" "}
                {servers.length === 1 ? "server" : "servers"} · {toolTotal}{" "}
                {toolTotal === 1 ? "tool" : "tools"}
                {tools.data && tools.data.length !== toolTotal
                  ? ` · ${tools.data.length} in the tool registry`
                  : ""}
              </Text>
            ) : null
          }
        />

        <Table<FilteredServer>
          data-testid="mcp-servers-table"
          rowKey={(row) => row.server.ref}
          columns={columns}
          // A failure already has its own message above. Leaving rows out of the
          // table as well keeps it from claiming "there are no servers", which is
          // a different thing from "we could not find out".
          dataSource={error ? [] : filtered}
          loading={isLoading}
          onChange={listTableChange<FilteredServer>(view)}
          pagination={paginationFor(view, filtered.length, PAGE_SIZE)}
          expandable={{
            // Keying the panel on the query remounts it when the filter changes,
            // so an already-open row re-renders against the tools that matched.
            expandedRowRender: (row) => (
              <ToolServerTools
                key={view.query}
                tools={row.tools}
                highlight={view.query}
              />
            ),
            // Controlled rather than `expandRowByClick`, so `clickableRow` can keep a
            // click on the delete button from also unfolding the row's tools.
            expandedRowKeys: expanded.keys,
            onExpand: (open, row) => expanded.set(row.server.ref, open),
          }}
          onRow={(row) => clickableRow(() => expanded.toggle(row.server.ref))}
          locale={{
            emptyText: isEmpty
              ? "No MCP servers yet."
              : view.isNarrowed && !error
                ? "No servers or tools match those filters."
                : " ",
          }}
        />
      </Space>
    </PageFrame>
  );
}
