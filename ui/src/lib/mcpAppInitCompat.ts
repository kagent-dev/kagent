/**
 * Compatibility shim for MCP Apps guest widgets that still send the base-MCP
 * `clientInfo` field in their `ui/initialize` request instead of the MCP Apps
 * `appInfo` field required by `@modelcontextprotocol/ext-apps`.
 *
 * Without this, the host's AppBridge rejects the handshake with
 * `-32603 invalid_type at params.appInfo`, never emits the `initialized`
 * acknowledgement, and therefore never sends `tool-input`/`tool-result` — so
 * such widgets hang forever on their loading state (e.g. "Waiting for data…").
 *
 * The guest posts `ui/initialize` straight to the host window (the sandbox
 * proxy `document.write`s the guest HTML into its own document, replacing its
 * relay listener), so the only host-side seam is the window `message` intake.
 * We register early — before `@mcp-ui/client` creates its transport — and
 * mutate the message in place so the ext-apps transport validates the fixed
 * payload. `MessageEvent.data` returns a stable object reference, so the
 * in-place edit is visible to listeners that run after us.
 *
 * ponytail: this only patches the one known-divergent field (`clientInfo` →
 * `appInfo`). The correct fix is conformant guest widgets; the upgrade path is
 * deleting this shim once widgets send `appInfo` themselves.
 */

interface AppIdentity {
  name: string;
  version: string;
  title?: string;
}

interface UiInitializeRequest {
  method: "ui/initialize";
  params: Record<string, unknown> & {
    appInfo?: unknown;
    clientInfo?: unknown;
  };
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isUiInitializeRequest(value: unknown): value is UiInitializeRequest {
  return (
    isPlainObject(value) &&
    value.method === "ui/initialize" &&
    isPlainObject(value.params)
  );
}

function isAppIdentity(value: unknown): value is AppIdentity {
  return (
    isPlainObject(value) &&
    typeof value.name === "string" &&
    typeof value.version === "string"
  );
}

/**
 * If `data` is a `ui/initialize` request that carries `clientInfo` but no
 * `appInfo`, derive `appInfo` from it in place. Returns true when a fix was
 * applied. Exported for unit testing.
 */
export function normalizeMcpAppInitializeMessage(data: unknown): boolean {
  if (!isUiInitializeRequest(data)) {
    return false;
  }
  const { params } = data;
  if (params.appInfo !== undefined) {
    return false;
  }
  if (!isAppIdentity(params.clientInfo)) {
    return false;
  }
  const { name, version, title } = params.clientInfo;
  params.appInfo = title ? { name, version, title } : { name, version };
  return true;
}

let installed = false;

/**
 * Idempotently install the host-side `ui/initialize` normalizer. Must run
 * before `@mcp-ui/client` attaches its transport listener so we win the
 * registration-order race at the message target; importing this from the
 * MCP App renderer module guarantees that.
 */
export function installMcpAppInitCompat(): void {
  if (installed || typeof window === "undefined") {
    return;
  }
  installed = true;
  window.addEventListener(
    "message",
    (event: MessageEvent) => {
      normalizeMcpAppInitializeMessage(event.data);
    },
    true,
  );
}
