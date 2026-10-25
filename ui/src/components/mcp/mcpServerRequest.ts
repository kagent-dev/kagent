/**
 * Turning the MCP server form's fields into the request the API takes.
 *
 * Kept apart from the form component so header validation and protocol-specific
 * timeout rules can be reasoned about and unit-tested without a DOM.
 */

import { credentialHeaderIssues } from "@/components/common/credentialHeaders";
import type { Header, ToolServerCreateRequest } from "@/api";
import {
  DEFAULT_NAMESPACE,
  RESOURCE_NAME_HINT,
  isValidResourceName,
  slugifyResourceName,
} from "@/components/common/resourceName";

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

export interface McpServerFormValues {
  name: string;
  namespace: string;
  url: string;
  // Selects HTTPS or plaintext HTTP for the scheme-less URL.
  tlsEnabled: boolean;
  streamableHttp: boolean;
  credentialHeaders: Header[];
  timeout: string;
  sseReadTimeout: string;
  terminateOnClose: boolean;
}

export function emptyMcpServerForm(): McpServerFormValues {
  return {
    name: "",
    namespace: DEFAULT_NAMESPACE,
    url: "",
    tlsEnabled: false,
    // Matches the CRD's own default, and the only protocol Codex accepts.
    streamableHttp: true,
    credentialHeaders: [],
    timeout: "5s",
    sseReadTimeout: "300s",
    terminateOnClose: true,
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
  for (const message of credentialHeaderIssues(values.credentialHeaders)) { issues.push({ field: "credentialHeaders", message }); }
  return issues;
}

/** Maps validated form values onto the API's create request. */
export function toCreateRequest(
  values: McpServerFormValues,
): ToolServerCreateRequest {
  const name = values.name.trim();
  const namespace = values.namespace.trim() || DEFAULT_NAMESPACE;

  // The scheme comes from the TLS toggle, not the field, so it is added back
  // here onto the scheme-less host the form holds.
  const url = schemeFor(values.tlsEnabled) + stripScheme(values.url);

  return {
    type: "RemoteMCPServer",
    remoteMCPServer: {
      metadata: { name, namespace },
      spec: {
        description: "",
        protocol: values.streamableHttp ? "STREAMABLE_HTTP" : "SSE",
        url,
        ...(values.credentialHeaders.length ? { headers: structuredClone(values.credentialHeaders) } : {}),
        timeout: values.timeout.trim() || undefined,
        // Only meaningful for SSE; sending it for streamable HTTP would
        // describe a timeout that never applies.
        sseReadTimeout: values.streamableHttp
          ? undefined
          : values.sseReadTimeout.trim() || undefined,
        terminateOnClose: values.terminateOnClose,
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
  return slugifyResourceName(hostnameOf(values.url));
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
