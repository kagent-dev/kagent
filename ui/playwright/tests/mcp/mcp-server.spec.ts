import { test, expect } from "../../fixtures/test";
import { loadPage, expectToast } from "../../helpers/page";

// Sub-stage 2.4 — create an MCP (RemoteMCPServer) tool server. /mcp/new blocks
// on GET /api/toolservertypes before rendering the form. Create POSTs
// /api/toolservers (captured by the stub).

const SERVER_NAME = "e2e-mcp";
const SERVER_URL = "https://example.com/mcp";

test.describe("mcp servers & tools", () => {
  test("creates a remote MCP server and POSTs the expected payload", async ({ page, mock }) => {
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByLabel("Server Name").fill(SERVER_NAME);
    await page.locator("#url").fill(SERVER_URL);

    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page).toHaveURL(/\/mcp(\?|$)/);

    const req = await mock.lastRequest<{
      type: string;
      remoteMCPServer: { metadata: { name: string }; spec: { url: string; protocol: string } };
    }>("POST", "/api/toolservers");
    expect(req, "expected a captured POST /api/toolservers").not.toBeNull();
    expect(req!.body.type).toBe("RemoteMCPServer");
    expect(req!.body.remoteMCPServer.spec.url).toBe(SERVER_URL);
    expect(req!.body.remoteMCPServer.metadata.name).toBe(SERVER_NAME);
  });

  test("shows an inline error when the create fails", async ({ page, mock }) => {
    await mock.setMutation("POST", "/api/toolservers", { status: 500, body: { error: "boom" } });
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByLabel("Server Name").fill(SERVER_NAME);
    await page.locator("#url").fill(SERVER_URL);
    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page.getByText("Couldn't create server")).toBeVisible();
    await expect(page).toHaveURL(/\/mcp\/new/);
  });

  test("blocks create when the URL is empty", async ({ page, mock }) => {
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page.getByText("URL is required")).toBeVisible();
    expect((await mock.capturedRequests()).filter((r) => r.method === "POST")).toHaveLength(0);
  });

  test("creates an MCP server via the Command (stdio) tab", async ({ page, mock }) => {
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByRole("tab", { name: "Command" }).click();
    await page.locator("#package-name").fill("my-mcp-package");
    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page).toHaveURL(/\/mcp(\?|$)/);
    const req = await mock.lastRequest<{
      type: string;
      mcpServer: { spec: { transportType: string; deployment: { cmd: string; args: string[] } } };
    }>("POST", "/api/toolservers");
    expect(req).not.toBeNull();
    expect(req!.body.type).toBe("MCPServer");
    expect(req!.body.mcpServer.spec.transportType).toBe("stdio");
    expect(req!.body.mcpServer.spec.deployment.args).toContain("my-mcp-package");
  });

  test("deletes a server", async ({ page, mock }) => {
    await loadPage(page, "/mcp", { heading: "MCP & tools" });

    await page.getByRole("button", { name: "Actions for server default/e2e-tool-server" }).click();
    await page.getByRole("menuitem", { name: "Remove server" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete MCP server")).toBeVisible();
    await dialog.getByRole("button", { name: "Confirm" }).click();

    await expectToast(page, /Server removed/i, { type: "success" });
    expect(
      await mock.lastRequest("DELETE", "/api/toolservers/default/e2e-tool-server"),
    ).not.toBeNull();
  });

  test("filters servers with the search box", async ({ page }) => {
    await loadPage(page, "/mcp", { heading: "MCP & tools" });
    await expect(page.getByText("default/e2e-tool-server")).toBeVisible();

    await page.locator("#mcp-search").fill("zzz");
    await expect(page.getByText("No servers or tools match that filter.")).toBeVisible();

    await page.getByRole("button", { name: "Clear search" }).click();
    await expect(page.getByText("default/e2e-tool-server")).toBeVisible();
  });
});
