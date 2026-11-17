import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { themeFor } from "@/theme/theme";
import { McpServerNewPage } from "./McpServerNewPage";

const mocks = vi.hoisted(() => ({ create: vi.fn(), success: vi.fn() }));

vi.mock("react-hot-toast", () => ({ default: { success: mocks.success } }));
vi.mock("@/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/api")>()),
  apiClient: { mcpServers: { create: mocks.create } },
  useInvalidateMcpServers: () => () => Promise.resolve(),
  useNamespaces: () => ({ data: [{ name: "kagent" }, { name: "team-a" }], isLoading: false, error: undefined }),
}));

function renderPage() {
  const router = createMemoryRouter(
    [
      { path: "/mcp", element: <p>Server list</p> },
      { path: "/mcp/new", element: <McpServerNewPage /> },
    ],
    { initialEntries: ["/mcp/new"] },
  );
  render(
    <ThemeProvider theme={themeFor("dark")}>
      <ConfigProvider theme={{ token: { motion: false } }}>
        <RouterProvider router={router} />
      </ConfigProvider>
    </ThemeProvider>,
  );
  return router;
}

const type = (testId: string, value: string) =>
  fireEvent.change(screen.getByTestId(testId), { target: { value } });

beforeEach(() => vi.clearAllMocks());

describe("MCP server create form", () => {
  it("shows a header back link and marks every field required or optional", () => {
    renderPage();
    expect(screen.getByTestId("form-back-link")).toHaveTextContent("MCP servers");
    // Name, Namespace, Server URL.
    expect(screen.getAllByTestId("field-required")).toHaveLength(3);
    // TLS, headers, connection timeout; the SSE timeout is hidden on streamable HTTP.
    expect(screen.getAllByTestId("field-optional")).toHaveLength(3);
  });

  it("picks the namespace from a dropdown", () => {
    renderPage();
    expect(screen.getByTestId("mcp-namespace")).toHaveClass("ant-select");
  });

  it("flags the empty fields on submit and sends nothing", () => {
    renderPage();
    fireEvent.click(screen.getByTestId("mcp-submit"));
    expect(screen.getByText("A server name is required.")).toBeInTheDocument();
    expect(screen.getByText("A server URL is required.")).toBeInTheDocument();
    expect(mocks.create).not.toHaveBeenCalled();
  });

  it("flags headers that are not a JSON object", () => {
    renderPage();
    type("mcp-name", "tools");
    type("mcp-url", "mcp.example.com/mcp");
    type("mcp-headers", "not json");
    fireEvent.click(screen.getByTestId("mcp-submit"));
    expect(screen.getByText(/Headers must be a JSON object/)).toBeInTheDocument();
  });

  it("explains a failed create above the buttons", async () => {
    mocks.create.mockRejectedValue(new Error("boom"));
    renderPage();
    type("mcp-name", "tools");
    type("mcp-url", "mcp.example.com/mcp");
    fireEvent.click(screen.getByTestId("mcp-submit"));
    expect(await screen.findByTestId("mcp-submit-error")).toHaveTextContent("The MCP server was not created");
    expect(mocks.success).not.toHaveBeenCalled();
  });

  it("creates the server, shows the toast and lands on the list", async () => {
    mocks.create.mockResolvedValue(undefined);
    renderPage();
    type("mcp-name", "tools");
    type("mcp-url", "mcp.example.com/mcp");
    fireEvent.click(screen.getByTestId("mcp-submit"));
    expect(await screen.findByText("Server list")).toBeInTheDocument();
    expect(mocks.create).toHaveBeenCalledWith(
      expect.objectContaining({
        remoteMCPServer: expect.objectContaining({
          metadata: { name: "tools", namespace: "kagent" },
          spec: expect.objectContaining({ url: "http://mcp.example.com/mcp", protocol: "STREAMABLE_HTTP" }),
        }),
      }),
    );
    expect(mocks.success).toHaveBeenCalledWith("Created MCP server tools");
  });

  it("leaves a clean form without asking", async () => {
    renderPage();
    fireEvent.click(screen.getByTestId("mcp-cancel"));
    expect(await screen.findByText("Server list")).toBeInTheDocument();
  });

  it("asks before a dirty form is left, and Keep editing stays", async () => {
    const router = renderPage();
    type("mcp-name", "tools");
    fireEvent.click(screen.getByTestId("mcp-cancel"));
    expect(await screen.findByTestId("mcp-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe("/mcp/new");
  });
});
