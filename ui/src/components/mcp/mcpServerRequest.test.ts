import { describe, expect, it } from "vitest";
import {
  emptyMcpServerForm,
  suggestName,
  toCreateRequest,
  validateMcpServerForm,
  type McpServerFormValues,
} from "./mcpServerRequest";

/** A URL-kind form with a name and host filled in, plus any overrides. */
function urlForm(overrides: Partial<McpServerFormValues> = {}): McpServerFormValues {
  return {
    ...emptyMcpServerForm(),
    kind: "url",
    name: "my-server",
    namespace: "kagent",
    url: "mcp.example.com/sse",
    ...overrides,
  };
}

describe("toCreateRequest — TLS mapping", () => {
  it("HTTP: prefixes http:// and sends no spec.tls or secrets", () => {
    const req = toCreateRequest(urlForm({ tlsEnabled: false }));
    expect(req.remoteMCPServer?.spec.url).toBe("http://mcp.example.com/sse");
    expect(req.remoteMCPServer?.spec.tls).toBeUndefined();
    expect(req.secrets).toBeUndefined();
  });

  it("HTTPS without a CA: prefixes https:// and sends an empty spec.tls (system trust)", () => {
    const req = toCreateRequest(urlForm({ tlsEnabled: true, caCertPem: "" }));
    expect(req.remoteMCPServer?.spec.url).toBe("https://mcp.example.com/sse");
    expect(req.remoteMCPServer?.spec.tls).toEqual({});
    expect(req.secrets).toBeUndefined();
  });

  it("HTTPS with a CA: references a materialised secret and ships its contents", () => {
    const pem = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n";
    const req = toCreateRequest(urlForm({ tlsEnabled: true, caCertPem: pem }));
    expect(req.remoteMCPServer?.spec.tls).toEqual({
      caCertSecretRef: "my-server-ca",
      caCertSecretKey: "ca.crt",
    });
    expect(req.secrets).toEqual([
      { name: "my-server-ca", key: "ca.crt", value: pem },
    ]);
  });

  it("ignores a CA bundle left over when TLS is off", () => {
    const req = toCreateRequest(
      urlForm({ tlsEnabled: false, caCertPem: "-----BEGIN CERTIFICATE-----" }),
    );
    expect(req.remoteMCPServer?.spec.tls).toBeUndefined();
    expect(req.secrets).toBeUndefined();
  });

  it("strips a scheme the operator left on the host so it is not doubled", () => {
    const req = toCreateRequest(
      urlForm({ url: "https://mcp.example.com/sse", tlsEnabled: true }),
    );
    expect(req.remoteMCPServer?.spec.url).toBe("https://mcp.example.com/sse");
  });
});

describe("validateMcpServerForm — scheme-less URL", () => {
  it("accepts a bare host now that the scheme lives on the toggle", () => {
    const issues = validateMcpServerForm(urlForm({ url: "mcp.example.com/sse" }));
    expect(issues.find((i) => i.field === "url")).toBeUndefined();
  });

  it("still requires a host", () => {
    const issues = validateMcpServerForm(urlForm({ url: "" }));
    expect(issues.find((i) => i.field === "url")).toBeDefined();
  });
});

describe("suggestName", () => {
  it("derives a name from the host or the package", () => {
    expect(suggestName(urlForm({ name: "", url: "mcp.example.com/sse" }))).toBe(
      "mcp-example-com",
    );
    expect(
      suggestName({
        ...emptyMcpServerForm(),
        kind: "command",
        packageName: "@modelcontextprotocol/server-filesystem@1.2.3",
      }),
    ).toBe("server-filesystem");
  });

  it("suggests nothing when there is nothing to derive from", () => {
    // Otherwise an unrelated edit — switching kind, typing a namespace — writes
    // an invented name into a field the reader has not filled in yet.
    expect(suggestName(emptyMcpServerForm())).toBe("");
    expect(suggestName({ ...emptyMcpServerForm(), kind: "command" })).toBe("");
  });
});
