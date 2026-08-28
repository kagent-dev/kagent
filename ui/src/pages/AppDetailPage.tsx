import { Alert, Button, Card, Descriptions, Space, Tag, Typography } from "antd";
import { useTheme } from "@emotion/react";
import { ArrowLeft } from "lucide-react";
import { Link, useParams, useSearchParams } from "react-router-dom";
import { PageFrame } from "@/components/Structure/PageFrame";
import { ToolServerTools } from "@/components/mcp/ToolServerTools";
import { paths } from "@/router/routes";
import { parseRef, useMcpServers, type DiscoveredTool, type ToolServerResponse } from "@/api";

const { Text } = Typography;

/** A tool found on a server — the pair is what identifies an app. */
interface AppMatch {
  server: ToolServerResponse;
  tool: DiscoveredTool;
}

/**
 * Every server exposing a tool of this name.
 *
 * A name alone is not a unique key — two servers may each expose `query` — so
 * this returns all of them and the page decides. `?ns=` and `?server=` narrow it
 * to one, which is how the MCP list links here.
 */
function findApp(
  servers: ToolServerResponse[],
  appName: string,
  namespace: string,
  serverName: string,
): AppMatch[] {
  const wanted = namespace && serverName ? `${namespace}/${serverName}` : undefined;

  return servers.flatMap((server) => {
    if (wanted !== undefined && server.ref !== wanted) return [];
    const tool = server.discoveredTools.find((candidate) => candidate.name === appName);
    return tool ? [{ server, tool }] : [];
  });
}

/**
 * One MCP app — a single tool on one MCP server.
 *
 * **What this page used to do, and why it does less.** The previous UI resolved
 * the app through two Next.js server actions: one listed a server's *MCP-UI*
 * tools (those carrying a `uiResourceUri`) with their input schemas, the other
 * invoked a tool and returned its `CallToolResult`, which a renderer then drew
 * as an interactive app. This application is a static single-page app with no
 * server to run actions on, and its data layer exposes neither of those calls —
 * the tool listing it does have (`useMcpServers`) reports a name and a
 * description per tool and nothing else. Reproducing the argument form and the
 * Run button on top of that would mean inventing an endpoint and a schema, and
 * would fail the moment it was pointed at a real cluster.
 *
 * So this identifies the app, shows everything actually known about it, and says
 * outright which capability is missing rather than staging a control that cannot
 * work.
 */
export function AppDetailPage() {
  const theme = useTheme();
  const { appName = "" } = useParams();
  const [searchParams] = useSearchParams();

  // The old route carried the server in the query string; keep the same link
  // shape so existing links and bookmarks still resolve.
  const namespace = searchParams.get("ns") ?? "";
  const serverName = searchParams.get("server") ?? "";

  const servers = useMcpServers();
  const matches = servers.error
    ? []
    : findApp(servers.data ?? [], appName, namespace, serverName);
  const match = matches.length === 1 ? matches[0] : undefined;

  return (
    <PageFrame
      title={appName || "App"}
      description={match ? `Tool on ${match.server.ref}` : "MCP app detail."}
      actions={
        <Button onClick={() => void servers.refresh()} loading={servers.isValidating}>
          Refresh
        </Button>
      }
    >
      <Space orientation="vertical" size="middle" css={{ display: "flex" }}>
        <Link
          to={paths.mcpServers}
          css={{
            display: "inline-flex",
            alignItems: "center",
            gap: theme.space(2),
            color: theme.color.textMuted,
          }}
        >
          <ArrowLeft size={16} aria-hidden />
          Back to MCP servers
        </Link>

        {servers.error ? (
          <Alert
            type="error"
            showIcon
            title="Could not load MCP servers"
            description={servers.error.message}
            data-testid="app-detail-error"
            action={
              <Button size="small" onClick={() => void servers.refresh()}>
                Try again
              </Button>
            }
          />
        ) : null}

        {!appName ? (
          <Alert
            type="warning"
            showIcon
            title="Missing app context"
            description="This page needs an app name. Open an app from the MCP servers list."
            data-testid="app-detail-missing-context"
          />
        ) : null}

        {/* Absence is only meaningful once the read finished and succeeded. */}
        {appName && !servers.error && !servers.isLoading && matches.length === 0 ? (
          <Alert
            type="warning"
            showIcon
            title="No such app"
            description={
              namespace && serverName
                ? `No tool named "${appName}" is reported by ${namespace}/${serverName}.`
                : `No registered MCP server reports a tool named "${appName}".`
            }
            data-testid="app-detail-not-found"
          />
        ) : null}

        {matches.length > 1 ? (
          <Alert
            type="info"
            showIcon
            title="More than one server reports this app"
            data-testid="app-detail-ambiguous"
            description={
              <div>
                <Text css={{ display: "block", marginBottom: theme.space(2) }}>
                  A tool name is not unique across servers. Pick the one you meant:
                </Text>
                <ul css={{ margin: 0, paddingLeft: theme.space(5) }}>
                  {matches.map(({ server }) => {
                    const ref = parseRef(server.ref);
                    return (
                      <li key={server.ref}>
                        <Link
                          to={`${paths.appDetail.replace(":appName", encodeURIComponent(appName))}?ns=${encodeURIComponent(ref.namespace)}&server=${encodeURIComponent(ref.name)}`}
                        >
                          {server.ref}
                        </Link>
                      </li>
                    );
                  })}
                </ul>
              </div>
            }
          />
        ) : null}

        {match ? (
          <>
            <Card title="App" data-testid="app-detail-card" loading={servers.isLoading}>
              <Descriptions
                column={1}
                size="small"
                items={[
                  { key: "name", label: "Name", children: match.tool.name },
                  {
                    key: "description",
                    label: "Description",
                    children: match.tool.description || "—",
                  },
                  { key: "server", label: "Server", children: match.server.ref },
                  {
                    key: "kind",
                    label: "Server kind",
                    children: <Tag>{match.server.groupKind.split(".")[0]}</Tag>,
                  },
                ]}
              />
            </Card>

            <Alert
              type="info"
              showIcon
              title="Running this app from the UI is not available"
              description="Invoking a tool and rendering its interactive result needs an MCP app endpoint this UI's data layer does not expose. Until it does, this page describes the app but cannot run it."
              data-testid="app-detail-invoke-note"
            />

            <Card
              title={`Other tools on ${match.server.ref}`}
              data-testid="app-detail-siblings"
            >
              <ToolServerTools
                tools={match.server.discoveredTools.filter(
                  (tool) => tool.name !== match.tool.name,
                )}
              />
            </Card>
          </>
        ) : null}
      </Space>
    </PageFrame>
  );
}
