import { describe, expect, it } from "vitest";
import {
  emptyMcpServerForm,
  suggestName,
  toCreateRequest,
  validateMcpServerForm,
  type McpServerFormValues,
} from "./mcpServerRequest";

/** A form with a name and host filled in, plus any overrides. */
function urlForm(overrides: Partial<McpServerFormValues> = {}): McpServerFormValues {
  return {
    ...emptyMcpServerForm(),
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

  it("HTTPS: prefixes https:// and uses system trust", () => {
    const req = toCreateRequest(urlForm({ tlsEnabled: true }));
    expect(req.remoteMCPServer?.spec.url).toBe("https://mcp.example.com/sse");
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

  it.each(["api.example.com/mcp", "host:8080/sse", "https://x.test/mcp", "10.0.0.5:3000", "[::1]:8080/mcp", "[2001:db8::1]/mcp", "user:pw@api.example.com/mcp", "münchen.de/mcp", "host_name/mcp"])(
    "accepts %s",
    (url) => {
      expect(validateMcpServerForm(urlForm({ url })).find((i) => i.field === "url")).toBeUndefined();
    },
  );

  it.each(["not a url", "api.example.com/m cp", "/mcp", ":8080/mcp", "https://bad host/mcp", "host:port/mcp", "user name@api.example.com/mcp", "münchen .de/mcp"])(
    "rejects %s",
    (url) => {
      expect(validateMcpServerForm(urlForm({ url })).find((i) => i.field === "url")?.message).toBe(
        "Enter a valid host and path, for example api.example.com/mcp.",
      );
    },
  );

  it("still requires a host", () => {
    const issues = validateMcpServerForm(urlForm({ url: "" }));
    expect(issues.find((i) => i.field === "url")).toBeDefined();
  });
});

describe("validateMcpServerForm — namespace", () => {
  it("rejects a typed namespace the cluster would refuse", () => {
    const issues = validateMcpServerForm(urlForm({ namespace: "Team A" }));
    expect(issues.find((i) => i.field === "namespace")?.message).toBe("The namespace must be a valid Kubernetes name.");
    expect(validateMcpServerForm(urlForm({ namespace: "team-a" }))).toEqual([]);
  });
});

describe("suggestName", () => {
  it("derives a name from the host", () => {
    expect(suggestName(urlForm({ name: "", url: "mcp.example.com/sse" }))).toBe(
      "mcp-example-com",
    );
  });

  it("leaves userinfo out of the name", () => {
    expect(suggestName(urlForm({ name: "", url: "user:pw@api.example.com/mcp" }))).toBe(
      "api-example-com",
    );
  });

  it("derives a valid name from an internationalised host", () => {
    expect(suggestName(urlForm({ name: "", url: "münchen.de/mcp" }))).toMatch(/^[a-z0-9]([-a-z0-9]*[a-z0-9])?$/);
  });

  it("suggests nothing for a host that is not valid", () => {
    expect(suggestName(urlForm({ name: "", url: "not a url" }))).toBe("");
  });

  it("suggests nothing when there is nothing to derive from", () => {
    // Otherwise an unrelated edit, such as typing a namespace, writes an invented
    // name into a field the reader has not filled in yet.
    expect(suggestName(emptyMcpServerForm())).toBe("");
  });
});
