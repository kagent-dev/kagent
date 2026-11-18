import { useEffect } from "react";
import { Alert, Button, Card, Descriptions, Skeleton, Space, Table, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { useLocation, useParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { RefreshButton } from "@/components/table/RefreshButton";
import { BackButton } from "@/components/common/BackButton";
import { useGoBack } from "@/router/backLink";
import { ResourceLink } from "@/components/common/ResourceLink";
import { ToolServerTools } from "@/components/mcp/ToolServerTools";
import { safeDecode, toolAnchor } from "@/components/mcp/toolAnchor";
import { useAgentTemplatesAcrossNamespaces, useMcpServers, type AgentTemplate } from "@/api";
import { backLinkState } from "@/router/backLink";
import { buildPath, paths } from "@/router/routes";

const { Text } = Typography;

/**
 * One MCP server and the tools it reports. There is no get for a tool server, so it
 * is found in the list. A `#tool-<name>` hash scrolls to and marks that tool.
 */
export function McpServerDetailsPage() {
  const theme = useTheme();
  const location = useLocation();
  const { hash } = location;
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const back = useGoBack({ to: paths.mcpServers, label: "Back to MCP servers" });
  const servers = useMcpServers();
  const server = servers.data?.find((row) => row.ref === `${namespace}/${name}`);
  const missing = Boolean(servers.data) && !server;
  const templates = useAgentTemplatesAcrossNamespaces(namespace ? [namespace] : undefined);
  const users = (templates.data?.templates ?? []).filter((template) =>
    (template.resource.spec.tools ?? []).some(
      (binding) =>
        binding.mcp?.server.name === name && (binding.mcp?.server.namespace || template.namespace) === namespace,
    ),
  );

  const anchor = safeDecode(hash.replace(/^#/, ""));
  const currentTool = anchor.startsWith(toolAnchor("")) ? anchor.slice(toolAnchor("").length) : undefined;
  useEffect(() => {
    if (server && currentTool) document.getElementById(toolAnchor(currentTool))?.scrollIntoView({ block: "center" });
  }, [server, currentTool]);

  return (
    <PageFrame
      title={name ? `MCP server ${name}` : "MCP server"}
      description={namespace && !missing ? `A tool server agents can call, in the ${namespace} namespace.` : undefined}
      actions={
        <Space size={8} wrap>
          <BackButton back={back} testId="mcp-server-back" />
          <RefreshButton onRefresh={servers.refresh} what="MCP server" loading={servers.isLoading} disabled={missing} />
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
                        .filter(
                          (binding) =>
                            binding.mcp?.server.name === name &&
                            (binding.mcp?.server.namespace || template.namespace) === namespace,
                        )
                        .flatMap((binding) => binding.mcp?.tools ?? []);
                      return names.length ? (
                        <Space size={[4, 4]} wrap>
                          {names.map((tool) => (
                            <Tag key={tool} css={{ fontFamily: theme.font.mono, marginInlineEnd: 0 }}>
                              {tool}
                            </Tag>
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
