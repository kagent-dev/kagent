/**
 * MCP / tool server domain models.
 *
 * Kagent agents bind RemoteMCPServer resources that describe HTTP MCP endpoints.
 */

import type { ResourceMetadata, TLSConfig } from "./common";
import type { Header } from "./headers";
import type { SecretMaterial } from "./models";

export type RemoteMCPServerProtocol = "SSE" | "STREAMABLE_HTTP";

export interface RemoteMCPServerSpec {
  description: string;
  protocol: RemoteMCPServerProtocol;
  url: string;
  headers?: Header[];
  timeout?: string;
  sseReadTimeout?: string;
  terminateOnClose?: boolean;
  tls?: TLSConfig;
}

export interface RemoteMCPServer {
  metadata: ResourceMetadata;
  spec: RemoteMCPServerSpec;
}

/** A tool the controller discovered by handshaking with the server. */
export interface DiscoveredTool {
  name: string;
  description: string;
}

/** One row of `ToolService.ListToolServers`. */
export interface ToolServerResponse {
  /** `namespace/name`. */
  ref: string;
  /** e.g. `RemoteMCPServer.api.kagent.dev`. */
  groupKind: string;
  discoveredTools: DiscoveredTool[];
}

export interface ToolServerCreateRequest {
  type: "RemoteMCPServer";
  remoteMCPServer: RemoteMCPServer;
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
