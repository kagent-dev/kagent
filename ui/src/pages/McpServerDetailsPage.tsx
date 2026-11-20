import { useEffect } from "react";
import { Alert, Button, Card, Descriptions, Skeleton, Space, Table, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useLocation, useParams, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { RefreshButton } from "@/components/table/RefreshButton";
import { BackButton } from "@/components/common/BackButton";
import { useGoBack } from "@/router/backLink";
import { ResourceLink } from "@/components/common/ResourceLink";
import { resourceIcons } from "@/components/common/resourceIcons";
import { ToolServerTools } from "@/components/mcp/ToolServerTools";
import { ToolTooltip } from "@/components/mcp/ToolTooltip";
import { safeDecode, toolAnchor } from "@/components/mcp/toolAnchor";
import {
  bindingGroupKind,
  findToolServer,
  useAgentTemplatesAcrossNamespaces,
  useMcpServers,
  type AgentTemplate,
  type ToolBinding,
} from "@/api";
import { backLinkState } from "@/router/backLink";
import { buildPath, MCP_SERVER_KIND_PARAM, paths } from "@/router/routes";

const { Text } = Typography;

/**
 * One MCP server and the tools it reports. There is no get for a tool server, so it
 * is found in the list. `?kind=` picks one when two kinds share a name; links made
 * without it match by name alone. A `#tool-<name>` hash scrolls to and marks that tool.
 */
export function McpServerDetailsPage() {
  const theme = useTheme();
  const location = useLocation();
  const { hash } = location;
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const back = useGoBack({ to: paths.mcpServers, label: "Back to MCP servers" });
  const [searchParams] = useSearchParams();
  const servers = useMcpServers();
  const kind = searchParams.get(MCP_SERVER_KIND_PARAM);
  const server = servers.data && findToolServer(servers.data, `${namespace}/${name}`, kind);
  const missing = Boolean(servers.data) && !server;
  const descriptions = new Map(server?.discoveredTools.map((tool) => [tool.name, tool.description]));
  const templates = useAgentTemplatesAcrossNamespaces(namespace ? [namespace] : undefined);
  const bindsThis = ({ mcp }: ToolBinding, template: AgentTemplate) =>
    mcp !== undefined &&
    mcp.server.name === name &&
    (mcp.server.namespace || template.namespace) === namespace &&
    bindingGroupKind(mcp.server) === server?.groupKind;
  const users = (templates.data?.templates ?? []).filter((template) =>
    (template.resource.spec.tools ?? []).some((binding) => bindsThis(binding, template)),
  );

  const anchor = safeDecode(hash.replace(/^#/, ""));
  const currentTool = anchor.startsWith(toolAnchor("")) ? anchor.slice(toolAnchor("").length) : undefined;
  // Keyed on a boolean, so a background refresh does not scroll the reader back.
  const found = Boolean(server);
  useEffect(() => {
    if (found && currentTool) document.getElementById(toolAnchor(currentTool))?.scrollIntoView({ block: "center" });
  }, [found, currentTool]);

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
        ) : !servers.error && !missing ? (
          <Skeleton active paragraph={{ rows: 4 }} data-testid="mcp-server-detail-loading" />
        ) : null}

        {server ? (
          <Card size="small" title="Templates using it" css={{ maxWidth: 900 }} data-testid="mcp-server-users">
            {templates.error ? (
              <Text css={{ color: theme.color.textMuted }}>Could not read the templates in this namespace.</Text>
            ) : !templates.data ? (
              <Skeleton active paragraph={{ rows: 2 }} title={false} />
            ) : users.length === 0 ? (
              <Text css={{ color: theme.color.textMuted }}>No templates in this namespace use it.</Text>
            ) : (
              <Table<AgentTemplate>
                size="small"
                rowKey={(row) => row.ref}
                dataSource={users}
                pagination={users.length > 10 ? { pageSize: 10, hideOnSinglePage: true } : false}
                columns={[
                  {
                    title: "Template",
                    key: "template",
                    render: (_, template) => (
                      <ResourceLink
                        mono
                        css={{ fontSize: 12 }}
                        to={buildPath(paths.agentTemplateDetail, { namespace: template.namespace, name: template.name })}
                        state={backLinkState(location, "Back to MCP server")}
                        icon={resourceIcons.template}
                        data-testid={`mcp-server-user-${template.name}`}
                      >
                        {template.name}
                      </ResourceLink>
                    ),
                  },
                  {
                    title: "Tools used",
                    key: "tools",
                    render: (_, template) => {
                      const names = (template.resource.spec.tools ?? [])
                        .filter((binding) => bindsThis(binding, template))
                        .flatMap((binding) => binding.mcp?.tools ?? []);
                      return names.length ? (
                        <Space size={[4, 4]} wrap>
                          {names.map((tool) => (
                            <ToolTooltip key={tool} name={tool} description={descriptions.get(tool)}>
                              {/* Focusable so keyboard users get the tooltip too. */}
                              <Tag
                                tabIndex={0}
                                data-testid="mcp-server-user-tool"
                                css={{
                                  fontFamily: theme.font.mono,
                                  marginInlineEnd: 0,
                                  cursor: "help",
                                  "&:hover, &:focus-visible": { borderColor: theme.color.primaryText },
                                  "&:active": { color: theme.color.text, borderColor: theme.color.text },
                                  "&:focus-visible": { outline: `2px solid ${theme.color.primaryText}`, outlineOffset: 1 },
                                }}
                              >
                                {tool}
                              </Tag>
                            </ToolTooltip>
                          ))}
                        </Space>
                      ) : (
                        <Text css={{ color: theme.color.textMuted }}>All tools</Text>
                      );
                    },
                  },
                ]}
              />
            )}
          </Card>
        ) : null}

        {server ? (
          <Card
            size="small"
            title={`Tools (${server.discoveredTools.length})`}
            data-testid="mcp-server-tools"
          >
            <ToolServerTools tools={server.discoveredTools} anchors={{ current: currentTool }} />
          </Card>
        ) : null}
      </Space>
    </PageFrame>
  );
}
