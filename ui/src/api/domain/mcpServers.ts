/**
 * MCP / tool server domain models.
 *
 * Two kinds share one list endpoint: `RemoteMCPServer` (an external URL the
 * controller connects out to) and `MCPServer` (a stdio server the controller
 * runs as a Deployment).
 */

import type { ResourceMetadata, TLSConfig, ValueRef } from "./common";
import type { SecretMaterial } from "./models";

export type RemoteMCPServerProtocol = "SSE" | "STREAMABLE_HTTP";

export interface RemoteMCPServerSpec {
  description: string;
  protocol: RemoteMCPServerProtocol;
  url: string;
  headersFrom: ValueRef[];
  timeout?: string;
  sseReadTimeout?: string;
  terminateOnClose?: boolean;
  tls?: TLSConfig;
}

export interface RemoteMCPServer {
  metadata: ResourceMetadata;
  spec: RemoteMCPServerSpec;
}

export interface MCPServerDeployment {
  image: string;
  port: number;
  cmd?: string;
  args?: string[];
  env?: Record<string, string>;
}

export type TransportType = "stdio";

export interface MCPServerSpec {
  deployment: MCPServerDeployment;
  transportType: TransportType;
  stdioTransport: Record<string, never>;
}

export interface MCPServer {
  metadata: ResourceMetadata;
  spec: MCPServerSpec;
}

/** A tool the controller discovered by handshaking with the server. */
export interface DiscoveredTool {
  name: string;
  description: string;
}

/** One row of `ToolService.ListToolServers`, for either server kind. */
export interface ToolServerResponse {
  /** `namespace/name`. */
  ref: string;
  /** e.g. `RemoteMCPServer.kagent.dev`. */
  groupKind: string;
  discoveredTools: DiscoveredTool[];
}

export type ToolServer = RemoteMCPServer | MCPServer;

export interface ToolServerCreateRequest {
  type: "RemoteMCPServer" | "MCPServer";
  remoteMCPServer?: RemoteMCPServer;
  mcpServer?: MCPServer;
  /** Secrets materialised alongside the server so K8s GC cleans them up on delete. */
  secrets?: SecretMaterial[];
}

/**
 * Flat tool row from `ToolService.ListTools`, across every server.
 *
 * Snake-cased because these are `database.Tool`'s own JSON names, carried through
 * the `StructuredObject` envelope verbatim rather than restated by a handler —
 * so renaming them here would be renaming them away from what arrives.
 *
 * `deleted_at` is `omitempty` on the Go struct, so a live tool has no such field
 * at all.
 */
export interface ToolsResponse {
  id: string;
  server_name: string;
  created_at: string;
  updated_at: string;
  deleted_at?: string;
  description: string;
  group_kind: string;
}
