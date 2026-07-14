"use client";

import { createContext, useContext, useEffect, useMemo, useState } from "react";
import { listMcpAppTools, type McpAppTool } from "@/app/actions/mcp-apps";
import { k8sRefUtils } from "@/lib/k8sUtils";
import { isMcpTool } from "@/lib/toolUtils";
import type { AgentResponse, Tool } from "@/types";
import type { ReactNode } from "react";

export interface ChatMcpTool {
  namespace: string;
  serverName: string;
  /**
   * groupKind of the backing tool-server CRD (e.g. "RemoteMCPServer.kagent.dev")
   * so MCP Apps calls resolve the right CRD when a RemoteMCPServer and MCPServer
   * share a namespace/name.
   */
  groupKind: string;
  toolName: string;
  uiResourceUri?: string;
  inputSchema?: unknown;
  meta?: Record<string, unknown>;
  appOnly: boolean;
  agentVisible: boolean;
}

export type ChatMcpAppTool = ChatMcpTool & { uiResourceUri: string };

type ChatMcpAppsContextValue = {
  getMcpAppForTool: (toolName: string) => ChatMcpAppTool | undefined;
  getMcpToolForAppCall: (namespace: string, serverName: string, toolName: string) => ChatMcpTool | undefined;
};

const ChatMcpAppsContext = createContext<ChatMcpAppsContextValue>({
  getMcpAppForTool: () => undefined,
  getMcpToolForAppCall: () => undefined,
});

interface ChatMcpAppsProviderProps {
  currentAgent: AgentResponse;
  children: ReactNode;
}

function isRemoteMCPServer(tool: Tool): boolean {
  const kind = tool.mcpServer?.kind || "RemoteMCPServer";
  const apiGroup = tool.mcpServer?.apiGroup || "kagent.dev";
  return kind === "RemoteMCPServer" && (apiGroup === "" || apiGroup === "kagent.dev");
}

// groupKind of the tool's backing server CRD, used to disambiguate the MCP Apps
// endpoint when a RemoteMCPServer and MCPServer share a namespace/name.
function serverGroupKind(tool: Tool): string {
  const kind = tool.mcpServer?.kind || "RemoteMCPServer";
  const apiGroup = tool.mcpServer?.apiGroup || "kagent.dev";
  return apiGroup ? `${kind}.${apiGroup}` : kind;
}

function resolveServerRef(tool: Tool, agentNamespace: string): { namespace: string; name: string } | undefined {
  const mcpServer = tool.mcpServer;
  if (!mcpServer?.name) {
    return undefined;
  }

  if (k8sRefUtils.isValidRef(mcpServer.name)) {
    return k8sRefUtils.fromRef(mcpServer.name);
  }

  return {
    namespace: mcpServer.namespace || agentNamespace,
    name: mcpServer.name,
  };
}

function isAppOnlyTool(tool: McpAppTool): boolean {
  const ui = tool._meta?.ui;
  if (!ui || typeof ui !== "object") {
    return false;
  }
  const visibility = (ui as Record<string, unknown>).visibility;
  if (typeof visibility === "string") {
    return visibility === "app";
  }
  if (!Array.isArray(visibility)) {
    return false;
  }
  // app-only means "app" is declared AND "model" is not — if both are
  // listed (e.g. ["model", "app"]) the tool is visible to the agent too.
  let hasApp = false;
  for (const v of visibility) {
    if (v === "model") {
      return false;
    }
    if (v === "app") {
      hasApp = true;
    }
  }
  return hasApp;
}

export function ChatMcpAppsProvider({ currentAgent, children }: ChatMcpAppsProviderProps) {
  const [appRegistry, setAppRegistry] = useState<Map<string, ChatMcpAppTool>>(new Map());
  const [toolRegistry, setToolRegistry] = useState<Map<string, ChatMcpTool>>(new Map());

  const configuredMcpServers = useMemo(() => {
    const agentNamespace = currentAgent.agent.metadata.namespace || "";
    const servers = new Map<string, {
      namespace: string;
      name: string;
      groupKind: string;
      selectedToolNames: Set<string>;
      selectsAllTools: boolean;
    }>();

    for (const tool of currentAgent.tools || []) {
      if (!isMcpTool(tool) || !isRemoteMCPServer(tool)) {
        continue;
      }
      const serverRef = resolveServerRef(tool, agentNamespace);
      if (!serverRef) {
        continue;
      }

      const key = `${serverRef.namespace}/${serverRef.name}`;
      const existing = servers.get(key) ?? {
        namespace: serverRef.namespace,
        name: serverRef.name,
        groupKind: serverGroupKind(tool),
        selectedToolNames: new Set<string>(),
        selectsAllTools: false,
      };

      const toolNames = tool.mcpServer.toolNames || [];
      if (toolNames.length === 0) {
        existing.selectsAllTools = true;
      } else {
        toolNames.forEach((name) => existing.selectedToolNames.add(name));
      }
      servers.set(key, existing);
    }

    return Array.from(servers.values());
  }, [currentAgent]);

  useEffect(() => {
    let cancelled = false;

    async function loadMcpApps() {
      if (configuredMcpServers.length === 0) {
        setAppRegistry(new Map());
        setToolRegistry(new Map());
        return;
      }

      const nextAppRegistry = new Map<string, ChatMcpAppTool>();
      const nextToolRegistry = new Map<string, ChatMcpTool>();
      const ambiguousToolNames = new Set<string>();

      await Promise.all(configuredMcpServers.map(async (server) => {
        const response = await listMcpAppTools(server.namespace, server.name, server.groupKind);
        if (cancelled || response.error || !response.data) {
          return;
        }

        for (const appTool of response.data) {
          const appOnly = isAppOnlyTool(appTool);
          const selectedForAgent = server.selectsAllTools || server.selectedToolNames.has(appTool.name);
          const agentVisible = selectedForAgent && !appOnly;

          const entry: ChatMcpTool = {
            namespace: server.namespace,
            serverName: server.name,
            groupKind: server.groupKind,
            toolName: appTool.name,
            uiResourceUri: appTool.uiResourceUri,
            inputSchema: appTool.inputSchema,
            meta: appTool._meta,
            appOnly,
            agentVisible,
          };

          nextToolRegistry.set(toolRegistryKey(server.namespace, server.name, appTool.name), entry);

          if (entry.uiResourceUri && entry.agentVisible) {
            const appEntry = entry as ChatMcpAppTool;
            const existing = nextAppRegistry.get(appTool.name);
            if (existing && (existing.namespace !== appEntry.namespace || existing.serverName !== appEntry.serverName || existing.uiResourceUri !== appEntry.uiResourceUri)) {
              ambiguousToolNames.add(appTool.name);
              nextAppRegistry.delete(appTool.name);
              continue;
            }
            if (!ambiguousToolNames.has(appTool.name)) {
              nextAppRegistry.set(appTool.name, appEntry);
            }
          }
        }
      }));

      if (!cancelled) {
        setAppRegistry(nextAppRegistry);
        setToolRegistry(nextToolRegistry);
      }
    }

    void loadMcpApps();
    return () => {
      cancelled = true;
    };
  }, [configuredMcpServers]);

  const value = useMemo<ChatMcpAppsContextValue>(() => ({
    getMcpAppForTool: (toolName: string) => appRegistry.get(toolName),
    getMcpToolForAppCall: (namespace: string, serverName: string, toolName: string) =>
      toolRegistry.get(toolRegistryKey(namespace, serverName, toolName)),
  }), [appRegistry, toolRegistry]);

  return (
    <ChatMcpAppsContext.Provider value={value}>
      {children}
    </ChatMcpAppsContext.Provider>
  );
}

export function useChatMcpApps() {
  return useContext(ChatMcpAppsContext);
}

function toolRegistryKey(namespace: string, serverName: string, toolName: string): string {
  return `${namespace}/${serverName}/${toolName}`;
}
