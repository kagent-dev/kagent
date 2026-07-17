import { test, expect } from "../../fixtures/test";
import { loadPage } from "../../helpers/page";

// MCP servers & tools — one success journey + one failure journey (two videos),
// run against the real backend.
//
// Success creates a uniquely-named throwaway RemoteMCPServer, confirms it appears
// in the list, exercises the search filter, then deletes it — only ever touching
// the server this test created. The form's namespace combobox auto-selects
// "kagent", so the created server is kagent/<name>.
//
// Only the remote transport is asserted: an MCPServer (stdio) becomes listable
// only once its backing deployment is ready (tens of seconds), too slow for an
// e2e assertion, and the remote path already gives full tool-server CRUD coverage.
//
// Failure covers the client-side url-required validation.

const NAMESPACE = "kagent";
const SERVER_URL = "https://example.com/mcp";

test("mcp server lifecycle: create remote, filter, and delete", async ({ page }, testInfo) => {
  // Generated per attempt (Date.now differs on retry) so re-runs never collide.
  const ref = `${NAMESPACE}/e2e-remote-${Date.now().toString(36)}-${testInfo.retry}`;
  const name = ref.split("/")[1];

  await test.step("creates a remote MCP server", async () => {
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByLabel("Server Name").fill(name);
    await page.locator("#url").fill(SERVER_URL);
    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page).toHaveURL(/\/mcp(\?|$)/);
    await expect(page.getByText(ref)).toBeVisible();
  });

  await test.step("filters servers with the search box", async () => {
    await loadPage(page, "/mcp", { heading: "MCP & tools" });
    await expect(page.getByText(ref)).toBeVisible();

    await page.locator("#mcp-search").fill("zzz-no-such-server");
    await expect(page.getByText("No servers or tools match that filter.")).toBeVisible();

    await page.getByRole("button", { name: "Clear search" }).click();
    await expect(page.getByText(ref)).toBeVisible();
  });

  await test.step("deletes the server it created", async () => {
    await page.getByRole("button", { name: `Actions for server ${ref}` }).click();
    await page.getByRole("menuitem", { name: "Remove server" }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog.getByText("Delete MCP server")).toBeVisible();
    await dialog.getByRole("button", { name: "Confirm" }).click();

    await expect(page.getByText(ref)).toHaveCount(0);
  });
});

test("mcp failures: url validation", async ({ page }) => {
  await test.step("blocks create when the URL is empty", async () => {
    await loadPage(page, "/mcp/new", { heading: "New MCP server" });

    await page.getByLabel("Server Name").fill("e2e-url-validation");
    await page.getByRole("button", { name: "Create server" }).click();

    await expect(page.getByText("URL is required")).toBeVisible();
    await expect(page).toHaveURL(/\/mcp\/new/);
  });
});
