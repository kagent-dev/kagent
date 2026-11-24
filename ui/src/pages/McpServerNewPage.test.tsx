import { ThemeProvider } from "@emotion/react";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { RouterProvider, createMemoryRouter } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { pauseTyping, useTypingTimers } from "@/components/form/typingPause.testing";
import { paths } from "@/router/routes";
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
      { path: paths.mcpServers, element: <p>Server list</p> },
      { path: paths.mcpServerNew, element: <McpServerNewPage /> },
    ],
    { initialEntries: [paths.mcpServerNew] },
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

// An input event, as typing sends: it also marks the field touched.
const type = (testId: string, value: string) =>
  fireEvent.input(screen.getByTestId(testId), { target: { value } });

beforeEach(() => vi.clearAllMocks());

afterEach(() => vi.useRealTimers());

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

  it("holds Create back until the required fields are filled, and flags a field once left", () => {
    renderPage();
    const submit = screen.getByTestId("mcp-submit");
    expect(submit).toHaveAttribute("aria-disabled", "true");
    fireEvent.click(submit);
    expect(mocks.create).not.toHaveBeenCalled();
    expect(screen.queryByText("A server name is required.")).not.toBeInTheDocument();

    fireEvent.blur(screen.getByTestId("mcp-url"));
    expect(screen.getByText("A server URL is required.")).toBeInTheDocument();
    expect(screen.queryByText("A server name is required.")).not.toBeInTheDocument();

    type("mcp-name", "tools");
    type("mcp-url", "mcp.example.com/mcp");
    expect(submit).not.toHaveAttribute("aria-disabled");
  });

  it("flags headers that are not a JSON object as they are typed", () => {
    useTypingTimers();
    renderPage();
    type("mcp-name", "tools");
    type("mcp-url", "mcp.example.com/mcp");
    type("mcp-headers", "not json");
    pauseTyping();
    expect(screen.getByText(/Headers must be a JSON object/)).toBeInTheDocument();
    expect(screen.getByTestId("mcp-submit")).toHaveAttribute("aria-disabled", "true");
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

  it("guards the form while the create is pending, then lands on the list", async () => {
    let finish!: (value?: unknown) => void;
    mocks.create.mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    const router = renderPage();
    type("mcp-name", "tools");
    type("mcp-url", "mcp.example.com/mcp");
    fireEvent.click(screen.getByTestId("mcp-submit"));
    await waitFor(() => expect(mocks.create).toHaveBeenCalled());

    fireEvent.click(screen.getByTestId("form-back-link"));
    expect(await screen.findByTestId("mcp-discard-body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Keep editing" }));
    expect(router.state.location.pathname).toBe(paths.mcpServerNew);

    finish();
    expect(await screen.findByText("Server list")).toBeInTheDocument();
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
    expect(router.state.location.pathname).toBe(paths.mcpServerNew);
  });
});
