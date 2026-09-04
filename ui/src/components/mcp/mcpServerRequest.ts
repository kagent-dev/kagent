/**
 * Turning the MCP server form's fields into the request the API takes.
 *
 * Kept apart from the form component so the mapping — which is where the fiddly
 * rules live (arg ordering, header parsing, which timeout applies to which
 * protocol) — can be reasoned about and unit-tested without a DOM.
 */

import type {
  SecretMaterial,
  TLSConfig,
  ToolServerCreateRequest,
  ValueRef,
} from "@/api";
import {
  DEFAULT_NAMESPACE,
  RESOURCE_NAME_HINT,
  isValidResourceName,
  slugifyResourceName,
} from "@/components/common/resourceName";

/** Which kind of server the form is describing. */
export type McpServerKind = "url" | "command";

/** The Secret key a materialised CA bundle is stored under. */
export const CA_CERT_SECRET_KEY = "ca.crt";

/** The Secret a custom CA is written to, named off the server so a delete GCs it. */
export const caCertSecretNameFor = (serverName: string): string =>
  `${serverName.trim()}-ca`;

/** The scheme the stored (scheme-less) URL is served under, per the TLS toggle. */
export const schemeFor = (tlsEnabled: boolean): string =>
  tlsEnabled ? "https://" : "http://";

/**
 * Drops a leading `http://` or `https://` from an operator-typed URL.
 *
 * The form stores the URL without a scheme — the scheme is a fixed prefix driven
 * by the TLS toggle — so a scheme pasted into the field is stripped rather than
 * doubled onto the one the toggle adds back at submit.
 */
export const stripScheme = (raw: string): string =>
  raw.trim().replace(/^https?:\/\//i, "");

/** Whether a string parses as an absolute URL. */
function isParsableUrl(url: string): boolean {
  try {
    new URL(url);
    return true;
  } catch {
    return false;
  }
}

/** The executors the command form offers, and the image each one runs in. */
export const COMMAND_EXECUTORS = {
  npx: { label: "npx", image: "node:24-alpine3.21" },
  uvx: { label: "uvx", image: "ghcr.io/astral-sh/uv:debian" },
} as const;

export type CommandExecutor = keyof typeof COMMAND_EXECUTORS;

/** A single environment variable row in the command form. */
export interface EnvRow {
  id: string;
  name: string;
  value: string;
}

export interface McpServerFormValues {
  kind: McpServerKind;
  name: string;
  namespace: string;

  // URL kind.
  url: string;
  // Whether the controller dials the upstream over TLS. On → https, and the
  // optional CA field below applies; off → plaintext http. The URL is stored
  // scheme-less (the scheme is shown as a fixed prefix driven by this flag), so
  // the two together are what the submitted `spec.url` and `spec.tls` come from.
  tlsEnabled: boolean;
  // A PEM CA bundle to verify the upstream against a private CA. Only meaningful
  // when `tlsEnabled`; empty means the system trust store.
  caCertPem: string;
  streamableHttp: boolean;
  headersJson: string;
  timeout: string;
  sseReadTimeout: string;
  terminateOnClose: boolean;

  // Command kind.
  executor: CommandExecutor;
  commandPrefix: string;
  packageName: string;
  args: string[];
  env: EnvRow[];
}

export function emptyMcpServerForm(): McpServerFormValues {
  return {
    kind: "url",
    name: "",
    namespace: DEFAULT_NAMESPACE,
    url: "",
    tlsEnabled: false,
    caCertPem: "",
    streamableHttp: false,
    headersJson: "",
    timeout: "5s",
    sseReadTimeout: "300s",
    terminateOnClose: true,
    executor: "npx",
    commandPrefix: "",
    packageName: "",
    args: [],
    env: [],
  };
}

/** A field-level complaint, addressed to the input that caused it. */
export interface ValidationIssue {
  field: keyof McpServerFormValues;
  message: string;
}

/**
 * Everything wrong with the form, not just the first thing.
 *
 * Returning the whole set lets the form mark every bad field at once rather than
 * making the user resubmit to discover the next problem.
 */
export function validateMcpServerForm(
  values: McpServerFormValues,
): ValidationIssue[] {
  const issues: ValidationIssue[] = [];

  if (!values.name.trim()) {
    issues.push({ field: "name", message: "A server name is required." });
  } else if (!isValidResourceName(values.name)) {
    issues.push({ field: "name", message: RESOURCE_NAME_HINT });
  }

  if (values.namespace.trim() && !isValidResourceName(values.namespace)) {
    issues.push({
      field: "namespace",
      message: "Namespaces follow the same rules as names.",
    });
  }

  if (values.kind === "url") {
    // The scheme lives on the TLS toggle, so the field holds only the host and
    // path; validate the whole thing the toggle would produce.
    const host = stripScheme(values.url);
    if (!host) {
      issues.push({ field: "url", message: "A server URL is required." });
    } else if (!isParsableUrl(schemeFor(values.tlsEnabled) + host)) {
      issues.push({
        field: "url",
        message: "Enter a valid host and path, for example api.example.com/mcp.",
      });
    }
    if (values.headersJson.trim() && parseHeaders(values.headersJson) === undefined) {
      issues.push({
        field: "headersJson",
        message: "Headers must be a JSON object, for example {\"Authorization\": \"…\"}.",
      });
    }
  } else if (!values.packageName.trim()) {
    issues.push({ field: "packageName", message: "A package name is required." });
  }

  return issues;
}

/** Parses the headers box into `ValueRef`s, or `undefined` if it isn't valid. */
export function parseHeaders(json: string): ValueRef[] | undefined {
  const text = json.trim();
  if (!text) return [];
  try {
    const parsed: unknown = JSON.parse(text);
    if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
      return undefined;
    }
    return Object.entries(parsed as Record<string, unknown>).map(([name, value]) => ({
      name,
      value: String(value),
    }));
  } catch {
    return undefined;
  }
}

/**
 * The command line a stdio server will be started with.
 *
 * The package goes last, after any prefix words and explicit arguments — that is
 * the order `npx`/`uvx` expect, and getting it wrong is the sort of thing that
 * fails only once the pod is running.
 */
export function buildCommandArgs(values: McpServerFormValues): string[] {
  const prefix = values.commandPrefix.trim().split(/\s+/).filter(Boolean);
  const args = values.args.map((arg) => arg.trim()).filter(Boolean);
  return [...prefix, ...args, values.packageName.trim()];
}

/** The whole command, for the form's preview line. */
export function previewCommand(values: McpServerFormValues): string {
  if (!values.packageName.trim()) return "";
  return [values.executor, ...buildCommandArgs(values)].join(" ");
}

/** Maps validated form values onto the API's create request. */
export function toCreateRequest(
  values: McpServerFormValues,
): ToolServerCreateRequest {
  const name = values.name.trim();
  const namespace = values.namespace.trim() || DEFAULT_NAMESPACE;

  if (values.kind === "url") {
    // The scheme comes from the TLS toggle, not the field, so it is added back
    // here onto the scheme-less host the form holds.
    const url = schemeFor(values.tlsEnabled) + stripScheme(values.url);

    // A CA bundle only matters over TLS. With one, `spec.tls` points at a Secret
    // materialised alongside the server (so a delete GCs it); without one but
    // still over TLS, an empty `spec.tls` selects the system trust store; over
    // plain HTTP there is no `spec.tls` at all.
    const customCA = values.tlsEnabled && values.caCertPem.trim() !== "";
    const caCertSecretName = customCA ? caCertSecretNameFor(name) : "";
    const tls: TLSConfig | undefined = values.tlsEnabled
      ? customCA
        ? {
            caCertSecretRef: caCertSecretName,
            caCertSecretKey: CA_CERT_SECRET_KEY,
          }
        : {}
      : undefined;
    const secrets: SecretMaterial[] | undefined = customCA
      ? [
          {
            name: caCertSecretName,
            key: CA_CERT_SECRET_KEY,
            value: values.caCertPem,
          },
        ]
      : undefined;

    return {
      type: "RemoteMCPServer",
      remoteMCPServer: {
        metadata: { name, namespace },
        spec: {
          description: "",
          protocol: values.streamableHttp ? "STREAMABLE_HTTP" : "SSE",
          url,
          headersFrom: parseHeaders(values.headersJson) ?? [],
          timeout: values.timeout.trim() || undefined,
          // Only meaningful for SSE; sending it for streamable HTTP would
          // describe a timeout that never applies.
          sseReadTimeout: values.streamableHttp
            ? undefined
            : values.sseReadTimeout.trim() || undefined,
          terminateOnClose: values.terminateOnClose,
          ...(tls !== undefined ? { tls } : {}),
        },
      },
      ...(secrets ? { secrets } : {}),
    };
  }

  const env: Record<string, string> = {};
  for (const row of values.env) {
    const key = row.name.trim();
    if (key) env[key] = row.value;
  }

  return {
    type: "MCPServer",
    mcpServer: {
      metadata: { name, namespace },
      spec: {
        deployment: {
          image: COMMAND_EXECUTORS[values.executor].image,
          port: 3000,
          cmd: values.executor,
          args: buildCommandArgs(values),
          ...(Object.keys(env).length > 0 ? { env } : {}),
        },
        transportType: "stdio",
        stdioTransport: {},
      },
    },
  };
}

/**
 * A server name guessed from whatever identifies it.
 *
 * Saves typing a name that is almost always derivable, while staying a plain
 * suggestion the form stops offering once the user types their own. Empty when
 * there is nothing to derive from: a guessed name in a field the reader has not
 * reached yet reads as a value they chose, and the placeholder says more.
 */
export function suggestName(values: McpServerFormValues): string {
  const source =
    values.kind === "url" ? hostnameOf(values.url) : lastSegment(values.packageName);
  return slugifyResourceName(source);
}

function hostnameOf(url: string): string {
  const host = stripScheme(url);
  if (!host) return "";
  try {
    // The field is scheme-less, so a scheme is added to make it parseable —
    // which one does not matter, only the hostname is read back out.
    return new URL(`http://${host}`).hostname;
  } catch {
    return "";
  }
}

/** `@scope/pkg@1.2.3` → `pkg`. Scope and version are noise in a resource name. */
function lastSegment(packageName: string): string {
  const withoutVersion = packageName.trim().replace(/@[^@/]*$/, "");
  const parts = withoutVersion.split("/").filter(Boolean);
  return parts[parts.length - 1] ?? "";
}
