import { useMemo, useRef, useState } from "react";
import { Alert, Button, Card, Checkbox, Descriptions, Empty, Input, Skeleton, Space, Switch, Table, Tabs, Tag, Tooltip, Typography, type InputRef } from "antd";
import { useTheme } from "@emotion/react";
import { Plus, Search } from "lucide-react";
import { useLocation, useParams, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { RefreshButton } from "@/components/table/RefreshButton";
import { BackButton } from "@/components/common/BackButton";
import { useGoBack } from "@/router/backLink";
import { ResourceLink } from "@/components/common/ResourceLink";
import { resourceIcons } from "@/components/common/resourceIcons";
import { DataPill } from "@/components/form/DataPill";
import { ToolList, type ToolListItem } from "@/components/mcp/ToolList";
import { readToolSearch, toolMatches, writeToolSearch, type ToolSearch } from "@/components/mcp/toolSearch";
import { groupToolsByServer, indexToolServers } from "@/api/domain/templateToolCount";
import {
  findToolServer,
  useAgentTemplatesAcrossNamespaces,
  useMcpServers,
  type AgentTemplate,
} from "@/api";
import { backLinkState, type BackLinkState } from "@/router/backLink";
import { buildPath, MCP_SERVER_KIND_PARAM, paths } from "@/router/routes";

const { Text } = Typography;

const TAB_PARAM = "tab";

/** A template that references this server, with the tools it shows in the Usage table. */
interface UsageRow {
  template: AgentTemplate;
  tools: ToolListItem[];
}

/**
 * One MCP server, its tools and the templates that use them. There is no get for a tool
 * server, so it is found in the list. `?kind=` picks one when two kinds share a name.
 * `?q=` (repeatable) and `?exact=1` search the tools; `?tab=usage` opens the usage tab.
 */
export function McpServerDetailsPage() {
  const theme = useTheme();
  const location = useLocation();
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const back = useGoBack({ to: paths.mcpServers, label: "MCP servers" });
  const [searchParams, setSearchParams] = useSearchParams();
  const servers = useMcpServers();
  const kind = searchParams.get(MCP_SERVER_KIND_PARAM);
  const server = servers.data && findToolServer(servers.data, `${namespace}/${name}`, kind);
  const missing = Boolean(servers.data) && !server;
  const templates = useAgentTemplatesAcrossNamespaces(namespace ? [namespace] : undefined);
  const serversByRef = useMemo(() => indexToolServers(servers.data ?? []), [servers.data]);

  const search = readToolSearch(searchParams);
  const [draft, setDraft] = useState("");
  const input = useRef<InputRef>(null);
  // What is typed filters at once; Enter or + keeps it as a term in the address.
  const live: ToolSearch = draft.trim() ? { ...search, terms: [...search.terms, draft.trim()] } : search;
  const highlight = live.exact ? [] : live.terms;
  // Replaced, keeping the state that says where Back leads.
  const navigation = { replace: true, state: location.state as unknown };
  const setSearch = (next: ToolSearch) => setSearchParams((current) => writeToolSearch(current, next), navigation);
  const addDraft = () => {
    if (!draft.trim()) return;
    setSearch({ ...search, terms: [...search.terms, draft.trim()] });
    setDraft("");
  };
  // A tool's name searches for exactly that tool, on whichever tab it was clicked.
  const toolTo = (tool: ToolListItem) =>
    tool.source ? undefined : { search: `?${writeToolSearch(searchParams, { terms: [tool.name], exact: true })}` };
  const linkState = location.state as BackLinkState | undefined;
  const onSelectTool = () => {
    setDraft("");
    input.current?.focus();
  };

  const tab = searchParams.get(TAB_PARAM) === "usage" ? "usage" : "tools";
  const [onlyThisServer, setOnlyThisServer] = useState(true);
  const discovered = useMemo(() => server?.discoveredTools ?? [], [server]);
  const matchingTools = discovered.filter((tool) => toolMatches(tool, live));

  const usage: UsageRow[] = (templates.data?.templates ?? []).flatMap((template) => {
    const bindings = template.resource.spec.tools ?? [];
    const groups = groupToolsByServer(bindings, serversByRef, template.namespace);
    const own = groups.find((group) => group.ref === server?.ref && group.groupKind === server?.groupKind);
    if (!own) return [];
    const ownTools = own.allTools ? discovered : named(own.selected, discovered);
    if (live.terms.length && !ownTools.some((tool) => toolMatches(tool, live))) return [];
    const otherTools = onlyThisServer
      ? []
      : [
          ...groups
            .filter((group) => group !== own)
            .flatMap((group) =>
              (group.allTools ? (group.discovered ?? []) : named(group.selected, group.discovered ?? [])).map((tool) => ({
                ...tool,
                source: group.ref,
              })),
            ),
          ...bindings.flatMap(({ subAgent }) =>
            subAgent ? [{ name: subAgent.name, description: subAgent.description, source: "Agent template" }] : [],
          ),
        ];
    return [{ template, tools: [...ownTools, ...otherTools].filter((tool) => toolMatches(tool, live)) }];
  });

  const searchBar = (
    <div data-testid="mcp-server-search" css={{ display: "grid", gap: theme.space(2) }}>
      <div css={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: theme.space(3) }}>
        <Space.Compact css={{ width: "100%", maxWidth: 420 }}>
          <Input
            ref={input}
            allowClear
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onPressEnter={addDraft}
            placeholder="Search tools"
            aria-label="Search this server's tools by name or description"
            data-testid="mcp-server-search-input"
            prefix={<Search size={14} color={theme.color.textMuted} aria-hidden />}
          />
          <Tooltip title="Add as a filter">
            <Button
              icon={<Plus size={14} aria-hidden />}
              aria-label="Add as a filter"
              disabled={!draft.trim()}
              onClick={addDraft}
              data-testid="mcp-server-search-add"
            />
          </Tooltip>
        </Space.Compact>
        <Checkbox
          checked={search.exact}
          onChange={(event) => setSearch({ ...search, exact: event.target.checked })}
          data-testid="mcp-server-search-exact"
        >
          Exact match
        </Checkbox>
      </div>
      {search.terms.length ? (
        <div css={{ display: "flex", flexWrap: "wrap", alignItems: "center", gap: theme.space(1) }}>
          {search.terms.map((term) => (
            <DataPill
              key={term}
              onRemove={() => setSearch({ ...search, terms: search.terms.filter((other) => other !== term) })}
              removeLabel={`Remove filter ${term}`}
              data-testid="mcp-server-search-term"
            >
              {term}
            </DataPill>
          ))}
          {search.terms.length > 1 ? (
            <Button type="link" size="small" onClick={() => setSearch({ ...search, terms: [] })} data-testid="mcp-server-search-clear">
              Clear all
            </Button>
          ) : null}
        </div>
      ) : null}
    </div>
  );

  const toolsTab = !discovered.length ? (
    <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} data-testid="tool-server-no-tools" description="This server has not reported any tools yet." />
  ) : !matchingTools.length ? (
    <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} data-testid="mcp-server-tools-no-match" description="No tools match this search." />
  ) : (
    <ToolList tools={matchingTools} highlight={highlight} toolTo={toolTo} linkState={linkState} onSelect={onSelectTool} />
  );

  const usageTab = (
    <Card
      size="small"
      title="Agent templates that reference this MCP server"
      data-testid="mcp-server-users"
      extra={
        <label css={{ display: "inline-flex", alignItems: "center", gap: theme.space(2), cursor: "pointer", fontSize: 13 }}>
          <Switch size="small" checked={onlyThisServer} onChange={setOnlyThisServer} data-testid="mcp-server-users-only-this" />
          Only show tools from this MCP server
        </label>
      }
    >
      {templates.error ? (
        <Text css={{ color: theme.color.textMuted }}>Could not read the templates in this namespace.</Text>
      ) : !templates.data ? (
        <Skeleton active paragraph={{ rows: 2 }} title={false} />
      ) : usage.length === 0 ? (
        <Text css={{ color: theme.color.textMuted }}>
          {live.terms.length ? "No templates use a tool that matches this search." : "No templates in this namespace use it."}
        </Text>
      ) : (
        <Table<UsageRow>
          size="small"
          rowKey={(row) => row.template.ref}
          dataSource={usage}
          pagination={usage.length > 10 ? { pageSize: 10, hideOnSinglePage: true } : false}
          tableLayout="fixed"
          css={{ ".ant-table-tbody .ant-table-cell": { verticalAlign: "top" } }}
          columns={[
            {
              title: "Template",
              key: "template",
              width: "38%",
              render: (_, { template }) => (
                <ResourceLink
                  mono
                  css={{ fontSize: 12 }}
                  to={buildPath(paths.agentTemplateDetail, { namespace: template.namespace, name: template.name })}
                  state={backLinkState(location, "MCP server")}
                  icon={resourceIcons.template}
                  data-testid={`mcp-server-user-${template.name}`}
                >
                  {template.name}
                </ResourceLink>
              ),
            },
            {
              title: "Tools",
              key: "tools",
              render: (_, row) => (
                <ToolList
                  dense
                  tools={row.tools}
                  highlight={highlight}
                  toolTo={toolTo}
                  linkState={linkState}
                  onSelect={onSelectTool}
                  itemTestId="mcp-server-user-tool"
                />
              ),
            },
          ]}
        />
      )}
    </Card>
  );

  return (
    <PageFrame
      title={name ? `MCP server ${name}` : "MCP server"}
      icon={resourceIcons.mcpServer}
      description={namespace && !missing ? `A tool server agents can call, in the ${namespace} namespace.` : undefined}
      actions={
        <Space size={8} wrap>
          <BackButton back={back} testId="mcp-server-back" />
          <RefreshButton onRefresh={servers.refresh} what="MCP server" loading={servers.isLoading} disabled={missing} iconOnly />
        </Space>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        {servers.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load this MCP server"
            description={servers.error.message}
            data-testid="mcp-server-detail-error"
            action={<Button size="small" onClick={() => void servers.refresh()}>Try again</Button>}
          />
        ) : missing ? (
          <Alert
            type="warning"
            showIcon
            title="This MCP server does not exist"
            description={`No MCP server named ${namespace}/${name} was found. It may have been deleted.`}
            data-testid="mcp-server-detail-not-found"
          />
        ) : null}

        {server ? (
          <>
            <Descriptions
              bordered
              column={1}
              size="small"
              css={{ maxWidth: 900 }}
              styles={{ label: { width: 180 } }}
              data-testid="mcp-server-details"
              items={[
                { key: "kind", label: "Kind", children: <Tag>{server.groupKind.split(".")[0] || server.groupKind}</Tag> },
              ]}
            />
            {searchBar}
            <Tabs
              activeKey={tab}
              data-testid="mcp-server-tabs"
              onChange={(key) =>
                setSearchParams((current) => {
                  const next = new URLSearchParams(current);
                  if (key === "usage") next.set(TAB_PARAM, key);
                  else next.delete(TAB_PARAM);
                  return next;
                }, navigation)
              }
              items={[
                {
                  key: "tools",
                  label: live.terms.length ? `Tools (${matchingTools.length} of ${discovered.length})` : `Tools (${discovered.length})`,
                  children: <div data-testid="mcp-server-tools">{toolsTab}</div>,
                },
                { key: "usage", label: "Usage", children: usageTab },
              ]}
            />
          </>
        ) : !servers.error && !missing ? (
          <Skeleton active paragraph={{ rows: 4 }} data-testid="mcp-server-detail-loading" />
        ) : null}
      </Space>
    </PageFrame>
  );
}

/** `names` as tools, with the description `reported` gives each. */
function named(names: readonly string[], reported: readonly { name: string; description: string }[]): ToolListItem[] {
  return names.map((name) => ({ name, description: reported.find((tool) => tool.name === name)?.description }));
}
