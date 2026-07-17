import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";

// MCP servers & tools — create/read/delete lifecycle journey. The UI has no edit
// surface for a tool server, so there's no Update stage. Creates a uniquely-named
// RemoteMCPServer, finds it via search and expands it, then deletes it — only ever
// touching the server it created. The form's namespace combobox auto-selects
// "kagent", so the server is kagent/<name>.
//
// Only the remote transport is asserted: an MCPServer (stdio) becomes listable
// only once its backing deployment is ready (tens of seconds), too slow for an
// e2e assertion, and the remote path already gives full tool-server coverage.
// Error journeys live in mcp-errors.spec.ts.

const NAMESPACE = "kagent";
const SERVER_URL = "https://example.com/mcp";

test("mcp: create, read, delete", async ({ page }, testInfo) => {
  // Generated per attempt (Date.now differs on retry) so re-runs never collide.
  const ref = `${NAMESPACE}/e2e-remote-${Date.now().toString(36)}-${testInfo.retry}`;
  const name = ref.split("/")[1];

  // region Creating — fill the form and POST a new RemoteMCPServer
  await test.step("creates a remote MCP server", async () => {
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByLabel("Server Name").fill(name);
    await page.locator("#url").fill(SERVER_URL);
    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page).toHaveURL(/\/mcp(\?|$)/);
    await expect(page.getByText(ref)).toBeVisible();
  });

  // region Reading — filter the list to the new server and expand its row
  await test.step("finds the server via search and expands it", async () => {
    await loadPage(page, "/mcp", { heading: "MCP & tools" });

    await page.locator("#mcp-search").fill("zzz-no-such-server");
    await expect(page.getByText("No servers or tools match that filter.")).toBeVisible();

    await page.getByRole("button", { name: "Clear search" }).click();
    await expect(page.getByText(ref)).toBeVisible();
    await page.getByRole("button", { name: new RegExp(`Expand server ${ref}`) }).click();
  });

  // region Deleting — remove the server and confirm the row is gone
  await test.step("deletes the server", async () => {
    await page.getByRole("button", { name: `Actions for server ${ref}` }).click();
    await page.getByRole("menuitem", { name: "Remove server" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete MCP server")).toBeVisible();
    await dialog.getByRole("button", { name: "Confirm" }).click();

    await expect(page.getByText(ref)).toHaveCount(0);
  });
});
